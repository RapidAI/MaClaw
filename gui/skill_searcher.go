package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillSearchResult is one SkillMarket search result.
type SkillSearchResult struct {
	ID                           string                   `json:"id"`
	Name                         string                   `json:"name"`
	Description                  string                   `json:"description"`
	Tags                         []string                 `json:"tags"`
	Score                        float64                  `json:"score"`
	Price                        int                      `json:"price"`
	Status                       skillSearchSourceKind    `json:"status"`
	InstallRef                   string                   `json:"install_ref,omitempty"`
	AvgRating                    float64                  `json:"avg_rating"`
	DownloadCount                int                      `json:"download_count"`
	Version                      string                   `json:"version,omitempty"`
	Author                       string                   `json:"author,omitempty"`
	CreatedAt                    string                   `json:"created_at,omitempty"`
	PackageSHA256                string                   `json:"package_sha256,omitempty"`
	SHA256                       string                   `json:"sha256,omitempty"`
	PackageChecksum              string                   `json:"package_checksum,omitempty"`
	Checksum                     string                   `json:"checksum,omitempty"`
	PackageSignature             string                   `json:"package_signature,omitempty"`
	Signature                    string                   `json:"signature,omitempty"`
	PackageDownloadURL           string                   `json:"package_download_url,omitempty"`
	DownloadURL                  string                   `json:"download_url,omitempty"`
	PackageSize                  int64                    `json:"package_size,omitempty"`
	ProductKind                  string                   `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                     `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                   `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                   `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                   `json:"maclaw_app_description,omitempty"`
	MaclawAppKind                string                   `json:"maclaw_app_kind,omitempty"`
	MaclawAppCategory            string                   `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                   `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                   `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string                 `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                   `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppSearchEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                     `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string                 `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                   `json:"artifact_contract_presentation,omitempty"`
}

type MaclawAppSearchEvidence struct {
	RunID                 string         `json:"run_id,omitempty"`
	VerifiedAt            string         `json:"verified_at,omitempty"`
	DefinitionFingerprint string         `json:"definition_fingerprint,omitempty"`
	ArtifactPresent       bool           `json:"artifact_present,omitempty"`
	ArtifactName          string         `json:"artifact_name,omitempty"`
	OutputCount           int            `json:"output_count,omitempty"`
	PrimaryResult         string         `json:"primary_result,omitempty"`
	ResultPayload         map[string]any `json:"result_payload,omitempty"`
}

func (r SkillSearchResult) SourceKind() skillSearchSourceKind {
	return skillSearchSourceFromStatus(r.Status.String())
}

// MixedSkillSearchResult is the GUI-facing unified search result model.
type MixedSkillSearchResult struct {
	ID                           string                   `json:"id"`
	Name                         string                   `json:"name"`
	Description                  string                   `json:"description"`
	Tags                         []string                 `json:"tags"`
	Source                       string                   `json:"source"`
	SourceLabel                  string                   `json:"source_label"`
	InstallRef                   string                   `json:"install_ref,omitempty"`
	FilePath                     string                   `json:"file_path,omitempty"`
	Version                      string                   `json:"version,omitempty"`
	Author                       string                   `json:"author,omitempty"`
	CreatedAt                    string                   `json:"created_at,omitempty"`
	PackageSHA256                string                   `json:"package_sha256,omitempty"`
	SHA256                       string                   `json:"sha256,omitempty"`
	PackageChecksum              string                   `json:"package_checksum,omitempty"`
	Checksum                     string                   `json:"checksum,omitempty"`
	PackageSignature             string                   `json:"package_signature,omitempty"`
	Signature                    string                   `json:"signature,omitempty"`
	PackageDownloadURL           string                   `json:"package_download_url,omitempty"`
	DownloadURL                  string                   `json:"download_url,omitempty"`
	PackageSize                  int64                    `json:"package_size,omitempty"`
	TrustLevel                   string                   `json:"trust_level,omitempty"`
	AvgRating                    float64                  `json:"avg_rating"`
	RatingCount                  int                      `json:"rating_count"`
	Downloads                    int                      `json:"downloads"`
	Score                        float64                  `json:"score"`
	Price                        int                      `json:"price"`
	RepoURL                      string                   `json:"repo_url,omitempty"`
	Installed                    bool                     `json:"installed"`
	InstalledName                string                   `json:"installed_name,omitempty"`
	CanUpdate                    bool                     `json:"can_update"`
	HasUpdate                    bool                     `json:"has_update"`
	ProductKind                  string                   `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                     `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                   `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                   `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                   `json:"maclaw_app_description,omitempty"`
	MaclawAppKind                string                   `json:"maclaw_app_kind,omitempty"`
	MaclawAppCategory            string                   `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                   `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                   `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string                 `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                   `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppSearchEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                     `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string                 `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                   `json:"artifact_contract_presentation,omitempty"`
}

// SkillSearcher handles mixed skill search across sources.
type SkillSearcher struct {
	client *SkillMarketClient
	app    *App
}

// NewSkillSearcher creates a searcher.
func NewSkillSearcher(client *SkillMarketClient) *SkillSearcher {
	var app *App
	if client != nil {
		app = client.app
	}
	return &SkillSearcher{client: client, app: app}
}

// Search queries SkillMarket through the current HubCenter pool.
func (s *SkillSearcher) Search(ctx context.Context, query string, tags []string, topN int) ([]SkillSearchResult, error) {
	if s.client == nil || s.app == nil {
		return nil, fmt.Errorf("hubcenter client not initialized")
	}
	if topN <= 0 {
		topN = 20
	}

	params := url.Values{}
	params.Set("q", query)
	if len(tags) > 0 {
		params.Set("tags", strings.Join(tags, ","))
	}
	params.Set("top_n", fmt.Sprintf("%d", topN))

	var wrapper struct {
		Results []SkillSearchResult `json:"results"`
	}
	if _, _, err := s.app.getHubCenterJSON(ctx, &http.Client{Timeout: 15 * time.Second}, "/api/v1/skillmarket/search?"+params.Encode(), 0, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Results, nil
}

// ClawHubMirrorURL re-exports the shared constant for backward compatibility.
// New code should use cskill.ClawHubMirrorURL directly.
const ClawHubMirrorURL = cskill.ClawHubMirrorURL

// SearchAll aggregates SkillMarket, ClawHub mirror, and GitHub results for GUI search.
func (s *SkillSearcher) SearchAll(ctx context.Context, query string) ([]MixedSkillSearchResult, error) {
	var results []MixedSkillSearchResult
	var errs []string

	// Determine allowed sources from Hub policy / local config.
	var allowedSources []string
	if s.app != nil {
		allowedSources = s.app.GetAllowedSkillSources()
		cfg, cfgErr := s.app.LoadConfig()
		if cfgErr == nil && len(cfg.SkillSourcesAllowed) > 0 {
			if p := s.app.hubSecurityCache.get(); p == nil || (!p.CentralizedSecurity && !p.SkillSourcesRestricted && len(p.SkillSourcesAllowed) == 0) {
				allowedSources = mergeAllowedSkillSources(allowedSources, cfg.SkillSourcesAllowed)
			}
		}
		if cfgErr == nil && skillMarketplaceEnterpriseOnlySearch(cfg) {
			allowedSources = []string{corelib.CapabilitySourceEnterpriseHub}
		}
	}

	if isAllowedSkillSourceList(corelib.CapabilitySourceEnterpriseHub, allowedSources) || s.enterpriseHubSearchConfigured() {
		if ok, reason := s.allowSearchSource(corelib.CapabilitySourceEnterpriseHub, s.enterpriseHubURL(), query); !ok {
			errs = append(errs, fmt.Sprintf("enterprise_hub: %s", reason))
		} else {
			hubResults, err := s.searchEnterpriseHubSkills(ctx, query)
			if err != nil {
				errs = append(errs, fmt.Sprintf("enterprise_hub: %v", err))
			} else {
				results = append(results, hubResults...)
			}
		}
	}

	if isAllowedSkillSourceList("skillhub", allowedSources) {
		if ok, reason := s.allowSearchSource("skillhub", s.skillHubSearchURL(), query); !ok {
			errs = append(errs, fmt.Sprintf("skillmarket: %s", reason))
		} else {
			marketResults, err := s.Search(ctx, query, nil, 10)
			if err != nil {
				errs = append(errs, fmt.Sprintf("skillmarket: %v", err))
			} else {
				for _, r := range marketResults {
					results = append(results, s.toMixedSkillSearchResult(r))
				}
			}
		}
	}

	// ClawHub + GitHub via shared HubClient (single implementation).
	hubClient := cskill.DefaultHubClient()
	if isAllowedSkillSourceList("clawhub", allowedSources) {
		if ok, reason := s.allowSearchSource("clawhub", cskill.ClawHubMirrorURL, query); !ok {
			errs = append(errs, fmt.Sprintf("clawhub: %s", reason))
		} else {
			for _, r := range hubClient.SearchClawHub(ctx, query) {
				results = append(results, hubSearchResultToMixed(r))
			}
		}
	}
	if isAllowedSkillSourceList("github", allowedSources) && contextErr(ctx) == nil {
		if ok, reason := s.allowSearchSource("github", "https://github.com", query); !ok {
			errs = append(errs, fmt.Sprintf("github: %s", reason))
		} else {
			for _, r := range hubClient.SearchGitHub(query) {
				results = append(results, hubSearchResultToMixed(r))
			}
		}
	}

	s.enrichInstalledState(results)
	if len(errs) > 0 {
		log.Printf("[skill-search] partial failures query=%q errors=%s", query, strings.Join(errs, "; "))
	}
	if len(results) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	sort.SliceStable(results, func(i, j int) bool {
		li := sourcePriority(results[i].Source)
		lj := sourcePriority(results[j].Source)
		if li != lj {
			return li < lj
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	// Demote results with poor local execution history (P2 #A signal feedback).
	// This stable sort preserves the source-priority order for results with
	// equal local penalty, but pushes consistently-failing or disabled skills
	// to the bottom regardless of their Hub popularity.
	if s.app != nil && s.app.skillExecutor != nil {
		localSkills := s.app.skillExecutor.loadSkills()
		if len(localSkills) > 0 {
			skillMap := make(map[string]*corelib.NLSkillEntry, len(localSkills))
			for i := range localSkills {
				skillMap[localSkills[i].Name] = &localSkills[i]
			}
			sort.SliceStable(results, func(i, j int) bool {
				li := sourcePriority(results[i].Source)
				lj := sourcePriority(results[j].Source)
				if li != lj {
					return li < lj
				}
				pi := localSearchPenalty(results[i].Name, results[i].InstalledName, skillMap)
				pj := localSearchPenalty(results[j].Name, results[j].InstalledName, skillMap)
				return pi < pj
			})
		}
	}

	return results, nil
}

func (s *SkillSearcher) allowSearchSource(source, endpoint, query string) (bool, string) {
	if s == nil || s.app == nil {
		return true, ""
	}
	args := map[string]interface{}{"action": "search", "query": query, "source": source}
	if strings.TrimSpace(endpoint) != "" {
		args["url"] = endpoint
	}
	return s.app.enforceHubSecurityAppPolicy("manage_skill", args)
}

func (s *SkillSearcher) skillHubSearchURL() string {
	if s == nil || s.client == nil {
		return ""
	}
	return s.client.baseURL()
}

func (s *SkillSearcher) enterpriseHubSearchConfigured() bool {
	if s == nil || s.app == nil {
		return false
	}
	cfg, err := s.app.LoadConfig()
	if err != nil {
		return false
	}
	return firstNonEmpty(cfg.RemoteHubURL, cfg.RemoteHubCenterURL, s.skillHubSearchURL()) != ""
}
func (s *SkillSearcher) enterpriseHubURL() string {
	if s == nil || s.app == nil {
		return ""
	}
	cfg, err := s.app.LoadConfig()
	if err != nil {
		return ""
	}
	return firstNonEmpty(cfg.RemoteHubURL, cfg.RemoteHubCenterURL, s.skillHubSearchURL())
}

// localSearchPenalty returns a penalty for a search result based on local
// execution history. Checks both the result name and the installed name
// (which may differ from the Hub name).
func localSearchPenalty(name, installedName string, skillMap map[string]*corelib.NLSkillEntry) int {
	if s, ok := skillMap[name]; ok {
		return cskill.LocalPenalty(s)
	}
	if installedName != "" {
		if s, ok := skillMap[installedName]; ok {
			return cskill.LocalPenalty(s)
		}
	}
	return 0
}

func mergeAllowedSkillSources(primary []string, extra []string) []string {
	if primary == nil && len(extra) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(primary)+len(extra))
	for _, source := range append(append([]string(nil), primary...), extra...) {
		normalized := normalizeHubSkillSource(source)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		merged = append(merged, normalized)
	}
	return merged
}
func sourcePriority(source string) int {
	switch skillSearchSourceFromStatus(source) {
	case skillSearchSourceEnterpriseHub:
		return 0
	case skillSearchSourceSkillMarket, skillSearchSourceSkillHub:
		return 1
	case skillSearchSourceClawHub:
		return 2
	case skillSearchSourceGitHub:
		return 3
	default:
		return 4
	}
}

func mixedSourceLabel(source string) string {
	switch skillSearchSourceFromStatus(source) {
	case skillSearchSourceEnterpriseHub:
		return "私有市场"
	case skillSearchSourceSkillMarket, skillSearchSourceSkillHub:
		return "公共市场"
	case skillSearchSourceClawHub:
		return "ClawHub"
	case skillSearchSourceGitHub:
		return "GitHub"
	default:
		return source
	}
}

func skillMarketplaceEnterpriseOnlySearch(cfg corelib.AppConfig) bool {
	policy := cfg.CapabilityMarketPolicy.WithDefaults()
	return firstNonEmpty(cfg.RemoteHubURL, cfg.RemoteHubCenterURL) != "" && policy.EffectiveEnterpriseOnlySearch()
}
func (s *SkillSearcher) searchEnterpriseHubSkills(ctx context.Context, query string) ([]MixedSkillSearchResult, error) {
	if s.app == nil {
		return nil, nil
	}
	cfg, err := s.app.LoadConfig()
	if err != nil {
		return nil, err
	}

	client, err := newCapabilityMarketClient(cfg)
	if err != nil {
		fallbackBaseURL := strings.TrimRight(strings.TrimSpace(firstNonEmpty(cfg.RemoteHubCenterURL, s.skillHubSearchURL())), "/")
		fallbackToken := capabilityMarketAuthToken(cfg)
		if fallbackBaseURL != "" && fallbackToken != "" {
			client = &capabilityMarketClient{baseURL: fallbackBaseURL, token: fallbackToken, http: &http.Client{Timeout: 20 * time.Second}}
		} else if skillMarketplaceEnterpriseOnlySearch(cfg) {
			return nil, err
		} else {
			return nil, nil
		}
	}
	items, err := client.listCapabilities(ctx, corelib.CapabilityTypeSkill, query)
	if err != nil {
		return nil, err
	}
	results := make([]MixedSkillSearchResult, 0, len(items))
	for _, item := range items {
		if !strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeSkill) {
			continue
		}
		id := firstNonEmpty(item.ID, item.CapabilityID)
		if id == "" {
			continue
		}
		metadata := capabilityMetadataMap(item.MetadataJSON)
		productKind := stringFromMap(metadata, "product_kind")
		isMaclawApp := boolFromMap(metadata, "is_maclaw_app") || strings.EqualFold(productKind, "maclaw_app_skill") || stringFromMap(metadata, "maclaw_app_id") != ""
		results = append(results, MixedSkillSearchResult{
			ID:                           id,
			Name:                         firstNonEmpty(stringFromMap(metadata, "maclaw_app_name"), item.DisplayName, item.CapabilityID, item.ID),
			Description:                  firstNonEmpty(stringFromMap(metadata, "maclaw_app_description"), item.Description),
			Source:                       corelib.CapabilitySourceEnterpriseHub,
			SourceLabel:                  mixedSourceLabel(corelib.CapabilitySourceEnterpriseHub),
			InstallRef:                   id,
			Version:                      item.CurrentVersionKey,
			TrustLevel:                   "enterprise",
			Score:                        100,
			ProductKind:                  productKind,
			IsMaclawApp:                  isMaclawApp,
			MaclawAppID:                  stringFromMap(metadata, "maclaw_app_id"),
			MaclawAppName:                stringFromMap(metadata, "maclaw_app_name"),
			MaclawAppDescription:         stringFromMap(metadata, "maclaw_app_description"),
			MaclawAppKind:                stringFromMap(metadata, "maclaw_app_kind"),
			MaclawAppCategory:            stringFromMap(metadata, "maclaw_app_category"),
			MaclawAppIcon:                stringFromMap(metadata, "maclaw_app_icon"),
			MaclawAppDefinitionSHA256:    stringFromMap(metadata, "maclaw_app_definition_sha256"),
			MaclawAppTestEvidence:        maclawAppSearchEvidenceFromMap(metadata["maclaw_app_test_evidence"]),
			ArtifactContractPresentation: stringFromMap(metadata, "artifact_contract_presentation"),
		})
	}
	return results, nil
}
func (s *SkillSearcher) toMixedSkillSearchResult(r SkillSearchResult) MixedSkillSearchResult {
	source := string(r.SourceKind())
	return MixedSkillSearchResult{
		ID:                 r.ID,
		Name:               r.Name,
		Description:        r.Description,
		Tags:               r.Tags,
		Source:             source,
		SourceLabel:        mixedSourceLabel(source),
		TrustLevel:         mixedTrustLevel(source),
		AvgRating:          r.AvgRating,
		Downloads:          r.DownloadCount,
		Score:              r.Score,
		Price:              r.Price,
		Version:            r.Version,
		Author:             r.Author,
		CreatedAt:          r.CreatedAt,
		PackageSHA256:      firstNonEmpty(r.PackageSHA256, r.SHA256),
		SHA256:             r.SHA256,
		PackageChecksum:    firstNonEmpty(r.PackageChecksum, r.Checksum),
		Checksum:           r.Checksum,
		PackageSignature:   firstNonEmpty(r.PackageSignature, r.Signature),
		Signature:          r.Signature,
		PackageDownloadURL: firstNonEmpty(r.PackageDownloadURL, r.DownloadURL),
		DownloadURL:        r.DownloadURL,
		PackageSize:        r.PackageSize, ProductKind: r.ProductKind,
		IsMaclawApp:                  r.IsMaclawApp || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill"),
		MaclawAppID:                  r.MaclawAppID,
		MaclawAppName:                r.MaclawAppName,
		MaclawAppDescription:         r.MaclawAppDescription,
		MaclawAppCategory:            r.MaclawAppCategory,
		MaclawAppIcon:                r.MaclawAppIcon,
		MaclawAppInputMode:           r.MaclawAppInputMode,
		MaclawAppOutputModes:         append([]string(nil), r.MaclawAppOutputModes...),
		MaclawAppDefinitionSHA256:    r.MaclawAppDefinitionSHA256,
		MaclawAppTestEvidence:        cloneMaclawAppSearchEvidence(r.MaclawAppTestEvidence),
		ArtifactContractRequired:     r.ArtifactContractRequired,
		ArtifactContractOutputModes:  append([]string(nil), r.ArtifactContractOutputModes...),
		ArtifactContractPresentation: r.ArtifactContractPresentation,
	}
}

func mixedTrustLevel(source string) string {
	switch skillSearchSourceFromStatus(source) {
	case skillSearchSourceEnterpriseHub:
		return "enterprise"
	case skillSearchSourceClawHub, skillSearchSourceGitHub:
		return "community"
	default:
		return ""
	}
}

// hubSearchResultToMixed converts a shared HubSearchResult (from corelib/skill.HubClient)
// to the GUI-specific MixedSkillSearchResult display type.
func hubSearchResultToMixed(r cskill.HubSearchResult) MixedSkillSearchResult {
	return MixedSkillSearchResult{
		ID:                           r.ID,
		Name:                         r.Name,
		Description:                  r.Description,
		Source:                       r.Source,
		SourceLabel:                  mixedSourceLabel(r.Source),
		TrustLevel:                   mixedTrustLevel(r.Source),
		Version:                      r.Version,
		Author:                       r.Author,
		AvgRating:                    r.AvgRating,
		Downloads:                    r.Downloads,
		Score:                        r.Score,
		InstallRef:                   r.InstallRef,
		FilePath:                     r.FilePath,
		RepoURL:                      r.RepoURL,
		ProductKind:                  r.ProductKind,
		IsMaclawApp:                  r.IsMaclawApp || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill"),
		MaclawAppID:                  r.MaclawAppID,
		MaclawAppName:                r.MaclawAppName,
		MaclawAppDescription:         r.MaclawAppDescription,
		MaclawAppKind:                r.ProductKind,
		MaclawAppCategory:            r.MaclawAppCategory,
		MaclawAppIcon:                r.MaclawAppIcon,
		MaclawAppInputMode:           r.MaclawAppInputMode,
		MaclawAppOutputModes:         append([]string(nil), r.MaclawAppOutputModes...),
		MaclawAppDefinitionSHA256:    r.MaclawAppDefinitionSHA256,
		MaclawAppTestEvidence:        cloneCoreMaclawAppSearchEvidence(r.MaclawAppTestEvidence),
		ArtifactContractRequired:     r.ArtifactContractRequired,
		ArtifactContractOutputModes:  append([]string(nil), r.ArtifactContractOutputModes...),
		ArtifactContractPresentation: r.ArtifactContractPresentation,
	}
}

func cloneMaclawAppSearchEvidence(e *MaclawAppSearchEvidence) *MaclawAppSearchEvidence {
	if e == nil {
		return nil
	}
	copy := *e
	return &copy
}

func cloneCoreMaclawAppSearchEvidence(e *cskill.MaclawAppTestEvidence) *MaclawAppSearchEvidence {
	if e == nil {
		return nil
	}
	return &MaclawAppSearchEvidence{
		RunID:                 e.RunID,
		VerifiedAt:            e.VerifiedAt,
		DefinitionFingerprint: e.DefinitionFingerprint,
		ArtifactPresent:       e.ArtifactPresent,
		ArtifactName:          e.ArtifactName,
		OutputCount:           e.OutputCount,
		PrimaryResult:         e.PrimaryResult,
		ResultPayload:         cloneMapAny(e.ResultPayload),
	}
}

func boolFromMap(m map[string]any, key string) bool {
	value, ok := m[key]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes") || strings.TrimSpace(v) == "1"
	default:
		return false
	}
}

func maclawAppSearchEvidenceFromMap(value any) *MaclawAppSearchEvidence {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	return &MaclawAppSearchEvidence{
		RunID:                 firstNonEmpty(stringFromMap(raw, "run_id"), stringFromMap(raw, "runId")),
		VerifiedAt:            firstNonEmpty(stringFromMap(raw, "verified_at"), stringFromMap(raw, "verifiedAt")),
		DefinitionFingerprint: firstNonEmpty(stringFromMap(raw, "definition_fingerprint"), stringFromMap(raw, "definitionFingerprint")),
		ArtifactPresent:       boolFromMap(raw, "artifact_present") || boolFromMap(raw, "artifactPresent"),
		ArtifactName:          firstNonEmpty(stringFromMap(raw, "artifact_name"), stringFromMap(raw, "artifactName")),
		OutputCount:           intFromMap(raw, "output_count", "outputCount"),
		PrimaryResult:         firstNonEmpty(stringFromMap(raw, "primary_result"), stringFromMap(raw, "primaryResult")),
		ResultPayload:         mapAnyFromMap(raw, "result_payload", "resultPayload"),
	}
}

func intFromMap(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}

func mapAnyFromMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if raw, ok := m[key].(map[string]any); ok {
			return cloneMapAny(raw)
		}
	}
	return nil
}
func (s *SkillSearcher) enrichInstalledState(results []MixedSkillSearchResult) {
	if s.app == nil || s.app.skillExecutor == nil {
		return
	}
	skills := s.app.skillExecutor.loadSkills()
	// Read update info from cache — zero HTTP requests.
	// The cache is populated by CheckHubSkillUpdates (frontend tab switch)
	// or refreshHubUpdateCacheAsync (background).
	updatesByHubID := s.app.getCachedHubUpdates() // may be nil if cache cold
	for i := range results {
		for _, skill := range skills {
			if mixedResultMatchesSkill(results[i], skill) {
				results[i].Installed = true
				results[i].InstalledName = skill.Name
				if normalizeSkillEntrySource(skill.Source) == skillEntrySourceHub && skill.HubSkillID != "" {
					results[i].CanUpdate = true
					if updatesByHubID != nil {
						results[i].HasUpdate = updatesByHubID[skill.HubSkillID]
					}
				}
				break
			}
		}
	}
}

func mixedResultMatchesSkill(result MixedSkillSearchResult, skill corelib.NLSkillEntry) bool {
	switch skillSearchSourceFromStatus(result.Source) {
	case skillSearchSourceEnterpriseHub:
		return skill.Capability != nil && (strings.EqualFold(skill.Capability.CapabilityID, result.ID))
	case skillSearchSourceSkillMarket, skillSearchSourceSkillHub:
		return normalizeSkillEntrySource(skill.Source) == skillEntrySourceHub && skill.HubSkillID == result.ID
	case skillSearchSourceClawHub:
		return normalizeSkillEntrySource(skill.Source) == skillEntrySourceClawHub && strings.EqualFold(skill.Name, result.Name)
	case skillSearchSourceGitHub:
		return normalizeSkillEntrySource(skill.Source) == skillEntrySourceGitHub && (strings.EqualFold(skill.SourceProject, result.RepoURL) || strings.EqualFold(skill.Name, result.Name))
	default:
		return false
	}
}

// SearchAndInstall searches and auto-installs the best matching skill.
// Search order: SkillMarket, then ClawHub mirror, then GitHub.
func (s *SkillSearcher) SearchAndInstall(ctx context.Context, query string) (*SkillSearchResult, error) {
	return s.SearchAndInstallForTask(ctx, query, query)
}

// SearchAndInstallForTask searches with query, then validates candidate
// capabilities against taskText. Retrieval text and user intent text can differ:
// async capability repair may distill a short search query from a failure, while
// compatibility must still reflect the original user action.
func (s *SkillSearcher) SearchAndInstallForTask(ctx context.Context, query string, taskText string) (*SkillSearchResult, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	taskText = strings.TrimSpace(taskText)
	if query == "" {
		return nil, nil
	}
	if taskText == "" {
		taskText = query
	}
	allowedSources := []string(nil)
	if s.app != nil {
		allowedSources = s.app.GetAllowedSkillSources()
	}
	var results []SkillSearchResult
	var blocked []string
	if isAllowedSkillSourceList("skillhub", allowedSources) {
		if ok, reason := s.allowSearchSource("skillhub", s.skillHubSearchURL(), query); !ok {
			blocked = append(blocked, "skillhub: "+reason)
		} else {
			var err error
			results, err = s.Search(ctx, query, nil, 5)
			if err != nil {
				log.Printf("[skill-search] skillmarket search error: %v", err)
			}
		}
	}
	if len(results) == 0 && isAllowedSkillSourceList("clawhub", allowedSources) {
		if ok, reason := s.allowSearchSource("clawhub", cskill.ClawHubMirrorURL, query); !ok {
			blocked = append(blocked, "clawhub: "+reason)
		} else {
			// Step 2: try ClawHub mirror.
			log.Printf("[skill-search] no skillmarket results for: %s, trying ClawHub mirror...", query)
			results = s.searchClawHubMirror(ctx, query)
		}
	}
	if len(results) == 0 && isAllowedSkillSourceList("github", allowedSources) {
		if ok, reason := s.allowSearchSource("github", "https://github.com", query); !ok {
			blocked = append(blocked, "github: "+reason)
		} else {
			// Step 3: GitHub fallback
			log.Printf("[skill-search] no ClawHub results for: %s, trying GitHub fallback...", query)
			best, err := s.searchGitHubFallback(ctx, query, taskText)
			if err != nil || best == nil {
				return best, err
			}
			results = []SkillSearchResult{*best}
		}
	}
	if len(results) == 0 {
		if len(blocked) > 0 {
			return nil, fmt.Errorf("skill search blocked by security policy: %s", strings.Join(blocked, "; "))
		}
		return nil, nil
	}

	// Apply configured purchase mode filter.
	mode := s.client.getSkillPurchaseMode()
	if mode == "free_only" {
		var filtered []SkillSearchResult
		for _, r := range results {
			if r.Price == 0 {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			log.Printf("[skill-search] no free results for query (free_only mode): %s", query)
			return nil, nil
		}
		results = filtered
	}

	results = filterSkillSearchResultsForIntent(taskText, results)
	if len(results) == 0 {
		log.Printf("[skill-search] no intent-compatible results for query: %s", query)
		return nil, nil
	}

	// Choose the first result; results are already sorted by quality.
	best := &results[0]
	return best, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func filterSkillSearchResultsForIntent(query string, results []SkillSearchResult) []SkillSearchResult {
	if len(results) == 0 {
		return results
	}
	userIntent := extractUserIntentCategory(query)
	filtered := results[:0]
	for _, result := range results {
		skillText := strings.TrimSpace(result.Name + " " + result.Description)
		if isTaskCompatibleWithSkillCandidate(userIntent, query, skillText) {
			filtered = append(filtered, result)
		} else {
			log.Printf("[skill-search] rejected intent-incompatible skill candidate %q for query intent %q", result.Name, userIntent)
		}
	}
	return filtered
}

// searchClawHubMirror queries the ClawHub China mirror for skills.
// Delegates to the shared HubClient and converts results to the legacy
// SkillSearchResult format (used by SearchAndInstall).
func (s *SkillSearcher) searchClawHubMirror(ctx context.Context, query string) []SkillSearchResult {
	hubClient := cskill.DefaultHubClient()
	hubResults := hubClient.SearchClawHub(ctx, query)
	if len(hubResults) == 0 {
		return nil
	}
	results := make([]SkillSearchResult, 0, len(hubResults))
	for _, r := range hubResults {
		results = append(results, SkillSearchResult{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Score:       r.Score,
			Status:      skillSearchSourceClawHub,
		})
	}
	log.Printf("[skill-search] clawhub mirror found %d results for: %s", len(results), query)
	return results
}

// searchGitHubFallback searches GitHub for skill.yaml files when SkillMarket
// returns no results. It filters by task intent before choosing the top result.
func (s *SkillSearcher) searchGitHubFallback(ctx context.Context, query string, taskText string) (*SkillSearchResult, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	gs := cskill.NewGitHubSearcher("")
	candidates, err := gs.SearchGitHub(query)
	if err != nil {
		log.Printf("[skill-search] GitHub fallback error: %v", err)
		return nil, nil
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		log.Printf("[skill-search] GitHub fallback: no results for query: %s", query)
		return nil, nil
	}
	candidates = filterGitHubSkillCandidatesForIntent(taskText, candidates)
	if len(candidates) == 0 {
		log.Printf("[skill-search] GitHub fallback: no intent-compatible results for query: %s", query)
		return nil, nil
	}
	best := candidates[0]
	installRefBytes, _ := json.Marshal(best)
	log.Printf("[skill-search] GitHub fallback found: %s (stars=%d)", best.RepoFullName, best.Stars)
	return &SkillSearchResult{
		ID:          best.RepoFullName,
		Name:        best.RepoFullName,
		Description: best.Description,
		Status:      skillSearchSourceGitHub,
		InstallRef:  string(installRefBytes),
	}, nil
}
