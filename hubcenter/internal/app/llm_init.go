package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// LLMModule holds all LLM service components for the HubCenter app.
type LLMModule struct {
	Service        *llmservice.Service
	AuthChecker    *llmservice.AuthorizationChecker
	ProxyCfg       *llmservice.ProxyConfig
	CardStoreSvc   *cardstore.Service
	BindingManager *ha.LLMBindingManager
	UsageRecorder  *llmservice.UsageRecorderImpl
}

// InitLLMModule initializes the LLM service module and registers routes.
// Call this after the SQLite provider and system settings are available.
func InitLLMModule(provider *sqlite.Provider, system store.SystemSettingsRepository, nodeID string, entrySvc *entry.Service) *LLMModule {
	// 1. Ensure database tables
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		log.Printf("[llm-init] failed to create LLM tables: %v", err)
		return nil
	}

	// 2. Create repositories
	authRepo := sqlite.NewLLMAuthRepo(provider)
	usageRepo := sqlite.NewLLMUsageRepo(provider)
	cardTypeRepo := sqlite.NewLLMCardTypeRepo(provider)
	bindingRepo := sqlite.NewLLMBindingRepo(provider)

	// 3. Create services
	llmSvc := llmservice.NewService(system)
	authChecker := llmservice.NewAuthorizationChecker(authRepo)
	usageRecorder := llmservice.NewUsageRecorder(usageRepo)
	bindingMgr := ha.NewLLMBindingManager(nodeID, bindingRepo)

	// 4. Create proxy config
	proxyCfg := &llmservice.ProxyConfig{
		Service:     llmSvc,
		AuthChecker: authChecker,
		Cache:       llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 256, MemoryMaxBytes: 16 << 20}),
		Concurrency: llmpool.NewConcurrencyController(),
		Resilience:  llmpool.NewResilienceController(),
		Usage:       usageRecorder,
		HTTPClient: &http.Client{
			Timeout: 180 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		NodeID: nodeID,
		CheckBinding: func(ctx context.Context, hubID, tenantID string) (bool, string) {
			ok, existing, err := bindingMgr.TryBind(ctx, hubID, tenantID)
			if err != nil {
				// On error, allow (fail-open for availability)
				return true, ""
			}
			if !ok && existing != nil {
				return false, existing.NodeID
			}
			return true, ""
		},
	}

	// 5. Create card store service
	orderRepo := sqlite.NewLLMOrderRepo(provider)
	cardStoreSvc := cardstore.NewService(cardTypeRepo, orderRepo, authRepo)
	cardStoreSvc.SetServiceGroupResolver(func(ctx context.Context, serviceGroupID string) (string, string, string) {
		reg, err := llmSvc.LoadRegistry(ctx)
		if err != nil {
			return serviceGroupID, "", ""
		}
		for _, group := range reg.ServiceGroups {
			if group.ID == serviceGroupID {
				return group.Name, group.AgentID, group.AgentName
			}
		}
		return serviceGroupID, "", ""
	})
	if entrySvc != nil {
		cardStoreSvc.SetTenantVerifier(func(ctx context.Context, hubID, tenantID, email string) error {
			resolved, err := entrySvc.ResolveByEmail(ctx, email)
			if err != nil {
				return err
			}
			for _, hub := range resolved.Hubs {
				if hub.HubID == hubID && normalizeCardStoreTenantID(hub.TenantID) == normalizeCardStoreTenantID(tenantID) {
					return nil
				}
			}
			return fmt.Errorf("email %s is not routed to hub=%s tenant=%s", email, hubID, tenantID)
		})
	}

	// Load payment config from system settings (admin configures via API)
	if raw, err := system.Get(context.Background(), "llm_cardstore_payment_config"); err == nil && raw != "" {
		var paymentCfg struct {
			PaymentMode string                              `json:"payment_mode"`
			Personal    corecardstore.PersonalPaymentConfig `json:"personal_payment"`
			Alipay      corecardstore.AlipayDirectConfig    `json:"alipay_direct"`
		}
		if json.Unmarshal([]byte(raw), &paymentCfg) == nil {
			personal, alipay := effectiveCardStorePaymentConfig(paymentCfg.PaymentMode, paymentCfg.Personal, paymentCfg.Alipay)
			cardStoreSvc.SetPaymentConfig(personal, alipay)
		}
	}

	// 6. Register LLM route hook — will be called during NewRouter
	statsSvc := llmservice.NewStatsService(usageRepo)
	httpapi.SetLLMRouteHook(func(mux *http.ServeMux, adminService *auth.AdminService) {
		httpapi.RegisterLLMRoutes(mux, adminService, llmSvc, proxyCfg, authChecker, cardStoreSvc, statsSvc)
	})

	log.Printf("[llm-init] LLM service module initialized (node=%s)", nodeID)

	return &LLMModule{
		Service:        llmSvc,
		AuthChecker:    authChecker,
		ProxyCfg:       proxyCfg,
		CardStoreSvc:   cardStoreSvc,
		BindingManager: bindingMgr,
		UsageRecorder:  usageRecorder,
	}
}

func effectiveCardStorePaymentConfig(mode string, personal corecardstore.PersonalPaymentConfig, alipay corecardstore.AlipayDirectConfig) (corecardstore.PersonalPaymentConfig, corecardstore.AlipayDirectConfig) {
	switch mode {
	case corecardstore.PaymentModeSemiManual:
		return personal, corecardstore.AlipayDirectConfig{}
	case corecardstore.PaymentModeAlipay:
		return corecardstore.PersonalPaymentConfig{}, alipay
	default:
		return personal, alipay
	}
}

func normalizeCardStoreTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == "tenant_default" {
		return "default"
	}
	return tenantID
}
