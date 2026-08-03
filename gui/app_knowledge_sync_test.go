package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestKnowledgeSyncConflictsIncludeDisabledLocalSources(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	active, err := store.SaveText(context.Background(), knowledge.TextSaveRequest{
		Text: "sync conflict active body", Title: "Active Note", OwnerID: "user-a", TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("save active: %v", err)
	}
	disabled, err := store.SaveText(context.Background(), knowledge.TextSaveRequest{
		Text: "sync conflict disabled body", Title: "Disabled Note", OwnerID: "user-a", TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if _, err := store.DisableSource(context.Background(), disabled.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	conflicts, err := app.knowledgeSyncConflicts(context.Background(), store, []guiKnowledgePackageSource{
		{ID: active.ID, Title: "Active Note"},
		{Title: "Disabled Note"},
		{Title: "No Local Match"},
	})
	if err != nil {
		t.Fatalf("knowledgeSyncConflicts: %v", err)
	}
	matched := map[string]string{}
	for _, conflict := range conflicts {
		matched[conflict.Title] = conflict.LocalID
	}
	if matched["Active Note"] != active.ID {
		t.Fatalf("active source conflict not detected: %#v", conflicts)
	}
	if matched["Disabled Note"] != disabled.ID {
		t.Fatalf("disabled local source must participate in conflict detection: %#v", conflicts)
	}
	if _, ok := matched["No Local Match"]; ok {
		t.Fatalf("unexpected conflict for unmatched remote source: %#v", conflicts)
	}
}

func TestKnowledgeSyncPasswordVerifier(t *testing.T) {
	verifier, err := encryptKnowledgeSyncPasswordVerifier("sync-secret")
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	if len(verifier) == 0 || verifier["ciphertext"] == "" {
		t.Fatalf("verifier missing ciphertext: %+v", verifier)
	}
	if err := decryptKnowledgeSyncPasswordVerifier("sync-secret", verifier); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if err := decryptKnowledgeSyncPasswordVerifier("wrong-secret", verifier); err == nil {
		t.Fatal("wrong password accepted")
	}
}
