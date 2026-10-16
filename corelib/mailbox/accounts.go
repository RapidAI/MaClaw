package mailbox

import (
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"
)

func MsgUnknownName(name string) string { return fmt.Sprintf("没有名为「%s」的邮箱", name) }
func MsgDisabled(name string) string    { return fmt.Sprintf("邮箱「%s」已停用", name) }
func MsgNoInbound(name string) string   { return fmt.Sprintf("邮箱「%s」没有配置收件", name) }
func MsgNoOutbound(name string) string  { return fmt.Sprintf("邮箱「%s」没有配置发件", name) }
func MsgDuplicateName(name string) string {
	return fmt.Sprintf("已经有名为「%s」的邮箱", name)
}

func SecretRef(id, side string) string {
	return "keyring://" + KeyringService + "/mailbox/" + id + "/" + side
}

func KeyringItem(id, side string) (string, error) {
	if !ValidID(id) || (side != SideInbound && side != SideOutbound) {
		return "", classErr("invalid", "邮箱标识无效")
	}
	return "mailbox/" + id + "/" + side, nil
}

// ParseSecretRef accepts only keyring://maclaw/mailbox/<id>/<side>.
func ParseSecretRef(ref string) (string, error) {
	const prefix = "keyring://" + KeyringService + "/mailbox/"
	rest := strings.TrimSpace(ref)
	if !strings.HasPrefix(rest, prefix) {
		return "", classErr("keyring", MsgKeyring)
	}
	id, side, ok := strings.Cut(strings.TrimPrefix(rest, prefix), "/")
	if !ok {
		return "", classErr("keyring", MsgKeyring)
	}
	return KeyringItem(id, side)
}

func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case i > 0 && (r == '.' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

func UpsertAccount(existing []Account, in SaveRequest, now time.Time) ([]Account, Account, error) {
	next := cloneAccounts(existing)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, Account{}, classErr("invalid", "邮箱名字不能为空")
	}
	address, err := normalizeAddress(in.Address)
	if err != nil {
		return nil, Account{}, err
	}
	inbound, err := normalizeEndpoint(in.Inbound, true)
	if err != nil {
		return nil, Account{}, err
	}
	outbound, err := normalizeEndpoint(in.Outbound, false)
	if err != nil {
		return nil, Account{}, err
	}
	if inbound == nil && outbound == nil {
		return nil, Account{}, classErr("invalid", "至少配置收件或发件中的一项")
	}
	id := strings.TrimSpace(in.ID)
	idx := -1
	if id != "" {
		if !ValidID(id) {
			return nil, Account{}, classErr("invalid", "邮箱标识无效")
		}
		idx = indexByID(next, id)
	} else {
		id = newAccountID(now, next)
	}
	for i := range next {
		if i == idx {
			continue
		}
		if next[i].Name == name {
			return nil, Account{}, classErr("duplicate_name", MsgDuplicateName(name))
		}
	}
	if idx < 0 && len(next) >= MaxAccounts {
		return nil, Account{}, classErr("limit", "最多只能保存 16 个邮箱")
	}
	acc := Account{
		ID: id, Name: name, Address: address,
		Inbound: inbound, Outbound: outbound,
	}
	if inbound != nil {
		acc.SecretRefInbound = SecretRef(id, SideInbound)
	}
	if outbound != nil {
		acc.SecretRefOutbound = SecretRef(id, SideOutbound)
	}
	if idx >= 0 {
		prev := next[idx]
		acc.CreatedAt = prev.CreatedAt
		if in.Enabled == nil {
			acc.Enabled = prev.Enabled
		} else {
			acc.Enabled = *in.Enabled
		}
		acc.Default = prev.Default
		if acc.SecretRefInbound == "" {
			acc.SecretRefInbound = ""
		} else if prev.SecretRefInbound != "" && prev.SecretRefInbound != acc.SecretRefInbound {
			return nil, Account{}, classErr("invalid", "不能改写密钥引用")
		}
		if in.MakeDefault != nil && *in.MakeDefault {
			if !acc.Enabled {
				return nil, Account{}, classErr("invalid", "停用的邮箱不能设为默认")
			}
			clearDefaults(next)
			acc.Default = true
		} else if in.MakeDefault != nil && !*in.MakeDefault {
			acc.Default = false
		}
		if !acc.Enabled {
			acc.Default = false
		}
		if in.SignatureID == nil {
			acc.SignatureID = prev.SignatureID
		} else {
			acc.SignatureID = strings.TrimSpace(*in.SignatureID)
		}
		if err := applyAuth(&acc, &prev, in); err != nil {
			return nil, Account{}, err
		}
		next[idx] = acc
	} else {
		acc.CreatedAt = now
		if in.Enabled == nil {
			acc.Enabled = true
		} else {
			acc.Enabled = *in.Enabled
		}
		if len(enabledAccounts(next)) == 0 && acc.Enabled {
			acc.Default = true
		}
		if in.MakeDefault != nil && *in.MakeDefault {
			if !acc.Enabled {
				return nil, Account{}, classErr("invalid", "停用的邮箱不能设为默认")
			}
			clearDefaults(next)
			acc.Default = true
		}
		if in.SignatureID != nil {
			acc.SignatureID = strings.TrimSpace(*in.SignatureID)
		}
		if err := applyAuth(&acc, nil, in); err != nil {
			return nil, Account{}, err
		}
		next = append(next, acc)
		idx = len(next) - 1
	}
	enforceDefault(next)
	return next, next[indexByID(next, id)], nil
}

func SetEnabled(existing []Account, id string, enabled bool) ([]Account, error) {
	next := cloneAccounts(existing)
	idx := indexByID(next, strings.TrimSpace(id))
	if idx < 0 {
		return nil, classErr("not_found", "没有这个邮箱")
	}
	next[idx].Enabled = enabled
	if !enabled {
		next[idx].Default = false
		enforceDefault(next)
		return next, nil
	}
	if !hasEnabledDefault(next) {
		clearDefaults(next)
		next[idx].Default = true
	}
	enforceDefault(next)
	return next, nil
}

func SetDefault(existing []Account, id string) ([]Account, error) {
	next := cloneAccounts(existing)
	idx := indexByID(next, strings.TrimSpace(id))
	if idx < 0 {
		return nil, classErr("not_found", "没有这个邮箱")
	}
	if !next[idx].Enabled {
		return nil, classErr("disabled", "停用的邮箱不能设为默认")
	}
	clearDefaults(next)
	next[idx].Default = true
	enforceDefault(next)
	return next, nil
}

func DeleteAccount(existing []Account, id string) ([]Account, Account, error) {
	id = strings.TrimSpace(id)
	idx := indexByID(existing, id)
	if idx < 0 {
		return nil, Account{}, classErr("not_found", "没有这个邮箱")
	}
	removed := existing[idx]
	next := make([]Account, 0, len(existing)-1)
	for i := range existing {
		if i != idx {
			next = append(next, existing[i])
		}
	}
	enforceDefault(next)
	return next, removed, nil
}

func Summary(a Account, hasInboundSecret, hasOutboundSecret bool) map[string]interface{} {
	out := map[string]interface{}{
		"id":                  a.ID,
		"name":                a.Name,
		"address":             a.Address,
		"enabled":             a.Enabled,
		"default":             a.Default,
		"has_inbound":         a.Inbound != nil,
		"has_outbound":        a.Outbound != nil,
		"has_inbound_secret":  hasInboundSecret && a.Inbound != nil,
		"has_outbound_secret": hasOutboundSecret && a.Outbound != nil,
		"signature_id":        a.SignatureID,
	}
	if a.Auth != "" {
		out["auth"] = a.Auth
	}
	if a.Provider != "" {
		out["provider"] = a.Provider
	}
	// Settings edits this payload. It includes the servers the user saved and
	// never a password or secret ref. The agent list stays on public fields.
	if a.Inbound != nil {
		out["inbound"] = endpointSummary(a.Inbound)
	}
	if a.Outbound != nil {
		out["outbound"] = endpointSummary(a.Outbound)
	}
	return out
}

func endpointSummary(ep *Endpoint) map[string]interface{} {
	if ep == nil {
		return nil
	}
	out := map[string]interface{}{
		"host":     ep.Host,
		"security": ep.Security,
	}
	if ep.Port > 0 {
		out["port"] = ep.Port
	}
	if strings.TrimSpace(ep.Username) != "" {
		out["username"] = ep.Username
	}
	if strings.TrimSpace(ep.Box) != "" {
		out["box"] = ep.Box
	}
	if strings.TrimSpace(ep.FromAddress) != "" {
		out["from_address"] = ep.FromAddress
	}
	return out
}

func applyAuth(acc *Account, prev *Account, in SaveRequest) error {
	if acc == nil {
		return classErr("invalid", "邮箱配置不能为空")
	}
	if in.Auth == nil {
		if prev != nil {
			acc.Auth = prev.Auth
			acc.Provider = prev.Provider
		}
		return nil
	}
	auth := strings.TrimSpace(*in.Auth)
	if auth == "" {
		acc.Auth = ""
		acc.Provider = ""
		return nil
	}
	provider := ""
	if in.Provider != nil {
		provider = strings.TrimSpace(*in.Provider)
	}
	if auth != AuthOAuth || !KnownOAuthProvider(provider) {
		return classErr("invalid", "不支持这个邮箱登录")
	}
	acc.Auth = AuthOAuth
	acc.Provider = provider
	return nil
}

func RejectSecretPayload(keys []string) error {
	for _, key := range keys {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "password" || lower == "secret" || lower == "token" ||
			strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
			return classErr("secret", "密码不能写进邮箱配置，请单独保存到系统密钥环")
		}
	}
	return nil
}

func normalizeAddress(raw string) (string, error) {
	addr := strings.TrimSpace(raw)
	if containsControl(addr) || strings.ContainsAny(addr, " \t\r\n") || strings.Count(addr, "@") != 1 {
		return "", classErr("invalid", "邮箱地址格式不正确")
	}
	local, domain, _ := strings.Cut(addr, "@")
	if local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", classErr("invalid", "邮箱地址格式不正确")
	}
	return addr, nil
}

func normalizeEndpoint(ep *Endpoint, inbound bool) (*Endpoint, error) {
	if ep == nil {
		return nil, nil
	}
	host := strings.TrimSpace(ep.Host)
	if host == "" {
		return nil, nil
	}
	sec := strings.ToLower(strings.TrimSpace(ep.Security))
	if sec == "" {
		sec = SecurityTLS
	}
	switch sec {
	case SecurityTLS, SecuritySTARTTLS, SecurityNone:
	default:
		return nil, classErr("invalid", "加密方式只能是 tls、starttls 或 none")
	}
	if sec == SecurityNone && !loopbackHost(host) {
		return nil, classErr("invalid", "不加密的连接只允许本机地址")
	}
	port := ep.Port
	if port == 0 {
		port = defaultPort(inbound, sec)
	}
	if port < 1 || port > 65535 {
		return nil, classErr("invalid", "端口必须在 1 到 65535 之间")
	}
	out := &Endpoint{
		Host: host, Port: port, Security: sec,
		Username: strings.TrimSpace(ep.Username),
	}
	if inbound {
		box := strings.TrimSpace(ep.Box)
		if box == "" {
			box = "INBOX"
		}
		out.Box = box
	} else if from := strings.TrimSpace(ep.FromAddress); from != "" {
		normalized, err := normalizeAddress(from)
		if err != nil {
			return nil, err
		}
		out.FromAddress = normalized
	}
	return out, nil
}

func defaultPort(inbound bool, sec string) int {
	if inbound {
		if sec == SecurityTLS {
			return 993
		}
		return 143
	}
	switch sec {
	case SecurityTLS:
		return 465
	case SecurityNone:
		return 25
	default:
		return 587
	}
}

func loopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func enforceDefault(accounts []Account) {
	enabled := make([]int, 0, len(accounts))
	for i := range accounts {
		if accounts[i].Enabled {
			enabled = append(enabled, i)
			continue
		}
		accounts[i].Default = false
	}
	if len(enabled) == 0 {
		return
	}
	marked := -1
	for _, i := range enabled {
		if !accounts[i].Default {
			continue
		}
		if marked < 0 || olderAccount(accounts[i], accounts[marked]) {
			if marked >= 0 {
				accounts[marked].Default = false
			}
			marked = i
			continue
		}
		accounts[i].Default = false
	}
	if marked >= 0 {
		return
	}
	oldest := enabled[0]
	for _, i := range enabled[1:] {
		if olderAccount(accounts[i], accounts[oldest]) {
			oldest = i
		}
	}
	accounts[oldest].Default = true
}

func olderAccount(a, b Account) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID < b.ID
	}
	if a.CreatedAt.IsZero() {
		return false
	}
	if b.CreatedAt.IsZero() {
		return true
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

func hasEnabledDefault(accounts []Account) bool {
	for i := range accounts {
		if accounts[i].Enabled && accounts[i].Default {
			return true
		}
	}
	return false
}

func enabledAccounts(accounts []Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out
}

func clearDefaults(accounts []Account) {
	for i := range accounts {
		accounts[i].Default = false
	}
}

func cloneAccounts(in []Account) []Account {
	out := make([]Account, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].Inbound != nil {
			cp := *in[i].Inbound
			out[i].Inbound = &cp
		}
		if in[i].Outbound != nil {
			cp := *in[i].Outbound
			out[i].Outbound = &cp
		}
	}
	return out
}

func indexByID(accounts []Account, id string) int {
	for i := range accounts {
		if accounts[i].ID == id {
			return i
		}
	}
	return -1
}

func newAccountID(now time.Time, existing []Account) string {
	base := fmt.Sprintf("mb%x", now.UnixNano())
	if !ValidID(base) || indexByID(existing, base) >= 0 {
		base = "mb" + strings.TrimLeft(base, "mb")
	}
	id := base
	n := 0
	for indexByID(existing, id) >= 0 || !ValidID(id) {
		n++
		id = fmt.Sprintf("%s%d", base, n)
		if len(id) > 64 {
			id = id[:64]
		}
	}
	return id
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
