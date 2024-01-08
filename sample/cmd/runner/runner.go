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

package runner

import (
	"context"
	"fmt"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	controlLabel = "sample.kusionstack.io/control-by"
)

var (
	PodName = os.Getenv("POD_NAME")
)

func New(c client.Client) Runner {
	return &runner{Client: c}
}

type Runner interface {
	Start(ctx context.Context) error
}

type runner struct {
	client.Client
}

// +kubebuilder:rbac:groups=,resources=namespaces,verbs=get;list;watch;create;update;patch

func (r *runner) Start(ctx context.Context) error {
	for {
		if err := r.holdTestResources(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("context done")
		case <-time.After(10 * time.Second):
		}
	}
}

func (r *runner) holdTestResources(ctx context.Context) error {
	nss := &v1.NamespaceList{}
	if err := r.List(ctx, nss); err != nil {
		return err
	}
	var holdNs []string
	for i := range nss.Items {
		ns := &nss.Items[i]
		holdNs = append(holdNs, ns.Name)
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Get(ctx, types.NamespacedName{Name: nss.Items[i].Name}, ns); err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			if ns.Labels == nil {
				ns.Labels = map[string]string{}
			}
			if controlName, ok := ns.Labels[controlLabel]; !ok || PodName != controlName {
				ns.Labels[controlLabel] = PodName
				return r.Update(ctx, ns)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	klog.Infof("hold namespaces %v", holdNs)
	return nil
}
