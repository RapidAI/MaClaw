package guiapp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestFileCompanionSessionPathDecodesJSONEscapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a&b.pdf")
	if err := os.WriteFile(path, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := companionSessionJSON(t, path)
	if !strings.Contains(string(body), `\u0026`) {
		t.Fatalf("session JSON did not escape &: %s", body)
	}
	file := filepath.Join(t.TempDir(), "fc_amp.json")
	if err := os.WriteFile(file, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got := pathFromCompanionSessionFile(file)
	if got != path {
		t.Fatalf("path = %q", got)
	}
}

func TestFileCompanionSessionPathStopsAtTheFirstLine(t *testing.T) {
	raw := []byte(`{"content":"[用户选择的本地文件路径]\nc:\\notes\\a\u0026b.pdf\n\npath=\"c:\\\\notes\\\\other.pdf\""}`)
	got, done := pathFromCompanionSessionBytes(raw)
	if !done || got != `c:\notes\a&b.pdf` {
		t.Fatalf("path = %q done=%v", got, done)
	}
}

func TestFileCompanionSessionPathAfterALongPreamble(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "late.pdf")
	if err := os.WriteFile(path, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := append(bytes.Repeat([]byte{' '}, 70*1024), companionSessionJSON(t, path)...)
	file := filepath.Join(t.TempDir(), "fc_late.json")
	if err := os.WriteFile(file, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pathFromCompanionSessionFile(file); got != path {
		t.Fatalf("path = %q", got)
	}
}

func TestFileCompanionSessionPathIgnoresACutOffLine(t *testing.T) {
	full := []byte(`{"content":"[用户选择的本地文件路径]\nc:\\notes\\a\u0026b.pdf\n"}`)
	cut := full[:bytes.LastIndex(full, []byte(`pdf`))]
	got, done := pathFromCompanionSessionBytes(cut)
	if done || got != "" {
		t.Fatalf("cut path = %q done=%v", got, done)
	}
	got, done = pathFromCompanionSessionBytes(full)
	if !done || got != `c:\notes\a&b.pdf` {
		t.Fatalf("full path = %q done=%v", got, done)
	}
}

// A reader that returns a few bytes at a time, and does not report EOF until
// the next read, is how a short read looks. The path line is split across
// those reads and must still be recovered.
func TestFileCompanionSessionPathSurvivesShortReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a&b.pdf")
	got := pathFromCompanionSessionReader(&shortReader{rest: companionSessionJSON(t, path), n: 7})
	if got != path {
		t.Fatalf("path = %q", got)
	}
	late := append(bytes.Repeat([]byte{' '}, 70*1024), companionSessionJSON(t, path)...)
	got = pathFromCompanionSessionReader(&shortReader{rest: late, n: 1024})
	if got != path {
		t.Fatalf("late path = %q", got)
	}
}

type shortReader struct {
	rest []byte
	n    int
}

func (r *shortReader) Read(p []byte) (int, error) {
	if len(r.rest) == 0 {
		return 0, io.EOF
	}
	n := r.n
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.rest) {
		n = len(r.rest)
	}
	copy(p, r.rest[:n])
	r.rest = r.rest[n:]
	return n, nil
}

func companionSessionJSON(t *testing.T, path string) []byte {
	t.Helper()
	content := "问\n\n" + agent.FilePathPromptPrefix + "\n" + path + "\n"
	body, err := json.Marshal(map[string]any{
		"sessions": map[string]any{
			"file-companion:fc_x": map[string]any{
				"entries": []any{map[string]any{"role": "user", "content": content}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFileCompanionResumePathsMigratesSessionsOnce(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.pdf")
	if err := os.WriteFile(keep, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.pdf")
	sessions := filepath.Join(app.GetDataDir(), "file-companion", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "fc_keep.json"), companionSessionJSON(t, keep), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "fc_gone.json"), companionSessionJSON(t, missing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "fc_verb.json"), companionSessionJSON(t, "open-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := app.FileCompanionResumePaths()
	if len(got) != 1 || got[0] != keep {
		t.Fatalf("resume = %#v", got)
	}
	if err := app.SetFileCompanionOpenSet(nil); err != nil {
		t.Fatal(err)
	}
	if again := app.FileCompanionResumePaths(); len(again) != 0 {
		t.Fatalf("cleared set resurrected sessions: %#v", again)
	}
}

func TestShowFileCompanionAsksInThisProcessWhenNothingIsRemembered(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	app.processHooks.chooseFiles = func() ([]string, error) {
		return []string{file, "open-file", filepath.Join(t.TempDir(), "missing.md")}, nil
	}
	if err := app.ShowFileCompanion(); err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[1] != "open-file" || argv[2] != file {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestShowFileCompanionDoesNotStartWhenTheDialogIsCancelled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	started := false
	app.processHooks.startProcess = func(got []string) error {
		started = true
		return nil
	}
	app.processHooks.chooseFiles = func() ([]string, error) {
		return nil, nil
	}
	if err := app.ShowFileCompanion(); err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("cancel started a companion")
	}
}

func TestShowFileCompanionLeavesResumeToTheFreshProcess(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SetFileCompanionOpenSet([]string{file, "open-file"}); err != nil {
		t.Fatal(err)
	}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	asked := false
	app.processHooks.chooseFiles = func() ([]string, error) {
		asked = true
		return nil, nil
	}
	if err := app.ShowFileCompanion(); err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Fatal("history still opened the file dialog")
	}
	if len(argv) != 2 || argv[1] != "open-file" {
		t.Fatalf("argv = %#v", argv)
	}
	if got := app.FileCompanionResumePaths(); len(got) != 1 || got[0] != file {
		t.Fatalf("resume = %#v", got)
	}
}

func TestSetFileCompanionOpenSetReplacesThePreviousList(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.SetFileCompanionOpenSet([]string{first}); err != nil {
		t.Fatal(err)
	}
	if err := app.SetFileCompanionOpenSet([]string{second, first}); err != nil {
		t.Fatal(err)
	}
	got := app.FileCompanionResumePaths()
	if len(got) != 2 || got[0] != second || got[1] != first {
		t.Fatalf("resume = %#v", got)
	}
}

func TestSetFileCompanionOpenSetLeavesAnUnchangedFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	file := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SetFileCompanionOpenSet([]string{file}); err != nil {
		t.Fatal(err)
	}
	stored := fileCompanionOpenSetPath(app)
	stamped := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stored, stamped, stamped); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetFileCompanionOpenSet([]string{file}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("unchanged list was rewritten: %v -> %v", before.ModTime(), after.ModTime())
	}
}

func TestFileCompanionResumePathsOmitsFilesThatAreGoneWithoutRewriting(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.md")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.md")
	if err := app.SetFileCompanionOpenSet([]string{keep, missing}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fileCompanionOpenSetPath(app))
	if err != nil {
		t.Fatal(err)
	}
	got := app.FileCompanionResumePaths()
	if len(got) != 1 || got[0] != keep {
		t.Fatalf("resume = %#v", got)
	}
	after, err := os.ReadFile(fileCompanionOpenSetPath(app))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("resume rewrote the set:\n%s", after)
	}
	if err := app.SetFileCompanionOpenSet(got); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fileCompanionOpenSetPath(app))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "gone.md") {
		t.Fatalf("window save kept a dead path: %s", raw)
	}
}

func TestFileCompanionExistingFilesKeepsADocumentNamedOpenFile(t *testing.T) {
	dir := t.TempDir()
	named := filepath.Join(dir, "open-file")
	if err := os.WriteFile(named, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fileCompanionExistingFiles([]string{"open-file", named, named})
	if len(got) != 1 || got[0] != named {
		t.Fatalf("kept = %#v", got)
	}
}
