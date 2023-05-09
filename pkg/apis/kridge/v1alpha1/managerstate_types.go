/*
Copyright 2023 The KusionStack Authors.
Modified from Kruise code, Copyright 2021 The Kruise Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NameOfManager = "kridge-manager"
)

// ManagerStateSpec defines the desired state of ManagerState
type ManagerStateSpec struct {
}

// ManagerStateStatus defines the observed state of ManagerState
type ManagerStateStatus struct {
	Namespace string                `json:"namespace,omitempty"`
	Endpoints ManagerStateEndpoints `json:"endpoints,omitempty"`
	Ports     *ManagerStatePorts    `json:"ports,omitempty"`
}

type ManagerStateEndpoints []ManagerStateEndpoint

func (e ManagerStateEndpoints) Len() int      { return len(e) }
func (e ManagerStateEndpoints) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e ManagerStateEndpoints) Less(i, j int) bool {
	return e[i].Name < e[j].Name
}

type ManagerStateEndpoint struct {
	Name   string `json:"name"`
	PodIP  string `json:"podIP"`
	Leader bool   `json:"leader"`
}

type ManagerStatePorts struct {
	GrpcLeaderElectionPort    int `json:"grpcLeaderElectionPort,omitempty"`
	GrpcNonLeaderElectionPort int `json:"grpcNonLeaderElectionPort,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:openapi-gen=true
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ManagerState is the Schema for the managerstates API
type ManagerState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagerStateSpec   `json:"spec,omitempty"`
	Status ManagerStateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ManagerStateList contains a list of ManagerState
type ManagerStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagerState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagerState{}, &ManagerStateList{})
}
