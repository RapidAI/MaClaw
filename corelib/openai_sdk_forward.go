package corelib

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
)

func forwardOpenAICompatRequestWithSDK(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	openaiClient := openai.NewClient(openAICompatSDKOptions(cfg, client)...)
	var responseBody []byte
	var response *http.Response
	_, err := openaiClient.Chat.Completions.New(
		ctx,
		param.Override[openai.ChatCompletionNewParams](body),
		option.WithResponseBodyInto(&responseBody),
		option.WithResponseInto(&response),
	)
	statusCode := http.StatusOK
	if response != nil {
		statusCode = response.StatusCode
	}
	if err != nil {
		if apiErr := openAICompatSDKError(err); apiErr != nil {
			statusCode = apiErr.StatusCode
			if len(responseBody) == 0 && strings.TrimSpace(apiErr.RawJSON()) != "" {
				responseBody = []byte(apiErr.RawJSON())
			}
		}
		return responseBody, statusCode, err
	}
	if strings.TrimSpace(responseModel) != "" {
		responseBody = OverrideOpenAIResponseModel(responseBody, responseModel)
	}
	return responseBody, statusCode, nil
}

func openAICompatSDKOptions(cfg MaclawLLMConfig, client *http.Client) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(openAICompatSDKBaseURL(cfg)),
		option.WithAPIKey(cfg.Key),
		option.WithHTTPClient(client),
		option.WithHeader("User-Agent", cfg.UserAgent()),
		option.WithHeader("Cache-Control", "no-cache"),
		option.WithMaxRetries(0),
	}
	if IsCodeGenURL(cfg.URL) {
		opts = append(opts, option.WithHeader(CodeGenClientNameHeader, NormalizeCodeGenClientName(cfg.UserAgent())))
	}
	if timeout := cfg.EffectiveTimeoutSec(); timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(time.Duration(timeout)*time.Second))
	}
	return opts
}

func openAICompatSDKBaseURL(cfg MaclawLLMConfig) string {
	base := NormalizeGLMCodingPlanOpenAIBaseURL(strings.TrimSpace(cfg.URL), cfg.UserAgent())
	base = strings.TrimRight(base, "/")
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "/chat/completions") {
		base = strings.TrimRight(base[:len(base)-len("/chat/completions")], "/")
	}
	if base == "" {
		return "https://api.openai.com/v1"
	}
	if parsed, err := url.Parse(base); err == nil && strings.Trim(parsed.Path, "/") == "" {
		return base + "/v1"
	}
	return base
}

func openAICompatSDKError(err error) *openai.Error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}
