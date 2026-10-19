package desktopbot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// Progress file for one background task. The three sections are replaced.
// A write does not append a log, and the body is not a second copy of the
// conversation. The summary that points at the file is the path alone.

const (
	ProgressKindTask   = "tasks"
	ProgressKindHelper = "helpers"
	ProgressFileName   = "PROGRESS.md"
	ProgressOpenNone   = "无"

	progressWhere = "## where we are"
	progressNext  = "## next step"
	progressOpen  = "## open problems"
)

// Sections is the whole progress file. OpenProblems is 无 when nothing is unsettled.
type Sections struct {
	WhereWeAre   string
	NextStep     string
	OpenProblems string
}

// ProgressRead is what a person gets when they ask how the work is going.
// SendToUser stays empty: this read is not a new progress bubble.
type ProgressRead struct {
	Text       string
	SendToUser string
}

// ProgressCopy is the last successful write, kept on the run so a sleeping
// machine does not have to wake to answer.
type ProgressCopy struct {
	Sections Sections
	Path     string
}

var (
	untrustedFence = regexp.MustCompile(`(?s)\[untrusted [a-z]+\](.*?)\[/untrusted\]`)
	progressCopies sync.Map
)

// TaskProgressPath is the container path of the main agent's own background task.
func TaskProgressPath(taskID string) string {
	return progressCanonical(ProgressKindTask, taskID)
}

// HelperProgressPath is the container path of one helper. It is not the task file.
func HelperProgressPath(helperID string) string {
	return progressCanonical(ProgressKindHelper, helperID)
}

func progressCanonical(kind, id string) string {
	if kind != ProgressKindTask && kind != ProgressKindHelper {
		kind = ProgressKindTask
	}
	return "/home/desktop/" + kind + "/" + SanitizeTaskID(id) + "/" + ProgressFileName
}

// CleanSections drops secrets and the original text inside an untrusted fence.
func CleanSections(s Sections) Sections {
	s.WhereWeAre = cleanProgressText(s.WhereWeAre)
	s.NextStep = cleanProgressText(s.NextStep)
	s.OpenProblems = cleanProgressText(s.OpenProblems)
	if strings.TrimSpace(s.OpenProblems) == "" {
		s.OpenProblems = ProgressOpenNone
	}
	return s
}

func cleanProgressText(s string) string {
	s = stripUntrustedFences(s)
	s = ScrubSecrets(s)
	return strings.TrimSpace(s)
}

func stripUntrustedFences(s string) string {
	s = untrustedFence.ReplaceAllString(s, "")
	if i := strings.Index(s, "[untrusted "); i >= 0 {
		s = s[:i]
	}
	return s
}

// RenderProgress is the file body. The previous body is not included.
func RenderProgress(s Sections) string {
	s = CleanSections(s)
	return progressWhere + "\n" + s.WhereWeAre + "\n\n" +
		progressNext + "\n" + s.NextStep + "\n\n" +
		progressOpen + "\n" + s.OpenProblems + "\n"
}

// ParseProgress reads a file that starts with the three headings.
// A tool result that only mentions a heading later is not a progress file.
func ParseProgress(text string) (Sections, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, progressWhere) {
		return Sections{}, false
	}
	where := progressBetween(text, progressWhere, progressNext)
	next := progressBetween(text, progressNext, progressOpen)
	open := progressBetween(text, progressOpen, "")
	if strings.TrimSpace(where) == "" && strings.TrimSpace(next) == "" && strings.TrimSpace(open) == "" {
		return Sections{}, false
	}
	return Sections{WhereWeAre: where, NextStep: next, OpenProblems: open}, true
}

func progressBetween(text, heading, nextHeading string) string {
	i := strings.Index(text, heading)
	if i < 0 {
		return ""
	}
	rest := strings.TrimPrefix(text[i+len(heading):], "\n")
	if nextHeading != "" {
		if j := strings.Index(rest, nextHeading); j >= 0 {
			rest = rest[:j]
		}
	}
	return strings.TrimSpace(rest)
}

// ApplyToolReturn replaces the three sections from a tool result.
// A result that is itself a progress file replaces all three.
// Any other result replaces where we are and keeps the previous next step.
func ApplyToolReturn(prev Sections, toolName, body string) Sections {
	if parsed, ok := ParseProgress(body); ok {
		return CleanSections(parsed)
	}
	prev = CleanSections(prev)
	prev.WhereWeAre = toolProgressLine(toolName, body)
	if strings.TrimSpace(prev.OpenProblems) == "" {
		prev.OpenProblems = ProgressOpenNone
	}
	return prev
}

func toolProgressLine(name, body string) string {
	body = cleanProgressText(body)
	if i := strings.IndexAny(body, "\r\n"); i >= 0 {
		body = body[:i]
	}
	body = strings.Join(strings.Fields(body), " ")
	name = strings.TrimSpace(name)
	line := body
	if name != "" && body != "" {
		line = name + ": " + body
	} else if line == "" {
		line = name
	}
	if utf8.RuneCountInString(line) > 200 {
		line = string([]rune(line)[:200])
	}
	return strings.TrimSpace(line)
}

// WriteProgressFile replaces the file under root/kind/id. The bytes are the
// three sections only. root is the host directory; the canonical path is the
// container path and is not a second file in the GUI data directory.
func WriteProgressFile(root, kind, taskID string, s Sections) (string, Sections, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", Sections{}, fmt.Errorf("progress directory is empty")
	}
	if kind != ProgressKindTask && kind != ProgressKindHelper {
		return "", Sections{}, fmt.Errorf("progress kind is invalid")
	}
	cleaned := CleanSections(s)
	dir := filepath.Join(root, kind, SanitizeTaskID(taskID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Sections{}, err
	}
	host := filepath.Join(dir, ProgressFileName)
	if err := os.WriteFile(host, []byte(RenderProgress(cleaned)), 0o644); err != nil {
		return "", Sections{}, err
	}
	return host, cleaned, nil
}

// RememberProgress stores the three sections on the run after a successful write.
func RememberProgress(taskID string, s Sections, path string) {
	id := SanitizeTaskID(taskID)
	progressCopies.Store(id, ProgressCopy{Sections: CleanSections(s), Path: strings.TrimSpace(path)})
}

// RecallProgress returns the last successful copy. A missing copy means this
// process has not written the file yet.
func RecallProgress(taskID string) (ProgressCopy, bool) {
	value, ok := progressCopies.Load(SanitizeTaskID(taskID))
	if !ok {
		return ProgressCopy{}, false
	}
	copy, ok := value.(ProgressCopy)
	return copy, ok
}

// ResetProgressCopies drops the in-process run copies. Tests use it.
func ResetProgressCopies() {
	progressCopies.Range(func(key, _ any) bool {
		progressCopies.Delete(key)
		return true
	})
}

// ResumeFromNextStep is the continuation after sleep, summary, or interrupted.
// The file wins when it is in hand. Otherwise the run copy is the next step.
func ResumeFromNextStep(fileText string, copy Sections) string {
	if parsed, ok := ParseProgress(fileText); ok && strings.TrimSpace(parsed.NextStep) != "" {
		return strings.TrimSpace(parsed.NextStep)
	}
	return strings.TrimSpace(CleanSections(copy).NextStep)
}

// ProgressSummary is the only text a summary stores for this file: the path.
func ProgressSummary(path string) string {
	return strings.TrimSpace(path)
}

// ReadProgress answers a progress question from the file when the machine is
// awake, and from the run copy when it is asleep. It does not set send_to_user.
func ReadProgress(fileText string, copy Sections, awake bool) ProgressRead {
	chosen := copy
	if awake {
		if parsed, ok := ParseProgress(fileText); ok {
			chosen = parsed
		}
	}
	chosen = CleanSections(chosen)
	return ProgressRead{Text: chosen.WhereWeAre + "\n" + chosen.NextStep + "\n" + chosen.OpenProblems}
}

var (
	helperConclusion = regexp.MustCompile(`^\s*([123])[\.、]\s*(.*)$`)
	helperFileLine   = regexp.MustCompile(`^\s*文件\s*[:：]\s*(.*)$`)
	helperHomePath   = regexp.MustCompile(`/home/desktop/\S+`)
)

// CollapseHelperReport keeps three conclusion lines and the named paths.
// Tool traces stay out of the text. An empty conclusion and no path is a failure.
func CollapseHelperReport(body string, extraPaths []string) (string, error) {
	var lines [3]string
	var paths []string
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(cleanProgressText(path)), "，。,;；")
		if path == "" || path == ProgressOpenNone {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if m := helperConclusion.FindStringSubmatch(line); m != nil {
			lines[m[1][0]-'1'] = cleanProgressText(m[2])
			continue
		}
		if m := helperFileLine.FindStringSubmatch(line); m != nil {
			for _, part := range strings.Fields(m[1]) {
				add(part)
			}
			continue
		}
		for _, path := range helperHomePath.FindAllString(line, -1) {
			add(path)
		}
	}
	for _, path := range extraPaths {
		add(path)
	}
	if strings.TrimSpace(lines[0]+lines[1]+lines[2]) == "" && len(paths) == 0 {
		return "", fmt.Errorf("helper report has no conclusion and no path")
	}
	file := ProgressOpenNone
	if len(paths) > 0 {
		file = strings.Join(paths, " ")
	}
	shown := fmt.Sprintf("1. %s\n2. %s\n3. %s\n文件：%s", lines[0], lines[1], lines[2], file)
	return cleanProgressText(shown), nil
}

// HelperStartEntries is the helper window at the start of a workflow.
// A new id is empty. The same id keeps its own entries. The main session is not copied.
func HelperStartEntries(_ []string, own []string, sameID bool) []string {
	if !sameID {
		return nil
	}
	return append([]string(nil), own...)
}
