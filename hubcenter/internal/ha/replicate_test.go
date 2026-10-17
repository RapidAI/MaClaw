package ha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestSyncPeerSnapshotCatchUpThenTail(t *testing.T) {
	payload := `{"id":"n-1","title":"caught up"}`
	sum := sha256.Sum256([]byte(payload))
	head := &store.HASyncOp{
		Seq: 4, OpID: "op-head", SourceNodeID: "hc-2", EntityType: EntityNewsArticle, EntityID: "n-1",
		OpType: OpUpsert, EntityVersion: 1, OccurredAt: time.Now().UTC(),
		PayloadJSON: payload, PayloadHash: hex.EncodeToString(sum[:]),
	}
	tailPayload := `{"id":"n-2","title":"tail"}`
	tailSum := sha256.Sum256([]byte(tailPayload))
	tail := &store.HASyncOp{
		Seq: 12, OpID: "op-tail", SourceNodeID: "hc-2", EntityType: EntityNewsArticle, EntityID: "n-2",
		OpType: OpUpsert, EntityVersion: 1, OccurredAt: time.Now().UTC(),
		PayloadJSON: tailPayload, PayloadHash: hex.EncodeToString(tailSum[:]),
	}
	var sawProtocol, sawAck bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/internal/ha/ops", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ProtocolHeader) == strconv.Itoa(ProtocolVersion) {
			sawProtocol = true
		}
		if r.URL.Query().Get("ack_seq") != "" && r.URL.Query().Get("node_id") == "hc-1" {
			sawAck = true
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
		resp := PullOpsResponse{NodeID: "hc-2", MinSeq: 10, MaxSeq: 12, Protocol: ProtocolVersion}
		if after >= 4 {
			resp.Ops = []*store.HASyncOp{tail}
			resp.NextAfterSeq = tail.Seq
			resp.HasMore = false
		} else {
			resp.NextAfterSeq = after
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/internal/ha/snapshot", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
		resp := PullOpsResponse{NodeID: "hc-2", Protocol: ProtocolVersion, HasMore: false}
		if after < head.Seq {
			resp.Ops = []*store.HASyncOp{head}
			resp.NextAfterSeq = head.Seq
		} else {
			resp.NextAfterSeq = after
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	news := &fakeHANewsRepo{}
	peer := &PeerRuntimeState{NodeID: "hc-2", BaseURL: server.URL}
	cursors := &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{}}
	svc := &Service{
		nodeID:   "hc-1",
		peers:    map[string]*PeerRuntimeState{"hc-2": peer},
		cursors:  cursors,
		ops:      &fakeHASyncOpRepo{},
		versions: &fakeHAEntityVersionRepo{},
		news:     news,
	}
	syncer := NewSyncer(svc, time.Second, 50)
	syncer.syncPeer(context.Background(), peer)

	if !sawProtocol || !sawAck {
		t.Fatalf("protocol header=%v ack query=%v", sawProtocol, sawAck)
	}
	if news.items["n-1"] == nil || news.items["n-1"].Title != "caught up" {
		t.Fatalf("snapshot head not applied: %+v", news.items)
	}
	if news.items["n-2"] == nil || news.items["n-2"].Title != "tail" {
		t.Fatalf("tail op not applied: %+v", news.items)
	}
	got, err := cursors.Get(context.Background(), "hc-2")
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if got == nil || got.LastPulledSeq != 12 {
		t.Fatalf("cursor = %+v, want seq 12", got)
	}
}

func TestSyncPeerOldPeerWithoutSnapshotStillAppliesTail(t *testing.T) {
	payload := `{"id":"n-9","title":"tail-only"}`
	sum := sha256.Sum256([]byte(payload))
	op := &store.HASyncOp{
		Seq: 3, OpID: "op-old", SourceNodeID: "hc-2", EntityType: EntityNewsArticle, EntityID: "n-9",
		OpType: OpUpsert, EntityVersion: 1, OccurredAt: time.Now().UTC(),
		PayloadJSON: payload, PayloadHash: hex.EncodeToString(sum[:]),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/ha/ops" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(PullOpsResponse{
			NodeID: "hc-2", Ops: []*store.HASyncOp{op}, NextAfterSeq: 3, MaxSeq: 3, MinSeq: 3,
		})
	}))
	defer server.Close()
	news := &fakeHANewsRepo{}
	peer := &PeerRuntimeState{NodeID: "hc-2", BaseURL: server.URL}
	svc := &Service{
		nodeID:   "hc-1",
		peers:    map[string]*PeerRuntimeState{"hc-2": peer},
		cursors:  &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{}},
		ops:      &fakeHASyncOpRepo{},
		versions: &fakeHAEntityVersionRepo{},
		news:     news,
	}
	NewSyncer(svc, time.Second, 20).syncPeer(context.Background(), peer)
	if news.items["n-9"] == nil {
		t.Fatal("old peer tail was not applied")
	}
}
