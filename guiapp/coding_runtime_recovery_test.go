package guiapp

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestCloseCodingRuntimeStoreIsTerminalAndFencesReopen(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if store := app.ensureCodingRuntimeStore(); store == nil {
		t.Fatal("runtime store unavailable")
	}
	app.closeCodingRuntimeStore()
	if store := app.ensureCodingRuntimeStore(); store != nil {
		t.Fatalf("runtime store reopened after close: %#v", store)
	}
	if store := app.openExistingCodingRuntimeStore(); store != nil {
		t.Fatalf("existing runtime store reopened after close: %#v", store)
	}
}

func TestCodingRuntimeStoreFollowsDataDirReplacement(t *testing.T) {
	firstBase := t.TempDir()
	secondBase := t.TempDir()
	app := &App{testHomeDir: firstBase}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	first := app.ensureCodingRuntimeStore()
	if first == nil {
		t.Fatal("runtime store unavailable")
	}
	firstDataDir := app.GetDataDir()
	app.effectiveBaseDir.Store(secondBase)
	if got := app.ensureCodingRuntimeStore(); got != first {
		t.Fatalf("coding store epoch changed before reset commit: got=%#v want=%#v", got, first)
	}
	app.resetCodingAuthorizationStoresForDataDirChange(firstDataDir)
	second := app.ensureCodingRuntimeStore()
	if second == nil {
		t.Fatal("replacement runtime store unavailable")
	}
	if second == first {
		t.Fatal("runtime store did not switch data directories")
	}
	if _, err := first.CreateTask(codingruntime.Task{ProjectRef: "project", Mode: "local"}); err == nil {
		t.Fatal("retired runtime store still accepts task creation")
	}
	if _, err := os.Stat(filepath.Join(secondBase, "data", "coding_runtime.db")); err != nil {
		t.Fatalf("replacement runtime ledger missing: %v", err)
	}
}

type cancelObserveExecutor struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (e *cancelObserveExecutor) Execute(ctx context.Context, _ codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	close(e.started)
	<-ctx.Done()
	close(e.cancelled)
	return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}
}

type contextObserveExecutor struct {
	started   chan struct{}
	cancelled chan struct{}
	ctx       chan context.Context
}

func (e *contextObserveExecutor) Execute(ctx context.Context, _ codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	e.ctx <- ctx
	close(e.started)
	<-ctx.Done()
	close(e.cancelled)
	return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}
}

func TestCodingRuntimeStoreDataDirResetCancelsLiveAttempt(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	executor := &cancelObserveExecutor{started: make(chan struct{}), cancelled: make(chan struct{})}
	type runOutcome struct {
		status codingruntime.TaskStatus
		err    error
	}
	done := make(chan runOutcome, 1)
	go func() {
		task, _, err := (codingruntime.Runner{Store: store, LeaseOwner: "owner", LeaseDuration: time.Minute}).Run(context.Background(), codingruntime.Task{ProjectRef: "project", Mode: "local"}, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local"}, executor)
		if task != nil {
			done <- runOutcome{status: task.Status, err: err}
		} else {
			done <- runOutcome{err: err}
		}
	}()
	<-executor.started
	app.effectiveBaseDir.Store(filepath.Join(t.TempDir(), ".maclaw"))
	app.resetCodingRuntimeStoreForDataDirChange()
	select {
	case <-executor.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("data-dir reset did not cancel the live coding attempt")
	}
	result := <-done
	if result.err != nil {
		t.Fatalf("cancelled attempt did not finish cleanly before ledger close: %v", result.err)
	}
	if result.status != codingruntime.TaskInterrupted {
		t.Fatalf("cancelled attempt status=%q, want interrupted", result.status)
	}
}

type blockingReleaseExecutor struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (e *blockingReleaseExecutor) Execute(_ context.Context, _ codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	close(e.started)
	<-e.release
	close(e.finished)
	return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}
}

func TestCodingRuntimeStoreDataDirResetInterruptsStuckLiveAttemptBeforeMigration(t *testing.T) {
	previousWait := codingRuntimeDataDirResetWait
	codingRuntimeDataDirResetWait = 25 * time.Millisecond
	t.Cleanup(func() { codingRuntimeDataDirResetWait = previousWait })
	app := &App{testHomeDir: t.TempDir()}
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: "project", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	executor := &blockingReleaseExecutor{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		_, _, _ = (codingruntime.Runner{Store: store, LeaseOwner: "owner", LeaseDuration: time.Minute}).Run(context.Background(), codingruntime.Task{TaskID: task.TaskID, ProjectRef: "project", Mode: "local"}, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local"}, executor)
	}()
	<-executor.started
	replacementBase := filepath.Join(t.TempDir(), ".maclaw")
	replacementDir := filepath.Join(replacementBase, "data")
	app.effectiveBaseDir.Store(replacementBase)
	app.resetCodingAuthorizationStoresForDataDirChange(app.codingAuthorizationDataDirForApp())
	migrated, err := codingruntime.NewSQLiteStore(filepath.Join(replacementDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := migrated.GetTask(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != codingruntime.TaskInterrupted {
		t.Fatalf("stuck live task migrated with status=%q, want interrupted", persisted.Status)
	}
	if err := migrated.Close(); err != nil {
		t.Fatal(err)
	}
	close(executor.release)
	<-executor.finished
	<-runnerDone
}

func TestCodingRuntimeStoreDataDirResetInterruptsQueuedWorkDurably(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: "project", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(app.GetDataDir(), "coding_runtime.db")
	app.effectiveBaseDir.Store(filepath.Join(t.TempDir(), ".maclaw"))
	app.resetCodingRuntimeStoreForDataDirChange()
	reopened, err := codingruntime.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.GetTask(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != codingruntime.TaskInterrupted {
		t.Fatalf("queued task status=%q after data-dir reset, want interrupted", persisted.Status)
	}
}

func TestCodingAuthorizationDataDirResetMigratesQuiescedHistory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: "project", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approvalTask, err := store.CreateTask(codingruntime.Task{ProjectRef: "approval-project", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTaskWaitingApproval(approvalTask.TaskID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	service, err := app.codingTaskRelationServiceForApp()
	if err != nil {
		t.Fatal(err)
	}
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := service.CreateCodingTask(subject, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := app.GetDataDir()
	replacementBase := filepath.Join(t.TempDir(), ".maclaw")
	replacementDir := filepath.Join(replacementBase, "data")
	app.effectiveBaseDir.Store(replacementBase)
	app.resetCodingAuthorizationStoresForDataDirChange(sourceDir)

	for _, name := range []string{"coding_runtime.db", "coding_task_relations.db"} {
		if _, err := os.Stat(filepath.Join(replacementDir, name)); err != nil {
			t.Fatalf("migrated %s missing: %v", name, err)
		}
	}
	migratedRuntime, err := codingruntime.NewSQLiteStore(filepath.Join(replacementDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := migratedRuntime.GetTask(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != codingruntime.TaskInterrupted {
		t.Fatalf("migrated queued task status=%q, want interrupted", persisted.Status)
	}
	persistedApproval, err := migratedRuntime.GetTask(approvalTask.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedApproval.Status != codingruntime.TaskInterrupted {
		t.Fatalf("migrated waiting-approval task status=%q, want interrupted", persistedApproval.Status)
	}
	if err := migratedRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	migratedRelations, err := newCodingTaskRelationService(filepath.Join(replacementDir, "coding_task_relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migratedRelations.VerifyCodingContinuation(subject, handle, time.Now().UTC(), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("migrated active handle continuation err=%v, want revoked", err)
	}
	if err := migratedRelations.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDataMigrationArtifactsUseWindowsCaseSemantics(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive artifact semantics are Windows-specific")
	}
	if !isDataMigrationSourceArtifact(".MIGRATED_TO") {
		t.Fatal("uppercase completion marker spelling was treated as payload on Windows")
	}
	if !isDataMigrationTargetArtifact(".MACLAW_DATA_MIGRATION.JSON") {
		t.Fatal("uppercase journal spelling was not recognized as target artifact on Windows")
	}
	if validDataMigrationRelPath("payload.MACLAW-MIGRATION-TMP") {
		t.Fatal("uppercase migration temp suffix escaped the reserved-path validator on Windows")
	}
}

func TestStartupDataMigrationMigratesCodingAuthorizationStores(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := t.TempDir()
	sourceDataDir := filepath.Join(defaultBase, "data")
	if err := os.MkdirAll(sourceDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(sourceDataDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "startup-source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)
	marker, err := os.ReadFile(filepath.Join(defaultBase, ".migrated_to"))
	if err != nil {
		t.Fatalf("startup migration did not write generic completion marker: %v", err)
	}
	if string(marker) != filepath.Clean(targetBase) {
		t.Fatalf("generic completion marker = %q, want %q", string(marker), filepath.Clean(targetBase))
	}
	targetStore, err := codingruntime.NewSQLiteStore(filepath.Join(targetBase, "data", "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetStore.Close()
	if _, err := targetStore.GetTask(sourceTask.TaskID); err != nil {
		t.Fatalf("startup migration source task missing from target: %v", err)
	}
}

func TestStartupDataMigrationCodingFailureBlocksGenericCompletion(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := t.TempDir()
	sourceDataDir := filepath.Join(defaultBase, "data")
	if err := os.MkdirAll(sourceDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDataDir, "coding_runtime.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultBase, "keep.txt"), []byte("generic"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)
	if _, err := os.Stat(filepath.Join(targetBase, "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("generic migration continued after coding migration failure: stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultBase, ".migrated_to")); !os.IsNotExist(err) {
		t.Fatalf("generic migration completion marker written after coding failure: stat err=%v", err)
	}
}

func TestStartupDataMigrationPreservesIndependentTargetData(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := t.TempDir()
	if err := os.WriteFile(filepath.Join(defaultBase, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	independentPath := filepath.Join(targetBase, "independent.txt")
	if err := os.WriteFile(independentPath, []byte("independent"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)

	if got, err := os.ReadFile(independentPath); err != nil || string(got) != "independent" {
		t.Fatalf("independent target data was not preserved: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, "source.txt")); !os.IsNotExist(err) {
		t.Fatalf("source file was copied into independent target, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultBase, ".migrated_to")); !os.IsNotExist(err) {
		t.Fatalf("migration completion marker written despite independent target, stat err=%v", err)
	}
	if _, err := os.Stat(dataMigrationJournalPath(targetBase)); !os.IsNotExist(err) {
		t.Fatalf("migration journal written before target ownership check, stat err=%v", err)
	}
}

func writeStartupMigrationTestJournal(t *testing.T, sourceBase, targetBase string) dataDirMigrationJournal {
	t.Helper()
	files, _, err := scanDataMigrationFiles(sourceBase)
	if err != nil {
		t.Fatal(err)
	}
	journal := dataDirMigrationJournal{
		Version:   dataDirMigrationJournalVersion,
		SourceDir: filepath.Clean(sourceBase),
		Files:     files,
		StartedAt: time.Now().Unix(),
	}
	if err := writeDataDirMigrationJournal(targetBase, journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestStartupDataMigrationResumesInterruptedGenericCopy(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := t.TempDir()
	sourceNotes := filepath.Join(defaultBase, "notes")
	if err := os.MkdirAll(sourceNotes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceNotes, "a.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceNotes, "b.txt"), []byte("bravo"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := writeStartupMigrationTestJournal(t, defaultBase, targetBase)
	if len(journal.Files) != 2 {
		t.Fatalf("journal files=%d, want 2", len(journal.Files))
	}
	targetNotes := filepath.Join(targetBase, "notes")
	if err := os.MkdirAll(targetNotes, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := copyMigrationFileAtomic(filepath.Join(sourceNotes, "a.txt"), filepath.Join(targetNotes, "a.txt")); err != nil {
		t.Fatal(err)
	}
	partialTemp := filepath.Join(targetNotes, "b.txt") + dataDirMigrationTempSuffix
	if err := os.WriteFile(partialTemp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)

	for name, want := range map[string]string{"a.txt": "alpha", "b.txt": "bravo"} {
		if got, err := os.ReadFile(filepath.Join(targetNotes, name)); err != nil || string(got) != want {
			t.Fatalf("resumed migration file %s=%q err=%v, want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(partialTemp); !os.IsNotExist(err) {
		t.Fatalf("partial migration temp survived resume, stat err=%v", err)
	}
	if _, err := os.Stat(dataMigrationJournalPath(targetBase)); !os.IsNotExist(err) {
		t.Fatalf("migration journal survived successful completion, stat err=%v", err)
	}
	if marker, err := os.ReadFile(filepath.Join(defaultBase, ".migrated_to")); err != nil || string(marker) != filepath.Clean(targetBase) {
		t.Fatalf("completion marker=%q err=%v, want %q", string(marker), err, filepath.Clean(targetBase))
	}
}

func TestStartupDataMigrationInterruptedCopyRejectsForeignTargetFile(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := t.TempDir()
	if err := os.WriteFile(filepath.Join(defaultBase, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStartupMigrationTestJournal(t, defaultBase, targetBase)
	foreignPath := filepath.Join(targetBase, "foreign.txt")
	if err := os.WriteFile(foreignPath, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)

	if got, err := os.ReadFile(foreignPath); err != nil || string(got) != "foreign" {
		t.Fatalf("foreign target file was not preserved: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, "source.txt")); !os.IsNotExist(err) {
		t.Fatalf("migration copied despite foreign target file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultBase, ".migrated_to")); !os.IsNotExist(err) {
		t.Fatalf("migration completion marker written despite foreign target file, stat err=%v", err)
	}
	if _, err := os.Stat(dataMigrationJournalPath(targetBase)); err != nil {
		t.Fatalf("migration recovery journal missing after fail-closed validation: %v", err)
	}
}

func TestStartupDataMigrationTreatsTargetConfigMarkerAndWriteCheckAsIndependentData(t *testing.T) {
	for _, name := range []string{"config.json", ".migrated_to", ".maclaw_write_check"} {
		t.Run(name, func(t *testing.T) {
			defaultBase := filepath.Join(t.TempDir(), "default-base")
			targetBase := filepath.Join(t.TempDir(), "target-base")
			if err := os.MkdirAll(filepath.Join(defaultBase, "data"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(defaultBase, "data", "payload.txt"), []byte("payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(targetBase, 0o755); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(targetBase, name)
			if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
				t.Fatal(err)
			}

			app := &App{}
			app.migrateDataDirBetweenBases(defaultBase, targetBase)
			if _, err := os.Stat(filepath.Join(targetBase, "data", "payload.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("migration copied into target despite independent %s, stat err=%v", name, err)
			}
			if payload, err := os.ReadFile(foreign); err != nil || string(payload) != "foreign" {
				t.Fatalf("independent target %s changed: %q err=%v", name, payload, err)
			}
		})
	}
}

func TestStartupDataMigrationCleansOnlyEmptyWriteCheckProbe(t *testing.T) {
	for _, tc := range []struct {
		name     string
		payload  string
		migrated bool
	}{
		{name: "empty probe is cleaned", payload: "", migrated: true},
		{name: "nonempty lookalike blocks", payload: "foreign", migrated: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaultBase := filepath.Join(t.TempDir(), "default-base")
			targetBase := filepath.Join(t.TempDir(), "target-base")
			if err := os.MkdirAll(filepath.Join(defaultBase, "data"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(defaultBase, "data", "payload.txt"), []byte("payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(targetBase, 0o755); err != nil {
				t.Fatal(err)
			}
			probe := filepath.Join(targetBase, ".maclaw_write_check-12345")
			if err := os.WriteFile(probe, []byte(tc.payload), 0o644); err != nil {
				t.Fatal(err)
			}

			app := &App{}
			app.migrateDataDirBetweenBases(defaultBase, targetBase)
			_, copyErr := os.Stat(filepath.Join(targetBase, "data", "payload.txt"))
			if tc.migrated && copyErr != nil {
				t.Fatalf("migration did not resume after empty probe cleanup: %v", copyErr)
			}
			if !tc.migrated && !errors.Is(copyErr, os.ErrNotExist) {
				t.Fatalf("migration copied despite nonempty probe lookalike, stat err=%v", copyErr)
			}
			if tc.migrated {
				if _, err := os.Stat(probe); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("empty probe was not cleaned, stat err=%v", err)
				}
			} else if payload, err := os.ReadFile(probe); err != nil || string(payload) != tc.payload {
				t.Fatalf("nonempty probe lookalike changed: %q err=%v", payload, err)
			}
		})
	}
}

func TestStartupDataMigrationDoesNotDeleteUnownedTempLookingTargetFile(t *testing.T) {
	defaultBase := filepath.Join(t.TempDir(), "default-base")
	targetBase := filepath.Join(t.TempDir(), "target-base")
	if err := os.MkdirAll(filepath.Join(defaultBase, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultBase, "data", "payload.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(targetBase, "data", "foreign.txt"+".maclaw-migration-tmp")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)
	if payload, err := os.ReadFile(foreign); err != nil || string(payload) != "foreign" {
		t.Fatalf("unowned temp-looking target file changed: %q err=%v", payload, err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, "data", "payload.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration copied into target despite unowned temp-looking file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, dataDirMigrationJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration journal exists despite target rejection, stat err=%v", err)
	}
}

func TestFinishDataMigrationRequiresAllJournaledOutputs(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	journal := &dataDirMigrationJournal{
		Version:   dataDirMigrationJournalVersion,
		SourceDir: filepath.Clean(src),
		Files: []dataDirMigrationFile{
			{Path: filepath.Join("data", "one.txt"), Size: 3},
			{Path: filepath.Join("data", "two.txt"), Size: 3},
		},
		StartedAt: time.Now().Unix(),
	}
	if err := writeDataDirMigrationJournal(dst, *journal); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "data", "one.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := finishDataMigration(src, dst, journal); err == nil {
		t.Fatal("completion succeeded with a journaled output still missing")
	}
	if _, err := os.Stat(filepath.Join(src, ".migrated_to")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completion marker written despite incomplete output, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, dataDirMigrationJournalName)); err != nil {
		t.Fatalf("recovery journal removed despite failed completion: %v", err)
	}
}

func TestStartupDataMigrationRejectsOverlappingDirectories(t *testing.T) {
	defaultBase := t.TempDir()
	targetBase := filepath.Join(defaultBase, "target")
	if err := os.WriteFile(filepath.Join(defaultBase, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	app.migrateDataDirBetweenBases(defaultBase, targetBase)

	if _, err := os.Stat(filepath.Join(targetBase, "source.txt")); !os.IsNotExist(err) {
		t.Fatalf("overlapping target received migrated file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultBase, ".migrated_to")); !os.IsNotExist(err) {
		t.Fatalf("overlapping migration wrote completion marker, stat err=%v", err)
	}
}

func TestGenericDataMigrationExcludesCodingAuthorizationSQLiteArtifacts(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	dataDir := filepath.Join(sourceRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := []string{
		"coding_runtime.db", "coding_runtime.db-wal", "coding_runtime.db-shm", "coding_runtime.db-journal",
		"coding_task_relations.db", "coding_task_relations.db-wal", "coding_task_relations.db-shm", "coding_task_relations.db-journal",
		".coding_authorization_migration.json", ".coding_authorization_migration.json.tmp", ".migrating-coding_runtime.db", ".migrating-coding_runtime.db-wal",
	}
	for _, name := range managed {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("managed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ordinary.txt"), []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.performDataMigration(sourceRoot, targetRoot)
	if got, err := os.ReadFile(filepath.Join(targetRoot, "data", "ordinary.txt")); err != nil || string(got) != "ordinary" {
		t.Fatalf("ordinary data file not copied: %q err=%v", got, err)
	}
	for _, name := range managed {
		if _, err := os.Stat(filepath.Join(targetRoot, "data", name)); !os.IsNotExist(err) {
			t.Fatalf("generic migration copied managed coding artifact %s err=%v", name, err)
		}
	}
}

func TestMigrateCodingAuthorizationStoresPreservesExistingDestination(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(sourceDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	targetStore, err := codingruntime.NewSQLiteStore(filepath.Join(targetDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	targetTask, err := targetStore.CreateTask(codingruntime.Task{ProjectRef: "target", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := targetStore.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(sourceDir, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Fatal("migration overwrote or reported success despite existing destination history")
	}
	reopened, err := codingruntime.NewSQLiteStore(filepath.Join(targetDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetTask(targetTask.TaskID); err != nil {
		t.Fatalf("existing destination task missing after migration skip: %v", err)
	}
	if _, err := reopened.GetTask(sourceTask.TaskID); !errors.Is(err, codingruntime.ErrNotFound) {
		t.Fatalf("source task was merged into existing destination: %v", err)
	}
}

func TestMigrateCodingAuthorizationStoresRejectsOrphanedDestinationSidecar(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(sourceDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(targetDir, "coding_runtime.db-wal")
	if err := os.WriteFile(orphan, []byte("orphaned wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(sourceDir, targetDir)
	if err == nil || !strings.Contains(err.Error(), "orphaned SQLite artifact") {
		t.Fatalf("orphaned destination sidecar migration err=%v, want fail-closed error", err)
	}
	if migrated {
		t.Fatal("migration reported success despite orphaned destination sidecar")
	}
	if got, readErr := os.ReadFile(orphan); readErr != nil || string(got) != "orphaned wal" {
		t.Fatalf("orphaned sidecar was not preserved: %q err=%v", got, readErr)
	}
}

func TestMigrateCodingAuthorizationStoresResumesOriginalSourceAfterInterruption(t *testing.T) {
	originalSource := t.TempDir()
	wrongSource := t.TempDir()
	targetDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(originalSource, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "original", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	wrongStore, err := codingruntime.NewSQLiteStore(filepath.Join(wrongSource, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	wrongTask, err := wrongStore.CreateTask(codingruntime.Task{ProjectRef: "wrong", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongStore.Close(); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"source_dir":"` + filepath.ToSlash(originalSource) + `","files":["coding_runtime.db"]}`)
	// The marker is JSON; forward slashes are valid path content on Windows for
	// this internal recovery metadata and avoid escaping every separator.
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationMarker), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_runtime.db")), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(wrongSource, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("interrupted migration did not resume from its recorded source")
	}
	reopened, err := codingruntime.NewSQLiteStore(filepath.Join(targetDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetTask(sourceTask.TaskID); err != nil {
		t.Fatalf("original source task missing after resume: %v", err)
	}
	if _, err := reopened.GetTask(wrongTask.TaskID); !errors.Is(err, codingruntime.ErrNotFound) {
		t.Fatalf("wrong-source task appeared after resume: %v", err)
	}
}

func TestMigrateCodingAuthorizationStoresRepairsPendingSourceBeforeCopy(t *testing.T) {
	originalSource := t.TempDir()
	intermediateDir := t.TempDir()
	finalDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(originalSource, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "chain-source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"source_dir":"` + filepath.ToSlash(originalSource) + `","files":["coding_runtime.db"]}`)
	if err := os.WriteFile(filepath.Join(intermediateDir, codingAuthorizationMigrationMarker), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intermediateDir, codingAuthorizationMigrationTempName("coding_runtime.db")), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(intermediateDir, finalDir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("migration did not copy repaired intermediate source into final target")
	}
	for _, dir := range []string{intermediateDir, finalDir} {
		if codingAuthorizationMigrationPending(dir) {
			t.Fatalf("migration marker still pending in %q", dir)
		}
		store, err := codingruntime.NewSQLiteStore(filepath.Join(dir, "coding_runtime.db"))
		if err != nil {
			t.Fatalf("open repaired ledger %q: %v", dir, err)
		}
		if _, err := store.GetTask(sourceTask.TaskID); err != nil {
			_ = store.Close()
			t.Fatalf("source task missing from repaired ledger %q: %v", dir, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateCodingAuthorizationStoresRejectsCyclicPendingSources(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstMarker := []byte(`{"source_dir":"` + filepath.ToSlash(secondDir) + `","files":["coding_runtime.db"]}`)
	secondMarker := []byte(`{"source_dir":"` + filepath.ToSlash(firstDir) + `","files":["coding_runtime.db"]}`)
	if err := os.WriteFile(filepath.Join(firstDir, codingAuthorizationMigrationMarker), firstMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDir, codingAuthorizationMigrationMarker), secondMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(firstDir, secondDir)
	if err == nil || !strings.Contains(err.Error(), "cyclic coding authorization migration chain") {
		t.Fatalf("cyclic migration err=%v, want cycle error", err)
	}
	if migrated {
		t.Fatal("cyclic migration reported success")
	}
}

func TestMigrateCodingAuthorizationStoresRejectsWindowsCaseVariantCycle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path case semantics")
	}
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstVariant := windowsCaseVariantPath(t, firstDir)
	secondVariant := windowsCaseVariantPath(t, secondDir)
	firstMarker := []byte(`{"source_dir":"` + filepath.ToSlash(secondVariant) + `","files":["coding_runtime.db"]}`)
	secondMarker := []byte(`{"source_dir":"` + filepath.ToSlash(firstVariant) + `","files":["coding_runtime.db"]}`)
	if err := os.WriteFile(filepath.Join(firstDir, codingAuthorizationMigrationMarker), firstMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDir, codingAuthorizationMigrationMarker), secondMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateCodingAuthorizationStoresForDataDirChange(firstDir, secondDir)
	if err == nil || !strings.Contains(err.Error(), "cyclic coding authorization migration chain") {
		t.Fatalf("case-variant cyclic migration err=%v, want cycle error", err)
	}
	if migrated {
		t.Fatal("case-variant cyclic migration reported success")
	}
}

func TestRecoverCodingAuthorizationMigrationDiscardsUnrenamedMarkerTemp(t *testing.T) {
	targetDir := t.TempDir()
	tempMarker := filepath.Join(targetDir, codingAuthorizationMigrationMarker+".tmp")
	if err := os.WriteFile(tempMarker, []byte(`{"files":["coding_runtime.db"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverCodingAuthorizationMigration(targetDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempMarker); !os.IsNotExist(err) {
		t.Fatalf("unrenamed migration marker temp survived recovery, stat err=%v", err)
	}
}

func TestRecoverCodingAuthorizationMigrationDiscardsPartialOutput(t *testing.T) {
	targetDir := t.TempDir()
	markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
	if err := os.WriteFile(markerPath, []byte(`{"files":["coding_runtime.db","coding_task_relations.db"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	partialFinal := filepath.Join(targetDir, "coding_runtime.db")
	partialTemp := filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_task_relations.db"))
	partialTempJournal := filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_task_relations.db")+"-journal")
	for _, path := range []string{partialFinal, partialTemp, partialTempJournal} {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := recoverCodingAuthorizationMigration(targetDir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{partialFinal, partialTemp, partialTempJournal} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial migration artifact %s survived recovery, stat err=%v", path, err)
		}
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("recovery marker missing before retry metadata could be rewritten: %v", err)
	}
}

func TestRecoverCodingAuthorizationMigrationRejectsUnexpectedManagedArtifact(t *testing.T) {
	targetDir := t.TempDir()
	markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
	if err := os.WriteFile(markerPath, []byte(`{"files":["coding_runtime.db"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(targetDir, "coding_task_relations.db")
	if err := os.WriteFile(unexpected, []byte("independent destination history"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverCodingAuthorizationMigration(targetDir); err == nil || !strings.Contains(err.Error(), "unexpected artifact") {
		t.Fatalf("unexpected managed artifact err=%v, want fail-closed recovery error", err)
	}
	if got, err := os.ReadFile(unexpected); err != nil || string(got) != "independent destination history" {
		t.Fatalf("unexpected destination history was not preserved: %q err=%v", got, err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("migration marker evidence missing after failed recovery: %v", err)
	}
}

func TestRecoverCodingAuthorizationMigrationRejectsInvalidMarkerFileSet(t *testing.T) {
	for _, marker := range []string{
		`{"files":["../coding_runtime.db"]}`,
		`{"files":["coding_runtime.db","coding_runtime.db"]}`,
	} {
		targetDir := t.TempDir()
		markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
		if err := os.WriteFile(markerPath, []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := recoverCodingAuthorizationMigration(targetDir); err == nil || !strings.Contains(err.Error(), "invalid coding authorization migration marker") {
			t.Fatalf("marker %s err=%v, want invalid marker error", marker, err)
		}
	}
}

func TestRecoverCodingAuthorizationMigrationCompletesInstalledOutput(t *testing.T) {
	targetDir := t.TempDir()
	markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
	if err := os.WriteFile(markerPath, []byte(`{"files":["coding_runtime.db"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(targetDir, "coding_runtime.db")
	if err := os.WriteFile(finalPath, []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverCodingAuthorizationMigration(targetDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("complete migration output missing after recovery: %v", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("complete migration marker survived recovery, stat err=%v", err)
	}
}

func TestOpenExistingCodingRuntimeStoreResumesInterruptedMigration(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(sourceDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "probe-resume-source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"source_dir":"` + filepath.ToSlash(sourceDir) + `","files":["coding_runtime.db"]}`)
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationMarker), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_runtime.db")), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	app.codingAuthorizationDataDir.Store(targetDir)
	store := app.openExistingCodingRuntimeStore()
	if store == nil {
		t.Fatal("existing-ledger probe did not resume interrupted migration")
	}
	if _, err := store.GetTask(sourceTask.TaskID); err != nil {
		t.Fatalf("source task missing after probe-time migration recovery: %v", err)
	}
	if codingAuthorizationMigrationPending(targetDir) {
		t.Fatal("migration marker still pending after existing-ledger probe recovery")
	}
}

func TestCodingRuntimeStoreOpenResumesInterruptedMigration(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	sourceStore, err := codingruntime.NewSQLiteStore(filepath.Join(sourceDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	sourceTask, err := sourceStore.CreateTask(codingruntime.Task{ProjectRef: "resume-source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"source_dir":"` + filepath.ToSlash(sourceDir) + `","files":["coding_runtime.db"]}`)
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationMarker), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	partialTemp := filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_runtime.db"))
	if err := os.WriteFile(partialTemp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	app.codingAuthorizationDataDir.Store(targetDir)
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable after interrupted migration recovery")
	}
	if _, err := store.GetTask(sourceTask.TaskID); err != nil {
		t.Fatalf("source task missing after open-time migration recovery: %v", err)
	}
	if codingAuthorizationMigrationPending(targetDir) {
		t.Fatal("migration marker still pending after successful open-time recovery")
	}
	if _, err := os.Stat(partialTemp); !os.IsNotExist(err) {
		t.Fatalf("partial migration temp survived recovery, stat err=%v", err)
	}
}

func TestRelationServiceOpenResumesInterruptedMigration(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	sourceService, err := newCodingTaskRelationService(filepath.Join(sourceDir, "coding_task_relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := sourceService.CreateCodingTask(subject, time.Unix(1_700_000_000, 0), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceService.Close(); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"source_dir":"` + filepath.ToSlash(sourceDir) + `","files":["coding_task_relations.db"]}`)
	if err := os.WriteFile(filepath.Join(targetDir, codingAuthorizationMigrationMarker), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	partialTemp := filepath.Join(targetDir, codingAuthorizationMigrationTempName("coding_task_relations.db"))
	if err := os.WriteFile(partialTemp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	issuer := &desktopCodingCapabilityIssuer{}
	service, err := issuer.relationService(targetDir)
	if err != nil {
		t.Fatalf("relation service did not resume interrupted migration: %v", err)
	}
	if service == nil {
		t.Fatal("relation service unavailable after interrupted migration recovery")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if codingAuthorizationMigrationPending(targetDir) {
		t.Fatal("migration marker still pending after successful relation-open recovery")
	}
	db, err := sql.Open("sqlite", filepath.Join(targetDir, "coding_task_relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM coding_task_relations WHERE handle_id=?`, handle.handleID).Scan(&status); err != nil {
		t.Fatalf("source relation row missing after open-time recovery: %v", err)
	}
	if status != string(codingTaskRelationRevoked) {
		t.Fatalf("migrated relation status=%q, want revoked history rather than live authority", status)
	}
	if _, err := os.Stat(partialTemp); !os.IsNotExist(err) {
		t.Fatalf("partial relation migration temp survived recovery, stat err=%v", err)
	}
}

func TestCodingAuthorizationMigrationPendingFencesStoreOpen(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, codingAuthorizationMigrationMarker), []byte(`{"files":["coding_runtime.db"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	app.codingAuthorizationDataDir.Store(dataDir)
	if store := app.ensureCodingRuntimeStore(); store != nil {
		t.Fatalf("runtime ledger opened while migration recovery was pending: %#v", store)
	}
	issuer := &desktopCodingCapabilityIssuer{}
	if service, err := issuer.relationService(dataDir); err == nil || service != nil {
		t.Fatalf("relation service opened while migration recovery was pending: service=%#v err=%v", service, err)
	}
}

func TestCodingAuthorizationDataDirResetSkipsWhenEpochAlreadyMatchesTarget(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
		app.closeCodingRuntimeStore()
	})
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	service, err := app.codingTaskRelationServiceForApp()
	if err != nil || service == nil {
		t.Fatalf("relation service unavailable: service=%#v err=%v", service, err)
	}
	dir := app.codingAuthorizationDataDirForApp()

	app.resetCodingAuthorizationStoresForDataDirChangeTo(dir, dir)

	if got := app.ensureCodingRuntimeStore(); got != store {
		t.Fatalf("no-op data-dir reset replaced runtime store: got=%#v want=%#v", got, store)
	}
	gotService, err := app.codingTaskRelationServiceForApp()
	if err != nil {
		t.Fatal(err)
	}
	if gotService != service {
		t.Fatalf("no-op data-dir reset replaced relation service: got=%#v want=%#v", gotService, service)
	}
}

func windowsCaseVariantPath(t *testing.T, path string) string {
	t.Helper()
	variant := strings.ToUpper(path)
	if variant == path {
		variant = strings.ToLower(path)
	}
	if variant == path {
		t.Fatalf("could not construct case variant for %q", path)
	}
	return variant
}

func TestCodingAuthorizationDataDirResetTreatsWindowsCaseOnlyPathAsSameEpoch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path case semantics")
	}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
		app.closeCodingRuntimeStore()
	})
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	service, err := app.codingTaskRelationServiceForApp()
	if err != nil || service == nil {
		t.Fatalf("relation service unavailable: service=%#v err=%v", service, err)
	}
	dir := app.codingAuthorizationDataDirForApp()
	variant := strings.ToUpper(dir)
	if variant == dir {
		variant = strings.ToLower(dir)
	}
	if variant == dir {
		t.Fatalf("could not construct case variant for %q", dir)
	}

	app.resetCodingAuthorizationStoresForDataDirChangeTo(dir, variant)

	if got := app.ensureCodingRuntimeStore(); got != store {
		t.Fatalf("case-only data-dir reset replaced runtime store: got=%#v want=%#v", got, store)
	}
	gotService, err := app.codingTaskRelationServiceForApp()
	if err != nil {
		t.Fatal(err)
	}
	if gotService != service {
		t.Fatalf("case-only data-dir reset replaced relation service: got=%#v want=%#v", gotService, service)
	}
}

func TestCodingAuthorizationDataDirResetMergesWindowsCaseOnlySourcePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path case semantics")
	}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
		app.closeCodingRuntimeStore()
	})
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: "case-source", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.codingTaskRelationServiceForApp()
	if err != nil || service == nil {
		t.Fatalf("relation service unavailable: service=%#v err=%v", service, err)
	}
	app.codingRuntimeStorePath = windowsCaseVariantPath(t, app.codingRuntimeStorePath)
	replacementDir := filepath.Join(t.TempDir(), "data")

	app.resetCodingAuthorizationStoresForDataDirChangeTo("", replacementDir)

	for _, name := range []string{"coding_runtime.db", "coding_task_relations.db"} {
		if _, err := os.Stat(filepath.Join(replacementDir, name)); err != nil {
			t.Fatalf("case-equivalent source paths blocked migration of %s: %v", name, err)
		}
	}
	migratedStore, err := codingruntime.NewSQLiteStore(filepath.Join(replacementDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer migratedStore.Close()
	if _, err := migratedStore.GetTask(task.TaskID); err != nil {
		t.Fatalf("source task missing after case-equivalent source migration: %v", err)
	}
}

func TestCodingAuthorizationRapidSwitchMigratesCommittedIntermediateEpoch(t *testing.T) {
	firstBase := t.TempDir()
	app := &App{testHomeDir: firstBase}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	firstStore := app.ensureCodingRuntimeStore()
	if firstStore == nil {
		t.Fatal("runtime store unavailable")
	}
	firstTask, err := firstStore.CreateTask(codingruntime.Task{ProjectRef: "first", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	firstDir := app.GetDataDir()

	intermediateBase := filepath.Join(t.TempDir(), ".maclaw")
	intermediateDir := filepath.Join(intermediateBase, "data")
	if err := os.MkdirAll(intermediateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	intermediateStore, err := codingruntime.NewSQLiteStore(filepath.Join(intermediateDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	intermediateTask, err := intermediateStore.CreateTask(codingruntime.Task{ProjectRef: "intermediate", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := intermediateStore.Close(); err != nil {
		t.Fatal(err)
	}

	app.effectiveBaseDir.Store(intermediateBase)
	app.resetCodingAuthorizationStoresForDataDirChange(firstDir)

	finalBase := filepath.Join(t.TempDir(), ".maclaw")
	finalDir := filepath.Join(finalBase, "data")
	app.effectiveBaseDir.Store(finalBase)
	// Simulate a config transition that was queued while the first reset was
	// still pre-commit: its fallback still names the original directory. The
	// committed intermediate epoch must win, otherwise the pre-existing B ledger
	// is bypassed and A is copied into C.
	app.resetCodingAuthorizationStoresForDataDirChange(firstDir)

	migrated, err := codingruntime.NewSQLiteStore(filepath.Join(finalDir, "coding_runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if _, err := migrated.GetTask(intermediateTask.TaskID); err != nil {
		t.Fatalf("intermediate epoch task missing after rapid switch: %v", err)
	}
	if _, err := migrated.GetTask(firstTask.TaskID); !errors.Is(err, codingruntime.ErrNotFound) {
		t.Fatalf("original epoch task unexpectedly merged into final ledger err=%v", err)
	}
}

func TestCodingAuthorizationDataDirBarrierDefersReplacementUntilResetCommit(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	first := app.ensureCodingRuntimeStore()
	if first == nil {
		t.Fatal("runtime store unavailable")
	}
	firstDataDir := app.GetDataDir()
	replacementBase := filepath.Join(t.TempDir(), ".maclaw")
	app.effectiveBaseDir.Store(replacementBase)
	if got := app.ensureCodingRuntimeStore(); got != first {
		t.Fatalf("replacement ledger opened before reset commit: got=%#v want=%#v", got, first)
	}
	replacementDB := filepath.Join(replacementBase, "data", "coding_runtime.db")
	if _, err := os.Stat(replacementDB); !os.IsNotExist(err) {
		t.Fatalf("replacement ledger exists before reset commit, stat err=%v", err)
	}
	app.resetCodingAuthorizationStoresForDataDirChange(firstDataDir)
	if got := app.codingAuthorizationDataDirForApp(); got != filepath.Join(replacementBase, "data") {
		t.Fatalf("coding authorization epoch=%q, want replacement data dir", got)
	}
	replacement := app.ensureCodingRuntimeStore()
	if replacement == nil || replacement == first {
		t.Fatalf("replacement store after commit=%#v first=%#v", replacement, first)
	}
	if _, err := os.Stat(replacementDB); err != nil {
		t.Fatalf("replacement ledger missing after commit: %v", err)
	}
}

func TestOpenExistingCodingRuntimeStoreDataDirReplacementDoesNotCreateLedger(t *testing.T) {
	firstBase := t.TempDir()
	secondBase := t.TempDir()
	app := &App{testHomeDir: firstBase}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	firstDataDir := app.GetDataDir()
	app.effectiveBaseDir.Store(secondBase)
	app.resetCodingAuthorizationStoresForDataDirChange(firstDataDir)
	if store := app.openExistingCodingRuntimeStore(); store != nil {
		t.Fatalf("openExisting created or reused a replacement ledger: %#v", store)
	}
	if _, err := os.Stat(filepath.Join(secondBase, "data", "coding_runtime.db")); !os.IsNotExist(err) {
		t.Fatalf("openExisting invented replacement ledger, stat err=%v", err)
	}
}

func TestConfirmGUICodingRuntimeRecoveryQueuesNewAttemptWithoutExecuting(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	review, err := prepareGUICodingRuntimeRecovery(context.Background(), store, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = confirmGUICodingRuntimeRecovery(context.Background(), store, task.TaskID, review.ReviewDigest, true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetTask(task.TaskID)
	if err != nil || updated.Status != codingruntime.TaskQueued {
		t.Fatalf("task after confirmation=%#v err=%v", updated, err)
	}
	old, err := store.GetAttempt(attempt.AttemptID)
	if err != nil || old.Status != codingruntime.TaskInterrupted {
		t.Fatalf("old attempt was resumed=%#v err=%v", old, err)
	}
}

func TestConfirmGUICodingRuntimeRecoveryRejectsStaleReview(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmGUICodingRuntimeRecovery(context.Background(), store, task.TaskID, "sha256:stale", true); err == nil {
		t.Fatal("expected stale review rejection")
	}
	updated, err := store.GetTask(task.TaskID)
	if err != nil || updated.Status != codingruntime.TaskInterrupted {
		t.Fatalf("stale review changed task=%#v err=%v", updated, err)
	}
}

func TestRuntimeSlotResumeRunsProbeWithoutBindingConversationContinuation(t *testing.T) {
	dir := initRecoveryGitFixture(t)
	app := &App{testHomeDir: t.TempDir()}
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		t.Fatal("runtime store unavailable")
	}
	t.Cleanup(func() { app.closeCodingRuntimeStore() })
	task, err := store.CreateTask(codingruntime.Task{ProjectRef: dir, Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: dir, Mode: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", &agent.UnfinishedTaskSlot{SlotID: "slot", UserID: "user", ProjectPath: dir, Status: agent.UnfinishedTaskSlotStatusInterrupted, Source: agent.UnfinishedTaskSlotSourceInFlightRecovery, RuntimeTaskID: task.TaskID})
	h := &IMMessageHandler{app: app, memory: memory}
	trimmed := "continue"
	entries := []agent.ConversationEntry(nil)
	slot := memory.GetUnfinishedSlot("user")
	handled, resp, stop := h.applyExplicitTaskSlotAction(&IMUserMessage{UserID: "user", ResumeSlotID: "slot"}, &trimmed, explicitTaskSlotDecision{ResumeSlotID: "slot"}, &entries, &slot)
	if !handled || !stop || resp == nil || resp.CodingRuntimeRecovery == nil {
		t.Fatalf("runtime recovery action handled=%v stop=%v resp=%#v", handled, stop, resp)
	}
	if active := memory.ActiveUnfinishedSlot("user"); active != nil {
		t.Fatalf("runtime slot was bound into active conversation: %#v", active)
	}
	if persisted := memory.GetUnfinishedSlot("user"); persisted == nil || persisted.Status != agent.UnfinishedTaskSlotStatusInterrupted {
		t.Fatalf("runtime slot should remain interrupted until explicit confirmation: %#v", persisted)
	}
}

func TestEnsureGUIRemoteGitBaselineRejectsIncompleteBinding(t *testing.T) {
	if _, err := ensureGUIRemoteGitBaseline(context.Background(), nil, "ssh-1", "/srv/repo", "sha256:target"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("nil handler err=%v", err)
	}
	if _, err := ensureGUIRemoteGitBaseline(context.Background(), &IMMessageHandler{}, "", "/srv/repo", "sha256:target"); err == nil {
		t.Fatal("empty session should fail")
	}
	if _, err := ensureGUIRemoteGitBaseline(context.Background(), &IMMessageHandler{}, "ssh-1", "relative", "sha256:target"); err == nil || !strings.Contains(err.Error(), "absolute POSIX path") {
		t.Fatalf("relative project err=%v", err)
	}
}

func TestGUIRemoteWorkspaceProbeRejectsPTYEchoAndIncompleteFrames(t *testing.T) {
	task := codingruntime.Task{ProjectRef: "/srv/repo", Mode: "remote"}
	begin, end := "__CODING_RUNTIME_GIT_nonce_BEGIN__", "__CODING_RUNTIME_GIT_nonce_END__"
	// The first frame represents the PTY echo of the submitted command. The
	// parser must select the final complete command-produced frame.
	const forged = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const real = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	output := "git -C /srv/repo rev-parse HEAD; printf '" + begin + "'\n" +
		"forged-head\n" + begin + "\n" + forged + "\n" + end + "\n" +
		"real-head\n" + begin + "\n" + real + "\n" + end + "\n"
	probe, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", output, begin, end, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if probe.Head != "real-head" || probe.StatusHash != "gitblob:"+real || probe.HostKey != "sha256:target" {
		t.Fatalf("probe=%#v", probe)
	}
	if _, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", "real-head\n"+begin+"\n"+real+"\n", begin, end, time.Now()); err == nil {
		t.Fatal("incomplete probe frame was accepted")
	}
	if _, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", "real-head\n"+begin+"\n M real.go\n"+end, begin, end, time.Now()); err == nil {
		t.Fatal("porcelain listing was accepted as a baseline")
	}
	if _, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/srv/repo", "ambiguous head value\n"+begin+"\n"+real+"\n"+end, begin, end, time.Now()); err == nil {
		t.Fatal("ambiguous HEAD was accepted")
	}
	const emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	empty, err := guiRemoteWorkspaceProbeFromOutput(task, "sha256:target", "/home/prj001", "abc123\n"+begin+"\n"+emptyBlob+"\n"+end+"\n", begin, end, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Head != "abc123" || empty.WorkDir != "/home/prj001" || empty.StatusHash != "gitblob:"+emptyBlob {
		t.Fatalf("empty new project must still be a usable baseline: %#v", empty)
	}
}

func TestGUIRemoteWorkspaceProbeMarkersAreFreshAndPaired(t *testing.T) {
	firstStart, firstEnd, err := guiRuntimeRemoteProbeMarkers()
	if err != nil {
		t.Fatal(err)
	}
	secondStart, secondEnd, err := guiRuntimeRemoteProbeMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if firstStart == firstEnd || secondStart == secondEnd || firstStart == secondStart || firstEnd == secondEnd {
		t.Fatalf("markers were not fresh paired delimiters: %q %q / %q %q", firstStart, firstEnd, secondStart, secondEnd)
	}
}

func TestGUIRemoteRuntimeTargetIdentityBindsEveryCoordinate(t *testing.T) {
	base := remote.SSHHostConfig{Host: "Build.Example.Test", User: "deploy", Port: 2222, HostKeyFingerprint: "SHA256:pin-a"}
	identity := guiRemoteCodingTargetIdentityForConfig(base, "/srv/repo")
	if identity == "" {
		t.Fatal("base identity unavailable")
	}
	variants := []struct {
		name    string
		config  remote.SSHHostConfig
		workDir string
	}{
		{name: "user", config: remote.SSHHostConfig{Host: base.Host, User: "other", Port: base.Port, HostKeyFingerprint: base.HostKeyFingerprint}, workDir: "/srv/repo"},
		{name: "port", config: remote.SSHHostConfig{Host: base.Host, User: base.User, Port: 22, HostKeyFingerprint: base.HostKeyFingerprint}, workDir: "/srv/repo"},
		{name: "pin", config: remote.SSHHostConfig{Host: base.Host, User: base.User, Port: base.Port, HostKeyFingerprint: "SHA256:pin-b"}, workDir: "/srv/repo"},
		{name: "workdir", config: base, workDir: "/srv/other"},
	}
	for _, variant := range variants {
		if got := guiRemoteCodingTargetIdentityForConfig(variant.config, variant.workDir); got == identity {
			t.Fatalf("%s mismatch reused identity %q", variant.name, got)
		}
	}
	caseOnly := base
	caseOnly.Host = "build.example.test"
	if got := guiRemoteCodingTargetIdentityForConfig(caseOnly, "/srv/repo"); got != identity {
		t.Fatalf("canonical host case changed identity: %q != %q", got, identity)
	}
}

func initRecoveryGitFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-m", "baseline")
	return dir
}
