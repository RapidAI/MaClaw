package main

// coding_knowledge_evolution.go implements evolution mechanisms for the
// coding knowledge base:
//
// 1. Graduate: Promote verified experiences to permanent steering rules
// 2. Export/Import: Share experience packs between machines/users
// 3. Eviction: Capacity management with intelligent pruning

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ---------------------------------------------------------------------------
// 1. Graduate to Steering
// ---------------------------------------------------------------------------

// CodingKnowledgeGraduateToSteering promotes a verified experience to a
// permanent steering rule file in ~/.maclaw/steering/.
// Returns the path of the created steering file.
func (a *App) CodingKnowledgeGraduateToSteering(id string) (string, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return "", fmt.Errorf("coding knowledge store not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exp, err := store.GetExperience(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get experience: %w", err)
	}

	if exp.Status != knowledge.CodingStatusVerified {
		return "", fmt.Errorf("only verified experiences can be graduated (current status: %s)", exp.Status)
	}

	// Build steering file content
	steeringContent := buildSteeringFileFromExperience(exp)
	filename := buildSteeringFilename(exp)

	// Write to user-level steering directory
	steeringDir := filepath.Join(a.getMaclawBaseDir(), "steering")
	if err := os.MkdirAll(steeringDir, 0o755); err != nil {
		return "", fmt.Errorf("create steering dir: %w", err)
	}

	filePath := filepath.Join(steeringDir, filename)
	if err := os.WriteFile(filePath, []byte(steeringContent), 0o644); err != nil {
		return "", fmt.Errorf("write steering file: %w", err)
	}

	// Mark the experience as graduated (deprecated with reason)
	_ = store.UpdateStatus(ctx, id, knowledge.CodingStatusDeprecated, []string{"graduated_to_steering"})

	log.Printf("[coding-knowledge] graduated experience %q to steering: %s", exp.Title, filePath)
	return filePath, nil
}

func buildSteeringFileFromExperience(exp knowledge.CodingExperience) string {
	var b strings.Builder

	// YAML front-matter
	b.WriteString("---\n")
	if exp.Scope == knowledge.CodingScopeLanguage && exp.Language != "" {
		// Language-specific: use fileMatch for relevant file extensions
		b.WriteString("inclusion: fileMatch\n")
		b.WriteString(fmt.Sprintf("fileMatchPattern: \"%s\"\n", languageToGlobPattern(exp.Language)))
	} else if exp.Scope == knowledge.CodingScopeProject {
		// Project-specific: always include (within that project)
		b.WriteString("inclusion: always\n")
	} else {
		// Universal: use contextMatch with trigger keywords
		b.WriteString("inclusion: contextMatch\n")
		if exp.TriggerCondition != "" {
			keywords := strings.Split(exp.TriggerCondition, " ")
			b.WriteString(fmt.Sprintf("contextKeywords: [%s]\n", strings.Join(keywords, ", ")))
		}
	}
	b.WriteString("priority: 80\n")
	b.WriteString("---\n\n")

	// Content
	b.WriteString(fmt.Sprintf("# %s\n\n", exp.Title))

	if exp.Category != "" {
		b.WriteString(fmt.Sprintf("> Category: %s | Scope: %s", exp.Category, exp.Scope))
		if exp.Language != "" {
			b.WriteString(fmt.Sprintf(" | Language: %s", exp.Language))
		}
		b.WriteString("\n\n")
	}

	b.WriteString(exp.Content)
	b.WriteString("\n")

	if exp.CodeSnippet != "" {
		b.WriteString("\n## Code Template\n\n```\n")
		b.WriteString(exp.CodeSnippet)
		b.WriteString("\n```\n")
	}

	if len(exp.FailedAttempts) > 0 {
		b.WriteString("\n## Avoid These Approaches\n\n")
		for _, fa := range exp.FailedAttempts {
			b.WriteString(fmt.Sprintf("- %s\n", fa))
		}
	}

	if len(exp.Contraindications) > 0 {
		b.WriteString("\n## Does NOT Apply When\n\n")
		for _, ci := range exp.Contraindications {
			b.WriteString(fmt.Sprintf("- %s\n", ci))
		}
	}

	return b.String()
}

func buildSteeringFilename(exp knowledge.CodingExperience) string {
	// Create a safe filename from the title
	name := strings.ToLower(exp.Title)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' || r == '_' {
			return '-'
		}
		return -1
	}, name)
	// Collapse multiple dashes
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if len(name) > 50 {
		name = name[:50]
	}
	if name == "" {
		name = "coding-experience"
	}
	return fmt.Sprintf("coding-exp-%s.md", name)
}

func languageToGlobPattern(lang string) string {
	switch lang {
	case "go":
		return "**/*.go"
	case "python":
		return "**/*.py"
	case "typescript":
		return "**/*.ts,**/*.tsx"
	case "javascript":
		return "**/*.js,**/*.jsx"
	case "cpp":
		return "**/*.cpp,**/*.cc,**/*.h,**/*.hpp"
	case "rust":
		return "**/*.rs"
	case "java":
		return "**/*.java"
	case "ruby":
		return "**/*.rb"
	case "csharp":
		return "**/*.cs"
	default:
		return "*"
	}
}

// ---------------------------------------------------------------------------
// 2. Export / Import
// ---------------------------------------------------------------------------

// CodingKnowledgeExportPack is the JSON structure for sharing experience packs.
type CodingKnowledgeExportPack struct {
	Version     string                       `json:"version"`
	ExportedAt  time.Time                    `json:"exported_at"`
	Description string                       `json:"description,omitempty"`
	Count       int                          `json:"count"`
	Experiences []knowledge.CodingExperience `json:"experiences"`
}

// CodingKnowledgeExport exports all active/verified experiences as a shareable JSON pack.
func (a *App) CodingKnowledgeExport() (CodingKnowledgeExportPack, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return CodingKnowledgeExportPack{}, fmt.Errorf("coding knowledge store not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	experiences, err := store.ListExperiences(ctx, knowledge.CodingListFilter{
		Limit: 10000,
	})
	if err != nil {
		return CodingKnowledgeExportPack{}, err
	}

	// Only export active + verified experiences, with hydrated content
	exported := make([]knowledge.CodingExperience, 0, len(experiences))
	for _, exp := range experiences {
		if exp.Status == knowledge.CodingStatusActive || exp.Status == knowledge.CodingStatusVerified {
			// Hydrate content from nodes if empty
			if exp.Content == "" {
				nodes, err := store.Inner().ListNodesBySource(ctx, exp.ID, 1)
				if err == nil && len(nodes) > 0 && nodes[0].Text != "" {
					exp.Content = nodes[0].Text
				}
			}
			exported = append(exported, exp)
		}
	}

	pack := CodingKnowledgeExportPack{
		Version:     "1.0",
		ExportedAt:  time.Now().UTC(),
		Count:       len(exported),
		Experiences: exported,
	}
	return pack, nil
}

// CodingKnowledgeExportToFile exports experiences to a JSON file.
func (a *App) CodingKnowledgeExportToFile(filePath string) error {
	pack, err := a.CodingKnowledgeExport()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0o644)
}

// CodingKnowledgeImportFromFile imports experiences from a JSON pack file.
// Experiences are imported as active status. Duplicates are skipped.
func (a *App) CodingKnowledgeImportFromFile(filePath string) (int, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return 0, fmt.Errorf("coding knowledge store not available")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}

	var pack CodingKnowledgeExportPack
	if err := json.Unmarshal(data, &pack); err != nil {
		return 0, fmt.Errorf("parse pack: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imported := 0
	for _, exp := range pack.Experiences {
		// Reset metadata for import
		exp.ID = "" // Let store assign new ID
		exp.Status = knowledge.CodingStatusActive
		exp.RecallCount = 0
		exp.SuccessCount = 0
		exp.FailureCount = 0
		exp.Confidence = knowledge.CodingConfidenceInitial
		exp.CreatedAt = time.Time{}
		exp.UpdatedAt = time.Time{}
		exp.LastRecalledAt = time.Time{}
		exp.Labels = append(exp.Labels, "imported")

		// Dedup check
		if isDuplicateExperience(ctx, store, exp) {
			continue
		}

		if _, err := store.SaveExperience(ctx, exp); err != nil {
			log.Printf("[coding-knowledge] import skip %q: %v", exp.Title, err)
			continue
		}
		imported++
	}

	log.Printf("[coding-knowledge] imported %d/%d experiences from %s", imported, len(pack.Experiences), filePath)
	return imported, nil
}

// ---------------------------------------------------------------------------
// 3. Capacity Eviction
// ---------------------------------------------------------------------------

const (
	defaultMaxPerProject = 200
	defaultMaxTotal      = 1000
)

// CodingKnowledgeProjectCapacity describes a project that exceeds the per-project limit.
type CodingKnowledgeProjectCapacity struct {
	ProjectPath string `json:"project_path"`
	Count       int    `json:"count"`
	Over        int    `json:"over"`
}

// CodingKnowledgeCapacityStatus is the capacity snapshot shown in the settings panel.
type CodingKnowledgeCapacityStatus struct {
	TotalCount    int                              `json:"total_count"`
	MaxTotal      int                              `json:"max_total"`
	MaxPerProject int                              `json:"max_per_project"`
	OverTotal     int                              `json:"over_total"`
	WouldEvict    int                              `json:"would_evict"`
	WithinLimit   bool                             `json:"within_limit"`
	ProjectsOver  []CodingKnowledgeProjectCapacity `json:"projects_over,omitempty"`
}

func resolveCodingKnowledgeLimits(cfg corelib.AppConfig) (maxTotal, maxPerProject int) {
	maxTotal = defaultMaxTotal
	maxPerProject = defaultMaxPerProject
	if cfg.CodingKnowledgeMaxTotal > 0 {
		maxTotal = cfg.CodingKnowledgeMaxTotal
	}
	if cfg.CodingKnowledgeMaxPerProject > 0 {
		maxPerProject = cfg.CodingKnowledgeMaxPerProject
	}
	return maxTotal, maxPerProject
}

// CodingKnowledgeCapacity returns total/per-project usage against configured limits.
func (a *App) CodingKnowledgeCapacity() (CodingKnowledgeCapacityStatus, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return CodingKnowledgeCapacityStatus{}, fmt.Errorf("coding knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, _ := a.LoadConfig()
	maxTotal, maxPerProject := resolveCodingKnowledgeLimits(cfg)

	stats, err := store.Stats(ctx)
	if err != nil {
		return CodingKnowledgeCapacityStatus{}, err
	}
	all, err := store.ListExperiences(ctx, knowledge.CodingListFilter{Limit: 10000})
	if err != nil {
		return CodingKnowledgeCapacityStatus{}, err
	}

	status := computeCodingKnowledgeCapacity(stats.TotalCount, maxTotal, maxPerProject, all)
	return status, nil
}

func computeCodingKnowledgeCapacity(totalCount, maxTotal, maxPerProject int, all []knowledge.CodingExperience) CodingKnowledgeCapacityStatus {
	if maxTotal <= 0 {
		maxTotal = defaultMaxTotal
	}
	if maxPerProject <= 0 {
		maxPerProject = defaultMaxPerProject
	}
	overTotal := 0
	if totalCount > maxTotal {
		overTotal = totalCount - maxTotal
	}

	byProject := map[string][]knowledge.CodingExperience{}
	for _, exp := range all {
		if exp.Scope != knowledge.CodingScopeProject {
			continue
		}
		path := strings.TrimSpace(exp.ProjectPath)
		if path == "" {
			path = "(unknown project)"
		}
		byProject[path] = append(byProject[path], exp)
	}

	var projectsOver []CodingKnowledgeProjectCapacity
	projectEvict := 0
	for path, exps := range byProject {
		if len(exps) <= maxPerProject {
			continue
		}
		over := len(exps) - maxPerProject
		projectEvict += over
		projectsOver = append(projectsOver, CodingKnowledgeProjectCapacity{
			ProjectPath: path,
			Count:       len(exps),
			Over:        over,
		})
	}
	sort.Slice(projectsOver, func(i, j int) bool {
		if projectsOver[i].Over == projectsOver[j].Over {
			return projectsOver[i].ProjectPath < projectsOver[j].ProjectPath
		}
		return projectsOver[i].Over > projectsOver[j].Over
	})

	// Global eviction may still be needed after project-level cleanup.
	// would_evict is a conservative upper bound: project overflow + remaining global overflow.
	remainingAfterProject := totalCount - projectEvict
	globalAfterProject := 0
	if remainingAfterProject > maxTotal {
		globalAfterProject = remainingAfterProject - maxTotal
	}
	wouldEvict := projectEvict + globalAfterProject
	if overTotal > wouldEvict {
		// If project buckets don't cover global overage, global pass alone would remove overTotal.
		wouldEvict = overTotal
	}

	return CodingKnowledgeCapacityStatus{
		TotalCount:    totalCount,
		MaxTotal:      maxTotal,
		MaxPerProject: maxPerProject,
		OverTotal:     overTotal,
		WouldEvict:    wouldEvict,
		WithinLimit:   wouldEvict == 0,
		ProjectsOver:  projectsOver,
	}
}

// CodingKnowledgeEvict runs the eviction policy to keep the store within capacity.
// Enforces per-project limits first, then the global total limit.
// Called periodically, after batch saves, or from the settings panel.
func (a *App) CodingKnowledgeEvict() (int, error) {
	store := a.ensureCodingKnowledgeStore()
	if store == nil {
		return 0, fmt.Errorf("coding knowledge store not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, _ := a.LoadConfig()
	maxTotal, maxPerProject := resolveCodingKnowledgeLimits(cfg)

	all, err := store.ListExperiences(ctx, knowledge.CodingListFilter{Limit: 10000})
	if err != nil {
		return 0, err
	}

	evicted := 0
	// 1) Per-project capacity
	byProject := map[string][]knowledge.CodingExperience{}
	for _, exp := range all {
		if exp.Scope != knowledge.CodingScopeProject {
			continue
		}
		path := strings.TrimSpace(exp.ProjectPath)
		if path == "" {
			path = "(unknown project)"
		}
		byProject[path] = append(byProject[path], exp)
	}
	for _, exps := range byProject {
		if len(exps) <= maxPerProject {
			continue
		}
		evicted += evictExperienceList(ctx, store, exps, len(exps)-maxPerProject)
	}

	// 2) Global total capacity (re-list after project eviction)
	stats, err := store.Stats(ctx)
	if err != nil {
		return evicted, err
	}
	if stats.TotalCount > maxTotal {
		evicted += evictExperiences(ctx, store, stats.TotalCount-maxTotal)
	}

	if evicted > 0 {
		log.Printf("[coding-knowledge] evicted %d experiences (limit total=%d project=%d)", evicted, maxTotal, maxPerProject)
	}
	return evicted, nil
}

func evictExperiences(ctx context.Context, store *knowledge.CodingKnowledgeStore, count int) int {
	all, err := store.ListExperiences(ctx, knowledge.CodingListFilter{Limit: 10000})
	if err != nil || len(all) == 0 {
		return 0
	}
	return evictExperienceList(ctx, store, all, count)
}

func evictExperienceList(ctx context.Context, store *knowledge.CodingKnowledgeStore, candidates []knowledge.CodingExperience, count int) int {
	if count <= 0 || len(candidates) == 0 {
		return 0
	}
	// Sort by eviction priority (first to evict → first in list):
	// 1. deprecated (already useless)
	// 2. candidate older than 30 days (never confirmed)
	// 3. lowest confidence + oldest last-recalled
	sorted := make([]knowledge.CodingExperience, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return evictionScore(sorted[i]) < evictionScore(sorted[j])
	})

	evicted := 0
	for _, exp := range sorted {
		if evicted >= count {
			break
		}
		if err := store.DeleteExperience(ctx, exp.ID); err != nil {
			continue
		}
		evicted++
	}
	return evicted
}

// evictionScore returns a score where LOWER = evict first.
func evictionScore(exp knowledge.CodingExperience) float64 {
	score := 0.0

	// Status-based base score (deprecated < candidate < active < verified)
	switch exp.Status {
	case knowledge.CodingStatusDeprecated:
		score = 0
	case knowledge.CodingStatusCandidate:
		score = 100
		// Stale candidates (>30 days) get penalized
		if time.Since(exp.CreatedAt) > 30*24*time.Hour {
			score = 10
		}
	case knowledge.CodingStatusActive:
		score = 200
	case knowledge.CodingStatusVerified:
		score = 300
	}

	// Confidence adds to score (higher confidence = less likely to be evicted)
	score += exp.Confidence * 50

	// Recent recall adds to score (recently useful = keep)
	if !exp.LastRecalledAt.IsZero() {
		daysSinceRecall := time.Since(exp.LastRecalledAt).Hours() / 24
		if daysSinceRecall < 7 {
			score += 100
		} else if daysSinceRecall < 30 {
			score += 50
		}
	}

	return score
}

// ---------------------------------------------------------------------------
// AppConfig additions for capacity management
// ---------------------------------------------------------------------------

// CodingKnowledgeMaxTotal and CodingKnowledgeMaxPerProject are added
// in corelib/app_config.go (see Phase 3 commit).
// These are read by CodingKnowledgeEvict to determine capacity limits.
