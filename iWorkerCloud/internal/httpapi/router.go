package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/auth"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/skillmarket"
)

func NewRouter(
	authSvc *auth.Service,
	centerSvc *centers.Service,
	licenseSvc *license.Service,
	dataDir string,
	computeHandler *ComputeHandler,
	skillMarketSvc *skillmarket.Service,
) http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "iWorkerCloud"})
	})

	// Auth
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(authSvc))
	mux.HandleFunc("POST /api/admin/setup", SetupHandler(authSvc))
	mux.HandleFunc("GET /api/admin/captcha", CaptchaHandler(authSvc))
	mux.HandleFunc("POST /api/admin/login", LoginHandler(authSvc))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(ChangePasswordHandler(authSvc)))

	// Centers (public: register + heartbeat)
	mux.HandleFunc("POST /api/centers/register", RegisterCenterHandler(centerSvc))
	mux.HandleFunc("POST /api/centers/{id}/heartbeat", HeartbeatHandler(centerSvc))

	// Centers (admin)
	mux.HandleFunc("GET /api/admin/centers", RequireAdmin(ListCentersHandler(centerSvc)))
	mux.HandleFunc("GET /api/admin/centers/management", RequireAdmin(CenterManagementHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm-trial", RequireAdmin(ConfirmCenterTrialHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", RequireAdmin(ConfirmCenterManualHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/centers/{id}/disable", RequireAdmin(DisableCenterHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/centers/{id}/enable", RequireAdmin(EnableCenterHandler(centerSvc)))
	mux.HandleFunc("PUT /api/admin/centers/{id}/integration", RequireAdmin(UpdateCenterIntegrationHandler(centerSvc)))
	mux.HandleFunc("POST /api/admin/centers/{id}/probe", RequireAdmin(ProbeCenterHandler(centerSvc)))
	mux.HandleFunc("GET /api/admin/centers/{id}/runtime-snapshot", RequireAdmin(RuntimeSnapshotHandler(centerSvc)))
	mux.HandleFunc("GET /api/admin/centers/{id}/provision-readiness", RequireAdmin(ProvisionReadinessHandler(centerSvc)))
	mux.HandleFunc("DELETE /api/admin/centers/{id}", RequireAdmin(DeleteCenterHandler(centerSvc)))

	// Provision tenant on a remote center (admin)
	mux.HandleFunc("POST /api/admin/centers/{id}/provision-tenant", RequireAdmin(ProvisionTenantHandler(centerSvc)))

	// Licenses (admin)
	mux.HandleFunc("GET /api/admin/licenses", RequireAdmin(ListLicensesHandler(licenseSvc)))
	mux.HandleFunc("POST /api/admin/licenses", RequireAdmin(IssueLicenseHandler(licenseSvc)))
	mux.HandleFunc("POST /api/admin/licenses/{id}/revoke", RequireAdmin(RevokeLicenseHandler(licenseSvc)))

	// Licenses (public: center fetches its own active license + public key)
	mux.HandleFunc("GET /api/centers/{id}/license", GetActiveLicenseHandler(licenseSvc, centerSvc))
	mux.HandleFunc("GET /api/public-key", GetPublicKeyHandler(dataDir))

	// Skill market management and distribution.
	if skillMarketSvc != nil {
		skillMarketHandler := NewSkillMarketHandler(centerSvc, licenseSvc, skillMarketSvc)
		mux.HandleFunc("GET /api/admin/skills", RequireAdmin(skillMarketHandler.ListAdminSkills()))
		mux.HandleFunc("POST /api/admin/skills", RequireAdmin(skillMarketHandler.CreateAdminSkill()))
		mux.HandleFunc("PUT /api/admin/skills/{skill_id}", RequireAdmin(skillMarketHandler.UpdateAdminSkill()))
		mux.HandleFunc("DELETE /api/admin/skills/{skill_id}", RequireAdmin(skillMarketHandler.DeleteAdminSkill()))
		mux.HandleFunc("POST /api/centers/{id}/skills", skillMarketHandler.PublishCenterSkill())
		mux.HandleFunc("GET /api/centers/{id}/skills/search", skillMarketHandler.SearchCenterSkills())
		mux.HandleFunc("GET /api/centers/{id}/skills/{skill_id}", skillMarketHandler.GetCenterSkill())
		mux.HandleFunc("GET /api/centers/{id}/skills/{skill_id}/package", skillMarketHandler.DownloadCenterSkillPackage())
	}

	// Compute power management (admin)
	if computeHandler != nil {
		mux.HandleFunc("POST /api/admin/compute/providers", RequireAdmin(computeHandler.CreateProvider()))
		mux.HandleFunc("GET /api/admin/compute/providers", RequireAdmin(computeHandler.ListProviders()))
		mux.HandleFunc("GET /api/admin/compute/providers/{id}", RequireAdmin(computeHandler.GetProvider()))
		mux.HandleFunc("PUT /api/admin/compute/providers/{id}", RequireAdmin(computeHandler.UpdateProvider()))
		mux.HandleFunc("DELETE /api/admin/compute/providers/{id}", RequireAdmin(computeHandler.DeleteProvider()))
		mux.HandleFunc("POST /api/admin/compute/providers/{id}/toggle", RequireAdmin(computeHandler.ToggleProvider()))
		mux.HandleFunc("POST /api/admin/compute/providers/{id}/test", RequireAdmin(computeHandler.TestProvider()))

		// Center compute permission management (admin)
		mux.HandleFunc("GET /api/admin/compute/permissions", RequireAdmin(computeHandler.ListCenterPermissions()))
		mux.HandleFunc("POST /api/admin/compute/permissions/{id}", RequireAdmin(computeHandler.ToggleCenterPermission()))
		mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", RequireAdmin(computeHandler.SetComputePermission()))

		// Center provider assignment management (admin)
		mux.HandleFunc("POST /api/admin/centers/{id}/compute-providers", RequireAdmin(computeHandler.AssignProviderToCenter()))
		mux.HandleFunc("DELETE /api/admin/centers/{id}/compute-providers/{provider_id}", RequireAdmin(computeHandler.UnassignProviderFromCenter()))
		mux.HandleFunc("GET /api/admin/centers/{id}/compute-providers", RequireAdmin(computeHandler.ListCenterAssignments()))

		// Center compute provider distribution (public: uses center secret auth)
		mux.HandleFunc("GET /api/centers/{id}/compute-providers", computeHandler.CenterComputeProviders())
	}

	// Static web assets: React SPA built to web/admin/dist
	registerStaticRoutes(mux, "./web/admin/dist", "/admin")

	// Root redirect
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	return mux
}
