package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cloudWorkspaceEntitlementPath      = "/api/v1/cloud-workspaces/entitlement"
	cloudWorkspaceResponseMaxSize      = 3 << 20
	cloudWorkspaceRequestTimeout       = 30 * time.Second
	cloudWorkspaceHubUnavailableBanner = "Hub 不可用，云端工作区暂不可用"
)

// cloudWorkspaceHubRequest calls a Hub cloud-workspace API with the same
// Bearer + X-Machine-ID headers as virtualRepositorySyncRequest.
func (a *App) cloudWorkspaceHubRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	hubURL, token, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		return nil, 0, err
	}
	var reader io.Reader
	if body != nil {
		raw, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, 0, marshalErr
		}
		reader = bytes.NewReader(raw)
	}
	url := strings.TrimRight(hubURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	timeout := cloudWorkspaceRequestTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining <= 0 {
			return nil, 0, ctx.Err()
		} else if remaining < timeout {
			timeout = remaining
		}
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, cloudWorkspaceResponseMaxSize+1))
	if readErr != nil {
		return data, resp.StatusCode, fmt.Errorf("read Hub response: %w", readErr)
	}
	if len(data) > cloudWorkspaceResponseMaxSize {
		return nil, resp.StatusCode, fmt.Errorf("Hub response exceeds %d byte limit", cloudWorkspaceResponseMaxSize)
	}
	return data, resp.StatusCode, nil
}

func (a *App) cloudWorkspaceRequestContext() (context.Context, context.CancelFunc) {
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	return context.WithTimeout(parent, cloudWorkspaceRequestTimeout)
}
