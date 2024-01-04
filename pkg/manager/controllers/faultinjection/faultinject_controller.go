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
	"errors"
	"flag"
	"fmt"
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
	defaultRequeueTime   = 60 * time.Second
	concurrentReconciles = flag.Int("ctrlmesh-inject-workers", 3, "Max concurrent workers for CtrlMesh Server controller.")
)

// FaultInjectionReconciler reconciles a faultinjection object
type FaultInjectionReconciler struct {
	client.Client
	recorder record.EventRecorder
}

func (r *FaultInjectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reconcileErr error) {
	fi := &ctrlmeshv1alpha1.FaultInjection{}
	if err := r.Get(ctx, req.NamespacedName, fi); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if shouldClear, err := r.tryClearOrAddFinalizer(ctx, fi); err != nil || shouldClear {
		return ctrl.Result{}, err
	}

	defer func() {
		if res.RequeueAfter == 0 && reconcileErr == nil {
			res.RequeueAfter = defaultRequeueTime
		}
	}()

	selector, _ := metav1.LabelSelectorAsSelector(fi.Spec.Selector)
	podList := &v1.PodList{}
	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace:     fi.Namespace,
		LabelSelector: selector,
	}); err != nil {
		klog.Infof("fail to list pods: %v", err)
		return ctrl.Result{}, err
	}
	protoFi := conv.ConvertFaultInjection(fi)
	var targetStatus []*ctrlmeshv1alpha1.FaultInjectionTargetStatus
	newTargetMap := make(map[string]*ctrlmeshv1alpha1.FaultInjectionTargetStatus)

	for _, po := range podList.Items {
		if !hasProxyContainer(&po) {
			continue
		}
		var msg, currentHash string
		var stateErr error
		state := r.currentPodStatus(fi, po.Name)
		if state != nil {
			currentHash = state.ConfigHash
		}
		if !isProxyAvailable(&po) {
			defaultPodConfigCache.Delete(po.Namespace, po.Name)
			msg = fmt.Sprintf("pod %s is not available to sync circuit breaker proto", utils.KeyFunc(&po))
			klog.Infof(msg)
		} else if stateErr = r.syncPodConfig(ctx, protoFi, po.Status.PodIP); stateErr != nil {
			msg = stateErr.Error()
			klog.Errorf(msg)
			reconcileErr = errors.Join(reconcileErr, stateErr)
		} else {
			klog.Infof("sync faultinjection %s to pod %s success, configHash=%s", fi.Name, utils.KeyFunc(&po), protoFi.ConfigHash)
			currentHash = protoFi.ConfigHash
			defaultPodConfigCache.Add(po.Namespace, po.Name, fi.Name)
		}
		status := &ctrlmeshv1alpha1.FaultInjectionTargetStatus{
			PodName:    po.Name,
			PodIP:      po.Status.PodIP,
			ConfigHash: currentHash,
			Message:    msg,
		}
		targetStatus = append(targetStatus, status)
		newTargetMap[po.Name] = status
	}

	var failedStatus []*ctrlmeshv1alpha1.FaultInjectionTargetStatus
	// delete unselected pods config
	for i, st := range fi.Status.TargetStatus {
		if _, ok := newTargetMap[st.PodName]; ok {
			continue
		}
		po := &v1.Pod{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: fi.Namespace, Name: st.PodName}, po); err != nil {
			if !k8sErr.IsNotFound(err) {
				reconcileErr = errors.Join(reconcileErr, err)
				klog.Errorf("failed to get pod %s, %v", st.PodName, err)
				failedStatus = append(failedStatus, fi.Status.TargetStatus[i])
			}
			continue
		}
		if !isProxyAvailable(po) {
			defaultPodConfigCache.Delete(po.Namespace, po.Name)
			continue
		}
		if err := disableConfig(ctx, st.PodIP, fi.Name); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
			klog.Errorf("failed to delete config in pod %s, %v", st.PodName, err)
			failedStatus = append(failedStatus, fi.Status.TargetStatus[i])
		} else {
			defaultPodConfigCache.Delete(fi.Namespace, po.Name, fi.Name)
		}
	}
	status := &ctrlmeshv1alpha1.FaultInjectionStatus{
		ObservedGeneration: fi.Generation,
		CurrentSpecHash:    protoFi.ConfigHash,
		TargetStatus:       append(targetStatus, failedStatus...),
	}
	if equality.Semantic.DeepEqual(status, &ctrlmeshv1alpha1.FaultInjectionStatus{
		ObservedGeneration: fi.Generation,
		CurrentSpecHash:    fi.Status.CurrentSpecHash,
		TargetStatus:       fi.Status.TargetStatus,
	}) {
		return ctrl.Result{}, reconcileErr
	}
	fi.Status = *status
	if err := r.Status().Update(ctx, fi); err != nil {
		klog.Errorf("fail to update fault injection %s status", utils.KeyFunc(fi))
		reconcileErr = errors.Join(reconcileErr, err)
	}
	return ctrl.Result{}, reconcileErr
}

func (r *FaultInjectionReconciler) syncPodConfig(ctx context.Context, fi *proto.FaultInjection, podIp string) error {
	fi.Option = proto.FaultInjection_UPDATE
	resp, err := protoClient(podIp).SendConfig(ctx, connect.NewRequest(fi))
	if err != nil {
		return err
	}
	if resp.Msg == nil {
		return fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, server return nil response", podIp, fi.Name)
	}
	if resp != nil && !resp.Msg.Success {
		return fmt.Errorf("fail to update pod [%s, %s] circuit breaker config, %s", podIp, fi.Name, resp.Msg.Message)
	}
	return nil
}

func (r *FaultInjectionReconciler) currentPodStatus(fi *ctrlmeshv1alpha1.FaultInjection, podName string) *ctrlmeshv1alpha1.FaultInjectionTargetStatus {
	for i, state := range fi.Status.TargetStatus {
		if state.PodName == podName {
			return fi.Status.TargetStatus[i]
		}
	}
	return nil
}

func (r *FaultInjectionReconciler) tryClearOrAddFinalizer(ctx context.Context, fi *ctrlmeshv1alpha1.FaultInjection) (shouldClear bool, err error) {

	var disabled bool
	if fi.Labels != nil {
		_, disabled = fi.Labels[ctrlmesh.CtrlmeshFaultInjectionDisableKey]
	}
	if disabled || fi.DeletionTimestamp != nil {
		shouldClear = true
	} else {
		if controllerutil.AddFinalizer(fi, ctrlmesh.ProtectFinalizer) {
			err = r.Update(ctx, fi)
		}
		return
	}
	hasFinalizer := controllerutil.ContainsFinalizer(fi, ctrlmesh.ProtectFinalizer)
	if !hasFinalizer || !shouldClear {
		return
	}
	err = r.clear(ctx, fi)
	if err != nil {
		return
	}
	if fi.Status.TargetStatus != nil {
		tm := metav1.Now()
		fi.Status = ctrlmeshv1alpha1.FaultInjectionStatus{
			ObservedGeneration: fi.Generation,
			LastUpdatedTime:    &tm,
		}
		if err = r.Status().Update(ctx, fi); err != nil {
			return
		}
	}
	controllerutil.RemoveFinalizer(fi, ctrlmesh.ProtectFinalizer)
	err = r.Update(ctx, fi)
	return
}

func (r *FaultInjectionReconciler) clear(ctx context.Context, fi *ctrlmeshv1alpha1.FaultInjection) error {
	var err error
	for _, state := range fi.Status.TargetStatus {
		if !defaultPodConfigCache.Has(fi.Namespace, state.PodName, fi.Name) {
			continue
		}

		po := &v1.Pod{}
		if getErr := r.Get(ctx, types.NamespacedName{Namespace: fi.Namespace, Name: state.PodName}, po); getErr != nil {
			if !k8sErr.IsNotFound(getErr) {
				err = errors.Join(err, getErr)
			} else {
				defaultPodConfigCache.Delete(fi.Namespace, state.PodName, fi.Name)
			}
			continue
		}
		if !isProxyAvailable(po) {
			// Whether to retain the configuration after proxy container restarting？
			continue
		}

		if localErr := disableConfig(ctx, state.PodIP, fi.Name); localErr != nil {
			err = errors.Join(err, localErr)
		} else {
			defaultPodConfigCache.Delete(fi.Namespace, state.PodName, fi.Name)
		}
	}
	return err
}

func disableConfig(ctx context.Context, podIp string, name string) error {
	req := &proto.FaultInjection{
		Option: proto.FaultInjection_DELETE,
		Name:   name,
	}
	resp, err := protoClient(podIp).SendConfig(ctx, connect.NewRequest(req))
	if err != nil {
		return err
	}
	if resp != nil && resp.Msg != nil && !resp.Msg.Success {
		return fmt.Errorf("fail to disable pod [%s, %s] fault injection config, %s", podIp, name, resp.Msg.Message)
	}
	klog.Infof("pod[ip=%s] faultinjection %s was disabled", podIp, name)
	return nil
}

func protoClient(podIp string) protoconnect.FaultInjectClient {
	return protoconnect.NewFaultInjectClient(proto.DefaultHttpClient, podAddr(podIp))
}

var proxyGRPCServerPort = constants.ProxyGRPCServerPort

func podAddr(podIp string) string {
	return fmt.Sprintf("https://%s:%d", podIp, proxyGRPCServerPort)
}

// isProxyAvailable check whether the proxy container is available
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

func hasProxyContainer(po *v1.Pod) bool {
	for _, c := range po.Spec.Containers {
		if c.Name == constants.ProxyContainerName {
			return true
		}
	}
	return false
}

func (r *FaultInjectionReconciler) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FaultInjectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sharding-config-controller")
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: *concurrentReconciles}).
		For(&ctrlmeshv1alpha1.FaultInjection{}, builder.WithPredicates(&FaultInjectionPredicate{})).
		Watches(&source.Kind{Type: &v1.Pod{}}, &podEventHandler{reader: mgr.GetCache()}).
		Complete(r)
}
