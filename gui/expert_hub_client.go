package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// expertHubClient talks to Hub /api/v1/experts/* using machine credentials.
// Mirrors surveyHubClient; experts sync is best-effort (local store wins).
type expertHubClient struct {
	baseURL    string
	machineID  string
	token      string
	httpClient *http.Client
}

func (a *App) newExpertHubClient() (*expertHubClient, error) {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return nil, err
	}
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Hub is not connected")
	}
	machineID := ""
	if cfg, err := a.LoadConfig(); err == nil {
		machineID = firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
	}
	if machineID == "" {
		return nil, fmt.Errorf("machine id missing; register to Hub first")
	}
	return &expertHubClient{
		baseURL:   hubURL,
		machineID: machineID,
		token:     token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *expertHubClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Machine-ID", c.machineID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var errBody struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		_ = json.Unmarshal(data, &errBody)
		msg := errBody.Message
		if msg == "" {
			msg = string(data)
		}
		return fmt.Errorf("hub %s: %s", resp.Status, msg)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *expertHubClient) expertPath(id string) string {
	return "/api/v1/experts/" + url.PathEscape(strings.TrimSpace(id))
}

// List returns all experts visible to this machine's tenant. The Hub response
// may be a bare array or a wrapped object; both are accepted.
func (c *expertHubClient) List(ctx context.Context) ([]ExpertDefinition, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/experts", nil, &raw); err != nil {
		return nil, err
	}
	return parseExpertListJSON(raw)
}

// parseExpertListJSON tolerates both `[...]` and `{"experts":[...]}` shapes.
func parseExpertListJSON(raw json.RawMessage) ([]ExpertDefinition, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var arr []ExpertDefinition
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var wrapped struct {
		Experts []ExpertDefinition `json:"experts"`
		Items   []ExpertDefinition `json:"items"`
		Data    []ExpertDefinition `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("parse hub expert list: %w", err)
	}
	switch {
	case wrapped.Experts != nil:
		return wrapped.Experts, nil
	case wrapped.Items != nil:
		return wrapped.Items, nil
	default:
		return wrapped.Data, nil
	}
}

// expertHubUpsertResult is the Hub's LWW decision and canonical expert payload.
// Applied=false means the request was accepted but an existing newer value (or
// a tombstone) won, so callers must not treat it as a successful local upload.
type expertHubUpsertResult struct {
	ExpertDefinition
	Applied *bool `json:"applied"`
}

func (r expertHubUpsertResult) applied() bool {
	// Older Hubs returned only the expert body. Treat that successful response
	// as applied for backwards compatibility; current Hubs send applied=false
	// whenever their LWW/tombstone decision rejected the write.
	return r.Applied == nil || *r.Applied
}

// Upsert creates or replaces an expert; the client-provided id is authoritative.
func (c *expertHubClient) Upsert(ctx context.Context, body json.RawMessage) (expertHubUpsertResult, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return expertHubUpsertResult{}, err
	}
	var result expertHubUpsertResult
	err := c.do(ctx, http.MethodPost, "/api/v1/experts", payload, &result)
	return result, err
}

func (c *expertHubClient) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.expertPath(id), nil, nil)
}
