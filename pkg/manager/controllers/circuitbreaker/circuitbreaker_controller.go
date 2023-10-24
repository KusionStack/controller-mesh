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
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8sErr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

var (
	PodName      = os.Getenv(constants.EnvPodName)
	PodNamespace = os.Getenv(constants.EnvPodNamespace)

	defaultRequeueTime   = 60 * time.Second
	concurrentReconciles = flag.Int("ctrlmesh-server-workers", 3, "Max concurrent workers for CtrlMesh Server controller.")
)

// CircuitBreakerReconciler reconciles a CircuitBreaker object
type CircuitBreakerReconciler struct {
	client.Client
	recorder record.EventRecorder
}

func (r *CircuitBreakerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reconcileErr error) {
	defer func() {
		if res.RequeueAfter == 0 {
			res.RequeueAfter = defaultRequeueTime
		}
	}()
	cb := &ctrlmeshv1alpha1.CircuitBreaker{}
	if err := r.Get(ctx, req.NamespacedName, cb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if shouldClear, err := r.tryClearOrAddFinalizer(ctx, cb); err != nil || shouldClear {
		return ctrl.Result{}, err
	}

	selector, _ := metav1.LabelSelectorAsSelector(cb.Spec.Selector)
	podList := &v1.PodList{}
	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace:     cb.Namespace,
		LabelSelector: selector,
	}); err != nil {
		klog.Infof("fail to list pods: %v", err)
		return ctrl.Result{}, err
	}
	protoCb := conv.ConvertCircuitBreaker(cb)
	var targetStatus []*ctrlmeshv1alpha1.TargetStatus
	newTargetMap := make(map[string]*ctrlmeshv1alpha1.TargetStatus)

	for _, po := range podList.Items {
		var msg, currentHash string
		var snapshots []*proto.LimitingSnapshot
		var stateErr error
		state := r.currentPodStatus(cb, po.Name)
		if state != nil {
			currentHash = state.ConfigHash
		}
		if !isProxyAvailable(&po) {
			msg = fmt.Sprintf("pod %s is not available to sync circuit breaker proto", utils.KeyFunc(&po))
			klog.Infof(msg)
		} else if snapshots, stateErr = r.syncPodConfig(ctx, protoCb, state.PodIP); stateErr != nil {
			msg = stateErr.Error()
			klog.Errorf(msg)
			reconcileErr = errors.Join(reconcileErr, stateErr)
		} else {
			currentHash = protoCb.ConfigHash
		}
		status := &ctrlmeshv1alpha1.TargetStatus{
			PodName:           po.Name,
			PodIP:             po.Status.PodIP,
			ConfigHash:        currentHash,
			Message:           msg,
			LimitingSnapshots: conv.ConvertSnapshots(snapshots),
		}
		targetStatus = append(targetStatus, status)
		newTargetMap[PodName] = status
	}

	var failedStatus []*ctrlmeshv1alpha1.TargetStatus
	// delete unselected pods config
	for i, st := range cb.Status.TargetStatus {
		if _, ok := newTargetMap[st.PodName]; ok {
			continue
		}
		po := &v1.Pod{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: cb.Namespace, Name: st.PodName}, po); err != nil {
			if !k8sErr.IsNotFound(err) {
				reconcileErr = errors.Join(reconcileErr, err)
				klog.Errorf("failed to get pod %s, %v", st.PodName, err)
				failedStatus = append(failedStatus, cb.Status.TargetStatus[i])
			}
			continue
		}
		if err := r.deletePodConfig(ctx, &proto.CircuitBreaker{Name: cb.Name}, st.PodIP); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
			klog.Errorf("failed to delete config in pod %s, %v", st.PodName, err)
			failedStatus = append(failedStatus, cb.Status.TargetStatus[i])
		}
	}
	status := &ctrlmeshv1alpha1.CircuitBreakerStatus{
		ObservedGeneration: cb.Generation,
		CurrentSpecHash:    protoCb.ConfigHash,
		TargetStatus:       append(targetStatus, failedStatus...),
	}
	if equality.Semantic.DeepEqual(status, &ctrlmeshv1alpha1.CircuitBreakerStatus{
		ObservedGeneration: cb.Generation,
		CurrentSpecHash:    cb.Status.CurrentSpecHash,
		TargetStatus:       cb.Status.TargetStatus,
	}) {
		return ctrl.Result{}, reconcileErr
	}
	cb.Status = *status
	if err := r.Status().Update(ctx, cb); err != nil {
		klog.Errorf("fail to update circuit breaker %s status", utils.KeyFunc(cb))
		reconcileErr = errors.Join(reconcileErr, err)
	}
	return ctrl.Result{}, reconcileErr
}

func (r *CircuitBreakerReconciler) syncPodConfig(ctx context.Context, cb *proto.CircuitBreaker, podIp string) ([]*proto.LimitingSnapshot, error) {
	cb.Option = proto.CircuitBreaker_UPDATE
	resp, err := protoClient(podIp).SendConfig(ctx, connect.NewRequest(cb))
	if err != nil {
		return nil, err
	}
	if resp.Msg == nil {
		return nil, fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, server return nil response", podIp, cb.Name)
	}
	if resp != nil && !resp.Msg.Success {
		return nil, fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, %s", podIp, cb.Name, resp.Msg.Message)
	}
	return resp.Msg.LimitingSnapshot, nil
}

func (r *CircuitBreakerReconciler) deletePodConfig(ctx context.Context, cb *proto.CircuitBreaker, podIp string) error {
	cb.Option = proto.CircuitBreaker_DELETE
	resp, err := protoClient(podIp).SendConfig(ctx, connect.NewRequest(cb))
	if err != nil {
		return err
	}
	if resp.Msg == nil {
		return fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, server return nil response", podIp, cb.Name)
	}
	if resp != nil && !resp.Msg.Success {
		return fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, %s", podIp, cb.Name, resp.Msg.Message)
	}
	return nil
}

func (r *CircuitBreakerReconciler) currentPodStatus(cb *ctrlmeshv1alpha1.CircuitBreaker, podName string) *ctrlmeshv1alpha1.TargetStatus {
	for i, state := range cb.Status.TargetStatus {
		if state.PodName == podName {
			return cb.Status.TargetStatus[i]
		}
	}
	return nil
}

func (r *CircuitBreakerReconciler) tryClearOrAddFinalizer(ctx context.Context, cb *ctrlmeshv1alpha1.CircuitBreaker) (shouldClear bool, err error) {

	var disabled bool
	if cb.Labels != nil {
		_, disabled = cb.Labels[ctrlmesh.CtrlmeshCircuitBreakerDisableKey]
	}
	if disabled || cb.DeletionTimestamp != nil {
		shouldClear = true
	} else {
		if controllerutil.AddFinalizer(cb, ctrlmesh.ProtectFinalizer) {
			err = r.Update(ctx, cb)
		}
		return
	}
	hasFinalizer := controllerutil.ContainsFinalizer(cb, ctrlmesh.ProtectFinalizer)
	if !hasFinalizer {
		return
	}
	err = r.clear(ctx, cb)
	if err != nil {
		return
	}
	if cb.Status.TargetStatus != nil {
		cb.Status = ctrlmeshv1alpha1.CircuitBreakerStatus{
			ObservedGeneration: cb.Generation,
			LastUpdatedTime:    metav1.Now(),
		}
		if err = r.Status().Update(ctx, cb); err != nil {
			return
		}
	}
	controllerutil.RemoveFinalizer(cb, ctrlmesh.ProtectFinalizer)
	err = r.Update(ctx, cb)
	return
}

func (r *CircuitBreakerReconciler) clear(ctx context.Context, cb *ctrlmeshv1alpha1.CircuitBreaker) error {
	var err error
	for _, state := range cb.Status.TargetStatus {
		key := cacheKey(cb.Namespace, state.PodName, cb.Name)
		if defaultPodCache.Get(key) == "" {
			continue
		}

		po := &v1.Pod{}
		if getErr := r.Get(ctx, types.NamespacedName{Namespace: cb.Namespace, Name: state.PodName}, po); getErr != nil {
			if !k8sErr.IsNotFound(getErr) {
				err = errors.Join(err, getErr)
			}
			continue
		}
		if !isProxyAvailable(po) {
			// Whether to retain the configuration after proxy container restarting？
			continue
		}

		if localErr := r.disableConfig(ctx, state.PodIP, cb.Name); localErr != nil {
			err = errors.Join(err, localErr)
		} else {
			defaultPodCache.Delete(key)
		}
	}
	return err
}

func (r *CircuitBreakerReconciler) disableConfig(ctx context.Context, podIp string, name string) error {
	req := &proto.CircuitBreaker{
		Option: proto.CircuitBreaker_DELETE,
		Name:   name,
	}
	resp, err := protoClient(podIp).SendConfig(ctx, connect.NewRequest(req))
	if err != nil {
		return err
	}
	if resp != nil && resp.Msg != nil && !resp.Msg.Success {
		return fmt.Errorf("fail to disable pod [%s, %s] circuit breaker config, %s", podIp, name, resp.Msg.Message)
	}
	return nil
}

func protoClient(podIp string) protoconnect.ThrottlingClient {
	return protoconnect.NewThrottlingClient(proto.DefaultHttpClient, podAddr(podIp))
}

func podAddr(podIp string) string {
	return fmt.Sprintf("https://%s:%d", podIp, constants.ProxyGRPCServerPort)
}

func isProxyAvailable(po *v1.Pod) bool {
	if po.DeletionTimestamp != nil {
		return false
	}
	if po.Status.PodIP == "" {
		return false
	}
	for _, c := range po.Status.ContainerStatuses {
		if c.Name == constants.ProxyContainerName {
			return c.Ready
		}
	}
	return false
}

func (r *CircuitBreakerReconciler) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CircuitBreakerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sharding-config-controller")
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: *concurrentReconciles}).
		For(&ctrlmeshv1alpha1.CircuitBreaker{}, builder.WithPredicates(&BreakerPredicate{})).
		Watches(&source.Kind{Type: &v1.Pod{}}, &podEventHandler{reader: mgr.GetCache()}).
		Complete(r)
}
