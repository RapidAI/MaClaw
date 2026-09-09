package guiapp

// Skill Suite download and installation support.  Suites are delivered by
// HubCenter as a JSON envelope containing ordinary HubSkillFull payloads; each
// member is still installed as an independent local Skill.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// UploadSkillSuite packages the selected local Skills and submits a Suite
// envelope to the configured capability-market targets. Members remain
// ordinary Skill listings on HubCenter while the Suite provides one-click
// distribution metadata.
func (a *App) UploadSkillSuite(skillNames []string, suiteName string, force bool) (string, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "upload_suite", "names": skillNames, "source": "skillmarket"}); err != nil {
		return "", err
	}
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	seen := map[string]bool{}
	var skills []SkillSuiteSkill
	var members []SkillSuiteMember
	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		var target *corelib.NLSkillEntry
		for _, entry := range a.skillExecutor.loadSkills() {
			if entry.MatchesName(name) {
				cp := entry
				target = &cp
				break
			}
		}
		if target == nil {
			return "", fmt.Errorf("skill %q not found", name)
		}
		zipPath, tmpDir, err := a.packageSkillForMarketWithDirForOutbound(target.Name)
		if err != nil {
			return "", err
		}
		_ = os.Remove(zipPath)
		defer os.RemoveAll(tmpDir)
		payload := SkillSuiteSkill{ID: target.HubSkillID, SkillID: target.HubSkillID, Name: target.Name, Description: target.Description, Version: target.HubVersion, AgentSkillMD: ""}
		if payload.ID == "" {
			payload.ID = target.Name
		}
		if payload.Version == "" {
			payload.Version = target.Version
		}
		payload.Files = map[string]string{}
		walkErr := filepath.Walk(tmpDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return walkErr
			}
			rel, e := filepath.Rel(tmpDir, path)
			if e != nil {
				return nil
			}
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			payload.Files[filepath.ToSlash(rel)] = base64.StdEncoding.EncodeToString(data)
			return nil
		})
		if walkErr != nil {
			return "", fmt.Errorf("read skill %q for Suite upload: %w", target.Name, walkErr)
		}
		skills = append(skills, payload)
		members = append(members, SkillSuiteMember{SkillID: payload.ID, SkillRef: payload.SkillID, Name: payload.Name, Path: "skills/" + target.Name, Version: payload.Version, Required: true})
	}
	if len(skills) < 2 {
		return "", fmt.Errorf("at least two skills are required for a suite")
	}
	if strings.TrimSpace(suiteName) == "" {
		suiteName = strings.Join(skillNames, " + ")
	}
	suiteID := toKebabCase(suiteName)
	if suiteID == "" {
		suiteID = "skill-suite"
	}
	payload := SkillSuiteFull{ID: suiteID, Name: suiteName, Description: "Skill Suite: " + suiteName, Version: "1.0.0", Members: members, Skills: skills, DefinitionSource: "maclaw-gui", Manifest: map[string]interface{}{"format": "skill-suite.v1", "generated_at": time.Now().UTC().Format(time.RFC3339)}}
	if raw, marshalErr := json.Marshal(payload.Skills); marshalErr == nil {
		sum := sha256.Sum256(raw)
		payload.PackageSHA256 = hex.EncodeToString(sum[:])
	}
	a.ensureSkillMarketClient()
	if a.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}
	return a.skillMarketClient.SubmitSkillSuite(context.Background(), payload)
}

// SkillSuiteMember is the public Suite member metadata returned by HubCenter.
type SkillSuiteMember struct {
	SkillID  string `json:"skill_id"`
	SkillRef string `json:"skill_ref,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Version  string `json:"version,omitempty"`
	Required bool   `json:"required"`
}

// SkillSuiteSkill is a complete downloadable Skill payload embedded in a Suite.
// Keep this public shape explicit so Wails callers can inspect member metadata.
type SkillSuiteSkill struct {
	ID           string            `json:"id"`
	SkillID      string            `json:"skill_id,omitempty"`
	SemVer       string            `json:"semver,omitempty"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Tags         []string          `json:"tags,omitempty"`
	Version      string            `json:"version,omitempty"`
	Author       string            `json:"author,omitempty"`
	TrustLevel   string            `json:"trust_level,omitempty"`
	Downloads    int               `json:"downloads,omitempty"`
	AvgRating    float64           `json:"avg_rating,omitempty"`
	RatingCount  int               `json:"rating_count,omitempty"`
	ProductKind  string            `json:"product_kind,omitempty"`
	Triggers     []string          `json:"triggers,omitempty"`
	Steps        []hubSkillStep    `json:"steps,omitempty"`
	Manifest     hubSkillManifest  `json:"manifest,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
	AgentSkillMD string            `json:"agent_skill_md,omitempty"`
}

// SkillSuiteFull is the JSON representation returned by
// GET /api/v1/skill-suites/{id}/download?format=json.
type SkillSuiteFull struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description,omitempty"`
	Version          string                     `json:"version,omitempty"`
	Author           string                     `json:"author,omitempty"`
	License          string                     `json:"license,omitempty"`
	Tags             []string                   `json:"tags,omitempty"`
	SourceURL        string                     `json:"source_url,omitempty"`
	SourceRevision   string                     `json:"source_revision,omitempty"`
	DefinitionSource string                     `json:"definition_source,omitempty"`
	Members          []SkillSuiteMember         `json:"members"`
	Skills           []SkillSuiteSkill          `json:"skills"`
	Manifest         map[string]interface{}     `json:"manifest,omitempty"`
	PackageSHA256    string                     `json:"package_sha256,omitempty"`
	PackageSize      int64                      `json:"package_size,omitempty"`
	Permissions      []string                   `json:"permissions,omitempty"`
	VersionHistory   []SkillSuiteVersionSummary `json:"version_history,omitempty"`
}

type SkillSuiteVersionSummary struct {
	Version        string `json:"version"`
	SourceRevision string `json:"source_revision,omitempty"`
	UpdatedAt      string `json:"updated_at"`
}

// SkillSuiteInstallResult reports an atomic Suite installation attempt.
type SkillSuiteInstallResult struct {
	SuiteID   string   `json:"suite_id"`
	Installed []string `json:"installed,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
	Failed    []string `json:"failed,omitempty"`
	State     string   `json:"state"` // committed, skipped, rolled_back, failed
	Error     string   `json:"error,omitempty"`
}

func (a *App) emitSkillSuiteInstallProgress(suiteID, skill string, index, total int, phase, status string) {
	if a == nil {
		return
	}
	if total < 1 {
		total = 1
	}
	if index < 1 {
		index = 1
	}
	if index > total {
		index = total
	}
	a.emitEvent("skill-suite-install-progress", map[string]interface{}{
		"suite_id": suiteID, "skill": skill, "index": index, "total": total,
		"phase": phase, "status": status, "percent": skillInstallProgressPercent(phase),
	})
}

// ListSkillSuites returns visible Suite catalog entries from HubCenter.
func (c *SkillHubClient) ListSkillSuites(ctx context.Context) ([]SkillSuiteFull, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	var resp struct {
		Suites []SkillSuiteFull `json:"suites"`
	}
	enterpriseOnlySearch := false
	if cfg, cfgErr := c.app.LoadConfig(); cfgErr == nil {
		enterpriseOnlySearch = cfg.CapabilityMarketPolicy.EffectiveEnterpriseOnlySearch() && strings.TrimSpace(cfg.RemoteHubURL) != "" && capabilityMarketAuthToken(cfg) != ""
	}
	var hubErr error
	if !enterpriseOnlySearch {
		_, _, hubErr = c.getJSON(ctx, "/api/v1/skill-suites", &resp)
	}
	enterprise, enterpriseErr := c.app.listEnterpriseSkillSuites(ctx)
	resp.Suites = append(resp.Suites, enterprise...)
	if enterpriseOnlySearch {
		if enterpriseErr != nil {
			return nil, enterpriseErr
		}
		return resp.Suites, nil
	}
	if hubErr != nil && enterpriseErr != nil {
		return nil, hubErr
	}
	// A Suite may be published to both HubCenter and Enterprise Hub. Present a
	// single catalog entry, preferring the enterprise copy when available.
	if len(resp.Suites) > 1 {
		uniq := make(map[string]SkillSuiteFull, len(resp.Suites))
		order := make([]string, 0, len(resp.Suites))
		for _, suite := range resp.Suites {
			key := strings.ToLower(strings.TrimSpace(suite.ID))
			if key == "" {
				continue
			}
			if existing, exists := uniq[key]; !exists {
				order = append(order, key)
				uniq[key] = suite
			} else if skillSuiteCatalogRichness(suite) >= skillSuiteCatalogRichness(existing) {
				// Prefer the copy that carries actual member metadata/content.
				// Enterprise catalog responses may intentionally expose only a
				// member_count placeholder while HubCenter has full names.
				uniq[key] = suite
			}
		}
		resp.Suites = resp.Suites[:0]
		for _, key := range order {
			resp.Suites = append(resp.Suites, uniq[key])
		}
	}
	return resp.Suites, nil
}

func skillSuiteCatalogRichness(suite SkillSuiteFull) int {
	score := len(suite.Skills) * 4
	for _, member := range suite.Members {
		if strings.TrimSpace(firstNonEmpty(member.SkillID, member.SkillRef, member.Name)) != "" {
			score++
		}
	}
	return score
}

func (a *App) listEnterpriseSkillSuites(ctx context.Context) ([]SkillSuiteFull, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RemoteHubURL) == "" || capabilityMarketAuthToken(cfg) == "" {
		return nil, fmt.Errorf("enterprise Hub is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.RemoteHubURL, "/")+"/api/capabilities?type=skill", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+capabilityMarketAuthToken(cfg))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enterprise suite list failed: %d", resp.StatusCode)
	}
	var payload struct {
		Items []struct {
			CapabilityID string `json:"capability_id"`
			DisplayName  string `json:"display_name"`
			Description  string `json:"description"`
			Version      string `json:"version"`
			MetadataJSON string `json:"metadata_json"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := []SkillSuiteFull{}
	for _, item := range payload.Items {
		var meta struct {
			PackageKind string   `json:"package_kind"`
			MemberCount int      `json:"member_count"`
			Permissions []string `json:"permissions"`
		}
		_ = json.Unmarshal([]byte(item.MetadataJSON), &meta)
		if meta.PackageKind != "suite" {
			continue
		}
		members := make([]SkillSuiteMember, meta.MemberCount)
		out = append(out, SkillSuiteFull{ID: item.CapabilityID, Name: item.DisplayName, Description: item.Description, Version: item.Version, Members: members, Permissions: meta.Permissions, DefinitionSource: "enterprise-hub"})
	}
	return out, nil
}

func (a *App) ListSkillSuites() ([]SkillSuiteFull, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	return a.skillHubClient.ListSkillSuites(context.Background())
}

// DownloadSkillSuite fetches a complete Suite envelope from HubCenter.
func (c *SkillHubClient) DownloadSkillSuite(ctx context.Context, suiteID string) (*SkillSuiteFull, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	suiteID = strings.TrimSpace(suiteID)
	if suiteID == "" {
		return nil, fmt.Errorf("suite id is required")
	}
	path := "/api/v1/skill-suites/" + url.PathEscape(suiteID) + "/download?format=json"
	if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
		path += "&purchase_id=" + url.QueryEscape(purchaseID)
	}
	preferEnterprise := false
	if cfg, cfgErr := c.app.LoadConfig(); cfgErr == nil {
		preferEnterprise = cfg.CapabilityMarketPolicy.EffectivePreferredUploadTarget() == corelib.CapabilitySourceEnterpriseHub || (cfg.CapabilityMarketPolicy.EffectiveEnterpriseOnlyInstall() && strings.TrimSpace(cfg.RemoteHubURL) != "" && capabilityMarketAuthToken(cfg) != "")
	}
	var data []byte
	var err error
	if !preferEnterprise {
		data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, c.installClient, "", path, maxDownloadSize)
	} else {
		err = fmt.Errorf("enterprise target preferred")
	}
	if err != nil {
		if preferEnterprise {
			data, err = c.app.downloadEnterpriseSkillSuite(ctx, suiteID)
			if err != nil {
				return nil, err
			}
			return decodeSkillSuiteJSON(data, suiteID)
		}
		// Older HubCenter builds used the skillmarket namespace; retain a
		// compatibility fallback while the canonical route is /skill-suites.
		legacyPath := "/api/v1/skillmarket/suites/" + url.PathEscape(suiteID) + "/download?format=json"
		if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
			legacyPath += "&purchase_id=" + url.QueryEscape(purchaseID)
		}
		data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, c.installClient, "", legacyPath, maxDownloadSize)
		if err != nil {
			data, err = c.app.downloadEnterpriseSkillSuite(ctx, suiteID)
			if err != nil {
				return nil, err
			}
		}
	}
	return decodeSkillSuiteJSON(data, suiteID)
}

func (a *App) downloadEnterpriseSkillSuite(ctx context.Context, suiteID string) ([]byte, error) {
	return a.downloadEnterpriseSkillSuiteFormat(ctx, suiteID, "")
}

func (a *App) downloadEnterpriseSkillSuiteFormat(ctx context.Context, suiteID, format string) ([]byte, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RemoteHubURL) == "" || capabilityMarketAuthToken(cfg) == "" {
		return nil, fmt.Errorf("enterprise Hub is not configured")
	}
	endpoint := strings.TrimRight(cfg.RemoteHubURL, "/") + "/api/capabilities/skill-suites/" + url.PathEscape(suiteID) + "/download"
	if strings.TrimSpace(format) != "" {
		endpoint += "?format=" + url.QueryEscape(format)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+capabilityMarketAuthToken(cfg))
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enterprise suite download failed: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxDownloadSize {
		return nil, fmt.Errorf("enterprise suite download exceeds size limit")
	}
	return data, nil
}

// DownloadSkillSuiteZip returns the distributable Suite archive bytes.
func (c *SkillHubClient) DownloadSkillSuiteZip(ctx context.Context, suiteID string) ([]byte, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	suiteID = strings.TrimSpace(suiteID)
	if suiteID == "" {
		return nil, fmt.Errorf("suite id is required")
	}
	path := "/api/v1/skill-suites/" + url.PathEscape(suiteID) + "/download?format=zip"
	if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
		path += "&purchase_id=" + url.QueryEscape(purchaseID)
	}
	preferEnterprise := false
	if cfg, cfgErr := c.app.LoadConfig(); cfgErr == nil {
		preferEnterprise = cfg.CapabilityMarketPolicy.EffectivePreferredUploadTarget() == corelib.CapabilitySourceEnterpriseHub || (cfg.CapabilityMarketPolicy.EffectiveEnterpriseOnlyInstall() && strings.TrimSpace(cfg.RemoteHubURL) != "" && capabilityMarketAuthToken(cfg) != "")
	}
	var data []byte
	var err error
	if !preferEnterprise {
		data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, c.installClient, "", path, maxDownloadSize)
	} else {
		err = fmt.Errorf("enterprise target preferred")
	}
	if err == nil {
		if err := validateSkillSuiteZipArchive(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	if preferEnterprise {
		data, err := c.app.downloadEnterpriseSkillSuiteFormat(ctx, suiteID, "zip")
		if err != nil {
			return nil, err
		}
		if err := validateSkillSuiteZipArchive(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	legacyPath := "/api/v1/skillmarket/suites/" + url.PathEscape(suiteID) + "/download?format=zip"
	if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
		legacyPath += "&purchase_id=" + url.QueryEscape(purchaseID)
	}
	data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, c.installClient, "", legacyPath, maxDownloadSize)
	if err == nil {
		if err := validateSkillSuiteZipArchive(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	data, err = c.app.downloadEnterpriseSkillSuiteFormat(ctx, suiteID, "zip")
	if err != nil {
		return nil, err
	}
	if err := validateSkillSuiteZipArchive(data); err != nil {
		return nil, err
	}
	return data, nil
}

// validateSkillSuiteZipArchive enforces safe paths and verifies the optional
// integrity manifest emitted by HubCenter/Enterprise Hub before exposing an
// archive to the GUI download flow.
func validateSkillSuiteZipArchive(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid skill suite zip: %w", err)
	}
	if len(zr.File) > maxSkillZipEntries {
		return fmt.Errorf("suite archive contains too many entries: %d > %d", len(zr.File), maxSkillZipEntries)
	}
	var suiteJSON []byte
	var suiteYAML []byte
	var hashes map[string]string
	manifestPresent := false
	contents := map[string][]byte{}
	seenEntries := map[string]struct{}{}
	var totalExpanded uint64
	for _, file := range zr.File {
		clean, ok := safeSkillSuiteArchivePath(file.Name)
		if !ok {
			return fmt.Errorf("invalid suite archive path: %s", file.Name)
		}
		if _, duplicate := seenEntries[strings.ToLower(clean)]; duplicate {
			return fmt.Errorf("duplicate suite archive entry: %s", file.Name)
		}
		seenEntries[strings.ToLower(clean)] = struct{}{}
		top := clean
		if idx := strings.IndexByte(clean, '/'); idx >= 0 {
			top = clean[:idx]
		}
		if top != "suite.json" && top != "suite.yaml" && top != "suite_integrity_manifest.json" && top != "skills" {
			return fmt.Errorf("unknown suite archive top-level entry: %s", file.Name)
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink entries are not allowed in suite archive: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if clean != "skills" && top != "skills" {
				return fmt.Errorf("unexpected suite archive directory: %s", file.Name)
			}
			continue
		}
		if clean == "skills" {
			return fmt.Errorf("suite archive skills entry must be a directory")
		}
		if file.UncompressedSize64 > uint64(maxSkillZipFileBytes) {
			return fmt.Errorf("suite archive entry too large: %s", file.Name)
		}
		if file.UncompressedSize64 > 0 {
			totalExpanded += file.UncompressedSize64
			if totalExpanded > uint64(maxSkillZipTotalExpandedBytes) {
				return fmt.Errorf("suite archive expands to too much data")
			}
		}
		rc, readErr := file.Open()
		if readErr != nil {
			return readErr
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, maxDownloadSize+1))
		_ = rc.Close()
		if readErr != nil {
			return readErr
		}
		if int64(len(content)) > maxDownloadSize {
			return fmt.Errorf("suite archive exceeds size limit")
		}
		switch clean {
		case "suite.json":
			suiteJSON = content
		case "suite.yaml":
			suiteYAML = content
		case "suite_integrity_manifest.json":
			manifestPresent = true
			var m struct {
				Files map[string]string `json:"files"`
			}
			if err := json.Unmarshal(content, &m); err != nil {
				return fmt.Errorf("invalid suite integrity manifest: %w", err)
			}
			hashes = m.Files
		}
		contents[clean] = content
	}
	if len(suiteJSON) == 0 {
		return fmt.Errorf("suite archive missing suite.json")
	}
	if len(suiteYAML) == 0 {
		return fmt.Errorf("suite archive missing suite.yaml")
	}
	if manifestPresent && len(hashes) == 0 {
		return fmt.Errorf("suite integrity manifest has no files")
	}
	for name, expected := range hashes {
		if _, ok := safeSkillSuiteArchivePath(name); !ok {
			return fmt.Errorf("invalid suite manifest path: %s", name)
		}
		content, ok := contents[name]
		if !ok || !strings.EqualFold(expected, skillSuiteSHA256Hex(content)) {
			return fmt.Errorf("suite archive checksum mismatch: %s", name)
		}
	}
	// Every payload file must be covered by the integrity manifest. The
	// manifest itself is intentionally excluded because it contains the hashes.
	if manifestPresent {
		for name := range contents {
			if name == "suite_integrity_manifest.json" {
				continue
			}
			if _, ok := hashes[name]; !ok {
				return fmt.Errorf("suite archive missing checksum: %s", name)
			}
		}
	}
	var suite SkillSuiteFull
	if err := json.Unmarshal(suiteJSON, &suite); err != nil {
		return fmt.Errorf("decode suite archive manifest: %w", err)
	}
	if suite.ID != "" && !strings.Contains(string(suiteYAML), "id: \""+suite.ID+"\"") && !strings.Contains(string(suiteYAML), "id: "+suite.ID) {
		return fmt.Errorf("suite yaml id does not match suite.json")
	}
	if len(suite.Members) > 0 && strings.Count(string(suiteYAML), "path:") < len(suite.Members) {
		return fmt.Errorf("suite yaml is missing member paths")
	}
	if err := validateSkillSuitePayload(&suite); err != nil {
		return err
	}
	return nil
}

// safeSkillSuiteArchivePath normalizes ZIP entry names while rejecting path
// traversal, absolute paths and Windows drive-qualified paths. ZIP archives
// use slash-separated names even when produced on Windows.
func safeSkillSuiteArchivePath(name string) (string, bool) {
	n := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if n == "" || strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") {
		return "", false
	}
	if len(n) >= 2 && ((n[0] >= 'A' && n[0] <= 'Z') || (n[0] >= 'a' && n[0] <= 'z')) && n[1] == ':' {
		return "", false
	}
	clean := path.Clean(n)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	return clean, true
}

func skillSuiteSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DownloadSkillSuite is also exposed on the marketplace client for callers
// that already use SkillMarketClient for HubCenter operations.
func (c *SkillMarketClient) DownloadSkillSuite(ctx context.Context, suiteID string) (*SkillSuiteFull, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("skill market client not initialized")
	}
	suiteID = strings.TrimSpace(suiteID)
	if suiteID == "" {
		return nil, fmt.Errorf("suite id is required")
	}
	httpClient := c.client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	preferEnterprise := false
	if cfg, cfgErr := c.app.LoadConfig(); cfgErr == nil {
		preferEnterprise = cfg.CapabilityMarketPolicy.EffectivePreferredUploadTarget() == corelib.CapabilitySourceEnterpriseHub || (cfg.CapabilityMarketPolicy.EffectiveEnterpriseOnlyInstall() && strings.TrimSpace(cfg.RemoteHubURL) != "" && capabilityMarketAuthToken(cfg) != "")
	}
	path := "/api/v1/skill-suites/" + url.PathEscape(suiteID) + "/download?format=json"
	if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
		path += "&purchase_id=" + url.QueryEscape(purchaseID)
	}
	var data []byte
	var err error
	if !preferEnterprise {
		data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, httpClient, "", path, maxDownloadSize)
	}
	if preferEnterprise || err != nil {
		if preferEnterprise {
			data, err = c.app.downloadEnterpriseSkillSuite(ctx, suiteID)
		} else {
			legacyPath := "/api/v1/skillmarket/suites/" + url.PathEscape(suiteID) + "/download?format=json"
			if purchaseID := c.app.getSuitePurchaseID(suiteID); purchaseID != "" {
				legacyPath += "&purchase_id=" + url.QueryEscape(purchaseID)
			}
			data, _, err = c.app.getHubCenterDownloadLocatorBytes(ctx, httpClient, "", legacyPath, maxDownloadSize)
			if err != nil {
				data, err = c.app.downloadEnterpriseSkillSuite(ctx, suiteID)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return decodeSkillSuiteJSON(data, suiteID)
}

func decodeSkillSuiteJSON(data []byte, suiteID string) (*SkillSuiteFull, error) {
	var suite SkillSuiteFull
	if err := json.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("decode skill suite: %w", err)
	}
	if strings.TrimSpace(suite.ID) == "" {
		suite.ID = strings.TrimSpace(suiteID)
	}
	if !safeSkillSuiteID(suite.ID) {
		return nil, fmt.Errorf("invalid skill suite id: %s", suite.ID)
	}
	if len(suite.Skills) == 0 {
		return nil, fmt.Errorf("skill suite %q contains no skills", suite.ID)
	}
	if expected := strings.TrimSpace(suite.PackageSHA256); expected != "" {
		raw, err := json.Marshal(suite.Skills)
		if err != nil {
			return nil, fmt.Errorf("marshal skill suite integrity payload: %w", err)
		}
		if !strings.EqualFold(skillSuiteSHA256Hex(raw), expected) {
			return nil, fmt.Errorf("skill suite integrity checksum mismatch")
		}
	}
	return &suite, nil
}

// DownloadSkillSuite is the Wails binding used by the GUI.
func (a *App) DownloadSkillSuite(suiteID string) (*SkillSuiteFull, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	return a.skillHubClient.DownloadSkillSuite(context.Background(), suiteID)
}

func (a *App) DownloadSkillSuiteZip(suiteID string) ([]byte, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	return a.skillHubClient.DownloadSkillSuiteZip(context.Background(), suiteID)
}

// PurchaseSkillSuite acquires a one-time Suite entitlement for the configured
// marketplace account. Enterprise capability hubs do not expose Credits
// purchases and therefore return the server error unchanged.
func (a *App) PurchaseSkillSuite(suiteID string) (map[string]interface{}, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" {
		return nil, fmt.Errorf("remote email is required to purchase a Suite")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if base == "" {
		return nil, fmt.Errorf("HubCenter URL is not configured")
	}
	endpoint := base + "/api/v1/skillmarket/suites/" + url.PathEscape(strings.TrimSpace(suiteID)) + "/purchase?email=" + url.QueryEscape(email)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if auth := a.skillHubClient.publishAuthHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Suite purchase failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if purchaseID, ok := out["purchase_id"].(string); ok && strings.TrimSpace(purchaseID) != "" {
		a.setSuitePurchaseID(suiteID, purchaseID)
	}
	return out, nil
}

func (a *App) setSuitePurchaseID(suiteID, purchaseID string) {
	if a == nil {
		return
	}
	a.suitePurchasesMu.Lock()
	if a.suitePurchases == nil {
		a.suitePurchases = make(map[string]string)
	}
	a.suitePurchases[strings.ToLower(strings.TrimSpace(suiteID))] = strings.TrimSpace(purchaseID)
	a.suitePurchasesMu.Unlock()
}

func (a *App) getSuitePurchaseID(suiteID string) string {
	if a == nil {
		return ""
	}
	a.suitePurchasesMu.Lock()
	defer a.suitePurchasesMu.Unlock()
	return a.suitePurchases[strings.ToLower(strings.TrimSpace(suiteID))]
}

func (a *App) GetSkillSuiteVersions(suiteID string) (map[string]interface{}, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	var out map[string]interface{}
	_, _, err := a.skillHubClient.getJSON(context.Background(), "/api/v1/skill-suites/"+url.PathEscape(strings.TrimSpace(suiteID))+"/versions", &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// InstallSkillSuite downloads and installs selected members. Required members
// are always included. Members are pre-decoded and scanned before publication;
// if a later member fails, the in-memory Skill registry is restored to its
// original snapshot and newly-created directories are removed.
func (a *App) InstallSkillSuite(suiteID string, selectedSkillIDs []string) (SkillSuiteInstallResult, error) {
	result := SkillSuiteInstallResult{SuiteID: strings.TrimSpace(suiteID), State: "failed"}
	suite, err := a.DownloadSkillSuite(suiteID)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := validateSkillSuitePayload(suite); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.SuiteID = suite.ID
	selected := map[string]bool{}
	for _, id := range selectedSkillIDs {
		if id = strings.TrimSpace(id); id != "" {
			selected[strings.ToLower(id)] = true
		}
	}
	// Enterprise catalog listings may expose only member_count (to avoid
	// leaking the package envelope), leaving placeholder members without IDs.
	// In that legacy/metadata-only shape an empty selection means "install the
	// Suite", so select every embedded member rather than reporting no skills.
	if len(selected) == 0 {
		hasMemberMetadata := false
		for _, member := range suite.Members {
			if strings.TrimSpace(firstNonEmpty(member.SkillID, member.SkillRef, member.Name)) != "" {
				hasMemberMetadata = true
				break
			}
		}
		if !hasMemberMetadata {
			for _, payload := range suite.Skills {
				if id := strings.TrimSpace(firstNonEmpty(payload.SkillID, payload.ID, payload.Name)); id != "" {
					selected[strings.ToLower(id)] = true
				}
			}
		}
	}
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		err = fmt.Errorf("skill executor not initialized")
		result.Error = err.Error()
		return result, err
	}
	original := a.skillExecutor.loadSkills()
	createdDirs := []string{}
	originalDirs := make(map[string]struct{}, len(original))
	for _, entry := range original {
		if dir := strings.TrimSpace(entry.SkillDir); dir != "" {
			originalDirs[filepath.Clean(dir)] = struct{}{}
		}
	}
	trackNewSkillDirs := func() {
		known := make(map[string]struct{}, len(createdDirs))
		for _, dir := range createdDirs {
			known[filepath.Clean(dir)] = struct{}{}
		}
		for _, entry := range a.skillExecutor.loadSkills() {
			dir := strings.TrimSpace(entry.SkillDir)
			if dir == "" {
				continue
			}
			clean := filepath.Clean(dir)
			if _, existed := originalDirs[clean]; existed {
				continue
			}
			if _, alreadyTracked := known[clean]; alreadyTracked {
				continue
			}
			createdDirs = append(createdDirs, dir)
			known[clean] = struct{}{}
		}
	}
	rollback := func() error {
		var rollbackErr error
		if err := a.skillExecutor.withSkillListMutate(func() error { return a.skillExecutor.restoreSkillsSnapshot(original) }); err != nil {
			rollbackErr = err
		}
		for _, d := range createdDirs {
			if err := os.RemoveAll(d); err != nil && rollbackErr == nil {
				rollbackErr = err
			}
		}
		return rollbackErr
	}
	persistRollbackCompensation := func(reason string) {
		record := cskill.NewEvolutionCompensationRecord("suite_"+suite.ID, suite.ID, "skill-suite-install", "", nil, false, original, reason)
		_ = cskill.PersistEvolutionCompensation(record)
	}
	type pendingSuiteInstall struct {
		id      string
		index   int
		entry   *corelib.NLSkillEntry
		staging string
		report  *cskill.ScanReport
	}
	var pending []pendingSuiteInstall
	var legacy []struct {
		id, ref string
		index   int
	}
	totalMembers := len(suite.Skills)
	for i := range suite.Skills {
		payload := suite.Skills[i]
		id := strings.TrimSpace(payload.SkillID)
		if id == "" {
			id = strings.TrimSpace(payload.ID)
		}
		if id == "" {
			id = strings.TrimSpace(payload.Name)
		}
		member := SkillSuiteMember{SkillID: id, Name: payload.Name, Version: payload.Version}
		for _, m := range suite.Members {
			if strings.EqualFold(firstNonEmpty(m.SkillID, m.SkillRef), id) || strings.EqualFold(m.Name, payload.Name) {
				member = m
				break
			}
		}
		if member.Required || selected[strings.ToLower(id)] || selected[strings.ToLower(member.SkillRef)] || selected[strings.ToLower(member.Name)] {
			// selected
		} else {
			continue
		}
		a.emitSkillSuiteInstallProgress(suite.ID, id, i+1, totalMembers, "queued", "Suite member queued")
		// Legacy Suite responses may contain metadata only. In that case defer to
		// the established mixed-skill transaction, which downloads the member
		// package and provides the full compensation/audit boundary.
		if len(payload.Files) == 0 && strings.TrimSpace(payload.AgentSkillMD) == "" {
			ref := firstNonEmpty(member.SkillRef, id)
			legacy = append(legacy, struct {
				id, ref string
				index   int
			}{id, ref, i + 1})
			continue
		}
		if existing := a.installedMixedSkillForUpdate(id, payload.Name); existing != nil && skillInstallAlreadyCurrent(existing, &corelib.NLSkillEntry{HubSkillID: id, HubVersion: payload.Version, Version: payload.Version}) {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		data, _ := json.Marshal(payload)
		stagingRoot, e := a.skillStagingDir()
		if e != nil {
			err = e
		} else {
			staging, e2 := cskill.PrepareStagingDirInRoot(stagingRoot, firstNonEmpty(id, "suite-skill"))
			if e2 != nil {
				err = e2
			} else {
				entry, e3 := decodeDownloadedSkillJSONToDir(data, staging)
				if e3 != nil {
					err = e3
				} else {
					entry.Source = "hub"
					entry.HubSkillID = id
					entry.HubVersion = payload.Version
					report, e4 := a.scanAndAdmitSkillBeforeRegister(context.Background(), entry, "skill-suite")
					if e4 != nil {
						err = e4
						a.emitSkillSuiteInstallProgress(suite.ID, id, i+1, totalMembers, "blocked", e4.Error())
					} else {
						a.emitSkillSuiteInstallProgress(suite.ID, id, i+1, totalMembers, "scan-complete", "Security scan passed")
						pending = append(pending, pendingSuiteInstall{id: id, index: i + 1, entry: entry, staging: staging, report: report})
					}
				}
			}
		}
		if err != nil {
			result.Failed = append(result.Failed, id)
			for _, item := range pending {
				cskill.CleanupStaging(item.staging)
			}
			rollbackErr := rollback()
			if rollbackErr != nil {
				persistRollbackCompensation(rollbackErr.Error())
			}
			result.State = "rolled_back"
			if rollbackErr != nil {
				result.State = "compensation_pending"
			}
			result.Error = err.Error()
			return result, err
		}
	}
	// All complete payload members have now passed decoding and security scan.
	// Only after this preflight barrier do we publish staged directories.
	for _, item := range pending {
		a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "installing", "Installing Suite member")
		e := a.commitStagedSkillInstall(context.Background(), item.entry, item.staging, "skill-suite", item.report, fmt.Sprintf("suite_%s_%s", suite.ID, item.id), skillEvolutionConfigRevision(a))
		if e != nil {
			a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "blocked", e.Error())
			result.Failed = append(result.Failed, item.id)
			if rollbackErr := rollback(); rollbackErr != nil {
				persistRollbackCompensation(rollbackErr.Error())
				result.State = "compensation_pending"
			}
			if result.State == "failed" {
				result.State = "rolled_back"
			}
			result.Error = e.Error()
			return result, e
		}
		a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "done", "Skill installed successfully.")
		if item.entry.SkillDir != "" {
			createdDirs = append(createdDirs, item.entry.SkillDir)
		}
		result.Installed = append(result.Installed, item.id)
	}
	for _, item := range legacy {
		a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "installing", "Installing Suite member")
		if e := a.InstallMixedSkill("skillhub", item.id, item.ref); e != nil {
			a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "blocked", e.Error())
			result.Failed = append(result.Failed, item.id)
			if rollbackErr := rollback(); rollbackErr != nil {
				persistRollbackCompensation(rollbackErr.Error())
				result.State = "compensation_pending"
			}
			if result.State == "failed" {
				result.State = "rolled_back"
			}
			result.Error = e.Error()
			return result, e
		}
		trackNewSkillDirs()
		a.emitSkillSuiteInstallProgress(suite.ID, item.id, item.index, totalMembers, "done", "Skill installed successfully.")
		result.Installed = append(result.Installed, item.id)
	}
	result.State = "committed"
	if len(result.Installed) == 0 && len(result.Skipped) > 0 {
		result.State = "skipped"
	} else if len(result.Installed) == 0 {
		err = fmt.Errorf("no suite skills selected")
		result.State, result.Error = "failed", err.Error()
		return result, err
	}
	return result, nil
}

func validateSkillSuitePayload(suite *SkillSuiteFull) error {
	if suite == nil || len(suite.Skills) == 0 {
		return fmt.Errorf("skill suite contains no skills")
	}
	if strings.TrimSpace(suite.ID) != "" && !safeSkillSuiteID(strings.TrimSpace(suite.ID)) {
		return fmt.Errorf("invalid skill suite id")
	}
	if len(suite.Skills) > 32 {
		return fmt.Errorf("skill suite contains too many skills (maximum 32)")
	}
	seen := map[string]bool{}
	for _, sk := range suite.Skills {
		id := firstNonEmpty(sk.SkillID, sk.ID, sk.Name)
		if id == "" {
			return fmt.Errorf("suite member has no id")
		}
		if seen[strings.ToLower(id)] {
			return fmt.Errorf("duplicate suite member id: %s", id)
		}
		seen[strings.ToLower(id)] = true
		total := 0
		seenFiles := map[string]struct{}{}
		for path, encoded := range sk.Files {
			clean, ok := safeSkillSuiteArchivePath(path)
			if !ok {
				return fmt.Errorf("invalid suite member file path: %s", path)
			}
			if _, duplicate := seenFiles[clean]; duplicate {
				return fmt.Errorf("duplicate suite member file path: %s", path)
			}
			seenFiles[clean] = struct{}{}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(data) > 256<<10 {
				return fmt.Errorf("invalid or oversized suite member file: %s", path)
			}
			total += len(data)
			if total > 10<<20 {
				return fmt.Errorf("suite member files exceed size limit")
			}
		}
	}
	if expected := strings.TrimSpace(suite.PackageSHA256); expected != "" {
		raw, err := json.Marshal(suite.Skills)
		if err != nil {
			return fmt.Errorf("marshal suite integrity payload: %w", err)
		}
		sum := sha256.Sum256(raw)
		if !strings.EqualFold(expected, hex.EncodeToString(sum[:])) {
			return fmt.Errorf("skill suite integrity checksum mismatch")
		}
	}
	return nil
}

func safeSkillSuiteID(id string) bool {
	if id == "" {
		return false
	}
	for i, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			if i == 0 && (r == '-' || r == '_' || r == '.') {
				return false
			}
			continue
		}
		return false
	}
	return true
}
