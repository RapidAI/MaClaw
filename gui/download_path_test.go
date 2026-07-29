package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolvePathWithBase_RelativeAndTilde(t *testing.T) {
	base := t.TempDir()
	got := resolvePathWithBase("papers/a.pdf", base)
	want := filepath.Join(base, "papers", "a.pdf")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("got %q want %q", got, want)
	}
	abs := filepath.Join(base, "abs.pdf")
	if filepath.Clean(resolvePathWithBase(abs, base)) != filepath.Clean(abs) {
		t.Fatalf("absolute path should pass through")
	}
}

func TestResolvePathWithBase_EmptyReturnsBase(t *testing.T) {
	base := t.TempDir()
	if filepath.Clean(resolvePathWithBase("", base)) != filepath.Clean(base) {
		t.Fatalf("empty path should return base")
	}
}

func TestSkillRunBlockedDownloadHint(t *testing.T) {
	if hint := skillRunBlockedDownloadHint(nil); hint != "" {
		t.Fatal("nil skill")
	}
	_ = runtime.GOOS
}

func TestIsImportantLogLineKeepsDownloadWorkdirDiagnostics(t *testing.T) {
	// When log_detail_enabled is false, only "important" lines hit maclaw.log.
	// Download/workdir wiring must stay visible for operators debugging save paths.
	must := []string{
		`[download_file] url="https://x" save_path="a.pdf" workdir="C:\\wd" abs="C:\\wd\\a.pdf"`,
		`[startup] LoadConfig done in 1ms download_file=builtin effective_wd_set=true`,
		`[startup] LoadConfig done configured_wd="C:\\wd" effective_wd="C:\\wd" skill_temp="C:\\wd\\.maclaw-tmp" download_file=builtin`,
		`[startup] workdir ready err=none download_file=builtin effective_wd_set=true`,
		`[skill-runner] inject workdir owner="desktop-user" workdir="C:\\wd" temp="C:\\wd\\.maclaw-tmp"`,
		`[startup] begin`,
		`[startup] complete in 2s`,
	}
	for _, line := range must {
		if !isImportantLogLine(line) {
			t.Fatalf("expected important: %s", line)
		}
	}
	if isImportantLogLine(`[frontend-diagnostic] {"stage":"app-render-begin"}`) {
		t.Fatal("noisy frontend diagnostics should not be forced-important")
	}
	// Registration / remote onboarding must stay visible even with log_detail off.
	regMust := []string{
		`[registration-contact] phone send rejected endpoint=/api/enroll/sms/send-code status=400 code=PHONE_REGISTRATION_DISABLED`,
		`[onboarding] ActivateRemote PatchConfig machine_id=m1 email=phone:187***`,
		`[frontend-diagnostic] {"tag":"onboarding","stage":"identity-continue"}`,
		`[frontend-diagnostic] {"tag": "onboarding", "stage":"identity-continue"}`,
	}
	for _, line := range regMust {
		if !isImportantLogLine(line) || !isRegistrationLogLine(line) {
			t.Fatalf("expected registration important: %s", line)
		}
	}
}

func TestToolDownloadBaseDirFallsBackToWorkspace(t *testing.T) {
	// No project tab / no app → same base download_file uses for local desktop sessions.
	h := &IMMessageHandler{}
	got := h.toolDownloadBaseDir()
	want := corelib.EffectiveWorkspaceDir()
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("toolDownloadBaseDir=%q want EffectiveWorkspaceDir %q", got, want)
	}
	// Relative save_path must land under that base (mirrors toolDownloadFile).
	rel := resolvePathWithBase("papers/2510.16079.pdf", got)
	if !filepath.IsAbs(rel) {
		t.Fatalf("resolved path not absolute: %q", rel)
	}
	if filepath.Dir(rel) != filepath.Join(got, "papers") {
		t.Fatalf("expected under papers/: %q base=%q", rel, got)
	}
}

func TestToolDownloadBaseDirForOwnerMatchesResolveToolWorkDir(t *testing.T) {
	tmp := t.TempDir()
	corelib.SetWorkspaceDir(tmp)
	t.Cleanup(func() { corelib.SetWorkspaceDir("") })
	h := &IMMessageHandler{}
	got := h.toolDownloadBaseDirForOwner(desktopUserID)
	if filepath.Clean(got) != filepath.Clean(tmp) {
		t.Fatalf("desktop owner base=%q want workspace %q", got, tmp)
	}
	// Project-tab style owner should use the bound path when it exists.
	proj := t.TempDir()
	owner := desktopUserID + ":" + proj
	gotProj := h.toolDownloadBaseDirForOwner(owner)
	if filepath.Clean(gotProj) != filepath.Clean(proj) {
		t.Fatalf("project owner base=%q want %q", gotProj, proj)
	}
}

func TestSkillPersistLooksLikeWipe(t *testing.T) {
	prev := []corelib.NLSkillEntry{
		{Name: "Paper Fetch", Status: "needs_review"},
		{Name: "Wget Tool", Status: "needs_review"},
		{Name: "Curl Tool", Status: "needs_review"},
		{Name: "paper_pdf_translator", Status: "active"},
	}
	if !skillPersistLooksLikeWipe(prev, []corelib.NLSkillEntry{{Name: "empty-skill"}}) {
		t.Fatal("4→unrelated singleton should look like wipe")
	}
	// Intentional delete leaving one known skill is OK.
	if skillPersistLooksLikeWipe(prev, []corelib.NLSkillEntry{{Name: "paper_pdf_translator", Status: "active"}}) {
		t.Fatal("4→1 known skill should NOT look like wipe")
	}
	if skillPersistLooksLikeWipe(prev, prev) {
		t.Fatal("same set is not a wipe")
	}
	// Updating statuses only is fine
	next := append([]corelib.NLSkillEntry(nil), prev...)
	next[0].Status = "needs_review"
	if skillPersistLooksLikeWipe(prev, next) {
		t.Fatal("status-only rewrite is not a wipe")
	}
	if !skillPersistLooksLikeWipe(prev, nil) {
		t.Fatal("clearing all skills should look like wipe")
	}
}

func TestSanitizeDownloadFileName(t *testing.T) {
	cases := map[string]string{
		"":                "download.bin",
		".":               "download.bin",
		"ok.pdf":          "ok.pdf",
		"a:b|c?.pdf":      "a_b_c_.pdf",
		"../escape.pdf":   "escape.pdf",
		"foo/bar.pdf":     "bar.pdf",
		"  spaced .pdf  ": "spaced .pdf",
	}
	for in, want := range cases {
		if got := sanitizeDownloadFileName(in); got != want {
			t.Fatalf("sanitizeDownloadFileName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDownloadFileNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://arxiv.org/pdf/2510.16079.pdf":     "2510.16079.pdf",
		"https://example.com/a/b/c.pdf?download=1": "c.pdf",
		"https://example.com/path%20x.pdf#section": "path x.pdf", // url.Path unescapes
		"https://example.com/":                     "download.bin",
		"https://example.com":                      "download.bin",
		"https://example.com/a:b|c.pdf":            "a_b_c.pdf",
	}
	for in, want := range cases {
		if got := downloadFileNameFromURL(in); got != want {
			t.Fatalf("downloadFileNameFromURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSanitizeDownloadSavePath(t *testing.T) {
	got := sanitizeDownloadSavePath(`papers/../a:b.pdf`)
	// parent segments dropped; leaf sanitized; under papers/
	wantLeaf := "a_b.pdf"
	if filepath.Base(got) != wantLeaf {
		t.Fatalf("leaf=%q want %q (full=%q)", filepath.Base(got), wantLeaf, got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("parent traversal survived: %q", got)
	}
}

func TestPathContainedInBaseAndResolveDownloadSavePath(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "papers", "a.pdf")
	if !pathContainedInBase(inside, base) {
		t.Fatal("expected inside path contained")
	}
	if !pathContainedInBase(base, base) {
		t.Fatal("base contains itself")
	}
	outside := filepath.Join(filepath.Dir(base), "other", "x.pdf")
	if pathContainedInBase(outside, base) {
		t.Fatal("outside path must not be contained")
	}

	abs, errMsg := resolveDownloadSavePath("papers/a:b.pdf", base)
	if errMsg != "" {
		t.Fatalf("unexpected err: %s", errMsg)
	}
	if !pathContainedInBase(abs, base) {
		t.Fatalf("resolved outside base: %q", abs)
	}
	if filepath.Base(abs) != "a_b.pdf" {
		t.Fatalf("leaf=%q", filepath.Base(abs))
	}

	// Explicit absolute escape must fail.
	if _, errMsg := resolveDownloadSavePath(outside, base); errMsg == "" {
		t.Fatal("expected escape rejection")
	}

	// Absolute path already under base must succeed (download_file → web_fetch handoff).
	abs2, errMsg := resolveDownloadSavePath(abs, base)
	if errMsg != "" {
		t.Fatalf("abs re-resolve: %s", errMsg)
	}
	if filepath.Clean(abs2) != filepath.Clean(abs) {
		t.Fatalf("abs re-resolve changed path: %q -> %q", abs, abs2)
	}

	// Windows case-insensitive containment (only meaningful on windows, harmless elsewhere).
	if runtime.GOOS == "windows" {
		mixed := strings.ToUpper(base[:1]) + base[1:]
		if !pathContainedInBase(inside, mixed) && !pathContainedInBase(inside, base) {
			t.Fatal("case-normalized containment should hold on windows")
		}
	}
}

func TestMergeSkillPersistSafeKeepsPrev(t *testing.T) {
	prev := []corelib.NLSkillEntry{
		{Name: "Paper Fetch", Status: "needs_review"},
		{Name: "paper_pdf_translator", Status: "active"},
	}
	next := []corelib.NLSkillEntry{
		{Name: "paper_pdf_translator", Status: "active", SuccessCount: 12},
	}
	got := mergeSkillPersistSafe(prev, next)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[1].SuccessCount != 12 {
		t.Fatalf("updated success=%d", got[1].SuccessCount)
	}
	if got[0].Name != "Paper Fetch" {
		t.Fatalf("lost Paper Fetch: %#v", got)
	}
}

func TestDetailAwareLogWriterSurvivesBrokenStderr(t *testing.T) {
	setLogDetailForTest(t, true)
	var file bytes.Buffer
	// stderr that always errors — must not prevent file capture
	w := &detailAwareLogWriter{file: &file, stderr: errWriter{}}
	msg := []byte("[download_file] url=\"u\" save_path=\"a.pdf\" workdir=\"w\" abs=\"w/a.pdf\"\n")
	if _, err := w.Write(msg); err != nil {
		t.Fatalf("Write should succeed when file sink works: %v", err)
	}
	if !bytes.Contains(file.Bytes(), []byte("[download_file]")) {
		t.Fatalf("file missing line: %q", file.String())
	}
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, os.ErrClosed
}

func TestNormalizeEmptySkillStatuses(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{Name: "paper_pdf_translator", Status: ""},
		{Name: "kept", Status: "needs_review"},
	}
	normalizeEmptySkillStatuses(skills)
	if skills[0].Status != "active" {
		t.Fatalf("empty -> active, got %q", skills[0].Status)
	}
	if skills[1].Status != "needs_review" {
		t.Fatalf("needs_review must stay, got %q", skills[1].Status)
	}
}

func TestListActiveSkillsIncludesUnknownExcludesNeedsReview(t *testing.T) {
	// Mirror provider filter contract used by BM25 skill routing.
	type item struct {
		name, status string
	}
	in := []item{
		{"paper_pdf_translator", ""},
		{"Wget Tool", "needs_review"},
		{"good", "active"},
	}
	var names []string
	for _, s := range in {
		switch normalizeSkillEntryStatus(s.status) {
		case skillEntryStatusActive, skillEntryStatusUnknown:
			names = append(names, s.name)
		}
	}
	if len(names) != 2 || names[0] != "paper_pdf_translator" || names[1] != "good" {
		t.Fatalf("names=%v", names)
	}
}

func TestDemoteBrokenGenericDownloadSkills(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{
			Name:         "Wget Tool",
			Description:  "download files with wget",
			Status:       "active",
			UsageCount:   1,
			FailureCount: 1,
			SuccessCount: 0,
			LastError:    "auto-repaired: craft_tool failed",
			Steps: []corelib.NLSkillStep{{
				Action: "shell_tool",
				Params: map[string]interface{}{"command": "wget -O o u"},
			}},
		},
		{
			Name:         "paper_pdf_translator",
			Description:  "translate papers",
			Status:       "active",
			UsageCount:   11,
			SuccessCount: 11,
		},
	}
	if !demoteBrokenGenericDownloadSkills(skills) {
		t.Fatal("expected demotion of Wget Tool")
	}
	if skills[0].Status != "needs_review" {
		t.Fatalf("wget status=%q", skills[0].Status)
	}
	if skills[1].Status != "active" {
		t.Fatalf("translator should stay active, got %q", skills[1].Status)
	}
	// Second pass is a no-op once needs_review.
	if demoteBrokenGenericDownloadSkills(skills) {
		t.Fatal("second demote should not change already-reviewed skills")
	}
}
