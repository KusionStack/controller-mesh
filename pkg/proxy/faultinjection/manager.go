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
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	// pkgfi "github.com/KusionStack/controller-mesh/circuitbreaker"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const timeLayout = "15:04:05"

var (
	logger                   = logf.Log.WithName("fault-injection-manager")
	randNum                  = rand.New(rand.NewSource(time.Now().UnixNano()))
	enableRestFaultInjection = os.Getenv(constants.EnvEnableRestFaultInjection) == "true"
)

type ManagerInterface interface {
	Validator
	Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error)
}

type ValidateResult struct {
	Allowed bool
	Reason  string
	Message string
	ErrCode int32
}

type Validator interface {
	ValidateRest(URL string, method string) (result *ValidateResult)
	ValidateResource(namespace, apiGroup, resource, verb string) (result *ValidateResult)
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
				Success:                 true,
				Message:                 fmt.Sprintf("faultInjection spec hash not updated, hash %s", fi.ConfigHash),
				FaultInjectionSnapshots: m.snapshot(config.Name),
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
				Success:                 true,
				Message:                 msg,
				FaultInjectionSnapshots: m.snapshot(config.Name),
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
				Success:                 false,
				Message:                 fmt.Sprintf("fault injection config %s not found", cb.Name),
				FaultInjectionSnapshots: m.snapshot(config.Name),
			}, nil
		} else if config.ConfigHash != cb.ConfigHash {
			return &ctrlmeshproto.FaultInjectConfigResp{
				Success:                 false,
				Message:                 fmt.Sprintf("unequal fault injection %s hash, old %s, new %s", cb.Name, cb.ConfigHash, config.ConfigHash),
				FaultInjectionSnapshots: m.snapshot(config.Name),
			}, nil
		}
		return &ctrlmeshproto.FaultInjectConfigResp{
			Success:                 true,
			Message:                 "",
			FaultInjectionSnapshots: m.snapshot(config.Name),
		}, nil
	case ctrlmeshproto.FaultInjection_RECOVER:
		var recoverNames string
		if config.HttpFaultInjections != nil {
			for _, hfi := range config.HttpFaultInjections {
				key := fmt.Sprintf("%s:%s", config.Name, hfi.Name)
				m.recoverFaultInjection(key)
				recoverNames = fmt.Sprintf("%s [%s]", recoverNames, key)
			}
		}
		return &ctrlmeshproto.FaultInjectConfigResp{
			Success:                 true,
			Message:                 fmt.Sprintf("recovered limiting rules %s", recoverNames),
			FaultInjectionSnapshots: m.snapshot(config.Name),
		}, nil
	default:
		return &ctrlmeshproto.FaultInjectConfigResp{
			Success: false,
			Message: fmt.Sprintf("illegal config option %s", config.Option),
		}, fmt.Errorf("illegal config option %s", config.Option)

	}
}

func (m *manager) snapshot(breaker string) []*ctrlmeshproto.FaultInjectionSnapshot {
	var res []*ctrlmeshproto.FaultInjectionSnapshot
	m.faultInjectionStore.mu.RLock()
	defer m.faultInjectionStore.mu.RUnlock()
	for key, sta := range m.faultInjectionStore.states {
		arr := strings.Split(key, ":")
		fiName, limitName := arr[0], arr[1]
		if fiName != breaker {
			continue
		}
		breakerState, lastTransitionTime, recoverTime := sta.read()
		res = append(res, &ctrlmeshproto.FaultInjectionSnapshot{
			LimitingName:       limitName,
			State:              breakerState,
			RecoverTime:        recoverTime,
			LastTransitionTime: lastTransitionTime,
		})
	}
	return res
}

func (m *manager) HandlerWrapper() func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return withFaultInjection(m, handler)
	}
}

// ValidateRest validate a rest request
// TODO: consider regex matching
func (m *manager) ValidateRest(URL string, method string) (result *ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result, "cost time", time.Since(now).String())
	}()

	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		limitings, states := m.faultInjectionStore.byIndex(IndexRest, url)
		if len(limitings) == 0 {
			continue
		}
		result = m.doValidation(limitings, states)
		return result
	}
	result = &ValidateResult{Allowed: true, Reason: "No rule match"}
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

// ValidateResource validate a request to api server
// TODO: consider regex matching
func (m *manager) ValidateResource(namespace, apiGroup, resource, verb string) (result *ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate resource", "namespace", namespace, "apiGroup", apiGroup, "resource", resource, "verb", verb, "result", result, "cost time", time.Since(now).String())
	}()
	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		limitings, states := m.faultInjectionStore.byIndex(IndexResource, seed)
		if len(limitings) == 0 {
			continue
		}
		result = m.doValidation(limitings, states)
		return result
	}
	result = &ValidateResult{Allowed: true, Reason: "No rule match"}
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

func withFaultInjection(validator Validator, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			// if this happens, the handler chain isn't setup correctly because there is no request info
			responsewriters.InternalError(w, req, errors.New("no RequestInfo found in the context"))
			return
		}
		result := validator.ValidateResource(requestInfo.Namespace, requestInfo.APIGroup, requestInfo.Resource, requestInfo.Verb)

		if !result.Allowed {
			apiErr := httpToAPIError(int(result.ErrCode), result.Message)
			if apiErr.Code != http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(int(apiErr.Code))
				json.NewEncoder(w).Encode(apiErr)
				logger.Info("faultinjection rule", fmt.Sprintf("fault injection, %s, %s,%d", result.Reason, result.Message, result.ErrCode))
				return
			}
		}

		realEndPointUrl, err := url.Parse(req.Header.Get("Mesh-Real-Endpoint"))
		logger.Info("receive proxy-host:%s, proxy-method:%s, Mesh-Real-Endpoint:%s", realEndPointUrl.Host, req.Method, req.Header.Get("Mesh-Real-Endpoint"))
		if err != nil || realEndPointUrl == nil {
			logger.Error(err, "Request Header Mesh-Real-Endpoint Parse Error")
			http.Error(w, fmt.Sprintf("Can not find real endpoint in header %s", err), http.StatusInternalServerError)
			return
		}
		// if enableRestFaultInjection {
		// 	result := pkgfi.ValidateRest(realEndPointUrl.Host, req.Method)
		// 	if result.Allowed == false {
		// 		logger.Error("ErrorTProxy: %s %s  ValidateTrafficIntercept NOPASSED ,checkresult:\t%s", realEndPointUrl.Host, req.Method, result.Reason)
		// 		http.Error(w, fmt.Sprintf("Forbidden by  ValidateTrafficIntercept breaker, %s, %s", result.Message, result.Reason), http.StatusForbidden)
		// 		return
		// 	}
		// }

		// ValidateRest check
		logger.Info("start ValidateRest checkrule %s %s", realEndPointUrl.Host, req.Method)
		validateresult := validator.ValidateRest(req.Header.Get("Mesh-Real-Endpoint"), req.Method)
		if !validateresult.Allowed {
			logger.Info("ErrorTProxy: %s %s  ValidateRest NOPASSED ,checkresult:%t, validateresultReason:%s", req.Header.Get("Mesh-Real-Endpoint"), req.Method, validateresult.Allowed, validateresult.Reason)
			// http.Error(w, fmt.Sprintf("Forbidden by circuit ValidateRest breaker, %s, %s", validateresult.Message, validateresult.Reason), http.StatusForbidden)
			// return
		}
		logger.Info("TProxy: %s %s ValidateRest check PASSED", realEndPointUrl.Host, req.Method)

		handler.ServeHTTP(w, req)
	})
}

func (m *manager) doValidation(limitings []*ctrlmeshproto.HTTPFaultInjection, states []*state) *ValidateResult {
	result := &ValidateResult{
		Allowed: true,
		Reason:  "Default allow",
	}
	for idx := range limitings {
		// check current fault injection status first
		// status, _, _ := states[idx].read()
		// switch status {
		// case ctrlmeshproto.FaultInjectionState_STATEOPENED: // fi already opened, just refuse
		// 	result.Allowed = false
		// 	result.Reason = "FaultInjectionTriggered"
		// 	result.Message = fmt.Sprintf("the fault injection is triggered. Limiting rule name: %s", limitings[idx].Name)
		// }

		if limitings[idx].EffectiveTime != nil && !isEffectiveTimeRange(limitings[idx].EffectiveTime) {
			continue
		}

		if limitings[idx].Delay != nil {
			if isInpercentRange(limitings[idx].Delay.Percent) {
				delay := limitings[idx].Delay.GetFixedDelay()
				delayDuration := delay.AsDuration()
				fmt.Println("Delaying for ", delayDuration)
				time.Sleep(delayDuration)
			}
		}
		if limitings[idx].Abort != nil {
			if isInpercentRange(limitings[idx].Abort.Percent) {
				result.Allowed = false
				result.Reason = "FaultInjectionTriggered"
				result.Message = fmt.Sprintf("the fault injection is triggered. Limiting rule name: %s", limitings[idx].Name)
				result.ErrCode = limitings[idx].Abort.GetHttpStatus()
			}

		}
		states[idx].triggerFaultInjection()
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
	now := time.Now()

	// Parse startTime string into a time.Time struct, only considering the time part
	startTime, err := time.Parse(timeLayout, timeRange.StartTime)
	if err != nil {
		return false
	}

	// Parse endTime string into a time.Time struct, only considering the time part
	endTime, err := time.Parse(timeLayout, timeRange.EndTime)
	if err != nil {
		return false
	}

	// Compare only the time components (hours, minutes, seconds) by extracting
	// them from the current time and the parsed start and end times
	currentHour, currentMinute, currentSecond := now.Clock()
	startHour, startMinute, startSecond := startTime.Clock()
	endHour, endMinute, endSecond := endTime.Clock()

	// Convert the hours, minutes, and seconds to a comparable integer value
	currentTimeInt := currentHour*3600 + currentMinute*60 + currentSecond
	startTimeInt := startHour*3600 + startMinute*60 + startSecond
	endTimeInt := endHour*3600 + endMinute*60 + endSecond

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
	reason := metav1.StatusReasonUnknown
	message := fmt.Sprintf("the server responded with the status code %d but did not return more information", code)
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
			&ctrlmeshproto.FaultInjectionSnapshot{
				State: ctrlmeshproto.FaultInjectionState_STATECLOSED,
			},
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

func (m *manager) recoverFaultInjection(key string) {
	if m.faultInjectionStore.states[key] == nil {
		logger.Error(fmt.Errorf("breaker not found"), fmt.Sprintf("limitingName %s not exist", key))
		return
	}
	logger.Info("RecoverBreaker", "name", key, "state", m.faultInjectionStore.states[key].state)
	m.faultInjectionStore.states[key].recoverBreaker()
}
