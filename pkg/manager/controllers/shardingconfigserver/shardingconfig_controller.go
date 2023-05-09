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

package shardingconfigserver

import (
	"context"
	"flag"
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kridgeproto "github.com/KusionStack/kridge/pkg/apis/kridge/proto"
	kridgev1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/utils"
)

var (
	concurrentReconciles = flag.Int("kridge-server-workers", 3, "Max concurrent workers for CtrlMesh Server controller.")

	// podHashExpectation type is map[types.UID]string
	podHashExpectation = sync.Map{}
	// podDeletionExpectation type is map[types.UID]struct{}
	podDeletionExpectation = sync.Map{}
)

type ShardingConfigReconciler struct {
	client.Client
	recorder record.EventRecorder
}

type syncPod struct {
	pod           *v1.Pod
	conn          *grpcSrvConnection
	dispatched    bool
	currentStatus *kridgeproto.ProxyStatus
	newSpec       *kridgeproto.ProxySpec
}

//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=shardingconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=shardingconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources=shardingconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

func (r *ShardingConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	startTime := time.Now()
	defer func() {
		if retErr == nil {
			if res.Requeue || res.RequeueAfter > 0 {
				klog.Infof("Finished syncing ShardingConfig %s, cost %v, result: %v", req, time.Since(startTime), res)
			} else {
				klog.Infof("Finished syncing ShardingConfig %s, cost %v", req, time.Since(startTime))
			}
		} else {
			klog.Errorf("Failed syncing ShardingConfig %s: %v", req, retErr)
		}
	}()

	shardingConfig := &kridgev1alpha1.ShardingConfig{}
	err := r.Get(ctx, req.NamespacedName, shardingConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// TODO: need to reset all Pods?
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	allPods, err := r.getPodsForShardingConfig(ctx, shardingConfig)
	if err != nil {
		return reconcile.Result{}, err
	}

	// filter and wait for all pods satisfied
	var activePods []*v1.Pod
	var syncPods []syncPod
	for _, pod := range allPods {
		if !utils.IsPodActive(pod) {
			// Guarantee that all containers in inactive pods should be stopped
			if runningContainers := getRunningContainers(pod); len(runningContainers) > 0 {
				klog.Warningf("Skip reconcile shardingConfig %s for Pod %s is inactive but containers %s are running", req, pod.Name, runningContainers)
				return reconcile.Result{}, nil
			}
			continue
		}
		if _, ok := podDeletionExpectation.Load(pod.UID); ok {
			klog.Warningf("Skip reconcile shardingConfig %s for Pod %s is expected to be already deleted", req, pod.Name)
			return reconcile.Result{}, nil
		}
		activePods = append(activePods, pod)

		var conn *grpcSrvConnection
		if v, ok := cachedGrpcSrvConnection.Load(pod.UID); !ok || v == nil {
			// No connection from the Pod, could be new Pod or the proxy container exited
			if pod.Status.Phase == v1.PodRunning && utils.IsPodReady(pod) {
				klog.Warningf("Skip reconcile shardingConfig %s find no connection from ready Pod %s yet", req, pod.Name)
				return reconcile.Result{RequeueAfter: time.Minute}, nil
			}

			if pod.Status.Phase == v1.PodPending && time.Since(pod.CreationTimestamp.Time) < time.Minute {
				klog.Warningf("Skip reconcile shardingConfig %s waiting for pending Pod %s at most 1min", req, pod.Name)
				return reconcile.Result{RequeueAfter: time.Minute - time.Since(pod.CreationTimestamp.Time)}, nil
			}

			klog.Warningf("shardingConfig %s ignores %s Pod %s that has no connection yet", req, pod.Status.Phase, pod.Name)
			continue
		} else {
			conn = v.(*grpcSrvConnection)
		}

		conn.mu.Lock()
		status := conn.status
		dispatched := conn.sendTimes > 0
		disconnected := conn.disconnected
		conn.mu.Unlock()

		if disconnected {
			klog.Warningf("Skip reconcile shardingConfig %s for Pod %s has disconnected", req, pod.Name)
			return reconcile.Result{}, nil
		}

		if v, ok := podHashExpectation.Load(pod.UID); ok {
			expectHash := v.(string)
			if status.MetaState == nil || expectHash != status.MetaState.ExpectedHash {
				klog.Warningf("Skip reconcile shardingConfig %s for Pod %s has unsatisfied hash state %v (expected %s)", req, pod.Name, utils.DumpJSON(status), expectHash)
				return reconcile.Result{}, nil
			}
			podHashExpectation.Delete(pod.UID)
		}

		syncPods = append(syncPods, syncPod{
			pod:           pod,
			conn:          conn,
			dispatched:    dispatched,
			currentStatus: status,
		})
	}

	if err != nil {
		return reconcile.Result{}, err
	}

	newSpec := generateProtoConfig(shardingConfig)

	// Complete sync states of Pods and calculate if pods need to dispatch, update or delete
	var podsProtoToDispatch, podsProtoToUpdate, leaderPodsToDelete, nonLeaderPodsToDelete []syncPod
	for i := range syncPods {
		syncingPod := &syncPods[i]
		metaState := syncingPod.currentStatus.MetaState

		// TODO: new spec
		syncingPod.newSpec = newSpec

		// Should be the first time start and connect
		if metaState == nil {
			podsProtoToDispatch = append(podsProtoToDispatch, *syncingPod)
			continue
		}
		var isLeader bool
		if syncingPod.currentStatus.LeaderElectionState != nil {
			isLeader = syncingPod.currentStatus.LeaderElectionState.IsLeader
		}

		// TODO:

		if metaState.ExpectedHash != metaState.CurrentHash {
			klog.Infof("shardingConfig %s find Pod %s should delete, expectedHash(%s) != currentHash(%s), because %s",
				req, syncingPod.pod.Name, metaState.ExpectedHash, metaState.CurrentHash, metaState.HashUnloadReason)
			if isLeader {
				leaderPodsToDelete = append(leaderPodsToDelete, *syncingPod)
			} else {
				nonLeaderPodsToDelete = append(nonLeaderPodsToDelete, *syncingPod)
			}
			continue
		}

		if syncingPod.newSpec.Meta.Hash != metaState.ExpectedHash {
			klog.Infof("shardingConfig %s find Pod %s should update, hash update from %s to %s",
				req, syncingPod.pod.Name, metaState.ExpectedHash, syncingPod.newSpec.Meta.Hash)
			podsProtoToUpdate = append(podsProtoToUpdate, *syncingPod)
			continue
		}

		// maybe re-connected Pods
		if !syncingPod.dispatched {
			podsProtoToDispatch = append(podsProtoToDispatch, *syncingPod)
		}
	}

	if len(podsProtoToDispatch) == 0 && len(podsProtoToUpdate) == 0 && len(leaderPodsToDelete) == 0 && len(nonLeaderPodsToDelete) == 0 {
		klog.Infof("shardingConfig %s find no Pods need to delete or send proto spec", req)
		return ctrl.Result{}, nil
	}

	// firstly delete those non-leader Pods, then leader Pods
	isLeaderMsg := "non-leader"
	podsToDelete := nonLeaderPodsToDelete
	if len(podsToDelete) == 0 {
		podsToDelete = leaderPodsToDelete
		isLeaderMsg = "leader"
	}
	for _, syncingPod := range podsToDelete {
		klog.Infof("shardingConfig %s preparing to delete %v Pod %s", req, isLeaderMsg, syncingPod.pod.Name)
		if err = r.Delete(ctx, syncingPod.pod); err != nil {
			r.recorder.Eventf(shardingConfig, v1.EventTypeWarning, "FailedToDeletePod", "failed to delete %v Pod %s: %v", isLeaderMsg, syncingPod.pod.Name, err)
		} else {
			r.recorder.Eventf(shardingConfig, v1.EventTypeNormal, "SuccessfullyDeletePod", "successfully delete %v Pod %s", isLeaderMsg, syncingPod.pod.Name)
			podDeletionExpectation.Store(syncingPod.pod.UID, struct{}{})
		}
	}
	if len(podsToDelete) > 0 {
		return ctrl.Result{}, err
	}

	// if there is no pods to delete, send new proto spec to all Pods need to update
	for _, syncingPod := range podsProtoToUpdate {
		klog.Infof("ShardingConfig %s preparing to update proto spec %v to Pod %s", req, utils.DumpJSON(syncingPod.newSpec), syncingPod.pod.Name)
		if err = syncingPod.conn.send(syncingPod.newSpec); err != nil {
			r.recorder.Eventf(shardingConfig, v1.EventTypeWarning, "FailedToUpdateProto", "failed to update Pod %s proto spec: %v", syncingPod.pod.Name, err)
		} else {
			r.recorder.Eventf(shardingConfig, v1.EventTypeNormal, "SuccessfullyUpdateProto", "successfully update Pod %s proto spec", syncingPod.pod.Name)
			podHashExpectation.Store(syncingPod.pod.UID, syncingPod.newSpec.Meta.Hash)
		}
	}
	if len(podsProtoToUpdate) > 0 {
		return ctrl.Result{}, err
	}

	// if there is no pods to delete and no proto to update, send new proto spec to all Pods first-time connected
	for _, syncingPod := range podsProtoToDispatch {
		klog.Infof("ShardingConfig %s preparing to dispatch proto spec %v to Pod %s", req, utils.DumpJSON(syncingPod.newSpec), syncingPod.pod.Name)
		if err = syncingPod.conn.send(syncingPod.newSpec); err != nil {
			r.recorder.Eventf(shardingConfig, v1.EventTypeWarning, "FailedToDispatchProto", "failed to dispatch Pod %s proto spec: %v", syncingPod.pod.Name, err)
		} else {
			r.recorder.Eventf(shardingConfig, v1.EventTypeNormal, "SuccessfullyDispatchProto", "successfully dispatch Pod %s proto spec", syncingPod.pod.Name)
			podHashExpectation.Store(syncingPod.pod.UID, syncingPod.newSpec.Meta.Hash)
		}
	}
	return ctrl.Result{}, err
}

func (r *ShardingConfigReconciler) getPodsForShardingConfig(ctx context.Context, shardingConfig *kridgev1alpha1.ShardingConfig) ([]*v1.Pod, error) {
	podList := v1.PodList{}
	if err := r.List(ctx, &podList, client.InNamespace(shardingConfig.Namespace), client.MatchingLabels{kridgev1alpha1.ShardingConfigInjectedKey: shardingConfig.Name}); err != nil {
		return nil, err
	}
	var pods []*v1.Pod
	for i := range podList.Items {
		pods = append(pods, &podList.Items[i])
	}
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardingConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sharding-config-controller")
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: *concurrentReconciles}).
		For(&kridgev1alpha1.ShardingConfig{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, &podEventHandler{reader: mgr.GetCache()}).
		Watches(&source.Channel{Source: grpcRecvTriggerChannel}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
