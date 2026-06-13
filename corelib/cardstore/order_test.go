package cardstore

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- in-memory OrderStore for testing ---

type memOrderStore struct {
	orders map[string]*Order
}

func newMemOrderStore() *memOrderStore {
	return &memOrderStore{orders: map[string]*Order{}}
}

func (s *memOrderStore) SaveOrder(order *Order) error {
	s.orders[order.OrderNo] = order
	return nil
}

func (s *memOrderStore) GetOrder(orderNo string) (*Order, error) {
	o := s.orders[orderNo]
	return o, nil
}

func (s *memOrderStore) ListOrders(filter OrderFilter) ([]*Order, error) {
	var result []*Order
	for _, o := range s.orders {
		if filter.Email != "" && o.Email != filter.Email {
			continue
		}
		if filter.Status != "" && o.Status != filter.Status {
			continue
		}
		result = append(result, o)
	}
	return result, nil
}

func (s *memOrderStore) UpdateOrderStatus(orderNo string, status string, updatedAt time.Time) error {
	o := s.orders[orderNo]
	if o == nil {
		return fmt.Errorf("not found")
	}
	o.Status = status
	o.UpdatedAt = updatedAt
	return nil
}

// --- test activator ---

type testActivator struct {
	called    bool
	returnRef string
	returnErr error
}

func (a *testActivator) Activate(order *Order) (string, error) {
	a.called = true
	return a.returnRef, a.returnErr
}

// --- tests ---

func TestGenerateOrderNo(t *testing.T) {
	no1 := GenerateOrderNo("HC")
	no2 := GenerateOrderNo("HC")
	if no1 == no2 {
		t.Fatal("order numbers should be unique")
	}
	if !strings.HasPrefix(no1, "HC") {
		t.Fatalf("expected prefix HC, got %s", no1)
	}
}

func TestConfirmPayment_Success(t *testing.T) {
	store := newMemOrderStore()
	order := &Order{
		OrderNo: "TEST001",
		Status:  StatusPersonalCreated,
		Amount:  99.0,
		Email:   "admin@example.com",
	}
	store.SaveOrder(order)

	activator := &testActivator{returnRef: "auth-123"}
	err := ConfirmPayment(store, activator, "TEST001", "reviewer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !activator.called {
		t.Fatal("activator not called")
	}

	final := store.orders["TEST001"]
	if final.Status != StatusActivated {
		t.Fatalf("expected activated, got %s", final.Status)
	}
	if final.ReviewedBy != "reviewer@example.com" {
		t.Fatalf("expected reviewer, got %s", final.ReviewedBy)
	}
	if final.PaymentID != "auth-123" {
		t.Fatalf("expected auth-123, got %s", final.PaymentID)
	}
}

func TestConfirmPayment_AlreadyPaid(t *testing.T) {
	store := newMemOrderStore()
	order := &Order{OrderNo: "TEST002", Status: StatusPaid}
	store.SaveOrder(order)

	err := ConfirmPayment(store, nil, "TEST002", "admin")
	if err != nil {
		t.Fatal("already-paid should be idempotent")
	}
}

func TestConfirmPayment_InvalidStatus(t *testing.T) {
	store := newMemOrderStore()
	order := &Order{OrderNo: "TEST003", Status: StatusCancelled}
	store.SaveOrder(order)

	err := ConfirmPayment(store, nil, "TEST003", "admin")
	if err == nil {
		t.Fatal("expected error for cancelled order")
	}
}

func TestCancelOrder_Success(t *testing.T) {
	store := newMemOrderStore()
	order := &Order{OrderNo: "TEST004", Status: StatusPending}
	store.SaveOrder(order)

	err := CancelOrder(store, "TEST004")
	if err != nil {
		t.Fatal(err)
	}
	if store.orders["TEST004"].Status != StatusCancelled {
		t.Fatal("expected cancelled")
	}
}

func TestCancelOrder_CannotCancelPaid(t *testing.T) {
	store := newMemOrderStore()
	order := &Order{OrderNo: "TEST005", Status: StatusPaid}
	store.SaveOrder(order)

	err := CancelOrder(store, "TEST005")
	if err == nil {
		t.Fatal("expected error when cancelling paid order")
	}
}

func TestCreateSemiManualOrder(t *testing.T) {
	cfg := &PersonalPaymentConfig{
		AdminEmails: []string{"admin@example.com"},
		Instruction: "请转账后截图发给管理员",
		Channels: []PersonalPaymentChannel{
			{ID: "wechat", Enabled: true, ImageURL: "https://example.com/wechat.png", Label: "微信"},
			{ID: "alipay", Enabled: true, ImageURL: "https://example.com/alipay.png", Label: "支付宝"},
		},
	}

	order := &Order{OrderNo: "TEST006", Amount: 99.0}
	err := CreateSemiManualOrder(order, cfg, "wechat")
	if err != nil {
		t.Fatal(err)
	}
	if order.PaymentMode != PaymentModeSemiManual {
		t.Fatalf("expected personal_semimanual, got %s", order.PaymentMode)
	}
	if order.PayChannel != "wechat" {
		t.Fatalf("expected wechat, got %s", order.PayChannel)
	}
	if order.PayQRURL != "https://example.com/wechat.png" {
		t.Fatalf("expected wechat QR URL, got %s", order.PayQRURL)
	}
	if order.Status != StatusPersonalCreated {
		t.Fatalf("expected personal_created, got %s", order.Status)
	}
}

func TestCreateSemiManualOrder_NoChannels(t *testing.T) {
	cfg := &PersonalPaymentConfig{}
	order := &Order{OrderNo: "TEST007"}
	err := CreateSemiManualOrder(order, cfg, "")
	if err == nil {
		t.Fatal("expected error with no channels")
	}
}
