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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/client/clientset/versioned/scheme"
)

var (
	env *envtest.Environment
	mgr manager.Manager
	c   client.Client

	request chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	utilruntime.Must(ctrlmeshv1alpha1.AddToScheme(clientgoscheme.Scheme))

	env = &envtest.Environment{
		Scheme:            scheme.Scheme,
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
	}

	config, err := env.Start()
	if err != nil {
		panic(err)
	}
	wg.Add(1)
	go func() {
		mgr, err = manager.New(config, manager.Options{
			MetricsBindAddress: "0",
		})

		request = make(chan struct{}, 5)
		if err = (&CircuitBreakerReconciler{
			Client: &warpClient{
				Client: mgr.GetClient(),
				ch:     request,
			},
		}).SetupWithManager(mgr); err != nil {
			panic(err)
		}

		c = mgr.GetClient()
		defer wg.Done()
		err = mgr.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()
	<-time.After(time.Second * 3)
	code := m.Run()
	env.Stop()
	os.Exit(code)
}

func Stop() {
	cancel()
	wg.Wait()
}

type warpClient struct {
	client.Client
	ch chan struct{}
}

func (w *warpClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	w.ch <- struct{}{}
	return w.Client.Get(ctx, key, obj, opts...)
}

func waitProcess() {
	waitProcessFinished(request)
}

func waitProcessFinished(reqChan chan struct{}) {
	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-time.After(5 * time.Second):
			return
		case <-timeout:
			fmt.Println("timeout!")
			return
		case <-reqChan:
			continue
		}
	}
}
