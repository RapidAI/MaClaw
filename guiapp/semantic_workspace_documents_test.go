package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangedWorkspaceDocumentsSeesNewPDFOnly(t *testing.T) {
	root := t.TempDir()
	before := snapshotWorkspaceDocuments(root)
	if err := os.WriteFile(filepath.Join(root, "notes.py"), []byte("print(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(root, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := changedWorkspaceDocuments(before, snapshotWorkspaceDocuments(root))
	if len(got) != 1 || got[0] != pdf {
		t.Fatalf("changed = %v, want %s", got, pdf)
	}
}

func TestTaskWorkspaceDeliversUnsentPDFNotHostCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "album.pdf")
	already := filepath.Join(root, "text.pdf")
	copy := filepath.Join(root, "text_162533_939.pdf")
	deck := filepath.Join(root, "deck.pptx")
	if err := os.WriteFile(real, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(already, []byte("%PDF-text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copy, []byte("%PDF-text-copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deck, []byte("pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
		semanticDeliveryFileData: "already-registered",
		deliveredPaths:           []string{real},
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != real {
		t.Fatalf("delivered = %v, want only %s", cb.deliveredPaths, real)
	}
	cb.deliveredPaths = nil
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 0 {
		t.Fatalf("ledger repeated delivery: %v", cb.deliveredPaths)
	}
}

func TestProducedPDFOutranksHostCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{workspaceDocRoot: root, workspaceDocBaseline: snapshotWorkspaceDocuments(root)}
	real := filepath.Join(root, "album.pdf")
	copy := filepath.Join(root, "album_162533_939.pdf")
	if err := os.WriteFile(copy, []byte("%PDF-text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != real {
		t.Fatalf("delivered = %v, want only %s", cb.deliveredPaths, real)
	}
}

func TestTaskIdentityReadsWorkspaceChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data", "tasks", "job")
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(ws, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
		semanticDeliveryFileData: "registered",
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != pdf {
		t.Fatalf("delivered = %v, want %s", cb.deliveredPaths, pdf)
	}
}

func TestProjectWorkspaceDoesNotDumpOldPDFs(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "manual.pdf")
	if err := os.WriteFile(old, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
		semanticDeliveryFileData: "weather-card",
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 0 {
		t.Fatalf("project pdf was attached without being produced this turn: %v", cb.deliveredPaths)
	}
}
