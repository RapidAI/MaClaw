package device

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatchCandidatesEmitsOnlyAfterBaselineChanges(t *testing.T) {
	var mu sync.Mutex
	current := []Candidate{{Port: "COM4", VendorID: "10c4", ProductID: "ea60", IsUSB: true}}
	list := func() ([]Candidate, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]Candidate(nil), current...), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan ChangeEvent, 2)
	go WatchCandidates(ctx, list, WatchPolicy{PollInterval: 5 * time.Millisecond}, func(event ChangeEvent) { events <- event })

	time.Sleep(15 * time.Millisecond)
	select {
	case event := <-events:
		t.Fatalf("baseline unexpectedly emitted: %+v", event)
	default:
	}

	mu.Lock()
	current = []Candidate{{Port: "COM7", VendorID: "303a", ProductID: "1001", IsUSB: true, LikelyEsp: true}}
	mu.Unlock()
	select {
	case event := <-events:
		if len(event.Previous) != 1 || event.Previous[0].Port != "COM4" || len(event.Candidates) != 1 || event.Candidates[0].Port != "COM7" {
			t.Fatalf("unexpected change event: %+v", event)
		}
		if event.DetectedAt.IsZero() {
			t.Fatal("change event has no detection timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for device change")
	}
}

func TestNormalizeCandidatesIgnoresEnumerationOrderAndCasing(t *testing.T) {
	left := normalizeCandidates([]Candidate{{Port: " COM7 ", VendorID: "303a", ProductID: "1001"}, {Port: "COM4", VendorID: "10c4", ProductID: "ea60"}})
	right := normalizeCandidates([]Candidate{{Port: "COM4", VendorID: "10C4", ProductID: "EA60"}, {Port: "COM7", VendorID: "303A", ProductID: "1001"}})
	if !sameCandidates(left, right) {
		t.Fatalf("equivalent snapshots were treated as changed: %#v != %#v", left, right)
	}
}
