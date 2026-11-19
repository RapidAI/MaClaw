package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDriftTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return p
}

func TestCollectFileDriftAuditClassifiesWrites(t *testing.T) {
	project := t.TempDir()
	agentFile := writeDriftTestFile(t, project, "agent.go", "v1")
	readFile := writeDriftTestFile(t, project, "read.go", "v1")
	stableFile := writeDriftTestFile(t, project, "stable.go", "v1")

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackReadFile(agentFile)
	cb.trackReadFile(readFile)
	cb.trackReadFile(stableFile)

	// Agent writes agent.go through the tracked file-tool path.
	if err := os.WriteFile(agentFile, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.trackFile(agentFile)
	cb.refreshFileSnapshot(agentFile)

	// Then drift: agent.go changed again after the tracked write (a later
	// command or external actor), and read.go moved under read-only evidence.
	if err := os.WriteFile(agentFile, []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readFile, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	afterWrite, readOnly := cb.collectFileDriftAudit()
	if len(afterWrite) != 1 || !strings.Contains(afterWrite[0], "agent.go") {
		t.Fatalf("afterWrite = %v, want [agent.go]", afterWrite)
	}
	if len(readOnly) != 1 || !strings.Contains(readOnly[0], "read.go") {
		t.Fatalf("readOnly = %v, want [read.go]", readOnly)
	}
}

func TestCollectFileDriftAuditCountsDeletionAsDrift(t *testing.T) {
	project := t.TempDir()
	gone := writeDriftTestFile(t, project, "gone.go", "v1")
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackReadFile(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	_, readOnly := cb.collectFileDriftAudit()
	if len(readOnly) != 1 {
		t.Fatalf("deleted read file must be drift, got readOnly=%v", readOnly)
	}
}

func TestCollectFileDriftAuditCleanWorkspace(t *testing.T) {
	project := t.TempDir()
	stable := writeDriftTestFile(t, project, "stable.go", "v1")
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: project}}
	cb.trackReadFile(stable)
	afterWrite, readOnly := cb.collectFileDriftAudit()
	if len(afterWrite) != 0 || len(readOnly) != 0 {
		t.Fatalf("clean workspace must report no drift, got %v / %v", afterWrite, readOnly)
	}
}

func TestSnapshotCodingFileSizeMatchesReadBytes(t *testing.T) {
	dir := t.TempDir()
	p := writeDriftTestFile(t, dir, "sized.bin", "hello 世界")
	snap, err := snapshotCodingFile(p)
	if err != nil {
		t.Fatalf("snapshotCodingFile: %v", err)
	}
	data, _ := os.ReadFile(p)
	if snap.Size != int64(len(data)) {
		t.Fatalf("Size = %d, want %d (bytes actually read)", snap.Size, len(data))
	}
	if snap.Hash == "" {
		t.Fatal("missing sha256 hash")
	}
}

func TestCodingSubAgentDriftNoteRendersBothKinds(t *testing.T) {
	note := codingSubAgentDriftNote([]string{"a.go"}, []string{"b.go"})
	if !strings.Contains(note, "a.go") || !strings.Contains(note, "b.go") {
		t.Fatalf("note must name both drifted files: %q", note)
	}
	if !strings.Contains(note, "写入后又发生了变化") || !strings.Contains(note, "读取后被外部改动") {
		t.Fatalf("note must distinguish the two drift kinds: %q", note)
	}
	if got := codingSubAgentDriftNote(nil, nil); got != "" {
		t.Fatalf("empty drift must produce no note, got %q", got)
	}
}
