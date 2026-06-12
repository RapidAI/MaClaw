package codegenproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
)

// OpenAIUpstreamClient sends requests to the OpenAI-compatible CodeGen
// upstream. It is intentionally raw-response shaped because codegenproxy owns
// Anthropic/OpenAI protocol conversion and SSE translation.
type OpenAIUpstreamClient interface {
	DoModels(ctx context.Context, endpoint, apiKey, clientName string) (*http.Response, error)
	DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error)
}

type httpOpenAIUpstreamClient struct {
	client *http.Client
}

func NewHTTPOpenAIUpstreamClient(client *http.Client) OpenAIUpstreamClient {
	if client == nil {
		client = http.DefaultClient
	}
	return httpOpenAIUpstreamClient{client: client}
}

func (c httpOpenAIUpstreamClient) DoModels(ctx context.Context, endpoint, apiKey, clientName string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(req, clientName)
	return c.client.Do(req)
}

func (c httpOpenAIUpstreamClient) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, _ bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(req, clientName)
	return c.client.Do(req)
}

type openAISDKUpstreamClient struct {
	httpClient *http.Client
	fallback   OpenAIUpstreamClient
}

// NewOpenAISDKUpstreamClient creates an upstream client backed by openai-go
// for non-streaming chat completions. Streaming and models stay on raw HTTP so
// existing SSE and models compatibility behavior remains byte-for-byte owned by
// the proxy.
func NewOpenAISDKUpstreamClient(client *http.Client) OpenAIUpstreamClient {
	if client == nil {
		client = http.DefaultClient
	}
	return openAISDKUpstreamClient{
		httpClient: client,
		fallback:   NewHTTPOpenAIUpstreamClient(client),
	}
}

func (c openAISDKUpstreamClient) DoModels(ctx context.Context, endpoint, apiKey, clientName string) (*http.Response, error) {
	return c.fallback.DoModels(ctx, endpoint, apiKey, clientName)
}

func (c openAISDKUpstreamClient) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error) {
	if stream {
		return c.fallback.DoChatCompletions(ctx, endpoint, apiKey, clientName, body, accept, stream)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var responseBody []byte
	var response *http.Response
	client := openai.NewClient(
		option.WithBaseURL(openAISDKBaseURLFromChatEndpoint(endpoint)),
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(c.httpClient),
		option.WithHeader("User-Agent", corelib.NormalizeCodeGenClientName(clientName)),
		option.WithHeader(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(clientName)),
		option.WithHeader("Cache-Control", "no-cache"),
		option.WithMaxRetries(0),
	)
	opts := []option.RequestOption{
		option.WithResponseBodyInto(&responseBody),
		option.WithResponseInto(&response),
	}
	if strings.TrimSpace(accept) != "" {
		opts = append(opts, option.WithHeader("Accept", accept))
	}
	_, err := client.Chat.Completions.New(ctx, param.Override[openai.ChatCompletionNewParams](payload), opts...)
	if response == nil {
		response = &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	}
	if err != nil {
		if apiErr := openAISDKError(err); apiErr != nil {
			response.StatusCode = apiErr.StatusCode
			if len(responseBody) == 0 && strings.TrimSpace(apiErr.RawJSON()) != "" {
				responseBody = []byte(apiErr.RawJSON())
			}
			response.Body = io.NopCloser(bytes.NewReader(responseBody))
			return response, nil
		}
		response.Body = io.NopCloser(bytes.NewReader(responseBody))
		return response, err
	}
	response.Body = io.NopCloser(bytes.NewReader(responseBody))
	return response, nil
}

func openAISDKBaseURLFromChatEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil {
		return strings.TrimRight(strings.TrimSuffix(endpoint, "/chat/completions"), "/")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(path), "/chat/completions") {
		parsed.Path = path[:len(path)-len("/chat/completions")]
	}
	return strings.TrimRight(parsed.String(), "/")
}

func openAISDKError(err error) *openai.Error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}
