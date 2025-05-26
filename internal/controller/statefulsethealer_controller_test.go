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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitorv1alpha1 "github.com/LouisPouliquen/statefulset-monitor-controller/api/v1alpha1"
)

var _ = Describe("StatefulSetHealer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const stsName = "fake-statefulset"
		ctx := context.Background()

		namespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind StatefulSetHealer")
			healer := &monitorv1alpha1.StatefulSetHealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: monitorv1alpha1.StatefulSetHealerSpec{
					TargetRef:            corev1.LocalObjectReference{Name: stsName},
					MaxRestartAttempts:   3,
					FailureTimeThreshold: "5s",
				},
			}
			Expect(k8sClient.Create(ctx, healer)).To(Succeed())

			By("creating the fake StatefulSet")
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas:    pointerInt32(1),
					ServiceName: "dummy",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "demo"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "dummy",
								Image: "busybox",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sts)).To(Succeed())

			By("creating a pod in CrashLoopBackOff state")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName + "-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "demo"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "dummy",
						Image: "busybox",
					}},
				},
				Status: corev1.PodStatus{
					Reason: "CrashLoopBackOff",
					Phase:  corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					}},
					ContainerStatuses: []corev1.ContainerStatus{{
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
						},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// patch the pod status after creation
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				}},
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		})

		AfterEach(func() {
			By("cleaning up StatefulSetHealer")
			healer := &monitorv1alpha1.StatefulSetHealer{}
			Expect(k8sClient.Get(ctx, namespacedName, healer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, healer)).To(Succeed())

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName,
					Namespace: "default",
				},
			}
			_ = k8sClient.Delete(ctx, sts)
		})

		// Test Case 1: Pod is unhealthy and over threshold → should be deleted
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			reconciler := &StatefulSetHealerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile: sets LastFailureTime
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate passage of time + persisted status
			healer := &monitorv1alpha1.StatefulSetHealer{}
			Expect(k8sClient.Get(ctx, namespacedName, healer)).To(Succeed())
			healer.Status.WatchedPods = []monitorv1alpha1.PodRestartStatus{{
				PodName:         stsName + "-0",
				RestartAttempts: 0,
				LastFailureTime: &metav1.Time{Time: time.Now().Add(-10 * time.Second)},
			}}
			Expect(k8sClient.Status().Update(ctx, healer)).To(Succeed())

			// Second reconcile: should detect failureTimeThreshold crossed
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// simulate time passing + status persistence
			time.Sleep(6 * time.Second)
			healer = &monitorv1alpha1.StatefulSetHealer{}
			Expect(k8sClient.Get(ctx, namespacedName, healer)).To(Succeed())
			healer.Status.WatchedPods = []monitorv1alpha1.PodRestartStatus{{
				PodName:         stsName + "-0",
				RestartAttempts: 0,
				LastFailureTime: &metav1.Time{Time: time.Now().Add(-10 * time.Second)},
			}}
			Expect(k8sClient.Status().Update(ctx, healer)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Asserting the pod was deleted")
			pod := &corev1.Pod{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: stsName + "-0", Namespace: "default"}, pod)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		// Test Case 2: Pod is unhealthy but NOT over threshold → should NOT be deleted
		It("should not delete pod if below failure threshold", func() {
			By("Reconciling the resource without exceeding threshold")
			reconciler := &StatefulSetHealerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Trigger first reconcile to populate LastFailureTime
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Set LastFailureTime to 1s ago (threshold is 5s)
			healer := &monitorv1alpha1.StatefulSetHealer{}
			Expect(k8sClient.Get(ctx, namespacedName, healer)).To(Succeed())
			healer.Status.WatchedPods = []monitorv1alpha1.PodRestartStatus{{
				PodName:         stsName + "-0",
				RestartAttempts: 0,
				LastFailureTime: &metav1.Time{Time: time.Now().Add(-1 * time.Second)},
			}}
			Expect(k8sClient.Status().Update(ctx, healer)).To(Succeed())

			// Trigger reconcile again (should NOT delete pod)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Asserting the pod was NOT deleted")
			pod := &corev1.Pod{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: stsName + "-0", Namespace: "default"}, pod)
			Expect(err).To(Succeed()) // Pod should still exist
		})
	})
})

func pointerInt32(i int32) *int32 {
	return &i
}
