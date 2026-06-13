// Package cardstore provides shared card store order management, payment flow
// (personal_semimanual + alipay_direct), and activation primitives used by
// both Hub and HubCenter.
package cardstore

import "time"

// Payment modes supported by both Hub and HubCenter card stores.
const (
	PaymentModeSemiManual = "personal_semimanual"
	PaymentModeAlipay     = "alipay_direct"
)

// Order status values.
const (
	StatusPending          = "pending"
	StatusPersonalCreated  = "personal_created"  // semi-manual: order placed, awaiting payment
	StatusPersonalOpened   = "personal_opened"   // semi-manual: buyer opened payment QR
	StatusPersonalRejected = "personal_rejected" // semi-manual: admin rejected
	StatusPaid             = "paid"              // payment confirmed (auto or manual)
	StatusActivated        = "activated"         // credits/card activated
	StatusCancelled        = "cancelled"
	StatusFailed           = "failed"
)

// Order represents a card store purchase order.
// This is the shared type; Hub and HubCenter each add domain-specific fields.
type Order struct {
	OrderNo         string    `json:"order_no"`
	ProductID       string    `json:"product_id"`
	ProductLabel    string    `json:"product_label,omitempty"`
	Email           string    `json:"email"`
	Amount          float64   `json:"amount"`
	Status          string    `json:"status"`
	PaymentMode     string    `json:"payment_mode,omitempty"`
	PayChannel      string    `json:"pay_channel,omitempty"`
	PayChannelLabel string    `json:"pay_channel_label,omitempty"`
	PayQRURL        string    `json:"pay_qr_url,omitempty"`
	PayDeepLink     string    `json:"pay_deep_link,omitempty"`
	PayInstruction  string    `json:"pay_instruction,omitempty"`
	PayURL          string    `json:"pay_url,omitempty"`
	PaymentID       string    `json:"payment_id,omitempty"`
	PaymentMsg      string    `json:"payment_msg,omitempty"`
	ActualPayAmount string    `json:"actual_pay_amount,omitempty"`
	PayTime         string    `json:"pay_time,omitempty"`
	PlatformOrderNo string    `json:"platform_order_no,omitempty"`
	ChannelOrderNo  string    `json:"channel_order_no,omitempty"`
	ReviewedBy      string    `json:"reviewed_by,omitempty"`
	ReviewedAt      time.Time `json:"reviewed_at,omitempty"`
	ReviewNote      string    `json:"review_note,omitempty"`
	PaidAt          time.Time `json:"paid_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PersonalPaymentConfig holds the semi-manual payment channel configuration
// (QR codes for WeChat/Alipay personal accounts).
type PersonalPaymentConfig struct {
	AdminEmails []string                 `json:"admin_emails,omitempty"`
	Instruction string                   `json:"instruction,omitempty"`
	Channels    []PersonalPaymentChannel `json:"channels,omitempty"`
}

// PersonalPaymentChannel represents a single payment channel (wechat/alipay QR).
type PersonalPaymentChannel struct {
	ID           string `json:"id"`          // "wechat" / "alipay" / "bank_transfer"
	Label        string `json:"label,omitempty"`
	Payee        string `json:"payee,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
	Enabled      bool   `json:"enabled"`
	AlipayUserID string `json:"alipay_user_id,omitempty"`
	DeepLinkMode string `json:"deep_link_mode,omitempty"`
	// Bank transfer fields
	BankName    string `json:"bank_name,omitempty"`
	BankAccount string `json:"bank_account,omitempty"`
	BankHolder  string `json:"bank_holder,omitempty"`
	ContactInfo string `json:"contact_info,omitempty"` // phone/email/wechat for post-payment confirmation
}

// AlipayDirectConfig holds the configuration for Alipay official API integration.
type AlipayDirectConfig struct {
	AppID           string `json:"app_id,omitempty"`
	PrivateKey      string `json:"private_key,omitempty"`
	AlipayPublicKey string `json:"alipay_public_key,omitempty"`
	GatewayURL      string `json:"gateway_url,omitempty"`
	ProductCode     string `json:"product_code,omitempty"`
	SubjectPrefix   string `json:"subject_prefix,omitempty"`
	ReturnURL       string `json:"return_url,omitempty"`
	NotifyURL       string `json:"notify_url,omitempty"`
	SignType        string `json:"sign_type,omitempty"`
	PaymentMethod   string `json:"payment_method,omitempty"`
}

// DefaultAlipayGatewayURL is the standard Alipay gateway endpoint.
const DefaultAlipayGatewayURL = "https://openapi.alipay.com/gateway.do"
