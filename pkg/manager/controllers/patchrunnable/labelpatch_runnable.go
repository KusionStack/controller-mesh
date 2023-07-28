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

package patchrunnable

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/KusionStack/kridge/pkg/apis/kridge"
	"github.com/KusionStack/kridge/pkg/apis/kridge/constants"
	util "github.com/KusionStack/kridge/pkg/utils"
	"github.com/KusionStack/kridge/pkg/utils/rand"
)

var (
	ignoredSystemNamespace = sets.New[string]("kube-system", "kube-public", "default", "")
	defaultRetryBackoff    = wait.Backoff{
		Steps:    20,
		Duration: 10 * time.Millisecond,
		Factor:   1.0,
		Jitter:   0.1,
	}
	default429BackOff = wait.Backoff{
		Steps:    50,
		Duration: 500 * time.Millisecond,
		Factor:   1.1,
		Jitter:   0.2,
	}
	patchWorkerSize    = 10
	listWorkerSize     = 2
	patchItemCacheSize = 200
)

func SetupWithManager(mgr manager.Manager) error {
	return mgr.Add(&PatchRunnable{
		Client:          mgr.GetClient(),
		directorClient:  util.NewDirectorClientFromManager(mgr, "patch-runnable"),
		discoveryClient: discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()),
	})
}

type PatchRunnable struct {
	client.Client
	directorClient  client.Client
	discoveryClient *discovery.DiscoveryClient

	groupVersionKinds []schema.GroupVersionKind

	invalid  sets.Set[string]
	invalidL sync.RWMutex
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=kridge.kusionstack.io,resources="",verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources="",verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="*",resources=pods;services;statefulsets;controllerrevisions;configmaps;persistentvolumeclaims;endpoints;deployments,verbs=get;list;watch;create;update;patch;delete

func (p *PatchRunnable) Start(ctx context.Context) error {
	namespaceList := v1.NamespaceList{}
	if err := p.directorClient.List(ctx, &namespaceList); err != nil {
		klog.Infof("fail to list all namespaces: %v", err)
		return err
	}

	ch := make(chan struct{}, patchWorkerSize)
	var namespaces []*v1.Namespace

	wg := sync.WaitGroup{}
	wg.Add(len(namespaceList.Items))
	// patch label on namespace
	for i := range namespaceList.Items {
		ns := &namespaceList.Items[i]
		ch <- struct{}{}
		go func() {
			defer func() {
				<-ch
				wg.Done()
			}()
			if ignoredSystemNamespace.Has(ns.Name) {
				return
			}
			if ns.DeletionTimestamp != nil {
				return
			}
			namespaces = append(namespaces, ns)

			if err := retry.RetryOnConflict(defaultRetryBackoff, func() error {
				if localErr := p.Get(context.TODO(), types.NamespacedName{Name: ns.Name}, ns); localErr != nil {
					return localErr
				}
				ok, localErr := updateLabel(ns)
				if localErr != nil {
					klog.Warningf("update namespace %s error, %v", ns.Name, localErr)
				}
				if !ok {
					return nil
				}
				return p.directorClient.Update(context.TODO(), ns)
			}); err != nil {
				klog.Errorf("update namespace %s failed with unexpected error, %v", ns.Name)
			}
		}()
	}
	wg.Wait()

	gvks, err := LoadGroupVersionKind(p.directorClient, p.discoveryClient)
	if err != nil {
		klog.Errorf("fail to load group version kind, %v", err)
		return err
	}
	klog.Infof("load group version kind %s", util.DumpJSON(gvks))
	p.groupVersionKinds = gvks
	p.invalid = sets.Set[string]{}

	waitPatchCh := make(chan patchItem, patchItemCacheSize)

	go p.getAllResources(&namespaceList, waitPatchCh)

	// patch all resources
	wg.Add(patchWorkerSize)
	for i := 0; i < patchWorkerSize; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case patchInfo, ok := <-waitPatchCh:
					if !ok {
						return
					}
					if patchErr := p.patch(ctx, patchInfo); patchErr != nil {
						klog.Errorf("fail to patch label on %s/%s, %v", patchInfo.Object.GetNamespace(), patchInfo.Object.GetName(), patchErr)
					} else {
						klog.Infof("finish patch label on %s/%s", patchInfo.Object.GetNamespace(), patchInfo.Object.GetName())
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	wg.Wait()
	klog.Infof("finished patching label")
	return nil
}

func (p *PatchRunnable) getAllResources(namespaceList *v1.NamespaceList, itemCh chan<- patchItem) {
	ch := make(chan struct{}, 10)
	wg := sync.WaitGroup{}
	wg.Add(len(namespaceList.Items))
	for i := range namespaceList.Items {
		ns := &namespaceList.Items[i]
		id := i
		ch <- struct{}{}
		go func() {
			defer func() {
				<-ch
				wg.Done()
				klog.Infof("finish collect resource in namespace %s, %d/%d", ns.Name, id+1, len(namespaceList.Items))
			}()
			p.getResourceInNamespace(ns, itemCh)
		}()
	}
	wg.Wait()
	defer klog.Infof("finish collect resources")
	for {
		if len(itemCh) == 0 {
			close(itemCh)
			return
		}
		<-time.After(5 * time.Second)
	}
}

func (p *PatchRunnable) getResourceInNamespace(namespace *v1.Namespace, itemCh chan<- patchItem) {

	shardingLabels := getShardingLabel(namespace)
	wg := sync.WaitGroup{}
	wg.Add(len(p.groupVersionKinds))
	ch := make(chan struct{}, listWorkerSize)
	for index := range p.groupVersionKinds {
		gvk := p.groupVersionKinds[index]
		ch <- struct{}{}
		go func() {
			defer func() {
				<-ch
				wg.Done()
			}()

			if p.isInvalid(gvk) {
				return
			}
			resourceList := &unstructured.UnstructuredList{}
			resourceList.SetAPIVersion(gvk.GroupVersion().String())
			resourceList.SetKind(gvk.Kind)

			ops := &client.ListOptions{
				Namespace: namespace.Name,
			}

			if err := RetryOnToManyRequests(default429BackOff, func() error {
				return p.directorClient.List(context.TODO(), resourceList, ops)
			}); err != nil {
				if strings.Contains(err.Error(), "no matches") {
					p.setInvalid(gvk)
				}
				klog.Errorf("current ns=%s, fail to list %s, %s", namespace.Name, gvk.Kind, err)
				return
			}
			if len(resourceList.Items) == 0 || resourceList.Items[0].GetNamespace() == "" {
				return
			}
			for i := range resourceList.Items {
				item := &resourceList.Items[i]
				patchLabels := shouldPatchLabel(item.GetLabels(), shardingLabels)

				if len(patchLabels) == 0 {
					continue
				}

				str, _ := json.Marshal(patchLabels)
				klog.Infof("send resource %s:%s/%s", item.GetKind(), item.GetNamespace(), item.GetName())
				itemCh <- patchItem{
					Object: item,
					Patch:  client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":%s}}`, str))),
				}
			}
		}()
	}
	wg.Wait()
}

func (p *PatchRunnable) patch(ctx context.Context, item patchItem) error {
	return RetryOnToManyRequests(default429BackOff, func() error {
		return p.directorClient.Patch(ctx, item.Object, item.Patch)
	})
}

type patchItem struct {
	Object client.Object
	Patch  client.Patch
}

func (p *PatchRunnable) isInvalid(gvk schema.GroupVersionKind) bool {
	p.invalidL.RLock()
	defer p.invalidL.RUnlock()
	return p.invalid.Has(groupVersionKindKey(gvk))
}

func (p *PatchRunnable) setInvalid(gvk schema.GroupVersionKind) {
	p.invalidL.Lock()
	defer p.invalidL.Unlock()
	p.invalid.Insert(groupVersionKindKey(gvk))
}

func groupVersionKindKey(gvk schema.GroupVersionKind) string {
	return gvk.GroupVersion().String() + "/" + gvk.Kind
}

func updateLabel(ns *v1.Namespace) (bool, error) {
	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}

	nsHash := strconv.Itoa(rand.Hash(ns.Name, constants.DefaultShardingSize))

	if _, ok := ns.Labels[kridge.KdControlKey]; !ok {
		if _, exist := ns.Labels[kridge.KdShardingHashKey]; exist {
			return false, fmt.Errorf("label %s already exist but can not find %s", kridge.KdShardingHashKey, kridge.KdControlKey)
		}
		if _, exist := ns.Labels[kridge.KdNamespaceKey]; exist {
			return false, fmt.Errorf("label %s already exist but can not find %s", kridge.KdNamespaceKey, kridge.KdControlKey)
		}
		ns.Labels[kridge.KdControlKey] = "true"
		ns.Labels[kridge.KdNamespaceKey] = ns.Name
		ns.Labels[kridge.KdShardingHashKey] = nsHash
		return true, nil
	} else {
		if val, exist := ns.Labels[kridge.KdShardingHashKey]; !exist || nsHash != val {
			ns.Labels[kridge.KdNamespaceKey] = ns.Name
			ns.Labels[kridge.KdShardingHashKey] = nsHash
			return true, nil
		}
		if val, exist := ns.Labels[kridge.KdNamespaceKey]; !exist || val != ns.Name {
			ns.Labels[kridge.KdNamespaceKey] = ns.Name
			return true, nil
		}
	}
	return false, nil
}

func RetryOnToManyRequests(backoff wait.Backoff, fn func() error) error {
	return retry.OnError(backoff, errors.IsTooManyRequests, fn)
}

func shouldPatchLabel(old, target map[string]string) map[string]string {
	patch := map[string]string{}
	for key, val := range target {
		if v, ok := old[key]; !ok || v != val {
			patch[key] = val
		}
	}
	return patch
}
