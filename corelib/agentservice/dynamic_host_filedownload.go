package agentservice

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	reviewedHostFileDownloadProviderID     = "core-filedownload"
	reviewedHostFileDownloadImplementation = "local"
	reviewedHostFileDownloadAdapterName    = "host_artifact_acquire_remote"
	reviewedHostFileDownloadDefaultName    = "download.bin"
	reviewedHostFileDownloadMaxNameRunes   = 200
)

type reviewedHostFileDownloader interface {
	AcquireReviewedHostRemoteArtifact(ctx context.Context, principal Principal, rawURL string) (string, error)
}

type reviewedHostURLDownloader func(ctx context.Context, rawURL, dest, root string) (int, error)

func reviewedHostFileDownloadInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func reviewedHostFileDownloadContractDigest() string {
	return coretool.SchemaDigest([]byte("artifact.acquire.remote:v1:host-filedownload-url"))
}

// ProjectReviewedHostFileDownloadProvider projects the host-owned workspace
// download. It is not a Skill/MCP discovery entry and must not import GUI
// download_file / web_fetch / wget / curl. The closed schema accepts url
// only. Path, save_path, channel, and destination stay out. This is not a
// send and not information.fetch.web.
func ProjectReviewedHostFileDownloadProvider(downloader reviewedHostFileDownloader) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if downloader == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host file downloader is unavailable")
	}
	parameters := reviewedHostFileDownloadInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host file download schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostFileDownloadContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-filedownload-url-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostFileDownloadAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostFileDownloadProviderID,
			ImplementationID: reviewedHostFileDownloadImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityFileDownload,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostFileDownload(downloader)}, nil
}

func AttachReviewedHostFileDownloadProvider(catalog DynamicSemanticCatalog, downloader reviewedHostFileDownloader) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostFileDownloadProvider(downloader)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostFileDownload(downloader reviewedHostFileDownloader) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if downloader == nil {
			return "", fmt.Errorf("host_file_download_unavailable")
		}
		rawURL, err := reviewedHostFileDownloadArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return downloader.AcquireReviewedHostRemoteArtifact(ctx, principal, rawURL)
	}
}

func reviewedHostFileDownloadArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_file_download_arguments_rejected")
	}
	raw, ok := args["url"]
	if !ok {
		return "", fmt.Errorf("host_file_download_arguments_rejected")
	}
	rawURL, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_file_download_arguments_rejected")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("host_file_download_url_required")
	}
	if strings.Contains(rawURL, "[file_base64") || strings.Contains(rawURL, "[voice_base64") {
		return "", fmt.Errorf("host_file_download_delivery_bypass")
	}
	if _, err := reviewedHostDownloadFileName(rawURL); err != nil {
		return "", err
	}
	return rawURL, nil
}

func reviewedHostDownloadFileName(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("host_file_download_url_rejected")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("host_file_download_url_rejected")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("host_file_download_url_rejected")
	}
	if websearch.IsBlockedPublicHost(websearch.FetchURLHostname(rawURL)) {
		return "", fmt.Errorf("host_file_download_url_rejected")
	}
	name := path.Base(parsed.EscapedPath())
	if unescaped, unescapeErr := url.PathUnescape(name); unescapeErr == nil {
		name = unescaped
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) || name == "/" || name == "\\" {
		name = reviewedHostFileDownloadDefaultName
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("host_file_download_name_rejected")
	}
	if strings.Contains(name, "[file_base64") || strings.Contains(name, "[voice_base64") {
		return "", fmt.Errorf("host_file_download_delivery_bypass")
	}
	if len([]rune(name)) > reviewedHostFileDownloadMaxNameRunes {
		return "", fmt.Errorf("host_file_download_name_rejected")
	}
	return name, nil
}

func reviewedHostDownloadRemoteURL(ctx context.Context, rawURL, dest, root string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := websearch.FetchCtx(ctx, rawURL, &websearch.FetchOptions{
		SavePath:          dest,
		SaveRoot:          root,
		PublicNetworkOnly: true,
		TimeoutS:          60,
	})
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, fmt.Errorf("host_file_download_unavailable")
	}
	if result.BytesRead > 0 {
		return result.BytesRead, nil
	}
	info, statErr := os.Stat(dest)
	if statErr != nil || info.IsDir() {
		return 0, fmt.Errorf("host_file_download_unavailable")
	}
	return int(info.Size()), nil
}

func (c *coreAgentCallbacks) AcquireReviewedHostRemoteArtifact(ctx context.Context, principal Principal, rawURL string) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_file_download_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_file_download_principal_mismatch")
	}
	rawURL = strings.TrimSpace(rawURL)
	name, err := reviewedHostDownloadFileName(rawURL)
	if err != nil {
		return "", err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	absPath, err := c.resolveWorkspacePath(name)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("host_file_download_path_is_directory")
	}
	downloader := c.reviewedHostURLDownloader
	if downloader == nil {
		downloader = reviewedHostDownloadRemoteURL
	}
	size, err := downloader(ctx, rawURL, absPath, strings.TrimSpace(c.workspace))
	if err != nil {
		return "", err
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, name)
	return fmt.Sprintf("Downloaded %s (%d bytes). This is not a send.", display, size), nil
}
