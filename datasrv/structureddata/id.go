package structureddata

import (
	"crypto/rand"
	"fmt"
	"strings"
)

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.TrimSpace(prefix) + "_fallback"
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s_%x-%x-%x-%x-%x", prefix, b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
