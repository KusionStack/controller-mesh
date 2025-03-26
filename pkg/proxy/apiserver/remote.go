/*
Copyright 2024 The KusionStack Authors.

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

package apiserver

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClusterStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters: make(map[string]*Cluster),
	}
}

type ClusterStore struct {
	sync.RWMutex
	// keyed by api server host
	remoteClusters map[string]*Cluster
}

// Cluster defines cluster struct
type Cluster struct {
	// Client for accessing the cluster.
	sync.RWMutex
	Config        *rest.Config
	Transport     http.RoundTripper
	KubeConfigSha [sha256.Size]byte
}

func (c *ClusterStore) Get(host string) *Cluster {
	c.RLock()
	defer c.RUnlock()
	return c.remoteClusters[host]
}

func (c *ClusterStore) StoreListOf(kubeConfigs ...[]byte) (err error) {
	for _, kubeConfig := range kubeConfigs {
		if localErr := c.Store(kubeConfig); localErr != nil {
			err = errors.Join(err, localErr)
		}
	}
	return err
}

func (c *ClusterStore) Store(kubeConfig []byte) error {
	sha := sha256.Sum256(kubeConfig)
	cfg, err := DefaultBuildRestConfig(kubeConfig)
	if err != nil {
		return err
	}
	c.Lock()
	defer c.Unlock()
	cluster, ok := c.remoteClusters[cfg.Host]
	if ok && bytes.Equal(sha[:], cluster.KubeConfigSha[:]) {
		return nil
	}
	tp, err := rest.TransportFor(cfg)
	if err != nil {
		return err
	}
	c.remoteClusters[cfg.Host] = &Cluster{
		Config:        cfg,
		KubeConfigSha: sha,
		Transport:     tp,
	}
	return nil
}

func (c *ClusterStore) Delete(kubeConfig []byte) error {
	cfg, err := DefaultBuildRestConfig(kubeConfig)
	if err != nil {
		return err
	}
	c.Lock()
	defer c.Unlock()
	delete(c.remoteClusters, cfg.Host)
	return nil
}

func DefaultBuildRestConfig(kubeConfig []byte) (*rest.Config, error) {
	if len(kubeConfig) == 0 {
		return nil, fmt.Errorf("kubeconfig is empty")
	}
	rawConfig, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig cannot be loaded: %v", err)
	}
	if err = clientcmd.Validate(*rawConfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not valid: %v", err)
	}
	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})
	return clientConfig.ClientConfig()
}
