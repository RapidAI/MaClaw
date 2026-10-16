package httpapi

import (
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

// LinkHealthMetricsHandler serves the device<->brain link health snapshot for
// the companion terminal (plan N0-3).
//
// It reports three things the plan needs before any offline-resilience work can
// be judged: how often a paired MaClaw GUI was actually reachable (online
// ratio, per tenant), how often the Hub had to answer 503 gui_offline, and how
// many device events reached a live brain.
//
// Read-only and cumulative for the process lifetime, mirroring VEMetricsHandler.
// The caller must wrap this in an admin guard: the numbers describe every
// tenant the Hub serves.
func LinkHealthMetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, im.LinkHealth().Snapshot(time.Now()))
	}
}
