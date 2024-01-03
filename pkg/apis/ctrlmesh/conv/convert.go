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

package conv

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

const (
	AllSubsetPublic = "AllSubsetPublic"
	ResourceAll     = "*"
)

type ResourceRequest struct {
	GR             schema.GroupResource  `json:"GR,omitempty"`
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty"`
}

func ConvertProtoSpecToInternal(protoSpec *proto.ProxySpec) *InternalSpec {
	is := &InternalSpec{ProxySpec: protoSpec, routeInternal: &internalRoute{subsetLimits: map[string][]*internalMatchLimitRule{}}}
	if r := protoSpec.Limits; r != nil {
		is.routeInternal.currentLimits = convertProtoMatchLimitRuleToInternal(r)
	}

	if eps := protoSpec.Endpoints; eps != nil {
		for i := range eps {
			if eps[i].Limits == nil {
				continue
			}
			is.routeInternal.subsetLimits[eps[i].ShardName] = convertProtoMatchLimitRuleToInternal(eps[i].Limits)
		}
	}
	return is
}

func convertProtoMatchLimitRuleToInternal(limits []*proto.Limit) []*internalMatchLimitRule {
	var internalLimits []*internalMatchLimitRule
	for _, limit := range limits {

		if len(limit.ObjectSelector) > 0 {
			selector := &metav1.LabelSelector{}
			if err := json.Unmarshal([]byte(limit.ObjectSelector), selector); err != nil {
				klog.Errorf("fail to unmarshal ObjectSelector %s", limit.ObjectSelector)
			}
			internalLimits = append(internalLimits, &internalMatchLimitRule{
				resources:      limit.Resources,
				objectSelector: selector,
			})
		} else {
			internalLimits = append(internalLimits, &internalMatchLimitRule{
				resources: limit.Resources,
			})
		}
	}
	return internalLimits
}

type InternalSpec struct {
	*proto.ProxySpec
	routeInternal *internalRoute
}

func (is *InternalSpec) GetObjectSelector(gr schema.GroupResource) (sel *metav1.LabelSelector) {
	return is.routeInternal.getObjectSelector(gr)
}

func (is *InternalSpec) GetMatchedSubsetEndpoint(ns string, gr schema.GroupResource) (ignore bool, self bool, hosts []string) {

	// TODO: how to route webhook request by object selector?

	return false, false, hosts
}

type internalRoute struct {
	currentLimits []*internalMatchLimitRule
	subsetLimits  map[string][]*internalMatchLimitRule

	currentCircuitBreaker []*internalCircuitBreaker
}

type internalCircuitBreaker struct {
	RateLimitings         []*ctrlmeshv1alpha1.Limiting
	TrafficInterceptRules []*ctrlmeshv1alpha1.TrafficInterceptRule
}

type internalMatchLimitRule struct {
	objectSelector *metav1.LabelSelector
	resources      []*proto.APIGroupResource
}

func (ir *internalRoute) getObjectSelector(gr schema.GroupResource) (sel *metav1.LabelSelector) {
	for _, limit := range ir.currentLimits {
		if limit.objectSelector == nil {
			continue
		}

		if len(limit.resources) > 0 && !isGRMatchedAPIResources(gr, limit.resources) {
			continue
		}
		sel = utils.MergeLabelSelector(sel, limit.objectSelector)
	}
	return
}

func isGRMatchedAPIResources(gr schema.GroupResource, resources []*proto.APIGroupResource) bool {
	for _, r := range resources {
		if containsString(gr.Group, r.ApiGroups, ResourceAll) && containsString(gr.Resource, r.Resources, ResourceAll) {
			return true
		}
	}
	return false
}

// containsString returns true if either `x` or `wildcard` is in
// `list`.  The wildcard is not a pattern to match against `x`; rather
// the presence of the wildcard in the list is the caller's way of
// saying that all values of `x` should match the list.  This function
// assumes that if `wildcard` is in `list` then it is the only member
// of the list, which is enforced by validation.
func containsString(x string, list []string, wildcard string) bool {
	if len(list) == 1 && list[0] == wildcard {
		return true
	}
	for _, y := range list {
		if x == y {
			return true
		}
	}
	return false
}
