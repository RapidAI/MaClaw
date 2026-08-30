package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	semanticTrustedAcquireRemoteAdapter        = "semantic_acquire_trusted_remote"
	semanticTrustedAcquireRemoteImplementation = "trusted-artifact-acquire-v1"
)

// The legacy download_file multiplexer accepted save_path, output, dest, path
// and filename, arbitrary request headers, a cookie, and a via_browser switch
// that borrowed the logged-in managed browser. All of those are host decisions
// or credentials, so the managed family binds them here instead.
func semanticUnpublishedLegacyDownloadProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityArtifactAcquireRemote {
			return true
		}
	}
	return false
}

func semanticTrustedAcquireRemoteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedAcquireRemoteAdapter,
			"description": "Acquire one approved HTTP URL into the workspace as an artifact. Only url is accepted; the destination is bound by the host.",
			"parameters":  semanticTrustedAcquireRemoteInvocationSchema(),
		},
	}
}

func semanticTrustedAcquireRemoteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func semanticTrustedAcquireRemoteArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("trusted_artifact_acquire_arguments_rejected")
	}
	raw, ok := args["url"]
	if !ok {
		return "", fmt.Errorf("trusted_artifact_acquire_arguments_rejected")
	}
	rawURL, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("trusted_artifact_acquire_arguments_rejected")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_url_required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil {
		return "", fmt.Errorf("trusted_artifact_acquire_url_invalid")
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("trusted_artifact_acquire_url_scheme_rejected")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_url_invalid")
	}
	// Userinfo in the URL is a credential the model would be supplying.
	if parsed.User != nil {
		return "", fmt.Errorf("trusted_artifact_acquire_url_credentials")
	}
	return rawURL, nil
}

func (h *IMMessageHandler) acquireTrustedRemote(principalID, rawURL string, publicNetworkOnly bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_artifact_acquire_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_principal_required")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_url_required")
	}
	if h.semanticTrustedArtifactAcquire != nil {
		return h.semanticTrustedArtifactAcquire(principalID, rawURL)
	}
	base := strings.TrimSpace(h.toolDownloadBaseDirForOwner(h.currentRuntimeOrLegacyPolicyOwnerID()))
	if base == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_workspace_unavailable")
	}
	name := downloadFileNameFromURL(rawURL)
	opts := &websearch.FetchOptions{
		SavePath:                   filepath.Join(base, name),
		SaveRoot:                   base,
		TimeoutS:                   corelib.NormalizeAgentTimeoutSec(corelib.DefaultAgentTimeoutSec),
		DisableCookies:             true,
		DisableBrowserAuthFallback: true,
		PublicNetworkOnly:          publicNetworkOnly,
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
	return semanticTrustedAcquireRemoteProjection(result, name), nil
}

// The projection names the artifact by its workspace-relative file name. The
// absolute SavedTo path stays on the host: a managed turn delivers artifacts
// through an ArtifactRef, so a model that learns the host location gains only
// the ability to name paths it was never granted.
func semanticTrustedAcquireRemoteProjection(result *websearch.FetchResult, name string) string {
	var b strings.Builder
	b.WriteString("Acquired remote artifact into the workspace.\n")
	fmt.Fprintf(&b, "Name: %s\n", name)
	if result != nil {
		if contentType := strings.TrimSpace(result.ContentType); contentType != "" {
			fmt.Fprintf(&b, "Type: %s\n", contentType)
		}
		fmt.Fprintf(&b, "Size: %d bytes", result.BytesRead)
	}
	return strings.TrimRight(b.String(), "\n")
}

func semanticTrustedAcquireRemoteResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_artifact_acquire_delivery_token")
	}
	if strings.Contains(text, "download_file") || strings.Contains(text, "web_fetch") ||
		strings.Contains(text, "save_path") || strings.Contains(text, "via_browser") {
		return "", fmt.Errorf("trusted_artifact_acquire_legacy_name")
	}
	if semanticTrustedAcquireRemoteHostPath(text) {
		return "", fmt.Errorf("trusted_artifact_acquire_host_path")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_artifact_acquire_empty")
	}
	return text, nil
}

// semanticTrustedAcquireRemoteHostPath reports whether the projection still
// carries a filesystem location. A drive letter or a rooted path only reaches
// this text if the projection leaked SavedTo or an error echoed the workspace,
// and either one hands the model a location the grant never bound.
func semanticTrustedAcquireRemoteHostPath(text string) bool {
	runes := []rune(text)
	for i, r := range runes {
		if r != ':' && r != '/' && r != '\\' {
			continue
		}
		var boundary bool
		switch r {
		case ':':
			// Windows drive letter: a single letter preceded by a boundary and
			// followed by a separator. "https://" fails the preceding test.
			if i == 0 || i+1 >= len(runes) {
				continue
			}
			if runes[i+1] != '/' && runes[i+1] != '\\' {
				continue
			}
			letter := runes[i-1]
			if !(letter >= 'A' && letter <= 'Z') && !(letter >= 'a' && letter <= 'z') {
				continue
			}
			boundary = i-1 == 0 || isAcquireRemotePathBoundary(runes[i-2])
		case '/', '\\':
			// Rooted POSIX path. In a URL the separator follows the scheme's
			// colon or another separator, so neither is a boundary.
			if i+1 >= len(runes) {
				continue
			}
			if !isAcquireRemotePathSegmentStart(runes[i+1]) {
				continue
			}
			boundary = i == 0 || isAcquireRemotePathBoundary(runes[i-1])
		}
		if boundary {
			return true
		}
	}
	return false
}

func isAcquireRemotePathBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '"', '\'', '(', '[', '=', ',':
		return true
	}
	return false
}

func isAcquireRemotePathSegmentStart(r rune) bool {
	if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
		return true
	}
	switch r {
	case '_', '.', '~':
		return true
	}
	return false
}
