package maclawappcontract

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// VerifyHubPackageSignature verifies a signed MaClaw App Hub package and
// returns the normalized sha256 public-key fingerprint trusted by GUI installs.
func VerifyHubPackageSignature(pkg map[string]any) (string, error) {
	signatureMap := anyMap(pkg["package_signature"])
	if signatureMap == nil {
		return "", nil
	}
	algorithm := strings.ToLower(strings.TrimSpace(stringValue(signatureMap["algorithm"])))
	if algorithm == "" || algorithm != "ed25519" {
		return "", nil
	}
	payload := strings.TrimSpace(stringValue(signatureMap["payload"]))
	if payload == "" {
		return "", fmt.Errorf("maclaw app package signature payload is missing")
	}
	publicKey, err := decodeSignatureBytes(firstString(signatureMap["public_key_base64"], signatureMap["public_key"]))
	if err != nil {
		return "", fmt.Errorf("maclaw app package signature invalid public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("maclaw app package signature invalid public key length: got %d", len(publicKey))
	}
	fingerprint := HubPackagePublicKeyFingerprint(publicKey)
	declared := normalizePublicKeyFingerprint(firstString(signatureMap["public_key_fingerprint"], signatureMap["key_fingerprint"], signatureMap["fingerprint"]))
	if declared != "" && declared != fingerprint {
		return "", fmt.Errorf("maclaw app package signature public key fingerprint mismatch: expected %s, got %s", declared, fingerprint)
	}
	if signedSHA := normalizeSHA256(stringValue(signatureMap["package_sha256"])); signedSHA != "" {
		if packageSHA := normalizeSHA256(stringValue(pkg["package_sha256"])); packageSHA != "" && packageSHA != signedSHA {
			return "", fmt.Errorf("maclaw app package signature checksum mismatch: expected sha256 %s, got %s", signedSHA, packageSHA)
		}
	}
	signature, err := decodeSignatureBytes(firstString(signatureMap["signature_base64"], signatureMap["signature"]))
	if err != nil {
		return "", fmt.Errorf("maclaw app package signature invalid signature bytes: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return "", fmt.Errorf("maclaw app package signature invalid signature length: got %d", len(signature))
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(payload), signature) {
		return "", fmt.Errorf("maclaw app package signature verification failed")
	}
	return fingerprint, nil
}

// HubPackagePublicKeyFingerprint matches the GUI Skill package trust format.
func HubPackagePublicKeyFingerprint(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func decodeSignatureBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "base64:")
	value = strings.TrimPrefix(value, "ed25519:")
	if value == "" {
		return nil, fmt.Errorf("empty value")
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func normalizePublicKeyFingerprint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "fingerprint:")
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "sha256:") && len(strings.TrimPrefix(value, "sha256:")) >= 16 {
		return value
	}
	return ""
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return ""
	}
	return value
}
