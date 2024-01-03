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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	// pkgfi "github.com/KusionStack/controller-mesh/circuitbreaker"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

const timeLayout = "15:04:05"

var (
	logger  = logf.Log.WithName("fault-injection-manager")
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
		cb, ok := m.faultInjectionMap[config.Name]
		if !ok {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: false,
				Message: fmt.Sprintf("fault injection config %s not found", cb.Name),
			}, nil
		} else if config.ConfigHash != cb.ConfigHash {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success: false,
				Message: fmt.Sprintf("unequal fault injection %s hash, old %s, new %s", cb.Name, cb.ConfigHash, config.ConfigHash),
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
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result, "cost time", time.Since(now).String())
	}()

	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		faultInjections, states := m.faultInjectionStore.byIndex(IndexRest, url)
		if len(faultInjections) == 0 {
			continue
		}
		result = m.doFaultInjection(faultInjections, states)
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
		logger.Error(err, "failed to url", "URL", URL, "method", method)
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
	defer func() {
		logger.Info("validate resource", "namespace", namespace, "apiGroup", apiGroup, "resource", resource, "verb", verb, "result", result, "cost time", time.Since(now).String())
	}()
	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		faultInjections, states := m.faultInjectionStore.byIndex(IndexResource, seed)
		if len(faultInjections) == 0 {
			continue
		}
		result = m.doFaultInjection(faultInjections, states)
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
			apiErr := httpToAPIError(int(result.ErrCode), result.Message)
			if apiErr.Code != http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(int(apiErr.Code))
				json.NewEncoder(w).Encode(apiErr)
				logger.Info("faultinjection rule", fmt.Sprintf("fault injection, %s, %s,%d", result.Reason, result.Message, result.ErrCode))
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
				fmt.Println("Delaying for ", delayDuration)
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

// httpToAPIError convert http error to kubernetes api error
func httpToAPIError(code int, serverMessage string) *metav1.Status {
	status := &metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    int32(code),
		Reason:  metav1.StatusReason(fmt.Sprintf("HTTP %d", code)),
		Message: serverMessage,
	}
	var (
		reason  metav1.StatusReason
		message string
	)
	switch code {
	case http.StatusOK:
		reason = ""
		message = "code is 200"
		status.Status = metav1.StatusSuccess
	case http.StatusConflict:

		reason = metav1.StatusReasonConflict

		message = "the server reported a conflict"
	case http.StatusNotFound:
		reason = metav1.StatusReasonNotFound
		message = "the server could not find the requested resource"
	case http.StatusBadRequest:
		reason = metav1.StatusReasonBadRequest
		message = "the server rejected our request for an unknown reason"
	case http.StatusUnauthorized:
		reason = metav1.StatusReasonUnauthorized
		message = "the server has asked for the client to provide credentials"
	case http.StatusForbidden:
		reason = metav1.StatusReasonForbidden
		// the server message has details about who is trying to perform what action.  Keep its message.
		message = serverMessage
	case http.StatusNotAcceptable:
		reason = metav1.StatusReasonNotAcceptable
		// the server message has details about what types are acceptable
		if len(serverMessage) == 0 || serverMessage == "unknown" {
			message = "the server was unable to respond with a content type that the client supports"
		} else {
			message = serverMessage
		}
	case http.StatusUnsupportedMediaType:
		reason = metav1.StatusReasonUnsupportedMediaType
		// the server message has details about what types are acceptable
		message = serverMessage
	case http.StatusMethodNotAllowed:
		reason = metav1.StatusReasonMethodNotAllowed
		message = "the server does not allow this method on the requested resource"
	case http.StatusUnprocessableEntity:
		reason = metav1.StatusReasonInvalid
		message = "the server rejected our request due to an error in our request"
	case http.StatusServiceUnavailable:
		reason = metav1.StatusReasonServiceUnavailable
		message = "the server is currently unable to handle the request"
	case http.StatusGatewayTimeout:
		reason = metav1.StatusReasonTimeout
		message = "the server was unable to return a response in the time allotted, but may still be processing the request"
	case http.StatusTooManyRequests:
		reason = metav1.StatusReasonTooManyRequests
		message = "the server has received too many requests and has asked us to try again later"
	default:
		// if code >= 500 {
		// 	reason = metav1.StatusReasonInternalError
		// 	message = fmt.Sprintf("an error on the server (%q) has prevented the request from succeeding", serverMessage)
		// }
		status.Status = metav1.StatusSuccess
		reason = "code is not allowed"
		message = "code is not allowed"
		status.Code = http.StatusOK
	}
	status.Reason = reason
	status.Message = message
	return status
}

// RegisterRules register a fault injection to the local store
func (m *manager) registerRules(fi *ctrlmeshproto.FaultInjection) {
	logger.Info("register rule", "faultInjection", fi.Name)
	if _, ok := m.faultInjectionMap[fi.Name]; ok {
		m.unregisterRules(fi.Name)
	}

	for _, faultInjection := range fi.HttpFaultInjections {
		key := fmt.Sprintf("%s:%s", fi.Name, faultInjection.Name)
		m.faultInjectionStore.createOrUpdateRule(
			key, faultInjection.DeepCopy(),
		)
	}
}

// UnregisterRules unregister a fault injection to the local store
func (m *manager) unregisterRules(fiName string) {
	logger.Info("unregister rule", "faultInjection", fiName)
	fi, ok := m.faultInjectionMap[fiName]
	if !ok {
		return
	}
	for _, faultInjection := range fi.HttpFaultInjections {
		key := fmt.Sprintf("%s:%s", fi.Name, faultInjection.Name)
		m.faultInjectionStore.deleteRule(key)
	}
}
