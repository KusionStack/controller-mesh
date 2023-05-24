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

package expectation

import (
	"fmt"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func NewExpectations(name string, timeOut time.Duration) *Expectations {
	if timeOut == 0 {
		timeOut = ExpectationsTimeout
	}
	return &Expectations{Store: cache.NewStore(key), name: name, timeOut: timeOut}
}

const (
	ExpectationsTimeout = 3 * time.Minute
)

type Expectations struct {
	cache.Store

	name    string
	timeOut time.Duration
}

func (e *Expectations) Record(key string, timestamp int64) error {
	if err := e.SetExpectation(key, timestamp); err != nil {
		return fmt.Errorf("fail to record %s %s status update", e.name, key)
	}
	return nil
}
func (e *Expectations) SetExpectation(key string, value int64) error {
	item := &Expectation{key: key, value: value, timestamp: time.Now()}
	return e.Add(item)
}

func (e *Expectations) Satisfied(key string, timestamp int64) bool {
	if expectation := e.GetExpectation(key); expectation == nil {
		return true
	} else {
		if expectation.value <= timestamp {
			return true
		}
		if expectation.isExpired() {
			klog.Errorf("expectation expired for key %s", key)
			panic(fmt.Sprintf("expected panic for expectation up-to-date timeout for key %s", key))
		}
		return false
	}
}

func (e *Expectations) GetExpectation(key string) *Expectation {
	exp, exist, err := e.GetByKey(key)
	if !exist {
		return nil
	}

	if err != nil {
		klog.Warningf("fail to get expectation with key %s: %s", key, err)
		return nil
	}

	return exp.(*Expectation)
}

func (e *Expectations) DeleteExpectation(key string) {
	if exp, exists, err := e.GetByKey(key); err == nil && exists {
		if err := e.Delete(exp); err != nil {
			klog.Warningf("fail deleting expectations for controller %s: %s", key, err)
		}
	}
}

type Expectation struct {
	key       string
	value     int64
	timestamp time.Time
}

func (e Expectation) isExpired() bool {
	if time.Since(e.timestamp) >= ExpectationsTimeout {
		return true
	}

	return false
}

func key(obj interface{}) (string, error) {
	if i, ok := obj.(*Expectation); !ok {
		return "", fmt.Errorf("expected Item")
	} else {
		return i.key, nil
	}
}
