package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStoreCloseConcurrentWithReads(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	store, release := app.acquireSessionStore()
	if store == nil {
		t.Fatal("session store did not initialize")
	}
	release()
	for round := 0; round < 5; round++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 20; i++ {
				_, _ = app.ListSessionHistory(10)
			}
		}()
		app.closeSessionStore()
		<-done
	}
	if _, err := app.ListSessionHistory(10); err != nil {
		t.Fatalf("session store did not recover after close: %v", err)
	}
	app.closeSessionStore()
}

func TestNilAppSessionStoreIsSafe(t *testing.T) {
	var app *App
	store, release := app.acquireSessionStore()
	defer release()
	if store != nil {
		t.Fatal("nil app returned a session store")
	}
	app.closeSessionStore()
}

func TestSessionStoreInitializationRetriesAfterFailure(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(home, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: home}
	store, release := app.acquireSessionStore()
	release()
	if store != nil {
		t.Fatal("initialization unexpectedly succeeded with an invalid home path")
	}
	if err := os.Remove(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	store, release = app.acquireSessionStore()
	if store == nil {
		release()
		t.Fatal("initialization did not retry after the path became valid")
	}
	release()
	app.closeSessionStore()
}
