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
	"net/http"
	_ "net/http/pprof"
	"os"

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
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ctrlmeshv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/ctrlmesh/pkg/cachelimiter"
	"github.com/KusionStack/ctrlmesh/pkg/client"
	"github.com/KusionStack/ctrlmesh/pkg/grpcregistry"
	"github.com/KusionStack/ctrlmesh/pkg/manager/controllers/managerstate"
	"github.com/KusionStack/ctrlmesh/pkg/manager/controllers/patchrunnable"
	"github.com/KusionStack/ctrlmesh/pkg/manager/controllers/shardingconfigserver"
	pkgcache "github.com/KusionStack/ctrlmesh/pkg/utils/cache"
	"github.com/KusionStack/ctrlmesh/pkg/utils/probe"
	"github.com/KusionStack/ctrlmesh/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	restConfigQPS   = flag.Int("rest-config-qps", 30, "QPS of rest config.")
	restConfigBurst = flag.Int("rest-config-burst", 50, "Burst of rest config.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(ctrlmeshv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete

func main() {
	var metricsAddr string
	var probeAddr string
	var pprofAddr string
	var leaderElectionNamespace string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "ctrlmesh-system",
		"This determines the namespace in which the leader election configmap will be created, it will use in-cluster namespace if empty.")
	flag.StringVar(&pprofAddr, "pprof-address", ":8090", "The address the pprof binds to.")

	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	ctrl.SetLogger(klogr.New())

	go func() {
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			setupLog.Error(err, "unable to start pprof")
		}
	}()

	cfg := ctrl.GetConfigOrDie()
	setRestConfig(cfg)
	cfg.UserAgent = "ctrlmesh-manager"
	if err := client.NewRegistry(cfg); err != nil {
		setupLog.Error(err, "unable to init clientset and informer")
		os.Exit(1)
	}

	mgr, err := manager.New(cfg, ctrl.Options{
		Scheme:                     scheme,
		MetricsBindAddress:         metricsAddr,
		HealthProbeBindAddress:     probeAddr,
		LeaderElection:             true,
		LeaderElectionID:           "ctrlmesh-manager",
		LeaderElectionNamespace:    leaderElectionNamespace,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		// limit manager cache
		NewCache: pkgcache.WarpNewCacheWithSelector(nil, cachelimiter.DefaultObjectSelector()),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	if err = webhook.SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to setup webhook")
		os.Exit(1)
	}

	if err := grpcregistry.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup gRPC registry")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("webhook-ready", webhook.Checker); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	go func() {
		setupLog.Info("wait webhook ready")
		if err = webhook.WaitReady(); err != nil {
			setupLog.Error(err, "unable to wait webhook ready")
			os.Exit(1)
		}

		if err = (&shardingconfigserver.ShardingConfigReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ShardingConfig")
			os.Exit(1)
		}
		if err = (&managerstate.ManagerStateReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ManagerState")
			os.Exit(1)
		}

		if err = patchrunnable.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create runnable", "runnable", "PatchRunnable")
			os.Exit(1)
		}
	}()

	probe.EnableDelay()
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setRestConfig(c *rest.Config) {
	if *restConfigQPS > 0 {
		c.QPS = float32(*restConfigQPS)
	}
	if *restConfigBurst > 0 {
		c.Burst = *restConfigBurst
	}
}
