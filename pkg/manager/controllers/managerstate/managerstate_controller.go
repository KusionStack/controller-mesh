/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

package managerstate

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kridgev1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/grpcregistry"
	"github.com/KusionStack/kridge/pkg/utils"
)

var (
	podSelector labels.Selector
	namespace   string
	localName   string
)

// ManagerStateReconciler reconciles a ManagerState object
type ManagerStateReconciler struct {
	client.Client
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=managerstates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=managerstates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=managerstates/finalizers,verbs=update

func (r *ManagerStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	if req.Name != kridgev1alpha1.NameOfManager {
		klog.Infof("Ignore ManagerState %s", req.Name)
		return reconcile.Result{}, nil
	}

	start := time.Now()
	klog.V(3).Infof("Starting to process ManagerState %v", req.Name)
	defer func() {
		if err != nil {
			klog.Warningf("Failed to process ManagerState %v, elapsedTime %v, error: %v", req.Name, time.Since(start), err)
		} else {
			klog.Infof("Finish to process ManagerState %v, elapsedTime %v", req.Name, time.Since(start))
		}
	}()

	podList := &v1.PodList{}
	err = r.List(context.TODO(), podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: podSelector})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("list pods in %s error: %v", namespace, err)
	}

	var hasLeader bool
	endpoints := make(kridgev1alpha1.ManagerStateEndpoints, 0, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !utils.IsPodActive(pod) {
			continue
		}

		e := kridgev1alpha1.ManagerStateEndpoint{Name: pod.Name, PodIP: pod.Status.PodIP}
		if pod.Name == localName {
			e.Leader = true
			hasLeader = true
		}
		endpoints = append(endpoints, e)
	}
	sort.Sort(endpoints)
	if !hasLeader {
		return reconcile.Result{}, fmt.Errorf("no leader %s in new endpoints %v", localName, utils.DumpJSON(endpoints))
	}
	tm := metav1.NewTime(start)
	ports := kridgev1alpha1.ManagerStatePorts{}
	ports.GrpcLeaderElectionPort, ports.GrpcNonLeaderElectionPort = grpcregistry.GetGrpcPorts()
	newStatus := kridgev1alpha1.ManagerStateStatus{
		Namespace:       namespace,
		Endpoints:       endpoints,
		Ports:           &ports,
		UpdateTimestamp: &tm,
	}

	managerState := &kridgev1alpha1.ManagerState{}
	err = r.Get(context.TODO(), req.NamespacedName, managerState)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("get ManagerState %s error: %v", req.Name, err)
		}

		managerState.Name = kridgev1alpha1.NameOfManager
		managerState.Status = newStatus
		err = r.Create(context.TODO(), managerState)
		if err != nil && !errors.IsAlreadyExists(err) {
			return reconcile.Result{}, fmt.Errorf("create ManagerState %s error: %v", req.Name, err)
		}
		return
	}

	if reflect.DeepEqual(managerState.Status, newStatus) {
		return reconcile.Result{}, nil
	}

	managerState.Status = newStatus
	err = r.Status().Update(context.TODO(), managerState)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("update ManagerState %s error: %v", req.Name, err)
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagerStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	namespace = utils.GetNamespace()
	if localName = os.Getenv("POD_NAME"); len(localName) == 0 {
		return fmt.Errorf("find no POD_NAME in env")
	}

	// Read the service of kridge-manager's webhook, to get the pod selector
	svc := &v1.Service{}
	svcNamespacedName := types.NamespacedName{Namespace: namespace, Name: utils.GetServiceName()}
	err := mgr.GetAPIReader().Get(context.TODO(), svcNamespacedName, svc)
	if err != nil {
		return fmt.Errorf("get service %s error: %v", svcNamespacedName, err)
	}

	podSelector, err = labels.ValidatedSelectorFromSet(svc.Spec.Selector)
	if err != nil {
		return fmt.Errorf("parse service %s selector %v error: %v", svcNamespacedName, svc.Spec.Selector, err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kridgev1alpha1.ManagerState{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, &enqueueHandler{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				pod := e.Object.(*v1.Pod)
				return podSelector.Matches(labels.Set(pod.Labels))
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				pod := e.ObjectNew.(*v1.Pod)
				return podSelector.Matches(labels.Set(pod.Labels))
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				pod := e.Object.(*v1.Pod)
				return podSelector.Matches(labels.Set(pod.Labels))
			},
			GenericFunc: func(e event.GenericEvent) bool {
				pod := e.Object.(*v1.Pod)
				return podSelector.Matches(labels.Set(pod.Labels))
			},
		})).
		Complete(r)
}

type enqueueHandler struct{}

func (e *enqueueHandler) Create(_ event.CreateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name: kridgev1alpha1.NameOfManager,
	}})
}

func (e *enqueueHandler) Update(_ event.UpdateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name: kridgev1alpha1.NameOfManager,
	}})
}

func (e *enqueueHandler) Delete(_ event.DeleteEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name: kridgev1alpha1.NameOfManager,
	}})
}

func (e *enqueueHandler) Generic(_ event.GenericEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name: kridgev1alpha1.NameOfManager,
	}})
}
