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

package http

import (
	"fmt"
	"strings"
)

func ParseRawQuery(rawQuery string) (map[string]string, error) {
	result := map[string]string{}
	if rawQuery == "" {
		return result, nil
	}

	pairs := strings.Split(rawQuery, "&")
	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			return result, fmt.Errorf("invalid url query: %s", rawQuery)
		}

		result[kv[0]] = kv[1]
	}

	return result, nil
}

func MarshalRawQuery(queryParm map[string]string) string {
	result := ""
	for k, v := range queryParm {
		if len(result) == 0 {
			result = result + fmt.Sprintf("%s=%s", k, v)
		} else {
			result = result + "&" + fmt.Sprintf("%s=%s", k, v)
		}
	}

	return result
}
