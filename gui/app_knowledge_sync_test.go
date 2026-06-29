package main

import "testing"

func TestKnowledgeSyncPasswordVerifier(t *testing.T) {
	verifier, err := encryptKnowledgeSyncPasswordVerifier("sync-secret")
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	if len(verifier) == 0 || verifier["ciphertext"] == "" {
		t.Fatalf("verifier missing ciphertext: %+v", verifier)
	}
	if err := decryptKnowledgeSyncPasswordVerifier("sync-secret", verifier); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if err := decryptKnowledgeSyncPasswordVerifier("wrong-secret", verifier); err == nil {
		t.Fatal("wrong password accepted")
	}
}
