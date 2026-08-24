package tool

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// Artifact payloads are encrypted at rest with AES-256-GCM when the host
// injects a 32-byte key (WithArtifactEncryptionKey). The stored form keeps a
// format prefix so databases holding pre-encryption plaintext rows stay
// readable:
//
//	enc:v1:<base64(nonce || ciphertext)>   encrypted (this slice and later)
//	<base64 plaintext>                     legacy plaintext, still accepted
//
// The prefix contains ':' which never appears in standard base64, so legacy
// rows can never be mistaken for ciphertext. Reads without a configured key
// fail closed on encrypted rows (artifact_encryption_key_required) instead of
// silently returning unreadable bytes.
//
// The AEAD additional data is the artifact store key, which binds ciphertext
// to the immutable scope/artifact identity: copying an encrypted payload to
// another artifact row fails authentication.
//
// Integrity semantics are unchanged: ArtifactRef.IntegrityDigest is always
// computed over the plaintext bytes (NewArtifactPayload), because its purpose
// is end-to-end content integrity for consumers. Encryption here only adds
// confidentiality at rest; it is not the integrity mechanism.
const artifactEncryptionPrefix = "enc:v1:"

// deriveArtifactEncryptionKey derives the AEAD key from the host-injected
// 32-byte key so the raw host secret is never used directly as a cipher key.
func deriveArtifactEncryptionKey(hostKey []byte) ([]byte, error) {
	if len(hostKey) != 32 {
		return nil, fmt.Errorf("artifact_encryption_key_invalid")
	}
	reader := hkdf.New(sha256.New, hostKey, []byte("maclaw-semantic-artifact-store"), []byte("artifact-payload-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// encodeStoredArtifactPayload encrypts a plaintext base64 payload for storage.
// Without a configured key it returns the plaintext unchanged (documented
// legacy behavior: payload remains base64 plaintext at rest).
func encodeStoredArtifactPayload(key []byte, artifactKey, plaintextBase64 string) (string, error) {
	if len(key) == 0 {
		return plaintextBase64, nil
	}
	plaintext, err := base64.StdEncoding.DecodeString(plaintextBase64)
	if err != nil {
		return "", fmt.Errorf("artifact content is not valid base64")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plaintext, []byte(artifactKey))
	return artifactEncryptionPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// decodeStoredArtifactPayload reverses encodeStoredArtifactPayload. Legacy
// plaintext rows (no prefix) pass through unchanged; encrypted rows require
// the configured key and authenticate against the artifact identity.
func decodeStoredArtifactPayload(key []byte, artifactKey, stored string) (string, error) {
	if !strings.HasPrefix(stored, artifactEncryptionPrefix) {
		return stored, nil
	}
	if len(key) == 0 {
		return "", fmt.Errorf("artifact_encryption_key_required")
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, artifactEncryptionPrefix))
	if err != nil {
		return "", fmt.Errorf("artifact_store_corrupt")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", fmt.Errorf("artifact_store_corrupt")
	}
	plaintext, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(artifactKey))
	if err != nil {
		return "", fmt.Errorf("artifact_store_corrupt")
	}
	return base64.StdEncoding.EncodeToString(plaintext), nil
}
