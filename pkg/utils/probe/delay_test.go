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
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestDelay(t *testing.T) {
	EnableDelay()
	cli := http.Client{}
	timeOut := time.Now().Add(20 * time.Second)
	fmt.Println(timeOut.String())
	for {
		req, _ := http.NewRequest("GET", "http://localhost:8083/delay", nil)
		res, err := cli.Do(req)
		if err != nil {
			fmt.Println(err)
			break
		} else {
			///../res.
			body, _ := ioutil.ReadAll(res.Body)
			fmt.Println(string(body))
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				break
			}
		}
		if time.Now().After(timeOut) {
			fmt.Println("timeout")
			break
		}
		<-time.After(1 * time.Second)
	}
	Expect(time.Now().After(timeOut)).NotTo(BeTrue())
}
