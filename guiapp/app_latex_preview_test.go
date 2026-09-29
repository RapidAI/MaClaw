package guiapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLatexPreviewFileURLChangesEachCompile(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	first := latexPreviewFileURL(token)
	second := latexPreviewFileURL(token)
	if first == second {
		t.Fatalf("preview url stayed %q", first)
	}
	for _, url := range []string{first, second} {
		if !strings.Contains(url, "t="+token) || !strings.Contains(url, "&v=") {
			t.Fatalf("url = %q", url)
		}
	}
}

func TestStageLatexPreviewPDFKeepsSourceReplaceable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := stageLatexPreviewPDF(src)
	if err != nil {
		t.Fatal(err)
	}
	if staged == src || filepath.Dir(staged) == dir {
		t.Fatalf("preview reused the paper file %s", staged)
	}
	view, err := os.Open(staged)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := os.WriteFile(src, []byte("%PDF-1.5\nnext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "%PDF-1.4\n" {
		t.Fatalf("preview changed with the source: %q", body)
	}
	var last string
	for i := 0; i < 20; i++ {
		last, err = stageLatexPreviewPDF(src)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(last); err != nil {
		t.Fatal(err)
	}
}

func TestCompileLatexPreviewReportsMissingTinyTeX(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()
	tex := filepath.Join(t.TempDir(), "main.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := app.CompileLatexPreview(tex)
	if err != nil {
		t.Fatal(err)
	}
	if got["ok"] != false {
		t.Fatalf("ok = %#v", got["ok"])
	}
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, "scheme-small") {
		t.Fatalf("error = %#v", got["error"])
	}
}

func TestScrubCloudCacheTextHandlesExpandingCaseFold(t *testing.T) {
	got := scrubCloudCacheText("C:/Users/İ/.maclaw/data/cloud-workspaces/tenant/ws/main.tex:1", "")
	if strings.Contains(strings.ToLower(got), "cloud-workspaces") || strings.Contains(got, "Users") {
		t.Fatalf("cache path leaked: %q", got)
	}
	repeated := strings.Repeat("C:/Users/me/.maclaw/data/cloud-workspaces/tenant/ws/main.tex\n", 200)
	if got := scrubCloudCacheText(repeated, ""); strings.Contains(strings.ToLower(got), "cloud-workspaces") || strings.Contains(got, "Users") || strings.Count(got, "main.tex") != 200 {
		t.Fatalf("long log leaked: %q", got)
	}
	pair := "(C:/Users/me/.maclaw/data/cloud-workspaces/tenant/ws/a.tex) (C:/Users/me/.maclaw/data/cloud-workspaces/tenant/ws/b.tex)"
	if got := scrubCloudCacheText(pair, ""); strings.Contains(strings.ToLower(got), "cloud-workspaces") || strings.Contains(got, "Users") || !strings.Contains(got, "a.tex") || !strings.Contains(got, "b.tex") {
		t.Fatalf("paired paths leaked: %q", got)
	}
	kept := scrubCloudCacheText("\\documentclass{article}\nC:\\Users\\My Name\\.maclaw\\data\\cloud-workspaces\\tenant\\ws\\paper\\main.tex:2: error", "")
	if !strings.Contains(kept, `\documentclass`) || strings.Contains(kept, "Users") || strings.Contains(kept, "My Name") {
		t.Fatalf("log text scrubbed wrong: %q", kept)
	}
	spaced := scrubCloudCacheText(`C:/Users/My Name/.maclaw/data/cloud-workspaces/tenant/ws/paper/main.tex:2: error`, "")
	if strings.Contains(spaced, "Users") || strings.Contains(spaced, "My Name") || strings.Contains(strings.ToLower(spaced), "cloud-workspaces") {
		t.Fatalf("spaced username leaked: %q", spaced)
	}
	readonly := scrubCloudCacheText(`C:\Users\me\.maclaw\data\cloud-workspaces-readonly\tenant\ws\main.tex:2`, "")
	if strings.Contains(strings.ToLower(readonly), "users") || strings.Contains(strings.ToLower(readonly), "cloud-workspaces") {
		t.Fatalf("readonly cache path leaked: %q", readonly)
	}
}

func TestLatexPreviewPublicPathKeepsWorkspaceRelativePath(t *testing.T) {
	got := latexPreviewPublicPath(`C:\Users\me\.maclaw\data\cloud-workspaces\tenant\ws\paper\main.tex`)
	if got != "paper/main.tex" {
		t.Fatalf("public path = %q", got)
	}
	other := latexPreviewPublicPath(`C:\Users\me\.maclaw\data\cloud-workspaces-readonly\tenant\ws\instance-1\notes\main.tex`)
	if other != "notes/main.tex" {
		t.Fatalf("readonly public path = %q", other)
	}
	if got := latexPreviewPublicPath(`D:\paper\main.tex`); got != `D:\paper\main.tex` {
		t.Fatalf("local path changed: %q", got)
	}
}

func TestHideCloudPathErrDropsCacheLocation(t *testing.T) {
	err := hideCloudPathErr(errors.New(`GetFileAttributesEx C:\Users\me\.maclaw\data\cloud-workspaces\tenant\ws\main.tex: not found`))
	if err == nil {
		t.Fatal("expected error")
	}
	got := strings.ToLower(err.Error())
	if strings.Contains(got, "users") || strings.Contains(got, "cloud-workspaces") {
		t.Fatalf("cache path leaked: %q", err.Error())
	}
	if hideCloudPathErr(nil) != nil {
		t.Fatal("nil error changed")
	}
}

func TestScrubCloudCacheTextDropsWorkspacePrefix(t *testing.T) {
	root := `C:\Users\me\.maclaw\data\cloud-workspaces\tenant_default\ws1`
	got := scrubCloudCacheText(root+`\paper\main.tex:12`, root)
	if strings.Contains(strings.ToLower(got), "cloud-workspaces") || strings.Contains(got, "Users") {
		t.Fatalf("cache path leaked: %q", got)
	}
	if !strings.Contains(got, "paper/main.tex") && !strings.Contains(got, "main.tex") {
		t.Fatalf("relative file missing: %q", got)
	}
	mixed := scrubCloudCacheText(strings.ToUpper(root)+`\PAPER\MAIN.TEX`, root)
	if strings.Contains(strings.ToLower(mixed), "cloud-workspaces") || strings.Contains(strings.ToLower(mixed), "users") {
		t.Fatalf("mixed-case cache path leaked: %q", mixed)
	}
	siblingRoot := `C:/data/cloud-workspaces/tenant_default/ws`
	sibling := scrubCloudCacheText(siblingRoot+"1/main.tex", siblingRoot)
	if strings.Contains(sibling, "1/main.tex") && !strings.Contains(sibling, "ws1") {
		t.Fatalf("longer directory was truncated: %q", sibling)
	}
	preview := scrubCloudLatexPreview(map[string]interface{}{
		"ok":       false,
		"path":     root + `\main.tex`,
		"pdf_path": root + `\main.pdf`,
		"log":      "I " + root + `\main.tex`,
	}, root)
	if _, ok := preview["path"]; ok {
		t.Fatal("path leaked")
	}
	if _, ok := preview["pdf_path"]; ok {
		t.Fatal("pdf path leaked")
	}
	logText, _ := preview["log"].(string)
	if strings.Contains(strings.ToLower(logText), "cloud-workspaces") {
		t.Fatalf("log leaked: %q", logText)
	}
}

func TestScrubCloudLatexLogFileKeepsTexCommands(t *testing.T) {
	_, root := latexCloudApp(t)
	logPath := filepath.Join(root, "main.log")
	body := "\\documentclass{article}\n" + root + "\\main.tex:3: error\n"
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	scrubCloudLatexLogFile(filepath.Join(root, "main.tex"))
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, `\documentclass`) || strings.Contains(text, "cloud-workspaces") || strings.Contains(strings.ToLower(text), "users") {
		t.Fatalf("log file = %q", text)
	}
}

func TestScrubCloudLatexLogFileClearsResolvedMain(t *testing.T) {
	_, root := latexCloudApp(t)
	if err := os.MkdirAll(filepath.Join(root, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte("\\documentclass{article}\n\\input{a}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chapter := filepath.Join(root, "chapters", "a.tex")
	if err := os.WriteFile(chapter, []byte("chapter body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "main.log")
	if err := os.WriteFile(logPath, []byte("\\documentclass{article}\n"+root+"\\main.tex:1: error\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scrubCloudLatexLogFile(chapter)
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, `\documentclass`) || strings.Contains(text, "cloud-workspaces") {
		t.Fatalf("main log = %q", text)
	}
}

func latexCloudApp(t *testing.T) (*App, string) {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	root := filepath.Join(app.GetDataDir(), "cloud-workspaces", "tenant_default", "ws1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return app, root
}

func TestCompileLatexWorkbenchFileHidesCloudCachePath(t *testing.T) {
	app, root := latexCloudApp(t)
	tex := filepath.Join(root, "main.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := app.CompileLatexWorkbenchFile(root, "main.tex")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["path"]; ok {
		t.Fatalf("cloud cache path leaked: %#v", got["path"])
	}
	if _, ok := got["pdf_path"]; ok {
		t.Fatalf("cloud pdf path leaked: %#v", got["pdf_path"])
	}
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, "scheme-small") {
		t.Fatalf("error = %#v", got["error"])
	}
	if _, err := app.CompileLatexWorkbenchFile(root, "../main.tex"); err == nil {
		t.Fatal("path escape was accepted")
	}
}

func TestLatexCompileDefersCloudPushUntilScrub(t *testing.T) {
	app, root := latexCloudApp(t)
	mount := &cloudWorkspaceHeldMount{WorkspaceID: "cws_latex_hold", LocalPath: root}
	storeCloudWorkspaceMount(mount)
	t.Cleanup(func() {
		mount.mu.Lock()
		mount.stopped = true
		if mount.pushTimer != nil {
			mount.pushTimer.Stop()
			mount.pushTimer = nil
		}
		mount.mu.Unlock()
		takeCloudWorkspaceMount(mount.WorkspaceID)
	})

	quiet := app.holdCloudWorkspaceLatexPush(root)
	quiet()
	mount.mu.Lock()
	if mount.pushTimer != nil || mount.latexPushHold != 0 {
		mount.mu.Unlock()
		t.Fatal("a compile that changed nothing scheduled an upload")
	}
	mount.mu.Unlock()

	app.scheduleCloudWorkspacePush(mount)
	release := app.holdCloudWorkspaceLatexPush(root)
	mount.mu.Lock()
	if mount.pushTimer != nil || !mount.latexPushDeferred || mount.latexPushHold != 1 {
		t.Fatalf("hold did not pause the queued upload: timer=%v deferred=%v hold=%d", mount.pushTimer != nil, mount.latexPushDeferred, mount.latexPushHold)
	}
	mount.mu.Unlock()
	app.scheduleCloudWorkspacePush(mount)
	mount.mu.Lock()
	if mount.pushTimer != nil || mount.syncPending {
		mount.mu.Unlock()
		t.Fatal("watcher upload started while compile held the workspace")
	}
	mount.mu.Unlock()

	release()
	mount.mu.Lock()
	defer mount.mu.Unlock()
	if mount.latexPushHold != 0 || mount.latexPushDeferred || mount.pushTimer == nil {
		t.Fatalf("release hold=%d deferred=%v timer=%v", mount.latexPushHold, mount.latexPushDeferred, mount.pushTimer != nil)
	}
}

func TestSaveCodingWorkbenchTextFileWritesLatexInsideCloudCache(t *testing.T) {
	app, root := latexCloudApp(t)
	if err := os.MkdirAll(filepath.Join(root, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	tex := filepath.Join(root, "chapters", "intro.tex")
	if err := os.WriteFile(tex, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	const next = "\\documentclass{article}\n"
	if err := app.SaveCodingWorkbenchTextFile(root, "chapters/intro.tex", next); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(tex)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != next {
		t.Fatalf("saved %q", body)
	}
	goFile := filepath.Join(root, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveCodingWorkbenchTextFile(root, "main.go", "package p\n"); err == nil {
		t.Fatal("non-latex save was accepted")
	}
	if err := app.SaveCodingWorkbenchTextFile(root, "../outside.tex", "x"); err == nil {
		t.Fatal("path escape was accepted")
	}
}
