package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/config"
)

// spaFallback is set by main.go after buildMux returns, used for SPA routing.
var spaFallback http.Handler

// buildMux creates the unified HTTP mux combining:
// 1. Modular business API routes (roles, colleagues, capabilities, etc.)
// 2. LLM proxy routes (/v1/chat/completions, /v1/models)
// 3. IM webhook routes (/webhook/feishu, /webhook/dingtalk, /webhook/wecom)
// 4. Dashboard/settings API routes
// 5. Admin Web Console SPA at /admin/ (set via spaFallback after build)
func buildMux(cfg *config.Config) (*http.ServeMux, func(), error) {
	mux := http.NewServeMux()
	var cleanups []func()
	var centerMux *http.ServeMux
	var bootstrapErr error

	// --- 1. Bootstrap modular backend ---
	center, err := app.Bootstrap()
	if err != nil {
		bootstrapErr = err
		log.Printf("[iWorkerCenter] bootstrap failed (running without modules): %v", err)
	} else {
		cleanups = append(cleanups, center.Close)
		centerMux = center.Mux
	}

	// /health — always available; reports bootstrap status
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if centerMux != nil {
			centerMux.ServeHTTP(w, r)
			return
		}
		writeBootstrapError(w, bootstrapErr)
	})

	// Non-admin routes forward directly (only when bootstrap succeeded)
	if centerMux != nil {
		mux.HandleFunc("/client/", func(w http.ResponseWriter, r *http.Request) {
			center.Mux.ServeHTTP(w, r)
		})
		mux.HandleFunc("/runtime/", func(w http.ResponseWriter, r *http.Request) {
			center.Mux.ServeHTTP(w, r)
		})
		mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
			center.Mux.ServeHTTP(w, r)
		})
	} else {
		// When bootstrap failed, return JSON errors for API routes
		unavailable := func(w http.ResponseWriter, _ *http.Request) {
			writeBootstrapError(w, bootstrapErr)
		}
		mux.HandleFunc("/client/", unavailable)
		mux.HandleFunc("/runtime/", unavailable)
		mux.HandleFunc("/auth/", unavailable)
	}

	// --- 2. LLM proxy server ---
	llmServer := newCenterServer(":9377")
	if center != nil {
		llmServer.center = center
	}
	mux.HandleFunc("/v1/models", llmServer.handleModels)
	mux.HandleFunc("/v1/chat/completions", llmServer.handleChatCompletions)

	// --- 3. IM gateways ---
	setupIMGateways(mux)

	// --- 4. Dashboard data API ---
	mux.HandleFunc("/api/dashboard", handleDashboardAPI)
	mux.HandleFunc("/api/center/status", func(w http.ResponseWriter, r *http.Request) {
		llmServer.handleHealth(w, r)
	})
	mux.HandleFunc("/api/center/settings", func(w http.ResponseWriter, r *http.Request) {
		handleCenterSettingsAPI(w, r)
	})

	// --- 5. /admin/ — API routes + SPA fallback ---
	// The center.Mux has routes like /admin/roles, /admin/colleagues, /admin/im-config.
	// We need to serve those as API, and everything else as SPA (index.html).
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is an API path (has a known admin API prefix)
		if isAdminAPIPath(r.URL.Path) {
			if centerMux != nil {
				centerMux.ServeHTTP(w, r)
			} else {
				// Bootstrap failed — return JSON error instead of SPA HTML
				writeBootstrapError(w, bootstrapErr)
			}
			return
		}
		// Otherwise serve SPA
		if spaFallback != nil {
			spaFallback.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	cleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	return mux, cleanup, nil
}

// isAdminAPIPath returns true if the path looks like a backend API route
// rather than a SPA navigation route.
func isAdminAPIPath(path string) bool {
	// Known admin API path prefixes registered by bootstrap modules
	apiPrefixes := []string{
		"/admin/roles",
		"/admin/colleagues",
		"/admin/memories",
		"/admin/capabilities",
		"/admin/capabilities-import",
		"/admin/collaborations",
		"/admin/workflows",
		"/admin/workflow-instances",
		"/admin/workflow-design",
		"/admin/audit",
		"/admin/config-bundles",
		"/admin/security",
		"/admin/model-endpoints",
		"/admin/model-routing-policies",
		"/admin/im-config",
		"/admin/diworker-auth",
		"/admin/compute",
		"/admin/recommend",
		"/admin/bootstrap",
		"/admin/profile",
		"/admin/password",
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// writeBootstrapError writes a JSON 503 response indicating bootstrap failure.
func writeBootstrapError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	msg := "bootstrap failed"
	if err != nil {
		msg = err.Error()
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"code":    "BOOTSTRAP_FAILED",
		"message": msg,
	})
}
