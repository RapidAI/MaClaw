package structureddata

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBusinessIntentUsesSemanticExpenseSignals(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_user"}
	query := "\u6211\u6628\u5929\u53bb\u676d\u5dde\u89c1\u5ba2\u6237\uff0c\u9ad8\u94c1174\uff0c\u5348\u991086\uff0c\u51ed\u8bc1\u5728\u9644\u4ef6\u91cc"
	out, err := svc.ResolveBusinessIntent(context.Background(), p, ResolveBusinessIntentInput{Query: query, Limit: 3})
	if err != nil {
		t.Fatalf("ResolveBusinessIntent: %v", err)
	}
	if len(out.Matches) == 0 {
		t.Fatalf("expected matches: %#v", out)
	}
	top := out.Matches[0]
	if top.BusinessActionID != "finance.expense_submit" {
		t.Fatalf("top action=%q want finance.expense_submit; matches=%#v", top.BusinessActionID, out.Matches)
	}
	if top.BusinessObjectID != "finance.expenses" {
		t.Fatalf("business object=%q want finance.expenses", top.BusinessObjectID)
	}
	if top.Confidence < 0.7 {
		t.Fatalf("confidence=%v want >=0.7; match=%#v", top.Confidence, top)
	}
	if !containsIntentSignal(top.IntentSignals, "expense_context") {
		t.Fatalf("expected expense_context signal: %#v", top.IntentSignals)
	}
}

func TestResolveBusinessIntentKeepsKeywordHintsAsSignalsOnly(t *testing.T) {
	score, matched := scoreBusinessUseCase("expense reimbursement", "finance", BusinessDomainUseCase{
		ID:              "finance.submit_expense",
		Title:           "Submit or update expense",
		IntentHints:     []string{"expense", "reimbursement"},
		PreferredAction: "finance.expense_submit",
	})
	if score <= 0 {
		t.Fatalf("expected lexical hint score")
	}
	for _, item := range matched {
		if strings.HasPrefix(item, "semantic:") {
			t.Fatalf("did not expect semantic signal from plain keyword hint: %#v", matched)
		}
	}
}

func containsIntentSignal(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
