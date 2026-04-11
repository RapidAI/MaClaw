package tenant

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
)

func testKeyPair(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return privKey, pubPEM
}

func TestVerifyProvisionSignature_Valid(t *testing.T) {
	privKey, _ := testKeyPair(t)

	timestamp := int64(1700000000)
	nonce := "test-nonce-abc"
	bodyHashHex := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	hash := sha256.Sum256([]byte(payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	err = VerifyProvisionSignature(&privKey.PublicKey, timestamp, nonce, bodyHashHex, sig)
	if err != nil {
		t.Errorf("expected valid signature, got: %v", err)
	}
}

func TestVerifyProvisionSignature_WrongKey(t *testing.T) {
	privKey, _ := testKeyPair(t)
	otherKey, _ := testKeyPair(t)

	timestamp := int64(1700000000)
	nonce := "test-nonce"
	bodyHashHex := "abc123"

	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	hash := sha256.Sum256([]byte(payload))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])

	err := VerifyProvisionSignature(&otherKey.PublicKey, timestamp, nonce, bodyHashHex, sig)
	if err == nil {
		t.Error("expected failure with wrong key")
	}
}

func TestVerifyProvisionSignature_TamperedNonce(t *testing.T) {
	privKey, _ := testKeyPair(t)

	timestamp := int64(1700000000)
	nonce := "original-nonce"
	bodyHashHex := "abc123"

	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	hash := sha256.Sum256([]byte(payload))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])

	err := VerifyProvisionSignature(&privKey.PublicKey, timestamp, "tampered-nonce", bodyHashHex, sig)
	if err == nil {
		t.Error("expected failure with tampered nonce")
	}
}

func TestParseRSAPublicKeyPEM(t *testing.T) {
	_, pubPEM := testKeyPair(t)

	key, err := ParseRSAPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestParseRSAPublicKeyPEM_Invalid(t *testing.T) {
	_, err := ParseRSAPublicKeyPEM([]byte("not a pem"))
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestPublicKeyCache_Warmup(t *testing.T) {
	_, pubPEM := testKeyPair(t)

	cache := NewPublicKeyCache(24, func(ctx context.Context) ([]byte, error) {
		return pubPEM, nil
	})

	cache.Warmup(context.Background())

	key, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestPublicKeyCache_FetchFailUsesCache(t *testing.T) {
	_, pubPEM := testKeyPair(t)

	callCount := 0
	cache := NewPublicKeyCache(0, func(ctx context.Context) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return pubPEM, nil
		}
		return nil, fmt.Errorf("network error")
	})

	// First call succeeds
	key1, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("first get: %v", err)
	}

	// Force TTL expiry
	cache.mu.Lock()
	cache.fetchedAt = cache.fetchedAt.Add(-cache.ttl * 2)
	cache.mu.Unlock()

	// Second call fails to fetch but returns cached key
	key2, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if key2 != key1 {
		t.Error("expected cached key to be returned")
	}
}

func TestPublicKeyCache_NoCache_FetchFail(t *testing.T) {
	cache := NewPublicKeyCache(24, func(ctx context.Context) ([]byte, error) {
		return nil, fmt.Errorf("unavailable")
	})

	_, err := cache.Get(context.Background())
	if err == nil {
		t.Error("expected error when no cache and fetch fails")
	}
}
