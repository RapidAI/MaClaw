package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestKnowledgeImportProgressShouldEmitThrottle(t *testing.T) {
	t.Parallel()
	id := "kjob_throttle_test_" + t.Name()
	clearKnowledgeImportProgressThrottle(id)
	t.Cleanup(func() { clearKnowledgeImportProgressThrottle(id) })

	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	if !knowledgeImportProgressShouldEmit(id, now, false) {
		t.Fatal("first emit should pass")
	}
	if knowledgeImportProgressShouldEmit(id, now.Add(100*time.Millisecond), false) {
		t.Fatal("emit within 500ms should be throttled")
	}
	if !knowledgeImportProgressShouldEmit(id, now.Add(100*time.Millisecond), true) {
		t.Fatal("force emit should bypass throttle")
	}
	// Force reset last emit to +100ms; next free emit needs +100ms+500ms.
	if knowledgeImportProgressShouldEmit(id, now.Add(400*time.Millisecond), false) {
		t.Fatal("still within 500ms of force emit should throttle")
	}
	if !knowledgeImportProgressShouldEmit(id, now.Add(100*time.Millisecond+knowledgeImportProgressMinInterval), false) {
		t.Fatal("emit after min interval should pass")
	}
}

func TestKnowledgeImportDoneToast(t *testing.T) {
	t.Parallel()

	msg, typ, duration := knowledgeImportDoneToast(knowledge.DirectoryImportResult{}, errors.New("disk full"))
	if typ != "error" || duration != 5000 || !strings.Contains(msg, "disk full") {
		t.Fatalf("error toast = %q %q %d", msg, typ, duration)
	}

	msg, typ, duration = knowledgeImportDoneToast(knowledge.DirectoryImportResult{
		Status:        knowledge.ImportStatusCompleted,
		ImportedFiles: 3,
		SkippedFiles:  1,
	}, nil)
	if typ != "success" || duration != 4000 || !strings.Contains(msg, "3") || !strings.Contains(msg, "1") {
		t.Fatalf("success toast = %q %q %d", msg, typ, duration)
	}

	msg, typ, duration = knowledgeImportDoneToast(knowledge.DirectoryImportResult{
		Status:        knowledge.ImportStatusFailed,
		ImportedFiles: 2,
		FailedFiles:   1,
	}, nil)
	if typ != "warning" || duration != 5000 || !strings.Contains(msg, "2") || !strings.Contains(msg, "1") {
		t.Fatalf("partial toast = %q %q %d", msg, typ, duration)
	}

	msg, typ, duration = knowledgeImportDoneToast(knowledge.DirectoryImportResult{
		Status:      knowledge.ImportStatusFailed,
		FailedFiles: 4,
	}, nil)
	if typ != "error" || duration != 5000 || !strings.Contains(msg, "4") {
		t.Fatalf("all-failed toast = %q %q %d", msg, typ, duration)
	}
}
