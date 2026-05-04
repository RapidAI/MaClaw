// iWorkerCenter service entry point.
// Runs as a headless HTTP service (no Wails GUI).
// Serves the admin web UI at /admin/ and all API endpoints.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/apiroutes"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
	llmcompute "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/compute"
	centercompute "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/compute"
)

//go:embed web
var webAssets embed.FS

func main() {
	addr := flag.String("addr", ":9377", "listen address")
	flag.Parse()

	log.Printf("[iWorkerCenter] starting service on %s", *addr)

	// Bootstrap the modular backend (DB, migrations, all module routes)
	center, bootstrapErr := app.Bootstrap()
	if bootstrapErr != nil {
		log.Printf("[iWorkerCenter] WARNING: bootstrap failed: %v", bootstrapErr)
		log.Printf("[iWorkerCenter] admin API routes will return 503 until the issue is resolved")
	} else {
		defer center.Close()
	}

	mux := http.NewServeMux()

	// /health - always available
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if center != nil && center.Mux != nil {
			center.Mux.ServeHTTP(w, r)
			return
		}
		writeBootstrapError(w, bootstrapErr)
	})

	// Forward known non-admin API prefixes to center.Mux
	forwardOrUnavailable := func(w http.ResponseWriter, r *http.Request) {
		if center != nil && center.Mux != nil {
			center.Mux.ServeHTTP(w, r)
			return
		}
		writeBootstrapError(w, bootstrapErr)
	}
	mux.HandleFunc("/client/", forwardOrUnavailable)
	mux.HandleFunc("/runtime/", forwardOrUnavailable)
	mux.HandleFunc("/auth/", forwardOrUnavailable)
	mux.HandleFunc("GET /api/center/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if center == nil {
			writeBootstrapError(w, bootstrapErr)
			return
		}
		_ = json.NewEncoder(w).Encode(center.RuntimeStatusSnapshot())
	})
	mux.HandleFunc("/api/", forwardOrUnavailable)
	registerV1Routes(mux, center)
	mux.HandleFunc("/v1/", forwardOrUnavailable)
	mux.HandleFunc("/diworker-auth/", forwardOrUnavailable)

	// Prepare SPA file server for the admin frontend.
	// Embed layout: web/admin/index.html -> after fs.Sub("web/admin") -> index.html
	adminFS, err := fs.Sub(webAssets, "web/admin")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}
	spaFS := http.FS(adminFS)
	spaFileServer := http.StripPrefix("/admin/", http.FileServer(spaFS))

	// /admin - redirect to /admin/
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})

	// /admin/ - API routes + SPA fallback
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		// If the path matches a known admin API prefix, forward to center.Mux
		if isAdminAPIPath(r.URL.Path) {
			if center != nil && center.Mux != nil {
				forwarded := r
				if center.Auth != nil {
					if withTenant, ok := center.Auth.AuthenticateWithContext(r); ok {
						forwarded = withTenant
					}
				}
				center.Mux.ServeHTTP(w, forwarded)
			} else {
				writeBootstrapError(w, bootstrapErr)
			}
			return
		}
		// Otherwise serve static files or fall back to index.html for SPA routing
		relPath := strings.TrimPrefix(r.URL.Path, "/admin/")
		if relPath == "" {
			relPath = "index.html"
		}
		// Try to open the file; if it exists, serve it directly
		if f, err := spaFS.Open(relPath); err == nil {
			_ = f.Close()
			spaFileServer.ServeHTTP(w, r)
			return
		}
		// File does not exist; serve index.html for SPA client-side routing
		r.URL.Path = "/admin/index.html"
		spaFileServer.ServeHTTP(w, r)
	})

	// Redirect root to admin
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	server := &http.Server{Addr: *addr, Handler: mux}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[iWorkerCenter] shutting down...")
		cancel()
		server.Close()
	}()

	fmt.Printf("iWorkerCenter listening on %s\n", *addr)
	fmt.Printf("Admin UI: http://localhost%s/admin/\n", *addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}

	_ = ctx
}

type centerComputeProviderSource struct {
	center *app.Center
}

type v1FallbackProvider struct {
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Protocol string `json:"protocol"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
}

func loadV1FallbackProviders() []v1FallbackProvider {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".iworkercenter", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Providers []v1FallbackProvider `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg.Providers
}

func (s centerComputeProviderSource) ActiveProviders() []llmcompute.ProviderConfig {
	if s.center == nil || s.center.ComputeSourceManager == nil {
		return centerProvidersToLLMConfigs(loadV1FallbackProviders())
	}
	providers := computeProvidersToLLMConfigs(s.center.ComputeSourceManager.GetActiveProviders())
	if len(providers) > 0 {
		return providers
	}
	return centerProvidersToLLMConfigs(loadV1FallbackProviders())
}

func computeProvidersToLLMConfigs(providers []centercompute.ComputeProvider) []llmcompute.ProviderConfig {
	out := make([]llmcompute.ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		if !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
			continue
		}
		out = append(out, llmcompute.ProviderConfig{
			Name:                 provider.Name,
			BaseURL:              provider.BaseURL,
			APIKey:               provider.APIKey,
			Protocol:             provider.Protocol,
			UserAgent:            provider.UserAgent,
			Model:                provider.Model,
			Enabled:              provider.Enabled,
			Priority:             provider.Priority,
			InputPricePerMToken:  provider.InputPricePerMToken,
			OutputPricePerMToken: provider.OutputPricePerMToken,
		})
	}
	return out
}

func centerProvidersToLLMConfigs(providers []v1FallbackProvider) []llmcompute.ProviderConfig {
	out := make([]llmcompute.ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		if !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
			continue
		}
		out = append(out, llmcompute.ProviderConfig{
			Name:     provider.Name,
			BaseURL:  provider.BaseURL,
			APIKey:   provider.APIKey,
			Protocol: provider.Protocol,
			Model:    provider.Model,
			Enabled:  provider.Enabled,
			Priority: provider.Priority,
		})
	}
	return out
}

func registerV1Routes(mux *http.ServeMux, center *app.Center) {
	source := centerComputeProviderSource{center: center}
	proxy := llmcompute.NewLLMProxy(source, nil)
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		providers := source.ActiveProviders()
		data := make([]map[string]any, 0, len(providers))
		for _, provider := range providers {
			model := strings.TrimSpace(provider.Model)
			if model == "" {
				continue
			}
			data = append(data, map[string]any{
				"id":       model,
				"object":   "model",
				"owned_by": provider.Protocol,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("POST /v1/chat/completions", proxy.HandleChatCompletions())
}

// isAdminAPIPath returns true if the path looks like a backend API route
// rather than a SPA navigation route.
func isAdminAPIPath(path string) bool {
	return apiroutes.IsAdminAPIPath(path)
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
