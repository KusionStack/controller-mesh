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

	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

// faultInjectionStore is a thread-safe local store for faultinjection rules
type store struct {
	mu sync.RWMutex

	resourcesMatchRules map[string]*ctrlmeshproto.HTTPFaultInjection
	restMatchRules      map[string]*ctrlmeshproto.HTTPFaultInjection
	regexMap            map[string]*regexp.Regexp

	ctx context.Context
}

func newFaultInjectionStore(ctx context.Context) *store {
	s := &store{
		regexMap:            map[string]*regexp.Regexp{},
		resourcesMatchRules: map[string]*ctrlmeshproto.HTTPFaultInjection{},
		restMatchRules:      map[string]*ctrlmeshproto.HTTPFaultInjection{},
		ctx:                 ctx,
	}
	return s
}

// createOrUpdateRule stores new rules (or updates existing rules) in local store
func (s *store) createOrUpdateRule(key string, faultInjection *ctrlmeshproto.HTTPFaultInjection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(faultInjection.Match.Resources) > 0 {
		s.resourcesMatchRules[key] = faultInjection
	}
	if len(faultInjection.Match.HttpMatch) > 0 {
		s.restMatchRules[key] = faultInjection
		for _, match := range faultInjection.Match.HttpMatch {
			if match.Path != nil && match.Path.Regex != "" {
				reg, err := regexp.Compile(match.Path.Regex)
				if err == nil {
					s.regexMap[match.Path.Regex] = reg
				} else {
					klog.Errorf("fail to compile regexp %s", match.Path.Regex)
				}
			}
			if match.Host != nil && match.Host.Regex != "" {
				reg, err := regexp.Compile(match.Host.Regex)
				if err == nil {
					s.regexMap[match.Host.Regex] = reg
				} else {
					klog.Errorf("fail to compile regexp %s", match.Host.Regex)
				}
			}
		}
	}
}

// deleteRule deletes rules by ley
func (s *store) deleteRule(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.resourcesMatchRules, key)
	delete(s.restMatchRules, key)
}

func indexForResource(namespace, apiGroup, resource, verb string) string {
	return fmt.Sprintf("%s:%s:%s:%s", namespace, apiGroup, resource, verb)
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
