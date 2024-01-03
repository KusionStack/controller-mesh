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

type StringMatch struct {
	MatchType StringMatchType `json:"matchType,omitempty"`
	Value     string          `json:"value,omitempty"`
}

type StringMatchType string

type HTTPFaultInjectionDelay struct {
	// FixedDelay is used to indicate the amount of delay in seconds.
	FixedDelay string `json:"fixedDelay,omitempty"`
	// Percent of requests on which the delay will be injected.
	// If left unspecified, no request will be delayed
	Percent string `json:"percent,omitempty"`
}

type HTTPFaultInjectionAbort struct {
	// HttpStatus is used to indicate the HTTP status code to
	// return to the caller.
	HttpStatus int `json:"httpStatus,omitempty"`
	// Percent of requests to be aborted with the error code provided.
	// If not specified, no request will be aborted.
	Percent string `json:"percent,omitempty"`
}

type ResourceMatch struct {
	ApiGroups  []string `json:"apiGroups,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Resources  []string `json:"resources,omitempty"`
	Verbs      []string `json:"verbs,omitempty"`
}

// MultiRestRule specifies the criteria for matching HTTP requests to RESTful resources
// as part of HTTP FaultInjection. Each rule can target one or more URLs and HTTP methods.
type MultiRestRule struct {
	// URL gives the location of the rest request, in standard URL form (`scheme://host:port/path`)
	URL []string `json:"url"`
	// Method specifies the http method of the request, like: PUT, POST, GET, DELETE.
	Method []string `json:"method"`
}

// HTTPMatchRequest defines a set of rules and criteria for matching incoming HTTP requests.
// It associates a name with the set of criteria and can relate to additional resources that are relevant
// to the request matching, enabling complex and granular control over request matching in HTTP FaultInjection.
type HTTPMatchRequest struct {
	Name             string           `json:"name,omitempty"`
	RelatedResources []*ResourceMatch `json:"relatedResources,omitempty"`

	RestRules []*MultiRestRule `json:"restRules,omitempty"`
}

// HTTPFaultInjection can be used to specify one or more faults to inject
// while forwarding HTTP requests to the destination specified in a route.
type HTTPFaultInjection struct {
	// Name is the name of the policy
	Name string `json:"name,omitempty"`
	// Delay requests before forwarding, emulating various failures such as
	// network issues, overloaded upstream service, etc.
	Delay *HTTPFaultInjectionDelay `json:"delay,omitempty"`
	// Abort Http request attempts and return error codes back to downstream
	// service, giving the impression that the upstream service is faulty.
	Abort *HTTPFaultInjectionAbort `json:"abort,omitempty"`

	// Match specifies a set of criterion to be met in order for the
	// rule to be applied to the HTTP request.
	Match *HTTPMatchRequest `json:"match,omitempty"`
	// Effective time of fault injection
	EffectiveTime *EffectiveTimeRange `json:"effectiveTime,omitempty"`
}

type FaultInjectionSpec struct {
	// Selector is a label query over pods of this configuration.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	Disabled bool `json:"disabled,omitempty"`

	HTTPFaultInjections []*HTTPFaultInjection `json:"httpFault,omitempty"`
}

type EffectiveTimeRange struct {
	// StartTime is the starting time of fault injection.
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the ending time of fault injection.
	EndTime string `json:"endTime,omitempty"`

	// DaysOfWeek specifies on which days of the week the fault injection configuration is effective.
	// 0 represents Sunday, 1 represents Monday, and so on.
	DaysOfWeek []int `json:"daysOfWeek,omitempty"`

	// DaysOfMonth specifies on which days of the month the fault injection configuration is effective.
	// For example, 1 represents the first day of the month, and so on.
	DaysOfMonth []int `json:"daysOfMonth,omitempty"`

	// Months specifies in which months of the year the fault injection configuration is effective.
	// 1 represents January, 2 represents February, and so on.
	Months []int `json:"months,omitempty"`
}

// FaultInjectionState is the status of the fault injection, which may be 'Opened' or 'Closed'.
type FaultInjectionState string

type FaultInjectionStatus struct {
	ObservedGeneration int64                         `json:"observedGeneration,omitempty"`
	LastUpdatedTime    *metav1.Time                  `json:"lastUpdatedTime,omitempty"`
	CurrentSpecHash    string                        `json:"currentSpecHash,omitempty"`
	TargetStatus       []*FaultInjectionTargetStatus `json:"faultinjectiontargetStatus,omitempty"`
}

type FaultInjectionTargetStatus struct {
	PodName    string `json:"podName,omitempty"`
	PodIP      string `json:"podIP,omitempty"`
	ConfigHash string `json:"configHash,omitempty"`
	Message    string `json:"message,omitempty"`
}

// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fj

type FaultInjection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FaultInjectionSpec   `json:"spec,omitempty"`
	Status FaultInjectionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FaultInjectionList contains a list of FaultInjection
type FaultInjectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FaultInjection `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FaultInjection{}, &FaultInjectionList{})
}
