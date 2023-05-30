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

package controller

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	admissionregistrationinformers "k8s.io/client-go/informers/admissionregistration/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/KusionStack/kridge/pkg/client"
	"github.com/KusionStack/kridge/pkg/utils"
	"github.com/KusionStack/kridge/pkg/webhook/util/generator"
	"github.com/KusionStack/kridge/pkg/webhook/util/writer"
)

var (
	validatingWebhookConfigurationName = "kridge-validating"
	mutatingWebhookConfigurationName   = "kridge-mutating"
)

var (
	namespace   = utils.GetNamespace()
	secretName  = utils.GetSecretName()
	host        = utils.GetHost()
	serviceName = utils.GetServiceName()
	port        = utils.GetPort()

	uninitialized = make(chan struct{})
	onceInit      = sync.Once{}
)

func Initialized() chan struct{} {
	return uninitialized
}

type Controller struct {
	kubeClient clientset.Interface
	handlers   map[string]admission.Handler

	informerFactory informers.SharedInformerFactory
	synced          []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func New() (*Controller, error) {
	c := &Controller{
		kubeClient: client.GetGenericClientWithName("webhook-controller").KubeClient,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "webhook-controller"),
	}

	c.informerFactory = informers.NewSharedInformerFactory(c.kubeClient, 0)
	secretInformer := coreinformers.New(c.informerFactory, namespace, nil).Secrets()
	admissionRegistrationInformer := admissionregistrationinformers.New(c.informerFactory, v1.NamespaceAll, nil)

	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret := obj.(*v1.Secret)
			if secret.Name == secretName {
				klog.Infof("Secret %s added", secretName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			secret := cur.(*v1.Secret)
			if secret.Name == secretName {
				klog.Infof("Secret %s updated", secretName)
				c.queue.AddAfter("", time.Millisecond*100)
			}
		},
	})

	admissionRegistrationInformer.MutatingWebhookConfigurations().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			conf := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
			if conf.Name == mutatingWebhookConfigurationName {
				klog.Infof("MutatingWebhookConfiguration %s added", mutatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			conf := cur.(*admissionregistrationv1.MutatingWebhookConfiguration)
			if conf.Name == mutatingWebhookConfigurationName {
				klog.Infof("MutatingWebhookConfiguration %s update", mutatingWebhookConfigurationName)
				c.queue.AddAfter("", time.Millisecond*100)
			}
		},
	})

	admissionRegistrationInformer.ValidatingWebhookConfigurations().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			conf := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
			if conf.Name == validatingWebhookConfigurationName {
				klog.Infof("ValidatingWebhookConfiguration %s added", validatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			conf := cur.(*admissionregistrationv1.ValidatingWebhookConfiguration)
			if conf.Name == validatingWebhookConfigurationName {
				klog.Infof("ValidatingWebhookConfiguration %s updated", validatingWebhookConfigurationName)
				c.queue.AddAfter("", time.Millisecond*100)
			}
		},
	})

	c.synced = []cache.InformerSynced{
		secretInformer.Informer().HasSynced,
		admissionRegistrationInformer.MutatingWebhookConfigurations().Informer().HasSynced,
		admissionRegistrationInformer.ValidatingWebhookConfigurations().Informer().HasSynced,
	}

	return c, nil
}

func (c *Controller) Start(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting webhook-controller")
	defer klog.Infof("Shutting down webhook-controller")

	c.informerFactory.Start(stopCh)
	if !cache.WaitForNamedCacheSync("webhook-controller", stopCh, c.synced...) {
		return
	}
	// trigger once
	c.queue.Add("")
	go wait.Until(func() {
		for c.processNextWorkItem() {
		}
	}, time.Second, stopCh)
	klog.Infof("Started webhook-controller")

	<-stopCh
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.sync()
	if err == nil {
		//c.queue.AddAfter(key, defaultResyncPeriod)
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	c.queue.AddRateLimited(key)

	return true
}

func (c *Controller) sync() error {
	klog.Infof("Starting to sync webhook certs and configurations")
	defer func() {
		klog.Infof("Finished to sync webhook certs and configurations")
	}()

	var dnsName string
	var certWriter writer.CertWriter
	var err error

	if dnsName = utils.GetHost(); len(dnsName) == 0 {
		dnsName = generator.ServiceToCommonName(utils.GetNamespace(), utils.GetServiceName())
	}

	certWriterType := utils.GetCertWriter()
	if certWriterType == writer.FsCertWriter || (len(certWriterType) == 0 && len(utils.GetHost()) != 0) {
		certWriter, err = writer.NewFSCertWriter(writer.FSCertWriterOptions{
			Path: utils.GetCertDir(),
		})
	} else {
		certWriter, err = writer.NewSecretCertWriter(writer.SecretCertWriterOptions{
			Clientset: c.kubeClient,
			Secret:    &types.NamespacedName{Namespace: utils.GetNamespace(), Name: utils.GetSecretName()},
		})
	}
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}

	klog.Infof("try to ensure cert with dns name %s", dnsName)
	certs, _, err := certWriter.EnsureCert(dnsName)
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}

	klog.Infof("try to write certs to dir %s", utils.GetCertDir())
	if err := writer.WriteCertsToDir(utils.GetCertDir(), certs); err != nil {
		return fmt.Errorf("failed to write certs to dir: %v", err)
	}

	klog.Infof("try ensure configurations")
	if err := c.ensureConfigurations(certs.CACert); err != nil {
		return fmt.Errorf("failed to ensure configuration: %v", err)
	}

	onceInit.Do(func() {
		close(uninitialized)
	})
	return nil
}

func (c *Controller) ensureConfigurations(caBundle []byte) error {
	mutatingResourceConfig, err := c.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), mutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("not found MutatingWebhookConfiguration %s", mutatingWebhookConfigurationName)
	}
	validatingConfig, err := c.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(), validatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("not found ValidatingWebhookConfiguration %s", validatingWebhookConfigurationName)
	}
	oldResourceMutatingConfig := mutatingResourceConfig.DeepCopy()
	oldValidatingConfig := validatingConfig.DeepCopy()

	for i := range mutatingResourceConfig.Webhooks {
		wh := &mutatingResourceConfig.Webhooks[i]
		wh.ClientConfig.CABundle = caBundle
		if wh.ClientConfig.Service != nil {
			wh.ClientConfig.Service.Namespace = namespace
			wh.ClientConfig.Service.Name = serviceName
		}
		if len(host) > 0 && wh.ClientConfig.Service != nil {
			convertClientConfig(&wh.ClientConfig, host, port)
		}
	}

	for i := range validatingConfig.Webhooks {
		wh := &validatingConfig.Webhooks[i]
		wh.ClientConfig.CABundle = caBundle
		if wh.ClientConfig.Service != nil {
			wh.ClientConfig.Service.Namespace = namespace
			wh.ClientConfig.Service.Name = serviceName
		}
		if len(host) > 0 && wh.ClientConfig.Service != nil {
			convertClientConfig(&wh.ClientConfig, host, port)
		}
	}

	if !reflect.DeepEqual(mutatingResourceConfig, oldResourceMutatingConfig) {
		if _, err := c.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.TODO(), mutatingResourceConfig, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update %s: %v", mutatingWebhookConfigurationName, err)
		}
	}

	if !reflect.DeepEqual(validatingConfig, oldValidatingConfig) {
		if _, err := c.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(context.TODO(), validatingConfig, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update %s: %v", validatingWebhookConfigurationName, err)
		}
	}

	return nil
}

func convertClientConfig(clientConfig *admissionregistrationv1.WebhookClientConfig, host string, port int) {
	url := fmt.Sprintf("https://%s:%d%s", host, port, *clientConfig.Service.Path)
	clientConfig.URL = &url
	clientConfig.Service = nil
}
