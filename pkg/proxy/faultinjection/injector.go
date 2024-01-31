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

package faultinjection

import (
	"encoding/json"
	"net/http"
	"time"

	"k8s.io/klog/v2"

	"github.com/KusionStack/controller-mesh/pkg/utils"
)

type Injector interface {
	Do(w http.ResponseWriter, req *http.Request) (abort bool)
	Abort() bool
}

type abortWithDelayInjector struct {
	abort   bool
	delay   time.Duration
	code    int
	message string
}

func (m *abortWithDelayInjector) Do(w http.ResponseWriter, req *http.Request) bool {
	if m.delay != 0 {
		<-time.After(m.delay)
	}
	if m.code != http.StatusOK && m.code != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.code)
		apiErr := utils.HttpToAPIError(m.code, req.Method, m.message)
		if err := json.NewEncoder(w).Encode(apiErr); err != nil {
			klog.Errorf("failed to write api error response: %v", err)
			return true
		}
		klog.Infof("abort by faultInjection, %s, %s, %d, with delay %s", apiErr.Reason, apiErr.Message, apiErr.Code, m.delay/time.Second)
		return true
	}
	klog.Infof("delay by faultInjection, %s", m.delay/time.Second)
	return false
}

func (m *abortWithDelayInjector) AddDelay(d time.Duration) {
	m.delay += d
}

func (m *abortWithDelayInjector) AddAbort(code int, message string) {
	m.abort = true
	m.code = code
	m.message = message
}

func (m *abortWithDelayInjector) Abort() bool {
	return m.abort
}
