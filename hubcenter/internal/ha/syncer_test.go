package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestSyncPeerClearsBacklogOnEmptySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&PullOpsResponse{
			NodeID:       "hc-2",
			Ops:          []*store.HASyncOp{},
			NextAfterSeq: 123,
			HasMore:      false,
			MaxSeq:       123,
		})
	}))
	defer server.Close()

	oldSuccess := time.Now().UTC().Add(-20 * time.Minute)
	peer := &PeerRuntimeState{
		NodeID:        "hc-2",
		NodeName:      "HubCenter 2",
		BaseURL:       server.URL,
		Reachable:     true,
		Backlog:       1713,
		LagSeconds:    1286,
		LastSuccessAt: &oldSuccess,
	}
	svc := &Service{
		nodeID:  "hc-1",
		peers:   map[string]*PeerRuntimeState{"hc-2": peer},
		cursors: &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{"hc-2": {PeerNodeID: "hc-2", LastPulledSeq: 123}}},
		ops:     &fakeHASyncOpRepo{},
	}
	syncer := NewSyncer(svc, time.Second, 200)

	syncer.syncPeer(context.Background(), peer)

	if peer.Backlog != 0 {
		t.Fatalf("peer.Backlog = %d, want 0", peer.Backlog)
	}
	if peer.LagSeconds != 0 {
		t.Fatalf("peer.LagSeconds = %d, want 0", peer.LagSeconds)
	}
	if peer.LastSuccessAt == nil || !peer.LastSuccessAt.After(oldSuccess) {
		t.Fatalf("peer.LastSuccessAt = %#v, want newer than %v", peer.LastSuccessAt, oldSuccess)
	}

	quality, err := svc.GetClientQuality(context.Background())
	if err != nil {
		t.Fatalf("GetClientQuality() error = %v", err)
	}
	if quality.Sync.Backlog != 0 {
		t.Fatalf("quality.Sync.Backlog = %d, want 0", quality.Sync.Backlog)
	}
}

func TestSyncPeerAdvancesCursorFromEmptyResponseNextSeq(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&PullOpsResponse{
			NodeID:       "hc-2",
			Ops:          []*store.HASyncOp{},
			NextAfterSeq: 250,
			HasMore:      false,
			MaxSeq:       250,
		})
	}))
	defer server.Close()

	peer := &PeerRuntimeState{NodeID: "hc-2", NodeName: "HubCenter 2", BaseURL: server.URL, Backlog: 20}
	cursors := &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{"hc-2": {PeerNodeID: "hc-2", LastPulledSeq: 200}}}
	svc := &Service{
		nodeID:  "hc-1",
		peers:   map[string]*PeerRuntimeState{"hc-2": peer},
		cursors: cursors,
		ops:     &fakeHASyncOpRepo{},
	}
	syncer := NewSyncer(svc, time.Second, 200)

	syncer.syncPeer(context.Background(), peer)

	got, err := cursors.Get(context.Background(), "hc-2")
	if err != nil {
		t.Fatalf("cursor Get() error = %v", err)
	}
	if got == nil || got.LastPulledSeq != 250 {
		t.Fatalf("LastPulledSeq = %#v, want 250", got)
	}
	if peer.Backlog != 0 {
		t.Fatalf("peer.Backlog = %d, want 0", peer.Backlog)
	}
}

func TestSyncPeerRecordsPullErrorInCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid cluster secret", http.StatusUnauthorized)
	}))
	defer server.Close()

	oldSuccess := time.Now().UTC().Add(-time.Hour)
	peer := &PeerRuntimeState{NodeID: "hc-2", NodeName: "HubCenter 2", BaseURL: server.URL}
	cursors := &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{"hc-2": {
		PeerNodeID:    "hc-2",
		LastPulledSeq: 200,
		LastSuccessAt: &oldSuccess,
	}}}
	svc := &Service{
		nodeID:  "hc-1",
		peers:   map[string]*PeerRuntimeState{"hc-2": peer},
		cursors: cursors,
		ops:     &fakeHASyncOpRepo{},
	}
	syncer := NewSyncer(svc, time.Second, 200)

	syncer.syncPeer(context.Background(), peer)

	got, err := cursors.Get(context.Background(), "hc-2")
	if err != nil {
		t.Fatalf("cursor Get() error = %v", err)
	}
	if got == nil || got.LastPulledSeq != 200 {
		t.Fatalf("LastPulledSeq = %#v, want 200", got)
	}
	if got.LastPulledAt == nil {
		t.Fatalf("LastPulledAt = nil, want pull attempt timestamp")
	}
	if got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(oldSuccess) {
		t.Fatalf("LastSuccessAt = %#v, want preserved %v", got.LastSuccessAt, oldSuccess)
	}
	if got.LastError != "pull ops failed: 401 Unauthorized" {
		t.Fatalf("LastError = %q", got.LastError)
	}
}

func TestSyncAllSkipsPeerAlreadyRunning(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		switch n {
		case 1:
			close(firstStarted)
			<-releaseFirst
		case 2:
			close(secondStarted)
		}
		_ = json.NewEncoder(w).Encode(&PullOpsResponse{
			NodeID:       "hc-2",
			Ops:          []*store.HASyncOp{},
			NextAfterSeq: 0,
			HasMore:      false,
			MaxSeq:       0,
		})
	}))
	defer server.Close()

	peer := &PeerRuntimeState{NodeID: "hc-2", NodeName: "HubCenter 2", BaseURL: server.URL}
	svc := &Service{
		nodeID:  "hc-1",
		peers:   map[string]*PeerRuntimeState{"hc-2": peer},
		cursors: &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{}},
		ops:     &fakeHASyncOpRepo{},
	}
	syncer := NewSyncer(svc, time.Second, 200)

	syncer.syncAll(context.Background())
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first sync did not start")
	}
	syncer.syncAll(context.Background())
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	gotDuringFirst := requests
	mu.Unlock()
	if gotDuringFirst != 1 {
		t.Fatalf("requests while first sync is running = %d, want 1", gotDuringFirst)
	}

	close(releaseFirst)
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		syncer.syncAll(context.Background())
		select {
		case <-secondStarted:
			return
		case <-ticker.C:
			continue
		case <-deadline:
			t.Fatal("second sync did not start after first completed")
		}
	}
}

func TestSyncAllCapsConcurrentPeerSyncs(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	requests := 0
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		requests++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(&PullOpsResponse{
			NodeID:       "hc-peer",
			Ops:          []*store.HASyncOp{},
			NextAfterSeq: 0,
			HasMore:      false,
			MaxSeq:       0,
		})
	}))
	defer server.Close()

	peers := make(map[string]*PeerRuntimeState)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("hc-peer-%d", i)
		peers[id] = &PeerRuntimeState{NodeID: id, NodeName: id, BaseURL: server.URL}
	}
	svc := &Service{
		nodeID:  "hc-1",
		peers:   peers,
		cursors: &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{}},
		ops:     &fakeHASyncOpRepo{},
	}
	syncer := NewSyncer(svc, time.Second, 200)
	syncer.slots = make(chan struct{}, 2)

	syncer.syncAll(context.Background())
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("sync did not reach concurrency cap")
		}
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	gotRequests := requests
	gotMaxActive := maxActive
	mu.Unlock()
	if gotRequests != 2 {
		t.Fatalf("requests before releasing slots = %d, want 2", gotRequests)
	}
	if gotMaxActive > 2 {
		t.Fatalf("max active syncs = %d, want <= 2", gotMaxActive)
	}
	close(release)
}
