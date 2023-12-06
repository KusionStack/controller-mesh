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

type FaultInjectionSpec struct {
	// Selector is a label query over pods of this configuration.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	Disabled bool `json:"disabled,omitempty"`

	HTTPFault []HTTPFaultInjection `json:"httpFault,omitempty"`
}

// HTTPFaultInjection can be used to specify one or more faults to inject
// while forwarding HTTP requests to the destination specified in a route.
type HTTPFaultInjection struct {
	// Delay requests before forwarding, emulating various failures such as
	// network issues, overloaded upstream service, etc.
	Delay *HTTPFaultInjectionDelay `json:"delay,omitempty"`
	// Abort Http request attempts and return error codes back to downstream
	// service, giving the impression that the upstream service is faulty.
	Abort *HTTPFaultInjectionAbort `json:"abort,omitempty"`

	// Match specifies a set of criterion to be met in order for the
	// rule to be applied to the HTTP request.
	Match *HTTPMatchRequest `json:"match,omitempty"`
}

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
	HttpStatus int32 `json:"httpStatus,omitempty"`
	// Percent of requests to be aborted with the error code provided.
	// If not specified, no request will be aborted.
	Percent string `json:"percent,omitempty"`
}

type HTTPMatchRequest struct {
	RelatedResources []*ResourceMatch `json:"relatedResources,omitempty"`

	// TODO: http match
	Url    []*StringMatch `json:"url,omitempty"`
	Method []string       `json:"method,omitempty"`
}

type ResourceMatch struct {
	ApiGroups  []string `json:"apiGroups,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Resources  []string `json:"resources,omitempty"`
	Verbs      []string `json:"verbs,omitempty"`
}

type StringMatch struct {
	MatchType StringMatchType `json:"matchType,omitempty"`
	Value     string          `json:"value,omitempty"`
}

type StringMatchType string

const (
	ExactMatchType  StringMatchType = "Exact"
	PrefixMatchType StringMatchType = "Prefix"
	RegexMatchType  StringMatchType = "Regex"
)

type FaultInjectionStatus struct {
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
