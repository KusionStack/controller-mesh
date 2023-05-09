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

package cache

import (
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

var (
	selectorsByObject cache.SelectorsByObject
	defaultSelector   *cache.ObjectSelector
)

func WarpNewCacheWithSelector(selector *cache.ObjectSelector, selectorsByObj cache.SelectorsByObject) func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
	return func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
		opts.DefaultSelector = *selector
		opts.SelectorsByObject = selectorsByObj
		return cache.New(config, opts)
	}
}

func WrapNewCache(config *rest.Config, opts cache.Options) (cache.Cache, error) {
	if defaultSelector != nil {
		opts.DefaultSelector = *defaultSelector
	}
	if selectorsByObject != nil {
		opts.SelectorsByObject = selectorsByObject
	}
	return cache.New(config, opts)
}

func SetGlobalSelector(objectSelector *cache.ObjectSelector) {
	defaultSelector = objectSelector
}

func SetSelectorsByObject(selectorsByObj cache.SelectorsByObject) {
	selectorsByObject = selectorsByObj
}
