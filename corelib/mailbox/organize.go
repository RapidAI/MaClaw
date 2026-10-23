package mailbox

// mailbox_organize: move mail between folders, mark read/unread, star, and
// add or remove labels. Every action is reversible, so it runs without a
// confirmation round; deleting stays in mailbox_delete.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

// OrganizeMaxUIDs caps one call.
const OrganizeMaxUIDs = 200

// OrganizeRequest is one parsed organize call.
type OrganizeRequest struct {
	Action string
	Box    string
	UIDs   []uint32
	Dest   string
	Labels []string
}

// MailOrganizer is implemented by the reader when it can change mail.
type MailOrganizer interface {
	Organize(ctx context.Context, account Account, secret string, req OrganizeRequest) (int, error)
}

var organizeActions = map[string]bool{
	"move": true, "mark_read": true, "mark_unread": true, "flag": true, "unflag": true,
	"add_labels": true, "remove_labels": true,
}

var organizeArgs = map[string]bool{
	"action": true, "account": true, "box": true, "uids": true, "to_box": true, "labels": true,
}

// Organize runs one organize call and returns the model-facing text.
func (h *Host) Organize(ctx context.Context, args map[string]interface{}) string {
	started := time.Now()
	var accountID string
	text, class := h.organize(ctx, args, &accountID)
	side := ""
	if accountID != "" {
		side = SideInbound
	}
	h.logResult(accountID, side, stringArg(args, "action"), class, started)
	return text
}

func (h *Host) organize(ctx context.Context, args map[string]interface{}, accountID *string) (string, string) {
	if err := rejectArgs(args, organizeArgs); err != nil {
		return err.Error(), "rejected_arg"
	}
	action := strings.TrimSpace(stringArg(args, "action"))
	if !organizeActions[action] {
		return "action 只能是 move、mark_read、mark_unread、flag、unflag、add_labels、remove_labels", "invalid"
	}
	uids, reason := parseUIDList(args["uids"])
	if reason != "" || len(uids) == 0 {
		return "需要 uids（邮件 UID 列表，来自 mailbox_query 的结果）", "invalid"
	}
	if len(uids) > OrganizeMaxUIDs {
		return fmt.Sprintf("一次最多整理 %d 封", OrganizeMaxUIDs), "invalid"
	}
	req := OrganizeRequest{Action: action, UIDs: uids}
	switch action {
	case "move":
		req.Dest = strings.TrimSpace(stringArg(args, "to_box"))
		if req.Dest == "" {
			return "move 需要 to_box（目标文件夹）", "invalid"
		}
	case "add_labels", "remove_labels":
		req.Labels = labelList(args["labels"])
		if len(req.Labels) == 0 {
			return "需要 labels", "invalid"
		}
	}
	if h.Budget != nil {
		if err := h.Budget.ConsumeQuery(); err != nil {
			return err.Error(), "budget"
		}
	}
	h.bumpTool()
	account, secret, err := h.openInbound(ctx, stringArg(args, "account"))
	if err != nil {
		return err.Error(), classOf(err)
	}
	*accountID = account.ID
	if isPOP3(account) {
		return MsgPOP3Unsupported, "unsupported"
	}
	req.Box = boxArg(account, args)
	org, ok := h.Reader.(MailOrganizer)
	if !ok || h.Reader == nil {
		return MsgConnect, "connect"
	}
	n, err := org.Organize(ctx, account, secret, req)
	if err != nil {
		return transportText(err), classOf(err)
	}
	return mustJSON(map[string]interface{}{"status": "ok", "action": action, "count": n}), "ok"
}

func labelList(raw interface{}) []string {
	var out []string
	switch v := raw.(type) {
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

func isGmailAccount(account Account) bool {
	if strings.EqualFold(account.Provider, ProviderGmail) {
		return true
	}
	return account.Inbound != nil && strings.EqualFold(strings.TrimSpace(account.Inbound.Host), "imap.gmail.com")
}

// organizeStore is the STORE item and values for a flag or label action.
func organizeStore(account Account, req OrganizeRequest) (imap.StoreItem, []interface{}) {
	var vals []interface{}
	switch req.Action {
	case "mark_read":
		return imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.SeenFlag}
	case "mark_unread":
		return imap.FormatFlagsOp(imap.RemoveFlags, true), []interface{}{imap.SeenFlag}
	case "flag":
		return imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.FlaggedFlag}
	case "unflag":
		return imap.FormatFlagsOp(imap.RemoveFlags, true), []interface{}{imap.FlaggedFlag}
	}
	for _, l := range req.Labels {
		vals = append(vals, l)
	}
	sign := "+"
	var op imap.FlagsOp = imap.AddFlags
	if req.Action == "remove_labels" {
		sign = "-"
		op = imap.RemoveFlags
	}
	if isGmailAccount(account) {
		return imap.StoreItem(sign + "X-GM-LABELS.SILENT"), vals
	}
	// Other servers keep labels as IMAP keywords: no spaces or specials.
	kw := make([]interface{}, 0, len(vals))
	for _, v := range vals {
		kw = append(kw, imapKeyword(v.(string)))
	}
	return imap.FormatFlagsOp(op, true), kw
}

func imapKeyword(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r <= ' ' || r == 0x7f || strings.ContainsRune(`(){%*"\]`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Organize implements MailOrganizer over IMAP.
func (d *NetDialer) Organize(ctx context.Context, account Account, secret string, req OrganizeRequest) (int, error) {
	if isPOP3(account) {
		return 0, classErr("unsupported", MsgPOP3Unsupported)
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return 0, err
	}
	defer c.Logout()
	return organizeOn(c, account, req)
}

func organizeOn(c *imapclient.Client, account Account, req OrganizeRequest) (int, error) {
	box := strings.TrimSpace(req.Box)
	if box == "" {
		box = "INBOX"
	}
	if _, err := c.Select(box, false); err != nil {
		return 0, mapIMAP(err, "select")
	}
	if req.Action == "move" {
		if err := moveUIDs(c, req.UIDs, req.Dest); err != nil {
			return 0, err
		}
		return len(req.UIDs), nil
	}
	item, vals := organizeStore(account, req)
	seq := uidSeq(req.UIDs)
	if err := c.UidStore(seq, item, vals, nil); err != nil {
		return 0, mapIMAP(err, "store")
	}
	return len(req.UIDs), nil
}

func OrganizeInvocationSchema() map[string]interface{} { return organizeSchema() }

func OrganizeProperties() map[string]interface{} { return schemaProperties(organizeSchema()) }

func ToolDescriptionOrganize() string {
	return "整理已配置邮箱里的邮件（可撤销，不删除、不发信）。action：move（移到 to_box 文件夹）、mark_read、mark_unread、flag（加星标）、unflag、add_labels、remove_labels（Gmail 是标签，其他邮箱写成关键字）。uids 来自 mailbox_query 的结果，box 是这些邮件当前所在文件夹。POP3 邮箱不支持。删除请用 mailbox_delete。"
}

func organizeSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":  map[string]interface{}{"type": "string", "enum": []string{"move", "mark_read", "mark_unread", "flag", "unflag", "add_labels", "remove_labels"}},
			"account": map[string]interface{}{"type": "string", "description": "邮箱名，不填用默认邮箱"},
			"box":     map[string]interface{}{"type": "string", "description": "邮件当前所在文件夹，默认 INBOX"},
			"uids":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}, "description": "邮件 UID，最多 200 个"},
			"to_box":  map[string]interface{}{"type": "string", "description": "move 的目标文件夹"},
			"labels":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required":             []string{"action", "uids"},
		"additionalProperties": false,
	}
}
