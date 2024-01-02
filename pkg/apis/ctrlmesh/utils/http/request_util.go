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
