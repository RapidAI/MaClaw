package mailbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/responses"
)

// expungeFake is one selected folder whose server can be told to honor only
// part of a multi-UID UID EXPUNGE, the way Tencent Exmail does.
type expungeFake struct {
	plus      bool
	live      map[uint32]bool
	flagged   map[uint32]bool
	setLimit  int
	refuseOne map[uint32]bool
	storeErr  error
	setCalls  []string
	oneCalls  []uint32
	expunges  int
}

func newExpungeFake(uids ...uint32) *expungeFake {
	f := &expungeFake{plus: true, live: map[uint32]bool{}, flagged: map[uint32]bool{}, refuseOne: map[uint32]bool{}}
	for _, uid := range uids {
		f.live[uid] = true
	}
	return f
}

func (f *expungeFake) Support(capability string) (bool, error) {
	return f.plus && capability == "UIDPLUS", nil
}

func (f *expungeFake) UidStore(seqset *imap.SeqSet, _ imap.StoreItem, _ interface{}, _ chan *imap.Message) error {
	if f.storeErr != nil {
		return f.storeErr
	}
	for _, uid := range seqSetUIDs(seqset) {
		if f.live[uid] {
			f.flagged[uid] = true
		}
	}
	return nil
}

func (f *expungeFake) UidFetch(seqset *imap.SeqSet, _ []imap.FetchItem, ch chan *imap.Message) error {
	defer close(ch)
	for _, uid := range seqSetUIDs(seqset) {
		if f.live[uid] {
			ch <- &imap.Message{Uid: uid}
		}
	}
	return nil
}

func (f *expungeFake) Expunge(_ chan uint32) error {
	f.expunges++
	for uid := range f.flagged {
		delete(f.live, uid)
		delete(f.flagged, uid)
	}
	return nil
}

func (f *expungeFake) Execute(cmdr imap.Commander, _ responses.Handler) (*imap.StatusResp, error) {
	cmd := cmdr.Command()
	if cmd.Name != "UID" || len(cmd.Arguments) != 2 {
		return nil, errors.New("not a UID command")
	}
	raw, ok := cmd.Arguments[1].(imap.RawString)
	if !ok {
		return nil, errors.New("no UID set")
	}
	set := string(raw)
	f.setCalls = append(f.setCalls, set)
	uids := parseSetUIDs(set)
	if len(uids) == 1 {
		f.oneCalls = append(f.oneCalls, uids[0])
		if !f.refuseOne[uids[0]] {
			delete(f.live, uids[0])
		}
		return &imap.StatusResp{Type: imap.StatusRespOk}, nil
	}
	removed := 0
	for _, uid := range uids {
		if !f.live[uid] {
			continue
		}
		if f.setLimit > 0 && removed >= f.setLimit {
			break
		}
		delete(f.live, uid)
		removed++
	}
	return &imap.StatusResp{Type: imap.StatusRespOk}, nil
}

func seqSetUIDs(seq *imap.SeqSet) []uint32 {
	if seq == nil {
		return nil
	}
	var out []uint32
	for _, s := range seq.Set {
		for id := s.Start; id <= s.Stop; id++ {
			out = append(out, id)
		}
	}
	return out
}

// parseSetUIDs expands the wire form ("1:3,9") back to individual UIDs.
func parseSetUIDs(set string) []uint32 {
	var out []uint32
	for _, part := range strings.Split(set, ",") {
		lo, hi := part, part
		if start, end, ok := strings.Cut(part, ":"); ok {
			lo, hi = start, end
		}
		var from, to uint32
		for _, ch := range lo {
			from = from*10 + uint32(ch-'0')
		}
		to = from
		if hi != lo {
			to = 0
			for _, ch := range hi {
				to = to*10 + uint32(ch-'0')
			}
		}
		for id := from; id <= to; id++ {
			out = append(out, id)
		}
	}
	return out
}

func TestExpungeUIDsFinishesASetTheServerOnlyPartlyApplies(t *testing.T) {
	// Exmail answers OK to UID EXPUNGE 1:3 after removing just one message.
	fake := newExpungeFake(1, 2, 3)
	fake.setLimit = 1
	removed, err := expungeUIDs(fake, []uint32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 || len(fake.live) != 0 {
		t.Fatalf("removed=%v live=%v", removed, fake.live)
	}
	if len(fake.oneCalls) != 2 {
		t.Fatalf("one-UID retries=%v, want the two survivors", fake.oneCalls)
	}
}

func TestExpungeUIDsReportsWhatTheServerKept(t *testing.T) {
	fake := newExpungeFake(4, 5)
	fake.setLimit = 1
	fake.refuseOne[5] = true
	removed, err := expungeUIDs(fake, []uint32{4, 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != 4 {
		t.Fatalf("removed=%v, want only 4", removed)
	}
	result := DeleteResult{}
	result.countRemoved(true, []uint32{4, 5}, removed, true)
	if result.Purged != 1 || len(result.NotRemoved) != 1 || result.NotRemoved[0] != 5 {
		t.Fatalf("result=%+v", result)
	}
}

func TestExpungeUIDsNeverExpungesAfterAStoreFails(t *testing.T) {
	fake := newExpungeFake(1, 2)
	fake.storeErr = errors.New("NO store")
	if _, err := expungeUIDs(fake, []uint32{1, 2}); err == nil {
		t.Fatal("store failure must surface")
	}
	if len(fake.setCalls) != 0 || fake.expunges != 0 {
		t.Fatalf("expunged after a failed store: %v %d", fake.setCalls, fake.expunges)
	}
}

func TestExpungeUIDsWithoutUIDPLUSUsesPlainExpunge(t *testing.T) {
	fake := newExpungeFake(1, 2, 3)
	fake.plus = false
	removed, err := expungeUIDs(fake, []uint32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 || fake.expunges != 1 {
		t.Fatalf("removed=%v expunges=%d", removed, fake.expunges)
	}
}

func TestDeleteDoneTextNamesWhatTheServerKept(t *testing.T) {
	kept := DeleteResult{Purged: 2, NotRemoved: []uint32{9}}
	text := deleteDoneText(kept)
	if !strings.Contains(text, MsgDeletePurged) || !strings.Contains(text, "1 封未能删除") {
		t.Fatalf("text=%q", text)
	}
	if got := deleteDoneText(DeleteResult{NotRemoved: []uint32{7}}); got != MsgDeleteNone+"。有 1 封未能删除，服务器仍保留。" {
		t.Fatalf("text=%q", got)
	}
	if got := deleteDoneText(DeleteResult{Moved: 2}); got != MsgDeleteMoved {
		t.Fatalf("text=%q", got)
	}
}

func TestExternalMutationKnownAcceptsAPartialDelete(t *testing.T) {
	result := DeleteResult{Purged: 1, NotRemoved: []uint32{5}}
	if !ExternalMutationKnown(deleteDoneText(result)) {
		t.Fatal("a partial delete is still a known mutation")
	}
	if !ExternalMutationKnown(MsgDeleteNone) {
		t.Fatal("a delete that removed nothing is a known outcome")
	}
}

func TestDeleteSessionContextKeepsACallerDeadline(t *testing.T) {
	caller, cancelCaller := context.WithTimeout(context.Background(), time.Minute)
	defer cancelCaller()
	got, cancel := deleteSessionContext(caller)
	defer cancel()
	deadline, ok := got.Deadline()
	if !ok || !deadline.Equal(mustDeadline(t, caller)) {
		t.Fatalf("caller deadline replaced: %v", deadline)
	}

	got, cancel = deleteSessionContext(context.Background())
	defer cancel()
	deadline, ok = got.Deadline()
	if !ok || time.Until(deadline) <= time.Minute {
		t.Fatalf("delete budget not applied: %v", deadline)
	}
}

func mustDeadline(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("no deadline")
	}
	return deadline
}
