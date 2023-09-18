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

package apiserver

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	utilwaitgroup "k8s.io/apimachinery/pkg/util/waitgroup"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/rest"

	protomanager "github.com/KusionStack/ctrlmesh/pkg/proxy/proto"
)

const (
	// DefaultLegacyAPIPrefix is where the legacy APIs will be located.
	DefaultLegacyAPIPrefix = "/api"

	// APIGroupPrefix is where non-legacy API group will be located.
	APIGroupPrefix = "/apis"
)

// Options contains everything necessary to create and run proxy.
type Options struct {
	Config               *rest.Config
	SecureServingOptions *options.SecureServingOptions

	RequestInfoResolver    apirequest.RequestInfoResolver
	LegacyAPIGroupPrefixes sets.Set[string]
	LongRunningFunc        apirequest.LongRunningRequestCheck
	HandlerChainWaitGroup  *utilwaitgroup.SafeWaitGroup

	LeaderElectionName string

	SpecManager *protomanager.SpecManager
}

func NewOptions() *Options {
	o := &Options{
		Config:                 new(rest.Config),
		SecureServingOptions:   options.NewSecureServingOptions(),
		LegacyAPIGroupPrefixes: sets.New[string](DefaultLegacyAPIPrefix),
		LongRunningFunc: genericfilters.BasicLongRunningRequestCheck(
			sets.NewString("watch", "proxy"),
			sets.NewString("attach", "exec", "proxy", "log", "portforward"),
		), // BasicLongRunningRequestCheck(sets.NewString("watch"), sets.NewString()),
		HandlerChainWaitGroup: new(utilwaitgroup.SafeWaitGroup),
	}
	o.RequestInfoResolver = NewRequestInfoResolver(o)
	return o
}

func (o *Options) ApplyTo(apiserver **server.SecureServingInfo) error {
	if o == nil {
		return fmt.Errorf("SecureServingInfo is empty")
	}
	err := o.SecureServingOptions.ApplyTo(apiserver)
	if err != nil {
		return err
	}
	return err
}

func (o *Options) Validate() []error {
	errors := []error{}
	errors = append(errors, o.SecureServingOptions.Validate()...)
	return errors
}

func NewRequestInfoResolver(o *Options) *apirequest.RequestInfoFactory {
	apiPrefixes := sets.NewString(strings.Trim(APIGroupPrefix, "/")) // all possible API prefixes
	legacyAPIPrefixes := sets.String{}                               // APIPrefixes that won't have groups (legacy)
	for legacyAPIPrefix := range o.LegacyAPIGroupPrefixes {
		apiPrefixes.Insert(strings.Trim(legacyAPIPrefix, "/"))
		legacyAPIPrefixes.Insert(strings.Trim(legacyAPIPrefix, "/"))
	}

	return &apirequest.RequestInfoFactory{
		APIPrefixes:          apiPrefixes,
		GrouplessAPIPrefixes: legacyAPIPrefixes,
	}
}
