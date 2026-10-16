package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeHASyncReader struct {
	nodeID     string
	maxSeq     int64
	ops        []*store.HASyncOp
	applied    []*store.HASyncOp
	gotAfter   int64
	gotLimit   int
	listCalled bool
	authFn     func(r *http.Request) error
	applyErr   error
}

func (f *fakeHASyncReader) NodeID() string { return f.nodeID }

func (f *fakeHASyncReader) AuthenticatePeerRequest(r *http.Request) error {
	if f.authFn == nil {
		return nil
	}
	return f.authFn(r)
}

func (f *fakeHASyncReader) ListOpsAfterSeq(_ context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error) {
	f.gotAfter = afterSeq
	f.gotLimit = limit
	f.listCalled = true
	return f.ops, nil
}

func (f *fakeHASyncReader) MaxOpSeq(_ context.Context) (int64, error) { return f.maxSeq, nil }

func (f *fakeHASyncReader) MinOpSeq(_ context.Context) (int64, error) { return 0, nil }

func (f *fakeHASyncReader) ApplyRemoteOps(_ context.Context, ops []*store.HASyncOp) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, ops...)
	return nil
}

func TestHAOpsPullRequiresAuthentication(t *testing.T) {
	svc := &fakeHASyncReader{nodeID: "hc-a", authFn: func(r *http.Request) error { return context.Canceled }}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops", nil)

	HAOpsPullHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if svc.listCalled {
		t.Fatalf("ListOpsAfterSeq() should not be called without valid auth")
	}
}

func TestHAOpsPullReturnsOpsWithValidAuth(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeHASyncReader{
		nodeID: "hc-a",
		authFn: func(r *http.Request) error { return nil },
		maxSeq: 9,
		ops: []*store.HASyncOp{
			{Seq: 6, OpID: "op-6", SourceNodeID: "hc-b", EntityType: "news_article", EntityID: "n-1", OpType: "upsert", EntityVersion: 1, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash"},
			{Seq: 7, OpID: "op-7", SourceNodeID: "hc-b", EntityType: "hub_instance", EntityID: "h-1", OpType: "upsert", EntityVersion: 2, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash"},
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops?after_seq=5&limit=2", nil)

	HAOpsPullHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if svc.gotAfter != 5 || svc.gotLimit != 2 {
		t.Fatalf("ListOpsAfterSeq(after=%d, limit=%d), want after=5 limit=2", svc.gotAfter, svc.gotLimit)
	}
	var payload struct {
		NodeID       string            `json:"node_id"`
		Ops          []*store.HASyncOp `json:"ops"`
		NextAfterSeq int64             `json:"next_after_seq"`
		HasMore      bool              `json:"has_more"`
		MaxSeq       int64             `json:"max_seq"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.NodeID != "hc-a" || payload.NextAfterSeq != 7 || !payload.HasMore || payload.MaxSeq != 9 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if len(payload.Ops) != 2 {
		t.Fatalf("ops len = %d, want 2", len(payload.Ops))
	}
}

func TestHAOpsPullCapsLimitAtLargeBatchSize(t *testing.T) {
	svc := &fakeHASyncReader{nodeID: "hc-a", authFn: func(r *http.Request) error { return nil }}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops?after_seq=5&limit=999999", nil)

	HAOpsPullHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if svc.gotLimit != 50000 {
		t.Fatalf("ListOpsAfterSeq limit = %d, want 50000", svc.gotLimit)
	}
}

func TestHAOpsPullHasMoreUsesMaxSeq(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeHASyncReader{
		nodeID: "hc-a",
		authFn: func(r *http.Request) error { return nil },
		maxSeq: 7,
		ops: []*store.HASyncOp{
			{Seq: 6, OpID: "op-6", SourceNodeID: "hc-b", EntityType: "news_article", EntityID: "n-1", OpType: "upsert", EntityVersion: 1, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash"},
			{Seq: 7, OpID: "op-7", SourceNodeID: "hc-b", EntityType: "hub_instance", EntityID: "h-1", OpType: "upsert", EntityVersion: 2, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash"},
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops?after_seq=5&limit=2", nil)

	HAOpsPullHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var payload struct {
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.HasMore {
		t.Fatalf("has_more = true, want false when max_seq equals next_after_seq")
	}
}

func TestHAOpsPullRejectsInvalidQuery(t *testing.T) {
	svc := &fakeHASyncReader{nodeID: "hc-a", authFn: func(r *http.Request) error { return nil }}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops?after_seq=-1", nil)

	HAOpsPullHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if svc.listCalled {
		t.Fatalf("ListOpsAfterSeq() should not be called for invalid query params")
	}
}

func TestHAOpsPullAcceptsSignedRequest(t *testing.T) {
	receiver := ha.NewService("hc-recv", "receiver", "https://recv.example.com", "shared-secret", []ha.StaticPeer{{NodeID: "hc-send", NodeName: "sender", BaseURL: "https://send.example.com", PublicKeyPEM: "placeholder"}})
	senderKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	sender := ha.NewService("hc-send", "sender", "https://send.example.com", "shared-secret", nil)
	sender.SetNodeKeyMaterial(&ha.NodeKeyMaterial{PrivateKey: senderKey})
	receiver = ha.NewService("hc-recv", "receiver", "https://recv.example.com", "shared-secret", []ha.StaticPeer{{NodeID: "hc-send", NodeName: "sender", BaseURL: "https://send.example.com", PublicKeyPEM: sender.PublicKeyPEM()}})

	svc := &fakeHASyncReader{nodeID: "hc-recv", authFn: receiver.AuthenticatePeerRequest}
	req := httptest.NewRequest(http.MethodGet, "/api/internal/ha/ops?after_seq=0&limit=1", nil)
	req.Header.Set("Authorization", "Bearer shared-secret")
	if err := sender.SignPeerRequest(req); err != nil {
		t.Fatalf("SignPeerRequest() error = %v", err)
	}
	rr := httptest.NewRecorder()

	HAOpsPullHandler(svc).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHAOpsApplyAppliesOpsWithValidAuth(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeHASyncReader{nodeID: "hc-a", authFn: func(r *http.Request) error { return nil }}
	body, _ := json.Marshal(map[string]any{
		"ops": []*store.HASyncOp{
			{Seq: 6, OpID: "op-6", SourceNodeID: "hc-b", EntityType: "news_article", EntityID: "n-1", OpType: "upsert", EntityVersion: 1, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "hash"},
		},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply", bytes.NewReader(body))

	HAOpsApplyHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if len(svc.applied) != 1 || svc.applied[0].OpID != "op-6" {
		t.Fatalf("applied = %#v", svc.applied)
	}
}

func TestHAOpsApplyRejectsInvalidRemoteOpAsBadRequest(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeHASyncReader{nodeID: "hc-a", authFn: func(r *http.Request) error { return nil }, applyErr: ha.InvalidRemoteOpError{Reason: "payload hash mismatch"}}
	body, _ := json.Marshal(map[string]any{
		"ops": []*store.HASyncOp{
			{Seq: 6, OpID: "op-6", SourceNodeID: "hc-b", EntityType: "news_article", EntityID: "n-1", OpType: "upsert", EntityVersion: 1, OccurredAt: now, PayloadJSON: `{}`, PayloadHash: "bad"},
		},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply", bytes.NewReader(body))

	HAOpsApplyHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("INVALID_HA_OP")) {
		t.Fatalf("body = %s, want INVALID_HA_OP", rr.Body.String())
	}
}

func TestTrimOpsToByteBudget(t *testing.T) {
	mk := func(seq int64) *store.HASyncOp {
		return &store.HASyncOp{Seq: seq, EntityType: "llm_official_class_head", EntityID: "head_v1", PayloadJSON: strings.Repeat("x", 1000)}
	}
	ops := []*store.HASyncOp{mk(1), mk(2), mk(3), mk(4)}

	// Single op over budget: keep exactly one so the batch still progresses.
	if got := trimOpsToByteBudget(ops, 1); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("single-op-over-budget trim = %d ops, want 1 (seq 1)", len(got))
	}
	// Budget fits a few ops: trimmed batch stays contiguous, under budget,
	// and strictly shorter than the input.
	got := trimOpsToByteBudget(ops, 3000)
	if len(got) == 0 || len(got) >= len(ops) {
		t.Fatalf("trimmed batch = %d ops, want between 1 and %d", len(got), len(ops)-1)
	}
	var encoded int64
	for _, op := range got {
		raw, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("marshal op: %v", err)
		}
		encoded += int64(len(raw)) + 1
	}
	if encoded > 3000 {
		t.Fatalf("trimmed batch encodes to %d bytes, want <= 3000", encoded)
	}
	for i, op := range got {
		if op.Seq != int64(i+1) {
			t.Fatalf("trimmed batch not contiguous at %d: seq=%d", i, op.Seq)
		}
	}
	// Generous budget: no trim.
	if got := trimOpsToByteBudget(ops, 1<<20); len(got) != 4 {
		t.Fatalf("no-trim case = %d ops, want 4", len(got))
	}
	// Empty input and zero budget are safe no-ops / guarded.
	if got := trimOpsToByteBudget(nil, 1000); len(got) != 0 {
		t.Fatalf("empty input = %d ops, want 0", len(got))
	}
	if got := trimOpsToByteBudget(ops, 0); len(got) != 4 {
		t.Fatalf("zero budget = %d ops, want untrimmed 4", len(got))
	}
}
