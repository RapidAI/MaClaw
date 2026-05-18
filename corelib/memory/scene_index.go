package memory

import (
	"sort"
	"strings"
	"time"
)

// SceneRecord is a deterministic, human-readable navigation layer over project
// memories. It points to source files instead of embedding full evidence.
type SceneRecord struct {
	ProjectPath     string          `json:"project_path"`
	Name            string          `json:"name,omitempty"`
	WorkflowTypes   []string        `json:"workflow_types,omitempty"`
	Tags            []string        `json:"tags,omitempty"`
	Categories      []Category      `json:"categories,omitempty"`
	SourceURLs      []string        `json:"source_urls,omitempty"`
	RecentArtifacts []SceneArtifact `json:"recent_artifacts,omitempty"`
	EntryCount      int             `json:"entry_count"`
	LastActivity    time.Time       `json:"last_activity"`
	Preview         string          `json:"preview,omitempty"`
}

type SceneArtifact struct {
	Title      string    `json:"title,omitempty"`
	Category   Category  `json:"category,omitempty"`
	SourceType string    `json:"source_type,omitempty"`
	SourceURL  string    `json:"source_url,omitempty"`
	Preview    string    `json:"preview,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

// BuildSceneIndex groups project-related memories into a compact scene/task
// index. It is deliberately rule-based so it can run during recall without an
// LLM and without mutating memory state.
func BuildSceneIndex(entries []Entry, limit int) []SceneRecord {
	if limit <= 0 {
		limit = 20
	}
	type sceneAccum struct {
		rec       SceneRecord
		seenIDs   map[string]bool
		sources   map[string]bool
		workflows map[string]bool
	}
	byProject := make(map[string]*sceneAccum)

	for i := range entries {
		e := entries[i]
		if !projectCategories[e.Category] && !projectCategories[MapToCanonical(e.Category)] {
			continue
		}
		if e.Status == StatusDormant || e.Status == StatusSuperseded {
			continue
		}
		projectPath := inferProjectPath(&e)
		if projectPath == "" {
			continue
		}
		acc := byProject[projectPath]
		if acc == nil {
			acc = &sceneAccum{
				rec:       SceneRecord{ProjectPath: projectPath},
				seenIDs:   make(map[string]bool),
				sources:   make(map[string]bool),
				workflows: make(map[string]bool),
			}
			byProject[projectPath] = acc
		}
		if e.ID == "" || !acc.seenIDs[e.ID] {
			acc.seenIDs[e.ID] = true
			acc.rec.EntryCount++
		}
		if e.UpdatedAt.After(acc.rec.LastActivity) {
			acc.rec.LastActivity = e.UpdatedAt
			acc.rec.Preview = truncateRunes(firstMeaningfulLine(e.Content), 150)
		}
		acc.rec.Tags = mergeTagsDedup(acc.rec.Tags, e.Tags)
		acc.rec.Categories = addCategoryDedup(acc.rec.Categories, e.Category)
		if acc.rec.Name == "" {
			acc.rec.Name = sceneEntryTitle(e)
		}
		for _, tag := range e.Tags {
			if strings.HasPrefix(tag, "workflow:") {
				wf := strings.TrimSpace(strings.TrimPrefix(tag, "workflow:"))
				if wf != "" && !acc.workflows[wf] {
					acc.workflows[wf] = true
					acc.rec.WorkflowTypes = append(acc.rec.WorkflowTypes, wf)
				}
			}
		}
		if e.SourceURL != "" && !acc.sources[e.SourceURL] {
			acc.sources[e.SourceURL] = true
			acc.rec.SourceURLs = append(acc.rec.SourceURLs, e.SourceURL)
		}
		if e.Category == CategoryTaskArtifact || MapToCanonical(e.Category) == CategoryTaskArtifact {
			acc.rec.RecentArtifacts = append(acc.rec.RecentArtifacts, SceneArtifact{
				Title:      sceneEntryTitle(e),
				Category:   e.Category,
				SourceType: e.SourceType,
				SourceURL:  e.SourceURL,
				Preview:    truncateRunes(firstMeaningfulLine(e.Content), 120),
				UpdatedAt:  e.UpdatedAt,
			})
		}
	}

	result := make([]SceneRecord, 0, len(byProject))
	for _, acc := range byProject {
		sort.Strings(acc.rec.WorkflowTypes)
		if len(acc.rec.SourceURLs) > 8 {
			acc.rec.SourceURLs = acc.rec.SourceURLs[:8]
		}
		sort.SliceStable(acc.rec.RecentArtifacts, func(i, j int) bool {
			return acc.rec.RecentArtifacts[i].UpdatedAt.After(acc.rec.RecentArtifacts[j].UpdatedAt)
		})
		if len(acc.rec.RecentArtifacts) > 5 {
			acc.rec.RecentArtifacts = acc.rec.RecentArtifacts[:5]
		}
		result = append(result, acc.rec)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].LastActivity.After(result[j].LastActivity)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (s *Store) SceneIndex(limit int) []SceneRecord {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	entries := append([]Entry(nil), s.entries...)
	s.mu.RUnlock()
	return BuildSceneIndex(entries, limit)
}

func sceneEntryTitle(e Entry) string {
	if strings.TrimSpace(e.Title) != "" {
		return strings.TrimSpace(e.Title)
	}
	if title := extractTitle(e.Content); title != "" {
		return title
	}
	return truncateRunes(firstMeaningfulLine(e.Content), 80)
}

func FormatSceneIndexForPrompt(scenes []SceneRecord, maxScenes, maxArtifacts int) string {
	if maxScenes <= 0 {
		maxScenes = 3
	}
	if maxArtifacts <= 0 {
		maxArtifacts = 3
	}
	if len(scenes) > maxScenes {
		scenes = scenes[:maxScenes]
	}
	var b strings.Builder
	for i, scene := range scenes {
		name := strings.TrimSpace(scene.Name)
		if name == "" {
			name = scene.ProjectPath
		}
		b.WriteString("- ")
		b.WriteString(name)
		if scene.ProjectPath != "" && scene.ProjectPath != name {
			b.WriteString(" | project: ")
			b.WriteString(scene.ProjectPath)
		}
		if len(scene.WorkflowTypes) > 0 {
			b.WriteString(" | workflows: ")
			b.WriteString(strings.Join(scene.WorkflowTypes, ", "))
		}
		b.WriteByte('\n')
		artifacts := scene.RecentArtifacts
		if len(artifacts) > maxArtifacts {
			artifacts = artifacts[:maxArtifacts]
		}
		for _, artifact := range artifacts {
			label := strings.TrimSpace(artifact.Title)
			if label == "" {
				label = strings.TrimSpace(artifact.Preview)
			}
			if label == "" && artifact.SourceURL == "" {
				continue
			}
			b.WriteString("  artifact: ")
			b.WriteString(label)
			if artifact.SourceURL != "" {
				b.WriteString(" (source: ")
				b.WriteString(artifact.SourceURL)
				if LooksLikeFilePath(artifact.SourceURL) {
					b.WriteString("; full: read_file")
				}
				b.WriteString(")")
			}
			b.WriteByte('\n')
		}
		if i == len(scenes)-1 && b.Len() > 0 {
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
