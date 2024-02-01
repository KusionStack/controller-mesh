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

package faultinjection

import (
	"context"
	"errors"
	"fmt"

	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

const timeLayout = "15:04:05"

var (
	randNum = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type ManagerInterface interface {
	FaultInjectorManager
	Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error)
}

type FaultInjectionResult struct {
	Abort   bool
	Reason  string
	Message string
	ErrCode int32
}

type FaultInjectorManager interface {
	GetInjectorByUrl(url *url.URL, method string) Injector
	GetInjectorByResource(namespace, apiGroup, resource, verb string) Injector
	HandlerWrapper() func(http.Handler) http.Handler
}

type manager struct {
	faultInjectionMap   map[string]*ctrlmeshproto.FaultInjection
	faultInjectionStore *store
	mu                  sync.RWMutex
}

func NewManager(ctx context.Context) ManagerInterface {
	return &manager{
		faultInjectionMap:   map[string]*ctrlmeshproto.FaultInjection{},
		faultInjectionStore: newFaultInjectionStore(ctx),
	}
}

func (m *manager) Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch config.Option {
	case ctrlmeshproto.FaultInjection_UPDATE:
		fi, ok := m.faultInjectionMap[config.Name]
		if ok && config.ConfigHash == fi.ConfigHash {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: true,
				Message: fmt.Sprintf("faultInjection spec hash not updated, hash %s", fi.ConfigHash),
			}, nil
		} else {
			if ok {
				m.unregisterRules(fi.Name)
			}
			m.faultInjectionMap[config.Name] = config
			m.registerRules(config)
			var msg string
			if fi == nil {
				msg = fmt.Sprintf("new faultInjection with spec hash %s", config.ConfigHash)
			} else {
				msg = fmt.Sprintf("faultInjection spec hash updated, old hash %s, new %s", fi.ConfigHash, config.ConfigHash)
			}
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: true,
				Message: msg,
			}, nil
		}
	case ctrlmeshproto.FaultInjection_DELETE:
		if fi, ok := m.faultInjectionMap[config.Name]; ok {
			m.unregisterRules(fi.Name)
			delete(m.faultInjectionMap, config.Name)
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: true,
				Message: fmt.Sprintf("faultInjection config %s success deleted", fi.Name),
			}, nil
		} else {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: true,
				Message: fmt.Sprintf("faultInjection config %s already deleted", config.Name),
			}, nil
		}
	case ctrlmeshproto.FaultInjection_CHECK:
		fi, ok := m.faultInjectionMap[config.Name]
		if !ok {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: false,
				Message: fmt.Sprintf("fault injection config %s not found", fi.Name),
			}, nil
		} else if config.ConfigHash != fi.ConfigHash {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: false,
				Message: fmt.Sprintf("unequal fault injection %s hash, old %s, new %s", fi.Name, fi.ConfigHash, config.ConfigHash),
			}, nil
		}
		return &ctrlmeshproto.FaultInjectConfigResp{
			Success: true,
			Message: "",
		}, nil
	default:
		return &ctrlmeshproto.FaultInjectConfigResp{
			Success: false,
			Message: fmt.Sprintf("illegal config option %s", config.Option),
		}, fmt.Errorf("illegal config option %s", config.Option)

	}
}

func (m *manager) HandlerWrapper() func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return withFaultInjection(m, handler)
	}
}

func (m *manager) GetInjectorByUrl(url *url.URL, method string) Injector {
	faultInjections := m.matchHTTPFaultInjection(url, method)
	return m.getInjector(faultInjections)
}

func (m *manager) matchHTTPFaultInjection(url *url.URL, method string) (res []*ctrlmeshproto.HTTPFaultInjection) {
	for _, rule := range m.faultInjectionStore.restMatchRules {
		if m.matchRest(rule.Match, url, method) {
			res = append(res, rule)
		}
	}
	return
}

func (m *manager) matchRest(match *ctrlmeshproto.Match, url *url.URL, method string) bool {
	for _, httpMatch := range match.HttpMatch {
		if httpMatch.Method != "*" && httpMatch.Method != "" && httpMatch.Method != method {
			continue
		}
		if httpMatch.Host != nil && !m.matchContent(httpMatch.Host, url.Host) {
			continue
		}
		if httpMatch.Path != nil && !m.matchContent(httpMatch.Path, url.Path) {
			continue
		}
		return true
	}
	return false
}

func (m *manager) matchContent(mc *ctrlmeshproto.MatchContent, content string) bool {
	if mc.Exact != "" {
		return mc.Exact == content
	}
	if mc.Regex != "" {
		regex, ok := m.faultInjectionStore.regexMap[mc.Regex]
		if ok {
			return regex.MatchString(content)
		}
	}
	return false
}

func (m *manager) GetInjectorByResource(namespace, apiGroup, resource, verb string) Injector {
	var fjs []*ctrlmeshproto.HTTPFaultInjection
	for _, rule := range m.faultInjectionStore.resourcesMatchRules {
		if m.resourceMatch(rule.Match.Resources, namespace, apiGroup, resource, verb) {
			fjs = append(fjs, rule)
		}
	}
	return m.getInjector(fjs)
}

func (m *manager) resourceMatch(matchs []*ctrlmeshproto.ResourceMatch, namespace, apiGroup, resource, verb string) bool {
	for _, match := range matchs {
		if !matchInList(match.ApiGroups, apiGroup) {
			continue
		}
		if !matchInList(match.Resources, resource) {
			continue
		}
		if !matchInList(match.Verbs, verb) {
			continue
		}
		if !matchInList(match.Namespaces, namespace) {
			continue
		}
		return true
	}
	return false
}

func matchInList(list []string, t string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == "*" {
			return true
		}
		if t == v {
			return true
		}
	}
	return false
}

func withFaultInjection(injectorManager FaultInjectorManager, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			// if this happens, the handler chain isn't setup correctly because there is no request info
			responsewriters.InternalError(w, req, errors.New("no RequestInfo found in the context"))
			return
		}
		injector := injectorManager.GetInjectorByResource(requestInfo.Namespace, requestInfo.APIGroup, requestInfo.Resource, requestInfo.Verb)
		if !injector.Do(w, req) {
			handler.ServeHTTP(w, req)
		}
	})
}

func (m *manager) getInjector(faultInjections []*ctrlmeshproto.HTTPFaultInjection) Injector {
	injector := &abortWithDelayInjector{}
	for idx := range faultInjections {
		if faultInjections[idx].EffectiveTime != nil && !isEffectiveTimeRange(faultInjections[idx].EffectiveTime) {
			//klog.Infof("FaultInjection %s effective time is not in range, %v", faultInjections[idx].Name, faultInjections[idx].EffectiveTime)
			continue
		}
		if faultInjections[idx].Delay != nil && isInPercentRange(faultInjections[idx].Delay.Percent) {
			injector.AddDelay(faultInjections[idx].Delay.GetFixedDelay().AsDuration())
		}
		if !injector.Abort() && faultInjections[idx].Abort != nil && isInPercentRange(faultInjections[idx].Abort.Percent) {
			injector.AddAbort(int(faultInjections[idx].Abort.GetHttpStatus()),
				fmt.Sprintf("the fault injection is triggered. Limiting rule name: %s", faultInjections[idx].Name))
		}
	}
	return injector
}

func isInPercentRange(value float64) bool {
	if value < 0 || value > 100 {
		return false
	}
	if value == 0 {
		return true
	}
	randomNumber := randNum.Float64() * 100
	return randomNumber < value
}

// isEffectiveTimeRange checks whether the current time falls within the specified EffectiveTimeRange.
// It considers the start time, end time, days of the week, days of the month, and months.
// The function returns true if the current time is within the effective time range, otherwise false.
func isEffectiveTimeRange(timeRange *ctrlmeshproto.EffectiveTimeRange) bool {
	if timeRange.StartTime == "" || timeRange.EndTime == "" {
		return true
	}
	location, err := time.LoadLocation("UTC")
	if err != nil {
		return false
	}
	now := time.Now().In(location)

	// Parse startTime string into a time.Time struct, only considering the time part
	startTime, err := time.ParseInLocation(timeLayout, timeRange.StartTime, location)
	if err != nil {
		return false
	}

	// Parse endTime string into a time.Time struct, only considering the time part
	endTime, err := time.ParseInLocation(timeLayout, timeRange.EndTime, location)
	if err != nil {
		return false
	}

	// Convert the hours, minutes, and seconds to a comparable integer value
	currentTimeInt := now.Hour()*3600 + now.Minute()*60 + now.Second()
	startTimeInt := startTime.Hour()*3600 + startTime.Minute()*60 + startTime.Second()
	endTimeInt := endTime.Hour()*3600 + endTime.Minute()*60 + endTime.Second()

	// Check if the current time is after the start time and before the end time,
	// only considering the time part and ignoring the date part
	if currentTimeInt < startTimeInt {
		return false
	}
	if currentTimeInt > endTimeInt {
		return false
	}

	// Check if the current day of the week is within the allowed range
	if len(timeRange.DaysOfWeek) > 0 {
		currentDayOfWeek := int32(now.Weekday())
		dayIncluded := false
		for _, day := range timeRange.DaysOfWeek {
			if currentDayOfWeek == day {
				dayIncluded = true
				break
			}
		}
		if !dayIncluded {
			return false
		}
	}

	// Check if the current day of the month is within the allowed range
	if len(timeRange.DaysOfMonth) > 0 {
		currentDayOfMonth := int32(now.Day())
		dayIncluded := false
		for _, day := range timeRange.DaysOfMonth {
			if currentDayOfMonth == day {
				dayIncluded = true
				break
			}
		}
		if !dayIncluded {
			return false
		}
	}

	// Check if the current month is within the allowed range
	if len(timeRange.Months) > 0 {
		currentMonth := int32(now.Month())
		monthIncluded := false
		for _, month := range timeRange.Months {
			if currentMonth == month {
				monthIncluded = true
				break
			}
		}
		if !monthIncluded {
			return false
		}
	}

	// If all checks pass, the current time is within the effective time range
	return true
}

// RegisterRules register a fault injection to the local store
func (m *manager) registerRules(fi *ctrlmeshproto.FaultInjection) {
	klog.Infof("register rule, faultInjection: %s", fi.Name)
	for _, faultInjection := range fi.HttpFaultInjections {
		key := fmt.Sprintf("%s:%s", fi.Name, faultInjection.Name)
		m.faultInjectionStore.createOrUpdateRule(
			key, faultInjection.DeepCopy(),
		)
	}
}

// UnregisterRules unregister a fault injection to the local store
func (m *manager) unregisterRules(fiName string) {
	klog.Infof("unregister rule, faultInjection: %s", fiName)
	fi, ok := m.faultInjectionMap[fiName]
	if !ok {
		return
	}
	for _, faultInjection := range fi.HttpFaultInjections {
		key := fmt.Sprintf("%s:%s", fi.Name, faultInjection.Name)
		m.faultInjectionStore.deleteRule(key)
	}
}
