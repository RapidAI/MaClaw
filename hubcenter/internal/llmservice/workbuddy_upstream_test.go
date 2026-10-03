package llmservice

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

func TestForwardWorkBuddyAppliesAccountHeaders(t *testing.T) {
	var seen http.Header
	var body []byte
	var target string
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		target = req.URL.String()
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body = payload
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:                    "wb",
		Name:                  workbuddy.NameGlobal,
		APIURL:                workbuddy.GlobalProfile().ChatURL,
		APIKey:                "access-token",
		Protocol:              "openai",
		AuthKind:              llmpool.ProviderAuthWorkBuddy,
		WorkBuddyEdition:      workbuddy.EditionGlobal,
		WorkBuddyUserID:       "user-1",
		WorkBuddyEnterpriseID: "ent-1",
		WorkBuddyDomain:       "dept",
		WorkBuddyRefreshToken: "refresh-token",
		Models:                []string{"gpt-5.4"},
	}
	resp, err := forwardToProvider(context.Background(), client, provider, map[string]any{
		"model":    "gpt-5.4",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}, "gpt-5.4", "gpt-5.4")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if !strings.Contains(target, "https://www.workbuddy.ai/v2/chat/completions") {
		t.Fatalf("url = %s", target)
	}
	if seen.Get("Authorization") != "Bearer access-token" || seen.Get("X-User-Id") != "user-1" || seen.Get("X-Enterprise-Id") != "ent-1" {
		t.Fatalf("headers = %v", seen)
	}
	if seen.Get("X-Domain") != "dept" || seen.Get("X-Refresh-Token") != "refresh-token" || seen.Get("X-Product") != workbuddy.Product {
		t.Fatalf("account headers = %v", seen)
	}
	if !bytes.Contains(body, []byte(`"stream":true`)) || !bytes.Contains(body, []byte(`"role":"system"`)) {
		t.Fatalf("body = %s", body)
	}
}

func TestForwardTokenBankWorkBuddyRewritesRealClientBody(t *testing.T) {
	var seen http.Header
	var body []byte
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body = payload
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:       "tbk_share__aHkz",
		Name:     "Token Bank · WorkBuddy 国际版 · hy3",
		APIURL:   "https://www.workbuddy.ai/v2",
		APIKey:   "shared-key",
		Protocol: "openai",
		Models:   []string{"hy3"},
	}
	resp, err := forwardToProvider(context.Background(), client, provider, map[string]any{
		"model": "official-mid",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "lookup",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
	}, "hy3", "official-mid")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if seen.Get("Authorization") != "Bearer shared-key" {
		t.Fatalf("headers = %v", seen)
	}
	if seen.Get("X-Product") != "" || seen.Get("X-User-Id") != "" || seen.Get("X-Refresh-Token") != "" ||
		seen.Get("X-No-User-Id") != "" || seen.Get("X-No-Enterprise-Id") != "" || seen.Get("X-No-Department-Info") != "" ||
		seen.Get("User-Agent") == workbuddy.UserAgent {
		t.Fatalf("static key gained account headers: %v", seen)
	}
	if !bytes.Contains(body, []byte(`"stream":true`)) || !bytes.Contains(body, []byte(`"role":"system"`)) {
		t.Fatalf("body = %s", body)
	}
	if !bytes.Contains(body, []byte(`"reasoning_effort":"high"`)) {
		t.Fatalf("hy3 thinking was not forced: %s", body)
	}
	if bytes.Contains(body, []byte(`"tool_choice":{"`)) || !bytes.Contains(body, []byte(`"tool_choice":"required"`)) {
		t.Fatalf("tool_choice = %s", body)
	}
}

func TestForwardTokenBankWorkBuddyKeepsDeepSeekToolChoice(t *testing.T) {
	var seen http.Header
	var body []byte
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body = payload
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:       "tbk_share__deepseek",
		Name:     "Token Bank · WorkBuddy 国际版 · deepseek-v4.1-flash",
		APIURL:   "https://www.workbuddy.ai/v2",
		APIKey:   "shared-key",
		Protocol: "openai",
		Models:   []string{"deepseek-v4.1-flash"},
	}
	resp, err := forwardToProvider(context.Background(), client, provider, map[string]any{
		"model": "official-high",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "lookup",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
	}, "deepseek-v4.1-flash", "official-high")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if seen.Get("X-Product") != "" || seen.Get("X-No-User-Id") != "" {
		t.Fatalf("static key gained account headers: %v", seen)
	}
	if bytes.Contains(body, []byte(`"tool_choice":"required"`)) || bytes.Contains(body, []byte(`"tool_choice":"auto"`)) || !bytes.Contains(body, []byte(`"tool_choice":{"`)) {
		t.Fatalf("deepseek tool_choice = %s", body)
	}
}

func TestForwardTokenBankWorkBuddyKeepsExplicitToolChoiceNone(t *testing.T) {
	var body []byte
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body = payload
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:       "tbk_share__aHkz",
		Name:     "Token Bank · WorkBuddy 国际版 · hy3",
		APIURL:   "https://www.workbuddy.ai/v2",
		APIKey:   "shared-key",
		Protocol: "openai",
		Models:   []string{"hy3"},
	}
	resp, err := forwardToProvider(context.Background(), client, provider, map[string]any{
		"model": "official-mid",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "lookup",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
		"tool_choice": map[string]any{"type": "none"},
	}, "hy3", "official-mid")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if bytes.Contains(body, []byte(`"tool_choice":{"`)) || !bytes.Contains(body, []byte(`"tool_choice":"none"`)) {
		t.Fatalf("tool_choice = %s", body)
	}
}
