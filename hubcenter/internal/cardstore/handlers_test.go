package cardstore

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
)

func TestAdminDeleteArchivedOrderHandlerDeletesArchivedPendingOrder(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ARCHIVED-PENDING": {
			Order:      corecardstore.Order{OrderNo: "HC-ARCHIVED-PENDING", Email: "owner@example.com", Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/cardstore/orders/HC-ARCHIVED-PENDING", nil)
	req.SetPathValue("orderNo", "HC-ARCHIVED-PENDING")
	rr := httptest.NewRecorder()

	AdminDeleteArchivedOrderHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	if got := orderRepo.byNo["HC-ARCHIVED-PENDING"]; got != nil {
		t.Fatalf("order still exists: %+v", got)
	}
}

func TestAdminDeleteArchivedOrderHandlerRejectsNonArchivedOrder(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVE": {
			Order: corecardstore.Order{OrderNo: "HC-ACTIVE", Email: "owner@example.com", Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/cardstore/orders/HC-ACTIVE", nil)
	req.SetPathValue("orderNo", "HC-ACTIVE")
	rr := httptest.NewRecorder()

	AdminDeleteArchivedOrderHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not archived") {
		t.Fatalf("body = %s, want not archived", rr.Body.String())
	}
	if got := orderRepo.byNo["HC-ACTIVE"]; got == nil {
		t.Fatal("non-archived order was deleted")
	}
}

func TestAdminRestoreArchivedOrderHandlerRestoresActivatedOrder(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ARCHIVED-ACTIVATED": {
			Order:      corecardstore.Order{OrderNo: "HC-ARCHIVED-ACTIVATED", Email: "owner@example.com", Status: corecardstore.StatusActivated, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cardstore/orders/HC-ARCHIVED-ACTIVATED/restore", nil)
	req.SetPathValue("orderNo", "HC-ARCHIVED-ACTIVATED")
	rr := httptest.NewRecorder()

	AdminRestoreArchivedOrderHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	if got := orderRepo.byNo["HC-ARCHIVED-ACTIVATED"].ArchivedAt; got != "" {
		t.Fatalf("ArchivedAt = %q, want empty after restore", got)
	}
	if !strings.Contains(rr.Body.String(), `"status":"restored"`) {
		t.Fatalf("body = %s, want restored status", rr.Body.String())
	}
}

func TestAdminRestoreArchivedOrderHandlerIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVATED": {
			Order: corecardstore.Order{OrderNo: "HC-ACTIVATED", Email: "owner@example.com", Status: corecardstore.StatusActivated, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cardstore/orders/HC-ACTIVATED/restore", nil)
	req.SetPathValue("orderNo", "HC-ACTIVATED")
	rr := httptest.NewRecorder()

	AdminRestoreArchivedOrderHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"restored"`) {
		t.Fatalf("body = %s, want restored status", rr.Body.String())
	}
}

func TestListCardTypesHandlerIncludesPaymentChannelDisplayDetails(t *testing.T) {
	cardRepo := &cardTypeTestRepo{all: []*CardType{{
		ID:       "ct1",
		Label:    "Starter",
		Credits:  100,
		Period:   "month",
		PriceRMB: 1,
		Enabled:  true,
	}}}
	svc := NewService(cardRepo, nil, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{
		Instruction: "Use order number as transfer remark.",
		Channels: []corecardstore.PersonalPaymentChannel{{
			ID:          "bank_transfer",
			Label:       "Bank",
			Payee:       "Finance",
			Enabled:     true,
			ImageURL:    "https://pay.example.com/bank.png",
			BankName:    "Example Bank",
			BankAccount: "6222000011112222",
			BankHolder:  "Example Ltd.",
			ContactInfo: "finance@example.com",
		}},
	}, corecardstore.AlipayDirectConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/cardstore/types", nil)
	rr := httptest.NewRecorder()

	ListCardTypesHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	var resp struct {
		PaymentChannels []map[string]any `json:"payment_channels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.PaymentChannels) != 1 {
		t.Fatalf("payment_channels len = %d, want 1", len(resp.PaymentChannels))
	}
	ch := resp.PaymentChannels[0]
	for key, want := range map[string]string{
		"id":           "bank_transfer",
		"payee":        "Finance",
		"image_url":    "https://pay.example.com/bank.png",
		"bank_name":    "Example Bank",
		"bank_account": "6222000011112222",
		"bank_holder":  "Example Ltd.",
		"contact_info": "finance@example.com",
		"instruction":  "Use order number as transfer remark.",
	} {
		if got := ch[key]; got != want {
			t.Fatalf("payment channel %s = %v, want %q", key, got, want)
		}
	}
}

func TestAlipayConfigForRequestFillsDefaultCallbackURLs(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/purchase", nil)
	req.Host = "hubcenter.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	svc := NewService(nil, nil, nil)
	cfg := alipayConfigForRequest(req, svc)

	if cfg.NotifyURL != "https://hubcenter.example.com/api/cardstore/payment/notify" {
		t.Fatalf("NotifyURL = %q", cfg.NotifyURL)
	}
	if cfg.ReturnURL != "https://hubcenter.example.com/api/cardstore/payment/return" {
		t.Fatalf("ReturnURL = %q", cfg.ReturnURL)
	}
}

func TestAlipayConfigForRequestMakesRelativeCallbacksAbsolute(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/purchase", nil)
	req.Host = "internal.local"
	req.Header.Set("X-Forwarded-Host", "pay.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	svc := NewService(nil, nil, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{
		NotifyURL: "/custom/notify",
		ReturnURL: "custom/return",
	})
	cfg := alipayConfigForRequest(req, svc)

	if cfg.NotifyURL != "https://pay.example.com/custom/notify" {
		t.Fatalf("NotifyURL = %q", cfg.NotifyURL)
	}
	if cfg.ReturnURL != "https://pay.example.com/custom/return" {
		t.Fatalf("ReturnURL = %q", cfg.ReturnURL)
	}
}

func TestAlipayConfigForRequestPreservesAbsoluteCallbacks(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/purchase", nil)
	req.Host = "hubcenter.example.com"

	svc := NewService(nil, nil, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{
		NotifyURL: "https://pay.example.com/notify",
		ReturnURL: "https://pay.example.com/return",
	})
	cfg := alipayConfigForRequest(req, svc)

	if cfg.NotifyURL != "https://pay.example.com/notify" {
		t.Fatalf("NotifyURL = %q", cfg.NotifyURL)
	}
	if cfg.ReturnURL != "https://pay.example.com/return" {
		t.Fatalf("ReturnURL = %q", cfg.ReturnURL)
	}
}

func TestAlipayConfigForRequestPrefersConfiguredPublicBaseURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/purchase", nil)
	req.Host = "attacker.example.com"
	req.Header.Set("X-Forwarded-Host", "spoof.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	svc := NewService(nil, nil, nil)
	svc.SetPublicBaseURLProvider(func(_ context.Context) (string, error) {
		return "https://center.example.com/", nil
	})

	cfg := alipayConfigForRequest(req, svc)

	if cfg.NotifyURL != "https://center.example.com/api/cardstore/payment/notify" {
		t.Fatalf("NotifyURL = %q", cfg.NotifyURL)
	}
	if cfg.ReturnURL != "https://center.example.com/api/cardstore/payment/return" {
		t.Fatalf("ReturnURL = %q", cfg.ReturnURL)
	}
}

func TestAlipayConfigWithOrderContextAddsReturnURLContext(t *testing.T) {
	order := &PurchaseOrder{
		Order:    corecardstore.Order{OrderNo: "HC-CONTEXT", Email: "owner@example.com"},
		HubID:    "hub-1",
		TenantID: "tenant-a",
	}
	cfg := alipayConfigWithOrderContext(corecardstore.AlipayDirectConfig{ReturnURL: "https://pay.example.com/api/cardstore/payment/return"}, order)
	ret, err := url.Parse(cfg.ReturnURL)
	if err != nil {
		t.Fatalf("parse return url: %v", err)
	}
	q := ret.Query()
	if q.Get("ctx_order_no") != "HC-CONTEXT" || q.Get("ctx_email") != "owner@example.com" || q.Get("ctx_hub_id") != "hub-1" || q.Get("ctx_tenant_id") != "tenant-a" {
		t.Fatalf("return_url query = %s", ret.RawQuery)
	}
}
func TestCreateOrderHandlerAddsDefaultAlipayCallbackURLsToPayURL(t *testing.T) {
	cardRepo := &cardTypeTestRepo{byID: map[string]*CardType{
		"ct1": {ID: "ct1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue", Enabled: true},
	}}
	orderRepo := &orderTestRepo{}
	svc := NewService(cardRepo, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{
		AppID:      "app-1",
		PrivateKey: testRSAPrivateKeyPEM(t),
	})

	body := `{"card_type_id":"ct1","admin_email":"owner@example.com","hub_id":"hub-1","tenant_id":"tenant-a"}`
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/purchase", strings.NewReader(body))
	req.Host = "internal.local"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Host", "pay.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()

	CreateOrderHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	var order PurchaseOrder
	if err := json.NewDecoder(rr.Body).Decode(&order); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	payURL, err := url.Parse(order.PayURL)
	if err != nil {
		t.Fatalf("parse pay_url: %v", err)
	}
	if got := payURL.Query().Get("notify_url"); got != "https://pay.example.com/api/cardstore/payment/notify" {
		t.Fatalf("notify_url = %q", got)
	}
	returnURL, err := url.Parse(payURL.Query().Get("return_url"))
	if err != nil {
		t.Fatalf("parse return_url: %v", err)
	}
	if got := returnURL.Scheme + "://" + returnURL.Host + returnURL.Path; got != "https://pay.example.com/api/cardstore/payment/return" {
		t.Fatalf("return_url path = %q", got)
	}
	returnQuery := returnURL.Query()
	if returnQuery.Get("ctx_email") != "owner@example.com" || returnQuery.Get("ctx_hub_id") != "hub-1" || returnQuery.Get("ctx_tenant_id") != "tenant-a" || returnQuery.Get("ctx_order_no") == "" {
		t.Fatalf("return_url query = %s", returnURL.RawQuery)
	}
}

func TestListOrdersHandlerHydratesAlipayPayURLWithDefaultCallbacks(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-PENDING-ALIPAY": {
			Order: corecardstore.Order{
				OrderNo:      "HC-PENDING-ALIPAY",
				ProductID:    "ct1",
				ProductLabel: "Plan",
				Email:        "owner@example.com",
				Amount:       10,
				Status:       corecardstore.StatusPending,
				PaymentMode:  corecardstore.PaymentModeAlipay,
				PayURL:       "https://openapi.alipay.com/gateway.do?out_trade_no=HC-PENDING-ALIPAY",
				CreatedAt:    now,
				UpdatedAt:    now,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "sg-1",
			Credits:        1000,
			Period:         "month",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{
		AppID:      "app-1",
		PrivateKey: testRSAPrivateKeyPEM(t),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cardstore/orders?email=owner@example.com&hub_id=hub-1&tenant_id=tenant-a", nil)
	req.Host = "internal.local"
	req.Header.Set("X-Forwarded-Host", "pay.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()

	ListOrdersHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}
	var body struct {
		Orders []*PurchaseOrder `json:"orders"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode orders: %v", err)
	}
	if len(body.Orders) != 1 {
		t.Fatalf("orders len = %d, want 1", len(body.Orders))
	}
	payURL, err := url.Parse(body.Orders[0].PayURL)
	if err != nil {
		t.Fatalf("parse pay_url: %v", err)
	}
	if got := payURL.Query().Get("notify_url"); got != "https://pay.example.com/api/cardstore/payment/notify" {
		t.Fatalf("notify_url = %q", got)
	}
	returnURL, err := url.Parse(payURL.Query().Get("return_url"))
	if err != nil {
		t.Fatalf("parse return_url: %v", err)
	}
	if got := returnURL.Scheme + "://" + returnURL.Host + returnURL.Path; got != "https://pay.example.com/api/cardstore/payment/return" {
		t.Fatalf("return_url path = %q", got)
	}
	returnQuery := returnURL.Query()
	if returnQuery.Get("ctx_email") != "owner@example.com" || returnQuery.Get("ctx_hub_id") != "hub-1" || returnQuery.Get("ctx_tenant_id") != "tenant-a" || returnQuery.Get("ctx_order_no") == "" {
		t.Fatalf("return_url query = %s", returnURL.RawQuery)
	}
}

func TestValidateAlipayNotificationOrderRejectsAmountMismatch(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-AMOUNT": {
			Order: corecardstore.Order{
				OrderNo:     "HC-AMOUNT",
				Amount:      88,
				PaymentMode: corecardstore.PaymentModeAlipay,
				Status:      corecardstore.StatusPending,
			},
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{AppID: "app-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/payment/notify", nil)

	err := validateAlipayNotificationOrder(req, svc, "HC-AMOUNT", url.Values{"app_id": {"app-1"}, "total_amount": {"1.00"}})

	if err == nil || !strings.Contains(err.Error(), "amount mismatch") {
		t.Fatalf("error = %v, want amount mismatch", err)
	}
}

func TestValidateAlipayNotificationOrderRejectsAppIDMismatch(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-APP": {
			Order: corecardstore.Order{OrderNo: "HC-APP", Amount: 88, PaymentMode: corecardstore.PaymentModeAlipay, Status: corecardstore.StatusPending},
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{AppID: "app-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/payment/notify", nil)

	err := validateAlipayNotificationOrder(req, svc, "HC-APP", url.Values{"app_id": {"other-app"}, "total_amount": {"88.00"}})

	if err == nil || !strings.Contains(err.Error(), "app_id mismatch") {
		t.Fatalf("error = %v, want app_id mismatch", err)
	}
}

func TestValidateAlipayNotificationOrderRequiresTotalAmount(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-NO-AMOUNT": {
			Order: corecardstore.Order{OrderNo: "HC-NO-AMOUNT", Amount: 88, PaymentMode: corecardstore.PaymentModeAlipay, Status: corecardstore.StatusPending},
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{AppID: "app-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/payment/notify", nil)

	err := validateAlipayNotificationOrder(req, svc, "HC-NO-AMOUNT", url.Values{"app_id": {"app-1"}})

	if err == nil || !strings.Contains(err.Error(), "total_amount is required") {
		t.Fatalf("error = %v, want total_amount required", err)
	}
}

func TestValidateAlipayNotificationOrderRejectsNonAlipayOrder(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-MANUAL": {
			Order: corecardstore.Order{
				OrderNo:     "HC-MANUAL",
				Amount:      88,
				PaymentMode: corecardstore.PaymentModeSemiManual,
				Status:      corecardstore.StatusPending,
			},
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{AppID: "app-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/payment/notify", nil)

	err := validateAlipayNotificationOrder(req, svc, "HC-MANUAL", url.Values{"app_id": {"app-1"}, "total_amount": {"88.00"}})

	if err == nil || !strings.Contains(err.Error(), "payment mode") {
		t.Fatalf("error = %v, want payment mode rejection", err)
	}
}

func TestVerifyAlipayReturnQueryIgnoresUnsignedContextParams(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	values := url.Values{
		"app_id":       {"app-1"},
		"out_trade_no": {"HC-RETURN-SIGNED"},
		"total_amount": {"88.00"},
	}
	values.Set("sign", signAlipayTestValues(t, values, key))
	values.Set("sign_type", "RSA2")
	values.Set("ctx_email", "owner@example.com")
	values.Set("ctx_hub_id", "hub-1")
	values.Set("ctx_tenant_id", "tenant-a")
	values.Set("ctx_hub_name", "Acme Hub")
	values.Set("ctx_tenant_name", "Acme Tenant")

	verified, err := verifyAlipayReturnQuery(values, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})))
	if err != nil {
		t.Fatalf("verifyAlipayReturnQuery: %v", err)
	}
	if got := verified.Get("out_trade_no"); got != "HC-RETURN-SIGNED" {
		t.Fatalf("out_trade_no = %q", got)
	}
}
func TestAlipayReturnStoreURLsFallsBackToReturnURLContext(t *testing.T) {
	svc := NewService(nil, &orderTestRepo{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/cardstore/payment/return?ctx_email=owner%40example.com&ctx_hub_id=hub-1&ctx_tenant_id=tenant-a&ctx_hub_name=Acme+Hub&ctx_tenant_name=Acme+Tenant", nil)

	storeURL, ordersURL := alipayReturnStoreURLs(req, svc, "HC-MISSING")

	parsed, err := url.Parse(storeURL)
	if err != nil {
		t.Fatalf("parse store url: %v", err)
	}
	q := parsed.Query()
	if q.Get("email") != "owner@example.com" || q.Get("hub_id") != "hub-1" || q.Get("tenant_id") != "tenant-a" || q.Get("hub_name") != "Acme Hub" || q.Get("tenant_name") != "Acme Tenant" {
		t.Fatalf("query = %s", parsed.RawQuery)
	}
	if ordersURL != storeURL+"#ordersPanel" {
		t.Fatalf("ordersURL = %q, want %q", ordersURL, storeURL+"#ordersPanel")
	}
}
func TestAlipayReturnStoreURLsPreserveOrderTenantContext(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-RETURN": {
			Order:          corecardstore.Order{OrderNo: "HC-RETURN", Email: "owner@example.com"},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "sg-1",
			Credits:        1000,
			Period:         "month",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/cardstore/payment/return?ctx_hub_name=Acme+Hub&ctx_tenant_name=Acme+Tenant", nil)

	storeURL, ordersURL := alipayReturnStoreURLs(req, svc, "HC-RETURN")

	parsed, err := url.Parse(storeURL)
	if err != nil {
		t.Fatalf("parse store url: %v", err)
	}
	if parsed.Path != "/compute-store" {
		t.Fatalf("path = %q", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("email") != "owner@example.com" || q.Get("hub_id") != "hub-1" || q.Get("tenant_id") != "tenant-a" || q.Get("hub_name") != "Acme Hub" || q.Get("tenant_name") != "Acme Tenant" {
		t.Fatalf("query = %s", parsed.RawQuery)
	}
	if ordersURL != storeURL+"#ordersPanel" {
		t.Fatalf("ordersURL = %q, want %q", ordersURL, storeURL+"#ordersPanel")
	}
}
func TestWriteAlipayReturnPageAutoRedirectsOnlyOnSuccess(t *testing.T) {
	rr := httptest.NewRecorder()
	writeAlipayReturnPage(rr, true, "HC-RETURN", "ok", "/compute-store?email=owner%40example.com&hub_id=hub-1&tenant_id=tenant-a", "/compute-store?email=owner%40example.com&hub_id=hub-1&tenant_id=tenant-a#ordersPanel")
	body := rr.Body.String()
	if !strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, `tenant_id=tenant-a#ordersPanel`) {
		t.Fatalf("success return page missing redirect: %s", body)
	}

	rr = httptest.NewRecorder()
	writeAlipayReturnPage(rr, false, "", "failed", "/compute-store", "/compute-store#ordersPanel")
	if strings.Contains(rr.Body.String(), `http-equiv="refresh"`) {
		t.Fatalf("failure return page should not auto redirect: %s", rr.Body.String())
	}
}
func TestAlipayNotifyHandlerConfirmsAndActivatesOrder(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER}))
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-NOTIFY-OK": {
			Order: corecardstore.Order{
				OrderNo:      "HC-NOTIFY-OK",
				ProductID:    "ct1",
				ProductLabel: "Plan",
				Email:        "owner@example.com",
				Amount:       88,
				Status:       corecardstore.StatusPending,
				PaymentMode:  corecardstore.PaymentModeAlipay,
				CreatedAt:    now,
				UpdatedAt:    now,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "sg-1",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{AppID: "app-1", AlipayPublicKey: publicPEM})
	values := url.Values{
		"app_id":       {"app-1"},
		"out_trade_no": {"HC-NOTIFY-OK"},
		"total_amount": {"88.00"},
		"trade_status": {"TRADE_SUCCESS"},
	}
	values.Set("sign", signAlipayTestValues(t, values, key))
	values.Set("sign_type", "RSA2")
	req := httptest.NewRequest(http.MethodPost, "/api/cardstore/payment/notify", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	AlipayNotifyHandler(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "success" {
		t.Fatalf("response = %d %q, want 200 success", rr.Code, rr.Body.String())
	}
	order := orderRepo.byNo["HC-NOTIFY-OK"]
	if order.Status != corecardstore.StatusActivated {
		t.Fatalf("order status = %s, want activated", order.Status)
	}
	if order.ReviewedBy != "alipay_callback" {
		t.Fatalf("reviewer = %q, want alipay_callback", order.ReviewedBy)
	}
	if len(authRepo.created) != 1 {
		t.Fatalf("created auth count = %d, want 1", len(authRepo.created))
	}
	if auth := authRepo.created[0]; auth.HubID != "hub-1" || auth.TenantID != "tenant-a" || auth.CreditsTotal != 1000 {
		t.Fatalf("auth = %+v, want hub/tenant/credits from order", auth)
	}
}

func signAlipayTestValues(t *testing.T, values url.Values, key *rsa.PrivateKey) string {
	t.Helper()
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values.Get(k))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign alipay values: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
func testRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return string(pem.EncodeToMemory(block))
}
