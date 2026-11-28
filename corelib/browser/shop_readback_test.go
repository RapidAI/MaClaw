package browser

import (
	"strings"
	"testing"
)

func TestShopSiteAndKind(t *testing.T) {
	cases := map[string][2]string{
		"https://cart.taobao.com/cart.htm":                                   {"taobao", "cart"},
		"https://buyertrade.taobao.com/trade/itemlist/list_bought_items.htm": {"taobao", "orders"},
		"https://order.jd.com/center/list.action":                            {"jd", "orders"},
		"https://www.amazon.com/progress-tracker/package":                    {"amazon", "tracking"},
		"https://example.com/":                                               {"", "page"},
	}
	for u, want := range cases {
		rb := ParseShopPage(u, "")
		if rb.Site != want[0] || rb.Kind != want[1] {
			t.Fatalf("%s -> %s/%s", u, rb.Site, rb.Kind)
		}
	}
}

func TestParseShopPageCart(t *testing.T) {
	text := "购物车\n小米 充电宝 20000mAh 快充版\n¥129.00\n数量: 2\n罗技 无线鼠标 M720\n￥ 199.50 x1\n合计: ¥457.50\n"
	rb := ParseShopPage("https://cart.taobao.com/cart.htm", text)
	if len(rb.Lines) != 2 || rb.Lines[0].Price != "¥129.00" || rb.Lines[0].Qty != 2 || !strings.Contains(rb.Lines[0].Title, "充电宝") {
		t.Fatalf("%+v", rb.Lines)
	}
	if rb.Lines[1].Price != "￥199.50" || rb.Lines[1].Qty != 1 || rb.Total != "¥457.50" {
		t.Fatalf("%+v total=%s", rb.Lines[1], rb.Total)
	}
}

func TestParseShopPageTracking(t *testing.T) {
	text := "订单号 123\nAnker USB-C Cable 6ft\n$12.99\nShipped\n承运: 顺丰 运单号: SF1234567890123 运输中\nTracking ID: TBA987654321000 Amazon Logistics Delivered"
	rb := ParseShopPage("https://www.amazon.com/gp/your-account/order-history", text)
	if rb.Kind != "orders" || len(rb.Lines) != 1 || rb.Lines[0].Status != "Shipped" {
		t.Fatalf("%+v", rb)
	}
	if len(rb.Tracking) != 2 || rb.Tracking[0].Number != "SF1234567890123" || rb.Tracking[0].Carrier != "顺丰" || rb.Tracking[1].Number != "TBA987654321000" {
		t.Fatalf("%+v", rb.Tracking)
	}
}

func TestCompareOffers(t *testing.T) {
	ranked, bad := CompareOffers([]ShopOffer{
		{Site: "jd", Title: "A", Price: "¥1,299.00"},
		{Site: "taobao", Title: "B", Price: "¥1,250", Shipping: "¥60"},
		{Site: "taobao", Title: "C", Price: "1199"},
		{Site: "amazon", Title: "D", Price: "$179.99", Shipping: "$5"},
		{Site: "jd", Title: "E", Price: "面议"},
		{Site: "amazon", Title: "F", Price: "$100", Shipping: "¥5"},
	})
	cny := ranked["CNY"]
	if len(cny) != 3 || cny[0].Title != "C" || cny[1].Title != "A" || cny[2].Landed != 1310 {
		t.Fatalf("%+v", cny)
	}
	if usd := ranked["USD"]; len(usd) != 1 || usd[0].Landed != 184.99 {
		t.Fatalf("%+v", usd)
	}
	if len(bad) != 2 {
		t.Fatalf("bad %+v", bad)
	}
	out := FormatOfferComparison(ranked, bad)
	if !strings.Contains(out, "不同币种未换算") || !strings.Contains(out, "1. [taobao] C") {
		t.Fatal(out)
	}
}

func TestParseShopPagePaymentSuccess(t *testing.T) {
	text := "支付成功\n送至: 北京海淀区上地街道上地东路上 马勇 170****4628\n商品: 共1件\n订单号: 3696476003659609\n实付: ¥45.80\n等待发货\n"
	rb := ParseShopPage("https://trade.jd.com/shopping/orderSuccess?orderId=3696476003659609&channelCode=x", text)
	if rb.Kind != "orders" || rb.PaymentStatus != "支付成功" {
		t.Fatalf("%+v", rb)
	}
	if len(rb.OrderIDs) != 1 || rb.OrderIDs[0] != "3696476003659609" {
		t.Fatalf("order ids %+v", rb.OrderIDs)
	}
	if rb.Address == "" || rb.ItemCount != 1 || rb.Total != "¥45.80" {
		t.Fatalf("%+v", rb)
	}
}

func TestOrderSuccessSuppressesPersonWallFlagsInScripts(t *testing.T) {
	for _, script := range []string{browserPageFlagsCollectJS, wrapPageScript("return true", "jd.com")} {
		for _, marker := range []string{"order_success: orderSuccess", "mfa: !orderSuccess", "payment_confirm: !orderSuccess"} {
			if !strings.Contains(script, marker) {
				t.Fatalf("script missing %q", marker)
			}
		}
	}
	if !strings.Contains(browserPageFlagsCollectJS, "signedInChrome = /退出登录|已登录/.test(fullText) || orderSuccess") {
		t.Fatal("peek flags do not treat order success as a terminal signed-in state")
	}
	if !strings.Contains(wrapPageScript("return true", "jd.com"), "const signedIn = orderSuccess") {
		t.Fatal("page-script flags do not treat order success as a terminal signed-in state")
	}
}
