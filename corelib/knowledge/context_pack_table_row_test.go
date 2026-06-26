package knowledge

import (
	"strings"
	"testing"
)

func TestContextPackFormatsTableRowEvidence(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	pack, err := store.ContextPack(t.Context(), ContextPackOptions{
		SearchOptions: SearchOptions{Query: "张三 法务", Limit: 5},
		MaxItems:      3,
		MaxChars:      1000,
	})
	if err != nil {
		t.Fatalf("ContextPack: %v", err)
	}
	for _, item := range pack.Items {
		if item.ResultType != "table_row" {
			continue
		}
		if !strings.Contains(item.Title, "row 2") {
			t.Fatalf("table row title = %q, want row number", item.Title)
		}
		if !strings.Contains(item.Text, "姓名: 张三") || !strings.Contains(item.Text, "部门: 法务") {
			t.Fatalf("table row text = %q", item.Text)
		}
		return
	}
	t.Fatalf("expected table row item, got %#v", pack.Items)
}
