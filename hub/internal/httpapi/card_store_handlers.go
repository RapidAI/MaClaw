package httpapi

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	cardStoreConfigKey              = "card_store_config"
	cardStoreOrdersKey              = "card_store_orders"
	defaultPaymentFMAPIBaseURL      = "https://api-4z7jye7ftfr4.zhifu.fm.it88168.com/api"
	defaultPaymentFMMerchantNo      = "655219576405377024"
	defaultPaymentFMAccessKey       = "a752f5fe60b3bffa94264e0318f33dbc"
	cardStorePaymentModeFM          = "payment_fm"
	cardStorePaymentModeManual      = "personal_semimanual"
	cardStorePaymentModeAlipay      = "alipay_direct"
	defaultAlipayGatewayURL         = "https://openapi.alipay.com/gateway.do"
	cardStoreStatusPersonalCreated  = "personal_created"
	cardStoreStatusPersonalOpened   = "personal_opened"
	cardStoreStatusPersonalRejected = "personal_rejected"
)

var cardStoreDefaultProductSpecs = []struct {
	ID           string
	Kind         string
	Label        string
	Description  string
	Price        float64
	DurationDays int
	Credits      float64
	PeriodLimits llmservice.CreditPeriodLimits
}{
	{ID: "service_day", Kind: "service_card", Label: "Day Card", DurationDays: 1, Credits: 300, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 150}},
	{ID: "service_week", Kind: "service_card", Label: "Week Card", DurationDays: 7, Credits: 1200, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 300, Daily: 600}},
	{ID: "service_month", Kind: "service_card", Label: "Month Card", DurationDays: 30, Credits: 5000, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 600, Daily: 1200, Weekly: 2400}},
	{ID: "service_quarter", Kind: "service_card", Label: "Quarter Card", DurationDays: 91, Credits: 17000, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 1200, Daily: 2400, Weekly: 4800, Monthly: 10000}},
	{ID: "service_year", Kind: "service_card", Label: "Year Card", DurationDays: 365, Credits: 70000, PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 2400, Daily: 4800, Weekly: 9600, Monthly: 40000}},
	{ID: "credits_10000", Kind: "credits", Label: "10,000 Credits", DurationDays: 365, Credits: 10000},
	{ID: "credits_50000", Kind: "credits", Label: "50,000 Credits", DurationDays: 365, Credits: 50000},
	{ID: "service_test_10", Kind: "credits", Label: "Test Card", Description: "Only for recharge flow testing. Issues 1 credit.", Price: 0.01, DurationDays: 365, Credits: 1},
}

type cardStoreConfig struct {
	Enabled           bool                           `json:"enabled"`
	PaymentMode       string                         `json:"payment_mode,omitempty"`
	PaymentMethods    []string                       `json:"payment_methods,omitempty"`
	PaymentAPIBaseURL string                         `json:"payment_api_base_url"`
	MerchantNum       string                         `json:"merchant_num,omitempty"`
	AccessKey         string                         `json:"access_key,omitempty"`
	PayType           string                         `json:"pay_type,omitempty"`
	NotifyURL         string                         `json:"notify_url,omitempty"`
	PersonalPayment   cardStorePersonalPaymentConfig `json:"personal_payment,omitempty"`
	AlipayDirect      cardStoreAlipayDirectConfig    `json:"alipay_direct,omitempty"`
	ServiceGroupIDs   []string                       `json:"service_group_ids,omitempty"`
	Products          []cardStoreProduct             `json:"products"`
}

type cardStoreAlipayDirectConfig struct {
	AppID           string `json:"app_id,omitempty"`
	PrivateKey      string `json:"private_key,omitempty"`
	AlipayPublicKey string `json:"alipay_public_key,omitempty"`
	GatewayURL      string `json:"gateway_url,omitempty"`
	ProductCode     string `json:"product_code,omitempty"`
	SubjectPrefix   string `json:"subject_prefix,omitempty"`
	ReturnURL       string `json:"return_url,omitempty"`
	NotifyURL       string `json:"notify_url,omitempty"`
	SignType        string `json:"sign_type,omitempty"`
	PaymentMethod   string `json:"payment_method,omitempty"`
}

type cardStorePersonalPaymentConfig struct {
	AdminEmails []string                          `json:"admin_emails,omitempty"`
	Instruction string                            `json:"instruction,omitempty"`
	Channels    []cardStorePersonalPaymentChannel `json:"channels,omitempty"`
}

type cardStorePersonalPaymentChannel struct {
	ID           string `json:"id"`
	Label        string `json:"label,omitempty"`
	Payee        string `json:"payee,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
	Enabled      bool   `json:"enabled"`
	AlipayUserID string `json:"alipay_user_id,omitempty"`
	DeepLinkMode string `json:"deep_link_mode,omitempty"`
}

type cardStoreProduct struct {
	ID              string                        `json:"id"`
	Kind            string                        `json:"kind"`
	Label           string                        `json:"label"`
	Description     string                        `json:"description,omitempty"`
	Enabled         bool                          `json:"enabled"`
	Price           float64                       `json:"price"`
	DurationDays    int                           `json:"duration_days"`
	Credits         float64                       `json:"credits"`
	ServiceGroupIDs []string                      `json:"service_group_ids,omitempty"`
	PeriodLimits    llmservice.CreditPeriodLimits `json:"period_limits,omitempty"`
}

type cardStoreOrder struct {
	OrderNo               string    `json:"order_no"`
	TenantID              string    `json:"tenant_id,omitempty"`
	ProductID             string    `json:"product_id"`
	ProductLabel          string    `json:"product_label,omitempty"`
	Email                 string    `json:"email,omitempty"`
	SecondaryEmail        string    `json:"secondary_email,omitempty"`
	Amount                float64   `json:"amount"`
	Status                string    `json:"status"`
	PaymentMode           string    `json:"payment_mode,omitempty"`
	PayType               string    `json:"pay_type,omitempty"`
	PayChannel            string    `json:"pay_channel,omitempty"`
	PayChannelLabel       string    `json:"pay_channel_label,omitempty"`
	Payee                 string    `json:"payee,omitempty"`
	PayCode               string    `json:"pay_code,omitempty"`
	PayQRURL              string    `json:"pay_qr_url,omitempty"`
	PayDeepLink           string    `json:"pay_deep_link,omitempty"`
	PayInstruction        string    `json:"pay_instruction,omitempty"`
	PaymentID             string    `json:"payment_id,omitempty"`
	PayURL                string    `json:"pay_url,omitempty"`
	PaymentMsg            string    `json:"payment_msg,omitempty"`
	ActualPayAmount       string    `json:"actual_pay_amount,omitempty"`
	PayTime               string    `json:"pay_time,omitempty"`
	PlatformOrderNo       string    `json:"platform_order_no,omitempty"`
	ChannelOrderNo        string    `json:"channel_order_no,omitempty"`
	TradeType             string    `json:"trade_type,omitempty"`
	CardID                string    `json:"card_id,omitempty"`
	EncryptedCode         string    `json:"encrypted_code,omitempty"`
	MailStatus            string    `json:"mail_status,omitempty"`
	MailError             string    `json:"mail_error,omitempty"`
	AutoRedeemedAt        time.Time `json:"auto_redeemed_at,omitempty"`
	AutoRedeemError       string    `json:"auto_redeem_error,omitempty"`
	AdminApproveTokenHash string    `json:"admin_approve_token_hash,omitempty"`
	AdminDeleteTokenHash  string    `json:"admin_delete_token_hash,omitempty"`
	OpenedPaymentAt       time.Time `json:"opened_payment_at,omitempty"`
	ReminderMailStatus    string    `json:"reminder_mail_status,omitempty"`
	ReminderMailError     string    `json:"reminder_mail_error,omitempty"`
	ReminderMailSentAt    time.Time `json:"reminder_mail_sent_at,omitempty"`
	ReviewedBy            string    `json:"reviewed_by,omitempty"`
	ReviewedAt            time.Time `json:"reviewed_at,omitempty"`
	ReviewNote            string    `json:"review_note,omitempty"`
	PaidAt                time.Time `json:"paid_at,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type cardStoreOrders struct {
	Orders []cardStoreOrder `json:"orders"`
}

type createCardStoreOrderRequest struct {
	ProductID      string `json:"product_id"`
	Email          string `json:"email"`
	SecondaryEmail string `json:"secondary_email,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	PayChannel     string `json:"pay_channel,omitempty"`
	PaymentMethod  string `json:"payment_method,omitempty"`
}

type cardStorePaymentOpenedRequest struct {
	Email    string `json:"email"`
	TenantID string `json:"tenant_id,omitempty"`
}

type cardStoreManualReviewRequest struct {
	Amount         float64 `json:"amount,omitempty"`
	PayTime        string  `json:"pay_time,omitempty"`
	ChannelOrderNo string  `json:"channel_order_no,omitempty"`
	Note           string  `json:"note,omitempty"`
	Token          string  `json:"token,omitempty"`
}

type cardStoreRecoverRequest struct {
	Email    string `json:"email"`
	TenantID string `json:"tenant_id,omitempty"`
}

type cardStoreSalesStatRow struct {
	Bucket  string  `json:"bucket"`
	Orders  int     `json:"orders"`
	Revenue float64 `json:"revenue"`
	Cards   int     `json:"cards"`
}

type cardStoreSoldCard struct {
	OrderNo         string  `json:"order_no"`
	Status          string  `json:"status,omitempty"`
	Message         string  `json:"message,omitempty"`
	ProductID       string  `json:"product_id"`
	ProductLabel    string  `json:"product_label"`
	Amount          float64 `json:"amount"`
	Email           string  `json:"email,omitempty"`
	CardID          string  `json:"card_id,omitempty"`
	Code            string  `json:"code,omitempty"`
	Credits         float64 `json:"credits,omitempty"`
	DurationDays    int     `json:"duration_days,omitempty"`
	PaidAt          string  `json:"paid_at,omitempty"`
	RedeemedEmail   string  `json:"redeemed_email,omitempty"`
	RedeemedAt      string  `json:"redeemed_at,omitempty"`
	MailStatus      string  `json:"mail_status,omitempty"`
	MailError       string  `json:"mail_error,omitempty"`
	AutoRedeemed    bool    `json:"auto_redeemed,omitempty"`
	AutoRedeemAt    string  `json:"auto_redeemed_at,omitempty"`
	AutoRedeemErr   string  `json:"auto_redeem_error,omitempty"`
	PaymentID       string  `json:"payment_id,omitempty"`
	PaymentOrder    string  `json:"payment_order,omitempty"`
	ChannelOrder    string  `json:"channel_order,omitempty"`
	PayCode         string  `json:"pay_code,omitempty"`
	PayChannel      string  `json:"pay_channel,omitempty"`
	PayChannelLabel string  `json:"pay_channel_label,omitempty"`
	OpenedPaymentAt string  `json:"opened_payment_at,omitempty"`
	ReviewNote      string  `json:"review_note,omitempty"`
}

type cardStoreMailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

type paymentFMStartOrderResponse struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Code    int    `json:"code"`
	Data    *struct {
		ID     string `json:"id"`
		PayURL string `json:"payUrl"`
	} `json:"data"`
}

type cardStorePaymentAdapter interface {
	Mode() string
	Start(ctx context.Context, client *http.Client, cfg cardStoreConfig, system store.SystemSettingsRepository, order cardStoreOrder, req createCardStoreOrderRequest, notifyURL string) (cardStoreOrder, error)
}

type paymentFMCardStoreAdapter struct{}

func (paymentFMCardStoreAdapter) Mode() string { return cardStorePaymentModeFM }

func (paymentFMCardStoreAdapter) Start(ctx context.Context, client *http.Client, cfg cardStoreConfig, _ store.SystemSettingsRepository, order cardStoreOrder, _ createCardStoreOrderRequest, notifyURL string) (cardStoreOrder, error) {
	payResp, err := startPaymentFMOrder(ctx, client, cfg, order.OrderNo, formatPaymentAmount(order.Amount), notifyURL)
	if err != nil {
		order.Status = "payment_failed"
		order.PaymentMsg = err.Error()
		order.UpdatedAt = time.Now().UTC()
		return order, err
	}
	order.Status = "payment_started"
	if payResp.Data != nil {
		order.PaymentID = strings.TrimSpace(payResp.Data.ID)
		order.PayURL = strings.TrimSpace(payResp.Data.PayURL)
	}
	order.UpdatedAt = time.Now().UTC()
	return order, nil
}

type personalSemimanualCardStoreAdapter struct{}

func (personalSemimanualCardStoreAdapter) Mode() string { return cardStorePaymentModeManual }

func (personalSemimanualCardStoreAdapter) Start(ctx context.Context, _ *http.Client, cfg cardStoreConfig, system store.SystemSettingsRepository, order cardStoreOrder, req createCardStoreOrderRequest, _ string) (cardStoreOrder, error) {
	prepared, err := preparePersonalSemimanualOrder(ctx, system, cfg, order, req.PayChannel)
	if err != nil {
		prepared.Status = "payment_failed"
		prepared.PaymentMode = cardStorePaymentModeManual
		prepared.PaymentMsg = err.Error()
		prepared.UpdatedAt = time.Now().UTC()
	}
	return prepared, err
}

type alipayDirectCardStoreAdapter struct{}

func (alipayDirectCardStoreAdapter) Mode() string { return cardStorePaymentModeAlipay }

func (alipayDirectCardStoreAdapter) Start(_ context.Context, _ *http.Client, cfg cardStoreConfig, _ store.SystemSettingsRepository, order cardStoreOrder, _ createCardStoreOrderRequest, notifyURL string) (cardStoreOrder, error) {
	if strings.TrimSpace(cfg.AlipayDirect.AppID) == "" || strings.TrimSpace(cfg.AlipayDirect.PrivateKey) == "" || strings.TrimSpace(cfg.AlipayDirect.AlipayPublicKey) == "" {
		return order, fmt.Errorf("alipay direct configuration is incomplete")
	}
	order.PaymentMode = cardStorePaymentModeAlipay
	order.PayType = cardStorePaymentModeAlipay
	order.PayChannel = "alipay"
	order.PayChannelLabel = "Alipay"
	order.Status = "payment_started"
	order.PayURL = cardStoreAlipayPayURL(order.OrderNo, order.TenantID)
	order.UpdatedAt = time.Now().UTC()
	return order, nil
}

func cardStorePaymentAdapterForMode(mode string) cardStorePaymentAdapter {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case cardStorePaymentModeManual:
		return personalSemimanualCardStoreAdapter{}
	case cardStorePaymentModeAlipay:
		return alipayDirectCardStoreAdapter{}
	default:
		return paymentFMCardStoreAdapter{}
	}
}

func normalizeCardStorePaymentMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case cardStorePaymentModeManual:
		return cardStorePaymentModeManual
	case cardStorePaymentModeAlipay:
		return cardStorePaymentModeAlipay
	case cardStorePaymentModeFM:
		return cardStorePaymentModeFM
	default:
		return cardStorePaymentModeFM
	}
}

func parseCardStorePaymentMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case cardStorePaymentModeFM:
		return cardStorePaymentModeFM, true
	case cardStorePaymentModeManual:
		return cardStorePaymentModeManual, true
	case cardStorePaymentModeAlipay:
		return cardStorePaymentModeAlipay, true
	default:
		return "", false
	}
}

func normalizeCardStorePaymentMethods(methods []string, fallback string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, method := range methods {
		mode, ok := parseCardStorePaymentMode(method)
		if !ok {
			continue
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	if preferred, ok := parseCardStorePaymentMode(fallback); ok {
		for i, method := range out {
			if method != preferred {
				continue
			}
			if i > 0 {
				copy(out[1:i+1], out[0:i])
				out[0] = preferred
			}
			break
		}
	}
	if len(out) == 0 {
		if mode, ok := parseCardStorePaymentMode(fallback); ok {
			out = append(out, mode)
		} else {
			out = append(out, cardStorePaymentModeFM)
		}
	}
	return out
}

func cardStorePaymentMethodEnabled(cfg cardStoreConfig, mode string) bool {
	mode, ok := parseCardStorePaymentMode(mode)
	if !ok {
		return false
	}
	if len(cfg.PaymentMethods) == 0 {
		return normalizeCardStorePaymentMode(cfg.PaymentMode) == mode
	}
	for _, method := range cfg.PaymentMethods {
		if parsed, ok := parseCardStorePaymentMode(method); ok && parsed == mode {
			return true
		}
	}
	return false
}

func resolveCardStorePaymentSelection(cfg cardStoreConfig, req createCardStoreOrderRequest) (string, string, error) {
	requestedMethod := strings.TrimSpace(req.PaymentMethod)
	requestedChannel := strings.TrimSpace(req.PayChannel)
	requested := firstNonEmptyString(requestedMethod, requestedChannel)
	if requested == "" {
		return cfg.PaymentMode, "", nil
	}
	key := strings.ToLower(requested)
	if strings.HasPrefix(key, "manual:") {
		rawChannel := strings.TrimPrefix(key, "manual:")
		channel := normalizeCardStorePaymentChannel(rawChannel)
		if channel == "" {
			channel = rawChannel
		}
		if !cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeManual) {
			return "", "", fmt.Errorf("payment method is not enabled: %s", cardStorePaymentModeManual)
		}
		if rawChannel != "" {
			if _, err := selectCardStorePersonalChannel(cfg.PersonalPayment, rawChannel); err != nil {
				return "", "", err
			}
		}
		return cardStorePaymentModeManual, channel, nil
	}
	switch key {
	case cardStorePaymentModeFM, cardStorePaymentModeAlipay, cardStorePaymentModeManual:
		mode := normalizeCardStorePaymentMode(key)
		if !cardStorePaymentMethodEnabled(cfg, mode) {
			return "", "", fmt.Errorf("payment method is not enabled: %s", mode)
		}
		return mode, "", nil
	default:
		if requestedMethod != "" {
			return "", "", fmt.Errorf("payment method is not enabled: %s", requested)
		}
		channel := normalizeCardStorePaymentChannel(key)
		if channel != "" && cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeManual) {
			if _, err := selectCardStorePersonalChannel(cfg.PersonalPayment, channel); err != nil {
				return "", "", err
			}
			return cardStorePaymentModeManual, channel, nil
		}
		if cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeManual) {
			if _, err := selectCardStorePersonalChannel(cfg.PersonalPayment, requested); err != nil {
				return "", "", err
			}
			return cardStorePaymentModeManual, normalizeCardStorePaymentChannel(requested), nil
		}
	}
	return "", "", fmt.Errorf("payment method is not enabled: %s", requested)
}

func GetCardStoreProductsHandler(system store.SystemSettingsRepository, tenants ...store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings := system
		tenantID := cardStoreTenantIDFromRequest(r, firstTenantRepository(tenants))
		if tenantID != store.DefaultTenantID {
			settings = scopedSystemSettingsForTenant(tenantID, system)
		}
		cfg := loadCardStoreConfig(r.Context(), settings)
		products := make([]cardStoreProduct, 0, len(cfg.Products))
		for _, product := range cfg.Products {
			if product.Enabled {
				products = append(products, publicCardStoreProduct(product))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": cfg.Enabled, "tenant_id": tenantID, "payment_mode": cfg.PaymentMode, "payment_channels": publicCardStorePaymentChannels(cfg), "products": products})
	}
}

func GetCardStoreMeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "email": principal.Email, "tenant_id": principal.TenantID})
	}
}

func GetCardStoreConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := loadCardStoreConfig(r.Context(), scopedSystemSettingsForRequest(r, system))
		if strings.TrimSpace(cfg.NotifyURL) == "" {
			cfg.NotifyURL = defaultCardStoreNotifyURL(r)
		}
		cfg.AccessKey = ""
		cfg.AlipayDirect.PrivateKey = ""
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateCardStoreConfigHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		oldCfg := loadCardStoreConfig(r.Context(), system)
		var req cardStoreConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if strings.TrimSpace(req.AccessKey) == "" {
			req.AccessKey = oldCfg.AccessKey
		}
		if strings.TrimSpace(req.AlipayDirect.PrivateKey) == "" {
			req.AlipayDirect.PrivateKey = oldCfg.AlipayDirect.PrivateKey
		}
		scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
		validatePayment := true
		if scope == "store" {
			req.PaymentMode = oldCfg.PaymentMode
			req.PaymentMethods = append([]string(nil), oldCfg.PaymentMethods...)
			req.PaymentAPIBaseURL = oldCfg.PaymentAPIBaseURL
			req.MerchantNum = oldCfg.MerchantNum
			req.AccessKey = oldCfg.AccessKey
			req.PayType = oldCfg.PayType
			req.NotifyURL = oldCfg.NotifyURL
			req.PersonalPayment = oldCfg.PersonalPayment
			req.AlipayDirect = oldCfg.AlipayDirect
			validatePayment = false
		} else if scope == "payment" {
			req.Enabled = oldCfg.Enabled
			req.ServiceGroupIDs = append([]string(nil), oldCfg.ServiceGroupIDs...)
			req.Products = append([]cardStoreProduct(nil), oldCfg.Products...)
		}
		if validatePayment && notifyURLMatchesCurrentHub(req.NotifyURL, defaultCardStoreNotifyURL(r)) {
			req.NotifyURL = ""
		}
		if validatePayment && cardStoreURLMatchesEndpointIgnoringTenantID(req.AlipayDirect.NotifyURL, "/api/card-store/payment/notify") {
			req.AlipayDirect.NotifyURL = ""
		}
		if validatePayment && cardStoreURLMatchesEndpointIgnoringTenantID(req.AlipayDirect.ReturnURL, "/card_store") {
			req.AlipayDirect.ReturnURL = ""
		}
		next := normalizeCardStoreConfig(req)
		if validatePayment {
			if err := validateCardStoreConfigForSave(next); err != nil {
				writeError(w, http.StatusBadRequest, "CARD_STORE_CONFIG_INVALID", err.Error())
				return
			}
		}
		data, err := json.Marshal(next)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		if err := system.Set(r.Context(), cardStoreConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "card_store.config.update", map[string]any{"enabled": next.Enabled, "payment_mode": next.PaymentMode, "payment_api_base_url": next.PaymentAPIBaseURL, "pay_type": next.PayType})
		next.AccessKey = ""
		next.AlipayDirect.PrivateKey = ""
		writeJSON(w, http.StatusOK, next)
	}
}

func GetCardStoreSalesStatsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
		if period != "month" {
			period = "day"
		}
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 30)
		if period == "month" && limit > 36 {
			limit = 36
		} else if period == "day" && limit > 366 {
			limit = 366
		}
		settings := scopedSystemSettingsForRequest(r, system)
		orders := loadCardStoreOrders(r.Context(), settings)
		rows, totalOrders, totalRevenue, totalCards := buildCardStoreSalesStats(orders.Orders, period, limit)
		reg, _ := llmservice.LoadRegistry(r.Context(), settings)
		soldCards := buildCardStoreSoldCards(orders.Orders, loadCardStoreConfig(r.Context(), settings), reg)
		writeJSON(w, http.StatusOK, map[string]any{"period": period, "limit": limit, "total_orders": totalOrders, "total_revenue": roundMoney(totalRevenue), "total_cards": totalCards, "rows": rows, "cards": soldCards})
	}
}

func AdminCardStorePaymentQRUploadHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channel := normalizeCardStorePaymentChannel(r.FormValue("channel"))
		if channel == "" {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_QR_CHANNEL_INVALID", "valid payment channel is required")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_QR_FILE_REQUIRED", "payment QR image file is required")
			return
		}
		defer file.Close()
		const maxQRSize = 2 << 20
		data, err := io.ReadAll(io.LimitReader(file, maxQRSize+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_QR_READ_FAILED", err.Error())
			return
		}
		if len(data) == 0 || len(data) > maxQRSize {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_QR_SIZE_INVALID", "payment QR image must be smaller than 2MB")
			return
		}
		contentType, ext, ok := cardStoreDetectPaymentQRImage(data)
		if !ok {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_QR_TYPE_INVALID", "payment QR image must be PNG, JPEG, GIF, or WebP")
			return
		}
		tenantID := store.NormalizeTenantID(RequestTenantID(r))
		base := filepath.Clean(strings.TrimSpace(uploadDir))
		if base == "." || base == "" {
			base = filepath.Join("data", "card-store", "payment-qr")
		}
		tenantDir := filepath.Join(base, cardStoreSafePathSegment(tenantID))
		if err := os.MkdirAll(tenantDir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_PAYMENT_QR_SAVE_FAILED", err.Error())
			return
		}
		sum := sha256.Sum256(data)
		filename := channel + "-" + hex.EncodeToString(sum[:8]) + ext
		path := filepath.Join(tenantDir, filename)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_PAYMENT_QR_SAVE_FAILED", err.Error())
			return
		}
		publicURL := "/api/card-store/payment-qr/" + url.PathEscape(tenantID) + "/" + url.PathEscape(filename)
		writeJSON(w, http.StatusOK, map[string]any{"url": publicURL, "image_url": publicURL, "channel": channel, "tenant_id": tenantID, "content_type": contentType, "size": len(data)})
	}
}

func CardStorePaymentQRImageHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := store.NormalizeTenantID(r.PathValue("tenantID"))
		filename := strings.TrimSpace(r.PathValue("filename"))
		if tenantID == "" || filename == "" || filepath.Base(filename) != filename {
			http.NotFound(w, r)
			return
		}
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		default:
			http.NotFound(w, r)
			return
		}
		base := filepath.Clean(strings.TrimSpace(uploadDir))
		if base == "." || base == "" {
			base = filepath.Join("data", "card-store", "payment-qr")
		}
		path := filepath.Join(base, cardStoreSafePathSegment(tenantID), filename)
		if filepath.Base(path) != filename {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	}
}

func cardStoreDetectPaymentQRImage(data []byte) (string, string, bool) {
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png", ".png", true
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg", ".jpg", true
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif", ".gif", true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp", ".webp", true
	}
	return "", "", false
}

func cardStoreSafePathSegment(value string) string {
	value = store.NormalizeTenantID(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return store.DefaultTenantID
	}
	return b.String()
}

func CreateCardStoreOrderHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, mailer cardStoreMailer, client *http.Client) http.HandlerFunc {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req createCardStoreOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := normalizeCardStoreEmail(req.Email)
		secondaryEmail := normalizeCardStoreEmail(req.SecondaryEmail)
		if !validCardStoreEmail(email) || (secondaryEmail != "" && !validCardStoreEmail(secondaryEmail)) {
			writeError(w, http.StatusBadRequest, "CARD_STORE_EMAIL_INVALID", "valid email is required")
			return
		}
		tenantID, err := resolveCardStoreTenantForEmail(r.Context(), identity, req.TenantID, email)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_TENANT_REQUIRED", err.Error())
			return
		}
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		cfg := loadCardStoreConfig(r.Context(), tenantSystem)
		if !cfg.Enabled {
			writeError(w, http.StatusServiceUnavailable, "CARD_STORE_DISABLED", "card store is disabled")
			return
		}
		product, ok := findCardStoreProduct(cfg, req.ProductID)
		if !ok || !product.Enabled {
			writeError(w, http.StatusNotFound, "CARD_STORE_PRODUCT_NOT_FOUND", "product not found")
			return
		}
		if product.Price <= 0 {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PRICE_REQUIRED", "product price is not configured")
			return
		}
		selectedMode, selectedPersonalChannel, err := resolveCardStorePaymentSelection(cfg, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_METHOD_INVALID", err.Error())
			return
		}
		if selectedMode == cardStorePaymentModeFM && (strings.TrimSpace(cfg.MerchantNum) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.PayType) == "") {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_CONFIG_REQUIRED", "payment configuration is incomplete")
			return
		}
		if selectedMode == cardStorePaymentModeAlipay {
			if err := validateCardStorePaymentMethodForSave(cfg, cardStorePaymentModeAlipay); err != nil {
				writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_CONFIG_REQUIRED", err.Error())
				return
			}
		}
		notifyURL := strings.TrimSpace(cfg.NotifyURL)
		if notifyURL == "" {
			notifyURL = defaultCardStoreNotifyURL(r)
		}
		notifyURL = cardStoreNotifyURLWithTenant(notifyURL, tenantID)
		now := time.Now().UTC()
		order := cardStoreOrder{OrderNo: newCardStoreOrderNo(now), TenantID: tenantID, ProductID: product.ID, ProductLabel: product.Label, Email: email, SecondaryEmail: secondaryEmail, Amount: roundMoney(product.Price), Status: "created", PaymentMode: selectedMode, PayType: cfg.PayType, CreatedAt: now, UpdatedAt: now}
		adapter := cardStorePaymentAdapterForMode(selectedMode)
		startReq := req
		if selectedPersonalChannel != "" {
			startReq.PayChannel = selectedPersonalChannel
		}
		if err := appendCardStoreOrder(r.Context(), tenantSystem, order); err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_ORDER_SAVE_FAILED", err.Error())
			return
		}
		started, err := adapter.Start(r.Context(), client, cfg, tenantSystem, order, startReq, notifyURL)
		if err != nil {
			_ = saveCardStorePaymentFailed(r.Context(), tenantSystem, started)
			if current, ok := findCardStoreOrder(r.Context(), tenantSystem, order.OrderNo); ok && isCardStorePaidLikeStatus(current.Status) {
				writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(tenantID, email, current))
				return
			}
			status := http.StatusBadGateway
			code := "CARD_STORE_PAYMENT_FAILED"
			if adapter.Mode() == cardStorePaymentModeManual {
				status = http.StatusBadRequest
				code = "CARD_STORE_PERSONAL_PAYMENT_CONFIG_INVALID"
			}
			writeError(w, status, code, err.Error())
			return
		}
		if err := saveCardStorePaymentStarted(r.Context(), tenantSystem, started); err != nil {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_ORDER_SAVE_FAILED", err.Error())
			return
		}
		if current, ok := findCardStoreOrder(r.Context(), tenantSystem, order.OrderNo); ok {
			writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(tenantID, email, current))
			return
		}
		writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(tenantID, email, started))
	}
}

func cardStoreOrderCreateResponse(tenantID string, email string, order cardStoreOrder) map[string]any {
	resp := map[string]any{"order_no": order.OrderNo, "tenant_id": tenantID, "email": email, "product_id": order.ProductID, "product_label": order.ProductLabel, "payment_mode": order.PaymentMode, "pay_url": order.PayURL, "payment_id": order.PaymentID, "status": order.Status, "mail_status": order.MailStatus}
	if order.PayChannel != "" {
		resp["pay_channel"] = order.PayChannel
		resp["pay_channel_label"] = order.PayChannelLabel
		resp["payee"] = order.Payee
		resp["pay_code"] = order.PayCode
		resp["pay_qr_url"] = order.PayQRURL
		resp["pay_deep_link"] = order.PayDeepLink
		resp["pay_instruction"] = order.PayInstruction
		resp["amount"] = order.Amount
	}
	if !order.OpenedPaymentAt.IsZero() {
		resp["opened_payment_at"] = order.OpenedPaymentAt.UTC().Format(time.RFC3339)
	}
	if order.ReminderMailStatus != "" {
		resp["reminder_mail_status"] = order.ReminderMailStatus
	}
	if order.ReminderMailError != "" {
		resp["reminder_mail_error"] = order.ReminderMailError
	}
	if order.ReviewNote != "" {
		resp["review_note"] = order.ReviewNote
	}
	if strings.EqualFold(strings.TrimSpace(order.Status), "paid") {
		resp["code"] = llmservice.DecryptCardCode(order.EncryptedCode)
		resp["card_id"] = order.CardID
	}
	if order.PaymentMsg != "" {
		resp["message"] = order.PaymentMsg
	}
	if order.MailError != "" {
		resp["mail_error"] = order.MailError
	}
	if !order.AutoRedeemedAt.IsZero() {
		resp["auto_redeemed"] = true
		resp["auto_redeemed_at"] = order.AutoRedeemedAt.UTC().Format(time.RFC3339)
	}
	if order.AutoRedeemError != "" {
		resp["auto_redeem_error"] = order.AutoRedeemError
	}
	return resp
}

func CardStorePaymentNotifyHandler(system store.SystemSettingsRepository, mailer cardStoreMailer, identities ...*auth.IdentityService) http.HandlerFunc {
	var identity *auth.IdentityService
	if len(identities) > 0 {
		identity = identities[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writePlainPaymentNotifyResult(w, "fail")
			return
		}
		if strings.TrimSpace(r.Form.Get("trade_status")) != "" || strings.TrimSpace(r.Form.Get("app_id")) != "" {
			handleAlipayDirectNotify(w, r, system, mailer, identity)
			return
		}
		tenantID := store.NormalizeTenantID(r.Form.Get("tenant_id"))
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		cfg := loadCardStoreConfig(r.Context(), tenantSystem)
		state := strings.TrimSpace(r.Form.Get("state"))
		merchantNum := strings.TrimSpace(r.Form.Get("merchantNum"))
		orderNo := strings.TrimSpace(r.Form.Get("orderNo"))
		amount := strings.TrimSpace(r.Form.Get("amount"))
		sign := strings.TrimSpace(r.Form.Get("sign"))
		if state != "1" || merchantNum == "" || orderNo == "" || amount == "" || sign == "" || strings.TrimSpace(cfg.AccessKey) == "" {
			writePlainPaymentNotifyResult(w, "fail")
			return
		}
		if !strings.EqualFold(merchantNum, strings.TrimSpace(cfg.MerchantNum)) {
			writePlainPaymentNotifyResult(w, "fail")
			return
		}
		wantSign := zhifuXPayNotifySign(state, merchantNum, orderNo, amount, cfg.AccessKey)
		if !strings.EqualFold(sign, wantSign) {
			writePlainPaymentNotifyResult(w, "fail")
			return
		}
		paidAt := time.Now().UTC()
		update := cardStoreOrder{
			OrderNo:         orderNo,
			Amount:          parsePaymentAmount(amount),
			Status:          "paid",
			PaymentMsg:      "payment notified",
			ActualPayAmount: strings.TrimSpace(r.Form.Get("actualPayAmount")),
			Payee:           strings.TrimSpace(r.Form.Get("payee")),
			PayTime:         strings.TrimSpace(r.Form.Get("payTime")),
			PlatformOrderNo: strings.TrimSpace(r.Form.Get("platformOrderNo")),
			ChannelOrderNo:  strings.TrimSpace(r.Form.Get("channelOrderNo")),
			PayType:         strings.TrimSpace(firstNonEmptyString(r.Form.Get("type"), r.Form.Get("tradeType"))),
			TradeType:       strings.TrimSpace(r.Form.Get("tradeType")),
			PaidAt:          paidAt,
			UpdatedAt:       paidAt,
		}
		if err := markCardStoreOrderPaid(r.Context(), tenantSystem, cfg, update, mailer, identity, tenantID, externalLLMBaseURL(r)); err != nil {
			writePlainPaymentNotifyResult(w, "fail")
			return
		}
		writePlainPaymentNotifyResult(w, "success")
	}
}

func CardStoreAlipayPayPageHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		tenantID := store.NormalizeTenantID(r.URL.Query().Get("tenant_id"))
		settings := scopedSystemSettingsForTenant(tenantID, system)
		order, ok := findCardStoreOrder(r.Context(), settings, orderNo)
		if !ok || order.PaymentMode != cardStorePaymentModeAlipay {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		cfg := loadCardStoreConfig(r.Context(), settings)
		if !cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeAlipay) {
			http.Error(w, "alipay direct payment is disabled", http.StatusConflict)
			return
		}
		if isCardStorePaidLikeStatus(order.Status) {
			http.Error(w, "order already paid", http.StatusConflict)
			return
		}
		notifyURL := strings.TrimSpace(cfg.AlipayDirect.NotifyURL)
		if notifyURL == "" {
			notifyURL = cardStoreNotifyURLWithTenant(defaultCardStoreAlipayNotifyURL(r), tenantID)
		} else {
			notifyURL = cardStoreNotifyURLWithTenant(notifyURL, tenantID)
		}
		returnURL := alipayDirectReturnURL(r, order, tenantID)
		values, err := buildAlipayDirectRequest(cfg.AlipayDirect, order, notifyURL, returnURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderAlipaySubmitPage(cfg.AlipayDirect.GatewayURL, values)))
	}
}

func CardStoreAlipayReturnHandler(system store.SystemSettingsRepository, mailer cardStoreMailer, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid alipay return", http.StatusBadRequest)
			return
		}
		tenantID := store.NormalizeTenantID(r.URL.Query().Get("tenant_id"))
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		cfg := loadCardStoreConfig(r.Context(), tenantSystem)
		appID := firstNonEmptyString(r.Form.Get("app_id"), r.Form.Get("auth_app_id"))
		if !cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeAlipay) || !strings.EqualFold(strings.TrimSpace(appID), strings.TrimSpace(cfg.AlipayDirect.AppID)) {
			http.Error(w, "alipay direct payment is disabled or app_id mismatch", http.StatusBadRequest)
			return
		}
		orderNo := strings.TrimSpace(firstNonEmptyString(r.Form.Get("out_trade_no"), r.PathValue("orderNo")))
		pathOrderNo := strings.TrimSpace(r.PathValue("orderNo"))
		if pathOrderNo != "" && orderNo != pathOrderNo {
			http.Error(w, "alipay return order mismatch", http.StatusBadRequest)
			return
		}
		order, ok := findCardStoreOrder(r.Context(), tenantSystem, orderNo)
		if !ok || order.PaymentMode != cardStorePaymentModeAlipay {
			http.Error(w, "order not found", http.StatusNotFound)
			return
		}
		if !verifyAlipayDirectNotify(alipayNotifySignedValues(r), cfg.AlipayDirect.AlipayPublicKey) {
			_ = updateCardStoreOrderPaymentMessage(r.Context(), tenantSystem, orderNo, "alipay return signature invalid; waiting async notify")
			http.Redirect(w, r, cardStoreOrderReturnRedirectURL(r, tenantID, order, "alipay_verify_pending"), http.StatusSeeOther)
			return
		}
		tradeStatus := strings.TrimSpace(r.Form.Get("trade_status"))
		if tradeStatus != "" && !isAlipayPaidTradeStatus(tradeStatus) {
			http.Redirect(w, r, cardStoreOrderReturnRedirectURL(r, tenantID, order, "alipay"), http.StatusSeeOther)
			return
		}
		paidAt := time.Now().UTC()
		amount := parsePaymentAmount(firstNonEmptyString(r.Form.Get("total_amount"), r.Form.Get("receipt_amount"), formatPaymentAmount(order.Amount)))
		update := cardStoreOrder{OrderNo: orderNo, Amount: amount, Status: "paid", PaymentMsg: "alipay direct returned", ActualPayAmount: firstNonEmptyString(r.Form.Get("receipt_amount"), r.Form.Get("total_amount")), PayType: cardStorePaymentModeAlipay, PayChannel: "alipay", PayChannelLabel: "Alipay", PlatformOrderNo: r.Form.Get("trade_no"), ChannelOrderNo: r.Form.Get("trade_no"), TradeType: firstNonEmptyString(tradeStatus, "SYNC_RETURN"), PaidAt: paidAt, UpdatedAt: paidAt}
		if err := markCardStoreOrderPaid(r.Context(), tenantSystem, cfg, update, mailer, identity, tenantID, externalLLMBaseURL(r)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if current, ok := findCardStoreOrder(r.Context(), tenantSystem, orderNo); ok {
			order = current
		}
		http.Redirect(w, r, cardStoreOrderReturnRedirectURL(r, tenantID, order, "alipay"), http.StatusSeeOther)
	}
}

func handleAlipayDirectNotify(w http.ResponseWriter, r *http.Request, system store.SystemSettingsRepository, mailer cardStoreMailer, identity *auth.IdentityService) {
	tenantID := store.NormalizeTenantID(r.URL.Query().Get("tenant_id"))
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	cfg := loadCardStoreConfig(r.Context(), tenantSystem)
	if !cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeAlipay) || !strings.EqualFold(strings.TrimSpace(r.Form.Get("app_id")), strings.TrimSpace(cfg.AlipayDirect.AppID)) {
		writePlainPaymentNotifyResult(w, "fail")
		return
	}
	if !verifyAlipayDirectNotify(alipayNotifySignedValues(r), cfg.AlipayDirect.AlipayPublicKey) {
		writePlainPaymentNotifyResult(w, "fail")
		return
	}
	status := strings.TrimSpace(r.Form.Get("trade_status"))
	if !isAlipayPaidTradeStatus(status) {
		writePlainPaymentNotifyResult(w, "success")
		return
	}
	orderNo := strings.TrimSpace(r.Form.Get("out_trade_no"))
	amount := parsePaymentAmount(firstNonEmptyString(r.Form.Get("total_amount"), r.Form.Get("receipt_amount")))
	order, ok := findCardStoreOrder(r.Context(), tenantSystem, orderNo)
	if !ok || order.PaymentMode != cardStorePaymentModeAlipay {
		writePlainPaymentNotifyResult(w, "fail")
		return
	}
	paidAt := time.Now().UTC()
	update := cardStoreOrder{OrderNo: orderNo, Amount: amount, Status: "paid", PaymentMsg: "alipay direct notified", ActualPayAmount: r.Form.Get("receipt_amount"), PayType: cardStorePaymentModeAlipay, PayChannel: "alipay", PayChannelLabel: "Alipay", PlatformOrderNo: r.Form.Get("trade_no"), ChannelOrderNo: r.Form.Get("trade_no"), TradeType: status, PaidAt: paidAt, UpdatedAt: paidAt}
	if err := markCardStoreOrderPaid(r.Context(), tenantSystem, cfg, update, mailer, identity, tenantID, externalLLMBaseURL(r)); err != nil {
		writePlainPaymentNotifyResult(w, "fail")
		return
	}
	writePlainPaymentNotifyResult(w, "success")
}

func CardStorePaymentOpenedHandler(system store.SystemSettingsRepository, mailer cardStoreMailer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		var req cardStorePaymentOpenedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := normalizeCardStoreEmail(req.Email)
		if orderNo == "" || !validCardStoreEmail(email) {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_OPENED_INVALID", "order_no and email are required")
			return
		}
		tenantID := store.NormalizeTenantID(req.TenantID)
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		cfg := loadCardStoreConfig(r.Context(), tenantSystem)
		order, approveToken, deleteToken, err := markCardStorePaymentOpened(r.Context(), tenantSystem, cfg, orderNo, email, externalBaseURL(r), mailer)
		_ = approveToken
		_ = deleteToken
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PAYMENT_OPENED_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(tenantID, email, order))
	}
}

func AdminApproveCardStorePersonalOrderHandler(system store.SystemSettingsRepository, mailer cardStoreMailer, identity *auth.IdentityService, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		if orderNo == "" {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_NOT_FOUND", "order not found")
			return
		}
		var req cardStoreManualReviewRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		settings := scopedSystemSettingsForRequest(r, system)
		cfg := loadCardStoreConfig(r.Context(), settings)
		update := cardStoreOrder{OrderNo: orderNo, Amount: roundMoney(req.Amount), Status: "paid", PaymentMsg: "personal payment confirmed", ChannelOrderNo: strings.TrimSpace(req.ChannelOrderNo), PayTime: strings.TrimSpace(req.PayTime), PayType: cardStorePaymentModeManual, PaidAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), ReviewedBy: adminAuditUserID(r), ReviewedAt: time.Now().UTC(), ReviewNote: strings.TrimSpace(req.Note)}
		if update.Amount <= 0 {
			if order, ok := findCardStoreOrder(r.Context(), settings, orderNo); ok {
				update.Amount = order.Amount
			}
		}
		if err := approveCardStorePersonalOrder(r.Context(), settings, cfg, update, mailer, identity, RequestTenantID(r), externalLLMBaseURL(r)); err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PERSONAL_APPROVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "card_store.personal_payment.approve", map[string]any{"order_no": orderNo, "amount": update.Amount})
		order, _ := findCardStoreOrder(r.Context(), settings, orderNo)
		writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(RequestTenantID(r), order.Email, order))
	}
}

func AdminCompleteCardStoreOrderHandler(system store.SystemSettingsRepository, mailer cardStoreMailer, identity *auth.IdentityService, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		var req cardStoreManualReviewRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		settings := scopedSystemSettingsForRequest(r, system)
		cfg := loadCardStoreConfig(r.Context(), settings)
		order, ok := findCardStoreOrder(r.Context(), settings, orderNo)
		if !ok {
			writeError(w, http.StatusNotFound, "CARD_STORE_ORDER_NOT_FOUND", "order not found")
			return
		}
		if isCardStorePaidLikeStatus(order.Status) {
			writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(RequestTenantID(r), order.Email, order))
			return
		}
		if !canCompleteCardStoreOrder(order.Status) {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_COMPLETE_FAILED", "order cannot be completed")
			return
		}
		amount := roundMoney(req.Amount)
		if amount <= 0 {
			amount = order.Amount
		}
		update := cardStoreOrder{OrderNo: orderNo, Amount: amount, Status: "paid", PaymentMsg: "admin completed paid order", ChannelOrderNo: strings.TrimSpace(req.ChannelOrderNo), PayTime: strings.TrimSpace(req.PayTime), PayType: firstNonEmptyString(order.PayType, order.PaymentMode), PaidAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), ReviewedBy: adminAuditUserID(r), ReviewedAt: time.Now().UTC(), ReviewNote: strings.TrimSpace(req.Note)}
		if err := markCardStoreOrderPaid(r.Context(), settings, cfg, update, mailer, identity, RequestTenantID(r), externalLLMBaseURL(r)); err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_COMPLETE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "card_store.order.complete", map[string]any{"order_no": orderNo, "amount": amount, "payment_mode": order.PaymentMode})
		order, ok = findCardStoreOrder(r.Context(), settings, orderNo)
		if !ok {
			writeError(w, http.StatusInternalServerError, "CARD_STORE_ORDER_COMPLETE_FAILED", "order was completed but could not be reloaded")
			return
		}
		writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(RequestTenantID(r), order.Email, order))
	}
}

func AdminDeleteCardStoreOrderHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		if orderNo == "" {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_DELETE_FAILED", "order not found")
			return
		}
		settings := scopedSystemSettingsForRequest(r, system)
		order, err := deleteCardStoreOrder(r.Context(), settings, orderNo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_DELETE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "card_store.order.delete", map[string]any{"order_no": orderNo, "status": order.Status})
		writeJSON(w, http.StatusOK, map[string]any{"order_no": orderNo, "deleted": true})
	}
}

func AdminRejectCardStorePersonalOrderHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		var req cardStoreManualReviewRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		settings := scopedSystemSettingsForRequest(r, system)
		order, err := rejectCardStorePersonalOrder(r.Context(), settings, orderNo, adminAuditUserID(r), strings.TrimSpace(req.Note))
		if err != nil {
			writeError(w, http.StatusBadRequest, "CARD_STORE_PERSONAL_REJECT_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "card_store.personal_payment.reject", map[string]any{"order_no": orderNo})
		writeJSON(w, http.StatusOK, cardStoreOrderCreateResponse(RequestTenantID(r), order.Email, order))
	}
}

func CardStorePersonalPaymentConfirmPageHandler(system store.SystemSettingsRepository, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.URL.Query().Get("order_no"))
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		tenantID := store.NormalizeTenantID(r.URL.Query().Get("tenant_id"))
		order, ok := findCardStoreOrder(r.Context(), scopedSystemSettingsForTenant(tenantID, system), orderNo)
		if !ok || token == "" || !cardStoreReviewTokenMatches(order, action, token) {
			http.Error(w, "invalid or expired confirmation link", http.StatusForbidden)
			return
		}
		button := "Confirm received payment and issue card"
		endpoint := "/api/card-store/personal-payment/confirm"
		if action == "reject" {
			button = "Reject/delete this order"
			endpoint = "/api/card-store/personal-payment/reject"
		}
		html := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>Card Store Payment Review</title><style>:root{color-scheme:light}*{box-sizing:border-box}html,body{margin:0;background:#f6f8fb!important;color:#172033!important;font-family:Segoe UI,Arial,sans-serif}body{padding:28px 14px}main{width:min(720px,100%%);margin:auto}h1{margin:0 0 18px;color:#111827!important;font-size:30px;line-height:1.15}.box{border:1px solid #d8e0ea;border-radius:16px;padding:22px;background:#fff!important;box-shadow:0 16px 42px rgba(24,34,49,.10)}dl{display:grid;grid-template-columns:108px minmax(0,1fr);gap:12px 18px;margin:0 0 18px}dt{font-weight:800;color:#526174!important}dd{margin:0;color:#172033!important;font-weight:700;word-break:break-word}.amount{font-size:22px;color:#0f8f68!important}.remark{display:inline-block;padding:4px 8px;border-radius:8px;background:#fff7ed;color:#9a3412!important;letter-spacing:.04em}.hint{margin:0 0 14px;color:#334155!important;line-height:1.55}button{width:100%%;min-height:50px;border:0;border-radius:10px;background:#0f8f68;color:#fff!important;font-size:18px;font-weight:900;padding:0 16px;cursor:pointer}textarea{width:100%%;min-height:112px;margin:0 0 14px;border:1px solid #cbd5e1;border-radius:10px;background:#fff!important;color:#172033!important;padding:12px;font:inherit;resize:vertical}textarea::placeholder{color:#64748b}@media(max-width:560px){body{padding:20px 12px}h1{font-size:26px}.box{padding:18px}dl{grid-template-columns:1fr;gap:4px}dd{margin:0 0 10px}button{font-size:16px}}</style></head><body><main><h1>Payment Review</h1><div class="box"><dl><dt>Order</dt><dd>%s</dd><dt>Product</dt><dd>%s</dd><dt>Buyer</dt><dd>%s</dd><dt>Channel</dt><dd>%s</dd><dt>Amount</dt><dd class="amount">%s</dd><dt>Remark</dt><dd><strong class="remark">%s</strong></dd><dt>Payee</dt><dd>%s</dd></dl><p class="hint">Check the payment record before continuing. The next button changes order state.</p><form method="post" action="%s"><input type="hidden" name="tenant_id" value="%s"><input type="hidden" name="order_no" value="%s"><input type="hidden" name="token" value="%s"><textarea name="note" placeholder="Optional note"></textarea><button type="submit">%s</button></form></div></main></body></html>`, escHTML(order.OrderNo), escHTML(order.ProductLabel), escHTML(order.Email), escHTML(order.PayChannelLabel), escHTML(formatPaymentAmount(order.Amount)), escHTML(order.PayCode), escHTML(order.Payee), endpoint, escHTML(tenantID), escHTML(order.OrderNo), escHTML(token), escHTML(button))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}
}

func CardStorePersonalPaymentTokenActionHandler(system store.SystemSettingsRepository, mailer cardStoreMailer, identity *auth.IdentityService, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		tenantID := store.NormalizeTenantID(r.Form.Get("tenant_id"))
		orderNo := strings.TrimSpace(r.Form.Get("order_no"))
		token := strings.TrimSpace(r.Form.Get("token"))
		note := strings.TrimSpace(r.Form.Get("note"))
		settings := scopedSystemSettingsForTenant(tenantID, system)
		order, ok := findCardStoreOrder(r.Context(), settings, orderNo)
		if !ok || token == "" || !cardStoreReviewTokenMatches(order, action, token) {
			http.Error(w, "invalid or expired confirmation link", http.StatusForbidden)
			return
		}
		if action == "reject" {
			if _, err := rejectCardStorePersonalOrder(r.Context(), settings, orderNo, "email-token", note); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>Order rejected</title><style>:root{color-scheme:light}body{margin:0;padding:32px 14px;background:#f6f8fb!important;color:#172033!important;font-family:Segoe UI,Arial,sans-serif}.box{max-width:560px;margin:auto;padding:24px;border:1px solid #d8e0ea;border-radius:16px;background:#fff!important;box-shadow:0 16px 42px rgba(24,34,49,.10)}h1{margin:0 0 8px;color:#111827!important}</style></head><body><main class="box"><h1>Order rejected.</h1><p>This one-time link is now used.</p></main></body></html>`))
			return
		}
		cfg := loadCardStoreConfig(r.Context(), settings)
		update := cardStoreOrder{OrderNo: orderNo, Amount: order.Amount, Status: "paid", PaymentMsg: "personal payment confirmed by email token", PayType: cardStorePaymentModeManual, PaidAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), ReviewedBy: "email-token", ReviewedAt: time.Now().UTC(), ReviewNote: note}
		if err := approveCardStorePersonalOrder(r.Context(), settings, cfg, update, mailer, identity, tenantID, externalLLMBaseURL(r)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>Payment confirmed</title><style>:root{color-scheme:light}body{margin:0;padding:32px 14px;background:#f6f8fb!important;color:#172033!important;font-family:Segoe UI,Arial,sans-serif}.box{max-width:560px;margin:auto;padding:24px;border:1px solid #d8e0ea;border-radius:16px;background:#fff!important;box-shadow:0 16px 42px rgba(24,34,49,.10)}h1{margin:0 0 8px;color:#0f8f68!important}</style></head><body><main class="box"><h1>Payment confirmed and card issued.</h1><p>This one-time link is now used.</p></main></body></html>`))
	}
}

func loadCardStoreConfig(ctx context.Context, system store.SystemSettingsRepository) cardStoreConfig {
	if system == nil {
		return normalizeCardStoreConfig(cardStoreConfig{})
	}
	raw, err := system.Get(ctx, cardStoreConfigKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return normalizeCardStoreConfig(cardStoreConfig{})
	}
	var cfg cardStoreConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return normalizeCardStoreConfig(cardStoreConfig{})
	}
	return normalizeCardStoreConfig(cfg)
}

func normalizeCardStoreConfig(cfg cardStoreConfig) cardStoreConfig {
	cfg.PaymentMode = normalizeCardStorePaymentMode(cfg.PaymentMode)
	if cfg.PaymentMode == "" {
		cfg.PaymentMode = cardStorePaymentModeFM
	}
	cfg.PaymentMethods = normalizeCardStorePaymentMethods(cfg.PaymentMethods, cfg.PaymentMode)
	cfg.PaymentMode = cfg.PaymentMethods[0]
	cfg.PaymentAPIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PaymentAPIBaseURL), "/")
	if cfg.PaymentAPIBaseURL == "" {
		cfg.PaymentAPIBaseURL = defaultPaymentFMAPIBaseURL
	}
	cfg.MerchantNum = strings.TrimSpace(cfg.MerchantNum)
	if cfg.MerchantNum == "" {
		cfg.MerchantNum = defaultPaymentFMMerchantNo
	}
	cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
	if cfg.AccessKey == "" {
		cfg.AccessKey = defaultPaymentFMAccessKey
	}
	cfg.PayType = strings.TrimSpace(cfg.PayType)
	if cfg.PayType == "" {
		cfg.PayType = "aloop"
	}
	cfg.NotifyURL = strings.TrimSpace(cfg.NotifyURL)
	cfg.PersonalPayment = normalizeCardStorePersonalPayment(cfg.PersonalPayment)
	cfg.AlipayDirect = normalizeCardStoreAlipayDirect(cfg.AlipayDirect)
	cfg.ServiceGroupIDs = normalizeStringSlice(cfg.ServiceGroupIDs)
	byID := map[string]cardStoreProduct{}
	for _, product := range cfg.Products {
		product.ID = strings.TrimSpace(product.ID)
		if product.ID == "" {
			continue
		}
		product.Kind = normalizeCardStoreProductKind(product.Kind)
		product.Label = strings.TrimSpace(product.Label)
		product.Description = strings.TrimSpace(product.Description)
		product.ServiceGroupIDs = normalizeStringSlice(product.ServiceGroupIDs)
		product.Price = roundMoney(product.Price)
		byID[product.ID] = product
	}
	products := make([]cardStoreProduct, 0, len(cardStoreDefaultProductSpecs))
	for _, spec := range cardStoreDefaultProductSpecs {
		product, ok := byID[spec.ID]
		if !ok {
			product = cardStoreProduct{ID: spec.ID, Kind: spec.Kind, Label: spec.Label, Description: spec.Description, Enabled: true, Price: spec.Price, DurationDays: spec.DurationDays, Credits: spec.Credits, PeriodLimits: spec.PeriodLimits}
		}
		if product.Kind == "" {
			product.Kind = spec.Kind
		}
		if spec.ID == "service_test_10" {
			product.Kind = spec.Kind
			product.DurationDays = spec.DurationDays
			product.PeriodLimits = llmservice.CreditPeriodLimits{}
		}
		if product.Label == "" {
			product.Label = spec.Label
		}
		if product.Description == "" {
			product.Description = spec.Description
		}
		if product.DurationDays <= 0 {
			product.DurationDays = spec.DurationDays
		}
		if product.Credits <= 0 {
			product.Credits = spec.Credits
		}
		if spec.ID == "service_test_10" && product.Credits == 10 {
			product.Credits = spec.Credits
		}
		if product.PeriodLimits == (llmservice.CreditPeriodLimits{}) {
			product.PeriodLimits = spec.PeriodLimits
		}
		product.PeriodLimits = sanitizeLLMServiceCardPeriodLimits(product.DurationDays, product.PeriodLimits)
		products = append(products, product)
	}
	cfg.Products = products
	return cfg
}

func normalizeCardStorePersonalPayment(cfg cardStorePersonalPaymentConfig) cardStorePersonalPaymentConfig {
	cfg.AdminEmails = normalizeCardStoreEmailSlice(cfg.AdminEmails)
	cfg.Instruction = strings.TrimSpace(cfg.Instruction)
	channels := make([]cardStorePersonalPaymentChannel, 0, len(cfg.Channels))
	seen := map[string]struct{}{}
	for _, ch := range cfg.Channels {
		ch.ID = normalizeCardStorePaymentChannel(ch.ID)
		if ch.ID == "" {
			continue
		}
		if _, ok := seen[ch.ID]; ok {
			continue
		}
		seen[ch.ID] = struct{}{}
		ch.Label = strings.TrimSpace(ch.Label)
		if ch.Label == "" {
			ch.Label = defaultCardStorePaymentChannelLabel(ch.ID)
		}
		ch.Payee = strings.TrimSpace(ch.Payee)
		ch.ImageURL = strings.TrimSpace(ch.ImageURL)
		ch.AlipayUserID = strings.TrimSpace(ch.AlipayUserID)
		ch.DeepLinkMode = strings.ToLower(strings.TrimSpace(ch.DeepLinkMode))
		if ch.DeepLinkMode == "" {
			ch.DeepLinkMode = "to_account"
		}
		channels = append(channels, ch)
	}
	if len(channels) == 0 {
		channels = []cardStorePersonalPaymentChannel{
			{ID: "alipay", Label: "Alipay", Enabled: true, DeepLinkMode: "to_account"},
			{ID: "wechat", Label: "WeChat Pay", Enabled: false},
		}
	}
	cfg.Channels = channels
	return cfg
}

func normalizeCardStoreAlipayDirect(cfg cardStoreAlipayDirectConfig) cardStoreAlipayDirectConfig {
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.PrivateKey = strings.TrimSpace(cfg.PrivateKey)
	cfg.AlipayPublicKey = strings.TrimSpace(cfg.AlipayPublicKey)
	cfg.GatewayURL = strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/")
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = defaultAlipayGatewayURL
	}
	cfg.ProductCode = "FAST_INSTANT_TRADE_PAY"
	cfg.SubjectPrefix = strings.TrimSpace(cfg.SubjectPrefix)
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "MaClaw Hub"
	}
	cfg.ReturnURL = strings.TrimSpace(cfg.ReturnURL)
	cfg.NotifyURL = strings.TrimSpace(cfg.NotifyURL)
	cfg.SignType = "RSA2"
	cfg.PaymentMethod = "page"
	return cfg
}

func validateCardStoreConfigForSave(cfg cardStoreConfig) error {
	for _, method := range normalizeCardStorePaymentMethods(cfg.PaymentMethods, cfg.PaymentMode) {
		if err := validateCardStorePaymentMethodForSave(cfg, method); err != nil {
			return err
		}
	}
	return nil
}

func validateCardStorePaymentMethodForSave(cfg cardStoreConfig, method string) error {
	method = normalizeCardStorePaymentMode(method)
	if method == cardStorePaymentModeManual {
		if len(cfg.PersonalPayment.AdminEmails) == 0 {
			return fmt.Errorf("personal payment store owner email is required")
		}
		hasEnabledChannel := false
		for _, channel := range cfg.PersonalPayment.Channels {
			if !channel.Enabled {
				continue
			}
			hasEnabledChannel = true
			if strings.TrimSpace(channel.ImageURL) == "" {
				return fmt.Errorf("payment QR image is required for channel: %s", channel.ID)
			}
		}
		if !hasEnabledChannel {
			return fmt.Errorf("no personal payment channel is enabled")
		}
		return nil
	}
	if method != cardStorePaymentModeAlipay {
		return nil
	}
	if strings.TrimSpace(cfg.AlipayDirect.AppID) == "" {
		return fmt.Errorf("alipay app_id is required")
	}
	if parsed, err := url.Parse(strings.TrimSpace(cfg.AlipayDirect.GatewayURL)); err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("alipay gateway_url must be an absolute http or https URL")
	}
	if _, err := parseAlipayRSAPrivateKey(cfg.AlipayDirect.PrivateKey); err != nil {
		return fmt.Errorf("alipay app private key is invalid: %w", err)
	}
	if _, err := parseAlipayRSAPublicKey(cfg.AlipayDirect.AlipayPublicKey); err != nil {
		return fmt.Errorf("alipay public key is invalid: %w", err)
	}
	return nil
}

func cardStoreAlipayPayURL(orderNo string, tenantID string) string {
	path := "/api/card-store/orders/" + url.PathEscape(orderNo) + "/alipay/pay"
	if store.NormalizeTenantID(tenantID) != store.DefaultTenantID {
		return path + "?tenant_id=" + url.QueryEscape(store.NormalizeTenantID(tenantID))
	}
	return path
}

func normalizeCardStorePaymentChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "alipay", "wechat", "qq", "unionpay":
		return strings.ToLower(strings.TrimSpace(channel))
	default:
		return ""
	}
}

func defaultCardStorePaymentChannelLabel(channel string) string {
	switch normalizeCardStorePaymentChannel(channel) {
	case "alipay":
		return "Alipay"
	case "wechat":
		return "WeChat Pay"
	case "qq":
		return "QQ Pay"
	case "unionpay":
		return "UnionPay"
	default:
		return strings.TrimSpace(channel)
	}
}

func normalizeCardStoreEmailSlice(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		email := normalizeCardStoreEmail(value)
		if !validCardStoreEmail(email) {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

func publicCardStoreProduct(product cardStoreProduct) cardStoreProduct {
	product.ServiceGroupIDs = nil
	return product
}

func publicCardStorePaymentChannels(cfg cardStoreConfig) []map[string]string {
	channels := make([]map[string]string, 0, len(cfg.PaymentMethods)+len(cfg.PersonalPayment.Channels))
	for _, method := range cfg.PaymentMethods {
		mode, ok := parseCardStorePaymentMode(method)
		if !ok {
			continue
		}
		switch mode {
		case cardStorePaymentModeFM:
			channels = append(channels, map[string]string{"id": cardStorePaymentModeFM, "label": "Payment FM"})
		case cardStorePaymentModeAlipay:
			channels = append(channels, map[string]string{"id": cardStorePaymentModeAlipay, "label": "Alipay direct"})
		}
	}
	if !cardStorePaymentMethodEnabled(cfg, cardStorePaymentModeManual) {
		return channels
	}
	for _, channel := range cfg.PersonalPayment.Channels {
		if !channel.Enabled || strings.TrimSpace(channel.ImageURL) == "" {
			continue
		}
		channels = append(channels, map[string]string{"id": "manual:" + channel.ID, "label": strings.TrimSpace(channel.Label + " QR")})
	}
	return channels
}

func normalizeCardStoreProductKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "credits":
		return "credits"
	default:
		return "service_card"
	}
}

func findCardStoreProduct(cfg cardStoreConfig, id string) (cardStoreProduct, bool) {
	id = strings.TrimSpace(id)
	for _, product := range cfg.Products {
		if product.ID == id {
			return product, true
		}
	}
	return cardStoreProduct{}, false
}

func preparePersonalSemimanualOrder(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, order cardStoreOrder, requestedChannel string) (cardStoreOrder, error) {
	if len(cfg.PersonalPayment.AdminEmails) == 0 {
		return order, fmt.Errorf("personal payment store owner email is required")
	}
	channel, err := selectCardStorePersonalChannel(cfg.PersonalPayment, requestedChannel)
	if err != nil {
		return order, err
	}
	if strings.TrimSpace(channel.ImageURL) == "" {
		return order, fmt.Errorf("payment QR image is required for channel: %s", channel.ID)
	}
	payCode, err := newCardStorePayCode(ctx, system)
	if err != nil {
		return order, err
	}
	order.PaymentMode = cardStorePaymentModeManual
	order.Status = cardStoreStatusPersonalCreated
	order.PayType = channel.ID
	order.PayChannel = channel.ID
	order.PayChannelLabel = channel.Label
	order.Payee = channel.Payee
	order.PayCode = payCode
	order.PayQRURL = channel.ImageURL
	order.PayDeepLink = buildPersonalPaymentDeepLink(channel, order.Amount, payCode)
	order.PayInstruction = buildPersonalPaymentInstruction(cfg.PersonalPayment, channel, order)
	order.UpdatedAt = time.Now().UTC()
	return order, nil
}

func selectCardStorePersonalChannel(cfg cardStorePersonalPaymentConfig, requested string) (cardStorePersonalPaymentChannel, error) {
	rawRequested := strings.TrimSpace(requested)
	requested = normalizeCardStorePaymentChannel(requested)
	if rawRequested != "" && requested == "" {
		return cardStorePersonalPaymentChannel{}, fmt.Errorf("payment channel is not supported: %s", rawRequested)
	}
	var first cardStorePersonalPaymentChannel
	for _, channel := range cfg.Channels {
		if !channel.Enabled {
			continue
		}
		if first.ID == "" {
			first = channel
		}
		if requested != "" && channel.ID == requested {
			return channel, nil
		}
	}
	if requested != "" {
		return cardStorePersonalPaymentChannel{}, fmt.Errorf("payment channel is not enabled: %s", requested)
	}
	if first.ID == "" {
		return cardStorePersonalPaymentChannel{}, fmt.Errorf("no personal payment channel is enabled")
	}
	return first, nil
}

func newCardStorePayCode(ctx context.Context, system store.SystemSettingsRepository) (string, error) {
	for i := 0; i < 20; i++ {
		buf := make([]byte, 3)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		value := (int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])) % 1000000
		code := fmt.Sprintf("CS%06d", value)
		if !cardStorePayCodeExists(ctx, system, code) {
			return code, nil
		}
	}
	return "", fmt.Errorf("could not generate unique pay code")
}

func cardStorePayCodeExists(ctx context.Context, system store.SystemSettingsRepository, code string) bool {
	orders := loadCardStoreOrders(ctx, system)
	for _, order := range orders.Orders {
		if strings.EqualFold(strings.TrimSpace(order.PayCode), strings.TrimSpace(code)) && !isCardStorePaidLikeStatus(order.Status) && order.Status != cardStoreStatusPersonalRejected {
			return true
		}
	}
	return false
}

func cardStoreTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func escHTML(value string) string { return html.EscapeString(value) }

func newCardStoreReviewToken() (string, string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	return token, cardStoreTokenHash(token), nil
}

func cardStoreReviewTokenMatches(order cardStoreOrder, action string, token string) bool {
	hash := cardStoreTokenHash(token)
	if action == "reject" {
		return hash != "" && strings.EqualFold(hash, strings.TrimSpace(order.AdminDeleteTokenHash))
	}
	return hash != "" && strings.EqualFold(hash, strings.TrimSpace(order.AdminApproveTokenHash))
}

func buildPersonalPaymentDeepLink(channel cardStorePersonalPaymentChannel, amount float64, payCode string) string {
	if channel.ID != "alipay" || strings.TrimSpace(channel.AlipayUserID) == "" {
		return ""
	}
	amountText := url.QueryEscape(formatPaymentAmount(amount))
	codeText := url.QueryEscape(payCode)
	userID := url.QueryEscape(strings.TrimSpace(channel.AlipayUserID))
	if channel.DeepLinkMode == "scan" {
		biz := fmt.Sprintf(`{"s":"money","u":"%s","a":"%s","m":"%s"}`, strings.TrimSpace(channel.AlipayUserID), formatPaymentAmount(amount), payCode)
		return "alipays://platformapi/startapp?appId=20000123&actionType=scan&biz_data=" + url.QueryEscape(biz)
	}
	return "alipays://platformapi/startapp?appId=09999988&actionType=toAccount&goBack=NO&userId=" + userID + "&amount=" + amountText + "&memo=" + codeText
}

func buildPersonalPaymentInstruction(cfg cardStorePersonalPaymentConfig, channel cardStorePersonalPaymentChannel, order cardStoreOrder) string {
	parts := []string{}
	if strings.TrimSpace(cfg.Instruction) != "" {
		parts = append(parts, strings.TrimSpace(cfg.Instruction))
	}
	parts = append(parts, fmt.Sprintf("Use %s to pay %s and include remark %s.", channel.Label, formatPaymentAmount(order.Amount), order.PayCode))
	if channel.Payee != "" {
		parts = append(parts, "Payee: "+channel.Payee)
	}
	return strings.Join(parts, "\n")
}

func startPaymentFMOrder(ctx context.Context, client *http.Client, cfg cardStoreConfig, orderNo, amount, notifyURL string) (*paymentFMStartOrderResponse, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.PaymentAPIBaseURL), "/") + "/startOrder"
	merchantNum := strings.TrimSpace(cfg.MerchantNum)
	sign := paymentFMSign(merchantNum, orderNo, amount, notifyURL, cfg.AccessKey)
	values := url.Values{}
	values.Set("merchantNum", merchantNum)
	values.Set("orderNo", orderNo)
	values.Set("amount", amount)
	values.Set("notifyUrl", notifyURL)
	values.Set("payType", strings.TrimSpace(cfg.PayType))
	values.Set("returnType", "json")
	values.Set("sign", sign)
	endpoint = endpoint + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload paymentFMStartOrderResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("payment response parse failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !payload.Success {
		msg := strings.TrimSpace(payload.Msg)
		if msg == "" {
			msg = fmt.Sprintf("payment request failed with status %d", resp.StatusCode)
		}
		return &payload, fmt.Errorf("%s (merchantNum=%s, apiBase=%s)", msg, merchantNum, strings.TrimRight(strings.TrimSpace(cfg.PaymentAPIBaseURL), "/"))
	}
	if payload.Data == nil || strings.TrimSpace(payload.Data.PayURL) == "" {
		return &payload, fmt.Errorf("payment response missing payUrl")
	}
	return &payload, nil
}

func buildAlipayDirectRequest(cfg cardStoreAlipayDirectConfig, order cardStoreOrder, notifyURL string, returnURL string) (url.Values, error) {
	cfg = normalizeCardStoreAlipayDirect(cfg)
	method := "alipay.trade.page.pay"
	productCode := cfg.ProductCode
	biz, err := json.Marshal(map[string]string{
		"out_trade_no": order.OrderNo,
		"product_code": productCode,
		"total_amount": formatPaymentAmount(order.Amount),
		"subject":      strings.TrimSpace(cfg.SubjectPrefix + " " + order.ProductLabel),
		"body":         order.Email,
	})
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("app_id", cfg.AppID)
	values.Set("method", method)
	values.Set("format", "JSON")
	values.Set("charset", "utf-8")
	values.Set("sign_type", cfg.SignType)
	values.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	values.Set("version", "1.0")
	values.Set("notify_url", notifyURL)
	if strings.TrimSpace(returnURL) != "" {
		values.Set("return_url", returnURL)
	}
	values.Set("biz_content", string(biz))
	sign, err := alipayRSA2Sign(alipaySignContent(values), cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	values.Set("sign", sign)
	return values, nil
}

func renderAlipaySubmitPage(gateway string, values url.Values) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Alipay</title></head><body><form id="pay" method="post" accept-charset="utf-8" action="`)
	b.WriteString(escHTML(alipayGatewayWithCharset(gateway, values.Get("charset"))))
	b.WriteString(`">`)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString(`<input type="hidden" name="`)
		b.WriteString(escHTML(key))
		b.WriteString(`" value="`)
		b.WriteString(escHTML(values.Get(key)))
		b.WriteString(`">`)
	}
	b.WriteString(`<noscript><button type="submit">Continue to Alipay</button></noscript></form><script>document.getElementById('pay').submit();</script></body></html>`)
	return b.String()
}

func alipayDirectReturnURL(r *http.Request, order cardStoreOrder, tenantID string) string {
	return cardStoreAlipayReturnURL(strings.TrimRight(externalBaseURL(r), "/"), order.OrderNo, tenantID)
}

func cardStoreAlipayReturnURL(baseURL string, orderNo string, tenantID string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path := "/api/card-store/orders/" + url.PathEscape(orderNo) + "/alipay/return"
	if baseURL != "" {
		path = baseURL + path
	}
	if store.NormalizeTenantID(tenantID) != store.DefaultTenantID {
		path += "?tenant_id=" + url.QueryEscape(store.NormalizeTenantID(tenantID))
	}
	return path
}

func cardStoreOrderReturnRedirectURL(r *http.Request, tenantID string, order cardStoreOrder, source string) string {
	base := strings.TrimRight(externalBaseURL(r), "/") + "/card_store"
	qs := url.Values{}
	if store.NormalizeTenantID(tenantID) != store.DefaultTenantID {
		qs.Set("tenant_id", store.NormalizeTenantID(tenantID))
	}
	if email := normalizeCardStoreEmail(order.Email); email != "" {
		qs.Set("email", email)
	}
	if orderNo := strings.TrimSpace(order.OrderNo); orderNo != "" {
		qs.Set("order_no", orderNo)
	}
	if source != "" {
		qs.Set("payment_return", source)
	}
	if query := qs.Encode(); query != "" {
		base += "?" + query
	}
	return base
}

func isAlipayPaidTradeStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		return true
	default:
		return false
	}
}

func alipayGatewayWithCharset(gateway string, charset string) string {
	gateway = strings.TrimSpace(gateway)
	charset = strings.TrimSpace(charset)
	if charset == "" {
		charset = "utf-8"
	}
	parsed, err := url.Parse(gateway)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		sep := "?"
		if strings.Contains(gateway, "?") {
			sep = "&"
		}
		return gateway + sep + "charset=" + url.QueryEscape(charset)
	}
	query := parsed.Query()
	query.Set("charset", charset)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func verifyAlipayDirectNotify(values url.Values, publicKey string) bool {
	sign := strings.TrimSpace(values.Get("sign"))
	if sign == "" || strings.TrimSpace(publicKey) == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false
	}
	pub, err := parseAlipayRSAPublicKey(publicKey)
	if err != nil {
		return false
	}
	for _, content := range alipayVerifySignContents(values) {
		digest := sha256.Sum256([]byte(content))
		if rsa.VerifyPKCS1v15(pub, cryptoHashSHA256(), digest[:], decoded) == nil {
			return true
		}
	}
	return false
}

func alipayNotifySignedValues(r *http.Request) url.Values {
	source := r.Form
	if len(r.PostForm) > 0 {
		source = r.PostForm
	}
	values := make(url.Values, len(source))
	for key, vals := range source {
		if key == "tenant_id" {
			continue
		}
		values[key] = append([]string(nil), vals...)
	}
	return values
}

func alipayRSA2Sign(content string, privateKey string) (string, error) {
	key, err := parseAlipayRSAPrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, cryptoHashSHA256(), digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func alipaySignContent(values url.Values) string {
	return alipaySignContentSkipping(values, map[string]bool{"sign": true})
}

func alipayVerifySignContents(values url.Values) []string {
	primary := alipaySignContentSkipping(values, map[string]bool{"sign": true, "sign_type": true})
	legacy := alipaySignContent(values)
	if primary == legacy {
		return []string{primary}
	}
	return []string{primary, legacy}
}

func alipaySignContentSkipping(values url.Values, skip map[string]bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if skip[key] {
			continue
		}
		if strings.TrimSpace(values.Get(key)) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	return strings.Join(parts, "&")
}

func parseAlipayRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, err := pemBlockFromAlipayKey(raw, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func parseAlipayRSAPublicKey(raw string) (*rsa.PublicKey, error) {
	block, err := pemBlockFromAlipayKey(raw, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaPub, ok := pub.(*rsa.PublicKey); ok {
			return rsaPub, nil
		}
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}

func pemBlockFromAlipayKey(raw string, kind string) (*pem.Block, error) {
	text := strings.TrimSpace(raw)
	if !strings.Contains(text, "-----BEGIN") {
		text = "-----BEGIN " + kind + "-----\n" + text + "\n-----END " + kind + "-----"
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("invalid RSA %s", strings.ToLower(kind))
	}
	return block, nil
}

func cryptoHashSHA256() crypto.Hash { return crypto.SHA256 }

func paymentFMSign(merchantNum, orderNo, amount, notifyURL, accessKey string) string {
	seed := strings.TrimSpace(merchantNum) + strings.TrimSpace(orderNo) + strings.TrimSpace(amount) + strings.TrimSpace(notifyURL) + strings.TrimSpace(accessKey)
	sum := md5.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func resolveCardStoreTenantForEmail(ctx context.Context, identity *auth.IdentityService, tenantID string, email string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		return store.NormalizeTenantID(tenantID), nil
	}
	if identity == nil {
		return store.DefaultTenantID, nil
	}
	resolved, found, ambiguous, err := identity.ResolveTenantByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if ambiguous {
		return "", fmt.Errorf("email belongs to multiple tenants; tenant_id is required")
	}
	if found && strings.TrimSpace(resolved) != "" {
		return store.NormalizeTenantID(resolved), nil
	}
	return store.DefaultTenantID, nil
}

func firstTenantRepository(repos []store.TenantRepository) store.TenantRepository {
	if len(repos) == 0 {
		return nil
	}
	return repos[0]
}

func cardStoreTenantIDFromRequest(r *http.Request, tenants store.TenantRepository) string {
	if r == nil {
		return store.DefaultTenantID
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
		return store.NormalizeTenantID(tenantID)
	}
	if tenants == nil {
		return store.DefaultTenantID
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	if comma := strings.Index(host, ","); comma >= 0 {
		host = strings.TrimSpace(host[:comma])
	}
	if parsed, err := url.Parse("//" + host); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	} else if colon := strings.LastIndex(host, ":"); colon > 0 {
		host = host[:colon]
	}
	host = normalizeDomain(host)
	if host == "" {
		return store.DefaultTenantID
	}
	items, err := tenants.List(r.Context())
	if err != nil {
		return store.DefaultTenantID
	}
	for _, tenant := range items {
		if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
			continue
		}
		for _, domain := range tenantEmailDomains(tenant) {
			if strings.EqualFold(host, domain) {
				return store.NormalizeTenantID(tenant.ID)
			}
		}
	}
	return store.DefaultTenantID
}

func cardStoreNotifyURLWithTenant(rawURL string, tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if strings.TrimSpace(rawURL) == "" || tenantID == store.DefaultTenantID {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		return rawURL + sep + "tenant_id=" + url.QueryEscape(tenantID)
	}
	q := parsed.Query()
	q.Set("tenant_id", tenantID)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func defaultCardStoreNotifyURL(r *http.Request) string {
	base := strings.TrimRight(externalBaseURL(r), "/")
	if base == "" {
		return "/api/zhifuxpay/notify"
	}
	return base + "/api/zhifuxpay/notify"
}

func defaultCardStoreAlipayNotifyURL(r *http.Request) string {
	base := strings.TrimRight(externalBaseURL(r), "/")
	if base == "" {
		return "/api/card-store/payment/notify"
	}
	return base + "/api/card-store/payment/notify"
}

func notifyURLMatchesCurrentHub(configured string, current string) bool {
	configured = strings.TrimSpace(configured)
	current = strings.TrimSpace(current)
	if configured == "" || current == "" {
		return false
	}
	configuredURL, errA := url.Parse(configured)
	currentURL, errB := url.Parse(current)
	if errA != nil || errB != nil || configuredURL.IsAbs() != currentURL.IsAbs() {
		return strings.EqualFold(strings.TrimRight(configured, "/"), strings.TrimRight(current, "/"))
	}
	return strings.EqualFold(configuredURL.Scheme, currentURL.Scheme) && strings.EqualFold(configuredURL.Host, currentURL.Host) && configuredURL.EscapedPath() == currentURL.EscapedPath() && configuredURL.RawQuery == currentURL.RawQuery
}

func cardStoreURLMatchesEndpointIgnoringTenantID(rawURL string, endpointPath string) bool {
	rawURL = strings.TrimSpace(rawURL)
	endpointPath = strings.TrimSpace(endpointPath)
	if rawURL == "" || endpointPath == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.EscapedPath() != endpointPath {
		return false
	}
	query := parsed.Query()
	query.Del("tenant_id")
	return query.Encode() == ""
}

func createCardStoreServiceCard(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, order cardStoreOrder) (llmservice.RechargeCard, string, error) {
	product, ok := findCardStoreProduct(cfg, order.ProductID)
	if !ok {
		return llmservice.RechargeCard{}, "", fmt.Errorf("product not found")
	}
	return createLLMServiceCardForCardStore(ctx, system, cfg, product, order)
}

// createLLMServiceCardForCardStore issues cards through the same registry path
// used by the service-card admin API, so purchased cards are ordinary
// llmservice.RechargeCard records and can be redeemed by existing clients.
func createLLMServiceCardForCardStore(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, product cardStoreProduct, order cardStoreOrder) (llmservice.RechargeCard, string, error) {
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		return llmservice.RechargeCard{}, "", err
	}
	cardID := cardStoreCardID(order.OrderNo)
	if card, _ := reg.FindCardByID(cardID); card != nil {
		return *card, llmservice.DecryptCardCode(card.EncryptedCode), nil
	}
	serviceGroupIDs := normalizeStringSlice(product.ServiceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		serviceGroupIDs = normalizeStringSlice(cfg.ServiceGroupIDs)
	}
	if len(serviceGroupIDs) == 0 {
		serviceGroupIDs = normalizeStringSlice(reg.DefaultNewUserServiceGroups)
	}
	for _, id := range serviceGroupIDs {
		if reg.FindModelServiceGroup(id) == nil {
			return llmservice.RechargeCard{}, "", fmt.Errorf("unknown service group: %s", id)
		}
	}
	existingHashes := make(map[string]struct{}, len(reg.Cards)+1)
	for _, card := range reg.Cards {
		if hash := strings.TrimSpace(card.CodeHash); hash != "" {
			existingHashes[hash] = struct{}{}
		}
	}
	for attempt := 0; attempt < 20; attempt++ {
		code, err := llmservice.GenerateCardCode()
		if err != nil {
			return llmservice.RechargeCard{}, "", err
		}
		if err := llmservice.ValidateCardCode(code); err != nil {
			return llmservice.RechargeCard{}, "", err
		}
		hash := llmserviceHashCode(code)
		if _, exists := existingHashes[hash]; exists {
			continue
		}
		enc, err := llmservice.EncryptCardCode(code)
		if err != nil {
			return llmservice.RechargeCard{}, "", err
		}
		card := llmservice.RechargeCard{ID: cardID, CodeHash: hash, EncryptedCode: enc, Label: order.ProductLabel, ServiceGroupIDs: serviceGroupIDs, DurationDays: product.DurationDays, Credits: product.Credits, PeriodLimits: product.PeriodLimits, CreatedAt: time.Now().UTC()}
		reg.Cards = append(reg.Cards, card)
		if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
			return llmservice.RechargeCard{}, "", err
		}
		invalidateLLMRuntimeCaches(system)
		return card, code, nil
	}
	return llmservice.RechargeCard{}, "", fmt.Errorf("could not generate unique card code")
}

func cardStoreActualPayAmountText(order cardStoreOrder) string {
	if strings.TrimSpace(order.ActualPayAmount) != "" {
		return strings.TrimSpace(order.ActualPayAmount)
	}
	return formatPaymentAmount(order.Amount)
}

func cardStorePayTimeText(order cardStoreOrder) string {
	if strings.TrimSpace(order.PayTime) != "" {
		return strings.TrimSpace(order.PayTime)
	}
	if !order.PaidAt.IsZero() {
		return order.PaidAt.UTC().Format(time.RFC3339)
	}
	return "-"
}

func sendCardStoreCodeEmail(ctx context.Context, mailer cardStoreMailer, order cardStoreOrder, code string) error {
	if mailer == nil || strings.TrimSpace(code) == "" {
		return nil
	}
	recipients := []string{order.Email}
	if secondary := normalizeCardStoreEmail(order.SecondaryEmail); secondary != "" && secondary != normalizeCardStoreEmail(order.Email) {
		recipients = append(recipients, secondary)
	}
	subject := "MaClaw Hub 服务兑换码"
	title := "MaClaw Hub 服务卡购买成功"
	autoRedeemLine := "兑换状态：未自动兑换，请使用下方服务兑换码手动兑换。\r\n"
	if !order.AutoRedeemedAt.IsZero() {
		subject = "MaClaw Hub 服务卡已自动兑换完成"
		title = "MaClaw Hub 服务卡已自动兑换完成"
		autoRedeemLine = fmt.Sprintf("兑换状态：已自动充值到账户\r\n兑换账户：%s\r\n兑换时间：%s\r\n", order.Email, order.AutoRedeemedAt.UTC().Format(time.RFC3339))
	}
	body := fmt.Sprintf("%s\r\n\r\n订单号：%s\r\n租户：%s\r\n购买邮箱：%s\r\n备用邮箱：%s\r\n商品：%s\r\n订单金额：%s\r\n实付金额：%s\r\n支付时间：%s\r\n支付方式：%s\r\n平台订单号：%s\r\n渠道订单号：%s\r\n服务卡 ID：%s\r\n%s服务兑换码：%s\r\n\r\n请妥善保存该兑换码，可用于后续核对或找回。\r\n", title, order.OrderNo, store.NormalizeTenantID(order.TenantID), order.Email, firstNonEmptyString(order.SecondaryEmail, "-"), order.ProductLabel, formatPaymentAmount(order.Amount), cardStoreActualPayAmountText(order), cardStorePayTimeText(order), firstNonEmptyString(order.PayChannelLabel, order.PayType, order.PaymentMode, "-"), firstNonEmptyString(order.PlatformOrderNo, "-"), firstNonEmptyString(order.ChannelOrderNo, "-"), firstNonEmptyString(order.CardID, "-"), autoRedeemLine, code)
	return mailer.Send(store.WithTenant(ctx, order.TenantID), recipients, subject, body)
}

func sendCardStorePersonalPaymentReminder(ctx context.Context, mailer cardStoreMailer, cfg cardStoreConfig, order cardStoreOrder, baseURL string, approveToken string, deleteToken string) error {
	recipients := cfg.PersonalPayment.AdminEmails
	if len(recipients) == 0 {
		return nil
	}
	subject := "MaClaw Hub personal payment pending: " + order.OrderNo
	baseURL = strings.TrimRight(baseURL, "/")
	confirmURL := baseURL + "/card_store/admin/confirm?order_no=" + url.QueryEscape(order.OrderNo)
	deleteURL := baseURL + "/card_store/admin/delete?order_no=" + url.QueryEscape(order.OrderNo)
	qrURL := absoluteCardStoreURL(baseURL, order.PayQRURL)
	if order.TenantID != "" {
		confirmURL += "&tenant_id=" + url.QueryEscape(order.TenantID)
		deleteURL += "&tenant_id=" + url.QueryEscape(order.TenantID)
	}
	if approveToken != "" {
		confirmURL += "&token=" + url.QueryEscape(approveToken)
	}
	if deleteToken != "" {
		deleteURL += "&token=" + url.QueryEscape(deleteToken)
	}
	body := fmt.Sprintf("MaClaw Hub personal payment needs confirmation\r\n\r\nOrder: %s\r\nTenant: %s\r\nProduct: %s\r\nBuyer email: %s\r\nPayment channel: %s\r\nPayee: %s\r\nAmount: %s\r\nRemark code: %s\r\nPayment QR: %s\r\nBuyer clicked paid at: %s\r\n\r\nCheck your Alipay/WeChat records before confirming:\r\n1. Amount equals %s\r\n2. Remark contains %s\r\n3. Payment time is after order creation\r\n\r\nOne-time confirm link: %s\r\nOne-time reject/delete link: %s\r\n\r\nEach link opens a review page and becomes invalid after confirm or reject succeeds.\r\n", order.OrderNo, order.TenantID, order.ProductLabel, order.Email, order.PayChannelLabel, order.Payee, formatPaymentAmount(order.Amount), order.PayCode, qrURL, order.OpenedPaymentAt.UTC().Format(time.RFC3339), formatPaymentAmount(order.Amount), order.PayCode, confirmURL, deleteURL)
	return mailer.Send(store.WithTenant(ctx, order.TenantID), recipients, subject, body)
}

func absoluteCardStoreURL(baseURL, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if parsed, err := url.Parse(rawURL); err == nil && parsed.IsAbs() {
		return rawURL
	}
	if strings.TrimSpace(baseURL) == "" || !strings.HasPrefix(rawURL, "/") {
		return rawURL
	}
	return strings.TrimRight(baseURL, "/") + rawURL
}

func autoRedeemCardStoreOrder(ctx context.Context, identity *auth.IdentityService, system store.SystemSettingsRepository, tenantID string, order cardStoreOrder, code string, hubBaseURL string) (bool, error) {
	if identity == nil || identity.UsersRepo() == nil || strings.TrimSpace(code) == "" || strings.TrimSpace(order.Email) == "" {
		return false, nil
	}
	tenantID = store.NormalizeTenantID(firstNonEmptyString(tenantID, order.TenantID))
	user, err := identity.UsersRepo().GetByTenantEmail(ctx, tenantID, order.Email)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	if strings.TrimSpace(hubBaseURL) == "" {
		hubBaseURL = "/api/llm/v1"
	}
	if _, err := llmservice.RedeemCard(store.WithTenant(ctx, tenantID), system, nil, order.Email, code, hubBaseURL); err != nil {
		if reg, loadErr := llmservice.LoadRegistry(ctx, system); loadErr == nil {
			if card, _ := reg.FindCardByID(order.CardID); card != nil && strings.EqualFold(normalizeCardStoreEmail(card.RedeemedByEmail), normalizeCardStoreEmail(order.Email)) {
				return true, nil
			}
		}
		return false, err
	}
	return true, nil
}

func normalizeCardStoreEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func cardStoreCardID(orderNo string) string {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return llmservice.NewID("card")
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", "?", "_", "&", "_", "=", "_", "#", "_", ":", "_", ";", "_")
	return "cardstore_" + replacer.Replace(orderNo)
}

func validCardStoreEmail(email string) bool {
	email = normalizeCardStoreEmail(email)
	if len(email) < 6 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-3 && strings.Contains(email[at+1:], ".")
}

func cardStoreOrderEmailMatches(order cardStoreOrder, email string) bool {
	email = normalizeCardStoreEmail(email)
	return email != "" && (normalizeCardStoreEmail(order.Email) == email || normalizeCardStoreEmail(order.SecondaryEmail) == email)
}

func loadCardStoreOrders(ctx context.Context, system store.SystemSettingsRepository) cardStoreOrders {
	if system == nil {
		return cardStoreOrders{}
	}
	raw, err := system.Get(ctx, cardStoreOrdersKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cardStoreOrders{}
	}
	var orders cardStoreOrders
	if err := json.Unmarshal([]byte(raw), &orders); err != nil {
		return cardStoreOrders{}
	}
	return orders
}

func appendCardStoreOrder(ctx context.Context, system store.SystemSettingsRepository, order cardStoreOrder) error {
	orders := loadCardStoreOrders(ctx, system)
	orders.Orders = append(orders.Orders, order)
	return saveCardStoreOrders(ctx, system, orders)
}

func saveCardStoreOrder(ctx context.Context, system store.SystemSettingsRepository, order cardStoreOrder) error {
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) == strings.TrimSpace(order.OrderNo) {
			orders.Orders[i] = order
			return saveCardStoreOrders(ctx, system, orders)
		}
	}
	orders.Orders = append(orders.Orders, order)
	return saveCardStoreOrders(ctx, system, orders)
}

func findCardStoreOrder(ctx context.Context, system store.SystemSettingsRepository, orderNo string) (cardStoreOrder, bool) {
	orders := loadCardStoreOrders(ctx, system)
	for _, order := range orders.Orders {
		if strings.TrimSpace(order.OrderNo) == strings.TrimSpace(orderNo) {
			return order, true
		}
	}
	return cardStoreOrder{}, false
}

func saveCardStorePaymentStarted(ctx context.Context, system store.SystemSettingsRepository, order cardStoreOrder) error {
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(order.OrderNo) {
			continue
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			if strings.TrimSpace(orders.Orders[i].PaymentID) == "" {
				orders.Orders[i].PaymentID = order.PaymentID
			}
			if strings.TrimSpace(orders.Orders[i].PayURL) == "" {
				orders.Orders[i].PayURL = order.PayURL
			}
			return saveCardStoreOrders(ctx, system, orders)
		}
		orders.Orders[i] = order
		return saveCardStoreOrders(ctx, system, orders)
	}
	orders.Orders = append(orders.Orders, order)
	return saveCardStoreOrders(ctx, system, orders)
}

func saveCardStorePaymentFailed(ctx context.Context, system store.SystemSettingsRepository, order cardStoreOrder) error {
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(order.OrderNo) {
			continue
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			if strings.TrimSpace(orders.Orders[i].PaymentMsg) == "" {
				orders.Orders[i].PaymentMsg = order.PaymentMsg
			}
			return saveCardStoreOrders(ctx, system, orders)
		}
		orders.Orders[i] = order
		return saveCardStoreOrders(ctx, system, orders)
	}
	orders.Orders = append(orders.Orders, order)
	return saveCardStoreOrders(ctx, system, orders)
}

func updateCardStoreOrderPaymentMessage(ctx context.Context, system store.SystemSettingsRepository, orderNo string, message string) error {
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(orderNo) {
			continue
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			return nil
		}
		orders.Orders[i].PaymentMsg = strings.TrimSpace(message)
		orders.Orders[i].UpdatedAt = time.Now().UTC()
		return saveCardStoreOrders(ctx, system, orders)
	}
	return fmt.Errorf("order not found")
}

func markCardStorePaymentOpened(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, orderNo, email, baseURL string, mailer cardStoreMailer) (cardStoreOrder, string, string, error) {
	orders := loadCardStoreOrders(ctx, system)
	now := time.Now().UTC()
	var updated cardStoreOrder
	var approveToken string
	var deleteToken string
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(orderNo) || !cardStoreOrderEmailMatches(orders.Orders[i], email) {
			continue
		}
		if orders.Orders[i].PaymentMode != cardStorePaymentModeManual {
			return cardStoreOrder{}, "", "", fmt.Errorf("order is not personal payment")
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			return orders.Orders[i], "", "", nil
		}
		if orders.Orders[i].Status == cardStoreStatusPersonalRejected {
			return cardStoreOrder{}, "", "", fmt.Errorf("order was rejected")
		}
		orders.Orders[i].Status = cardStoreStatusPersonalOpened
		orders.Orders[i].OpenedPaymentAt = now
		orders.Orders[i].UpdatedAt = now
		if mailer == nil {
			orders.Orders[i].ReminderMailStatus = "failed"
			orders.Orders[i].ReminderMailError = "mail service is not configured"
		}
		shouldSend := mailer != nil && (orders.Orders[i].ReminderMailSentAt.IsZero() || now.Sub(orders.Orders[i].ReminderMailSentAt) > 5*time.Minute)
		if shouldSend {
			var approveHash, deleteHash string
			var err error
			approveToken, approveHash, err = newCardStoreReviewToken()
			if err != nil {
				return cardStoreOrder{}, "", "", err
			}
			deleteToken, deleteHash, err = newCardStoreReviewToken()
			if err != nil {
				return cardStoreOrder{}, "", "", err
			}
			orders.Orders[i].AdminApproveTokenHash = approveHash
			orders.Orders[i].AdminDeleteTokenHash = deleteHash
			if err := sendCardStorePersonalPaymentReminder(ctx, mailer, cfg, orders.Orders[i], baseURL, approveToken, deleteToken); err != nil {
				orders.Orders[i].ReminderMailStatus = "failed"
				orders.Orders[i].ReminderMailError = err.Error()
			} else {
				orders.Orders[i].ReminderMailStatus = "sent"
				orders.Orders[i].ReminderMailError = ""
				orders.Orders[i].ReminderMailSentAt = now
			}
		}
		updated = orders.Orders[i]
		if err := saveCardStoreOrders(ctx, system, orders); err != nil {
			return cardStoreOrder{}, "", "", err
		}
		return updated, approveToken, deleteToken, nil
	}
	return cardStoreOrder{}, "", "", fmt.Errorf("order not found")
}

func saveCardStoreOrders(ctx context.Context, system store.SystemSettingsRepository, orders cardStoreOrders) error {
	sort.SliceStable(orders.Orders, func(i, j int) bool { return orders.Orders[i].CreatedAt.After(orders.Orders[j].CreatedAt) })
	if len(orders.Orders) > 500 {
		orders.Orders = orders.Orders[:500]
	}
	data, err := json.Marshal(orders)
	if err != nil {
		return err
	}
	return system.Set(ctx, cardStoreOrdersKey, string(data))
}

func markCardStoreOrderPaid(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, update cardStoreOrder, mailer cardStoreMailer, identity *auth.IdentityService, tenantID string, hubBaseURL string) error {
	orders := loadCardStoreOrders(ctx, system)
	matched := false
	var code string
	var mailErr error
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(update.OrderNo) {
			continue
		}
		matched = true
		if roundMoney(orders.Orders[i].Amount) != roundMoney(update.Amount) {
			return fmt.Errorf("payment amount mismatch")
		}
		if strings.EqualFold(strings.TrimSpace(orders.Orders[i].Status), "paid") && strings.TrimSpace(orders.Orders[i].EncryptedCode) != "" {
			autoRedeemChanged := false
			if orders.Orders[i].AutoRedeemedAt.IsZero() {
				if redeemed, redeemErr := autoRedeemCardStoreOrder(ctx, identity, system, tenantID, orders.Orders[i], llmservice.DecryptCardCode(orders.Orders[i].EncryptedCode), hubBaseURL); redeemed {
					orders.Orders[i].AutoRedeemedAt = update.UpdatedAt
					orders.Orders[i].AutoRedeemError = ""
					autoRedeemChanged = true
				} else if redeemErr != nil {
					orders.Orders[i].AutoRedeemError = redeemErr.Error()
					autoRedeemChanged = true
				}
			}
			paymentDetailsChanged := applyCardStorePaymentDetails(&orders.Orders[i], update)
			paidAtChanged := false
			if orders.Orders[i].PaidAt.IsZero() && !update.PaidAt.IsZero() {
				orders.Orders[i].PaidAt = update.PaidAt
				paidAtChanged = true
			}
			if mailer == nil || (strings.EqualFold(strings.TrimSpace(orders.Orders[i].MailStatus), "sent") && !autoRedeemChanged) {
				if autoRedeemChanged || paymentDetailsChanged || paidAtChanged {
					orders.Orders[i].UpdatedAt = update.UpdatedAt
					return saveCardStoreOrders(ctx, system, orders)
				}
				return nil
			}
			code = llmservice.DecryptCardCode(orders.Orders[i].EncryptedCode)
			if code == "" {
				return nil
			}
			if err := sendCardStoreCodeEmail(ctx, mailer, orders.Orders[i], code); err != nil {
				orders.Orders[i].MailStatus = "failed"
				orders.Orders[i].MailError = err.Error()
			} else {
				orders.Orders[i].MailStatus = "sent"
				orders.Orders[i].MailError = ""
			}
			orders.Orders[i].UpdatedAt = update.UpdatedAt
			return saveCardStoreOrders(ctx, system, orders)
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			return nil
		}
		if strings.TrimSpace(orders.Orders[i].EncryptedCode) == "" {
			card, generatedCode, err := createCardStoreServiceCard(ctx, system, cfg, orders.Orders[i])
			if err != nil {
				orders.Orders[i].Status = "issue_failed"
				orders.Orders[i].PaymentMsg = err.Error()
				applyCardStorePaymentDetails(&orders.Orders[i], update)
				if orders.Orders[i].PaymentMode == "" && update.PayType == cardStorePaymentModeManual {
					orders.Orders[i].PaymentMode = cardStorePaymentModeManual
				}
				orders.Orders[i].PaidAt = update.PaidAt
				orders.Orders[i].UpdatedAt = update.UpdatedAt
				if data, marshalErr := json.Marshal(orders); marshalErr == nil {
					_ = system.Set(ctx, cardStoreOrdersKey, string(data))
				}
				return err
			}
			orders.Orders[i].CardID = card.ID
			orders.Orders[i].EncryptedCode = card.EncryptedCode
			code = generatedCode
		} else {
			code = llmservice.DecryptCardCode(orders.Orders[i].EncryptedCode)
		}
		if redeemed, redeemErr := autoRedeemCardStoreOrder(ctx, identity, system, tenantID, orders.Orders[i], code, hubBaseURL); redeemed {
			orders.Orders[i].AutoRedeemedAt = update.PaidAt
			orders.Orders[i].AutoRedeemError = ""
		} else if redeemErr != nil {
			orders.Orders[i].AutoRedeemError = redeemErr.Error()
		}
		orders.Orders[i].Status = "paid"
		orders.Orders[i].PaymentMsg = update.PaymentMsg
		applyCardStorePaymentDetails(&orders.Orders[i], update)
		if orders.Orders[i].PaymentMode == "" && update.PayType == cardStorePaymentModeManual {
			orders.Orders[i].PaymentMode = cardStorePaymentModeManual
		}
		orders.Orders[i].PaidAt = update.PaidAt
		orders.Orders[i].UpdatedAt = update.UpdatedAt
		if code != "" && mailer != nil {
			mailErr = sendCardStoreCodeEmail(ctx, mailer, orders.Orders[i], code)
			if mailErr != nil {
				orders.Orders[i].MailStatus = "failed"
				orders.Orders[i].MailError = mailErr.Error()
			} else {
				orders.Orders[i].MailStatus = "sent"
				orders.Orders[i].MailError = ""
			}
		}
		break
	}
	if !matched {
		return fmt.Errorf("order not found")
	}
	data, err := json.Marshal(orders)
	if err != nil {
		return err
	}
	return system.Set(ctx, cardStoreOrdersKey, string(data))
}

func applyCardStorePaymentDetails(order *cardStoreOrder, update cardStoreOrder) bool {
	if order == nil {
		return false
	}
	changed := false
	if update.ActualPayAmount != "" {
		changed = changed || order.ActualPayAmount != update.ActualPayAmount
		order.ActualPayAmount = update.ActualPayAmount
	}
	if update.Payee != "" {
		changed = changed || order.Payee != update.Payee
		order.Payee = update.Payee
	}
	if update.PayTime != "" {
		changed = changed || order.PayTime != update.PayTime
		order.PayTime = update.PayTime
	}
	if update.PlatformOrderNo != "" {
		changed = changed || order.PlatformOrderNo != update.PlatformOrderNo
		order.PlatformOrderNo = update.PlatformOrderNo
	}
	if update.ChannelOrderNo != "" {
		changed = changed || order.ChannelOrderNo != update.ChannelOrderNo
		order.ChannelOrderNo = update.ChannelOrderNo
	}
	if update.PayType != "" {
		changed = changed || order.PayType != update.PayType
		order.PayType = update.PayType
	}
	if update.TradeType != "" {
		changed = changed || order.TradeType != update.TradeType
		order.TradeType = update.TradeType
	}
	return changed
}

func approveCardStorePersonalOrder(ctx context.Context, system store.SystemSettingsRepository, cfg cardStoreConfig, update cardStoreOrder, mailer cardStoreMailer, identity *auth.IdentityService, tenantID string, hubBaseURL string) error {
	order, ok := findCardStoreOrder(ctx, system, update.OrderNo)
	if !ok {
		return fmt.Errorf("order not found")
	}
	if order.PaymentMode != cardStorePaymentModeManual {
		return fmt.Errorf("order is not personal payment")
	}
	if order.Status != cardStoreStatusPersonalOpened && !isCardStorePaidLikeStatus(order.Status) {
		return fmt.Errorf("order status cannot be approved: %s", order.Status)
	}
	if roundMoney(order.Amount) != roundMoney(update.Amount) {
		return fmt.Errorf("payment amount mismatch")
	}
	if err := markCardStoreOrderPaid(ctx, system, cfg, update, mailer, identity, tenantID, hubBaseURL); err != nil {
		return err
	}
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) == strings.TrimSpace(update.OrderNo) {
			orders.Orders[i].ReviewedBy = update.ReviewedBy
			orders.Orders[i].ReviewedAt = update.ReviewedAt
			orders.Orders[i].ReviewNote = update.ReviewNote
			orders.Orders[i].AdminApproveTokenHash = ""
			orders.Orders[i].AdminDeleteTokenHash = ""
			return saveCardStoreOrders(ctx, system, orders)
		}
	}
	return nil
}

func rejectCardStorePersonalOrder(ctx context.Context, system store.SystemSettingsRepository, orderNo, reviewedBy, note string) (cardStoreOrder, error) {
	orders := loadCardStoreOrders(ctx, system)
	now := time.Now().UTC()
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(orderNo) {
			continue
		}
		if orders.Orders[i].PaymentMode != cardStorePaymentModeManual {
			return cardStoreOrder{}, fmt.Errorf("order is not personal payment")
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			return cardStoreOrder{}, fmt.Errorf("paid order cannot be rejected")
		}
		orders.Orders[i].Status = cardStoreStatusPersonalRejected
		orders.Orders[i].ReviewedBy = reviewedBy
		orders.Orders[i].ReviewedAt = now
		orders.Orders[i].ReviewNote = note
		orders.Orders[i].AdminApproveTokenHash = ""
		orders.Orders[i].AdminDeleteTokenHash = ""
		orders.Orders[i].UpdatedAt = now
		updated := orders.Orders[i]
		return updated, saveCardStoreOrders(ctx, system, orders)
	}
	return cardStoreOrder{}, fmt.Errorf("order not found")
}

func deleteCardStoreOrder(ctx context.Context, system store.SystemSettingsRepository, orderNo string) (cardStoreOrder, error) {
	orders := loadCardStoreOrders(ctx, system)
	for i := range orders.Orders {
		if strings.TrimSpace(orders.Orders[i].OrderNo) != strings.TrimSpace(orderNo) {
			continue
		}
		if isCardStorePaidLikeStatus(orders.Orders[i].Status) {
			return cardStoreOrder{}, fmt.Errorf("paid order cannot be deleted")
		}
		removed := orders.Orders[i]
		orders.Orders = append(orders.Orders[:i], orders.Orders[i+1:]...)
		return removed, saveCardStoreOrders(ctx, system, orders)
	}
	return cardStoreOrder{}, fmt.Errorf("order not found")
}

func canCompleteCardStoreOrder(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" || isCardStorePaidLikeStatus(status) || status == cardStoreStatusPersonalRejected {
		return false
	}
	return true
}

func GetCardStoreOrderHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := strings.TrimSpace(r.PathValue("orderNo"))
		email := normalizeCardStoreEmail(r.URL.Query().Get("email"))
		if orderNo == "" || !validCardStoreEmail(email) {
			writeError(w, http.StatusBadRequest, "CARD_STORE_ORDER_LOOKUP_INVALID", "order_no and email are required")
			return
		}
		tenantID := store.NormalizeTenantID(r.URL.Query().Get("tenant_id"))
		orders := loadCardStoreOrders(r.Context(), scopedSystemSettingsForTenant(tenantID, system))
		for _, order := range orders.Orders {
			if strings.TrimSpace(order.OrderNo) != orderNo || !cardStoreOrderEmailMatches(order, email) {
				continue
			}
			resp := cardStoreOrderCreateResponse(tenantID, order.Email, order)
			if order.SecondaryEmail != "" {
				resp["secondary_email"] = order.SecondaryEmail
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		writeError(w, http.StatusNotFound, "CARD_STORE_ORDER_NOT_FOUND", "order not found")
	}
}

func RecoverCardStoreCodesHandler(system store.SystemSettingsRepository, mailer cardStoreMailer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req cardStoreRecoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := normalizeCardStoreEmail(req.Email)
		if !validCardStoreEmail(email) {
			writeError(w, http.StatusBadRequest, "CARD_STORE_EMAIL_INVALID", "valid email is required")
			return
		}
		tenantID := store.NormalizeTenantID(req.TenantID)
		orders := loadCardStoreOrders(r.Context(), scopedSystemSettingsForTenant(tenantID, system))
		lines := []string{}
		for _, order := range orders.Orders {
			if order.Status != "paid" || !cardStoreOrderEmailMatches(order, email) {
				continue
			}
			code := llmservice.DecryptCardCode(order.EncryptedCode)
			if code == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s - %s - %s", order.OrderNo, order.ProductLabel, code))
		}
		if len(lines) > 0 && mailer != nil {
			body := "MaClaw Hub 服务兑换码找回\r\n\r\n" + strings.Join(lines, "\r\n") + "\r\n"
			if err := mailer.Send(store.WithTenant(r.Context(), tenantID), []string{email}, "MaClaw Hub 服务兑换码找回", body); err != nil {
				writeError(w, http.StatusBadGateway, "CARD_STORE_RECOVER_MAIL_FAILED", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"sent": len(lines) > 0 && mailer != nil, "count": len(lines)})
	}
}

func zhifuXPayNotifySign(state, merchantNum, orderNo, amount, accessKey string) string {
	seed := strings.TrimSpace(state) + strings.TrimSpace(merchantNum) + strings.TrimSpace(orderNo) + strings.TrimSpace(amount) + strings.TrimSpace(accessKey)
	sum := md5.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func writePlainPaymentNotifyResult(w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(result))
}

func parsePaymentAmount(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func newCardStoreOrderNo(now time.Time) string {
	return "CS" + now.Format("20060102150405") + strconv.FormatInt(now.UnixNano()%1000000, 10)
}

func formatPaymentAmount(amount float64) string {
	return fmt.Sprintf("%.2f", roundMoney(amount))
}

func roundMoney(amount float64) float64 {
	value, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", amount), 64)
	return value
}

func buildCardStoreSalesStats(orders []cardStoreOrder, period string, limit int) ([]cardStoreSalesStatRow, int, float64, int) {
	if limit <= 0 {
		limit = 30
	}
	now := time.Now().UTC()
	keys := make([]string, 0, limit)
	for i := limit - 1; i >= 0; i-- {
		when := now.AddDate(0, 0, -i)
		if period == "month" {
			when = now.AddDate(0, -i, 0)
		}
		keys = append(keys, cardStoreSalesBucket(when, period))
	}
	byBucket := make(map[string]*cardStoreSalesStatRow, len(keys))
	for _, key := range keys {
		row := &cardStoreSalesStatRow{Bucket: key}
		byBucket[key] = row
	}
	var totalOrders int
	var totalRevenue float64
	var totalCards int
	for _, order := range orders {
		if !isCardStorePaidLikeStatus(order.Status) {
			continue
		}
		when := order.PaidAt
		if when.IsZero() {
			when = order.UpdatedAt
		}
		if when.IsZero() {
			continue
		}
		key := cardStoreSalesBucket(when.UTC(), period)
		row := byBucket[key]
		if row == nil {
			continue
		}
		row.Orders++
		row.Revenue = roundMoney(row.Revenue + order.Amount)
		if strings.TrimSpace(order.CardID) != "" || strings.TrimSpace(order.EncryptedCode) != "" {
			row.Cards++
		}
	}
	rows := make([]cardStoreSalesStatRow, 0, len(keys))
	for _, key := range keys {
		row := *byBucket[key]
		rows = append(rows, row)
		totalOrders += row.Orders
		totalRevenue += row.Revenue
		totalCards += row.Cards
	}
	return rows, totalOrders, roundMoney(totalRevenue), totalCards
}

func cardStoreSalesBucket(when time.Time, period string) string {
	if period == "month" {
		return when.Format("2006-01")
	}
	return when.Format("2006-01-02")
}

func buildCardStoreSoldCards(orders []cardStoreOrder, cfg cardStoreConfig, reg *llmservice.Registry) []cardStoreSoldCard {
	cards := make([]cardStoreSoldCard, 0)
	registryCards := map[string]llmservice.RechargeCard{}
	if reg != nil {
		for _, card := range reg.Cards {
			registryCards[strings.TrimSpace(card.ID)] = card
		}
	}
	for _, order := range orders {
		if !isCardStoreVisibleInSales(order.Status) {
			continue
		}
		code := llmservice.DecryptCardCode(order.EncryptedCode)
		product, _ := findCardStoreProduct(cfg, order.ProductID)
		paidAt := ""
		if !order.PaidAt.IsZero() {
			paidAt = order.PaidAt.UTC().Format(time.RFC3339)
		}
		openedAt := ""
		if !order.OpenedPaymentAt.IsZero() {
			openedAt = order.OpenedPaymentAt.UTC().Format(time.RFC3339)
		} else if !order.UpdatedAt.IsZero() {
			openedAt = order.UpdatedAt.UTC().Format(time.RFC3339)
		} else if !order.CreatedAt.IsZero() {
			openedAt = order.CreatedAt.UTC().Format(time.RFC3339)
		}
		autoRedeemedAt := ""
		if !order.AutoRedeemedAt.IsZero() {
			autoRedeemedAt = order.AutoRedeemedAt.UTC().Format(time.RFC3339)
		}
		redeemedEmail := ""
		redeemedAt := ""
		if card, ok := registryCards[strings.TrimSpace(order.CardID)]; ok {
			redeemedEmail = strings.TrimSpace(card.RedeemedByEmail)
			if card.RedeemedAt != nil && !card.RedeemedAt.IsZero() {
				redeemedAt = card.RedeemedAt.UTC().Format(time.RFC3339)
			}
		}
		cards = append(cards, cardStoreSoldCard{
			OrderNo:         order.OrderNo,
			Status:          order.Status,
			Message:         order.PaymentMsg,
			ProductID:       order.ProductID,
			ProductLabel:    order.ProductLabel,
			Amount:          roundMoney(order.Amount),
			Email:           order.Email,
			CardID:          order.CardID,
			Code:            code,
			Credits:         product.Credits,
			DurationDays:    product.DurationDays,
			PaidAt:          paidAt,
			RedeemedEmail:   redeemedEmail,
			RedeemedAt:      redeemedAt,
			MailStatus:      order.MailStatus,
			MailError:       order.MailError,
			AutoRedeemed:    !order.AutoRedeemedAt.IsZero(),
			AutoRedeemAt:    autoRedeemedAt,
			AutoRedeemErr:   order.AutoRedeemError,
			PaymentID:       order.PaymentID,
			PaymentOrder:    order.PlatformOrderNo,
			ChannelOrder:    order.ChannelOrderNo,
			PayCode:         order.PayCode,
			PayChannel:      order.PayChannel,
			PayChannelLabel: order.PayChannelLabel,
			OpenedPaymentAt: openedAt,
			ReviewNote:      order.ReviewNote,
		})
	}
	sort.SliceStable(cards, func(i, j int) bool {
		left := firstNonEmptyString(cards[i].PaidAt, cards[i].OpenedPaymentAt)
		right := firstNonEmptyString(cards[j].PaidAt, cards[j].OpenedPaymentAt)
		return left > right
	})
	return cards
}

func isCardStorePaidLikeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "paid", "issue_failed":
		return true
	default:
		return false
	}
}

func isCardStoreVisibleInSales(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "created", "payment_started", "payment_failed", cardStoreStatusPersonalCreated, cardStoreStatusPersonalOpened, cardStoreStatusPersonalRejected:
		return true
	default:
		return isCardStorePaidLikeStatus(status)
	}
}

func externalBaseURL(r *http.Request) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	if r != nil {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = proto
		}
		host := r.Host
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			host = forwarded
		}
		return scheme + "://" + host
	}
	return ""
}
