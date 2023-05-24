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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/KusionStack/kridge/pkg/apis/kridge"
	kridgeutils "github.com/KusionStack/kridge/pkg/apis/kridge/utils"
	kridgev1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/utils"
	"github.com/KusionStack/kridge/pkg/utils/expectation"
)

var (
	rollingDeleteExpectation = expectation.NewExpectations("DeletePod", 10*time.Minute)
)

type RolloutReconciler struct {
	client.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return constantsLabelKey(e.Object, kridge.KdInRollingLabel)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return constantsLabelKey(e.ObjectNew, kridge.KdInRollingLabel)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return constantsLabelKey(e.Object, kridge.KdInRollingLabel)
			},
		})).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &podEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

func (r *RolloutReconciler) Reconcile(ctx context.Context, req reconcile.Request) (res reconcile.Result, reconcileErr error) {

	klog.Infof("start to reconcile %s ...", req.NamespacedName.String())
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, req.NamespacedName, sts); err != nil {
		klog.Errorf("fail to get sts %s, %v", req.NamespacedName.String(), err)
		if apierrors.IsNotFound(err) {
			err = nil
		}
		return reconcile.Result{}, err
	}
	if sts.DeletionTimestamp != nil || sts.Spec.Replicas == nil {
		return reconcile.Result{}, nil
	}

	if sts.Status.Replicas != *sts.Spec.Replicas {
		return reconcile.Result{RequeueAfter: time.Second * 3}, fmt.Errorf("wait sts create pods, expected replicas %d, current replicas %d", *sts.Spec.Replicas, sts.Status.Replicas)
	}

	rollType, ok := sts.Labels[kridge.KdInRollingLabel]
	if !ok {
		return reconcile.Result{}, nil
	}

	if !isOnDeleteStrategy(sts) {
		deleteLabel(sts, kridge.KdInRollingLabel)
		return reconcile.Result{}, r.Update(ctx, sts)
	}

	// list ShardingConfigs
	configs := &kridgev1alpha1.ShardingConfigList{}
	if err := r.List(ctx, configs, client.InNamespace(req.Namespace)); err != nil {
		klog.Errorf("fail to list shardingConfig in namespace %s, %v", req.Namespace, err)
		return reconcile.Result{}, err
	}

	// list sts Pods
	pods := &corev1.PodList{}
	sel, _ := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err := r.List(ctx, pods, &client.ListOptions{LabelSelector: sel, Namespace: req.Namespace}); err != nil || pods.Items == nil {
		klog.Errorf("fail to list pods %v", err)
		return reconcile.Result{}, err
	}
	var stsPods []*corev1.Pod
	for i, pod := range pods.Items {
		if pod.OwnerReferences == nil || len(pod.OwnerReferences) != 1 {
			continue
		}
		if pod.OwnerReferences[0].Name == sts.Name {
			stsPods = append(stsPods, &pods.Items[i])
		}
	}

	if len(stsPods) != int(*sts.Spec.Replicas) {
		return reconcile.Result{}, fmt.Errorf("wait sts %s create pods", sts.Name)
	}

	shardingPods := map[string][]*corev1.Pod{}
	allConfigs := kridgeutils.ShardingConfigs{}

	for i, po := range stsPods {
		if po.OwnerReferences == nil || len(po.OwnerReferences) == 0 || po.OwnerReferences[0].Name != sts.Name {
			continue
		}
		configName, ok := po.Labels[kridgev1alpha1.ShardingConfigInjectedKey]
		if !ok {
			klog.Errorf("not found sharding-config-injected label on pod %s/%s ", po.Namespace, po.Name)
			continue
		}
		shardingPods[configName] = append(shardingPods[configName], stsPods[i])
	}

	for i, vapp := range configs.Items {
		if _, ok := shardingPods[vapp.Name]; !ok {
			continue
		}
		allConfigs = append(allConfigs, &configs.Items[i])
	}

	sort.Sort(allConfigs)
	var upgradedPods []string
	expectedPodRevision := map[string]string{}

	finishRollout := false
	defer func() {
		updateAnno := updateRolloutAnno(sts, upgradedPods)
		deleted := finishRollout && deleteLabel(sts, kridge.KdInRollingLabel)
		if updateAnno || deleted {
			updateErr := r.Update(ctx, sts)
			if updateErr != nil {
				klog.Errorf("fail to update sts %s, %v", req.NamespacedName.String(), updateErr)
				reconcileErr = updateErr
			}
		}
		klog.Infof("finish reconcile rollout, updated pods: %v", upgradedPods)
	}()
	for _, cfg := range allConfigs {
		for _, po := range shardingPods[cfg.Name] {
			if isUpdatedRevision(sts, po) {
				upgradedPods = append(upgradedPods, po.Name)
			}
		}
		if rollType != "all" && rollType != "" && !strings.Contains(cfg.Name, rollType) {
			for _, po := range shardingPods[cfg.Name] {
				expectedPodRevision[po.Name] = po.Labels["controller-revision-hash"]
			}
			continue
		}
		revision := recentRevision(sts)
		for _, po := range shardingPods[cfg.Name] {
			expectedPodRevision[po.Name] = revision
		}
	}

	if SetExpectedRevision(sts, expectedPodRevision) {
		updateErr := r.Update(ctx, sts)
		if updateErr != nil {
			return reconcile.Result{}, updateErr
		}
	}

	for i, cfg := range allConfigs {
		if rollType != "all" && rollType != "" && !strings.Contains(cfg.Name, rollType) {
			continue
		}
		finish, requeue, upgradeErr := r.upgrade(sts, allConfigs[i], shardingPods[cfg.Name])
		if finish {
			continue
		}
		if !requeue {
			return reconcile.Result{}, upgradeErr
		}
		return reconcile.Result{RequeueAfter: time.Second * 3}, upgradeErr
	}
	finishRollout = true
	return reconcile.Result{}, reconcileErr
}

func (r *RolloutReconciler) upgrade(sts *appsv1.StatefulSet, cfg *kridgev1alpha1.ShardingConfig, pods []*corev1.Pod) (finish, requeue bool, upgradeErr error) {

	hasTerminatingPod := false
	expectDeletePods := map[string]*corev1.Pod{}
	for i, po := range pods {
		if po.DeletionTimestamp != nil || rollingDeleteExpectation.GetExpectation(po.Namespace+"/"+po.Name) != nil {
			klog.Infof("po %s/%s is on deleting, waiting...", po.Namespace, po.Name)
			hasTerminatingPod = true
		}
		if isUpdatedRevision(sts, po) {
			if !utils.IsPodReady(po) {
				klog.Infof("po %s/%s is not ready, waiting...", po.Namespace, po.Name)
				return false, false, nil
			}
			continue
		}
		expectDeletePods[po.Name] = pods[i]
	}
	if hasTerminatingPod {
		return false, false, nil
	}
	if len(expectDeletePods) == 0 {
		return true, false, nil
	}
	holderIdentity := r.getHolderIdentity(context.TODO(), cfg)
	if holderIdentity == nil {
		klog.Infof("vapp %s/%s lease  has nil holderIdentity, wait for holder", cfg.Namespace, cfg.Name)
		return false, true, nil
	}
	deleteSize := 0
	var leaderPo *corev1.Pod
	for _, po := range expectDeletePods {
		if isLeader(po, *holderIdentity) {
			leaderPo = po
			continue
		}
		if err := r.Delete(context.TODO(), po); err != nil {
			upgradeErr = err
			klog.Errorf("fail to delete po %s/%s", po.Namespace, po.Name)
			continue
		}
		deleteSize++
		rollingDeleteExpectation.Record(po.Namespace+"/"+po.Name, time.Now().Unix())
	}
	if deleteSize > 0 || upgradeErr != nil {
		klog.Infof("delete %d pods on vapp %s/%s", deleteSize, cfg.Namespace, cfg.Name)
		return false, upgradeErr != nil, upgradeErr
	}
	// delete leader pod
	if leaderPo != nil {
		if upgradeErr = r.Delete(context.TODO(), leaderPo); upgradeErr != nil {
			klog.Errorf("fail to delete leader po %s/%s", leaderPo.Namespace, leaderPo.Name)
		}
		return false, true, upgradeErr
	}
	return true, false, nil
}

func (r *RolloutReconciler) getHolderIdentity(ctx context.Context, cfg *kridgev1alpha1.ShardingConfig) *string {
	lease := &coordinationv1.Lease{}
	laederName := ""
	if cfg.Spec.Controller != nil {
		laederName = cfg.Spec.Controller.LeaderElectionName
	} else {
		return nil
	}
	if err := r.Get(ctx, types.NamespacedName{Name: laederName + "---default-" + cfg.Name, Namespace: cfg.Namespace}, lease); err != nil {
		klog.Errorf("fail to get lease %s, %v", laederName+"---default-"+cfg.Name, err)
		return nil
	}
	return lease.Spec.HolderIdentity
}

func isLeader(po *corev1.Pod, identity string) bool {
	if strings.HasPrefix(identity, po.Name) {
		return true
	}
	if host, ok := po.Labels["meta.k8s.alipay.com/hostname"]; ok {
		return strings.HasPrefix(identity, host)
	}
	return false
}

func updateRolloutAnno(sts *appsv1.StatefulSet, upgradedPods []string) (updated bool) {
	msg, _ := json.Marshal(upgradedPods)
	val := string(msg)
	if upgradedPods == nil || len(upgradedPods) == 0 {
		val = ""
	}
	if sts.Annotations == nil {
		sts.Annotations = map[string]string{kridge.KdRollingStatusAnno: val}
		return true
	}
	if sts.Annotations[kridge.KdRollingStatusAnno] != val {
		sts.Annotations[kridge.KdRollingStatusAnno] = val
		return true
	}
	return false
}

func constantsLabelKey(obj client.Object, key string) bool {
	if obj.GetLabels() == nil {
		return false
	}
	_, ok := obj.GetLabels()[key]
	return ok
}

func isOnDeleteStrategy(sts *appsv1.StatefulSet) bool {
	return sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType
}

func deleteLabel(obj client.Object, key string) bool {
	_, ok := obj.GetLabels()[key]
	if ok {
		delete(obj.GetLabels(), key)
	}
	return ok
}
