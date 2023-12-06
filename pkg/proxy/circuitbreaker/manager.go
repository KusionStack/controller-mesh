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

package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

var (
	logger = logf.Log.WithName("limiter-manager")
)

type ManagerInterface interface {
	Validator
	Sync(config *ctrlmeshproto.CircuitBreaker) (*ctrlmeshproto.ConfigResp, error)
}

type Validator interface {
	ValidateTrafficIntercept(URL string, method string) (result *ValidateResult)
	ValidateRest(URL string, method string) (result ValidateResult)
	ValidateResource(namespace, apiGroup, resource, verb string) (result ValidateResult)
	HandlerWrapper() func(http.Handler) http.Handler
}

type manager struct {
	breakerMap map[string]*ctrlmeshproto.CircuitBreaker
	mu         sync.RWMutex

	limiterStore          *store
	trafficInterceptStore *trafficInterceptStore
}

func NewManager(ctx context.Context) ManagerInterface {
	return &manager{
		breakerMap:            map[string]*ctrlmeshproto.CircuitBreaker{},
		limiterStore:          newLimiterStore(ctx),
		trafficInterceptStore: newTrafficInterceptStore(),
	}
}

func (m *manager) Sync(config *ctrlmeshproto.CircuitBreaker) (*ctrlmeshproto.ConfigResp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch config.Option {
	case ctrlmeshproto.CircuitBreaker_UPDATE:
		cb, ok := m.breakerMap[config.Name]
		if ok && config.ConfigHash == cb.ConfigHash {
			return &ctrlmeshproto.ConfigResp{
				Success:          true,
				Message:          fmt.Sprintf("circuitBreaker spec hash not updated, hash %s", cb.ConfigHash),
				LimitingSnapshot: m.snapshot(config.Name),
			}, nil
		} else {
			m.breakerMap[config.Name] = config
			m.registerRules(config)
			var msg string
			if cb == nil {
				msg = fmt.Sprintf("new circuitBreaker with spec hash %s", config.ConfigHash)
			} else {
				msg = fmt.Sprintf("circuitBreaker spec hash updated, old hash %s, new %s", cb.ConfigHash, config.ConfigHash)
			}
			return &ctrlmeshproto.ConfigResp{
				Success:          true,
				Message:          msg,
				LimitingSnapshot: m.snapshot(config.Name),
			}, nil
		}
	case ctrlmeshproto.CircuitBreaker_DELETE:
		if cb, ok := m.breakerMap[config.Name]; ok {
			m.unregisterRules(cb.Name)
			delete(m.breakerMap, config.Name)
			return &ctrlmeshproto.ConfigResp{
				Success: true,
				Message: fmt.Sprintf("circuitBreaker config %s success deleted", cb.Name),
			}, nil
		} else {
			return &ctrlmeshproto.ConfigResp{
				Success: true,
				Message: fmt.Sprintf("circuitBreaker config %s already deleted", config.Name),
			}, nil
		}
	case ctrlmeshproto.CircuitBreaker_CHECK:
		cb, ok := m.breakerMap[config.Name]
		if !ok {
			return &ctrlmeshproto.ConfigResp{Success: false, Message: fmt.Sprintf("circuit breaker config %s not found", cb.Name), LimitingSnapshot: m.snapshot(config.Name)}, nil
		} else if config.ConfigHash != cb.ConfigHash {
			return &ctrlmeshproto.ConfigResp{Success: false, Message: fmt.Sprintf("unequal circuit breaker %s hash, old %s, new %s", cb.Name, cb.ConfigHash, config.ConfigHash), LimitingSnapshot: m.snapshot(config.Name)}, nil
		}
		return &ctrlmeshproto.ConfigResp{Success: true, Message: "", LimitingSnapshot: m.snapshot(config.Name)}, nil
	case ctrlmeshproto.CircuitBreaker_RECOVER:
		var recoverNames string
		if config.RateLimitings != nil {
			for _, rl := range config.RateLimitings {
				key := fmt.Sprintf("%s:%s", config.Name, rl.Name)
				m.recoverBreaker(key)
				recoverNames = fmt.Sprintf("%s [%s]", recoverNames, key)
			}
		}
		return &ctrlmeshproto.ConfigResp{Success: true, Message: fmt.Sprintf("recovered limiting rules %s", recoverNames), LimitingSnapshot: m.snapshot(config.Name)}, nil
	default:
		return &ctrlmeshproto.ConfigResp{
			Success: false,
			Message: fmt.Sprintf("illegal config option %s", config.Option),
		}, fmt.Errorf("illegal config option %s", config.Option)
	}
}

func (m *manager) snapshot(breaker string) []*ctrlmeshproto.LimitingSnapshot {
	var res []*ctrlmeshproto.LimitingSnapshot
	m.limiterStore.mu.RLock()
	defer m.limiterStore.mu.RUnlock()
	for key, sta := range m.limiterStore.states {
		arr := strings.Split(key, ":")
		cbName, limitName := arr[0], arr[1]
		if cbName != breaker {
			continue
		}
		breakerState, lastTransitionTime, recoverTime := sta.read()
		res = append(res, &ctrlmeshproto.LimitingSnapshot{
			LimitingName:       limitName,
			State:              breakerState,
			RecoverTime:        recoverTime,
			LastTransitionTime: lastTransitionTime,
		})
	}
	return res
}

type ValidateResult struct {
	Allowed bool
	Reason  string
	Message string
}

// RegisterRules register a circuit breaker to the local limiter store
func (m *manager) registerRules(cb *ctrlmeshproto.CircuitBreaker) {
	logger.Info("register rule", "circuit-breaker", cb.Name)
	if _, ok := m.breakerMap[cb.Name]; ok {
		m.unregisterRules(cb.Name)
	}

	for _, limiting := range cb.RateLimitings {
		key := fmt.Sprintf("%s:%s", cb.Name, limiting.Name)
		m.limiterStore.createOrUpdateRule(key, limiting.DeepCopy(), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_CLOSED})
	}
	for _, trafficInterceptRule := range cb.TrafficInterceptRules {
		key := fmt.Sprintf("%s:%s", cb.Name, trafficInterceptRule.Name)
		m.trafficInterceptStore.createOrUpdateRule(key, trafficInterceptRule)
	}
}

// UnregisterRules unregister a circuit breaker to the local limiter store
func (m *manager) unregisterRules(cbName string) {
	logger.Info("unregister rule", "CircuitBreaker", cbName)
	cb, ok := m.breakerMap[cbName]
	if !ok {
		return
	}
	for _, limiting := range cb.RateLimitings {
		key := fmt.Sprintf("%s:%s", cb.Name, limiting.Name)
		m.limiterStore.deleteRule(key)
	}
	for _, trafficInterceptRule := range cb.TrafficInterceptRules {
		key := fmt.Sprintf("%s:%s", cb.Name, trafficInterceptRule.Name)
		m.trafficInterceptStore.deleteRule(key)
	}
}

func (m *manager) unregisterLimitingRule(cb *ctrlmeshproto.CircuitBreaker, limiting *ctrlmeshproto.RateLimiting) {
	logger.Info("unregister limiting rule", "circuit-breaker", cb.Name+"/"+limiting.Name)
	key := fmt.Sprintf("%s:%s", cb.Name, limiting.Name)
	m.limiterStore.deleteRule(key)
}

func (m *manager) unregisterTrafficInterceptRule(cb *ctrlmeshproto.CircuitBreaker, intercept *ctrlmeshproto.TrafficInterceptRule) {
	logger.Info("unregister traffic intercept rule", "circuit-breaker", cb.Name+"/"+intercept.Name)
	key := fmt.Sprintf("%s:%s", cb.Name, intercept.Name)
	m.trafficInterceptStore.deleteRule(key)
}

func (m *manager) recoverBreaker(key string) {
	if m.limiterStore.states[key] == nil {
		logger.Error(fmt.Errorf("breaker not found"), fmt.Sprintf("limitingName %s not exist", key))
		return
	}
	logger.Info("RecoverBreaker", "name", key, "state", m.limiterStore.states[key].state)
	m.limiterStore.states[key].recoverBreaker()
}

// ValidateTrafficIntercept validate a rest request
func (m *manager) ValidateTrafficIntercept(URL string, method string) (result *ValidateResult) {
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result)
	}()

	indexes := m.trafficInterceptStore.normalIndexes[indexForRest(URL, method)]
	if indexes == nil {
		indexes = m.trafficInterceptStore.normalIndexes[indexForRest(URL, "*")]
	}
	for key := range indexes {
		trafficInterceptRule := m.trafficInterceptStore.rules[key]
		if trafficInterceptRule != nil {
			if ctrlmeshproto.TrafficInterceptRule_INTERCEPT_WHITELIST == trafficInterceptRule.InterceptType {
				result = &ValidateResult{Allowed: true, Reason: "Whitelist", Message: fmt.Sprintf("Traffic is allow by whitelist rule, name: %s", trafficInterceptRule.Name)}
				return result
			} else if ctrlmeshproto.TrafficInterceptRule_INTERCEPT_BLACKLIST == trafficInterceptRule.InterceptType {
				result = &ValidateResult{Allowed: false, Reason: "Blacklist", Message: fmt.Sprintf("Traffic is intercept by blacklist rule, name: %s", trafficInterceptRule.Name)}
				return result
			}
		}
	}

	for key, regs := range m.trafficInterceptStore.regexpIndexes {
		for _, reg := range regs {
			if reg.method == method && reg.reg.MatchString(URL) {
				if ctrlmeshproto.TrafficInterceptRule_INTERCEPT_WHITELIST == reg.regType {
					result = &ValidateResult{Allowed: true, Reason: "Whitelist", Message: fmt.Sprintf("Traffic is allow by whitelist rule: %s, regexp: %s", key, reg.reg.String())}
					return result
				} else if ctrlmeshproto.TrafficInterceptRule_INTERCEPT_BLACKLIST == reg.regType {
					result = &ValidateResult{Allowed: false, Reason: "Blacklist", Message: fmt.Sprintf("Traffic is intercept by blacklist rule: %s, regexp: %s", key, reg.reg.String())}
					return result
				}
			}
		}
	}

	for _, rule := range m.trafficInterceptStore.rules {
		if rule.InterceptType == ctrlmeshproto.TrafficInterceptRule_INTERCEPT_WHITELIST {
			result = &ValidateResult{Allowed: false, Reason: "No rule match", Message: "Default strategy is whitelist, should deny"}
			break
		}
	}
	if result == nil {
		result = &ValidateResult{Allowed: true, Reason: "No rule match", Message: "Default strategy is blacklist, should allow"}
	}
	return result
}

// ValidateRest validate a rest request
// TODO: consider regex matching
func (m *manager) ValidateRest(URL string, method string) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result, "cost time", time.Since(now).String())
	}()

	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		limitings, limiters, states := m.limiterStore.byIndex(IndexRest, url)
		if len(limitings) == 0 {
			continue
		}
		result = m.doValidation(limitings, limiters, states)
		return result
	}
	result = ValidateResult{Allowed: true, Reason: "No rule match"}
	return result
}

// ValidateResource validate a request to api server
// TODO: consider regex matching
func (m *manager) ValidateResource(namespace, apiGroup, resource, verb string) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate resource", "namespace", namespace, "apiGroup", apiGroup, "resource", resource, "verb", verb, "result", result, "cost time", time.Since(now).String())
	}()

	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		limitings, limiters, states := m.limiterStore.byIndex(IndexResource, seed)
		if len(limitings) == 0 {
			continue
		}
		result = m.doValidation(limitings, limiters, states)
		return result
	}
	result = ValidateResult{Allowed: true, Reason: "No rule match"}
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

func stringAppendWildcard(value string) []string {
	if value == "*" {
		return []string{"*"}
	}
	return []string{value, "*"}
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

func (m *manager) doValidation(limitings []*ctrlmeshproto.RateLimiting, limiters []*rate.Limiter, states []*state) ValidateResult {
	result := ValidateResult{
		Allowed: true,
		Reason:  "Default allow",
	}
	for idx := range limitings {
		// check current circuit breaker status first
		status, _, _ := states[idx].read()
		switch status {
		case ctrlmeshproto.BreakerState_OPENED: // cb already opened, just refuse
			result.Allowed = false
			result.Reason = "CircuitBreakerTriggered"
			result.Message = fmt.Sprintf("the circuit breaker is triggered. Limiting rule name: %s", limitings[idx].Name)
		}
		switch limitings[idx].TriggerPolicy {
		case ctrlmeshproto.RateLimiting_TRIGGER_POLICY_FORCE_OPENED: // force open policy, just refuse
			states[idx].triggerBreaker()
			result.Allowed = false
			result.Reason = "CircuitBreakerForceOpened"
			result.Message = fmt.Sprintf("the circuit breaker is force opened. Limiting rule name: %s", limitings[idx].Name) // force
		case ctrlmeshproto.RateLimiting_TRIGGER_POLICY_FORCE_CLOSED: // force close policy, just allow
			states[idx].recoverBreaker()
			result.Allowed = true
			result.Reason = "CircuitBreakerForceClosed"
			result.Message = fmt.Sprintf("the circuit breaker is force closed. Limiting rule name: %s", limitings[idx].Name)
		case ctrlmeshproto.RateLimiting_TRIGGER_POLICY_LIMITER_ONLY: // limiter only policy, determine by limiter
			states[idx].recoverBreaker()
			if !limiters[idx].Allow() {
				result.Allowed = false
				result.Reason = "OverLimit"
				result.Message = fmt.Sprintf("the request is over limit by limiting rule: %s", limitings[idx].Name)
			} else {
				result.Reason = "Limiter Allowed"
			}
		case ctrlmeshproto.RateLimiting_TRIGGER_POLICY_NORMAL: // normal policy, determine by limiter, and trigger cb open when refuse
			if states[idx].state == ctrlmeshproto.BreakerState_CLOSED {
				if !limiters[idx].Allow() {
					result.Allowed = false
					result.Reason = "OverLimit"
					result.Message = fmt.Sprintf("the request is over limit by limiting rule: %s", limitings[idx].Name)
					if limitings[idx].RecoverPolicy.Type == ctrlmeshproto.RateLimiting_RECOVER_POLICY_SLEEPING_WINDOW {
						d, _ := time.ParseDuration(limitings[idx].RecoverPolicy.SleepingWindowSize)
						states[idx].triggerBreakerWithTimeWindow(d)
						m.limiterStore.registerState(states[idx])
					} else {
						states[idx].triggerBreaker()
					}
				} else {
					states[idx].recoverBreaker()
				}
			}
		}
	}
	return result
}

func (m *manager) HandlerWrapper() func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return withBreaker(m, handler)
	}
}

func withBreaker(validator Validator, handler http.Handler) http.Handler {
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
			http.Error(w, fmt.Sprintf("Circuit breaking by TrafficPolicy, %s, %s", result.Reason, result.Message), http.StatusExpectationFailed)
			return
		}

		handler.ServeHTTP(w, req)
	})
}
