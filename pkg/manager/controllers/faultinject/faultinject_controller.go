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

package faultinject

import (
	"context"

	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// FaultInjectReconciler reconciles a FaultInjection object
type FaultInjectReconciler struct {
	client.Client
	recorder record.EventRecorder
}

func (r *FaultInjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reconcileErr error) {

	// TODO: sync FaultInjection config to proxy container

	return reconcile.Result{}, nil
}
