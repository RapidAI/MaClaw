package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	semanticTrustedWebFetchAdapter        = "semantic_fetch_trusted_web"
	semanticTrustedWebFetchImplementation = "trusted-web-fetch-v1"
	semanticTrustedWebFetchMaxChars       = 16384
	semanticTrustedWebFetchMaxBytes       = 2 * 1024 * 1024
)

func semanticUnpublishedLegacyWebFetchProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityInformationFetchWeb {
			return true
		}
	}
	return false
}

func semanticTrustedWebFetchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedWebFetchAdapter,
			"description": "Fetch one approved HTTP URL and extract page text. Only url is accepted.",
			"parameters":  semanticTrustedWebFetchInvocationSchema(),
		},
	}
}

func semanticTrustedWebFetchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func semanticTrustedWebFetchArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("trusted_web_fetch_arguments_rejected")
	}
	raw, ok := args["url"]
	if !ok {
		return "", fmt.Errorf("trusted_web_fetch_arguments_rejected")
	}
	rawURL, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("trusted_web_fetch_arguments_rejected")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("trusted_web_fetch_url_required")
	}
	return rawURL, nil
}

func (h *IMMessageHandler) fetchTrustedWeb(principalID, rawURL string, publicNetworkOnly bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_web_fetch_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_web_fetch_principal_required")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("trusted_web_fetch_url_required")
	}
	if h.semanticTrustedWebFetch != nil {
		return h.semanticTrustedWebFetch(principalID, rawURL)
	}
	opts := &websearch.FetchOptions{
		Offset:            0,
		MaxChars:          semanticTrustedWebFetchMaxChars,
		MaxBytes:          semanticTrustedWebFetchMaxBytes,
		TimeoutS:          corelib.NormalizeAgentTimeoutSec(corelib.DefaultAgentTimeoutSec),
		PublicNetworkOnly: publicNetworkOnly,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.TimeoutS)*time.Second)
	defer cancel()
	result, err := websearch.FetchCtx(ctx, rawURL, opts)
	if err != nil {
		return "", err
	}
	return semanticTrustedWebFetchProjection(result), nil
}

func semanticTrustedWebFetchProjection(result *websearch.FetchResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Fetched web evidence.\n")
	if title := strings.TrimSpace(result.Title); title != "" {
		fmt.Fprintf(&b, "Title: %s\n", title)
	}
	if pageURL := strings.TrimSpace(result.URL); pageURL != "" {
		fmt.Fprintf(&b, "URL: %s\n", pageURL)
	}
	fmt.Fprintf(&b, "Type: %s | Size: %d bytes\n\n", result.ContentType, result.BytesRead)
	b.WriteString(result.Content)
	if result.Truncated {
		b.WriteString("\n...(truncated)")
	}
	return strings.TrimRight(b.String(), "\n")
}

func semanticTrustedWebFetchResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_web_fetch_delivery_token")
	}
	if strings.Contains(text, "web_fetch") || strings.Contains(text, "download_file") || strings.Contains(text, "save_path") {
		return "", fmt.Errorf("trusted_web_fetch_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_web_fetch_empty")
	}
	return text, nil
}
