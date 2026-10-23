package main

import (
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Read-only shopping actions. Neither clicks, so neither can order or pay.

func desktopShopOffersSchema() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "shop_compare_offers: [{site, title, price, shipping?, url?}], price like ¥1299 or $19.99. Never converts currencies.",
		"items":       map[string]any{"type": "object"},
	}
}

func desktopShopReadback(session *browser.BrowserAgentSession) (string, error) {
	rb, err := session.ShopReadback()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(rb)
	return string(body), nil
}

func desktopShopCompareOffers(args map[string]any) (string, error) {
	raw, err := json.Marshal(args["offers"])
	var offers []browser.ShopOffer
	if err == nil {
		err = json.Unmarshal(raw, &offers)
	}
	if err != nil || len(offers) == 0 {
		return "", fmt.Errorf("shop_compare_offers requires offers: [{site, title, price}]")
	}
	if len(offers) > 100 {
		offers = offers[:100]
	}
	ranked, bad := browser.CompareOffers(offers)
	return browser.FormatOfferComparison(ranked, bad), nil
}
