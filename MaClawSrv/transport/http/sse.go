// Package transporthttp contains protocol-only HTTP helpers shared by the
// MaClawSrv transport handlers. It intentionally has no dependency on the
// application service or server composition types.
package transporthttp

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ParseLastEventID accepts either the numeric per-run sequence form or an
// opaque durable event id. The caller resolves opaque ids through its event
// store; keeping parsing here prevents handlers from drifting in edge-case
// handling.
func ParseLastEventID(raw string) (sequence uint64, opaqueID string) {
	if raw == "" {
		return 0, ""
	}
	if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
		return parsed, ""
	}
	return 0, raw
}

// WriteSSEEvent writes one sanitized SSE event and flushes it immediately.
// Event names and IDs are persisted data, so line breaks are stripped before
// they reach protocol fields and cannot inject additional SSE records.
func WriteSSEEvent(w io.Writer, flusher http.Flusher, eventID, eventName string, payload []byte) error {
	eventName = strings.NewReplacer("\r", "", "\n", "").Replace(eventName)
	if eventName == "" {
		return fmt.Errorf("event name is required")
	}
	eventID = strings.NewReplacer("\r", "", "\n", "").Replace(eventID)
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", eventID, eventName, payload); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
