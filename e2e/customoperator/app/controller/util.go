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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	cmName = "test-resource-recorder"
)

func initConfigMap(c client.Client) *v1.ConfigMap {
	//podName := os.Getenv("POD_NAME")
	cm := &v1.ConfigMap{}
	cm.Name = cmName
	cm.Namespace = podNamespace
	cm.Data = map[string]string{}
	if err := c.Create(context.TODO(), cm); err != nil {
		klog.Errorf("Failed to create cm %v", err)
	}
	return cm
}

var Retry = wait.Backoff{
	Steps:    50,
	Duration: 10 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

func add(c client.Client, namespace, kind string) error {
	if namespace == "" || kind == "" {
		return nil
	}
	key := podName
	return retry.RetryOnConflict(Retry, func() error {
		cm := &v1.ConfigMap{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: cmName, Namespace: podNamespace}, cm); err != nil {
			klog.Errorf("Failed to get cm %v, try init", err)
			cm = initConfigMap(c)
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		val, _ := cm.Data[key]
		sto := &store{}
		if val == "" {
			sto.Kinds = map[string]string{}
		} else {
			json.Unmarshal([]byte(val), sto)
		}
		names, _ := sto.Kinds[kind]
		nameSet := list(names)
		if nameSet.Has(namespace) {
			return nil
		}
		nameSet.Insert(namespace)
		sto.Kinds[kind] = toString(nameSet)
		bt, _ := json.Marshal(sto)
		cm.Data[key] = string(bt)
		return c.Update(context.TODO(), cm)
	})
}

func Checker(ctx context.Context, c client.Client) {
	cm := &v1.ConfigMap{}
	for {
		resources := map[string]sets.Set[string]{}
		<-time.After(20 * time.Second)

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := c.Get(context.TODO(), types.NamespacedName{Name: cmName, Namespace: podNamespace}, cm); err != nil {
			klog.Errorf("fail to get configMap %s, %v", cmName, err)
			continue
		}
		if cm.Data == nil {
			klog.Infof("nil configmap data")
			continue
		}
		for pod, val := range cm.Data {
			sto := &store{}
			if err := json.Unmarshal([]byte(val), sto); err != nil {
				klog.Errorf("fail to unmarshal store, %v", err)
			}
			if sto.Kinds == nil {
				continue
			}
			for kind, names := range sto.Kinds {
				set, ok := resources[kind]
				if !ok {
					set = list(names)
					resources[kind] = set
					continue
				}
				nameList := list(names).UnsortedList()
				for _, name := range nameList {
					if set.Has(name) {
						msg := fmt.Sprintf("conflict namespace %s in store, pod: %s", name, pod)
						klog.Errorf(msg)
						<-time.After(20 * time.Second)
						panic(msg)
					}
					set.Insert(name)
				}
				resources[kind] = set
			}
		}
	}
}

func list(val string) sets.Set[string] {
	res := sets.New[string]()
	if val == "" {
		return res
	}

	res.Insert(strings.Split(val, ",")...)
	return res
}

func toString(names sets.Set[string]) string {
	return strings.Join(sets.List(names), ",")
}

type store struct {
	Kinds map[string]string
}
