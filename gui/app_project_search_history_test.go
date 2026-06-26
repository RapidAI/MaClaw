package main

import (
	"sync"
	"testing"

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
