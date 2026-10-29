package cardstore

import (
	"encoding/json"
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
			writeAlipayReturnPage(w, r, false, "", "Alipay return verification is not configured.", storeURL, ordersURL)
			return
		}
		values, err := verifyAlipayReturnQuery(r.URL.Query(), svc.alipay.AlipayPublicKey)
		if err != nil {
			storeURL, ordersURL = alipayReturnStoreURLs(r, svc, "")
			writeAlipayReturnPage(w, r, false, "", "Alipay return signature verification failed. Please check the order status in the compute store.", storeURL, ordersURL)
			return
		}
		orderNo := strings.TrimSpace(values.Get("out_trade_no"))
		storeURL, ordersURL = alipayReturnStoreURLs(r, svc, orderNo)
		writeAlipayReturnPage(w, r, true, orderNo, alipayReturnMessage(r, svc, orderNo), storeURL, ordersURL)
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
	return []string{"ctx_order_no", "ctx_email", "ctx_hub_id", "ctx_tenant_id", "ctx_hub_name", "ctx_tenant_name"}
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
	if r != nil {
		ctx := r.URL.Query()
		if q.Get("email") == "" && strings.TrimSpace(ctx.Get("ctx_email")) != "" {
			q.Set("email", strings.TrimSpace(ctx.Get("ctx_email")))
		}
		if q.Get("hub_id") == "" && strings.TrimSpace(ctx.Get("ctx_hub_id")) != "" {
			q.Set("hub_id", strings.TrimSpace(ctx.Get("ctx_hub_id")))
		}
		if q.Get("tenant_id") == "" && strings.TrimSpace(ctx.Get("ctx_tenant_id")) != "" {
			q.Set("tenant_id", strings.TrimSpace(ctx.Get("ctx_tenant_id")))
		}
		if strings.TrimSpace(ctx.Get("ctx_hub_name")) != "" {
			q.Set("hub_name", strings.TrimSpace(ctx.Get("ctx_hub_name")))
		}
		if strings.TrimSpace(ctx.Get("ctx_tenant_name")) != "" {
			q.Set("tenant_name", strings.TrimSpace(ctx.Get("ctx_tenant_name")))
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

func alipayReturnUILang(r *http.Request) string {
	if r == nil {
		return "en"
	}
	tag := r.Header.Get("Accept-Language")
	if i := strings.IndexAny(tag, ",;"); i >= 0 {
		tag = tag[:i]
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch {
	case tag == "mn" || strings.HasPrefix(tag, "mn-mn") || strings.HasPrefix(tag, "mn-cyrl"):
		return "mn"
	case strings.HasPrefix(tag, "zh"):
		return "zh"
	default:
		return "en"
	}
}

// alipayReturnPhrases is the browser-return chrome. A sentence that is not
// listed stays as it was passed in, so a test token is not rewritten.
var alipayReturnPhrases = map[string]struct{ zh, mn string }{
	"Payment returned":                              {"支付已返回", "Төлбөр буцаж ирлээ"},
	"Payment return failed":                         {"支付返回失败", "Төлбөрийн буцаалт амжилтгүй"},
	"Alipay return verification is not configured.": {"尚未配置支付宝返回校验。", "Alipay буцаалтын баталгаажуулалт тохируулаагүй."},
	"Alipay return signature verification failed. Please check the order status in the compute store.": {"支付宝返回签名校验失败。请在算力商店查看订单状态。", "Alipay буцаалтын гарын үсгийн баталгаажуулалт амжилтгүй. Захиалгын төлөвийг тооцооллын дэлгүүрээс шалгана уу."},
	"Payment returned. Credits will activate after the Alipay server notification is received.":        {"支付已返回。收到支付宝服务器通知后，信用点会开通。", "Төлбөр буцаж ирлээ. Alipay серверийн мэдэгдэл ирсний дараа Credits идэвхжинэ."},
	"Order confirmed. Compute credits have been activated.":                                            {"订单已确认。算力信用点已开通。", "Захиалга баталгаажлаа. Тооцооллын Credits идэвхжлээ."},
	"Payment confirmed. Compute credits are activating.":                                               {"支付已确认。算力信用点正在开通。", "Төлбөр баталгаажлаа. Тооцооллын Credits идэвхжиж байна."},
	"Order":                     {"订单号", "Захиалгын дугаар"},
	"Back to the compute store": {"返回算力商店", "Тооцооллын дэлгүүр лүү буцах"},
	"View orders":               {"查看订单", "Захиалга харах"},
}

func alipayReturnLocalized(lang, english string) string {
	row, ok := alipayReturnPhrases[english]
	if !ok {
		return english
	}
	switch lang {
	case "zh":
		return row.zh
	case "mn":
		return row.mn
	default:
		return english
	}
}

func alipayReturnChrome(lang string) (htmlLang, back, orders string) {
	switch lang {
	case "zh":
		return "zh-CN", alipayReturnLocalized("zh", "Back to the compute store"), alipayReturnLocalized("zh", "View orders")
	case "mn":
		return "mn", alipayReturnLocalized("mn", "Back to the compute store"), alipayReturnLocalized("mn", "View orders")
	default:
		return "en", "Back to the compute store", "View orders"
	}
}

type alipayReturnScriptRow struct {
	HTML       string `json:"html"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	OrderLabel string `json:"orderLabel"`
	Back       string `json:"back"`
	Orders     string `json:"orders"`
}

func alipayReturnScriptCopy(status, message, orderNo string) string {
	payload := map[string]alipayReturnScriptRow{}
	for _, lang := range []string{"en", "zh", "mn"} {
		htmlLang, back, orders := alipayReturnChrome(lang)
		orderLabel := ""
		if strings.TrimSpace(orderNo) != "" {
			orderLabel = alipayReturnLocalized(lang, "Order") + ": " + orderNo
		}
		payload[lang] = alipayReturnScriptRow{
			HTML:       htmlLang,
			Status:     alipayReturnLocalized(lang, status),
			Message:    alipayReturnLocalized(lang, message),
			OrderLabel: orderLabel,
			Back:       back,
			Orders:     orders,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func writeAlipayReturnPage(w http.ResponseWriter, r *http.Request, ok bool, orderNo, message, storeURL, ordersURL string) {
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
	lang := alipayReturnUILang(r)
	htmlLang, backLabel, ordersLabel := alipayReturnChrome(lang)
	statusText := alipayReturnLocalized(lang, status)
	messageText := alipayReturnLocalized(lang, message)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	redirectMeta := ""
	if ok {
		redirectMeta = `<meta http-equiv="refresh" content="1;url=` + html.EscapeString(ordersURL) + `">`
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html lang="` + htmlLang + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` + redirectMeta + `<title>` + html.EscapeString(statusText) + `</title><style>body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif;background:#f6f8fb;color:#172033}.wrap{max-width:560px;margin:12vh auto;padding:28px;background:#fff;border:1px solid #dbe3ef;border-radius:10px;box-shadow:0 10px 28px rgba(15,23,42,.07)}h1{margin:0 0 12px;font-size:24px}p{line-height:1.7;color:#667085}.meta{padding:10px 12px;background:#f8fafc;border:1px solid #e5eaf2;border-radius:8px;color:#344054}.actions{display:flex;gap:10px;margin-top:22px}.btn{height:38px;display:inline-flex;align-items:center;padding:0 14px;border-radius:8px;text-decoration:none;font-weight:700}.primary{background:#2563eb;color:#fff}.secondary{border:1px solid #dbe3ef;color:#2563eb;background:#fff}</style></head><body><main class="wrap"><h1>` + html.EscapeString(statusText) + `</h1><p>` + html.EscapeString(messageText) + `</p>`))
	if orderNo != "" {
		_, _ = w.Write([]byte(`<p class="meta">` + html.EscapeString(alipayReturnLocalized(lang, "Order")+": "+orderNo) + `</p>`))
	}
	_, _ = w.Write([]byte(`<div class="actions"><a class="btn primary" href="` + html.EscapeString(storeURL) + `">` + html.EscapeString(backLabel) + `</a><a class="btn secondary" href="` + html.EscapeString(ordersURL) + `">` + html.EscapeString(ordersLabel) + `</a></div></main><script>(function(){var saved="";try{saved=localStorage.getItem("maclaw.computeStore.language")||"";}catch(e){}if(saved!=="en"&&saved!=="zh"&&saved!=="mn")return;var copy=` + alipayReturnScriptCopy(status, message, orderNo) + `;var row=copy[saved];if(!row)return;document.documentElement.lang=row.html;document.title=row.status;var h=document.querySelector("h1");if(h)h.textContent=row.status;var p=document.querySelector("main p");if(p)p.textContent=row.message;var meta=document.querySelector(".meta");if(meta&&row.orderLabel)meta.textContent=row.orderLabel;var links=document.querySelectorAll(".actions a");if(links[0])links[0].textContent=row.back;if(links[1])links[1].textContent=row.orders;})();</script></body></html>`))
}
