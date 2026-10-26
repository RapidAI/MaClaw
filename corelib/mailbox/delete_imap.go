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
	return applyDeleteClient(ctx, c, plan)
}

// DeleteFolderPage previews, applies, and reads the newest page on one IMAP
// login. A list failure after a finished delete is ListErr, not an error:
// the messages are already gone, and the caller must not report the delete
// as failed.
func (d *NetDialer) DeleteFolderPage(ctx context.Context, account Account, secret, box string, uids []uint32, limit int) (FolderDeletePage, error) {
	if account.Inbound == nil {
		return FolderDeletePage{}, classErr("no_inbound", MsgNoInbound(account.Name))
	}
	if isPOP3(account) {
		return FolderDeletePage{}, classErr("unsupported", MsgPOP3Unsupported)
	}
	if len(uids) == 0 {
		return FolderDeletePage{}, classErr("not_found", MsgNotFound)
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return FolderDeletePage{}, err
	}
	defer c.Logout()
	infos, err := listMailboxes(c)
	if err != nil {
		return FolderDeletePage{}, mapIMAP(err, "list")
	}
	folders := classifyFolders(infos)
	trash := resolveTrashBox(folders)
	canonical := canonicalBox(folders, mailboxBox(account, box))
	preview, err := previewFolderDelete(ctx, c, canonical, trash, DeleteRequest{
		Scope: DeleteScopeFolder,
		Box:   canonical,
		UIDs:  uids,
	})
	if err != nil {
		return FolderDeletePage{}, err
	}
	result, err := applyDeleteClient(ctx, c, preview.Plan)
	if err != nil {
		return FolderDeletePage{}, err
	}
	messages, listErr := listRemainingPage(c, preview.Plan.Box, limit, preview.Plan.UIDs)
	if listErr != nil {
		return FolderDeletePage{Result: result, ListErr: listErr}, nil
	}
	return FolderDeletePage{Result: result, Messages: messages}, nil
}

var _ FolderPageDeleter = (*NetDialer)(nil)

func applyDeleteClient(ctx context.Context, c *imapclient.Client, plan DeletePlan) (DeleteResult, error) {
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

// listRemainingPage re-selects the folder and reads its newest messages.
// MOVE updates sequence numbers, so the page is taken after a fresh SELECT.
// A bulk move can still leave that high window naming the uids just removed,
// or leave the high range empty while EXISTS still counts it. The next window
// is the range under the one just fetched, not a mailbox count read later:
// UID FETCH can shrink EXISTS and would otherwise skip a page. UID FETCH of
// the removed set asks which ones still exist. The ones that are gone are
// dropped. A window that is only those uids is skipped, not returned. That is not a search of the
// whole mailbox. Uids that still exist stay on the page. An empty folder is
// an empty page, not an error.
func listRemainingPage(c *imapclient.Client, box string, limit int, removed []uint32) ([]MessageSummary, error) {
	page, windowStart, err := readRecentWindow(c, box, limit)
	if err != nil {
		return nil, err
	}
	// A high range that comes back with no FETCH rows is not an empty
	// folder: those sequences were vacated while EXISTS still counted them.
	// Read the window under the range that was actually fetched. Messages
	// that arrived without a UID are not treated as a vacant range.
	page, windowStart = fillVacantWindow(c, page, windowStart, limit)
	page = newestSummaries(page, clampPageLimit(limit))
	if !pageRepeatsRemoved(page, removed) {
		return page, nil
	}
	live, liveErr := presentUIDs(c, removed)
	if liveErr != nil {
		again, againStart, againErr := readRecentWindow(c, box, limit)
		if againErr != nil {
			return nil, againErr
		}
		again, _ = fillVacantWindow(c, again, againStart, limit)
		return newestSummaries(again, clampPageLimit(limit)), nil
	}
	absent := absentRemoved(removed, live)
	if len(absent) == 0 {
		return page, nil
	}
	limit = clampPageLimit(limit)
	// UID FETCH said these uids are gone. A sequence window that still
	// names them is not the folder. Drop them and read the windows under
	// the range just fetched until the page fills, the mailbox bottom is
	// reached, or a few windows have been tried. Do not hand the ghost
	// page back: the dialog would keep those rows.
	filtered := newestSummaries(withoutUIDs(page, absent), limit)
	start := windowStart
	const ghostWindowCap = 4
	for step := 0; len(filtered) < limit && step < ghostWindowCap; step++ {
		bStart, bEnd, ok := windowBefore(start, limit)
		if !ok {
			break
		}
		extra, fetchErr := fetchSummaryWindow(c, bStart, bEnd)
		if fetchErr != nil {
			if len(filtered) == 0 {
				return page, nil
			}
			break
		}
		start = bStart
		filtered = pageAfterAbsent(filtered, extra, absent, limit)
	}
	if len(filtered) == 0 {
		return []MessageSummary{}, nil
	}
	return filtered, nil
}

// fillVacantWindow reads the next lower sequence window when the fetched
// range was empty but did not start at sequence 1. A failed or empty lower
// read keeps the empty page; it does not fail the delete.
func fillVacantWindow(c *imapclient.Client, page []MessageSummary, windowStart uint32, limit int) ([]MessageSummary, uint32) {
	if len(page) > 0 {
		return page, windowStart
	}
	start, end, ok := windowBefore(windowStart, limit)
	if !ok {
		return page, windowStart
	}
	extra, err := fetchSummaryWindow(c, start, end)
	if err != nil || len(extra) == 0 {
		return page, windowStart
	}
	return extra, start
}

// readRecentWindow selects the folder and fetches one sequence window.
// The returned start is that window, captured from the SELECT count before
// any later command can change it.
func readRecentWindow(c *imapclient.Client, box string, limit int) ([]MessageSummary, uint32, error) {
	limit = clampPageLimit(limit)
	status, err := c.Select(box, true)
	if err != nil {
		return nil, 0, mapIMAP(err, "select")
	}
	count := uint32(0)
	if status != nil {
		count = status.Messages
	}
	start, end, ok := recentSequence(count, limit)
	if !ok {
		return []MessageSummary{}, 0, nil
	}
	page, err := fetchSummaryWindow(c, start, end)
	if err != nil {
		return nil, 0, err
	}
	return page, start, nil
}

func clampPageLimit(limit int) int {
	if limit <= 0 || limit > ListMaxLimit {
		return 30
	}
	return limit
}

// sequenceBelow is the sequence window immediately under the newest page.
// The newest page is the high end of messages. There is no lower window
// when that page already starts at sequence 1.
func sequenceBelow(messages uint32, limit int) (start, end uint32, ok bool) {
	top, _, topOK := recentSequence(messages, limit)
	if !topOK {
		return 0, 0, false
	}
	return windowBefore(top, limit)
}

// windowBefore is the sequence window immediately under a range that starts
// at topStart. topStart is the range that was fetched, not a later EXISTS.
func windowBefore(topStart uint32, limit int) (start, end uint32, ok bool) {
	if topStart <= 1 {
		return 0, 0, false
	}
	end = topStart - 1
	width := uint32(clampPageLimit(limit))
	start = 1
	if end >= width {
		start = end - width + 1
	}
	return start, end, true
}

func fetchSummaryWindow(c *imapclient.Client, start, end uint32) ([]MessageSummary, error) {
	items, _ := summaryFetchItems(false)
	seq := new(imap.SeqSet)
	seq.AddRange(start, end)
	msgs, err := seqFetch(c, seq, items)
	if err != nil {
		return nil, mapIMAP(err, "read")
	}
	return summariesFromMessages(msgs), nil
}

func summariesFromMessages(msgs []*imap.Message) []MessageSummary {
	out := make([]MessageSummary, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		out = append(out, summaryFromMessage(msg, ""))
	}
	return out
}

// presentUIDs is the subset of removed that a UID FETCH still finds.
// Missing uids produce no FETCH row; that is an empty set, not an error.
func presentUIDs(c *imapclient.Client, removed []uint32) (map[uint32]struct{}, error) {
	seq := uidSeq(removed)
	if seq.Empty() {
		return map[uint32]struct{}{}, nil
	}
	msgs, err := uidFetch(c, seq, []imap.FetchItem{imap.FetchUid})
	if err != nil {
		return nil, mapIMAP(err, "read")
	}
	want := make(map[uint32]struct{}, len(removed))
	for _, id := range removed {
		if id != 0 {
			want[id] = struct{}{}
		}
	}
	live := make(map[uint32]struct{}, len(msgs))
	for _, msg := range msgs {
		if msg == nil || msg.Uid == 0 {
			continue
		}
		if _, ok := want[msg.Uid]; !ok {
			continue
		}
		live[msg.Uid] = struct{}{}
	}
	return live, nil
}

func absentRemoved(removed []uint32, live map[uint32]struct{}) []uint32 {
	out := make([]uint32, 0, len(removed))
	for _, id := range removed {
		if id == 0 {
			continue
		}
		if _, ok := live[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

func withoutUIDs(messages []MessageSummary, skip []uint32) []MessageSummary {
	if len(messages) == 0 || len(skip) == 0 {
		return messages
	}
	drop := make(map[uint32]struct{}, len(skip))
	for _, id := range skip {
		if id != 0 {
			drop[id] = struct{}{}
		}
	}
	if len(drop) == 0 {
		return messages
	}
	out := make([]MessageSummary, 0, len(messages))
	for _, msg := range messages {
		if _, ok := drop[msg.UID]; ok {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func newestSummaries(messages []MessageSummary, limit int) []MessageSummary {
	seen := make(map[uint32]struct{}, len(messages))
	out := make([]MessageSummary, 0, len(messages))
	for _, msg := range messages {
		if msg.UID == 0 {
			continue
		}
		if _, ok := seen[msg.UID]; ok {
			continue
		}
		seen[msg.UID] = struct{}{}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID > out[j].UID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// pageAfterAbsent drops uids UID FETCH showed are gone and fills from the
// next lower sequence window. A window that is only those uids adds nothing:
// the caller reads further down instead of returning that page.
func pageAfterAbsent(page, below []MessageSummary, absent []uint32, limit int) []MessageSummary {
	if limit <= 0 {
		limit = 30
	}
	filtered := withoutUIDs(page, absent)
	lower := withoutUIDs(below, absent)
	next := newestSummaries(append(append([]MessageSummary{}, filtered...), lower...), limit)
	if len(next) == 0 {
		return []MessageSummary{}
	}
	return next
}

// pageRepeatsRemoved reports that a just-removed uid is still in the page.
func pageRepeatsRemoved(messages []MessageSummary, removed []uint32) bool {
	if len(messages) == 0 || len(removed) == 0 {
		return false
	}
	skip := make(map[uint32]struct{}, len(removed))
	for _, id := range removed {
		if id != 0 {
			skip[id] = struct{}{}
		}
	}
	if len(skip) == 0 {
		return false
	}
	for _, msg := range messages {
		if _, ok := skip[msg.UID]; ok {
			return true
		}
	}
	return false
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
