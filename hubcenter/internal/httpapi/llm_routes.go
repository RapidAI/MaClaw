package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// RegisterLLMRoutes registers the LLM service and card store API routes.
// Call this from NewRouter after the main routes are registered.
func RegisterLLMRoutes(
	mux *http.ServeMux,
	adminService *auth.AdminService,
	llmSvc *llmservice.Service,
	proxyCfg *llmservice.ProxyConfig,
	authChecker *llmservice.AuthorizationChecker,
	cardStoreSvc *cardstore.Service,
	statsSvc *llmservice.StatsService,
) {
	if llmSvc == nil {
		return
	}

	// --- LLM Proxy (called by Hubs) ---
	if proxyCfg != nil {
		mux.HandleFunc("POST /api/llm/v1/chat/completions", llmservice.ProxyHandler(proxyCfg))
	}

	// --- Authorization query (called by Hubs) ---
	if authChecker != nil {
		mux.HandleFunc("GET /api/llm/v1/authorization", llmservice.AuthorizationQueryHandler(authChecker))
	}

	// --- Admin: LLM Providers ---
	mux.HandleFunc("GET /api/admin/llm/providers", RequireAdmin(adminService, adminListLLMProviders(llmSvc)))
	mux.HandleFunc("POST /api/admin/llm/providers", RequireAdmin(adminService, adminAddLLMProvider(llmSvc)))
	mux.HandleFunc("POST /api/admin/llm/providers/probe-models", RequireAdmin(adminService, adminProbeLLMProviderModels(llmSvc)))
	mux.HandleFunc("PUT /api/admin/llm/providers/{id}", RequireAdmin(adminService, adminUpdateLLMProvider(llmSvc)))
	mux.HandleFunc("DELETE /api/admin/llm/providers/{id}", RequireAdmin(adminService, adminDeleteLLMProvider(llmSvc)))

	// --- Admin: LLM Service Groups ---
	mux.HandleFunc("GET /api/admin/llm/service-groups", RequireAdmin(adminService, adminListLLMServiceGroups(llmSvc)))
	mux.HandleFunc("POST /api/admin/llm/service-groups", RequireAdmin(adminService, adminAddLLMServiceGroup(llmSvc)))
	mux.HandleFunc("PUT /api/admin/llm/service-groups/{id}", RequireAdmin(adminService, adminUpdateLLMServiceGroup(llmSvc)))
	mux.HandleFunc("DELETE /api/admin/llm/service-groups/{id}", RequireAdmin(adminService, adminDeleteLLMServiceGroup(llmSvc)))

	// --- Admin: Tenant Authorizations ---
	if authChecker != nil {
		mux.HandleFunc("GET /api/admin/llm/authorizations", RequireAdmin(adminService, adminListLLMAuthorizations(authChecker)))
		mux.HandleFunc("POST /api/admin/llm/authorizations", RequireAdmin(adminService, adminCreateLLMAuthorization(authChecker)))
	}

	// --- Admin: Usage Statistics ---
	if statsSvc != nil {
		mux.HandleFunc("GET /api/admin/llm/usage", RequireAdmin(adminService, adminLLMUsageHandler(statsSvc)))
	}

	// --- Card Store (public, for Hub tenant admins) ---
	if cardStoreSvc != nil {
		mux.HandleFunc("GET /api/cardstore/types", cardstore.ListCardTypesHandler(cardStoreSvc))
		mux.HandleFunc("POST /api/cardstore/purchase", cardstore.CreateOrderHandler(cardStoreSvc))
		mux.HandleFunc("GET /api/cardstore/orders", cardstore.ListOrdersHandler(cardStoreSvc))
		mux.HandleFunc("GET /api/cardstore/templates", cardstore.TemplatesHandler())
		mux.HandleFunc("POST /api/cardstore/payment/notify", cardstore.AlipayNotifyHandler(cardStoreSvc))

		// Admin: Card Type management
		mux.HandleFunc("GET /api/admin/cardstore/types", RequireAdmin(adminService, cardstore.AdminListCardTypesHandler(cardStoreSvc)))
		mux.HandleFunc("POST /api/admin/cardstore/types", RequireAdmin(adminService, cardstore.AdminCreateCardTypeHandler(cardStoreSvc)))
		mux.HandleFunc("PUT /api/admin/cardstore/types/{id}", RequireAdmin(adminService, cardstore.AdminUpdateCardTypeHandler(cardStoreSvc)))

		// Admin: Order management
		mux.HandleFunc("GET /api/admin/cardstore/orders", RequireAdmin(adminService, cardstore.AdminListOrdersHandler(cardStoreSvc)))
		mux.HandleFunc("POST /api/admin/cardstore/orders/{orderNo}/confirm", RequireAdmin(adminService, cardstore.AdminConfirmOrderHandler(cardStoreSvc, adminEmailFromRequest)))

		// Admin: Payment config
		mux.HandleFunc("GET /api/admin/llm/payment-config", RequireAdmin(adminService, adminGetPaymentConfig(llmSvc)))
		mux.HandleFunc("PUT /api/admin/llm/payment-config", RequireAdmin(adminService, adminSavePaymentConfig(llmSvc, cardStoreSvc)))
	}

	// --- Compute Store static page ---
	mux.HandleFunc("GET /compute-store", serveComputeStore)
	mux.HandleFunc("GET /compute-store/", serveComputeStore)
	mux.HandleFunc("GET /compute-store/professional.css", serveComputeStoreCSS)
	mux.Handle("GET /compute-store/assets/", http.StripPrefix("/compute-store/assets/", http.FileServer(http.Dir("web/compute-store/assets"))))
}

func serveComputeStore(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/compute-store/index.html")
}

func serveComputeStoreCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, "web/compute-store/professional.css")
}

func adminEmailFromRequest(r *http.Request) string {
	// Extract admin email from auth context (set by RequireAdmin middleware)
	if admin := AdminFromContext(r.Context()); admin != nil {
		return admin.Email
	}
	return ""
}
