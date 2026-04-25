package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeHASyncReader struct {
	nodeID     string
	maxSeq     int64
	ops        []*store.HASyncOp
	gotAfter   int64
	gotLimit   int
	listCalled bool
	authFn     func(r *http.Request) error
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
