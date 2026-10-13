package mailbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Signature is a reusable sign-off. Body may contain mailbox macros that are
// expanded with the sending account, so one public signature can name whichever
// mailbox actually sends.
type Signature struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	Default   bool      `json:"default"`
	CreatedAt time.Time `json:"created_at"`
}

// SignatureSave is a create or update. Nil MakeDefault keeps the current flag
// on update. The first saved signature becomes the public default.
type SignatureSave struct {
	ID          string
	Name        string
	Body        string
	MakeDefault *bool
}

func MsgSignatureMissing(accountName string) string {
	return fmt.Sprintf("邮箱「%s」指定的签名不存在", accountName)
}

func UpsertSignature(existing []Signature, in SignatureSave, now time.Time) ([]Signature, Signature, error) {
	next := cloneSignatures(existing)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, Signature{}, classErr("invalid", "签名名字不能为空")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, Signature{}, classErr("invalid", "签名内容不能为空")
	}
	if len([]rune(body)) > SignatureBodyCap {
		return nil, Signature{}, classErr("invalid", "签名内容过长")
	}
	if signatureHasUnsafeControl(body) {
		return nil, Signature{}, classErr("invalid", "签名内容含有不可见字符")
	}
	id := strings.TrimSpace(in.ID)
	idx := -1
	if id != "" {
		if !ValidID(id) {
			return nil, Signature{}, classErr("invalid", "签名标识无效")
		}
		idx = indexSignature(next, id)
	} else {
		id = newSignatureID(now, next)
	}
	for i := range next {
		if i == idx {
			continue
		}
		if next[i].Name == name {
			return nil, Signature{}, classErr("duplicate_name", fmt.Sprintf("已经有名为「%s」的签名", name))
		}
	}
	if idx < 0 && len(next) >= MaxSignatures {
		return nil, Signature{}, classErr("limit", "最多只能保存 32 个签名")
	}
	sig := Signature{ID: id, Name: name, Body: body}
	if idx >= 0 {
		sig.CreatedAt = next[idx].CreatedAt
		sig.Default = next[idx].Default
		if in.MakeDefault != nil && *in.MakeDefault {
			clearSignatureDefaults(next)
			sig.Default = true
		} else if in.MakeDefault != nil && !*in.MakeDefault {
			sig.Default = false
		}
		next[idx] = sig
	} else {
		sig.CreatedAt = now
		if len(next) == 0 {
			sig.Default = true
		}
		if in.MakeDefault != nil && *in.MakeDefault {
			clearSignatureDefaults(next)
			sig.Default = true
		}
		next = append(next, sig)
	}
	enforceSignatureDefault(next)
	return next, next[indexSignature(next, id)], nil
}

func DeleteSignature(sigs []Signature, accounts []Account, id string) ([]Signature, []Account, error) {
	id = strings.TrimSpace(id)
	idx := indexSignature(sigs, id)
	if idx < 0 {
		return nil, nil, classErr("not_found", "没有这个签名")
	}
	next := make([]Signature, 0, len(sigs)-1)
	for i := range sigs {
		if i != idx {
			next = append(next, sigs[i])
		}
	}
	enforceSignatureDefault(next)
	cleaned := cloneAccounts(accounts)
	for i := range cleaned {
		if cleaned[i].SignatureID == id {
			cleaned[i].SignatureID = ""
		}
	}
	return next, cleaned, nil
}

func SetDefaultSignature(existing []Signature, id string) ([]Signature, error) {
	next := cloneSignatures(existing)
	idx := indexSignature(next, strings.TrimSpace(id))
	if idx < 0 {
		return nil, classErr("not_found", "没有这个签名")
	}
	clearSignatureDefaults(next)
	next[idx].Default = true
	enforceSignatureDefault(next)
	return next, nil
}

// ResolveSignature picks the mailbox's signature, or the public default when
// the mailbox does not name one. A mailbox that names a missing signature
// fails and does not fall through to the default.
func ResolveSignature(sigs []Signature, account Account) (Signature, bool, error) {
	id := strings.TrimSpace(account.SignatureID)
	if id != "" {
		for _, sig := range sigs {
			if sig.ID == id {
				return sig, true, nil
			}
		}
		return Signature{}, false, classErr("signature", MsgSignatureMissing(account.Name))
	}
	for _, sig := range sigs {
		if sig.Default {
			return sig, true, nil
		}
	}
	return Signature{}, false, nil
}

var signatureMacro = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// ExpandSignature replaces mailbox macros with the account that is sending.
// Unknown macros are left unchanged. Replacements are not scanned again.
func ExpandSignature(body string, account Account, from string) string {
	if strings.TrimSpace(from) == "" {
		from = account.Address
	}
	return signatureMacro.ReplaceAllStringFunc(body, func(match string) string {
		parts := signatureMacro.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		switch strings.ToLower(strings.TrimSpace(parts[1])) {
		case "name", "mailbox", "邮箱名":
			return account.Name
		case "email", "address", "邮箱", "邮箱地址":
			return account.Address
		case "from", "发件人":
			return from
		default:
			return match
		}
	})
}

func (h *Host) applySignature(account Account, msg OutboundMessage) (OutboundMessage, error) {
	if h == nil {
		return msg, nil
	}
	sig, ok, err := ResolveSignature(h.Signatures, account)
	if err != nil || !ok {
		return msg, err
	}
	expanded := strings.TrimSpace(ExpandSignature(sig.Body, account, msg.From))
	if expanded == "" {
		return msg, nil
	}
	text := strings.TrimRight(msg.Text, "\n")
	if text == "" {
		msg.Text = expanded
		return msg, nil
	}
	msg.Text = text + "\n\n-- \n" + expanded
	return msg, nil
}

func SignatureSummary(s Signature) map[string]interface{} {
	return map[string]interface{}{
		"id":      s.ID,
		"name":    s.Name,
		"body":    s.Body,
		"default": s.Default,
	}
}

func enforceSignatureDefault(sigs []Signature) {
	if len(sigs) == 0 {
		return
	}
	marked := -1
	for i := range sigs {
		if !sigs[i].Default {
			continue
		}
		if marked < 0 || olderSignature(sigs[i], sigs[marked]) {
			if marked >= 0 {
				sigs[marked].Default = false
			}
			marked = i
			continue
		}
		sigs[i].Default = false
	}
	if marked >= 0 {
		return
	}
	oldest := 0
	for i := range sigs {
		if olderSignature(sigs[i], sigs[oldest]) {
			oldest = i
		}
	}
	sigs[oldest].Default = true
}

func olderSignature(a, b Signature) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		if a.CreatedAt.IsZero() && b.CreatedAt.IsZero() {
			return a.ID < b.ID
		}
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

func clearSignatureDefaults(sigs []Signature) {
	for i := range sigs {
		sigs[i].Default = false
	}
}

func cloneSignatures(in []Signature) []Signature {
	out := make([]Signature, len(in))
	copy(out, in)
	return out
}

func indexSignature(sigs []Signature, id string) int {
	for i := range sigs {
		if sigs[i].ID == id {
			return i
		}
	}
	return -1
}

func signatureHasUnsafeControl(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func newSignatureID(now time.Time, existing []Signature) string {
	base := fmt.Sprintf("sg%x", now.UnixNano())
	id := base
	n := 0
	for indexSignature(existing, id) >= 0 || !ValidID(id) {
		n++
		id = fmt.Sprintf("%s%d", base, n)
		if len(id) > 64 {
			id = fmt.Sprintf("sg%d", n)
		}
	}
	return id
}
