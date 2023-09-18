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

package main

import (
	"flag"
	"fmt"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appcontroller "github.com/KusionStack/ctrlmesh/e2e/customoperator/app/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

func main() {

	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	ctrl.SetLogger(klogr.New())

	cfg := ctrl.GetConfigOrDie()
	cfg.UserAgent = "testapp"

	mgr, err := manager.New(cfg, ctrl.Options{
		Scheme:                     scheme,
		LeaderElection:             true,
		LeaderElectionID:           "testapp",
		LeaderElectionNamespace:    os.Getenv("POD_NAMESPACE"),
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	directorClient := NewDirectorClientFromManager(mgr, "checker")

	go func() {
		if err = (&appcontroller.PodReconciler{
			Client:         mgr.GetClient(),
			DirectorClient: directorClient,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Recorder")
			os.Exit(1)
		}
	}()

	go func() {
		<-time.After(20 * time.Second)
		appcontroller.Checker(ctx, directorClient)
	}()
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func NewDirectorClientFromManager(mgr manager.Manager, name string) client.Client {
	cfg := rest.CopyConfig(mgr.GetConfig())
	cfg.UserAgent = fmt.Sprintf("director-client/%s", name)

	c, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()})
	if err != nil {
		panic(err)
	}
	return c
}
