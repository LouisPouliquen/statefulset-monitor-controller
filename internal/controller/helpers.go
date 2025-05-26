package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func isPodUnhealthy(pod corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			return true
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			return true
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
