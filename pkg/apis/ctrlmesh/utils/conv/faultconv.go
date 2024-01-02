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
	"strconv"
	"time"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
	"google.golang.org/protobuf/types/known/durationpb"
)

func ConvertFaultInjection(faultInjection *ctrlmeshv1alpha1.FaultInjection) *ctrlmeshproto.FaultInjection {
	protoFaultInjection := &ctrlmeshproto.FaultInjection{
		Name: faultInjection.Name,
	}
	if faultInjection.Spec.HTTPFaultInjections != nil && len(faultInjection.Spec.HTTPFaultInjections) > 0 {
		protoFaultInjection.HttpFaultInjections = make([]*ctrlmeshproto.HTTPFaultInjection, len(faultInjection.Spec.HTTPFaultInjections))
		for i := range faultInjection.Spec.HTTPFaultInjections {
			protoFaultInjection.HttpFaultInjections[i] = ConvertHTTPFaultInjection(faultInjection.Spec.HTTPFaultInjections[i])
		}
	}

	protoFaultInjection.ConfigHash = utils.GetMD5Hash(utils.DumpJSON(protoFaultInjection))
	return protoFaultInjection
}

func ConvertHTTPFaultInjection(faultInjection *ctrlmeshv1alpha1.HTTPFaultInjection) *ctrlmeshproto.HTTPFaultInjection {
	protoFaultInjection := &ctrlmeshproto.HTTPFaultInjection{
		Name: faultInjection.Name,
	}
	if faultInjection.Delay != nil {
		d, err := time.ParseDuration(faultInjection.Delay.FixedDelay)
		if err != nil {
			return nil
		}
		percent, err := strconv.ParseFloat(faultInjection.Delay.Percent, 64) // 64 表示使用 float64 类型
		if err != nil {
			return nil
		}
		delay := durationpb.New(d)
		protoFaultInjection.Delay = &ctrlmeshproto.HTTPFaultInjection_Delay{
			HttpDelayType: &ctrlmeshproto.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: delay,
			},
			Percent: percent,
		}
	}
	if faultInjection.Abort != nil {
		percent, err := strconv.ParseFloat(faultInjection.Abort.Percent, 64) // 64 表示使用 float64 类型
		if err != nil {
			return nil
		}
		protoFaultInjection.Abort = &ctrlmeshproto.HTTPFaultInjection_Abort{
			Percent: percent,
			ErrorType: &ctrlmeshproto.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: int32(faultInjection.Abort.HttpStatus),
			},
		}
	}
	if faultInjection.Match != nil {
		protoFaultInjection.Match = ConvertHTTPMatch(faultInjection.Match)

	}
	if faultInjection.EffectiveTime != nil {
		protoFaultInjection.EffectiveTime = ConvertEffectiveTime(faultInjection.EffectiveTime)
	}
	return protoFaultInjection
}

func ConvertHTTPMatch(match *ctrlmeshv1alpha1.HTTPMatchRequest) *ctrlmeshproto.HTTPMatchRequest {
	httpMatchRequest := &ctrlmeshproto.HTTPMatchRequest{
		Name: match.Name,
	}
	if match.RestRules != nil {
		httpMatchRequest.RestRules = make([]*ctrlmeshproto.MultiRestRule, len(match.RestRules))
		for i, restRule := range match.RestRules {
			httpMatchRequest.RestRules[i] = &ctrlmeshproto.MultiRestRule{}
			if restRule.URL != nil {
				httpMatchRequest.RestRules[i].Url = make([]string, len(restRule.URL))
				copy(httpMatchRequest.RestRules[i].Url, restRule.URL)
			}
			if restRule.Method != nil {
				httpMatchRequest.RestRules[i].Method = make([]string, len(restRule.Method))
				copy(httpMatchRequest.RestRules[i].Method, restRule.Method)
			}
		}
	}
	if match.RelatedResources != nil {
		httpMatchRequest.RelatedResources = make([]*ctrlmeshproto.ResourceMatch, len(match.RelatedResources))
		for i, relatedResource := range match.RelatedResources {

			httpMatchRequest.RelatedResources[i] = ConvertRelatedResources(relatedResource)
		}

	}
	return httpMatchRequest
}

func ConvertEffectiveTime(timeRange *ctrlmeshv1alpha1.EffectiveTimeRange) *ctrlmeshproto.EffectiveTimeRange {
	if timeRange == nil {
		return nil
	}

	timeRangeRes := &ctrlmeshproto.EffectiveTimeRange{
		StartTime: timeRange.StartTime,
		EndTime:   timeRange.EndTime,
	}

	// Convert DaysOfWeek slice to protobuf repeated field
	for _, day := range timeRange.DaysOfWeek {
		timeRangeRes.DaysOfWeek = append(timeRangeRes.DaysOfWeek, int32(day))
	}

	// Convert DaysOfMonth slice to protobuf repeated field
	for _, day := range timeRange.DaysOfMonth {
		timeRangeRes.DaysOfMonth = append(timeRangeRes.DaysOfMonth, int32(day))
	}

	// Convert Months slice to protobuf repeated field
	for _, month := range timeRange.Months {
		timeRangeRes.Months = append(timeRangeRes.Months, int32(month))
	}

	return timeRangeRes
}

func ConvertRelatedResources(resourceRule *ctrlmeshv1alpha1.ResourceMatch) *ctrlmeshproto.ResourceMatch {
	protoResourceRule := &ctrlmeshproto.ResourceMatch{}
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

func ConvertFaultInjectionSnapshots(in []*ctrlmeshproto.FaultInjectionSnapshot) []*ctrlmeshv1alpha1.FaultInjectionSnapshot {
	var res []*ctrlmeshv1alpha1.FaultInjectionSnapshot
	for _, s := range in {
		var state ctrlmeshv1alpha1.FaultInjectionState
		switch s.State {
		case ctrlmeshproto.FaultInjectionState_STATEOPENED:
			state = ctrlmeshv1alpha1.FaultInjectionStatusOpened
		case ctrlmeshproto.FaultInjectionState_STATECLOSED:
			state = ctrlmeshv1alpha1.FaultInjectionStatusClosed
		}
		res = append(res, &ctrlmeshv1alpha1.FaultInjectionSnapshot{
			Name:               s.LimitingName,
			State:              state,
			LastTransitionTime: s.LastTransitionTime,
		})
	}
	return res
}
