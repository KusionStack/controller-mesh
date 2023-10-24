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
	"sync"
)

var (
	defaultPodCache = &podCache{
		currentHash: make(map[string]string),
	}
	//defaultHashCache = &hashCache{
	//	store: make(map[string]sets.String),
	//}
)

type podCache struct {
	currentHash map[string]string
	mu          sync.RWMutex
}

func cacheKey(namespace, podName, cbName string) string {
	return namespace + "/" + podName + "/" + cbName
}

func (c *podCache) Get(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentHash[key]
}

func (c *podCache) Update(key string, hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentHash[key] = hash
}

func (c *podCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.currentHash, key)
}

//
//type hashCache struct {
//	store map[string]sets.String
//	mu    sync.RWMutex
//}
//
//func (h *hashCache) Get(hash string) sets.String {
//	h.mu.RLock()
//	defer h.mu.RUnlock()
//	val, ok := h.store[hash]
//	if !ok {
//		return sets.NewString()
//	}
//	return val.Clone()
//}
//
//func (h *hashCache) Add(hash string, items ...string) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	val, ok := h.store[hash]
//	if !ok {
//		h.store[hash] = sets.NewString(items...)
//	} else {
//		val.Insert(items...)
//	}
//}
//
//func (h *hashCache) Replace(hash string, items ...string) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	h.store[hash] = sets.NewString(items...)
//}
//
//func (h *hashCache) DeleteHash(hash string) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	delete(h.store, hash)
//}
//
//func (h *hashCache) DeleteVal(hash string, items ...string) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	val, ok := h.store[hash]
//	if ok {
//		val.Delete(items...)
//	}
//}
