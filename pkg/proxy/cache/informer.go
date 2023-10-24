package cache

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	ctrlmeshclient "github.com/KusionStack/controller-mesh/pkg/client"
	ctrlmeshinformers "github.com/KusionStack/controller-mesh/pkg/client/informers/externalversions/ctrlmesh/v1alpha1"
	ctrlmeshv1alpha1listers "github.com/KusionStack/controller-mesh/pkg/client/listers/ctrlmesh/v1alpha1"
)

func NewManagerStateCache(ctx context.Context) (ManagerStateInterface, error) {

	informer := ctrlmeshinformers.NewFilteredManagerStateInformer(ctrlmeshclient.GetGenericClient().MeshClient, 0, cache.Indexers{}, func(opts *metav1.ListOptions) {
		opts.FieldSelector = "metadata.name=" + ctrlmeshv1alpha1.NameOfManager
	})
	lister := ctrlmeshv1alpha1listers.NewManagerStateLister(informer.GetIndexer())
	go func() {
		informer.Run(ctx.Done())
	}()
	if ok := cache.WaitForCacheSync(ctx.Done(), informer.HasSynced); !ok {
		return nil, fmt.Errorf("error waiting ManagerState informer synced")
	}

	return &managerStateCache{
		informer: informer,
		lister:   lister,
	}, nil
}

type ManagerStateInterface interface {
	Get() (*ctrlmeshv1alpha1.ManagerState, error)
}

type managerStateCache struct {
	informer cache.SharedIndexInformer
	lister   ctrlmeshv1alpha1listers.ManagerStateLister
}

func (m *managerStateCache) Get() (*ctrlmeshv1alpha1.ManagerState, error) {
	return m.lister.Get(ctrlmeshv1alpha1.NameOfManager)
}
