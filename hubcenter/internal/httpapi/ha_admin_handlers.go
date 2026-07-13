package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

type adminHAStatusProvider interface {
	GetAdminStatus(ctx context.Context) (*ha.AdminStatusView, error)
}

type adminHASkillBroadcastProvider interface {
	ForceBroadcastSkillHubSnapshot(ctx context.Context) (skillCount int, err error)
	ForceBroadcastSkillMarketSnapshot(ctx context.Context) error
}

func AdminHAStatusHandler(svc adminHAStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, &ha.AdminStatusView{
				Enabled:       false,
				ServiceStatus: "standalone",
				QualityScore:  100,
				Routable:      true,
				Cluster: ha.AdminClusterView{
					ReachableNodes: 1,
					TotalNodes:     1,
					QuorumSize:     1,
					Status:         "standalone",
				},
				Sync: ha.AdminSyncView{
					Enabled: false,
				},
				Peers:       []ha.AdminPeerView{},
				GeneratedAt: time.Now().UTC(),
			})
			return
		}

		view, err := svc.GetAdminStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_STATUS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// AdminHABroadcastSkillHubHandler force-reappends the local SkillHub snapshot to the
// HA op log so peers can catch up after prune/offline gaps (admin-only recovery).
func AdminHABroadcastSkillHubHandler(svc adminHASkillBroadcastProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotImplemented, "HA_NOT_ENABLED", "HA sync is not enabled")
			return
		}
		count, err := svc.ForceBroadcastSkillHubSnapshot(r.Context())
		if err != nil {
			status, code := haBroadcastErrorStatus(err, "HA_SKILLHUB_BROADCAST_FAILED")
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"entity":      "skillhub_snapshot",
			"skill_count": count,
		})
	}
}

// AdminHABroadcastSkillMarketHandler force-reappends the local Skill Market snapshot.
func AdminHABroadcastSkillMarketHandler(svc adminHASkillBroadcastProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotImplemented, "HA_NOT_ENABLED", "HA sync is not enabled")
			return
		}
		if err := svc.ForceBroadcastSkillMarketSnapshot(r.Context()); err != nil {
			status, code := haBroadcastErrorStatus(err, "HA_SKILLMARKET_BROADCAST_FAILED")
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"entity": "skillmarket_snapshot",
		})
	}
}

func haBroadcastErrorStatus(err error, defaultCode string) (int, string) {
	if err == nil {
		return http.StatusOK, defaultCode
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "refusing empty"):
		return http.StatusBadRequest, "HA_BROADCAST_EMPTY"
	case strings.Contains(msg, "not attached"), strings.Contains(msg, "not configured"):
		return http.StatusServiceUnavailable, "HA_BROADCAST_UNAVAILABLE"
	default:
		return http.StatusInternalServerError, defaultCode
	}
}
