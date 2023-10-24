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
	hash, _ := c.currentHash[key]
	return hash
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
	return
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
