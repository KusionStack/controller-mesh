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

package shardingconfigserver

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ctrlmeshv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/ctrlmesh/pkg/utils"
)

type podEventHandler struct {
	reader client.Reader
}

func (h *podEventHandler) Create(e event.CreateEvent, q workqueue.RateLimitingInterface) {
	pod := e.Object.(*v1.Pod)
	cfg, err := h.getShardingConfigForPod(pod)
	if err != nil {
		klog.Warningf("Failed to get ShardingConfig for Pod %s/%s creation: %v", pod.Namespace, pod.Name, err)
		return
	} else if cfg == nil {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: cfg.Namespace,
		Name:      cfg.Name,
	}})
}

func (h *podEventHandler) Update(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	oldPod := e.ObjectOld.(*v1.Pod)
	newPod := e.ObjectNew.(*v1.Pod)

	cfg, err := h.getShardingConfigForPod(newPod)
	if err != nil {
		klog.Warningf("Failed to get ShardingConfig for Pod %s/%s update: %v", newPod.Namespace, newPod.Name, err)
		return
	} else if cfg == nil {
		return
	}

	if newPod.DeletionTimestamp != nil {
		if oldPod.DeletionTimestamp == nil || len(getRunningContainers(oldPod)) != len(getRunningContainers(newPod)) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: cfg.Namespace,
				Name:      cfg.Name,
			}})
		}
		return
	}

	if oldPod.Status.Phase != newPod.Status.Phase || utils.IsPodReady(oldPod) != utils.IsPodReady(newPod) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
		}})
	}
}

func (h *podEventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	pod := e.Object.(*v1.Pod)
	cfg, err := h.getShardingConfigForPod(pod)
	if err != nil {
		klog.Warningf("Failed to get ShardingConfig for Pod %s/%s delete: %v", pod.Namespace, pod.Name, err)
		return
	} else if cfg == nil {
		return
	}
	podHashExpectation.Delete(pod.UID)
	podDeletionExpectation.Delete(pod.UID)
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: cfg.Namespace,
		Name:      cfg.Name,
	}})
}

func (h *podEventHandler) Generic(e event.GenericEvent, q workqueue.RateLimitingInterface) {
}

func (h *podEventHandler) getShardingConfigForPod(pod *v1.Pod) (*ctrlmeshv1alpha1.ShardingConfig, error) {
	name := pod.Labels[ctrlmeshv1alpha1.ShardingConfigInjectedKey]
	if name == "" {
		return nil, nil
	}
	cfg := &ctrlmeshv1alpha1.ShardingConfig{}
	err := h.reader.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: name}, cfg)
	return cfg, err
}
