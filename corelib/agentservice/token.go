package agentservice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

type tokenClaims struct {
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id"`
	CredentialID string    `json:"credential_id,omitempty"`
	JTI          string    `json:"jti,omitempty"`
	Exp          time.Time `json:"exp"`
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func HashSecret(secret string) string {
	return HashSecretWithPepper(secret, "")
}

func HashSecretWithPepper(secret, pepper string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return ""
	}
	key, err := scrypt.Key(secretWithPepper(secret, pepper), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(pepper) != "" {
		return fmt.Sprintf("scrypt$32768$8$1$peppered$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(key))
	}
	return fmt.Sprintf("scrypt$32768$8$1$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(key))
}

func (m *TokenManager) Issue(p Principal) (string, time.Time, error) {
	return m.IssueForCredential(p, "")
}

func (m *TokenManager) IssueForCredential(p Principal, credentialID string) (string, time.Time, error) {
	if len(m.secret) == 0 {
		return "", time.Time{}, errors.New("token secret is empty")
	}
	exp := time.Now().Add(m.ttl)
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, err
	}
	claims := tokenClaims{TenantID: p.TenantID, UserID: p.UserID, CredentialID: credentialID, JTI: hex.EncodeToString(jti), Exp: exp}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := m.sign(body)
	return body + "." + sig, exp, nil
}

func VerifySecret(secret, digest string) bool {
	return VerifySecretWithPepper(secret, digest, "")
}

func VerifySecretWithPepper(secret, digest, pepper string) bool {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(digest) == "" {
		return false
	}
	parts := strings.Split(digest, "$")
	switch {
	case len(parts) == 7 && parts[0] == "scrypt" && parts[4] == "peppered":
		if strings.TrimSpace(pepper) == "" {
			return false
		}
		salt, err := hex.DecodeString(parts[5])
		if err != nil {
			return false
		}
		expected, err := hex.DecodeString(parts[6])
		if err != nil {
			return false
		}
		key, err := scrypt.Key(secretWithPepper(secret, pepper), salt, 1<<15, 8, 1, 32)
		if err != nil {
			return false
		}
		return subtle.ConstantTimeCompare(key, expected) == 1
	case len(parts) == 6 && parts[0] == "scrypt":
		salt, err := hex.DecodeString(parts[4])
		if err != nil {
			return false
		}
		expected, err := hex.DecodeString(parts[5])
		if err != nil {
			return false
		}
		key, err := scrypt.Key(secretWithPepper(secret, ""), salt, 1<<15, 8, 1, 32)
		if err != nil {
			return false
		}
		return subtle.ConstantTimeCompare(key, expected) == 1
	}
	legacy := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(legacy[:])), []byte(digest)) == 1
}

func secretWithPepper(secret, pepper string) []byte {
	if strings.TrimSpace(pepper) == "" {
		return []byte(secret)
	}
	return []byte(secret + "::pepper::" + pepper)
}

func (m *TokenManager) Parse(token string) (*Principal, time.Time, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, time.Time{}, "", ErrUnauthorized
	}
	if !hmac.Equal([]byte(m.sign(parts[0])), []byte(parts[1])) {
		return nil, time.Time{}, "", ErrUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, time.Time{}, "", ErrUnauthorized
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, time.Time{}, "", ErrUnauthorized
	}
	if time.Now().After(claims.Exp) {
		return nil, time.Time{}, "", ErrUnauthorized
	}
	return &Principal{TenantID: claims.TenantID, UserID: claims.UserID, Roles: []string{"user"}}, claims.Exp, claims.CredentialID, nil
}

func (m *TokenManager) sign(body string) string {
	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
