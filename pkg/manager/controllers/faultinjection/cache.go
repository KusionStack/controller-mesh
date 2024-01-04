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
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultPodConfigCache = &podCache{
		currentHash: make(map[string]sets.Set[string]),
	}
)

type podCache struct {
	currentHash map[string]sets.Set[string]
	mu          sync.RWMutex
}

func cacheKey(namespace, podName string) string {
	return namespace + "/" + podName
}

func (c *podCache) Has(namespace, podName, cbHash string) bool {
	key := cacheKey(namespace, podName)
	configs, ok := c.currentHash[key]
	if !ok {
		return false
	}
	return configs.Has(cbHash)
}

func (c *podCache) Add(namespace, podName, cbHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(namespace, podName)
	_, ok := c.currentHash[key]
	if !ok {
		c.currentHash[key] = sets.New[string](cbHash)
		return
	}
	c.currentHash[key].Insert(cbHash)
}

func (c *podCache) Delete(namespace, podName string, cbHash ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(namespace, podName)
	if len(cbHash) == 0 {
		delete(c.currentHash, key)
		return
	}
	_, ok := c.currentHash[key]
	if !ok {
		return
	}
	c.currentHash[key].Delete(cbHash...)
}

func newDeleteProcessor(c client.Client, ctx context.Context) *deleteProcessor {
	processor := &deleteProcessor{
		q:   workqueue.NewNamedDelayingQueue("delete-processor"),
		ctx: ctx,
		processFunc: func(item deleteItem) error {
			po := &v1.Pod{}
			if err := c.Get(ctx, types.NamespacedName{
				Namespace: item.Namespace,
				Name:      item.PodName,
			}, po); err != nil {
				return client.IgnoreNotFound(err)
			}
			if po.DeletionTimestamp != nil {
				return nil
			}
			return disableConfig(ctx, po.Status.PodIP, item.ConfigName)
		},
	}
	go processor.processLoop()
	return processor
}

type deleteProcessor struct {
	q           workqueue.DelayingInterface
	processFunc func(deleteItem) error
	ctx         context.Context
}

func (d *deleteProcessor) Add(namespace, podName, config string) {
	d.q.Add(deleteItem{Namespace: namespace, PodName: podName, ConfigName: config})
}

func (d *deleteProcessor) processLoop() {
	go func() {
		for {
			item, shutdown := d.q.Get()
			if shutdown {
				break
			}
			val := item.(deleteItem)
			if err := d.processFunc(val); err != nil {
				d.q.AddAfter(item, 5*time.Second)
			}
			d.q.Done(item)
		}
	}()
	<-d.ctx.Done()
	d.q.ShutDown()
}

type deleteItem struct {
	Namespace  string
	PodName    string
	ConfigName string
}
