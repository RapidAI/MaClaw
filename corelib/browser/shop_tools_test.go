package browser

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestShopToolsRegistered(t *testing.T) {
	r := tool.NewRegistry()
	RegisterTools(r)
	for _, n := range []string{"shop_readback", "shop_compare_offers"} {
		if _, ok := r.Get(n); !ok {
			t.Fatalf("%s not registered", n)
		}
	}
}

func TestShopCompareOffersTool(t *testing.T) {
	out := shopCompareOffersTool(map[string]interface{}{"offers": []interface{}{
		map[string]interface{}{"site": "jd", "title": "A", "price": "¥10"},
		map[string]interface{}{"site": "taobao", "title": "B", "price": "¥8", "shipping": "¥1"},
	}})
	if !strings.Contains(out, "1. [taobao] B") || !strings.Contains(out, `"ok":true`) {
		t.Fatal(out)
	}
	if bad := shopCompareOffersTool(map[string]interface{}{"offers": "x"}); !strings.Contains(bad, `"ok":false`) {
		t.Fatal(bad)
	}
}

func TestShopReadbackToolNoSession(t *testing.T) {
	if out := shopReadbackTool(map[string]interface{}{"session_id": "nope"}); !strings.Contains(out, `"ok":false`) {
		t.Fatal(out)
	}
}

func TestFormatShopReadback(t *testing.T) {
	s := formatShopReadback(ShopReadback{Site: "jd", Kind: "cart", Lines: []ShopLine{{Title: "x"}}, Total: "¥1"})
	if s != "jd cart: 1 lines, total ¥1" {
		t.Fatal(s)
	}
}
