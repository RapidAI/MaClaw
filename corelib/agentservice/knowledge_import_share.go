package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// shareImportHTTPClient is a shared HTTP client for Hub knowledge share operations.
// Uses TLS-skip transport because Hub servers commonly use self-signed certificates.
var shareImportHTTPClient = remote.NewHubHTTPClient()

// executeKnowledgeImportShare handles the knowledge_import_share tool call
// by resolving the share link, fetching the package, and importing it.
// This is the in-process equivalent of MaClawSrv's handleKnowledgeImportShare HTTP handler.
func (c *coreAgentCallbacks) executeKnowledgeImportShare(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}

	shareLink := firstStringArg(args, "share_link", "link", "url")
	knowledgeID := firstStringArg(args, "knowledge_id", "id")
	hubURL := stringArg(args, "hub_url")
	hubToken := stringArg(args, "hub_token")

	if shareLink == "" && knowledgeID == "" {
		return "Error: knowledge_id or share_link is required"
	}

	// Fall back to configured Hub credentials when not explicitly provided.
	if hubURL == "" {
		hubURL = c.appCfg.RemoteHubURL
	}
	if hubToken == "" {
		hubToken = c.appCfg.RemoteViewerToken
	}

	// Resolve the API URL from the share link or knowledge_id + hub_url.
	apiURL, resolvedKnowledgeID, err := resolveShareAPIURL(shareLink, knowledgeID, hubURL)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// Use separate timeouts: metadata fetch is fast, package download may be slow.
	ctx, cancel := context.WithTimeout(c.parentContext(), 60*time.Second)
	defer cancel()

	authHeader := buildShareAuthHeader(hubToken)

	// Step 1: Fetch share metadata.
	shareMetadata, err := fetchShareMetadata(ctx, apiURL, authHeader)
	if err != nil {
		return fmt.Sprintf("Error: failed to fetch share metadata: %v", err)
	}

	// Step 2: Resolve package URL from metadata.
	packageURL := resolveSharePackageURL(apiURL, shareMetadata)
	if packageURL == "" {
		// No package URL — return metadata so the agent can inform the user.
		metaJSON, _ := json.Marshal(map[string]interface{}{
			"status":       "resolved_metadata_only",
			"knowledge_id": resolvedKnowledgeID,
			"api_url":      apiURL,
			"share":        shareMetadata,
			"note":         "Share metadata resolved but package_url is not available. The share may not have downloadable content.",
		})
		return string(metaJSON)
	}

	// Step 3: Download the package.
	pkg, err := downloadSharePackage(ctx, packageURL, authHeader)
	if err != nil {
		return fmt.Sprintf("Error: failed to download knowledge package: %v", err)
	}

	// Step 4: Validate.
	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return "Error: unsupported knowledge package format"
	}
	if len(pkg.Sources) == 0 {
		return "Error: package has no sources"
	}

	// Step 5: Import into local knowledge store.
	importResult := knowledge.ImportPackageSources(ctx, c.knowledgeStore, convertPkgSources(pkg.Sources), knowledge.PackageImportOptions{
		OwnerID:  c.principal.UserID,
		TenantID: c.principal.TenantID,
	})

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"status":       "imported",
		"knowledge_id": resolvedKnowledgeID,
		"package_id":   pkg.Manifest.PackageID,
		"title":        pkg.Manifest.Title,
		"imported":     importResult.Imported,
		"skipped":      importResult.Skipped,
		"total":        importResult.Total,
		"warnings":     importResult.Warnings,
	})
	return string(resultJSON)
}

// executeKnowledgeImportPackage handles the knowledge_import_package tool call
// by parsing the JSON package payload and importing it directly.
func (c *coreAgentCallbacks) executeKnowledgeImportPackage(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}

	// The package JSON can be provided as:
	// 1. package_json (string): raw JSON text
	// 2. package (object): nested JSON object from LLM tool call
	// 3. package_path (string): path to a local .json file
	var pkg sharePackage
	if rawJSON := stringArg(args, "package_json"); rawJSON != "" {
		if err := json.Unmarshal([]byte(rawJSON), &pkg); err != nil {
			return fmt.Sprintf("Error: failed to parse package_json: %v", err)
		}
	} else if rawObj, ok := args["package"]; ok {
		// Try to marshal and re-parse the nested object.
		b, err := json.Marshal(rawObj)
		if err != nil {
			return fmt.Sprintf("Error: failed to serialize package object: %v", err)
		}
		if err := json.Unmarshal(b, &pkg); err != nil {
			return fmt.Sprintf("Error: failed to parse package object: %v", err)
		}
	} else if pkgPath := firstStringArg(args, "package_path", "path"); pkgPath != "" {
		resolvedPath, pathErr := c.resolveWorkspacePath(pkgPath)
		if pathErr != nil {
			return fmt.Sprintf("Error: package_path rejected: %v", pathErr)
		}
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return fmt.Sprintf("Error: failed to read package file %q: %v", resolvedPath, err)
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return fmt.Sprintf("Error: failed to parse package file: %v", err)
		}
	} else {
		return "Error: package_json, package, or package_path parameter is required"
	}

	if strings.TrimSpace(pkg.Manifest.Format) != "maclaw.knowledge.package" {
		return "Error: unsupported knowledge package format"
	}
	if len(pkg.Sources) == 0 {
		return "Error: package has no sources"
	}

	ctx, cancel := context.WithTimeout(c.parentContext(), 60*time.Second)
	defer cancel()

	importResult := knowledge.ImportPackageSources(ctx, c.knowledgeStore, convertPkgSources(pkg.Sources), knowledge.PackageImportOptions{
		OwnerID:  c.principal.UserID,
		TenantID: c.principal.TenantID,
	})

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"status":     "imported",
		"package_id": pkg.Manifest.PackageID,
		"title":      pkg.Manifest.Title,
		"imported":   importResult.Imported,
		"skipped":    importResult.Skipped,
		"total":      importResult.Total,
		"warnings":   importResult.Warnings,
	})
	return string(resultJSON)
}

// --- Share link resolution helpers (mirrors MaClawSrv/http_knowledge.go logic) ---

// convertPkgSources converts internal package source structs to the canonical
// knowledge.PackageSource type used by ImportPackageSources.
func convertPkgSources(items []sharePackageSource) []knowledge.PackageSource {
	out := make([]knowledge.PackageSource, 0, len(items))
	for _, item := range items {
		out = append(out, knowledge.PackageSource{
			ID:           item.ID,
			Kind:         item.Kind,
			URI:          item.URI,
			CanonicalURI: item.CanonicalURI,
			Title:        item.Title,
			TopicHint:    item.TopicHint,
			Labels:       item.Labels,
			Content:      item.Content,
		})
	}
	return out
}

// sharePackageManifest is the manifest section of a knowledge package.
type sharePackageManifest struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	PackageID string `json:"package_id"`
	Title     string `json:"title,omitempty"`
}

// sharePackageSource is one source item in a knowledge package.
type sharePackageSource struct {
	ID           string   `json:"id,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	URI          string   `json:"uri,omitempty"`
	CanonicalURI string   `json:"canonical_uri,omitempty"`
	Title        string   `json:"title,omitempty"`
	TopicHint    string   `json:"topic_hint,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Content      string   `json:"content,omitempty"`
}

// sharePackage is the top-level structure of a knowledge package JSON.
type sharePackage struct {
	Manifest sharePackageManifest `json:"manifest"`
	Sources  []sharePackageSource `json:"sources"`
}

// resolveShareAPIURL converts a human share link or knowledge_id + hub_url
// into the canonical API endpoint for fetching share metadata.
func resolveShareAPIURL(shareLink, knowledgeID, hubURL string) (string, string, error) {
	shareLink = strings.TrimSpace(shareLink)
	knowledgeID = strings.TrimSpace(knowledgeID)
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")

	if shareLink != "" {
		parsed, err := url.Parse(shareLink)
		if err != nil {
			return "", "", fmt.Errorf("invalid share_link: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return "", "", fmt.Errorf("share_link must be an absolute URL")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if knowledgeID == "" && len(parts) > 0 {
			knowledgeID = parts[len(parts)-1]
		}
		return parsed.Scheme + "://" + parsed.Host + "/api/knowledge/shares/" + url.PathEscape(knowledgeID) + "?intent=import", knowledgeID, nil
	}

	if hubURL == "" {
		return "", "", fmt.Errorf("hub_url is required when share_link is not provided")
	}
	if knowledgeID == "" {
		return "", "", fmt.Errorf("knowledge_id is required")
	}
	parsed, err := url.Parse(hubURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	return parsed.Scheme + "://" + parsed.Host + "/api/knowledge/shares/" + url.PathEscape(knowledgeID) + "?intent=import", knowledgeID, nil
}

// buildShareAuthHeader constructs an Authorization header from hub_token.
func buildShareAuthHeader(hubToken string) string {
	hubToken = strings.TrimSpace(hubToken)
	if hubToken == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(hubToken), "bearer ") {
		return hubToken
	}
	return "Bearer " + hubToken
}

// fetchShareMetadata fetches the share metadata from the resolved API URL.
func fetchShareMetadata(ctx context.Context, apiURL, authorization string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := shareImportHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch knowledge share: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("share resolver returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode share metadata: %w", err)
	}
	return out, nil
}

// resolveSharePackageURL extracts the package download URL from share metadata.
func resolveSharePackageURL(apiURL string, share map[string]interface{}) string {
	raw, _ := share["package_url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(apiURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

// downloadSharePackage fetches and parses the knowledge package JSON.
func downloadSharePackage(ctx context.Context, packageURL, authorization string) (sharePackage, error) {
	var pkg sharePackage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return pkg, err
	}
	req.Header.Set("Accept", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := shareImportHTTPClient.Do(req)
	if err != nil {
		return pkg, fmt.Errorf("fetch knowledge package: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return pkg, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pkg, fmt.Errorf("package download returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return pkg, fmt.Errorf("decode knowledge package: %w", err)
	}
	return pkg, nil
}
