package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func isPodUnhealthy(pod corev1.Pod) bool {
	// 1. Pod is not ready
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			return true
		}
	}

	// 2. Pod phase is Failed or Unknown
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodUnknown {
		return true
	}

	// 3. Container failure reasons
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "CrashLoopBackOff" ||
				reason == "ImagePullBackOff" ||
				reason == "ErrImagePull" ||
				reason == "RunContainerError" {
				return true
			}
		}
		if cs.LastTerminationState.Terminated != nil {
			// Check for OOMKilled or frequent restarts
			term := cs.LastTerminationState.Terminated
			if term.Reason == "OOMKilled" {
				return true
			}
		}
	}

	// 4. Init container failures
	for _, init := range pod.Status.InitContainerStatuses {
		if init.State.Terminated != nil {
			if init.State.Terminated.ExitCode != 0 {
				return true
			}
		}
		if init.State.Waiting != nil {
			reason := init.State.Waiting.Reason
			if reason == "CrashLoopBackOff" ||
				reason == "ErrImagePull" ||
				reason == "ImagePullBackOff" {
				return true
			}
		}
	}

	return false
}

// generateStatefulSetPodNames returns the list of pod names given a StatefulSet name and replica count.
func generateStatefulSetPodNames(statefulSetName string, replicas int32) []string {
	podNames := make([]string, 0, replicas)
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", statefulSetName, i)
		podNames = append(podNames, podName)
	}
	return podNames
}
