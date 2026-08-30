package agentservice

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

type SkillSearchInput struct {
	Query            string   `json:"query"`
	Sources          []string `json:"sources,omitempty"`
	TopN             int      `json:"top_n,omitempty"`
	SkillHubURL      string   `json:"skill_hub_url,omitempty"`
	SkillMarketURL   string   `json:"skill_market_url,omitempty"`
	GitHubToken      string   `json:"github_token,omitempty"`
	IncludeInstalled bool     `json:"include_installed,omitempty"`
}

type SkillSearchResult struct {
	Source         string   `json:"source"`
	ID             string   `json:"id,omitempty"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Version        string   `json:"version,omitempty"`
	Author         string   `json:"author,omitempty"`
	TrustLevel     string   `json:"trust_level,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Downloads      int      `json:"downloads,omitempty"`
	AvgRating      float64  `json:"avg_rating,omitempty"`
	Price          int      `json:"price,omitempty"`
	RepoFullName   string   `json:"repo_full_name,omitempty"`
	RepoURL        string   `json:"repo_url,omitempty"`
	RawURL         string   `json:"raw_url,omitempty"`
	FilePath       string   `json:"file_path,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	DefinitionType string   `json:"definition_type,omitempty"`
	Installed      bool     `json:"installed,omitempty"`
}

type SkillInstallInput struct {
	Source         string `json:"source"`
	RepoURL        string `json:"repo_url,omitempty"`
	RawURL         string `json:"raw_url,omitempty"`
	RepoFullName   string `json:"repo_full_name,omitempty"`
	FilePath       string `json:"file_path,omitempty"`
	Branch         string `json:"branch,omitempty"`
	DefinitionType string `json:"definition_type,omitempty"`
	ZipBase64      string `json:"zip_base64,omitempty"`
	SkillHubURL    string `json:"skill_hub_url,omitempty"`
	SkillMarketURL string `json:"skill_market_url,omitempty"`
	SkillID        string `json:"skill_id,omitempty"`
	Overwrite      bool   `json:"overwrite,omitempty"`
	GitHubToken    string `json:"github_token,omitempty"`
}

type SkillImportInput struct {
	ZipBase64   string `json:"zip_base64"`
	Overwrite   bool   `json:"overwrite,omitempty"`
	ArchiveName string `json:"archive_name,omitempty"`
}

type SkillExportResult struct {
	Name          string `json:"name"`
	FileName      string `json:"file_name"`
	ArchiveBase64 string `json:"archive_base64"`
	SizeBytes     int64  `json:"size_bytes"`
}

type SkillValidateResult struct {
	Report      *skill.PortabilityReport `json:"report"`
	SummaryText string                   `json:"summary_text,omitempty"`
}

type SkillImproveInput struct {
	AutoFix bool `json:"auto_fix,omitempty"`
}

type SkillImproveResult struct {
	ReportBefore *skill.PortabilityReport  `json:"report_before,omitempty"`
	Changes      []skill.PortabilityChange `json:"changes,omitempty"`
	ReportAfter  *skill.PortabilityReport  `json:"report_after,omitempty"`
	SummaryText  string                    `json:"summary_text,omitempty"`
}

type SkillUploadInput struct {
	SkillMarketURL string `json:"skill_market_url,omitempty"`
	Email          string `json:"email"`
	AuthToken      string `json:"auth_token,omitempty"`
}

type SkillUploadResult struct {
	SubmissionID string `json:"submission_id"`
	Status       string `json:"status"`
}

type SkillSubmissionStatus struct {
	Status   string `json:"status"`
	ErrorMsg string `json:"error_msg,omitempty"`
}

type SkillMarketAccount struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	Credits           int64  `json:"credits"`
	SettledCredits    int64  `json:"settled_credits"`
	PendingSettlement int64  `json:"pending_settlement"`
	VoucherCount      int    `json:"voucher_count"`
}

type skillHubSearchResponse struct {
	Skills []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Version     string   `json:"version"`
		Author      string   `json:"author"`
		TrustLevel  string   `json:"trust_level"`
		Downloads   int      `json:"downloads"`
		AvgRating   float64  `json:"avg_rating"`
	} `json:"skills"`
}

type skillHubDownloadEnvelope struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Triggers     []string              `json:"triggers"`
	Version      string                `json:"version"`
	TrustLevel   string                `json:"trust_level"`
	Source       string                `json:"source,omitempty"`
	Steps        []corelib.NLSkillStep `json:"steps,omitempty"`
	Type         string                `json:"type,omitempty"`
	Content      string                `json:"content,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty"`
	AgentSkillMD string                `json:"agent_skill_md,omitempty"`
}

type skillMarketSearchResponse struct {
	Results []struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Price       int     `json:"price"`
		AvgRating   float64 `json:"avg_rating"`
		Downloads   int     `json:"downloads"`
		Author      string  `json:"author"`
	} `json:"results"`
}

var invalidSkillDirChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

// skillHubJSONMaxBytes caps skill *download/install* payloads (base64 file maps).
// Multi-asset packages need far more than 5 MiB once files are base64-encoded.
const skillHubJSONMaxBytes = skill.MaxSkillPackageDownloadBytes

func (s *Service) userSkillsRoot(tenantID, userID string) string {
	return filepath.Join(s.userRoot(tenantID, userID), "skills")
}

// UserSkillsRoot returns the on-disk skills directory for a principal.
// Hosts (e.g. Hub mobile) use this to seed or inspect installed skills.
func (s *Service) UserSkillsRoot(tenantID, userID string) string {
	return s.userSkillsRoot(tenantID, userID)
}

func (s *Service) ListSkills(ctx context.Context, p Principal) ([]corelib.NLSkillEntry, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	root, err := s.ensureUserSkillsRoot(p)
	if err != nil {
		return nil, err
	}
	items := skill.ScanSkillDirAll(root)
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func (s *Service) GetSkill(ctx context.Context, p Principal, name string) (*corelib.NLSkillEntry, error) {
	_ = ctx
	entry, _, err := s.findSkill(p, name)
	if err != nil {
		return nil, err
	}
	copy := entry
	return &copy, nil
}

func (s *Service) DeleteSkill(ctx context.Context, p Principal, name string) error {
	if s == nil {
		return fmt.Errorf("service is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.skillInstallTxnMu.Lock()
	defer s.skillInstallTxnMu.Unlock()
	if s.skillInstallRecoveryBlocked {
		// A transient startup failure must be retried under the same transaction
		// lock; keep the operation fail-closed if recovery still cannot prove
		// ownership or completion.
		s.skillInstallRecoveryBlocked = false
	}
	if _, pending, err := s.recoverAgentSkillInstallCompensations(); err != nil {
		s.skillInstallRecoveryBlocked = true
		return fmt.Errorf("recover pending skill compensation: %w", err)
	} else if pending > 0 {
		s.skillInstallRecoveryBlocked = true
		return fmt.Errorf("pending skill compensation requires review")
	}
	entry, dir, err := s.findSkill(p, name)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("skill directory not found")
	}
	quarantine := filepath.Join(filepath.Dir(dir), fmt.Sprintf(".skill-delete-pending-%d", time.Now().UnixNano()))
	requestID := fmt.Sprintf("agent_skill_delete_%d", time.Now().UnixNano())
	stableID := skillStableID(entry)
	previousContract, hadPreviousContract := s.dynamicCapabilities.ResolveSkillDynamicContract(ctx, p, stableID)
	contractSnapshotKey := "skill_contract|" + p.TenantID + "|" + p.UserID + "|" + stableID
	contractSnapshotPayload := ""
	if hadPreviousContract {
		contractSnapshotPayload, err = encodeSkillContractExternalSnapshot(p, stableID, previousContract, requestID, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("prepare Skill contract compensation snapshot: %w", err)
		}
	}
	contractRevoked := false
	committer := &skill.SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return skill.ScanSkillDirAll(filepath.Dir(dir)) },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		// Delete is a directory lifecycle operation. The directory is moved to
		// quarantine and removed after final audit; it must not invoke the
		// default YAML writer against the now-absent original path.
		DefinitionWriter: func(*corelib.NLSkillEntry) error { return nil },
		EntriesMutator: func(items []corelib.NLSkillEntry) ([]corelib.NLSkillEntry, error) {
			filtered := make([]corelib.NLSkillEntry, 0, len(items))
			for _, item := range items {
				if item.Name == entry.Name || item.MatchesName(entry.Name) || filepath.Clean(item.SkillDir) == filepath.Clean(dir) {
					continue
				}
				filtered = append(filtered, item)
			}
			return filtered, nil
		},
		IndexRefresher: func() error { return nil },
		FinalAuditor: func(event string, data map[string]string) error {
			// Contract revocation is completed in ExternalCommit, before this
			// final-audit callback. Keeping this callback audit-only ensures a
			// successful skill.deleted event is never emitted before the
			// authorization fence has crossed, while rollback can restore both
			// directory and contract if audit persistence fails.
			return s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: event, ResourceType: "skill", ResourceID: entry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: data})
		},
		ConfigRevision: "agentservice-skill-delete-v1",
		CompensationMutator: func(record *skill.EvolutionCompensationRecord) {
			record.SetRecoveryScope(s.dataRoot)
			record.SetDirectoryMoves([]skill.EvolutionDirectoryMove{{OriginalPath: dir, BackupPath: quarantine, HadPrevious: true}})
			record.SetAffectedSkills([]string{entry.Name})
			if hadPreviousContract {
				record.SetExternalSnapshot(contractSnapshotKey, contractSnapshotPayload)
			}
		},
		ExternalCommitWithCompensation: func(record *skill.EvolutionCompensationRecord) error {
			if err := skill.RetryDirectoryRename(dir, quarantine); err != nil {
				return fmt.Errorf("quarantine skill %s: %w", entry.Name, err)
			}
			moves := []skill.EvolutionDirectoryMove{{OriginalPath: dir, BackupPath: quarantine, HadPrevious: true, Moved: true}}
			record.SetDirectoryMoves(moves)
			if err := skill.ReplaceEvolutionCompensation(*record); err != nil {
				return err
			}
			// Revoke while the directory is quarantined, before any index or
			// audit success can make the deletion visible. ExternalRollback
			// restores the contract and directory on every later failure.
			// Mark the transition before calling the registry: a registry failure
			// may have applied the revoke before returning an error.
			contractRevoked = hadPreviousContract
			if contractRevoked {
				record.MarkExternalApplied(contractSnapshotKey, true)
				if err := skill.ReplaceEvolutionCompensation(*record); err != nil {
					return err
				}
			}
			if err := s.revokeSkillDynamicContract(p, stableID); err != nil {
				return err
			}
			return nil
		},
		ExternalRollback: func() error {
			if _, statErr := os.Stat(quarantine); os.IsNotExist(statErr) {
				if contractRevoked && hadPreviousContract {
					if err := s.dynamicCapabilities.PublishSkillContract(p, stableID, previousContract); err != nil {
						return fmt.Errorf("restore Skill dynamic capability contract: %w", err)
					}
					contractRevoked = false
				}
				return nil
			}
			if err := skill.RetryDirectoryRename(quarantine, dir); err != nil {
				return err
			}
			if contractRevoked && hadPreviousContract {
				if err := s.dynamicCapabilities.PublishSkillContract(p, stableID, previousContract); err != nil {
					return fmt.Errorf("restore Skill dynamic capability contract: %w", err)
				}
				contractRevoked = false
			}
			return nil
		},
		PostCommitCleanup: func() error {
			if err := os.RemoveAll(quarantine); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove deleted skill quarantine: %w", err)
			}
			return nil
		},
	}
	result := committer.Commit(skill.WithEvolutionRequestMetadata(ctx, requestID, 1), entry.Name, &entry, "skill.deleted", map[string]string{
		// Keep deletion under the AgentService install/recovery namespace so
		// NewService startup recovery cannot miss a crash-window quarantine.
		"skill": entry.Name, "action": "agentservice_install_delete", "decision": "applied", "request_id": requestID,
		"attempt": "1", "config_revision": "agentservice-skill-delete-v1", "schema_version": "2", "evidence_mode": "none",
		"external_state": "dynamic_skill_contract", "contract_revocation": map[bool]string{true: "applied", false: "none"}[hadPreviousContract],
	})
	if result.State != "committed" || result.CleanupStatus != "clear" {
		return fmt.Errorf("skill delete not committed: state=%s cleanup_status=%s reason=%s", result.State, result.CleanupStatus, result.FailureReason)
	}
	return nil
}

func (s *Service) SearchSkills(ctx context.Context, p Principal, in SkillSearchInput) ([]SkillSearchResult, error) {
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	topN := in.TopN
	if topN <= 0 {
		topN = 10
	}
	sources := normalizeSkillSearchSources(in.Sources)

	// Apply source control filter: remove disallowed sources before searching.
	if s.SkillSourceFilter != nil {
		allowed := s.SkillSourceFilter(p.TenantID, p.UserID)
		if allowed != nil {
			if len(allowed) == 0 {
				return nil, nil
			}
			sources = filterAllowedSources(sources, allowed)
			if len(sources) == 0 {
				return nil, nil // all requested sources are blocked
			}
		}
	}

	installed := map[string]bool{}
	if in.IncludeInstalled {
		if skills, err := s.ListSkills(ctx, p); err == nil {
			for _, item := range skills {
				installed[strings.ToLower(strings.TrimSpace(item.Name))] = true
			}
		}
	}
	items := make([]SkillSearchResult, 0)
	seen := map[string]bool{}
	add := func(item SkillSearchResult) {
		key := strings.ToLower(item.Source + ":" + strings.TrimSpace(item.ID) + ":" + strings.TrimSpace(item.Name) + ":" + strings.TrimSpace(item.RawURL) + ":" + strings.TrimSpace(item.RepoURL))
		if seen[key] {
			return
		}
		if in.IncludeInstalled {
			item.Installed = installed[strings.ToLower(strings.TrimSpace(item.Name))]
		}
		seen[key] = true
		items = append(items, item)
	}
	for _, source := range sources {
		switch source {
		case "github":
			gs := skill.NewGitHubSearcher(strings.TrimSpace(in.GitHubToken))
			found, err := gs.SearchGitHub(query)
			if err != nil {
				continue
			}
			for _, item := range found {
				add(SkillSearchResult{Source: "github", Name: inferSkillNameFromGitHub(item), Description: item.Description, RepoFullName: item.RepoFullName, RepoURL: item.RepoURL, RawURL: item.RawURL, FilePath: item.FilePath, Branch: item.Branch, DefinitionType: item.DefinitionType, Downloads: item.Stars})
			}
		case "skillhub":
			baseURLs, err := s.resolveUserHubCenterBaseURLs(p, in.SkillHubURL)
			if err != nil {
				continue
			}
			found, err := searchSkillHubCandidates(ctx, baseURLs, query, topN)
			if err != nil {
				continue
			}
			for _, item := range found {
				add(item)
			}
		case "clawhub":
			found := skill.DefaultHubClient().SearchClawHub(ctx, query)
			for i, item := range found {
				if i >= topN {
					break
				}
				add(SkillSearchResult{Source: item.Source, ID: item.ID, Name: item.Name, Description: item.Description, Version: item.Version, Author: item.Author, TrustLevel: item.TrustLevel, Downloads: item.Downloads, AvgRating: item.AvgRating})
			}
		case "skillmarket":
			baseURLs, err := s.resolveUserHubCenterBaseURLs(p, in.SkillMarketURL)
			if err != nil {
				continue
			}
			found, err := searchSkillMarketCandidates(ctx, baseURLs, query, topN)
			if err != nil {
				continue
			}
			for _, item := range found {
				add(item)
			}
		}
	}
	return items, nil
}

func (s *Service) ImportSkillArchive(ctx context.Context, p Principal, in SkillImportInput) ([]corelib.NLSkillEntry, error) {
	if strings.TrimSpace(in.ZipBase64) == "" {
		return nil, fmt.Errorf("zip_base64 is required")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ZipBase64))
	if err != nil {
		return nil, fmt.Errorf("decode zip_base64: %w", err)
	}
	return s.installSkillArchiveBytes(ctx, p, data, in.Overwrite)
}

func (s *Service) InstallSkill(ctx context.Context, p Principal, in SkillInstallInput) ([]corelib.NLSkillEntry, error) {
	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		return nil, fmt.Errorf("source is required")
	}

	// Check source control before downloading.
	if s.SkillSourceFilter != nil {
		allowed := s.SkillSourceFilter(p.TenantID, p.UserID)
		if allowed != nil {
			if len(allowed) == 0 {
				return nil, fmt.Errorf("%s", skill.FormatSourcePolicyDenied(source, allowed))
			}
			// Map install source names to the canonical source identifiers.
			canonical := installSourceToCanonical(source)
			if canonical != "" && !sourceAllowedByPolicy(canonical, allowed) {
				return nil, fmt.Errorf("%s", skill.FormatSourcePolicyDenied(source, allowed))
			}
		}
	}

	switch source {
	case "zip":
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ZipBase64))
		if err != nil {
			return nil, fmt.Errorf("decode zip_base64: %w", err)
		}
		return s.installSkillArchiveBytes(ctx, p, data, in.Overwrite)
	case "github_repo":
		gs := skill.NewGitHubSearcher(strings.TrimSpace(in.GitHubToken))
		entries, err := gs.ImportFromRepoURL(strings.TrimSpace(in.RepoURL))
		if err != nil {
			return nil, err
		}
		return s.persistImportedEntries(ctx, p, entries, in.Overwrite)
	case "github_candidate", "github":
		gs := skill.NewGitHubSearcher(strings.TrimSpace(in.GitHubToken))
		entry, err := gs.ImportFromCandidate(skill.GitHubSkillCandidate{RepoFullName: strings.TrimSpace(in.RepoFullName), RepoURL: strings.TrimSpace(in.RepoURL), RawURL: strings.TrimSpace(in.RawURL), FilePath: strings.TrimSpace(in.FilePath), Branch: strings.TrimSpace(in.Branch), DefinitionType: strings.TrimSpace(in.DefinitionType)})
		if err != nil {
			return nil, err
		}
		return s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{*entry}, in.Overwrite)
	case "skillhub":
		baseURLs, err := s.resolveUserHubCenterBaseURLs(p, in.SkillHubURL)
		if err != nil {
			return nil, err
		}
		entry, usedBaseURL, err := downloadSkillHubEntryCandidates(ctx, baseURLs, strings.TrimSpace(in.SkillID))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.SkillHubURL) == "" {
			s.rememberUserHubCenterSelection(p, usedBaseURL, baseURLs)
		}
		return s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{*entry}, in.Overwrite)
	case "clawhub":
		entry, err := skill.DefaultHubClient().DownloadClawHub(ctx, strings.TrimSpace(in.SkillID))
		if err != nil {
			return nil, err
		}
		return s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{*entry}, in.Overwrite)
	case "skillmarket", "market", "hubcenter", "hub_center":
		cfg, _ := s.getOrLoadUserConfig(p.TenantID, p.UserID)
		user, err := s.store.GetUser(p.TenantID, p.UserID)
		if err != nil {
			return nil, err
		}
		baseURLs, err := s.resolveUserHubCenterBaseURLs(p, in.SkillMarketURL)
		if err != nil {
			return nil, err
		}
		email := firstNonEmpty(user.Email, cfg.AppConfig.RemoteEmail)
		entry, usedBaseURL, err := s.downloadSkillMarketEntryCandidates(ctx, p, cfg, baseURLs, strings.TrimSpace(in.SkillID), email)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.SkillMarketURL) == "" {
			s.rememberUserHubCenterSelection(p, usedBaseURL, baseURLs)
		}
		return s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{*entry}, in.Overwrite)
	default:
		return nil, fmt.Errorf("unsupported skill install source %q", source)
	}
}

func (s *Service) skillMarketAuthToken(ctx context.Context, p Principal, cfg UserConfig, baseURL, email string) string {
	if token := strings.TrimSpace(cfg.AppConfig.SkillMarketSessionToken); token != "" {
		return token
	}
	userID := firstNonEmpty(cfg.AppConfig.RemoteUserID, cfg.UserID, p.UserID, email)
	if userID == "" || strings.TrimSpace(cfg.AppConfig.RemoteMachineID) == "" || strings.TrimSpace(cfg.AppConfig.RemoteViewerToken) == "" {
		return ""
	}
	result, err := remote.NewSkillMarketAuthClient().MachineLogin(ctx, strings.TrimRight(strings.TrimSpace(baseURL), "/"), cfg.AppConfig.RemoteHubID, userID, email, cfg.AppConfig.RemoteMachineID, cfg.AppConfig.RemoteViewerToken)
	if err != nil || result == nil || strings.TrimSpace(result.SessionToken) == "" {
		return ""
	}
	cfg.AppConfig.SkillMarketSessionToken = strings.TrimSpace(result.SessionToken)
	if cfg.TenantID == "" {
		cfg.TenantID = p.TenantID
	}
	if cfg.UserID == "" {
		cfg.UserID = p.UserID
	}
	cfg.AppConfig = effectiveLLMFlatConfig(cfg.AppConfig)
	_ = s.store.SaveUserConfig(cfg)
	_ = saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg)
	return cfg.AppConfig.SkillMarketSessionToken
}

func (s *Service) resolveUserHubCenterBaseURL(p Principal, explicit string) (string, error) {
	baseURLs, err := s.resolveUserHubCenterBaseURLs(p, explicit)
	if err != nil {
		return "", err
	}
	return baseURLs[0], nil
}

func (s *Service) resolveUserHubCenterBaseURLs(p Principal, explicit string) ([]string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		baseURLs := remote.NormalizeHubCenterURLs([]string{explicit})
		if len(baseURLs) == 0 {
			return nil, fmt.Errorf("hubcenter URL is not configured; activate remote HubCenter first")
		}
		return baseURLs, nil
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	// Unique enrollment algorithm — same as GUI/TUI HubCenterBaseURLs.
	baseURLs := remote.EffectiveHubCenterSeeds(
		cfg.AppConfig.RemoteHubCenterURL,
		cfg.AppConfig.RemoteHubCenterURLs,
		remote.DefaultRemoteHubCenterURLs,
	)
	if len(baseURLs) == 0 {
		return nil, fmt.Errorf("hubcenter URL is not configured; activate remote HubCenter first")
	}
	return baseURLs, nil
}

func (s *Service) rememberUserHubCenterSelection(p Principal, selected string, candidates []string) {
	selected = remote.NormalizeHubCenterURL(selected)
	if selected == "" || remote.IsLoopbackURL(selected) {
		return
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return
	}
	// Persist only valid public addresses that were actually used/discovered for
	// this selection — never expand with official DefaultRemoteHubCenterURLs.
	public := remote.RegisteredPublicHubCenterURLs(selected, candidates)
	if len(public) == 0 {
		return
	}
	cfg.AppConfig.RemoteHubCenterURL = selected
	cfg.AppConfig.RemoteHubCenterURLs = public
	cfg.UpdatedAt = s.now()
	_ = s.store.SaveUserConfig(cfg)
	_ = saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg)
}

func (s *Service) ExportSkill(ctx context.Context, p Principal, name string) (*SkillExportResult, error) {
	entry, dir, err := s.findSkill(p, name)
	if err != nil {
		return nil, err
	}
	if err := s.scanSkillForOutbound(ctx, p, entry, dir, "export"); err != nil {
		return nil, err
	}
	archive, err := zipDirectoryBytes(dir)
	if err != nil {
		return nil, err
	}
	fileName := normalizeSkillDirName(entry.Name)
	if fileName == "" {
		fileName = "skill"
	}
	return &SkillExportResult{Name: entry.Name, FileName: fileName + ".zip", ArchiveBase64: base64.StdEncoding.EncodeToString(archive), SizeBytes: int64(len(archive))}, nil
}

func (s *Service) scanSkillForOutbound(ctx context.Context, p Principal, entry corelib.NLSkillEntry, dir, phase string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scanEntry := entry
	scanEntry.SkillDir = dir
	report := skill.NewSecurityScanner(nil).ScanInstallStaged(ctx, &scanEntry, dir, nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	if report != nil && !report.NeedsUserReview() {
		return nil
	}
	level := security.RiskCritical
	summary := "security scan unavailable"
	if report != nil {
		level = report.FinalLevel
		summary = report.Summary
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "skill.rejected", ResourceType: "skill", ResourceID: entry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"phase": phase, "risk_level": string(level), "summary": summary}})
	return fmt.Errorf("skill %s blocked by security scan: level=%s summary=%s", phase, level, summary)
}

func (s *Service) ValidateSkill(ctx context.Context, p Principal, name string) (*SkillValidateResult, error) {
	_ = ctx
	_, dir, err := s.findSkill(p, name)
	if err != nil {
		return nil, err
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		return nil, err
	}
	return &SkillValidateResult{Report: report, SummaryText: skill.FormatPortabilityReport(report)}, nil
}

func (s *Service) ImproveSkill(ctx context.Context, p Principal, name string, in SkillImproveInput) (*SkillImproveResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	entry, dir, err := s.findSkill(p, name)
	if err != nil {
		return nil, err
	}
	before, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		return nil, err
	}
	result := &SkillImproveResult{ReportBefore: before, ReportAfter: before, SummaryText: skill.FormatPortabilityReport(before)}
	if !in.AutoFix {
		return result, nil
	}
	snapshot, err := zipDirectoryBytes(dir)
	if err != nil {
		return nil, fmt.Errorf("snapshot skill before improvement: %w", err)
	}
	changes, err := skill.AutoFixPortability(dir)
	if err != nil {
		return nil, err
	}
	after, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		return nil, err
	}
	improvedEntry, loadErr := loadImportedSkillEntry(dir)
	if loadErr != nil {
		if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
			return nil, fmt.Errorf("improved skill is no longer importable: %w; rollback failed: %v", loadErr, restoreErr)
		}
		return nil, fmt.Errorf("improved skill is no longer importable; changes rolled back: %w", loadErr)
	}
	if report, scanErr := scanImportedSkillBeforeInstall(ctx, improvedEntry, dir); scanErr != nil {
		s.recordSkillScanRejection(p, improvedEntry, report, scanErr)
		if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
			return nil, fmt.Errorf("skill improvement blocked by security scan: %w; rollback failed: %v", scanErr, restoreErr)
		}
		return nil, fmt.Errorf("skill improvement blocked by security scan and rolled back: %w", scanErr)
	}
	result.Changes = changes
	result.ReportAfter = after
	result.SummaryText = skill.FormatPortabilityReport(after)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "skill.improved", ResourceType: "skill", ResourceID: entry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"auto_fix": fmt.Sprintf("%v", in.AutoFix), "changes": fmt.Sprintf("%d", len(changes))}})
	return result, nil
}

func (s *Service) UploadSkill(ctx context.Context, p Principal, name string, in SkillUploadInput) (*SkillUploadResult, error) {
	if s == nil {
		return nil, fmt.Errorf("skill %q is blocked by pending compensation", name)
	}
	s.skillInstallTxnMu.Lock()
	defer s.skillInstallTxnMu.Unlock()
	if s.hasPendingSkillCompensationLocked(name) {
		return nil, fmt.Errorf("skill %q is blocked by pending compensation", name)
	}
	entry, dir, err := s.findSkill(p, name)
	if err != nil {
		return nil, err
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	baseURLs, err := s.resolveUserHubCenterBaseURLs(p, in.SkillMarketURL)
	if err != nil {
		return nil, err
	}
	snapshot, err := zipDirectoryBytes(dir)
	if err != nil {
		return nil, fmt.Errorf("snapshot skill before upload preflight: %w", err)
	}
	preflight, err := skill.PrepareSkillForUpload(dir)
	if err != nil {
		if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
			return nil, fmt.Errorf("upload preflight failed: %w; rollback failed: %v", err, restoreErr)
		}
		return nil, err
	}
	if !preflight.Portable() {
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "skill.rejected", ResourceType: "skill", ResourceID: entry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"phase": "upload_preflight", "summary": skill.PreflightSummaryLine(preflight)}})
		if len(preflight.AutoFixed) > 0 {
			if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
				return nil, fmt.Errorf("%s; rollback failed: %v", skill.FormatUploadPreflight(preflight), restoreErr)
			}
		}
		return nil, fmt.Errorf("%s", skill.FormatUploadPreflight(preflight))
	}
	uploadEntry, err := loadImportedSkillEntry(dir)
	if err != nil {
		if len(preflight.AutoFixed) > 0 {
			if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
				return nil, fmt.Errorf("reload skill after upload preflight: %w; rollback failed: %v", err, restoreErr)
			}
		}
		return nil, fmt.Errorf("reload skill after upload preflight: %w", err)
	}
	if strings.TrimSpace(uploadEntry.Name) == "" {
		uploadEntry.Name = entry.Name
	}
	uploadEntry.SkillDir = dir
	if err := s.scanSkillForOutbound(ctx, p, *uploadEntry, dir, "upload"); err != nil {
		if len(preflight.AutoFixed) > 0 {
			if restoreErr := restoreSkillDirFromArchive(dir, snapshot); restoreErr != nil {
				return nil, fmt.Errorf("%w; rollback failed: %v", err, restoreErr)
			}
		}
		return nil, err
	}
	archive, err := zipSkillUploadArchiveBytes(dir)
	if err != nil {
		return nil, err
	}
	fileName := normalizeSkillDirName(uploadEntry.Name)
	if fileName == "" {
		fileName = normalizeSkillDirName(entry.Name)
	}
	if fileName == "" {
		fileName = "skill"
	}
	submissionID, usedBaseURL, err := submitSkillArchiveCandidates(ctx, baseURLs, email, fileName+".zip", archive, strings.TrimSpace(in.AuthToken))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.SkillMarketURL) == "" {
		s.rememberUserHubCenterSelection(p, usedBaseURL, baseURLs)
	}
	statusPath := filepath.Join(dir, "upload_status.json")
	statusBody, _ := json.MarshalIndent(map[string]string{"submission_id": submissionID, "uploaded_at": s.now().Format(time.RFC3339)}, "", "  ")
	_ = fileutil.AtomicWriteFile(statusPath, append(statusBody, '\n'), 0o600)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "skill.uploaded", ResourceType: "skill", ResourceID: uploadEntry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"submission_id": submissionID}})
	return &SkillUploadResult{SubmissionID: submissionID, Status: "submitted"}, nil
}

func (s *Service) GetSkillUploadStatus(ctx context.Context, p Principal, submissionID, baseURL string) (*SkillSubmissionStatus, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	baseURLs, err := s.resolveUserHubCenterBaseURLs(p, baseURL)
	if err != nil {
		return nil, err
	}
	return fetchSkillSubmissionStatusCandidates(ctx, baseURLs, strings.TrimSpace(submissionID))
}

func (s *Service) GetSkillMarketAccount(ctx context.Context, p Principal, baseURL, email string) (*SkillMarketAccount, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	baseURLs, err := s.resolveUserHubCenterBaseURLs(p, baseURL)
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	return fetchSkillMarketAccountCandidates(ctx, baseURLs, email)
}

func (s *Service) ensureUserSkillsRoot(p Principal) (string, error) {
	root := s.userSkillsRoot(p.TenantID, p.UserID)
	if err := secureMkdirAll(root); err != nil {
		return "", err
	}
	return root, nil
}

func (s *Service) findSkill(p Principal, name string) (corelib.NLSkillEntry, string, error) {
	items, err := s.ListSkills(context.Background(), p)
	if err != nil {
		return corelib.NLSkillEntry{}, "", err
	}
	query := strings.TrimSpace(name)
	for _, item := range items {
		if item.MatchesName(query) {
			return item, item.SkillDir, nil
		}
	}
	return corelib.NLSkillEntry{}, "", fmt.Errorf("skill %q not found", name)
}
func (s *Service) installSkillArchiveBytes(ctx context.Context, p Principal, data []byte, overwrite bool) ([]corelib.NLSkillEntry, error) {
	tmpDir, err := os.MkdirTemp("", "maclawsrv-skill-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	if err := unzipBytes(data, tmpDir); err != nil {
		return nil, err
	}
	packageRoots, err := resolveImportedSkillPackageRoots(tmpDir)
	if err != nil {
		return nil, err
	}
	type scannedPackageRoot struct {
		entry *corelib.NLSkillEntry
		root  string
	}
	scanned := make([]scannedPackageRoot, 0, len(packageRoots))
	for _, root := range packageRoots {
		entry, err := loadImportedSkillEntry(root)
		if err != nil {
			return nil, err
		}
		if report, err := scanImportedSkillBeforeInstall(ctx, entry, root); err != nil {
			s.recordSkillScanRejection(p, entry, report, err)
			return nil, err
		}
		scanned = append(scanned, scannedPackageRoot{entry: entry, root: root})
	}
	// Keep every package in one durable transaction. A multi-package archive
	// must not leave earlier entries installed when a later directory publish
	// or final audit fails.
	entries := make([]corelib.NLSkillEntry, 0, len(scanned))
	for _, item := range scanned {
		entries = append(entries, *item.entry)
	}
	return s.persistImportedEntries(ctx, p, entries, overwrite)
}

func scanImportedSkillBeforeInstall(ctx context.Context, entry *corelib.NLSkillEntry, skillDir string) (*skill.ScanReport, error) {
	if entry == nil {
		return nil, fmt.Errorf("skill entry is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(entry.TrustLevel) == "" {
		entry.TrustLevel = "community"
	}
	if strings.TrimSpace(skillDir) != "" {
		entry.SkillDir = skillDir
	}
	report := skill.NewSecurityScanner(nil).ScanInstallStaged(ctx, entry, skillDir, nil)
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if report == nil {
		return nil, fmt.Errorf("skill security scan produced no report")
	}
	if report.NeedsUserReview() {
		return report, fmt.Errorf("skill security scan blocked installation: level=%s summary=%s", report.FinalLevel, report.Summary)
	}
	return report, nil
}

func (s *Service) persistImportedEntries(ctx context.Context, p Principal, entries []corelib.NLSkillEntry, overwrite bool) ([]corelib.NLSkillEntry, error) {
	if s == nil {
		return nil, fmt.Errorf("service is unavailable")
	}
	s.skillInstallTxnMu.Lock()
	defer s.skillInstallTxnMu.Unlock()
	// A queue that cannot be read is a hard admission failure. Recover only
	// AgentService-owned records before preparing a new package so a stale
	// process cannot be silently overwritten by a later import.
	if s.skillInstallRecoveryBlocked {
		// Retry recovery under the same transaction lock. A pending record is
		// still an unresolved filesystem decision and must prevent a new
		// install from competing with it.
		s.skillInstallRecoveryBlocked = false
	}
	if _, pending, err := s.recoverAgentSkillInstallCompensations(); err != nil {
		s.skillInstallRecoveryBlocked = true
		return nil, fmt.Errorf("recover pending skill install compensation: %w", err)
	} else if pending > 0 {
		s.skillInstallRecoveryBlocked = true
		return nil, fmt.Errorf("pending skill install compensation requires review")
	}
	root, err := s.ensureUserSkillsRoot(p)
	if err != nil {
		return nil, err
	}
	scanned := make([]corelib.NLSkillEntry, 0, len(entries))
	seenNames := make(map[string]struct{}, len(entries))
	seenDirs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			return nil, fmt.Errorf("skill name is required")
		}
		if report, err := scanImportedSkillBeforeInstall(ctx, &entry, entry.SkillDir); err != nil {
			s.recordSkillScanRejection(p, &entry, report, err)
			return nil, err
		}
		nameKey := strings.ToLower(strings.TrimSpace(entry.Name))
		if _, ok := seenNames[nameKey]; ok {
			return nil, fmt.Errorf("duplicate skill %q in import", entry.Name)
		}
		seenNames[nameKey] = struct{}{}
		if _, _, err := s.findSkill(p, entry.Name); err == nil {
			if !overwrite {
				return nil, fmt.Errorf("skill %q already exists", entry.Name)
			}
		}
		dir := filepath.Join(root, normalizeSkillDirName(firstNonEmpty(entry.DirName, entry.Name)))
		cleanDir := filepath.Clean(dir)
		dirKey := cleanDir
		if runtime.GOOS == "windows" {
			dirKey = strings.ToLower(dirKey)
		}
		if _, ok := seenDirs[dirKey]; ok {
			return nil, fmt.Errorf("duplicate skill directory %q in import", filepath.Base(cleanDir))
		}
		seenDirs[dirKey] = struct{}{}
		scanned = append(scanned, entry)
	}
	return s.persistImportedEntriesWithCommitter(ctx, p, root, scanned, overwrite)
}

// persistImportedEntryWithCommitter is retained as a compatibility adapter for
// callers that install one package. The actual implementation is batch based
// so a multi-package archive cannot leave a committed first package when a
// later package fails.
func (s *Service) persistImportedEntryWithCommitter(ctx context.Context, p Principal, root string, entry corelib.NLSkillEntry, overwrite bool) (corelib.NLSkillEntry, error) {
	items, err := s.persistImportedEntriesWithCommitter(ctx, p, root, []corelib.NLSkillEntry{entry}, overwrite)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if len(items) == 0 {
		entry.Status = "skipped"
		return entry, nil
	}
	return items[0], nil
}

type agentSkillInstallItem struct {
	entry     corelib.NLSkillEntry
	stageDir  string
	finalDir  string
	backup    string
	hadPrev   bool
	moved     bool
	published bool
}

// persistImportedEntriesWithCommitter publishes all packages from one import
// through one durable directory transaction. The AgentService registry is
// filesystem-derived, so config/index callbacks are intentionally no-ops; the
// checked directory scan and strict audit are still part of the commit.
func (s *Service) persistImportedEntriesWithCommitter(ctx context.Context, p Principal, root string, entries []corelib.NLSkillEntry, overwrite bool) ([]corelib.NLSkillEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items := make([]agentSkillInstallItem, 0, len(entries))
	for _, entry := range entries {
		finalDir := filepath.Clean(filepath.Join(root, normalizeSkillDirName(firstNonEmpty(entry.DirName, entry.Name))))
		existing, _, findErr := s.findSkill(p, entry.Name)
		item := agentSkillInstallItem{entry: entry, finalDir: finalDir}
		if findErr == nil {
			if !overwrite {
				return nil, fmt.Errorf("skill %q already exists", entry.Name)
			}
			if strings.TrimSpace(existing.SkillDir) != "" {
				item.finalDir = filepath.Clean(existing.SkillDir)
			}
			item.backup = item.finalDir + ".prev"
			item.hadPrev = true
			if _, statErr := os.Stat(item.backup); statErr == nil {
				return nil, fmt.Errorf("skill backup conflict: %s already exists", filepath.Base(item.backup))
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return nil, statErr
			}
		} else if !strings.Contains(strings.ToLower(findErr.Error()), "not found") {
			return nil, findErr
		} else if info, statErr := os.Stat(item.finalDir); statErr == nil && info.IsDir() {
			return nil, fmt.Errorf("skill directory %q already exists under a different identity", filepath.Base(item.finalDir))
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		stageDir, err := os.MkdirTemp(root, ".skill-install-*")
		if err != nil {
			return nil, err
		}
		item.stageDir = stageDir
		if err := writeEntryToSkillDir(item.stageDir, entry); err != nil {
			for _, prior := range items {
				_ = os.RemoveAll(prior.stageDir)
			}
			_ = os.RemoveAll(item.stageDir)
			return nil, err
		}
		loaded, err := loadImportedSkillEntry(item.stageDir)
		if err != nil || !loaded.MatchesName(entry.Name) {
			for _, prior := range items {
				_ = os.RemoveAll(prior.stageDir)
			}
			_ = os.RemoveAll(item.stageDir)
			if err == nil {
				err = fmt.Errorf("parsed name %q does not match", loaded.Name)
			}
			return nil, fmt.Errorf("validate staged skill %q: %w", entry.Name, err)
		}
		if findErr == nil && sameImportedSkillDirectory(item.stageDir, existing.SkillDir) {
			_ = os.RemoveAll(item.stageDir)
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, nil
	}
	requestID := fmt.Sprintf("agent_skill_install_%d", time.Now().UnixNano())
	cleanupStages := func() {
		for _, item := range items {
			if item.stageDir != "" {
				_ = os.RemoveAll(item.stageDir)
			}
		}
	}
	defer cleanupStages()
	first := items[0].entry
	first.SkillDir = items[0].finalDir
	first.Source = firstNonEmpty(first.Source, "file")
	previousContracts := make(map[string]DynamicCapabilityContract)
	contractSnapshotKeys := make(map[string]string)
	contractSnapshotPayloads := make(map[string]string)
	for _, item := range items {
		if existing, _, findErr := s.findSkill(p, item.entry.Name); findErr == nil {
			stableID := skillStableID(existing)
			if contract, ok := s.dynamicCapabilities.ResolveSkillDynamicContract(ctx, p, stableID); ok {
				previousContracts[stableID] = contract
				contractSnapshotKeys[stableID] = "skill_contract|" + p.TenantID + "|" + p.UserID + "|" + stableID
				encoded, encodeErr := encodeSkillContractExternalSnapshot(p, stableID, contract, requestID, time.Now().UTC())
				if encodeErr != nil {
					return nil, fmt.Errorf("prepare Skill contract compensation snapshot: %w", encodeErr)
				}
				contractSnapshotPayloads[stableID] = encoded
			}
		}
	}
	revokedContracts := make(map[string]bool)
	committer := &skill.SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return skill.ScanSkillDirAll(root) },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		IndexRefresher: func() error {
			// The AgentService registry is filesystem-derived; force a scan by
			// validating the published definition before final audit.
			for _, item := range items {
				if _, statErr := os.Stat(item.finalDir); os.IsNotExist(statErr) {
					return fmt.Errorf("published skill directory missing: %s", filepath.Base(item.finalDir))
				} else if statErr != nil {
					return statErr
				}
				if _, err := loadImportedSkillEntry(item.finalDir); err != nil {
					return err
				}
			}
			return nil
		},
		FinalAuditor: func(event string, data map[string]string) error {
			return s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: event, ResourceType: "skill", ResourceID: first.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: data})
		},
		ConfigRevision: "agentservice-skill-install-v1",
		AllowCreate:    true,
		CompensationMutator: func(record *skill.EvolutionCompensationRecord) {
			record.SetRecoveryScope(s.dataRoot)
			created := make([]string, 0, len(items))
			moves := make([]skill.EvolutionDirectoryMove, 0, len(items))
			affected := make([]string, 0, len(items))
			for i := range items {
				created = append(created, items[i].finalDir)
				moves = append(moves, skill.EvolutionDirectoryMove{OriginalPath: items[i].finalDir, BackupPath: items[i].backup, HadPrevious: items[i].hadPrev})
				affected = append(affected, items[i].entry.Name)
			}
			record.SetCreatedDirectories(created)
			record.SetDirectoryMoves(moves)
			record.SetAffectedSkills(affected)
			for stableID := range previousContracts {
				record.SetExternalSnapshot(contractSnapshotKeys[stableID], contractSnapshotPayloads[stableID])
			}
		},
		ExternalCommitWithCompensation: func(record *skill.EvolutionCompensationRecord) error {
			moves := append([]skill.EvolutionDirectoryMove(nil), record.DirectoryMoves...)
			for i := range items {
				if items[i].hadPrev {
					if err := skill.RetryDirectoryRename(items[i].finalDir, items[i].backup); err != nil {
						return fmt.Errorf("backup existing skill %s: %w", items[i].entry.Name, err)
					}
					items[i].moved = true
					moves[i].Moved = true
					if err := replaceAgentSkillCompensation(record, moves); err != nil {
						return err
					}
				}
				if err := skill.RetryDirectoryRename(items[i].stageDir, items[i].finalDir); err != nil {
					return fmt.Errorf("publish skill %s: %w", items[i].entry.Name, err)
				}
				items[i].published = true
				moves[i].Published = true
				items[i].stageDir = ""
				if err := replaceAgentSkillCompensation(record, moves); err != nil {
					return err
				}
			}
			for stableID := range previousContracts {
				revokedContracts[stableID] = true
				record.MarkExternalApplied(contractSnapshotKeys[stableID], true)
				if err := replaceAgentSkillCompensation(record, moves); err != nil {
					return err
				}
				if err := s.revokeSkillDynamicContract(p, stableID); err != nil {
					return err
				}
			}
			return nil
		},
		ExternalRollback: func() error {
			for i := len(items) - 1; i >= 0; i-- {
				if items[i].published {
					if err := os.RemoveAll(items[i].finalDir); err != nil && !os.IsNotExist(err) {
						return err
					}
				}
				if items[i].moved {
					if err := skill.RetryDirectoryRename(items[i].backup, items[i].finalDir); err != nil {
						return err
					}
				}
			}
			for stableID := range revokedContracts {
				if contract, ok := previousContracts[stableID]; ok {
					if err := s.dynamicCapabilities.PublishSkillContract(p, stableID, contract); err != nil {
						return fmt.Errorf("restore Skill dynamic capability contract: %w", err)
					}
				}
			}
			return nil
		},
		PostCommitCleanup: func() error {
			for _, item := range items {
				if !item.hadPrev || strings.TrimSpace(item.backup) == "" {
					continue
				}
				if err := os.RemoveAll(item.backup); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove committed backup %s: %w", filepath.Base(item.backup), err)
				}
			}
			return nil
		},
	}
	result := committer.Commit(skill.WithEvolutionRequestMetadata(ctx, requestID, 1), first.Name, &first, "skill.installed", map[string]string{"skill": first.Name, "action": "agentservice_install", "decision": "applied", "source": first.Source, "request_id": requestID, "attempt": "1", "config_revision": "agentservice-skill-install-v1", "schema_version": "2", "evidence_mode": "none", "package_count": fmt.Sprintf("%d", len(items)), "external_state": "dynamic_skill_contract", "contract_revocation_count": fmt.Sprintf("%d", len(previousContracts))})
	if result.State != "committed" || result.CleanupStatus != "clear" {
		return nil, fmt.Errorf("skill install not committed: state=%s cleanup_status=%s reason=%s", result.State, result.CleanupStatus, result.FailureReason)
	}
	installed := make([]corelib.NLSkillEntry, 0, len(items))
	for _, item := range items {
		item.entry.SkillDir = item.finalDir
		item.entry.Source = firstNonEmpty(item.entry.Source, "file")
		installed = append(installed, item.entry)
	}
	return installed, nil
}

func replaceAgentSkillCompensation(record *skill.EvolutionCompensationRecord, moves []skill.EvolutionDirectoryMove) error {
	record.SetDirectoryMoves(moves)
	return skill.ReplaceEvolutionCompensation(*record)
}

func sameImportedSkillDirectory(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if left == "." || right == "." || left == "" || right == "" {
		return false
	}
	leftDigest, leftErr := importedSkillDirectoryDigest(left)
	rightDigest, rightErr := importedSkillDirectoryDigest(right)
	return leftErr == nil && rightErr == nil && leftDigest == rightDigest
}

func importedSkillDirectoryDigest(root string) (string, error) {
	h := sha256.New()
	paths := make([]string, 0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) recordSkillScanRejection(p Principal, entry *corelib.NLSkillEntry, report *skill.ScanReport, scanErr error) {
	if s == nil || entry == nil {
		return
	}
	level := ""
	summary := ""
	scannedBy := ""
	if report != nil {
		level = string(report.FinalLevel)
		summary = report.Summary
		scannedBy = report.ScannedBy
	}
	if summary == "" && scanErr != nil {
		summary = scanErr.Error()
	}
	_ = s.recordAudit(auditRecord{
		TenantID:      p.TenantID,
		UserID:        p.UserID,
		Action:        "skill.rejected",
		ResourceType:  "skill",
		ResourceID:    entry.Name,
		ActorType:     "system",
		ActorTenantID: p.TenantID,
		ActorUserID:   p.UserID,
		Metadata: map[string]string{
			"level":      level,
			"summary":    summary,
			"scanned_by": scannedBy,
			"source":     firstNonEmpty(entry.Source, "file"),
		},
	})
}

func (s *Service) persistExtractedSkillDir(p Principal, entry corelib.NLSkillEntry, extractedDir string, overwrite bool) (corelib.NLSkillEntry, error) {
	root, err := s.ensureUserSkillsRoot(p)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if existing, _, err := s.findSkill(p, entry.Name); err == nil {
		if !overwrite {
			return corelib.NLSkillEntry{}, fmt.Errorf("skill %q already exists", entry.Name)
		}
		if err := s.revokeSkillDynamicContract(p, skillStableID(existing)); err != nil {
			return corelib.NLSkillEntry{}, err
		}
	}
	dir := filepath.Join(root, normalizeSkillDirName(firstNonEmpty(entry.DirName, entry.Name)))
	if overwrite {
		_ = os.RemoveAll(dir)
	}
	if err := secureMkdirAll(dir); err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if err := copyDirContents(extractedDir, dir); err != nil {
		return corelib.NLSkillEntry{}, err
	}
	entry.SkillDir = dir
	entry.Source = firstNonEmpty(entry.Source, "file")
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "skill.imported", ResourceType: "skill", ResourceID: entry.Name, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"source": entry.Source}})
	return entry, nil
}

func restoreSkillDirFromArchive(skillDir string, archive []byte) error {
	if strings.TrimSpace(skillDir) == "" {
		return fmt.Errorf("skill directory is required")
	}
	parent := filepath.Dir(skillDir)
	tmpDir, err := os.MkdirTemp(parent, ".skill-rollback-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := unzipBytes(archive, tmpDir); err != nil {
		return err
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	if err := secureMkdirAll(skillDir); err != nil {
		return err
	}
	return copyDirContents(tmpDir, skillDir)
}

func normalizeSkillSearchSources(sources []string) []string {
	if len(sources) == 0 {
		return []string{"github", "skillmarket", "skillhub", "clawhub"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		normalized := strings.ToLower(strings.TrimSpace(source))
		switch normalized {
		case "market", "hubcenter", "hub_center":
			normalized = "skillmarket"
		case "skill_hub":
			normalized = "skillhub"
		}
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return []string{"github", "skillmarket", "skillhub", "clawhub"}
	}
	return out
}

func inferSkillNameFromGitHub(candidate skill.GitHubSkillCandidate) string {
	if base := filepath.Base(filepath.Dir(candidate.FilePath)); base != "." && base != "" {
		return base
	}
	if parts := strings.Split(strings.TrimSpace(candidate.RepoFullName), "/"); len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "github-skill"
}

func searchSkillHub(ctx context.Context, baseURL, query string, topN int) ([]SkillSearchResult, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("skill hub url is required")
	}
	if topN <= 0 {
		topN = 10
	}
	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1", strings.TrimRight(baseURL, "/"), url.QueryEscape(query))
	body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, nil, 2<<20)
	if err != nil {
		return nil, err
	}
	var payload skillHubSearchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	results := make([]SkillSearchResult, 0, len(payload.Skills))
	for i, item := range payload.Skills {
		if i >= topN {
			break
		}
		results = append(results, SkillSearchResult{Source: "skillhub", ID: item.ID, Name: item.Name, Description: item.Description, Version: item.Version, Author: item.Author, TrustLevel: item.TrustLevel, Tags: item.Tags, Downloads: item.Downloads, AvgRating: item.AvgRating})
	}
	return results, nil
}

func searchSkillHubCandidates(ctx context.Context, baseURLs []string, query string, topN int) ([]SkillSearchResult, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		results, err := searchSkillHub(ctx, baseURL, query, topN)
		if err == nil {
			return results, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("skill hub url is required")
}

func searchSkillMarket(ctx context.Context, baseURL, query string, topN int) ([]SkillSearchResult, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("skill market url is required")
	}
	if topN <= 0 {
		topN = 10
	}
	endpoint := fmt.Sprintf("%s/api/v1/skillmarket/search?q=%s&top_n=%d", strings.TrimRight(baseURL, "/"), url.QueryEscape(query), topN)
	body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, nil, 2<<20)
	if err != nil {
		return nil, err
	}
	var payload skillMarketSearchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	results := make([]SkillSearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, SkillSearchResult{Source: "skillmarket", ID: item.ID, Name: item.Name, Description: item.Description, Price: item.Price, AvgRating: item.AvgRating, Downloads: item.Downloads, Author: item.Author})
	}
	return results, nil
}

func searchSkillMarketCandidates(ctx context.Context, baseURLs []string, query string, topN int) ([]SkillSearchResult, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		results, err := searchSkillMarket(ctx, baseURL, query, topN)
		if err == nil {
			return results, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("skill market url is required")
}

func downloadSkillHubEntry(ctx context.Context, baseURL, skillID string) (*corelib.NLSkillEntry, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	skillID = strings.TrimSpace(skillID)
	if baseURL == "" || skillID == "" {
		return nil, fmt.Errorf("skill_hub_url and skill_id are required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download", baseURL, url.PathEscape(skillID))
	body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, nil, skillHubJSONMaxBytes)
	if err != nil {
		return nil, err
	}
	// Extract into a temp dir; persistImportedEntries copies into the user skill root.
	tmpDir, err := os.MkdirTemp("", "maclaw-skillhub-dl-*")
	if err != nil {
		return nil, err
	}
	entry, err := skill.ParseSkillHubDownloadJSON(body, skill.HubDownloadOptions{
		HubURL:    baseURL,
		SkillID:   skillID,
		Source:    "skillhub",
		TargetDir: tmpDir,
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	return entry, nil
}

func downloadSkillHubEntryCandidates(ctx context.Context, baseURLs []string, skillID string) (*corelib.NLSkillEntry, string, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		entry, err := downloadSkillHubEntry(ctx, baseURL, skillID)
		if err == nil {
			return entry, baseURL, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, "", err
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("skill hub url is required")
}

func downloadSkillMarketEntry(ctx context.Context, baseURL, skillID, email, authToken string) (*corelib.NLSkillEntry, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	skillID = strings.TrimSpace(skillID)
	email = strings.TrimSpace(email)
	if baseURL == "" || skillID == "" || email == "" {
		return nil, fmt.Errorf("skill_market_url, skill_id, and email are required")
	}
	headers := map[string]string{}
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}
	endpoints := []string{
		fmt.Sprintf("%s/api/v1/skillmarket/%s/download?email=%s&format=agent_skill", baseURL, url.PathEscape(skillID), url.QueryEscape(email)),
		fmt.Sprintf("%s/api/v1/skillmarket/skills/%s/download?email=%s&format=agent_skill", baseURL, url.PathEscape(skillID), url.QueryEscape(email)),
		fmt.Sprintf("%s/api/capability-market/capabilities/%s/download?email=%s&format=agent_skill", baseURL, url.PathEscape(skillID), url.QueryEscape(email)),
	}
	var lastErr error
	var retryableErr error
	var encryptedResponse bool
	for _, endpoint := range endpoints {
		body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, headers, skillHubJSONMaxBytes)
		if err != nil {
			lastErr = err
			if retryableErr == nil && isRetryableHubCenterError(err) {
				retryableErr = err
			}
			continue
		}
		var encrypted struct {
			EncryptedData any `json:"encrypted_data"`
		}
		if err := json.Unmarshal(body, &encrypted); err == nil && encrypted.EncryptedData != nil {
			encryptedResponse = true
			lastErr = fmt.Errorf("skill market returned encrypted package without installable agent skill payload")
			continue
		}
		tmpDir, tmpErr := os.MkdirTemp("", "maclaw-skillmarket-dl-*")
		if tmpErr != nil {
			return nil, tmpErr
		}
		entry, err := skill.ParseSkillHubDownloadJSON(body, skill.HubDownloadOptions{
			HubURL:    baseURL,
			SkillID:   skillID,
			Source:    "skillmarket",
			TargetDir: tmpDir,
		})
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			// Fall back to legacy envelope for partial payloads without steps/files.
			var payload skillHubDownloadEnvelope
			if uerr := json.Unmarshal(body, &payload); uerr == nil {
				if legacy, lerr := skillEntryFromHubDownloadPayload(payload, baseURL, "skillmarket"); lerr == nil {
					return legacy, nil
				}
			}
			lastErr = err
			continue
		}
		return entry, nil
	}
	if encryptedResponse {
		return nil, fmt.Errorf("skill market download endpoint does not expose format=agent_skill")
	}
	if retryableErr != nil {
		return nil, retryableErr
	}
	return nil, lastErr
}

func (s *Service) downloadSkillMarketEntryCandidates(ctx context.Context, p Principal, cfg UserConfig, baseURLs []string, skillID, email string) (*corelib.NLSkillEntry, string, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		authToken := s.skillMarketAuthToken(ctx, p, cfg, baseURL, email)
		entry, err := downloadSkillMarketEntry(ctx, baseURL, skillID, email, authToken)
		if err == nil {
			return entry, baseURL, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, "", err
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("skill market url is required")
}

func skillEntryFromHubDownloadPayload(payload skillHubDownloadEnvelope, sourceProject, defaultSource string) (*corelib.NLSkillEntry, error) {
	if strings.TrimSpace(payload.AgentSkillMD) != "" {
		entry, err := skill.ParseMarkdownSkill(payload.AgentSkillMD, skill.MarkdownSkillOptions{NameFallback: payload.Name, DescriptionFallback: payload.Description, Source: firstNonEmpty(payload.Source, defaultSource), SourceProject: sourceProject, TrustLevel: payload.TrustLevel, Triggers: payload.Triggers})
		if err != nil {
			return nil, fmt.Errorf("parse agent skill markdown: %w", err)
		}
		entry.HubSkillID = payload.ID
		entry.HubVersion = payload.Version
		entry.TrustLevel = firstNonEmpty(entry.TrustLevel, payload.TrustLevel)
		return entry, nil
	}
	if strings.TrimSpace(payload.Name) == "" {
		return nil, fmt.Errorf("skill download response missing name")
	}
	return &corelib.NLSkillEntry{Name: payload.Name, Description: payload.Description, Triggers: payload.Triggers, Steps: payload.Steps, Status: "active", CreatedAt: time.Now().Format(time.RFC3339), Source: firstNonEmpty(payload.Source, defaultSource), SourceProject: sourceProject, HubSkillID: payload.ID, HubVersion: payload.Version, TrustLevel: payload.TrustLevel, Type: payload.Type, Content: payload.Content, Capabilities: payload.Capabilities}, nil
}

func submitSkillArchive(ctx context.Context, baseURL, email, fileName string, archive []byte, authToken string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("zip", fileName)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(archive); err != nil {
		return "", err
	}
	_ = writer.WriteField("email", email)
	if err := writer.Close(); err != nil {
		return "", err
	}
	headers := map[string]string{"Content-Type": writer.FormDataContentType()}
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}
	respBody, err := doJSONRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/skills/submit", &body, headers, 1<<20)
	if err != nil {
		return "", err
	}
	var payload struct {
		SubmissionID string `json:"submission_id"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.SubmissionID) == "" {
		return "", fmt.Errorf("skill market response missing submission_id")
	}
	return payload.SubmissionID, nil
}

func submitSkillArchiveCandidates(ctx context.Context, baseURLs []string, email, fileName string, archive []byte, authToken string) (string, string, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		submissionID, err := submitSkillArchive(ctx, baseURL, email, fileName, archive, authToken)
		if err == nil {
			return submissionID, baseURL, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return "", "", err
		}
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("skill market url is required")
}

func fetchSkillSubmissionStatus(ctx context.Context, baseURL, submissionID string) (*SkillSubmissionStatus, error) {
	if submissionID == "" {
		return nil, fmt.Errorf("submission_id is required")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/skill-submissions/" + url.PathEscape(submissionID)
	body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, nil, 1<<20)
	if err != nil {
		return nil, err
	}
	var payload SkillSubmissionStatus
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func fetchSkillSubmissionStatusCandidates(ctx context.Context, baseURLs []string, submissionID string) (*SkillSubmissionStatus, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		status, err := fetchSkillSubmissionStatus(ctx, baseURL, submissionID)
		if err == nil {
			return status, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("skill market url is required")
}

func fetchSkillMarketAccount(ctx context.Context, baseURL, email string) (*SkillMarketAccount, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/account/" + url.PathEscape(email)
	body, err := doJSONRequest(ctx, http.MethodGet, endpoint, nil, nil, 1<<20)
	if err != nil {
		return nil, err
	}
	var payload SkillMarketAccount
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func fetchSkillMarketAccountCandidates(ctx context.Context, baseURLs []string, email string) (*SkillMarketAccount, error) {
	var lastErr error
	for _, baseURL := range remote.NormalizeHubCenterURLs(baseURLs) {
		account, err := fetchSkillMarketAccount(ctx, baseURL, email)
		if err == nil {
			return account, nil
		}
		lastErr = err
		if !isRetryableHubCenterError(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("skill market url is required")
}

type hubCenterStatusError struct {
	StatusCode int
	Message    string
}

func (e hubCenterStatusError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("request failed (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("request failed (%d)", e.StatusCode)
}

func isRetryableHubCenterError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr hubCenterStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusRequestTimeout ||
			statusErr.StatusCode == http.StatusTooManyRequests ||
			(statusErr.StatusCode >= 500 && statusErr.StatusCode <= 599)
	}
	return true
}

func doJSONRequest(ctx context.Context, method, endpoint string, body io.Reader, headers map[string]string, maxBytes int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	timeout := 45 * time.Second
	if maxBytes > skill.MaxSkillHubSearchJSONBytes {
		// Large skill install payloads need more time on slow links.
		timeout = 180 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Cap error-page reads; ignore Content-Length so a huge CL does not
		// mask the real HTTP status as "package too large".
		data, _ := skill.ReadLimitedHTTPBody(resp.Body, -1, 64<<10)
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			trimmed = resp.Status
		}
		return nil, hubCenterStatusError{StatusCode: resp.StatusCode, Message: trimmed}
	}
	return skill.ReadLimitedHTTPBody(resp.Body, resp.ContentLength, maxBytes)
}

// readBoundedJSONResponse is kept for unit tests and thin-wraps the shared helper.
func readBoundedJSONResponse(body io.Reader, contentLength, maxBytes int64) ([]byte, error) {
	return skill.ReadLimitedHTTPBody(body, contentLength, maxBytes)
}

func normalizeSkillDirName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "skill_" + strings.ReplaceAll(NewID("skill"), "-", "")
	}
	v = invalidSkillDirChars.ReplaceAllString(v, "_")
	v = strings.Trim(v, " .")
	if v == "" {
		return "skill_" + strings.ReplaceAll(NewID("skill"), "-", "")
	}
	return v
}

func writeEntryToSkillDir(dir string, entry corelib.NLSkillEntry) error {
	if err := secureMkdirAll(dir); err != nil {
		return err
	}
	// Copy package files from the download/extract location so scripts and
	// SKILL.md survive into the user-scoped skill directory.
	if src := strings.TrimSpace(entry.SkillDir); src != "" {
		if err := copySkillPackageFiles(src, dir); err != nil {
			return fmt.Errorf("copy skill package files: %w", err)
		}
		// Best-effort cleanup of download temp dirs created by downloadSkillHubEntry /
		// downloadSkillMarketEntry (maclaw-skillhub-dl-* / maclaw-skillmarket-dl-*).
		if isEphemeralSkillDownloadDir(src) {
			_ = os.RemoveAll(src)
		}
	}
	sf := &skill.SkillYAMLFile{Name: entry.Name, Description: entry.Description, Triggers: entry.Triggers, Status: firstNonEmpty(entry.Status, "active"), Platforms: entry.Platforms, RequiresGUI: entry.RequiresGUI, Type: entry.Type, Content: entry.Content, Mode: entry.Mode, ExecMode: entry.ExecMode, GlobalTimeout: entry.GlobalTimeout, RequiredArgs: entry.RequiredArgs, RequiredEnv: entry.RequiredEnv, PreferredShell: entry.PreferredShell, Capabilities: entry.Capabilities, RequiresTools: entry.RequiresTools, FallbackForTools: entry.FallbackForTools, RequiresToolsets: entry.RequiresToolsets, FallbackForToolsets: entry.FallbackForToolsets, RequiredCredentialFiles: entry.RequiredCredentialFiles}
	for _, op := range entry.Operations {
		sf.Operations = append(sf.Operations, skill.SkillYAMLOperation{Name: op.Name, Description: op.Description, Params: op.Params, Labels: op.Labels})
	}
	for _, step := range entry.Steps {
		sf.Steps = append(sf.Steps, skill.SkillYAMLStep{Action: step.Action, Params: step.Params, OnError: step.OnError, Name: step.Name, Condition: step.Condition, When: step.When, Label: step.Label, Capture: step.Capture})
	}
	if entry.Type == "knowledge" && strings.TrimSpace(entry.Content) != "" {
		sf.Steps = nil
	}
	data, err := skill.FormatSkillYAMLFile(sf)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, "skill.yaml"), append(data, '\n'), 0o644)
}

func isEphemeralSkillDownloadDir(dir string) bool {
	base := filepath.Base(strings.TrimSpace(dir))
	return strings.HasPrefix(base, "maclaw-skillhub-dl-") ||
		strings.HasPrefix(base, "maclaw-skillmarket-dl-")
}

// copySkillPackageFiles copies regular files from src into dst, preserving
// relative paths. Symlinks and paths that escape dst are skipped. When src and
// dst resolve to the same directory, this is a no-op.
func copySkillPackageFiles(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if srcAbs == dstAbs {
		return nil
	}
	info, err := os.Stat(srcAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.Walk(srcAbs, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstAbs, rel)
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		// Refuse path escapes.
		if relEsc, err := filepath.Rel(dstAbs, targetAbs); err != nil || relEsc == ".." || strings.HasPrefix(relEsc, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe package path %q", rel)
		}
		if fi.IsDir() {
			return os.MkdirAll(targetAbs, 0o755)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetAbs, data, 0o644)
	})
}

func unzipBytes(data []byte, dest string) error {
	result := archiveutil.ExtractZIPBytesToDirectory(data, dest, archiveutil.Limits{
		MaxInputBytes: 64 << 20,
		MaxFiles:      maxImportedSkillZipEntries,
		MaxFileBytes:  maxImportedSkillZipFileBytes,
		MaxTotalBytes: maxImportedSkillZipTotalExpandedBytes,
	}, archiveutil.ExtractionPolicy{})
	if !result.OK {
		switch result.Code {
		case archiveutil.CodeUnsafeEntry:
			if strings.Contains(result.Message, "symlink") {
				return fmt.Errorf("zip contains unsupported symlink: %s", result.Message)
			}
			return fmt.Errorf("zip contains invalid path: %s", result.Message)
		case archiveutil.CodeLimitExceeded:
			if strings.Contains(result.Message, "file count") {
				return fmt.Errorf("zip contains too many entries: %s", result.Message)
			}
		}
		return fmt.Errorf("extract skill ZIP: %s: %s", result.Code, result.Message)
	}
	return nil
}

const (
	maxImportedSkillZipEntries            = 2048
	maxImportedSkillZipFileBytes          = 64 << 20
	maxImportedSkillZipTotalExpandedBytes = 256 << 20
)

func resolveImportedSkillPackageRoots(sandboxDir string) ([]string, error) {
	if importedSkillDefinitionExists(sandboxDir) {
		return []string{sandboxDir}, nil
	}
	entries, err := os.ReadDir(sandboxDir)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "__MACOSX" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		candidate := filepath.Join(sandboxDir, entry.Name())
		if importedSkillDefinitionExists(candidate) {
			roots = append(roots, candidate)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("zip package does not contain a recognizable skill definition")
	}
	return roots, nil
}

func importedSkillDefinitionExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case "skill.yaml", "skill.md", "SKILL.md", "KNOWLEDGE.md":
			return true
		}
	}
	return false
}

func loadImportedSkillEntry(skillDir string) (*corelib.NLSkillEntry, error) {
	if parsed, _, err := loadSkillFromExtractedDir(skillDir); err == nil {
		parsed.SkillDir = skillDir
		if parsed.CreatedAt == "" {
			parsed.CreatedAt = time.Now().Format(time.RFC3339)
		}
		return parsed, nil
	}
	return nil, fmt.Errorf("skill package at %s is not importable", skillDir)
}

func loadSkillFromExtractedDir(skillDir string) (*corelib.NLSkillEntry, string, error) {
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		parsed, err := skill.ParseSkillYAMLFile(data)
		if err != nil {
			return nil, "", err
		}
		entry := &corelib.NLSkillEntry{Name: firstNonEmpty(strings.TrimSpace(parsed.Name), filepath.Base(skillDir)), DirName: filepath.Base(skillDir), Description: parsed.Description, Triggers: parsed.Triggers, Status: firstNonEmpty(parsed.Status, "active"), Source: "file", Platforms: parsed.Platforms, RequiresGUI: parsed.RequiresGUI, Type: parsed.Type, Content: parsed.Content, Mode: parsed.Mode, ExecMode: parsed.ExecMode, GlobalTimeout: parsed.GlobalTimeout, RequiredArgs: parsed.RequiredArgs, RequiredEnv: parsed.RequiredEnv, PreferredShell: parsed.PreferredShell, Capabilities: parsed.Capabilities, RequiresTools: parsed.RequiresTools, FallbackForTools: parsed.FallbackForTools, RequiresToolsets: parsed.RequiresToolsets, FallbackForToolsets: parsed.FallbackForToolsets, RequiredCredentialFiles: parsed.RequiredCredentialFiles, CreatedAt: time.Now().Format(time.RFC3339)}
		for _, op := range parsed.Operations {
			entry.Operations = append(entry.Operations, corelib.NLSkillOperation{Name: op.Name, Description: op.Description, Params: op.Params, Labels: op.Labels})
		}
		for _, step := range parsed.Steps {
			entry.Steps = append(entry.Steps, corelib.NLSkillStep{Action: step.Action, Params: step.Params, OnError: step.OnError, Name: step.Name, Condition: step.Condition, When: step.When, Label: step.Label, Capture: step.Capture})
		}
		if len(entry.Steps) == 0 {
			if markdownEntry, err := skill.ImportMarkdownSkillDir(skillDir, skill.MarkdownSkillOptions{NameFallback: entry.Name, DescriptionFallback: entry.Description, Triggers: entry.Triggers, Source: "file", SkillDir: skillDir}); err == nil {
				markdownEntry.Platforms = entry.Platforms
				markdownEntry.RequiresGUI = entry.RequiresGUI
				markdownEntry.Capabilities = entry.Capabilities
				return markdownEntry, yamlPath, nil
			}
		}
		return entry, yamlPath, nil
	}
	if markdownEntry, err := skill.ImportMarkdownSkillDir(skillDir, skill.MarkdownSkillOptions{NameFallback: filepath.Base(skillDir), Source: "file", SkillDir: skillDir}); err == nil {
		return markdownEntry, filepath.Join(skillDir, "skill.md"), nil
	}
	knowledgePath := filepath.Join(skillDir, "KNOWLEDGE.md")
	if data, err := os.ReadFile(knowledgePath); err == nil {
		return &corelib.NLSkillEntry{Name: filepath.Base(skillDir), DirName: filepath.Base(skillDir), Description: filepath.Base(skillDir), Status: "active", Source: "file", SkillDir: skillDir, Type: "knowledge", Content: strings.TrimSpace(string(data)), CreatedAt: time.Now().Format(time.RFC3339)}, knowledgePath, nil
	}
	return nil, "", fmt.Errorf("skill definition not found")
}

func copyDirContents(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink %q", rel)
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func zipDirectoryBytes(srcDir string) ([]byte, error) {
	return zipDirectoryBytesFiltered(srcDir, nil)
}

func zipSkillUploadArchiveBytes(srcDir string) ([]byte, error) {
	return zipDirectoryBytesFiltered(srcDir, shouldSkipSkillUploadArchiveEntry)
}

func zipDirectoryBytesFiltered(srcDir string, skip func(rel string, info os.FileInfo) bool) ([]byte, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip != nil && skip(rel, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to zip symlink %q", rel)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		w, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func shouldSkipSkillUploadArchiveEntry(rel string, info os.FileInfo) bool {
	if info.IsDir() {
		return skill.IsSkillRuntimePackageDir(rel)
	}
	return skill.IsSkillRuntimePackageFile(rel)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// filterAllowedSources returns only the sources that are in the allowed list.
// Maps search source names (github, skillmarket, skillhub) to canonical names
// (github, skillhub, skillhub) for comparison.
func filterAllowedSources(requested, allowed []string) []string {
	var result []string
	for _, src := range normalizeSkillSearchSources(requested) {
		canonical := searchSourceToCanonical(src)
		if canonical == "" || sourceAllowedByPolicy(canonical, allowed) {
			result = append(result, src)
		}
	}
	return result
}

// searchSourceToCanonical maps search source names to the canonical source
// identifiers used by the source control system.
func searchSourceToCanonical(source string) string {
	switch source {
	case "github":
		return "github"
	case "skillhub", "skill_hub", "skillmarket", "market", "hubcenter", "hub_center":
		return "skillhub"
	case "clawhub":
		return "clawhub"
	default:
		return ""
	}
}

// installSourceToCanonical maps install source names to canonical identifiers.
func installSourceToCanonical(source string) string {
	switch source {
	case "github", "github_repo", "github_candidate":
		return "github"
	case "skillhub", "skillmarket", "market", "hubcenter", "hub_center":
		return "skillhub"
	case "clawhub":
		return "clawhub"
	case "zip":
		return "local"
	default:
		return ""
	}
}

func isInSlice(s string, slice []string) bool {
	return skill.IsSourceAllowed(s, slice)
}

func sourceAllowedByPolicy(source string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	return skill.IsSourceAllowed(source, allowed)
}
