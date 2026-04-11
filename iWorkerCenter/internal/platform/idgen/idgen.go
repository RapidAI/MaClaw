package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New generates a time-prefixed random ID: "yyyyMMddHHmmss-<8 random hex bytes>".
func New(prefix string) string {
	ts := time.Now().Format("20060102150405")
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s-%s", prefix, ts, hex.EncodeToString(b))
}
