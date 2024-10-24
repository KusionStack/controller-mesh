/*
Copyright 2024 The KusionStack Authors.

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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshrest "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/rest"
)

const (
	DialInterval = 2 * time.Second
	DialTimeOut  = 5 * time.Second
)

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
	}
}

type Client struct {
	httpClient *http.Client
}

func (c *Client) WaitForServerReady(ctx context.Context) error {
	address := fmt.Sprintf("http://127.0.0.1:%d", constants.ProxyMetricsHealthPort)
	for {
		conn, err := net.DialTimeout("tcp", address, DialTimeOut)
		if err == nil {
			conn.Close()
			return nil
		}
		klog.Infof("waiting for server to become ready... DialErr: %s", err.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(DialInterval):
			continue
		}
	}
}

func (c *Client) Sync(ctx context.Context, action ctrlmeshrest.Action, kubeconfigs ...[]byte) error {
	req := &ctrlmeshrest.ConfigRequest{
		Action:      action,
		Kubeconfigs: kubeconfigs,
	}
	byt, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("http://127.0.0.1:%d%s", constants.ProxyRemoteApiServerPort, ctrlmeshrest.RemoteRegisterPath),
		strings.NewReader(string(byt)))
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		klog.Errorf("failed to do http request, %v", err)
		return err
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		klog.Infof("sync kubeconfigs success, Action: %s, StatusCode: %s", action, resp.StatusCode)
		return nil
	}

	var errBody []byte
	errBody, err = io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("failed to read response body, %v", err)
		return err
	}
	return fmt.Errorf("failed by response status code: %d, body: %s", resp.StatusCode, string(errBody))
}
