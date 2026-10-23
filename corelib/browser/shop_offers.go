package browser

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ShopOffer is one listing of the same product on one site.
type ShopOffer struct {
	Site     string `json:"site"`
	Title    string `json:"title"`
	Price    string `json:"price"`
	Shipping string `json:"shipping,omitempty"`
	URL      string `json:"url,omitempty"`
}

// RankedOffer adds the parsed landed cost.
type RankedOffer struct {
	ShopOffer
	Currency string  `json:"currency"`
	Landed   float64 `json:"landed"`
}

// ParseMoney returns currency (CNY/USD) and amount from "¥1,299.00" etc.
func ParseMoney(s string) (string, float64, bool) {
	s = strings.TrimSpace(s)
	cur := ""
	switch {
	case strings.HasPrefix(s, "¥"), strings.HasPrefix(s, "￥"):
		cur = "CNY"
	case strings.HasPrefix(s, "US$"), strings.HasPrefix(s, "$"):
		cur = "USD"
	case strings.HasPrefix(strings.ToUpper(s), "CNY"), strings.HasPrefix(strings.ToUpper(s), "RMB"):
		cur = "CNY"
	}
	num := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' {
			return r
		}
		return -1
	}, s)
	if num == "" {
		return cur, 0, false
	}
	v, err := strconv.ParseFloat(num, 64)
	return cur, v, err == nil
}

// CompareOffers ranks offers by price+shipping within each currency. Offers
// in different currencies are never converted or mixed; unparseable ones are
// dropped into the second return value.
func CompareOffers(offers []ShopOffer) (map[string][]RankedOffer, []ShopOffer) {
	out := map[string][]RankedOffer{}
	var bad []ShopOffer
	for _, o := range offers {
		cur, p, ok := ParseMoney(o.Price)
		if !ok {
			bad = append(bad, o)
			continue
		}
		if cur == "" {
			cur = defaultCurrency(o.Site)
		}
		landed := p
		if o.Shipping != "" {
			scur, sp, sok := ParseMoney(o.Shipping)
			if !sok || (scur != "" && scur != cur) {
				bad = append(bad, o)
				continue
			}
			landed += sp
		}
		out[cur] = append(out[cur], RankedOffer{ShopOffer: o, Currency: cur, Landed: landed})
	}
	for _, list := range out {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Landed < list[j].Landed })
	}
	return out, bad
}

func defaultCurrency(site string) string {
	if site == "amazon" {
		return "USD"
	}
	return "CNY"
}

// FormatOfferComparison renders a short ranked table for the user.
func FormatOfferComparison(ranked map[string][]RankedOffer, bad []ShopOffer) string {
	var b strings.Builder
	curs := make([]string, 0, len(ranked))
	for c := range ranked {
		curs = append(curs, c)
	}
	sort.Strings(curs)
	for _, c := range curs {
		fmt.Fprintf(&b, "%s:\n", c)
		for i, o := range ranked[c] {
			fmt.Fprintf(&b, "%d. [%s] %s — %.2f", i+1, o.Site, o.Title, o.Landed)
			if o.Shipping != "" {
				fmt.Fprintf(&b, " (含运费 %s)", o.Shipping)
			}
			b.WriteString("\n")
		}
	}
	if len(curs) > 1 {
		b.WriteString("不同币种未换算，请勿直接比较。\n")
	}
	for _, o := range bad {
		fmt.Fprintf(&b, "无法解析价格: [%s] %s %q\n", o.Site, o.Title, o.Price)
	}
	return b.String()
}
