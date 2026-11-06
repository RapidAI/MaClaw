package guiapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSkillInstallAlreadyCurrentReplacesAnEmptyRecordedDirectory(t *testing.T) {
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte("# Drawing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}}
	existing := &corelib.NLSkillEntry{Name: "Drawing", Status: "needs_setup", SkillDir: empty, Steps: steps}
	candidate := &corelib.NLSkillEntry{Name: "Drawing", Status: "active", SkillDir: full, Steps: steps}
	if skillInstallAlreadyCurrent(existing, candidate) {
		t.Fatal("empty recorded directory was treated as the current package")
	}
	existing.SkillDir = full
	if !skillInstallAlreadyCurrent(existing, candidate) {
		t.Fatal("identical documented package was not current")
	}
	existing.SkillDir = ""
	if !skillInstallAlreadyCurrent(existing, candidate) {
		t.Fatal("config-only entry was treated as an incomplete directory")
	}
	existing.SkillDir = filepath.Join(t.TempDir(), "gone")
	if skillInstallAlreadyCurrent(existing, candidate) {
		t.Fatal("missing directory was treated as the current package")
	}
}

func TestInstalledSkillDirIsScanResidue(t *testing.T) {
	residue := t.TempDir()
	if err := os.WriteFile(filepath.Join(residue, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(residue, ".maclaw_scan_status.db-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !installedSkillDirIsScanResidue(residue) {
		t.Fatal("scan database was not residue")
	}
	if err := removeInstalledSkillScanResidue(residue); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(residue); !os.IsNotExist(err) {
		t.Fatalf("residue remove: %v", err)
	}
	empty := t.TempDir()
	if installedSkillDirIsScanResidue(empty) || installedSkillDirIsScanResidue(filepath.Join(empty, "gone")) {
		t.Fatal("empty or missing directory was residue")
	}
	kept := t.TempDir()
	if err := os.WriteFile(filepath.Join(kept, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if installedSkillDirIsScanResidue(kept) {
		t.Fatal("a real file was residue")
	}
	documented := t.TempDir()
	if err := os.WriteFile(filepath.Join(documented, "SKILL.md"), []byte("# Drawing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if installedSkillDirIsScanResidue(documented) {
		t.Fatal("a skill document was residue")
	}
}

func TestScanResidueStaysWhenPreviousBackupExists(t *testing.T) {
	root := t.TempDir()
	finalDir := filepath.Join(root, "Drawing")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	backup := finalDir + ".prev"
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "SKILL.md"), []byte("# keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := prepareInstalledSkillPublishDestination(finalDir, false)
	if err == nil || !strings.Contains(err.Error(), "stale previous backup") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(finalDir, ".maclaw_scan_status.db")); statErr != nil {
		t.Fatalf("residue was removed despite backup: %v", statErr)
	}
	if body, readErr := os.ReadFile(filepath.Join(backup, "SKILL.md")); readErr != nil || string(body) != "# keep\n" {
		t.Fatalf("backup changed: %v %q", readErr, body)
	}

	if err := os.RemoveAll(backup); err != nil {
		t.Fatal(err)
	}
	if err := prepareInstalledSkillPublishDestination(finalDir, false); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(finalDir); !os.IsNotExist(statErr) {
		t.Fatalf("residue without a backup was kept: %v", statErr)
	}

	kept := filepath.Join(root, "Kept")
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "SKILL.md"), []byte("# kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareInstalledSkillPublishDestination(kept, true); err != nil {
		t.Fatalf("replacement: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(kept, "SKILL.md")); statErr != nil {
		t.Fatalf("replacement removed the previous package: %v", statErr)
	}
	blocked := filepath.Join(root, "Blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = prepareInstalledSkillPublishDestination(blocked, false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("real directory err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(blocked, "notes.txt")); statErr != nil {
		t.Fatalf("blocked directory was removed: %v", statErr)
	}
}

func TestInstalledSkillPackageMissingIgnoresUnreadableStat(t *testing.T) {
	decided, missing := installedSkillListMissing(os.ErrPermission)
	if !decided || missing {
		t.Fatal("permission error was treated as a missing package listing")
	}
	decided, missing = installedSkillListMissing(os.ErrNotExist)
	if !decided || !missing {
		t.Fatal("a directory that disappeared was kept")
	}
	decided, missing = installedSkillListMissing(nil)
	if decided || missing {
		t.Fatal("a readable directory listing was decided")
	}
	decided, missing = installedSkillDirectoryStat(nil, os.ErrPermission)
	if !decided || missing {
		t.Fatal("permission error was treated as a missing package")
	}
	decided, missing = installedSkillDirectoryStat(nil, os.ErrNotExist)
	if !decided || !missing {
		t.Fatal("a missing path was kept")
	}
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !installedSkillPackageMissing(&corelib.NLSkillEntry{SkillDir: file}) {
		t.Fatal("a file path was kept as a package directory")
	}
	nested := filepath.Join(file, "child")
	_, statErr := os.Stat(nested)
	if statErr == nil || os.IsNotExist(statErr) {
		return
	}
	if installedSkillPackageMissing(&corelib.NLSkillEntry{Name: "Drawing", SkillDir: nested}) {
		t.Fatalf("unreadable path %v was treated as an incomplete download", statErr)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte("# Drawing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}}
	existing := &corelib.NLSkillEntry{Name: "Drawing", SkillDir: nested, Steps: steps}
	candidate := &corelib.NLSkillEntry{Name: "Drawing", SkillDir: full, Steps: steps}
	if !skillInstallAlreadyCurrent(existing, candidate) {
		t.Fatal("unreadable recorded directory was replaced")
	}
}

func TestInstalledSkillPackageMissingKeepsUnreadableSubtree(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "aaa")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hidden := t.TempDir()
	scripts := filepath.Join(hidden, "scripts")
	if err := os.Mkdir(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseRoot, err := sealSkillSubtreeListing(locked)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseRoot)
	releaseHidden, err := sealSkillSubtreeListing(scripts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseHidden)
	if _, err := os.ReadDir(root); err != nil {
		t.Fatalf("root list: %v", err)
	}
	if _, err := os.ReadDir(locked); err == nil || os.IsNotExist(err) {
		t.Fatalf("locked child list = %v, want a read error that is not absence", err)
	}
	if found, _ := installedSkillProgramScan(root); !found {
		t.Fatal("a program beside an unreadable directory was missed")
	}
	if installedSkillPackageMissing(&corelib.NLSkillEntry{Name: "Drawing", SkillDir: root}) {
		t.Fatal("unreadable sibling directory made a package with a program look incomplete")
	}
	if _, err := os.ReadDir(scripts); err == nil || os.IsNotExist(err) {
		t.Fatalf("hidden scripts list = %v, want a read error that is not absence", err)
	}
	if found, walkErr := installedSkillProgramScan(hidden); found || walkErr == nil || os.IsNotExist(walkErr) {
		t.Fatalf("hidden program scan = found %v err %v", found, walkErr)
	}
	if installedSkillPackageMissing(&corelib.NLSkillEntry{Name: "Drawing", SkillDir: hidden}) {
		t.Fatal("unreadable script directory was treated as an incomplete download")
	}

	listable := t.TempDir()
	if err := os.Mkdir(filepath.Join(listable, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(listable, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !installedSkillPackageMissing(&corelib.NLSkillEntry{Name: "Drawing", SkillDir: listable}) {
		t.Fatal("a listable directory without a document or program was kept")
	}
}

func TestSkillInitialExecutionFailureNoteSkipsRetryOnTruncatedModel(t *testing.T) {
	note := skillInitialExecutionFailureNote("Interview", fmt.Errorf("skill execution stopped at step 1: parse response: unexpected end of JSON input"))
	if !strings.Contains(note, "marked as needs_setup") || !strings.Contains(note, "truncated") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "manage_skill") || strings.Contains(note, "search_and_install_skill") {
		t.Fatalf("truncated model response still names a skill tool: %q", note)
	}
	unpersisted := skillInitialExecutionUnpersistedNote("Interview", fmt.Errorf("skill execution stopped at step 1: parse response: unexpected end of JSON input"), fmt.Errorf("durable App status transaction unavailable"))
	if strings.Contains(unpersisted, "manage_skill") || strings.Contains(unpersisted, "before retrying") || strings.Contains(unpersisted, "marked as needs_setup") {
		t.Fatalf("unpersisted truncated note = %q", unpersisted)
	}
	retry := skillInitialExecutionFailureNote("Interview", fmt.Errorf("exit status 7"))
	if !strings.Contains(retry, `manage_skill(action="run", name="Interview")`) {
		t.Fatalf("ordinary failure note = %q", retry)
	}
	download := skillInitialExecutionFailureNote("Interview", fmt.Errorf("download skill failed: unexpected EOF"))
	if !strings.Contains(download, `manage_skill(action="run", name="Interview")`) {
		t.Fatalf("command EOF was treated as a truncated model body: %q", download)
	}
}

func TestIMInstallOnlyConfigDefinitionUsesSharedCommitter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	h := &IMMessageHandler{app: app}
	result := h.registerSkillWithoutExecution(context.Background(), &corelib.NLSkillEntry{
		Name: "im-config-only", Description: "config-only import", Source: "auto_github", Status: "active",
	}, "im-config-only", "auto_github", "desktop", "user", "user", func(string) {})
	if !result.Success {
		t.Fatalf("registerSkillWithoutExecution() = %+v", result)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Name != "im-config-only" {
		t.Fatalf("config-only install was not durably registered: %#v", cfg.NLSkills)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful config-only install left compensation records: %#v", summaries)
	}
	auditPath := cskill.DefaultEvolutionAuditPath()
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "\"kind\":\"definition_installed\"") {
		t.Fatalf("shared install audit missing definition_installed event: %s", audit)
	}
}

func TestIMInitialExecutionFailureUsesDurableStatusCommitter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	command := "exit 7"
	if os.PathSeparator == '\\' {
		command = "cmd /c exit 7"
	}
	installed := corelib.NLSkillEntry{
		Name: "im-status-committer", Source: "manual", Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": command}}},
	}
	if err := app.skillExecutor.Register(installed); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	h := &IMMessageHandler{app: app}
	result := h.executeInstalledSkill(context.Background(), &installed, installed.Name, "desktop", "user", "user", nil)
	if !strings.Contains(result.Text, "marked as needs_setup") {
		t.Fatalf("executeInstalledSkill() = %+v", result)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Status != "needs_setup" {
		t.Fatalf("status after failed initial execution = %#v", entries)
	}
	audit, err := os.ReadFile(cskill.DefaultEvolutionAuditPath())
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "status_changed") {
		t.Fatalf("durable status audit missing: %s", audit)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful status transaction left compensation records: %#v", summaries)
	}
}

func TestExecuteInstalledSkillSkipsUnrunnablePackage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("VOLC_IMAGE_KEY", "")
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	h := &IMMessageHandler{app: app}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := corelib.NLSkillEntry{
		Name: "empty-picture", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw a cat"}}},
	}
	if err := app.skillExecutor.Register(empty); err != nil {
		t.Fatal(err)
	}
	skipped := h.executeInstalledSkill(context.Background(), &empty, empty.Name, "desktop", "user", "user", func(string) {
		t.Fatal("empty package announced execution")
	})
	if skipped.Success || strings.Contains(skipped.Text, "manage_skill") || strings.Contains(skipped.Text, "search_and_install_skill") || !strings.Contains(skipped.Text, "package has no runnable program") || !strings.Contains(skipped.Text, "needs_setup") || !strings.Contains(skipped.Text, "craft_tool") || !strings.HasPrefix(skipped.Text, "failed:") {
		t.Fatalf("empty package = %+v", skipped)
	}
	if agentruntime.RegisteredToolTextOutcome(skipped.Text) != agentruntime.RegisteredToolOutcomeFailed {
		t.Fatalf("unrunnable note is not a tool failure: %q", skipped.Text)
	}
	unpersisted := skillInitialExecutionUnpersistedUnrunnableNote(empty.Name, "package has no runnable program", fmt.Errorf("durable App status transaction unavailable"))
	if !strings.HasPrefix(unpersisted, "failed:") || !strings.Contains(unpersisted, "craft_tool") || strings.Contains(unpersisted, "manage_skill") || strings.Contains(unpersisted, "search_and_install_skill") {
		t.Fatalf("unpersisted note = %q", unpersisted)
	}

	missing := corelib.NLSkillEntry{
		Name: "missing-key", Source: "manual", Status: "active",
		RequiredEnv: []string{"VOLC_IMAGE_KEY"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "cmd /c exit 7"}}},
	}
	if err := app.skillExecutor.Register(missing); err != nil {
		t.Fatal(err)
	}
	refused := h.executeInstalledSkill(context.Background(), &missing, missing.Name, "desktop", "user", "user", nil)
	if refused.Success || strings.Contains(refused.Text, "manage_skill") || strings.Contains(refused.Text, "search_and_install_skill") || strings.Contains(refused.Text, "craft_tool") || strings.Contains(refused.Text, "exit status") || !strings.Contains(refused.Text, "required credentials are empty") || !strings.HasPrefix(refused.Text, "failed:") {
		t.Fatalf("missing credential = %+v", refused)
	}
	entries := app.skillExecutor.loadSkills()
	status := map[string]string{}
	for _, entry := range entries {
		status[entry.Name] = entry.Status
	}
	if status["empty-picture"] != "needs_setup" || status["missing-key"] != "needs_setup" {
		t.Fatalf("status = %#v", entries)
	}
}

func TestExecuteInstalledSkillLeavesUnreadablePackageActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	h := &IMMessageHandler{app: app}

	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	if err := os.Mkdir(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := sealSkillSubtreeListing(scripts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	unread := corelib.NLSkillEntry{
		Name: "locked-picture", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw a cat"}}},
	}
	if err := app.skillExecutor.Register(unread); err != nil {
		t.Fatal(err)
	}
	result := h.executeInstalledSkill(context.Background(), &unread, unread.Name, "desktop", "user", "user", func(string) {
		t.Fatal("unreadable package announced execution")
	})
	if result.Success || !strings.HasPrefix(result.Text, "failed:") || !strings.Contains(result.Text, "could not be read") {
		t.Fatalf("unreadable package = %+v", result)
	}
	if strings.Contains(result.Text, "craft_tool") || strings.Contains(result.Text, "manage_skill") || strings.Contains(result.Text, "search_and_install_skill") || strings.Contains(result.Text, "needs_setup") {
		t.Fatalf("unreadable package named a closed tool or a status change: %q", result.Text)
	}
	if agentruntime.RegisteredToolTextOutcome(result.Text) != agentruntime.RegisteredToolOutcomeFailed {
		t.Fatalf("unreadable note is not a tool failure: %q", result.Text)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Status != "active" {
		t.Fatalf("status after an unreadable package = %#v", entries)
	}
}

func TestInstalledSkillAutoRunRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := &corelib.NLSkillEntry{
		Name: "pic", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}},
	}
	if got := installedSkillAutoRunRefusal(empty); got != "package has no runnable program" {
		t.Fatalf("empty package = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installedSkillAutoRunRefusal(empty); got != "" {
		t.Fatalf("scripted package = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "")
	cred := &corelib.NLSkillEntry{
		Name: "cred", Status: "active", RequiredEnv: []string{"VOLC_IMAGE_KEY"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(cred); got != "required credentials are empty" {
		t.Fatalf("missing credential = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "present")
	if got := installedSkillAutoRunRefusal(cred); got != "" {
		t.Fatalf("present credential = %q", got)
	}
	t.Setenv("OPENAI_API_KEY", "")
	proxy := &corelib.NLSkillEntry{
		Name: "proxy", Status: "active", RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(proxy); got != "" {
		t.Fatalf("proxy-provided OpenAI key = %q", got)
	}
	proxy.NoLLMAPI = true
	if got := installedSkillAutoRunRefusal(proxy); got != "required credentials are empty" {
		t.Fatalf("no_llm_api OpenAI key = %q", got)
	}
	command := &corelib.NLSkillEntry{
		Name: "py", Status: "active", RequiredEnv: []string{"python"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(command); got != "" {
		t.Fatalf("command-shaped required env = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "")
	stepEnv := &corelib.NLSkillEntry{
		Name: "step-env", Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
			"command":      "echo hi",
			"required_env": []string{"VOLC_IMAGE_KEY"},
		}}},
	}
	if got := installedSkillAutoRunRefusal(stepEnv); got != "required credentials are empty" {
		t.Fatalf("step required env = %q", got)
	}
	if got := installedSkillAutoRunRefusal(&corelib.NLSkillEntry{Name: "none", Status: "active"}); got != "no runnable steps" {
		t.Fatalf("no steps = %q", got)
	}

	docDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(docDir, "SKILL.md"), []byte("# Drawing\n\nUse the configured image model.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "prompt-system.md"), []byte("prompts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	guided := &corelib.NLSkillEntry{
		Name: "Drawing", Source: "auto_clawhub", Status: "active", SkillDir: docDir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw a lively picture"}}},
	}
	if got := installedSkillAutoRunRefusal(guided); got != installedSkillInstructionDocument {
		t.Fatalf("instruction document = %q", got)
	}
	if err := os.WriteFile(filepath.Join(docDir, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installedSkillAutoRunRefusal(guided); got != installedSkillInstructionDocument {
		t.Fatalf("instruction document with a loose script = %q", got)
	}
	bashDoc := &corelib.NLSkillEntry{
		Name: "diagram", Source: "auto_clawhub", Status: "active", SkillDir: docDir,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "python draw.py"}}},
	}
	if got := installedSkillAutoRunRefusal(bashDoc); got != "" {
		t.Fatalf("bash package with a document = %q", got)
	}
	knowledge := &corelib.NLSkillEntry{Name: "notes", Type: "knowledge", Status: "active", SkillDir: docDir}
	if got := installedSkillAutoRunRefusal(knowledge); got != "no runnable steps" {
		t.Fatalf("knowledge document = %q", got)
	}

	lockedRoot := t.TempDir()
	lockedScripts := filepath.Join(lockedRoot, "scripts")
	if err := os.Mkdir(lockedScripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockedScripts, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := sealSkillSubtreeListing(lockedScripts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	locked := &corelib.NLSkillEntry{
		Name: "locked", Source: "auto_clawhub", Status: "active", SkillDir: lockedRoot,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}},
	}
	if got := installedSkillAutoRunRefusal(locked); got != installedSkillUnreadablePackage {
		t.Fatalf("unreadable package = %q", got)
	}
	if err := os.WriteFile(filepath.Join(lockedRoot, "SKILL.md"), []byte("# Drawing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installedSkillAutoRunRefusal(locked); got != installedSkillInstructionDocument {
		t.Fatalf("document beside an unreadable directory = %q", got)
	}
	if err := os.Remove(filepath.Join(lockedRoot, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockedRoot, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installedSkillAutoRunRefusal(locked); got != "" {
		t.Fatalf("visible program beside an unreadable directory = %q", got)
	}
}

func TestExecuteInstalledSkillAdoptsInstructionDocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	h := &IMMessageHandler{app: app}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Drawing\n\nUse the configured image model.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt-system.md"), []byte("sibling\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := corelib.NLSkillEntry{
		Name: "Drawing", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}},
	}
	if err := app.skillExecutor.Register(entry); err != nil {
		t.Fatal(err)
	}
	adopted := h.executeInstalledSkill(context.Background(), &entry, entry.Name, "desktop", "user", "user", func(msg string) {
		if strings.Contains(msg, "正在执行") {
			t.Fatalf("instruction skill announced execution: %s", msg)
		}
	})
	if !adopted.Success || adopted.ProcedureName != "Drawing" || adopted.ProcedureDir != filepath.Clean(dir) {
		t.Fatalf("adopt = %+v", adopted)
	}
	if strings.HasPrefix(adopted.Text, "failed:") || strings.Contains(adopted.Text, "craft_tool") || strings.Contains(adopted.Text, "needs_setup") || !strings.Contains(adopted.Text, dir) {
		t.Fatalf("note = %q", adopted.Text)
	}
	if agentruntime.RegisteredToolTextOutcome(adopted.Text) != agentruntime.RegisteredToolOutcomeSucceeded {
		t.Fatalf("instruction note is not success: %q", adopted.Text)
	}
	for _, loaded := range app.skillExecutor.loadSkills() {
		if loaded.Name == "Drawing" && loaded.Status != "active" {
			t.Fatalf("status = %q", loaded.Status)
		}
	}
}

func TestIMPatchTextUsesSharedCommitter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	skillDir := t.TempDir()
	definition := "id: test.patchable\nname: patchable\ndescription: before\nsource: learned\nstatus: active\nsteps:\n  - action: shell\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(definition), 0644); err != nil {
		t.Fatalf("write skill definition: %v", err)
	}

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		SkillID: "test.patchable", Name: "patchable", Description: "before", Status: "active",
		Source: "learned", SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{Action: "shell", Params: map[string]interface{}{"command": "echo old"}}},
	}); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	h := &IMMessageHandler{app: app}
	result := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patchable", "find": "echo old", "replace": "echo new", "reason": "test patch",
	})
	if !strings.Contains(result, "patched successfully") {
		t.Fatalf("toolPatchSkill() = %q", result)
	}
	structuredResult := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patchable", "mode": "step", "step_index": 0,
		"field": "command", "value": "echo structured", "reason": "structured test patch",
	})
	if !strings.Contains(structuredResult, "updated to") {
		t.Fatalf("structured toolPatchSkill() = %q", structuredResult)
	}
	patched, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil || !strings.Contains(string(patched), "echo structured") {
		t.Fatalf("patched YAML = %q, err=%v", patched, err)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Name != "patchable" || entries[0].SkillDir != skillDir {
		t.Fatalf("config identity was not preserved by patch: %#v", entries)
	}
	loaded, err := loadImportedSkillEntry(skillDir)
	if err != nil || loaded == nil || len(loaded.Steps) != 1 || loaded.Steps[0].Params["command"] != "echo structured" {
		t.Fatalf("patched definition did not reload: entry=%#v err=%v", loaded, err)
	}
	patches, err := os.ReadFile(filepath.Join(skillDir, ".patches.json"))
	if err != nil || !strings.Contains(string(patches), "test patch") {
		t.Fatalf("patch history = %q, err=%v", patches, err)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful patch left compensation records: %#v", summaries)
	}
	audit, err := os.ReadFile(cskill.DefaultEvolutionAuditPath())
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "definition_patched") {
		t.Fatalf("patch audit missing definition_patched event: %s", audit)
	}
}

func TestIMPatchRollsBackWhenCheckedIndexRefreshFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	skillDir := t.TempDir()
	original := "id: test.patch-failure\nname: patch-failure\ndescription: before\nsource: learned\nstatus: active\nsteps:\n  - action: shell\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		SkillID: "test.patch-failure", Name: "patch-failure", Description: "before", Status: "active",
		Source: "learned", SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{Action: "shell", Params: map[string]interface{}{"command": "echo old"}}},
	}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return errors.New("injected index failure")
		}
		return nil
	}
	h := &IMMessageHandler{app: app}
	got := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patch-failure", "find": "echo old", "replace": "echo new", "reason": "failure test",
	})
	if !strings.Contains(got, "not committed") || !strings.Contains(got, "index_refresh_failed") {
		t.Fatalf("toolPatchSkill() = %q, want checked-index failure", got)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("YAML changed after failed patch\n got: %q\nwant: %q", data, original)
	}
	if _, err := os.Stat(filepath.Join(skillDir, ".patches.json")); !os.IsNotExist(err) {
		t.Fatalf("patch history should be absent after rollback, stat err=%v", err)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("failed patch left compensation records: %#v", summaries)
	}
}
