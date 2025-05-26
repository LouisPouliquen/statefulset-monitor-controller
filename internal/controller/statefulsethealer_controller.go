/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	monitorv1alpha1 "github.com/LouisPouliquen/statefulset-monitor-controller/api/v1alpha1"
	"github.com/go-logr/logr"
)

// StatefulSetHealerReconciler reconciles a StatefulSetHealer object
type StatefulSetHealerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=monitor.cluster-tools.dev,resources=statefulsethealers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitor.cluster-tools.dev,resources=statefulsethealers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitor.cluster-tools.dev,resources=statefulsethealers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch

// Reconcile handles reconciliation logic for StatefulSetHealer resources.
// It loads the associated StatefulSet and monitored pods, checks pod health,
// deletes unhealthy pods, and updates the healer status accordingly.
func (r *StatefulSetHealerReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("StatefulSetHealer", req.NamespacedName)

	healer, sts, threshold, now, err := r.fetchResources(ctx, req, logger)
	if err != nil {
		return reconcile.Result{}, err
	}

	updatedStatus, err := r.reconcilePods(ctx, healer, sts, threshold, now, logger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Update the healer status with retry on conflict
	for i := 0; i < 3; i++ {
		healer.Status.WatchedPods = updatedStatus
		healer.Status.LastScanTime = metav1.Now()

		err := r.Status().Update(ctx, &healer)
		if err == nil {
			logger.V(1).Info("Healer status updated successfully")
			break
		}

		if errors.IsConflict(err) {
			logger.V(1).Info("Conflict on status update, retrying", "attempt", i+1)
			if err := r.Get(ctx, req.NamespacedName, &healer); err != nil {
				logger.Error(err, "Failed to re-fetch StatefulSetHealer after conflict")
				return reconcile.Result{}, err
			}
			continue
		} else {
			logger.Error(err, "Failed to update StatefulSetHealer status")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

// fetchInputs retrieves the StatefulSetHealer resource and the target StatefulSet,
// parses the failure threshold duration, and captures the current timestamp.
func (r *StatefulSetHealerReconciler) fetchResources(ctx context.Context, req reconcile.Request, logger logr.Logger) (monitorv1alpha1.StatefulSetHealer, appsv1.StatefulSet, time.Duration, metav1.Time, error) {
	var healer monitorv1alpha1.StatefulSetHealer
	if err := r.Get(ctx, req.NamespacedName, &healer); err != nil {
		logger.V(1).Info("Healer resource not found or deleted")
		return healer, appsv1.StatefulSet{}, 0, metav1.Time{}, client.IgnoreNotFound(err)
	}
	logger.V(1).Info("Healer resource fetched", "targetRef", healer.Spec.TargetRef.Name)

	var sts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKey{Namespace: healer.Namespace, Name: healer.Spec.TargetRef.Name}, &sts); err != nil {
		logger.Error(err, "Failed to get target StatefulSet")
		return healer, sts, 0, metav1.Time{}, err
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	logger.V(1).Info("Target StatefulSet found", "replicas", replicas)

	threshold, err := time.ParseDuration(healer.Spec.FailureTimeThreshold)
	if err != nil {
		logger.Error(err, "Invalid failureTimeThreshold format", "value", healer.Spec.FailureTimeThreshold)
		return healer, sts, 0, metav1.Time{}, err
	}

	return healer, sts, threshold, metav1.Now(), nil
}

// reconcilePods monitors all pods in the target StatefulSet and updates their restart status
// based on their health. The function follows these core steps:
//
// 1. Build a map of the current pod restart statuses from the StatefulSetHealer's Status.
// 2. For each pod in the StatefulSet:
//   - Check if the pod is unhealthy (e.g., CrashLoopBackOff or NotReady).
//   - If unhealthy:
//     a. Record the first failure timestamp if it's the first time.
//     b. Check how long the pod has been unhealthy.
//     c. If the unhealthy duration exceeds the configured threshold:
//   - If the pod has not reached the maximum restart attempts, delete it to trigger recreation.
//   - If it has reached the limit, do nothing further.
//   - If healthy:
//     a. If the pod was previously unhealthy, check how long it's been healthy.
//     b. If it has been healthy longer than the threshold, reset the restart count.
//
// 3. Save the updated pod status back into the status map.
// 4. Return the updated list of PodRestartStatus, which will be stored in the StatefulSetHealer status.
//
// This mechanism ensures that pods are restarted only after a sustained failure
// and are not restarted indefinitely. It also ensures that restart history is preserved
// across reconciliations and cleared only after a full recovery period.
func (r *StatefulSetHealerReconciler) reconcilePods(ctx context.Context, healer monitorv1alpha1.StatefulSetHealer, sts appsv1.StatefulSet, threshold time.Duration, now metav1.Time, logger logr.Logger) ([]monitorv1alpha1.PodRestartStatus, error) {
	// Build a map of existing pod statuses
	statusMap := make(map[string]monitorv1alpha1.PodRestartStatus)
	for _, s := range healer.Status.WatchedPods {
		statusMap[s.PodName] = s
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	podNames := generateStatefulSetPodNames(sts.Name, replicas)
	logger.V(1).Info("Checking pods", "count", len(podNames))

	for _, podName := range podNames {
		var pod corev1.Pod
		if err := r.Get(ctx, client.ObjectKey{Namespace: healer.Namespace, Name: podName}, &pod); err != nil {
			if errors.IsNotFound(err) {
				logger.V(1).Info("Pod not found, skipping", "pod", podName)
				continue
			}
			logger.Error(err, "Failed to fetch pod", "pod", podName)
			return nil, err
		}

		isUnhealthy := isPodUnhealthy(pod)
		prev := statusMap[podName]

		if isUnhealthy {
			logger.V(1).Info("Pod is unhealthy", "pod", podName)

			if prev.LastFailureTime == nil {
				prev.LastFailureTime = &now
				logger.V(1).Info("First failure timestamp recorded", "pod", podName)
			}

			duration := now.Sub(prev.LastFailureTime.Time)
			logger.V(1).Info("Unhealthy duration", "pod", podName, "duration", duration.String())

			if duration > threshold {
				if prev.RestartAttempts < healer.Spec.MaxRestartAttempts {
					logger.Info("Deleting pod for recovery", "pod", podName, "attempts", prev.RestartAttempts)
					if err := r.Delete(ctx, &pod); err != nil {
						logger.Error(err, "Failed to delete pod", "pod", podName)
					} else {
						prev.RestartAttempts++
						prev.LastFailureTime = &now // reset failure timer
						logger.Info("Pod deleted successfully", "pod", podName, "newAttempts", prev.RestartAttempts)
					}
				} else {
					logger.Info("Max restart attempts reached, skipping deletion", "pod", podName, "attempts", prev.RestartAttempts)
				}
			}
		} else {
			// Pod is currently healthy
			if prev.LastFailureTime != nil {
				duration := now.Sub(prev.LastFailureTime.Time)
				if duration > threshold {
					logger.V(1).Info("Pod recovered after threshold, resetting status", "pod", podName)
					prev.RestartAttempts = 0
					prev.LastFailureTime = nil
				} else {
					logger.V(1).Info("Pod not unhealthy anymore, but not yet stable", "pod", podName, "durationSinceRecovery", duration.String())
				}
			}
			// else: pod was always healthy, keep status as is
		}

		// Save updated status
		statusMap[podName] = monitorv1alpha1.PodRestartStatus{
			PodName:         podName,
			RestartAttempts: prev.RestartAttempts,
			LastFailureTime: prev.LastFailureTime,
		}
	}

	// Rebuild updatedStatus slice
	var updatedStatus []monitorv1alpha1.PodRestartStatus
	for _, podName := range podNames {
		updatedStatus = append(updatedStatus, statusMap[podName])
	}

	return updatedStatus, nil
}

func (r *StatefulSetHealerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitorv1alpha1.StatefulSetHealer{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.mapPodToHealer),
		).
		Complete(r)
}

// mapPodToHealer maps a Pod event to StatefulSetHealer reconcile requests.
// It checks if the pod name matches the target StatefulSet of any healer
// and returns reconcile requests for the matched healers.
func (r *StatefulSetHealerReconciler) mapPodToHealer(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx).WithValues("source", "mapPodToHealer")

	pod := obj.(*corev1.Pod)
	logger.V(1).Info("Received pod event", "pod", pod.Name, "namespace", pod.Namespace, "uid", pod.UID, "resourceVersion", pod.ResourceVersion, "phase", pod.Status.Phase)

	var healers monitorv1alpha1.StatefulSetHealerList
	if err := r.List(ctx, &healers, client.InNamespace(pod.Namespace)); err != nil {
		logger.Error(err, "Failed to list StatefulSetHealer resources")
		return nil
	}

	var requests []reconcile.Request
	for _, healer := range healers.Items {
		logger.V(1).Info("Inspecting healer", "healer", healer.Name, "targetRef", healer.Spec.TargetRef.Name)

		if healer.Spec.TargetRef.Name != "" &&
			strings.HasPrefix(pod.Name, healer.Spec.TargetRef.Name+"-") {

			logger.V(1).Info("Matched pod to healer", "pod", pod.Name, "healer", healer.Name)

			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name:      healer.Name,
					Namespace: healer.Namespace,
				},
			})
		} else {
			logger.V(2).Info("Pod does not match healer", "pod", pod.Name, "targetRef", healer.Spec.TargetRef.Name)
		}
	}

	logger.V(1).Info("Mapped pod to reconcile requests", "pod", pod.Name, "matches", len(requests))
	return requests
}
