package guiapp

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSQLiteCommitApprovalStoreSurvivesReopenAndSettlesAtomically(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "commit-approvals.db")
	store, err := newSQLiteCommitApprovalStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	note := commitAskNote{TaskKey: "desktop\nuser\nroot", Fingerprint: "mailbox_send\nto\noutside", Nonce: "nonce-1"}
	if err := store.prepare(note); err != nil {
		t.Fatal(err)
	}
	forged := note
	forged.Nonce = "forged"
	if err := store.settle(forged, agent.CommitAllowTask); err == nil {
		t.Fatal("forged ticket settled")
	}
	if err := store.validate(note); err != nil {
		t.Fatalf("forged settlement consumed the real ticket: %v", err)
	}
	reopened, err := newSQLiteCommitApprovalStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.closeLocked()
	if err := reopened.validate(note); err != nil {
		t.Fatalf("pending ask did not survive reopen: %v", err)
	}
	if err := reopened.settle(note, agent.CommitAllowTask); err != nil {
		t.Fatal(err)
	}
	if err := reopened.validate(note); err == nil {
		t.Fatal("settled ticket remained pending")
	}
	decision, found, err := reopened.lookup(note.TaskKey, note.Fingerprint)
	if err != nil || !found || decision != agent.CommitAllowTask {
		t.Fatalf("decision=%q found=%v err=%v", decision, found, err)
	}
	if err := reopened.settle(note, agent.CommitDeny); err == nil {
		t.Fatal("same ticket settled twice")
	}
	decision, _, _ = reopened.lookup(note.TaskKey, note.Fingerprint)
	if decision != agent.CommitAllowTask {
		t.Fatalf("replayed ticket changed decision to %q", decision)
	}
	if err := reopened.storeDecision(note.TaskKey, note.Fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := reopened.lookup(note.TaskKey, note.Fingerprint); found {
		t.Fatal("once decision was not consumed")
	}
}
