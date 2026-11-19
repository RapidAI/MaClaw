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

// LocalClaims lets a same-node srv read a screenshot or a file from desktopd
// without the bytes crossing Hub. The desktopd API token signs it and is not
// written into the ticket.
type LocalClaims struct {
	ID       string `json:"id"`
	NodeID   string `json:"node"`
	TenantID string `json:"tenant"`
	UserID   string `json:"user"`
	Action   string `json:"action"`
	Exp      int64  `json:"exp"`
}

// SignLocal builds a d1 ticket. action is "screenshot" or "file".
func SignLocal(key string, claims LocalClaims) (string, error) {
	key = strings.TrimSpace(key)
	claims.Action = strings.TrimSpace(claims.Action)
	if key == "" || strings.TrimSpace(claims.NodeID) == "" || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.UserID) == "" || claims.Exp <= 0 {
		return "", errors.New("local ticket is incomplete")
	}
	if claims.Action != "screenshot" && claims.Action != "file" {
		return "", errors.New("local ticket action is invalid")
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
	mac := hmac.New(sha256.New, []byte("local:"+key))
	_, _ = mac.Write(payload)
	return "d1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyLocal checks the signature, the action and the expiry. A viewer
// ticket does not verify here: the prefix and the key domain differ.
func VerifyLocal(key, ticket string, now time.Time) (LocalClaims, error) {
	key = strings.TrimSpace(key)
	parts := strings.Split(strings.TrimSpace(ticket), ".")
	if key == "" || len(parts) != 3 || parts[0] != "d1" {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	mac := hmac.New(sha256.New, []byte("local:"+key))
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	var claims LocalClaims
	if json.Unmarshal(payload, &claims) != nil || claims.ID == "" || claims.NodeID == "" || claims.UserID == "" {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	if claims.Action != "screenshot" && claims.Action != "file" {
		return LocalClaims{}, errors.New("local ticket is invalid")
	}
	if !now.Before(time.Unix(claims.Exp, 0)) {
		return LocalClaims{}, errors.New("local ticket expired")
	}
	return claims, nil
}
