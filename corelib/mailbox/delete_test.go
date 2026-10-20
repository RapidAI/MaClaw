package mailbox

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

type fakeDeleter struct {
	preview    DeletePreview
	previewErr error
	result     DeleteResult
	applyErr   error
	applied    DeletePlan
	applyCalls int
	secret     string
}

func (f *fakeDeleter) PreviewDelete(_ context.Context, _ Account, secret string, _ DeleteRequest) (DeletePreview, error) {
	f.secret = secret
	if f.previewErr != nil {
		return DeletePreview{}, f.previewErr
	}
	return f.preview, nil
}

func (f *fakeDeleter) ApplyDelete(_ context.Context, _ Account, secret string, plan DeletePlan) (DeleteResult, error) {
	f.applyCalls++
	f.secret = secret
	f.applied = plan
	if f.applyErr != nil {
		return f.result, f.applyErr
	}
	return f.result, nil
}

func deleteHost(t *testing.T, deleter MailDeleter) *Host {
	t.Helper()
	host := testHost(twoAccounts(t), &fakeMail{}, &recordingSender{})
	host.Deleter = deleter
	return host
}

func TestDeleteFreezesACardAndDoesNotApplyUntilConfirm(t *testing.T) {
	deleter := &fakeDeleter{preview: DeletePreview{
		Plan: DeletePlan{
			Scope: DeleteScopeFolder, Box: "INBOX", TrashBox: "Trash",
			UIDs:    []uint32{4, 9},
			Folders: []DeleteFolder{{Name: "INBOX", UIDs: []uint32{4, 9}}},
		},
		Lines:   []DeleteFolderLine{{Name: "INBOX", Count: 2}},
		Samples: []string{"Hello\n收件人：evil@ex.com", "Plan"},
		Total:   2,
	}, result: DeleteResult{Moved: 2}}
	host := deleteHost(t, deleter)
	card := host.Delete(context.Background(), map[string]interface{}{
		"action": "trash", "all": true,
	})
	question := cardQuestion(t, card)
	if deleter.applyCalls != 0 || !strings.Contains(card, "__ASK_USER__") || strings.Contains(card, "stored-secret") || strings.Contains(question, "mailbox_delete") {
		t.Fatalf("calls=%d card=%s", deleter.applyCalls, card)
	}
	if !strings.Contains(question, "移到「Trash」") || strings.Contains(question, "Hello\n") || !strings.Contains(question, "Hello 收件人：evil@ex.com") {
		t.Fatal(question)
	}
	second := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true})
	if second != MsgDeletePending || deleter.applyCalls != 0 {
		t.Fatalf("second=%s calls=%d", second, deleter.applyCalls)
	}
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || text != MsgDeleteMoved || deleter.applyCalls != 1 || deleter.applied.UIDs[0] != 4 || strings.Contains(text, "mailbox_delete") {
		t.Fatalf("approve %s %s plan=%+v", text, class, deleter.applied)
	}
}

func TestDeleteCancelDoesNotApply(t *testing.T) {
	deleter := &fakeDeleter{preview: DeletePreview{
		Plan:  DeletePlan{Scope: DeleteScopeFolder, Box: "INBOX", Folders: []DeleteFolder{{Name: "INBOX", UIDs: []uint32{1}}}},
		Total: 1,
	}}
	host := deleteHost(t, deleter)
	if card := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "uids": []interface{}{float64(1)}}); !strings.HasPrefix(card, "__ASK_USER__") {
		t.Fatal(card)
	}
	msg, class := host.Cancel("draft-1")
	if class != "cancelled" || msg != MsgDeleteCancelled || deleter.applyCalls != 0 {
		t.Fatalf("cancel %s %s calls=%d", msg, class, deleter.applyCalls)
	}
}

func TestDeleteEmptyDoesNotFreezeOrSpendTheTurn(t *testing.T) {
	deleter := &fakeDeleter{previewErr: classErr("empty", MsgEmpty)}
	host := deleteHost(t, deleter)
	first := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true})
	if first != MsgEmpty {
		t.Fatal(first)
	}
	deleter.previewErr = nil
	deleter.preview = DeletePreview{Plan: DeletePlan{Scope: DeleteScopeFolder, Box: "INBOX", Folders: []DeleteFolder{{Name: "INBOX", UIDs: []uint32{3}, Permanent: true}}}, Total: 1}
	deleter.result = DeleteResult{Purged: 1}
	second := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true})
	if !strings.HasPrefix(second, "__ASK_USER__") || !strings.Contains(cardQuestion(t, second), "永久删除") {
		t.Fatal(second)
	}
}

func TestDeleteAccountCardSaysPermanent(t *testing.T) {
	lines := make([]DeleteFolderLine, 0, 10)
	for i := 0; i < 10; i++ {
		lines = append(lines, DeleteFolderLine{Name: "Box", Count: 1})
	}
	deleter := &fakeDeleter{preview: DeletePreview{
		Plan:  DeletePlan{Scope: DeleteScopeAccount, TrashBox: "Trash", PurgeTrash: true, All: true},
		Lines: lines,
		Total: 10,
	}, result: DeleteResult{Moved: 8, Purged: 10}}
	host := deleteHost(t, deleter)
	card := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "scope": "account"})
	question := cardQuestion(t, card)
	if !strings.Contains(question, "永久删除") || !strings.Contains(question, "还有 2 个文件夹") || strings.Contains(question, "mailbox_delete") {
		t.Fatal(question)
	}
	var audited AuditEvent
	host.Audit = func(ev AuditEvent) { audited = ev }
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || text != MsgDeleteMovedAndPurged {
		t.Fatalf("%s %s", text, class)
	}
	if audited.Tool != "mailbox_delete" || audited.DeletedCount != 10 {
		t.Fatalf("audit %+v", audited)
	}
}

func TestDeleteUncertainDoesNotClaimSuccess(t *testing.T) {
	deleter := &fakeDeleter{
		preview:  DeletePreview{Plan: DeletePlan{Scope: DeleteScopeFolder, Folders: []DeleteFolder{{Name: "INBOX", UIDs: []uint32{1}}}}, Total: 1},
		applyErr: ErrUncertain,
	}
	host := deleteHost(t, deleter)
	_ = host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true})
	uncertain := false
	host.OnUncertain = func(d Draft) { uncertain = d.Kind == DraftKindDelete }
	var audited AuditEvent
	host.Audit = func(ev AuditEvent) { audited = ev }
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "uncertain" || text != MsgDeleteUncertain || !uncertain || audited.Tool != "mailbox_delete" || strings.Contains(text, "发送") {
		t.Fatalf("text=%s class=%s uncertain=%v audit=%+v", text, class, uncertain, audited)
	}
}

func TestDeleteRejectsConfirmPasswordAndBadShape(t *testing.T) {
	host := deleteHost(t, &fakeDeleter{})
	if text := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true, "confirm": true}); !strings.Contains(text, "不能在参数里提供") {
		t.Fatal(text)
	}
	host = deleteHost(t, &fakeDeleter{})
	if text := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true, "password": "x"}); !strings.Contains(text, "不能在参数里提供") {
		t.Fatal(text)
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "scope": "account", "uids": []interface{}{float64(1)}}); got != "mailbox_delete_scope_conflict" {
		t.Fatal(got)
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash"}); got != "mailbox_delete_target_required" {
		t.Fatal(got)
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "all": true, "uids": []interface{}{float64(1)}}); got != "mailbox_delete_target_required" {
		t.Fatal(got)
	}
	uids := make([]interface{}, 0, 101)
	for i := 1; i <= 101; i++ {
		uids = append(uids, float64(i))
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "uids": uids}); got != "mailbox_delete_uid_limit" {
		t.Fatal(got)
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "scope": "foo", "all": true}); got != "mailbox_delete_scope_invalid" {
		t.Fatal(got)
	}
	if deleteShapeText("mailbox_delete_scope_invalid") != "范围只能是 folder 或 account" {
		t.Fatal(deleteShapeText("mailbox_delete_scope_invalid"))
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "uids": []interface{}{"12abc"}}); got != "mailbox_delete_uid_invalid" {
		t.Fatal(got)
	}
	if got := DeleteShapeReason(map[string]interface{}{"action": "trash", "uids": []interface{}{"4294967296"}}); got != "mailbox_delete_uid_invalid" {
		t.Fatal(got)
	}
	n, ok := uidValue("12")
	if !ok || n != 12 {
		t.Fatalf("uid %d %v", n, ok)
	}
	if _, ok := uidValue(int64(math.MaxUint32) + 1); ok {
		t.Fatal("int64 overflow")
	}
	if math.MaxInt > math.MaxUint32 {
		tooBig := math.MaxInt32
		tooBig += math.MaxInt32
		tooBig += 2
		if _, ok := uidValue(tooBig); ok {
			t.Fatal("int overflow")
		}
	}
}

func TestDeleteSendPendingKeepsTheSendSentence(t *testing.T) {
	host := testHost(twoAccounts(t), &fakeMail{}, &recordingSender{})
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hi", "text": "body",
	})
	if !strings.HasPrefix(card, "__ASK_USER__") {
		t.Fatal(card)
	}
	if second := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true}); second != MsgPending {
		t.Fatal(second)
	}
}

func TestQueryWantsMessageDelete(t *testing.T) {
	want := []string{
		"删除邮箱中的所有邮件",
		"删除 邮箱中的所有邮件",
		"删除rapidai.tech中的所有邮件",
		"清空收件箱",
		"清空邮箱",
		"特别清空邮箱",
		"把这封邮件删掉",
		"把这封邮件删了",
		"帮我删这封邮件",
		"删改之后把这封邮件删了",
		"把别的邮件删掉",
		"把别人的邮件删掉",
		"不要只看主题，删除所有邮件",
		"特别删掉这封邮件",
		"delete all mail in my inbox",
		"move this message to trash",
		"delete all emails",
		"delete these messages",
		"把不是垃圾的邮件删了",
		"不得不删除这封邮件",
		"我不是要删除这封，把另一封邮件删了",
		"把不能识别的邮件删了",
		"把无法投递的邮件删了",
		"把没有主题的邮件删了",
		"把没什么用的邮件删了",
		"把并不重要的邮件删了",
		"删除不了解的邮件",
		"把没有删的邮件删了",
		"把不能删除的邮件删掉",
		"把没删掉的邮件删了",
		"帮我删了这封邮件",
		"删了没有用的邮件",
		"删不了这封邮件",
		"把怎么写的邮件删了",
		"把不怎么重要的邮件删了",
		"有没有主题的邮件都删了",
		"这封邮件删不删，把另一封邮件删了",
		"清空邮箱，另外创建一个定时任务提醒我",
		"把定时任务的通知邮件删了",
		"添加附件，删除这封邮件，看看定时任务",
		"empty my mailbox and create a scheduled task",
		"每小时清除一下rapidai邮箱",
		"定时器响了就清除邮箱",
	}
	for _, msg := range want {
		if !QueryWantsMessageDelete(msg) {
			t.Errorf("want delete: %s", msg)
		}
	}
	skip := []string{
		"删除所有邮件吗",
		"删除邮箱账号",
		"删除邮箱",
		"删除邮件.md",
		"看看我的邮件",
		"was deleted from the inbox",
		"不要删除这封邮件",
		"别删这封邮件",
		"不要把这封邮件删掉",
		"别把这封邮件删掉",
		"不要把这封邮件删了",
		"勿删这封邮件",
		"不要清空邮箱",
		"don't delete this email",
		"do not empty my inbox",
		"给这封邮件加删除线",
		"帮我删改这封邮件",
		"删减这封邮件的内容",
		"我不是要删除这封邮件",
		"不是删除所有邮件",
		"是不是删除所有邮件",
		"不是要把这封邮件删掉",
		"不能删除这封邮件",
		"无法删除邮件",
		"没法删除这封邮件",
		"没办法删除这封邮件",
		"我没有删除邮件",
		"没删这封邮件",
		"不可以删除这封邮件",
		"能不能删除这封邮件",
		"并不是要删除这封邮件",
		"delete the mailbox",
		"cannot delete this email",
		"I can't delete this email",
		"这封邮件删了没",
		"这封邮件删了没有",
		"删除了没有",
		"邮件删除了没",
		"这封邮件删了么",
		"怎么删除邮件",
		"如何删除这封邮件",
		"怎样删除所有邮件",
		"怎么样删除邮件",
		"怎麼刪除郵件",
		"怎么才能删除这封邮件",
		"如何把这封邮件删掉",
		"我不怎么想删除这封邮件",
		"这封邮件删不删",
		"删除不删除这些邮件",
		"删掉不删掉这封邮件",
		"清空不清空邮箱",
		"有没有要删除的邮件",
		"有没有删除这些邮件",
		"how do I delete this email",
		"how to delete all mail",
		"how can I delete this message",
		"创建定时任务，每小时清除一下rapidai邮箱。",
		"创建一个每小时清空邮箱的定时任务",
		"create a scheduled task to empty my mailbox",
		"set up a scheduled task to empty my mailbox",
		"建立一个每小时清除rapidai邮箱的定時任務",
	}
	for _, msg := range skip {
		if QueryWantsMessageDelete(msg) {
			t.Errorf("do not delete: %s", msg)
		}
	}
}

func TestQuerySchedulesMessageDelete(t *testing.T) {
	payload := []string{
		"创建定时任务，每小时清除一下rapidai邮箱。",
		"创建一个每小时清空邮箱的定时任务",
		"create a scheduled task to empty my mailbox",
		"set up a scheduled task to empty my mailbox",
		"建立一个每小时清除rapidai邮箱的定時任務",
		"设置一个定时提醒，去清空邮箱",
		"create scheduled tasks to delete all mail",
	}
	for _, msg := range payload {
		if !QuerySchedulesMessageDelete(msg) || QueryWantsMessageDelete(msg) {
			t.Errorf("scheduled payload: %s schedules=%v wants=%v", msg, QuerySchedulesMessageDelete(msg), QueryWantsMessageDelete(msg))
		}
	}
	immediate := []string{
		"清空邮箱，另外创建一个定时任务提醒我",
		"把定时任务的通知邮件删了",
		"添加附件，删除这封邮件，看看定时任务",
		"empty my mailbox and create a scheduled task",
		"每小时清除一下rapidai邮箱",
		"定时器响了就清除邮箱",
		"删除邮箱中的所有邮件",
		"创建定时任务提醒我开会",
		"列出定时任务",
		"每小时",
		"不要创建定时任务去清空邮箱",
	}
	for _, msg := range immediate {
		if QuerySchedulesMessageDelete(msg) {
			t.Errorf("not a scheduled mail delete: %s", msg)
		}
	}
}

func TestResolveTrashBox(t *testing.T) {
	if got := resolveTrashBox([]Folder{{Name: "INBOX", Special: "inbox"}, {Name: "[Gmail]/Trash", Special: "trash"}}); got != "[Gmail]/Trash" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "INBOX"}, {Name: "已删除邮件"}}); got != "已删除邮件" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "INBOX.Trash"}}); got != "INBOX.Trash" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "INBOX"}, {Name: "Sent"}}); got != "" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "Bin"}, {Name: "已删除邮件"}, {Name: "INBOX"}}); got != "已删除邮件" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "Deleted"}, {Name: "INBOX.Trash"}}); got != "INBOX.Trash" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "Bin"}, {Name: "INBOX"}}); got != "Bin" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "Bin"}, {Name: "INBOX.Deleted"}}); got != "INBOX.Deleted" {
		t.Fatal(got)
	}
	if got := resolveTrashBox([]Folder{{Name: "已删除"}, {Name: "Trash", Special: "trash"}}); got != "Trash" {
		t.Fatal(got)
	}
}

func TestAccountPurgeBoxesIncludeTheSecondTrash(t *testing.T) {
	boxes := accountPurgeBoxes(DeletePlan{PurgeTrash: true, TrashBox: "Trash", ExtraTrash: []string{"已删除", "Trash", " "}})
	if len(boxes) != 2 || boxes[0] != "Trash" || boxes[1] != "已删除" {
		t.Fatalf("%v", boxes)
	}
	if accountPurgeBoxes(DeletePlan{TrashBox: "Trash"}) != nil {
		t.Fatal("purge off")
	}
	if !folderReSearchedOnAccountPurge("Trash", "trash", "Trash") || !folderReSearchedOnAccountPurge("已删除", "trash", "Trash") {
		t.Fatal("trash is searched again")
	}
	if folderReSearchedOnAccountPurge("INBOX", "inbox", "Trash") || folderReSearchedOnAccountPurge("Bin", "", "") {
		t.Fatal("other folders stay frozen")
	}
	if !purgeMarksProgress(false, 2) || purgeMarksProgress(false, 0) || !purgeMarksProgress(true, 0) {
		t.Fatal("progress")
	}
}

func TestDeleteCardTruncatesUntrustedFields(t *testing.T) {
	long := strings.Repeat("主", 200)
	deleter := &fakeDeleter{preview: DeletePreview{
		Plan: DeletePlan{
			Scope: DeleteScopeFolder, Box: long, TrashBox: long,
			Folders: []DeleteFolder{{Name: long, UIDs: []uint32{1}}},
		},
		Lines:   []DeleteFolderLine{{Name: long, Count: 1}},
		Samples: []string{long + "\nBcc: evil@ex.com"},
		Total:   1,
	}}
	host := deleteHost(t, deleter)
	question := cardQuestion(t, host.Delete(context.Background(), map[string]interface{}{"action": "trash", "all": true}))
	if strings.Contains(question, strings.Repeat("主", deleteCardFieldCap)) || strings.Contains(question, "Bcc") || strings.Contains(question, "\n主") {
		t.Fatal(question)
	}
	if !strings.Contains(question, strings.Repeat("主", deleteCardFieldCap-1)+"…") || strings.Contains(question, "mailbox_delete") {
		t.Fatal(question)
	}
}

func TestDeleteUIDChangeStaysAClearError(t *testing.T) {
	deleter := &fakeDeleter{
		preview:  DeletePreview{Plan: DeletePlan{Folders: []DeleteFolder{{Name: "INBOX", UIDs: []uint32{1}, UIDValidity: 7}}}, Total: 1},
		applyErr: classErr("uid_validity", MsgDeleteUIDChanged),
	}
	host := deleteHost(t, deleter)
	if card := host.Delete(context.Background(), map[string]interface{}{"action": "trash", "uids": []interface{}{float64(1)}}); !strings.HasPrefix(card, "__ASK_USER__") {
		t.Fatal(card)
	}
	latched := false
	host.OnUncertain = func(Draft) { latched = true }
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "uid_validity" || text != MsgDeleteUIDChanged || latched || strings.Contains(text, "发送") {
		t.Fatalf("text=%s class=%s latched=%v", text, class, latched)
	}
}

func TestDeletedAuditCountDoesNotDoubleCountAPurge(t *testing.T) {
	purged := DeletePlan{PurgeTrash: true}
	if got := deletedAuditCount(purged, DeleteResult{Moved: 8, Purged: 10}); got != 10 {
		t.Fatal(got)
	}
	if got := deletedAuditCount(purged, DeleteResult{Moved: 3}); got != 3 {
		t.Fatal(got)
	}
	if got := deletedAuditCount(DeletePlan{}, DeleteResult{Moved: 2, Purged: 1}); got != 3 {
		t.Fatal(got)
	}
}

func TestKeepRequestedRowsDropsServerExtras(t *testing.T) {
	rows := []envelopeRow{{uid: 9, subject: "extra"}, {uid: 4, subject: "keep"}, {uid: 1, subject: "other"}}
	kept := keepRequestedRows(rows, []uint32{4, 2})
	if len(kept) != 1 || kept[0].uid != 4 || kept[0].subject != "keep" {
		t.Fatalf("%+v", kept)
	}
	dups := keepRequestedRows([]envelopeRow{{uid: 4, subject: "keep"}, {uid: 4, subject: "again"}}, []uint32{4})
	if len(dups) != 1 || dups[0].subject != "keep" {
		t.Fatalf("%+v", dups)
	}
	if keepRequestedRows(rows, nil) != nil {
		t.Fatal("empty request")
	}
}

func TestUIDExpungePlanSkipsAnEmptySet(t *testing.T) {
	if uidExpungePlan(false, nil) != expungeNone || uidExpungePlan(true, uidSeq(nil)) != expungeNone {
		t.Fatal("empty set")
	}
	if uidExpungePlan(true, uidSeq([]uint32{0})) != expungeNone {
		t.Fatal("uid 0 is not a message")
	}
	if uidExpungePlan(true, uidSeq([]uint32{4})) != expungeUID {
		t.Fatal("uidplus")
	}
	if uidExpungePlan(false, uidSeq([]uint32{4})) != expungeAllDeleted {
		t.Fatal("fallback")
	}
}

func TestUIDSeqNeverEncodesZeroAsStar(t *testing.T) {
	if !uidSeq([]uint32{0}).Empty() || !uidSeq(nil).Empty() {
		t.Fatal("zero set")
	}
	got := uidSeq([]uint32{0, 4, 0}).String()
	if got != "4" || strings.Contains(got, "*") {
		t.Fatal(got)
	}
}

func TestCopyUIDsDropsZero(t *testing.T) {
	got := copyUIDs([]uint32{0, 4, 0, 2})
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("%v", got)
	}
	if copyUIDs([]uint32{0, 0}) != nil || dropZeroUIDs([]uint32{0}) != nil {
		t.Fatal("all zero")
	}
	filtered := dropZeroUIDs([]uint32{0, 9, 0})
	if len(filtered) != 1 || filtered[0] != 9 {
		t.Fatalf("%v", filtered)
	}
}

func TestPurgeFailureStaysClearBeforeAnyChange(t *testing.T) {
	raw := errors.New("NO store")
	if err := deleteProgressError(false, raw); err != raw {
		t.Fatal(err)
	}
	if err := deleteProgressError(false, ErrFolder); err != ErrFolder {
		t.Fatal(err)
	}
	if err := deleteProgressError(false, ErrUncertain); err != ErrUncertain {
		t.Fatal(err)
	}
	if err := deleteProgressError(true, raw); err != ErrUncertain {
		t.Fatal(err)
	}
	if deleteProgressError(false, nil) != nil {
		t.Fatal("nil")
	}
}

func TestDeleteContextStopAfterProgressIsUncertain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := deleteContextStop(ctx, false); err == nil || err.Error() != MsgDeleteAborted {
		t.Fatal(err)
	}
	if err := deleteContextStop(ctx, true); err != ErrUncertain {
		t.Fatal(err)
	}
	if err := deleteContextStop(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}

func TestUIDValidityMismatchIgnoresAMissingValue(t *testing.T) {
	if uidValidityMismatch(0, 5) || uidValidityMismatch(5, 0) || uidValidityMismatch(5, 5) {
		t.Fatal("same or unknown validity stays usable")
	}
	if !uidValidityMismatch(5, 6) {
		t.Fatal("changed validity blocks the frozen uids")
	}
}
