package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// syncSkillHubTools registers the search_and_install_skill tool when a
// SkillMarket (HubCenter) is reachable, giving the LLM the ability to
// proactively search the SkillMarket and install skills during a session.
func (h *IMMessageHandler) syncSkillHubTools() {
	if h.registry == nil {
		return
	}
	// The tool is available as long as we have an App (which provides HubCenter URL).
	hasApp := h.app != nil
	_, hasSearchTool := h.registry.Get("search_and_install_skill")

	if hasApp && !hasSearchTool {
		h.registry.Register(RegisteredTool{
			Name: "search_and_install_skill",
			Description: "Search and install a skill from SkillMarket when existing tools cannot fulfill the request. " +
				"Search and install a skill from SkillMarket when existing tools cannot fulfill the request.",
			Category: ToolCategoryBuiltin,
			Tags:     []string{"skill", "skillmarket", "install", "search", "capability"},
			Status:   RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "Search query describing the capability you need."},
			},
			Required: []string{"query"},
			HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
				return h.toolSearchAndInstallSkill(args, onProgress)
			},
		})
	} else if !hasApp && hasSearchTool {
		h.registry.Unregister("search_and_install_skill")
	}
}

// toolSearchAndInstallSkill handles the search_and_install_skill tool call.
// Search order: SkillMarket (HubCenter) 鈫?ClawHub mirror 鈫?GitHub.
// If a match is found, it downloads and registers the skill locally.
func (h *IMMessageHandler) toolSearchAndInstallSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "閿欒: 缂哄皯 query 鍙傛暟"
	}

	sendStatus := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	ctx := context.Background()

	smClient := NewSkillMarketClient(h.app)
	searcher := NewSkillSearcher(smClient)

	sendStatus("馃攳 姝ｅ湪鎼滅储 SkillMarket...")
	best, err := searcher.SearchAndInstall(ctx, query)
	if err != nil {
		return fmt.Sprintf("鎼滅储 SkillMarket 澶辫触: %v", err)
	}
	if best == nil {
		return fmt.Sprintf("鍦?SkillMarket銆丆lawHub 鍜?GitHub 涓婂潎鏈壘鍒颁笌 %q 鍖归厤鐨?Skill", query)
	}

	sendStatus(fmt.Sprintf("馃摝 鎵惧埌 Skill: %s 鈥?%s (鏉ユ簮: %s)", best.Name, best.Description, best.Status))

	// Read platform/userID from the active loop context (valid during agent loop).
	platform := ""
	if h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}
	return h.installAndExecuteSkill(ctx, best, query, platform, h.lastUserID, sendStatus).Text
}

// installAndExecuteSkill handles the download, security review, registration,
// and execution of a found skill. Shared by both active (tool call) and
// passive (capability gap) paths.
//
// platform and userID are passed explicitly for the same reason as
// registerAndExecuteSkill 鈥?async callers must capture these before
// the agent loop's defer clears currentLoopCtx.
func (h *IMMessageHandler) installAndExecuteSkill(ctx context.Context, best *SkillSearchResult, query, platform, userID string, sendStatus func(string)) skillInstallExecutionResult {
	// GitHub result 鈫?import via a stable install ref when available.
	if best.SourceKind() == skillSearchSourceGitHub {
		var imported *corelib.NLSkillEntry
		if strings.TrimSpace(best.InstallRef) != "" {
			var candidate cskill.GitHubSkillCandidate
			if err := json.Unmarshal([]byte(best.InstallRef), &candidate); err == nil && strings.TrimSpace(candidate.RawURL) != "" {
				imported, err = cskill.NewGitHubSearcher("").ImportFromCandidate(candidate)
			}
		}
		if imported == nil {
			gs := cskill.NewGitHubSearcher("")
			candidates, err := gs.SearchGitHub(best.ID)
			if err != nil || len(candidates) == 0 {
				return skillInstallExecutionResult{Text: fmt.Sprintf("GitHub skill import failed: %v", err)}
			}
			imported, err = gs.ImportFromCandidate(candidates[0])
			if err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("GitHub skill import failed: %v", err)}
			}
		}
		imported.Source = "auto_github"
		return h.registerAndExecuteSkill(ctx, imported, best.Name, "auto_github", platform, userID, sendStatus)
	}

	// SkillMarket or ClawHub result 鈫?download and register locally.
	sendStatus(fmt.Sprintf("猬囷笍 姝ｅ湪瀹夎: %s ...", best.Name))

	if best.SourceKind() == skillSearchSourceClawHub {
		skill, dlErr := downloadClawHubSkill(ctx, best.ID)
		if dlErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Found ClawHub skill %s but download failed: %v", best.Name, dlErr)}
		}
		skill.Source = "auto_clawhub"
		return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_clawhub", platform, userID, sendStatus)
	}

	// SkillMarket result: download through the HubCenter failover pool.
	skill, dlErr := downloadSkillJSONFromHubCenter(ctx, h.app, "/api/v1/skills/"+url.PathEscape(best.ID)+"/download")
	if dlErr != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but download failed: %v", best.Name, dlErr)}
	}
	skill.Source = "auto_hub"
	return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_hub", platform, userID, sendStatus)
}

// downloadClawHubSkill fetches a skill from the ClawHub mirror and converts
// the SKILL.md content into an NLSkillEntry with a single craft_tool step
// that uses the SKILL.md as instructions.
// Delegates to the shared corelib/skill.HubClient.
func downloadClawHubSkill(ctx context.Context, slug string) (*corelib.NLSkillEntry, error) {
	client := cskill.DefaultHubClient()
	return client.DownloadClawHub(ctx, slug)
}

// downloadSkillJSON fetches a skill definition from the given URL and
// converts it to an NLSkillEntry ready for local registration.
// Handles both step-based skills (steps array) and file-based skills
// (files map with SKILL.md in base64). All bundled files are extracted
// to ~/.maclaw/data/skills/<name>/.
func downloadSkillJSON(ctx context.Context, endpoint string) (*corelib.NLSkillEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var full struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Triggers    []string          `json:"triggers"`
		TrustLevel  string            `json:"trust_level"`
		Version     string            `json:"version"`
		Steps       []json.RawMessage `json:"steps"`
		Files       map[string]string `json:"files"` // path 鈫?base64 content
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 5<<20)).Decode(&full); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var steps []corelib.NLSkillStep

	if len(full.Steps) > 0 {
		for _, raw := range full.Steps {
			var s struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			}
			if err := json.Unmarshal(raw, &s); err == nil && s.Action != "" {
				steps = append(steps, corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError})
			}
		}
	}

	// Extract all bundled files to ~/.maclaw/data/skills/<name>/.
	installSkillDir := ""
	if len(full.Files) > 0 && full.Name != "" {
		extractSkillFiles(full.Name, full.Files, "")
		if skillsRoot, err := cskill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, full.Name)
		}
	}

	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("skill %s has no steps and no SKILL.md", full.Name)
	}

	// Skills from the configured hub (official store) are treated as "trusted".
	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		Name:        full.Name,
		Description: full.Description,
		Triggers:    full.Triggers,
		Steps:       steps,
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "hub",
		HubSkillID:  full.ID,
		HubVersion:  full.Version,
		TrustLevel:  trustLevel,
	}, nil
}

// extractSkillFiles decodes base64-encoded files and writes them to the
// specified targetDir, preserving subdirectory structure.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<skillName>/.
func extractSkillFiles(skillName string, files map[string]string, targetDir string) {
	skillDir := targetDir
	if skillDir == "" {
		skillsRoot, err := cskill.PrimarySkillsDir()
		if err != nil {
			log.Printf("[skill-install] cannot determine skills dir: %v", err)
			return
		}
		skillDir = filepath.Join(skillsRoot, skillName)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		log.Printf("[skill-install] cannot create %s: %v", skillDir, err)
		return
	}

	for relPath, b64Content := range files {
		data, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			log.Printf("[skill-install] decode %s: %v", relPath, err)
			continue
		}

		// Sanitize path 鈥?prevent directory traversal.
		clean := filepath.ToSlash(filepath.Clean(relPath))
		if strings.Contains(clean, "..") || filepath.IsAbs(relPath) || strings.HasPrefix(clean, "/") {
			log.Printf("[skill-install] skipping unsafe path: %s", relPath)
			continue
		}

		dest := filepath.Join(skillDir, filepath.FromSlash(clean))
		if !strings.HasPrefix(dest, skillDir+string(filepath.Separator)) && dest != skillDir {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			log.Printf("[skill-install] write %s: %v", dest, err)
		}
	}
	log.Printf("[skill-install] extracted %d files to %s", len(files), skillDir)
}

// registerAndExecuteSkill registers a skill locally, runs security review,
// registerAndExecuteSkill registers a skill locally, runs security review,
// executes it, and returns the result string.
//
// platform and userID are passed explicitly (not read from h.currentLoopCtx)
// because this function may be called from async goroutines where
// currentLoopCtx has already been cleared by the agent loop's defer.
func (h *IMMessageHandler) registerAndExecuteSkill(ctx context.Context, skill *corelib.NLSkillEntry, displayName, source string, platform, userID string, sendStatus func(string)) skillInstallExecutionResult {
	if h.getSkillExecutor() == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but SkillExecutor is not initialized", displayName)}
	}

	// Security review: staging + intelligent scan.
	// Developer mode: skip security review entirely.
	if !h.isSecurityDeveloperMode() {
		scanner := NewSkillSecurityScanner(h.app, nil)
		scanReport := scanner.ScanStaged(ctx, skill, skill.SkillDir, sendStatus)

		if scanReport.IsDangerous() {
			confirmed := h.confirmCriticalRiskSkill(
				ctx, displayName, source, scanReport.PatternAssessment.Factors, platform, userID,
			)
			if !confirmed {
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     source + "_skill_install",
						RiskLevel:    security.RiskCritical,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected critical skill %s: %s", displayName, scanReport.Summary),
					})
				}
				return skillInstallExecutionResult{Text: FormatScanReportForUser(scanReport, displayName) +
					fmt.Sprintf("\nSkill %s was rejected and not installed.", displayName)}
			}
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     source + "_skill_install",
					RiskLevel:    security.RiskCritical,
					PolicyAction: security.PolicyUserOverride,
					Result:       fmt.Sprintf("user confirmed critical skill %s, scanned_by=%s", displayName, scanReport.ScannedBy),
				})
			}
		} else if scanReport.NeedsUserReview() {
			confirmed := h.confirmCriticalRiskSkill(
				ctx, displayName, source, scanReport.PatternAssessment.Factors, platform, userID,
			)
			if !confirmed {
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     source + "_skill_install",
						RiskLevel:    security.RiskHigh,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected high-risk skill %s: %s", displayName, scanReport.Summary),
					})
				}
				return skillInstallExecutionResult{Text: FormatScanReportForUser(scanReport, displayName) +
					fmt.Sprintf("\nSkill %s was rejected and not installed.", displayName)}
			}
		}
	}

	sendStatus(fmt.Sprintf("Registering Skill: %s ...", skill.Name))
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Registering Skill %s failed: %v", displayName, err)}
	}

	// Refresh skill BM25 index so the router picks up the new skill.
	if h.getAppToolRouter() != nil {
		h.getAppToolRouter().RefreshSkillIndex()
	}

	// Audit log.
	_ = h.getAuditLog() // ensure
	if h.getAuditLog() != nil {
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     source + "_skill_install",
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s from %s", displayName, source),
		})
	}

	sendStatus(fmt.Sprintf("鈻讹笍 姝ｅ湪鎵ц Skill: %s ...", skill.Name))
	execResult, execErr := h.getSkillExecutor().Execute(skill.Name)
	if execErr != nil {
		log.Printf("[skill-auto] execute skill %s failed: %v", skill.Name, execErr)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed but execution failed: %v", skill.Name, execErr)}
	}
	return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed and executed.\n%s", skill.Name, execResult), Success: true}
}
