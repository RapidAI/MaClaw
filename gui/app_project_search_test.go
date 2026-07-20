package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func newProjectSearchTestApp(t *testing.T) *App {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	app := &App{testHomeDir: tempHome}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	return app
}

func TestEnsureMemoryStoreDefaultsToSQLite(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}
	dbPath := filepath.Join(memory.DataDirStoreDir(app.getMaclawBaseDir()), "memory.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected SQLite memory database at %s: %v", dbPath, err)
	}
}

func TestProjectIndexChangeTriggersDebouncedMemoryPipeline(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.memoryPipelineDebounce = 10 * time.Millisecond
	app.ensureMemoryStore()
	if app.memPipeline == nil {
		t.Fatal("memory pipeline was not initialized")
	}
	app.memPipeline.Stop()
	app.memPipeline = memory.NewMaintenance(app.memoryStore, nil, nil).Pipeline()
	app.memPipeline.Start()

	_, lastRun, _ := app.memPipeline.Status()
	if lastRun.IsZero() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			_, lastRun, _ = app.memPipeline.Status()
			if !lastRun.IsZero() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if lastRun.IsZero() {
		t.Fatal("memory pipeline did not complete its initial run")
	}

	taskDir := filepath.Join(app.GetDataDir(), "tasks", "pipeline-trigger-smoke")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", taskDir, err)
	}
	taskFile := filepath.Join(taskDir, "task.md")
	if err := os.WriteFile(taskFile, []byte("# Pipeline trigger smoke\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", taskFile, err)
	}
	now := time.Now()
	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Pipeline trigger smoke",
		Content:    "# Pipeline trigger smoke\n",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"manual_task", "recent_task"},
		SourceURL:  taskFile,
		SourceType: "manual",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Save(memory.Entry): %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, nextRun, _ := app.memPipeline.Status()
		if nextRun.After(lastRun) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("project index change did not trigger a debounced memory pipeline run")
}

func TestTriggerMemoryPipelineWaitsForGlobalQuietPeriod(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := newProjectSearchTestApp(t)
	app.memoryPipelineDebounce = 1 * time.Millisecond
	app.ensureMemoryStore()
	if app.memPipeline == nil {
		t.Fatal("memory pipeline was not initialized")
	}
	app.memPipeline.Stop()
	app.memPipeline = memory.NewMaintenance(app.memoryStore, nil, nil).Pipeline()

	setOwnerQuietPeriodForTest("test-global", time.Now())
	app.triggerMemoryPipelineSoon(1 * time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	_, lastRun, _ := app.memPipeline.Status()
	if !lastRun.IsZero() {
		t.Fatal("memory pipeline ran during foreground quiet period")
	}

	setOwnerQuietPeriodForTest("test-global", time.Now().Add(-foregroundAgentBackgroundQuietPeriod))
	app.triggerMemoryPipelineSoon(1 * time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, lastRun, _ = app.memPipeline.Status()
		if !lastRun.IsZero() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("memory pipeline did not run after quiet period elapsed")
}

func TestTriggerMemoryPipelineDefersWhilePreviousRunActive(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := newProjectSearchTestApp(t)
	app.memoryPipelineDebounce = 1 * time.Millisecond
	app.ensureMemoryStore()
	if app.memPipeline == nil {
		t.Fatal("memory pipeline was not initialized")
	}
	app.memPipeline.Stop()
	app.memPipeline = memory.NewMaintenance(app.memoryStore, nil, nil).Pipeline()

	app.memoryPipelineScheduleMu.Lock()
	app.memoryPipelineScheduleSeq = 11
	app.memoryPipelineRunActive = true
	app.memoryPipelineScheduleMu.Unlock()

	app.runMemoryPipelineWhenIdle(11)

	app.memoryPipelineScheduleMu.Lock()
	timer := app.memoryPipelineTimer
	active := app.memoryPipelineRunActive
	app.memoryPipelineScheduleMu.Unlock()
	if timer == nil {
		t.Fatal("active memory pipeline did not defer later run")
	}
	if !active {
		t.Fatal("deferred active run marker was cleared too early")
	}
	_, lastRun, _ := app.memPipeline.Status()
	if !lastRun.IsZero() {
		t.Fatal("memory pipeline started a second run while previous run was active")
	}
}

func TestMemoryPipelinePanicRecoversAndReschedules(t *testing.T) {
	resetForegroundAgentOwnersForTest()
	defer resetForegroundAgentOwnersForTest()

	app := newProjectSearchTestApp(t)
	app.memoryPipelineDebounce = 1 * time.Millisecond
	app.memPipeline = memory.NewPipeline(nil, nil, nil, nil, nil)
	app.memoryPipelineScheduleMu.Lock()
	app.memoryPipelineScheduleSeq = 21
	app.memoryPipelineScheduleMu.Unlock()

	app.runMemoryPipelineWhenIdle(21)

	app.memoryPipelineScheduleMu.Lock()
	timer := app.memoryPipelineTimer
	active := app.memoryPipelineRunActive
	runSeq := app.memoryPipelineRunSeq
	app.memoryPipelineScheduleMu.Unlock()
	if active || runSeq != 0 {
		t.Fatalf("panic cleanup left active=%v runSeq=%d", active, runSeq)
	}
	if timer == nil {
		t.Fatal("panic did not reschedule memory pipeline")
	}
}

func TestCreateRecentTaskAddsSearchableRecentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("  \u65b0\u4efb\u52a1  ")
	if created.Name != "\u65b0\u4efb\u52a1" {
		t.Fatalf("Name = %q, want new task title", created.Name)
	}
	if created.ProjectPath == "" || created.ID == "" {
		t.Fatalf("expected project identifiers, got %#v", created)
	}
	if !strings.Contains(filepath.Clean(created.ProjectPath), filepath.Clean(filepath.Join(".maclaw", "data", "tasks"))) {
		t.Fatalf("ProjectPath = %q, want a synthetic task path under data/tasks", created.ProjectPath)
	}
	if created.EntryCount != 1 {
		t.Fatalf("EntryCount = %d, want 1", created.EntryCount)
	}
	if !created.HasOutput {
		t.Fatalf("HasOutput = false, want true for manual task")
	}
	taskFile := filepath.Join(created.ProjectPath, "task.md")
	content, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", taskFile, err)
	}
	if !strings.Contains(string(content), "# \u65b0\u4efb\u52a1") {
		t.Fatalf("task file content = %q, want task title", content)
	}

	found := false
	for _, result := range app.SearchProjects("\u65b0\u4efb\u52a1", 10) {
		if result.ProjectPath == created.ProjectPath && result.Name == "\u65b0\u4efb\u52a1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created task was not returned by SearchProjects")
	}
}

func TestSearchProjectsFiltersNonOutputRecords(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	now := time.Now()

	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Small Talk Continue",
		Content:    "Task: continue\nResult: ok",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", filepath.Join(app.GetDataDir(), "tasks", "small-talk")},
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Save non-output: %v", err)
	}
	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Improve Task Management Filtering",
		Content:    "Task: task management\nResult: Added has_output filtering",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", filepath.Join(app.GetDataDir(), "tasks", "output-task")},
		CreatedAt:  now,
		UpdatedAt:  now.Add(time.Second),
	}); err != nil {
		t.Fatalf("Save output: %v", err)
	}

	results := app.SearchProjects("", 10)
	if len(results) != 1 {
		t.Fatalf("SearchProjects returned %d records, want 1: %+v", len(results), results)
	}
	if results[0].Name != "Improve Task Management Filtering" || !results[0].HasOutput {
		t.Fatalf("result = %+v, want output-backed task", results[0])
	}
}

func TestListTasksOnlyShowsExplicitTaskManagementRecords(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	now := time.Now()

	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Automatic sediment",
		Content:    "Task: automatic\nResult: generated",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", filepath.Join(app.GetDataDir(), "tasks", "auto")},
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Save automatic sediment: %v", err)
	}

	created := app.CreateTask("Explicit created task", "")
	saved := app.createTaskRecord("Explicit saved task", "# Explicit saved task\n", []string{taskManagementTag, taskUserSavedTag}, true)
	forked := app.ForkRecentTask(created.ProjectPath)
	if created.ProjectPath == "" || saved.ProjectPath == "" || forked.ProjectPath == "" {
		t.Fatalf("setup task paths created=%q saved=%q forked=%q", created.ProjectPath, saved.ProjectPath, forked.ProjectPath)
	}

	results := app.ListTasks(20)
	paths := map[string]bool{}
	for _, result := range results {
		paths[result.ProjectPath] = true
	}
	if !paths[created.ProjectPath] || !paths[saved.ProjectPath] {
		t.Fatalf("ListTasks paths = %#v, want explicit created and saved tasks", paths)
	}
	if paths[forked.ProjectPath] {
		t.Fatalf("ListTasks included forked task path %q", forked.ProjectPath)
	}
	if paths[filepath.Join(app.GetDataDir(), "tasks", "auto")] {
		t.Fatalf("ListTasks included automatic sediment path")
	}
}

func TestCreateRecentTaskUsesTaskNamePreview(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Draft task management filtering implementation")
	if created.Name != "Draft task management filtering implementation" {
		t.Fatalf("Name = %q, want task name", created.Name)
	}
	if created.Preview == "Manual task placeholder." {
		t.Fatalf("Preview = %q, want a user-facing task preview", created.Preview)
	}
}

func TestNormalizeCreateTaskMode(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"chat":             "",
		"coding_dev":       taskCodingDevTag,
		"CODING":           taskCodingDevTag,
		"programming":      taskCodingDevTag,
		"code":             taskCodingDevTag,
		"remote_coding_dev": taskRemoteCodingDevTag,
		"remote_coding":    taskRemoteCodingDevTag,
		"remote_code":      taskRemoteCodingDevTag,
	}
	for input, want := range cases {
		if got := NormalizeCreateTaskMode(input); got != want {
			t.Fatalf("NormalizeCreateTaskMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCreateRemoteCodingTaskTagsMetadata(t *testing.T) {
	app := newProjectSearchTestApp(t)
	hasTag := func(tags []string, target string) bool {
		for _, tag := range tags {
			if tag == target {
				return true
			}
		}
		return false
	}

	created := app.CreateRemoteCodingTask("Fix remote API", "10.0.0.8", "ubuntu", "/home/ubuntu/app", 22)
	if created.ProjectPath == "" {
		t.Fatal("CreateRemoteCodingTask returned empty project path")
	}
	if !hasTag(created.Tags, taskRemoteCodingDevTag) {
		t.Fatalf("tags = %#v, want remote_coding_dev", created.Tags)
	}
	if !hasTag(created.Tags, taskRemoteHostTagPrefix+"10.0.0.8") {
		t.Fatalf("tags = %#v, want remote_host", created.Tags)
	}
	if !hasTag(created.Tags, taskRemoteUserTagPrefix+"ubuntu") {
		t.Fatalf("tags = %#v, want remote_user", created.Tags)
	}
	if !hasTag(created.Tags, taskRemoteWorkDirTagPrefix+"/home/ubuntu/app") {
		t.Fatalf("tags = %#v, want remote_workdir", created.Tags)
	}
	// Password must never appear in tags.
	for _, tag := range created.Tags {
		if strings.Contains(strings.ToLower(tag), "password") || strings.Contains(tag, "secret") {
			t.Fatalf("tags must not contain password material: %#v", created.Tags)
		}
	}

	if empty := app.CreateRemoteCodingTask("x", "", "u", "/tmp", 22); empty.ProjectPath != "" {
		t.Fatal("missing host should fail")
	}

	// Host values that contain ":" (e.g. IPv6) must remain connectable.
	ipv6 := app.CreateRemoteCodingTask("ipv6", "2001:db8::1", "root", "/tmp/app", 22)
	if !hasTag(ipv6.Tags, taskRemoteHostTagPrefix+"2001:db8::1") {
		t.Fatalf("ipv6 host tags = %#v", ipv6.Tags)
	}
	meta, err := app.GetRemoteCodingTaskMeta(ipv6.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Host != "2001:db8::1" {
		t.Fatalf("GetRemoteCodingTaskMeta host=%q", meta.Host)
	}
}

func TestSanitizeTaskMetadataTagValue(t *testing.T) {
	if got := sanitizeTaskMetadataTagValue(" a:b\nc "); got != "a:b c" {
		t.Fatalf("sanitize = %q want %q", got, "a:b c")
	}
	if got := sanitizeTaskMetadataTagValue("2001:db8::1"); got != "2001:db8::1" {
		t.Fatalf("ipv6 sanitize = %q", got)
	}
	if got := sanitizeTaskMetadataTagValue("x\x00y"); got != "xy" {
		t.Fatalf("control strip = %q", got)
	}
	if got := normalizeSSHHostInput("[2001:db8::1]"); got != "2001:db8::1" {
		t.Fatalf("bracket ipv6 = %q", got)
	}
	if !looksLikeAbsoluteProjectPathTag(`D:\work\tasks\x`) || looksLikeAbsoluteProjectPathTag("remote_host:10.0.0.1") {
		t.Fatal("looksLikeAbsoluteProjectPathTag")
	}
	if shouldKeepTaskTagOnRemoteMetaUpdate("task_sediment") || !shouldKeepTaskTagOnRemoteMetaUpdate(taskRemoteCodingDevTag) {
		t.Fatal("shouldKeepTaskTagOnRemoteMetaUpdate")
	}
}

func TestTestRemoteSSHConnectionValidation(t *testing.T) {
	app := newProjectSearchTestApp(t)
	if _, err := app.TestRemoteSSHConnection("", "u", "p", "/tmp", 22); err == nil {
		t.Fatal("expected missing host error")
	}
	if _, err := app.TestRemoteSSHConnection("h", "", "p", "/tmp", 22); err == nil {
		t.Fatal("expected missing user error")
	}
	if _, err := app.TestRemoteSSHConnection("h", "u", "", "/tmp", 22); err == nil {
		t.Fatal("expected missing password error")
	}
}

func TestUpdateRemoteCodingTaskMeta(t *testing.T) {
	app := newProjectSearchTestApp(t)
	hasTag := func(tags []string, target string) bool {
		for _, tag := range tags {
			if tag == target {
				return true
			}
		}
		return false
	}
	created := app.CreateRemoteCodingTask("Edit remote meta", "10.0.0.1", "dev", "/old/path", 22)
	if created.ProjectPath == "" {
		t.Fatal("create failed")
	}
	taskFile := filepath.Join(created.ProjectPath, "task.md")
	before, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read task.md: %v", err)
	}
	if !strings.Contains(string(before), "Edit remote meta") && !strings.Contains(string(before), "Created from task management") {
		t.Fatalf("unexpected initial task.md body: %q", before)
	}
	if err := app.UpdateRemoteCodingTaskMeta(created.ProjectPath, "10.0.0.9", "ops", "/new/app", 2222); err != nil {
		t.Fatalf("UpdateRemoteCodingTaskMeta: %v", err)
	}
	meta, err := app.GetRemoteCodingTaskMeta(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Host != "10.0.0.9" || meta.User != "ops" || meta.Port != 2222 {
		t.Fatalf("meta=%+v", meta)
	}
	if meta.WorkDir != "/new/app" {
		t.Fatalf("workdir=%q want /new/app", meta.WorkDir)
	}
	// Re-read from index tags after update.
	pi := app.memoryStore.ProjectIndex()
	rec := pi.Get(created.ProjectPath)
	if rec == nil {
		t.Fatal("record missing")
	}
	if !hasTag(rec.Tags, taskRemoteHostTagPrefix+"10.0.0.9") {
		t.Fatalf("tags=%#v", rec.Tags)
	}
	if hasTag(rec.Tags, taskRemoteHostTagPrefix+"10.0.0.1") {
		t.Fatalf("old host should be replaced: %#v", rec.Tags)
	}
	if hasTag(rec.Tags, taskRemoteWorkDirTagPrefix+"/old/path") {
		t.Fatalf("old workdir should be replaced: %#v", rec.Tags)
	}
	if !hasTag(rec.Tags, taskRemoteWorkDirTagPrefix+"/new/app") {
		t.Fatalf("new workdir missing: %#v", rec.Tags)
	}
	after, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read task.md after update: %v", err)
	}
	// Body must not be wiped to bare title — original create content kept.
	if !strings.Contains(string(after), "Created from task management") {
		t.Fatalf("task.md content wiped: %q", after)
	}
	if !strings.Contains(string(after), "Remote work directory: /new/app") {
		t.Fatalf("task.md missing workdir line: %q", after)
	}
	// Second update should rewrite the workdir line, not stack duplicates.
	if err := app.UpdateRemoteCodingTaskMeta(created.ProjectPath, "10.0.0.9", "ops", "/newer", 2222); err != nil {
		t.Fatalf("second update: %v", err)
	}
	after2, _ := os.ReadFile(taskFile)
	if c := strings.Count(string(after2), "Remote work directory:"); c != 1 {
		t.Fatalf("expected 1 workdir line, got %d in %q", c, after2)
	}
	// Non-remote task should reject.
	chat := app.CreateTask("plain", "")
	if err := app.UpdateRemoteCodingTaskMeta(chat.ProjectPath, "h", "u", "/w", 22); err == nil {
		t.Fatal("expected reject for non-remote task")
	}
}

func TestMergeTagsReplacePrefixed(t *testing.T) {
	existing := []string{
		"manual_task", "recent_task", "/task",
		taskRemoteHostTagPrefix + "old",
		taskRemoteWorkDirTagPrefix + "/old/path",
		"coding_extra",
	}
	desired := []string{
		"manual_task", "recent_task", "/task",
		taskRemoteHostTagPrefix + "new",
		taskRemoteWorkDirTagPrefix + "/new/app",
	}
	got := mergeTagsReplacePrefixed(existing, desired, remoteCodingMetaTagPrefixes)
	has := func(s string) bool {
		for _, t := range got {
			if t == s {
				return true
			}
		}
		return false
	}
	if !has(taskRemoteHostTagPrefix+"new") || has(taskRemoteHostTagPrefix+"old") {
		t.Fatalf("host replace failed: %#v", got)
	}
	if !has(taskRemoteWorkDirTagPrefix+"/new/app") || has(taskRemoteWorkDirTagPrefix+"/old/path") {
		t.Fatalf("workdir replace failed: %#v", got)
	}
	if !has("coding_extra") {
		t.Fatalf("should keep non-meta existing tag: %#v", got)
	}
}

func TestUpsertRemoteWorkDirContentLine(t *testing.T) {
	base := "# Title\n\nCreated from task management.\n"
	once := upsertRemoteWorkDirContentLine(base, "/a")
	if !strings.Contains(once, "Remote work directory: /a") {
		t.Fatalf("append failed: %q", once)
	}
	twice := upsertRemoteWorkDirContentLine(once, "/b")
	if strings.Count(twice, "Remote work directory:") != 1 {
		t.Fatalf("duplicate lines: %q", twice)
	}
	if !strings.Contains(twice, "Remote work directory: /b") {
		t.Fatalf("replace failed: %q", twice)
	}
	if !strings.Contains(twice, "Created from task management") {
		t.Fatalf("body wiped: %q", twice)
	}
}

func TestUpdateRemoteCodingTaskMetaSlashPathVariant(t *testing.T) {
	app := newProjectSearchTestApp(t)
	created := app.CreateRemoteCodingTask("Path identity", "10.0.0.1", "dev", "/old", 22)
	if created.ProjectPath == "" {
		t.Fatal("create failed")
	}
	// UI may send forward-slash paths on Windows; update must still hit the same artifact.
	alt := strings.ReplaceAll(created.ProjectPath, `\`, `/`)
	if err := app.UpdateRemoteCodingTaskMeta(alt, "10.0.0.2", "ops", "/new", 22); err != nil {
		t.Fatalf("update via slash variant: %v", err)
	}
	meta, err := app.GetRemoteCodingTaskMeta(created.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Host != "10.0.0.2" || meta.WorkDir != "/new" {
		t.Fatalf("meta=%+v", meta)
	}
	rec := app.memoryStore.ProjectIndex().Get(created.ProjectPath)
	if rec == nil {
		t.Fatal("record missing")
	}
	hostTags := 0
	for _, tag := range rec.Tags {
		if strings.HasPrefix(tag, taskRemoteHostTagPrefix) {
			hostTags++
		}
	}
	if hostTags != 1 {
		t.Fatalf("want single remote_host tag, got %#v", rec.Tags)
	}
}

func TestBuildRemoteCodingMetaTags(t *testing.T) {
	tags := buildRemoteCodingMetaTags("h", "u", "/w", 0)
	if len(tags) != 4 || tags[2] != taskRemotePortTagPrefix+"22" {
		t.Fatalf("default port tags=%#v", tags)
	}
	if !isRemoteCodingMetaTag(taskRemoteHostTagPrefix + "x") {
		t.Fatal("isRemoteCodingMetaTag host")
	}
	if isRemoteCodingMetaTag(taskRemoteCodingDevTag) {
		t.Fatal("mode tag is not meta host/user/port/workdir")
	}
}

func TestCreateTaskWithModeTagsCodingDev(t *testing.T) {
	app := newProjectSearchTestApp(t)
	hasTag := func(tags []string, target string) bool {
		for _, tag := range tags {
			if tag == target {
				return true
			}
		}
		return false
	}

	ordinary := app.CreateTask("Ordinary chat task", "")
	if ordinary.ProjectPath == "" {
		t.Fatal("CreateTask returned empty project path")
	}
	if hasTag(ordinary.Tags, taskCodingDevTag) {
		t.Fatalf("ordinary CreateTask tags = %#v, did not expect coding_dev", ordinary.Tags)
	}

	coding := app.CreateTaskWithMode("Implement auth module", "", "coding_dev")
	if coding.ProjectPath == "" {
		t.Fatal("CreateTaskWithMode returned empty project path")
	}
	if !hasTag(coding.Tags, taskCodingDevTag) {
		t.Fatalf("coding CreateTaskWithMode tags = %#v, want coding_dev", coding.Tags)
	}
	if !hasTag(coding.Tags, taskManagementTag) || !hasTag(coding.Tags, taskUserCreatedTag) {
		t.Fatalf("coding CreateTaskWithMode tags = %#v, want management + user_created", coding.Tags)
	}

	// Alias modes should also stamp coding_dev.
	aliased := app.CreateTaskWithMode("Fix bug with tools", "", "programming")
	if !hasTag(aliased.Tags, taskCodingDevTag) {
		t.Fatalf("alias CreateTaskWithMode tags = %#v, want coding_dev", aliased.Tags)
	}

	// History list must surface coding tags so the sidebar can show pure-coding badges.
	listed := app.ListTasks(50)
	foundCoding := false
	for _, item := range listed {
		if item.ProjectPath == coding.ProjectPath {
			foundCoding = true
			if !hasTag(item.Tags, taskCodingDevTag) {
				t.Fatalf("ListTasks coding tags = %#v, want coding_dev", item.Tags)
			}
		}
	}
	if !foundCoding {
		t.Fatal("ListTasks missing coding task")
	}
}

func TestEnsureCodingWorkbenchArmedRestoresPureCodingOnly(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.interactionInfraDone.Store(true)
	app.warmupDone.Store(true)
	app.disableBackgroundEmbeddingForTest = true
	// Wire a hub/IM handler so Ensure can re-arm sticky coding sessions.
	manager := NewRemoteSessionManager(app)
	app.remoteSessions = manager
	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	handler := client.ensureIMHandler()
	if handler == nil {
		t.Fatal("expected IM handler")
	}

	ordinary := app.CreateTask("Chat only task", "")
	coding := app.CreateTaskWithMode("Pure coding restore", "", "coding_dev")
	if ordinary.ProjectPath == "" || coding.ProjectPath == "" {
		t.Fatal("failed to create tasks")
	}

	// Simulate sticky pollution (legacy bug left kind=local on a chat task).
	ordinaryUser := projectSessionOwnerID(ordinary.ProjectPath)
	handler.markStickyCodingSessionFullAccess(ordinaryUser, "local", ordinary.ProjectPath)
	handler.rearmStickyLocalCodingEnvironment(ordinaryUser, ordinary.ProjectPath)
	if !handler.hasPendingTemplateSubAgentExecution(ordinaryUser) {
		t.Fatal("setup: expected polluted sticky pending for ordinary task")
	}

	// Ordinary tasks must NOT arm CodingSubAgent routing, even with sticky pollution.
	stOrdinary, err := app.EnsureCodingWorkbenchArmed(ordinary.ProjectPath)
	if err != nil {
		t.Fatalf("Ensure ordinary: %v", err)
	}
	if stOrdinary.Armed || stOrdinary.Kind == "local" || stOrdinary.Kind == "remote" {
		t.Fatalf("ordinary task should not arm pure coding, status=%+v", stOrdinary)
	}
	if handler.hasPendingTemplateSubAgentExecution(ordinaryUser) {
		t.Fatal("ordinary task should clear polluted sticky coding pending")
	}
	if mem := handler.getStickyCodingWorkbenchMemory(ordinaryUser); mem.Kind == "local" || mem.Kind == "remote" {
		t.Fatalf("ordinary task should clear polluted sticky kind, got %+v", mem)
	}

	// Pure coding tasks re-arm local CodingSubAgent and seed plan for compaction recovery.
	stCoding, err := app.EnsureCodingWorkbenchArmed(coding.ProjectPath)
	if err != nil {
		t.Fatalf("Ensure coding: %v", err)
	}
	if !stCoding.Armed || stCoding.Kind != "local" {
		t.Fatalf("coding task should arm local workbench, status=%+v", stCoding)
	}
	userID := projectSessionOwnerID(coding.ProjectPath)
	if !handler.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("coding restore should re-arm sticky local coding pending")
	}
	mem := handler.getStickyCodingWorkbenchMemory(userID)
	if mem.Kind != "local" {
		t.Fatalf("sticky kind = %q, want local", mem.Kind)
	}
	if strings.TrimSpace(mem.SessionPlan) == "" {
		t.Fatal("expected session plan seeded from task title for compaction recovery")
	}
}

func TestCreateRecentTaskWithWorkingDirPersistsDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "project")

	created := app.CreateRecentTaskWithWorkingDir("Build local feature", "  "+workingDir+"  ")
	if created.ProjectPath == "" {
		t.Fatalf("ProjectPath is empty: %#v", created)
	}
	if created.WorkingDir != filepath.Clean(workingDir) {
		t.Fatalf("WorkingDir = %q, want %q", created.WorkingDir, filepath.Clean(workingDir))
	}
	if got := app.recentTaskWorkingDir(created.ProjectPath); got != filepath.Clean(workingDir) {
		t.Fatalf("recentTaskWorkingDir = %q, want %q", got, filepath.Clean(workingDir))
	}
	if got := app.recentTaskExecutionProjectPath(created.ProjectPath); got != filepath.Clean(workingDir) {
		t.Fatalf("recentTaskExecutionProjectPath = %q, want %q", got, filepath.Clean(workingDir))
	}
	taskFile := filepath.Join(created.ProjectPath, "task.md")
	content, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", taskFile, err)
	}
	if !strings.Contains(string(content), "Working directory: "+filepath.Clean(workingDir)) {
		t.Fatalf("task file content missing working directory: %q", content)
	}

	found := false
	for _, result := range app.SearchProjects("Build local feature", 10) {
		if result.ProjectPath == created.ProjectPath {
			found = true
			if result.WorkingDir != filepath.Clean(workingDir) {
				t.Fatalf("search WorkingDir = %q, want %q", result.WorkingDir, filepath.Clean(workingDir))
			}
		}
	}
	if !found {
		t.Fatalf("created task was not returned by SearchProjects")
	}
}

func TestEnsureRecentTaskExecutionWorkingDirCreatesMissingDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "project", "presentation_design")
	created := app.CreateRecentTaskWithWorkingDir("Generate presentation", workingDir)
	if created.ProjectPath == "" {
		t.Fatalf("ProjectPath is empty: %#v", created)
	}
	if _, err := os.Stat(workingDir); !os.IsNotExist(err) {
		t.Fatalf("workingDir should start missing, stat err=%v", err)
	}

	executionPath := app.recentTaskExecutionProjectPath(created.ProjectPath)
	if err := app.ensureRecentTaskExecutionWorkingDir(created.ProjectPath, executionPath); err != nil {
		t.Fatalf("ensureRecentTaskExecutionWorkingDir failed: %v", err)
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		t.Fatalf("workingDir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workingDir is not a directory: %s", workingDir)
	}
}

func TestEnsureRecentTaskExecutionWorkingDirRejectsRelativeDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)

	err := app.ensureRecentTaskExecutionWorkingDir(filepath.Join(t.TempDir(), "task"), "relative-presentation-design")
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("ensureRecentTaskExecutionWorkingDir error = %v, want not-absolute error", err)
	}
}

func TestCreateRecentTaskWithWorkingDirRejectsFilePath(t *testing.T) {
	app := newProjectSearchTestApp(t)
	filePath := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	created := app.CreateRecentTaskWithWorkingDir("Reject invalid working dir", filePath)
	if created.ProjectPath == "" {
		t.Fatalf("ProjectPath is empty: %#v", created)
	}
	if created.WorkingDir != "" {
		t.Fatalf("WorkingDir = %q, want empty for file path", created.WorkingDir)
	}
	if got := app.recentTaskWorkingDir(created.ProjectPath); got != "" {
		t.Fatalf("recentTaskWorkingDir = %q, want empty", got)
	}
	wantExecutionPath := filepath.Join(created.ProjectPath, "workspace")
	if got := app.recentTaskExecutionProjectPath(created.ProjectPath); got != wantExecutionPath {
		t.Fatalf("recentTaskExecutionProjectPath = %q, want workspace path %q", got, wantExecutionPath)
	}
}

func TestCreateRecentTaskWithWorkingDirRejectsRelativePath(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTaskWithWorkingDir("Reject relative working dir", "relative-presentation-design")
	if created.ProjectPath == "" {
		t.Fatalf("ProjectPath is empty: %#v", created)
	}
	if created.WorkingDir != "" {
		t.Fatalf("WorkingDir = %q, want empty for relative path", created.WorkingDir)
	}
	if got := app.recentTaskWorkingDir(created.ProjectPath); got != "" {
		t.Fatalf("recentTaskWorkingDir = %q, want empty", got)
	}
	wantExecutionPath := filepath.Join(created.ProjectPath, "workspace")
	if got := app.recentTaskExecutionProjectPath(created.ProjectPath); got != wantExecutionPath {
		t.Fatalf("recentTaskExecutionProjectPath = %q, want workspace path %q", got, wantExecutionPath)
	}
}

func searchResultsContainProjectPath(results []ProjectSearchResult, projectPath string) bool {
	for _, result := range results {
		if result.ProjectPath == projectPath {
			return true
		}
	}
	return false
}

func TestForkRecentTaskCreatesIndependentTaskPath(t *testing.T) {
	app := newProjectSearchTestApp(t)

	workingDir := filepath.Join(t.TempDir(), "worktree")
	source := app.CreateRecentTaskWithWorkingDir("Draft task management filtering implementation", workingDir)
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}
	if forked.Name != source.Name {
		t.Fatalf("ForkRecentTask Name = %q, want %q", forked.Name, source.Name)
	}
	if forked.WorkingDir != filepath.Clean(workingDir) {
		t.Fatalf("ForkRecentTask WorkingDir = %q, want %q", forked.WorkingDir, filepath.Clean(workingDir))
	}
	if got := app.recentTaskExecutionProjectPath(forked.ProjectPath); got != filepath.Clean(workingDir) {
		t.Fatalf("fork execution project path = %q, want %q", got, filepath.Clean(workingDir))
	}
	content, err := os.ReadFile(filepath.Join(forked.ProjectPath, "task.md"))
	if err != nil {
		t.Fatalf("ReadFile(fork task.md): %v", err)
	}
	if !strings.Contains(string(content), "Opened from task management.") || !strings.Contains(string(content), source.ProjectPath) {
		t.Fatalf("fork task content = %q, want source metadata", content)
	}
	if !strings.Contains(string(content), "Working directory: "+filepath.Clean(workingDir)) {
		t.Fatalf("fork task content missing working directory: %q", content)
	}
	recent := app.SearchProjects("", 10)
	if len(recent) != 1 || !searchResultsContainProjectPath(recent, forked.ProjectPath) {
		t.Fatalf("task management after fork = %+v, want only independent fork", recent)
	}
	search := app.SearchProjects("Draft task management", 10)
	if len(search) != 1 || !searchResultsContainProjectPath(search, forked.ProjectPath) {
		t.Fatalf("search after fork = %+v, want only independent fork", search)
	}
}

func TestForkRecentTaskRepeatedOpenReusesIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Draft task management filtering implementation")
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	second := app.ForkRecentTask(source.ProjectPath)
	if second.ProjectPath != forked.ProjectPath {
		t.Fatalf("second ForkRecentTask path = %q, want reused fork %q", second.ProjectPath, forked.ProjectPath)
	}
	if second.Name != forked.Name {
		t.Fatalf("second ForkRecentTask Name = %q, want %q", second.Name, forked.Name)
	}
	child := app.ForkRecentTask(forked.ProjectPath)
	if child.ProjectPath != forked.ProjectPath {
		t.Fatalf("child ForkRecentTask path = %q, want same existing fork %q", child.ProjectPath, forked.ProjectPath)
	}
	recent := app.SearchProjects("Draft task management", 10)
	if len(recent) != 1 || !searchResultsContainProjectPath(recent, forked.ProjectPath) {
		t.Fatalf("task management after repeated opens = %+v, want one independent fork", recent)
	}
}

func TestRecentTaskCarriesActiveWorkflowState(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowEngine = workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)

	source := app.CreateRecentTask("Workflow stage output")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	if _, err := app.workflowEngine.StartWorkflowWithOptions(projectSessionOwnerID(source.ProjectPath), workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}, workflow.WorkflowStartOptions{ProjectPath: source.ProjectPath}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}

	recent := app.SearchProjects("Workflow stage output", 10)
	if len(recent) != 1 {
		t.Fatalf("SearchProjects returned %d results: %+v", len(recent), recent)
	}
	if recent[0].ActiveWorkflow == nil || recent[0].ActiveWorkflow.ProjectPath != source.ProjectPath || recent[0].ActiveWorkflow.Type != string(workflow.WorkflowCoding) {
		t.Fatalf("ActiveWorkflow = %#v, want source coding workflow", recent[0].ActiveWorkflow)
	}
}

func TestForkedRecentTaskKeepsPointerToSourceWorkflow(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowEngine = workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)

	source := app.CreateRecentTask("Forked workflow stage output")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	if _, err := app.workflowEngine.StartWorkflowWithOptions(projectSessionOwnerID(source.ProjectPath), workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}, workflow.WorkflowStartOptions{ProjectPath: source.ProjectPath}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}

	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	recent := app.SearchProjects("Forked workflow stage output", 10)
	if len(recent) != 1 || recent[0].ProjectPath != forked.ProjectPath {
		t.Fatalf("SearchProjects after fork = %+v, want visible fork only", recent)
	}
	if recent[0].ActiveWorkflow == nil || recent[0].ActiveWorkflow.ProjectPath != source.ProjectPath {
		t.Fatalf("fork ActiveWorkflow = %#v, want pointer to source %q", recent[0].ActiveWorkflow, source.ProjectPath)
	}
	scene, err := app.GetProjectScene(forked.ProjectPath)
	if err != nil {
		t.Fatalf("GetProjectScene(fork) error = %v", err)
	}
	if scene.ActiveWorkflow == nil || scene.ActiveWorkflow.ProjectPath != source.ProjectPath {
		t.Fatalf("scene ActiveWorkflow = %#v, want pointer to source %q", scene.ActiveWorkflow, source.ProjectPath)
	}
}

func TestRecentTaskWithWorkingDirCarriesExecutionWorkflowState(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowEngine = nil
	app.workflowV2 = buildWorkflowV2State(workflow.NewMemoryStore())

	workingDir := filepath.Join(t.TempDir(), "workflow-workdir")
	task := app.CreateRecentTaskWithWorkingDir("Working dir workflow state", workingDir)
	if task.ProjectPath == "" {
		t.Fatal("CreateRecentTaskWithWorkingDir returned empty project path")
	}
	now := time.Now()
	state := &workflow.WorkflowState{
		ID:          workflow.GenerateID(projectSessionOwnerID(filepath.Clean(workingDir))),
		UserID:      projectSessionOwnerID(filepath.Clean(workingDir)),
		Type:        string(workflow.WorkflowCoding),
		ProjectPath: filepath.Clean(workingDir),
		Summary:     "build app",
		Phases: []workflow.Phase{{
			ID:     workflow.PhaseCodingRequirements,
			Name:   "Requirements",
			Status: workflow.PhaseRunning,
		}},
		CurrentPhase: 0,
		Status:       workflow.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := app.workflowV2.store.Save(state); err != nil {
		t.Fatalf("workflowV2 Save failed: %v", err)
	}

	recent := app.SearchProjects("Working dir workflow state", 10)
	if len(recent) != 1 {
		t.Fatalf("SearchProjects returned %d results: %+v", len(recent), recent)
	}
	if recent[0].ActiveWorkflow == nil || recent[0].ActiveWorkflow.ProjectPath != filepath.Clean(workingDir) || recent[0].ActiveWorkflow.Type != string(workflow.WorkflowCoding) {
		t.Fatalf("ActiveWorkflow = %#v, want execution-path coding workflow", recent[0].ActiveWorkflow)
	}

	scene, err := app.GetProjectScene(task.ProjectPath)
	if err != nil {
		t.Fatalf("GetProjectScene error = %v", err)
	}
	if scene.ActiveWorkflow == nil || scene.ActiveWorkflow.ProjectPath != filepath.Clean(workingDir) {
		t.Fatalf("scene ActiveWorkflow = %#v, want execution-path workflow", scene.ActiveWorkflow)
	}
}

func TestNonForkSourceTagDoesNotBorrowWorkflowState(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.workflowEngine = workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)

	source := app.CreateRecentTask("Workflow source owner")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty source project path")
	}
	if _, err := app.workflowEngine.StartWorkflowWithOptions(projectSessionOwnerID(source.ProjectPath), workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}, workflow.WorkflowStartOptions{ProjectPath: source.ProjectPath}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}

	other := app.createTaskRecord("Ordinary task with source tag", "ordinary output", []string{"source:" + source.ProjectPath}, false)
	if other.ProjectPath == "" {
		t.Fatal("createTaskRecord returned empty ordinary project path")
	}

	recent := app.SearchProjects("Ordinary task with source tag", 10)
	var ordinary *ProjectSearchResult
	for i := range recent {
		if recent[i].ProjectPath == other.ProjectPath {
			ordinary = &recent[i]
			break
		}
	}
	if ordinary == nil {
		t.Fatalf("SearchProjects results = %+v, want ordinary project %q", recent, other.ProjectPath)
	}
	if ordinary.ActiveWorkflow != nil {
		t.Fatalf("ordinary source-tagged task borrowed workflow state: %#v", ordinary.ActiveWorkflow)
	}
}

func TestNonForkSourceTagDoesNotCollapseRecentTaskLineage(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Ordinary source task")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty source project path")
	}
	other := app.createTaskRecord("Ordinary task with metadata source", "ordinary output", []string{"source:" + source.ProjectPath}, false)
	if other.ProjectPath == "" {
		t.Fatal("createTaskRecord returned empty ordinary project path")
	}

	recent := app.SearchProjects("", 10)
	if !searchResultsContainProjectPath(recent, source.ProjectPath) || !searchResultsContainProjectPath(recent, other.ProjectPath) {
		t.Fatalf("SearchProjects collapsed ordinary source tag lineage: %+v", recent)
	}
}

func TestForkRecentTaskRepeatedOpenRepairsMissingForkWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Repair missing fork workspace")
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}
	if err := os.RemoveAll(forked.ProjectPath); err != nil {
		t.Fatalf("RemoveAll fork workspace: %v", err)
	}

	reopened := app.ForkRecentTask(source.ProjectPath)
	if reopened.ProjectPath != forked.ProjectPath {
		t.Fatalf("reopened fork path = %q, want existing fork %q", reopened.ProjectPath, forked.ProjectPath)
	}
	if info, err := os.Stat(reopened.ProjectPath); err != nil || !info.IsDir() {
		t.Fatalf("reopened fork workspace not repaired, info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(reopened.ProjectPath, "task.md")); err != nil {
		t.Fatalf("reopened fork task.md missing: %v", err)
	}
}

func TestForkRecentTaskReopenAfterClosedTabReusesIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Reopen forked task")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	tabID := "proj-reopen-forked"
	_ = app.CreateProjectTabSession(tabID, forked.ProjectPath)
	app.CloseProjectTabSession(tabID)
	if got := app.LoadProjectTabIndex(); len(got) != 0 {
		t.Fatalf("LoadProjectTabIndex after close = %+v, want no restored tabs", got)
	}

	reopened := app.ForkRecentTask(source.ProjectPath)
	if reopened.ProjectPath != forked.ProjectPath {
		t.Fatalf("reopened fork path = %q, want existing fork %q", reopened.ProjectPath, forked.ProjectPath)
	}
	_ = app.CreateProjectTabSession(tabID, reopened.ProjectPath)
	got := app.LoadProjectTabIndex()
	if len(got) != 1 || got[0].ID != tabID || got[0].ProjectPath != forked.ProjectPath || got[0].Archived {
		t.Fatalf("LoadProjectTabIndex after reopen = %+v, want same fork tab active", got)
	}
	search := app.SearchProjects("Reopen forked task", 10)
	if len(search) != 1 || !searchResultsContainProjectPath(search, forked.ProjectPath) {
		t.Fatalf("search after reopen = %+v, want only existing fork", search)
	}
}

func TestForkRecentTaskConcurrentCallsReuseIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Concurrent task fork")
	const workers = 8
	results := make([]ProjectSearchResult, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = app.ForkRecentTask(source.ProjectPath)
		}(i)
	}
	close(start)
	wg.Wait()

	forks := map[string]bool{}
	for i, result := range results {
		if result.ProjectPath == "" {
			t.Fatalf("result[%d].ProjectPath empty", i)
		}
		if result.ProjectPath == source.ProjectPath {
			t.Fatalf("result[%d].ProjectPath = source %q", i, source.ProjectPath)
		}
		forks[result.ProjectPath] = true
	}
	if len(forks) != 1 {
		t.Fatalf("concurrent fork paths = %+v, want one reused independent fork", forks)
	}
	recent := app.SearchProjects("", 10)
	if len(recent) != 1 {
		t.Fatalf("task management after concurrent open = %+v, want one fork", recent)
	}
	for forkPath := range forks {
		if !searchResultsContainProjectPath(recent, forkPath) {
			t.Fatalf("task management after concurrent fork = %+v, missing fork %q", recent, forkPath)
		}
	}
}

func TestForkRecentTaskDoesNotReuseHiddenIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Hidden independent task")
	forked := app.ForkRecentTask(source.ProjectPath)
	app.HideTask(forked.ProjectPath)

	self := app.ForkRecentTask(forked.ProjectPath)
	if self.ProjectPath != "" {
		t.Fatalf("ForkRecentTask hidden fork path = %q, want empty", self.ProjectPath)
	}
}

func TestForkRecentTaskDoesNotCancelSourceProjectLoop(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Fork cancels running source task")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	mgr := NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, mgr)
	h := &IMMessageHandler{}
	client.imHandler = h
	mgr.SetHubClient(client)
	app.remoteSessions = mgr

	userID := "desktop-user:" + source.ProjectPath
	loop := NewLoopContext("project-loop", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()

	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	select {
	case <-loop.DoneC:
		t.Fatal("forking managed task cancelled source project loop")
	case <-time.After(50 * time.Millisecond):
	}
	loop.Cancel()
	select {
	case <-loop.DoneC:
	case <-time.After(2 * time.Second):
		t.Fatal("test cleanup did not stop source project loop")
	}
}

func TestForkRecentTaskRepeatedForkDoesNotCancelSourceProjectLoop(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Reuse fork cancels running source task")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	mgr := NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, mgr)
	h := &IMMessageHandler{}
	client.imHandler = h
	mgr.SetHubClient(client)
	app.remoteSessions = mgr

	userID := "desktop-user:" + forked.ProjectPath
	loop := NewLoopContext("project-loop", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()

	again := app.ForkRecentTask(forked.ProjectPath)
	if again.ProjectPath != forked.ProjectPath {
		t.Fatalf("second ForkRecentTask path = %q, want same existing fork %q", again.ProjectPath, forked.ProjectPath)
	}

	select {
	case <-loop.DoneC:
		t.Fatal("repeated fork cancelled source project loop")
	case <-time.After(50 * time.Millisecond):
	}
	loop.Cancel()
	select {
	case <-loop.DoneC:
	case <-time.After(2 * time.Second):
		t.Fatal("test cleanup did not stop source project loop")
	}
}

func TestLoadProjectTabIndexSkipsHiddenTasks(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Hidden restored tab task")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	_ = app.CreateProjectTabSession("proj-hidden", source.ProjectPath)
	if got := app.LoadProjectTabIndex(); len(got) != 1 || got[0].ProjectPath != source.ProjectPath {
		t.Fatalf("LoadProjectTabIndex before hide = %+v, want one active tab", got)
	}

	app.HideTask(source.ProjectPath)
	if got := app.LoadProjectTabIndex(); len(got) != 0 {
		t.Fatalf("LoadProjectTabIndex after hide = %+v, want hidden task skipped", got)
	}
}

func TestCreateProjectTabSessionReopensArchivedExistingSession(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Reopen closed managed task tab")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	tabID := "proj-reopen-existing"
	_ = app.CreateProjectTabSession(tabID, source.ProjectPath)
	app.CloseProjectTabSession(tabID)
	if got := app.LoadProjectTabIndex(); len(got) != 0 {
		t.Fatalf("LoadProjectTabIndex after close = %+v, want no active tabs", got)
	}

	_ = app.CreateProjectTabSession(tabID, source.ProjectPath)
	got := app.LoadProjectTabIndex()
	if len(got) != 1 || got[0].ID != tabID || got[0].ProjectPath != source.ProjectPath || got[0].Archived {
		t.Fatalf("LoadProjectTabIndex after reopen = %+v, want active reopened tab", got)
	}
	if got[0].Title != source.Name {
		t.Fatalf("restored tab title = %q, want task name %q", got[0].Title, source.Name)
	}
}

func TestProjectSessionOwnerIDNormalizesPathVariants(t *testing.T) {
	base := filepath.Clean(`D:\workprj\managed-task`)
	variants := []string{
		` D:\workprj\managed-task\ `,
		`d:/workprj/managed-task`,
		`D:/workprj/managed-task/.`,
	}
	want := projectSessionOwnerID(base)
	for _, variant := range variants {
		if got := projectSessionOwnerID(variant); got != want {
			t.Fatalf("projectSessionOwnerID(%q) = %q, want %q", variant, got, want)
		}
	}
}

func TestCreateProjectTabSessionNormalizesStoredProjectPath(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "task")
	variant := projectPath + string(filepath.Separator) + "."
	_ = app.CreateProjectTabSession("proj-normalized", variant)

	got := app.LoadProjectTabIndex()
	if len(got) != 1 {
		t.Fatalf("LoadProjectTabIndex = %+v, want one tab", got)
	}
	if got[0].ProjectPath != normalizeProjectSessionPath(projectPath) {
		t.Fatalf("stored project path = %q, want normalized %q", got[0].ProjectPath, normalizeProjectSessionPath(projectPath))
	}
}

func TestLoadProjectTabIndexRestoresRecentTaskDisplayName(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("北京天气")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	tabID := "proj-display-name"
	_ = app.CreateProjectTabSession(tabID, source.ProjectPath)
	got := app.LoadProjectTabIndex()
	if len(got) != 1 {
		t.Fatalf("LoadProjectTabIndex = %+v, want one active tab", got)
	}
	if got[0].Title != "北京天气" {
		t.Fatalf("restored tab title = %q, want task display name", got[0].Title)
	}
}

func TestCreateProjectTabSessionRejectsHiddenTask(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Reject hidden project tab create")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	app.HideTask(source.ProjectPath)

	msg := app.CreateProjectTabSession("proj-hidden-create", source.ProjectPath)
	if msg != "" {
		t.Fatalf("CreateProjectTabSession hidden task message = %q, want empty", msg)
	}
	if got := app.LoadProjectTabIndex(); len(got) != 0 {
		t.Fatalf("LoadProjectTabIndex after hidden create = %+v, want no active tabs", got)
	}
}

func TestCancelProjectTaskLoopForHandlerCancelsMatchingProjectSession(t *testing.T) {
	projectPath := "D:/tasks/running"
	userID := projectSessionOwnerID(projectPath)
	h := &IMMessageHandler{}
	loop := NewLoopContext("project-loop", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()

	if !cancelProjectTaskLoopForHandler(h, projectPath) {
		t.Fatal("cancelProjectTaskLoopForHandler returned false, want true for active project loop")
	}
	select {
	case <-loop.DoneC:
	case <-time.After(2 * time.Second):
		t.Fatal("project loop was not cancelled")
	}
	if !loop.IsCancelled() {
		t.Fatal("project loop CancelC was not closed")
	}
}

func TestCloseProjectTabSessionCancelsRunningProjectLoop(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Close running managed task tab")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	tabID := "proj-close-running"
	_ = app.CreateProjectTabSession(tabID, source.ProjectPath)
	mgr := NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, mgr)
	h := &IMMessageHandler{}
	client.imHandler = h
	mgr.SetHubClient(client)
	app.remoteSessions = mgr

	userID := "desktop-user:" + source.ProjectPath
	loop := NewLoopContext("project-loop", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()

	app.CloseProjectTabSession(tabID)

	select {
	case <-loop.DoneC:
	case <-time.After(2 * time.Second):
		t.Fatal("closing project tab did not cancel running project loop")
	}
	if _, ok := app.tabProjectPaths.Load(tabID); ok {
		t.Fatal("tab project path cache was not cleared")
	}

	index, err := app.ensureProjectTabSessionPersist().LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	for _, entry := range index.Tabs {
		if entry.ID == tabID {
			if !entry.Archived {
				t.Fatalf("tab index entry = %+v, want archived", entry)
			}
			return
		}
	}
	t.Fatalf("tab %q not found in index: %+v", tabID, index.Tabs)
}

func TestCloseProjectTabSessionCancelsCachedLoopWhenTabMissingFromIndex(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(app.GetDataDir(), "tasks", "cached-running")
	tabID := "proj-cached-running"
	app.tabProjectPaths.Store(tabID, projectPath)

	mgr := NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, mgr)
	h := &IMMessageHandler{}
	client.imHandler = h
	mgr.SetHubClient(client)
	app.remoteSessions = mgr

	userID := "desktop-user:" + projectPath
	loop := NewLoopContext("project-loop", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()

	app.CloseProjectTabSession(tabID)

	select {
	case <-loop.DoneC:
	case <-time.After(2 * time.Second):
		t.Fatal("closing cached project tab did not cancel running project loop")
	}
	if _, ok := app.tabProjectPaths.Load(tabID); ok {
		t.Fatal("tab project path cache was not cleared")
	}
}

func TestSendMessageForTabRejectsHiddenTask(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Reject hidden project tab send")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	tabID := "proj-hidden-send"
	_ = app.CreateProjectTabSession(tabID, source.ProjectPath)
	app.HideTask(source.ProjectPath)

	if _, err := app.SendMessageForTab(tabID, "continue", ""); err == nil || !strings.Contains(err.Error(), "project task is closed") {
		t.Fatalf("SendMessageForTab hidden task err = %v, want closed-task error", err)
	}
}

func TestSendAIAssistantMessageRejectsHiddenProjectPath(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Reject hidden direct project send")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}
	app.HideTask(source.ProjectPath)

	if _, err := app.SendAIAssistantMessage(AIAssistantSendRequest{Text: "continue", ProjectPath: source.ProjectPath}); err == nil || !strings.Contains(err.Error(), "project task is closed") {
		t.Fatalf("SendAIAssistantMessage hidden project err = %v, want closed-task error", err)
	}
}

func TestSendAIAssistantMessageRoutesRecentTaskToWorkingDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "task-worktree")
	source := app.CreateRecentTaskWithWorkingDir("Route direct project send", workingDir)
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTaskWithWorkingDir returned empty project path")
	}

	resp, err := app.SendAIAssistantMessage(AIAssistantSendRequest{
		Text:         "continue",
		ProjectPath:  source.ProjectPath,
		EventScopeID: "tab-direct-working-dir",
	})
	if err != nil {
		t.Fatalf("SendAIAssistantMessage err = %v", err)
	}
	if resp == nil || !resp.Deferred {
		t.Fatalf("SendAIAssistantMessage resp = %#v, want deferred response", resp)
	}
	if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
		t.Fatalf("workingDir was not prepared, info=%v err=%v", info, err)
	}

	taskSession := desktopAIAssistantUserIDForProjectPath(source.ProjectPath)
	scope, ok := app.sessionEventScopeIDs.Load(taskSession)
	if !ok || scope != "tab-direct-working-dir" {
		t.Fatalf("task session scope = %#v, %v; want tab-direct-working-dir", scope, ok)
	}
	workingDirSession := desktopAIAssistantUserIDForProjectPath(filepath.Clean(workingDir))
	if _, ok := app.sessionEventScopeIDs.Load(workingDirSession); ok {
		t.Fatalf("working dir session %q unexpectedly received event scope", workingDirSession)
	}
}

func TestSendMessageForTabRoutesRecentTaskToWorkingDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := filepath.Join(t.TempDir(), "task-worktree")
	source := app.CreateRecentTaskWithWorkingDir("Route tab project send", workingDir)
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTaskWithWorkingDir returned empty project path")
	}

	tabID := "proj-working-dir-send"
	if msg := app.CreateProjectTabSession(tabID, source.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("CreateProjectTabSession returned empty context message")
	}

	resp, err := app.SendMessageForTab(tabID, "continue", source.ProjectPath)
	if err != nil {
		t.Fatalf("SendMessageForTab err = %v", err)
	}
	if resp == nil || !resp.Deferred {
		t.Fatalf("SendMessageForTab resp = %#v, want deferred response", resp)
	}
	if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
		t.Fatalf("workingDir was not prepared, info=%v err=%v", info, err)
	}

	taskSession := desktopAIAssistantUserIDForProjectPath(source.ProjectPath)
	scope, ok := app.sessionEventScopeIDs.Load(taskSession)
	if !ok || scope != tabID {
		t.Fatalf("task session scope = %#v, %v; want %q", scope, ok, tabID)
	}
	workingDirSession := desktopAIAssistantUserIDForProjectPath(filepath.Clean(workingDir))
	if _, ok := app.sessionEventScopeIDs.Load(workingDirSession); ok {
		t.Fatalf("working dir session %q unexpectedly received event scope", workingDirSession)
	}
}

func TestCreateRecentTaskRejectsGenericCommandName(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Review/Fix/Optimize")
	if created.ID != "" || created.Name != "" || created.ProjectPath != "" || created.HasOutput {
		t.Fatalf("generic CreateRecentTask = %#v, want zero result", created)
	}
	if got := app.SearchProjects("", 10); len(got) != 0 {
		t.Fatalf("SearchProjects returned %d records for generic task", len(got))
	}
}

func TestCreateRecentTaskIgnoresBlankName(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask(" \t\n ")
	if created.ID != "" || created.Name != "" || created.ProjectPath != "" || len(created.Tags) != 0 {
		t.Fatalf("blank CreateRecentTask = %#v, want zero result", created)
	}
	if got := app.SearchProjects("", 10); len(got) != 0 {
		t.Fatalf("SearchProjects returned %d records for blank task", len(got))
	}
}

func TestRecentTaskSlug(t *testing.T) {
	tests := map[string]string{
		"Hello World!":             "hello-world",
		"---Alpha_Beta---":         "alpha_beta",
		"\u4e2d\u6587\u4efb\u52a1": "\u4e2d\u6587\u4efb\u52a1", // Chinese letters kept
		"\u6280\u672f\u670d\u52a1 2026": "\u6280\u672f\u670d\u52a1-2026",
		"  A   B   C  ":            "a-b-c",
		"!!!":                      "task",
		"CON":                      "con-task", // Windows reserved device name
		"com1":                     "com1-task",
		"01234567890123456789012345678901234567890123456789": "0123456789012345678901234567890123456789",
	}
	for input, want := range tests {
		if got := recentTaskSlug(input); got != want {
			t.Fatalf("recentTaskSlug(%q) = %q, want %q", input, got, want)
		}
	}
	// Rune cap (not byte): 50 CJK letters → 40-rune slug.
	longCJK := strings.Repeat("\u6d4b", 50)
	if got := recentTaskSlug(longCJK); len([]rune(got)) != 40 {
		t.Fatalf("recentTaskSlug long CJK runes = %d, want 40 (got %q)", len([]rune(got)), got)
	}
}

func TestCreateTaskSeedsWorkspaceAndChineseSlug(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store nil")
	}

	created := app.CreateTask("技术服务响应分析", "")
	if created.ProjectPath == "" {
		t.Fatal("CreateTask returned empty project path")
	}
	base := filepath.Base(created.ProjectPath)
	if !strings.HasPrefix(base, "技术服务响应分析-") {
		t.Fatalf("expected Chinese slug prefix, got base=%q path=%q", base, created.ProjectPath)
	}
	ws := filepath.Join(created.ProjectPath, "workspace")
	if info, err := os.Stat(ws); err != nil || !info.IsDir() {
		t.Fatalf("workspace dir missing: %v", err)
	}
	readme := filepath.Join(ws, "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("workspace README missing: %v", err)
	}
	if !strings.Contains(string(data), "默认执行目录") {
		t.Fatalf("README content unexpected: %s", data)
	}
	// Execution path for managed task with no working_dir tag is workspace/.
	if got := app.recentTaskExecutionProjectPath(created.ProjectPath); got != ws {
		t.Fatalf("execution path = %q, want %q", got, ws)
	}
}

func TestCreateTaskWithWorkingDirSkipsSandboxReadme(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	custom := t.TempDir()
	created := app.CreateTask("WithDir", custom)
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	wsReadme := filepath.Join(created.ProjectPath, "workspace", "README.md")
	if _, err := os.Stat(wsReadme); !os.IsNotExist(err) {
		t.Fatalf("sandbox README should not be written when custom working dir is set; err=%v", err)
	}
	if got := app.recentTaskExecutionProjectPath(created.ProjectPath); filepath.Clean(got) != filepath.Clean(custom) {
		t.Fatalf("execution path = %q, want custom %q", got, custom)
	}
}

func TestNormalizeRecentTaskName(t *testing.T) {
	if got := normalizeRecentTaskName("  Alpha\n\tBeta   Gamma  "); got != "Alpha Beta Gamma" {
		t.Fatalf("normalizeRecentTaskName whitespace = %q", got)
	}
	long := strings.Repeat("\u6d4b", 130)
	if got := normalizeRecentTaskName(long); len([]rune(got)) != 120 {
		t.Fatalf("normalizeRecentTaskName length = %d, want 120", len([]rune(got)))
	}
}
