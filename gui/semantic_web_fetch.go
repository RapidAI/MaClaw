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
	if websearch.IsPlaceholderFetchHost(websearch.FetchURLHostname(rawURL)) {
		return "", fmt.Errorf("trusted_web_fetch_url_host_rejected")
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
	if !publicNetworkOnly {
		if err := websearch.ApplyHubDownload(opts, h.getWebSearchStrategy()); err != nil {
			return "", err
		}
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
	if strings.HasPrefix(strings.TrimSpace(result.Content), "[二进制") {
		// The fetch layer's binary advisory is written for the legacy surface
		// ("请使用 save_path 参数") — this adapter's schema is closed over url
		// alone, so that advice is literally uncallable here, and it never names
		// the real petitionable capability. Production 2026-08-26 (ragdoll
		// birthday deck): the model fetched three images, could not act on the
		// advisory, concluded "无法直接下载图片" and shipped a text-only PPT
		// while download_file sat unused in the petition whitelist. Replace the
		// legacy advisory with guidance that is actionable on THIS surface.
		fmt.Fprintf(&b, "[binary content: %s, %d bytes — web_fetch extracts text only. To save the file to disk, call download_file with {\"url\": %q}; the destination is host-bound. To place images in a presentation, call office with slides[].images. Native charts go in slides[].charts.]", result.ContentType, result.BytesRead, strings.TrimSpace(result.URL))
	} else {
		b.WriteString(result.Content)
	}
	if result.Truncated {
		b.WriteString("\n...(truncated)")
	}
	return strings.TrimRight(b.String(), "\n")
}

func semanticTrustedWebFetchResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_web_fetch_delivery_token")
	}
	// No tool-name token scan here. Fetched content cannot grant capabilities —
	// the semantic router renders the tool list and gates every call — while a
	// literal scan rejected legitimate pages and even the fetch layer's own
	// binary advisory ("[二进制内容 … 请使用 save_path 参数下载]") with an opaque
	// trusted_web_fetch_legacy_name, burning the fetch grant on a host-made
	// message (production 2026-08-26, ragdoll birthday deck turn).
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_web_fetch_empty")
	}
	return text, nil
}
