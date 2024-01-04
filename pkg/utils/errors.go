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

package utils

import (
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HashChangedError struct {
	oldHash string
	newHash string
}

func (e HashChangedError) Error() string {
	return e.String()
}

func (e HashChangedError) String() string {
	return fmt.Sprintf("hash changed from %s to %s", e.oldHash, e.newHash)
}

func IsHashChangedError(err error) bool {
	switch err.(type) {
	case HashChangedError:
		return true
	}
	return false
}

// HttpToAPIError convert http error to kubernetes api error
func HttpToAPIError(code int, verb, serverMessage string) *metav1.Status {
	status := &metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    int32(code),
		Reason:  metav1.StatusReason(fmt.Sprintf("HTTP %d", code)),
		Message: serverMessage,
	}
	var (
		reason  metav1.StatusReason
		message string
	)
	switch code {
	case http.StatusOK:
		reason = ""
		message = "code is 200"
		status.Status = metav1.StatusSuccess
	case http.StatusConflict:
		if verb == "POST" {
			reason = metav1.StatusReasonAlreadyExists
		} else {
			reason = metav1.StatusReasonConflict
		}
		message = "the server reported a conflict"
	case http.StatusNotFound:
		reason = metav1.StatusReasonNotFound
		message = "the server could not find the requested resource"
	case http.StatusBadRequest:
		reason = metav1.StatusReasonBadRequest
		message = "the server rejected our request for an unknown reason"
	case http.StatusUnauthorized:
		reason = metav1.StatusReasonUnauthorized
		message = "the server has asked for the client to provide credentials"
	case http.StatusForbidden:
		reason = metav1.StatusReasonForbidden
		// the server message has details about who is trying to perform what action.  Keep its message.
		message = serverMessage
	case http.StatusNotAcceptable:
		reason = metav1.StatusReasonNotAcceptable
		// the server message has details about what types are acceptable
		if len(serverMessage) == 0 || serverMessage == "unknown" {
			message = "the server was unable to respond with a content type that the client supports"
		} else {
			message = serverMessage
		}
	case http.StatusUnsupportedMediaType:
		reason = metav1.StatusReasonUnsupportedMediaType
		// the server message has details about what types are acceptable
		message = serverMessage
	case http.StatusMethodNotAllowed:
		reason = metav1.StatusReasonMethodNotAllowed
		message = "the server does not allow this method on the requested resource"
	case http.StatusUnprocessableEntity:
		reason = metav1.StatusReasonInvalid
		message = "the server rejected our request due to an error in our request"
	case http.StatusServiceUnavailable:
		reason = metav1.StatusReasonServiceUnavailable
		message = "the server is currently unable to handle the request"
	case http.StatusGatewayTimeout:
		reason = metav1.StatusReasonTimeout
		message = "the server was unable to return a response in the time allotted, but may still be processing the request"
	case http.StatusTooManyRequests:
		reason = metav1.StatusReasonTooManyRequests
		message = "the server has received too many requests and has asked us to try again later"
	default:
		if code >= 500 {
			reason = metav1.StatusReasonInternalError
			message = fmt.Sprintf("an error on the server (%q) has prevented the request from succeeding", serverMessage)
		}
	}
	status.Reason = reason
	status.Message = fmt.Sprintf("FaultInjected, [%s]", message)
	return status
}
