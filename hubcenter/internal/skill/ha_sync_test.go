package skill

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingSkillSyncRecorder struct {
	mu      sync.Mutex
	count   int
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func (r *blockingSkillSyncRecorder) AppendSkillHubSnapshot(context.Context, *Snapshot) {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	r.started <- struct{}{}
	<-r.release
	r.done <- struct{}{}
}

func (r *blockingSkillSyncRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func TestCountSnapshotRecordsAvoidsDumpingSnapshot(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	if err := store.Publish(HubSkillFull{HubSkillMeta: HubSkillMeta{ID: "skill-1", Name: "Skill 1", Visible: true}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := store.Publish(HubSkillFull{HubSkillMeta: HubSkillMeta{ID: "skill-2", Name: "Skill 2", Visible: true}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := store.Rate("skill-1", "maclaw-1", 5); err != nil {
		t.Fatalf("Rate() error = %v", err)
	}
	if err := store.Rate("skill-1", "maclaw-2", 4); err != nil {
		t.Fatalf("Rate() error = %v", err)
	}

	if got := store.CountSnapshotRecords(); got != 4 {
		t.Fatalf("CountSnapshotRecords() = %d, want 4", got)
	}
}

func TestSkillHubEmitSyncCoalescesConcurrentSnapshots(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	recorder := &blockingSkillSyncRecorder{
		started: make(chan struct{}, 4),
		release: make(chan struct{}, 4),
		done:    make(chan struct{}, 4),
	}
	store.SetSyncRecorder(recorder)

	store.emitSync(context.Background())
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("first sync emission did not start")
	}
	store.emitSync(context.Background())
	store.emitSync(context.Background())

	recorder.release <- struct{}{}
	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("first sync emission did not finish")
	}
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("coalesced sync emission did not start")
	}
	recorder.release <- struct{}{}
	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("coalesced sync emission did not finish")
	}

	select {
	case <-recorder.started:
		t.Fatal("unexpected third sync emission")
	case <-time.After(100 * time.Millisecond):
	}
	if got := recorder.Count(); got != 2 {
		t.Fatalf("sync emissions = %d, want 2", got)
	}
}
