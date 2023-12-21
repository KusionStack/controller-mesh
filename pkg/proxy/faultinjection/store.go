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
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"

	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

const (
	IndexResource = "resource"
	IndexRest     = "rest"
)

var (
	indexFunctions = map[string]func(faultinjection *ctrlmeshproto.HTTPFaultInjection) []string{
		IndexResource: indexFuncForResource,
		IndexRest:     indexFuncForRest,
	}
)

type index map[string]sets.Set[string]

type indices map[string]index

type state struct {
	mu                 sync.RWMutex
	key                string
	state              ctrlmeshproto.FaultInjectionState
	lastTransitionTime *metav1.Time
	recoverAt          *metav1.Time
}

func (s *state) read() (state ctrlmeshproto.FaultInjectionState, lastTime *metav1.Time, recoverTime *metav1.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastTransitionTime != nil {
		lastTime = s.lastTransitionTime.DeepCopy()
	}
	if s.recoverAt != nil {
		recoverTime = s.recoverAt.DeepCopy()
	}
	state = s.state
	return
}

func (s *state) triggerFaultInjection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tm := metav1.Now()
	if s.transitionTo(ctrlmeshproto.FaultInjectionState_STATEOPENED, &tm) {
		s.recoverAt = nil
	}
}

func (s *state) triggerBreakerWithTimeWindow(windowSize time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tm := metav1.Now()
	if s.transitionTo(ctrlmeshproto.FaultInjectionState_STATEOPENED, &tm) {
		t := metav1.Now().Add(windowSize)
		s.recoverAt = &metav1.Time{Time: t}
	}
}

func (s *state) recoverBreaker() {
	s.mu.Lock()
	defer s.mu.Unlock()
	tm := metav1.Now()
	if s.transitionTo(ctrlmeshproto.FaultInjectionState_STATECLOSED, &tm) {
		s.recoverAt = nil
	}
}

func (s *state) transitionTo(newStatus ctrlmeshproto.FaultInjectionState, t *metav1.Time) bool {
	if s.state != newStatus {
		s.state = newStatus
		s.lastTransitionTime = t
		return true
	}
	return false
}

// limiterStore is a thread-safe local store for faultinjection rules and limiters
type store struct {
	mu sync.RWMutex
	// limiter cache, key is {cb.namespace}:{cb.name}:{rule.name}
	limiters map[string]*rate.Limiter
	// rule cache, key is {cb.namespace}:{cb.name}:{rule.name}
	rules map[string]*ctrlmeshproto.HTTPFaultInjection
	// circuit breaker states
	states map[string]*state
	// limiter indices: resource indices and rest indices
	indices indices

	faultinjectionLease *lease

	ctx context.Context
}

func newLimiterStore(ctx context.Context) *store {
	s := &store{
		limiters:            make(map[string]*rate.Limiter),
		rules:               make(map[string]*ctrlmeshproto.HTTPFaultInjection),
		states:              make(map[string]*state),
		indices:             indices{},
		faultinjectionLease: newFaultInjectionLease(ctx),
		ctx:                 ctx,
	}
	return s
}

func (s *store) registerState(st *state) {
	s.faultinjectionLease.registerState(st)
}

// createOrUpdateRule stores new rules (or updates existing rules) in local store
func (s *store) createOrUpdateRule(key string, faultinjection *ctrlmeshproto.HTTPFaultInjection, snapshot *ctrlmeshproto.FaultInjectionSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldOne, ok := s.rules[key]
	if !ok {
		// all new, just assign limiter, rules and states, and update indices
		s.rules[key] = faultinjection
		//s.limiters[key] = rateLimiter(faultinjection.Bucket)
		s.states[key] = &state{
			key:                key,
			state:              snapshot.State,
			lastTransitionTime: snapshot.LastTransitionTime,
		}
		s.updateIndices(nil, faultinjection, key)
	} else {
		// there is an old one, assign the new rule, update indices
		s.rules[key] = faultinjection.DeepCopy()
		s.updateIndices(oldOne, faultinjection, key)
		// if limiter bucket changes, assign a new limiter and re-calculate the limits
		// if !bucketEquals(oldOne.Bucket, faultinjection.Bucket) {
		// 	s.limiters[key] = rateLimiter(faultinjection.Bucket)
		// }
	}
}

// deleteRule deletes rules by ley
func (s *store) deleteRule(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if obj, ok := s.rules[key]; ok {
		s.deleteFromIndices(obj, key)
		delete(s.rules, key)
		delete(s.limiters, key)
		delete(s.states, key)
	}
}

// byKey get the rule by a specific key
func (s *store) byKey(key string) (*ctrlmeshproto.HTTPFaultInjection, *rate.Limiter, *state) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rules[key], s.limiters[key], s.states[key]
}

// byIndex lists rules by a specific index
func (s *store) byIndex(indexName, indexedValue string) ([]*ctrlmeshproto.HTTPFaultInjection, []*rate.Limiter, []*state) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	indexFunc := indexFunctions[indexName]
	if indexFunc == nil {
		return nil, nil, nil
	}
	idx := s.indices[indexName]
	set := idx[indexedValue]

	limitings := make([]*ctrlmeshproto.HTTPFaultInjection, 0, set.Len())
	limiters := make([]*rate.Limiter, 0, set.Len())
	states := make([]*state, 0, set.Len())
	for key := range set {
		limitings = append(limitings, s.rules[key])
		limiters = append(limiters, s.limiters[key])
		states = append(states, s.states[key])
	}
	return limitings, limiters, states
}

// updateIndices updates current indices of the rules
func (s *store) updateIndices(oldOne, newOne *ctrlmeshproto.HTTPFaultInjection, key string) {
	if oldOne != nil {
		s.deleteFromIndices(oldOne, key)
	}
	for name, indexFunc := range indexFunctions {
		indexValues := indexFunc(newOne)
		idx := s.indices[name]
		if idx == nil {
			idx = index{}
			s.indices[name] = idx
		}

		for _, indexValue := range indexValues {
			set := idx[indexValue]
			if set == nil {
				set = sets.New[string]()
				idx[indexValue] = set
			}
			set.Insert(key)
		}
	}
}

// deleteFromIndices deletes indices of specified keys
func (s *store) deleteFromIndices(oldOne *ctrlmeshproto.HTTPFaultInjection, key string) {
	for name, indexFunc := range indexFunctions {
		indexValues := indexFunc(oldOne)
		idx := s.indices[name]
		if idx == nil {
			continue
		}
		for _, indexValue := range indexValues {
			set := idx[indexValue]
			if set != nil {
				set.Delete(key)
				if len(set) == 0 {
					delete(idx, indexValue)
				}
			}
		}
	}
}

func (s *store) iterate(f func(key string)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key := range s.rules {
		f(key)
	}
}

func bucketEquals(oldOne, newOne *ctrlmeshproto.Bucket) bool {
	return oldOne.Interval == newOne.Interval && oldOne.Limit == newOne.Limit && oldOne.Burst == newOne.Burst
}

func rateLimiter(bucket *ctrlmeshproto.Bucket) *rate.Limiter {
	// ensure no failure with webhook
	interval, _ := time.ParseDuration(bucket.Interval)

	r := func() rate.Limit {
		if bucket.Limit == 0 {
			return rate.Every(math.MaxInt64)
		}
		return rate.Every(interval / time.Duration(bucket.Limit))
	}()
	rt := rate.NewLimiter(r, int(bucket.Burst))
	rt.AllowN(time.Now(), 0)
	return rt
}

func indexForResource(namespace, apiGroup, resource, verb string) string {
	return fmt.Sprintf("%s:%s:%s:%s", namespace, apiGroup, resource, verb)
}

func indexForRest(URL, method string) string {
	return fmt.Sprintf("%s:%s", URL, method)
}

func indexFuncForResource(faultinjection *ctrlmeshproto.HTTPFaultInjection) []string {
	var result []string
	for _, rule := range faultinjection.Match.RelatedResources {
		for _, namespace := range rule.Namespaces {
			for _, apiGroup := range rule.ApiGroups {
				for _, resource := range rule.Resources {
					for _, verb := range rule.Verbs {
						result = append(result, indexForResource(namespace, apiGroup, resource, verb))
					}
				}
			}
		}
	}
	return result
}

func indexFuncForRest(faultinjection *ctrlmeshproto.HTTPFaultInjection) []string {
	var result []string
	for _, rest := range faultinjection.Match.RestRules {
		for _, url := range rest.Url {
			for _, method := range rest.Method {
				result = append(result, indexForRest(url, method))
			}
		}
	}
	return result
}

func slicesFilterWildcard(slices []string) []string {
	for _, slice := range slices {
		if slice == "*" {
			return []string{"*"}
		}
	}
	return slices
}

// regexpInfo is contain regexp info
type regexpInfo struct {
	// reg is after regexp compiled result
	reg *regexp.Regexp
	// regType is represent intercept type
	regType ctrlmeshproto.TrafficInterceptRule_InterceptType
	// method is represent url request method
	method string
}

// trafficInterceptStore is a thread-safe local store for traffic intercept rules
type trafficInterceptStore struct {
	mu sync.RWMutex
	// rules cache, key is {cb.namespace}:{cb.name}:{rule.name}
	rules map[string]*ctrlmeshproto.TrafficInterceptRule
	// normalIndex cache, key is {trafficInterceptStore.Content}:{trafficInterceptStore.Method}, value is {cb.namespace}:{cb.name}:{rule.name}
	normalIndexes map[string]sets.Set[string]
	// regexpIndex cache, key is {cb.namespace}:{cb.name}:{rule.name}, value is regexpInfo
	regexpIndexes map[string][]*regexpInfo
}

func newTrafficInterceptStore() *trafficInterceptStore {
	return &trafficInterceptStore{
		rules:         make(map[string]*ctrlmeshproto.TrafficInterceptRule),
		normalIndexes: make(map[string]sets.Set[string]),
		regexpIndexes: make(map[string][]*regexpInfo),
	}
}

// createOrUpdateRule stores new rules (or updates existing rules) in local trafficInterceptStore
func (s *trafficInterceptStore) createOrUpdateRule(key string, trafficInterceptRule *ctrlmeshproto.TrafficInterceptRule) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldOne, ok := s.rules[key]
	if !ok {
		s.rules[key] = trafficInterceptRule.DeepCopy()
		s.updateIndex(nil, trafficInterceptRule, key)
	} else {
		// there is an old one, assign the new rule, update indices
		s.rules[key] = trafficInterceptRule.DeepCopy()
		s.updateIndex(oldOne, trafficInterceptRule, key)
	}
}

// deleteRule deletes rules by ley
func (s *trafficInterceptStore) deleteRule(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if obj, ok := s.rules[key]; ok {
		s.deleteFromIndex(obj, key)
	}
}

// updateIndices updates current indices of the rules
func (s *trafficInterceptStore) updateIndex(oldOne, newOne *ctrlmeshproto.TrafficInterceptRule, key string) {
	if oldOne != nil {
		s.deleteFromIndex(oldOne, key)
	}
	if newOne.ContentType == ctrlmeshproto.TrafficInterceptRule_NORMAL {
		for _, content := range newOne.Contents {
			for _, method := range newOne.Methods {
				urlMethod := indexForRest(content, method)
				set := s.normalIndexes[urlMethod]
				if set == nil {
					set = sets.New[string]()
					s.normalIndexes[urlMethod] = set
				}
				set.Insert(key)
			}
		}
	} else if newOne.ContentType == ctrlmeshproto.TrafficInterceptRule_REGEXP {
		for _, content := range newOne.Contents {
			if reg, err := regexp.Compile(content); err != nil {
				klog.Error("Regexp compile with error %v", err)
			} else {
				for _, method := range newOne.Methods {
					s.regexpIndexes[key] = append(s.regexpIndexes[key], &regexpInfo{reg: reg, regType: newOne.InterceptType, method: method})
				}
			}
		}
	}
}

// deleteFromIndex delete index of specified key
func (s *trafficInterceptStore) deleteFromIndex(oldOne *ctrlmeshproto.TrafficInterceptRule, key string) {
	if oldOne.ContentType == ctrlmeshproto.TrafficInterceptRule_NORMAL {
		for _, content := range oldOne.Contents {
			for _, method := range oldOne.Methods {
				urlMethod := indexForRest(content, method)
				if s.normalIndexes[urlMethod] != nil {
					s.normalIndexes[urlMethod].Delete(key)
					if s.normalIndexes[urlMethod].Len() == 0 {
						delete(s.normalIndexes, urlMethod)
					}
				}
			}
		}
	} else if oldOne.ContentType == ctrlmeshproto.TrafficInterceptRule_REGEXP {
		for regKey := range s.regexpIndexes {
			if regKey == key {
				delete(s.regexpIndexes, key)
			}
		}
	}
}
