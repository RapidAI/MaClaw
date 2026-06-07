package httpapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type cardStoreTestMailer struct {
	to      []string
	subject string
	body    string
	count   int
	err     error
}

func (m *cardStoreTestMailer) Send(_ context.Context, to []string, subject string, body string) error {
	m.count++
	m.to = append([]string(nil), to...)
	m.subject = subject
	m.body = body
	if m.err != nil {
		return m.err
	}
	return nil
}

func resetCardStoreRecoverRateLimiterForTest() {
	globalCardStoreRecoverRateLimiter.mu.Lock()
	defer globalCardStoreRecoverRateLimiter.mu.Unlock()
	globalCardStoreRecoverRateLimiter.day = ""
	globalCardStoreRecoverRateLimiter.ips = nil
	globalCardStoreRecoverRateLimiter.emails = nil
}

func newAlipayTestKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return string(privatePEM), string(publicPEM)
}

func TestPaymentFMSignUsesDocumentedConcatOrder(t *testing.T) {
	got := paymentFMSign("mch", "order-1", "10.01", "https://hub.example.com/notify", "secret")
	sum := md5.Sum([]byte("mch" + "order-1" + "10.01" + "https://hub.example.com/notify" + "secret"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("sign = %q, want %q", got, want)
	}
}

func TestCardStoreConfigUsesPaymentFMDefaults(t *testing.T) {
	cfg := normalizeCardStoreConfig(cardStoreConfig{})
	if cfg.PaymentAPIBaseURL != defaultPaymentFMAPIBaseURL || cfg.MerchantNum != defaultPaymentFMMerchantNo || cfg.AccessKey != defaultPaymentFMAccessKey {
		t.Fatalf("payment defaults not applied: %#v", cfg)
	}
	product, ok := findCardStoreProduct(cfg, "service_test_10")
	if !ok || product.Price != 0.01 || product.Credits != 1 || !strings.Contains(strings.ToLower(product.Description), "testing") {
		t.Fatalf("test product defaults not applied: %#v", product)
	}
	if product.Kind != "credits" || product.DurationDays != 365 || product.PeriodLimits != (llmservice.CreditPeriodLimits{}) {
		t.Fatalf("test product should be a credits test card without period limits: %#v", product)
	}
}

func TestCardStoreConfigAppliesDefaultPeriodLimits(t *testing.T) {
	cfg := normalizeCardStoreConfig(cardStoreConfig{})
	tests := []struct {
		id   string
		want llmservice.CreditPeriodLimits
	}{
		{id: "service_day", want: llmservice.CreditPeriodLimits{FiveHour: 150}},
		{id: "service_week", want: llmservice.CreditPeriodLimits{FiveHour: 300, Daily: 600}},
		{id: "service_month", want: llmservice.CreditPeriodLimits{FiveHour: 600, Daily: 1200, Weekly: 2400}},
		{id: "service_quarter", want: llmservice.CreditPeriodLimits{FiveHour: 1200, Daily: 2400, Weekly: 4800, Monthly: 10000}},
		{id: "service_year", want: llmservice.CreditPeriodLimits{FiveHour: 2400, Daily: 4800, Weekly: 9600, Monthly: 40000}},
		{id: "credits_10000", want: llmservice.CreditPeriodLimits{}},
		{id: "service_test_10", want: llmservice.CreditPeriodLimits{}},
	}
	for _, tc := range tests {
		product, ok := findCardStoreProduct(cfg, tc.id)
		if !ok {
			t.Fatalf("missing product %s", tc.id)
		}
		if product.PeriodLimits != tc.want {
			t.Fatalf("%s period limits = %#v, want %#v", tc.id, product.PeriodLimits, tc.want)
		}
	}
}

func TestCardStoreConfigNormalizesLegacyTestProductAsCredits(t *testing.T) {
	cfg := normalizeCardStoreConfig(cardStoreConfig{Products: []cardStoreProduct{{
		ID:           "service_test_10",
		Kind:         "service_card",
		DurationDays: 1,
		Credits:      999,
		PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 50, Daily: 100},
	}}})
	product, ok := findCardStoreProduct(cfg, "service_test_10")
	if !ok {
		t.Fatal("missing service_test_10")
	}
	if product.Kind != "credits" || product.DurationDays != 365 || product.Credits != 1 || product.PeriodLimits != (llmservice.CreditPeriodLimits{}) {
		t.Fatalf("legacy test product normalization failed: %#v", product)
	}
}

func TestCardStoreConfigKeepsCustomPeriodLimitsWhenDurationDefaults(t *testing.T) {
	cfg := normalizeCardStoreConfig(cardStoreConfig{Products: []cardStoreProduct{{
		ID:           "service_month",
		PeriodLimits: llmservice.CreditPeriodLimits{FiveHour: 60, Daily: 120, Weekly: 240, Monthly: 480},
	}}})
	product, ok := findCardStoreProduct(cfg, "service_month")
	if !ok {
		t.Fatal("missing service_month")
	}
	want := llmservice.CreditPeriodLimits{FiveHour: 60, Daily: 120, Weekly: 240}
	if product.DurationDays != 30 || product.PeriodLimits != want {
		t.Fatalf("service_month = days %d limits %#v, want days 30 limits %#v", product.DurationDays, product.PeriodLimits, want)
	}
}

func TestRecoverCardStoreCodesRateLimitsByEmailPerDay(t *testing.T) {
	resetCardStoreRecoverRateLimiterForTest()
	defer resetCardStoreRecoverRateLimiterForTest()
	handler := RecoverCardStoreCodesHandler(newTestLLMServiceSystemSettings(), nil)
	for i := 0; i < cardStoreRecoverDailyEmailLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(`{"email":"buyer@example.com"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("recover request %d status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(`{"email":"buyer@example.com"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third email recover status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "CARD_STORE_RECOVER_RATE_LIMITED" || body["scope"] != "email" {
		t.Fatalf("rate limit body = %#v", body)
	}
}

func TestRecoverCardStoreCodesRateLimitsByIPPerDay(t *testing.T) {
	resetCardStoreRecoverRateLimiterForTest()
	defer resetCardStoreRecoverRateLimiterForTest()
	handler := RecoverCardStoreCodesHandler(newTestLLMServiceSystemSettings(), nil)
	for i := 0; i < cardStoreRecoverDailyIPLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(fmt.Sprintf(`{"email":"buyer%d@example.com"}`, i)))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("recover request %d status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(`{"email":"overflow@example.com"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("eleventh IP recover status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "CARD_STORE_RECOVER_RATE_LIMITED" || body["scope"] != "ip" {
		t.Fatalf("rate limit body = %#v", body)
	}
}

func TestRecoverCardStoreCodesEmailLimitedRequestsStillCountTowardIP(t *testing.T) {
	resetCardStoreRecoverRateLimiterForTest()
	defer resetCardStoreRecoverRateLimiterForTest()
	handler := RecoverCardStoreCodesHandler(newTestLLMServiceSystemSettings(), nil)
	var last map[string]any
	for i := 0; i < cardStoreRecoverDailyIPLimit+1; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(`{"email":"buyer@example.com"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.20")
		handler(rec, req)
		last = map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &last); err != nil {
			t.Fatal(err)
		}
	}
	if last["code"] != "CARD_STORE_RECOVER_RATE_LIMITED" || last["scope"] != "ip" {
		t.Fatalf("eleventh repeated email recover should hit IP scope, body = %#v", last)
	}
}

func TestRecoverCardStoreCodesIgnoresSpoofedForwardedForFromPublicRemote(t *testing.T) {
	resetCardStoreRecoverRateLimiterForTest()
	defer resetCardStoreRecoverRateLimiterForTest()
	handler := RecoverCardStoreCodesHandler(newTestLLMServiceSystemSettings(), nil)
	for i := 0; i < cardStoreRecoverDailyIPLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(fmt.Sprintf(`{"email":"buyer%d@example.com"}`, i)))
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("recover request %d status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/recover", strings.NewReader(`{"email":"overflow@example.com"}`))
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.250")
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed forwarded-for should not bypass public remote IP limit, status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestCardStoreRecoverRateLimiterResetsNextDay(t *testing.T) {
	limiter := &cardStoreRecoverRateLimiter{}
	now := time.Date(2026, 6, 6, 23, 59, 0, 0, time.UTC)
	for i := 0; i < cardStoreRecoverDailyEmailLimit; i++ {
		if ok, scope := limiter.allow("203.0.113.7", "buyer@example.com", now); !ok {
			t.Fatalf("request %d limited with scope %q", i+1, scope)
		}
	}
	if ok, scope := limiter.allow("203.0.113.7", "buyer@example.com", now); ok || scope != "email" {
		t.Fatalf("same day extra request = ok %v scope %q, want email limit", ok, scope)
	}
	if ok, scope := limiter.allow("203.0.113.7", "buyer@example.com", now.Add(2*time.Minute)); !ok {
		t.Fatalf("next day request limited with scope %q", scope)
	}
}

func TestGetCardStoreConfigReturnsCurrentHubNotifyURL(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{AlipayDirect: cardStoreAlipayDirectConfig{PrivateKey: "alipay-secret"}})
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/card-store/config", nil)
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	GetCardStoreConfigHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var respCfg cardStoreConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &respCfg); err != nil {
		t.Fatal(err)
	}
	if respCfg.NotifyURL != "https://hub.example.com/api/zhifuxpay/notify" {
		t.Fatalf("notify_url = %q", respCfg.NotifyURL)
	}
	if respCfg.AlipayDirect.PrivateKey != "" {
		t.Fatalf("alipay private key leaked")
	}
}

func TestUpdateCardStoreConfigKeepsDefaultNotifyURLDynamic(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	oldCfg := normalizeCardStoreConfig(cardStoreConfig{AlipayDirect: cardStoreAlipayDirectConfig{PrivateKey: "old-alipay-private"}})
	oldData, _ := json.Marshal(oldCfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(oldData)); err != nil {
		t.Fatal(err)
	}
	payload := `{"enabled":true,"notify_url":"https://hub.example.com/api/zhifuxpay/notify","products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	if strings.TrimSpace(stored.NotifyURL) != "" {
		t.Fatalf("stored notify_url = %q, want empty dynamic default", stored.NotifyURL)
	}
	if stored.AlipayDirect.PrivateKey != "old-alipay-private" {
		t.Fatalf("stored alipay private key = %q, want preserved", stored.AlipayDirect.PrivateKey)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/card-store/config", nil)
	req.Host = "newhub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	GetCardStoreConfigHandler(system).ServeHTTP(rec, req)
	var cfg cardStoreConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyURL != "https://newhub.example.com/api/zhifuxpay/notify" {
		t.Fatalf("notify_url after host change = %q", cfg.NotifyURL)
	}
}

func TestUpdateCardStoreConfigKeepsAlipayDefaultURLsDynamic(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	oldCfg := normalizeCardStoreConfig(cardStoreConfig{AlipayDirect: cardStoreAlipayDirectConfig{PrivateKey: "old-alipay-private"}})
	oldData, _ := json.Marshal(oldCfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(oldData)); err != nil {
		t.Fatal(err)
	}
	payload := `{"enabled":true,"payment_mode":"payment_fm","alipay_direct":{"notify_url":"https://oldhub.example.com/api/card-store/payment/notify?tenant_id=tenant_default","return_url":"https://oldhub.example.com/card_store?tenant_id=tenant_default"},"products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	if strings.TrimSpace(stored.AlipayDirect.NotifyURL) != "" || strings.TrimSpace(stored.AlipayDirect.ReturnURL) != "" {
		t.Fatalf("stored alipay default urls should be dynamic: notify=%q return=%q", stored.AlipayDirect.NotifyURL, stored.AlipayDirect.ReturnURL)
	}
	if stored.AlipayDirect.PrivateKey != "old-alipay-private" {
		t.Fatalf("stored alipay private key = %q, want preserved", stored.AlipayDirect.PrivateKey)
	}
}

func TestUpdateCardStoreConfigValidatesAlipayDirectKeys(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	payload := `{"enabled":true,"payment_mode":"alipay_direct","alipay_direct":{"app_id":"2021000000000000","gateway_url":"https://openapi.alipay.com/gateway.do","private_key":"bad","alipay_public_key":"bad"},"products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "private key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCardStoreConfigValidatesEnabledAlipayDirectMethod(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	payload := `{"enabled":true,"payment_mode":"payment_fm","payment_methods":["payment_fm","alipay_direct"],"alipay_direct":{"app_id":"2021000000000000","gateway_url":"https://openapi.alipay.com/gateway.do","private_key":"bad","alipay_public_key":"bad"},"products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "private key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCardStoreConfigStoreScopeDoesNotValidateOrMutatePayment(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	oldCfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, PaymentMethods: []string{cardStorePaymentModeAlipay}, NotifyURL: "https://hub.example.com/api/zhifuxpay/notify", AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", GatewayURL: "https://openapi.alipay.com/gateway.do", PrivateKey: "bad", AlipayPublicKey: "bad"}})
	oldCfg.Products[0].Price = 10
	oldData, _ := json.Marshal(oldCfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(oldData)); err != nil {
		t.Fatal(err)
	}
	payload := `{"enabled":true,"payment_mode":"alipay_direct","payment_methods":["alipay_direct"],"alipay_direct":{"app_id":"2021000000000000","gateway_url":"https://openapi.alipay.com/gateway.do","private_key":"","alipay_public_key":"bad"},"products":[{"id":"service_day","kind":"service_card","label":"Day Card","enabled":true,"price":3,"duration_days":1,"credits":300}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config?scope=store", strings.NewReader(payload))
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	if stored.PaymentMode != cardStorePaymentModeAlipay || len(stored.PaymentMethods) != 1 || stored.PaymentMethods[0] != cardStorePaymentModeAlipay || stored.NotifyURL != "https://hub.example.com/api/zhifuxpay/notify" || stored.AlipayDirect.PrivateKey != "bad" {
		t.Fatalf("payment settings mutated: mode=%q methods=%#v alipay=%#v", stored.PaymentMode, stored.PaymentMethods, stored.AlipayDirect)
	}
	if stored.Products[0].Price != 3 {
		t.Fatalf("store product price = %v, want 3", stored.Products[0].Price)
	}
}

func TestUpdateCardStoreConfigPaymentScopeDoesNotMutateStore(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	oldCfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, ServiceGroupIDs: []string{"svc-a"}, Products: []cardStoreProduct{{ID: "service_day", Kind: "service_card", Label: "Day Card", Enabled: true, Price: 3, DurationDays: 1, Credits: 300}}, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"owner@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Enabled: true, ImageURL: "https://pay.example.com/wx.png"}}}})
	oldData, _ := json.Marshal(oldCfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(oldData)); err != nil {
		t.Fatal(err)
	}
	payload := `{"enabled":false,"payment_mode":"personal_semimanual","payment_methods":["personal_semimanual"],"personal_payment":{"admin_emails":["owner@example.com"],"channels":[{"id":"wechat","enabled":true,"image_url":"https://pay.example.com/new-wx.png"}]},"service_group_ids":["svc-b"],"products":[{"id":"service_day","kind":"service_card","label":"Day Card","enabled":false,"price":99,"duration_days":1,"credits":300}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config?scope=payment", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	day, ok := findCardStoreProduct(stored, "service_day")
	if !stored.Enabled || len(stored.ServiceGroupIDs) != 1 || stored.ServiceGroupIDs[0] != "svc-a" || !ok || day.Price != 3 || !day.Enabled {
		t.Fatalf("store settings mutated by payment scope: enabled=%v groups=%#v products=%#v", stored.Enabled, stored.ServiceGroupIDs, stored.Products)
	}
	if stored.PaymentMode != cardStorePaymentModeManual || stored.PersonalPayment.Channels[0].ImageURL != "https://pay.example.com/new-wx.png" {
		t.Fatalf("payment settings not saved: mode=%q personal=%#v", stored.PaymentMode, stored.PersonalPayment)
	}
}

func TestUpdateCardStoreConfigKeepsSelectedDefaultPaymentMethod(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	payload := `{"enabled":true,"payment_mode":"personal_semimanual","payment_methods":["payment_fm","personal_semimanual"],"payment_api_base_url":"https://pay.example.com/api","merchant_num":"merchant-a","access_key":"key-a","pay_type":"aloop","personal_payment":{"admin_emails":["admin@example.com"],"channels":[{"id":"wechat","enabled":true,"image_url":"https://pay.example.com/wx.png"}]},"products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	if stored.PaymentMode != cardStorePaymentModeManual || len(stored.PaymentMethods) != 2 || stored.PaymentMethods[0] != cardStorePaymentModeManual || stored.PaymentMethods[1] != cardStorePaymentModeFM {
		t.Fatalf("payment mode/method order not preserved: mode=%q methods=%#v", stored.PaymentMode, stored.PaymentMethods)
	}
}

func TestCardStorePaymentQRUploadAndServe(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, 520)...)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("channel", "alipay"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "qr.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/card-store/payment-qr/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(WithRequestTenant(req.Context(), "tenant_store"))
	rec := httptest.NewRecorder()
	AdminCardStorePaymentQRUploadHandler(dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URL      string `json:"url"`
		ImageURL string `json:"image_url"`
		Channel  string `json:"channel"`
		TenantID string `json:"tenant_id"`
		Size     int    `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Channel != "alipay" || body.TenantID != "tenant_store" || body.URL == "" || body.URL != body.ImageURL || body.Size != len(png) {
		t.Fatalf("unexpected upload body: %+v", body)
	}
	if !strings.HasPrefix(body.URL, "/api/card-store/payment-qr/tenant_store/alipay-") || !strings.HasSuffix(body.URL, ".png") {
		t.Fatalf("unexpected image url: %s", body.URL)
	}
	if _, err := os.Stat(filepath.Join(dir, "tenant_store", strings.TrimPrefix(body.URL, "/api/card-store/payment-qr/tenant_store/"))); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}

	serveReq := httptest.NewRequest(http.MethodGet, body.URL, nil)
	serveReq.SetPathValue("tenantID", "tenant_store")
	serveReq.SetPathValue("filename", strings.TrimPrefix(body.URL, "/api/card-store/payment-qr/tenant_store/"))
	serveRec := httptest.NewRecorder()
	CardStorePaymentQRImageHandler(dir).ServeHTTP(serveRec, serveReq)
	if serveRec.Code != http.StatusOK || !bytes.Equal(serveRec.Body.Bytes(), png) {
		t.Fatalf("serve status=%d len=%d", serveRec.Code, serveRec.Body.Len())
	}
}

func TestCardStorePaymentQRUploadRejectsNonImage(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("channel", "wechat")
	part, err := writer.CreateFormFile("file", "qr.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("not an image"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/card-store/payment-qr/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	AdminCardStorePaymentQRUploadHandler(t.TempDir()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "CARD_STORE_PAYMENT_QR_TYPE_INVALID") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCardStoreConfigValidatesPersonalPayment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "admin email",
			payload: `{"enabled":true,"payment_mode":"personal_semimanual","personal_payment":{"channels":[{"id":"wechat","enabled":true,"image_url":"https://pay.example.com/wx.png"}]},"products":[]}`,
			want:    "store owner email",
		},
		{
			name:    "qr image",
			payload: `{"enabled":true,"payment_mode":"personal_semimanual","personal_payment":{"admin_emails":["admin@example.com"],"channels":[{"id":"wechat","enabled":true}]},"products":[]}`,
			want:    "QR image",
		},
		{
			name:    "all enabled channels need qr image",
			payload: `{"enabled":true,"payment_mode":"personal_semimanual","personal_payment":{"admin_emails":["admin@example.com"],"channels":[{"id":"alipay","enabled":true,"image_url":"https://pay.example.com/alipay.png"},{"id":"wechat","enabled":true}]},"products":[]}`,
			want:    "channel: wechat",
		},
		{
			name:    "enabled channel",
			payload: `{"enabled":true,"payment_mode":"personal_semimanual","personal_payment":{"admin_emails":["admin@example.com"],"channels":[{"id":"wechat","enabled":false,"image_url":"https://pay.example.com/wx.png"}]},"products":[]}`,
			want:    "no personal payment channel",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			system := newTestLLMServiceSystemSettings()
			req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateCardStoreConfigPreservesStoredAlipayPrivateKey(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	oldCfg := normalizeCardStoreConfig(cardStoreConfig{PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	oldData, _ := json.Marshal(oldCfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(oldData)); err != nil {
		t.Fatal(err)
	}
	payload := `{"enabled":true,"payment_mode":"alipay_direct","alipay_direct":{"app_id":"2021000000000000","gateway_url":"https://openapi.alipay.com/gateway.do","alipay_public_key":` + strconv.Quote(publicKey) + `},"products":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/card-store/config", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	UpdateCardStoreConfigHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := loadCardStoreConfig(context.Background(), system)
	if strings.TrimSpace(stored.AlipayDirect.PrivateKey) != strings.TrimSpace(privateKey) {
		t.Fatalf("stored private key was not preserved")
	}
}

func TestCreateCardStoreOrderStartsPaymentFMOrder(t *testing.T) {
	var seenPath, seenQuery, seenContentType, seenBody string
	var seenForm url.Values
	var orderPersistedBeforePayment bool
	system := newTestLLMServiceSystemSettings()
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		seenContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		seenBody = string(body)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		seenForm = r.Form
		orders := loadCardStoreOrders(context.Background(), system)
		orderPersistedBeforePayment = len(orders.Orders) == 1 && orders.Orders[0].Status == "created"
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "success", "code": 200, "data": map[string]any{"id": "pay-1", "payUrl": "https://pay.example.com/pay?orderNo=pay-1"}})
	}))
	defer paySrv.Close()

	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentAPIBaseURL: paySrv.URL, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success"})
	cfg.Products[0].Price = 10.01
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if seenPath != "/startOrder" || seenQuery == "" || seenBody != "" || seenForm.Get("merchantNum") != "merchant-a" || seenForm.Get("payType") != "aloop" || seenForm.Get("returnType") != "json" {
		t.Fatalf("unexpected payment request path=%q query=%q body=%q form=%v", seenPath, seenQuery, seenBody, seenForm)
	}
	if !strings.Contains(seenContentType, "application/x-www-form-urlencoded") {
		t.Fatalf("content-type = %q", seenContentType)
	}
	if !orderPersistedBeforePayment {
		t.Fatalf("order was not persisted before payment request")
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["pay_url"] != "https://pay.example.com/pay?orderNo=pay-1" {
		t.Fatalf("pay_url = %#v", resp["pay_url"])
	}
}

func TestCreateCardStoreOrderReturnsPaymentFMMessage(t *testing.T) {
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "msg": "sign invalid", "code": 500, "data": nil})
	}))
	defer paySrv.Close()
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentAPIBaseURL: paySrv.URL, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success"})
	cfg.Products[0].Price = 10
	data, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(data))
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "sign invalid") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_failed" || !strings.Contains(orders.Orders[0].PaymentMsg, "sign invalid") {
		t.Fatalf("payment failure order not persisted once: %#v", orders.Orders)
	}
}

func TestCreateCardStoreOrderAllowsUnregisteredEmailWhenTenantExplicit(t *testing.T) {
	identity, cleanup := newBindHandlerIdentity(t)
	defer cleanup()
	system := newTestLLMServiceSystemSettings()
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "success", "code": 200, "data": map[string]any{"id": "pay-unknown", "payUrl": "https://pay.example.com/unknown"}})
	}))
	defer paySrv.Close()

	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentAPIBaseURL: paySrv.URL, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop"})
	cfg.Products[0].Price = 10
	data, _ := json.Marshal(cfg)
	tenantSystem := scopedSystemSettingsForTenant("tenant_store", system)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"unknown@example.com","tenant_id":"tenant_store"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(identity, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), tenantSystem)
	if len(orders.Orders) != 1 || orders.Orders[0].Email != "unknown@example.com" || orders.Orders[0].TenantID != "tenant_store" {
		t.Fatalf("order not saved in explicit tenant: %#v", orders.Orders)
	}
}

func TestPersonalSemimanualOrderOpenSendsAdminReminder(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "alipay", Label: "Alipay", Enabled: true, Payee: "Alice", ImageURL: "https://pay.example.com/alipay.png", AlipayUserID: "2088000000000000"}}}})
	cfg.Products[0].Price = 12.34
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","pay_channel":"alipay"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["payment_mode"] != cardStorePaymentModeManual || created["status"] != cardStoreStatusPersonalCreated || created["pay_code"] == "" || created["pay_qr_url"] == "" {
		t.Fatalf("unexpected manual order response: %#v", created)
	}
	if created["product_id"] != "service_day" || created["product_label"] == "" || created["amount"] != 12.34 {
		t.Fatalf("manual response missing product/amount detail: %#v", created)
	}

	orderNo := created["order_no"].(string)
	mailer := &cardStoreTestMailer{}
	openReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders/"+orderNo+"/payment-opened", strings.NewReader(`{"email":"buyer@example.com"}`))
	openReq.Header.Set("Content-Type", "application/json")
	openReq.SetPathValue("orderNo", orderNo)
	openReq.Host = "hub.example.com"
	openReq.Header.Set("X-Forwarded-Proto", "https")
	openRec := httptest.NewRecorder()
	CardStorePaymentOpenedHandler(system, mailer).ServeHTTP(openRec, openReq)
	if openRec.Code != http.StatusOK {
		t.Fatalf("open status=%d body=%s", openRec.Code, openRec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != cardStoreStatusPersonalOpened || orders.Orders[0].AdminApproveTokenHash == "" || orders.Orders[0].AdminDeleteTokenHash == "" {
		t.Fatalf("order not opened with review tokens: %#v", orders.Orders)
	}
	if mailer.count != 1 || len(mailer.to) != 1 || mailer.to[0] != "admin@example.com" {
		t.Fatalf("admin reminder not sent: count=%d to=%#v", mailer.count, mailer.to)
	}
	for _, want := range []string{orderNo, "12.34", orders.Orders[0].PayCode, "One-time confirm link", "One-time reject/delete link", "https://hub.example.com/card_store/admin/confirm", "https://hub.example.com/card_store/admin/delete", "https://pay.example.com/alipay.png"} {
		if !strings.Contains(mailer.body, want) {
			t.Fatalf("reminder missing %q: %s", want, mailer.body)
		}
	}
}

func TestPersonalSemimanualEmailExpandsRelativeQRURL(t *testing.T) {
	mailer := &cardStoreTestMailer{}
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"owner@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "alipay", Enabled: true, ImageURL: "/api/card-store/payment-qr/default/alipay-test.png"}}}})
	order := cardStoreOrder{OrderNo: "manual-relative-qr", TenantID: store.DefaultTenantID, ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, PayChannelLabel: "Alipay", Payee: "Alice", PayCode: "CS123456", PayQRURL: "/api/card-store/payment-qr/default/alipay-test.png", OpenedPaymentAt: time.Now().UTC()}
	if err := sendCardStorePersonalPaymentReminder(context.Background(), mailer, cfg, order, "https://hub.example.com", "approve-token", "delete-token"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mailer.body, "Payment QR: https://hub.example.com/api/card-store/payment-qr/default/alipay-test.png") {
		t.Fatalf("relative QR URL was not expanded: %s", mailer.body)
	}
}

func TestPersonalSemimanualPaymentOpenedReturnsReminderFailure(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"owner@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "alipay", Label: "Alipay", Enabled: true, ImageURL: "https://pay.example.com/alipay.png"}}}})
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "manual-mail-failed", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: cardStoreStatusPersonalCreated, PaymentMode: cardStorePaymentModeManual, PayChannel: "alipay", PayChannelLabel: "Alipay", Payee: "Alice", PayCode: "CS123456", PayQRURL: "https://pay.example.com/alipay.png", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders/manual-mail-failed/payment-opened", strings.NewReader(`{"email":"buyer@example.com"}`))
	req.SetPathValue("orderNo", "manual-mail-failed")
	rec := httptest.NewRecorder()
	CardStorePaymentOpenedHandler(system, &cardStoreTestMailer{err: fmt.Errorf("smtp down")}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reminder_mail_status":"failed"`) || !strings.Contains(rec.Body.String(), "smtp down") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	lookupReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/manual-mail-failed?email=buyer@example.com", nil)
	lookupReq.SetPathValue("orderNo", "manual-mail-failed")
	lookupRec := httptest.NewRecorder()
	GetCardStoreOrderHandler(system).ServeHTTP(lookupRec, lookupReq)
	if lookupRec.Code != http.StatusOK || !strings.Contains(lookupRec.Body.String(), `"reminder_mail_status":"failed"`) || !strings.Contains(lookupRec.Body.String(), "smtp down") {
		t.Fatalf("lookup status=%d body=%s", lookupRec.Code, lookupRec.Body.String())
	}
}

func TestPersonalSemimanualEmailTokenLinksAreOneTime(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"owner@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, Payee: "Alice", ImageURL: "https://pay.example.com/wx.png"}}}})
	cfg.Products[0].Price = 12.34
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "manual-token-1", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: cardStoreStatusPersonalCreated, PaymentMode: cardStorePaymentModeManual, PayChannel: "wechat", PayChannelLabel: "WeChat Pay", Payee: "Alice", PayCode: "CS123456", PayQRURL: "https://pay.example.com/wx.png", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	mailer := &cardStoreTestMailer{}
	if _, approveToken, _, err := markCardStorePaymentOpened(context.Background(), system, cfg, order.OrderNo, order.Email, "https://hub.example.com", mailer); err != nil {
		t.Fatal(err)
	} else if approveToken == "" || !strings.Contains(mailer.body, "One-time confirm link") {
		t.Fatalf("missing one-time token/link: token=%q body=%s", approveToken, mailer.body)
	} else {
		form := url.Values{"order_no": {order.OrderNo}, "token": {approveToken}, "note": {"ok"}}
		req := httptest.NewRequest(http.MethodPost, "/api/card-store/personal-payment/confirm", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		CardStorePersonalPaymentTokenActionHandler(system, nil, nil, "approve").ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("first token action status=%d body=%s", rec.Code, rec.Body.String())
		}
		retryReq := httptest.NewRequest(http.MethodPost, "/api/card-store/personal-payment/confirm", strings.NewReader(form.Encode()))
		retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		retryRec := httptest.NewRecorder()
		CardStorePersonalPaymentTokenActionHandler(system, nil, nil, "approve").ServeHTTP(retryRec, retryReq)
		if retryRec.Code != http.StatusForbidden {
			t.Fatalf("reused token status=%d body=%s", retryRec.Code, retryRec.Body.String())
		}
	}
}

func TestGetCardStoreProductsReturnsPublicManualChannels(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "alipay", Label: "Alipay", Enabled: true, Payee: "Alice", ImageURL: "https://pay.example.com/alipay.png", AlipayUserID: "2088000000000000"}, {ID: "wechat", Label: "WeChat Pay", Enabled: true, Payee: "Bob", ImageURL: "https://pay.example.com/wx.png"}, {ID: "qq", Label: "QQ Pay", Enabled: true, Payee: "Charlie"}}}})
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/products", nil)
	rec := httptest.NewRecorder()
	GetCardStoreProductsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		PaymentMode     string              `json:"payment_mode"`
		PaymentChannels []map[string]string `json:"payment_channels"`
		Products        []cardStoreProduct  `json:"products"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PaymentMode != cardStorePaymentModeManual || len(body.PaymentChannels) != 2 {
		t.Fatalf("unexpected public channels: %#v", body)
	}
	if strings.Contains(rec.Body.String(), "2088000000000000") || strings.Contains(rec.Body.String(), "pay.example.com") || strings.Contains(rec.Body.String(), "Alice") {
		t.Fatalf("public product response leaked payment details: %s", rec.Body.String())
	}
	defaultLimits := map[string]llmservice.CreditPeriodLimits{
		"service_day":     {FiveHour: 150},
		"service_week":    {FiveHour: 300, Daily: 600},
		"service_month":   {FiveHour: 600, Daily: 1200, Weekly: 2400},
		"service_quarter": {FiveHour: 1200, Daily: 2400, Weekly: 4800, Monthly: 10000},
		"service_year":    {FiveHour: 2400, Daily: 4800, Weekly: 9600, Monthly: 40000},
	}
	for id, want := range defaultLimits {
		product, ok := findCardStoreProduct(cardStoreConfig{Products: body.Products}, id)
		if !ok || product.PeriodLimits != want {
			t.Fatalf("public %s product period limits = %#v, want %#v", id, product.PeriodLimits, want)
		}
	}
	credits, ok := findCardStoreProduct(cardStoreConfig{Products: body.Products}, "credits_10000")
	if !ok || credits.PeriodLimits != (llmservice.CreditPeriodLimits{}) {
		t.Fatalf("public credits product should not have period limits: %#v", credits)
	}
}

func TestCreateCardStoreOrderSelectsEnabledPaymentMethod(t *testing.T) {
	var fmCalled int
	system := newTestLLMServiceSystemSettings()
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmCalled++
		if r.URL.Path != "/startOrder" {
			t.Fatalf("payment path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "success", "code": 200, "data": map[string]any{"id": "pay-1", "payUrl": "https://pay.example.com/pay?orderNo=pay-1"}})
	}))
	defer paySrv.Close()

	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeFM, PaymentMethods: []string{cardStorePaymentModeFM, cardStorePaymentModeManual}, PaymentAPIBaseURL: paySrv.URL, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success", PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, Payee: "Bob", ImageURL: "https://pay.example.com/wx.png"}}}})
	cfg.Products[0].Price = 12.34
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}

	productsReq := httptest.NewRequest(http.MethodGet, "/api/card-store/products", nil)
	productsRec := httptest.NewRecorder()
	GetCardStoreProductsHandler(system).ServeHTTP(productsRec, productsReq)
	if productsRec.Code != http.StatusOK || !strings.Contains(productsRec.Body.String(), `"id":"payment_fm"`) || !strings.Contains(productsRec.Body.String(), `"id":"manual:wechat"`) {
		t.Fatalf("products status=%d body=%s", productsRec.Code, productsRec.Body.String())
	}

	fmReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","payment_method":"payment_fm"}`))
	fmReq.Header.Set("Content-Type", "application/json")
	fmRec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(fmRec, fmReq)
	if fmRec.Code != http.StatusOK || !strings.Contains(fmRec.Body.String(), `"payment_mode":"payment_fm"`) || !strings.Contains(fmRec.Body.String(), `"pay_url":"https://pay.example.com/pay?orderNo=pay-1"`) {
		t.Fatalf("fm status=%d body=%s", fmRec.Code, fmRec.Body.String())
	}

	invalidMethodReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","payment_method":"wechat"}`))
	invalidMethodReq.Header.Set("Content-Type", "application/json")
	invalidMethodRec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(invalidMethodRec, invalidMethodReq)
	if invalidMethodRec.Code != http.StatusBadRequest || !strings.Contains(invalidMethodRec.Body.String(), "CARD_STORE_PAYMENT_METHOD_INVALID") {
		t.Fatalf("invalid method status=%d body=%s", invalidMethodRec.Code, invalidMethodRec.Body.String())
	}

	invalidManualReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","payment_method":"manual:qq"}`))
	invalidManualReq.Header.Set("Content-Type", "application/json")
	invalidManualRec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(invalidManualRec, invalidManualReq)
	if invalidManualRec.Code != http.StatusBadRequest || !strings.Contains(invalidManualRec.Body.String(), "payment channel is not enabled") {
		t.Fatalf("invalid manual status=%d body=%s", invalidManualRec.Code, invalidManualRec.Body.String())
	}

	manualReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","payment_method":"manual:wechat"}`))
	manualReq.Header.Set("Content-Type", "application/json")
	manualRec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(manualRec, manualReq)
	if manualRec.Code != http.StatusOK || !strings.Contains(manualRec.Body.String(), `"payment_mode":"personal_semimanual"`) || !strings.Contains(manualRec.Body.String(), `"pay_channel":"wechat"`) {
		t.Fatalf("manual status=%d body=%s", manualRec.Code, manualRec.Body.String())
	}

	legacyManualReq := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com","pay_channel":"manual:wechat"}`))
	legacyManualReq.Header.Set("Content-Type", "application/json")
	legacyManualRec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(legacyManualRec, legacyManualReq)
	if legacyManualRec.Code != http.StatusOK || !strings.Contains(legacyManualRec.Body.String(), `"payment_mode":"personal_semimanual"`) || !strings.Contains(legacyManualRec.Body.String(), `"pay_channel":"wechat"`) {
		t.Fatalf("legacy manual status=%d body=%s", legacyManualRec.Code, legacyManualRec.Body.String())
	}
	if fmCalled != 1 {
		t.Fatalf("payment fm called %d times, want 1", fmCalled)
	}
}

func TestCreateCardStoreOrderRejectsUnknownPaymentMethodBeforePersist(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "payment_method channel without manual prefix", body: `{"product_id":"service_day","email":"buyer@example.com","payment_method":"wechat"}`},
		{name: "legacy pay_channel unsupported", body: `{"product_id":"service_day","email":"buyer@example.com","pay_channel":"manual:qq"}`},
		{name: "legacy pay_channel unknown", body: `{"product_id":"service_day","email":"buyer@example.com","pay_channel":"bitcoin"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCreateCardStoreOrderRejectsBeforePersist(t, tc.body)
		})
	}
}

func assertCreateCardStoreOrderRejectsBeforePersist(t *testing.T, body string) {
	t.Helper()
	var adapterCalled int
	system := newTestLLMServiceSystemSettings()
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adapterCalled++
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "success", "code": 200})
	}))
	defer paySrv.Close()

	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeFM, PaymentMethods: []string{cardStorePaymentModeFM, cardStorePaymentModeManual}, PaymentAPIBaseURL: paySrv.URL, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success", PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, Payee: "Bob", ImageURL: "https://pay.example.com/wx.png"}}}})
	cfg.Products[0].Price = 12.34
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "CARD_STORE_PAYMENT_METHOD_INVALID") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if adapterCalled != 0 {
		t.Fatalf("payment adapter called %d times, want 0", adapterCalled)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 0 {
		t.Fatalf("invalid payment_method should not create order: %#v", orders.Orders)
	}
}

func TestGetCardStoreProductsReturnsDistinctPublicPaymentMethods(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, PaymentMethods: []string{"bogus", cardStorePaymentModeAlipay, cardStorePaymentModeManual, "bogus"}, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "alipay", Label: "Alipay", Enabled: true, Payee: "Alice", ImageURL: "https://pay.example.com/alipay.png"}}}})
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/products", nil)
	rec := httptest.NewRecorder()
	GetCardStoreProductsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		PaymentChannels []map[string]string `json:"payment_channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.PaymentChannels) != 2 || body.PaymentChannels[0]["id"] != cardStorePaymentModeAlipay || body.PaymentChannels[0]["label"] != "Alipay direct" || body.PaymentChannels[1]["id"] != "manual:alipay" || body.PaymentChannels[1]["label"] != "Alipay QR" {
		t.Fatalf("unexpected public payment methods: %#v", body.PaymentChannels)
	}
	if strings.Contains(rec.Body.String(), "2088000000000000") || strings.Contains(rec.Body.String(), "pay.example.com") || strings.Contains(rec.Body.String(), "Alice") {
		t.Fatalf("public product response leaked payment details: %s", rec.Body.String())
	}
}

func TestPersonalSemimanualOrderRequiresAdminEmailAndQRCode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cfg        cardStoreConfig
		payChannel string
		want       string
		persist    bool
	}{
		{
			name:       "admin email",
			cfg:        cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, ImageURL: "https://pay.example.com/wx.png"}}}},
			payChannel: "wechat",
			want:       "store owner email",
			persist:    true,
		},
		{
			name:       "qr image",
			cfg:        cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true}}}},
			payChannel: "wechat",
			want:       "QR image",
			persist:    true,
		},
		{
			name:       "unsupported channel",
			cfg:        cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{AdminEmails: []string{"admin@example.com"}, Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, ImageURL: "https://pay.example.com/wx.png"}}}},
			payChannel: "bitcoin",
			want:       "payment channel is not supported",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			system := newTestLLMServiceSystemSettings()
			cfg := normalizeCardStoreConfig(tc.cfg)
			cfg.Products[0].Price = 12.34
			data, _ := json.Marshal(cfg)
			if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"product_id":"service_day","email":"buyer@example.com","pay_channel":%q}`, tc.payChannel)
			req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			CreateCardStoreOrderHandler(nil, system, nil, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			orders := loadCardStoreOrders(context.Background(), system)
			if !tc.persist {
				if len(orders.Orders) != 0 {
					t.Fatalf("invalid manual request should not be persisted: %#v", orders.Orders)
				}
				return
			}
			if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_failed" || !strings.Contains(orders.Orders[0].PaymentMsg, tc.want) {
				t.Fatalf("manual config failure was not persisted: %#v", orders.Orders)
			}
		})
	}
}

func TestPersonalSemimanualApprovalRequiresOpenedPayment(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeManual, PersonalPayment: cardStorePersonalPaymentConfig{Channels: []cardStorePersonalPaymentChannel{{ID: "wechat", Label: "WeChat Pay", Enabled: true, Payee: "Alice"}}}})
	cfg.Products[0].Price = 9.99
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "manual-1", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 9.99, Status: cardStoreStatusPersonalCreated, PaymentMode: cardStorePaymentModeManual, PayChannel: "wechat", PayChannelLabel: "WeChat Pay", Payee: "Alice", PayCode: "CS123456", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	update := cardStoreOrder{OrderNo: "manual-1", Amount: 9.99, Status: "paid", PaymentMsg: "personal payment confirmed", PayType: cardStorePaymentModeManual, PaidAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := approveCardStorePersonalOrder(context.Background(), system, cfg, update, nil, nil, store.DefaultTenantID, ""); err == nil || !strings.Contains(err.Error(), "personal_created") {
		t.Fatalf("approve before open err = %v", err)
	}

	if _, _, _, err := markCardStorePaymentOpened(context.Background(), system, cfg, "manual-1", "buyer@example.com", "https://hub.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if err := approveCardStorePersonalOrder(context.Background(), system, cfg, update, nil, nil, store.DefaultTenantID, ""); err != nil {
		t.Fatalf("approve after open: %v", err)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].CardID == "" || orders.Orders[0].Payee != "Alice" {
		t.Fatalf("manual approve did not issue card or preserved payee: %#v", orders.Orders)
	}
	if err := approveCardStorePersonalOrder(context.Background(), system, cfg, update, nil, nil, store.DefaultTenantID, ""); err != nil {
		t.Fatalf("idempotent approve failed: %v", err)
	}
}

func TestCreateCardStoreOrderAlipayDirectUsesComputerWebsitePay(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey, ProductCode: "QUICK_WAP_WAY", SignType: "RSA", PaymentMethod: "wap", SubjectPrefix: "MaClaw Hub"}})
	cfg.Products[0].Price = 12.34
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	payURL, _ := resp["pay_url"].(string)
	if resp["payment_mode"] != cardStorePaymentModeAlipay || !strings.Contains(payURL, "/alipay/pay") {
		t.Fatalf("unexpected alipay order response: %#v", resp)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_started" || orders.Orders[0].PayChannel != "alipay" {
		t.Fatalf("order not started as alipay direct: %#v", orders.Orders)
	}
	if orders.Orders[0].PaymentMsg != "" {
		t.Fatalf("alipay order leaked internal notify URL in message: %#v", orders.Orders[0])
	}
	payReq := httptest.NewRequest(http.MethodGet, payURL, nil)
	payReq.SetPathValue("orderNo", orders.Orders[0].OrderNo)
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
	body := payRec.Body.String()
	if !strings.Contains(body, "alipay.trade.page.pay") || !strings.Contains(body, "FAST_INSTANT_TRADE_PAY") || !strings.Contains(body, `name="sign_type" value="RSA2"`) || strings.Contains(body, "alipay.trade.wap.pay") || strings.Contains(body, "QUICK_WAP_WAY") {
		t.Fatalf("pay page did not use computer website pay: %s", body)
	}
	if !strings.Contains(body, `accept-charset="utf-8"`) || !strings.Contains(body, `action="https://openapi.alipay.com/gateway.do?charset=utf-8"`) {
		t.Fatalf("pay page did not put charset on gateway action: %s", body)
	}
}

func TestRenderAlipaySubmitPageKeepsCharsetOnGatewayQuery(t *testing.T) {
	values := url.Values{}
	values.Set("charset", "utf-8")
	values.Set("app_id", "2021000000000000")
	body := renderAlipaySubmitPage("https://openapi.alipay.com/gateway.do?foo=bar", values)
	if !strings.Contains(body, `action="https://openapi.alipay.com/gateway.do?charset=utf-8&amp;foo=bar"`) {
		t.Fatalf("gateway action missing encoded charset query: %s", body)
	}
}

func TestCreateCardStoreOrderAlipayDirectRejectsInvalidStoredKey(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	_, publicKey := newAlipayTestKeys(t)
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: "not-a-private-key", AlipayPublicKey: publicKey}})
	cfg.Products[0].Price = 12.34
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "private key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 0 {
		t.Fatalf("invalid alipay config should not create order: %#v", orders.Orders)
	}
}

func TestAlipayDirectPayPageAddsTenantToConfiguredNotifyURL(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	tenantID := "tenant_alipay"
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey, NotifyURL: "https://hub.example.com/api/card-store/payment/notify", ReturnURL: "https://hub.example.com/card_store"}})
	cfgData, _ := json.Marshal(cfg)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-TENANT", TenantID: tenantID, ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), tenantSystem, order); err != nil {
		t.Fatal(err)
	}
	payReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-TENANT/alipay/pay?tenant_id="+tenantID, nil)
	payReq.Host = "hub.example.com"
	payReq.Header.Set("X-Forwarded-Proto", "https")
	payReq.SetPathValue("orderNo", "ALI-TENANT")
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
	if !strings.Contains(payRec.Body.String(), `name="notify_url" value="https://hub.example.com/api/card-store/payment/notify?tenant_id=tenant_alipay"`) {
		t.Fatalf("pay page notify_url missing tenant_id: %s", payRec.Body.String())
	}
	if !strings.Contains(payRec.Body.String(), `name="return_url" value="https://hub.example.com/api/card-store/orders/ALI-TENANT/alipay/return?tenant_id=tenant_alipay"`) {
		t.Fatalf("pay page return_url missing tenant-aware alipay return handler: %s", payRec.Body.String())
	}
}

func TestAlipayDirectPayPageForcesReturnThroughOrderHandler(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey, ReturnURL: "https://hub.example.com/custom/thanks"}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-CUSTOM-RETURN", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	payReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-CUSTOM-RETURN/alipay/pay", nil)
	payReq.Host = "hub.example.com"
	payReq.Header.Set("X-Forwarded-Proto", "https")
	payReq.SetPathValue("orderNo", "ALI-CUSTOM-RETURN")
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
	if !strings.Contains(payRec.Body.String(), `name="return_url" value="https://hub.example.com/api/card-store/orders/ALI-CUSTOM-RETURN/alipay/return"`) || strings.Contains(payRec.Body.String(), `custom/thanks`) {
		t.Fatalf("pay page return_url should use order handler, got: %s", payRec.Body.String())
	}
}

func TestAlipayDirectPayPageRebasesStoredStoreReturnURLToCurrentHub(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey, ReturnURL: "https://oldhub.example.com/card_store?tenant_id=tenant_alipay"}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-REBASING-RETURN", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	payReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-REBASING-RETURN/alipay/pay", nil)
	payReq.Host = "newhub.example.com"
	payReq.Header.Set("X-Forwarded-Proto", "https")
	payReq.SetPathValue("orderNo", "ALI-REBASING-RETURN")
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
	if !strings.Contains(payRec.Body.String(), `name="return_url" value="https://newhub.example.com/api/card-store/orders/ALI-REBASING-RETURN/alipay/return"`) || strings.Contains(payRec.Body.String(), `oldhub.example.com`) {
		t.Fatalf("pay page should rebase stale store return_url to current hub, got: %s", payRec.Body.String())
	}
}

func TestAlipayDirectPayPageUsesAlipayNotifyDefault(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-DEFAULT-NOTIFY", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	payReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-DEFAULT-NOTIFY/alipay/pay", nil)
	payReq.SetPathValue("orderNo", "ALI-DEFAULT-NOTIFY")
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
	if !strings.Contains(payRec.Body.String(), `name="notify_url" value="http://example.com/api/card-store/payment/notify"`) || strings.Contains(payRec.Body.String(), `/api/zhifuxpay/notify`) {
		t.Fatalf("pay page should use alipay notify endpoint by default: %s", payRec.Body.String())
	}
}

func TestAlipayDirectPayPageAllowsAlipayWhenNotDefaultPaymentMethod(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeFM, PaymentMethods: []string{cardStorePaymentModeFM, cardStorePaymentModeAlipay}, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-NONDEFAULT", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	payReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-NONDEFAULT/alipay/pay", nil)
	payReq.SetPathValue("orderNo", "ALI-NONDEFAULT")
	payRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(payRec, payReq)
	if payRec.Code != http.StatusOK || !strings.Contains(payRec.Body.String(), "alipay.trade.page.pay") {
		t.Fatalf("pay page status=%d body=%s", payRec.Code, payRec.Body.String())
	}
}

func TestAlipayDirectPayPageRejectsPaidOrDisabledOrders(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	paidOrder := cardStoreOrder{OrderNo: "ALI-PAID", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "paid", PaymentMode: cardStorePaymentModeAlipay, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, paidOrder); err != nil {
		t.Fatal(err)
	}
	paidReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-PAID/alipay/pay", nil)
	paidReq.SetPathValue("orderNo", "ALI-PAID")
	paidRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(paidRec, paidReq)
	if paidRec.Code != http.StatusConflict || !strings.Contains(paidRec.Body.String(), "already paid") {
		t.Fatalf("paid pay page status=%d body=%s", paidRec.Code, paidRec.Body.String())
	}

	cfg.PaymentMode = cardStorePaymentModeFM
	cfg.PaymentMethods = []string{cardStorePaymentModeFM}
	cfgData, _ = json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	openOrder := cardStoreOrder{OrderNo: "ALI-OPEN", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, openOrder); err != nil {
		t.Fatal(err)
	}
	disabledReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-OPEN/alipay/pay", nil)
	disabledReq.SetPathValue("orderNo", "ALI-OPEN")
	disabledRec := httptest.NewRecorder()
	CardStoreAlipayPayPageHandler(system).ServeHTTP(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusConflict || !strings.Contains(disabledRec.Body.String(), "disabled") {
		t.Fatalf("disabled pay page status=%d body=%s", disabledRec.Code, disabledRec.Body.String())
	}
}

func TestAlipayDirectReturnMarksOrderPaidAndRedirectsToStore(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-RETURN", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("auth_app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("out_trade_no", "ALI-RETURN")
	form.Set("trade_no", "2026060122000000000099")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-RETURN/alipay/return?"+form.Encode(), nil)
	req.SetPathValue("orderNo", "ALI-RETURN")
	rec := httptest.NewRecorder()
	CardStoreAlipayReturnHandler(system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/card_store") || !strings.Contains(location, "order_no=ALI-RETURN") || !strings.Contains(location, "email=buyer%40example.com") {
		t.Fatalf("unexpected redirect location: %s", location)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "2026060122000000000099" || orders.Orders[0].CardID == "" {
		t.Fatalf("alipay return did not mark paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectReturnVerifiesAlipayPageReturnSignature(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	tenantID := "vantagics"
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	appID := "2021006157654681"
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: appID, PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "CS20260601095027522285", ProductID: "service_test_10", ProductLabel: "Test Card", Email: "buyer@example.com", Amount: 0.01, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), tenantSystem, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", appID)
	form.Set("auth_app_id", appID)
	form.Set("charset", "utf-8")
	form.Set("method", "alipay.trade.page.pay.return")
	form.Set("out_trade_no", order.OrderNo)
	form.Set("seller_id", "2088002001399080")
	form.Set("sign_type", "RSA2")
	form.Set("timestamp", "2026-06-01 17:50:53")
	form.Set("total_amount", "0.01")
	form.Set("trade_no", "2026060122001457361443083207")
	form.Set("version", "1.0")
	sign, err := alipayRSA2Sign(alipaySignContentSkipping(form, map[string]bool{"sign": true, "sign_type": true}), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/CS20260601095027522285/alipay/return?tenant_id="+tenantID+"&"+form.Encode(), nil)
	req.SetPathValue("orderNo", order.OrderNo)
	rec := httptest.NewRecorder()
	CardStoreAlipayReturnHandler(system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), tenantSystem)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "2026060122001457361443083207" || orders.Orders[0].CardID == "" {
		t.Fatalf("alipay page return did not mark paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectReturnAcceptsTenantIDQueryOutsideSignature(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	tenantID := "tenant_alipay_return"
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-TENANT-RETURN", TenantID: tenantID, ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), tenantSystem, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("out_trade_no", "ALI-TENANT-RETURN")
	form.Set("trade_no", "2026060122000000000101")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-TENANT-RETURN/alipay/return?tenant_id="+tenantID+"&"+form.Encode(), nil)
	req.SetPathValue("orderNo", "ALI-TENANT-RETURN")
	rec := httptest.NewRecorder()
	CardStoreAlipayReturnHandler(system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "tenant_id="+tenantID) || !strings.Contains(location, "order_no=ALI-TENANT-RETURN") {
		t.Fatalf("unexpected redirect location: %s", location)
	}
	orders := loadCardStoreOrders(context.Background(), tenantSystem)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "2026060122000000000101" || orders.Orders[0].CardID == "" {
		t.Fatalf("tenant alipay return did not mark paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectReturnDoesNotMarkUnpaidTradeStatusPaid(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-WAIT", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("out_trade_no", "ALI-WAIT")
	form.Set("trade_no", "2026060122000000000100")
	form.Set("total_amount", "12.34")
	form.Set("trade_status", "WAIT_BUYER_PAY")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-WAIT/alipay/return?"+form.Encode(), nil)
	req.SetPathValue("orderNo", "ALI-WAIT")
	rec := httptest.NewRecorder()
	CardStoreAlipayReturnHandler(system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status == "paid" || orders.Orders[0].CardID != "" {
		t.Fatalf("unpaid alipay return marked paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectReturnInvalidSignatureRedirectsPendingOrder(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	appID := "2021000000000000"
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: appID, PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-BAD-RETURN", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", appID)
	form.Set("charset", "utf-8")
	form.Set("out_trade_no", order.OrderNo)
	form.Set("trade_no", "2026060122000000000999")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	form.Set("sign", "invalid-signature")
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/ALI-BAD-RETURN/alipay/return?"+form.Encode(), nil)
	req.SetPathValue("orderNo", order.OrderNo)
	rec := httptest.NewRecorder()
	CardStoreAlipayReturnHandler(system, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("return status=%d body=%s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "order_no=ALI-BAD-RETURN") || !strings.Contains(location, "payment_return=alipay_verify_pending") {
		t.Fatalf("unexpected redirect location: %s", location)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status == "paid" || orders.Orders[0].CardID != "" {
		t.Fatalf("invalid return signature marked paid: %#v", orders.Orders)
	}
	if !strings.Contains(orders.Orders[0].PaymentMsg, "waiting async notify") {
		t.Fatalf("invalid return signature message missing: %#v", orders.Orders[0])
	}
}

func TestAlipayDirectNotifyMarksOrderPaid(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("out_trade_no", "ALI123")
	form.Set("trade_no", "2026060122000000000001")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base64.StdEncoding.DecodeString(sign); err != nil {
		t.Fatalf("sign is not base64: %v", err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mailer := &cardStoreTestMailer{}
	CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "2026060122000000000001" || orders.Orders[0].CardID == "" {
		t.Fatalf("alipay notify did not mark paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectNotifyAcceptsTenantIDQueryOutsideSignature(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	tenantID := "tenant_alipay"
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-TENANT-NOTIFY", TenantID: tenantID, ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), tenantSystem, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("out_trade_no", "ALI-TENANT-NOTIFY")
	form.Set("trade_no", "2026060122000000000002")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify?tenant_id="+tenantID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), tenantSystem)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "2026060122000000000002" {
		t.Fatalf("tenant alipay notify did not mark paid: %#v", orders.Orders)
	}
}

func TestAlipayDirectNotifyIgnoresUnsignedBodyTenantID(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	tenantID := "tenant_alipay_body"
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := tenantSystem.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-BODY-TENANT", TenantID: tenantID, ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), tenantSystem, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("out_trade_no", "ALI-BODY-TENANT")
	form.Set("trade_no", "2026060122000000000003")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	form.Set("tenant_id", tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "fail" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), tenantSystem)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_started" || orders.Orders[0].PlatformOrderNo != "" {
		t.Fatalf("unsigned body tenant_id changed order: %#v", orders.Orders)
	}
}

func TestAlipayDirectNotifyRejectsInvalidAppSignAndAmount(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	baseOrder := cardStoreOrder{OrderNo: "ALI-BAD", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}

	for _, tc := range []struct {
		name       string
		appID      string
		amount     string
		corruptSig bool
	}{
		{name: "app", appID: "2021999999999999", amount: "12.34"},
		{name: "sign", appID: "2021000000000000", amount: "12.34", corruptSig: true},
		{name: "amount", appID: "2021000000000000", amount: "1.23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = saveCardStoreOrders(context.Background(), system, cardStoreOrders{Orders: []cardStoreOrder{baseOrder}})
			form := url.Values{}
			form.Set("app_id", tc.appID)
			form.Set("charset", "utf-8")
			form.Set("trade_status", "TRADE_SUCCESS")
			form.Set("out_trade_no", "ALI-BAD")
			form.Set("trade_no", "2026060122000000000002")
			form.Set("total_amount", tc.amount)
			form.Set("sign_type", "RSA2")
			sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
			if err != nil {
				t.Fatal(err)
			}
			if tc.corruptSig {
				sign = "bad" + sign
			}
			form.Set("sign", sign)
			req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
			if strings.TrimSpace(rec.Body.String()) != "fail" {
				t.Fatalf("notify body = %q", rec.Body.String())
			}
			orders := loadCardStoreOrders(context.Background(), system)
			if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_started" || orders.Orders[0].CardID != "" {
				t.Fatalf("invalid notify changed order: %#v", orders.Orders)
			}
		})
	}
}

func TestAlipaySignContentExcludesSignButKeepsSignType(t *testing.T) {
	values := url.Values{}
	values.Set("app_id", "2021000000000000")
	values.Set("method", "alipay.trade.page.pay")
	values.Set("sign_type", "RSA2")
	values.Set("sign", "signature")
	got := alipaySignContent(values)
	if !strings.Contains(got, "sign_type=RSA2") {
		t.Fatalf("sign_type missing from sign content: %q", got)
	}
	if strings.Contains(got, "signature") {
		t.Fatalf("sign value leaked into sign content: %q", got)
	}
}

func TestAlipayDirectNotifyRejectsNonAlipayOrder(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "FM123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 12.34, Status: "payment_started", PaymentMode: cardStorePaymentModeFM, PayChannel: "payment_fm", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("out_trade_no", "FM123")
	form.Set("trade_no", "2026060122000000000003")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "fail" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "payment_started" || orders.Orders[0].CardID != "" {
		t.Fatalf("non-alipay order changed: %#v", orders.Orders)
	}
}

func TestAlipayDirectNotifyDoesNotReviveIssueFailedOrder(t *testing.T) {
	privateKey, publicKey := newAlipayTestKeys(t)
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, PaymentMode: cardStorePaymentModeAlipay, AlipayDirect: cardStoreAlipayDirectConfig{AppID: "2021000000000000", PrivateKey: privateKey, AlipayPublicKey: publicKey}})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "ALI-ISSUE-FAILED", ProductID: "missing_product", ProductLabel: "Missing Card", Email: "buyer@example.com", Amount: 12.34, Status: "issue_failed", PaymentMode: cardStorePaymentModeAlipay, PayChannel: "alipay", PaymentMsg: "product not found", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("charset", "utf-8")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("out_trade_no", "ALI-ISSUE-FAILED")
	form.Set("trade_no", "2026060122000000000004")
	form.Set("total_amount", "12.34")
	form.Set("sign_type", "RSA2")
	sign, err := alipayRSA2Sign(alipaySignContent(form), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", sign)
	req := httptest.NewRequest(http.MethodPost, "/api/card-store/payment/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "issue_failed" || orders.Orders[0].CardID != "" || orders.Orders[0].PlatformOrderNo != "" {
		t.Fatalf("issue_failed order changed: %#v", orders.Orders)
	}
}

func TestCreateCardStoreOrderDoesNotOverwriteFastPaidNotify(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success"})
	cfg.Products[0].Price = 10
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	mailer := &cardStoreTestMailer{}
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		orderNo := r.Form.Get("orderNo")
		values := url.Values{}
		values.Set("amount", "10.00")
		values.Set("orderNo", orderNo)
		values.Set("merchantNum", "merchant-a")
		values.Set("state", "1")
		values.Set("sign", zhifuXPayNotifySign("1", "merchant-a", orderNo, "10.00", "key-a"))
		notifyReq := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
		CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(httptest.NewRecorder(), notifyReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "success", "code": 200, "data": map[string]any{"id": "pay-fast", "payUrl": "https://pay.example.com/fast"}})
	}))
	defer paySrv.Close()
	cfg.PaymentAPIBaseURL = paySrv.URL
	cfgData, _ = json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))

	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "paid" || resp["code"] == "" || resp["card_id"] == "" || resp["payment_id"] != "pay-fast" {
		t.Fatalf("fast notify response missing paid card data: %#v", resp)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].CardID == "" || orders.Orders[0].PaymentID != "pay-fast" {
		t.Fatalf("fast notify was overwritten: %#v", orders.Orders)
	}
	if mailer.count != 1 {
		t.Fatalf("mailer count = %d", mailer.count)
	}
}

func TestCreateCardStoreOrderPaymentFailureDoesNotOverwriteFastPaidNotify(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "merchant-a", AccessKey: "key-a", PayType: "aloop", NotifyURL: "https://hub.example.com/success"})
	cfg.Products[0].Price = 10
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	mailer := &cardStoreTestMailer{}
	paySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		orderNo := r.Form.Get("orderNo")
		values := url.Values{}
		values.Set("amount", "10.00")
		values.Set("orderNo", orderNo)
		values.Set("merchantNum", "merchant-a")
		values.Set("state", "1")
		values.Set("sign", zhifuXPayNotifySign("1", "merchant-a", orderNo, "10.00", "key-a"))
		notifyReq := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
		CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(httptest.NewRecorder(), notifyReq)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer paySrv.Close()
	cfg.PaymentAPIBaseURL = paySrv.URL
	cfgData, _ = json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))

	req := httptest.NewRequest(http.MethodPost, "/api/card-store/orders", strings.NewReader(`{"product_id":"service_day","email":"buyer@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateCardStoreOrderHandler(nil, system, nil, paySrv.Client()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "paid" {
		t.Fatalf("response status = %#v", resp)
	}
	if resp["code"] == "" || resp["card_id"] == "" {
		t.Fatalf("response missing paid card data: %#v", resp)
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].CardID == "" {
		t.Fatalf("fast notify was overwritten after payment failure: %#v", orders.Orders)
	}
	if mailer.count != 1 {
		t.Fatalf("mailer count = %d", mailer.count)
	}
}

func TestZhifuXPayNotifyUpdatesOrderWhenSignMatches(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret", PayType: "wxpaynative"})
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	order := cardStoreOrder{OrderNo: "AI123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", SecondaryEmail: "backup@example.com", Amount: 888, Status: "payment_started", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("payee", "test")
	values.Set("amount", "888")
	values.Set("orderNo", "AI123")
	values.Set("actualPayAmount", "888")
	values.Set("payTime", "2026-04-02 11:50:24")
	values.Set("platformOrderNo", "633718715472560128")
	values.Set("merchantNum", "1234567890")
	values.Set("state", "1")
	values.Set("type", "wxpaynative")
	values.Set("tradeType", "wxpaynative")
	values.Set("channelOrderNo", "4200003058202604028462299362")
	values.Set("sign", zhifuXPayNotifySign("1", "1234567890", "AI123", "888", "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	mailer := &cardStoreTestMailer{}
	CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].PlatformOrderNo != "633718715472560128" || orders.Orders[0].CardID == "" || orders.Orders[0].EncryptedCode == "" {
		t.Fatalf("order not marked paid: %#v", orders.Orders)
	}
	if len(mailer.to) != 2 || !strings.Contains(mailer.body, llmservice.DecryptCardCode(orders.Orders[0].EncryptedCode)) {
		t.Fatalf("code email not sent: to=%#v body=%q", mailer.to, mailer.body)
	}
	CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(httptest.NewRecorder(), req)
	reg, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Cards) != 1 || mailer.count != 1 {
		t.Fatalf("duplicate notify was not idempotent: cards=%d mails=%d", len(reg.Cards), mailer.count)
	}
}

func TestZhifuXPayNotifyAutoRedeemsWhenRegisteredEmailExists(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	identity, cleanup := newBindHandlerIdentity(t)
	defer cleanup()
	seedBindUser(t, identity, "tenant_auto", "buyer@example.com")
	serviceReg := llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding", AccessPolicy: llmservice.AccessPolicyGrantRequired}}}
	if err := llmservice.SaveRegistry(context.Background(), scopedSystemSettingsForTenant("tenant_auto", system), &serviceReg); err != nil {
		t.Fatal(err)
	}
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret", PayType: "wxpaynative", ServiceGroupIDs: []string{"coding-basic"}})
	cfgData, _ := json.Marshal(cfg)
	_ = scopedSystemSettingsForTenant("tenant_auto", system).Set(context.Background(), cardStoreConfigKey, string(cfgData))
	order := cardStoreOrder{OrderNo: "AI123", TenantID: "tenant_auto", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 888, Status: "payment_started", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), scopedSystemSettingsForTenant("tenant_auto", system), order); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("amount", "888")
	values.Set("actualPayAmount", "888")
	values.Set("payTime", "2026-04-02 11:50:24")
	values.Set("platformOrderNo", "633718715472560128")
	values.Set("channelOrderNo", "4200003058202604028462299362")
	values.Set("type", "wxpaynative")
	values.Set("orderNo", "AI123")
	values.Set("merchantNum", "1234567890")
	values.Set("state", "1")
	values.Set("tenant_id", "tenant_auto")
	values.Set("sign", zhifuXPayNotifySign("1", "1234567890", "AI123", "888", "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	mailer := &cardStoreTestMailer{}
	CardStorePaymentNotifyHandler(system, mailer, identity).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), scopedSystemSettingsForTenant("tenant_auto", system))
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].AutoRedeemedAt.IsZero() || orders.Orders[0].AutoRedeemError != "" {
		t.Fatalf("order not auto redeemed: %#v", orders.Orders)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), scopedSystemSettingsForTenant("tenant_auto", system))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].Email != "buyer@example.com" || reg.Cards[0].RedeemedByEmail != "buyer@example.com" || reg.Cards[0].RedeemedAt == nil {
		t.Fatalf("registry not redeemed to buyer: grants=%#v cards=%#v", reg.Grants, reg.Cards)
	}
	for _, want := range []string{"服务卡已自动兑换完成", "兑换账户：buyer@example.com", "兑换时间：", "订单号：AI123", "商品：Day Card", "订单金额：888.00", "实付金额：888", "支付时间：2026-04-02 11:50:24", "平台订单号：633718715472560128", "渠道订单号：4200003058202604028462299362", "服务卡 ID：cardstore_AI123"} {
		if !strings.Contains(mailer.body, want) {
			t.Fatalf("auto redeem email detail %q missing: %q", want, mailer.body)
		}
	}
	lookupReq := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/AI123?tenant_id=tenant_auto&email=buyer@example.com", nil)
	lookupReq.SetPathValue("orderNo", "AI123")
	lookupRec := httptest.NewRecorder()
	GetCardStoreOrderHandler(system).ServeHTTP(lookupRec, lookupReq)
	var lookupBody map[string]any
	if err := json.Unmarshal(lookupRec.Body.Bytes(), &lookupBody); err != nil {
		t.Fatal(err)
	}
	if lookupBody["auto_redeemed"] != true || lookupBody["auto_redeemed_at"] == "" {
		t.Fatalf("lookup response missing auto redeem state: %#v", lookupBody)
	}
	CardStorePaymentNotifyHandler(system, mailer, identity).ServeHTTP(httptest.NewRecorder(), req)
	reg, err = llmservice.LoadRegistry(context.Background(), scopedSystemSettingsForTenant("tenant_auto", system))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 {
		t.Fatalf("duplicate notify created extra grants: %#v", reg.Grants)
	}
	if mailer.count != 1 {
		t.Fatalf("mailer count = %d", mailer.count)
	}
}

func TestZhifuXPayNotifySendsCompletionEmailWhenExistingPaidOrderAutoRedeems(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	identity, cleanup := newBindHandlerIdentity(t)
	defer cleanup()
	seedBindUser(t, identity, "tenant_auto", "buyer@example.com")
	code, err := llmservice.GenerateCardCode()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatal(err)
	}
	serviceReg := llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, Cards: []llmservice.RechargeCard{{ID: "cardstore_AI123", CodeHash: llmserviceHashCode(code), EncryptedCode: enc, Label: "Day Card", ServiceGroupIDs: []string{"coding-basic"}, Credits: 300, DurationDays: 1, CreatedAt: time.Now().UTC()}}}
	settings := scopedSystemSettingsForTenant("tenant_auto", system)
	if err := llmservice.SaveRegistry(context.Background(), settings, &serviceReg); err != nil {
		t.Fatal(err)
	}
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret", PayType: "wxpaynative", ServiceGroupIDs: []string{"coding-basic"}})
	cfgData, _ := json.Marshal(cfg)
	_ = settings.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	order := cardStoreOrder{OrderNo: "AI123", TenantID: "tenant_auto", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 888, Status: "paid", CardID: "cardstore_AI123", EncryptedCode: enc, MailStatus: "sent", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), settings, order); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("amount", "888")
	values.Set("actualPayAmount", "888")
	values.Set("payTime", "2026-04-02 11:50:24")
	values.Set("platformOrderNo", "633718715472560128")
	values.Set("channelOrderNo", "4200003058202604028462299362")
	values.Set("orderNo", "AI123")
	values.Set("merchantNum", "1234567890")
	values.Set("state", "1")
	values.Set("tenant_id", "tenant_auto")
	values.Set("sign", zhifuXPayNotifySign("1", "1234567890", "AI123", "888", "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	mailer := &cardStoreTestMailer{}
	CardStorePaymentNotifyHandler(system, mailer, identity).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), settings)
	if len(orders.Orders) != 1 || orders.Orders[0].AutoRedeemedAt.IsZero() || orders.Orders[0].MailStatus != "sent" {
		t.Fatalf("order not auto redeemed with mail sent: %#v", orders.Orders)
	}
	if mailer.count != 1 || !strings.Contains(mailer.subject, "已自动兑换完成") || !strings.Contains(mailer.body, "兑换账户：buyer@example.com") || !strings.Contains(mailer.body, "平台订单号：633718715472560128") || !strings.Contains(mailer.body, "支付时间：2026-04-02 11:50:24") {
		t.Fatalf("completion email not sent: count=%d subject=%q body=%q", mailer.count, mailer.subject, mailer.body)
	}
}

func TestZhifuXPayNotifyPersistsDetailsForExistingPaidOrderWithoutMailer(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret", PayType: "wxpaynative"})
	cfgData, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(cfgData)); err != nil {
		t.Fatal(err)
	}
	code, err := llmservice.GenerateCardCode()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "AI124", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 888, Status: "paid", EncryptedCode: enc, MailStatus: "sent", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("amount", "888")
	values.Set("actualPayAmount", "887.99")
	values.Set("payTime", "2026-04-02 11:50:24")
	values.Set("platformOrderNo", "633718715472560128")
	values.Set("channelOrderNo", "4200003058202604028462299362")
	values.Set("type", "wxpaynative")
	values.Set("orderNo", "AI124")
	values.Set("merchantNum", "1234567890")
	values.Set("state", "1")
	values.Set("sign", zhifuXPayNotifySign("1", "1234567890", "AI124", "888", "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].ActualPayAmount != "887.99" || orders.Orders[0].PayTime != "2026-04-02 11:50:24" || orders.Orders[0].PlatformOrderNo != "633718715472560128" || orders.Orders[0].ChannelOrderNo != "4200003058202604028462299362" || orders.Orders[0].PayType != "wxpaynative" {
		t.Fatalf("payment details not persisted: %#v", orders.Orders)
	}
}

func TestZhifuXPayNotifyRetriesFailedCodeEmail(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret", PayType: "wxpaynative"})
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	code, err := llmservice.GenerateCardCode()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatal(err)
	}
	order := cardStoreOrder{OrderNo: "AI123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 888, Status: "paid", CardID: "cardstore_AI123", EncryptedCode: enc, MailStatus: "failed", MailError: "smtp down", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	values.Set("amount", "888")
	values.Set("orderNo", "AI123")
	values.Set("merchantNum", "1234567890")
	values.Set("state", "1")
	values.Set("sign", zhifuXPayNotifySign("1", "1234567890", "AI123", "888", "secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/zhifuxpay/notify?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	mailer := &cardStoreTestMailer{}
	CardStorePaymentNotifyHandler(system, mailer).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].MailStatus != "sent" || orders.Orders[0].MailError != "" || mailer.count != 1 || !strings.Contains(mailer.body, code) {
		t.Fatalf("mail retry failed: orders=%#v count=%d body=%q", orders.Orders, mailer.count, mailer.body)
	}
}

func TestZhifuXPayNotifyRejectsMerchantAndAmountMismatch(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret"})
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	_ = appendCardStoreOrder(context.Background(), system, cardStoreOrder{OrderNo: "AI123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 888, Status: "payment_started", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	for _, tc := range []struct{ name, merchant, amount string }{{"merchant", "other", "888"}, {"amount", "1234567890", "1"}} {
		form := url.Values{}
		form.Set("state", "1")
		form.Set("merchantNum", tc.merchant)
		form.Set("orderNo", "AI123")
		form.Set("amount", tc.amount)
		form.Set("sign", zhifuXPayNotifySign("1", tc.merchant, "AI123", tc.amount, "secret"))
		req := httptest.NewRequest(http.MethodPost, "/api/zhifuxpay/notify", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
		if strings.TrimSpace(rec.Body.String()) != "fail" {
			t.Fatalf("%s mismatch body = %q", tc.name, rec.Body.String())
		}
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if orders.Orders[0].Status != "payment_started" {
		t.Fatalf("order status changed on invalid notify: %#v", orders.Orders[0])
	}
}

func TestZhifuXPayNotifyReturnsFailWhenSignMismatch(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{Enabled: true, MerchantNum: "1234567890", AccessKey: "secret"})
	cfgData, _ := json.Marshal(cfg)
	_ = system.Set(context.Background(), cardStoreConfigKey, string(cfgData))
	_ = appendCardStoreOrder(context.Background(), system, cardStoreOrder{OrderNo: "AI123", Amount: 888, Status: "payment_started", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	form := url.Values{}
	form.Set("state", "1")
	form.Set("merchantNum", "1234567890")
	form.Set("orderNo", "AI123")
	form.Set("amount", "888")
	form.Set("sign", "bad")
	req := httptest.NewRequest(http.MethodPost, "/api/zhifuxpay/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CardStorePaymentNotifyHandler(system, nil).ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "fail" {
		t.Fatalf("notify body = %q", rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if orders.Orders[0].Status != "payment_started" {
		t.Fatalf("order status changed on bad sign: %#v", orders.Orders[0])
	}
}

func TestGetCardStoreOrderReturnsIssueFailureMessage(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	order := cardStoreOrder{OrderNo: "AI123", ProductID: "service_day", ProductLabel: "Day Card", Email: "buyer@example.com", Amount: 88, Status: "issue_failed", PaymentMsg: "unknown service group", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/card-store/orders/AI123?email=buyer@example.com", nil)
	req.SetPathValue("orderNo", "AI123")
	rec := httptest.NewRecorder()
	GetCardStoreOrderHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "issue_failed" || body["message"] != "unknown service group" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestCardStoreSalesStatsGroupsPaidOrders(t *testing.T) {
	now := time.Now().UTC()
	orders := []cardStoreOrder{
		{OrderNo: "paid-1", Status: "paid", Amount: 10.01, CardID: "card-1", PaidAt: now, AutoRedeemedAt: now.Add(time.Minute)},
		{OrderNo: "paid-2", Status: "paid", Amount: 5.25, EncryptedCode: "enc", PaidAt: now},
		{OrderNo: "issue-1", Status: "issue_failed", Amount: 2.00, PaidAt: now, AutoRedeemError: "redeem failed"},
		{OrderNo: "open-1", Status: "payment_started", Amount: 99, PaidAt: now},
	}
	rows, totalOrders, totalRevenue, totalCards := buildCardStoreSalesStats(orders, "day", 1)
	if totalOrders != 3 || totalRevenue != 17.26 || totalCards != 2 {
		t.Fatalf("totals orders=%d revenue=%.2f cards=%d", totalOrders, totalRevenue, totalCards)
	}
	if len(rows) != 1 || rows[0].Bucket != now.Format("2006-01-02") || rows[0].Orders != 3 || rows[0].Revenue != 17.26 || rows[0].Cards != 2 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	redeemedAt := now.Add(time.Hour)
	sold := buildCardStoreSoldCards(orders, normalizeCardStoreConfig(cardStoreConfig{}), &llmservice.Registry{Cards: []llmservice.RechargeCard{{ID: "card-1", RedeemedByEmail: "redeemer@example.com", RedeemedAt: &redeemedAt}}})
	if len(sold) != 4 {
		t.Fatalf("unexpected sold cards: %#v", sold)
	}
	var sawOpen bool
	if sold[0].OrderNo == "paid-1" && (sold[0].RedeemedEmail != "redeemer@example.com" || sold[0].RedeemedAt == "") {
		t.Fatalf("redeem fields missing: %#v", sold[0])
	}
	for _, card := range sold {
		if card.OrderNo == "open-1" {
			sawOpen = true
		}
		if card.OrderNo == "paid-1" && (!card.AutoRedeemed || card.AutoRedeemAt == "") {
			t.Fatalf("auto redeem fields missing: %#v", card)
		}
		if card.OrderNo == "issue-1" && card.AutoRedeemErr != "redeem failed" {
			t.Fatalf("auto redeem error missing: %#v", card)
		}
	}
	if !sawOpen {
		t.Fatalf("pending order missing from sales cards: %#v", sold)
	}
}

func TestAdminCompleteCardStoreOrderFinishesPendingOrder(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	cfg := normalizeCardStoreConfig(cardStoreConfig{})
	order := cardStoreOrder{OrderNo: "ADMIN-COMPLETE", ProductID: "service_test_10", ProductLabel: "Test Card", Email: "buyer@example.com", Amount: 0.01, Status: "payment_started", PaymentMode: cardStorePaymentModeAlipay, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := appendCardStoreOrder(context.Background(), system, order); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(context.Background(), cardStoreConfigKey, string(data)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/card-store/orders/ADMIN-COMPLETE/complete", strings.NewReader(`{"amount":0.01}`))
	req.SetPathValue("orderNo", "ADMIN-COMPLETE")
	rec := httptest.NewRecorder()
	AdminCompleteCardStoreOrderHandler(system, nil, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	orders := loadCardStoreOrders(context.Background(), system)
	if len(orders.Orders) != 1 || orders.Orders[0].Status != "paid" || orders.Orders[0].EncryptedCode == "" || orders.Orders[0].CardID == "" {
		t.Fatalf("order not completed: %#v", orders.Orders)
	}
}

func TestAdminDeleteCardStoreOrderRemovesOnlyUnpaidOrders(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	orders := cardStoreOrders{Orders: []cardStoreOrder{
		{OrderNo: "PENDING-DELETE", ProductID: "service_test_10", Email: "buyer@example.com", Amount: 0.01, Status: "payment_started", CreatedAt: now, UpdatedAt: now},
		{OrderNo: "PAID-KEEP", ProductID: "service_test_10", Email: "paid@example.com", Amount: 0.01, Status: "paid", EncryptedCode: "enc", CreatedAt: now, UpdatedAt: now},
	}}
	if err := saveCardStoreOrders(context.Background(), system, orders); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/card-store/orders/PENDING-DELETE", nil)
	req.SetPathValue("orderNo", "PENDING-DELETE")
	rec := httptest.NewRecorder()
	AdminDeleteCardStoreOrderHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := findCardStoreOrder(context.Background(), system, "PENDING-DELETE"); ok {
		t.Fatalf("pending order still exists")
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/card-store/orders/PAID-KEEP", nil)
	req.SetPathValue("orderNo", "PAID-KEEP")
	rec = httptest.NewRecorder()
	AdminDeleteCardStoreOrderHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("paid delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := findCardStoreOrder(context.Background(), system, "PAID-KEEP"); !ok {
		t.Fatalf("paid order was deleted")
	}
}
