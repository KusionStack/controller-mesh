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
	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

func ConvertCircuitBreaker(breaker *ctrlmeshv1alpha1.CircuitBreaker) *ctrlmeshproto.CircuitBreaker {
	protoBreaker := &ctrlmeshproto.CircuitBreaker{
		Name: breaker.Name,
	}
	if breaker.Spec.TrafficInterceptRules != nil {
		protoBreaker.TrafficInterceptRules = make([]*ctrlmeshproto.TrafficInterceptRule, len(breaker.Spec.TrafficInterceptRules))
		for i := range breaker.Spec.TrafficInterceptRules {
			protoBreaker.TrafficInterceptRules[i] = ConvertTrafficInterceptRule(breaker.Spec.TrafficInterceptRules[i])
		}
	}
	if breaker.Spec.RateLimitings != nil {
		protoBreaker.RateLimitings = make([]*ctrlmeshproto.RateLimiting, len(breaker.Spec.RateLimitings))
		for i := range breaker.Spec.RateLimitings {
			protoBreaker.RateLimitings[i] = ConvertLimiting(breaker.Spec.RateLimitings[i])
		}
	}
	protoBreaker.ConfigHash = utils.GetMD5Hash(utils.DumpJSON(protoBreaker))
	return protoBreaker
}

func ConvertTrafficInterceptRule(rule *ctrlmeshv1alpha1.TrafficInterceptRule) *ctrlmeshproto.TrafficInterceptRule {
	protoRule := &ctrlmeshproto.TrafficInterceptRule{
		Name: rule.Name,
	}
	switch rule.InterceptType {
	case ctrlmeshv1alpha1.InterceptTypeWhitelist:
		protoRule.InterceptType = ctrlmeshproto.TrafficInterceptRule_INTERCEPT_WHITELIST
	case ctrlmeshv1alpha1.InterceptTypeBlacklist:
		protoRule.InterceptType = ctrlmeshproto.TrafficInterceptRule_INTERCEPT_BLACKLIST
	}
	switch rule.ContentType {
	case ctrlmeshv1alpha1.ContentTypeRegexp:
		protoRule.ContentType = ctrlmeshproto.TrafficInterceptRule_REGEXP
	case ctrlmeshv1alpha1.ContentTypeNormal:
		protoRule.ContentType = ctrlmeshproto.TrafficInterceptRule_NORMAL
	}
	if rule.Contents != nil {
		protoRule.Contents = make([]string, len(rule.Contents))
		copy(protoRule.Contents, rule.Contents)
	}
	if rule.Methods != nil {
		protoRule.Methods = make([]string, len(rule.Methods))
		copy(protoRule.Methods, rule.Methods)
	}
	return protoRule
}

func ConvertLimiting(limit *ctrlmeshv1alpha1.Limiting) *ctrlmeshproto.RateLimiting {
	protoLimit := &ctrlmeshproto.RateLimiting{
		Name: limit.Name,
	}
	if limit.ResourceRules != nil {
		protoLimit.ResourceRules = make([]*ctrlmeshproto.ResourceRule, len(limit.ResourceRules))
		for i := range limit.ResourceRules {
			protoLimit.ResourceRules[i] = ConvertResourceRule(&limit.ResourceRules[i])
		}
	}
	if limit.RestRules != nil {
		protoLimit.RestRules = make([]*ctrlmeshproto.RestRules, len(limit.RestRules))
		for i, r := range limit.RestRules {
			protoLimit.RestRules[i] = &ctrlmeshproto.RestRules{
				Url:    r.URL,
				Method: r.Method,
			}
		}
	}
	protoLimit.Bucket = &ctrlmeshproto.Bucket{
		Limit:    limit.Bucket.Limit,
		Burst:    limit.Bucket.Burst,
		Interval: limit.Bucket.Interval,
	}
	switch limit.TriggerPolicy {
	case ctrlmeshv1alpha1.TriggerPolicyLimiterOnly:
		protoLimit.TriggerPolicy = ctrlmeshproto.RateLimiting_TRIGGER_POLICY_LIMITER_ONLY
	case ctrlmeshv1alpha1.TriggerPolicyNormal:
		protoLimit.TriggerPolicy = ctrlmeshproto.RateLimiting_TRIGGER_POLICY_NORMAL
	case ctrlmeshv1alpha1.TriggerPolicyForceClosed:
		protoLimit.TriggerPolicy = ctrlmeshproto.RateLimiting_TRIGGER_POLICY_FORCE_CLOSED
	case ctrlmeshv1alpha1.TriggerPolicyForceOpened:
		protoLimit.TriggerPolicy = ctrlmeshproto.RateLimiting_TRIGGER_POLICY_FORCE_OPENED
	}
	if limit.RecoverPolicy != nil {
		protoLimit.RecoverPolicy = &ctrlmeshproto.RateLimiting_RecoverPolicy{}
		switch limit.RecoverPolicy.RecoverType {
		case ctrlmeshv1alpha1.RecoverPolicyManual:
			protoLimit.RecoverPolicy.Type = ctrlmeshproto.RateLimiting_RECOVER_POLICY_MANUAL
		case ctrlmeshv1alpha1.RecoverPolicySleepingWindow:
			protoLimit.RecoverPolicy.Type = ctrlmeshproto.RateLimiting_RECOVER_POLICY_SLEEPING_WINDOW
		}
		if limit.RecoverPolicy.SleepingWindowSize != nil {
			protoLimit.RecoverPolicy.SleepingWindowSize = *limit.RecoverPolicy.SleepingWindowSize
		}
	}
	if limit.Properties != nil {
		protoLimit.Properties = make(map[string]string, len(limit.Properties))
		for k, v := range limit.Properties {
			protoLimit.Properties[k] = v
		}
	}
	return protoLimit
}

func ConvertResourceRule(resourceRule *ctrlmeshv1alpha1.ResourceRule) *ctrlmeshproto.ResourceRule {
	protoResourceRule := &ctrlmeshproto.ResourceRule{}
	if resourceRule.Resources != nil {
		protoResourceRule.Resources = make([]string, len(resourceRule.Resources))
		copy(protoResourceRule.Resources, resourceRule.Resources)
	}
	if resourceRule.Verbs != nil {
		protoResourceRule.Verbs = make([]string, len(resourceRule.Verbs))
		copy(protoResourceRule.Verbs, resourceRule.Verbs)
	}
	if resourceRule.ApiGroups != nil {
		protoResourceRule.ApiGroups = make([]string, len(resourceRule.ApiGroups))
		copy(protoResourceRule.ApiGroups, resourceRule.ApiGroups)
	}
	if resourceRule.Namespaces != nil {
		protoResourceRule.Namespaces = make([]string, len(resourceRule.Namespaces))
		copy(protoResourceRule.Namespaces, resourceRule.Namespaces)
	}
	return protoResourceRule
}

func ConvertSnapshots(in []*ctrlmeshproto.LimitingSnapshot) []*ctrlmeshv1alpha1.LimitingSnapshot {
	var res []*ctrlmeshv1alpha1.LimitingSnapshot
	for _, s := range in {
		var state ctrlmeshv1alpha1.BreakerState
		switch s.State {
		case ctrlmeshproto.BreakerState_OPENED:
			state = ctrlmeshv1alpha1.BreakerStatusOpened
		case ctrlmeshproto.BreakerState_CLOSED:
			state = ctrlmeshv1alpha1.BreakerStatusClosed
		}
		res = append(res, &ctrlmeshv1alpha1.LimitingSnapshot{
			Name:               s.LimitingName,
			State:              state,
			LastTransitionTime: s.LastTransitionTime,
		})
	}
	return res
}
