package llmservice

import (
	"crypto/rand"
	"encoding/hex"
)

// newLoginSessionID is the shared random id for every vendor's in-memory
// login. One session names one browser approval.
func newLoginSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
