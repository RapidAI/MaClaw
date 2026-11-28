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
	if IsCheckoutControl("支付密码", "") || IsCheckoutControl("请输入密码", "") || IsCheckoutControl("password", "") {
		t.Error("a password field is not the pay button")
	}
	if !IsCheckoutControl("使用密码支付", "") {
		t.Error("a pay button that mentions a password still commits")
	}
	if !IsCheckoutControl("待付款去支付", "") || !IsCheckoutControl("结算并支付", "") {
		t.Error("a pay verb next to a safe phrase still commits")
	}
	if IsCheckoutControl("确认支付方式", "") {
		t.Error("confirming the payment method is not the pay button")
	}
	if !IsCheckoutControl("立即支付方式", "") || !IsCheckoutControl("支付方式", ".pay-btn") {
		t.Error("an immediate pay label or a pay selector still commits")
	}
	if IsCheckoutControl("立即查看支付方式", "") {
		t.Error("viewing the payment method is not the pay button")
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

func TestCheckoutConfirmationMustMatchPayableTotal(t *testing.T) {
	owner := "payable-owner"
	t.Cleanup(func() { ClearCheckoutConfirmation(owner) })
	page := "商品价 ¥11.74\n运费 ¥2.00\n应付总额 ¥9.98"
	if _, ok := RecordCheckoutConfirmation(owner, "确认支付 ¥11.74"); !ok {
		t.Fatal("record")
	}
	if err := consumeCheckoutConfirmation(owner, page); err == nil || !isPolicyDenied(err) {
		t.Fatalf("strikethrough amount approved the order: %v", err)
	}
	if got, ok := PendingCheckoutConfirmation(owner); !ok || got != "11.74" {
		t.Fatalf("mismatch dropped the confirmation %q %v", got, ok)
	}
	ClearCheckoutConfirmation(owner)
	if _, ok := RecordCheckoutConfirmation(owner, "确认支付 ¥9.98"); !ok {
		t.Fatal("record payable")
	}
	if err := consumeCheckoutConfirmation(owner, page); err != nil {
		t.Fatal(err)
	}
}

func TestPageScriptNamesCheckout(t *testing.T) {
	if !pageScriptNamesCheckout(`[...document.querySelectorAll('a')].find(a=>a.innerText.includes('提交订单')).click()`) {
		t.Fatal("script clicking 提交订单 not caught")
	}
	if pageScriptNamesCheckout(`document.querySelector('#J_LinkBasket').click() // 加入购物车`) {
		t.Fatal("add-to-cart script wrongly caught")
	}
	if !pageScriptNamesCheckout(`await click(".pay-btn")`) {
		t.Fatal("pay button selector not caught")
	}
	if !pageScriptNamesCheckout(`document.querySelector('.go-pay').click()`) {
		t.Fatal("go-pay selector not caught")
	}
	if pageScriptNamesCheckout(`await click("#J_LinkBasket")`) {
		t.Fatal("cart selector wrongly caught")
	}
	if pageScriptNamesCheckout(`await click("支付密码")`) {
		t.Fatal("clicking the password field is not the pay button")
	}
	if !pageScriptNamesCheckout(`await click("使用密码支付")`) {
		t.Fatal("a pay button that mentions a password must still be caught")
	}
	if pageScriptNamesCheckout(`await click("支付方式")`) || pageScriptNamesCheckout(`await click("待付款")`) || pageScriptNamesCheckout(`await click("支付宝账号")`) || pageScriptNamesCheckout(`await click("确认支付方式")`) {
		t.Fatal("a payment method, an unpaid order, or an account row is not the pay button")
	}
	if !pageScriptNamesCheckout(`await click("待付款去支付")`) || !pageScriptNamesCheckout(`await click("立即支付方式")`) || !pageScriptNamesCheckout("await click(\"提交 订单\")") {
		t.Fatal("a pay verb beside an unpaid-order or method label must be caught")
	}
	if pageScriptNamesCheckout("await click(\"支付方式\")\n// 立即刷新") {
		t.Fatal("立即 elsewhere must not turn the payment method into pay")
	}
	if !pageScriptNamesCheckout(`await click("支付方式"); await click("立即支付")`) {
		t.Fatal("a later pay click must still be caught")
	}
	if pageScriptNamesCheckout("var t=document.body.innerText; var has=t.includes('提交订单'); document.querySelector('#publish').click()") {
		t.Fatal("reading order text must not poison an unrelated publish click")
	}
	if pageScriptNamesCheckout(`var t=document.body.innerText.replace(/\s+/g,' ');
if(t.includes('提交订单')) console.log(t)
document.querySelector('#publish').click()`) {
		t.Fatal("a read-only checkout string in one statement must not poison another click")
	}
}
