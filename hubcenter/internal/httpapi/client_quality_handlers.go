package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

type clientQualityProvider interface {
	GetClientQuality(ctx context.Context) (*ha.ClientQualityView, error)
	ListClientEndpoints(ctx context.Context) (*ha.EndpointsView, error)
}

func ClientQualityHandler(svc clientQualityProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             true,
				"node_id":        "",
				"node_name":      "",
				"service_status": "healthy",
				"quality_score":  100,
				"routable":       true,
				"cluster": map[string]any{
					"reachable_nodes": 1,
					"total_nodes":     1,
					"status":          "healthy",
				},
				"sync": map[string]any{
					"enabled":         false,
					"lag_seconds":     0,
					"backlog":         0,
					"last_success_at": nil,
				},
				"features": map[string]any{
					"can_register":  true,
					"can_heartbeat": true,
					"can_resolve":   true,
				},
				"server_time": time.Now().UTC(),
				"ttl_seconds": 15,
			})
			return
		}

		view, err := svc.GetClientQuality(r.Context())
		if err != nil {
			writeClientAwareError(w, http.StatusInternalServerError, "CLIENT_QUALITY_FAILED", err.Error(), true, true)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func ClientEndpointsHandler(svc clientQualityProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":          true,
				"nodes":       []any{},
				"ttl_seconds": 60,
				"server_time": time.Now().UTC(),
			})
			return
		}

		view, err := svc.ListClientEndpoints(r.Context())
		if err != nil {
			writeClientAwareError(w, http.StatusInternalServerError, "CLIENT_ENDPOINTS_FAILED", err.Error(), true, true)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
