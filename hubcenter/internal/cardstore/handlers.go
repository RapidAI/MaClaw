package cardstore

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
)

// ---------------------------------------------------------------------------
// Public endpoints (Hub tenant admins)
// ---------------------------------------------------------------------------

// ListCardTypesHandler returns enabled card types for purchase.
// GET /api/cardstore/types
func ListCardTypesHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		types, err := svc.ListEnabledCardTypes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Return payment channels so the frontend can render payment method selection
		var channels []map[string]any
		for _, ch := range svc.payment.Channels {
			if !ch.Enabled {
				continue
			}
			channels = append(channels, map[string]any{
				"id":           ch.ID,
				"label":        ch.Label,
				"enabled":      ch.Enabled,
				"has_qr":       ch.ImageURL != "",
				"has_bank":     ch.BankName != "",
				"contact_info": ch.ContactInfo,
			})
		}
		paymentMode := ""
		if len(channels) > 0 {
			paymentMode = corecardstore.PaymentModeSemiManual
		}
		if svc.alipay.AppID != "" && len(channels) == 0 {
			paymentMode = corecardstore.PaymentModeAlipay
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"card_types":       types,
			"templates":        BuiltinCardTemplates,
			"payment_channels": channels,
			"payment_mode":     paymentMode,
			"alipay_direct":    svc.alipay.AppID != "",
		})
	}
}

// CreateOrderHandler creates a purchase order.
// POST /api/cardstore/purchase
// Body: { "card_type_id": "...", "admin_email": "...", "hub_id": "...", "tenant_id": "...", "pay_channel": "wechat" }
func CreateOrderHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CardTypeID string `json:"card_type_id"`
			AdminEmail string `json:"admin_email"`
			HubID      string `json:"hub_id"`
			TenantID   string `json:"tenant_id"`
			PayChannel string `json:"pay_channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if req.CardTypeID == "" || req.AdminEmail == "" || req.HubID == "" || req.TenantID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "card_type_id, admin_email, hub_id, tenant_id are required"})
			return
		}

		order, err := svc.CreateOrder(r.Context(), req.CardTypeID, req.AdminEmail, req.HubID, req.TenantID, req.PayChannel)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, order)
	}
}

// ListOrdersHandler lists orders for a hub/tenant.
// GET /api/cardstore/orders?hub_id=X&tenant_id=Y&status=Z&statuses=A,B&offset=0&limit=20
func ListOrdersHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 {
			limit = 20
		}
		offset, _ := strconv.Atoi(q.Get("offset"))

		filter := OrderFilter{
			HubID:          q.Get("hub_id"),
			TenantID:       q.Get("tenant_id"),
			Email:          q.Get("email"),
			ServiceGroupID: q.Get("service_group_id"),
			Status:         q.Get("status"),
			Statuses:       parseStatusList(q),
			ArchivedOnly:   q.Get("archived") == "1" || strings.EqualFold(q.Get("archived"), "true"),
			IncludeArchived: strings.EqualFold(q.Get("include_archived"), "1") ||
				strings.EqualFold(q.Get("include_archived"), "true"),
			Limit:  limit,
			Offset: offset,
		}
		orders, total, err := svc.ListOrders(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"orders": orders,
			"total":  total,
		})
	}
}

func parseStatusList(q url.Values) []string {
	seen := make(map[string]bool)
	var statuses []string
	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			status := strings.TrimSpace(part)
			if status == "" || seen[status] {
				continue
			}
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	for _, raw := range q["statuses"] {
		add(raw)
	}
	if len(statuses) == 0 {
		for _, raw := range q["status"] {
			add(raw)
		}
	}
	return statuses
}

// DeleteOrderHandler deletes an unpaid order owned by a Hub tenant admin.
// DELETE /api/cardstore/orders/:orderNo?email=X&hub_id=Y&tenant_id=Z
func DeleteOrderHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := extractPathParam(r, "orderNo")
		q := r.URL.Query()
		email := q.Get("email")
		hubID := q.Get("hub_id")
		tenantID := q.Get("tenant_id")
		if email == "" || hubID == "" || tenantID == "" {
			var req struct {
				Email    string `json:"email"`
				HubID    string `json:"hub_id"`
				TenantID string `json:"tenant_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if email == "" {
				email = req.Email
			}
			if hubID == "" {
				hubID = req.HubID
			}
			if tenantID == "" {
				tenantID = req.TenantID
			}
		}
		if err := svc.DeleteUnprocessedOrder(r.Context(), orderNo, email, hubID, tenantID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ---------------------------------------------------------------------------
// Admin endpoints (HubCenter admin)
// ---------------------------------------------------------------------------

// AdminListCardTypesHandler returns all card types (including disabled).
// GET /api/admin/cardstore/types
func AdminListCardTypesHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		types, err := svc.ListAllCardTypes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"card_types":     types,
			"templates":      BuiltinCardTemplates,
			"credit_options": DefaultCreditOptions,
		})
	}
}

// AdminCreateCardTypeHandler creates a new card type.
// POST /api/admin/cardstore/types
func AdminCreateCardTypeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ct CardType
		if err := json.NewDecoder(r.Body).Decode(&ct); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if ct.ID == "" {
			ct.ID = generateCardTypeID(ct.Period, ct.Credits)
		}
		if err := svc.CreateCardType(r.Context(), &ct); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ct)
	}
}

// AdminUpdateCardTypeHandler updates a card type.
// PUT /api/admin/cardstore/types/:id
func AdminUpdateCardTypeHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := extractPathParam(r, "id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		var ct CardType
		if err := json.NewDecoder(r.Body).Decode(&ct); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		ct.ID = id
		if err := svc.UpdateCardType(r.Context(), &ct); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ct)
	}
}

// AdminConfirmOrderHandler manually confirms a pending order payment.
// POST /api/admin/cardstore/orders/:orderNo/confirm
func AdminConfirmOrderHandler(svc *Service, getAdminEmail func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := extractPathParam(r, "orderNo")
		if orderNo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "orderNo is required"})
			return
		}
		reviewer := ""
		if getAdminEmail != nil {
			reviewer = getAdminEmail(r)
		}
		if err := svc.ConfirmOrder(r.Context(), orderNo, reviewer); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
	}
}

// AdminListOrdersHandler lists all orders (admin view with all filters).
// GET /api/admin/cardstore/orders?...
func AdminListOrdersHandler(svc *Service) http.HandlerFunc {
	return ListOrdersHandler(svc) // same logic, admin auth handled by middleware
}

// AdminArchiveOrderHandler archives an order so it leaves the active order queue.
// POST /api/admin/cardstore/orders/:orderNo/archive
func AdminArchiveOrderHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderNo := extractPathParam(r, "orderNo")
		if orderNo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "orderNo is required"})
			return
		}
		if err := svc.ArchiveOrder(r.Context(), orderNo); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
	}
}

// TemplatesHandler returns available card templates.
// GET /api/cardstore/templates
func TemplatesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"templates": BuiltinCardTemplates,
		})
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func extractPathParam(r *http.Request, name string) string {
	// Go 1.22+ routing: use PathValue
	if v := r.PathValue(name); v != "" {
		return v
	}
	// Fallback: extract last path segment
	parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func generateCardTypeID(period string, credits float64) string {
	prefix := "ct"
	switch period {
	case "month":
		prefix = "month"
	case "quarter":
		prefix = "quarter"
	case "year":
		prefix = "year"
	}
	c := int(credits)
	if c >= 1000000 {
		return prefix + "_1m"
	}
	if c >= 100000 {
		return prefix + "_100k"
	}
	if c >= 10000 {
		return prefix + "_10k"
	}
	return prefix + "_" + strconv.Itoa(c)
}
