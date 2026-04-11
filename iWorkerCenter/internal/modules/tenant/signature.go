package tenant

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"sync"
	"time"
)

// PublicKeyCache caches the iWorkerCloud RSA public key with TTL-based refresh.
type PublicKeyCache struct {
	mu        sync.RWMutex
	pubKey    *rsa.PublicKey
	fetchedAt time.Time
	ttl       time.Duration
	fetcher   func(ctx context.Context) ([]byte, error)
}

func NewPublicKeyCache(ttlHours int, fetcher func(ctx context.Context) ([]byte, error)) *PublicKeyCache {
	if ttlHours <= 0 {
		ttlHours = 24
	}
	return &PublicKeyCache{
		ttl:     time.Duration(ttlHours) * time.Hour,
		fetcher: fetcher,
	}
}

// Get returns the cached public key, refreshing if expired.
func (c *PublicKeyCache) Get(ctx context.Context) (*rsa.PublicKey, error) {
	c.mu.RLock()
	if c.pubKey != nil && time.Since(c.fetchedAt) < c.ttl {
		k := c.pubKey
		c.mu.RUnlock()
		return k, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// double-check after acquiring write lock
	if c.pubKey != nil && time.Since(c.fetchedAt) < c.ttl {
		return c.pubKey, nil
	}

	pemData, err := c.fetcher(ctx)
	if err != nil {
		if c.pubKey != nil {
			log.Printf("[tenant] failed to refresh public key, using cached: %v", err)
			return c.pubKey, nil
		}
		return nil, fmt.Errorf("fetch public key: %w", err)
	}

	key, err := ParseRSAPublicKeyPEM(pemData)
	if err != nil {
		if c.pubKey != nil {
			log.Printf("[tenant] failed to parse refreshed public key, using cached: %v", err)
			return c.pubKey, nil
		}
		return nil, err
	}

	c.pubKey = key
	c.fetchedAt = time.Now()
	return c.pubKey, nil
}

// Warmup pre-fetches the public key at startup.
func (c *PublicKeyCache) Warmup(ctx context.Context) {
	if _, err := c.Get(ctx); err != nil {
		log.Printf("[tenant] public key warmup failed: %v", err)
	}
}

// ParseRSAPublicKeyPEM parses a PEM-encoded RSA public key.
func ParseRSAPublicKeyPEM(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in public key data")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not RSA")
	}
	return rsaPub, nil
}

// VerifyProvisionSignature verifies an RSA-SHA256 signature.
// The signed payload is: "{timestamp}:{nonce}:{bodyHashHex}"
func VerifyProvisionSignature(pubKey *rsa.PublicKey, timestamp int64, nonce string, bodyHashHex string, signature []byte) error {
	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	hash := sha256.Sum256([]byte(payload))
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
}
