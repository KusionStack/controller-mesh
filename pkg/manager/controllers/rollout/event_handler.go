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

package rollout

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/KusionStack/kridge/pkg/apis/kridge"
)

type podEventHandler struct {
	client client.Client
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (h *podEventHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	//oldOne := evt.ObjectOld.(*v1.Pod)
	newOne := evt.ObjectNew.(*v1.Pod)
	if !onMeshStsControl(newOne) || !inRolling(evt.ObjectNew.GetNamespace(), newOne.OwnerReferences[0].Name, h.client) {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      newOne.OwnerReferences[0].Name,
		Namespace: evt.ObjectNew.GetNamespace(),
	}})
}

// Create implements EventHandler
func (h *podEventHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	newOne := evt.Object.(*v1.Pod)
	if !onMeshStsControl(newOne) || !inRolling(evt.Object.GetNamespace(), newOne.OwnerReferences[0].Name, h.client) {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      newOne.OwnerReferences[0].Name,
		Namespace: evt.Object.GetNamespace(),
	}})
}

// Delete implements EventHandler
func (h *podEventHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	rollingDeleteExpectation.DeleteExpectation(evt.Object.GetNamespace() + "/" + evt.Object.GetName())
}

// Generic implements EventHandler
func (h *podEventHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}

type stsEventHandler struct {
	client client.Client
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (h *stsEventHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	if !inRolling(evt.ObjectNew.GetNamespace(), evt.ObjectNew.GetName(), h.client) {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.ObjectNew.GetName(),
		Namespace: evt.ObjectNew.GetNamespace(),
	}})
}

// Create implements EventHandler
func (h *stsEventHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	if !inRolling(evt.Object.GetNamespace(), evt.Object.GetName(), h.client) {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.Object.GetName(),
		Namespace: evt.Object.GetNamespace(),
	}})
}

// Delete implements EventHandler
func (h *stsEventHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
}

// Generic implements EventHandler
func (h *stsEventHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}

func onMeshStsControl(po *v1.Pod) bool {
	_, ok := po.Labels[kridge.KdEnableProxyKey]
	if !ok {
		return false
	}
	if po.OwnerReferences == nil || len(po.OwnerReferences) == 0 || po.OwnerReferences[0].Kind != "StatefulSet" {
		return false
	}
	return true
}

func inRolling(namespace, name string, c client.Client) bool {
	sts := &appsv1.StatefulSet{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, sts); err != nil {
		klog.Infof("fail to get sts %s/%s", namespace, name)
		return false
	}
	if sts.Labels == nil {
		return false
	}
	_, ok := sts.Labels[kridge.KdInRollingLabel]
	return ok
}
