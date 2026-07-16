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

// surveyHubClient talks to Hub /api/v1/surveys/* using machine credentials.
type surveyHubClient struct {
	baseURL    string
	machineID  string
	token      string
	httpClient *http.Client
}

func (a *App) newSurveyHubClient() (*surveyHubClient, error) {
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
		machineID = strings.TrimSpace(cfg.RemoteMachineID)
	}
	if machineID == "" {
		return nil, fmt.Errorf("machine id missing; register to Hub first")
	}
	return &surveyHubClient{
		baseURL:   hubURL,
		machineID: machineID,
		token:     token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (c *surveyHubClient) do(ctx context.Context, method, path string, body any, out any) error {
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

func (c *surveyHubClient) List(ctx context.Context, status string) (json.RawMessage, error) {
	path := "/api/v1/surveys"
	if s := strings.TrimSpace(status); s != "" && s != "all" {
		// Whitelist expected values client-side; Hub also validates.
		switch s {
		case "draft", "published", "closed", "archived":
			path += "?status=" + url.QueryEscape(s)
		default:
			return nil, fmt.Errorf("invalid status filter")
		}
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, path, nil, &raw)
	return raw, err
}

func (c *surveyHubClient) Get(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/v1/surveys/"+id, nil, &raw)
	return raw, err
}

func (c *surveyHubClient) Create(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys", payload, &raw)
	return raw, err
}

func (c *surveyHubClient) Publish(ctx context.Context, id string, body json.RawMessage) (json.RawMessage, error) {
	var payload any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/publish", payload, &raw)
	return raw, err
}

func (c *surveyHubClient) Close(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/close", map[string]any{}, &raw)
	return raw, err
}

func (c *surveyHubClient) Reopen(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/reopen", map[string]any{}, &raw)
	return raw, err
}

func (c *surveyHubClient) Archive(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/archive", map[string]any{}, &raw)
	return raw, err
}

func (c *surveyHubClient) Duplicate(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/duplicate", map[string]any{}, &raw)
	return raw, err
}

func (c *surveyHubClient) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/surveys/"+id, nil, nil)
}

func (c *surveyHubClient) Update(ctx context.Context, id string, body json.RawMessage) (json.RawMessage, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPatch, "/api/v1/surveys/"+id, payload, &raw)
	return raw, err
}

func (c *surveyHubClient) Bind(ctx context.Context, id string, body json.RawMessage) (json.RawMessage, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/"+id+"/bindings", payload, &raw)
	return raw, err
}

func (c *surveyHubClient) Unbind(ctx context.Context, id, platform, groupID string) error {
	path := fmt.Sprintf(
		"/api/v1/surveys/%s/bindings/%s/%s",
		url.PathEscape(id),
		url.PathEscape(platform),
		url.PathEscape(groupID),
	)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *surveyHubClient) Stats(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/v1/surveys/"+id+"/stats", nil, &raw)
	return raw, err
}

func (c *surveyHubClient) Responses(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/v1/surveys/"+id+"/responses", nil, &raw)
	return raw, err
}

func (c *surveyHubClient) IMHandle(ctx context.Context, body map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/v1/surveys/im/handle", body, &out)
	return out, err
}
