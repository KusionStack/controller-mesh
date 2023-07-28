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

package probe

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

var (
	once           sync.Once
	done           bool
	delayProbePort int
	delayDuration  int
)

func init() {
	flag.IntVar(&delayProbePort, "delay-probe-bind-address", 8083, "The address the delay probe endpoint binds to.")
	flag.IntVar(&delayDuration, "delay-duration", 15, "The lease duration in seconds leader election lock is.")
}

func EnableDelay() {
	mux := http.NewServeMux()
	mux.Handle("/delay", http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		if err := Checker(req); err != nil {
			http.Error(res, fmt.Sprintf("internal server error: %v", err), http.StatusInternalServerError)
		} else {
			fmt.Fprint(res, "ok")
		}
	}))

	server := http.Server{
		Handler: mux,
	}
	// Run the server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", delayProbePort))
	if err != nil {
		klog.Fatalf("Failed to listen on :%d: %v", delayProbePort, err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("Failed to serve HTTP on :%d: %v", delayProbePort, err)
		}
	}()
}

func Checker(_ *http.Request) error {
	go once.Do(func() {
		<-time.After(time.Duration(delayDuration) * time.Second)
		done = true
	})
	if done {
		return nil
	}
	return fmt.Errorf(fmt.Sprintf("wait for delay checker %d seconds", delayDuration))
}
