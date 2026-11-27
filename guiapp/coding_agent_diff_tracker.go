package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"
)

const codingAgentDiffSummaryDetailMaxRunes = 240

type CodingDiffSnapshot struct {
	FilesModified []string
	FilesCreated  []string
	DiffSummary   string
}

func newCodingDiffSnapshot(filesModified, filesCreated []string, diffSummary string) CodingDiffSnapshot {
	return CodingDiffSnapshot{
		FilesModified: uniqueSortedSubAgentStrings(filesModified),
		FilesCreated:  uniqueSortedSubAgentStrings(filesCreated),
		DiffSummary:   strings.TrimSpace(diffSummary),
	}
}

func (s CodingDiffSnapshot) Files() []string {
	files := make([]string, 0, len(s.FilesModified)+len(s.FilesCreated))
	files = append(files, s.FilesModified...)
	files = append(files, s.FilesCreated...)
	return uniqueSortedSubAgentStrings(files)
}

func (s CodingDiffSnapshot) Count() int {
	return len(s.Files())
}

func (s CodingDiffSnapshot) Detail() string {
	count := s.Count()
	if count == 0 {
		return "no file changes"
	}
	parts := []string{fmt.Sprintf("%d files", count)}
	if len(s.FilesCreated) > 0 {
		parts = append(parts, fmt.Sprintf("%d created", len(s.FilesCreated)))
	}
	if strings.TrimSpace(s.DiffSummary) != "" {
		parts = append(parts, truncateRunesForSubAgent(firstLine(s.DiffSummary), codingAgentDiffSummaryDetailMaxRunes))
	}
	return strings.Join(parts, " | ")
}

func newCodingAgentDiffSummaryEvent(task *TaskItem, title string, snapshot CodingDiffSnapshot) CodingAgentEvent {
	return newCodingAgentDiffSummaryEventWithStat(task, title, snapshot, nil)
}

func newCodingAgentDiffSummaryEventWithStat(task *TaskItem, title string, snapshot CodingDiffSnapshot, stat *SubAgentDiffStat) CodingAgentEvent {
	event := newCodingAgentTaskEvent(codingAgentEventPhaseResult, task, title, "")
	event.Event = codingAgentEventKindDiffSummary.String()
	event.Detail = snapshot.Detail()
	event.Count = snapshot.Count()
	event.Files = snapshot.Files()
	attachCodingDiffFileChanges(&event, snapshot, stat)
	return event
}

func attachCodingDiffFileChanges(event *CodingAgentEvent, snapshot CodingDiffSnapshot, stat *SubAgentDiffStat) {
	if event == nil {
		return
	}
	changes := codingAgentFileChangesFromSnapshot(snapshot, stat)
	if len(changes) == 0 {
		return
	}
	event.FileChanges = changes
	files := make([]string, len(changes))
	for i, change := range changes {
		files[i] = change.Path
	}
	event.Files = files
	event.Count = len(changes)
	added, removed := 0, 0
	if stat != nil && (stat.Insertions > 0 || stat.Deletions > 0) {
		added, removed = stat.Insertions, stat.Deletions
	} else {
		for _, change := range changes {
			added += change.Added
			removed += change.Removed
		}
	}
	event.Added = added
	event.Removed = removed
	if added > 0 || removed > 0 {
		event.Detail = fmt.Sprintf("%d files changed (+%d -%d)", event.Count, added, removed)
	}
}

func codingAgentFileChangesFromSnapshot(snapshot CodingDiffSnapshot, stat *SubAgentDiffStat) []CodingAgentFileChange {
	files := snapshot.Files()
	if stat != nil {
		for _, file := range stat.FileStats {
			path := strings.TrimSpace(file.Path)
			if path == "" {
				continue
			}
			files = append(files, path)
		}
	}
	files = uniqueSubAgentDiffPaths(files)
	if len(files) == 0 {
		return nil
	}
	if len(files) > codingSubAgentResultFilesMax {
		files = files[:codingSubAgentResultFilesMax]
	}
	changes := make([]CodingAgentFileChange, 0, len(files))
	for _, path := range files {
		added, removed := lookupSubAgentFileDiffStat(stat, path)
		hasCounts := false
		if i := lookupSubAgentFileDiffStatIndex(stat, path); i >= 0 {
			hasCounts = stat.FileStats[i].HasCounts
		}
		changes = append(changes, CodingAgentFileChange{
			Path:      compactSubAgentPathText(path),
			Added:     added,
			Removed:   removed,
			HasCounts: hasCounts,
		})
	}
	return changes
}

func codingToolLineDeltaReplacesFile(name string) bool {
	bare := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "ssh_")
	return bare == "write_file"
}

func mergeSubAgentDiffStat(stat *SubAgentDiffStat, fallbacks []SubAgentFileDiffStat) *SubAgentDiffStat {
	if len(fallbacks) == 0 {
		return stat
	}
	merged := &SubAgentDiffStat{}
	if stat != nil && len(stat.FileStats) > 0 {
		merged.FileStats = append([]SubAgentFileDiffStat{}, stat.FileStats...)
	}
	fallbackEvidenceBefore := func(path string, before int, exact bool) bool {
		for i := 0; i < before; i++ {
			candidate := fallbacks[i]
			if candidate.HasCounts == exact && subAgentDiffEvidencePathsMatch(candidate.Path, path) {
				return true
			}
		}
		return false
	}
	for fallbackIndex, fallback := range fallbacks {
		path := strings.TrimSpace(fallback.Path)
		if path == "" {
			continue
		}
		if i := lookupSubAgentFileDiffStatIndex(merged, path); i >= 0 {
			merged.FileStats[i].Path = preferredSubAgentDiffPath(merged.FileStats[i].Path, path)
			if fallback.HasCounts {
				if fallbackEvidenceBefore(path, fallbackIndex, false) {
					// Once the turn was downgraded to unknown, a later per-operation
					// diff must not resurrect precision: that diff starts from
					// intermediate bytes rather than the start of the turn.
					continue
				}
				// Tool baselines measure this turn's net edit. They are more
				// authoritative than git diff output, which is relative to HEAD and
				// may include work that was already dirty before the turn.
				merged.FileStats[i].Insertions = fallback.Insertions
				merged.FileStats[i].Deletions = fallback.Deletions
				merged.FileStats[i].Deleted = fallback.Deleted
				merged.FileStats[i].HasCounts = true
				continue
			}
			if !fallback.HasCounts {
				if fallbackEvidenceBefore(path, fallbackIndex, true) {
					continue
				}
				// A touched file whose turn-net counts are unavailable must not
				// borrow HEAD-relative git precision: that precision may include
				// changes which already existed before this turn.
				merged.FileStats[i].Insertions = 0
				merged.FileStats[i].Deletions = 0
				merged.FileStats[i].Deleted = fallback.Deleted
				merged.FileStats[i].HasCounts = false
				continue
			}
		}
		merged.FileStats = append(merged.FileStats, SubAgentFileDiffStat{
			Path:       path,
			Insertions: fallback.Insertions,
			Deletions:  fallback.Deletions,
			Deleted:    fallback.Deleted,
			HasCounts:  fallback.HasCounts,
		})
	}
	if len(merged.FileStats) == 0 {
		return stat
	}
	for _, file := range merged.FileStats {
		if !file.HasCounts {
			continue
		}
		merged.Insertions += file.Insertions
		merged.Deletions += file.Deletions
	}
	merged.FilesChanged = len(merged.FileStats)
	return merged
}

// filterSubAgentDiffStatToFiles scopes repository-wide diff evidence to the
// files this turn actually touched. `git diff` is relative to HEAD, so an
// already-dirty user workspace must not leak unrelated pre-existing changes
// into the task's final table.
func filterSubAgentDiffStatToFiles(stat *SubAgentDiffStat, fileGroups ...[]string) *SubAgentDiffStat {
	if stat == nil || len(stat.FileStats) == 0 {
		return stat
	}
	out := &SubAgentDiffStat{}
	for _, file := range stat.FileStats {
		kept := false
		for _, group := range fileGroups {
			for _, path := range group {
				if subAgentDiffEvidencePathsMatch(file.Path, path) {
					kept = true
					break
				}
			}
			if kept {
				break
			}
		}
		if !kept {
			continue
		}
		out.FileStats = append(out.FileStats, file)
		if file.HasCounts {
			out.Insertions += file.Insertions
			out.Deletions += file.Deletions
		}
	}
	if len(out.FileStats) == 0 {
		return nil
	}
	out.FilesChanged = len(out.FileStats)
	return out
}

// subAgentDiffEvidencePathsMatch is stricter than the UI-oriented suffix
// matcher: suffix equivalence is accepted only when one side is absolute. Two
// different relative nested files may share a tail and must not borrow each
// other's measured line counts.
func subAgentDiffEvidencePathsMatch(a, b string) bool {
	a = normalizeSubAgentDiffPath(a)
	b = normalizeSubAgentDiffPath(b)
	if a == "" || b == "" {
		return false
	}
	windowsCaseInsensitive := subAgentDiffPathLooksWindowsAbsolute(a) || subAgentDiffPathLooksWindowsAbsolute(b)
	if windowsCaseInsensitive {
		if strings.EqualFold(a, b) {
			return true
		}
	} else if a == b {
		return true
	}
	if !subAgentDiffPathLooksAbsolute(a) && !subAgentDiffPathLooksAbsolute(b) {
		return false
	}
	if windowsCaseInsensitive {
		al, bl := strings.ToLower(a), strings.ToLower(b)
		return strings.HasSuffix(al, "/"+bl) || strings.HasSuffix(bl, "/"+al)
	}
	return strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

func subAgentDiffPathLooksWindowsAbsolute(path string) bool {
	return len(path) > 2 && path[1] == ':' && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z'))
}

func subAgentDiffPathLooksAbsolute(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return true
	}
	return len(path) > 2 && path[1] == ':' && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z'))
}

func lookupSubAgentFileDiffStat(stat *SubAgentDiffStat, path string) (added, removed int) {
	i := lookupSubAgentFileDiffStatIndex(stat, path)
	if i < 0 {
		return 0, 0
	}
	return stat.FileStats[i].Insertions, stat.FileStats[i].Deletions
}

func lookupSubAgentFileDiffStatIndex(stat *SubAgentDiffStat, path string) int {
	if stat == nil {
		return -1
	}
	want := strings.TrimSpace(path)
	if want == "" {
		return -1
	}
	for i, file := range stat.FileStats {
		if subAgentDiffEvidencePathsMatch(file.Path, want) {
			return i
		}
	}
	return -1
}

func codingToolMutatesTrackedFiles(name string) bool {
	bare := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "ssh_")
	switch bare {
	case "write_file", "edit_file", "edit_lines", "apply_patch", "str_replace":
		return true
	default:
		return false
	}
}

func attachCodingToolFileChanges(event *CodingAgentEvent, name, argsJSON string, stat *SubAgentDiffStat) {
	if event == nil || !codingToolMutatesTrackedFiles(name) {
		return
	}
	files := event.Files
	if len(files) == 0 {
		files = codingToolEventFiles(name, argsJSON, "")
		event.Files = files
	}
	if len(files) == 0 {
		return
	}
	path := files[0]
	added, removed := lookupSubAgentFileDiffStat(stat, path)
	if added == 0 && removed == 0 {
		added, removed = estimateCodingToolLineDelta(argsJSON)
	}
	event.Added = added
	event.Removed = removed
	event.FileChanges = []CodingAgentFileChange{{
		Path:    compactSubAgentPathText(path),
		Added:   added,
		Removed: removed,
	}}
}

func estimateCodingToolLineDelta(argsJSON string) (added, removed int) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args == nil {
		return 0, 0
	}
	content := firstNonEmptySubAgentString(
		codingToolEventArgString(args, "content"),
		codingToolEventArgString(args, "new_content"),
	)
	oldText := firstNonEmptySubAgentString(
		codingToolEventArgString(args, "old_string"),
		codingToolEventArgString(args, "old_str"),
		codingToolEventArgString(args, "old_content"),
	)
	newText := firstNonEmptySubAgentString(
		codingToolEventArgString(args, "new_string"),
		codingToolEventArgString(args, "new_str"),
		codingToolEventArgString(args, "new_content"),
		codingToolEventArgString(args, "replacement"),
	)
	if oldText != "" || newText != "" {
		oldN := codingToolContentLineCount(oldText)
		newN := codingToolContentLineCount(newText)
		if newN > oldN {
			added = newN - oldN
		}
		if oldN > newN {
			removed = oldN - newN
		}
		if added == 0 && removed == 0 && oldText != newText {
			return 1, 1
		}
		return added, removed
	}
	if content != "" {
		return codingToolContentLineCount(content), 0
	}
	return 0, 0
}

func codingToolContentLineCount(text string) int {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}
