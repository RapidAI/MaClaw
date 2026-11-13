package browser

import (
	"strings"
	"testing"
	"time"
)

func TestCheckoutCommitUsesNameContextAndURL(t *testing.T) {
	order := CheckoutTarget{Name: "提交订单", Context: "收货地址 张三", URL: "https://shop.example/trade/confirm", Submits: true}
	if !order.Commits() {
		t.Fatal("an order-submit control commits on any shop")
	}
	method := CheckoutTarget{Name: "支付方式", Context: "应付 ¥10", URL: "https://shop.example/checkout", Submits: true}
	if method.Commits() {
		t.Fatal("a payment-method control is not the commit")
	}
	settle := CheckoutTarget{Name: "去结算", URL: "https://shop.example/cart", Submits: true}
	if settle.Commits() {
		t.Fatal("opening the cart is not a commit")
	}
	pay := CheckoutTarget{Name: "Pay", Context: "order total $10", URL: "https://shop.example/checkout/pay", Submits: true}
	if !pay.Commits() {
		t.Fatal("a pay control on a checkout URL commits")
	}
	search := CheckoutTarget{Name: "搜索", Key: "Return", Context: "搜索商品", URL: "https://shop.example/", Submits: true}
	if search.Commits() {
		t.Fatal("enter in search is not a purchase")
	}
	enter := CheckoutTarget{Name: "提交订单", Key: "Return", Submits: true}
	if !enter.Commits() {
		t.Fatal("enter on the submit control commits")
	}
	typed := CheckoutTarget{Name: "地址", Key: "Return", Context: "收货地址 应付 ¥10", URL: "https://shop.example/checkout", Submits: true}
	if typed.Commits() {
		t.Fatal("enter in the address field is not the commit control")
	}
	bare := CheckoutTarget{Key: "Return", Context: "应付总额 收货地址", URL: "https://shop.example/checkout", Submits: true}
	if !bare.Commits() {
		t.Fatal("enter on an order page with no readable field still commits")
	}
	script := CheckoutTarget{Text: `document.querySelector('button').click()`, Name: "提交订单", Submits: true}
	if !script.Commits() {
		t.Fatal("a script that clicks the submit control commits")
	}
	look := CheckoutTarget{Name: "提交订单", Submits: false}
	if look.Commits() {
		t.Fatal("naming the control without activating it is not a commit")
	}
}

func TestPreselectedCartItemsAreNotThisRuns(t *testing.T) {
	ClearRunCart("run-cart")
	t.Cleanup(func() { ClearRunCart("run-cart") })
	NoteRunCartAdd("run-cart", "牛奶")
	got := PreselectedCartItems("run-cart", []string{"牛奶", "面包", "牛奶"})
	if len(got) != 1 || got[0] != "面包" {
		t.Fatalf("preselected = %#v", got)
	}
	if other := PreselectedCartItems("run-other", []string{"牛奶"}); len(other) != 1 {
		t.Fatalf("another run's cart = %#v", other)
	}
}

func TestCheckoutQuestionNamesTheControlAndTheExtraItems(t *testing.T) {
	q := CheckoutConfirmQuestion("提交订单", []string{"面包"})
	if !strings.Contains(q, "提交订单") || !strings.Contains(q, "面包") || !strings.Contains(q, "确认支付") {
		t.Fatalf("%s", q)
	}
}

func TestCheckoutReadyDoesNotSpendTheConfirmation(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	checkoutNow = func() time.Time { return base }
	t.Cleanup(func() { checkoutNow = time.Now })
	owner := "tenant:alice"
	ClearCheckoutConfirmation(owner)
	if CheckoutReady(owner, "应付 ¥20.00") {
		t.Fatal("ready without a confirmation")
	}
	if _, ok := RecordCheckoutConfirmation(owner, "确认支付 ¥20"); !ok {
		t.Fatal("record")
	}
	if !CheckoutReady(owner, "应付 ¥20.00") || !CheckoutReady(owner, "应付 ¥20.00") {
		t.Fatal("reading the confirmation spent it")
	}
	if err := TakeCheckoutConfirmation(owner, "应付 ¥20.00"); err != nil {
		t.Fatal(err)
	}
	if CheckoutReady(owner, "应付 ¥20.00") {
		t.Fatal("spent confirmation still ready")
	}
}

func TestParseCheckoutPageAndElement(t *testing.T) {
	page := ParseCheckoutPage(`{"url":"https://shop.example/checkout","context":"应付 ¥9","focused":"提交订单","selected":["面包"]}`)
	if page.URL == "" || page.Focused != "提交订单" || len(page.Selected) != 1 {
		t.Fatalf("%+v", page)
	}
	name, text := ParseCheckoutElement(`{"name":"提交订单","text":"提交订单"}`)
	if name != "提交订单" || text != "提交订单" {
		t.Fatalf("%q %q", name, text)
	}
	if !strings.Contains(CheckoutElementExpr(12, 34), "elementFromPoint") {
		t.Fatal("element expression")
	}
}
