package cardstore

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"testing"
)

func TestCreateAlipayOrderEscapesBizContent(t *testing.T) {
	order := &Order{
		OrderNo:      "HC-ESCAPE",
		ProductLabel: "Plan \"Pro\" \\ GPU",
		Amount:       12.34,
	}
	_, err := CreateAlipayOrder(order, &AlipayDirectConfig{
		AppID:      "app-1",
		PrivateKey: testAlipayPrivateKeyPEM(t),
	})
	if err != nil {
		t.Fatalf("CreateAlipayOrder: %v", err)
	}
	payURL, err := url.Parse(order.PayURL)
	if err != nil {
		t.Fatalf("parse pay url: %v", err)
	}
	var biz map[string]string
	if err := json.Unmarshal([]byte(payURL.Query().Get("biz_content")), &biz); err != nil {
		t.Fatalf("biz_content is not JSON: %v", err)
	}
	if got := biz["subject"]; got != "MaClaw - Plan \"Pro\" \\ GPU" {
		t.Fatalf("subject = %q", got)
	}
	if got := biz["total_amount"]; got != "12.34" {
		t.Fatalf("total_amount = %q", got)
	}
}

func TestVerifyAlipayNotificationAcceptsPKCS1PublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	params := map[string]string{
		"app_id":       "app-1",
		"out_trade_no": "HC-PKCS1",
		"total_amount": "12.34",
		"trade_status": "TRADE_SUCCESS",
	}
	signStr := buildAlipaySignString(params)
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	sign, err := rsaSign(signStr, privatePEM)
	if err != nil {
		t.Fatalf("rsaSign: %v", err)
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("sign", sign)
	values.Set("sign_type", "RSA2")
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))

	parsed, err := VerifyAlipayNotification(values.Encode(), publicPEM)
	if err != nil {
		t.Fatalf("VerifyAlipayNotification: %v", err)
	}
	if got := parsed.Get("out_trade_no"); got != "HC-PKCS1" {
		t.Fatalf("out_trade_no = %q", got)
	}
}
func TestVerifyAlipayNotificationRejectsTamperedAmount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	params := map[string]string{
		"app_id":       "app-1",
		"out_trade_no": "HC-TAMPER",
		"total_amount": "12.34",
		"trade_status": "TRADE_SUCCESS",
	}
	signStr := buildAlipaySignString(params)
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	sign, err := rsaSign(signStr, privatePEM)
	if err != nil {
		t.Fatalf("rsaSign: %v", err)
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("total_amount", "0.01")
	values.Set("sign", sign)
	values.Set("sign_type", "RSA2")
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))

	if _, err := VerifyAlipayNotification(values.Encode(), publicPEM); err == nil {
		t.Fatal("VerifyAlipayNotification accepted a tampered signed amount")
	}
}
func TestCreateAlipayOrderAcceptsRawPKCS8PrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	order := &Order{OrderNo: "HC-PKCS8", ProductLabel: "Plan", Amount: 1}
	_, err = CreateAlipayOrder(order, &AlipayDirectConfig{
		AppID:      "app-1",
		PrivateKey: base64.StdEncoding.EncodeToString(pkcs8),
	})
	if err != nil {
		t.Fatalf("CreateAlipayOrder: %v", err)
	}
	if order.PayURL == "" {
		t.Fatal("PayURL was empty")
	}
}

func TestVerifyAlipayNotificationAcceptsRawPKCS1PublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	params := map[string]string{
		"app_id":       "app-1",
		"out_trade_no": "HC-RAW-PKCS1",
		"total_amount": "12.34",
		"trade_status": "TRADE_SUCCESS",
	}
	signStr := buildAlipaySignString(params)
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	sign, err := rsaSign(signStr, privatePEM)
	if err != nil {
		t.Fatalf("rsaSign: %v", err)
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("sign", sign)
	values.Set("sign_type", "RSA2")
	rawPublicKey := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey))

	parsed, err := VerifyAlipayNotification(values.Encode(), rawPublicKey)
	if err != nil {
		t.Fatalf("VerifyAlipayNotification: %v", err)
	}
	if got := parsed.Get("out_trade_no"); got != "HC-RAW-PKCS1" {
		t.Fatalf("out_trade_no = %q", got)
	}
}
func testAlipayPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}
