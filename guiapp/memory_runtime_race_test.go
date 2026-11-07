package guiapp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryStoreSwitchConcurrentWithPromptBuild(t *testing.T) {
	storeA, err := memory.NewStore(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Stop()
	storeB, err := memory.NewStore(filepath.Join(t.TempDir(), "b.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Stop()
	app := &App{testHomeDir: t.TempDir()}
	handler := &IMMessageHandler{app: app, memoryStore: storeA}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			var prompt strings.Builder
			handler.appendMemorySection(&prompt, false, desktopUserID, lifecycle.EventContext{}, "hello")
			_ = handler.sessionStartMemoryExtractor()
		}
	}()
	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			handler.SetMemoryStore(storeB)
		} else {
			handler.SetMemoryStore(storeA)
		}
	}
	<-done
}

func TestAppMemoryResetConcurrentWithSnapshotRefresh(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	app.installMemoryRuntime(store, nil, "old")
	handler := &IMMessageHandler{app: app, memoryStore: store}
	app.imHandler = handler

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			app.refreshActiveAgentMemorySnapshots()
		}
	}()
	app.resetPathBoundStateForDataDirChange()
	<-done
}

func TestAppMemoryResetConcurrentWithManagementList(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "manage.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	app.installMemoryRuntime(store, nil, "old")
	app.ensureInteractionInfra()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = app.ListMemories("", "")
		}
	}()
	app.resetPathBoundStateForDataDirChange()
	<-done
}
