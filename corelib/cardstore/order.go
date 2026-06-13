package cardstore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// OrderStore is the persistence interface for card store orders.
// Hub uses SystemSettings JSON; HubCenter uses a dedicated SQL table.
type OrderStore interface {
	SaveOrder(order *Order) error
	GetOrder(orderNo string) (*Order, error)
	ListOrders(filter OrderFilter) ([]*Order, error)
	UpdateOrderStatus(orderNo string, status string, updatedAt time.Time) error
}

// OrderFilter specifies query parameters for listing orders.
type OrderFilter struct {
	Email      string `json:"email,omitempty"`
	Status     string `json:"status,omitempty"`
	HubID      string `json:"hub_id,omitempty"`      // HubCenter only
	TenantID   string `json:"tenant_id,omitempty"`   // HubCenter only
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

// Activator is the callback interface invoked when an order is confirmed paid.
// Hub implements this to create LLM service grants for users.
// HubCenter implements this to add credits to tenant authorizations.
type Activator interface {
	// Activate is called after payment is confirmed. It should provision the
	// purchased resource (e.g., create a grant, add credits).
	// Returns an activation reference (e.g., card_id, authorization_id).
	Activate(order *Order) (string, error)
}

// GenerateOrderNo creates a unique order number with the given prefix.
func GenerateOrderNo(prefix string) string {
	ts := time.Now().Format("20060102150405")
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%s%s", prefix, ts, hex.EncodeToString(b))
}

// ConfirmPayment transitions an order from pending/created → paid → activated.
// It is the shared state machine used by both payment modes:
//   - personal_semimanual: called by admin manual confirmation
//   - alipay_direct: called by payment callback handler
func ConfirmPayment(store OrderStore, activator Activator, orderNo string, reviewer string) error {
	order, err := store.GetOrder(orderNo)
	if err != nil {
		return fmt.Errorf("get order %s: %w", orderNo, err)
	}
	if order == nil {
		return fmt.Errorf("order %s not found", orderNo)
	}

	// Validate current status allows confirmation
	switch order.Status {
	case StatusPending, StatusPersonalCreated, StatusPersonalOpened:
		// OK — can transition to paid
	case StatusPaid, StatusActivated:
		// Already processed — idempotent
		return nil
	default:
		return fmt.Errorf("order %s has status %s, cannot confirm", orderNo, order.Status)
	}

	now := time.Now().UTC()

	// Mark as paid
	order.Status = StatusPaid
	order.PaidAt = now
	order.ReviewedBy = reviewer
	order.ReviewedAt = now
	order.UpdatedAt = now
	if err := store.UpdateOrderStatus(orderNo, StatusPaid, now); err != nil {
		return fmt.Errorf("update order %s to paid: %w", orderNo, err)
	}

	// Activate (create grant / add credits)
	if activator != nil {
		ref, actErr := activator.Activate(order)
		if actErr != nil {
			// Mark activation failure but keep paid status
			order.PaymentMsg = fmt.Sprintf("activation failed: %v", actErr)
			_ = store.SaveOrder(order)
			return fmt.Errorf("activate order %s: %w", orderNo, actErr)
		}
		order.PaymentID = ref
	}

	// Mark as activated
	order.Status = StatusActivated
	order.UpdatedAt = time.Now().UTC()
	if err := store.SaveOrder(order); err != nil {
		return fmt.Errorf("save activated order %s: %w", orderNo, err)
	}
	return nil
}

// CancelOrder transitions a pending order to cancelled.
func CancelOrder(store OrderStore, orderNo string) error {
	order, err := store.GetOrder(orderNo)
	if err != nil {
		return fmt.Errorf("get order %s: %w", orderNo, err)
	}
	if order == nil {
		return fmt.Errorf("order %s not found", orderNo)
	}
	switch order.Status {
	case StatusPending, StatusPersonalCreated, StatusPersonalOpened:
		// OK — can cancel
	default:
		return fmt.Errorf("order %s has status %s, cannot cancel", orderNo, order.Status)
	}
	now := time.Now().UTC()
	order.Status = StatusCancelled
	order.UpdatedAt = now
	return store.UpdateOrderStatus(orderNo, StatusCancelled, now)
}
