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
	"regexp"
	"sync"

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
	lastTransitionTime *metav1.Time
}

func (s *state) read() (lastTime *metav1.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastTransitionTime != nil {
		lastTime = s.lastTransitionTime.DeepCopy()
	}
	return
}

// regexpInfo is contain regexp info
type regexpInfo struct {
	// reg is after regexp compiled result
	reg *regexp.Regexp

	// method is represent url request method
	method string
}

// faultInjectionStore is a thread-safe local store for faultinjection rules
type store struct {
	mu sync.RWMutex
	// rule cache, key is {fi.namespace}:{fi.name}:{fi.name}
	rules map[string]*ctrlmeshproto.HTTPFaultInjection
	// indices: resource indices and rest indices
	indices indices
	// normalIndex cache, key is {faultinjection.Content}:{faultinjection.Method}, value is {fi.namespace}:{fi.name}:{rule.name}
	normalIndexes map[string]sets.Set[string]
	// regexpIndex cache, key is {fi.namespace}:{fi.name}:{rule.name}, value is regexpInfo
	regexpIndexes map[string][]*regexpInfo

	faultInjectionLease *lease

	ctx context.Context
}

func newFaultInjectionStore(ctx context.Context) *store {
	s := &store{
		rules:               make(map[string]*ctrlmeshproto.HTTPFaultInjection),
		indices:             indices{},
		faultInjectionLease: newFaultInjectionLease(ctx),
		ctx:                 ctx,
		normalIndexes:       make(map[string]sets.Set[string]),
		regexpIndexes:       make(map[string][]*regexpInfo),
	}
	return s
}

// createOrUpdateRule stores new rules (or updates existing rules) in local store
func (s *store) createOrUpdateRule(key string, faultInjection *ctrlmeshproto.HTTPFaultInjection) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldOne, ok := s.rules[key]
	if !ok {
		// all new, just assign rules and states, and update indices
		s.rules[key] = faultInjection
		s.updateIndices(nil, faultInjection, key)
	} else {
		// there is an old one, assign the new rule, update indices
		s.rules[key] = faultInjection.DeepCopy()
		s.updateIndices(oldOne, faultInjection, key)
	}
}

// deleteRule deletes rules by ley
func (s *store) deleteRule(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if obj, ok := s.rules[key]; ok {
		s.deleteFromIndices(obj, key)
		delete(s.rules, key)
	}
}

// byIndex lists rules by a specific index
func (s *store) byIndex(indexName, indexedValue string) ([]*ctrlmeshproto.HTTPFaultInjection, []*state) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	indexFunc := indexFunctions[indexName]
	if indexFunc == nil {
		return nil, nil
	}
	idx := s.indices[indexName]
	set := idx[indexedValue]

	limitings := make([]*ctrlmeshproto.HTTPFaultInjection, 0, set.Len())
	states := make([]*state, 0, set.Len())
	for key := range set {
		limitings = append(limitings, s.rules[key])

	}
	return limitings, states
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

	for _, newStringMatch := range newOne.Match.StringMatch {
		if newStringMatch.MatchType == ctrlmeshproto.StringMatch_NORMAL {
			for _, content := range newStringMatch.Contents {
				for _, method := range newStringMatch.Methods {
					urlMethod := indexForRest(content, method)
					set := s.normalIndexes[urlMethod]
					if set == nil {
						set = sets.New[string]()
						s.normalIndexes[urlMethod] = set
					}
					set.Insert(key)
				}
			}
		} else if newStringMatch.MatchType == ctrlmeshproto.StringMatch_REGEXP {
			for _, content := range newStringMatch.Contents {
				if reg, err := regexp.Compile(content); err != nil {
					klog.Error("Regexp compile with error %v", err)
				} else {
					for _, method := range newStringMatch.Methods {
						s.regexpIndexes[key] = append(s.regexpIndexes[key], &regexpInfo{reg: reg, method: method})
					}
				}
			}
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
	for _, oldStringMatch := range oldOne.Match.StringMatch {
		if oldStringMatch.MatchType == ctrlmeshproto.StringMatch_NORMAL {
			for _, content := range oldStringMatch.Contents {
				for _, method := range oldStringMatch.Methods {
					urlMethod := indexForRest(content, method)
					if s.normalIndexes[urlMethod] != nil {
						s.normalIndexes[urlMethod].Delete(key)
						if s.normalIndexes[urlMethod].Len() == 0 {
							delete(s.normalIndexes, urlMethod)
						}
					}
				}
			}
		} else if oldStringMatch.MatchType == ctrlmeshproto.StringMatch_REGEXP {
			for regKey := range s.regexpIndexes {
				if regKey == key {
					delete(s.regexpIndexes, key)
				}
			}
		}
	}
}

func indexForResource(namespace, apiGroup, resource, verb string) string {
	return fmt.Sprintf("%s:%s:%s:%s", namespace, apiGroup, resource, verb)
}

func indexForRest(URL, method string) string {
	return fmt.Sprintf("%s:%s", URL, method)
}

func indexFuncForResource(faultinjection *ctrlmeshproto.HTTPFaultInjection) []string {
	var result []string
	for _, rule := range faultinjection.Match.Resources {
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

func indexFuncForRest(faultInjection *ctrlmeshproto.HTTPFaultInjection) []string {
	var result []string
	for _, rest := range faultInjection.Match.HttpMatch {
		for _, url := range rest.Url {
			for _, method := range rest.Method {
				result = append(result, indexForRest(url, method))
			}
		}
	}
	return result
}
