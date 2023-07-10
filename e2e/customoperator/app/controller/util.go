package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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
	c.Create(context.TODO(), cm)
	return cm
}

func add(c client.Client, namespace, kind string) error {
	if namespace == "" || kind == "" {
		return nil
	}
	key := podName
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm := &v1.ConfigMap{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: cmName, Namespace: podNamespace}, cm); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
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
			klog.Errorf("fail to get configMap %s", cmName)
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
