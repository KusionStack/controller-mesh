package circuitbreaker

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
	breakers, err := effectiveBreakers(h.reader, pod)
	if err != nil {
		klog.Errorf("fail to get effective CircuitBreakers by pod %s/%s, %v", pod.Namespace, pod.Name, err)
		return
	}
	add(q, breakers)
}

func (h *podEventHandler) Update(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	oldPod := e.ObjectOld.(*v1.Pod)
	newPod := e.ObjectNew.(*v1.Pod)
	if !sidecarInjected(oldPod) {
		return
	}
	if !isProxyAvailable(newPod) || isProxyAvailable(oldPod) {
		return
	}
	breakers, err := matchChangedBreakers(h.reader, oldPod, newPod)
	if err != nil {
		klog.Errorf("fail to get effective CircuitBreakers by pod %s/%s, %v", newPod.Namespace, newPod.Name, err)
		return
	}
	add(q, breakers)
}

func (h *podEventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
}

func (h *podEventHandler) Generic(e event.GenericEvent, q workqueue.RateLimitingInterface) {
}

func sidecarInjected(po *v1.Pod) bool {
	for _, c := range po.Spec.Containers {
		if c.Name == constants.ProxyContainerName {
			return true
		}
	}
	return false
}

func add(q workqueue.RateLimitingInterface, items ...interface{}) {
	for _, item := range items {
		obj := item.(metav1.Object)
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}})
	}
}

func effectiveBreakers(c client.Reader, po *v1.Pod) ([]*ctrlmeshv1alpha1.CircuitBreaker, error) {
	var res []*ctrlmeshv1alpha1.CircuitBreaker
	breakers := &ctrlmeshv1alpha1.CircuitBreakerList{}
	if err := c.List(context.TODO(), breakers); err != nil {
		return nil, err
	}
	for i, b := range breakers.Items {
		selector, err := metav1.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return nil, err
		}
		if selector.Matches(labels.Set(po.Labels)) {
			res = append(res, &breakers.Items[i])
		}
	}
	return res, nil
}

func matchChangedBreakers(c client.Reader, old, new *v1.Pod) ([]*ctrlmeshv1alpha1.CircuitBreaker, error) {
	var res []*ctrlmeshv1alpha1.CircuitBreaker
	breakers := &ctrlmeshv1alpha1.CircuitBreakerList{}
	if err := c.List(context.TODO(), breakers); err != nil {
		return nil, err
	}
	for i, b := range breakers.Items {
		selector, err := metav1.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return nil, err
		}
		oldMatch := selector.Matches(labels.Set(old.Labels))
		newMatch := selector.Matches(labels.Set(new.Labels))
		if oldMatch == newMatch {
			res = append(res, &breakers.Items[i])
		}
	}
	return res, nil
}
