package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestForkRecentTaskReturnsAfterConversationCopy(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Forked history is available immediately")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr

	handler.memory.Save(projectSessionOwnerID(source.ProjectPath), []agent.ConversationEntry{
		{Role: "user", Content: "show my previous conversation"},
		{Role: "assistant", Content: "Here is the restored answer."},
	})

	forked := app.ForkRecentTask(source.ProjectPath)
	if forked.ProjectPath == "" || forked.ProjectPath == source.ProjectPath {
		t.Fatalf("ForkRecentTask ProjectPath = %q, source = %q", forked.ProjectPath, source.ProjectPath)
	}

	history := app.LoadProjectConversationHistory(forked.ProjectPath)
	if len(history) != 2 {
		t.Fatalf("LoadProjectConversationHistory(%q) len = %d, want 2", forked.ProjectPath, len(history))
	}
	if history[0].Role != "user" || history[0].Content != "show my previous conversation" {
		t.Fatalf("history[0] = %+v, want copied user message", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "Here is the restored answer." {
		t.Fatalf("history[1] = %+v, want copied assistant message", history[1])
	}
}

func TestSaveCurrentChatAsTaskCopiesMainConversation(t *testing.T) {
	app := newProjectSearchTestApp(t)
	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr

	handler.memory.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: "Generate a Beijing celebration PPT"},
		{Role: "assistant", Content: "I will create the deck."},
	})

	saved := app.SaveCurrentChatAsTask("Beijing celebration PPT")
	if saved.ProjectPath == "" {
		t.Fatalf("SaveCurrentChatAsTask returned empty project path: %+v", saved)
	}
	if len(app.ListTasks(20)) != 1 {
		t.Fatalf("ListTasks did not include exactly the saved task")
	}
	history := app.LoadProjectConversationHistory(saved.ProjectPath)
	if len(history) != 2 {
		t.Fatalf("LoadProjectConversationHistory(%q) len = %d, want 2", saved.ProjectPath, len(history))
	}
	if history[0].Content != "Generate a Beijing celebration PPT" || history[1].Content != "I will create the deck." {
		t.Fatalf("history = %+v, want saved main conversation", history)
	}
}

func TestLoadProjectConversationHistoryFallsBackToPersistedTabSession(t *testing.T) {
	app := newProjectSearchTestApp(t)
	saved := app.CreateRecentTask("Restore durable tab history")
	if saved.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned an empty project path")
	}

	// Do not seed the project-scoped in-memory conversation. This matches a
	// restarted app, where the history task still has its durable tab snapshot.
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:       "history-tab",
		ProjectPath: saved.ProjectPath,
		Conversation: []interface{}{
			map[string]interface{}{"id": "old-user", "role": "user", "content": "Please reopen my saved task"},
			map[string]interface{}{"id": "old-assistant", "role": "assistant", "content": "Here is the historical answer", "reasoning": "Recovered from tab storage"},
		},
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	history := app.LoadProjectConversationHistory(saved.ProjectPath)
	if len(history) != 2 {
		t.Fatalf("LoadProjectConversationHistory len = %d, want 2: %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "Please reopen my saved task" {
		t.Fatalf("history[0] = %+v, want persisted user turn", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "Here is the historical answer" || history[1].ReasoningContent != "Recovered from tab storage" {
		t.Fatalf("history[1] = %+v, want persisted assistant turn", history[1])
	}
}

func TestClearProjectHistoryAlsoClearsPersistedTabSnapshots(t *testing.T) {
	app := newProjectSearchTestApp(t)
	saved := app.CreateRecentTask("Clear durable tab history")
	if saved.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned an empty project path")
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:       "clear-history-tab",
		ProjectPath: saved.ProjectPath,
		Conversation: []interface{}{
			map[string]interface{}{"role": "user", "content": "history that must remain cleared"},
			map[string]interface{}{"role": "assistant", "content": "old answer that must remain cleared"},
		},
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	if err := app.ClearAIAssistantHistoryForSession(projectSessionOwnerID(saved.ProjectPath)); err != nil {
		t.Fatalf("ClearAIAssistantHistoryForSession: %v", err)
	}
	if history := app.LoadProjectConversationHistory(saved.ProjectPath); len(history) != 0 {
		t.Fatalf("history after clear = %+v, want no restored turns", history)
	}
	if session, err := persist.LoadSession("clear-history-tab"); err != nil {
		t.Fatal(err)
	} else if session == nil || len(session.Conversation) != 0 {
		t.Fatalf("persisted session after clear = %+v, want empty conversation", session)
	}
}

func TestSaveProjectTabConversationEmptyDoesNotCreateUnknownSession(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.SaveProjectTabConversation("unknown-tab", []interface{}{})
	if session, err := app.ensureProjectTabSessionPersist().LoadSession("unknown-tab"); err != nil {
		t.Fatal(err)
	} else if session != nil {
		t.Fatalf("empty save created an unknown session: %+v", session)
	}
}

func TestClearProjectHistoryPreventsStaleTabSnapshotSave(t *testing.T) {
	app := newProjectSearchTestApp(t)
	saved := app.CreateRecentTask("Fence stale tab save")
	if saved.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned an empty project path")
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        "stale-save-tab",
		ProjectPath:  saved.ProjectPath,
		Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "old durable turn"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Hold the same lock used by ordinary tab saves. The clear must wait for
	// this stale write and then clear its result, rather than allowing a late
	// debounced snapshot to resurrect the old task transcript.
	app.tabWorkingDirMu.Lock()
	clearDone := make(chan error, 1)
	go func() {
		clearDone <- app.ClearAIAssistantHistoryForSession(projectSessionOwnerID(saved.ProjectPath))
	}()
	// The session-store write models the deferred save that has already reached
	// persistence while the app-level lifecycle lock is held by its caller.
	// The clear queued behind that lock must still erase this newer snapshot.
	if err := persist.SaveSession(&TabSessionData{
		TabID:       "stale-save-tab",
		ProjectPath: saved.ProjectPath,
		Conversation: []interface{}{
			map[string]interface{}{"role": "user", "content": "stale deferred turn"},
		},
	}); err != nil {
		app.tabWorkingDirMu.Unlock()
		t.Fatalf("SaveSession: %v", err)
	}
	app.tabWorkingDirMu.Unlock()
	if err := <-clearDone; err != nil {
		t.Fatalf("ClearAIAssistantHistoryForSession: %v", err)
	}

	if history := app.LoadProjectConversationHistory(saved.ProjectPath); len(history) != 0 {
		t.Fatalf("history after clear race = %+v, want no restored turns", history)
	}
}

func TestSaveProjectTabConversationRejectsSnapshotOlderThanClearFence(t *testing.T) {
	app := newProjectSearchTestApp(t)
	saved := app.CreateRecentTask("Reject stale snapshot")
	if saved.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned an empty project path")
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        "clear-fence-tab",
		ProjectPath:  saved.ProjectPath,
		Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "old", "timestamp": time.Now().Add(-time.Hour).UnixMilli()}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearAIAssistantHistoryForSession(projectSessionOwnerID(saved.ProjectPath)); err != nil {
		t.Fatal(err)
	}
	session, err := persist.LoadSession("clear-fence-tab")
	if err != nil || session == nil || session.ConversationClearedAt == 0 {
		t.Fatalf("cleared session = %+v err=%v, want clear fence", session, err)
	}

	app.SaveProjectTabConversation("clear-fence-tab", []interface{}{
		map[string]interface{}{"role": "user", "content": "stale", "timestamp": session.ConversationClearedAt - 1},
	})
	if session, err = persist.LoadSession("clear-fence-tab"); err != nil || len(session.Conversation) != 0 {
		t.Fatalf("stale save restored conversation: session=%+v err=%v", session, err)
	}

	app.SaveProjectTabConversation("clear-fence-tab", []interface{}{
		map[string]interface{}{"role": "user", "content": "new", "timestamp": session.ConversationClearedAt + 1},
	})
	if session, err = persist.LoadSession("clear-fence-tab"); err != nil || len(session.Conversation) != 1 {
		t.Fatalf("new save did not persist: session=%+v err=%v", session, err)
	}
}

func TestClearProjectHistoryWritesFenceForAlreadyEmptySnapshot(t *testing.T) {
	app := newProjectSearchTestApp(t)
	saved := app.CreateRecentTask("Fence an already empty snapshot")
	if saved.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned an empty project path")
	}
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        "empty-clear-fence-tab",
		ProjectPath:  saved.ProjectPath,
		Conversation: []interface{}{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearAIAssistantHistoryForSession(projectSessionOwnerID(saved.ProjectPath)); err != nil {
		t.Fatal(err)
	}
	session, err := persist.LoadSession("empty-clear-fence-tab")
	if err != nil || session == nil || session.ConversationClearedAt == 0 {
		t.Fatalf("cleared empty session = %+v err=%v, want clear fence", session, err)
	}
	app.SaveProjectTabConversation("empty-clear-fence-tab", []interface{}{
		map[string]interface{}{"role": "user", "content": "stale", "timestamp": session.ConversationClearedAt - 1},
	})
	if session, err = persist.LoadSession("empty-clear-fence-tab"); err != nil || len(session.Conversation) != 0 {
		t.Fatalf("stale save restored empty-cleared conversation: session=%+v err=%v", session, err)
	}
}

func TestSaveCurrentChatAsTaskPersistsCurrentWorkingDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := t.TempDir()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{{Id: "ppt", Name: "PPT workspace", Path: workingDir}}
	cfg.CurrentProject = "ppt"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr
	handler.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: "Continue this PPT work later"}})

	saved := app.SaveCurrentChatAsTask("Continue PPT workspace")
	if saved.ProjectPath == "" {
		t.Fatalf("SaveCurrentChatAsTask returned empty project path: %+v", saved)
	}
	wantDir := filepath.Clean(workingDir)
	if saved.WorkingDir != wantDir {
		t.Fatalf("saved.WorkingDir = %q, want %q", saved.WorkingDir, wantDir)
	}
	if got := app.recentTaskWorkingDir(saved.ProjectPath); got != wantDir {
		t.Fatalf("recentTaskWorkingDir = %q, want %q", got, wantDir)
	}
	if got := app.recentTaskExecutionProjectPath(saved.ProjectPath); got != wantDir {
		t.Fatalf("recentTaskExecutionProjectPath = %q, want %q", got, wantDir)
	}
	executionEntries := handler.memory.Load(projectSessionOwnerID(wantDir))
	if len(executionEntries) != 0 {
		t.Fatalf("execution project conversation = %+v, want no task history stored under shared working directory", executionEntries)
	}
	taskEntries := handler.memory.Load(projectSessionOwnerID(saved.ProjectPath))
	handler.memory.Save(projectSessionOwnerID(saved.ProjectPath), append(taskEntries, agent.ConversationEntry{Role: "assistant", Content: "Continuing from the restored PPT workspace."}))
	history := app.LoadProjectConversationHistory(saved.ProjectPath)
	if len(history) != 2 {
		t.Fatalf("LoadProjectConversationHistory(%q) len = %d, want 2 from isolated task session", saved.ProjectPath, len(history))
	}
	if history[1].Content != "Continuing from the restored PPT workspace." {
		t.Fatalf("history[1] = %+v, want task-session continuation", history[1])
	}
}

func TestSavedTasksSharingWorkingDirectoryKeepSeparateConversations(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := t.TempDir()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{{Id: "shared", Name: "Shared workspace", Path: workingDir}}
	cfg.CurrentProject = "shared"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr

	handler.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: "First task context"}})
	first := app.SaveCurrentChatAsTask("First shared task")
	handler.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: "Second task context"}})
	second := app.SaveCurrentChatAsTask("Second shared task")
	if first.ProjectPath == "" || second.ProjectPath == "" || first.ProjectPath == second.ProjectPath {
		t.Fatalf("saved task paths first=%q second=%q", first.ProjectPath, second.ProjectPath)
	}
	if first.WorkingDir != filepath.Clean(workingDir) || second.WorkingDir != filepath.Clean(workingDir) {
		t.Fatalf("working dirs first=%q second=%q want %q", first.WorkingDir, second.WorkingDir, filepath.Clean(workingDir))
	}

	firstHistory := app.LoadProjectConversationHistory(first.ProjectPath)
	secondHistory := app.LoadProjectConversationHistory(second.ProjectPath)
	if len(firstHistory) != 1 || firstHistory[0].Content != "First task context" {
		t.Fatalf("first history = %+v, want first task context only", firstHistory)
	}
	if len(secondHistory) != 1 || secondHistory[0].Content != "Second task context" {
		t.Fatalf("second history = %+v, want second task context only", secondHistory)
	}
	if entries := handler.memory.Load(projectSessionOwnerID(filepath.Clean(workingDir))); len(entries) != 0 {
		t.Fatalf("shared working directory session has task history: %+v", entries)
	}
}

func TestForkRecentTaskConcurrentCallsReturnCopiedConversation(t *testing.T) {
	app := newProjectSearchTestApp(t)
	source := app.CreateRecentTask("Concurrent fork returns copied conversation")
	if source.ProjectPath == "" {
		t.Fatal("CreateRecentTask returned empty project path")
	}

	mgr := NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, mgr)
	handler := &IMMessageHandler{memory: app.ensureConversationMemory()}
	hubClient.imHandler = handler
	mgr.SetHubClient(hubClient)
	app.remoteSessions = mgr

	handler.memory.Save(projectSessionOwnerID(source.ProjectPath), []agent.ConversationEntry{
		{Role: "user", Content: "copied under concurrency"},
		{Role: "assistant", Content: "concurrent answer"},
	})

	const workers = 6
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

	forkPath := ""
	for i, result := range results {
		if result.ProjectPath == "" {
			t.Fatalf("result[%d].ProjectPath empty", i)
		}
		if forkPath == "" {
			forkPath = result.ProjectPath
			continue
		}
		if result.ProjectPath != forkPath {
			t.Fatalf("result[%d].ProjectPath = %q, want reused fork %q", i, result.ProjectPath, forkPath)
		}
	}

	history := app.LoadProjectConversationHistory(forkPath)
	if len(history) != 2 {
		t.Fatalf("LoadProjectConversationHistory(%q) len = %d, want 2", forkPath, len(history))
	}
	if history[0].Content != "copied under concurrency" || history[1].Content != "concurrent answer" {
		t.Fatalf("history = %+v, want copied conversation", history)
	}
}
