package ha

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newSigningV2Key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return key
}

func TestSignPeerRequestAddsV2SignatureAndBodyHash(t *testing.T) {
	senderKey := newSigningV2Key(t)
	s := NewService("hc-send", "sender", "https://send.example.com", "", nil)
	s.SetNodeKeyMaterial(&NodeKeyMaterial{PrivateKey: senderKey})

	body := []byte(`{"ops":[{"seq":1}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply?x=1", bytes.NewReader(body))

	if err := s.SignPeerRequest(req); err != nil {
		t.Fatalf("SignPeerRequest() error = %v", err)
	}

	if req.Header.Get(haHeaderSignatureV2) == "" {
		t.Fatal("expected V2 signature header to be set")
	}
	if req.Header.Get(haHeaderBodyHash) == "" {
		t.Fatal("expected body hash header to be set")
	}
	if req.Header.Get(haHeaderSignature) == "" {
		t.Fatal("expected V1 signature header to be set")
	}

	// The body must be restored so downstream consumers can still read it.
	bodyHash, err := requestBodySHA256Hex(req)
	if err != nil {
		t.Fatalf("requestBodySHA256Hex() error = %v", err)
	}
	if bodyHash != req.Header.Get(haHeaderBodyHash) {
		t.Fatalf("body hash mismatch: header=%q computed=%q", req.Header.Get(haHeaderBodyHash), bodyHash)
	}
	canonical := requestCanonicalPayloadV2(req, "hc-send", req.Header.Get(haHeaderTimestamp), bodyHash)
	if err := verifyHACanonicalRequest(&senderKey.PublicKey, canonical, req.Header.Get(haHeaderSignatureV2)); err != nil {
		t.Fatalf("V2 signature does not verify: %v", err)
	}
}

func TestAuthenticatePeerRequestRejectsTamperedBody(t *testing.T) {
	senderKey := newSigningV2Key(t)
	sender := NewService("hc-send", "sender", "https://send.example.com", "", nil)
	sender.SetNodeKeyMaterial(&NodeKeyMaterial{PrivateKey: senderKey})
	receiver := NewService("hc-recv", "receiver", "https://recv.example.com", "", []StaticPeer{{
		NodeID:       "hc-send",
		PublicKeyPEM: encodeRSAPublicKeyPEM(&senderKey.PublicKey),
	}})

	body := []byte(`{"ops":[{"seq":1}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply", bytes.NewReader(body))
	if err := sender.SignPeerRequest(req); err != nil {
		t.Fatalf("SignPeerRequest() error = %v", err)
	}

	if err := receiver.AuthenticatePeerRequest(req); err != nil {
		t.Fatalf("AuthenticatePeerRequest() valid request error = %v", err)
	}

	// Same signature headers, different body: the V2 signature must reject it.
	tampered := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply", bytes.NewReader([]byte(`{"ops":[{"seq":999}]}`)))
	tampered.Header = req.Header.Clone()
	if err := receiver.AuthenticatePeerRequest(tampered); err == nil {
		t.Fatal("AuthenticatePeerRequest() accepted a tampered body")
	}
}

func TestAuthenticatePeerRequestV1FallbackAndEnforcement(t *testing.T) {
	senderKey := newSigningV2Key(t)
	receiver := NewService("hc-recv", "receiver", "https://recv.example.com", "", []StaticPeer{{
		NodeID:       "hc-send",
		PublicKeyPEM: encodeRSAPublicKeyPEM(&senderKey.PublicKey),
	}})

	body := []byte(`{"ops":[]}`)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	canonical := canonicalHARequest(http.MethodPost, "/api/internal/ha/ops/apply", "", "hc-send", timestamp)
	sig, err := signHACanonicalRequest(senderKey, canonical)
	if err != nil {
		t.Fatalf("signHACanonicalRequest() error = %v", err)
	}

	buildV1Only := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/ha/ops/apply", bytes.NewReader(body))
		req.Header.Set(haHeaderNodeID, "hc-send")
		req.Header.Set(haHeaderTimestamp, timestamp)
		req.Header.Set(haHeaderSignature, sig)
		return req
	}

	// Accepted while V2 enforcement is disabled (rolling-upgrade grace period).
	if err := receiver.AuthenticatePeerRequest(buildV1Only()); err != nil {
		t.Fatalf("AuthenticatePeerRequest() V1 fallback error = %v", err)
	}

	// Rejected once V2 is enforced.
	receiver.SetRequireSignatureV2(true)
	if err := receiver.AuthenticatePeerRequest(buildV1Only()); err == nil {
		t.Fatal("AuthenticatePeerRequest() accepted a V1-only request when V2 is required")
	}
}
