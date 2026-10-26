package mailbox

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// DraftKindDelete marks a frozen deletion. An empty kind stays a send.
	DraftKindDelete = "delete"

	DeleteScopeFolder  = "folder"
	DeleteScopeAccount = "account"

	// DeleteUIDCap is the explicit uid list. all has no such cap.
	DeleteUIDCap = 100
	// deleteUIDBatch is one UID set per IMAP command.
	deleteUIDBatch = 200
	// deleteSampleCap is how many subjects the confirmation card shows.
	deleteSampleCap = 5
	// deleteFolderCardCap is how many folders the card lists before a remainder.
	deleteFolderCardCap = 8
	// deleteCardFieldCap limits one untrusted card field. A subject is not a
	// second instruction, and a huge header must not flood the confirmation.
	deleteCardFieldCap = 120
)

// MailDeleter previews and applies one frozen deletion. It is separate from
// MailReader so a read fake does not have to move mail.
type MailDeleter interface {
	PreviewDelete(ctx context.Context, account Account, secret string, req DeleteRequest) (DeletePreview, error)
	ApplyDelete(ctx context.Context, account Account, secret string, plan DeletePlan) (DeleteResult, error)
}

// FolderPageDeleter deletes one folder's messages and reads the newest page
// that remains, on one login. Browse uses it when the dialer implements it.
// Agent deletes stay on MailDeleter and do not list.
type FolderPageDeleter interface {
	DeleteFolderPage(ctx context.Context, account Account, secret, box string, uids []uint32, limit int) (FolderDeletePage, error)
}

// FolderDeletePage is a finished folder delete plus the page that remains.
// ListErr is set only when the delete itself succeeded and the following
// page read failed. Messages is unset in that case.
type FolderDeletePage struct {
	Result   DeleteResult
	Messages []MessageSummary
	ListErr  error
}

// DeleteRequest is the parsed trash call. Scope account ignores Box and UIDs.
type DeleteRequest struct {
	Scope string
	Box   string
	All   bool
	UIDs  []uint32
}

// DeleteFolder is one mailbox the frozen plan will change.
// Permanent means the messages are expunged. Otherwise they move to Trash.
// UIDValidity is the value from the preview SELECT. Zero means the server
// omitted it. Apply refuses the folder when a later SELECT reports another
// non-zero value, because the frozen UIDs would name different messages.
type DeleteFolder struct {
	Name        string
	UIDs        []uint32
	Permanent   bool
	UIDValidity uint32
}

// DeletePlan is the exact set Approve will delete. It does not grow if new
// mail arrives, except an account purge re-reads every trash folder because
// MOVE assigns new UIDs and mail can arrive while the user is confirming.
// ExtraTrash is every \Trash folder other than TrashBox.
type DeletePlan struct {
	Scope      string
	Box        string
	TrashBox   string
	ExtraTrash []string
	All        bool
	PurgeTrash bool
	UIDs       []uint32
	Folders    []DeleteFolder
}

// DeleteFolderLine is one count line on the confirmation card.
type DeleteFolderLine struct {
	Name  string
	Count int
}

// DeletePreview is the frozen plan plus the subjects the card may show.
type DeletePreview struct {
	Plan    DeletePlan
	Lines   []DeleteFolderLine
	Samples []string
	Total   int
}

// DeleteResult counts messages moved into trash and messages expunged.
type DeleteResult struct {
	Moved  int
	Purged int
}

// Delete freezes a confirmation. It does not move or expunge mail.
func (h *Host) Delete(ctx context.Context, args map[string]interface{}) string {
	started := time.Now()
	var accountID string
	var count int
	text, class := h.remove(ctx, args, &accountID, &count)
	log.Printf("[mailbox] account_id=%s side=%s action=%s result_class=%s duration_ms=%d count=%d",
		accountID, SideInbound, "trash", class, time.Since(started).Milliseconds(), count)
	return text
}

func (h *Host) remove(ctx context.Context, args map[string]interface{}, accountID *string, count *int) (string, string) {
	if err := rejectArgs(args, deleteArgs); err != nil {
		return err.Error(), "rejected_arg"
	}
	if text, blocked := h.pendingBlock(); blocked {
		return text, "pending"
	}
	if reason := DeleteShapeReason(args); reason != "" {
		return deleteShapeText(reason), "invalid"
	}
	account, secret, err := h.openInbound(ctx, stringArg(args, "account"))
	if err != nil {
		return err.Error(), classOf(err)
	}
	if accountID != nil {
		*accountID = account.ID
	}
	deleter := h.deleter()
	if deleter == nil {
		return MsgConnect, "connect"
	}
	req := deleteRequest(args)
	preview, err := deleter.PreviewDelete(ctx, account, secret, req)
	if err != nil {
		return transportText(err), classOf(err)
	}
	if preview.Total <= 0 {
		if req.Scope == DeleteScopeFolder && !req.All {
			return MsgNotFound, "not_found"
		}
		return MsgEmpty, "empty"
	}
	// An empty folder must not burn the turn. The budget starts only once
	// there is something to freeze.
	if err := h.Budget.ConsumeDelete(); err != nil {
		return err.Error(), "budget"
	}
	h.bumpTool()
	draft := Draft{
		ID: h.draftID(), Account: account, Kind: DraftKindDelete, Delete: preview.Plan,
		ExpiresAt: h.now().Add(DraftTTL),
		Principal: h.Principal, Session: h.Session,
	}
	if h.Drafts == nil {
		h.Drafts = NewDraftStore()
	}
	h.Drafts.Put(draft)
	if count != nil {
		*count = preview.Total
	}
	return askUserConfirm(deleteConfirmationCard(account, preview)), "confirm"
}

func (h *Host) deleter() MailDeleter {
	if h == nil {
		return nil
	}
	if h.Deleter != nil {
		return h.Deleter
	}
	return DeleterFromReader(h.Reader)
}

// DeleterFromReader uses the IMAP dialer when it implements deletion.
// A test reader that only lists mail stays unable to delete.
func DeleterFromReader(reader MailReader) MailDeleter {
	deleter, _ := reader.(MailDeleter)
	return deleter
}

func (h *Host) pendingBlock() (string, bool) {
	if h == nil || h.Drafts == nil {
		return "", false
	}
	draft, ok := h.Drafts.Pending(h.Principal, h.Session)
	if !ok {
		return "", false
	}
	if !draft.ExpiresAt.IsZero() && !h.now().Before(draft.ExpiresAt) {
		h.Drafts.Drop(draft.ID)
		return "", false
	}
	if draft.Kind == DraftKindDelete {
		return MsgDeletePending, true
	}
	return MsgPending, true
}

func (h *Host) approveDelete(ctx context.Context, draft Draft) (text string, class string) {
	started := time.Now()
	count := 0
	defer func() {
		log.Printf("[mailbox] account_id=%s side=%s action=%s result_class=%s duration_ms=%d count=%d",
			draft.Account.ID, SideInbound, "trash", class, time.Since(started).Milliseconds(), count)
	}()
	secret, err := h.secretFor(ctx, draft.Account, SideInbound)
	if err != nil {
		return err.Error(), classOf(err)
	}
	deleter := h.deleter()
	if deleter == nil {
		h.noteUncertain(draft)
		h.countSend("uncertain")
		h.auditDelete(draft, "uncertain", 0)
		return MsgDeleteUncertain, "uncertain"
	}
	result, err := deleter.ApplyDelete(ctx, draft.Account, secret, draft.Delete)
	if err != nil {
		class = classOf(err)
		if class == "uncertain" {
			count = deletedAuditCount(draft.Delete, result)
			h.noteUncertain(draft)
			h.countSend("uncertain")
			h.auditDelete(draft, "uncertain", count)
			return MsgDeleteUncertain, "uncertain"
		}
		h.countSend(class)
		h.auditDelete(draft, class, 0)
		return transportText(err), class
	}
	text = deleteDoneText(result)
	count = deletedAuditCount(draft.Delete, result)
	h.countSend("approved")
	h.auditDelete(draft, "approved", count)
	if err := h.finishStep("mailbox_delete", text); err != nil {
		return err.Error(), "error"
	}
	return text, "approved"
}

func (h *Host) auditDelete(draft Draft, result string, count int) {
	if h == nil || h.Audit == nil {
		return
	}
	h.Audit(AuditEvent{
		AccountID:    draft.Account.ID,
		Result:       result,
		Tool:         "mailbox_delete",
		DeletedCount: count,
	})
}

// deletedAuditCount is how many messages the apply actually accounted for.
// An account purge expunges trash after MOVE, and that search already includes
// the messages just moved there. Adding Moved again would count them twice.
// A purge that stops early leaves Purged at zero, so Moved is the progress.
func deletedAuditCount(plan DeletePlan, result DeleteResult) int {
	if plan.PurgeTrash && result.Purged > 0 {
		return result.Purged
	}
	return result.Moved + result.Purged
}

func deleteDoneText(result DeleteResult) string {
	switch {
	case result.Moved > 0 && result.Purged > 0:
		return MsgDeleteMovedAndPurged
	case result.Purged > 0:
		return MsgDeletePurged
	default:
		return MsgDeleteMoved
	}
}

func deleteConfirmationCard(account Account, preview DeletePreview) string {
	var b strings.Builder
	b.WriteString("请确认删除这些邮件\n")
	fmt.Fprintf(&b, "邮箱：%s <%s>\n", cardField(account.Name), cardField(account.Address))
	if preview.Plan.Scope == DeleteScopeAccount {
		b.WriteString("范围：整个邮箱\n")
	} else {
		fmt.Fprintf(&b, "范围：%s\n", cardField(preview.Plan.Box))
	}
	fmt.Fprintf(&b, "数量：%d\n", preview.Total)
	writeDeleteFolderLines(&b, preview.Lines)
	b.WriteString(deleteEffectSentence(preview.Plan))
	b.WriteByte('\n')
	writeDeleteSamples(&b, preview.Samples)
	return strings.TrimRight(b.String(), "\n")
}

func writeDeleteFolderLines(b *strings.Builder, lines []DeleteFolderLine) {
	if len(lines) == 0 {
		return
	}
	b.WriteString("文件夹：\n")
	shown := len(lines)
	if shown > deleteFolderCardCap {
		shown = deleteFolderCardCap
	}
	for _, line := range lines[:shown] {
		fmt.Fprintf(b, "- %s：%d\n", cardField(line.Name), line.Count)
	}
	if rest := len(lines) - shown; rest > 0 {
		fmt.Fprintf(b, "还有 %d 个文件夹\n", rest)
	}
}

func writeDeleteSamples(b *strings.Builder, samples []string) {
	if len(samples) == 0 {
		return
	}
	b.WriteString("示例：\n")
	for _, sample := range samples {
		sample = cardField(sample)
		if sample == "" {
			sample = "（无主题）"
		}
		fmt.Fprintf(b, "- %s\n", sample)
	}
}

// cardField flattens a header onto one line and caps it. oneLine itself
// stays shared and unlimited; only the confirmation card is truncated.
func cardField(s string) string {
	return truncateRunes(oneLine(s), deleteCardFieldCap)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func deleteEffectSentence(plan DeletePlan) string {
	if plan.Scope == DeleteScopeAccount {
		if strings.TrimSpace(plan.TrashBox) == "" {
			return "没有已删除文件夹，确认后这些邮件会被永久删除。"
		}
		return "确认后，整个邮箱里的邮件会被永久删除，包括已删除文件夹里的邮件。确认等待期间新进入已删除的邮件也会被永久删除。"
	}
	if plan.PurgeTrash || len(plan.Folders) == 0 || plan.Folders[0].Permanent || strings.TrimSpace(plan.TrashBox) == "" {
		return "确认后这些邮件会被永久删除。"
	}
	return fmt.Sprintf("确认后这些邮件会移到「%s」。", cardField(plan.TrashBox))
}

var deleteArgs = map[string]bool{
	"action": true, "account": true, "box": true, "uids": true, "all": true, "scope": true,
}

// DeleteShapeReason reports a call that cannot freeze a draft. It does not
// open a mailbox. A one-shot grant must be refused before admission when
// this is non-empty.
func DeleteShapeReason(args map[string]interface{}) string {
	if args == nil {
		args = map[string]interface{}{}
	}
	if strings.TrimSpace(stringArg(args, "action")) != "trash" {
		return "mailbox_delete_action_required"
	}
	uids, uidReason := parseUIDList(args["uids"])
	if uidReason != "" {
		return uidReason
	}
	if len(uids) > DeleteUIDCap {
		return "mailbox_delete_uid_limit"
	}
	all, allOK, allPresent := boolArgPresent(args, "all")
	if allPresent && !allOK {
		return "mailbox_delete_target_required"
	}
	scope, scopeOK := deleteScope(stringArg(args, "scope"))
	if !scopeOK {
		return "mailbox_delete_scope_invalid"
	}
	if scope == DeleteScopeAccount {
		if len(uids) > 0 || strings.TrimSpace(stringArg(args, "box")) != "" {
			return "mailbox_delete_scope_conflict"
		}
		return ""
	}
	if all == (len(uids) > 0) {
		return "mailbox_delete_target_required"
	}
	return ""
}

func deleteShapeText(reason string) string {
	switch reason {
	case "mailbox_delete_scope_invalid":
		return "范围只能是 folder 或 account"
	case "mailbox_delete_scope_conflict":
		return "删除整个邮箱时不要再指定文件夹或具体邮件"
	case "mailbox_delete_target_required":
		return "需要指定要删除的邮件，或删除整个文件夹"
	case "mailbox_delete_uid_limit":
		return "一次最多指定 100 封邮件"
	case "mailbox_delete_uid_invalid":
		return "邮件编号无效"
	default:
		return "不支持的删除操作"
	}
}

func deleteRequest(args map[string]interface{}) DeleteRequest {
	uids, _ := parseUIDList(args["uids"])
	all, _, _ := boolArgPresent(args, "all")
	scope, _ := deleteScope(stringArg(args, "scope"))
	return DeleteRequest{
		Scope: scope,
		Box:   strings.TrimSpace(stringArg(args, "box")),
		All:   all,
		UIDs:  uids,
	}
}

func deleteScope(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DeleteScopeFolder:
		return DeleteScopeFolder, true
	case DeleteScopeAccount:
		return DeleteScopeAccount, true
	default:
		return "", false
	}
}

func boolArgPresent(args map[string]interface{}, key string) (value bool, ok bool, present bool) {
	if args == nil {
		return false, true, false
	}
	raw, present := args[key]
	if !present || raw == nil {
		return false, true, false
	}
	value, ok = raw.(bool)
	return value, ok, true
}

func parseUIDList(raw interface{}) ([]uint32, string) {
	if raw == nil {
		return nil, ""
	}
	var items []interface{}
	switch v := raw.(type) {
	case []interface{}:
		if len(v) == 0 {
			return nil, ""
		}
		items = v
	case []uint32:
		return dedupeUIDs(v)
	default:
		return nil, "mailbox_delete_uid_invalid"
	}
	out := make([]uint32, 0, len(items))
	for _, item := range items {
		n, ok := uidValue(item)
		if !ok {
			return nil, "mailbox_delete_uid_invalid"
		}
		out = append(out, n)
	}
	return dedupeUIDs(out)
}

func dedupeUIDs(in []uint32) ([]uint32, string) {
	if len(in) == 0 {
		return nil, ""
	}
	seen := make(map[uint32]bool, len(in))
	out := make([]uint32, 0, len(in))
	for _, n := range in {
		if n == 0 {
			return nil, "mailbox_delete_uid_invalid"
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, ""
}

func uidValue(raw interface{}) (uint32, bool) {
	switch v := raw.(type) {
	case float64:
		if v <= 0 || v > math.MaxUint32 || v != math.Trunc(v) {
			return 0, false
		}
		return uint32(v), true
	case int:
		if v <= 0 || uint64(v) > math.MaxUint32 {
			return 0, false
		}
		return uint32(v), true
	case int32:
		if v <= 0 {
			return 0, false
		}
		return uint32(v), true
	case int64:
		if v <= 0 || v > math.MaxUint32 {
			return 0, false
		}
		return uint32(v), true
	case uint32:
		if v == 0 {
			return 0, false
		}
		return v, true
	case jsonInt64:
		i, err := v.Int64()
		if err != nil || i <= 0 || i > math.MaxUint32 {
			return 0, false
		}
		return uint32(i), true
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
		if err != nil || n == 0 {
			return 0, false
		}
		return uint32(n), true
	default:
		return 0, false
	}
}

// jsonInt64 is the encoding/json number type. parseUIDList accepts it when
// the decoder used UseNumber. messages.go already binds the name jsonNumber.
type jsonInt64 interface {
	Int64() (int64, error)
}

func DeleteProperties() map[string]interface{} {
	return schemaProperties(deleteSchema())
}

func DeleteInvocationSchema() map[string]interface{} { return deleteSchema() }

func deleteSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":  map[string]interface{}{"type": "string", "enum": []string{"trash"}},
			"account": map[string]interface{}{"type": "string", "description": "邮箱名字。不填则使用默认邮箱。"},
			"box":     map[string]interface{}{"type": "string", "description": "IMAP 文件夹。不填则用帐号上的收件箱。scope 为 account 时不要填。"},
			"uids":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}, "description": "要删除的邮件 UID。与 all 二选一，最多 100 个。"},
			"all":     map[string]interface{}{"type": "boolean", "description": "为 true 时删除该文件夹里的全部邮件。"},
			"scope":   map[string]interface{}{"type": "string", "enum": []string{"folder", "account"}, "description": "folder 删除一个文件夹。account 清空整个邮箱，包括已删除。"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func ToolDescriptionDelete() string {
	return "删除已配置邮箱里的邮件。action 只能是 trash。不填 account 就用默认邮箱。scope 为 folder（默认）时，用 uids 指定邮件，或 all=true 删除该文件夹里的全部邮件。scope 为 account 时清空每个可选文件夹，并清空已删除。第一次调用只生成确认，不会删除。用户确认后才执行。不能在参数里提供确认标记或密码。"
}
