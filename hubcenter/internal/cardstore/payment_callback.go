package cardstore

import (
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
)

// AlipayNotifyHandler handles Alipay async payment notifications.
// POST /api/cardstore/payment/notify
func AlipayNotifyHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil || strings.TrimSpace(svc.alipay.AlipayPublicKey) == "" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fail"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verify signature
		values, err := corecardstore.VerifyAlipayNotification(string(body), svc.alipay.AlipayPublicKey)
		if err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fail"))
			return
		}

		// Check trade status
		tradeStatus := values.Get("trade_status")
		if tradeStatus != "TRADE_SUCCESS" && tradeStatus != "TRADE_FINISHED" {
			// Not a successful payment; acknowledge but do not process
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("success"))
			return
		}

		// Find order by out_trade_no
		orderNo := strings.TrimSpace(values.Get("out_trade_no"))
		if orderNo == "" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("fail"))
			return
		}
		if err := validateAlipayNotificationOrder(r, svc, orderNo, values); err != nil {
			log.Printf("[cardstore] alipay notify validation failed: order=%s err=%v", orderNo, err)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("success"))
			return
		}

		// Confirm payment (idempotent)
		err = svc.ConfirmOrder(r.Context(), orderNo, "alipay_callback")
		if err != nil {
			// Log but still return success to Alipay (prevent retry storm)
			log.Printf("[cardstore] alipay notify confirm failed: order=%s err=%v", orderNo, err)
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("success"))
	}
}

func validateAlipayNotificationOrder(r *http.Request, svc *Service, orderNo string, values map[string][]string) error {
	if svc == nil || svc.orders == nil {
		return fmt.Errorf("order repository not configured")
	}
	order, err := svc.orders.GetByOrderNo(r.Context(), orderNo)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order not found")
	}
	if order.PaymentMode != corecardstore.PaymentModeAlipay {
		return fmt.Errorf("order payment mode is %s", order.PaymentMode)
	}
	appID := strings.TrimSpace(firstValue(values, "app_id"))
	if strings.TrimSpace(svc.alipay.AppID) != "" && appID != strings.TrimSpace(svc.alipay.AppID) {
		return fmt.Errorf("app_id mismatch: got=%s", appID)
	}
	totalAmount := strings.TrimSpace(firstValue(values, "total_amount"))
	if totalAmount == "" {
		return fmt.Errorf("total_amount is required")
	}
	paid, err := strconv.ParseFloat(totalAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid total_amount %q", totalAmount)
	}
	if absFloat(paid-order.Amount) > 0.01 {
		return fmt.Errorf("amount mismatch: paid=%.2f order=%.2f", paid, order.Amount)
	}
	return nil
}

func firstValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// AlipayReturnHandler handles the browser return from Alipay.
// GET /api/cardstore/payment/return
func AlipayReturnHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeURL, ordersURL := alipayReturnStoreURLs(r, svc, "")
		if svc == nil || strings.TrimSpace(svc.alipay.AlipayPublicKey) == "" {
			writeAlipayReturnPage(w, false, "", "Alipay return verification is not configured.", storeURL, ordersURL)
			return
		}
		values, err := verifyAlipayReturnQuery(r.URL.Query(), svc.alipay.AlipayPublicKey)
		if err != nil {
			storeURL, ordersURL = alipayReturnStoreURLs(r, svc, "")
			writeAlipayReturnPage(w, false, "", "Alipay return signature verification failed. Please check the order status in the compute store.", storeURL, ordersURL)
			return
		}
		orderNo := strings.TrimSpace(values.Get("out_trade_no"))
		storeURL, ordersURL = alipayReturnStoreURLs(r, svc, orderNo)
		writeAlipayReturnPage(w, true, orderNo, alipayReturnMessage(r, svc, orderNo), storeURL, ordersURL)
	}
}

func verifyAlipayReturnQuery(values url.Values, alipayPublicKey string) (url.Values, error) {
	full := cloneValues(values)
	verified, err := corecardstore.VerifyAlipayNotification(full.Encode(), alipayPublicKey)
	if err == nil {
		return verified, nil
	}
	for _, key := range alipayReturnContextKeys() {
		full.Del(key)
	}
	return corecardstore.VerifyAlipayNotification(full.Encode(), alipayPublicKey)
}

func cloneValues(values url.Values) url.Values {
	clone := url.Values{}
	for key, list := range values {
		for _, value := range list {
			clone.Add(key, value)
		}
	}
	return clone
}

func alipayReturnContextKeys() []string {
	return []string{"ctx_order_no", "ctx_email", "ctx_hub_id", "ctx_tenant_id"}
}

func alipayReturnStoreURLs(r *http.Request, svc *Service, orderNo string) (string, string) {
	storeURL := "/compute-store"
	ordersURL := "/compute-store#ordersPanel"
	q := url.Values{}
	if orderNo != "" && svc != nil && svc.orders != nil {
		order, err := svc.orders.GetByOrderNo(r.Context(), orderNo)
		if err == nil && order != nil {
			if strings.TrimSpace(order.Email) != "" {
				q.Set("email", strings.TrimSpace(order.Email))
			}
			if strings.TrimSpace(order.HubID) != "" {
				q.Set("hub_id", strings.TrimSpace(order.HubID))
			}
			if strings.TrimSpace(order.TenantID) != "" {
				q.Set("tenant_id", strings.TrimSpace(order.TenantID))
			}
		}
	}
	if len(q) == 0 && r != nil {
		ctx := r.URL.Query()
		if strings.TrimSpace(ctx.Get("ctx_email")) != "" {
			q.Set("email", strings.TrimSpace(ctx.Get("ctx_email")))
		}
		if strings.TrimSpace(ctx.Get("ctx_hub_id")) != "" {
			q.Set("hub_id", strings.TrimSpace(ctx.Get("ctx_hub_id")))
		}
		if strings.TrimSpace(ctx.Get("ctx_tenant_id")) != "" {
			q.Set("tenant_id", strings.TrimSpace(ctx.Get("ctx_tenant_id")))
		}
	}
	if len(q) == 0 {
		return storeURL, ordersURL
	}
	storeURL = "/compute-store?" + q.Encode()
	ordersURL = storeURL + "#ordersPanel"
	return storeURL, ordersURL
}

func alipayReturnMessage(r *http.Request, svc *Service, orderNo string) string {
	if orderNo == "" || svc == nil || svc.orders == nil {
		return "Payment returned. Credits will activate after the Alipay server notification is received."
	}
	order, err := svc.orders.GetByOrderNo(r.Context(), orderNo)
	if err != nil || order == nil {
		return "Payment returned. Credits will activate after the Alipay server notification is received."
	}
	switch order.Status {
	case corecardstore.StatusActivated:
		return "Order confirmed. Compute credits have been activated."
	case corecardstore.StatusPaid:
		return "Payment confirmed. Compute credits are activating."
	default:
		return "Payment returned. Credits will activate after the Alipay server notification is received."
	}
}

func writeAlipayReturnPage(w http.ResponseWriter, ok bool, orderNo, message, storeURL, ordersURL string) {
	status := "Payment return failed"
	if ok {
		status = "Payment returned"
	}
	if strings.TrimSpace(storeURL) == "" {
		storeURL = "/compute-store"
	}
	if strings.TrimSpace(ordersURL) == "" {
		ordersURL = "/compute-store#ordersPanel"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	redirectMeta := ""
	if ok {
		redirectMeta = `<meta http-equiv="refresh" content="1;url=` + html.EscapeString(ordersURL) + `">`
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` + redirectMeta + `<title>` + html.EscapeString(status) + `</title><style>body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif;background:#f6f8fb;color:#172033}.wrap{max-width:560px;margin:12vh auto;padding:28px;background:#fff;border:1px solid #dbe3ef;border-radius:10px;box-shadow:0 10px 28px rgba(15,23,42,.07)}h1{margin:0 0 12px;font-size:24px}p{line-height:1.7;color:#667085}.meta{padding:10px 12px;background:#f8fafc;border:1px solid #e5eaf2;border-radius:8px;color:#344054}.actions{display:flex;gap:10px;margin-top:22px}.btn{height:38px;display:inline-flex;align-items:center;padding:0 14px;border-radius:8px;text-decoration:none;font-weight:700}.primary{background:#2563eb;color:#fff}.secondary{border:1px solid #dbe3ef;color:#2563eb;background:#fff}</style></head><body><main class="wrap"><h1>` + html.EscapeString(status) + `</h1><p>` + html.EscapeString(message) + `</p>`))
	if orderNo != "" {
		_, _ = w.Write([]byte(`<p class="meta">Order: ` + html.EscapeString(orderNo) + `</p>`))
	}
	_, _ = w.Write([]byte(`<div class="actions"><a class="btn primary" href="` + html.EscapeString(storeURL) + `">&#36820;&#22238;&#31639;&#21147;&#21830;&#24215;</a><a class="btn secondary" href="` + html.EscapeString(ordersURL) + `">&#26597;&#30475;&#35746;&#21333;</a></div></main></body></html>`))
}
