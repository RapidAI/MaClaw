package mailbox

import (
	"context"
	"sort"
	"strings"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/commands"
)

// PreviewDelete logs in, counts the messages the call would change, and
// returns the frozen plan. It selects folders read-only.
func (d *NetDialer) PreviewDelete(ctx context.Context, account Account, secret string, req DeleteRequest) (DeletePreview, error) {
	if account.Inbound == nil {
		return DeletePreview{}, classErr("no_inbound", MsgNoInbound(account.Name))
	}
	if isPOP3(account) {
		return DeletePreview{}, classErr("unsupported", MsgPOP3Unsupported)
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return DeletePreview{}, err
	}
	defer c.Logout()
	infos, err := listMailboxes(c)
	if err != nil {
		return DeletePreview{}, mapIMAP(err, "list")
	}
	folders := classifyFolders(infos)
	trash := resolveTrashBox(folders)
	if req.Scope == DeleteScopeAccount {
		return previewAccountDelete(ctx, c, folders, trash)
	}
	box := canonicalBox(folders, mailboxBox(account, req.Box))
	return previewFolderDelete(ctx, c, box, trash, req)
}

// ApplyDelete performs the frozen plan. A later folder failure, after an
// earlier folder changed, is uncertain and must not be retried blindly.
func (d *NetDialer) ApplyDelete(ctx context.Context, account Account, secret string, plan DeletePlan) (DeleteResult, error) {
	if account.Inbound == nil {
		return DeleteResult{}, classErr("no_inbound", MsgNoInbound(account.Name))
	}
	if isPOP3(account) {
		return DeleteResult{}, classErr("unsupported", MsgPOP3Unsupported)
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return DeleteResult{}, err
	}
	defer c.Logout()
	var result DeleteResult
	progressed := false
	for _, folder := range plan.Folders {
		uids := copyUIDs(folder.UIDs)
		if len(uids) == 0 {
			continue
		}
		if err := deleteContextStop(ctx, progressed); err != nil {
			return result, err
		}
		status, err := c.Select(folder.Name, false)
		if err != nil {
			if progressed {
				return result, ErrUncertain
			}
			return result, mapIMAP(err, "select")
		}
		if uidValidityMismatch(folder.UIDValidity, selectedUIDValidity(status)) {
			if progressed {
				return result, ErrUncertain
			}
			return result, classErr("uid_validity", MsgDeleteUIDChanged)
		}
		permanent := folder.Permanent || strings.TrimSpace(plan.TrashBox) == "" || folder.Name == plan.TrashBox
		var opErr error
		if permanent {
			opErr = expungeUIDs(c, uids)
		} else {
			opErr = moveUIDs(c, uids, plan.TrashBox)
		}
		if opErr != nil {
			if progressed || opErr == ErrUncertain {
				return result, ErrUncertain
			}
			return result, mapIMAP(opErr, "store")
		}
		if permanent {
			result.Purged += len(uids)
		} else {
			result.Moved += len(uids)
		}
		progressed = true
	}
	for _, box := range accountPurgeBoxes(plan) {
		if err := deleteContextStop(ctx, progressed); err != nil {
			return result, err
		}
		purged, err := purgeMailbox(c, box)
		if err != nil {
			return result, deleteProgressError(progressed, err)
		}
		result.Purged += purged
		progressed = purgeMarksProgress(progressed, purged)
	}
	return result, nil
}

// accountPurgeBoxes is the trash folders an account delete searches again.
// The resolved trash is first, so mail just moved there is included.
func accountPurgeBoxes(plan DeletePlan) []string {
	if !plan.PurgeTrash {
		return nil
	}
	var boxes []string
	if name := strings.TrimSpace(plan.TrashBox); name != "" {
		boxes = append(boxes, name)
	}
	for _, name := range plan.ExtraTrash {
		name = strings.TrimSpace(name)
		if name == "" || name == strings.TrimSpace(plan.TrashBox) {
			continue
		}
		boxes = append(boxes, name)
	}
	if len(boxes) == 0 {
		return nil
	}
	return boxes
}

// purgeMarksProgress records that a trash search already expunged mail.
// A later trash folder that fails must stay uncertain.
func purgeMarksProgress(progressed bool, purged int) bool {
	return progressed || purged > 0
}

func previewFolderDelete(ctx context.Context, c *imapclient.Client, box, trash string, req DeleteRequest) (DeletePreview, error) {
	if err := deleteContextStop(ctx, false); err != nil {
		return DeletePreview{}, err
	}
	status, err := c.Select(box, true)
	if err != nil {
		return DeletePreview{}, mapIMAP(err, "select")
	}
	var uids []uint32
	var subjects []string
	if req.All {
		uids, err = searchAllUIDs(c)
		if err != nil {
			return DeletePreview{}, err
		}
		subjects = sampleNewestSubjects(c, uids)
	} else {
		uids, subjects, err = fetchExistingUIDs(c, req.UIDs)
		if err != nil {
			return DeletePreview{}, err
		}
	}
	if len(uids) == 0 {
		if req.All {
			return DeletePreview{}, classErr("empty", MsgEmpty)
		}
		return DeletePreview{}, classErr("not_found", MsgNotFound)
	}
	permanent := trash == "" || box == trash
	plan := DeletePlan{
		Scope:    DeleteScopeFolder,
		Box:      box,
		TrashBox: trash,
		All:      req.All,
		UIDs:     copyUIDs(uids),
		Folders: []DeleteFolder{{
			Name:        box,
			UIDs:        copyUIDs(uids),
			Permanent:   permanent,
			UIDValidity: selectedUIDValidity(status),
		}},
	}
	return DeletePreview{
		Plan:    plan,
		Lines:   []DeleteFolderLine{{Name: box, Count: len(uids)}},
		Samples: subjects,
		Total:   len(uids),
	}, nil
}

func previewAccountDelete(ctx context.Context, c *imapclient.Client, folders []Folder, trash string) (DeletePreview, error) {
	var planFolders []DeleteFolder
	var lines []DeleteFolderLine
	var samples []string
	var extraTrash []string
	trashCount := 0
	for _, folder := range folders {
		if folder.Special == "all" || folder.Name == "" {
			continue
		}
		if err := deleteContextStop(ctx, false); err != nil {
			return DeletePreview{}, err
		}
		status, err := c.Select(folder.Name, true)
		if err != nil {
			return DeletePreview{}, mapIMAP(err, "select")
		}
		uids, err := searchAllUIDs(c)
		if err != nil {
			return DeletePreview{}, err
		}
		// Every trash folder is searched again at apply. Freezing its UIDs
		// would miss mail that arrives during confirmation, and moving one
		// trash into another assigns new UIDs the frozen list does not name.
		if folderReSearchedOnAccountPurge(folder.Name, folder.Special, trash) && folder.Name != trash {
			extraTrash = append(extraTrash, folder.Name)
		}
		if len(uids) == 0 {
			continue
		}
		if len(samples) < deleteSampleCap {
			for _, subject := range sampleNewestSubjects(c, uids) {
				if len(samples) >= deleteSampleCap {
					break
				}
				samples = append(samples, subject)
			}
		}
		if folderReSearchedOnAccountPurge(folder.Name, folder.Special, trash) {
			trashCount += len(uids)
			lines = append(lines, DeleteFolderLine{Name: folder.Name, Count: len(uids)})
			continue
		}
		permanent := trash == ""
		planFolders = append(planFolders, DeleteFolder{
			Name:        folder.Name,
			UIDs:        copyUIDs(uids),
			Permanent:   permanent,
			UIDValidity: selectedUIDValidity(status),
		})
		lines = append(lines, DeleteFolderLine{Name: folder.Name, Count: len(uids)})
	}
	total := trashCount
	var frozen []uint32
	for _, folder := range planFolders {
		total += len(folder.UIDs)
		frozen = append(frozen, folder.UIDs...)
	}
	if total == 0 {
		return DeletePreview{}, classErr("empty", MsgEmpty)
	}
	return DeletePreview{
		Plan: DeletePlan{
			Scope:      DeleteScopeAccount,
			TrashBox:   trash,
			All:        true,
			PurgeTrash: trash != "",
			ExtraTrash: extraTrash,
			UIDs:       frozen,
			Folders:    planFolders,
		},
		Lines:   lines,
		Samples: samples,
		Total:   total,
	}, nil
}

func purgeMailbox(c *imapclient.Client, box string) (int, error) {
	if _, err := c.Select(box, false); err != nil {
		return 0, mapIMAP(err, "select")
	}
	uids, err := searchAllUIDs(c)
	if err != nil {
		return 0, err
	}
	if len(uids) == 0 {
		return 0, nil
	}
	// expungeUIDs returns ErrUncertain only after a store succeeded.
	// A rejected first store must stay that error, or an untouched trash
	// folder would latch the mailbox.
	if err := expungeUIDs(c, uids); err != nil {
		return 0, err
	}
	return len(uids), nil
}

func searchAllUIDs(c *imapclient.Client) ([]uint32, error) {
	uids, err := c.UidSearch(imap.NewSearchCriteria())
	if err != nil {
		return nil, mapIMAP(err, "search")
	}
	uids = dropZeroUIDs(uids)
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids, nil
}

func fetchExistingUIDs(c *imapclient.Client, want []uint32) ([]uint32, []string, error) {
	if len(want) == 0 {
		return nil, nil, nil
	}
	msgs, err := uidFetch(c, uidSeq(want), []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid})
	if err != nil {
		return nil, nil, mapIMAP(err, "read")
	}
	rows := keepRequestedRows(envelopeRows(msgs), want)
	uids := make([]uint32, len(rows))
	for i, row := range rows {
		uids[i] = row.uid
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids, subjectsFromRows(rows), nil
}

// keepRequestedRows drops UIDs the server returned outside the requested set.
// A FETCH can include extra messages; those must not enter the frozen plan.
func keepRequestedRows(rows []envelopeRow, want []uint32) []envelopeRow {
	if len(rows) == 0 || len(want) == 0 {
		return nil
	}
	allowed := make(map[uint32]struct{}, len(want))
	for _, id := range want {
		if id != 0 {
			allowed[id] = struct{}{}
		}
	}
	out := make([]envelopeRow, 0, len(rows))
	seen := make(map[uint32]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := allowed[row.uid]; !ok {
			continue
		}
		if _, dup := seen[row.uid]; dup {
			continue
		}
		seen[row.uid] = struct{}{}
		out = append(out, row)
	}
	return out
}

func selectedUIDValidity(status *imap.MailboxStatus) uint32 {
	if status == nil {
		return 0
	}
	return status.UidValidity
}

// uidValidityMismatch is true only when both values were reported and differ.
// A zero value means the server omitted UIDVALIDITY, which is not proof that
// the mailbox was rebuilt. Account trash purge does not use this check: that
// pass searches the trash folder again and deletes whatever is there.
func uidValidityMismatch(frozen, current uint32) bool {
	return frozen != 0 && current != 0 && frozen != current
}

func sampleNewestSubjects(c *imapclient.Client, uids []uint32) []string {
	if len(uids) == 0 || c == nil {
		return nil
	}
	newest := append([]uint32(nil), uids...)
	sort.Slice(newest, func(i, j int) bool { return newest[i] > newest[j] })
	if len(newest) > deleteSampleCap {
		newest = newest[:deleteSampleCap]
	}
	msgs, err := uidFetch(c, uidSeq(newest), []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid})
	if err != nil {
		return nil
	}
	return subjectsFromRows(envelopeRows(msgs))
}

type envelopeRow struct {
	uid     uint32
	subject string
}

func envelopeRows(msgs []*imap.Message) []envelopeRow {
	rows := make([]envelopeRow, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Uid == 0 {
			continue
		}
		subject := ""
		if msg.Envelope != nil {
			subject = decodeMIMEHeader(msg.Envelope.Subject)
		}
		rows = append(rows, envelopeRow{uid: msg.Uid, subject: subject})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].uid > rows[j].uid })
	return rows
}

func subjectsFromRows(rows []envelopeRow) []string {
	n := len(rows)
	if n > deleteSampleCap {
		n = deleteSampleCap
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, oneLine(rows[i].subject))
	}
	return out
}

func moveUIDs(c *imapclient.Client, uids []uint32, dest string) error {
	moved := false
	for _, chunk := range chunkUIDs(uids, deleteUIDBatch) {
		seq := uidSeq(chunk)
		if seq.Empty() {
			continue
		}
		if err := moveUIDChunk(c, seq, dest); err != nil {
			if moved || err == ErrUncertain {
				return ErrUncertain
			}
			return err
		}
		moved = true
	}
	return nil
}

func moveUIDChunk(c *imapclient.Client, seq *imap.SeqSet, dest string) error {
	ok, err := c.Support("MOVE")
	if err != nil {
		return err
	}
	if ok {
		return c.UidMove(seq, dest)
	}
	if err := c.UidCopy(seq, dest); err != nil {
		return err
	}
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.UidStore(seq, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
		return ErrUncertain
	}
	if err := uidExpunge(c, seq); err != nil {
		return ErrUncertain
	}
	return nil
}

func expungeUIDs(c *imapclient.Client, uids []uint32) error {
	mutated := false
	for _, chunk := range chunkUIDs(uids, deleteUIDBatch) {
		seq := uidSeq(chunk)
		if seq.Empty() {
			continue
		}
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		if err := c.UidStore(seq, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
			if mutated {
				return ErrUncertain
			}
			return err
		}
		mutated = true
		if err := uidExpunge(c, seq); err != nil {
			return ErrUncertain
		}
	}
	return nil
}

// uidExpunge removes the selected UIDs. UIDPLUS limits the expunge to that
// set. Without it, Expunge clears every \Deleted message in the mailbox.
// An empty set removes nothing: it must not fall through to that Expunge.
func uidExpunge(c *imapclient.Client, seq *imap.SeqSet) error {
	ok, err := c.Support("UIDPLUS")
	if err != nil {
		return err
	}
	switch uidExpungePlan(ok, seq) {
	case expungeNone:
		return nil
	case expungeAllDeleted:
		return c.Expunge(nil)
	}
	status, err := c.Execute(&commands.Uid{Cmd: &imap.Command{
		Name:      "EXPUNGE",
		Arguments: []interface{}{imap.RawString(seq.String())},
	}}, nil)
	if err != nil {
		return err
	}
	if status == nil {
		return nil
	}
	return status.Err()
}

type expungePlan int

const (
	expungeNone expungePlan = iota
	expungeUID
	expungeAllDeleted
)

func uidExpungePlan(hasUIDPLUS bool, seq *imap.SeqSet) expungePlan {
	if seq == nil || seq.Empty() {
		return expungeNone
	}
	if hasUIDPLUS {
		return expungeUID
	}
	return expungeAllDeleted
}

// deleteContextStop refuses to start another mailbox change after the caller
// has cancelled. A folder that already changed stays uncertain.
func deleteContextStop(ctx context.Context, progressed bool) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if progressed {
		return ErrUncertain
	}
	return classErr("cancelled", MsgDeleteAborted)
}

// deleteProgressError leaves a failure clear when no folder has changed.
// ErrUncertain means a store already landed, so that failure stays latched
// even when this was the first folder.
func deleteProgressError(progressed bool, err error) error {
	if err == nil {
		return nil
	}
	if progressed || err == ErrUncertain {
		return ErrUncertain
	}
	return err
}

// uidSeq builds a UID set. go-imap's AddNum treats 0 as "*", which would
// store or expunge every message in the selected mailbox.
func uidSeq(uids []uint32) *imap.SeqSet {
	seq := new(imap.SeqSet)
	for _, id := range uids {
		if id == 0 {
			continue
		}
		seq.AddNum(id)
	}
	return seq
}

func chunkUIDs(uids []uint32, size int) [][]uint32 {
	if size <= 0 {
		size = deleteUIDBatch
	}
	if len(uids) == 0 {
		return nil
	}
	out := make([][]uint32, 0, (len(uids)+size-1)/size)
	for len(uids) > 0 {
		n := size
		if n > len(uids) {
			n = len(uids)
		}
		out = append(out, uids[:n])
		uids = uids[n:]
	}
	return out
}

func copyUIDs(uids []uint32) []uint32 {
	out := dropZeroUIDs(uids)
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// dropZeroUIDs removes UID 0. RFC 3501 UIDs start at 1, and 0 is not a message.
func dropZeroUIDs(uids []uint32) []uint32 {
	if len(uids) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(uids))
	for _, id := range uids {
		if id != 0 {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalBox(folders []Folder, box string) string {
	box = strings.TrimSpace(box)
	for _, folder := range folders {
		if folder.Name == box {
			return folder.Name
		}
	}
	if strings.EqualFold(box, "INBOX") {
		for _, folder := range folders {
			if strings.EqualFold(folder.Name, "INBOX") {
				return folder.Name
			}
		}
	}
	return box
}

func resolveTrashBox(folders []Folder) string {
	for _, folder := range folders {
		if folder.Special == "trash" {
			return folder.Name
		}
	}
	best := ""
	bestRank := 0
	for _, folder := range folders {
		rank := trashNameRank(folder.Name)
		if rank > bestRank {
			best = folder.Name
			bestRank = rank
		}
	}
	return best
}

// folderReSearchedOnAccountPurge reports a trash folder whose messages are
// deleted by a later SEARCH ALL, not by the frozen UID list.
func folderReSearchedOnAccountPurge(name, special, trash string) bool {
	return trash != "" && (name == trash || special == "trash")
}

// trashNameRank scores a fallback trash name when the server sets no \Trash.
// A specific folder such as 已删除 or INBOX.Trash beats a generic Bin or Deleted,
// which would otherwise win just because it sorts first.
func trashNameRank(name string) int {
	if !folderMatchesTrash(name) {
		return 0
	}
	folded := strings.ToLower(strings.TrimSpace(name))
	switch folded {
	case "[gmail]/trash", "inbox.trash", "deleted items", "deleted messages", "已删除", "已删除邮件", "废件箱":
		return 40
	case "trash":
		return 30
	case "deleted", "bin":
		return 10
	}
	segment := folded
	if i := strings.LastIndex(folded, "/"); i >= 0 {
		segment = folded[i+1:]
	} else if i := strings.LastIndex(folded, "."); i > 0 {
		segment = folded[i+1:]
	}
	switch segment {
	case "trash", "deleted", "deleted items", "deleted messages", "已删除", "已删除邮件", "废件箱":
		return 20
	default:
		return 10
	}
}

func folderMatchesTrash(name string) bool {
	folded := strings.ToLower(strings.TrimSpace(name))
	if folded == "" {
		return false
	}
	if trashNames[folded] {
		return true
	}
	slash := folded
	if i := strings.LastIndex(folded, "/"); i >= 0 {
		slash = folded[i+1:]
	}
	if trashNames[slash] {
		return true
	}
	if i := strings.LastIndex(folded, "."); i > 0 && trashNames[folded[i+1:]] {
		return true
	}
	return false
}

var trashNames = map[string]bool{
	"trash":            true,
	"deleted":          true,
	"deleted items":    true,
	"deleted messages": true,
	"bin":              true,
	"inbox.trash":      true,
	"[gmail]/trash":    true,
	"已删除":              true,
	"已删除邮件":            true,
	"废件箱":              true,
}
