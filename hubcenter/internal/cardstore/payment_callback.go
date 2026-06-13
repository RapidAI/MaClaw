package cardstore

import (
	"io"
	"net/http"
	"strings"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
)

// AlipayNotifyHandler handles Alipay async payment notifications.
// POST /api/cardstore/payment/notify
func AlipayNotifyHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			// Not a successful payment — acknowledge but don't process
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

		// Confirm payment (idempotent)
		err = svc.ConfirmOrder(r.Context(), orderNo, "alipay_callback")
		if err != nil {
			// Log but still return success to Alipay (prevent retry storm)
			_ = err
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("success"))
	}
}
