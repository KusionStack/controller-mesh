/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

package webhook

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	webhookutil "github.com/KusionStack/kridge/pkg/utils"
	"github.com/KusionStack/kridge/pkg/webhook/ns"
	"github.com/KusionStack/kridge/pkg/webhook/pod"
	"github.com/KusionStack/kridge/pkg/webhook/resources"
	"github.com/KusionStack/kridge/pkg/webhook/shardingconfig"
	webhookcontroller "github.com/KusionStack/kridge/pkg/webhook/util/controller"
	"github.com/KusionStack/kridge/pkg/webhook/util/health"
)

var (
	// HandlerMap contains all admission webhook handlers.
	HandlerMap = map[string]admission.Handler{}

	Checker = health.Checker
)

func init() {
	addHandlers(pod.HandlerMap)
	addHandlers(shardingconfig.HandlerMap)
	addHandlers(resources.HandlerMap)
	addHandlers(ns.HandlerMap)
}

func addHandlers(m map[string]admission.Handler) {
	for path, handler := range m {
		if len(path) == 0 {
			klog.Warningf("Skip handler with empty path.")
			continue
		}
		if path[0] != '/' {
			path = "/" + path
		}
		_, found := HandlerMap[path]
		if found {
			klog.V(1).Infof("conflicting webhook builder path %v in handler map", path)
		}
		HandlerMap[path] = handler
	}
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;update;patch

func SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	server := mgr.GetWebhookServer()
	server.Host = "0.0.0.0"
	server.Port = webhookutil.GetPort()
	server.CertDir = webhookutil.GetCertDir()

	// register admission handlers
	for path, handler := range HandlerMap {
		server.Register(path, &webhook.Admission{Handler: handler})
		klog.V(3).Infof("Registered webhook handler %s", path)
	}

	// register conversion webhook
	server.Register("/convert", &conversion.Webhook{})

	// register health handler
	server.Register("/healthz", &health.Handler{})

	c, err := webhookcontroller.New()
	if err != nil {
		return err
	}
	go func() {
		c.Start(ctx.Done())
	}()

	timer := time.NewTimer(time.Second * 20)
	defer timer.Stop()
	select {
	case <-webhookcontroller.Initialized():
		return nil
	case <-timer.C:
		return fmt.Errorf("failed to start webhook controller for waiting more than 20s")
	}
}

func WaitReady() error {
	startTS := time.Now()
	var err error
	for {
		duration := time.Since(startTS)
		if err = Checker(nil); err == nil {
			return nil
		}

		if duration > time.Second*5 {
			klog.Warningf("Failed to wait webhook ready over %s: %v", duration, err)
		}
		if duration > time.Minute {
			return err
		}
		time.Sleep(time.Second * 2)
	}

}
