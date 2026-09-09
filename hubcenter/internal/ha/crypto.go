package ha

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
)

const (
	haHeaderNodeID      = "X-HubCenter-Node"
	haHeaderTimestamp   = "X-HubCenter-Timestamp"
	haHeaderSignature   = "X-HubCenter-Signature"
	haHeaderBodyHash    = "X-HubCenter-Body-Hash"
	haHeaderSignatureV2 = "X-HubCenter-Signature-V2"
)

// haMaxSignedBodyBytes bounds how much of a request body is buffered while
// computing its SHA-256 for V2 signing/verification. It matches the HA
// ops/apply JSON body ceiling (128 MiB) so a legitimate batch is never
// truncated while an oversized body is rejected on the pre-auth path.
const haMaxSignedBodyBytes = 128 << 20

type NodeKeyMaterial struct {
	PrivateKey     *rsa.PrivateKey
	PublicKeyPEM   string
	PrivateKeyPath string
}

func EnsureNodeKeyPair(dataDir string, cfg *config.HAConfig) (*NodeKeyMaterial, error) {
	if cfg == nil {
		return nil, fmt.Errorf("ha config is required")
	}
	privatePath := strings.TrimSpace(cfg.PrivateKeyPath)
	if privatePath == "" {
		privatePath = filepath.Join(dataDir, "ha_keys", safeHAKeyFileName(cfg.NodeID, cfg.SelfFQDN)+".pem")
	}
	if err := os.MkdirAll(filepath.Dir(privatePath), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(privatePath); err == nil {
		privateKey, err := loadRSAPrivateKeyFromFile(privatePath)
		if err != nil {
			return nil, err
		}
		cfg.PrivateKeyPath = privatePath
		return &NodeKeyMaterial{PrivateKey: privateKey, PublicKeyPEM: encodeRSAPublicKeyPEM(&privateKey.PublicKey), PrivateKeyPath: privatePath}, nil
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	pemBytes, err := encodeRSAPrivateKeyPEM(privateKey)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(privatePath, pemBytes, 0o600); err != nil {
		return nil, err
	}
	cfg.PrivateKeyPath = privatePath
	return &NodeKeyMaterial{PrivateKey: privateKey, PublicKeyPEM: encodeRSAPublicKeyPEM(&privateKey.PublicKey), PrivateKeyPath: privatePath}, nil
}

func loadRSAPrivateKeyFromFile(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRSAPrivateKeyPEM(string(data))
}

func parseRSAPrivateKeyPEM(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("invalid rsa private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not rsa")
	}
	return key, nil
}

func parseRSAPublicKeyPEM(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("invalid rsa public key pem")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not rsa")
		}
		return pub, nil
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported rsa public key format")
}

func encodeRSAPrivateKeyPEM(key *rsa.PrivateKey) ([]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("private key is required")
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func encodeRSAPublicKeyPEM(key *rsa.PublicKey) string {
	if key == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func safeHAKeyFileName(nodeID, fqdn string) string {
	base := strings.TrimSpace(nodeID)
	if base == "" {
		base = strings.TrimSpace(fqdn)
	}
	if base == "" {
		base = "hubcenter-node"
	}
	re := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	base = re.ReplaceAllString(base, "_")
	base = strings.Trim(base, "._-")
	if base == "" {
		base = "hubcenter-node"
	}
	return base
}

func canonicalHARequest(method, path, rawQuery, nodeID, timestamp string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + "\n" + strings.TrimSpace(path) + "\n" + strings.TrimSpace(rawQuery) + "\n" + strings.TrimSpace(nodeID) + "\n" + strings.TrimSpace(timestamp)
}

// canonicalHARequestV2 extends the V1 canonical request with the lowercase
// hex SHA-256 of the raw request body, so a signature cannot be replayed
// against a different body. The leading "v2" line prevents ambiguity with the
// V1 format (a V1 canonical string is never a prefix of a V2 one).
func canonicalHARequestV2(method, path, rawQuery, nodeID, timestamp, bodyHashHex string) string {
	return "v2\n" + canonicalHARequest(method, path, rawQuery, nodeID, timestamp) + "\n" + strings.TrimSpace(bodyHashHex)
}

func signHACanonicalRequest(privateKey *rsa.PrivateKey, canonical string) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("private key is required")
	}
	digest := sha256.Sum256([]byte(canonical))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func verifyHACanonicalRequest(publicKey *rsa.PublicKey, canonical, signature string) error {
	if publicKey == nil {
		return fmt.Errorf("public key is required")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(canonical))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], sig)
}

func timestampWithinHABounds(value string, now time.Time, skew time.Duration) error {
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return err
	}
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	if delta > skew {
		return fmt.Errorf("timestamp outside allowed skew")
	}
	return nil
}

func requestCanonicalPayload(r *http.Request, nodeID, timestamp string) string {
	if r == nil || r.URL == nil {
		return canonicalHARequest("GET", "", "", nodeID, timestamp)
	}
	return canonicalHARequest(r.Method, r.URL.EscapedPath(), r.URL.RawQuery, nodeID, timestamp)
}

func requestCanonicalPayloadV2(r *http.Request, nodeID, timestamp, bodyHashHex string) string {
	if r == nil || r.URL == nil {
		return canonicalHARequestV2("GET", "", "", nodeID, timestamp, bodyHashHex)
	}
	return canonicalHARequestV2(r.Method, r.URL.EscapedPath(), r.URL.RawQuery, nodeID, timestamp, bodyHashHex)
}

// requestBodySHA256Hex reads and hashes the raw request body, then restores it
// so downstream consumers can still read it. Nil and empty bodies hash to the
// SHA-256 of the empty byte slice. Reading is bounded to haMaxSignedBodyBytes
// to prevent an unbounded-memory DoS on the pre-auth verification path.
func requestBodySHA256Hex(r *http.Request) (string, error) {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:]), nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, haMaxSignedBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read request body: %w", err)
	}
	if int64(len(body)) > haMaxSignedBodyBytes {
		return "", fmt.Errorf("request body exceeds %d bytes", haMaxSignedBodyBytes)
	}
	if err := r.Body.Close(); err != nil {
		return "", fmt.Errorf("close request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
