package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

type adminHAStatusProvider interface {
	GetAdminStatus(ctx context.Context) (*ha.AdminStatusView, error)
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
