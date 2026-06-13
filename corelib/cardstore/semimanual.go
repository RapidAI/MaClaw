package cardstore

import "fmt"

// CreateSemiManualOrder prepares a personal_semimanual order by selecting the
// payment channel and populating the QR code / deep link / instructions.
// This is the shared logic used by both Hub and HubCenter.
func CreateSemiManualOrder(order *Order, cfg *PersonalPaymentConfig, channelID string) error {
	if cfg == nil || len(cfg.Channels) == 0 {
		return fmt.Errorf("no personal payment channels configured")
	}

	// Find the requested channel (or first enabled one)
	var channel *PersonalPaymentChannel
	for i := range cfg.Channels {
		ch := &cfg.Channels[i]
		if !ch.Enabled {
			continue
		}
		if channelID == "" || ch.ID == channelID {
			channel = ch
			break
		}
	}
	if channel == nil {
		return fmt.Errorf("payment channel %q not found or not enabled", channelID)
	}

	order.PaymentMode = PaymentModeSemiManual
	order.PayChannel = channel.ID
	order.PayChannelLabel = channel.Label
	if order.PayChannelLabel == "" {
		order.PayChannelLabel = channel.ID
	}
	order.PayQRURL = channel.ImageURL
	order.PayInstruction = cfg.Instruction

	// Build payment info based on channel type
	if channel.ID == "bank_transfer" {
		// Bank transfer: show account info + contact
		instruction := cfg.Instruction
		if instruction == "" {
			instruction = "请转账至以下银行账户，转账备注请填写订单号。转账完成后请联系我们确认。"
		}
		order.PayInstruction = instruction + "\n\n" +
			"开户行：" + channel.BankName + "\n" +
			"账号：" + channel.BankAccount + "\n" +
			"户名：" + channel.BankHolder
		if channel.ContactInfo != "" {
			order.PayInstruction += "\n\n付款确认联系方式：" + channel.ContactInfo
		}
		order.PayInstruction += "\n\n请在转账备注中填写订单号：" + order.OrderNo
	} else if channel.AlipayUserID != "" && channel.DeepLinkMode != "" {
		// Build deep link for Alipay
		order.PayDeepLink = buildAlipayTransferDeepLink(channel.AlipayUserID, order.Amount)
	}

	order.Status = StatusPersonalCreated
	return nil
}

// IsAdminEmail checks if the given email is one of the configured admin emails.
func IsAdminEmail(cfg *PersonalPaymentConfig, email string) bool {
	if cfg == nil {
		return false
	}
	for _, admin := range cfg.AdminEmails {
		if admin == email {
			return true
		}
	}
	return false
}

func buildAlipayTransferDeepLink(userID string, amount float64) string {
	return fmt.Sprintf("alipays://platformapi/startapp?appId=20000067&url=https://render.alipay.com/p/s/i/?scheme=alipays://platformapi/startapp?appId=20000123&actionType=scan&biz_data={\"s\":\"money\",\"u\":\"%s\",\"a\":\"%s\",\"m\":\"\"}",
		userID, fmt.Sprintf("%.2f", amount))
}
