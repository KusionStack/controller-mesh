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
	"encoding/json"
	"errors"
	"fmt"

	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

const timeLayout = "15:04:05"

var (
	randNum = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type ManagerInterface interface {
	FaultInjector
	Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error)
}

type FaultInjectionResult struct {
	Abort   bool
	Reason  string
	Message string
	ErrCode int32
}

type FaultInjector interface {
	FaultInjectionRest(URL string, method string) (result *FaultInjectionResult)
	FaultInjectionResource(namespace, apiGroup, resource, verb string) (result *FaultInjectionResult)
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
			if ok{
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

func (m *manager) FaultInjectionRest(URL string, method string) (result *FaultInjectionResult) {
	now := time.Now()
	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		faultInjections, states := m.faultInjectionStore.byIndex(IndexRest, url)
		if len(faultInjections) == 0 {
			continue
		}
		result = m.doFaultInjection(faultInjections, states)
		klog.Infof("validate rest, URL: %s, method:%s, result: %v, cost time: %v ", URL, method, result, time.Since(now).String())
		return result
	}
	result = &FaultInjectionResult{Abort: false, Reason: "No rule match"}
	return result
}

func generateWildcardUrls(URL string, method string) []string {
	var result []string
	result = append(result, indexForRest(URL, method))
	URL = strings.TrimSuffix(URL, "/")
	u, err := url.Parse(URL)
	if err != nil {
		klog.Errorf("failed to url, URL: %s, method: %s,err: %v", URL, method, err)
		return result
	}
	if len(u.Path) > 0 {
		subPaths := strings.Split(u.Path, "/")
		for i := len(subPaths) - 1; i > 0; i-- {
			subPath := u.Scheme + "://" + u.Host + strings.Join(subPaths[:i], "/") + "/*"
			result = append(result, indexForRest(subPath, method))
		}
	}

	return result
}

func (m *manager) FaultInjectionResource(namespace, apiGroup, resource, verb string) (result *FaultInjectionResult) {
	now := time.Now()
	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		faultInjections, states := m.faultInjectionStore.byIndex(IndexResource, seed)
		if len(faultInjections) == 0 {
			continue
		}
		result = m.doFaultInjection(faultInjections, states)
		klog.Infof("validate resource, namespace: %s, apiGroup: %s, resource: %s, verb: %s, result: %v, cost time: %v, ", namespace, apiGroup, resource, verb, result, time.Since(now).String())
		return result
	}
	result = &FaultInjectionResult{Abort: false, Reason: "No rule match"}
	return result
}

func generateWildcardSeeds(namespace, apiGroup, resource, verb string) []string {
	/* priority strategy
	   1、if wildcard number not equal, less wildcard number priority higher. eg. 0* > 1* > 2* > 3* > 4*
	   2、if wildcard number equal, the priority order is verb > resource > apiGroup > namespace.
	      eg. verb exist first match verb, then match resource, after match apiGroup, last match namespace
	*/
	result := []string{
		// zero wildcard
		indexForResource(namespace, apiGroup, resource, verb),
		// one wildcard
		indexForResource("*", apiGroup, resource, verb),
		indexForResource(namespace, "*", resource, verb),
		indexForResource(namespace, apiGroup, "*", verb),
		indexForResource(namespace, apiGroup, resource, "*"),
		// two wildcard
		indexForResource("*", "*", resource, verb),
		indexForResource("*", apiGroup, "*", verb),
		indexForResource(namespace, "*", "*", verb),
		indexForResource("*", apiGroup, resource, "*"),
		indexForResource(namespace, "*", resource, "*"),
		indexForResource(namespace, apiGroup, "*", "*"),
		// three wildcard
		indexForResource("*", "*", "*", verb),
		indexForResource("*", "*", resource, "*"),
		indexForResource("*", apiGroup, "*", "*"),
		indexForResource(namespace, "*", "*", "*"),
		// four wildcard
		indexForResource("*", "*", "*", "*"),
	}
	return result
}

func withFaultInjection(injector FaultInjector, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			// if this happens, the handler chain isn't setup correctly because there is no request info
			responsewriters.InternalError(w, req, errors.New("no RequestInfo found in the context"))
			return
		}
		result := injector.FaultInjectionResource(requestInfo.Namespace, requestInfo.APIGroup, requestInfo.Resource, requestInfo.Verb)

		if result.Abort {
			apiErr := utils.HttpToAPIError(int(result.ErrCode), req.Method, result.Message)
			if apiErr.Code != http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(int(apiErr.Code))
				if err := json.NewEncoder(w).Encode(apiErr); err != nil {
					// Error encoding the JSON response, at this point the headers are already written.
					klog.Errorf("failed to write api error response: %v", err)
					return
				}
				klog.Infof("faultinjection rule: %s", fmt.Sprintf("fault injection, %s, %s,%d", apiErr.Reason, apiErr.Message, apiErr.Code))
				return
			}
		}
		handler.ServeHTTP(w, req)
	})
}

func (m *manager) doFaultInjection(faultInjections []*ctrlmeshproto.HTTPFaultInjection, states []*state) *FaultInjectionResult {
	result := &FaultInjectionResult{
		Abort:  false,
		Reason: "Default allow",
	}
	for idx := range faultInjections {

		if faultInjections[idx].EffectiveTime != nil && !isEffectiveTimeRange(faultInjections[idx].EffectiveTime) {
			fmt.Println("effective time is not in range", faultInjections[idx].EffectiveTime)
			continue
		}

		if faultInjections[idx].Delay != nil {
			if isInpercentRange(faultInjections[idx].Delay.Percent) {
				delay := faultInjections[idx].Delay.GetFixedDelay()
				delayDuration := delay.AsDuration()
				klog.Infof("Delaying time: %v ", delayDuration)
				time.Sleep(delayDuration)
			}
		}
		if faultInjections[idx].Abort != nil {
			if isInpercentRange(faultInjections[idx].Abort.Percent) {
				result.Abort = true
				result.Reason = "FaultInjectionTriggered"
				result.Message = fmt.Sprintf("the fault injection is triggered. Limiting rule name: %s", faultInjections[idx].Name)
				result.ErrCode = faultInjections[idx].Abort.GetHttpStatus()
			}

		}
	}
	return result
}

func isInpercentRange(value float64) bool {
	if value < 0 || value > 100 {
		fmt.Println("Value must be between 0 and 100")
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
