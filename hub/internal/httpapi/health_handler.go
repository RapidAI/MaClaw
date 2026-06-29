package httpapi

import (
	"net/http"
	"sync/atomic"
)

// hubReady is set to true once all critical subsystems are initialized:
// - MaClawModule (LLM provider client, auth sync)
// - LLM Provider Registry loaded/cached
// - HTTP listener accepting connections
//
// Before hubReady is true, the /healthz/ready endpoint returns 503,
// signaling to nginx/load-balancers that this instance should not receive
// traffic yet. This prevents 503 cascades during Hub redeployment.
var hubReady atomic.Bool

// MarkHubReady signals that the Hub instance is fully initialized and
// ready to serve LLM requests. Should be called after all critical
// subsystems (MaClawModule, AuthSync, ProviderRegistry) are initialized.
func MarkHubReady() {
	hubReady.Store(true)
}

// IsHubReady returns whether the Hub instance is fully initialized.
func IsHubReady() bool {
	return hubReady.Load()
}

func HealthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": serviceName,
		})
	}
}

// ReadinessHandler returns 200 only when the Hub is fully initialized and
// ready to handle LLM requests. Returns 503 during startup bootstrap.
// Use this endpoint for nginx upstream health checks during rolling deploys.
func ReadinessHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !hubReady.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":      false,
				"service": serviceName,
				"reason":  "hub is still initializing",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": serviceName,
			"ready":   true,
		})
	}
}
