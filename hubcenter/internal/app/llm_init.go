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
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
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
func InitLLMModule(provider *sqlite.Provider, system store.SystemSettingsRepository, nodeID string, entrySvc *entry.Service, haSvc *ha.Service) *LLMModule {
	// 1. Ensure database tables
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		log.Printf("[llm-init] failed to create LLM tables: %v", err)
		return nil
	}

	// 2. Create repositories
	baseAuthRepo := sqlite.NewLLMAuthRepo(provider)
	authRepo := llmservice.TenantAuthorizationRepository(baseAuthRepo)
	usageRepo := sqlite.NewLLMUsageRepo(provider)
	baseCardTypeRepo := sqlite.NewLLMCardTypeRepo(provider)
	cardTypeRepo := cardstore.CardTypeRepository(baseCardTypeRepo)
	baseOrderRepo := sqlite.NewLLMOrderRepo(provider)
	orderRepo := cardstore.PurchaseOrderRepository(baseOrderRepo)
	bindingRepo := sqlite.NewLLMBindingRepo(provider)
	if haSvc != nil {
		haSvc.AttachLLMAuthorizations(baseAuthRepo)
		haSvc.AttachLLMBindings(bindingRepo)
		authRepo = &haLLMAuthorizationRepo{inner: baseAuthRepo, sync: haSvc}
		haSvc.AttachCardTypes(baseCardTypeRepo)
		cardTypeRepo = &haCardTypeRepo{inner: baseCardTypeRepo, sync: haSvc}
		haSvc.AttachCardOrders(baseOrderRepo)
		orderRepo = &haCardOrderRepo{inner: baseOrderRepo, sync: haSvc}
	}
	// 3. Create services
	llmSvc := llmservice.NewService(system)
	authChecker := llmservice.NewAuthorizationChecker(authRepo)
	usageRecorder := llmservice.NewUsageRecorder(usageRepo)
	bindingMgr := ha.NewLLMBindingManager(nodeID, bindingRepo)
	if haSvc != nil {
		bindingMgr.SetSyncBinding(haSvc.AppendLLMNodeBinding)
		seedLLMAuthorizationHAOps(context.Background(), haSvc, baseAuthRepo)
		seedLLMCardOrderHAOps(context.Background(), haSvc, baseOrderRepo)
	}

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
			if ok, err := entrySvc.EmailHasHubTenantLink(ctx, email, hubID, tenantID); err != nil {
				return err
			} else if ok {
				return nil
			}
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
	if err := ensureCardBackedServiceGroupsRequireGrant(context.Background(), cardStoreSvc, llmSvc); err != nil {
		log.Printf("[llm-init] failed to repair card-backed service group access policy: %v", err)
	}
	if err := ensureDefaultComputeCardTypes(context.Background(), cardStoreSvc, llmSvc); err != nil {
		log.Printf("[llm-init] failed to seed default compute card types: %v", err)
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
	httpapi.SetLLMAuthorizationSyncChecker(authChecker)
	httpapi.SetLLMRouteHook(func(mux *http.ServeMux, adminService *auth.AdminService, hubService *hubs.Service) {
		if hubService != nil {
			cardStoreSvc.SetPublicBaseURLProvider(hubService.PublicBaseURL)
		}
		httpapi.RegisterLLMRoutes(mux, adminService, hubService, llmSvc, proxyCfg, authChecker, cardStoreSvc, statsSvc)
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

func seedLLMCardOrderHAOps(ctx context.Context, haSvc *ha.Service, repo cardstore.PurchaseOrderRepository) {
	if haSvc == nil || repo == nil {
		return
	}
	orders, _, err := repo.List(ctx, cardstore.OrderFilter{IncludeArchived: true, Limit: 10000})
	if err != nil {
		log.Printf("[llm-init] seed llm card order HA ops failed: %v", err)
		return
	}
	seeded := 0
	for _, order := range orders {
		if order == nil {
			continue
		}
		exists, err := haSvc.HasEntityVersion(ctx, ha.EntityLLMCardOrder, order.OrderNo)
		if err != nil {
			log.Printf("[llm-init] inspect llm card order HA entity version failed: order=%s err=%v", order.OrderNo, err)
			continue
		}
		if exists {
			continue
		}
		haSvc.AppendLLMCardOrder(ctx, order)
		seeded++
	}
	if seeded > 0 {
		log.Printf("[llm-init] seeded llm card order HA ops: count=%d", seeded)
	}
}

func seedLLMAuthorizationHAOps(ctx context.Context, haSvc *ha.Service, repo llmservice.TenantAuthorizationRepository) {
	if haSvc == nil || repo == nil {
		return
	}
	seeded, err := haSvc.HasEntityTypeOps(ctx, ha.EntityLLMTenantAuth)
	if err != nil {
		log.Printf("[llm-init] inspect llm authorization HA seed state failed: %v", err)
		return
	}
	if seeded {
		return
	}
	auths, err := repo.ListAll(ctx)
	if err != nil {
		log.Printf("[llm-init] seed llm authorization HA ops failed: %v", err)
		return
	}
	for _, auth := range auths {
		haSvc.AppendLLMAuthorization(ctx, auth)
	}
	if len(auths) > 0 {
		log.Printf("[llm-init] seeded llm authorization HA ops: count=%d", len(auths))
	}
}

func effectiveCardStorePaymentConfig(mode string, personal corecardstore.PersonalPaymentConfig, alipay corecardstore.AlipayDirectConfig) (corecardstore.PersonalPaymentConfig, corecardstore.AlipayDirectConfig) {
	switch mode {
	case corecardstore.PaymentModeSemiManual:
		if cardstoreHasEnabledPaymentChannel(personal) {
			return personal, corecardstore.AlipayDirectConfig{}
		}
		return corecardstore.PersonalPaymentConfig{}, alipay
	case corecardstore.PaymentModeAlipay:
		if strings.TrimSpace(alipay.AppID) != "" {
			return corecardstore.PersonalPaymentConfig{}, alipay
		}
		return personal, corecardstore.AlipayDirectConfig{}
	default:
		return personal, alipay
	}
}

func cardstoreHasEnabledPaymentChannel(cfg corecardstore.PersonalPaymentConfig) bool {
	for _, ch := range cfg.Channels {
		if ch.Enabled {
			return true
		}
	}
	return false
}

func ensureCardBackedServiceGroupsRequireGrant(ctx context.Context, cardStoreSvc *cardstore.Service, llmSvc *llmservice.Service) error {
	if cardStoreSvc == nil || llmSvc == nil {
		return nil
	}
	cardBackedGroups := map[string]struct{}{}
	cardTypes, err := cardStoreSvc.ListAllCardTypes(ctx)
	if err != nil {
		return err
	}
	for _, ct := range cardTypes {
		if ct == nil {
			continue
		}
		if groupID := strings.TrimSpace(ct.ServiceGroupID); groupID != "" {
			cardBackedGroups[groupID] = struct{}{}
		}
	}
	orders, _, err := cardStoreSvc.ListOrders(ctx, cardstore.OrderFilter{IncludeArchived: true, Limit: 10000})
	if err != nil {
		return err
	}
	for _, order := range orders {
		if order == nil {
			continue
		}
		if groupID := strings.TrimSpace(order.ServiceGroupID); groupID != "" {
			cardBackedGroups[groupID] = struct{}{}
		}
	}
	if len(cardBackedGroups) == 0 {
		return nil
	}
	reg, err := llmSvc.LoadRegistry(ctx)
	if err != nil || reg == nil {
		return err
	}
	changed := false
	var repaired []string
	for i := range reg.ServiceGroups {
		groupID := strings.TrimSpace(reg.ServiceGroups[i].ID)
		if groupID == "" {
			continue
		}
		if _, ok := cardBackedGroups[groupID]; !ok {
			continue
		}
		if reg.ServiceGroups[i].AccessPolicy == llmservice.AccessPolicyGrantRequired {
			continue
		}
		reg.ServiceGroups[i].AccessPolicy = llmservice.AccessPolicyGrantRequired
		changed = true
		repaired = append(repaired, groupID)
	}
	if !changed {
		return nil
	}
	if err := llmSvc.SaveRegistry(ctx, reg); err != nil {
		return err
	}
	log.Printf("[llm-init] repaired card-backed LLM service groups to grant_required: %s", strings.Join(repaired, ","))
	return nil
}

func ensureDefaultComputeCardTypes(ctx context.Context, cardStoreSvc *cardstore.Service, llmSvc *llmservice.Service) error {
	if cardStoreSvc == nil || llmSvc == nil {
		return nil
	}
	serviceGroupID, ok := defaultComputeCardServiceGroupID(ctx, llmSvc)
	if !ok {
		log.Printf("[llm-init] skip default compute card types: no grant-required LLM service group configured")
		return nil
	}
	if err := cardStoreSvc.EnsureDefaultComputeCardTypes(ctx, serviceGroupID); err != nil {
		return err
	}
	return nil
}

func defaultComputeCardServiceGroupID(ctx context.Context, llmSvc *llmservice.Service) (string, bool) {
	reg, err := llmSvc.LoadRegistry(ctx)
	if err != nil || reg == nil {
		return "", false
	}
	for _, group := range reg.ServiceGroups {
		if group.ID != "" && group.AccessPolicy == llmservice.AccessPolicyGrantRequired {
			return group.ID, true
		}
	}
	return "", false
}

func normalizeCardStoreTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == "tenant_default" {
		return "default"
	}
	return tenantID
}
