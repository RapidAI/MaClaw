package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pageSize = 40

// SkillStore manages Hub Center skill storage on disk.
type SkillStore struct {
	mu          sync.RWMutex
	dir         string
	index       []HubSkillMeta
	skills      map[string]*HubSkillFull
	suites      map[string]*SkillSuiteFull
	ratings     map[string][]SkillRating
	syncMu      sync.Mutex
	sync        SyncRecorder
	syncRunning bool
	syncPending bool
}

func NewSkillStore(dir string) *SkillStore {
	s := &SkillStore{
		dir:     dir,
		skills:  make(map[string]*HubSkillFull),
		suites:  make(map[string]*SkillSuiteFull),
		ratings: make(map[string][]SkillRating),
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = s.RebuildIndex()
	s.loadAllSuites()
	s.loadAllRatings()
	return s
}

// PublishSuite persists a Suite and its member references. Existing download
// and timestamp fields are preserved on update.
func (s *SkillStore) PublishSuite(suite SkillSuiteFull) error {
	suite.ID = strings.TrimSpace(suite.ID)
	suite.Version = strings.TrimSpace(suite.Version)
	suite.Status = strings.TrimSpace(suite.Status)
	suite.TrustLevel = strings.TrimSpace(suite.TrustLevel)
	statusProvided := strings.TrimSpace(suite.Status) != ""
	if strings.TrimSpace(suite.ID) == "" {
		return fmt.Errorf("suite id is required")
	}
	if strings.ContainsAny(suite.ID, `/\\`) || suite.ID == "." || suite.ID == ".." {
		return fmt.Errorf("invalid suite id")
	}
	if !validSuiteID(suite.ID) {
		return fmt.Errorf("invalid suite id")
	}
	if len(suite.Members) == 0 {
		return fmt.Errorf("suite must contain at least one member")
	}
	if len(suite.Members) > 32 {
		return fmt.Errorf("suite must contain at most 32 members")
	}
	if suite.Price < 0 {
		return fmt.Errorf("suite price cannot be negative")
	}
	seenMembers := make(map[string]struct{}, len(suite.Members))
	for i := range suite.Members {
		suite.Members[i].SkillID = strings.TrimSpace(suite.Members[i].SkillID)
		id := suite.Members[i].SkillID
		if id == "" {
			return fmt.Errorf("suite member skill id is required")
		}
		if !validSuiteID(id) {
			return fmt.Errorf("invalid suite member skill id: %s", id)
		}
		if _, ok := seenMembers[id]; ok {
			return fmt.Errorf("duplicate suite member: %s", id)
		}
		if p := strings.TrimSpace(suite.Members[i].Path); p != "" {
			n := strings.ReplaceAll(p, "\\", "/")
			clean := path.Clean(n)
			if strings.HasPrefix(n, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || (len(clean) > 1 && clean[1] == ':') {
				return fmt.Errorf("invalid suite member path: %s", p)
			}
			suite.Members[i].Path = clean
		}
		seenMembers[id] = struct{}{}
	}
	if len(suite.Skills) > 0 {
		if len(suite.Skills) != len(suite.Members) {
			return fmt.Errorf("suite members must match embedded skills")
		}
		skillIDs := make(map[string]struct{}, len(suite.Skills))
		for _, item := range suite.Skills {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				return fmt.Errorf("embedded suite skill id is required")
			}
			if _, exists := skillIDs[id]; exists {
				return fmt.Errorf("duplicate embedded suite skill: %s", id)
			}
			skillIDs[id] = struct{}{}
		}
		for id := range seenMembers {
			if _, ok := skillIDs[id]; !ok {
				return fmt.Errorf("suite member %s has no embedded skill", id)
			}
		}
	}
	if suite.CreatedAt == "" {
		suite.CreatedAt = fmtTimeNow()
	}
	if suite.UpdatedAt == "" {
		suite.UpdatedAt = suite.CreatedAt
	}
	if suite.Status == "" {
		suite.Status = "published"
	}
	if suite.Manifest.Format == "" {
		suite.Manifest.Format = "skill-suite.v1"
	}
	if suite.Manifest.GeneratedAt == "" {
		suite.Manifest.GeneratedAt = suite.UpdatedAt
	}
	s.mu.Lock()
	if s.suites == nil {
		s.suites = make(map[string]*SkillSuiteFull)
	}
	if old := s.suites[suite.ID]; old != nil {
		// Persist the complete previous revision so operators can roll back,
		// while keeping the compact VersionHistory metadata for API clients.
		if err := s.persistSuiteVersion(*old); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("persist suite version: %w", err)
		}
		suite.CreatedAt = old.CreatedAt
		suite.UpdatedAt = fmtTimeNow()
		// Keep operational counters and moderation metadata across idempotent
		// updates; these belong to the Suite identity rather than a revision.
		suite.Downloads = old.Downloads
		if !statusProvided {
			suite.Status = old.Status
		}
		if statusProvided && strings.EqualFold(strings.TrimSpace(suite.Status), "withdrawn") {
			suite.Visible = false
		} else if strings.EqualFold(strings.TrimSpace(suite.Status), "published") && !strings.EqualFold(strings.TrimSpace(old.Status), "published") {
			suite.Visible = true
		} else {
			suite.Visible = old.Visible
		}
		if strings.TrimSpace(suite.TrustLevel) == "" {
			suite.TrustLevel = old.TrustLevel
		}
		history := append([]SuiteVersionSummary(nil), old.VersionHistory...)
		if strings.TrimSpace(old.Version) != "" && old.Version != suite.Version {
			history = append(history, SuiteVersionSummary{Version: old.Version, SourceRevision: old.SourceRevision, UpdatedAt: old.UpdatedAt})
			if len(history) > 20 {
				history = history[len(history)-20:]
			}
		}
		suite.VersionHistory = history
	} else {
		suite.Visible = true
	}
	data, err := json.MarshalIndent(&suite, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(s.dir, "suite-"+suite.ID+".json"), data, 0o644)
	}
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write suite: %w", err)
	}
	s.suites[suite.ID] = &suite
	s.mu.Unlock()
	// Keep Suite updates consistent with ordinary Skill publishes so HA
	// replicas receive the complete snapshot (including Suite members).
	s.emitSync(context.Background())
	return nil
}

func validSuiteID(id string) bool {
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

func suiteVersionKey(version string) string {
	sum := sha256.Sum256([]byte(version))
	return hex.EncodeToString(sum[:])
}

func (s *SkillStore) persistSuiteVersion(suite SkillSuiteFull) error {
	dir := filepath.Join(s.dir, "suite-versions", suite.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(&suite, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, suiteVersionKey(suite.Version)+".json"), b, 0o644)
}

// RollbackSuite restores a previously published complete Suite revision.
func (s *SkillStore) RollbackSuite(id, version string) (*SkillSuiteFull, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	s.mu.RLock()
	current := s.suites[id]
	var currentDownloads int
	var currentCreatedAt string
	if current != nil {
		currentDownloads, currentCreatedAt = current.Downloads, current.CreatedAt
	}
	s.mu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("suite not found: %s", id)
	}
	if current.Version == version {
		cp := *current
		return &cp, nil
	}
	path := filepath.Join(s.dir, "suite-versions", id, suiteVersionKey(version)+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("suite version %s not found", version)
	}
	var target SkillSuiteFull
	if err = json.Unmarshal(b, &target); err != nil {
		return nil, err
	}
	if target.ID == "" {
		target.ID = id
	}
	// Operational counters and creation identity belong to the Suite, not a
	// particular revision, so rolling back must not reset them.
	target.Downloads = currentDownloads
	target.CreatedAt = currentCreatedAt
	target.UpdatedAt = ""
	if err = s.PublishSuite(target); err != nil {
		return nil, err
	}
	return s.GetSuite(id)
}

func (s *SkillStore) GetSuite(id string) (*SkillSuiteFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	su, ok := s.suites[id]
	if !ok {
		return nil, fmt.Errorf("suite not found: %s", id)
	}
	cp := *su
	cp.Members = append([]SkillSuiteMember(nil), su.Members...)
	cp.Skills = append([]HubSkillFull(nil), su.Skills...)
	cp.Tags = append([]string(nil), su.Tags...)
	cp.Permissions = append([]string(nil), su.Permissions...)
	cp.VersionHistory = append([]SuiteVersionSummary(nil), su.VersionHistory...)
	return &cp, nil
}

// SetSuiteVisibility updates moderation visibility without creating a new
// revision or resetting Suite counters.
func (s *SkillStore) SetSuiteVisibility(id string, visible bool, status string) error {
	s.mu.Lock()
	su, ok := s.suites[strings.TrimSpace(id)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("suite not found: %s", id)
	}
	cp := *su
	cp.Visible = visible
	if strings.TrimSpace(status) != "" {
		cp.Status = strings.TrimSpace(status)
	}
	cp.UpdatedAt = fmtTimeNow()
	b, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, "suite-"+cp.ID+".json"), b, 0o644); err != nil {
		s.mu.Unlock()
		return err
	}
	s.suites[cp.ID] = &cp
	s.mu.Unlock()
	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) ListSuites() []SkillSuiteFull {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SkillSuiteFull, 0, len(s.suites))
	for _, su := range s.suites {
		if su.Visible {
			cp := *su
			cp.Members = append([]SkillSuiteMember(nil), su.Members...)
			// Catalog responses expose metadata only; full member payloads are
			// served by GET /skill-suites/{id} and /download.
			cp.Skills = nil
			cp.Tags = append([]string(nil), su.Tags...)
			cp.Permissions = append([]string(nil), su.Permissions...)
			cp.VersionHistory = append([]SuiteVersionSummary(nil), su.VersionHistory...)
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// ListSuitesAll returns Suite metadata for administrators, including hidden
// and withdrawn entries.
func (s *SkillStore) ListSuitesAll() []SkillSuiteFull {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SkillSuiteFull, 0, len(s.suites))
	for _, su := range s.suites {
		cp := *su
		cp.Skills = nil
		cp.Members = append([]SkillSuiteMember(nil), su.Members...)
		cp.Tags = append([]string(nil), su.Tags...)
		cp.Permissions = append([]string(nil), su.Permissions...)
		cp.VersionHistory = append([]SuiteVersionSummary(nil), su.VersionHistory...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// SearchSuites performs a lightweight name/description/tag search over
// visible suites. Pagination follows the skill catalog conventions.
func (s *SkillStore) SearchSuites(query string, page int) SkillSuiteSearchResult {
	if page < 1 {
		page = 1
	}
	q := strings.ToLower(strings.TrimSpace(query))
	s.mu.RLock()
	items := make([]SkillSuiteMeta, 0, len(s.suites))
	for _, su := range s.suites {
		if !su.Visible {
			continue
		}
		if q != "" {
			matched := strings.Contains(strings.ToLower(su.Name), q) || strings.Contains(strings.ToLower(su.Description), q)
			if !matched {
				for _, tag := range su.Tags {
					if strings.Contains(strings.ToLower(tag), q) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		meta := su.SkillSuiteMeta
		meta.Tags = append([]string(nil), su.Tags...)
		meta.Members = append([]SkillSuiteMember(nil), su.Members...)
		meta.VersionHistory = append([]SuiteVersionSummary(nil), su.VersionHistory...)
		items = append(items, meta)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return SkillSuiteSearchResult{Suites: []SkillSuiteMeta{}, Total: total, Page: page}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return SkillSuiteSearchResult{Suites: items[start:end], Total: total, Page: page}
}

func (s *SkillStore) loadAllSuites() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	suites := make(map[string]*SkillSuiteFull)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "suite-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var su SkillSuiteFull
		if json.Unmarshal(data, &su) == nil && su.ID != "" {
			suites[su.ID] = &su
		}
	}
	s.mu.Lock()
	s.suites = suites
	s.mu.Unlock()
}

func (s *SkillStore) Search(query string, tags []string, page int) SkillSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)
	// Choose the current revision before matching. Otherwise a query that only
	// matches an old title or tag could surface the latest revision even though
	// that revision itself does not match the user's search.
	matched := groupSkillVersions(s.index)
	// A hidden latest revision hides the entire public catalog entry. Do not
	// silently fall back to an older visible revision, because clients would
	// otherwise install a superseded release as if it were current.
	matched = onlyVisibleSkillGroups(matched)
	matched = filterMatchingSkills(matched, queryTerms, tags)
	total := len(matched)
	start := (page - 1) * pageSize
	if start >= total {
		return SkillSearchResult{Skills: []HubSkillMeta{}, Total: total, Page: page}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return SkillSearchResult{Skills: matched[start:end], Total: total, Page: page}
}

func (s *SkillStore) ListAll(page int) SkillSearchResult {
	return s.ListAllPaged(page, pageSize)
}

func (s *SkillStore) ListAllPaged(page, perPage int) SkillSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = pageSize
	}
	items := groupSkillVersions(s.index)
	total := len(items)
	start := (page - 1) * perPage
	if start >= total {
		return SkillSearchResult{Skills: []HubSkillMeta{}, Total: total, Page: page}
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return SkillSearchResult{Skills: items[start:end], Total: total, Page: page}
}

func (s *SkillStore) Get(id string) (*HubSkillFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return skill, nil
}

// GetVisible returns a public skill only when the selected revision is visible.
// Administrative callers should use Get so they can review hidden revisions.
func (s *SkillStore) GetVisible(id string) (*HubSkillFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[id]
	if !ok || !skill.Visible {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return skill, nil
}

// GetCurrentVisible returns a public skill only when it is both visible and
// the current revision of its version group. Historical revisions remain
// readable through GetVisible for the version-details view, but cannot be
// downloaded or mutated through public endpoints.
func (s *SkillStore) GetCurrentVisible(id string) (*HubSkillFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[id]
	if !ok || !skill.Visible || !isCurrentRevision(s.index, skill.HubSkillMeta) {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return skill, nil
}

// skillFilePath resolves a skill ID to its on-disk JSON path, refusing any ID
// that escapes the store directory.
//
// P0 (2026-09-09 review): the previous code did filepath.Join(s.dir, sk.ID)
// with no validation, so "../../etc/foo" wrote outside s.dir. The check is
// done on the cleaned path AND re-verified with filepath.Rel after joining,
// because on Windows a leading separator or a drive-relative form can survive
// a naive ".." substring test.
func (s *SkillStore) skillFilePath(id string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(id))
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "", fmt.Errorf("invalid skill id %q", id)
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, `\`) {
		return "", fmt.Errorf("invalid skill id %q", id)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) ||
		strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, `..\`) ||
		strings.Contains(cleaned, "/../") || strings.Contains(cleaned, `\..\`) ||
		strings.HasSuffix(cleaned, "/..") || strings.HasSuffix(cleaned, `\..`) {
		return "", fmt.Errorf("invalid skill id %q", id)
	}
	// Backslashes are separators on Windows and legal filename characters on
	// POSIX; rejecting them keeps the ID unambiguous across platforms.
	if strings.ContainsAny(cleaned, `\`) {
		return "", fmt.Errorf("invalid skill id %q", id)
	}
	dir := filepath.Clean(s.dir)
	target := filepath.Join(dir, cleaned+".json")
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid skill id %q", id)
	}
	return target, nil
}

func (s *SkillStore) Publish(sk HubSkillFull) error {
	s.mu.Lock()
	if existing, ok := s.skills[sk.ID]; ok {
		// Preserve moderation fields when an idempotent Suite re-import updates
		// the member payload. New publishes remain visible by default.
		if sk.Status == "" {
			sk.Status = existing.Status
		}
		if sk.TrustLevel == "" {
			sk.TrustLevel = existing.TrustLevel
		}
		sk.Downloads = existing.Downloads
		sk.DownloadCount = existing.DownloadCount
		sk.RatingSum = existing.RatingSum
		sk.RatingCount = existing.RatingCount
		sk.AvgRating = existing.AvgRating
		sk.CreatedAt = existing.CreatedAt
		sk.UpdatedAt = fmtTimeNow()
	} else {
		sk.Visible = true
	}

	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("marshal skill: %w", err)
	}
	// P0 (2026-09-09 review): sk.ID reached filepath.Join unvalidated, so an
	// ID of "../../<path>" wrote a .json file outside the skill directory
	// (arbitrary file write reachable by any authenticated publisher).
	path, err := s.skillFilePath(sk.ID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write skill file: %w", err)
	}
	s.skills[sk.ID] = &sk
	s.rebuildIndexFromSkills()
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) GetByID(id string) *HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sk, ok := s.skills[id]; ok {
		m := sk.HubSkillMeta
		return &m
	}
	return nil
}

// FindBySkillID returns the current revision for a stable publisher.name
// skill_id, including a current revision linked through a legacy identity
// alias (such as the upload fingerprint).
func (s *SkillStore) FindBySkillID(skillID string) *HubSkillMeta {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, current := range groupSkillVersions(s.index) {
		if strings.EqualFold(current.SkillID, skillID) {
			result := current
			return &result
		}
		for _, revision := range current.VersionHistory {
			if candidate, ok := s.skills[revision.ID]; ok && strings.EqualFold(candidate.SkillID, skillID) {
				result := current
				return &result
			}
		}
	}
	return nil
}

func fmtTimeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *SkillStore) RebuildIndex() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skill dir: %w", err)
	}
	skills := make(map[string]*HubSkillFull)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), "_ratings.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var sk HubSkillFull
		if err := json.Unmarshal(data, &sk); err != nil {
			continue
		}
		if sk.ID == "" {
			continue
		}
		// Migrate legacy trust_level: empty or "unknown" → "trusted"
		if sk.TrustLevel == "" || sk.TrustLevel == "unknown" {
			sk.TrustLevel = "trusted"
			// Persist migration to disk so it only happens once
			if migrated, err := json.MarshalIndent(&sk, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(s.dir, entry.Name()), migrated, 0o644)
			}
		}
		skills[sk.ID] = &sk
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills = skills
	s.rebuildIndexFromSkills()
	return nil
}

func (s *SkillStore) rebuildIndexFromSkills() {
	index := make([]HubSkillMeta, 0, len(s.skills))
	for _, sk := range s.skills {
		index = append(index, sk.HubSkillMeta)
	}
	// Keep the paginated admin catalog deterministic and useful. Map iteration
	// order is intentionally random, which could hide a newly uploaded skill on
	// an arbitrary later page of the capability catalog.
	sort.Slice(index, func(i, j int) bool {
		if index[i].UpdatedAt != index[j].UpdatedAt {
			return index[i].UpdatedAt > index[j].UpdatedAt
		}
		if index[i].CreatedAt != index[j].CreatedAt {
			return index[i].CreatedAt > index[j].CreatedAt
		}
		return index[i].ID < index[j].ID
	})
	s.index = index
}

func (s *SkillStore) TopByDownloads(n int) []HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.index) == 0 {
		return nil
	}
	visible := onlyVisibleSkillGroups(groupSkillVersions(s.index))
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Downloads > visible[j].Downloads
	})
	if n > len(visible) {
		n = len(visible)
	}
	return visible[:n]
}

func (s *SkillStore) SetVisibility(id string, visible bool) error {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.Visible = visible
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("marshal skill: %w", err)
	}
	path := filepath.Join(s.dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write skill file: %w", err)
	}
	s.rebuildIndexFromSkills()
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

// SetTrustLevel updates the trust level of a skill.
// Valid values: "builtin", "trusted", "community", "agent-created".
func (s *SkillStore) SetTrustLevel(id string, trustLevel string) error {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.TrustLevel = trustLevel
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("marshal skill: %w", err)
	}
	path := filepath.Join(s.dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write skill file: %w", err)
	}
	s.rebuildIndexFromSkills()
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) DeleteSkill(id string) error {
	s.mu.Lock()
	if _, ok := s.skills[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", id)
	}
	_ = os.Remove(filepath.Join(s.dir, id+".json"))
	_ = os.Remove(filepath.Join(s.dir, id+"_ratings.json"))
	delete(s.skills, id)
	delete(s.ratings, id)
	s.rebuildIndexFromSkills()
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) Rate(skillID, maclawID string, score int) error {
	if score < 1 || score > 5 {
		return fmt.Errorf("score must be between 1 and 5")
	}
	s.mu.Lock()
	sk, ok := s.skills[skillID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", skillID)
	}
	if !sk.Visible {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", skillID)
	}
	if !isCurrentRevision(s.index, sk.HubSkillMeta) {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", skillID)
	}
	ratings := s.ratings[skillID]
	found := false
	oldScore := 0
	for i, r := range ratings {
		if r.MaclawID == maclawID {
			oldScore = r.Score
			ratings[i].Score = score
			ratings[i].CreatedAt = time.Now().Format(time.RFC3339)
			found = true
			break
		}
	}
	if found {
		sk.RatingSum = sk.RatingSum - oldScore + score
	} else {
		ratings = append(ratings, SkillRating{
			SkillID:   skillID,
			MaclawID:  maclawID,
			Score:     score,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		sk.RatingSum += score
		sk.RatingCount++
	}
	if sk.RatingCount > 0 {
		sk.AvgRating = float64(sk.RatingSum) / float64(sk.RatingCount)
	}
	s.ratings[skillID] = ratings
	if err := s.saveRatings(skillID); err != nil {
		s.mu.Unlock()
		return err
	}
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, skillID+".json"), data, 0o644); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) saveRatings(skillID string) error {
	ratings := s.ratings[skillID]
	data, err := json.MarshalIndent(ratings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ratings: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, skillID+"_ratings.json"), data, 0o644)
}

func (s *SkillStore) loadAllRatings() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "_ratings.json") {
			continue
		}
		skillID := strings.TrimSuffix(name, "_ratings.json")
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		var ratings []SkillRating
		if json.Unmarshal(data, &ratings) == nil {
			s.ratings[skillID] = ratings
		}
	}
}

func matchesSkill(meta HubSkillMeta, queryTerms []string, tags []string) bool {
	if len(queryTerms) == 0 && len(tags) == 0 {
		return true
	}
	if len(tags) > 0 {
		tagSet := make(map[string]struct{}, len(meta.Tags))
		for _, t := range meta.Tags {
			tagSet[strings.ToLower(t)] = struct{}{}
		}
		for _, t := range tags {
			if _, ok := tagSet[strings.ToLower(t)]; !ok {
				return false
			}
		}
	}
	if len(queryTerms) == 0 {
		return true
	}
	searchText := strings.ToLower(meta.Name + " " + meta.Description + " " + strings.Join(meta.Tags, " "))
	for _, term := range queryTerms {
		if !strings.Contains(searchText, term) {
			return false
		}
	}
	return true
}

func filterMatchingSkills(items []HubSkillMeta, queryTerms, tags []string) []HubSkillMeta {
	matched := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		if matchesSkill(item, queryTerms, tags) {
			matched = append(matched, item)
		}
	}
	return matched
}

func isCurrentRevision(index []HubSkillMeta, candidate HubSkillMeta) bool {
	for _, current := range groupSkillVersions(index) {
		if current.ID == candidate.ID {
			return true
		}
		for _, revision := range current.VersionHistory {
			if revision.ID == candidate.ID {
				return false
			}
		}
	}
	// An item missing from the catalog index is not a historical revision.
	return true
}

// groupSkillVersions keeps one catalog entry per skill identity. Older uploads
// may have no skill_id, and a migration may leave versions with different IDs.
// Therefore identities are joined through every reliable alias carried by a
// record, rather than picking a single field and accidentally splitting a
// version history.
func groupSkillVersions(items []HubSkillMeta) []HubSkillMeta {
	parents := make([]int, len(items))
	for i := range parents {
		parents[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parents[i] != i {
			parents[i] = find(parents[i])
		}
		return parents[i]
	}
	join := func(a, b int) {
		a, b = find(a), find(b)
		if a != b {
			parents[b] = a
		}
	}
	seen := make(map[string]int, len(items)*2)
	ambiguousWeakAliases := ambiguousWeakVersionAliases(items)
	for i, item := range items {
		for _, alias := range skillVersionAliases(item, ambiguousWeakAliases) {
			if previous, ok := seen[alias]; ok {
				join(i, previous)
				continue
			}
			seen[alias] = i
		}
	}

	groups := make(map[int][]HubSkillMeta, len(items))
	order := make([]int, 0, len(items))
	for i, item := range items {
		root := find(i)
		if _, ok := groups[root]; !ok {
			order = append(order, root)
		}
		groups[root] = append(groups[root], item)
	}

	grouped := make([]HubSkillMeta, 0, len(order))
	for _, root := range order {
		versions := groups[root]
		sort.SliceStable(versions, func(i, j int) bool { return skillVersionNewer(versions[i], versions[j]) })
		latest := versions[0]
		latest.VersionCount = len(versions)
		if len(versions) > 1 {
			latest.VersionHistory = make([]SkillVersionSummary, 0, len(versions))
			for _, version := range versions {
				latest.VersionHistory = append(latest.VersionHistory, SkillVersionSummary{
					ID: version.ID, Version: version.Version, SemVer: version.SemVer,
					CreatedAt: version.CreatedAt, UpdatedAt: version.UpdatedAt,
					Status: version.Status, Visible: version.Visible,
				})
			}
		}
		grouped = append(grouped, latest)
	}
	sort.SliceStable(grouped, func(i, j int) bool {
		if grouped[i].UpdatedAt != grouped[j].UpdatedAt {
			return grouped[i].UpdatedAt > grouped[j].UpdatedAt
		}
		if grouped[i].CreatedAt != grouped[j].CreatedAt {
			return grouped[i].CreatedAt > grouped[j].CreatedAt
		}
		return grouped[i].ID < grouped[j].ID
	})
	return grouped
}

// skillVersionAliases returns identity aliases that can safely link revisions.
// Fingerprint is uploader_email + ":" + skill_name in the upload processor,
// so it bridges legacy revisions and newer skill_id-based revisions without
// merging same-named skills from different publishers. Weak aliases are
// deliberately ignored when the catalog contains more than one explicit
// skill_id for the same alias.
func skillVersionAliases(item HubSkillMeta, ambiguousWeakAliases map[string]bool) []string {
	aliases := make([]string, 0, 3)
	if skillID := strings.TrimSpace(item.SkillID); skillID != "" {
		aliases = append(aliases, "skill:"+normalizeVersionIdentity(skillID))
	}
	if fingerprint := strings.TrimSpace(item.Fingerprint); fingerprint != "" {
		alias := "fingerprint:" + normalizeVersionIdentity(fingerprint)
		if !ambiguousWeakAliases[alias] {
			aliases = append(aliases, alias)
		}
	}
	if appID := strings.TrimSpace(item.MaclawAppID); appID != "" {
		publisher := firstNonEmpty(item.UploaderID, item.UploaderEmail, item.Author)
		if publisher != "" {
			aliases = append(aliases, "app-id:"+normalizeVersionIdentity(publisher)+":"+normalizeVersionIdentity(appID))
		} else {
			aliases = append(aliases, "app-id:"+normalizeVersionIdentity(appID))
		}
	}
	if item.IsMaclawApp || item.ProductKind == "maclaw_app_skill" {
		publisher := firstNonEmpty(item.UploaderID, item.UploaderEmail, item.Author)
		name := firstNonEmpty(item.MaclawAppName, item.Name)
		if publisher != "" && name != "" {
			alias := "legacy-app:" + normalizeVersionIdentity(publisher) + ":" + normalizeVersionIdentity(name)
			if !ambiguousWeakAliases[alias] {
				aliases = append(aliases, alias)
			}
		}
	}
	if len(aliases) == 0 {
		aliases = append(aliases, "id:"+item.ID)
	}
	return aliases
}

// ambiguousWeakVersionAliases identifies weak aliases attached to more than
// one explicit skill_id. They can bridge old records, but must never collapse
// independently declared modern skills that happen to share a display name.
func ambiguousWeakVersionAliases(items []HubSkillMeta) map[string]bool {
	idsByAlias := make(map[string]map[string]struct{})
	for _, item := range items {
		skillID := normalizeVersionIdentity(item.SkillID)
		if skillID == "" {
			continue
		}
		weakAliases := make([]string, 0, 2)
		if fingerprint := normalizeVersionIdentity(item.Fingerprint); fingerprint != "" {
			weakAliases = append(weakAliases, "fingerprint:"+fingerprint)
		}
		if item.IsMaclawApp || item.ProductKind == "maclaw_app_skill" {
			publisher := firstNonEmpty(item.UploaderID, item.UploaderEmail, item.Author)
			name := firstNonEmpty(item.MaclawAppName, item.Name)
			if publisher != "" && name != "" {
				weakAliases = append(weakAliases, "legacy-app:"+normalizeVersionIdentity(publisher)+":"+normalizeVersionIdentity(name))
			}
		}
		for _, alias := range weakAliases {
			if idsByAlias[alias] == nil {
				idsByAlias[alias] = make(map[string]struct{})
			}
			idsByAlias[alias][skillID] = struct{}{}
		}
	}
	ambiguous := make(map[string]bool)
	for alias, ids := range idsByAlias {
		if len(ids) > 1 {
			ambiguous[alias] = true
		}
	}
	return ambiguous
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func normalizeVersionIdentity(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

// onlyVisibleSkillGroups filters the public catalog after choosing the latest
// revision. It also removes hidden historical revisions from the response so
// their metadata is not exposed by the public search endpoint.
func onlyVisibleSkillGroups(groups []HubSkillMeta) []HubSkillMeta {
	visible := make([]HubSkillMeta, 0, len(groups))
	for _, group := range groups {
		if !group.Visible {
			continue
		}
		if len(group.VersionHistory) > 0 {
			history := make([]SkillVersionSummary, 0, len(group.VersionHistory))
			for _, version := range group.VersionHistory {
				if version.Visible {
					history = append(history, version)
				}
			}
			group.VersionHistory = history
			group.VersionCount = len(history)
		}
		visible = append(visible, group)
	}
	return visible
}

func skillVersionNewer(a, b HubSkillMeta) bool {
	versionA, versionB := firstVersion(a), firstVersion(b)
	// Only let a version decide precedence when both revisions carry a
	// semver-shaped value. Legacy labels such as "latest" or "draft" must use
	// timestamps instead of accidentally sorting lexicographically above a
	// numeric release.
	if isSemanticVersion(versionA) && isSemanticVersion(versionB) {
		if cmp := compareVersion(versionA, versionB); cmp != 0 {
			return cmp > 0
		}
	}
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return a.ID < b.ID
}

func isSemanticVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(version)), "v")
	version = strings.SplitN(version, "+", 2)[0]
	core, prerelease, hasPrerelease := splitPrerelease(version)
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	if !hasPrerelease {
		return true
	}
	return prerelease != ""
}

func firstVersion(item HubSkillMeta) string {
	if strings.TrimSpace(item.SemVer) != "" {
		return item.SemVer
	}
	return item.Version
}

// compareVersion compares common dotted versions (including v-prefixed
// versions). Numeric parts sort numerically, so v10 correctly follows v9.
func compareVersion(a, b string) int {
	a, b = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(a)), "v"), strings.TrimPrefix(strings.TrimSpace(strings.ToLower(b)), "v")
	// Build metadata does not affect semantic-version precedence.
	a = strings.SplitN(a, "+", 2)[0]
	b = strings.SplitN(b, "+", 2)[0]
	if a == b {
		return 0
	}
	coreA, preA, hasPreA := splitPrerelease(a)
	coreB, preB, hasPreB := splitPrerelease(b)
	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")
	maxParts := len(partsA)
	if len(partsB) > maxParts {
		maxParts = len(partsB)
	}
	for i := 0; i < maxParts; i++ {
		partA, partB := "0", "0"
		if i < len(partsA) {
			partA = partsA[i]
		}
		if i < len(partsB) {
			partB = partsB[i]
		}
		numA, errA := strconv.Atoi(partA)
		numB, errB := strconv.Atoi(partB)
		if errA == nil && errB == nil && numA != numB {
			if numA > numB {
				return 1
			}
			return -1
		}
		if errA != nil || errB != nil {
			if partA > partB {
				return 1
			}
			if partA < partB {
				return -1
			}
		}
	}
	// A stable release follows its prereleases: 1.0.0 > 1.0.0-rc.1.
	if hasPreA != hasPreB {
		if hasPreA {
			return -1
		}
		return 1
	}
	if hasPreA {
		return comparePrerelease(preA, preB)
	}
	return 0
}

func splitPrerelease(version string) (core, prerelease string, hasPrerelease bool) {
	parts := strings.SplitN(version, "-", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return version, "", false
}

func comparePrerelease(a, b string) int {
	partsA, partsB := strings.Split(a, "."), strings.Split(b, ".")
	maxParts := len(partsA)
	if len(partsB) > maxParts {
		maxParts = len(partsB)
	}
	for i := 0; i < maxParts; i++ {
		if i >= len(partsA) {
			return -1
		}
		if i >= len(partsB) {
			return 1
		}
		numA, errA := strconv.Atoi(partsA[i])
		numB, errB := strconv.Atoi(partsB[i])
		if errA == nil && errB == nil {
			if numA > numB {
				return 1
			}
			if numA < numB {
				return -1
			}
			continue
		}
		if errA == nil {
			return -1
		}
		if errB == nil {
			return 1
		}
		if partsA[i] > partsB[i] {
			return 1
		}
		if partsA[i] < partsB[i] {
			return -1
		}
	}
	return 0
}

func (s *SkillStore) IncrementDownloadCount(id string) error {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.DownloadCount++
	sk.Downloads = sk.DownloadCount
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o644); err != nil {
		s.mu.Unlock()
		return err
	}
	s.rebuildIndexFromSkills()
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

// IncrementSuiteDownloadCount records one download of a Suite distribution.
func (s *SkillStore) IncrementSuiteDownloadCount(id string) error {
	s.mu.Lock()
	su, ok := s.suites[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("suite not found: %s", id)
	}
	su.Downloads++
	data, err := json.MarshalIndent(su, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(s.dir, "suite-"+id+".json"), data, 0o644)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) UpdateStatus(id, expectedStatus, newStatus string) error {
	s.mu.Lock()
	sk, ok := s.skills[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("skill not found: %s", id)
	}
	if expectedStatus != "" && sk.Status != expectedStatus {
		s.mu.Unlock()
		return fmt.Errorf("concurrent conflict: expected status %s, got %s", expectedStatus, sk.Status)
	}
	sk.Status = newStatus
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.rebuildIndexFromSkills()
	if err := os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o644); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.emitSync(context.Background())
	return nil
}

func (s *SkillStore) GetByFingerprint(fingerprint string) *HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.index {
		if m.Fingerprint == fingerprint {
			return &m
		}
	}
	return nil
}

func (s *SkillStore) ListByUploader(email string) []HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	emailLower := strings.ToLower(email)
	var result []HubSkillMeta
	for _, m := range s.index {
		if strings.ToLower(m.UploaderEmail) == emailLower {
			result = append(result, m)
		}
	}
	return result
}

func (s *SkillStore) FindBySourceURL(sourceURL, name string) *HubSkillMeta {
	if sourceURL == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.index {
		if m.SourceURL == sourceURL && m.Name == name {
			return &m
		}
	}
	return nil
}
