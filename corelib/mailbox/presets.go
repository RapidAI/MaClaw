package mailbox

import "strings"

// ProviderPreset is the well-known server pair for a mail domain plus the
// one-line hint about which secret the provider accepts.
type ProviderPreset struct {
	ID       string
	Label    string
	Domains  []string
	Inbound  Endpoint
	Outbound Endpoint
	// Hint tells the user what to paste as the password. Never a secret.
	Hint string
}

var providerPresets = []ProviderPreset{
	{
		ID: "netease", Label: "网易邮箱",
		Domains:  []string{"163.com", "126.com", "yeah.net"},
		Inbound:  Endpoint{Host: "imap.{domain}", Port: 993, Security: SecurityTLS},
		Outbound: Endpoint{Host: "smtp.{domain}", Port: 465, Security: SecurityTLS},
		Hint:     "网易邮箱要用授权码，不是登录密码：网页版 设置 → POP3/SMTP/IMAP → 开启 IMAP/SMTP 服务，按提示生成授权码后填入 password。",
	},
	{
		ID: "qq", Label: "QQ 邮箱",
		Domains:  []string{"qq.com", "foxmail.com"},
		Inbound:  Endpoint{Host: "imap.qq.com", Port: 993, Security: SecurityTLS},
		Outbound: Endpoint{Host: "smtp.qq.com", Port: 465, Security: SecurityTLS},
		Hint:     "QQ 邮箱要用授权码：网页版 设置 → 账号 → 开启 IMAP/SMTP 服务，生成授权码后填入 password。",
	},
	{
		ID: "gmail", Label: "Gmail",
		Domains:  []string{"gmail.com", "googlemail.com"},
		Inbound:  Endpoint{Host: "imap.gmail.com", Port: 993, Security: SecurityTLS},
		Outbound: Endpoint{Host: "smtp.gmail.com", Port: 465, Security: SecurityTLS},
		Hint:     "Gmail 推荐用浏览器登录（OAuth）。若用密码，必须是开启两步验证后生成的应用专用密码（16 位），普通密码会被拒绝。",
	},
	{
		ID: "outlook", Label: "Outlook.com / Hotmail",
		Domains:  []string{"outlook.com", "hotmail.com", "live.com", "msn.com"},
		Inbound:  Endpoint{Host: "outlook.office365.com", Port: 993, Security: SecurityTLS},
		Outbound: Endpoint{Host: "smtp-mail.outlook.com", Port: 587, Security: SecuritySTARTTLS},
		Hint:     "Outlook.com/Hotmail 已停用密码登录（基本认证），请用浏览器登录（OAuth）接入。",
	},
}

// PresetForAddress returns the preset for the address's domain.
func PresetForAddress(address string) (ProviderPreset, bool) {
	_, domain, ok := strings.Cut(strings.TrimSpace(address), "@")
	if !ok {
		return ProviderPreset{}, false
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, p := range providerPresets {
		for _, d := range p.Domains {
			if d == domain {
				out := p
				out.Inbound.Host = strings.ReplaceAll(p.Inbound.Host, "{domain}", domain)
				out.Outbound.Host = strings.ReplaceAll(p.Outbound.Host, "{domain}", domain)
				return out, true
			}
		}
	}
	return ProviderPreset{}, false
}

// ProviderHint is the secret hint for an address, or "".
func ProviderHint(address string) string {
	if p, ok := PresetForAddress(address); ok {
		return p.Hint
	}
	return ""
}

// fillPresetEndpoints supplies the preset servers for a side the caller left
// out. Given endpoints are never changed.
func fillPresetEndpoints(email, username string, inbound, outbound *Endpoint) (*Endpoint, *Endpoint) {
	p, ok := PresetForAddress(email)
	if !ok {
		return inbound, outbound
	}
	user := strings.TrimSpace(username)
	if user == "" {
		user = strings.TrimSpace(email)
	}
	if inbound == nil {
		ep := p.Inbound
		ep.Username = user
		inbound = &ep
	}
	if outbound == nil {
		ep := p.Outbound
		ep.Username = user
		ep.FromAddress = strings.TrimSpace(email)
		outbound = &ep
	}
	return inbound, outbound
}
