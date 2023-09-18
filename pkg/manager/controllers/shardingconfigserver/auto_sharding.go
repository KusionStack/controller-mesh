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

package shardingconfigserver

import (
	"context"
	"math"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh"
	ctrlmeshv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/ctrlmesh/pkg/utils"
)

func (r *ShardingConfigReconciler) AutoSharding(ctx context.Context, root *ctrlmeshv1alpha1.ShardingConfig) error {

	oldCfgs := &ctrlmeshv1alpha1.ShardingConfigList{}
	if err := r.List(ctx, oldCfgs, client.MatchingLabels{ctrlmesh.KdAutoShardingRootLabel: root.Name}); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if root.DeletionTimestamp != nil || (root.Spec.Root.Disable != nil && *root.Spec.Root.Disable) {
		for i := range oldCfgs.Items {
			err := r.Delete(ctx, &oldCfgs.Items[i])
			if err != nil {
				return err
			}
		}
		if clearStatus(root) {
			err := r.Status().Update(ctx, root)
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
		if clearFinalizers(root, ctrlmesh.ProtectFinalizer) {
			return r.Update(ctx, root)
		}
		return nil
	}

	if addFinalizers(root, ctrlmesh.ProtectFinalizer) {
		if err := r.Update(ctx, root); err != nil {
			return err
		}
	}

	newHash := utils.GetMD5Hash(utils.DumpJSON(root.Spec.Root))
	oldMap := map[string]*ctrlmeshv1alpha1.ShardingConfig{}
	for i, cfg := range oldCfgs.Items {
		if cfg.Annotations[ctrlmesh.KdAutoShardingHashAnno] != newHash {
			err := r.Delete(ctx, &oldCfgs.Items[i])
			if err != nil {
				return err
			}
			continue
		}
		oldMap[cfg.Name] = &oldCfgs.Items[i]
	}
	newCfg := getChild(root)
	for i, cfg := range newCfg {
		if _, ok := oldMap[cfg.Name]; ok {
			continue
		}
		if err := r.Create(ctx, newCfg[i]); err != nil {
			return err
		}
	}
	if syncStatus(root, newCfg) {
		return r.Status().Update(ctx, root)
	}
	return nil
}

func getChild(root *ctrlmeshv1alpha1.ShardingConfig) (result []*ctrlmeshv1alpha1.ShardingConfig) {

	if root.Spec.Root.Auto == nil {
		// TODO: Manual
		return
	}

	hash := utils.GetMD5Hash(utils.DumpJSON(root.Spec.Root))
	if root.Spec.Root.Canary != nil && root.Spec.Root.Canary.Replicas != nil && (len(root.Spec.Root.Canary.InNamespaces) > 0 || len(root.Spec.Root.Canary.InShardHash) > 0) {
		var podSelector *metav1.LabelSelector
		if root.Spec.Selector == nil {
			podSelector = &metav1.LabelSelector{}
		} else {
			podSelector = root.Spec.Selector.DeepCopy()
		}
		podSelector.MatchExpressions = append(podSelector.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      "statefulset.kubernetes.io/pod-name",
			Operator: metav1.LabelSelectorOpIn,
			Values:   genNameInRange(root.Spec.Root.TargetStatefulSet, 0, *root.Spec.Root.Canary.Replicas-1),
		})
		canary := &ctrlmeshv1alpha1.ShardingConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      root.Spec.Root.Prefix + "-0-canary",
				Namespace: root.Namespace,
				Annotations: map[string]string{
					ctrlmesh.KdAutoShardingHashAnno: hash,
				},
				Labels: map[string]string{
					ctrlmesh.KdAutoShardingRootLabel: root.Name,
				},
			},
			Spec: ctrlmeshv1alpha1.ShardingConfigSpec{
				Selector: podSelector,
			},
		}
		if root.Spec.Controller != nil {
			canary.Spec.Controller = root.Spec.Controller.DeepCopy()
		}
		if root.Spec.Webhook != nil {
			canary.Spec.Webhook = root.Spec.Webhook.DeepCopy()
		}
		canary.Spec.Limits = genCanaryLimits(root.Spec.Root.Canary, root.Spec.Root.ResourceSelector)
		result = append(result, canary)
	} else {
		var rp int
		root.Spec.Root.Canary.Replicas = &rp
	}
	var id int
	for id = 1; id <= root.Spec.Root.Auto.ShardingSize; id++ {
		var podSelector *metav1.LabelSelector
		if root.Spec.Selector == nil {
			podSelector = &metav1.LabelSelector{}
		} else {
			podSelector = root.Spec.Selector.DeepCopy()
		}
		podSelector.MatchExpressions = append(podSelector.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      "statefulset.kubernetes.io/pod-name",
			Operator: metav1.LabelSelectorOpIn,
			Values:   genNameInRange(root.Spec.Root.TargetStatefulSet, *root.Spec.Root.Canary.Replicas+root.Spec.Root.Auto.EveryShardReplicas*(id-1), *root.Spec.Root.Canary.Replicas+root.Spec.Root.Auto.EveryShardReplicas*id-1),
		})
		_, _, batch := getRange(id, root.Spec.Root.Auto.ShardingSize, 32)
		cfg := &ctrlmeshv1alpha1.ShardingConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      root.Spec.Root.Prefix + "-" + strconv.Itoa(id) + "-normal",
				Namespace: root.Namespace,
				Annotations: map[string]string{
					ctrlmesh.KdAutoShardingHashAnno: hash,
				},
				Labels: map[string]string{
					ctrlmesh.KdAutoShardingRootLabel: root.Name,
				},
			},
			Spec: ctrlmeshv1alpha1.ShardingConfigSpec{
				Selector: podSelector,
			},
		}
		if root.Spec.Controller != nil {
			cfg.Spec.Controller = root.Spec.Controller.DeepCopy()
		}
		if root.Spec.Webhook != nil {
			cfg.Spec.Webhook = root.Spec.Webhook.DeepCopy()
		}
		cfg.Spec.Limits = genShardingGlobalLimits(root, batch, root.Spec.Root.ResourceSelector)
		result = append(result, cfg)
	}
	return result
}

func genCanaryLimits(canaryConfig *ctrlmeshv1alpha1.CanaryConfig, originLimiter []ctrlmeshv1alpha1.ObjectLimiter) (result []ctrlmeshv1alpha1.ObjectLimiter) {
	for i := range originLimiter {
		newSel := originLimiter[i].DeepCopy()
		if newSel.Selector == nil {
			newSel.Selector = &metav1.LabelSelector{}
		}
		if len(canaryConfig.InNamespaces) > 0 {
			newSel.Selector.MatchExpressions = append(newSel.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      ctrlmesh.KdNamespaceKey,
				Operator: metav1.LabelSelectorOpIn,
				Values:   canaryConfig.InNamespaces,
			})
		}
		if len(canaryConfig.InShardHash) > 0 {
			newSel.Selector.MatchExpressions = append(newSel.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      ctrlmesh.KdShardHashKey,
				Operator: metav1.LabelSelectorOpIn,
				Values:   canaryConfig.InShardHash,
			})
		}
		result = append(result, *newSel)
	}
	return result
}

func genShardingGlobalLimits(root *ctrlmeshv1alpha1.ShardingConfig, batch []string, originLimiter []ctrlmeshv1alpha1.ObjectLimiter) (result []ctrlmeshv1alpha1.ObjectLimiter) {
	canaryConfig := root.Spec.Root.Canary
	if canaryConfig != nil && len(canaryConfig.InShardHash) > 0 {
		batch = clearSame(batch, canaryConfig.InShardHash)
	}
	for i := range originLimiter {
		newSel := originLimiter[i].DeepCopy()
		if newSel.Selector == nil {
			newSel.Selector = &metav1.LabelSelector{}
		}
		if canaryConfig != nil && len(canaryConfig.InNamespaces) > 0 && *root.Spec.Root.Canary.Replicas > 0 {
			newSel.Selector.MatchExpressions = append(newSel.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      ctrlmesh.KdNamespaceKey,
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   canaryConfig.InNamespaces,
			})
		}
		if canaryConfig != nil && len(canaryConfig.InShardHash) > 0 && *root.Spec.Root.Canary.Replicas > 0 {
			newSel.Selector.MatchExpressions = append(newSel.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      ctrlmesh.KdShardHashKey,
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   canaryConfig.InShardHash,
			})
		}
		newSel.Selector.MatchExpressions = append(newSel.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      ctrlmesh.KdShardHashKey,
			Operator: metav1.LabelSelectorOpIn,
			Values:   batch,
		})
		result = append(result, *newSel)
	}
	return
}

func syncStatus(root *ctrlmeshv1alpha1.ShardingConfig, scs []*ctrlmeshv1alpha1.ShardingConfig) bool {
	var newScs []string
	for _, cfg := range scs {
		newScs = append(newScs, cfg.Name)
	}
	if utils.GetMD5Hash(utils.DumpJSON(root.Status.Root.Child)) != utils.GetMD5Hash(utils.DumpJSON(newScs)) {
		root.Status.Root.Child = newScs
		return true
	}
	return false
}

func clearStatus(cfg *ctrlmeshv1alpha1.ShardingConfig) bool {
	if len(cfg.Status.Root.Child) > 0 {
		cfg.Status.Root.Child = nil
		return true
	}
	return false
}

func getRange(id, shardingSize, all int) (s, t int, batch []string) {
	batchSize := math.Ceil(float64(all) / float64(shardingSize))
	s = int(batchSize) * (id - 1)
	t = int(batchSize)*id - 1
	if t > all-1 {
		t = all
	}
	for i := s; i <= t && i < all; i++ {
		batch = append(batch, strconv.Itoa(i))
	}
	return
}

func clearSame(data []string, target []string) (result []string) {
	for _, id := range data {
		for _, targetId := range target {
			if id == targetId {
				continue
			}
		}
		result = append(result, id)
	}
	return
}

func genNameInRange(prefix string, s, t int) (result []string) {
	for i := s; i <= t; i++ {
		result = append(result, prefix+"-"+strconv.Itoa(i))
	}
	return result
}

func clearFinalizers(cfg *ctrlmeshv1alpha1.ShardingConfig, key string) (res bool) {
	for i := range cfg.Finalizers {
		if cfg.Finalizers[i] == key {
			cfg.Finalizers = append(cfg.Finalizers[:i], cfg.Finalizers[i+1:]...)
			res = true
		}
	}
	return res
}

func addFinalizers(cfg *ctrlmeshv1alpha1.ShardingConfig, key string) bool {
	for i := range cfg.Finalizers {
		if cfg.Finalizers[i] == key {
			return false
		}
	}
	cfg.Finalizers = append(cfg.Finalizers, key)
	return true
}
