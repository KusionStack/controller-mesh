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

	"google.golang.org/protobuf/types/known/durationpb"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
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

func ConvertHTTPMatch(match *ctrlmeshv1alpha1.Match) *ctrlmeshproto.Match {
	Match := &ctrlmeshproto.Match{}
	if match.HttpMatch != nil {
		Match.HttpMatch = make([]*ctrlmeshproto.HttpMatch, len(match.HttpMatch))
		for i, restRule := range match.HttpMatch {
			Match.HttpMatch[i] = &ctrlmeshproto.HttpMatch{}
			if restRule.URL != nil {
				Match.HttpMatch[i].Url = make([]string, len(restRule.URL))
				copy(Match.HttpMatch[i].Url, restRule.URL)
			}
			if restRule.Method != nil {
				Match.HttpMatch[i].Method = make([]string, len(restRule.Method))
				copy(Match.HttpMatch[i].Method, restRule.Method)
			}
		}
	}
	if match.Resources != nil {
		Match.Resources = make([]*ctrlmeshproto.ResourceMatch, len(match.Resources))
		for i, relatedResource := range match.Resources {
			Match.Resources[i] = ConvertRelatedResources(relatedResource)
		}
	}
	if match.ContentMatch != nil {
		Match.StringMatch = make([]*ctrlmeshproto.StringMatch, len(match.ContentMatch))
		for i, stringMatch := range match.ContentMatch {
			Match.StringMatch[i] = ConvertStringMatch(stringMatch)
		}
	}
	return Match
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

func ConvertStringMatch(stringMatch *ctrlmeshv1alpha1.StringMatch) *ctrlmeshproto.StringMatch {
	res := &ctrlmeshproto.StringMatch{}
	if stringMatch != nil {
		if stringMatch.MatchType == ctrlmeshv1alpha1.StringMatchTypeNormal {
			res.MatchType = ctrlmeshproto.StringMatch_NORMAL
		} else if stringMatch.MatchType == ctrlmeshv1alpha1.StringMatchTypeRegexp {
			res.MatchType = ctrlmeshproto.StringMatch_REGEXP
		} else {
			return res
		}
		if stringMatch.Contents != nil {
			res.Contents = make([]string, len(stringMatch.Contents))
			copy(res.Contents, stringMatch.Contents)
		}
		if stringMatch.Methods != nil {
			res.Methods = make([]string, len(stringMatch.Methods))
			copy(res.Methods, stringMatch.Methods)
		}
	}
	return res
}
