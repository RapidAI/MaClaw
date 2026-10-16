package llmservice

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// InvalidBindingReleaseCode is the machine-readable error code for every
// binding-release 400/500 response (malformed body, missing node_id, or a
// release the server could not complete).
const InvalidBindingReleaseCode = "INVALID_BINDING_RELEASE"

// ProxyBindingReleaseHandler lets an authenticated Hub force-release its own
// tenant leases on a dead HubCenter node so they can rebind immediately.
// POST /api/llm/v1/binding/release
//
// Headers:
//   - Authorization: Bearer <hub_machine_token>
//   - X-Hub-ID: hub instance ID
//
// Body: {"node_id": "<dead hubcenter node id>"}
func ProxyBindingReleaseHandler(cfg *ProxyConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			NodeID string `json:"node_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeCodedJSONError(w, http.StatusBadRequest, InvalidBindingReleaseCode, "invalid JSON body")
			return
		}
		nodeID := strings.TrimSpace(body.NodeID)
		if nodeID == "" {
			writeCodedJSONError(w, http.StatusBadRequest, InvalidBindingReleaseCode, "node_id is required")
			return
		}
		hubID := strings.TrimSpace(r.Header.Get("X-Hub-ID"))
		released := 0
		if cfg != nil && cfg.ReleaseBinding != nil {
			n, err := cfg.ReleaseBinding(r.Context(), hubID, nodeID)
			if err != nil {
				log.Printf("[llm-proxy] release bindings failed hub=%s node=%s: %v", hubID, nodeID, err)
				writeJSONError(w, http.StatusInternalServerError, "failed to release bindings")
				return
			}
			released = n
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"released": released})
	}
}

// writeCodedJSONError mirrors writeJSONError but accepts a string error code
// instead of an HTTP status, so clients can match on a stable machine code.
func writeCodedJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": code,
		"error": map[string]any{
			"message": message,
			"code":    code,
		},
	})
}
