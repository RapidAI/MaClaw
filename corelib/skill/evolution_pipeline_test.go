package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type observingMutationLocker struct {
	mu     sync.Mutex
	locked atomic.Bool
}

func (l *observingMutationLocker) Lock() {
	l.mu.Lock()
	l.locked.Store(true)
}

func (l *observingMutationLocker) Unlock() {
	l.locked.Store(false)
	l.mu.Unlock()
}

func (l *observingMutationLocker) IsLocked() bool { return l.locked.Load() }

func TestEvolutionPipelineMutationBoundaryRechecksAdmission(t *testing.T) {
	locker := &observingMutationLocker{}
	var admissionCalls atomic.Int32
	var callbacksOutsideLock atomic.Int32
	old := corelib.NLSkillEntry{Name: "mutation-boundary", Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, Description: "new"}
	p := NewEvolutionPipeline()
	p.MutationMutex = locker
	p.MutationAdmission = func(string) error {
		admissionCalls.Add(1)
		return nil
	}
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} }
	p.SkillSaver = func([]corelib.NLSkillEntry) error {
		if !locker.IsLocked() {
			callbacksOutsideLock.Add(1)
		}
		return nil
	}
	p.IndexRefresher = func() error {
		if !locker.IsLocked() {
			callbacksOutsideLock.Add(1)
		}
		return nil
	}
	p.FinalAuditor = func(string, map[string]string) error {
		if !locker.IsLocked() {
			callbacksOutsideLock.Add(1)
		}
		return nil
	}
	result := p.persistDefinitionChangeWithAudit(context.Background(), old.Name, after, "skill:test_commit", map[string]string{"action": "repair"})
	if result.State != "committed" {
		t.Fatalf("result = %+v, want committed", result)
	}
	if got := admissionCalls.Load(); got != 2 {
		t.Fatalf("admission calls = %d, want pre-lock and post-lock checks", got)
	}
	if got := callbacksOutsideLock.Load(); got != 0 {
		t.Fatalf("callbacks observed outside mutation lock: %d", got)
	}
}

func TestRepairCooldownFromHours(t *testing.T) {
	if got := RepairCooldownFromHours(0); got != DefaultRepairCooldown {
		t.Fatalf("0 hours = %v, want default %v", got, DefaultRepairCooldown)
	}
	if got := RepairCooldownFromHours(2); got != 2*time.Hour {
		t.Fatalf("2 hours = %v", got)
	}
}

func TestEvolutionPipeline_Status(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableRepair = true
	p.EnableOptimizer = true
	st := p.Status()
	if !st.EnableRepair || !st.EnableOptimizer {
		t.Fatalf("status flags = %+v", st)
	}
	if st.RepairCooldown != DefaultRepairCooldown {
		t.Fatalf("cooldown = %v", st.RepairCooldown)
	}
	if st.PendingSkills != 0 {
		t.Fatalf("pending = %d", st.PendingSkills)
	}
}

func TestSummarizeEvolutionCompensationErrorRedactsPaths(t *testing.T) {
	err := fmt.Errorf("decode compensation record %s: invalid snapshot at %s", `C:\Users\alice\.maclaw\skill_evolution\audit_pending.jsonl`, `/srv/private/secret.yaml`)
	got := SummarizeEvolutionCompensationError(err)
	if strings.Contains(got, "alice") || strings.Contains(got, "secret.yaml") || strings.Contains(got, "C:\\") || strings.Contains(got, "/srv/") {
		t.Fatalf("summary leaked path details: %q", got)
	}
	if got == "" {
		t.Fatal("summary is empty")
	}
}

func TestRetryEvolutionCompensationByIdentityCleansCommittedRecord(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	artifact := filepath.Join(base, "staging", "retry-me.tmp")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("manual-retry", "retry-skill", "repair", "", nil, false, nil, "cleanup retry exhausted")
	record.TransactionState = "committed"
	record.CleanupStatus = "needs_review"
	record.Status = "needs_review"
	record.SetPostCommitCleanupPaths([]string{artifact})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	got, recovered, err := RetryEvolutionCompensation("manual-retry", "retry-skill", "repair", NewEvolutionPipeline())
	if err != nil {
		t.Fatal(err)
	}
	if !recovered || got.RequestID != "manual-retry" {
		t.Fatalf("retry result=%+v recovered=%v", got, recovered)
	}
	if got.CleanupStatus != "clear" || got.TransactionState != "committed" || got.FailureReason != "recovered" {
		t.Fatalf("retry result summary = %+v, want explicit committed/clear recovered terminal state", got)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("cleanup artifact still exists, err=%v", err)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("queue after manual retry: records=%v err=%v", records, err)
	}
	events, err := ListEvolutionAudit(DefaultEvolutionAuditPath(), EvolutionAuditMaxKeep)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "compensation_manual_retry" && event.RequestID == "manual-retry" && event.Decision == "requested" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("manual retry audit event missing: %+v", events)
	}
}

func TestRetryEvolutionCompensationByIdentityLeavesSiblingRecordPending(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	firstArtifact := filepath.Join(base, "first.tmp")
	secondArtifact := filepath.Join(base, "second.tmp")
	if err := os.WriteFile(firstArtifact, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondArtifact, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := NewEvolutionCompensationRecord("exact-first", "same-skill", "repair", "", nil, false, nil, "pending")
	first.TransactionState = "committed"
	first.SetPostCommitCleanupPaths([]string{firstArtifact})
	second := NewEvolutionCompensationRecord("exact-second", "sibling-skill", "repair", "", nil, false, nil, "pending")
	second.TransactionState = "committed"
	second.SetPostCommitCleanupPaths([]string{secondArtifact})
	if err := PersistEvolutionCompensation(first); err != nil {
		t.Fatal(err)
	}
	if err := PersistEvolutionCompensation(second); err != nil {
		t.Fatal(err)
	}
	if _, recovered, err := RetryEvolutionCompensation("exact-first", "same-skill", "repair", NewEvolutionPipeline()); err != nil || !recovered {
		t.Fatalf("retry target: recovered=%v err=%v", recovered, err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 || records[0].RequestID != "exact-second" {
		t.Fatalf("sibling queue record lost: records=%v err=%v", records, err)
	}
	if _, err := os.Stat(secondArtifact); err != nil {
		t.Fatalf("sibling artifact unexpectedly removed: %v", err)
	}
}

func TestRetryEvolutionCompensationReturnsUpdatedPendingSummary(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := NewEvolutionCompensationRecord("manual-retry-failed", "retry-failed", "repair", "", nil, false, nil, "cleanup pending")
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	pipeline := NewEvolutionPipeline()
	pipeline.CommittedCleanup = func(EvolutionCompensationRecord) error { return fmt.Errorf("cleanup unavailable") }
	got, recovered, err := RetryEvolutionCompensation("manual-retry-failed", "retry-failed", "repair", pipeline)
	if err != nil {
		t.Fatalf("RetryEvolutionCompensation() error = %v", err)
	}
	if recovered {
		t.Fatal("failed cleanup was reported as recovered")
	}
	if got.Attempts != 1 || got.TransactionState != "committed" || got.CleanupStatus != "pending" {
		t.Fatalf("retry summary = %+v, want committed/pending with attempts=1", got)
	}
	if got.LastError == "" {
		t.Fatal("retry summary omitted durable cleanup error")
	}
}

func TestClearEvolutionCompensationManuallyRejectsUncommittedRecord(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := NewEvolutionCompensationRecord("manual-clear", "clear-skill", "install", "", nil, false, nil, "rollback pending")
	record.TransactionState = "audit_pending"
	record.Status = "needs_review"
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	if _, err := ClearEvolutionCompensationManually("manual-clear", "clear-skill", "install"); err == nil {
		t.Fatal("manual clear accepted uncommitted record")
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("queue after rejected clear: records=%v err=%v", records, err)
	}
}

func TestClearEvolutionCompensationManuallyRequiresArtifactsAbsent(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	artifact := filepath.Join(base, "staging", "leftover.tmp")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("manual-clear-artifact", "clear-artifact", "install", "", nil, false, nil, "cleanup pending")
	record.TransactionState = "committed"
	record.CleanupStatus = "needs_review"
	record.SetPostCommitCleanupPaths([]string{artifact})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	if _, err := ClearEvolutionCompensationManually("manual-clear-artifact", "clear-artifact", "install"); err == nil {
		t.Fatal("manual clear removed record while cleanup artifact exists")
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 1 {
		t.Fatalf("queue after unsafe clear: records=%v err=%v", records, err)
	}
}

func TestClearEvolutionCompensationManuallyReturnsClearedTerminalSummary(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := NewEvolutionCompensationRecord("manual-clear-ok", "clear-ok", "install", "", nil, false, nil, "cleanup pending")
	record.TransactionState = "committed"
	record.CleanupStatus = "needs_review"
	record.Status = "needs_review"
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	got, err := ClearEvolutionCompensationManually("manual-clear-ok", "clear-ok", "install")
	if err != nil {
		t.Fatalf("ClearEvolutionCompensationManually() error = %v", err)
	}
	if got.TransactionState != "committed" || got.CleanupStatus != "clear" || got.FailureReason != "cleared_manually" || got.Status != "" {
		t.Fatalf("clear summary = %+v, want committed/clear terminal state", got)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("queue after manual clear: records=%v err=%v", records, err)
	}
}

func TestEvolutionPipeline_StatusIncludesFailureSummary(t *testing.T) {
	p := NewEvolutionPipeline()
	p.recordFailure(evolutionRequest{
		SkillName: "broken",
		RunArgs:   map[string]string{"input": "secret-value"},
		ExecResult: &SkillExecutionResultCompat{
			Success:    false,
			Error:      "[class: dependency] missing package",
			ErrorClass: "dependency",
		},
	})
	status := p.Status()
	if len(status.FailureSummaries) != 1 {
		t.Fatalf("failure summaries = %+v", status.FailureSummaries)
	}
	summary := status.FailureSummaries[0]
	if summary.Skill != "broken" || summary.FailureCount != 1 || summary.LastErrorClass != "dependency" {
		t.Fatalf("unexpected failure summary: %+v", summary)
	}
	if summary.LastArgsDigest == "" || strings.Contains(summary.LastArgsDigest, "secret-value") {
		t.Fatalf("arguments were not redacted: %+v", summary)
	}
}

func TestEvolutionCompensationReadModifyWriteIsSerialized(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record := NewEvolutionCompensationRecord(
				fmt.Sprintf("concurrent-%d", i), fmt.Sprintf("skill-%d", i),
				"concurrent_test", "", nil, false, nil, "test")
			if err := PersistEvolutionCompensation(record); err != nil {
				errs <- err
				return
			}
			record.TransactionState = "committed"
			record.CleanupStatus = "pending"
			if err := ReplaceEvolutionCompensation(record); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	records, err := readEvolutionCompensations()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		seen[record.RequestID] = true
	}
	for i := 0; i < workers; i++ {
		if !seen[fmt.Sprintf("concurrent-%d", i)] {
			t.Fatalf("concurrent replacement lost request concurrent-%d; got %d records", i, len(records))
		}
	}
}

func TestEvolutionPipeline_DifferentSkillsRunConcurrently(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = time.Millisecond
	p.MaxConcurrentWorkers = 2
	p.EnableOptimizer = false
	p.EnablePromoter = false
	var active, maxActive atomic.Int32
	p.RepairHook = func(_ *corelib.NLSkillEntry, _ map[string]string) {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		active.Add(-1)
	}
	p.Start()
	defer p.Stop()
	for _, name := range []string{"parallel-a", "parallel-b"} {
		entry := &corelib.NLSkillEntry{
			Name: name, Source: "hub", Status: "active", UsageCount: 1,
			LastError: "[class: command_not_found] missing",
			Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "missing"}}},
		}
		p.NotifySkillExecution(name, entry, &SkillExecutionResultCompat{Success: false}, nil)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && maxActive.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if p.requestCount.Load() < 2 {
		t.Fatalf("requests not processed: %d", p.requestCount.Load())
	}
	if maxActive.Load() < 2 {
		t.Fatalf("different skills did not run concurrently, max_active=%d", maxActive.Load())
	}
}

func TestEvolutionPipeline_CancelPendingSkill(t *testing.T) {
	p := NewEvolutionPipeline()
	p.NotifySkillExecution("pending-cancel", &corelib.NLSkillEntry{Name: "pending-cancel"}, &SkillExecutionResultCompat{Success: true}, nil)
	if !p.CancelSkill("pending-cancel") {
		t.Fatal("CancelSkill should remove a pending request")
	}
	if p.PendingSkillCount() != 0 {
		t.Fatalf("pending count=%d after cancellation", p.PendingSkillCount())
	}
	if p.CancelSkill("pending-cancel") {
		t.Fatal("second cancellation should be idempotent")
	}
}

func TestEvolutionPipeline_CancelActiveSkill(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false
	started := make(chan struct{})
	finished := make(chan struct{})
	p.RepairHook = func(_ *corelib.NLSkillEntry, _ map[string]string) {
		close(started)
		<-finished
	}
	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("active-cancel", &corelib.NLSkillEntry{
		Name: "active-cancel", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "missing"}}},
	}, &SkillExecutionResultCompat{Success: false}, nil)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("active worker did not start")
	}
	if !p.CancelSkill("active-cancel") {
		t.Fatal("CancelSkill should signal active worker")
	}
	close(finished)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && p.Status().ActiveSkills != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := p.Status().CancelledRequests; got == 0 {
		t.Fatal("cancellation metric was not updated")
	}
}

func TestEvolutionPipeline_CancelActiveContextAwareHook(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false
	started := make(chan struct{})
	cancelled := make(chan struct{})
	p.RepairHookWithContext = func(ctx context.Context, _ *corelib.NLSkillEntry, _ map[string]string) {
		close(started)
		<-ctx.Done()
		close(cancelled)
	}
	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("context-cancel", &corelib.NLSkillEntry{
		Name: "context-cancel", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing",
	}, &SkillExecutionResultCompat{Success: false}, nil)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("context-aware hook did not start")
	}
	if !p.CancelSkill("context-cancel") {
		t.Fatal("CancelSkill should signal context-aware hook")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("context-aware hook did not observe cancellation")
	}
}

func TestEvolutionPipeline_WorkerTimeoutEmitsEvent(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = time.Millisecond
	p.WorkerTimeout = 25 * time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false
	started := make(chan struct{})
	done := make(chan struct{})
	events := make(chan string, 4)
	p.RepairHookWithContext = func(ctx context.Context, _ *corelib.NLSkillEntry, _ map[string]string) {
		close(started)
		<-ctx.Done()
		close(done)
	}
	p.EventEmitter = func(event string, _ map[string]string) { events <- event }
	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("timeout-skill", &corelib.NLSkillEntry{
		Name: "timeout-skill", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing",
	}, &SkillExecutionResultCompat{Success: false}, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timeout worker did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout context was not observed")
	}
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event == EventSkillEvolutionTimedOut {
				if p.Status().TimedOutRequests == 0 {
					t.Fatal("timeout event emitted before timeout metric was updated")
				}
				return
			}
		case <-deadline:
			t.Fatalf("timeout event not emitted; status=%+v", p.Status())
		}
	}
}

func TestEvolutionPipeline_LifecycleEventsCarryCorrelationAndTermination(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = time.Millisecond
	p.WorkerTimeout = 20 * time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false
	started := make(chan struct{})
	events := make(chan map[string]string, 8)
	p.RepairHookWithContext = func(ctx context.Context, _ *corelib.NLSkillEntry, _ map[string]string) {
		close(started)
		<-ctx.Done()
	}
	p.EventEmitter = func(_ string, data map[string]string) { events <- data }
	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("correlated-timeout", &corelib.NLSkillEntry{
		Name: "correlated-timeout", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing",
	}, &SkillExecutionResultCompat{Success: false, ErrorClass: "command_not_found"}, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	deadline := time.After(time.Second)
	for {
		select {
		case data := <-events:
			if data["request_id"] == "" || data["schema_version"] != "2" || data["config_revision"] == "" {
				t.Fatalf("missing correlation metadata: %#v", data)
			}
			if data["termination"] == "worker_timeout" {
				if data["failure_reason"] != "deadline_exceeded" || data["reason"] != "worker_deadline" {
					t.Fatalf("bad timeout metadata: %#v", data)
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout lifecycle event not observed")
		}
	}
}

func TestEventConstantsStable(t *testing.T) {
	// Guard against accidental renames that would desync GUI/frontend listeners.
	if EventSkillRepaired != "skill:repaired" || EventSkillOptimized != "skill:optimized" {
		t.Fatalf("event constants drifted: repaired=%q optimized=%q", EventSkillRepaired, EventSkillOptimized)
	}
}

func TestNotifySkillExecution_CoalescesSameSkill(t *testing.T) {
	p := NewEvolutionPipeline()
	// Don't Start — we only exercise the coalesce map + wake path.
	entry := &corelib.NLSkillEntry{
		Name:  "demo",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo 1"}}},
	}

	p.NotifySkillExecution("demo", entry, &SkillExecutionResultCompat{Success: true}, map[string]string{"input": "a"})
	p.NotifySkillExecution("demo", entry, &SkillExecutionResultCompat{Success: false}, map[string]string{"input": "b"})
	p.NotifySkillExecution("other", entry, &SkillExecutionResultCompat{Success: true}, nil)

	if got := p.PendingSkillCount(); got != 2 {
		t.Fatalf("PendingSkillCount = %d, want 2 (demo coalesced + other)", got)
	}
	if n := p.CoalescedNotifications.Load(); n != 1 {
		t.Fatalf("CoalescedNotifications = %d, want 1", n)
	}

	// Latest demo request should win.
	p.pendingMu.Lock()
	demo := p.pendingBySkill["demo"]
	p.pendingMu.Unlock()
	if demo.ExecResult == nil || demo.ExecResult.Success {
		t.Fatal("expected latest demo result Success=false")
	}
	if demo.RunArgs["input"] != "b" {
		t.Fatalf("RunArgs.input = %q, want b", demo.RunArgs["input"])
	}
}

func TestEvolutionPipeline_TakeAllPendingClearsMap(t *testing.T) {
	p := NewEvolutionPipeline()
	entry := &corelib.NLSkillEntry{Name: "x"}
	p.NotifySkillExecution("x", entry, nil, nil)
	batch := p.takeAllPending()
	if len(batch) != 1 {
		t.Fatalf("batch len = %d", len(batch))
	}
	if p.PendingSkillCount() != 0 {
		t.Fatal("map should be empty after takeAllPending")
	}
}

func TestEvolutionPipeline_tryRepair_InvokesHook(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	p.PostExecDelay = time.Millisecond

	var hooked atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		if entry == nil || entry.Name != "broken" {
			t.Errorf("unexpected entry: %#v", entry)
		}
		if runArgs["input"] != "x" {
			t.Errorf("runArgs = %#v", runArgs)
		}
		hooked.Add(1)
	}

	entry := &corelib.NLSkillEntry{
		Name:         "broken",
		Source:       "hub",
		Status:       "active",
		UsageCount:   1,
		SuccessCount: 0,
		LastError:    "[class: command_not_found] missing foo",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	// ShouldAttemptRepair: hub source + usage<=2 + repairable class.
	if !ShouldAttemptRepair(entry) {
		t.Fatal("fixture should be repair-eligible")
	}

	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("broken", entry, &SkillExecutionResultCompat{Success: false}, map[string]string{"input": "x"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hooked.Load() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("RepairHook not called, hooked=%d", hooked.Load())
}

func TestEvolutionPipeline_tryRepair_Throttles(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	p.RepairCooldown = time.Hour
	var calls atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		calls.Add(1)
	}
	entry := &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, LastError: "[class: command_not_found] x",
	}
	// First call records throttle timestamp.
	p.tryRepair(context.Background(), evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	// Immediate second call should be throttled.
	p.tryRepair(context.Background(), evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (second throttled)", calls.Load())
	}
}

func TestEvolutionPipeline_LoopProcessesCoalescedBatch(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = 20 * time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false

	var processed atomic.Int32
	// processRequest always increments requestCount; use that + short run.
	p.Start()
	defer p.Stop()

	entry := &corelib.NLSkillEntry{Name: "batch-skill"}
	p.NotifySkillExecution("batch-skill", entry, &SkillExecutionResultCompat{Success: true}, nil)
	p.NotifySkillExecution("batch-skill", entry, &SkillExecutionResultCompat{Success: true}, nil)

	// Wait for delay + process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.PendingSkillCount() == 0 && p.requestCount.Load() >= 1 {
			processed.Store(int32(p.requestCount.Load()))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if processed.Load() < 1 {
		t.Fatalf("expected at least 1 processed request, requestCount=%d pending=%d", p.requestCount.Load(), p.PendingSkillCount())
	}
	// Coalesce means one process for two notifies of same skill (after delay drain).
	if p.requestCount.Load() != 1 {
		// If wake happened twice somehow, still ok if coalesced before first take —
		// but typically 1. Allow 1–2 for race of second notify after first take.
		if p.requestCount.Load() > 2 {
			t.Fatalf("requestCount = %d, want 1 or 2", p.requestCount.Load())
		}
	}
}

func TestTriggerOptimize_Guards(t *testing.T) {
	ctx := context.Background()
	entry := &corelib.NLSkillEntry{Name: "opt-me", UsageCount: 2, SuccessCount: 1}

	// nil pipeline
	res := (*EvolutionPipeline)(nil).TriggerOptimize(ctx, entry, true)
	if res.SkipReason == "" {
		t.Fatal("nil pipeline should set SkipReason")
	}

	// missing optimizer
	p := NewEvolutionPipeline()
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p.UsageTracker = tracker
	res = p.TriggerOptimize(ctx, entry, true)
	if !strings.Contains(res.SkipReason, "optimizer") {
		t.Fatalf("missing optimizer SkipReason=%q", res.SkipReason)
	}

	// missing tracker
	p2 := NewEvolutionPipeline()
	p2.Optimizer = NewSkillOptimizer(nil, nil, nil)
	res = p2.TriggerOptimize(ctx, entry, true)
	if !strings.Contains(res.SkipReason, "usage tracker") {
		t.Fatalf("missing tracker SkipReason=%q", res.SkipReason)
	}

	// file-backed hard block
	p3 := NewEvolutionPipeline()
	p3.UsageTracker = tracker
	p3.Optimizer = NewSkillOptimizer(nil, nil, nil)
	fileEntry := &corelib.NLSkillEntry{
		Name: "file-skill", Source: "file", SkillDir: "/tmp/skill",
		UsageCount: 10, SuccessCount: 7,
	}
	res = p3.TriggerOptimize(ctx, fileEntry, true)
	if !strings.Contains(res.SkipReason, "file-backed") {
		t.Fatalf("file-backed SkipReason=%q", res.SkipReason)
	}

	// Agent-guided Markdown project workflows need interactive orchestration,
	// not an LLM rewrite of their single craft_tool adapter step.
	workflowEntry := &corelib.NLSkillEntry{
		Name: "Book-PDF", Source: "clawhub", Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "Phase 1 research with multiple background agents; confirm with the user; use templates/ and scripts/; maintain version.json."},
		}},
	}
	res = p3.TriggerOptimize(ctx, workflowEntry, true)
	if !strings.Contains(res.SkipReason, "agent-guided") {
		t.Fatalf("agent-guided SkipReason=%q", res.SkipReason)
	}
}

func TestTriggerOptimize_SkipsThresholdUnlessForce(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	// nil LLM is fine — ShouldOptimize rejects before Optimize when force=false
	p.Optimizer = NewSkillOptimizer(nil, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "low-usage", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1, // below MinUsageCount=8
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	res := p.TriggerOptimize(context.Background(), entry, false)
	if !res.Skipped || res.Attempted {
		t.Fatalf("expected skip without force, got %+v", res)
	}
	if !strings.Contains(res.SkipReason, "threshold") {
		t.Fatalf("SkipReason=%q", res.SkipReason)
	}
}

func TestTriggerOptimize_ForceAttemptsLLM(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	// configured mock that returns no optimization
	llm := &mockLLMRepairer{
		response: `{"optimized": false, "explanation": "nothing to improve"}`,
	}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "force-opt", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1, // would fail ShouldOptimize
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	res := p.TriggerOptimize(context.Background(), entry, true)
	if res.Skipped {
		t.Fatalf("force should not skip thresholds, got %+v", res)
	}
	if !res.Attempted {
		t.Fatalf("expected Attempted, got %+v", res)
	}
	if res.Optimized {
		t.Fatalf("mock returned optimized=false, got %+v", res)
	}
	if !strings.Contains(res.Explanation, "nothing to improve") {
		t.Fatalf("Explanation=%q", res.Explanation)
	}
}

func TestTriggerOptimize_ForceAppliesAndEmits(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker

	llm := &mockLLMRepairer{
		response: `{
  "optimized": true,
  "explanation": "tighter command",
  "new_description": "improved",
  "new_steps": [{"action":"bash","params":{"command":"echo better"},"on_error":"stop"}]
}`,
	}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	var saved []corelib.NLSkillEntry
	var saveCalls int
	p.SkillLoader = func() []corelib.NLSkillEntry {
		return []corelib.NLSkillEntry{{
			Name: "apply-opt", Source: "learned", Status: "active",
			UsageCount: 2, SuccessCount: 1,
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		}}
	}
	p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
		saveCalls++
		saved = append([]corelib.NLSkillEntry(nil), skills...)
		return nil
	}
	var emitted string
	p.EventEmitter = func(event string, payload map[string]string) {
		if event == EventSkillOptimized {
			emitted = payload["skill"]
		}
	}

	entry := &corelib.NLSkillEntry{
		Name: "apply-opt", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	res := p.TriggerOptimize(context.Background(), entry, true)
	if !res.Optimized || !res.Attempted {
		t.Fatalf("expected optimized result, got %+v", res)
	}
	if entry.OptimizationCount != 1 || entry.Description != "improved" {
		t.Fatalf("entry not updated: count=%d desc=%q", entry.OptimizationCount, entry.Description)
	}
	if len(saved) != 1 || saved[0].Name != "apply-opt" {
		t.Fatalf("SkillSaver not called with updated skill: %#v", saved)
	}
	if emitted != "apply-opt" {
		t.Fatalf("EventSkillOptimized not emitted, skill=%q", emitted)
	}
}

func TestTriggerOptimize_ThrottleBypassedByForce(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	llm := &mockLLMRepairer{
		response: `{"optimized": false, "explanation": "noop"}`,
	}
	// Pre-seed throttle as if auto path just ran.
	p.optimizeAttempts = map[string]time.Time{"throttled": time.Now()}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "throttled", Source: "learned", Status: "active",
		UsageCount: 10, SuccessCount: 7,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	// Without force: either threshold skip (no retry evidence) or 24h throttle skip.
	resAuto := p.runOptimize(context.Background(), evolutionRequest{SkillName: "throttled", Entry: entry}, false)
	if resAuto.Attempted {
		t.Fatalf("auto path should not attempt while throttled/under threshold, got %+v", resAuto)
	}

	resForce := p.TriggerOptimize(context.Background(), entry, true)
	if resForce.Skipped && !resForce.Attempted {
		t.Fatalf("force should bypass throttle/threshold, got %+v", resForce)
	}
	if !resForce.Attempted {
		t.Fatalf("force should attempt LLM, got %+v", resForce)
	}
}

func TestEvolutionPipeline_tryRepair_CancelledCtxSkipsCooldown(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	var calls atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		calls.Add(1)
	}
	entry := &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, LastError: "[class: command_not_found] x",
	}
	req := evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tryRepair(ctx, req)

	if calls.Load() != 0 {
		t.Fatalf("hook called %d times with cancelled ctx", calls.Load())
	}
	p.throttleMu.Lock()
	_, recorded := p.repairAttempts["broken"]
	p.throttleMu.Unlock()
	if recorded {
		t.Fatal("cancelled attempt must not consume repair cooldown")
	}

	// 冷却未被消耗：立即重试应直接发起修复。
	p.tryRepair(context.Background(), req)
	if calls.Load() != 1 {
		t.Fatalf("retry after cancelled attempt should run hook once, got %d", calls.Load())
	}
}

func TestEvolutionPipeline_tryRepair_DisableUsesDurableCommitter(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	stored := corelib.NLSkillEntry{
		Name: "unfixable", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing binary",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "missing"}}},
	}
	p := NewEvolutionPipeline()
	p.LLM = &stubRepairLLM{respond: `{"repaired":false,"should_disable":true,"explanation":"dependency permanently removed"}`}
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{stored} }
	p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
		if len(skills) != 1 {
			return fmt.Errorf("unexpected skill count %d", len(skills))
		}
		stored = skills[0]
		return nil
	}
	p.FinalAuditor = func(string, map[string]string) error { return nil }
	var disabled atomic.Int32
	p.EventEmitter = func(event string, _ map[string]string) {
		if event == EventSkillRepairDisabled {
			disabled.Add(1)
		}
	}
	entry := CloneNLSkillEntry(&stored)
	p.tryRepair(context.Background(), evolutionRequest{
		RequestID: "req-disable", SkillName: entry.Name, Entry: entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	if stored.Status != "needs_review" || disabled.Load() != 1 {
		t.Fatalf("stored=%#v disabled_events=%d, want durable needs_review + one event", stored, disabled.Load())
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("disable compensation records = %+v, err=%v; want cleared", records, err)
	}
}

func TestEvolutionPipeline_tryRepair_DisablePersistenceFailureDoesNotReportSuccess(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	entry := &corelib.NLSkillEntry{
		Name: "unfixable-failure", Source: "hub", Status: "active", UsageCount: 1,
		LastError: "[class: command_not_found] missing binary",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "missing"}}},
	}
	p := NewEvolutionPipeline()
	p.LLM = &stubRepairLLM{respond: `{"repaired":false,"should_disable":true,"explanation":"dependency permanently removed"}`}
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{*entry} }
	p.SkillSaver = func([]corelib.NLSkillEntry) error { return fmt.Errorf("injected persistence outage") }
	p.FinalAuditor = func(string, map[string]string) error { return nil }
	var successEvents atomic.Int32
	p.EventEmitter = func(event string, _ map[string]string) {
		if event == EventSkillRepairDisabled {
			successEvents.Add(1)
		}
	}
	p.tryRepair(context.Background(), evolutionRequest{
		RequestID: "req-disable-failure", SkillName: entry.Name, Entry: entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	if entry.Status != "active" {
		t.Fatalf("failed disable mutated in-memory entry to %q", entry.Status)
	}
	if successEvents.Load() != 0 {
		t.Fatalf("disable success event emitted after persistence failure: %d", successEvents.Load())
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 1 || records[0].TransactionState != "audit_pending" || records[0].Attempts == 0 {
		t.Fatalf("disable persistence failure compensation = %+v, err=%v; want durable blocker", records, err)
	}
}

func TestRunOptimize_NotPersistedSkipsEventAndUpload(t *testing.T) {
	newPipeline := func(t *testing.T, withSaver bool) (*EvolutionPipeline, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
		tracker, err := tool.NewUsageTracker("")
		if err != nil {
			t.Fatal(err)
		}
		p := NewEvolutionPipeline()
		p.UsageTracker = tracker
		p.Optimizer = NewSkillOptimizer(&mockLLMRepairer{
			response: `{"optimized": true, "explanation": "tighter command", "new_steps": [{"action":"bash","params":{"command":"echo better"},"on_error":"stop"}]}`,
		}, nil, nil)
		// SkillLoader 列表里找不到该技能。
		p.SkillLoader = func() []corelib.NLSkillEntry { return nil }
		var saved, events, uploads atomic.Int32
		if withSaver {
			p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
				saved.Add(1)
				return nil
			}
		}
		p.EventEmitter = func(event string, payload map[string]string) { events.Add(1) }
		p.UploadTrigger = func(skillName string, result *SkillExecutionResultCompat) { uploads.Add(1) }
		return p, &saved, &events, &uploads
	}
	entry := func() *corelib.NLSkillEntry {
		return &corelib.NLSkillEntry{
			Name: "ghost", Source: "learned", Status: "active",
			UsageCount: 10, SuccessCount: 5,
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		}
	}

	t.Run("skill not found in storage", func(t *testing.T) {
		p, saved, events, uploads := newPipeline(t, true)
		res := p.TriggerOptimize(context.Background(), entry(), true)
		if !res.Attempted || res.Optimized {
			t.Fatalf("result = %+v, want attempted but not optimized", res)
		}
		if !strings.Contains(res.Explanation, "not persisted") {
			t.Fatalf("Explanation = %q", res.Explanation)
		}
		if saved.Load() != 0 || events.Load() != 0 || uploads.Load() != 0 {
			t.Fatalf("saved=%d events=%d uploads=%d, want all 0", saved.Load(), events.Load(), uploads.Load())
		}
	})

	t.Run("SkillSaver nil", func(t *testing.T) {
		p, _, events, uploads := newPipeline(t, false)
		res := p.TriggerOptimize(context.Background(), entry(), true)
		if !res.Attempted || res.Optimized {
			t.Fatalf("result = %+v, want attempted but not optimized", res)
		}
		if !strings.Contains(res.Explanation, "not persisted") {
			t.Fatalf("Explanation = %q", res.Explanation)
		}
		if events.Load() != 0 || uploads.Load() != 0 {
			t.Fatalf("events=%d uploads=%d, want all 0", events.Load(), uploads.Load())
		}
	})
}

func TestPersistDefinitionChange_RollsBackYAMLWhenConfigSaveFails(t *testing.T) {
	dir := t.TempDir()
	original := []byte("name: tx-skill\ndescription: old\nsteps:\n  - action: bash\n    params:\n      command: echo old\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{Name: "tx-skill", SkillDir: dir, Description: "new", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}}}
	p := NewEvolutionPipeline()
	p.SkillLoader = func() []corelib.NLSkillEntry {
		return []corelib.NLSkillEntry{{Name: "tx-skill", SkillDir: dir, Description: "old", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}}}}
	}
	var saveCalls int
	p.SkillSaver = func([]corelib.NLSkillEntry) error {
		saveCalls++
		if saveCalls == 1 {
			return fmt.Errorf("injected config failure")
		}
		return nil
	}
	got := p.persistDefinitionChange(context.Background(), entry.Name, entry)
	if got.State != "rolled_back" || !got.RollbackComplete || saveCalls != 2 {
		t.Fatalf("commit result = %+v, want rolled_back", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("YAML changed after config failure: %q", string(data))
	}
}

func TestPersistDefinitionChange_RollsBackConfigWhenYAMLWriteFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: tx-yaml-fail\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "tx-yaml-fail", SkillDir: dir, Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}}}
	var saved []corelib.NLSkillEntry
	var saveCalls int
	p := NewEvolutionPipeline()
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} }
	p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
		saveCalls++
		saved = append([]corelib.NLSkillEntry(nil), skills...)
		return nil
	}
	p.DefinitionWriter = func(*corelib.NLSkillEntry) error { return fmt.Errorf("injected YAML failure") }
	got := p.persistDefinitionChange(context.Background(), after.Name, after)
	if got.State != "rolled_back" {
		t.Fatalf("commit result = %+v, want rolled_back", got)
	}
	if saveCalls != 2 {
		t.Fatalf("save calls = %d, want forward and rollback", saveCalls)
	}
	if saved[len(saved)-1].Description != "old" {
		t.Fatalf("config rollback description = %q, want old", saved[len(saved)-1].Description)
	}
}

func TestPersistDefinitionChange_RollsBackOnFinalAuditFailure(t *testing.T) {
	dir := t.TempDir()
	originalYAML := []byte("name: tx-audit\ndescription: old\nsteps:\n  - action: bash\n    params:\n      command: echo old\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), originalYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "tx-audit", SkillDir: dir, Description: "old", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}}}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}}}
	var saveCalls int
	p := NewEvolutionPipeline()
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} }
	p.SkillSaver = func([]corelib.NLSkillEntry) error { saveCalls++; return nil }
	p.DefinitionWriter = func(entry *corelib.NLSkillEntry) error { return WriteBackOptimizedSteps(entry) }
	p.FinalAuditor = func(string, map[string]string) error { return fmt.Errorf("injected final audit failure") }
	got := p.persistDefinitionChangeWithAudit(context.Background(), after.Name, after, "skill:test_commit", map[string]string{"skill": after.Name})
	if got.State != "rolled_back" || !got.RollbackComplete || saveCalls != 2 {
		t.Fatalf("commit result = %+v, saveCalls=%d, want complete rollback", got, saveCalls)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(originalYAML) {
		t.Fatalf("YAML changed after final audit failure: %q", data)
	}
}

func TestSkillCommitter_CommitsOnlyAfterAuditAndCleansCompensation(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: committer-skill\ndescription: old\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "committer-skill", SkillDir: dir, Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"}
	var saved []corelib.NLSkillEntry
	var auditorCalls int
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver: func(skills []corelib.NLSkillEntry) error {
			saved = cloneSkillEntries(skills)
			return nil
		},
		DefinitionWriter: WriteBackOptimizedSteps,
		IndexRefresher:   func() error { return nil },
		FinalAuditor: func(event string, data map[string]string) error {
			auditorCalls++
			if event != "skill:test_commit" || data["skill"] != old.Name {
				t.Fatalf("audit = event:%q data:%+v", event, data)
			}
			return nil
		},
		ConfigRevision: "sha256:test-policy",
	}
	ctx := WithEvolutionRequestMetadata(context.Background(), "req-committer", 1)
	got := committer.Commit(ctx, old.Name, after, "skill:test_commit", map[string]string{"skill": old.Name, "action": "repair"})
	if got.State != "committed" || got.CleanupStatus != "clear" || !got.RollbackComplete || got.RequestID != "req-committer" || got.ConfigRevision != "sha256:test-policy" {
		t.Fatalf("commit result = %+v", got)
	}
	if auditorCalls != 1 || len(saved) != 1 || saved[0].Description != "new" {
		t.Fatalf("auditorCalls=%d saved=%+v", auditorCalls, saved)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("compensation records = %+v, err=%v; want cleaned", records, err)
	}
}

func TestSkillCommitter_CleansDurablePostCommitArtifactsBeforeClearingQueue(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	artifact := filepath.Join(base, "staging", "post-commit-artifact")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "post-commit-cleanup", Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, Description: "new"}
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		CompensationMutator: func(record *EvolutionCompensationRecord) {
			record.SetPostCommitCleanupPaths([]string{artifact})
		},
		SkipDefinitionBackup: true,
		FinalAuditor:         func(string, map[string]string) error { return nil },
	}
	result := committer.Commit(context.Background(), old.Name, after, "skill:test_post_commit_cleanup", map[string]string{"skill": old.Name, "action": "test"})
	if result.State != "committed" || result.CleanupStatus != "clear" {
		t.Fatalf("commit result = %+v, want committed/clear", result)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("post-commit artifact remains after queue clear: %v", err)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("compensation records = %+v, err=%v; want cleaned", records, err)
	}
}

func TestSkillCommitter_NoChangeSkipsPersistenceAndAudit(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committer-noop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := corelib.NLSkillEntry{
		Name: "committer-noop", SkillDir: dir, Description: "same",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo same"}}},
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: committer-noop\ndescription: same\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := struct{ save, index, audit int }{}
	committer := &SkillCommitter{
		SkillLoader:      func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{entry} },
		SkillSaver:       func([]corelib.NLSkillEntry) error { calls.save++; return nil },
		DefinitionWriter: func(*corelib.NLSkillEntry) error { t.Fatal("no-op wrote YAML"); return nil },
		IndexRefresher:   func() error { calls.index++; return nil },
		FinalAuditor:     func(string, map[string]string) error { calls.audit++; return nil },
		SkipIfUnchanged:  true,
	}
	result := committer.Commit(context.Background(), entry.Name, &entry, "skill:test_noop", map[string]string{"action": "maintenance"})
	if result.State != "skipped" || result.FailureReason != "no_change" || result.CleanupStatus != "clear" {
		t.Fatalf("no-op result = %+v", result)
	}
	if calls.save != 0 || calls.index != 0 || calls.audit != 0 {
		t.Fatalf("no-op side effects save=%d index=%d audit=%d", calls.save, calls.index, calls.audit)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("no-op compensation records = %+v, err=%v", records, err)
	}
}

func TestSkillCommitter_RejectsNilMutatorResultForNonEmptyRegistry(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	entry := corelib.NLSkillEntry{Name: "mutator-guard", Status: "active"}
	saveCalls := 0
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{entry} },
		SkillSaver: func([]corelib.NLSkillEntry) error {
			saveCalls++
			return nil
		},
		EntriesMutator: func([]corelib.NLSkillEntry) ([]corelib.NLSkillEntry, error) {
			// A buggy planner must not be able to turn a maintenance request into
			// an implicit "delete every Skill" write.
			return nil, nil
		},
		SkipDefinitionBackup: true,
	}
	result := committer.Commit(context.Background(), entry.Name, &entry, "skill:test_nil_mutator", map[string]string{"action": "maintenance"})
	if result.State != "rolled_back" || result.FailureReason != "candidate_mutation_failed" {
		t.Fatalf("result = %+v, want candidate_mutation_failed", result)
	}
	if saveCalls != 0 {
		t.Fatalf("nil mutator result reached saver %d times", saveCalls)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("nil mutator result created compensation records = %+v, err=%v", records, err)
	}
}

func TestSkillCommitter_RejectsCreateIdentityMismatch(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	saveCalls := 0
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return nil },
		SkillSaver: func([]corelib.NLSkillEntry) error {
			saveCalls++
			return nil
		},
		AllowCreate:          true,
		SkipDefinitionBackup: true,
	}
	result := committer.Commit(context.Background(), "requested-name", &corelib.NLSkillEntry{Name: "other-name"}, "skill:test_identity", map[string]string{"action": "install"})
	if result.State != "rolled_back" || result.FailureReason != "candidate_identity_mismatch" {
		t.Fatalf("result = %+v, want candidate_identity_mismatch", result)
	}
	if saveCalls != 0 {
		t.Fatalf("identity mismatch reached saver %d times", saveCalls)
	}
}

func TestSkillCommitter_UsesForwardIndexOnCommitAndRollbackIndexOnFailure(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	dir := filepath.Join(base, "skills", "index-callbacks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: index-callbacks\ndescription: old\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "index-callbacks", SkillDir: dir, Description: "old"}
	forward, rollback := 0, 0
	committer := &SkillCommitter{
		SkillLoader:            func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:             func([]corelib.NLSkillEntry) error { return nil },
		DefinitionWriter:       WriteBackOptimizedSteps,
		IndexRefresher:         func() error { forward++; return nil },
		RollbackIndexRefresher: func() error { rollback++; return nil },
		FinalAuditor:           func(string, map[string]string) error { return fmt.Errorf("injected audit failure") },
	}
	got := committer.Commit(context.Background(), old.Name, &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"}, "skill:test_index_callbacks", map[string]string{"action": "repair"})
	if got.State != "rolled_back" || forward != 1 || rollback != 1 {
		t.Fatalf("result=%+v forward=%d rollback=%d", got, forward, rollback)
	}
}

func TestSkillCommitter_FinalAuditFailureRestoresAndClearsCompensation(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committer-audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalYAML := []byte("name: committer-audit\ndescription: old\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), originalYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "committer-audit", SkillDir: dir, Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"}
	var saveCalls int
	committer := &SkillCommitter{
		SkillLoader:      func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:       func([]corelib.NLSkillEntry) error { saveCalls++; return nil },
		DefinitionWriter: WriteBackOptimizedSteps,
		IndexRefresher:   func() error { return nil },
		FinalAuditor:     func(string, map[string]string) error { return fmt.Errorf("injected final audit failure") },
	}
	got := committer.Commit(context.Background(), old.Name, after, "skill:test_commit", map[string]string{"action": "repair"})
	if got.State != "rolled_back" || got.CleanupStatus != "clear" || !got.RollbackComplete || saveCalls != 2 {
		t.Fatalf("commit result = %+v saveCalls=%d", got, saveCalls)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil || string(data) != string(originalYAML) {
		t.Fatalf("YAML after rollback = %q, err=%v", data, err)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("compensation records = %+v, err=%v; want cleaned", records, err)
	}
}

func TestSkillCommitter_RollbackCleanupFailureUsesBoundedRetryState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committer-rollback-cleanup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: committer-rollback-cleanup\ndescription: old\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "committer-rollback-cleanup", SkillDir: dir, Description: "old"}
	committer := &SkillCommitter{
		SkillLoader:      func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:       func([]corelib.NLSkillEntry) error { return nil },
		DefinitionWriter: WriteBackOptimizedSteps,
		IndexRefresher:   func() error { return nil },
		FinalAuditor:     func(string, map[string]string) error { return fmt.Errorf("injected final audit failure") },
		CompensationClear: func(string, string, string) error {
			return fmt.Errorf("injected compensation clear failure")
		},
	}
	result := committer.Commit(
		WithEvolutionRequestMetadata(context.Background(), "req-rollback-cleanup", 1),
		old.Name,
		&corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"},
		"skill:test_rollback_cleanup",
		map[string]string{"action": "repair"},
	)
	if result.State != "rolled_back" || !result.RollbackComplete || result.CleanupStatus != "pending" {
		t.Fatalf("result=%+v, want rolled_back/pending", result)
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Attempts != 1 || records[0].TransactionState != "rolled_back" {
		t.Fatalf("compensation records=%+v, want one rolled_back attempt=1", records)
	}
	if records[0].FailureReason != "rollback_cleanup_failed" {
		t.Fatalf("failure reason=%q, want rollback_cleanup_failed", records[0].FailureReason)
	}
}

func TestSkillCommitter_RollbackRemovesTransactionBackupOnly(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committer-backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: committer-backup\ndescription: old\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	// Keep an older user-visible backup. Rollback must remove only the backup
	// created by this transaction and preserve the older history.
	oldBackup := filepath.Join(dir, "skill.yaml.v1")
	if err := os.WriteFile(oldBackup, []byte("name: committer-backup\ndescription: history\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newBackup := filepath.Join(dir, "skill.yaml.v2")
	old := corelib.NLSkillEntry{Name: "committer-backup", SkillDir: dir, Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"}
	refreshCalls := 0
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		DefinitionWriter: func(entry *corelib.NLSkillEntry) error {
			if _, err := (&Versioner{}).BackupCurrent(dir); err != nil {
				return err
			}
			return WriteBackOptimizedSteps(entry)
		},
		CompensationMutator: func(record *EvolutionCompensationRecord) {
			record.SetRollbackCleanupPaths([]string{newBackup})
		},
		IndexRefresher: func() error {
			refreshCalls++
			if refreshCalls == 1 {
				return fmt.Errorf("injected index failure")
			}
			return nil
		},
	}
	got := committer.Commit(context.Background(), old.Name, after, "skill:test_backup_rollback", map[string]string{"action": "maintenance"})
	if got.State != "rolled_back" || !got.RollbackComplete || got.CleanupStatus != "clear" {
		t.Fatalf("commit result = %+v", got)
	}
	if _, err := os.Stat(newBackup); !os.IsNotExist(err) {
		t.Fatalf("transaction backup remains after rollback: %v", err)
	}
	if _, err := os.Stat(oldBackup); err != nil {
		t.Fatalf("pre-existing backup was removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil || string(data) != string(original) {
		t.Fatalf("YAML after rollback = %q, err=%v", data, err)
	}
}

func TestRestoreEvolutionCompensationRemovesAllNewMultiPackageDirectories(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "skills", "first")
	second := filepath.Join(root, "skills", "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "skill.yaml"), []byte("name: test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	record := NewEvolutionCompensationRecord("multi-new", "first", "agentservice_install", "", nil, false, nil, "rollback")
	record.SetDirectoryMoves([]EvolutionDirectoryMove{
		{OriginalPath: first, HadPrevious: false, Published: true},
		{OriginalPath: second, HadPrevious: false, Published: true},
	})
	record.SetCreatedDirectories([]string{first, second})
	if err := restoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("restoreEvolutionCompensation() error = %v", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("new package directory %s remains: %v", path, err)
		}
	}
}

func TestRestoreEvolutionCompensationMixedExistingAndNewMultiPackageDirectories(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "skills", "existing")
	backup := existing + ".prev"
	newDir := filepath.Join(root, "skills", "new")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "skill.yaml"), []byte("name: replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("mixed", "existing", "agentservice_install", "", nil, false, nil, "rollback")
	record.SetDirectoryMoves([]EvolutionDirectoryMove{
		{OriginalPath: existing, BackupPath: backup, HadPrevious: true, Moved: true, Published: true},
		{OriginalPath: newDir, HadPrevious: false, Published: true},
	})
	record.SetCreatedDirectories([]string{existing, newDir})
	if err := restoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("restoreEvolutionCompensation() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(existing, "skill.yaml"))
	if err != nil || string(data) != "name: original\n" {
		t.Fatalf("existing package after restore = %q, err=%v", data, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("existing backup remains: %v", err)
	}
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Fatalf("new package directory remains: %v", err)
	}
}

func TestRestoreEvolutionCompensationProtectsExistingPathInCreatedDirs(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "skills", "existing")
	backup := original + ".prev"
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "skill.yaml"), []byte("name: replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("protect-existing", "existing", "install", "", nil, false, nil, "rollback")
	record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: original, BackupPath: backup, HadPrevious: true, Moved: true, Published: true}})
	record.SetCreatedDirectories([]string{original})
	if err := restoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("restoreEvolutionCompensation() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(original, "skill.yaml"))
	if err != nil || string(data) != "name: original\n" {
		t.Fatalf("protected existing package after restore = %q, err=%v", data, err)
	}
}

func TestRestoreEvolutionCompensationDoesNotDeleteUnpublishedIntentTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "skills", "concurrent")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "skill.yaml"), []byte("name: concurrent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("intent-only", "concurrent", "install", "", nil, false, nil, "rollback")
	record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: target, HadPrevious: false, Moved: false, Published: false}})
	record.SetCreatedDirectories([]string{target})
	if err := restoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("restoreEvolutionCompensation() error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("unpublished intent target was removed: %v", err)
	}
}

func TestRestoreEvolutionCompensationRejectsRelativeDurablePaths(t *testing.T) {
	record := NewEvolutionCompensationRecord("relative-path", "relative", "install", "", nil, false, nil, "rollback")
	record.SetCreatedDirectories([]string{"relative-target"})
	if err := RestoreEvolutionCompensation(record, nil, nil); err == nil {
		t.Fatal("RestoreEvolutionCompensation() accepted relative durable path")
	}
}

func TestEvolutionCompensationRecoveryCleansRollbackArtifacts(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "recovery-backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldYAML := []byte("name: recovery-backup\ndescription: old\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: recovery-backup\ndescription: partial\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, "skill.yaml.v9")
	if err := os.WriteFile(artifact, []byte("transaction backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := newEvolutionCompensationRecord("req-recovery-backup", "recovery-backup", "maintenance", filepath.Join(dir, "skill.yaml"), oldYAML, true, []corelib.NLSkillEntry{{Name: "recovery-backup", SkillDir: dir, Description: "old"}}, "index_refresh_failed")
	record.SetRollbackCleanupPaths([]string{artifact})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.SkillSaver = func([]corelib.NLSkillEntry) error { return nil }
	p.IndexRefresher = func() error { return nil }
	recovered, pending, err := p.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("rollback artifact remains after recovery: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil || string(data) != string(oldYAML) {
		t.Fatalf("YAML after recovery = %q, err=%v", data, err)
	}
}

func TestSkillCommitter_RecoveryNeverRollsBackCommittedCleanupPending(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "committed-recovery")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := dir + ".prev"
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: committed-recovery\ndescription: new\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: committed-recovery\ndescription: old\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := newEvolutionCompensationRecord("req-committed-recovery", "committed-recovery", "repair", "", nil, false, nil, "post_commit_cleanup_pending")
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	record.SetDirectoryBackup(dir, backup, true)
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	var saved int
	p := NewEvolutionPipeline()
	p.SkillSaver = func([]corelib.NLSkillEntry) error { saved++; return nil }
	p.IndexRefresher = func() error { return nil }
	recovered, pending, err := p.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if saved != 0 {
		t.Fatalf("committed cleanup recovery unexpectedly restored config (%d saves)", saved)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists after cleanup: err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil || !strings.Contains(string(data), "description: new") {
		t.Fatalf("published YAML changed during cleanup: %q err=%v", data, err)
	}
}

func TestCompensationRecoveryUsesDurableFinalAuditMarker(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	created := filepath.Join(base, "skills", "audit-marker")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	requestID := "req-audit-marker"
	record := NewEvolutionCompensationRecord(requestID, "audit-marker", "legacy_gui_skill_install", "", nil, false, nil, "post_commit_state_persist_failed")
	record.FinalAuditKind = KindFromEventName("skill:legacy_install_committed")
	record.TransactionState = "prepared" // simulate a crash before queue state replacement
	record.CleanupStatus = "pending"
	record.SetCreatedDirectories([]string{created})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	if err := RecordEvolutionEventStrict("skill:legacy_install_committed", map[string]string{
		"skill": "audit-marker", "action": "legacy_gui_skill_install", "decision": "applied", "request_id": requestID,
		"schema_version": "2",
	}, "test"); err != nil {
		t.Fatal(err)
	}

	p := NewEvolutionPipeline()
	recovered, pending, err := p.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, statErr := os.Stat(created); statErr != nil {
		t.Fatalf("committed created directory was removed after audit-marker recovery: %v", statErr)
	}
	if summaries, readErr := ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after audit-marker recovery = %#v, want empty", summaries)
	}
}

func TestRecoverPendingCompensationsScopedByServiceRoot(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	serviceA := filepath.Join(base, "service-a")
	serviceB := filepath.Join(base, "service-b")
	makePending := func(scope, name string) (string, string) {
		original := filepath.Join(scope, "skills", name)
		backup := original + ".prev"
		if err := os.MkdirAll(backup, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: "+name+"\ndescription: restored\nsteps: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		record := NewEvolutionCompensationRecord("scope-"+name, name, "agentservice_install", "", nil, false, nil, "publish interrupted")
		record.TransactionState = "audit_pending"
		record.CleanupStatus = "pending"
		record.SetRecoveryScope(scope)
		record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: original, BackupPath: backup, HadPrevious: true, Moved: true, Published: true}})
		if err := PersistEvolutionCompensation(record); err != nil {
			t.Fatal(err)
		}
		return original, backup
	}
	aOriginal, aBackup := makePending(serviceA, "scope-a")
	bOriginal, bBackup := makePending(serviceB, "scope-b")

	recovered, pending, err := RecoverPendingEvolutionCompensationsForActionPrefixAndScope("agentservice_install", serviceA, func([]corelib.NLSkillEntry) error { return nil }, nil)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("scoped recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, err := os.Stat(filepath.Join(aOriginal, "skill.yaml")); err != nil {
		t.Fatalf("service A was not restored: %v", err)
	}
	if _, err := os.Stat(aBackup); !os.IsNotExist(err) {
		t.Fatalf("service A backup remains: %v", err)
	}
	if _, err := os.Stat(bOriginal); !os.IsNotExist(err) {
		t.Fatalf("scoped recovery touched service B original: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bBackup, "skill.yaml")); err != nil {
		t.Fatalf("service B backup was changed or removed: %v", err)
	}

	if recovered, pending, err = RecoverPendingEvolutionCompensationsForActionPrefixAndScope("agentservice_install", serviceB, func([]corelib.NLSkillEntry) error { return nil }, nil); err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("service B recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
}

func TestRecoverPendingCompensationsForActionPrefixAndSkillDoesNotClaimSibling(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	makePending := func(name string) string {
		dir := filepath.Join(base, "skills", name)
		backup := dir + ".prev"
		if err := os.MkdirAll(backup, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: "+name+"\ndescription: restored\nsteps: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		record := NewEvolutionCompensationRecord("legacy-"+name, name, "legacy_gui_skill_install", "", nil, false, nil, "install interrupted")
		record.TransactionState = "audit_pending"
		record.CleanupStatus = "pending"
		record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: dir, BackupPath: backup, HadPrevious: true, Moved: true, Published: true}})
		if err := PersistEvolutionCompensation(record); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	targetDir := makePending("target")
	siblingDir := makePending("sibling")

	recovered, pending, err := RecoverPendingEvolutionCompensationsForActionPrefixAndSkill("legacy_gui_skill_", "target", nil, nil)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("filtered recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "skill.yaml")); err != nil {
		t.Fatalf("target compensation was not recovered: %v", err)
	}
	if _, err := os.Stat(siblingDir + ".prev"); err != nil {
		t.Fatalf("sibling compensation was claimed or removed: %v", err)
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		t.Fatalf("read remaining compensations: %v", err)
	}
	if len(records) != 1 || records[0].Skill != "sibling" {
		t.Fatalf("remaining compensations = %#v, want only sibling", records)
	}
}

func TestRecoverPendingCompensationsScopeRejectsOutOfRootPaths(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	serviceA := filepath.Join(base, "service-a")
	serviceB := filepath.Join(base, "service-b")
	original := filepath.Join(serviceB, "skills", "escape")
	backup := original + ".prev"
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: escape\ndescription: keep\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("scope-escape", "escape", "agentservice_install", "", nil, false, nil, "out-of-scope path")
	record.TransactionState = "audit_pending"
	record.CleanupStatus = "pending"
	record.SetRecoveryScope(serviceA)
	record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: original, BackupPath: backup, HadPrevious: true, Moved: true, Published: true}})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}

	recovered, pending, err := RecoverPendingEvolutionCompensationsForActionPrefixAndScope("agentservice_install", serviceA, func([]corelib.NLSkillEntry) error { return nil }, nil)
	if err != nil {
		t.Fatalf("scoped recovery returned error: %v", err)
	}
	if recovered != 0 || pending != 1 {
		t.Fatalf("out-of-scope recovery = recovered:%d pending:%d", recovered, pending)
	}
	if _, err := os.Stat(filepath.Join(backup, "skill.yaml")); err != nil {
		t.Fatalf("out-of-scope backup was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(original, "skill.yaml")); !os.IsNotExist(err) {
		t.Fatalf("out-of-scope original was unexpectedly restored: %v", err)
	}
}

func TestRecoverPendingCompensationsWithExternalSnapshotFailsClosedWithoutRestorer(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	scope := filepath.Join(base, "service")
	original := filepath.Join(scope, "skills", "external")
	backup := original + ".prev"
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skill.yaml"), []byte("name: external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("external-no-restorer", "external", "agentservice_install", "", nil, false, nil, "external state pending")
	record.TransactionState = "audit_pending"
	record.CleanupStatus = "pending"
	record.SetRecoveryScope(scope)
	record.SetDirectoryMoves([]EvolutionDirectoryMove{{OriginalPath: original, BackupPath: backup, HadPrevious: true, Moved: true, Published: true}})
	record.SetExternalSnapshot("opaque_external_state", "{}")
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	recovered, pending, err := RecoverPendingEvolutionCompensationsForActionPrefixAndScope("agentservice_install", scope, func([]corelib.NLSkillEntry) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 || pending != 1 {
		t.Fatalf("recovery without external restorer = recovered:%d pending:%d", recovered, pending)
	}
	if _, err := os.Stat(filepath.Join(backup, "skill.yaml")); err != nil {
		t.Fatalf("external snapshot without restorer was allowed to mutate directory: %v", err)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 1 {
		t.Fatalf("pending external compensation was not retained: records=%#v err=%v", records, err)
	}
}

func TestRecoverExternalCompensationExhaustionAuditsNeedsReview(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	scope := filepath.Join(base, "service")
	created := filepath.Join(scope, "skills", "external-review")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("external-review-request", "external-review", "agentservice_install", "", nil, false, nil, "external state pending")
	record.TransactionState = "audit_pending"
	record.CleanupStatus = "pending"
	record.SetRecoveryScope(scope)
	record.SetCreatedDirectories([]string{created})
	record.SetExternalSnapshot("opaque_external_state", "{}")
	record.MarkExternalApplied("opaque_external_state", true)
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	restarted := &EvolutionPipeline{}
	for i := 0; i < evolutionCompensationMaxAttempts; i++ {
		if _, _, err := restarted.RecoverPendingCompensationsForActionPrefixAndScopeWithExternalRecovery("agentservice_install", scope, func(EvolutionCompensationRecord) error {
			return fmt.Errorf("external restore unavailable")
		}); err != nil {
			t.Fatalf("recovery attempt %d: %v", i+1, err)
		}
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	if records[0].Status != "needs_review" || records[0].Attempts != evolutionCompensationMaxAttempts {
		t.Fatalf("record=%+v, want bounded needs_review", records[0])
	}
	events, err := ListEvolutionAudit(DefaultEvolutionAuditPath(), 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "compensation_needs_review" && event.RequestID == record.RequestID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing strict needs_review audit for external recovery: %#v", events)
	}
}

// Recovery retry state must be durable before the pass reaches the final
// compaction write.  Simulate a process crash after the first failed external
// restore and verify that both the updated row and an unrelated queue tail
// survive the crash window.
func TestRecoverPersistsFailureBeforeProcessingQueueTail(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	first := NewEvolutionCompensationRecord("recovery-tail-first", "tail-first", "agentservice_install", "", nil, false, nil, "pending")
	first.SetExternalSnapshot("opaque-first", "{}")
	first.MarkExternalApplied("opaque-first", true)
	second := NewEvolutionCompensationRecord("recovery-tail-second", "tail-second", "agentservice_install", "", nil, false, nil, "pending")
	second.SetExternalSnapshot("opaque-second", "{}")
	second.MarkExternalApplied("opaque-second", true)
	if err := PersistEvolutionCompensation(first); err != nil {
		t.Fatal(err)
	}
	if err := PersistEvolutionCompensation(second); err != nil {
		t.Fatal(err)
	}

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		calls := 0
		_, _, _ = NewEvolutionPipeline().RecoverPendingCompensationsForActionPrefixAndScopeWithExternalRecovery(
			"agentservice_install", "", func(EvolutionCompensationRecord) error {
				calls++
				if calls == 1 {
					return fmt.Errorf("external restore unavailable")
				}
				panic("simulated process crash")
			})
	}()
	if !panicked {
		t.Fatal("expected simulated process crash")
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("queue records=%d, want failed row plus untouched tail: %#v", len(records), records)
	}
	var gotFirst, gotSecond *EvolutionCompensationRecord
	for i := range records {
		switch records[i].RequestID {
		case first.RequestID:
			gotFirst = &records[i]
		case second.RequestID:
			gotSecond = &records[i]
		}
	}
	if gotFirst == nil || gotFirst.Attempts != 1 {
		t.Fatalf("first recovery state was not persisted before crash: %#v", gotFirst)
	}
	if gotSecond == nil || gotSecond.Attempts != 0 {
		t.Fatalf("queue tail was lost or mutated: %#v", gotSecond)
	}
}

func TestRestoreEvolutionCompensationPreflightsCorruptFileSnapshots(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	first, second := filepath.Join(base, "first.json"), filepath.Join(base, "second.json")
	if err := os.WriteFile(first, []byte("new-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("new-second"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("corrupt-file-snapshot", "legacy", "legacy_gui_skill_install", "", nil, false, nil, "restore")
	record.SetFileSnapshots([]EvolutionFileSnapshot{
		{Path: first, Exists: true, BackupB64: base64.StdEncoding.EncodeToString([]byte("old-first"))},
		{Path: second, Exists: true, BackupB64: "%%%not-base64%%%"},
	})
	err := RestoreEvolutionCompensation(record, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "decode file snapshot") {
		t.Fatalf("restore corrupt snapshots error = %v, want decode failure", err)
	}
	got, readErr := os.ReadFile(first)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "new-first" {
		t.Fatalf("first snapshot was partially restored after preflight failure: %q", got)
	}
}

func TestRestoreEvolutionCompensationPreflightsCorruptYAMLBeforeDirectoryMutation(t *testing.T) {
	base := t.TempDir()
	createdDir := filepath.Join(base, "created-skill")
	if err := os.MkdirAll(createdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(base, "skill.yaml")
	record := NewEvolutionCompensationRecord("corrupt-yaml-before-dir", "legacy", "legacy_gui_skill_install", yamlPath, nil, true, nil, "restore")
	record.SetCreatedDirectories([]string{createdDir})
	record.YAMLBackup = "%%%not-base64%%%"
	err := RestoreEvolutionCompensation(record, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "decode YAML backup") {
		t.Fatalf("restore corrupt YAML error = %v, want decode failure", err)
	}
	if _, statErr := os.Stat(createdDir); statErr != nil {
		t.Fatalf("created directory was mutated before YAML preflight completed: %v", statErr)
	}
	parentFile := filepath.Join(base, "parent-file")
	if err := os.WriteFile(parentFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	directoryRecord := NewEvolutionCompensationRecord("invalid-directory-parent", "legacy", "legacy_gui_skill_install", "", nil, false, nil, "restore")
	directoryRecord.DirPath = filepath.Join(parentFile, "target")
	directoryRecord.DirBackupPath = filepath.Join(parentFile, "target.prev")
	directoryRecord.DirMoved = true
	directoryRecord.SetCreatedDirectories([]string{createdDir})
	if err := RestoreEvolutionCompensation(directoryRecord, nil, nil); err == nil || !strings.Contains(err.Error(), "parent is not a directory") {
		t.Fatalf("restore invalid directory parent error = %v, want parent-shape failure", err)
	}
	if _, statErr := os.Stat(createdDir); statErr != nil {
		t.Fatalf("created directory was mutated before directory preflight completed: %v", statErr)
	}
}

func TestRestoreEvolutionCompensationPostImageFence(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "config.json")
	oldData := []byte(`{"version":1}`)
	postData := []byte(`{"version":2}`)
	if err := os.WriteFile(path, postData, 0o600); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("post-image-match", "mcp", "capability_mcp_config", "", nil, false, nil, "restore")
	record.SetFileSnapshots([]EvolutionFileSnapshot{{Path: path, Exists: true, BackupB64: base64.StdEncoding.EncodeToString(oldData)}})
	if err := record.SetFileSnapshotPostImage(path, postData, true); err != nil {
		t.Fatal(err)
	}
	if err := RestoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("matching post-image should restore: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(oldData) {
		t.Fatalf("restored data=%q err=%v, want old image", got, err)
	}

	if err := os.WriteFile(path, postData, 0o600); err != nil {
		t.Fatal(err)
	}
	// A previous rollback may have restored the pre-image successfully before
	// failing on a later index step. Retrying must be idempotent rather than
	// treating the already-restored bytes as an unrelated concurrent write.
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}
	record.RequestID = "post-image-already-restored"
	if err := RestoreEvolutionCompensation(record, nil, nil); err != nil {
		t.Fatalf("already-restored pre-image should be accepted: %v", err)
	}
	if err := os.WriteFile(path, postData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	record.RequestID = "post-image-mismatch"
	if err := RestoreEvolutionCompensation(record, nil, nil); err == nil || !strings.Contains(err.Error(), "post-image digest changed") {
		t.Fatalf("mismatched post-image error=%v, want fence rejection", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != `{"version":3}` {
		t.Fatalf("mismatch path was mutated: %q err=%v", got, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	record.RequestID = "post-image-missing"
	if err := RestoreEvolutionCompensation(record, nil, nil); err == nil || !strings.Contains(err.Error(), "post-image changed") {
		t.Fatalf("missing post-image error=%v, want fence rejection", err)
	}
}

func TestReadEvolutionCompensationsRejectsExternalAppliedWithoutSnapshot(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := NewEvolutionCompensationRecord("external-marker-without-snapshot", "legacy", "agentservice_install", "", nil, false, nil, "invalid")
	record.ExternalApplied = map[string]bool{"contract": true}
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	if _, err := readEvolutionCompensations(); err == nil || !strings.Contains(err.Error(), "no snapshots") {
		t.Fatalf("read invalid external transition error = %v, want fail-closed schema error", err)
	}
}

func TestReadEvolutionCompensationsRejectsDuplicateFileSnapshotPaths(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := filepath.Join(base, "metadata.json")
	record := NewEvolutionCompensationRecord("duplicate-file-snapshot", "legacy", "legacy_gui_skill_install", "", nil, false, nil, "invalid")
	encoded := base64.StdEncoding.EncodeToString([]byte("{}"))
	record.SetFileSnapshots([]EvolutionFileSnapshot{{Path: path, Exists: true, BackupB64: encoded}, {Path: path, Exists: true, BackupB64: encoded}})
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	if _, err := readEvolutionCompensations(); err == nil || !strings.Contains(err.Error(), "duplicate file snapshot") {
		t.Fatalf("read duplicate snapshot error = %v, want fail-closed schema error", err)
	}
}

func TestSkillCommitter_ExternalPublishPersistsRecoveryIntentBeforeIndex(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	staging := filepath.Join(base, "staging", "external-publish")
	final := filepath.Join(base, "skills", "external-publish")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "skill.yaml"), []byte("name: external-publish\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "external-publish", Status: "active"}
	entry := &corelib.NLSkillEntry{Name: old.Name, Status: "active", SkillDir: final}
	var indexCalls int
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		ExternalCommitWithCompensation: func(record *EvolutionCompensationRecord) error {
			if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
				return err
			}
			if err := os.Rename(staging, final); err != nil {
				return err
			}
			record.SetCreatedDirectories([]string{final})
			record.SetDirectoryPublished(true)
			return nil
		},
		ExternalRollback: func() error { return os.RemoveAll(final) },
		IndexRefresher: func() error {
			indexCalls++
			if indexCalls == 1 {
				return fmt.Errorf("injected external publish index failure")
			}
			return nil
		},
	}
	got := committer.Commit(context.Background(), entry.Name, entry, "skill:test_external_publish", map[string]string{"skill": entry.Name, "action": "install"})
	if got.State != "rolled_back" || !got.RollbackComplete || got.CleanupStatus != "clear" {
		t.Fatalf("commit result = %+v, want complete rollback", got)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("published directory remains after rollback: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging directory unexpectedly remains after rollback: %v", err)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("compensation records = %+v, err=%v; want cleaned", records, err)
	}
}

func TestPersistDefinitionChange_PersistsAndRecoversAuditPending(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := filepath.Join(base, "skills", "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalYAML := []byte("name: pending-skill\ndescription: old\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), originalYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	old := corelib.NLSkillEntry{Name: "pending-skill", SkillDir: dir, Description: "old"}
	after := &corelib.NLSkillEntry{Name: old.Name, SkillDir: dir, Description: "new"}

	var indexCalls int
	p := NewEvolutionPipeline()
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{old} }
	p.SkillSaver = func([]corelib.NLSkillEntry) error { return nil }
	p.DefinitionWriter = func(entry *corelib.NLSkillEntry) error {
		return WriteBackOptimizedSteps(entry)
	}
	p.IndexRefresher = func() error {
		indexCalls++
		if indexCalls == 2 { // rollback refresh fails, leaving compensation pending
			return fmt.Errorf("injected rollback index failure")
		}
		return nil
	}
	p.FinalAuditor = func(string, map[string]string) error {
		return fmt.Errorf("injected final audit failure")
	}
	ctx := WithEvolutionRequestMetadata(context.Background(), "req-pending", 1)
	result := p.persistDefinitionChangeWithAudit(ctx, old.Name, after, "skill:test_commit", map[string]string{"action": "repair"})
	if result.State != "audit_pending" || result.RollbackComplete {
		t.Fatalf("result = %+v, want incomplete audit_pending", result)
	}
	queuePath := DefaultEvolutionCompensationPath()
	if _, err := os.Stat(queuePath); err != nil {
		t.Fatalf("compensation queue missing: %v", err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err=%v; want one durable record", len(records), err)
	}
	if records[0].RequestID != "req-pending" || records[0].Skill != old.Name {
		t.Fatalf("record = %+v, missing correlation fields", records[0])
	}

	// A fresh pipeline simulates a process restart. Recovery succeeds and
	// atomically removes the queue record.
	restarted := NewEvolutionPipeline()
	restarted.SkillSaver = func([]corelib.NLSkillEntry) error { return nil }
	restarted.IndexRefresher = func() error { return nil }
	recovered, pending, err := restarted.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Fatalf("queue still exists after recovery, stat err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(originalYAML) {
		t.Fatalf("YAML after recovery = %q, want original", data)
	}
}

func TestRecoverPendingCompensationsEventuallyNeedsReview(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-review", "review-skill", "repair", "", nil, false, nil, "rollback_incomplete")
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.SkillSaver = func([]corelib.NLSkillEntry) error { return fmt.Errorf("still unavailable") }
	for i := 0; i < evolutionCompensationMaxAttempts; i++ {
		_, _, err := p.RecoverPendingCompensations()
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err=%v", len(records), err)
	}
	if records[0].Status != "needs_review" || records[0].Attempts != evolutionCompensationMaxAttempts {
		t.Fatalf("record = %+v, want bounded needs_review", records[0])
	}
}

func TestRecoverPendingCommittedCleanupNeedsReviewRemainsBlocked(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-cleanup-review", "cleanup-review-skill", "agentservice_install", "", nil, false, nil, "post_commit_cleanup_failed")
	record.TransactionState = "committed"
	record.CleanupStatus = "needs_review"
	record.Status = "needs_review"
	record.Attempts = evolutionCompensationMaxAttempts
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	recovered, pending, err := p.RecoverPendingCompensations()
	if err != nil || recovered != 0 || pending != 1 {
		t.Fatalf("recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err=%v; want retained review record", len(records), err)
	}
	if records[0].TransactionState != "committed" || records[0].CleanupStatus != "needs_review" || records[0].Attempts != evolutionCompensationMaxAttempts {
		t.Fatalf("record mutated during review hold: %+v", records[0])
	}
}

func TestRecoverPendingCommittedCleanupFailureEscalatesWithoutRollback(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	root := t.TempDir()
	backup := filepath.Join(root, "skill.prev")
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := newEvolutionCompensationRecord("req-cleanup-escalate", "cleanup-escalate", "agentservice_install", "", nil, false, nil, "post_commit_cleanup_failed")
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	record.DirectoryMoves = []EvolutionDirectoryMove{{BackupPath: backup, Moved: true}}
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	var cleanupCalls int
	p := NewEvolutionPipeline()
	p.CommittedCleanup = func(EvolutionCompensationRecord) error {
		cleanupCalls++
		return fmt.Errorf("cleanup unavailable")
	}
	for i := 0; i < evolutionCompensationMaxAttempts; i++ {
		if _, _, err := p.RecoverPendingCompensations(); err != nil {
			t.Fatal(err)
		}
	}
	if cleanupCalls != evolutionCompensationMaxAttempts {
		t.Fatalf("cleanup calls = %d, want %d", cleanupCalls, evolutionCompensationMaxAttempts)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("committed backup was altered on cleanup failure: %v", err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %d, err=%v", len(records), err)
	}
	if records[0].TransactionState != "committed" || records[0].CleanupStatus != "needs_review" || records[0].Status != "needs_review" {
		t.Fatalf("record = %+v, want committed/needs_review", records[0])
	}
	if _, _, err := p.RecoverPendingCompensations(); err != nil {
		t.Fatal(err)
	}
	if cleanupCalls != evolutionCompensationMaxAttempts {
		t.Fatalf("needs_review record was retried after escalation: %d calls", cleanupCalls)
	}
}

func TestRecoverPendingCommittedCleanupRunsDeclaredTargetsAfterCustomHook(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	artifact := filepath.Join(base, "staging", "custom-hook-artifact")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := newEvolutionCompensationRecord("req-custom-hook-cleanup", "custom-hook-cleanup", "agentservice_install", "", nil, false, nil, "post_commit_cleanup_pending")
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	record.SetPostCommitCleanupPaths([]string{artifact})
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	callbackCalls := 0
	p.CommittedCleanup = func(EvolutionCompensationRecord) error {
		callbackCalls++
		return nil
	}
	recovered, pending, err := p.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 || callbackCalls != 1 {
		t.Fatalf("recovery = recovered:%d pending:%d callback_calls:%d err:%v", recovered, pending, callbackCalls, err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("declared artifact remains after custom cleanup recovery: %v", err)
	}
}

func TestMarkCompensationNeedsReviewDemotesAllAffectedSkills(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{Name: "batch-primary", Status: "active"},
		{Name: "batch-secondary", Status: "active"},
		{Name: "unrelated", Status: "active"},
	}
	record := NewEvolutionCompensationRecord("batch-review", "batch-primary", "agentservice_install", "", nil, false, nil, "rollback exhausted")
	record.SetAffectedSkills([]string{"batch-primary", "batch-secondary"})
	record.LastError = "rollback unavailable"
	var saved []corelib.NLSkillEntry
	p := &EvolutionPipeline{
		SkillLoader: func() []corelib.NLSkillEntry { return cloneSkillEntries(skills) },
		SkillSaver: func(updated []corelib.NLSkillEntry) error {
			saved = cloneSkillEntries(updated)
			return nil
		},
	}
	if err := p.markCompensationNeedsReview(record); err != nil {
		t.Fatalf("markCompensationNeedsReview() error = %v", err)
	}
	if len(saved) != len(skills) {
		t.Fatalf("saved skills = %#v, want %d entries", saved, len(skills))
	}
	for _, name := range []string{"batch-primary", "batch-secondary"} {
		var found *corelib.NLSkillEntry
		for i := range saved {
			if saved[i].Name == name {
				found = &saved[i]
				break
			}
		}
		if found == nil || found.Status != "needs_review" {
			t.Fatalf("affected skill %q was not demoted: %#v", name, saved)
		}
	}
	if saved[2].Status != "active" {
		t.Fatalf("unrelated skill was changed: %#v", saved[2])
	}
}

func TestMarkEvolutionCompensationCleanupFailurePersistsBoundedState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-helper", "helper-skill", "install", "", nil, false, nil, "prepared")
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= evolutionCompensationMaxAttempts; i++ {
		records, err := readEvolutionCompensations()
		if err != nil || len(records) != 1 {
			t.Fatalf("before attempt %d records=%d err=%v", i, len(records), err)
		}
		record = records[0]
		if err := MarkEvolutionCompensationCleanupFailure(&record, fmt.Errorf("cleanup-%d", i)); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if record.TransactionState != "committed" || record.Attempts != i {
			t.Fatalf("attempt %d state = %+v", i, record)
		}
		if i < evolutionCompensationMaxAttempts && record.CleanupStatus != "pending" {
			t.Fatalf("attempt %d cleanup status = %q, want pending", i, record.CleanupStatus)
		}
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	got := records[0]
	if got.TransactionState != "committed" || got.CleanupStatus != "needs_review" || got.Status != "needs_review" || got.Attempts != evolutionCompensationMaxAttempts {
		t.Fatalf("unexpected helper state: %+v", got)
	}
	if got.FailureReason != "post_commit_cleanup_failed" || got.LastError != "cleanup-3" {
		t.Fatalf("unexpected failure metadata: %+v", got)
	}
	audit, err := ListEvolutionAudit(DefaultEvolutionAuditPath(), 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range audit {
		if event.Kind == "compensation_needs_review" && event.RequestID == "req-helper" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing needs_review audit event: %+v", audit)
	}
	// Re-reporting an already escalated artifact is idempotent: manual review
	// must remain the only operation that can move it out of the terminal hold.
	if err := MarkEvolutionCompensationCleanupFailure(&got, fmt.Errorf("cleanup-after-review")); err != nil {
		t.Fatalf("re-report needs_review: %v", err)
	}
	if got.Attempts != evolutionCompensationMaxAttempts || got.CleanupStatus != "needs_review" {
		t.Fatalf("needs_review state changed on duplicate report: %+v", got)
	}
}

func TestMarkEvolutionCompensationCleanupFailurePersistenceErrorIsReturned(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-helper-persist", "helper-persist", "install", "", nil, false, nil, "prepared")
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	// Point the queue root at a regular file so replacement cannot persist.
	badRoot := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(badRoot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	corelib.SetMaclawBaseDir(badRoot)
	err := MarkEvolutionCompensationCleanupFailure(&record, fmt.Errorf("cleanup unavailable"))
	if err == nil {
		t.Fatal("expected persistence error")
	}
	if record.TransactionState != "committed" || record.CleanupStatus != "pending" || record.Attempts != 1 {
		t.Fatalf("helper mutated in-memory state unexpectedly: %+v", record)
	}
}

func TestMarkEvolutionCompensationRollbackFailurePreservesUncommittedState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-rollback-helper", "rollback-skill", "capability_mcp_config", "", nil, false, nil, "prepared")
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= evolutionCompensationMaxAttempts; i++ {
		records, err := readEvolutionCompensations()
		if err != nil || len(records) != 1 {
			t.Fatalf("before attempt %d records=%d err=%v", i, len(records), err)
		}
		record = records[0]
		if err := MarkEvolutionCompensationRollbackFailure(&record, fmt.Errorf("rollback-cleanup-%d", i)); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if record.TransactionState == "committed" || record.Attempts != i {
			t.Fatalf("attempt %d crossed commit boundary: %+v", i, record)
		}
		if i < evolutionCompensationMaxAttempts && record.Status == "needs_review" {
			t.Fatalf("attempt %d escalated too early: %+v", i, record)
		}
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	got := records[0]
	if got.TransactionState != "audit_pending" || got.CleanupStatus != "pending" || got.Status != "needs_review" || got.Attempts != evolutionCompensationMaxAttempts {
		t.Fatalf("unexpected rollback failure state: %+v", got)
	}
	if got.FailureReason != "rollback_cleanup_failed" || got.LastError != "rollback-cleanup-3" {
		t.Fatalf("unexpected rollback failure metadata: %+v", got)
	}
}

func TestRecoverPendingCompensationsSerializesConcurrentCalls(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-serial", "serial-skill", "repair", "", nil, false, nil, "test")
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.SkillSaver = func([]corelib.NLSkillEntry) error {
		time.Sleep(25 * time.Millisecond)
		return nil
	}
	p.IndexRefresher = func() error { return nil }
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, _ = p.RecoverPendingCompensations()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("concurrent recovery did not complete")
		}
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 0 {
		t.Fatalf("queue after concurrent recovery = %d, err=%v; want empty", len(records), err)
	}
}

func TestRestoreEvolutionCompensationCanSkipIndexForConfigOnlyRecord(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "settings.json")
	oldData := []byte(`{"enabled":false}`)
	postData := []byte(`{"enabled":true}`)
	if err := os.WriteFile(path, postData, 0o600); err != nil {
		t.Fatal(err)
	}
	record := NewEvolutionCompensationRecord("config-only-skip-index", "config", "legacy_gui_marketplace", "", nil, false, nil, "restore")
	record.SetFileSnapshots([]EvolutionFileSnapshot{{Path: path, Exists: true, BackupB64: base64.StdEncoding.EncodeToString(oldData)}})
	record.SetSkipIndexRefresh(true)
	if err := record.SetFileSnapshotPostImage(path, postData, true); err != nil {
		t.Fatal(err)
	}
	indexCalls := 0
	if err := RestoreEvolutionCompensation(record, nil, func() error {
		indexCalls++
		return fmt.Errorf("index refresh should not run for config-only restore")
	}); err != nil {
		t.Fatalf("config-only restore error = %v", err)
	}
	if indexCalls != 0 {
		t.Fatalf("index refresher calls = %d, want 0", indexCalls)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(oldData) {
		t.Fatalf("restored settings = %q err=%v, want old image", got, err)
	}
}

func TestRestoreEvolutionCompensationRemovesDraftWhenSnapshotAbsent(t *testing.T) {
	root := t.TempDir()
	draftPath := filepath.Join(root, "draft.json")
	if err := os.WriteFile(draftPath, []byte(`{"stale":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	record := EvolutionCompensationRecord{
		SchemaVersion: evolutionCompensationSchemaVersion,
		Skill:         "demo",
		YAMLPath:      filepath.Join(root, "skill.yaml"),
		DraftPath:     draftPath,
		DraftExists:   false,
		ConfigBackup:  []corelib.NLSkillEntry{{Name: "demo", Status: "active"}},
	}
	if err := restoreEvolutionCompensation(record, func([]corelib.NLSkillEntry) error { return nil }, nil); err != nil {
		t.Fatalf("restoreEvolutionCompensation() error = %v", err)
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("draft still exists after recovery, stat err=%v", err)
	}
}

func TestListEvolutionCompensationSummariesRedactsPathsAndBoundsErrors(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-summary", "summary-skill", "repair", "C:\\private\\skill.yaml", nil, false, nil, "")
	record.LastError = "restore failed at C:\\private\\skill.yaml: " + strings.Repeat("x", 400)
	if err := appendEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	summaries, err := ListEvolutionCompensationSummaries()
	if err != nil || len(summaries) != 1 {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	if strings.Contains(summaries[0].LastError, "C:\\private") || len([]rune(summaries[0].LastError)) > 256 {
		t.Fatalf("summary leaked/unbounded path error: %q", summaries[0].LastError)
	}
}

func TestReadEvolutionCompensationsRejectsUnsupportedSchemaAndCorruptRecords(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		`{"schema_version":"99","skill":"demo","attempts":0}`,
		`{"schema_version":"1","skill":"demo","attempts":-1}`,
		`{"schema_version":"1","skill":"demo","status":"mystery"}`,
		`{"schema_version":"1","attempts":0}`,
		`{"schema_version":"1","skill":"demo","created_dirs":["."]}`,
		`{"schema_version":"1","skill":"demo","directory_moves":[{"original_path":"relative","backup_path":"relative.prev"}]}`,
	}
	for _, line := range cases {
		if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readEvolutionCompensations(); err == nil {
			t.Fatalf("readEvolutionCompensations() accepted invalid record %s", line)
		}
	}
}

func TestReadEvolutionCompensationsQuarantinesCorruptQueueCopy(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"schema_version":"1","skill":"broken","attempts":` + "\n")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEvolutionCompensations(); err == nil {
		t.Fatal("readEvolutionCompensations() accepted malformed queue")
	}
	// The canonical queue remains present so admission stays fail-closed, while
	// a timestamped forensic copy and bounded reason file preserve evidence.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("canonical corrupt queue was removed: %v", err)
	}
	copies, err := filepath.Glob(path + ".corrupt-*.jsonl")
	if err != nil || len(copies) != 1 {
		t.Fatalf("quarantine copies = %#v err=%v, want exactly one", copies, err)
	}
	copyData, err := os.ReadFile(copies[0])
	if err != nil || string(copyData) != string(corrupt) {
		t.Fatalf("quarantine copy = %q err=%v, want original bytes", copyData, err)
	}
	if _, err := os.Stat(copies[0] + ".reason"); err != nil {
		t.Fatalf("quarantine reason file missing: %v", err)
	}
	// Repeated admission checks for the unchanged corrupt file are rate-limited
	// and must not create another forensic copy.
	if _, err := readEvolutionCompensations(); err == nil {
		t.Fatal("second read accepted malformed queue")
	}
	copies, _ = filepath.Glob(path + ".corrupt-*.jsonl")
	if len(copies) != 1 {
		t.Fatalf("repeated read created duplicate quarantine copies: %#v", copies)
	}
}

func TestReadEvolutionCompensationsAcceptsLegacyMissingSchema(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"skill":"legacy","action":"repair","attempts":0}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 || records[0].SchemaVersion != evolutionCompensationSchemaVersion {
		t.Fatalf("legacy records=%+v err=%v", records, err)
	}
}

func TestMigrateEvolutionCompensationQueuePersistsCurrentSchema(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"skill":"legacy-migrate","action":"repair","attempts":0}` + "\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateEvolutionCompensationQueue(); err != nil {
		t.Fatalf("MigrateEvolutionCompensationQueue() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema_version":"1"`) {
		t.Fatalf("migrated queue missing current schema: %s", data)
	}
	if !strings.Contains(string(data), `"transaction_state":"audit_pending"`) || !strings.Contains(string(data), `"cleanup_status":"pending"`) {
		t.Fatalf("migrated queue missing conservative state defaults: %s", data)
	}
	if records, err := readEvolutionCompensations(); err != nil || len(records) != 1 {
		t.Fatalf("migrated records=%+v err=%v", records, err)
	}
}

func TestMigrateEvolutionCompensationQueueNormalizesEmptySchemaValues(t *testing.T) {
	for _, tc := range []struct {
		name, rawVersion string
	}{{"empty-string", `""`}, {"null", `null`}} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			oldBase := corelib.MaclawBaseDir()
			corelib.SetMaclawBaseDir(base)
			t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
			path := DefaultEvolutionCompensationPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			legacy := []byte(fmt.Sprintf(`{"skill":"empty-schema","action":"repair","schema_version":%s}`+"\n", tc.rawVersion))
			if err := os.WriteFile(path, legacy, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := MigrateEvolutionCompensationQueue(); err != nil {
				t.Fatalf("migration error = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"schema_version":"1"`) {
				t.Fatalf("normalized queue missing current schema: %s", data)
			}
		})
	}
}

func TestWriteEvolutionCompensationsReplacesExistingQueueWithoutBackupResidue(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	first := newEvolutionCompensationRecord("req-replace-1", "replace-skill", "repair", "", nil, false, nil, "first")
	second := newEvolutionCompensationRecord("req-replace-2", "replace-skill", "repair", "", nil, false, nil, "second")
	if err := writeEvolutionCompensations([]EvolutionCompensationRecord{first}); err != nil {
		t.Fatal(err)
	}
	if err := writeEvolutionCompensations([]EvolutionCompensationRecord{second}); err != nil {
		t.Fatal(err)
	}
	records, err := readEvolutionCompensations()
	if err != nil || len(records) != 1 || records[0].RequestID != "req-replace-2" {
		t.Fatalf("records=%+v err=%v, want latest queue record", records, err)
	}
	if _, err := os.Stat(DefaultEvolutionCompensationPath() + ".replace-backup"); !os.IsNotExist(err) {
		t.Fatalf("replace backup residue exists, stat err=%v", err)
	}
}

func TestReplaceEvolutionCompensationKeepsOneAuthoritativeSnapshot(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	record := newEvolutionCompensationRecord("req-authoritative", "replace-skill", "install", "", nil, false, nil, "prepared")
	if err := PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	record.TransactionState = "audit_pending"
	record.LastError = "rollback failed"
	if err := ReplaceEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RequestID != record.RequestID || records[0].TransactionState != "audit_pending" || records[0].LastError != "rollback failed" {
		t.Fatalf("records=%+v, want one replaced authoritative snapshot", records)
	}
}

func TestRunOptimize_YAMLErrorDoesNotEmitSuccessOrUpload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: optimize-tx\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	p.Optimizer = NewSkillOptimizer(&mockLLMRepairer{response: `{"optimized":true,"explanation":"change","new_steps":[{"action":"bash","params":{"command":"echo new"}}]}`}, nil, nil)
	entry := corelib.NLSkillEntry{Name: "optimize-tx", SkillDir: dir, Status: "active", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}}}
	p.SkillLoader = func() []corelib.NLSkillEntry { return []corelib.NLSkillEntry{entry} }
	var saveCalls, optimizedEvents, rollbackEvents, uploads atomic.Int32
	p.SkillSaver = func([]corelib.NLSkillEntry) error { saveCalls.Add(1); return nil }
	p.EventEmitter = func(event string, _ map[string]string) {
		if event == EventSkillOptimized {
			optimizedEvents.Add(1)
		}
		if event == EventSkillEvolutionRolledBack {
			rollbackEvents.Add(1)
		}
	}
	p.UploadTrigger = func(string, *SkillExecutionResultCompat) { uploads.Add(1) }
	p.DefinitionWriter = func(*corelib.NLSkillEntry) error { return fmt.Errorf("injected YAML failure") }

	res := p.TriggerOptimize(context.Background(), &entry, true)
	if res.Optimized || !res.Attempted {
		t.Fatalf("result = %+v, want attempted but not optimized", res)
	}
	if optimizedEvents.Load() != 0 || uploads.Load() != 0 {
		t.Fatalf("optimized events=%d uploads=%d, want both zero", optimizedEvents.Load(), uploads.Load())
	}
	if rollbackEvents.Load() != 1 {
		t.Fatalf("rollback events=%d, want 1", rollbackEvents.Load())
	}
	if saveCalls.Load() != 2 {
		t.Fatalf("save calls=%d, want forward and rollback", saveCalls.Load())
	}
}

func TestMergeEvolvedEntry_PreservesLiveStats(t *testing.T) {
	dst := &corelib.NLSkillEntry{
		Name: "s", Description: "old", Status: "active",
		UsageCount: 10, SuccessCount: 8, FailureCount: 2,
		LastError: "latest error", LastUsedAt: "2026-01-02T00:00:00Z",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "old"}}},
	}
	src := &corelib.NLSkillEntry{
		Name: "s", Description: "improved", Status: "needs_review",
		UsageCount: 1, SuccessCount: 0, FailureCount: 1,
		LastError: "stale error", LastUsedAt: "2025-01-01T00:00:00Z",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "new"}}},
		RepairAttemptCount: 2, LastRepairAt: "2026-01-01T00:00:00Z",
		RepairHistory:     []corelib.SkillRepairRecord{{Explanation: "fix"}},
		OptimizationCount: 3, LastOptimizedAt: "2026-01-03T00:00:00Z",
	}
	mergeEvolvedEntry(dst, src)

	// 进化修改的字段从 src 拷贝。
	if dst.Description != "improved" || dst.Status != "needs_review" {
		t.Fatalf("desc/status not merged: %q %q", dst.Description, dst.Status)
	}
	if len(dst.Steps) != 1 || dst.Steps[0].Params["command"] != "new" {
		t.Fatalf("steps not merged: %#v", dst.Steps)
	}
	if dst.RepairAttemptCount != 2 || dst.LastRepairAt == "" || len(dst.RepairHistory) != 1 {
		t.Fatalf("repair metadata not merged: %#v", dst)
	}
	if dst.OptimizationCount != 3 || dst.LastOptimizedAt == "" {
		t.Fatalf("optimization metadata not merged: %#v", dst)
	}
	// dst 的实时计数字段必须保留。
	if dst.UsageCount != 10 || dst.SuccessCount != 8 || dst.FailureCount != 2 {
		t.Fatalf("usage stats clobbered: %#v", dst)
	}
	if dst.LastError != "latest error" || dst.LastUsedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("last error/used clobbered: %#v", dst)
	}
}

func TestEvolutionPipeline_tryFileBackedRepairDraft(t *testing.T) {
	skillDir := t.TempDir()
	// draft 流要求技能目录里有 skill.yaml/skill.yml（SKILL.md-only 技能在
	// LLM 调用前就被跳过）。
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: file-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "file-skill", Source: "file", SkillDir: skillDir, Status: "active",
		UsageCount: 5, SuccessCount: 0, FailureCount: 5,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	if ok, reason := ExplainRepairGate(entry); ok || reason != "file_backed" {
		t.Fatalf("fixture gate = (%v, %q), want (false, file_backed)", ok, reason)
	}

	var llmCalls atomic.Int32
	p := NewEvolutionPipeline()
	p.RepairCooldown = time.Millisecond
	p.LLM = &stubRepairLLM{
		respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}],"should_disable":false}`,
		onCall:  func([]map[string]string) { llmCalls.Add(1) },
	}
	var emitted map[string]string
	p.EventEmitter = func(event string, payload map[string]string) {
		if event == EventSkillRepairDraftReady {
			emitted = payload
		}
	}
	req := evolutionRequest{
		SkillName:  "file-skill",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p.tryRepair(context.Background(), req)

	// entry 不被修改：draft 流不落地任何变更。
	if entry.RepairAttemptCount != 0 || len(entry.RepairHistory) != 0 {
		t.Fatalf("entry repair metadata mutated: %#v", entry)
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Params["command"] != "foo" {
		t.Fatalf("entry steps mutated: %#v", entry.Steps)
	}

	// draft 文件已写盘且内容完整。
	draftsDir := filepath.Join(skillDir, RepairDraftsDirName)
	files, err := os.ReadDir(draftsDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("draft files = %v, err = %v, want exactly 1", files, err)
	}
	data, err := os.ReadFile(filepath.Join(draftsDir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var draft RepairDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Skill != "file-skill" || draft.Explanation != "use echo" || draft.CreatedAt == "" {
		t.Fatalf("draft = %#v", draft)
	}
	if draft.LastError != entry.LastError {
		t.Fatalf("draft.LastError = %q", draft.LastError)
	}
	if len(draft.OldSteps) != 1 || draft.OldSteps[0].Params["command"] != "foo" {
		t.Fatalf("draft.OldSteps = %#v", draft.OldSteps)
	}
	if len(draft.NewSteps) != 1 || draft.NewSteps[0].Params["command"] != "echo fixed" {
		t.Fatalf("draft.NewSteps = %#v", draft.NewSteps)
	}

	// 事件携带 skill + draft 文件名。
	if emitted == nil || emitted["skill"] != "file-skill" || emitted["draft"] != files[0].Name() {
		t.Fatalf("event payload = %#v, draft file = %s", emitted, files[0].Name())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("llm calls = %d, want 1", llmCalls.Load())
	}

	// 已有未评审 draft 时不再调 LLM、不重复生成（越过冷却后再试）。
	time.Sleep(5 * time.Millisecond)
	p.tryRepair(context.Background(), req)
	if llmCalls.Load() != 1 {
		t.Fatalf("pending draft should skip LLM, calls = %d", llmCalls.Load())
	}
	files, err = os.ReadDir(draftsDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("draft files after retry = %v, err = %v, want exactly 1", files, err)
	}
}

func TestEvolutionPipeline_TriggerFileBackedRepairDraft_ForceCreatesReviewedDraft(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: local-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "local-skill", Source: "file", SkillDir: skillDir, Status: "active",
		// Deliberately below the automatic threshold: force must only bypass
		// that statistical gate, never the reviewed-draft requirement.
		UsageCount: 1, FailureCount: 1,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	p := NewEvolutionPipeline()
	p.LLM = &stubRepairLLM{respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}]}`}
	result := p.TriggerFileBackedRepairDraft(context.Background(), entry, nil, true)
	if !result.Created || !result.RequiresReview || result.Draft == "" {
		t.Fatalf("result = %#v, want a reviewed draft", result)
	}
	if _, err := os.Stat(filepath.Join(skillDir, RepairDraftsDirName, result.Draft)); err != nil {
		t.Fatalf("draft not written: %v", err)
	}
	if entry.Steps[0].Params["command"] != "foo" {
		t.Fatalf("manual draft must not mutate file-backed entry: %#v", entry.Steps)
	}
}

func TestMergeEvolvedEntry_MergesRepairMarkers(t *testing.T) {
	newPair := func(srcErr string) (*corelib.NLSkillEntry, *corelib.NLSkillEntry) {
		dst := &corelib.NLSkillEntry{Name: "s", LastError: "live error"}
		src := &corelib.NLSkillEntry{Name: "s", LastError: srcErr}
		return dst, src
	}

	// 修复产物标记必须拷贝，否则落盘的仍是修复前旧错误、技能会被重复修复。
	for _, marker := range []string{"auto-repaired: rewrote command", "auto-disabled: unfixable"} {
		dst, src := newPair(marker)
		mergeEvolvedEntry(dst, src)
		if dst.LastError != marker {
			t.Fatalf("marker %q not merged, LastError = %q", marker, dst.LastError)
		}
	}

	// 普通实时错误统计仍保留 dst 的值。
	dst, src := newPair("stale pre-repair error")
	mergeEvolvedEntry(dst, src)
	if dst.LastError != "live error" {
		t.Fatalf("live LastError clobbered: %q", dst.LastError)
	}
}

// errorRepairLLM 总是让 LLM 调用失败，用于验证"LLM 已调用但失败"也消耗冷却。
type errorRepairLLM struct{ configured bool }

func (e *errorRepairLLM) IsConfigured() bool { return e.configured }
func (e *errorRepairLLM) ChatCall([]map[string]string) (string, error) {
	return "", fmt.Errorf("simulated LLM outage")
}

func repairCooldownRecorded(p *EvolutionPipeline, skill string) bool {
	p.throttleMu.Lock()
	defer p.throttleMu.Unlock()
	_, ok := p.repairAttempts[skill]
	return ok
}

func repairEligibleCoreEntry() *corelib.NLSkillEntry {
	return &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, SuccessCount: 0,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
}

// TestEvolutionPipeline_tryRepair_LLMErrorConsumesCooldown covers the core
// (non-hook, non-file-backed) path: an LLM call that was actually made but
// failed must consume the cooldown, otherwise a persistently failing skill
// would burn one LLM call on every failed execution.
func TestEvolutionPipeline_tryRepair_LLMErrorConsumesCooldown(t *testing.T) {
	req := evolutionRequest{
		SkillName:  "broken",
		Entry:      repairEligibleCoreEntry(),
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p := NewEvolutionPipeline()
	p.LLM = &errorRepairLLM{configured: true}
	p.tryRepair(context.Background(), req)
	if !repairCooldownRecorded(p, "broken") {
		t.Fatal("failed LLM call must consume the repair cooldown")
	}

	// LLM 未配置：没有真实调用成本，不写时间戳。
	p2 := NewEvolutionPipeline()
	p2.LLM = &errorRepairLLM{configured: false}
	p2.tryRepair(context.Background(), req)
	if repairCooldownRecorded(p2, "broken") {
		t.Fatal("unconfigured LLM must not consume the repair cooldown")
	}

	p3 := NewEvolutionPipeline() // LLM nil
	p3.tryRepair(context.Background(), req)
	if repairCooldownRecorded(p3, "broken") {
		t.Fatal("nil LLM must not consume the repair cooldown")
	}
}

func TestRecordRepairAttemptFailureEnforcesSharedLimit(t *testing.T) {
	entry := repairEligibleCoreEntry()
	for i := 0; i < SelfRepairMaxAttempts; i++ {
		if entry.Status != "active" {
			t.Fatalf("attempt %d status=%q before limit, want active", i+1, entry.Status)
		}
		RecordRepairAttemptFailure(entry, "gate_rejected", fmt.Sprintf("rejected-%d", i+1))
		want := i + 1
		if entry.RepairAttemptCount != want {
			t.Fatalf("attempt count=%d, want %d", entry.RepairAttemptCount, want)
		}
	}
	if entry.Status != "needs_review" {
		t.Fatalf("status after limit=%q, want needs_review", entry.Status)
	}
	if ok, reason := ExplainRepairGate(entry); ok || reason != "status_not_active" {
		t.Fatalf("ExplainRepairGate after limit=(%v,%q), want status_not_active", ok, reason)
	}
	if CanForceAttemptRepair(entry) {
		t.Fatal("force repair must not bypass the shared attempt limit")
	}
	RecordRepairAttemptFailure(entry, "gate_rejected", "extra")
	if entry.RepairAttemptCount != SelfRepairMaxAttempts {
		t.Fatalf("attempt count must remain bounded at %d, got %d", SelfRepairMaxAttempts, entry.RepairAttemptCount)
	}
}

// TestEvolutionPipeline_tryFileBackedRepairDraft_CooldownSemantics covers the
// draft path: LLM failure consumes the cooldown; a draft that cannot be
// written to disk does not (nothing was produced, immediate retry is OK).
func TestEvolutionPipeline_tryFileBackedRepairDraft_CooldownSemantics(t *testing.T) {
	newFileBackedReq := func(skillDir string) evolutionRequest {
		// draft 流要求技能目录里有 skill.yaml/skill.yml（SKILL.md-only 技能
		// 在 LLM 调用前就被跳过）。
		if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: file-skill\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return evolutionRequest{
			SkillName: "file-skill",
			Entry: &corelib.NLSkillEntry{
				Name: "file-skill", Source: "file", SkillDir: skillDir, Status: "active",
				UsageCount: 5, SuccessCount: 0, FailureCount: 5,
				LastError: "[class: command_not_found] missing foo",
				Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
			},
			ExecResult: &SkillExecutionResultCompat{Success: false},
		}
	}

	t.Run("LLM error consumes cooldown", func(t *testing.T) {
		p := NewEvolutionPipeline()
		p.LLM = &errorRepairLLM{configured: true}
		p.tryRepair(context.Background(), newFileBackedReq(t.TempDir()))
		if !repairCooldownRecorded(p, "file-skill") {
			t.Fatal("failed LLM call must consume the repair cooldown on the draft path")
		}
	})

	t.Run("draft write failure keeps cooldown", func(t *testing.T) {
		skillDir := t.TempDir()
		// 把 .evolution-drafts 预先建成普通文件，让 WriteRepairDraft 的
		// MkdirAll 失败，模拟写盘失败。
		if err := os.WriteFile(filepath.Join(skillDir, RepairDraftsDirName), []byte("block"), 0o644); err != nil {
			t.Fatal(err)
		}
		var emitted atomic.Int32
		p := NewEvolutionPipeline()
		p.LLM = &stubRepairLLM{
			respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}],"should_disable":false}`,
		}
		p.EventEmitter = func(event string, payload map[string]string) { emitted.Add(1) }
		p.tryRepair(context.Background(), newFileBackedReq(skillDir))
		if emitted.Load() != 0 {
			t.Fatal("no event expected when draft write fails")
		}
		if repairCooldownRecorded(p, "file-skill") {
			t.Fatal("failed draft write must not consume the repair cooldown")
		}
	})

	t.Run("unconfigured LLM keeps cooldown", func(t *testing.T) {
		p := NewEvolutionPipeline()
		p.LLM = &errorRepairLLM{configured: false}
		p.tryRepair(context.Background(), newFileBackedReq(t.TempDir()))
		if repairCooldownRecorded(p, "file-skill") {
			t.Fatal("unconfigured LLM must not consume the repair cooldown on the draft path")
		}
	})
}
