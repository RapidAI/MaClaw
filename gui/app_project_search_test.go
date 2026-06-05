package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
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

	foregroundAgentLastDoneUnixNano.Store(time.Now().UnixNano())
	app.triggerMemoryPipelineSoon(1 * time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	_, lastRun, _ := app.memPipeline.Status()
	if !lastRun.IsZero() {
		t.Fatal("memory pipeline ran during foreground quiet period")
	}

	foregroundAgentLastDoneUnixNano.Store(time.Now().Add(-foregroundAgentBackgroundQuietPeriod).UnixNano())
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
		t.Fatalf("HasOutput = false, want true for manual recent task")
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
		Title:      "Improve Recent Tasks Filtering",
		Content:    "Task: recent tasks\nResult: Added has_output filtering",
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
	if results[0].Name != "Improve Recent Tasks Filtering" || !results[0].HasOutput {
		t.Fatalf("result = %+v, want output-backed task", results[0])
	}
}

func TestCreateRecentTaskUsesTaskNamePreview(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Draft recent task filtering implementation")
	if created.Name != "Draft recent task filtering implementation" {
		t.Fatalf("Name = %q, want task name", created.Name)
	}
	if created.Preview == "Manual task placeholder." {
		t.Fatalf("Preview = %q, want a user-facing task preview", created.Preview)
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

	source := app.CreateRecentTask("Draft recent task filtering implementation")
	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}
	if forked.Name != source.Name {
		t.Fatalf("ForkRecentTask Name = %q, want %q", forked.Name, source.Name)
	}
	content, err := os.ReadFile(filepath.Join(forked.ProjectPath, "task.md"))
	if err != nil {
		t.Fatalf("ReadFile(fork task.md): %v", err)
	}
	if !strings.Contains(string(content), "Forked from recent task") || !strings.Contains(string(content), source.ProjectPath) {
		t.Fatalf("fork task content = %q, want source metadata", content)
	}
	recent := app.SearchProjects("", 10)
	if len(recent) != 1 || !searchResultsContainProjectPath(recent, forked.ProjectPath) {
		t.Fatalf("recent tasks after fork = %+v, want only independent fork", recent)
	}
	search := app.SearchProjects("Draft recent task", 10)
	if len(search) != 1 || !searchResultsContainProjectPath(search, forked.ProjectPath) {
		t.Fatalf("search after fork = %+v, want only independent fork", search)
	}
}

func TestForkRecentTaskRepeatedOpenReusesIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Draft recent task filtering implementation")
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
	recent := app.SearchProjects("Draft recent task", 10)
	if len(recent) != 1 || !searchResultsContainProjectPath(recent, forked.ProjectPath) {
		t.Fatalf("recent tasks after repeated opens = %+v, want one independent fork", recent)
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

	source := app.CreateRecentTask("Reopen forked recent task")
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
	search := app.SearchProjects("Reopen forked recent task", 10)
	if len(search) != 1 || !searchResultsContainProjectPath(search, forked.ProjectPath) {
		t.Fatalf("search after reopen = %+v, want only existing fork", search)
	}
}

func TestForkRecentTaskConcurrentCallsReuseIndependentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	source := app.CreateRecentTask("Concurrent recent task fork")
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
		t.Fatalf("recent tasks after concurrent open = %+v, want one fork", recent)
	}
	for forkPath := range forks {
		if !searchResultsContainProjectPath(recent, forkPath) {
			t.Fatalf("recent tasks after concurrent fork = %+v, missing fork %q", recent, forkPath)
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
		t.Fatal("forking recent task cancelled source project loop")
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
	source := app.CreateRecentTask("Reopen closed recent task tab")
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
		t.Fatalf("restored tab title = %q, want recent task name %q", got[0].Title, source.Name)
	}
}

func TestProjectSessionOwnerIDNormalizesPathVariants(t *testing.T) {
	base := filepath.Clean(`D:\workprj\recent-task`)
	variants := []string{
		` D:\workprj\recent-task\ `,
		`d:/workprj/recent-task`,
		`D:/workprj/recent-task/.`,
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
		t.Fatalf("restored tab title = %q, want recent task display name", got[0].Title)
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
	source := app.CreateRecentTask("Close running recent task tab")
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
		"\u4e2d\u6587\u4efb\u52a1": "task",
		"  A   B   C  ":            "a-b-c",
		"01234567890123456789012345678901234567890123456789": "0123456789012345678901234567890123456789",
	}
	for input, want := range tests {
		if got := recentTaskSlug(input); got != want {
			t.Fatalf("recentTaskSlug(%q) = %q, want %q", input, got, want)
		}
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
