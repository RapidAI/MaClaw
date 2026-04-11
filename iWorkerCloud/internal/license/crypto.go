package license

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

const (
	rsaKeyBits     = 2048
	privateKeyFile = "license_private.pem"
	publicKeyFile  = "license_public.pem"
)

// EnsureKeyPair generates RSA key pair if not exists, loads if exists.
func EnsureKeyPair(dataDir string) (*rsa.PrivateKey, error) {
	privPath := filepath.Join(dataDir, privateKeyFile)
	pubPath := filepath.Join(dataDir, publicKeyFile)

	if privKey, err := loadPrivateKey(privPath); err == nil {
		return privKey, nil
	}

	privKey, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dataDir, err)
	}

	privDER := x509.MarshalPKCS1PrivateKey(privKey)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	return privKey, nil
}

// LoadPublicKeyPEM returns the public key PEM bytes.
func LoadPublicKeyPEM(dataDir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dataDir, publicKeyFile))
}

// SignData signs data with RSA-SHA256.
func SignData(privKey *rsa.PrivateKey, data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
}

// VerifySignature verifies RSA-SHA256 signature.
func VerifySignature(pubKey *rsa.PublicKey, data, sig []byte) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig)
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
