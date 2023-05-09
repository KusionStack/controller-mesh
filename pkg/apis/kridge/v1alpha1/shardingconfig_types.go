/*
Copyright 2023 The KusionStack Authors.

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
	ShardingConfigInjectedKey = "kridge.kusionstack.io/sharding-config-injected"
	SignalAll                 = "*"
)

// ShardingConfigSpec defines the desired state of ShardingConfig
type ShardingConfigSpec struct {
	// Selector is a label query over pods of this configuration.
	Selector *metav1.LabelSelector `json:"selector"`

	Controller *ShardingConfigControllerConfiguration `json:"controller,omitempty"`

	Webhook *ShardingConfigWebhookConfiguration `json:"webhook,omitempty"`

	Limits []ObjectLimiter `json:"limits,omitempty"`

	Root *ShardingConfigRoot `json:"root,omitempty"`
}

type ShardingConfigRoot struct {
	Disable           *bool           `json:"disable,omitempty"`
	Prefix            string          `json:"prefix,omitempty"`
	Canary            *CanaryConfig   `json:"canary,omitempty"`
	Size              int             `json:"size,omitempty"`
	Type              ShardingType    `json:"type,omitempty"`
	Manual            []ManualConfig  `json:"manual,omitempty"`
	CanaryReplicas    int             `json:"canaryReplicas,omitempty"`
	NormalReplicas    int             `json:"normalReplicas,omitempty"`
	TargetStatefulSet string          `json:"targetStatefulSet,omitempty"`
	ResourceSelector  []ObjectLimiter `json:"resourceSelector,omitempty"`
}

type CanaryConfig struct {
	InNamespaces []string `json:"inNamespaces,omitempty"`
	InNumbers    []string `json:"inNumbers,omitempty"`
}

type ManualConfig struct {
	ID      int      `json:"id"`
	Numbers []string `json:"numbers"`
}

type ShardingType string

const (
	ShardingAuto   ShardingType = "Auto"
	ShardingManual ShardingType = "Manual"
)

// ShardingConfigRestConfigOverrides defines overrides to the application's rest config.
type ShardingConfigRestConfigOverrides struct {
	// UserAgentOrPrefix can override the UserAgent of application.
	// If it ends with '/', we consider it as prefix and will be add to the front of original UserAgent.
	// Otherwise it will replace the original UserAgent.
	UserAgentOrPrefix *string `json:"userAgentOrPrefix,omitempty"`
}

// ShardingConfigControllerConfiguration defines the configuration of controller in this application.
type ShardingConfigControllerConfiguration struct {
	LeaderElectionName string `json:"leaderElectionName"`
}

// ShardingConfigWebhookConfiguration defines the configuration of webhook in this application.
type ShardingConfigWebhookConfiguration struct {
	CertDir string `json:"certDir"`
	Port    int    `json:"port"`
}

type ObjectLimiter struct {
	RelatedResources []ResourceGroup       `json:"relateResources,omitempty"`
	Selector         *metav1.LabelSelector `json:"selector,omitempty"`
}

type ResourceGroup struct {
	Resources []string `json:"resources,omitempty"`
	APIGroups []string `json:"apiGroups,omitempty"`
}

// ShardingConfigStatus defines the observed state of ShardingConfig
type ShardingConfigStatus struct {
	Root RootStatus `json:"root,omitempty"`
}

type RootStatus struct {
	Child []string `json:"childShardingConfigs,omitempty"`
}

// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=shard

// ShardingConfig is the Schema for the ShardingConfigs API
type ShardingConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShardingConfigSpec   `json:"spec,omitempty"`
	Status ShardingConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ShardingConfigList contains a list of ShardingConfig
type ShardingConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShardingConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ShardingConfig{}, &ShardingConfigList{})
}
