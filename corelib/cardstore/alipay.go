package cardstore

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// CreateAlipayOrder generates an Alipay trade creation request URL for a given order.
// Returns the full pay URL that the buyer should be redirected to.
func CreateAlipayOrder(order *Order, cfg *AlipayDirectConfig) (string, error) {
	if cfg == nil || cfg.AppID == "" {
		return "", fmt.Errorf("alipay direct config not configured")
	}
	if cfg.PrivateKey == "" {
		return "", fmt.Errorf("alipay private key not configured")
	}

	gateway := cfg.GatewayURL
	if gateway == "" {
		gateway = DefaultAlipayGatewayURL
	}

	subject := cfg.SubjectPrefix
	if subject == "" {
		subject = "MaClaw"
	}
	subject = subject + " - " + order.ProductLabel

	// Build biz_content
	bizContentBytes, err := json.Marshal(map[string]string{
		"out_trade_no": order.OrderNo,
		"total_amount": fmt.Sprintf("%.2f", order.Amount),
		"subject":      subject,
		"product_code": alipayProductCode(cfg),
	})
	if err != nil {
		return "", fmt.Errorf("marshal alipay biz_content: %w", err)
	}
	bizContent := string(bizContentBytes)

	// Build request params
	params := map[string]string{
		"app_id":      cfg.AppID,
		"method":      "alipay.trade.page.pay",
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": bizContent,
	}
	if cfg.NotifyURL != "" {
		params["notify_url"] = cfg.NotifyURL
	}
	if cfg.ReturnURL != "" {
		params["return_url"] = cfg.ReturnURL
	}

	// Sign
	signStr := buildAlipaySignString(params)
	sig, err := rsaSign(signStr, cfg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("sign alipay request: %w", err)
	}
	params["sign"] = sig

	// Build URL
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	payURL := gateway + "?" + values.Encode()

	order.PaymentMode = PaymentModeAlipay
	order.PayURL = payURL
	order.Status = StatusPending
	return payURL, nil
}

// VerifyAlipayNotification verifies an Alipay async notification signature.
// Returns the parsed form values if verification succeeds.
func VerifyAlipayNotification(body string, alipayPublicKey string) (url.Values, error) {
	values, err := url.ParseQuery(body)
	if err != nil {
		return nil, fmt.Errorf("parse notification body: %w", err)
	}

	sign := values.Get("sign")
	if sign == "" {
		return nil, fmt.Errorf("notification missing sign")
	}

	// Build sign string (exclude sign and sign_type)
	params := map[string]string{}
	for k := range values {
		if k == "sign" || k == "sign_type" {
			continue
		}
		params[k] = values.Get(k)
	}
	signStr := buildAlipaySignString(params)

	if err := rsaVerify(signStr, sign, alipayPublicKey); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}
	return values, nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func alipayProductCode(cfg *AlipayDirectConfig) string {
	if cfg.ProductCode != "" {
		return cfg.ProductCode
	}
	return "FAST_INSTANT_TRADE_PAY"
}

func buildAlipaySignString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		v := params[k]
		if v == "" {
			continue
		}
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, "&")
}

func decodePEMBlock(raw string, labels ...string) (*pem.Block, []byte) {
	block, rest := pem.Decode([]byte(raw))
	if block != nil {
		return block, rest
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, label := range labels {
		wrapped := "-----BEGIN " + label + "-----\n" + raw + "\n-----END " + label + "-----"
		block, rest = pem.Decode([]byte(wrapped))
		if block != nil {
			return block, rest
		}
	}
	return nil, nil
}
func rsaSign(content, privateKeyPEM string) (string, error) {
	block, _ := decodePEMBlock(privateKeyPEM, "RSA PRIVATE KEY", "PRIVATE KEY")
	if block == nil {
		return "", fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parse private key: %w", err)
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("key is not RSA")
	}
	h := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func rsaVerify(content, signBase64, publicKeyPEM string) error {
	block, _ := decodePEMBlock(publicKeyPEM, "PUBLIC KEY", "RSA PUBLIC KEY")
	if block == nil {
		return fmt.Errorf("invalid public key PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		rsaPub, pkcs1Err := x509.ParsePKCS1PublicKey(block.Bytes)
		if pkcs1Err != nil {
			return fmt.Errorf("parse public key: %w", err)
		}
		return verifyRSASHA256(rsaPub, content, signBase64)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("key is not RSA public key")
	}
	return verifyRSASHA256(rsaPub, content, signBase64)
}

func verifyRSASHA256(rsaPub *rsa.PublicKey, content, signBase64 string) error {
	sig, err := base64.StdEncoding.DecodeString(signBase64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	h := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, h[:], sig)
}
