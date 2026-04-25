package ha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
