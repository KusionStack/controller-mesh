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
	"fmt"
	"os"
	"testing"

	"github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

func init() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestRestTrafficIntercept(t *testing.T) {
	fmt.Println("TestRestTrafficIntercept")

}

func TestIsEffectiveTimeRange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var tests = []struct {
		timeRange *ctrlmeshproto.EffectiveTimeRange
		want      bool
	}{
		{&ctrlmeshproto.EffectiveTimeRange{
			StartTime:   "00:00:00",
			EndTime:     "23:00:00",
			DaysOfWeek:  []int32{1, 3, 5},
			DaysOfMonth: []int32{1, 2, 3, 15},
			Months:      []int32{1, 4, 7, 10},
		}, true},
		{&ctrlmeshproto.EffectiveTimeRange{
			StartTime:   "00:00:00",
			EndTime:     "23:59:59",
			DaysOfWeek:  []int32{1, 2, 3, 4, 5, 7},
			DaysOfMonth: []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
			Months:      []int32{2, 3, 4},
		}, false},
	}
	for _, tt := range tests {
		got := isEffectiveTimeRange(tt.timeRange)
		g.Expect(got).Should(gomega.BeEquivalentTo(tt.want))
	}
}
