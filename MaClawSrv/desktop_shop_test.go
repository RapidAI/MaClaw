package main

import (
	"strings"
	"testing"
)

func TestDesktopShopActionsAreReadOnlyVocabulary(t *testing.T) {
	for _, a := range []string{"shop_readback", "shop_compare_offers"} {
		if !desktopActionAllowed(a) || !desktopPlanActionAllowed(a) {
			t.Fatalf("%s must be allowed in execute and plan", a)
		}
	}
}

func TestDesktopShopCompareOffers(t *testing.T) {
	out, err := desktopShopCompareOffers(map[string]any{"offers": []any{
		map[string]any{"site": "jd", "title": "A", "price": "¥10"},
		map[string]any{"site": "taobao", "title": "B", "price": "¥8", "shipping": "¥1"},
	}})
	if err != nil || !strings.Contains(out, "1. [taobao] B") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := desktopShopCompareOffers(map[string]any{}); err == nil {
		t.Fatal("empty offers must fail")
	}
}

func TestDesktopShopReadbackNilSession(t *testing.T) {
	if _, err := desktopShopReadback(nil); err == nil {
		t.Fatal("nil session must fail")
	}
}
