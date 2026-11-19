package edgelink

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ViewerClaims is a screen ticket Hub signs with the desktopd key agreed at
// enrollment. desktopd checks the signature itself, then asks Hub again when
// a WebSocket opens so a withdrawn view stops immediately.
type ViewerClaims struct {
	ID       string `json:"id"`
	NodeID   string `json:"node"`
	TenantID string `json:"tenant"`
	UserID   string `json:"user"`
	UserKey  string `json:"key"`
	Display  string `json:"display"`
	Control  bool   `json:"control"`
	Exp      int64  `json:"exp"`
}

// SignViewer builds a ticket. The key is the desktopd API token both sides
// kept at enrollment. It is not written into the ticket.
func SignViewer(key string, claims ViewerClaims) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.TrimSpace(claims.NodeID) == "" || strings.TrimSpace(claims.UserKey) == "" || claims.Exp <= 0 {
		return "", errors.New("viewer ticket is incomplete")
	}
	if strings.TrimSpace(claims.ID) == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		claims.ID = base64.RawURLEncoding.EncodeToString(buf)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return "v1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyViewer checks the signature and the expiry. A wrong key or a changed
// byte fails closed. The key is not compared in the clear.
func VerifyViewer(key, ticket string, now time.Time) (ViewerClaims, error) {
	key = strings.TrimSpace(key)
	parts := strings.Split(strings.TrimSpace(ticket), ".")
	if key == "" || len(parts) != 3 || parts[0] != "v1" {
		return ViewerClaims{}, errors.New("viewer ticket is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ViewerClaims{}, errors.New("viewer ticket is invalid")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ViewerClaims{}, errors.New("viewer ticket is invalid")
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ViewerClaims{}, errors.New("viewer ticket is invalid")
	}
	var claims ViewerClaims
	if json.Unmarshal(payload, &claims) != nil || claims.ID == "" || claims.UserKey == "" || claims.NodeID == "" {
		return ViewerClaims{}, errors.New("viewer ticket is invalid")
	}
	if !now.Before(time.Unix(claims.Exp, 0)) {
		return ViewerClaims{}, errors.New("viewer ticket expired")
	}
	return claims, nil
}
