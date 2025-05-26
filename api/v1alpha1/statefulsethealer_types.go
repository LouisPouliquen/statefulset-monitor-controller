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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatefulSetHealerSpec defines the desired state of StatefulSetHealer.
type StatefulSetHealerSpec struct {
	// TargetRef points to the StatefulSet to be monitored.
	// It must be in the same namespace as this CR.
	// +kubebuilder:validation:Required
	TargetRef corev1.LocalObjectReference `json:"targetRef"`

	// MaxRestartAttempts defines how many times a pod can be restarted
	// before it is considered permanently failed.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=3
	MaxRestartAttempts int `json:"maxRestartAttempts"`

	// FailureTimeThreshold is the duration a pod can stay unhealthy
	// (e.g. NotReady or CrashLoopBackOff) before being restarted.
	// Duration should be in the format "30s", "5m", "1h".
	// +kubebuilder:validation:Pattern=`^[0-9]+(s|m|h)$`
	// +kubebuilder:default:="5m"
	FailureTimeThreshold string `json:"failureTimeThreshold"`
}

type PodRestartStatus struct {
	PodName         string       `json:"podName"`
	RestartAttempts int          `json:"restartAttempts"`
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`
}

// StatefulSetHealerStatus defines the observed state of StatefulSetHealer.
type StatefulSetHealerStatus struct {
	WatchedPods  []PodRestartStatus `json:"watchedPods,omitempty"`
	LastScanTime metav1.Time        `json:"lastScanTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// StatefulSetHealer is the Schema for the statefulsethealers API.
type StatefulSetHealer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StatefulSetHealerSpec   `json:"spec,omitempty"`
	Status StatefulSetHealerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StatefulSetHealerList contains a list of StatefulSetHealer.
type StatefulSetHealerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StatefulSetHealer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StatefulSetHealer{}, &StatefulSetHealerList{})
}
