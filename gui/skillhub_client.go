package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// HubSkillMeta is the client-side Skill metadata returned from SkillHub searches.
type HubSkillMeta struct {
	ID                           string                 `json:"id"`
	Name                         string                 `json:"name"`
	Description                  string                 `json:"description"`
	Tags                         []string               `json:"tags"`
	Version                      string                 `json:"version"`
	Author                       string                 `json:"author"`
	TrustLevel                   string                 `json:"trust_level"`
	Downloads                    int                    `json:"downloads"`
	HubURL                       string                 `json:"hub_url"`
	AvgRating                    float64                `json:"avg_rating"`
	RatingCount                  int                    `json:"rating_count"`
	ProductKind                  string                 `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                   `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                 `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                 `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                 `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                 `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                 `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                 `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                   `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                 `json:"artifact_contract_presentation,omitempty"`
}

type MaclawAppTestEvidence struct {
	RunID                 string         `json:"run_id,omitempty"`
	VerifiedAt            string         `json:"verified_at,omitempty"`
	DefinitionFingerprint string         `json:"definition_fingerprint,omitempty"`
	ArtifactPresent       bool           `json:"artifact_present,omitempty"`
	ArtifactName          string         `json:"artifact_name,omitempty"`
	OutputCount           int            `json:"output_count,omitempty"`
	PrimaryResult         string         `json:"primary_result,omitempty"`
	ResultPayload         map[string]any `json:"result_payload,omitempty"`
}

// cachedSearchResult holds a cached search response with expiry.
type cachedSearchResult struct {
	results   []HubSkillMeta
	expiresAt time.Time
}

const (
	maxCacheEntries = 100
	// Search/list/metadata only — keep tight so a runaway endpoint cannot pin RAM.
	maxSearchJSONSize = skill.MaxSkillHubSearchJSONBytes
	// Skill install JSON includes base64 file maps; multi-asset packages
	// (templates, fonts, SVGs) routinely exceed 5 MiB on the wire.
	maxDownloadSize = skill.MaxSkillPackageDownloadBytes
)

// SkillHubClient queries the hub's own SkillHub API for skill search, download, and recommendations.
type SkillHubClient struct {
	app           *App
	mu            sync.RWMutex
	cache         map[string]cachedSearchResult
	cacheTTL      time.Duration
	recIndex      []HubSkillMeta
	client        *http.Client // 10s timeout for search/metadata APIs
	installClient *http.Client // long timeout for multi-asset skill package downloads
}

// NewSkillHubClient creates a new SkillHubClient with default settings.
func NewSkillHubClient(app *App) *SkillHubClient {
	return &SkillHubClient{
		app:      app,
		cache:    make(map[string]cachedSearchResult),
		cacheTTL: 5 * time.Minute,
		client:   &http.Client{Timeout: 10 * time.Second},
		// installClient uses a longer timeout for multi-asset skill packages
		// (up to MaxSkillPackageDownloadBytes) on slow networks.
		installClient: &http.Client{Timeout: 180 * time.Second},
	}
}

func (c *SkillHubClient) selectBaseURL(ctx context.Context) (string, []string, error) {
	base, discovered, err := c.app.resolveHubCenterBaseURLCached(ctx, c.client)
	if err != nil {
		return "", nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return base, discovered, nil
}

// hubSkillSearchResult mirrors the hub's SkillSearchResult JSON.
type hubSkillSearchResult struct {
	Skills []hubSkillItem `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

// hubSkillItem mirrors the hub's HubSkillMeta JSON.
type hubSkillItem struct {
	ID                           string                 `json:"id"`
	SkillID                      string                 `json:"skill_id,omitempty"`
	SemVer                       string                 `json:"semver,omitempty"`
	Name                         string                 `json:"name"`
	Description                  string                 `json:"description"`
	Tags                         []string               `json:"tags"`
	Version                      string                 `json:"version"`
	Author                       string                 `json:"author"`
	TrustLevel                   string                 `json:"trust_level"`
	Downloads                    int                    `json:"downloads"`
	AvgRating                    float64                `json:"avg_rating"`
	RatingCount                  int                    `json:"rating_count"`
	ProductKind                  string                 `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                   `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                 `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                 `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                 `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                 `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                 `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                 `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                   `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                 `json:"artifact_contract_presentation,omitempty"`
}

// hubSkillFull mirrors the hub's HubSkillFull JSON for download.
type hubSkillFull struct {
	hubSkillItem
	Triggers     []string          `json:"triggers"`
	Steps        []hubSkillStep    `json:"steps"`
	Manifest     hubSkillManifest  `json:"manifest,omitempty"`
	Files        map[string]string `json:"files,omitempty"`          // path -> base64 content
	AgentSkillMD string            `json:"agent_skill_md,omitempty"` // SKILL.md content
}

// hubSkillManifest mirrors the hub's SkillManifest.
type hubSkillManifest struct {
	MinMaclawVersion string               `json:"min_maclaw_version,omitempty"`
	RequiredMCP      []string             `json:"required_mcp,omitempty"`
	Permissions      []string             `json:"permissions,omitempty"`
	Dependencies     []hubSkillDependency `json:"dependencies,omitempty"`
	Compatibility    string               `json:"compatibility,omitempty"`
}

// hubSkillDependency mirrors the hub's SkillDependency.
type hubSkillDependency struct {
	Type    string `json:"type"` // "pip", "npm", "brew", "apt", "binary"
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type hubSkillStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`
	Name      string                 `json:"name,omitempty"`
	Condition string                 `json:"condition,omitempty"`
}

func hubItemToMeta(item hubSkillItem, hubURL string) HubSkillMeta {
	return HubSkillMeta{
		ID:                           item.ID,
		Name:                         item.Name,
		Description:                  item.Description,
		Tags:                         item.Tags,
		Version:                      item.Version,
		Author:                       item.Author,
		TrustLevel:                   item.TrustLevel,
		Downloads:                    item.Downloads,
		HubURL:                       hubURL,
		AvgRating:                    item.AvgRating,
		RatingCount:                  item.RatingCount,
		ProductKind:                  item.ProductKind,
		IsMaclawApp:                  item.IsMaclawApp || strings.EqualFold(strings.TrimSpace(item.ProductKind), "maclaw_app_skill"),
		MaclawAppID:                  item.MaclawAppID,
		MaclawAppName:                item.MaclawAppName,
		MaclawAppDescription:         item.MaclawAppDescription,
		MaclawAppCategory:            item.MaclawAppCategory,
		MaclawAppIcon:                item.MaclawAppIcon,
		MaclawAppInputMode:           item.MaclawAppInputMode,
		MaclawAppOutputModes:         append([]string(nil), item.MaclawAppOutputModes...),
		MaclawAppDefinitionSHA256:    item.MaclawAppDefinitionSHA256,
		MaclawAppTestEvidence:        cloneSkillHubMaclawAppTestEvidence(item.MaclawAppTestEvidence),
		ArtifactContractRequired:     item.ArtifactContractRequired,
		ArtifactContractOutputModes:  append([]string(nil), item.ArtifactContractOutputModes...),
		ArtifactContractPresentation: item.ArtifactContractPresentation,
	}
}

func cloneSkillHubMaclawAppTestEvidence(e *MaclawAppTestEvidence) *MaclawAppTestEvidence {
	if e == nil {
		return nil
	}
	copy := *e
	return &copy
}

// Search queries the hub's SkillHub API and returns matching skills.
func (c *SkillHubClient) Search(ctx context.Context, query string) ([]HubSkillMeta, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Check cache first.
	c.mu.RLock()
	if cached, ok := c.cache[query]; ok && time.Now().Before(cached.expiresAt) {
		results := cached.results
		c.mu.RUnlock()
		return results, nil
	}
	c.mu.RUnlock()

	path := "/api/v1/skills/search?q=" + url.QueryEscape(query) + "&page=1"
	var result hubSkillSearchResult
	base, _, err := c.getJSON(ctx, path, &result)
	if err != nil {
		return nil, fmt.Errorf("search SkillHub failed: %v", err)
	}

	skills := make([]HubSkillMeta, 0, len(result.Skills))
	for _, item := range result.Skills {
		skills = append(skills, hubItemToMeta(item, base))
	}

	c.cacheResults(query, skills)
	return skills, nil
}

// Install downloads a Skill from the hub, extracts bundled files to
// ~/.maclaw/data/skills/<name>/, and converts the skill to an NLSkillEntry.
// Dependency installation is intentionally deferred until after security scan.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*corelib.NLSkillEntry, error) {
	return c.InstallToDir(ctx, skillID, hubURL, "")
}

// InstallToDir downloads a Skill and extracts bundled files to targetDir.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<name>/.
// The returned entry's SkillDir is set to the actual extraction directory.
// It must not perform dependency installation before the caller scans the skill.
func (c *SkillHubClient) InstallToDir(ctx context.Context, skillID, hubURL, targetDir string) (*corelib.NLSkillEntry, error) {
	return c.InstallToDirWithIntegrity(ctx, skillID, hubURL, targetDir, "", "")
}

func (c *SkillHubClient) InstallToDirWithIntegrity(ctx context.Context, skillID, hubURL, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	path := "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	base, _, data, err := c.getBytesFromExplicitHubURL(ctx, hubURL, path, maxDownloadSize)
	if err != nil {
		return nil, fmt.Errorf("download skill %q from %s failed: %v", skillID, strings.TrimRight(hubURL, "/"), err)
	}
	if err := verifyDownloadedSkillPackageSHA256(data, expectedSHA256); err != nil {
		return nil, err
	}
	var trustedFingerprints []string
	if c != nil && c.app != nil {
		trustedFingerprints = c.app.trustedSkillPackageKeyFingerprints()
	}
	if err := verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, expectedSignature, trustedFingerprints); err != nil {
		return nil, err
	}
	var full hubSkillFull
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, fmt.Errorf("decode skill %q from %s failed after %d bytes: %w", skillID, strings.TrimRight(hubURL, "/"), len(data), err)
	}

	steps := make([]corelib.NLSkillStep, 0, len(full.Steps))
	for _, s := range full.Steps {
		steps = append(steps, corelib.NLSkillStep{
			Action:    s.Action,
			Params:    s.Params,
			OnError:   s.OnError,
			Name:      s.Name,
			Condition: s.Condition,
		})
	}

	installName := firstNonEmpty(full.Name, skillID)
	installSkillDir := targetDir
	if installSkillDir == "" && installName != "" {
		if skillsRoot, err := skill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, installName)
		}
	}
	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}

	// Extract bundled files to targetDir (or default skills dir).
	if len(full.Files) > 0 {
		if err := extractBundledSkillFiles(installName, full.Files, targetDir); err != nil {
			return nil, fmt.Errorf("extract bundled files for skill %q: %w", installName, err)
		}
	}

	// Skills downloaded from the configured hub (official store) should be
	// treated as "trusted" rather than "community". The hub server currently
	// hardcodes all published skills to "community", which causes the risk
	// assessor to escalate their risk level and block legitimate installs.
	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		SkillID:       full.SkillID,
		Name:          full.Name,
		Description:   full.Description,
		Triggers:      full.Triggers,
		Steps:         steps,
		Status:        skillEntryStatusActive.String(),
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        skillEntrySourceHub.String(),
		SourceProject: base,
		HubSkillID:    full.ID,
		HubVersion:    full.Version,
		Version:       full.SemVer,
		TrustLevel:    trustLevel,
		SkillDir:      installSkillDir,
	}, nil
}

func craftToolStepsFromBundledSkillFiles(files map[string]string, skillDir string) []corelib.NLSkillStep {
	if len(files) == 0 {
		return nil
	}
	for _, key := range []string{"SKILL.md", "skill.md"} {
		b64, ok := files[key]
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(decoded) == 0 {
			continue
		}
		params := map[string]interface{}{
			"instructions":      string(decoded),
			"verification_mode": "artifact_optional",
			"register_policy":   "manual",
		}
		if strings.TrimSpace(skillDir) != "" {
			params["working_dir"] = skillDir
		}
		return []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: params,
		}}
	}
	return nil
}

// extractFiles writes bundled files (base64-encoded) to the specified targetDir.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<name>/.
// It preserves the downloaded package as-is and only rejects structurally unsafe
// paths or corrupt payloads; security scanning runs on the staged result before
// registration.
func (c *SkillHubClient) extractFiles(skillName string, files map[string]string, targetDir string) error {
	return extractBundledSkillFiles(skillName, files, targetDir)
}

func extractBundledSkillFiles(skillName string, files map[string]string, targetDir string) error {
	skillDir := targetDir
	if skillDir == "" {
		if strings.TrimSpace(skillName) == "" {
			return fmt.Errorf("skill name is required when target directory is empty")
		}
		skillsRoot, err := skill.PrimarySkillsDir()
		if err != nil {
			return err
		}
		skillDir = filepath.Join(skillsRoot, skillName)
	}
	skillDirAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return err
	}
	rootExists := true
	if info, err := os.Lstat(skillDirAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe skill directory %q", skillDir)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		rootExists = false
	}

	decodedFiles := make([]bundledSkillFile, 0, len(files))
	for relPath, b64Content := range files {
		data, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			return fmt.Errorf("decode bundled file %q: %w", relPath, err)
		}

		// Sanitize path to prevent directory traversal and absolute paths.
		normalized := strings.ReplaceAll(relPath, "\\", "/")
		clean := filepath.Clean(filepath.FromSlash(normalized))
		if isUnsafeBundledFilePath(normalized, clean) {
			return fmt.Errorf("unsafe bundled file path %q", relPath)
		}

		dest := filepath.Join(skillDirAbs, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil {
			return fmt.Errorf("resolve bundled file %q: %w", relPath, err)
		}
		// Double-check the resolved path is still under skillDir.
		relToRoot, err := filepath.Rel(skillDirAbs, destAbs)
		if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
			return fmt.Errorf("unsafe bundled file path %q", relPath)
		}
		decodedFiles = append(decodedFiles, bundledSkillFile{
			OriginalPath: relPath,
			RelDir:       filepath.Dir(relToRoot),
			DestAbs:      destAbs,
			Data:         data,
		})
	}
	if len(decodedFiles) == 0 {
		return fmt.Errorf("downloaded skill package contained no writable files")
	}

	rootCreated := false
	if !rootExists {
		parent := filepath.Dir(skillDirAbs)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		if err := os.Mkdir(skillDirAbs, 0o755); err != nil {
			if !os.IsExist(err) {
				return err
			}
		} else {
			rootCreated = true
		}
	}
	if info, err := os.Lstat(skillDirAbs); err != nil {
		return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, fmt.Errorf("unsafe skill directory %q", skillDir))
	}

	for _, file := range decodedFiles {
		if err := ensureBundledParentDir(skillDirAbs, file.RelDir); err != nil {
			return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, fmt.Errorf("create directory for bundled file %q: %w", file.OriginalPath, err))
		}
		if info, err := os.Lstat(file.DestAbs); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
				return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, fmt.Errorf("unsafe bundled file target %q", file.OriginalPath))
			}
		} else if !os.IsNotExist(err) {
			return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, fmt.Errorf("inspect bundled file target %q: %w", file.OriginalPath, err))
		}
	}

	for _, file := range decodedFiles {
		if err := os.WriteFile(file.DestAbs, file.Data, 0o644); err != nil {
			return cleanupNewBundledSkillDirOnError(skillDirAbs, rootCreated, fmt.Errorf("write bundled file %q: %w", file.OriginalPath, err))
		}
	}
	return nil
}

func cleanupNewBundledSkillDirOnError(skillDirAbs string, rootCreated bool, err error) error {
	if err != nil && rootCreated {
		_ = os.RemoveAll(skillDirAbs)
	}
	return err
}

type bundledSkillFile struct {
	OriginalPath string
	RelDir       string
	DestAbs      string
	Data         []byte
}

func isUnsafeBundledFilePath(normalized, clean string) bool {
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(normalized, "/") {
		return true
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return true
		}
		// Windows drive letters and alternate data streams both use ':'. Treat
		// colon as non-portable for bundled skill paths on every platform.
		if strings.Contains(part, ":") {
			return true
		}
	}
	return false
}

func ensureBundledParentDir(rootAbs, relDir string) error {
	relDir = filepath.Clean(relDir)
	if relDir == "." || relDir == "" {
		return nil
	}
	if relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) || filepath.IsAbs(relDir) {
		return fmt.Errorf("unsafe parent directory %q", relDir)
	}
	current := rootAbs
	for _, part := range strings.Split(relDir, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("parent directory %q is a symlink", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("parent path %q is not a directory", current)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// installDependencies attempts to install declared runtime dependencies.
// Each install command has a 60-second timeout. Returns an error if any
// dependency fails; individual failures are tolerated.
func (c *SkillHubClient) installDependencies(deps []hubSkillDependency) error {
	var failures []string
	for _, dep := range deps {
		var args []string
		var bin string
		switch dep.Type {
		case "pip":
			bin = "pip"
			pkg := dep.Name
			if dep.Version != "" {
				pkg += dep.Version // e.g. "requests>=2.0"
			}
			args = []string{"install", pkg}
		case "npm":
			bin = "npm"
			pkg := dep.Name
			if dep.Version != "" {
				pkg += "@" + dep.Version
			}
			args = []string{"install", "-g", pkg}
		case "binary":
			// Just check if binary exists; can't auto-install.
			if _, err := exec.LookPath(dep.Name); err != nil {
				failures = append(failures, fmt.Sprintf("binary %q not found", dep.Name))
			}
			continue
		case "brew":
			if runtime.GOOS != "darwin" {
				continue
			}
			bin = "brew"
			args = []string{"install", dep.Name}
		case "apt":
			if runtime.GOOS != "linux" {
				continue
			}
			bin = "apt-get"
			args = []string{"install", "-y", dep.Name}
		default:
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, bin, args...)
		hideCommandWindow(cmd)
		err := cmd.Run()
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s %s: %v", dep.Type, dep.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("dependency install failures: %s", strings.Join(failures, "; "))
	}
	return nil
}

// CheckUpdate checks whether a Skill has a newer version available on the hub.
func (c *SkillHubClient) CheckUpdate(ctx context.Context, skillID string, currentVersion string) (*HubSkillMeta, error) {
	path := "/api/v1/skills/" + url.PathEscape(skillID)
	var item hubSkillItem
	base, _, err := c.getJSON(ctx, path, &item)
	if err != nil {
		return nil, nil
	}

	if item.Version == "" || item.Version == currentVersion {
		return nil, nil
	}

	meta := hubItemToMeta(item, base)
	return &meta, nil
}

// Rate submits a rating for a skill to the hub.
func (c *SkillHubClient) Rate(ctx context.Context, skillID string, maclawID string, score int) error {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]interface{}{
		"maclaw_id": maclawID,
		"score":     score,
	})

	endpoint := base + "/api/v1/skills/" + url.PathEscape(skillID) + "/rate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d rating skill %s", resp.StatusCode, skillID)
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

// Publish publishes a local skill to the hub's SkillHub.
func (c *SkillHubClient) Publish(ctx context.Context, full hubSkillFull) error {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(full)
	if err != nil {
		return err
	}

	endpoint := base + "/api/v1/skills"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d publishing skill", resp.StatusCode)
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

// RefreshRecommendations fetches popular skills and caches them.
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	var items []hubSkillItem
	base, _, err := c.getJSON(ctx, "/api/v1/skills/popular", &items)
	if err != nil {
		return err
	}

	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, hubItemToMeta(item, base))
	}

	c.mu.Lock()
	c.recIndex = skills
	c.mu.Unlock()
	return nil
}

// GetRecommendations returns the locally cached recommendation list (thread-safe).
func (c *SkillHubClient) GetRecommendations() []HubSkillMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]HubSkillMeta, len(c.recIndex))
	copy(result, c.recIndex)
	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (c *SkillHubClient) getJSON(ctx context.Context, path string, dest interface{}) (string, []string, error) {
	return c.app.getHubCenterJSON(ctx, c.client, path, maxSearchJSONSize, dest)
}

func (c *SkillHubClient) getJSONFromExplicitHubURL(ctx context.Context, hubURL string, path string, dest interface{}) (string, []string, error) {
	base := strings.TrimSpace(hubURL)
	if base == "" {
		return c.getJSON(ctx, path, dest)
	}
	if authHeader := c.enterpriseHubAuthHeaderForBase(base); authHeader != "" {
		return c.getJSONFromExplicitHubURLWithAuth(ctx, base, path, dest, authHeader)
	}
	return c.app.getHubCenterJSONFromCandidates(ctx, c.client, []string{base}, path, maxSearchJSONSize, dest)
}

func (c *SkillHubClient) getBytesFromExplicitHubURL(ctx context.Context, hubURL string, path string, limit int64) (string, []string, []byte, error) {
	base := strings.TrimSpace(hubURL)
	// Use installClient (long timeout) for multi-asset package downloads.
	downloadClient := c.installClient
	if downloadClient == nil {
		downloadClient = c.client
	}
	if base == "" {
		return c.app.getHubCenterBytes(ctx, downloadClient, path, limit)
	}
	if authHeader := c.enterpriseHubAuthHeaderForBase(base); authHeader != "" {
		return c.getBytesFromExplicitHubURLWithAuth(ctx, base, path, limit, authHeader)
	}
	base = strings.TrimRight(base, "/")
	return c.app.getHubCenterBytesFromCandidates(ctx, downloadClient, []string{base}, path, limit)
}

func (c *SkillHubClient) getBytesFromExplicitHubURLWithAuth(ctx context.Context, base, path string, limit int64, authHeader string) (string, []string, []byte, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", "MaClaw/1.0")
	// Use installClient (long timeout) for multi-asset package downloads.
	httpClient := c.installClient
	if httpClient == nil {
		httpClient = c.client
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", []string{base}, nil, fmt.Errorf("request hub bytes %s%s failed (%d): %s", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := readLimitedHubCenterBodyWithLength(resp.Body, resp.ContentLength, limit)
	if err != nil {
		return "", []string{base}, nil, fmt.Errorf("read hub bytes %s%s failed: %w", base, path, err)
	}
	return base, []string{base}, data, nil
}

func (c *SkillHubClient) enterpriseHubAuthHeaderForBase(base string) string {
	if c == nil || c.app == nil {
		return ""
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	configured := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if configured == "" || !strings.EqualFold(configured, base) {
		return ""
	}
	if token := strings.TrimSpace(cfg.RemoteViewerToken); token != "" {
		return "Bearer " + token
	}
	return ""
}

func (c *SkillHubClient) getJSONFromExplicitHubURLWithAuth(ctx context.Context, base, path string, dest interface{}, authHeader string) (string, []string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", []string{base}, fmt.Errorf("request hub JSON %s%s failed (%d): %s", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := readHubCenterJSONBody(resp.Body, maxSearchJSONSize)
	if err != nil {
		return "", []string{base}, fmt.Errorf("read hub JSON %s%s failed: %w", base, path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return "", []string{base}, fmt.Errorf("decode hub JSON %s%s failed after %d bytes: %w", base, path, len(data), err)
	}
	return base, []string{base}, nil
}

func (c *SkillHubClient) cacheResults(query string, results []HubSkillMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.cache) >= maxCacheEntries {
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expiresAt) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= maxCacheEntries {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range c.cache {
				if oldestKey == "" || v.expiresAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.expiresAt
				}
			}
			delete(c.cache, oldestKey)
		}
	}
	c.cache[query] = cachedSearchResult{
		results:   results,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
}
