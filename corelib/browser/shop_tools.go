package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// shopTools are read-only shopping helpers. Neither clicks or navigates, so
// they cannot place an order or pay; the checkout guard still covers clicks.
func shopTools() []tool.RegisteredTool {
	return []tool.RegisteredTool{
		{
			Name:        "shop_readback",
			Description: "Read back the current Taobao/Tmall, JD or Amazon page in a browser session (cart lines, order list, totals, tracking numbers). Read-only: never clicks, orders or pays.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"shop", "cart", "order", "tracking", "购物车", "订单", "物流", "淘宝", "京东", "amazon"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: shopReadbackTool,
		},
		{
			Name:        "shop_compare_offers",
			Description: "Rank offers for the same product across sites by price plus shipping, per currency (never converts currencies). Pure computation on the offers you pass in.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"shop", "compare", "price", "比价", "价格", "淘宝", "京东", "amazon"},
			Priority:    5,
			Required:    []string{"offers"},
			InputSchema: map[string]interface{}{
				"offers": map[string]interface{}{
					"type":        "array",
					"description": "offers: {site, title, price, shipping?, url?}; price like ¥1299 or $19.99",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"site":     map[string]interface{}{"type": "string"},
							"title":    map[string]interface{}{"type": "string"},
							"price":    map[string]interface{}{"type": "string"},
							"shipping": map[string]interface{}{"type": "string"},
							"url":      map[string]interface{}{"type": "string"},
						},
						"required": []string{"site", "title", "price"},
					},
				},
			},
			Handler: shopCompareOffersTool,
		},
	}
}

func shopReadbackTool(args map[string]interface{}) string {
	s, err := GetAgentSession(strArg(args, "session_id", ""))
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	rb, err := s.ShopReadback()
	if err != nil {
		return marshalBrowserResult(false, llmSafeBrowserError(err), nil)
	}
	return marshalBrowserResult(true, formatShopReadback(rb), map[string]interface{}{"readback": rb})
}

func formatShopReadback(rb ShopReadback) string {
	var b strings.Builder
	site := rb.Site
	if site == "" {
		site = "unknown site"
	}
	fmt.Fprintf(&b, "%s %s: %d lines", site, rb.Kind, len(rb.Lines))
	if rb.Total != "" {
		fmt.Fprintf(&b, ", total %s", rb.Total)
	}
	if len(rb.Tracking) > 0 {
		fmt.Fprintf(&b, ", %d tracking numbers", len(rb.Tracking))
	}
	if len(rb.Lines) == 0 && len(rb.Tracking) == 0 {
		b.WriteString(" (nothing recognized; use browser_observe to read the page)")
	}
	return b.String()
}

func shopCompareOffersTool(args map[string]interface{}) string {
	raw, err := json.Marshal(args["offers"])
	var offers []ShopOffer
	if err == nil {
		err = json.Unmarshal(raw, &offers)
	}
	if err != nil || len(offers) == 0 {
		return marshalBrowserResult(false, "offers must be a non-empty array of {site, title, price}", nil)
	}
	if len(offers) > 100 {
		offers = offers[:100]
	}
	ranked, bad := CompareOffers(offers)
	return marshalBrowserResult(true, FormatOfferComparison(ranked, bad), map[string]interface{}{"ranked": ranked, "unparsed": bad})
}
