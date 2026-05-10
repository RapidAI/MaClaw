package skillmarket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const signedSessionTokenPrefix = "sm2."

type signedSessionPayload struct {
	Version   int    `json:"v"`
	UserID    string `json:"uid"`
	Email     string `json:"email"`
	ExpiresAt int64  `json:"exp"`
	CreatedAt int64  `json:"iat"`
	Nonce     string `json:"nonce"`
}

func (a *AuthService) newSessionToken(user *SkillMarketUser, createdAt, expiresAt time.Time) string {
	if a == nil || len(a.sessionSigningSecret) == 0 || user == nil {
		return generateToken()
	}
	payload := signedSessionPayload{
		Version:   1,
		UserID:    user.ID,
		Email:     normalizeEmail(user.Email),
		ExpiresAt: expiresAt.Unix(),
		CreatedAt: createdAt.Unix(),
		Nonce:     generateToken(),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return generateToken()
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return signedSessionTokenPrefix + payloadPart + "." + a.signSessionPayload(payloadPart)
}

func (a *AuthService) validateSignedSessionToken(token string) (*Session, error) {
	if a == nil || len(a.sessionSigningSecret) == 0 {
		return nil, ErrTokenExpired
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, signedSessionTokenPrefix) {
		return nil, ErrTokenExpired
	}
	parts := strings.Split(strings.TrimPrefix(token, signedSessionTokenPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrTokenExpired
	}
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenExpired
	}
	wantSig, err := base64.RawURLEncoding.DecodeString(a.signSessionPayload(parts[0]))
	if err != nil {
		return nil, ErrTokenExpired
	}
	if !hmac.Equal(gotSig, wantSig) {
		return nil, ErrTokenExpired
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrTokenExpired
	}
	var payload signedSessionPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, ErrTokenExpired
	}
	if payload.Version != 1 || strings.TrimSpace(payload.UserID) == "" || normalizeEmail(payload.Email) == "" {
		return nil, ErrTokenExpired
	}
	if payload.ExpiresAt <= 0 || time.Now().After(time.Unix(payload.ExpiresAt, 0)) {
		return nil, ErrTokenExpired
	}
	if payload.CreatedAt <= 0 || payload.CreatedAt > payload.ExpiresAt {
		return nil, ErrTokenExpired
	}
	return &Session{
		Token:     token,
		UserID:    payload.UserID,
		Email:     normalizeEmail(payload.Email),
		ExpiresAt: time.Unix(payload.ExpiresAt, 0),
		CreatedAt: time.Unix(payload.CreatedAt, 0),
	}, nil
}

func (a *AuthService) signSessionPayload(payloadPart string) string {
	mac := hmac.New(sha256.New, a.sessionSigningSecret)
	_, _ = mac.Write([]byte(payloadPart))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
