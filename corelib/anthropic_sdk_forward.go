package corelib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	anthropicparam "github.com/anthropics/anthropic-sdk-go/packages/param"
)

func forwardAnthropicMessageWithSDK(ctx context.Context, cfg MaclawLLMConfig, anthropicReq map[string]interface{}, client *http.Client) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body := make(map[string]interface{}, len(anthropicReq))
	for k, v := range anthropicReq {
		body[k] = v
	}
	delete(body, "stream")

	var response *http.Response
	sdkClient := anthropic.NewClient(anthropicSDKForwardOptions(cfg, client)...)
	msg, err := sdkClient.Messages.New(
		ctx,
		anthropicparam.Override[anthropic.MessageNewParams](body),
		anthropicopt.WithResponseInto(&response),
	)
	status := http.StatusOK
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		if apiStatus, raw := anthropicSDKForwardErrorStatusAndRaw(err); raw != "" {
			if apiStatus > 0 {
				status = apiStatus
			}
			return []byte(raw), status, err
		}
		return nil, status, err
	}
	var responseBody []byte
	if msg != nil && strings.TrimSpace(msg.RawJSON()) != "" {
		responseBody = []byte(msg.RawJSON())
	}
	return responseBody, status, nil
}

func anthropicSDKForwardOptions(cfg MaclawLLMConfig, client *http.Client) []anthropicopt.RequestOption {
	opts := []anthropicopt.RequestOption{
		anthropicopt.WithBaseURL(anthropicSDKForwardBaseURL(cfg.URL)),
		anthropicopt.WithAPIKey(cfg.Key),
		anthropicopt.WithAuthToken(cfg.Key),
		anthropicopt.WithHTTPClient(client),
		anthropicopt.WithHeader("User-Agent", cfg.UserAgent()),
		anthropicopt.WithMaxRetries(0),
	}
	if IsCodeGenURL(cfg.URL) {
		opts = append(opts, anthropicopt.WithHeader(CodeGenClientNameHeader, NormalizeCodeGenClientName(cfg.UserAgent())))
	}
	if timeout := cfg.EffectiveTimeoutSec(); timeout > 0 {
		opts = append(opts, anthropicopt.WithRequestTimeout(time.Duration(timeout)*time.Second))
	}
	return opts
}

func anthropicSDKForwardBaseURL(raw string) string {
	return AnthropicBaseURL(raw)
}

func anthropicSDKForwardRawJSON(err error) string {
	type rawJSONError interface {
		RawJSON() string
	}
	if err == nil {
		return ""
	}
	if raw, ok := err.(rawJSONError); ok {
		return raw.RawJSON()
	}
	var payload map[string]interface{}
	if json.Unmarshal([]byte(err.Error()), &payload) == nil {
		if raw, marshalErr := json.Marshal(payload); marshalErr == nil {
			return string(raw)
		}
	}
	return ""
}

func anthropicSDKForwardErrorStatusAndRaw(err error) (int, string) {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, strings.TrimSpace(apiErr.RawJSON())
	}
	return 0, strings.TrimSpace(anthropicSDKForwardRawJSON(err))
}

func openAICompatAnthropicUpstreamError(statusCode int, body []byte) ([]byte, int, error) {
	errResp := map[string]interface{}{
		"error": map[string]interface{}{
			"message": openAICompatForwardUpstreamErrorMessage(statusCode, body),
			"type":    "server_error",
		},
	}
	data, err := json.Marshal(errResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal upstream error: %w", err)
	}
	return data, statusCode, nil
}
