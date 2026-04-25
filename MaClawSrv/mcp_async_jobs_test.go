package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAsyncMCPCreateJobLifecycle(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo Async Create","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers?async=true", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async create MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async create job: %v", err)
	}
	if job.Kind != "mcp.create" || job.ID == "" {
		t.Fatalf("unexpected async create job: %#v", job)
	}

	final := waitForAsyncMCPJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusSucceeded {
		t.Fatalf("expected succeeded mcp create job, got %#v", final)
	}
	var result agentservice.MCPServerView
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("decode async mcp create result: %v", err)
	}
	if result.ID == "" || result.Name != "Local Echo Async Create" || result.Kind != "local" {
		t.Fatalf("unexpected async mcp create result: %#v", result)
	}
}

func TestAsyncMCPUpdateJobLifecycle(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo Update","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode MCP server: %v", err)
	}

	updateBody := bytes.NewBufferString(`{"name":"Local Echo Updated","disabled":true}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/mcp/servers/"+created.ID+"?async=true", updateBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async update MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async update job: %v", err)
	}
	if job.Kind != "mcp.update" || job.ID == "" {
		t.Fatalf("unexpected async update job: %#v", job)
	}

	final := waitForAsyncMCPJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusSucceeded {
		t.Fatalf("expected succeeded mcp update job, got %#v", final)
	}
	var result agentservice.MCPServerView
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("decode async mcp update result: %v", err)
	}
	if result.Name != "Local Echo Updated" || !result.Disabled {
		t.Fatalf("unexpected async mcp update result: %#v", result)
	}
}

func TestAsyncMCPStartJobLifecycle(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode local MCP server: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/start?async=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async start MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async start job: %v", err)
	}
	if job.Kind != "mcp.start" || job.ID == "" {
		t.Fatalf("unexpected async start job: %#v", job)
	}

	final := waitForAsyncMCPJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusSucceeded {
		t.Fatalf("expected succeeded mcp start job, got %#v", final)
	}
	var result agentservice.MCPServerView
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("decode async mcp start result: %v", err)
	}
	if !result.Running || result.HealthStatus != "running" {
		t.Fatalf("unexpected async mcp start result: %#v", result)
	}
}

func TestAsyncMCPHealthCheckJobLifecycle(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode local MCP server: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start MCP status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/health-check?async=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async health-check MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async health-check job: %v", err)
	}
	if job.Kind != "mcp.health_check" || job.ID == "" {
		t.Fatalf("unexpected async health-check job: %#v", job)
	}

	final := waitForAsyncMCPJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusSucceeded {
		t.Fatalf("expected succeeded mcp health-check job, got %#v", final)
	}
	var result agentservice.MCPServerView
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("decode async mcp health-check result: %v", err)
	}
	if !result.Running || result.HealthStatus != "running" {
		t.Fatalf("unexpected async mcp health-check result: %#v", result)
	}
}

func waitForAsyncMCPJob(t *testing.T, server *HTTPServer, token, jobID string) asyncJobView {
	t.Helper()
	return waitForAsyncJob(t, server, token, jobID)
}
