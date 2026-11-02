package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/windsurf"
)

// IsDevinProvider reports a Devin Cascade provider. The built-in name is
// "Devin". Token Bank keeps the official API host and changes the display name,
// so the host match is required as well.
func IsDevinProvider(name, apiURL string) bool {
	return windsurf.MatchesProvider(name, apiURL)
}

func devinMatches(provider *llmpool.ProviderConfig) bool {
	if provider == nil {
		return false
	}
	return IsDevinProvider(provider.Name, provider.APIURL)
}

// DevinHTTPClient returns a client that translates OpenAI chat and model
// calls into Devin Cascade calls. Any other name and host is returned as-is.
func DevinHTTPClient(client *http.Client, name, apiURL string) *http.Client {
	if !IsDevinProvider(name, apiURL) {
		return client
	}
	return windsurf.WrapClient(client)
}

func tryForwardDevin(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, _ bool) (*providerForwardResponse, bool, error) {
	if !devinMatches(provider) {
		return nil, false, nil
	}
	model := strings.TrimSpace(upstreamModel)
	if model == "" && len(provider.Models) == 1 {
		model = strings.TrimSpace(provider.Models[0])
	}
	cfg := corelib.MaclawLLMConfig{
		URL:          strings.TrimSpace(provider.APIURL),
		Key:          strings.TrimSpace(provider.APIKey),
		Model:        model,
		Protocol:     "openai",
		ProviderName: provider.Name,
		TimeoutSec:   provider.UpstreamTimeoutSec,
		AgentType:    RequestAgentType(provider.AgentType),
	}
	cfg = WithMemberUpstreamOverrides(cfg, provider)
	client = DevinHTTPClient(client, provider.Name, provider.APIURL)
	// The Cascade transport turns a rejected key, an unknown model, and a
	// transport failure into one finished HTTP response, often 502. The shared
	// retry treats 502 as transient and would repeat that completed call.
	callCtx, cancel := devinForwardContext(ctx, cfg)
	defer cancel()
	fwd := make(map[string]any, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	respBody, statusCode, err := corelib.ForwardOpenAICompatRequest(callCtx, cfg, fwd, client, responseModel)
	if err != nil {
		if statusCode >= 400 && statusCode <= 599 {
			if len(respBody) == 0 {
				respBody = []byte(err.Error())
			}
			return &providerForwardResponse{StatusCode: statusCode, Body: respBody}, true, nil
		}
		return nil, true, fmt.Errorf("forward to %s: %w", provider.ID, err)
	}
	return &providerForwardResponse{StatusCode: statusCode, Body: respBody}, true, nil
}

// devinStreamOpenFailure reads a Cascade open failure. The transport keeps
// HTTP 200 and puts the message in the assistant text so desktop chat can
// show it. The header tells this proxy the body is that failure.
func devinStreamOpenFailure(resp *http.Response) ([]byte, bool) {
	if resp == nil || strings.TrimSpace(resp.Header.Get(windsurf.StreamErrorHeader)) == "" {
		return nil, false
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	text := devinStreamErrorText(raw)
	if text == "" {
		text = strings.TrimSpace(string(raw))
	}
	if text == "" {
		text = "Devin stream failed"
	}
	return []byte(text), true
}

// markedDevinStreamError reports a Cascade failure carried in an SSE chunk
// after the response headers were already sent.
func markedDevinStreamError(payload map[string]any) (string, bool) {
	if payload == nil {
		return "", false
	}
	if _, ok := payload[windsurf.StreamErrorField]; !ok {
		return "", false
	}
	text := strings.TrimSpace(proxyStreamChunkText(payload))
	if text == "" {
		text = "Devin stream failed"
	}
	return text, true
}

func devinStreamErrorText(raw []byte) string {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var payload struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &payload) != nil || len(payload.Choices) == 0 {
			continue
		}
		if text := strings.TrimSpace(payload.Choices[0].Delta.Content); text != "" {
			return text
		}
	}
	return ""
}

func prepareDevinStream(client *http.Client, provider *llmpool.ProviderConfig) *http.Client {
	if !devinMatches(provider) {
		return client
	}
	return DevinHTTPClient(client, provider.Name, provider.APIURL)
}

// devinForwardContext keeps the deadline the shared retry wrapper would have
// installed when the caller did not set one.
func devinForwardContext(ctx context.Context, cfg corelib.MaclawLLMConfig) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(cfg.EffectiveTimeoutSec())*time.Second)
}
