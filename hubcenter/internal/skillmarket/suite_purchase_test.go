package skillmarket

import (
	"context"
	"testing"
)

func TestSuitePurchaseRefundAndAuditLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	rec := &SuitePurchaseRecord{ID: "suite-purchase-1", SuiteID: "office", MemberSkillIDs: []string{"pdf", "sheet"}, BuyerEmail: "buyer@example.com", BuyerID: "buyer-1", AmountPaid: 20, Version: "1.0.0"}
	if err := store.CreateSuitePurchase(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetActiveSuitePurchase(ctx, "office", "buyer-1")
	if err != nil || got.ID != rec.ID {
		t.Fatalf("active purchase = %#v, err=%v", got, err)
	}
	if err := store.RecordSuiteAuditEvent(ctx, "office", "purchase", "buyer-1", rec.ID, rec.MemberSkillIDs); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuitePurchaseRefunded(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	refunded, err := store.GetSuitePurchaseByID(ctx, rec.ID)
	if err != nil || refunded.Status != "refunded" {
		t.Fatalf("refund status = %#v, err=%v", refunded, err)
	}
	events, err := store.ListSuiteAuditEventsFiltered(ctx, "office", "purchase", 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("audit events = %#v, err=%v", events, err)
	}
}
