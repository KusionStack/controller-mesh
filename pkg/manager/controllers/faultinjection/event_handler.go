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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

type podEventHandler struct {
	reader client.Reader
}

func (h *podEventHandler) Create(e event.CreateEvent, q workqueue.RateLimitingInterface) {
	pod := e.Object.(*v1.Pod)
	if !sidecarInjected(pod) {
		return
	}
	if !isProxyAvailable(pod) {
		return
	}
	faults, err := effectiveFaults(h.reader, pod)
	if err != nil {
		klog.Errorf("fail to get effective CircuitBreakers by pod %s/%s, %v", pod.Namespace, pod.Name, err)
		return
	}
	add(q, faults)
}

func (h *podEventHandler) Update(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	oldPod := e.ObjectOld.(*v1.Pod)
	newPod := e.ObjectNew.(*v1.Pod)
	if !sidecarInjected(oldPod) {
		return
	}
	if !isProxyAvailable(newPod) {
		return
	}
	if !isProxyAvailable(oldPod) {
		breakers, err := effectiveFaults(h.reader, newPod)
		if err != nil {
			klog.Errorf("fail to get effective CircuitBreakers by pod %s/%s, %v", newPod.Namespace, newPod.Name, err)
			return
		}
		add(q, breakers)
		return
	}
	faults, err := matchChangedFaults(h.reader, oldPod, newPod)
	if err != nil {
		klog.Errorf("fail to get effective CircuitBreakers by pod %s/%s, %v", newPod.Namespace, newPod.Name, err)
		return
	}
	add(q, faults)
}

func (h *podEventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	defaultPodConfigCache.Delete(e.Object.GetNamespace(), e.Object.GetName())
}

func (h *podEventHandler) Generic(e event.GenericEvent, q workqueue.RateLimitingInterface) {
}

// sidecarInjected check whether has proxy container
func sidecarInjected(po *v1.Pod) bool {
	for _, c := range po.Spec.Containers {
		if c.Name == constants.ProxyContainerName {
			return true
		}
	}
	return false
}

func add(q workqueue.RateLimitingInterface, items []*ctrlmeshv1alpha1.FaultInjection) {
	for _, item := range items {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: item.GetNamespace(),
			Name:      item.GetName(),
		}})
	}
}

func effectiveFaults(c client.Reader, po *v1.Pod) ([]*ctrlmeshv1alpha1.FaultInjection, error) {
	var res []*ctrlmeshv1alpha1.FaultInjection
	faults := &ctrlmeshv1alpha1.FaultInjectionList{}
	if err := c.List(context.TODO(), faults); err != nil {
		return nil, err
	}
	for i, b := range faults.Items {
		selector, err := metav1.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return nil, err
		}
		if selector.Matches(labels.Set(po.Labels)) {
			res = append(res, &faults.Items[i])
		}
	}
	return res, nil
}

func matchChangedFaults(c client.Reader, old, new *v1.Pod) ([]*ctrlmeshv1alpha1.FaultInjection, error) {
	var res []*ctrlmeshv1alpha1.FaultInjection
	faults := &ctrlmeshv1alpha1.FaultInjectionList{}
	if err := c.List(context.TODO(), faults); err != nil {
		return nil, err
	}
	for i, b := range faults.Items {
		selector, err := metav1.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return nil, err
		}
		oldMatch := selector.Matches(labels.Set(old.Labels))
		newMatch := selector.Matches(labels.Set(new.Labels))
		if oldMatch != newMatch {
			res = append(res, &faults.Items[i])
		}
	}
	return res, nil
}
