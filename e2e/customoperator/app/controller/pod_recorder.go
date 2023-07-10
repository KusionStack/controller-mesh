/*
Copyright 2023 The KusionStack Authors.
Modified from Kruise code, Copyright 2021 The Kruise Authors.

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

package controller

import (
	"context"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	podName, podNamespace string
)

// ManagerStateReconciler reconciles a ManagerState object
type PodReconciler struct {
	client.Client
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	podNamespace = os.Getenv("POD_NAMESPACE")
	podName = os.Getenv("POD_NAME")
	// default handler.EnqueueRequestForObject
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Pod{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, &enqueueHandler{Client: r.Client, kind: "Pod"}).
		Watches(&source.Kind{Type: &v1.ConfigMap{}}, &enqueueHandler{Client: r.Client, kind: "ConfigMap"}).
		Complete(r)
}

type enqueueHandler struct {
	client.Client
	kind string
}

func (e *enqueueHandler) Create(event event.CreateEvent, q workqueue.RateLimitingInterface) {
	add(e.Client, event.Object.GetNamespace(), e.kind)
}

func (e *enqueueHandler) Update(event event.UpdateEvent, q workqueue.RateLimitingInterface) {
	add(e.Client, event.ObjectNew.GetNamespace(), e.kind)
}

func (e *enqueueHandler) Delete(event event.DeleteEvent, q workqueue.RateLimitingInterface) {
	add(e.Client, event.Object.GetNamespace(), e.kind)
}

func (e *enqueueHandler) Generic(event event.GenericEvent, q workqueue.RateLimitingInterface) {
	add(e.Client, event.Object.GetNamespace(), e.kind)
}
