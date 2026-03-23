package httpapi

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
)

// TLSConfigResponse is the JSON shape returned by GET /api/admin/tls_config.
type TLSConfigResponse struct {
	Enabled      bool   `json:"enabled"`
	AutoGenerate bool   `json:"auto_generate"`
	CertFile     string `json:"cert_file"`
	KeyFile      string `json:"key_file"`
	CertValid    bool   `json:"cert_valid"`
	CertExpiry   string `json:"cert_expiry,omitempty"` // RFC3339
	CertSANs     string `json:"cert_sans,omitempty"`
}

// GetTLSConfigHandler returns the current TLS configuration and cert status.
func GetTLSConfigHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := TLSConfigResponse{
			Enabled:      cfg.TLS.Enabled,
			AutoGenerate: cfg.TLS.AutoGenerate,
			CertFile:     cfg.TLS.CertFile,
			KeyFile:      cfg.TLS.KeyFile,
		}
		// Check certificate validity
		if data, err := os.ReadFile(cfg.TLS.CertFile); err == nil {
			if block, _ := pem.Decode(data); block != nil {
				if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
					resp.CertValid = time.Now().Before(cert.NotAfter)
					resp.CertExpiry = cert.NotAfter.Format(time.RFC3339)
					var sans []string
					for _, dns := range cert.DNSNames {
						sans = append(sans, dns)
					}
					for _, ip := range cert.IPAddresses {
						sans = append(sans, ip.String())
					}
					resp.CertSANs = strings.Join(sans, ", ")
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// UpdateTLSConfigHandler toggles TLS enabled/disabled, saves to YAML config,
// and triggers a process re-exec so the change takes effect.
// ensureCert is called when enabling TLS to pre-generate the certificate
// before restarting (pass nil to skip).
func UpdateTLSConfigHandler(cfg *config.Config, configPath string, ensureCert func(certFile, keyFile string) error, centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if configPath == "" {
			writeError(w, http.StatusInternalServerError, "NO_CONFIG_PATH", "Config file path not available; cannot persist TLS changes")
			return
		}

		// When enabling TLS with auto_generate, ensure cert exists before
		// restarting — otherwise the new process will fail to bind.
		if req.Enabled && cfg.TLS.AutoGenerate && ensureCert != nil {
			if err := ensureCert(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil {
				writeError(w, http.StatusInternalServerError, "TLS_CERT_GENERATE_FAILED", err.Error())
				return
			}
		}

		// Save to YAML
		log.Printf("[tls] saving tls.enabled=%v to %s", req.Enabled, configPath)
		if err := config.SaveTLSEnabled(configPath, req.Enabled); err != nil {
			log.Printf("[tls] SaveTLSEnabled failed: %v", err)
			writeError(w, http.StatusInternalServerError, "TLS_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		log.Printf("[tls] config saved successfully")

		// Update in-memory config
		cfg.TLS.Enabled = req.Enabled

		// Auto-update public_base_url scheme: http↔https
		if centerSvc != nil {
			ctx := context.Background()
			if currentURL := centerSvc.GetPublicBaseURL(ctx); currentURL != "" {
				if parsed, err := url.Parse(currentURL); err == nil {
					newScheme := ""
					if req.Enabled && parsed.Scheme == "http" {
						newScheme = "https"
					} else if !req.Enabled && parsed.Scheme == "https" {
						newScheme = "http"
					}
					if newScheme != "" {
						parsed.Scheme = newScheme
						if _, err := centerSvc.SetPublicBaseURL(ctx, parsed.String()); err != nil {
							log.Printf("[tls] failed to update public_base_url to %s: %v", newScheme, err)
						} else {
							log.Printf("[tls] public_base_url updated to %s", parsed.String())
						}
					}
				}
			}
		}

		// Respond before restarting
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"enabled":    req.Enabled,
			"restarting": true,
		})
		// Ensure the response is flushed to the client before we restart.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Schedule process re-exec after a short delay so the HTTP response
		// has time to flush to the client.
		go func() {
			time.Sleep(1 * time.Second)
			log.Printf("[tls] TLS toggled to %v, re-execing process...", req.Enabled)
			execSelf()
		}()
	}
}

// execSelf replaces the current process with a fresh copy of itself.
// On Unix this uses syscall.Exec; on Windows it falls back to os.Exit
// (the user must restart manually or use a wrapper script).
func execSelf() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[tls] cannot find executable path: %v, falling back to os.Exit", err)
		os.Exit(0)
		return
	}
	if runtime.GOOS == "windows" {
		log.Printf("[tls] Windows detected — process will exit; please restart manually")
		os.Exit(0)
		return
	}
	// Unix: replace process in-place
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		log.Printf("[tls] syscall.Exec failed: %v, falling back to os.Exit", err)
		os.Exit(0)
	}
}
