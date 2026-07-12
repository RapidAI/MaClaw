package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var warnOnce sync.Once

func AdminKey() string {
	if v := strings.TrimSpace(os.Getenv("DS2API_ADMIN_KEY")); v != "" {
		return v
	}
	warnOnce.Do(func() {
		slog.Warn("DS2API_ADMIN_KEY is not set! Using insecure default \"admin\". Set a strong key in production!")
	})
	return "admin"
}

func jwtSecret() string {
	if v := strings.TrimSpace(os.Getenv("DS2API_JWT_SECRET")); v != "" {
		return v
	}
	return AdminKey()
}

func jwtExpireHours() int {
	if v := strings.TrimSpace(os.Getenv("DS2API_JWT_EXPIRE_HOURS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 24
}

func CreateJWT(expireHours int) (string, error) {
	if expireHours <= 0 {
		expireHours = jwtExpireHours()
	}
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{"iat": time.Now().Unix(), "exp": time.Now().Add(time.Duration(expireHours) * time.Hour).Unix(), "role": "admin"}
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	headerB64 := rawB64Encode(h)
	payloadB64 := rawB64Encode(p)
	msg := headerB64 + "." + payloadB64
	sig := signHS256(msg)
	return msg + "." + rawB64Encode(sig), nil
}

func VerifyJWT(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}
	msg := parts[0] + "." + parts[1]
	expected := signHS256(msg)
	actual, err := rawB64Decode(parts[2])
	if err != nil {
		return nil, errors.New("invalid signature")
	}
	if !hmac.Equal(expected, actual) {
		return nil, errors.New("invalid signature")
	}
	payloadBytes, err := rawB64Decode(parts[1])
	if err != nil {
		return nil, errors.New("invalid payload")
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, errors.New("invalid payload")
	}
	exp, _ := payload["exp"].(float64)
	if int64(exp) < time.Now().Unix() {
		return nil, errors.New("token expired")
	}
	return payload, nil
}

func VerifyAdminRequest(r *http.Request) error {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return errors.New("authentication required")
	}
	token := strings.TrimSpace(authHeader[7:])
	if token == "" {
		return errors.New("authentication required")
	}
	if token == AdminKey() {
		return nil
	}
	if _, err := VerifyJWT(token); err == nil {
		return nil
	}
	return errors.New("invalid credentials")
}

func signHS256(msg string) []byte {
	h := hmac.New(sha256.New, []byte(jwtSecret()))
	_, _ = h.Write([]byte(msg))
	return h.Sum(nil)
}

func rawB64Encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func rawB64Decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
