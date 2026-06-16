// Package cardstore implements the HubCenter credits card store —
// dynamic card type management, purchase orders, and activation.
package cardstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// ---------------------------------------------------------------------------
// Card Templates (predefined card face designs)
// ---------------------------------------------------------------------------

// CardTemplate represents a predefined card face design.
type CardTemplate struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tones []string `json:"tones"` // 3-color gradient tones for SVG card art
	Image string   `json:"image,omitempty"`
	Color string   `json:"color,omitempty"` // deprecated CSS fallback
}

// BuiltinCardTemplates are the predefined card face designs.
// Frontend renders these as SVG artwork using the tones array.
var BuiltinCardTemplates = []CardTemplate{
	{ID: "enterprise_monthly_blue", Name: "企业月度蓝", Image: "/compute-store/assets/cards/enterprise-monthly-blue.svg", Tones: []string{"#081424", "#123d73", "#0b7768"}},
	{ID: "enterprise_quarter_emerald", Name: "企业季度绿", Image: "/compute-store/assets/cards/enterprise-quarter-emerald.svg", Tones: []string{"#061a18", "#075f57", "#1f6feb"}},
	{ID: "enterprise_annual_slate", Name: "企业年度银灰", Image: "/compute-store/assets/cards/enterprise-annual-slate.svg", Tones: []string{"#050816", "#172033", "#334155"}},
	{ID: "enterprise_pool_indigo", Name: "企业算力池", Image: "/compute-store/assets/cards/enterprise-pool-indigo.svg", Tones: []string{"#090b2b", "#233d8f", "#0f766e"}},
	{ID: "enterprise_ha_teal", Name: "高可用青蓝", Image: "/compute-store/assets/cards/enterprise-ha-teal.svg", Tones: []string{"#06121f", "#0b4c6d", "#115e59"}},
	{ID: "circuit_navy", Name: "深蓝电路", Tones: []string{"#102a43", "#1f5f99", "#0b7768"}},
	{ID: "emerald_wave", Name: "翡翠波", Tones: []string{"#064e3b", "#047857", "#34d399"}},
	{ID: "slate_tech", Name: "银灰科技", Tones: []string{"#0f172a", "#334155", "#64748b"}},
}

// DefaultCreditOptions are the quick-select credit amounts in the admin UI.
var DefaultCreditOptions = []float64{10000, 100000, 1000000}

// DefaultComputeCardTypes returns the built-in on-shelf cards used when a
// HubCenter has no card products yet.
func DefaultComputeCardTypes(serviceGroupID string) []*CardType {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	return []*CardType{
		{
			ID:             "maclaw_compute_month_10000",
			ServiceGroupID: serviceGroupID,
			Label:          "MaClaw 官方月卡",
			Description:    "适合轻量研发与日常问答的官方算力额度。",
			Credits:        10000,
			Period:         "month",
			PriceRMB:       99,
			Template:       "enterprise_monthly_blue",
			Enabled:        true,
		},
		{
			ID:             "maclaw_compute_quarter_100000",
			ServiceGroupID: serviceGroupID,
			Label:          "MaClaw 官方季度卡",
			Description:    "适合团队持续使用的季度算力额度。",
			Credits:        100000,
			Period:         "quarter",
			PriceRMB:       799,
			Template:       "enterprise_quarter_emerald",
			Enabled:        true,
		},
		{
			ID:             "maclaw_compute_year_1000000",
			ServiceGroupID: serviceGroupID,
			Label:          "MaClaw 官方年度卡",
			Description:    "适合高频业务与自动化流程的年度算力池。",
			Credits:        1000000,
			Period:         "year",
			PriceRMB:       5999,
			Template:       "enterprise_annual_slate",
			Enabled:        true,
		},
	}
}

// ---------------------------------------------------------------------------
// Card Type (dynamic, admin-created)
// ---------------------------------------------------------------------------

// CardType represents a purchasable card type created by HubCenter admin.
type CardType struct {
	ID             string    `json:"id"`
	ServiceGroupID string    `json:"service_group_id"`
	ServiceGroup   string    `json:"service_group,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	AgentName      string    `json:"agent_name,omitempty"`
	Label          string    `json:"label"`
	Description    string    `json:"description"`
	Credits        float64   `json:"credits"`
	Period         string    `json:"period"` // "month" / "quarter" / "year"
	PriceRMB       float64   `json:"price_rmb"`
	Template       string    `json:"template"` // must be one of BuiltinCardTemplates IDs
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PeriodToDays converts a period string to duration days.
func PeriodToDays(period string) int {
	switch period {
	case "month":
		return 30
	case "quarter":
		return 91
	case "year":
		return 365
	default:
		return 30
	}
}

// IsValidTemplate checks if a template ID is one of the builtin templates.
// Also accepts legacy template IDs for backward compatibility with existing data.
func IsValidTemplate(id string) bool {
	for _, t := range BuiltinCardTemplates {
		if t.ID == id {
			return true
		}
	}
	// Legacy IDs (deprecated, accepted for backward compat)
	switch id {
	case "gradient_blue", "gradient_purple", "gradient_gold", "dark_tech", "minimal_white", "ocean_green":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Card Type Repository
// ---------------------------------------------------------------------------

// CardTypeRepository persists card type definitions.
type CardTypeRepository interface {
	Create(ctx context.Context, ct *CardType) error
	Update(ctx context.Context, ct *CardType) error
	GetByID(ctx context.Context, id string) (*CardType, error)
	ListEnabled(ctx context.Context) ([]*CardType, error)
	ListAll(ctx context.Context) ([]*CardType, error)
	Delete(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// Purchase Order (extends corelib/cardstore.Order with HubCenter fields)
// ---------------------------------------------------------------------------

// PurchaseOrder extends the shared order with HubCenter-specific fields.
type PurchaseOrder struct {
	corecardstore.Order

	// HubCenter-specific fields
	HubID          string  `json:"hub_id"`
	TenantID       string  `json:"tenant_id"`
	CardTypeID     string  `json:"card_type_id"`
	ServiceGroupID string  `json:"service_group_id"`
	AgentID        string  `json:"agent_id,omitempty"`
	AgentName      string  `json:"agent_name,omitempty"`
	Credits        float64 `json:"credits"`
	Period         string  `json:"period"`
	ArchivedAt     string  `json:"archived_at,omitempty"`
}

// PurchaseOrderRepository persists purchase orders.
type PurchaseOrderRepository interface {
	Create(ctx context.Context, order *PurchaseOrder) error
	GetByOrderNo(ctx context.Context, orderNo string) (*PurchaseOrder, error)
	List(ctx context.Context, filter OrderFilter) ([]*PurchaseOrder, int, error)
	UpdateStatus(ctx context.Context, orderNo, status string, now time.Time) error
	Update(ctx context.Context, order *PurchaseOrder) error
	Delete(ctx context.Context, orderNo string) error
	Archive(ctx context.Context, orderNo string, archivedAt time.Time) error
}

// OrderFilter for querying orders.
type OrderFilter struct {
	HubID           string
	TenantID        string
	Email           string
	ServiceGroupID  string
	Status          string
	ArchivedOnly    bool
	IncludeArchived bool
	Offset          int
	Limit           int
}

// ---------------------------------------------------------------------------
// Service (business logic)
// ---------------------------------------------------------------------------

// Service manages the HubCenter card store operations.
type Service struct {
	cardTypes    CardTypeRepository
	orders       PurchaseOrderRepository
	authRepo     llmservice.TenantAuthorizationRepository
	payment      corecardstore.PersonalPaymentConfig
	alipay       corecardstore.AlipayDirectConfig
	verifyTenant func(ctx context.Context, hubID, tenantID, email string) error // security check
	auditLog     func(ctx context.Context, action, detail string)               // audit trail
	resolveGroup func(ctx context.Context, serviceGroupID string) (serviceGroupName, agentID, agentName string)
}

// NewService creates a card store service.
func NewService(
	cardTypes CardTypeRepository,
	orders PurchaseOrderRepository,
	authRepo llmservice.TenantAuthorizationRepository,
) *Service {
	return &Service{
		cardTypes: cardTypes,
		orders:    orders,
		authRepo:  authRepo,
	}
}

// SetTenantVerifier sets the function that validates hub+tenant+email identity.
func (s *Service) SetTenantVerifier(fn func(ctx context.Context, hubID, tenantID, email string) error) {
	s.verifyTenant = fn
}

// SetServiceGroupResolver enriches card type views with service group and agent metadata.
func (s *Service) SetServiceGroupResolver(fn func(ctx context.Context, serviceGroupID string) (serviceGroupName, agentID, agentName string)) {
	s.resolveGroup = fn
}

// SetAuditLogger sets the audit log function for tracking admin operations.
func (s *Service) SetAuditLogger(fn func(ctx context.Context, action, detail string)) {
	s.auditLog = fn
}

// SetPaymentConfig updates the payment configuration.
func (s *Service) SetPaymentConfig(personal corecardstore.PersonalPaymentConfig, alipay corecardstore.AlipayDirectConfig) {
	s.payment = personal
	s.alipay = alipay
}

// ---------------------------------------------------------------------------
// Card Type Management
// ---------------------------------------------------------------------------

// CreateCardType creates a new card type (admin operation).
func (s *Service) CreateCardType(ctx context.Context, ct *CardType) error {
	if err := normalizeAndValidateCardType(ct, true); err != nil {
		return err
	}
	now := time.Now().UTC()
	ct.CreatedAt = now
	ct.UpdatedAt = now
	return s.cardTypes.Create(ctx, ct)
}

// EnsureDefaultComputeCardTypes creates default purchasable cards only when no
// card products exist yet. Existing admin-configured products are never changed.
func (s *Service) EnsureDefaultComputeCardTypes(ctx context.Context, serviceGroupID string) error {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if serviceGroupID == "" {
		return fmt.Errorf("service_group_id is required")
	}
	existing, err := s.ListAllCardTypes(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	for _, ct := range DefaultComputeCardTypes(serviceGroupID) {
		if err := s.CreateCardType(ctx, ct); err != nil {
			if s.defaultComputeCardTypesExist(ctx, serviceGroupID) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *Service) defaultComputeCardTypesExist(ctx context.Context, serviceGroupID string) bool {
	for _, ct := range DefaultComputeCardTypes(serviceGroupID) {
		existing, err := s.cardTypes.GetByID(ctx, ct.ID)
		if err != nil || existing == nil || !existing.Enabled || existing.ServiceGroupID != serviceGroupID {
			return false
		}
	}
	return true
}

// UpdateCardType updates an existing card type.
func (s *Service) UpdateCardType(ctx context.Context, ct *CardType) error {
	if err := normalizeAndValidateCardType(ct, false); err != nil {
		return err
	}
	ct.UpdatedAt = time.Now().UTC()
	return s.cardTypes.Update(ctx, ct)
}

func normalizeAndValidateCardType(ct *CardType, creating bool) error {
	if ct == nil {
		return fmt.Errorf("card type is required")
	}
	ct.ID = strings.TrimSpace(ct.ID)
	ct.ServiceGroupID = strings.TrimSpace(ct.ServiceGroupID)
	ct.Label = strings.TrimSpace(ct.Label)
	ct.Description = strings.TrimSpace(ct.Description)
	ct.Period = strings.TrimSpace(ct.Period)
	ct.Template = strings.TrimSpace(ct.Template)
	if ct.Template == "" {
		ct.Template = "enterprise_monthly_blue"
	}
	if !creating && ct.ID == "" {
		return fmt.Errorf("id is required")
	}
	if ct.ServiceGroupID == "" {
		return fmt.Errorf("service_group_id is required")
	}
	if ct.Label == "" {
		return fmt.Errorf("label is required")
	}
	if ct.Credits <= 0 {
		return fmt.Errorf("credits must be positive")
	}
	if ct.PriceRMB <= 0 {
		return fmt.Errorf("price must be positive")
	}
	if !IsValidTemplate(ct.Template) {
		return fmt.Errorf("invalid card template: %s", ct.Template)
	}
	switch ct.Period {
	case "month", "quarter", "year":
	default:
		return fmt.Errorf("period must be month, quarter, or year")
	}
	return nil
}

// ListEnabledCardTypes returns all enabled (on-shelf) card types.
func (s *Service) ListEnabledCardTypes(ctx context.Context) ([]*CardType, error) {
	types, err := s.cardTypes.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	s.enrichCardTypes(ctx, types)
	return types, nil
}

// ListAllCardTypes returns all card types (admin view).
func (s *Service) ListAllCardTypes(ctx context.Context) ([]*CardType, error) {
	types, err := s.cardTypes.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	s.enrichCardTypes(ctx, types)
	return types, nil
}

func (s *Service) enrichCardTypes(ctx context.Context, types []*CardType) {
	if s.resolveGroup == nil {
		return
	}
	for _, ct := range types {
		if ct == nil || ct.ServiceGroupID == "" {
			continue
		}
		ct.ServiceGroup, ct.AgentID, ct.AgentName = s.resolveGroup(ctx, ct.ServiceGroupID)
	}
}

// ---------------------------------------------------------------------------
// Purchase Flow
// ---------------------------------------------------------------------------

// CreateOrder creates a purchase order for a card type.
func (s *Service) CreateOrder(ctx context.Context, cardTypeID, adminEmail, hubID, tenantID, payChannel string) (*PurchaseOrder, error) {
	cardTypeID = strings.TrimSpace(cardTypeID)
	adminEmail = strings.TrimSpace(adminEmail)
	hubID = strings.TrimSpace(hubID)
	tenantID = strings.TrimSpace(tenantID)
	payChannel = strings.TrimSpace(payChannel)
	if adminEmail == "" || hubID == "" || tenantID == "" {
		return nil, fmt.Errorf("admin_email, hub_id, and tenant_id are required")
	}

	// Security: verify the email is authorized for this hub+tenant
	if s.verifyTenant != nil {
		if err := s.verifyTenant(ctx, hubID, tenantID, adminEmail); err != nil {
			return nil, fmt.Errorf("identity verification failed: %w", err)
		}
	}

	ct, err := s.cardTypes.GetByID(ctx, cardTypeID)
	if err != nil {
		return nil, fmt.Errorf("get card type: %w", err)
	}
	if ct == nil {
		return nil, fmt.Errorf("card type %s not found", cardTypeID)
	}
	if !ct.Enabled {
		return nil, fmt.Errorf("card type %s is not available", cardTypeID)
	}
	if s.resolveGroup != nil && ct.ServiceGroupID != "" {
		ct.ServiceGroup, ct.AgentID, ct.AgentName = s.resolveGroup(ctx, ct.ServiceGroupID)
	}

	order := &PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:      corecardstore.GenerateOrderNo("HC"),
			ProductID:    ct.ID,
			ProductLabel: ct.Label,
			Email:        adminEmail,
			Amount:       ct.PriceRMB,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		},
		HubID:          hubID,
		TenantID:       tenantID,
		CardTypeID:     ct.ID,
		ServiceGroupID: ct.ServiceGroupID,
		AgentID:        ct.AgentID,
		AgentName:      ct.AgentName,
		Credits:        ct.Credits,
		Period:         ct.Period,
	}

	// Apply payment mode
	if payChannel != "" {
		// Semi-manual with specific channel
		if err := corecardstore.CreateSemiManualOrder(&order.Order, &s.payment, payChannel); err != nil {
			return nil, fmt.Errorf("create semi-manual order: %w", err)
		}
	} else if s.alipay.AppID != "" {
		// Alipay direct
		if _, err := corecardstore.CreateAlipayOrder(&order.Order, &s.alipay); err != nil {
			return nil, fmt.Errorf("create alipay order: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no payment method configured")
	}

	if err := s.orders.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("save order: %w", err)
	}
	return order, nil
}

// ConfirmOrder manually confirms payment and activates the order.
func (s *Service) ConfirmOrder(ctx context.Context, orderNo, reviewer string) error {
	order, err := s.orders.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order %s not found", orderNo)
	}

	// Use shared state machine for status validation
	switch order.Status {
	case corecardstore.StatusPending, corecardstore.StatusPersonalCreated, corecardstore.StatusPersonalOpened:
		// OK — first confirmation
	case corecardstore.StatusPaid:
		// Previously paid but activation may have failed — retry activation
		authID, err := s.activateOrder(ctx, order)
		if err != nil {
			return fmt.Errorf("retry activate: %w", err)
		}
		order.Status = corecardstore.StatusActivated
		order.PaymentID = authID
		order.UpdatedAt = time.Now().UTC()
		_ = s.orders.Update(ctx, order)
		if s.auditLog != nil {
			s.auditLog(ctx, "cardstore.order.reactivated", fmt.Sprintf("order=%s hub=%s tenant=%s", orderNo, order.HubID, order.TenantID))
		}
		return nil
	case corecardstore.StatusActivated:
		return nil // fully idempotent
	default:
		return fmt.Errorf("order %s has status %s, cannot confirm", orderNo, order.Status)
	}

	now := time.Now().UTC()
	order.Status = corecardstore.StatusPaid
	order.PaidAt = now
	order.ReviewedBy = reviewer
	order.ReviewedAt = now
	order.UpdatedAt = now

	// Activate: create/extend tenant authorization
	authID, err := s.activateOrder(ctx, order)
	if err != nil {
		order.PaymentMsg = fmt.Sprintf("activation failed: %v", err)
		_ = s.orders.Update(ctx, order)
		return fmt.Errorf("activate: %w", err)
	}

	order.Status = corecardstore.StatusActivated
	order.PaymentID = authID
	order.UpdatedAt = time.Now().UTC()
	if err := s.orders.Update(ctx, order); err != nil {
		return fmt.Errorf("save activated order %s: %w", orderNo, err)
	}

	// Audit log
	if s.auditLog != nil {
		s.auditLog(ctx, "cardstore.order.confirmed", fmt.Sprintf("order=%s reviewer=%s hub=%s tenant=%s credits=%.0f", orderNo, reviewer, order.HubID, order.TenantID, order.Credits))
	}
	return nil
}

// ListOrders returns orders matching the filter.
func (s *Service) ListOrders(ctx context.Context, filter OrderFilter) ([]*PurchaseOrder, int, error) {
	return s.orders.List(ctx, filter)
}

// ArchiveOrder hides an order from the default admin order queue without changing its payment status.
func (s *Service) ArchiveOrder(ctx context.Context, orderNo string) error {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return fmt.Errorf("order_no is required")
	}
	order, err := s.orders.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order %s not found", orderNo)
	}
	now := time.Now().UTC()
	if err := s.orders.Archive(ctx, orderNo, now); err != nil {
		return fmt.Errorf("archive order: %w", err)
	}
	if s.auditLog != nil {
		s.auditLog(ctx, "cardstore.order.archived", fmt.Sprintf("order=%s hub=%s tenant=%s", orderNo, order.HubID, order.TenantID))
	}
	return nil
}

// DeleteUnprocessedOrder removes an unpaid order owned by the requesting tenant admin.
func (s *Service) DeleteUnprocessedOrder(ctx context.Context, orderNo, email, hubID, tenantID string) error {
	orderNo = strings.TrimSpace(orderNo)
	email = strings.TrimSpace(email)
	hubID = strings.TrimSpace(hubID)
	tenantID = strings.TrimSpace(tenantID)
	if orderNo == "" || email == "" || hubID == "" || tenantID == "" {
		return fmt.Errorf("order_no, email, hub_id, and tenant_id are required")
	}
	order, err := s.orders.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order %s not found", orderNo)
	}
	if !strings.EqualFold(strings.TrimSpace(order.Email), email) || strings.TrimSpace(order.HubID) != hubID || strings.TrimSpace(order.TenantID) != tenantID {
		return fmt.Errorf("order %s does not match requester", orderNo)
	}
	switch order.Status {
	case corecardstore.StatusPending, corecardstore.StatusPersonalCreated, corecardstore.StatusPersonalOpened:
	default:
		return fmt.Errorf("order %s has status %s and cannot be deleted", orderNo, order.Status)
	}
	if err := s.orders.Delete(ctx, orderNo); err != nil {
		return fmt.Errorf("delete order: %w", err)
	}
	if s.auditLog != nil {
		s.auditLog(ctx, "cardstore.order.deleted", fmt.Sprintf("order=%s hub=%s tenant=%s", orderNo, hubID, tenantID))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Activation (creates/extends TenantAuthorization)
// ---------------------------------------------------------------------------

func (s *Service) activateOrder(ctx context.Context, order *PurchaseOrder) (string, error) {
	durationDays := PeriodToDays(order.Period)
	now := time.Now().UTC()

	auth := &llmservice.TenantAuthorization{
		ID:             fmt.Sprintf("auth_%s_%d", order.OrderNo, now.UnixMilli()),
		HubID:          order.HubID,
		TenantID:       order.TenantID,
		AdminEmail:     order.Email,
		ServiceGroupID: order.ServiceGroupID,
		CreditsTotal:   order.Credits,
		CreditsUsed:    0,
		StartsAt:       now,
		ExpiresAt:      now.AddDate(0, 0, durationDays),
		Status:         "active",
		Source:         "card",
		CardOrderID:    order.OrderNo,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if order.ServiceGroupID == llmservice.ExternalComputePermissionServiceGroupID {
		auth.AllowExternalProviders = true
	}

	if err := s.authRepo.Create(ctx, auth); err != nil {
		return "", fmt.Errorf("create authorization: %w", err)
	}
	return auth.ID, nil
}
