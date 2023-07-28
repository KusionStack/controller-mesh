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
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/KusionStack/kridge/pkg/apis/kridge/constants"
	appsv1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/utils"
)

const (
	syncPeriod = 5 * time.Second
	statusTTL  = 1 * time.Minute
)

var (
	logger                       = logf.Log.WithName("limiter-manager")
	defaultLimiterStore          = newLimiterStore()
	defaultBreakerLease          = NewBreakerLease()
	defaultTrafficInterceptStore = newTrafficInterceptStore()

	EnableCircuitBreaker = os.Getenv(constants.EnvEnableCircuitBreaker) == "true"
)

type ValidateResult struct {
	Allowed bool
	Reason  string
	Message string
	Force   bool
}

// RegisterRules register a circuit breaker to the local limiter store
func RegisterRules(cb *appsv1alpha1.CircuitBreaker) {
	logger.Info("register rule", "circuit-breaker", cb.Namespace+"/"+cb.Name)
	m := make(map[string]appsv1alpha1.LimitingSnapshot)
	for i := range cb.Status.LimitingSnapshots {
		snapshot := cb.Status.LimitingSnapshots[i]
		if snapshot.Endpoint == utils.EnvPodIpVal && snapshot.PodName == utils.EnvPodNameVal {
			m[snapshot.Name] = snapshot
		}
	}
	for _, limiting := range cb.Spec.RateLimitings {
		key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, limiting.Name)
		snapshot, ok := m[limiting.Name]
		if ok {
			defaultLimiterStore.createOrUpdateRule(key, limiting.DeepCopy(), snapshot.DeepCopy())
		} else {
			defaultLimiterStore.createOrUpdateRule(key, limiting.DeepCopy(), &appsv1alpha1.LimitingSnapshot{Status: appsv1alpha1.BreakerStatusClosed})
		}
	}
	for _, trafficInterceptRule := range cb.Spec.TrafficInterceptRules {
		key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, trafficInterceptRule.Name)
		defaultTrafficInterceptStore.createOrUpdateRule(key, &trafficInterceptRule)
	}
}

// UnregisterRules unregister a circuit breaker to the local limiter store
func UnregisterRules(cb *appsv1alpha1.CircuitBreaker) {
	logger.Info("unregister rule", "circuit-breaker", cb.Namespace+"/"+cb.Name)
	for _, limiting := range cb.Spec.RateLimitings {
		key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, limiting.Name)
		defaultLimiterStore.deleteRule(key)
	}
	for _, trafficInterceptRule := range cb.Spec.TrafficInterceptRules {
		key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, trafficInterceptRule.Name)
		defaultTrafficInterceptStore.deleteRule(key)
	}
}

func UnregisterLimitingRule(cb *appsv1alpha1.CircuitBreaker, limiting *appsv1alpha1.Limiting) {
	logger.Info("unregister limiting rule", "circuit-breaker", cb.Namespace+"/"+cb.Name+"/"+limiting.Name)
	key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, limiting.Name)
	defaultLimiterStore.deleteRule(key)
}

func UnregisterTrafficInterceptRule(cb *appsv1alpha1.CircuitBreaker, intercept *appsv1alpha1.TrafficInterceptRule) {
	logger.Info("unregister traffic intercept rule", "circuit-breaker", cb.Namespace+"/"+cb.Name+"/"+intercept.Name)
	key := fmt.Sprintf("%s:%s:%s", cb.Namespace, cb.Name, intercept.Name)
	defaultTrafficInterceptStore.deleteRule(key)
}

func RecoverBreaker(limitingName string) {
	if defaultLimiterStore.states[limitingName] != nil {
		logger.Info("RecoverBreaker", "name", limitingName, "status", defaultLimiterStore.states[limitingName].status)
		defaultLimiterStore.states[limitingName].recoverBreaker()
		return
	}
	logger.Error(fmt.Errorf("breaker not found"), fmt.Sprintf("limitingName %s not exist", limitingName))
}

// ValidateTrafficIntercept validate a rest request
func ValidateTrafficIntercept(URL string, method string) (result *ValidateResult) {
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result)
	}()

	if !EnableCircuitBreaker {
		result = &ValidateResult{Allowed: true, Reason: "disable CircuitBreaker"}
		return result
	}

	indexes := defaultTrafficInterceptStore.normalIndexes[indexForRest(URL, method)]
	if indexes == nil {
		indexes = defaultTrafficInterceptStore.normalIndexes[indexForRest(URL, "*")]
	}
	for key := range indexes {
		trafficInterceptRule := defaultTrafficInterceptStore.rules[key]
		if trafficInterceptRule != nil {
			if appsv1alpha1.InterceptTypeWhite == trafficInterceptRule.InterceptType {
				result = &ValidateResult{Allowed: true, Reason: "white", Message: fmt.Sprintf("Traffic is allow by white rule, name: %s", trafficInterceptRule.Name)}
				return result
			} else if appsv1alpha1.InterceptTypeBlack == trafficInterceptRule.InterceptType {
				result = &ValidateResult{Allowed: false, Reason: "black", Message: fmt.Sprintf("Traffic is intercept by black rule, name: %s", trafficInterceptRule.Name)}
				return result
			}
		}
	}

	for key, regs := range defaultTrafficInterceptStore.regexpIndexes {
		for _, reg := range regs {
			if reg.method == method && reg.reg.MatchString(URL) {
				if appsv1alpha1.InterceptTypeWhite == reg.regType {
					result = &ValidateResult{Allowed: true, Reason: "white", Message: fmt.Sprintf("Traffic is allow by white rule: %s, regexp: %s", key, reg.reg.String())}
					return result
				} else if appsv1alpha1.InterceptTypeBlack == reg.regType {
					result = &ValidateResult{Allowed: false, Reason: "black", Message: fmt.Sprintf("Traffic is intercept by black rule: %s, regexp: %s", key, reg.reg.String())}
					return result
				}
			}
		}
	}

	for _, rule := range defaultTrafficInterceptStore.rules {
		if rule.InterceptType == appsv1alpha1.InterceptTypeWhite {
			result = &ValidateResult{Allowed: false, Reason: "No rule match", Message: "Default strategy is white, should deny"}
			break
		}
	}
	if result == nil {
		result = &ValidateResult{Allowed: true, Reason: "No rule match", Message: "Default strategy is black, should allow"}
	}
	return result
}

// ValidateRest validate a rest request
// TODO: consider regex matching
func ValidateRest(URL string, method string) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result, "cost time", time.Since(now).String())
	}()

	if !EnableCircuitBreaker {
		result = ValidateResult{Allowed: true, Reason: "disable CircuitBreaker", Force: true}
		return result
	}

	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		limitings, limiters, states := defaultLimiterStore.byIndex(IndexRest, url)
		if len(limitings) == 0 {
			continue
		}
		result = doValidation(limitings, limiters, states, false)
		return result
	}
	result = ValidateResult{Allowed: true, Reason: "No rule match", Force: true}
	return result
}

func ValidateRestWithOption(URL string, method string, isPre bool) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate rest", "URL", URL, "method", method, "result", result, "cost time", time.Since(now).String())
	}()

	if !EnableCircuitBreaker {
		result = ValidateResult{Allowed: true, Reason: "disable CircuitBreaker", Force: true}
		return result
	}

	urls := generateWildcardUrls(URL, method)
	for _, url := range urls {
		limitings, limiters, states := defaultLimiterStore.byIndex(IndexRest, url)
		if len(limitings) == 0 {
			continue
		}
		result = doValidation(limitings, limiters, states, isPre)
		return result
	}
	result = ValidateResult{Allowed: true, Reason: "No rule match", Force: true}
	return result
}

// ValidateResource validate a request to api server
// TODO: consider regex matching
func ValidateResource(namespace, apiGroup, resource, verb string) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate resource", "namespace", namespace, "apiGroup", apiGroup, "resource", resource, "verb", verb, "result", result, "cost time", time.Since(now).String())
	}()

	if !EnableCircuitBreaker {
		result = ValidateResult{Allowed: true, Reason: "disable CircuitBreaker", Force: true}
		return result
	}

	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		limitings, limiters, states := defaultLimiterStore.byIndex(IndexResource, seed)
		if len(limitings) == 0 {
			continue
		}
		result = doValidation(limitings, limiters, states, false)
		return result
	}
	result = ValidateResult{Allowed: true, Reason: "No rule match"}
	return result
}

func ValidateResourceWithOption(namespace, apiGroup, resource, verb string, isPre bool) (result ValidateResult) {
	now := time.Now()
	defer func() {
		logger.Info("validate resource", "namespace", namespace, "apiGroup", apiGroup, "resource", resource, "verb", verb, "result", result, "cost time", time.Since(now).String())
	}()

	if !EnableCircuitBreaker {
		result = ValidateResult{Allowed: true, Reason: "disable CircuitBreaker", Force: true}
		return result
	}

	seeds := generateWildcardSeeds(namespace, apiGroup, resource, verb)
	for _, seed := range seeds {
		limitings, limiters, states := defaultLimiterStore.byIndex(IndexResource, seed)
		if len(limitings) == 0 {
			continue
		}
		result = doValidation(limitings, limiters, states, isPre)
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

func doValidation(limitings []*appsv1alpha1.Limiting, limiters []*rate.Limiter, states []*state, isPre bool) ValidateResult {
	result := ValidateResult{
		Allowed: true,
		Reason:  "Default allow",
		Force:   true,
	}
	for idx := range limitings {
		// check current circuit breaker status first
		status, _, _ := states[idx].read()
		switch status {
		case appsv1alpha1.BreakerStatusOpened: // cb already opened, just refuse
			result.Allowed = false
			result.Reason = "CircuitBreakerTriggered"
			result.Message = fmt.Sprintf("the circuit breaker is triggered. Limiting rule name: %s", limitings[idx].Name)
		}
		// check limiter and policy
		if limitings[idx].ValidatePolicy == appsv1alpha1.AfterHttpSuccess && isPre {
			switch limitings[idx].TriggerPolicy {
			case appsv1alpha1.TriggerPolicyNormal: // normal policy, determine by limiter, and trigger cb open when refuse
				if states[idx].status == appsv1alpha1.BreakerStatusClosed {
					result.Reason = result.Reason + "-PreAllow"
					// should acquire token after
					result.Force = false
				} else {
					result.Reason = result.Reason + "-PreReject"
				}
				continue
			}
		}
		switch limitings[idx].TriggerPolicy {
		case appsv1alpha1.TriggerPolicyForceOpened: // force open policy, just refuse
			states[idx].triggerBreaker()
			result.Allowed = false
			result.Reason = "CircuitBreakerForceOpened"
			result.Message = fmt.Sprintf("the circuit breaker is force opened. Limiting rule name: %s", limitings[idx].Name) // force
		case appsv1alpha1.TriggerPolicyForceClosed: // force close policy, just allow
			states[idx].recoverBreaker()
			result.Allowed = true
			result.Reason = "CircuitBreakerForceClosed"
			result.Message = fmt.Sprintf("the circuit breaker is force closed. Limiting rule name: %s", limitings[idx].Name)
		case appsv1alpha1.TriggerPolicyLimiterOnly: // limiter only policy, determine by limiter
			states[idx].recoverBreaker()
			if !limiters[idx].Allow() {
				result.Allowed = false
				result.Reason = "OverLimit"
				result.Message = fmt.Sprintf("the request is over limit by limiting rule: %s", limitings[idx].Name)
			}
		case appsv1alpha1.TriggerPolicyNormal: // normal policy, determine by limiter, and trigger cb open when refuse
			if states[idx].status == appsv1alpha1.BreakerStatusClosed {
				if !limiters[idx].Allow() {
					result.Allowed = false
					result.Reason = "OverLimit"
					result.Message = fmt.Sprintf("the request is over limit by limiting rule: %s", limitings[idx].Name)
					if limitings[idx].RecoverPolicy == appsv1alpha1.RecoverPolicySleepingWindow {
						d, _ := time.ParseDuration(limitings[idx].Properties["sleepingWindowSize"])
						states[idx].triggerBreakerWithTimeWindow(d)
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

func StartSync(ctx context.Context, client client.Client, stop <-chan struct{}) {
	// sync status every 5 seconds
	wait.Until(func() {
		defaultLimiterStore.iterate(func(key string) {
			limiting := defaultLimiterStore.rules[key]
			limiter := defaultLimiterStore.limiters[key]
			s := defaultLimiterStore.states[key]
			status, _, _ := s.read()
			nowTime := metav1.Now()
			arr := strings.Split(key, ":")
			namespace, name, ruleName := arr[0], arr[1], arr[2]

			// To get the latest tokens， should do AllowN function
			//limiter.AllowN(time.Now(), 0)
			tokens, last := parseLimiter(limiter)

			// register breaker lease if necessary
			if limiting.TriggerPolicy == appsv1alpha1.TriggerPolicyNormal &&
				limiting.RecoverPolicy == appsv1alpha1.RecoverPolicySleepingWindow {
				defaultBreakerLease.registerState(s)
			}

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				var cb appsv1alpha1.CircuitBreaker
				err := client.Get(context.TODO(), types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				}, &cb)
				if err != nil {
					return err
				}
				cleared := false
				cb.Status.LastUpdatedTime = metav1.Now()
				limit := make([]appsv1alpha1.LimitingSnapshot, 0, len(cb.Status.LimitingSnapshots))
				for _, snapshot := range cb.Status.LimitingSnapshots {
					if snapshot.Name == ruleName && utils.EnvPodIpVal == snapshot.Endpoint && snapshot.PodName == utils.EnvPodNameVal {
						limit = append(limit, appsv1alpha1.LimitingSnapshot{
							Name:               snapshot.Name,
							LastTransitionTime: &nowTime,
							Status:             status,
							Endpoint:           utils.EnvPodIpVal,
							PodName:            utils.EnvPodNameVal,
							Bucket: appsv1alpha1.BucketSnapshot{
								// TODO: last acquire fix
								LastAcquireTimestamp: uint64(last.Unix()),
								AvailableTokens:      uint64(math.Floor(tokens + 0.5)),
							},
						})
					} else if utils.EnvPodIpVal != snapshot.Endpoint && snapshot.LastTransitionTime != nil && nowTime.Sub(snapshot.LastTransitionTime.Time) > statusTTL {
						cleared = true
						logger.Info("clear ttl limiting snapshot", "endpoint", snapshot.Endpoint, "name", snapshot.Name)
						continue
					} else if utils.EnvPodIpVal != snapshot.Endpoint && snapshot.LastTransitionTime == nil {
						cleared = true
						logger.Info("clear null lastTransitionTime limiting snapshot", "endpoint", snapshot.Endpoint, "name", snapshot.Name)
						continue
					} else {
						limit = append(limit, *snapshot.DeepCopy())
					}
				}
				if cleared {
					oldSnapByte, _ := json.Marshal(cb.Status.LimitingSnapshots)
					newSnapByte, _ := json.Marshal(limit)
					logger.Info("clear ttl status", "old", string(oldSnapByte), "new", string(newSnapByte))
				}
				cb.Status.LimitingSnapshots = limit

				return client.Status().Update(ctx, &cb)
			})

			if err != nil {
				logger.Error(err, "failed to sync circuit breaker status", "namespace", namespace, "name", name)
			}
		})
	}, syncPeriod, stop)
}

// parseLimiter parse tokens and last acquire time from rate.Limiter
// TODO: may cause data racing, since the mutex in the rate.limiter is not considered here
func parseLimiter(limiter *rate.Limiter) (float64, time.Time) {
	p := unsafe.Pointer(limiter)
	sfTokens, _ := reflect.TypeOf(limiter).Elem().FieldByName("tokens")
	tokensPtr := unsafe.Pointer(uintptr(p) + sfTokens.Offset)
	sfLast, _ := reflect.TypeOf(limiter).Elem().FieldByName("last")
	lastPtr := unsafe.Pointer(uintptr(p) + sfLast.Offset)

	tokens := (*float64)(tokensPtr)
	last := (*time.Time)(lastPtr)
	return *tokens, *last
}
