package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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

// semanticAcquireRemoteInvocationArgs washes model-supplied download
// arguments before canonical schema validation, the same boundary role as
// semanticOfficeWriteInvocationArgs. The adapter takes exactly one url, and
// strict admission burns the one-shot grant on correctable shapes: a null
// companion key ({"url": ..., "path": null}), a single URL-valued field
// under a legacy alias ({"link": ...}, {"image": ...}), or a destination-
// shaped decoration like save_path (2026-08-26 PPT turn: the destination is
// host-bound, so save_path can change nothing, yet its presence burned the
// first acquire grant on parameter_schema_invalid). Null values are
// dropped; when no url key survives, exactly one surviving string value that
// parses as an http(s) URL is promoted to url; known destination keys are
// dropped once a url survives. Anything else (multiple URL candidates,
// unknown keys) passes through unchanged so admission still fails closed.
func semanticAcquireRemoteInvocationArgs(argsJSON string) string {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	changed := false
	for key, raw := range parsed {
		if raw == nil {
			delete(parsed, key)
			changed = true
		}
	}
	if _, ok := parsed["url"]; !ok {
		candidate := ""
		candidates := 0
		for _, raw := range parsed {
			text, isString := raw.(string)
			if !isString {
				continue
			}
			if u, err := url.Parse(strings.TrimSpace(text)); err == nil && u != nil && u.Host != "" && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
				candidate = text
				candidates++
			}
		}
		if candidates == 1 {
			for key, raw := range parsed {
				if raw == candidate {
					delete(parsed, key)
				}
			}
			parsed["url"] = candidate
			changed = true
		}
	}
	if _, ok := parsed["url"]; ok {
		for key, raw := range parsed {
			if key == "url" {
				continue
			}
			// A second URL-shaped value is a real ambiguity (mirror? replace?):
			// leave everything for admission to reject.
			if text, isString := raw.(string); isString {
				if u, err := url.Parse(strings.TrimSpace(text)); err == nil && u != nil && u.Host != "" && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
					continue
				}
			}
			// Destination/naming is host-bound, so these keys are decoration
			// by construction; unknown keys stay and fail closed.
			switch strings.ToLower(key) {
			case "save_path", "path", "output", "output_path", "destination", "file_path", "filename", "save_as", "target", "dir", "directory", "name":
				delete(parsed, key)
				changed = true
			}
		}
	}
	if !changed {
		return argsJSON
	}
	body, err := json.Marshal(parsed)
	if err != nil {
		return argsJSON
	}
	return string(body)
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
	if websearch.IsPlaceholderFetchHost(websearch.FetchURLHostname(rawURL)) {
		return "", fmt.Errorf("trusted_artifact_acquire_url_host_rejected")
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

// semanticOptionalURLSelection is download_file / web_fetch (and the host
// adapters that render as those names). Optional on an office face: a listed
// name is not a command to probe a skip URL.
func semanticOptionalURLSelection(selection tool.PlannedSelection, adapterName string) bool {
	switch strings.TrimSpace(adapterName) {
	case semanticTrustedAcquireRemoteAdapter, semanticTrustedWebFetchAdapter:
		return true
	}
	switch selection.FitProof.MatchedCapability {
	case tool.CapabilityArtifactAcquireRemote, tool.CapabilityInformationFetchWeb:
		return true
	}
	return false
}

const semanticPlaceholderRemoteURLReason = " needs a real HTTP(S) URL. Placeholder or reserved hosts (example.invalid, localhost) are not a skip token and do not consume this tool. If you do not need a remote file, skip this tool and continue with the other listed tools."

// semanticPlaceholderRemoteURLIntakeReason is Intake, not Admission. A probe
// like https://example.invalid/skip must not reach Coordinator.Reject and burn
// the acquire grant the office face still needs for a real image URL.
func semanticPlaceholderRemoteURLIntakeReason(name, argsJSON string) string {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return ""
	}
	raw, _ := parsed["url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	host := websearch.FetchURLHostname(raw)
	if host == "" || !websearch.IsPlaceholderFetchHost(host) {
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "download_file"
	}
	return name + semanticPlaceholderRemoteURLReason
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
		return "", semanticTrustedAcquireRemoteError(err)
	}
	name = semanticAcquireNormalizeExtension(base, name, result)
	return semanticTrustedAcquireRemoteProjection(result, name), nil
}

// semanticAcquireNormalizeExtension renames a freshly acquired artifact so it
// carries the extension its Content-Type implies. URL tail segments often
// lack one ("cat", "photo-1514…"), and an extension-less file is hostile to
// every downstream consumer: the model has to guess the suffix (2026-08-27
// birthday-deck turn: "cat" became three image_missing rejections on
// "cat.jpg"), and MIME sniffers that key on the extension misfire. The
// rename is best-effort — a collision or IO error keeps the original name.
func semanticAcquireNormalizeExtension(dir, name string, result *websearch.FetchResult) string {
	if result == nil || strings.TrimSpace(filepath.Ext(name)) != "" {
		return name
	}
	ext := semanticAcquireExtensionForMIME(result.ContentType)
	if ext == "" {
		return name
	}
	renamed := name + ext
	if err := os.Rename(filepath.Join(dir, name), filepath.Join(dir, renamed)); err != nil {
		return name
	}
	return renamed
}

func semanticAcquireExtensionForMIME(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	default:
		return ""
	}
}

// semanticTrustedAcquireRemoteError rewrites host error suggestions that name
// legacy-only parameters (save_path, via_browser) into guidance the managed
// surface can actually act on. Suggestion text must be written against the
// rendered surface (§4.13): the shared downloader's anti-crawl hint tells the
// model to call download_file(url, save_path, via_browser=true) — a shape the
// closed schema rejects — and the 2026-08-27 birthday-deck turn followed it
// straight into parameter_schema_invalid. The managed escalation path is the
// petitionable browser leg plus a same-arguments retry.
func semanticTrustedAcquireRemoteError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if !strings.Contains(text, "via_browser") && !strings.Contains(text, "save_path") {
		return err
	}
	if idx := strings.Index(text, "（目标站点存在反爬验证"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return fmt.Errorf("%s（目标站点存在反爬验证。先请愿 browser 工具打开目标页完成人机验证，然后用相同参数重试 download_file；browser 未列出时直接调用一次即可请愿授权）", text)
}

// The projection names the artifact by its workspace-relative file name. The
// absolute SavedTo path stays on the host: a managed turn delivers artifacts
// through an ArtifactRef, so a model that learns the host location gains only
// the ability to name paths it was never granted. The workspace-relative name
// is NOT withheld — downstream tools resolve against the same workspace (the
// office writer's slide images, file reads), and hiding it forced the model
// to invent a location (2026-08-27 birthday-deck turn: it referenced the
// save_path it had passed, which the host binds and ignores, and the deck
// write failed twice on image_missing).
func semanticTrustedAcquireRemoteProjection(result *websearch.FetchResult, name string) string {
	var b strings.Builder
	b.WriteString("Acquired remote artifact into the workspace.\n")
	fmt.Fprintf(&b, "Name: %s\n", name)
	fmt.Fprintf(&b, "Path: %s (workspace-relative; reference the artifact by this path in later tools such as office slide images)\n", name)
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
