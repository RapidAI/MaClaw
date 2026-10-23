package browser

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ShopLine is one priced row read back from a cart or order page.
type ShopLine struct {
	Title  string `json:"title"`
	Price  string `json:"price,omitempty"`
	Qty    int    `json:"qty,omitempty"`
	Status string `json:"status,omitempty"`
}

// ShopTracking is one shipment number found on an order/logistics page.
type ShopTracking struct {
	Carrier string `json:"carrier,omitempty"`
	Number  string `json:"number"`
	Status  string `json:"status,omitempty"`
}

// ShopReadback is a read-only summary of what a shopping page shows.
type ShopReadback struct {
	Site     string         `json:"site"`
	Kind     string         `json:"kind"` // cart | orders | tracking | page
	Lines    []ShopLine     `json:"lines,omitempty"`
	Total    string         `json:"total,omitempty"`
	Tracking []ShopTracking `json:"tracking,omitempty"`
}

// ShopSite maps a URL to taobao | jd | amazon | "" (unknown).
func ShopSite(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(h, "taobao.com") || strings.HasSuffix(h, "tmall.com"):
		return "taobao"
	case strings.HasSuffix(h, "jd.com"):
		return "jd"
	case strings.Contains(h, "amazon."):
		return "amazon"
	}
	return ""
}

func shopKind(raw string, text string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "cart") || strings.Contains(l, "/gp/cart"):
		return "cart"
	case strings.Contains(l, "logistics") || strings.Contains(l, "progress-tracker") || strings.Contains(l, "track"):
		return "tracking"
	case strings.Contains(l, "order") || strings.Contains(l, "bought") || strings.Contains(l, "your-orders"):
		return "orders"
	case strings.Contains(text, "购物车") || strings.Contains(text, "Shopping Cart"):
		return "cart"
	}
	return "page"
}

var (
	shopPriceRe   = regexp.MustCompile(`(?:[¥￥]|US\$|\$|CNY\s?|RMB\s?)\s?\d[\d,]*(?:\.\d{1,2})?`)
	shopQtyRe     = regexp.MustCompile(`(?i)(?:[x×]\s?(\d{1,3})\b|数量[:：]?\s?(\d{1,3})|qty[:：]?\s?(\d{1,3}))`)
	shopTotalRe   = regexp.MustCompile(`(?i)(?:合计|总计|实付款|应付总额|实付|subtotal|order total|total)[^\d¥￥$]{0,12}((?:[¥￥]|US\$|\$)\s?\d[\d,]*(?:\.\d{1,2})?)`)
	shopTrackRe   = regexp.MustCompile(`(?i)(?:运单号|快递单号|物流单号|tracking (?:id|number)|tracking#?)[:：\s]*([A-Z0-9]{8,30})`)
	shopCarrierRe = regexp.MustCompile(`(顺丰|中通|圆通|申通|韵达|京东物流|极兔|邮政|EMS|UPS|FedEx|USPS|DHL|Amazon Logistics)`)
	shopStatusRe  = regexp.MustCompile(`(?i)(待付款|待发货|已发货|待收货|运输中|派送中|已签收|交易成功|已取消|Delivered|Shipped|Out for delivery|Arriving [A-Za-z]+ ?\d*|Cancelled)`)
)

// ParseShopPage extracts cart lines, totals and tracking from page text.
// It is best effort: each line is the nearest non-price text above a price.
func ParseShopPage(rawURL, text string) ShopReadback {
	rb := ShopReadback{Site: ShopSite(rawURL), Kind: shopKind(rawURL, text)}
	rows := strings.Split(text, "\n")
	lastTitle := ""
	for i, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		if shopTotalRe.MatchString(row) {
			if m := shopTotalRe.FindStringSubmatch(row); m != nil {
				rb.Total = strings.ReplaceAll(m[1], " ", "")
			}
			continue
		}
		price := shopPriceRe.FindString(row)
		if price == "" {
			if len([]rune(row)) >= 6 && !shopStatusRe.MatchString(row) && !shopTrackRe.MatchString(row) {
				lastTitle = row
			}
			continue
		}
		title := strings.TrimSpace(shopPriceRe.ReplaceAllString(row, ""))
		if len([]rune(title)) < 6 {
			title = lastTitle
		}
		if title == "" {
			continue
		}
		line := ShopLine{Title: truncateRunes(title, 120), Price: strings.ReplaceAll(price, " ", "")}
		window := row
		for j := i + 1; j < len(rows) && j <= i+2; j++ {
			window += "\n" + rows[j]
		}
		if m := shopQtyRe.FindStringSubmatch(window); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					fmt.Sscanf(g, "%d", &line.Qty)
				}
			}
		}
		if s := shopStatusRe.FindString(window); s != "" {
			line.Status = s
		}
		rb.Lines = append(rb.Lines, line)
		lastTitle = ""
		if len(rb.Lines) >= 50 {
			break
		}
	}
	for _, m := range shopTrackRe.FindAllStringSubmatchIndex(text, 10) {
		t := ShopTracking{Number: text[m[2]:m[3]]}
		lo, hi := m[0]-80, m[1]+80
		if lo < 0 {
			lo = 0
		}
		if hi > len(text) {
			hi = len(text)
		}
		ctx := text[lo:hi]
		t.Carrier = shopCarrierRe.FindString(ctx)
		t.Status = shopStatusRe.FindString(ctx)
		rb.Tracking = append(rb.Tracking, t)
	}
	return rb
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ShopReadback reads the current page and summarizes it. Read-only: it never
// clicks, so it cannot place or pay for an order.
func (s *BrowserAgentSession) ShopReadback() (ShopReadback, error) {
	if s == nil || s.session == nil {
		return ShopReadback{}, fmt.Errorf("no browser session")
	}
	href, err := s.session.Eval(`location.href`)
	if err != nil {
		return ShopReadback{}, err
	}
	return ParseShopPage(strings.Trim(href, `"`), s.checkoutPageText()), nil
}
