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

func TestKnowledgeCancelImportIndexingOnlyWhenIndexing(t *testing.T) {
	t.Parallel()
	id := "kjob_cancel_phase_" + t.Name()
	knowledgeImportJobs.Store(id, KnowledgeImportJob{
		ID:     id,
		Status: knowledge.ImportStatusRunning,
		Result: knowledge.DirectoryImportResult{Status: knowledge.ImportStatusRunning, ImportedFiles: 0},
	})
	t.Cleanup(func() {
		knowledgeImportJobs.Delete(id)
		knowledgeImportActiveStores.Delete(id)
		clearKnowledgeImportProgressThrottle(id)
		knowledgeImportToastSent.Delete(id)
	})

	err := (&App{}).KnowledgeCancelImportIndexing(id)
	if err == nil {
		t.Fatal("expected error when cancelling non-indexing job")
	}
	v, _ := knowledgeImportJobs.Load(id)
	job := v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusRunning {
		t.Fatalf("status changed to %s", job.Status)
	}

	job.Status = knowledge.ImportStatusIndexing
	job.Result.Status = knowledge.ImportStatusIndexing
	job.Result.ImportedFiles = 3
	knowledgeImportJobs.Store(id, job)
	if err := (&App{}).KnowledgeCancelImportIndexing(id); err != nil {
		t.Fatalf("cancel indexing: %v", err)
	}
	v, _ = knowledgeImportJobs.Load(id)
	job = v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusCompleted {
		t.Fatalf("status=%s want completed", job.Status)
	}

	// Partial failure should keep failed when skipping indexing.
	id2 := "kjob_cancel_failed_" + t.Name()
	knowledgeImportJobs.Store(id2, KnowledgeImportJob{
		ID:     id2,
		Status: knowledge.ImportStatusIndexing,
		Result: knowledge.DirectoryImportResult{
			Status:        knowledge.ImportStatusIndexing,
			ImportedFiles: 2,
			FailedFiles:   1,
		},
	})
	t.Cleanup(func() {
		knowledgeImportJobs.Delete(id2)
		knowledgeImportToastSent.Delete(id2)
	})
	if err := (&App{}).KnowledgeCancelImportIndexing(id2); err != nil {
		t.Fatalf("cancel partial: %v", err)
	}
	v, _ = knowledgeImportJobs.Load(id2)
	job = v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusFailed {
		t.Fatalf("status=%s want failed", job.Status)
	}
}

func TestFinishKnowledgeImportJobDoesNotRegressTerminal(t *testing.T) {
	t.Parallel()
	id := "kjob_no_regress_" + t.Name()
	knowledgeImportJobs.Store(id, KnowledgeImportJob{
		ID:     id,
		Status: knowledge.ImportStatusCompleted,
		Result: knowledge.DirectoryImportResult{
			Status:         knowledge.ImportStatusCompleted,
			ImportedFiles:  2,
			ProcessedFiles: 2,
		},
	})
	t.Cleanup(func() {
		knowledgeImportJobs.Delete(id)
		clearKnowledgeImportProgressThrottle(id)
		knowledgeImportToastSent.Delete(id)
	})

	// Late finish with indexing must not overwrite completed.
	finishKnowledgeImportJob(nil, id, knowledge.DirectoryImportResult{
		Status:         knowledge.ImportStatusIndexing,
		ImportedFiles:  2,
		ProcessedFiles: 2,
		CurrentStep:    "linking",
	}, nil)

	v, ok := knowledgeImportJobs.Load(id)
	if !ok {
		t.Fatal("job missing")
	}
	job := v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusCompleted {
		t.Fatalf("status=%s want completed", job.Status)
	}
}

func TestUpdateProgressDoesNotRegressTerminalOrOverwriteFailed(t *testing.T) {
	t.Parallel()
	id := "kjob_prog_regress_" + t.Name()
	knowledgeImportJobs.Store(id, KnowledgeImportJob{
		ID:     id,
		Status: knowledge.ImportStatusFailed,
		Result: knowledge.DirectoryImportResult{
			Status:        knowledge.ImportStatusFailed,
			ImportedFiles: 1,
			FailedFiles:   1,
		},
	})
	t.Cleanup(func() {
		knowledgeImportJobs.Delete(id)
		clearKnowledgeImportProgressThrottle(id)
		knowledgeImportToastSent.Delete(id)
	})

	// Late indexing tick after cancel/finish must not revive job.
	updateKnowledgeImportJobProgress(nil, id, knowledge.DirectoryImportResult{
		Status:         knowledge.ImportStatusIndexing,
		ImportedFiles:  1,
		FailedFiles:    1,
		CurrentStep:    "embedding",
		StepProgress:   50,
	})
	v, _ := knowledgeImportJobs.Load(id)
	job := v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusFailed {
		t.Fatalf("status=%s want failed after indexing tick", job.Status)
	}

	// completed must not overwrite failed.
	updateKnowledgeImportJobProgress(nil, id, knowledge.DirectoryImportResult{
		Status:        knowledge.ImportStatusCompleted,
		ImportedFiles: 1,
		FailedFiles:   1,
	})
	v, _ = knowledgeImportJobs.Load(id)
	job = v.(KnowledgeImportJob)
	if job.Status != knowledge.ImportStatusFailed {
		t.Fatalf("status=%s want failed after completed tick", job.Status)
	}
}

func TestKnowledgeImportToastOnce(t *testing.T) {
	t.Parallel()
	id := "kjob_toast_once_" + t.Name()
	t.Cleanup(func() { knowledgeImportToastSent.Delete(id) })

	// Without App, toast still records once (no panic).
	knowledgeImportToastOnce(nil, id, knowledge.DirectoryImportResult{ImportedFiles: 1}, nil)
	if _, ok := knowledgeImportToastSent.Load(id); !ok {
		t.Fatal("expected toast sent marker")
	}
	// Second call is a no-op.
	knowledgeImportToastOnce(nil, id, knowledge.DirectoryImportResult{ImportedFiles: 1}, nil)
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
