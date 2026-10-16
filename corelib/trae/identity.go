package trae

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// AccountIdentity derives a stable per-account device fingerprint. Upstream
// risk control reacts to N accounts behind one shared machine id, so the
// identity is keyed on the account's user id and never rotates with the
// access token.
type AccountIdentity struct {
	UserID    string
	MachineID string // 64 hex chars
	DeviceID  string // decimal string, the SOLO device id shape
}

// IdentityForUserID derives the machine/device pair for one account. The
// digest is keyed (not a plain hash) so the derived ids never accidently
// mirror another tool's derivation.
func IdentityForUserID(userID string) AccountIdentity {
	uid := strings.TrimSpace(userID)
	digest := func(tag string) []byte {
		mac := hmac.New(sha256.New, []byte("maclaw-trae-"+tag))
		mac.Write([]byte(uid))
		return mac.Sum(nil)
	}
	machine := hex.EncodeToString(digest("machine"))
	return AccountIdentity{
		UserID:    uid,
		MachineID: machine,
		DeviceID:  hashDeviceID(machine),
	}
}

// hashDeviceID folds a machine id into the decimal device id shape the SOLO
// client uses for its device ids: a fixed 16-digit number whose first digit
// is 1-9 (never zero-padded, never 17+ digits).
func hashDeviceID(machineID string) string {
	sum := sha256.Sum256([]byte(machineID))
	var value uint64
	for _, b := range sum[:8] {
		value = value<<8 | uint64(b)
	}
	value %= 9_000_000_000_000_000
	value += 1_000_000_000_000_000
	return decimal(value)
}

func decimal(value uint64) string {
	if value == 0 {
		return "0"
	}
	var raw [20]byte
	index := len(raw)
	for value > 0 {
		index--
		raw[index] = byte('0' + value%10)
		value /= 10
	}
	return string(raw[index:])
}

// randomHex returns 2n hex chars.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// Login cannot proceed without random bits; surface the failure the
		// same way StartLogin does.
		return ""
	}
	return hex.EncodeToString(buf)
}

// jwtClaims decodes the access token's payload without verifying the
// signature — claims are only read to recover the account id (stable across
// token rotation).
type jwtClaims struct {
	UID        string
	UserID     string
	Enterprise string
	Region     string
}

func parseJWTClaims(token string) jwtClaims {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return jwtClaims{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return jwtClaims{}
	}
	// Claims may be strings or numbers depending on the deployment.
	claims := jwtClaims{
		UID:        firstNonEmptyStringClaim(payload["uid"], payload["userId"], payload["user_id"], payload["sub"]),
		UserID:     firstNonEmptyStringClaim(payload["userId"], payload["user_id"]),
		Enterprise: stringClaim(payload["enterprise_id"]),
	}
	if dataRaw, ok := payload["data"].(map[string]any); ok {
		claims.UserID = firstNonEmptyStringClaim(dataRaw["id"], dataRaw["uid"], dataRaw["user_id"])
		claims.Enterprise = strings.TrimSpace(stringClaim(dataRaw["enterprise_id"]))
	}
	if strings.TrimSpace(claims.UID) == "" {
		claims.UID = claims.UserID
	}
	var region struct {
		Region string `json:"region"`
	}
	if raw, ok := payload["userRegion"].(map[string]any); ok {
		if json.Unmarshal(mustJSONValue(raw), &region) == nil {
			claims.Region = strings.TrimSpace(region.Region)
		}
	}
	return claims
}

func mustJSONValue(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

// firstNonEmptyStringClaim accepts string or numeric JSON values (the
// upstream serializes user ids either way) and returns the first non-empty.
func firstNonEmptyStringClaim(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		case float64:
			if v > 0 {
				return decimal(uint64(v))
			}
		}
	}
	return ""
}

func stringClaim(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
