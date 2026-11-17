package browser

import (
	"testing"
	"time"
)

func TestIsCheckoutControl(t *testing.T) {
	block := []string{"提交订单", "立即付款", "确认支付", "去付款", "Place your order", "Pay now", "Buy Now", " 提交 订单 "}
	for _, l := range block {
		if !IsCheckoutControl(l, "") && !IsCheckoutControl(normalizeCheckoutText(l), "") {
			t.Errorf("not blocked: %q", l)
		}
	}
	allow := []string{"加入购物车", "Add to Cart", "去结算", "结算(2)", "待付款", "支付方式", "查看物流", "Proceed to checkout", ""}
	for _, l := range allow {
		if IsCheckoutControl(l, "") {
			t.Errorf("wrongly blocked: %q", l)
		}
	}
	if !IsCheckoutControl("", "#submitOrderPC_1") && !IsCheckoutControl("", "input[name=placeYourOrder1]") {
		t.Error("selector markers not matched")
	}
	if !IsCheckoutControl("", "input[name=placeYourOrder1]") {
		t.Error("amazon place order selector")
	}
}

func TestIsCashierURL(t *testing.T) {
	yes := []string{
		"https://cashier.alipay.com/standard/lightpay/x.htm",
		"https://excashier.alipay.com/standard/auth.htm",
		"https://pay.jd.com/x", "https://cashier.jd.com/payment/pay.action", "https://payc.m.jd.com/",
		"https://wx.tenpay.com/cgi-bin/x", "https://www.amazon.com/gp/buy/spc/handlers/display.html",
		"https://www.amazon.co.jp/checkout/p/x", "https://www.paypal.com/checkoutnow?token=1",
	}
	no := []string{
		"https://cart.taobao.com/cart.htm", "https://cart.jd.com/cart_index", "https://www.amazon.com/gp/cart/view.html",
		"https://item.taobao.com/item.htm?id=1", "https://www.alipay.com/", "https://order.jd.com/center/list.action", "not a url",
	}
	for _, u := range yes {
		if !IsCashierURL(u) {
			t.Errorf("cashier missed: %s", u)
		}
	}
	for _, u := range no {
		if IsCashierURL(u) {
			t.Errorf("not a cashier: %s", u)
		}
	}
}

func TestCheckoutConfirmationBoundToAmount(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	checkoutNow = func() time.Time { return base }
	defer func() { checkoutNow = time.Now }()
	owner := "u1"
	ClearCheckoutConfirmation(owner)

	if err := consumeCheckoutConfirmation(owner, "应付总额 ¥128.50"); err == nil || !isPolicyDenied(err) {
		t.Fatal("no confirmation must block")
	}
	if _, ok := RecordCheckoutConfirmation(owner, "好的"); ok {
		t.Fatal("plain ok is not a confirmation")
	}
	if _, ok := RecordCheckoutConfirmation(owner, "确认支付"); ok {
		t.Fatal("confirmation without amount must not count")
	}
	amt, ok := RecordCheckoutConfirmation(owner, "确认支付 ¥128.5")
	if !ok || amt != "128.50" {
		t.Fatalf("got %q %v", amt, ok)
	}
	if err := consumeCheckoutConfirmation("other", "应付总额 ¥128.50"); err == nil {
		t.Fatal("confirmation leaked to another owner")
	}
	if err := consumeCheckoutConfirmation(owner, "应付总额 ¥139.00"); err == nil {
		t.Fatal("amount mismatch must block")
	}
	if err := consumeCheckoutConfirmation(owner, "实付款：￥128.50 元"); err != nil {
		t.Fatalf("matching amount refused: %v", err)
	}
	if err := consumeCheckoutConfirmation(owner, "实付款：￥128.50"); err == nil {
		t.Fatal("confirmation must be single-use")
	}
	RecordCheckoutConfirmation(owner, "confirm payment $1,299.00")
	checkoutNow = func() time.Time { return base.Add(CheckoutConfirmTTL + time.Second) }
	if err := consumeCheckoutConfirmation(owner, "Order total: $1,299.00"); err == nil {
		t.Fatal("expired confirmation accepted")
	}
}

func TestPageScriptNamesCheckout(t *testing.T) {
	if !pageScriptNamesCheckout(`[...document.querySelectorAll('a')].find(a=>a.innerText.includes('提交订单')).click()`) {
		t.Fatal("script clicking 提交订单 not caught")
	}
	if pageScriptNamesCheckout(`document.querySelector('#J_LinkBasket').click() // 加入购物车`) {
		t.Fatal("add-to-cart script wrongly caught")
	}
}
