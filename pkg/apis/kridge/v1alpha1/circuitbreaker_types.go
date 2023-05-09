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

// ResourceRule defines the target k8s resource of the limiting policy
type ResourceRule struct {
	// APIGroups is the name of the APIGroup that contains the resources.  If multiple API groups are specified, any action requested against one of
	// the enumerated resources in any API group will be allowed.  "*" means all.
	ApiGroups []string `json:"apiGroups"`
	// Resources is a list of resources this rule applies to.  "*" means all in the specified apiGroups.
	//  "*/foo" represents the subresource 'foo' for all resources in the specified apiGroups.
	Resources []string `json:"resources"`
	// Verb is a list of kubernetes resource API verbs, like: get, list, watch, create, update, delete, proxy.  "*" means all.
	Verbs []string `json:"verbs"`
	// Namespaces is a list of namespaces the rule applies to. "*" means all.
	Namespaces []string `json:"namespaces"`
}

// RestRule defines the target rest resource of the limiting policy
type RestRule struct {
	// URL gives the location of the rest request, in standard URL form (`scheme://host:port/path`)
	URL string `json:"url"`
	// Method specifies the http method of the request, like: PUT, POST, GET, DELETE.
	Method string `json:"method"`
}

// TriggerPolicy defines how the circuit-breaking policy triggered from 'Closed' to 'Opened'
type TriggerPolicy string

var (
	TriggerPolicyNormal      TriggerPolicy = "Normal"
	TriggerPolicyLimiterOnly TriggerPolicy = "LimiterOnly"
	TriggerPolicyForceOpened TriggerPolicy = "ForceOpened"
	TriggerPolicyForceClosed TriggerPolicy = "ForceClosed"
)

// RecoverPolicy defines how the circuit-breaking policy recovered from 'Opened' to 'Closed'
type RecoverPolicy string

var (
	RecoverPolicyManual         RecoverPolicy = "Manual"
	RecoverPolicySleepingWindow RecoverPolicy = "SleepingWindow"
)

// InterceptType defines how the circuit-breaking traffic intercept from 'White' to 'Black'
type InterceptType string

var (
	InterceptTypeWhite InterceptType = "White"
	InterceptTypeBlack InterceptType = "Black"
)

// ContentType defines how the circuit-breaking traffic intercept content type from 'Normal' to 'Regexp'
type ContentType string

var (
	ContentTypeNormal ContentType = "Normal"
	ContentTypeRegexp ContentType = "Regexp"
)

type ValidatePolicy string

var (
	AfterHttpSuccess ValidatePolicy = "AfterHttpSuccess"
)

// Bucket defines the whole token bucket of the policy
type Bucket struct {
	// Burst is the max token number of the bucket
	Burst uint32 `json:"burst"`
	// Interval is the time interval of the limiting policy, in format of time like: 1h, 3m, 5s.
	Interval string `json:"interval"`
	// Limit is the token number of the limiting policy.
	Limit uint32 `json:"limit"`
}

// Limiting defines the limit policy
type Limiting struct {
	// Name is the name of the policy
	Name string `json:"name"`
	// ResourceRules defines the target k8s resource of the limiting policy
	ResourceRules []ResourceRule `json:"resourceRules,omitempty"`
	// RestRules defines the target rest resource of the limiting policy
	RestRules []RestRule `json:"restRules,omitempty"`
	// Bucket defines the whole token bucket of the policy
	Bucket Bucket `json:"bucket"`
	// TriggerPolicy defines how the circuit-breaking policy triggered from 'Closed' to 'Opened'
	TriggerPolicy TriggerPolicy `json:"triggerPolicy"`
	// RecoverPolicy defines how the circuit-breaking policy recovered from 'Opened' to 'Closed'
	RecoverPolicy RecoverPolicy `json:"recoverPolicy"`
	// ValidatePolicy determine the opportunity to validate req
	ValidatePolicy ValidatePolicy `json:"validatePolicy,omitempty"`
	// Properties defines the additional properties of the policy, like: SleepingWindowSize
	Properties map[string]string `json:"properties,omitempty"`
}

// TrafficInterceptRule defines the traffic intercept rule
type TrafficInterceptRule struct {
	// Name is the name of the traffic rule
	Name string `json:"name"`
	// InterceptType is the intercept type of the traffic rule
	InterceptType InterceptType `json:"interceptType"`
	// ContentType is the content type of the traffic rule
	ContentType ContentType `json:"contentType"`
	// Content is the content of the traffic rule
	Contents []string `json:"contents"`
	// Method specifies the http method of the request, like: PUT, POST, GET, DELETE.
	Methods []string `json:"methods"`
}

// CircuitBreakerSpec defines the desired state of CircuitBreaker
type CircuitBreakerSpec struct {
	// Selector is a label query over pods of this application.
	Selector TargetSelector `json:"selector,omitempty"`
	// RateLimitings defines the limit policies
	RateLimitings []Limiting `json:"rateLimitings"`
	// TrafficInterceptRules defines the traffic rules
	TrafficInterceptRules []TrafficInterceptRule `json:"trafficInterceptRules,omitempty"`
}

type TargetSelector struct {
	Targets       []string              `json:"targets,omitempty"`
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// BreakerStatus is the status of the circuit breaker, which may be 'Opened' or 'Closed'.
type BreakerStatus string

var (
	BreakerStatusOpened BreakerStatus = "Opened"
	BreakerStatusClosed BreakerStatus = "Closed"
)

// BucketSnapshot defines the whole snapshot of the token bucket
type BucketSnapshot struct {
	// AvailableTokens defines the rest tokens of the bucket.
	AvailableTokens uint64 `json:"availableTokens"`
	// LastAcquireTimestamp is the unix timestamp that the last token(s) were acquired.
	LastAcquireTimestamp uint64 `json:"lastAcquireTimestamp"`
}

// LimitingSnapshot defines the snapshot of the whole limiting policy
type LimitingSnapshot struct {
	// Name specifies the name of the policy
	Name string `json:"name"`
	// Endpoint specifies the users who use this rule
	Endpoint string `json:"endpoint,omitempty"`
	// PodName specifies the users pod name
	PodName string `json:"podName,omitempty"`
	// Bucket defines the whole snapshot of the token bucket
	Bucket BucketSnapshot `json:"bucket"`
	// Status is the status of the circuit breaker, which may be 'Opened' or 'Closed'.
	Status BreakerStatus `json:"status"`
	// LastTransitionTime is the last time that the status changed
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// CircuitBreakerStatus defines the observed state of CircuitBreaker
type CircuitBreakerStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastUpdatedTime    metav1.Time        `json:"lastUpdatedTime,omitempty"`
	LimitingSnapshots  []LimitingSnapshot `json:"limitingSnapshots"`
}

// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cbk

// CircuitBreaker is the Schema for the circuitbreakers API
type CircuitBreaker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CircuitBreakerSpec   `json:"spec,omitempty"`
	Status CircuitBreakerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CircuitBreakerList contains a list of CircuitBreaker
type CircuitBreakerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CircuitBreaker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CircuitBreaker{}, &CircuitBreakerList{})
}

func (in *CircuitBreakerStatus) UpdateSnapshots(podIp string, snapshot []LimitingSnapshot) {
	var newSnapshot []LimitingSnapshot
	for _, sp := range snapshot {
		newSnapshot = append(newSnapshot, *sp.DeepCopy())
	}
	for _, sp := range in.LimitingSnapshots {
		if sp.Endpoint != podIp {
			newSnapshot = append(newSnapshot, *sp.DeepCopy())
		}
	}
	in.LimitingSnapshots = newSnapshot
}
