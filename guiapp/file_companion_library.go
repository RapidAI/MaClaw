package guiapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The companion library is the reading history for 文档管理. It is not the
// open-set (files this window still has open) and not the mobile draft store.
// A local row is this machine's own project and is never uploaded as a project
// list. Sharing that document retires the local row: the share is the cloud
// project, and the file stays where it is. Opening the same path again does
// not create a second local project. When the share is gone, the owner's own
// file (not a downloaded copy) becomes a local project again.

const fileCompanionLibraryCap = 200

var (
	fileCompanionLibraryMu        sync.Mutex
	fileCompanionLibraryRefreshMu sync.Mutex
	// fileCompanionShareLiveOnce keeps a second poll from stacking another
	// changes read while a download is already in progress. The transcript
	// counter still has to move; the heavy refresh does not.
	fileCompanionShareLiveOnce    atomic.Bool
	fileCompanionShareTokenMatch  = regexp.MustCompile(`^cd_[0-9a-f]{32}$`)
	fileCompanionShareTokenFind   = regexp.MustCompile(`cd_[0-9a-f]{32}`)
	fileCompanionReaderGrantMatch = regexp.MustCompile(`^cgr_[0-9a-f]{32}$`)
	fileCompanionSessionDirMatch  = regexp.MustCompile(`^fc_[0-9a-f]{32}$`)
	// fileCompanionShareEpoch advances when a share is dismissed or brought
	// back. A download captures the value before its HTTP call and publishes
	// only when the value is unchanged. The map lives as long as this process,
	// which is also how long an in-flight pull can last.
	fileCompanionShareEpoch = map[string]uint64{}
)

// fileCompanionShareWatermark is the Hub change sequence of one share-list
// response: seq is the ETag the Hub sends, remoteSeq the per-viewer stamp of
// documents on other hubs. Each poller records the watermark of its own
// response. A process-wide value would let the discussion watcher's newer
// response cover the library's older one; the 5-second pre-check then reads
// "nothing changed" while the transcript on disk is behind, and the host's
// new chat only lands on the 10-minute full refresh.
type fileCompanionShareWatermark struct {
	seq       uint64
	remoteSeq uint64
}

// fileCompanionShareRefreshCheck remembers what the last full library refresh
// saw, so the 5-second poll can ask the cheap changes endpoint instead of
// re-downloading the whole share list. Keyed by Hub URL: switching hubs (or
// tests pointing at a fresh one) starts over with a full refresh.
type fileCompanionShareRefreshState struct {
	mu          sync.Mutex
	hub         string
	seq         uint64
	remoteSeq   uint64
	lastFull    time.Time
	fullDone    bool
	unsupported bool
	hadLogin    bool
}

var fileCompanionShareRefreshCheck fileCompanionShareRefreshState

// fileCompanionShareRefreshFullInterval forces a full refresh even when the
// changes endpoint reports nothing new: expiry, revocation probes, and clock
// skew all reconcile on the full path.
const fileCompanionShareRefreshFullInterval = 10 * time.Minute

// errFileCompanionShareChangesUnsupported marks a Hub that predates the
// changes endpoint; the refresh check then falls back to full refreshes.
var errFileCompanionShareChangesUnsupported = errors.New("companion share changes unsupported")

// Notices 文档管理 shows when a refresh cannot ask the Hub. The full list and
// a quiet poll that skips that list use the same sentences.
const (
	fileCompanionLibraryLoginNotice   = "登录后可以查看云端分享"
	fileCompanionLibraryOfflineNotice = "暂时无法连接云端，本机文档仍可查看"
)

func (st *fileCompanionShareRefreshState) reset(hub string) {
	st.hub = hub
	st.seq = 0
	st.remoteSeq = 0
	st.lastFull = time.Time{}
	st.fullDone = false
	st.unsupported = false
	st.hadLogin = false
}

// fileCompanionShareChange is one share's counters on the cheap changes feed.
// The transcript and the reader marks move here without the note bodies or
// the project zip, which is what a participant's open chat actually needs.
type fileCompanionShareChange struct {
	Token         string `json:"token"`
	Revision      int    `json:"revision"`
	NotesRevision int    `json:"notes_revision"`
	MarksRevision int    `json:"marks_revision"`
	ChatRevision  int    `json:"chat_revision"`
	// Pointers distinguish a Hub that sends the identity of the share from an
	// older feed that only has the counters. Absent means "do not compare".
	Visibility  *string `json:"visibility"`
	ExpiresAt   *string `json:"expires_at"`
	Target      *string `json:"target"`
	GroupID     *string `json:"group_id"`
	Host        *bool   `json:"host"`
	PendingHost *bool   `json:"pending_host"`
	// HubOrigin is set only on a share that lives on another Hub. The home
	// feed leaves it empty.
	HubOrigin string `json:"hub_origin,omitempty"`
}

// fileCompanionShareChangeNeedsList reports that this row moved in a way the
// transcript blob cannot carry: a new document, a note, or the share's own
// identity (audience, expiry, host seat). Chat and marks counters are applied
// either way.
func fileCompanionShareChangeNeedsList(row fileCompanionShareChange, item FileCompanionLibraryItem) bool {
	if row.Revision > item.Revision || row.NotesRevision > item.NotesRevision {
		return true
	}
	if row.Visibility == nil {
		return false
	}
	// The share list stores a missing audience as public. The changes feed
	// sends the Hub value unchanged, and an older share can still have an
	// empty audience. Treating those two spellings as different made every
	// later chat line download the full list.
	if fileCompanionShareAudience(*row.Visibility) != fileCompanionShareAudience(item.Visibility) {
		return true
	}
	if row.ExpiresAt != nil && !fileCompanionShareTimeEqual(*row.ExpiresAt, item.ExpiresAt) {
		return true
	}
	// Target and a pending nomination are stored on the admin's row only. The
	// full list does not give them to a participant, so comparing them there
	// made every host line look like an audience edit and took the slow path.
	if item.Admin {
		if row.Target != nil && strings.TrimSpace(*row.Target) != strings.TrimSpace(item.Target) {
			return true
		}
		if row.PendingHost != nil && *row.PendingHost != item.PendingHost {
			return true
		}
	}
	// A group member's row keeps the group id. An empty stored id is a public
	// share or a row from before the field existed; the feed's id is not a
	// change those rows can see.
	if row.GroupID != nil && strings.TrimSpace(item.GroupID) != "" && strings.TrimSpace(*row.GroupID) != strings.TrimSpace(item.GroupID) {
		return true
	}
	// A row saved before the host seat was known still follows Mine. Comparing
	// it here would turn every later chat line into a full refresh. The
	// ten-minute reconcile learns the seat.
	if row.Host != nil && item.HostKnown && *row.Host != item.Host {
		return true
	}
	return false
}

// fileCompanionShareAudience folds the two spellings of a public share into
// one. An empty audience is the older record; the library writes it as public.
func fileCompanionShareAudience(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "public"
	}
	return raw
}

// fileCompanionShareTimeEqual reports that two expiry strings are the same
// instant. The list and the changes feed can spell one UTC time with Z or
// with an offset; a byte compare would send every later chat line through
// the full share list.
func fileCompanionShareTimeEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	parse := func(value string) (time.Time, bool) {
		if value == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed, true
		}
		return time.Time{}, false
	}
	leftTime, leftOK := parse(left)
	rightTime, rightOK := parse(right)
	return leftOK && rightOK && leftTime.Equal(rightTime)
}

// fileCompanionShareChanges is one answer from the changes endpoint.
// HasShares is false when this Hub only sends the two sequences. ForceFull
// is the library poll asking for a reconcile even after the counters land:
// the first snapshot, or the ten-minute pass that checks expiry and revocation.
type fileCompanionShareChanges struct {
	Seq             uint64
	RemoteSeq       uint64
	Shares          []fileCompanionShareChange
	HasShares       bool
	RemoteShares    []fileCompanionShareChange
	HasRemoteShares bool
	ForceFull       bool
}

// fetchFileCompanionShareChanges asks the Hub for its change sequence and the
// per-share counters. The answer is a small JSON body, unlike the full list
// that carries every share and note, so a poll round costs almost nothing.
func (a *App) fetchFileCompanionShareChanges(hub, viewer string) (fileCompanionShareChanges, error) {
	var empty fileCompanionShareChanges
	req, err := http.NewRequest(http.MethodGet, hub+"/api/mobile/documents/shares/changes", nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return empty, errFileCompanionShareChangesUnsupported
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return empty, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	var payload struct {
		Seq          uint64          `json:"seq"`
		RemoteSeq    uint64          `json:"remote_seq"`
		Shares       json.RawMessage `json:"shares"`
		RemoteShares json.RawMessage `json:"remote_shares"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return empty, errors.New("云端分享状态无法读取")
	}
	out := fileCompanionShareChanges{Seq: payload.Seq, RemoteSeq: payload.RemoteSeq}
	shares := strings.TrimSpace(string(payload.Shares))
	if shares != "" && shares != "null" {
		out.HasShares = true
		if json.Unmarshal(payload.Shares, &out.Shares) != nil {
			return empty, errors.New("云端分享状态无法读取")
		}
	}
	remote := strings.TrimSpace(string(payload.RemoteShares))
	if remote != "" && remote != "null" {
		out.HasRemoteShares = true
		if json.Unmarshal(payload.RemoteShares, &out.RemoteShares) != nil {
			return empty, errors.New("云端分享状态无法读取")
		}
	}
	return out, nil
}

// fileCompanionShareRefreshProbeOnly classifies the round before it starts.
// A known, fresh Hub state only needs the small changes request. A first login,
// an old Hub, or a stale snapshot goes straight to the full refresh phase.
func fileCompanionShareRefreshProbeOnly(hub string, loginOK bool) bool {
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.hub != hub || !st.fullDone || st.unsupported {
		return false
	}
	if !loginOK {
		return true
	}
	return st.hadLogin && time.Since(st.lastFull) <= fileCompanionShareRefreshFullInterval
}

// fileCompanionShareRefreshQuiet reports whether the next library refresh can
// skip the Hub round trip. A matching changes sequence skips it and returns
// an empty notice. Once a full refresh has already run, a signed-out reader
// or a Hub that cannot be reached also skips it: the full list would fail
// the same way, and the notice is the sentence that list would have shown.
// A fresh login, an older Hub, or a full refresh that has gone stale returns
// quiet false so the refresh runs and reconciles. The changes payload comes
// back either way once this Hub has been read, so a transcript counter can
// be published before that reconcile.
func (a *App) fileCompanionShareRefreshQuiet(hub, viewer string, loginOK bool) (bool, string, fileCompanionShareChanges) {
	var none fileCompanionShareChanges
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	if st.hub != hub {
		st.reset(hub)
	}
	if st.unsupported {
		st.mu.Unlock()
		return false, "", none
	}
	if !loginOK {
		st.hadLogin = false
		quiet := st.fullDone
		st.mu.Unlock()
		if !quiet {
			return false, "", none
		}
		return true, fileCompanionLibraryLoginNotice, none
	}
	if !st.hadLogin {
		st.hadLogin = true
		st.mu.Unlock()
		return false, "", none
	}
	forceFull := !st.fullDone || time.Since(st.lastFull) > fileCompanionShareRefreshFullInterval
	st.mu.Unlock()
	changes, err := a.fetchFileCompanionShareChanges(hub, viewer)
	if err != nil {
		if errors.Is(err, errFileCompanionShareChangesUnsupported) {
			st.mu.Lock()
			st.unsupported = true
			st.mu.Unlock()
			return false, "", none
		}
		// A stale snapshot still has to try the full list: the changes
		// request can fail while the share list is the call that reconciles.
		// After a successful refresh, the same failure skips that list. The
		// opening disk answer has no notice, and this event is what settles
		// the panel.
		if forceFull {
			return false, "", none
		}
		var status *fileCompanionShareStatus
		if errors.As(err, &status) {
			return true, status.Error(), none
		}
		return true, fileCompanionLibraryOfflineNotice, none
	}
	changes.ForceFull = forceFull
	if forceFull {
		return false, "", changes
	}
	st.mu.Lock()
	quiet := changes.Seq == st.seq && changes.RemoteSeq == st.remoteSeq
	st.mu.Unlock()
	return quiet, "", changes
}

// applyFileCompanionShareLiveCounters writes a newer host transcript or reader
// marks sequence onto the stored row. Those two counters are enough for an
// open share to fetch the blob. A new share, a missing home-hub share, a
// moved document, or a moved note still needs the full list: the changes feed
// does not carry the file or the note text. A share on another Hub arrives in
// remote_shares when this Hub sends it. An older Hub only moves remote_seq,
// and that still needs the full list.
func (a *App) applyFileCompanionShareLiveCounters(changes fileCompanionShareChanges) (moved bool, structural bool) {
	if a == nil || !changes.HasShares {
		return false, true
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return false, true
	}
	byToken := make(map[string]fileCompanionShareChange, len(changes.Shares))
	for _, row := range changes.Shares {
		token := strings.TrimSpace(row.Token)
		if !fileCompanionShareTokenMatch.MatchString(token) {
			continue
		}
		row.Token = token
		byToken[token] = row
	}
	dismissed := a.readDismissedShareTokensLocked()
	now := time.Now().UTC()
	known := map[string]bool{}
	changed := false
	for i := range items {
		item := &items[i]
		token := strings.TrimSpace(item.Token)
		if item.Source != "cloud" || item.HubOrigin != "" || !fileCompanionShareTokenMatch.MatchString(token) || dismissed[token] {
			continue
		}
		known[token] = true
		row, found := byToken[token]
		if !found {
			if item.HubOrigin == "" && !fileCompanionShareExpiredAt(item.ExpiresAt, now) {
				structural = true
			}
			continue
		}
		if fileCompanionShareChangeNeedsList(row, *item) {
			structural = true
		}
		if row.ChatRevision > item.ChatRevision {
			item.ChatRevision = row.ChatRevision
			changed = true
		}
		if row.MarksRevision > item.MarksRevision {
			item.MarksRevision = row.MarksRevision
			changed = true
		}
	}
	for token := range byToken {
		if dismissed[token] || known[token] {
			continue
		}
		structural = true
		break
	}
	if changes.HasRemoteShares {
		remoteByKey := make(map[string]fileCompanionShareChange, len(changes.RemoteShares))
		for _, row := range changes.RemoteShares {
			token := strings.TrimSpace(row.Token)
			origin := fileCompanionHubOrigin(row.HubOrigin)
			if !fileCompanionShareTokenMatch.MatchString(token) || origin == "" {
				continue
			}
			row.Token = token
			row.HubOrigin = origin
			remoteByKey[origin+"\n"+token] = row
		}
		seenRemote := map[string]bool{}
		for i := range items {
			item := &items[i]
			token := strings.TrimSpace(item.Token)
			if item.Source != "cloud" || item.HubOrigin == "" || !fileCompanionShareTokenMatch.MatchString(token) || dismissed[token] {
				continue
			}
			origin := fileCompanionHubOrigin(item.HubOrigin)
			if origin == "" {
				continue
			}
			key := origin + "\n" + token
			seenRemote[key] = true
			row, found := remoteByKey[key]
			if !found {
				// remote_shares is a capped set of group invites, not every
				// foreign share. A public link from another Hub, or a group
				// past that cap, is absent on every poll. Treating the absence
				// as a deletion made the next home chat download the full
				// list. A share that really left still changes remote_seq. When
				// no counter in this snapshot was applied, that poll takes the
				// list. When a transcript moved in the same snapshot, the
				// deletion waits for the ten-minute reconcile.
				continue
			}
			if fileCompanionShareChangeNeedsList(row, *item) {
				structural = true
			}
			if row.ChatRevision > item.ChatRevision {
				item.ChatRevision = row.ChatRevision
				changed = true
			}
			if row.MarksRevision > item.MarksRevision {
				item.MarksRevision = row.MarksRevision
				changed = true
			}
		}
		for key, row := range remoteByKey {
			if seenRemote[key] || dismissed[row.Token] {
				continue
			}
			structural = true
			break
		}
	}
	if !changed {
		return false, structural
	}
	if err := a.writeFileCompanionLibraryLocked(items); err != nil {
		return false, true
	}
	return true, structural
}

// emitFileCompanionLibrarySettle publishes the library already stored. The
// snapshot itself is empty; the stamp reads the disk, so a transcript counter
// written just before this call is what the open chat fetches.
func (a *App) emitFileCompanionLibrarySettle(notice string) {
	if a == nil {
		return
	}
	listed := FileCompanionLibraryList{
		Notice:      notice,
		Syncing:     false,
		SyncGen:     fileCompanionLibrarySyncGen.Add(1),
		NotesReply:  a.fileCompanionNotesReply.Load(),
		NotesReview: a.fileCompanionNotesReview.Load(),
	}
	a.emitEvent("file-companion:library", a.stampFileCompanionLibraryList(listed))
}

// catchUpFileCompanionShareLiveCounters publishes a host transcript that
// arrived while a download or a revocation probe is still running. The heavy
// refresh stays single-flight, and waiting for it is what left the
// participant's chat behind the host's line.
func (a *App) catchUpFileCompanionShareLiveCounters() {
	if a == nil || !fileCompanionShareLiveOnce.CompareAndSwap(false, true) {
		return
	}
	defer fileCompanionShareLiveOnce.Store(false)
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return
	}
	changes, err := a.fetchFileCompanionShareChanges(hub, viewer)
	if err != nil || !changes.HasShares {
		return
	}
	moved, _ := a.applyFileCompanionShareLiveCounters(changes)
	if moved {
		a.emitFileCompanionLibrarySettle("")
	}
}

// recordFileCompanionShareListWatermark pins the sequences to the list this
// refresh actually fetched, while that response is the newest fact it has.
// A hub switch in flight leaves the new hub's state alone.
func recordFileCompanionShareListWatermark(hub string, mark fileCompanionShareWatermark) {
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.hub != hub {
		return
	}
	st.seq = mark.seq
	st.remoteSeq = mark.remoteSeq
}

// noteFileCompanionShareFullRefresh records that a full refresh succeeded, so
// the next pre-check has a clock to age against. The sequences were pinned
// when the list itself was fetched.
func noteFileCompanionShareFullRefresh(hub string) {
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.hub != hub {
		return
	}
	st.fullDone = true
	st.lastFull = time.Now()
}

// FileCompanionLibraryItem is one row in 文档管理.
type FileCompanionLibraryItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Title is the document's own title, read from the file. The card shows
	// it under Name and keeps Name as the filename. Empty means this file
	// had no title when it was last read.
	Title    string `json:"title,omitempty"`
	Path     string `json:"path,omitempty"`
	Source   string `json:"source"`
	OpenedAt string `json:"openedAt,omitempty"`
	Owner    string `json:"owner,omitempty"`
	ShareURL string `json:"shareUrl,omitempty"`
	// HubOrigin is the other Hub a public or private link was imported from.
	// Empty means this share lives on the Hub the reader is logged into, even
	// when ShareURL advertises a different public host for that same Hub.
	HubOrigin  string `json:"hubOrigin,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Permanent  bool   `json:"permanent,omitempty"`
	Mine       bool   `json:"mine,omitempty"`
	// Admin is the share creator. Admin keeps edit/delete/transfer rights even
	// after handing the host seat to someone else.
	Admin bool `json:"admin,omitempty"`
	// Host marks the current host seat for this viewer. The desktop only
	// auto-syncs and answers notes from the host seat.
	Host      bool   `json:"host,omitempty"`
	HostLabel string `json:"hostLabel,omitempty"`
	// HostKnown separates "the Hub said this seat is not mine" from a row
	// written before the host seat existed. An unknown seat follows Mine. It is
	// stored: the library is read back from disk on the next start, and a row
	// whose seat answer was dropped would fall back to Mine and hand a
	// transferred host's rights back to the admin. Only true is written, so a
	// legacy row keeps the unknown seat.
	HostKnown bool `json:"hostKnown,omitempty"`
	// PendingHost is the nomination waiting for acceptance; only the admin row
	// carries it.
	PendingHost      bool   `json:"pendingHost,omitempty"`
	PendingHostLabel string `json:"pendingHostLabel,omitempty"`
	Token            string `json:"token,omitempty"`
	Revision         int    `json:"revision,omitempty"`
	Target           string `json:"target,omitempty"`
	GroupID          string `json:"groupId,omitempty"`
	// Expired is derived on every list: the hub no longer lists a link whose
	// time is up, but the project stays here so the reader keeps the paper.
	// An expired share does not block sharing the same paper again.
	Expired bool `json:"expired,omitempty"`
	// Notes are other readers' discussion on this share. The host transcript
	// stays in Messages. NotesKnown is set when this list actually carried
	// the notes field, so an older hub does not wipe notes already stored.
	Notes         []FileCompanionShareNote `json:"notes,omitempty"`
	NotesRevision int                      `json:"notesRevision,omitempty"`
	NotesKnown    bool                     `json:"-"`
	// MarksRevision is the host's reader-marks sequence (highlights,
	// annotations, drawings, bookmarks, chat marks). A reader who already has
	// the paper fetches only the marks blob while it moves without a full
	// project revision bump. An older Hub omits the field and it stays 0.
	MarksRevision int `json:"marksRevision,omitempty"`
	// ChatRevision is the host transcript sequence. Readers who already have
	// the paper fetch that blob when it moves, instead of the project zip.
	// An older Hub omits the field and it stays 0.
	ChatRevision int `json:"chatRevision,omitempty"`
	// Highlights is set on an import that unpacked a companion project.
	// It is not stored in the library file.
	Highlights string `json:"highlights,omitempty"`
	// ChatMarks is the same import-only value for answer highlights. It is
	// absent from a legacy project, which leaves a reader's own marks alone.
	ChatMarks string `json:"chatMarks,omitempty"`
	// ChatAnnotations is the same import-only value for notes on the answers.
	// A legacy project omits it and leaves a reader's own notes alone.
	ChatAnnotations string `json:"chatAnnotations,omitempty"`
	// Annotations is the same import-only value for the host's placed notes.
	Annotations string `json:"annotations,omitempty"`
	// Drawings is the same import-only value for the host's freehand marks.
	Drawings string `json:"drawings,omitempty"`
	// Bookmarks is the same import-only value for named places on the paper.
	// A legacy project omits it and leaves a reader's own bookmarks alone.
	Bookmarks string `json:"bookmarks,omitempty"`
	// KeptShareCopy marks a 本机项目 row whose file still lives inside a
	// retired share's library-files/<token> directory. The owner's own copy
	// is a local project once the share is gone, so the share-cleanup passes
	// that delete download directories must leave this file alone. A
	// participant's copy never gets this flag: it is removed with the share.
	KeptShareCopy bool `json:"keptShareCopy,omitempty"`
}

// FileCompanionShareNote is one reader's discussion on a shared paper.
// Host marks the share owner's notes, including the owner agent's answers.
// ReplyTo names the reader note a host note answers. Decision is the host's
// review of a reader note: pending, approved, or ignored.
type FileCompanionShareNote struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt,omitempty"`
	Host      bool   `json:"host,omitempty"`
	ReplyTo   string `json:"replyTo,omitempty"`
	Decision  string `json:"decision,omitempty"`
}

// FileCompanionLibraryList is the local history plus cloud shares.
type FileCompanionLibraryList struct {
	Items   []FileCompanionLibraryItem `json:"items"`
	Notice  string                     `json:"notice,omitempty"`
	Updates []FileCompanionShareUpdate `json:"updates,omitempty"`
	Removed []string                   `json:"removed,omitempty"`
	// RemovedHosts names whom a reader should ask about each path in Removed:
	// the current host, or the admin when this machine holds the host seat.
	// The window shows it before closing the tab. A row written before the
	// host label existed has no entry, and the window falls back to a generic
	// note.
	RemovedHosts map[string]string `json:"removedHosts,omitempty"`
	// NotesReply is true when the Hub records host replies. The window keeps a
	// participant's own turn only when the Hub cannot carry the answer back.
	NotesReply bool `json:"notesReply"`
	// NotesReview is true when a reader note waits for the host to pass or
	// ignore it before the host agent sees it.
	NotesReview bool `json:"notesReview"`
	// Syncing is true when this answer is the library already stored on disk
	// and a cloud refresh is still copying shares. The panel paints this
	// immediately; the refresh pushes the finished list when it is done.
	Syncing bool `json:"syncing,omitempty"`
	// Catalog is the hub index published before file bodies finish
	// downloading. Syncing on a list call is only "a refresh started"; the
	// index is the snapshot whose revision the file on disk may not have yet.
	Catalog bool `json:"catalog,omitempty"`
	// SyncGen orders answers on screen. A disk read taken while a refresh is
	// still copying shares keeps the generation from the start of that refresh.
	// The hub index and the finished list publish strictly newer ones, so a
	// disk answer cannot cover the index whichever result the window applies
	// first.
	SyncGen uint64 `json:"syncGen,omitempty"`
	// LibraryRev is the library write this answer was read from. It moves on
	// every save. A refresh that started earlier keeps its old sync generation,
	// and the panel uses this revision to paint a delete that landed while
	// that refresh was still copying shares.
	LibraryRev uint64 `json:"libraryRev,omitempty"`
}

// fileCompanionLibraryListInline keeps a list call on the cloud refresh. Tests
// use that so one call still imports and probes. The app returns the stored
// library first and refreshes in the background, because the refresh downloads
// documents and the panel was waiting on 正在读取 until every download finished.
var fileCompanionLibraryListInline bool

// fileCompanionLibraryTitleFillAsync reads document titles off the list call.
// Tests turn it off so a fill cannot write the library after the test returns.
var fileCompanionLibraryTitleFillAsync = true

var fileCompanionLibrarySyncRunning atomic.Bool

const (
	fileCompanionLibrarySyncIdle uint32 = iota
	fileCompanionLibrarySyncProbe
	fileCompanionLibrarySyncRefresh
)

// A change probe is not a visible content refresh. The phase is separate from
// SyncRunning so polling can check the Hub without making the library repaint
// loading and syncing affordances before the stored rows are shown again.
var fileCompanionLibrarySyncPhase atomic.Uint32

var fileCompanionLibrarySyncGen atomic.Uint64

// fileCompanionLibraryRev counts successful library saves. A list stamps the
// value it actually read, so a delete is newer than a refresh snapshot that
// was already in flight.
var fileCompanionLibraryRev atomic.Uint64

// fileCompanionLibrarySyncEpoch is the generation disk answers keep while the
// current refresh is running. The hub index publishes a newer one.
var fileCompanionLibrarySyncEpoch atomic.Uint64

// FileCompanionShareResult is the link created for the open file.
type FileCompanionShareResult struct {
	URL           string `json:"url"`
	Name          string `json:"name"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	Visibility    string `json:"visibility"`
	Permanent     bool   `json:"permanent"`
	Token         string `json:"token,omitempty"`
	Revision      int    `json:"revision,omitempty"`
	MarksRevision int    `json:"marksRevision,omitempty"`
	ChatRevision  int    `json:"chatRevision,omitempty"`
}

// FileCompanionShareUpdate is a newer project the reader should show.
// Highlights and messages replace what that path had on screen. ChatMarks,
// ChatAnnotations, Annotations, Drawings, and Bookmarks are present only when
// the project carries them; a legacy project omits them and the reader's own
// marks stay.
type FileCompanionShareUpdate struct {
	Path            string                 `json:"path"`
	Token           string                 `json:"token"`
	Name            string                 `json:"name"`
	Revision        int                    `json:"revision"`
	Highlights      string                 `json:"highlights,omitempty"`
	ChatMarks       *string                `json:"chatMarks,omitempty"`
	ChatAnnotations *string                `json:"chatAnnotations,omitempty"`
	Annotations     *string                `json:"annotations,omitempty"`
	Drawings        *string                `json:"drawings,omitempty"`
	Bookmarks       *string                `json:"bookmarks,omitempty"`
	Messages        []FileCompanionMessage `json:"messages"`
	Project         bool                   `json:"project"`
}

type fileCompanionShareView struct {
	Token string `json:"token"`
	Name  string `json:"name"`
	// Host/Admin are per-viewer flags on list rows. Host is a pointer so an
	// older Hub (field absent) can default to the owner. Mine marks the share
	// this member published from another Hub: it arrives on the received list
	// but behaves like an owned row.
	Host             *bool                    `json:"host,omitempty"`
	Mine             bool                     `json:"mine,omitempty"`
	Admin            bool                     `json:"admin,omitempty"`
	HostLabel        string                   `json:"host_label,omitempty"`
	PendingHost      bool                     `json:"pending_host,omitempty"`
	PendingHostLabel string                   `json:"pending_host_label,omitempty"`
	Size             int64                    `json:"size"`
	Owner            string                   `json:"owner"`
	Visibility       string                   `json:"visibility"`
	Permanent        bool                     `json:"permanent"`
	Expired          bool                     `json:"expired"`
	URL              string                   `json:"url"`
	CreatedAt        string                   `json:"created_at"`
	ExpiresAt        string                   `json:"expires_at"`
	Revision         int                      `json:"revision"`
	Target           string                   `json:"target"`
	GroupID          string                   `json:"group_id"`
	Notes            *[]fileCompanionNoteWire `json:"notes"`
	NotesRevision    *int                     `json:"notes_revision"`
	// MarksRevision is a pointer so an older Hub (field absent) reads as 0.
	MarksRevision *int `json:"marks_revision"`
	// ChatRevision is a pointer for the same reason: an older Hub has no
	// transcript blob, and a missing field must not look like revision 0
	// catching up.
	ChatRevision *int `json:"chat_revision"`
	// HubOrigin and ReaderGrant are set when the share lives on another hub.
	// The home hub lists it so the reader can see a group they joined there.
	HubOrigin   string `json:"hub_origin,omitempty"`
	ReaderGrant string `json:"reader_grant,omitempty"`
}

type fileCompanionNoteWire struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Created   string `json:"createdAt"`
	Host      bool   `json:"host"`
	ReplyTo   string `json:"reply_to"`
	Decision  string `json:"decision"`
}

type fileCompanionShareStatus struct {
	status  int
	message string
	// A create that collides with a live share also names that share. The
	// desktop adopts it instead of leaving a duplicate dead end. For a share
	// that lives on another Hub the view additionally carries its origin and
	// the reader grant, so the adopt path can keep writing to that Hub.
	token  string
	origin string
	grant  string
}

func (e *fileCompanionShareStatus) Error() string {
	if e == nil {
		return "云端分享失败"
	}
	// A hub that knows this route sends its own message, including a missing
	// share. A 404 with no message is an old hub that has no share route.
	if msg := strings.TrimSpace(e.message); msg != "" {
		return msg
	}
	if e.status == http.StatusNotFound {
		return "云端还没有文档分享，需要更新 Hub"
	}
	if e.status == http.StatusUnauthorized {
		return "请先登录 MaClaw Hub"
	}
	return fmt.Sprintf("云端分享失败（HTTP %d）", e.status)
}

func (a *App) fileCompanionLibraryPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "library.json")
}

// rememberFileCompanionDocument records a file that opened. A failed write
// leaves the open itself alone.
func (a *App) rememberFileCompanionDocument(doc FileCompanionDocument) {
	if a == nil || strings.TrimSpace(doc.Error) != "" || strings.TrimSpace(doc.Path) == "" {
		return
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return
	}
	items, changed := upsertFileCompanionLocal(items, doc)
	// Opening a file that is already a cloud project must not rewrite the
	// library. A no-op write would replace notes a refresh just stored.
	if !changed {
		return
	}
	_ = a.writeFileCompanionLibraryLocked(items)
}

func upsertFileCompanionLocal(items []FileCompanionLibraryItem, doc FileCompanionDocument) ([]FileCompanionLibraryItem, bool) {
	// A path a cloud project already names is that cloud project. Opening it
	// must not write a parallel local row for the sync to upload or delete.
	if fileCompanionCloudOwnsPath(items, doc.Path) {
		return items, false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	next := FileCompanionLibraryItem{
		ID:       fileCompanionLibraryLocalID(doc.Path),
		Name:     strings.TrimSpace(doc.Name),
		Path:     doc.Path,
		Source:   "local",
		OpenedAt: now,
		Size:     doc.Size,
	}
	if next.Name == "" {
		next.Name = filepath.Base(doc.Path)
	}
	kept := make([]FileCompanionLibraryItem, 0, len(items)+1)
	for _, item := range items {
		if item.Source == "local" && fileCompanionSameLibraryPath(item.Path, doc.Path) {
			if item.ID != "" {
				next.ID = item.ID
			}
			continue
		}
		kept = append(kept, item)
	}
	kept = append([]FileCompanionLibraryItem{next}, kept...)
	return trimFileCompanionLibrary(kept, fileCompanionLibraryCap), true
}

func fileCompanionLibraryLocalID(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return "local:" + hex.EncodeToString(sum[:8])
}

func (a *App) readFileCompanionLibraryLocked() ([]FileCompanionLibraryItem, bool) {
	stored, ok := a.readFileCompanionLibraryFileLocked()
	if !ok {
		return nil, false
	}
	if stored.Items == nil {
		return []FileCompanionLibraryItem{}, true
	}
	return stored.Items, true
}

func (a *App) readFileCompanionLibraryFileLocked() (fileCompanionLibraryFile, bool) {
	path := a.fileCompanionLibraryPath()
	if path == "" {
		return fileCompanionLibraryFile{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileCompanionLibraryFile{}, true
		}
		return fileCompanionLibraryFile{}, false
	}
	var stored fileCompanionLibraryFile
	if json.Unmarshal(raw, &stored) != nil {
		return fileCompanionLibraryFile{}, false
	}
	return stored, true
}

func (a *App) writeFileCompanionLibraryLocked(items []FileCompanionLibraryItem) error {
	prev, ok := a.readFileCompanionLibraryFileLocked()
	var titles map[string]fileCompanionTitleRecord
	if ok {
		titles = prev.Titles
	}
	return a.writeFileCompanionLibraryFileLocked(items, titles, true)
}

func (a *App) writeFileCompanionLibraryFileLocked(items []FileCompanionLibraryItem, titles map[string]fileCompanionTitleRecord, bumpRev bool) error {
	path := a.fileCompanionLibraryPath()
	if path == "" {
		return errors.New("library path is empty")
	}
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	titles = pruneFileCompanionTitles(items, titles)
	body, err := json.Marshal(fileCompanionLibraryFile{Items: items, Titles: titles})
	if err != nil {
		return err
	}
	if err := fileCompanionWriteAtomicFile(path, body, 0o600); err != nil {
		return err
	}
	// A title-only write must not move the revision. The panel treats a newer
	// revision as a row change and will paint that older generation over a
	// hub catalog that is already on screen.
	if bumpRev {
		fileCompanionLibraryRev.Add(1)
	}
	return nil
}

func fileCompanionWriteAtomicFile(dest string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ListFileCompanionLibrary returns this machine's reading history and, when
// the Hub login is present, shares this person created or imported.
func (a *App) ListFileCompanionLibrary() (FileCompanionLibraryList, error) {
	if fileCompanionLibraryListInline {
		return a.listFileCompanionLibrary()
	}
	// Stamp the generation before the disk read. While a refresh is running,
	// every disk answer keeps the generation from the start of that refresh.
	// The hub index published during the read is newer and stays on screen
	// even when this answer is applied first.
	started := a.startFileCompanionLibrarySync()
	// A download already in flight must not hide the host's next line. The
	// changes feed carries the transcript counter, and the open chat fetches
	// that blob without waiting for the project zip.
	if !started && fileCompanionLibrarySyncRunning.Load() {
		go a.catchUpFileCompanionShareLiveCounters()
	}
	phase := fileCompanionLibrarySyncPhase.Load()
	running := started || phase != fileCompanionLibrarySyncIdle
	var gen uint64
	if running {
		gen = fileCompanionLibrarySyncEpoch.Load()
	} else {
		gen = fileCompanionLibrarySyncGen.Load()
	}
	listed := a.listFileCompanionLibraryStored()
	listed.SyncGen = gen
	// A probe only answers whether the content needs refreshing. It must not
	// present the stored rows as a visible sync in progress.
	if fileCompanionLibrarySyncPhase.Load() == fileCompanionLibrarySyncRefresh {
		listed.Syncing = true
	}
	a.startFileCompanionTitleFill()
	return listed, nil
}

// publishFileCompanionLibraryCatalog shows the hub index while file bodies are
// still downloading. The synchronous list used by tests keeps a single result.
func (a *App) publishFileCompanionLibraryCatalog(items []FileCompanionLibraryItem, notice string, libraryRev uint64) {
	if a == nil || fileCompanionLibraryListInline {
		return
	}
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	fileCompanionMarkLibraryExpired(items, time.Now().UTC())
	listed := FileCompanionLibraryList{
		Items:       items,
		Notice:      notice,
		Syncing:     true,
		Catalog:     true,
		SyncGen:     fileCompanionLibrarySyncGen.Add(1),
		LibraryRev:  libraryRev,
		NotesReply:  a.fileCompanionNotesReply.Load(),
		NotesReview: a.fileCompanionNotesReview.Load(),
	}
	if fileCompanionLibraryCatalogHook != nil {
		fileCompanionLibraryCatalogHook(listed)
	}
	// The hook can wait while a delete lands. Stamp the event from the library
	// now, so the panel does not paint the snapshot the hook just observed.
	a.emitEvent("file-companion:library", a.stampFileCompanionLibraryList(listed))
}

// fileCompanionLibraryCatalogHook observes the hub index in tests, before the
// refresh downloads file bodies. Production leaves it nil.
var fileCompanionLibraryCatalogHook func(FileCompanionLibraryList)

// startFileCompanionLibrarySync runs one cloud refresh off the list call. A
// second open while it is still copying shares gets the stored library and
// the same refresh, instead of waiting for the downloads.
func (a *App) startFileCompanionLibrarySync() bool {
	if a == nil || fileCompanionLibraryListInline {
		return false
	}
	hub, viewer, loginErr := a.fileCompanionHubLogin()
	phase := uint32(fileCompanionLibrarySyncRefresh)
	if fileCompanionShareRefreshProbeOnly(hub, loginErr == nil) {
		phase = fileCompanionLibrarySyncProbe
	}
	if !fileCompanionLibrarySyncPhase.CompareAndSwap(fileCompanionLibrarySyncIdle, phase) {
		return false
	}
	fileCompanionLibrarySyncRunning.Store(true)
	// Disk answers keep this generation until the refresh publishes the hub
	// index and, later, the finished list.
	fileCompanionLibrarySyncEpoch.Store(fileCompanionLibrarySyncGen.Load())
	go func() {
		defer func() {
			fileCompanionLibrarySyncRunning.Store(false)
			fileCompanionLibrarySyncPhase.Store(fileCompanionLibrarySyncIdle)
		}()
		// The fast poll ticks every few seconds while a share is open. When
		// the Hub reports no change since the last full refresh, skip the
		// share list, revocation probes, and downloads. A host line only
		// moves chat_revision. That counter is in this same small response,
		// so the open chat can fetch the transcript without the share list.
		quiet, notice, look := a.fileCompanionShareRefreshQuiet(hub, viewer, loginErr == nil)
		if !quiet && look.HasShares {
			moved, structural := a.applyFileCompanionShareLiveCounters(look)
			st := &fileCompanionShareRefreshCheck
			st.mu.Lock()
			remoteMoved := st.hub == hub && look.RemoteSeq != st.remoteSeq
			st.mu.Unlock()
			// An older Hub only sends remote_seq. A newer one also sends the
			// foreign counters, and apply already decided whether they need
			// the list. Forcing the list here made every cross-hub chat wait
			// on the share list, the probes, and the zip.
			if remoteMoved && !look.HasRemoteShares {
				structural = true
			}
			if moved {
				a.emitFileCompanionLibrarySettle("")
			}
			// Skip the share list only when the new sequence is a transcript
			// or marks counter we just published. A host transfer, a new
			// audience, or an expiry bumps the same sequence without moving
			// those counters, and skipping it would hide them until the
			// ten-minute reconcile. A sequence bump this feed cannot explain
			// still takes the list.
			if moved && !structural && !look.ForceFull {
				recordFileCompanionShareListWatermark(hub, fileCompanionShareWatermark{seq: look.Seq, remoteSeq: look.RemoteSeq})
				return
			}
		}
		if quiet {
			// The hub list is not worth fetching: nothing changed, the reader
			// is signed out, or the hub cannot be reached. Settle the panel on
			// the library as it is now. Leaving the opening answer marked
			// syncing made every later disk read look like a duplicate of that
			// refresh, so a delete the reader just confirmed stayed on screen.
			// The notice is the one the full list would have shown. Stamp reads
			// the disk once; an empty snapshot adds nothing a later delete did
			// not save.
			a.emitFileCompanionLibrarySettle(notice)
			return
		}
		fileCompanionLibrarySyncPhase.Store(fileCompanionLibrarySyncRefresh)
		listed, err := a.listFileCompanionLibrary()
		if err != nil {
			return
		}
		if loginErr == nil {
			noteFileCompanionShareFullRefresh(hub)
		}
		listed.Syncing = false
		listed.SyncGen = fileCompanionLibrarySyncGen.Add(1)
		a.emitEvent("file-companion:library", a.stampFileCompanionLibraryList(listed))
	}()
	return true
}

// listFileCompanionLibraryStored is the library this machine can already show.
// It does not call the Hub and does not download. A refresh that is copying
// shares keeps the disk: writing the pre-refresh rows here would cover files
// that refresh just stored.
func (a *App) listFileCompanionLibraryStored() FileCompanionLibraryList {
	empty := FileCompanionLibraryList{Items: []FileCompanionLibraryItem{}}
	if a == nil {
		return empty
	}
	fileCompanionLibraryMu.Lock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		fileCompanionLibraryMu.Unlock()
		return empty
	}
	if !fileCompanionLibrarySyncRunning.Load() {
		var changed bool
		items, changed = dropMissingFileCompanionLibrary(items)
		if len(items) > fileCompanionLibraryCap {
			items = trimFileCompanionLibrary(items, fileCompanionLibraryCap)
			changed = true
		}
		if changed {
			_ = a.writeFileCompanionLibraryLocked(items)
		}
	}
	// One card per file on screen. The local row stays on disk until a
	// refresh has seen the share still live: writing the fold here would
	// delete it when the Hub cannot be reached.
	if folded, foldChanged := foldFileCompanionPromotedLocals(items); foldChanged {
		items = folded
	}
	rev := fileCompanionLibraryRev.Load()
	fileCompanionLibraryMu.Unlock()
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	fileCompanionMarkLibraryExpired(items, time.Now().UTC())
	a.applyFileCompanionTitles(items)
	return FileCompanionLibraryList{Items: items, NotesReply: a.fileCompanionNotesReply.Load(), NotesReview: a.fileCompanionNotesReview.Load(), LibraryRev: rev}
}

func (a *App) listFileCompanionLibrary() (FileCompanionLibraryList, error) {
	empty := FileCompanionLibraryList{Items: []FileCompanionLibraryItem{}}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	fileCompanionLibraryMu.Lock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		fileCompanionLibraryMu.Unlock()
		return empty, nil
	}
	libraryRev := uint64(0)
	items, changed := dropMissingFileCompanionLibrary(items)
	if len(items) > fileCompanionLibraryCap {
		items = trimFileCompanionLibrary(items, fileCompanionLibraryCap)
		changed = true
	}
	if changed {
		_ = a.writeFileCompanionLibraryLocked(items)
	}
	// Same as the stored list: hide the duplicate for this answer, and
	// leave the local row on disk until the Hub list below is in hand.
	if folded, foldChanged := foldFileCompanionPromotedLocals(items); foldChanged {
		items = folded
	}
	libraryRev = fileCompanionLibraryRev.Load()
	fileCompanionLibraryMu.Unlock()

	notice := ""
	var updates []FileCompanionShareUpdate
	var removed []string
	var removedHosts map[string]string
	hub, viewer, loginErr := a.fileCompanionHubLogin()
	if loginErr != nil {
		notice = fileCompanionLibraryLoginNotice
	} else {
		// One refresh at a time. A second open of 文档管理 waits, then sees
		// the revision the first refresh already stored, instead of downloading
		// the same project twice and writing an older library over the newer one.
		fileCompanionLibraryRefreshMu.Lock()
		remote, notesReply, notesReview, _, mark, err := a.fetchFileCompanionShareList(hub, viewer)
		if err != nil {
			fileCompanionLibraryRefreshMu.Unlock()
			var status *fileCompanionShareStatus
			if errors.As(err, &status) {
				notice = status.Error()
			} else {
				notice = fileCompanionLibraryOfflineNotice
			}
		} else {
			recordFileCompanionShareListWatermark(hub, mark)
			a.fileCompanionNotesReply.Store(notesReply)
			a.fileCompanionNotesReview.Store(notesReview)
			// A received link can disappear from the list because the host
			// deleted it, or because it expired. Ask the Hub which one before
			// the merge keeps a downloaded copy forever. No library lock is
			// held while these network probes run.
			fileCompanionLibraryMu.Lock()
			probeSnapshot := append([]FileCompanionLibraryItem(nil), items...)
			dismissedForProbe := a.readDismissedShareTokensLocked()
			fileCompanionLibraryMu.Unlock()
			revoked, foreignViews := a.probeRevokedFileCompanionShares(hub, viewer, probeSnapshot, remote, dismissedForProbe)
			fileCompanionLibraryMu.Lock()
			fresh, freshOK := a.readFileCompanionLibraryLocked()
			if freshOK {
				items = fresh
			}
			previous := map[string]FileCompanionLibraryItem{}
			epochs := map[string]uint64{}
			for _, item := range items {
				if item.Source == "cloud" && item.Token != "" {
					previous[item.Token] = item
					epochs[item.Token] = fileCompanionShareEpoch[item.Token]
				}
			}
			dismissedNow, userDismissed := a.readDismissedSharesLocked()
			// The fast poll runs while a share is open. The directory walk is
			// only needed to rediscover a copy the library row no longer names.
			knownPaths := a.fileCompanionKnownSharePathsLocked(items, remote, dismissedNow, userDismissed)
			// A probe dismissal is not the reader's delete. The Hub list still
			// naming the share means the link is live, and the open file is
			// still that discussion. A 文档管理 delete stays hidden.
			dismissedNow = a.reviveProbeDismissedSharesLocked(remote, dismissedNow, userDismissed, knownPaths)
			items = mergeFileCompanionSharesWithRevoked(items, remote, hub, dismissedNow, revoked, knownPaths)
			for _, item := range items {
				if item.Source != "cloud" || !fileCompanionShareTokenMatch.MatchString(item.Token) {
					continue
				}
				if fileCompanionLibraryFileExists(item.Path) {
					knownPaths[item.Token] = item.Path
				}
			}
			a.rememberFileCompanionSharePathsLocked(knownPaths)
			items = applyForeignCompanionShares(items, foreignViews)
			fileCompanionLibraryMu.Unlock()
			removedTokens := map[string]bool{}
			var purgePaths []string
			var restored []FileCompanionLibraryItem
			removed, purgePaths, removedHosts, restored = a.dropRevokedFileCompanionShares(items, previous, remote, revoked, removedTokens, epochs)
			// The restored 本机项目 is already on disk. The catalog published
			// below is this snapshot, and the downloads can take a while, so
			// the owner's file has to be in it or 文档管理 hides the paper
			// until the refresh finishes.
			if len(restored) > 0 {
				items = append(restored, items...)
			}
			// Drop local rows only after revoke has seen them. A host paper
			// still listed as local keeps its tab; a live share then retires
			// that row because the cloud project is the document.
			items, _ = foldFileCompanionPromotedLocals(items)
			// A previous refresh may already have dropped the cloud row and
			// still left the open copy, because the local row shared its path.
			// Finish that withdrawal once the Hub list confirms the share is gone.
			extraRemoved, extraPurge := a.finishProbeDismissedInstalledCopies(hub, viewer, remote, removedTokens)
			removed = append(removed, extraRemoved...)
			purgePaths = append(purgePaths, extraPurge...)
			// A group leave does not dismiss the token. When the open PDF could
			// not be deleted, or the cloud row was already gone, the next list
			// still has to close that tab and remove the downloaded copy.
			leftRemoved, leftPurge := a.finishAbandonedShareCopies(remote, removedTokens)
			removed = append(removed, leftRemoved...)
			purgePaths = append(purgePaths, leftPurge...)
			// The panel can show the hub index before the file bodies arrive.
			// Pull still decides from items, so this copy must not share its
			// backing array with the downloads. A delete that landed while the
			// hub list was in flight is already on disk; publish that library,
			// not the snapshot taken before the delete.
			items, libraryRev = a.projectFileCompanionLibraryForPublish(items, libraryRev)
			a.publishFileCompanionLibraryCatalog(append([]FileCompanionLibraryItem(nil), items...), notice, libraryRev)
			updates = a.pullNewerFileCompanionShares(hub, viewer, items, previous)
			fileCompanionLibraryMu.Lock()
			if fresh, freshOK = a.readFileCompanionLibraryLocked(); freshOK {
				// A delete that landed while this refresh was downloading has
				// already hidden the share. The snapshot must not put the row
				// back. The download publishes only while that share is still
				// wanted, and Delete purges when it clears the last row, so
				// this refresh does not remove reading data after the lock.
				// Doing so here would race a re-import that already wrote the
				// transcript the reader just asked for.
				dismissedNow := a.readDismissedShareTokensLocked()
				for token := range dismissedNow {
					removedTokens[token] = true
				}
				// The revoke decision was made before these downloads. An import
				// in that gap owns the share now. Leaving the token dropped would
				// remove the row and close the tab after the file was published again.
				superseded := map[string]bool{}
				for token := range removedTokens {
					if dismissedNow[token] || fileCompanionShareEpoch[token] == epochs[token] {
						continue
					}
					delete(removedTokens, token)
					superseded[token] = true
				}
				removed = a.withoutSupersededRevokePaths(removed, superseded)
				items = reconcileFileCompanionLibrary(fresh, items, removedTokens)
				items = omitOpenedShareCopies(items, removed)
				// Keep a tab (and its reader state) when another row still uses
				// that path. Only a path whose last row is gone is withdrawn.
				if len(removed) > 0 {
					keptRemoved := make([]string, 0, len(removed))
					for _, path := range removed {
						if path != "" && !fileCompanionLibraryUsesPath(items, path) {
							keptRemoved = append(keptRemoved, path)
						}
					}
					removed = keptRemoved
				}
				removedHosts = onlyRemovedFileCompanionHosts(removed, removedHosts)
				updates = withoutDismissedShareUpdates(updates, dismissedNow)
				// Persist a local row until the Hub list itself names that share.
				// The list returned below still shows one card per file. A failed
				// re-read must not write the catalog snapshot: that snapshot
				// already hid the local row, and saving it would delete it.
				liveShareTokens := map[string]bool{}
				for _, item := range remote {
					if token := strings.TrimSpace(item.Token); token != "" {
						liveShareTokens[token] = true
					}
				}
				stored, _ := foldFileCompanionPromotedLocalsLive(items, liveShareTokens)
				shown, _ := foldFileCompanionPromotedLocals(stored)
				if err := a.writeFileCompanionLibraryLocked(stored); err == nil {
					items = shown
					// Judge the purge against the rows just saved. The shown
					// list hides a local row the disk still has; purging from
					// that list would delete reading data the row still uses.
					for _, path := range purgePaths {
						if path != "" && !fileCompanionLibraryUsesPath(stored, path) {
							a.purgeFileCompanionProjectData(path)
						}
					}
				}
				libraryRev = fileCompanionLibraryRev.Load()
			}
			fileCompanionLibraryMu.Unlock()
			fileCompanionLibraryRefreshMu.Unlock()
		}
	}
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	// Expiry is derived, never stored: a row read here may predate the current
	// moment, and the UI needs to know it can publish the paper again.
	fileCompanionMarkLibraryExpired(items, time.Now().UTC())
	a.applyFileCompanionTitles(items)
	return FileCompanionLibraryList{Items: items, Notice: notice, Updates: updates, Removed: removed, RemovedHosts: removedHosts, NotesReply: a.fileCompanionNotesReply.Load(), NotesReview: a.fileCompanionNotesReview.Load(), LibraryRev: libraryRev}, nil
}

// projectFileCompanionLibraryForPublish drops rows the reader deleted while
// this refresh was still talking to the Hub. The snapshot was merged earlier.
// Publishing it unchanged would put a removed 本机项目 or 云端项目 back on
// screen with a newer sync generation. The returned revision is the library
// this projection was read from, so the panel can tell it from that snapshot.
func (a *App) projectFileCompanionLibraryForPublish(snapshot []FileCompanionLibraryItem, fallbackRev uint64) ([]FileCompanionLibraryItem, uint64) {
	if a == nil {
		return snapshot, fallbackRev
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, rev, _ := a.projectFileCompanionLibraryLocked(snapshot, fallbackRev)
	return items, rev
}

// stampFileCompanionLibraryList is the list an event may send. The refresh
// builds that list, then keeps working; a delete in the gap has already saved.
// Rows and the revision come from the same locked read, and a dismissed share
// loses its download update so the window does not apply notes for a card
// that just left.
func (a *App) stampFileCompanionLibraryList(listed FileCompanionLibraryList) FileCompanionLibraryList {
	if a == nil {
		return listed
	}
	fileCompanionLibraryMu.Lock()
	items, rev, dismissed := a.projectFileCompanionLibraryLocked(listed.Items, listed.LibraryRev)
	listed.Items = items
	listed.LibraryRev = rev
	listed.Updates = withoutDismissedShareUpdates(listed.Updates, dismissed)
	// A path the refresh decided to close can belong to a row again: the
	// reader imported it while this list waited to be sent. Closing it now
	// would drop the tab of the project just saved.
	if len(listed.Removed) > 0 {
		kept := make([]string, 0, len(listed.Removed))
		for _, path := range listed.Removed {
			if path != "" && fileCompanionLibraryUsesPath(items, path) {
				continue
			}
			kept = append(kept, path)
		}
		listed.Removed = kept
		listed.RemovedHosts = onlyRemovedFileCompanionHosts(listed.Removed, listed.RemovedHosts)
	}
	fileCompanionLibraryMu.Unlock()
	// Expired is derived at send time. The rows just read from disk do not
	// carry it, and a share that lapsed while the refresh ran has to say so.
	fileCompanionMarkLibraryExpired(listed.Items, time.Now().UTC())
	a.applyFileCompanionTitles(listed.Items)
	return listed
}

func fileCompanionMarkLibraryExpired(items []FileCompanionLibraryItem, now time.Time) {
	for i := range items {
		if items[i].Source == "cloud" {
			items[i].Expired = fileCompanionShareExpiredAt(items[i].ExpiresAt, now)
		}
	}
}

func (a *App) projectFileCompanionLibraryLocked(snapshot []FileCompanionLibraryItem, fallbackRev uint64) ([]FileCompanionLibraryItem, uint64, map[string]bool) {
	dismissed := a.readDismissedShareTokensLocked()
	fresh, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return snapshot, fallbackRev, dismissed
	}
	items := reconcileFileCompanionLibrary(fresh, snapshot, dismissed)
	items, _ = foldFileCompanionPromotedLocals(items)
	return items, fileCompanionLibraryRev.Load(), dismissed
}

// fileCompanionShareExpiredAt reports whether an RFC3339 expiry has passed.
// An empty value is a permanent share; an unreadable one is left alone so a
// malformed hub value does not silently unshare a project.
func fileCompanionShareExpiredAt(expiresAt string, now time.Time) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return !now.Before(at)
}

func dropMissingFileCompanionLibrary(items []FileCompanionLibraryItem) ([]FileCompanionLibraryItem, bool) {
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	changed := false
	for _, item := range items {
		if item.Source != "local" {
			if item.Source == "cloud" && strings.TrimSpace(item.Path) != "" {
				info, err := os.Stat(item.Path)
				if err == nil && info.IsDir() {
					item.Path = ""
					changed = true
				} else if err != nil && os.IsNotExist(err) {
					item.Path = ""
					changed = true
				}
			}
			kept = append(kept, item)
			continue
		}
		path := strings.TrimSpace(item.Path)
		if path == "" {
			changed = true
			continue
		}
		info, err := os.Stat(path)
		if err != nil && os.IsNotExist(err) {
			changed = true
			continue
		}
		if err == nil && info.IsDir() {
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	return kept, changed
}

func mergeFileCompanionShares(items, remote []FileCompanionLibraryItem, hub string, dismissed map[string]bool) []FileCompanionLibraryItem {
	return mergeFileCompanionSharesWithRevoked(items, remote, hub, dismissed, nil, nil)
}

// mergeFileCompanionSharesWithRevoked also drops a previous cloud row the Hub
// confirmed as deleted. A non-group share the list no longer returns is kept
// only while it still exists on the Hub (for example, an expired link); a
// revoked one must not linger just because its downloaded file is present.
// knownPaths restores the local file when the cloud row itself was dropped:
// the Hub list never carries a path, so a later list cannot reattach the open
// tab unless this machine still remembers where the copy is.
func mergeFileCompanionSharesWithRevoked(items, remote []FileCompanionLibraryItem, hub string, dismissed, revoked map[string]bool, knownPaths map[string]string) []FileCompanionLibraryItem {
	previous := map[string]FileCompanionLibraryItem{}
	kept := make([]FileCompanionLibraryItem, 0, len(items)+len(remote))
	for _, item := range items {
		if item.Source == "cloud" && item.Token != "" {
			previous[item.Token] = item
			continue
		}
		kept = append(kept, item)
	}
	seen := map[string]bool{}
	for _, item := range remote {
		if item.Token == "" || seen[item.Token] || dismissed[item.Token] {
			continue
		}
		seen[item.Token] = true
		if prev, ok := previous[item.Token]; ok {
			if item.Path == "" && fileCompanionLibraryFileExists(prev.Path) {
				item.Path = prev.Path
			}
			if prev.OpenedAt != "" {
				item.OpenedAt = prev.OpenedAt
			}
			if item.ShareURL == "" {
				item.ShareURL = prev.ShareURL
			}
			if item.HubOrigin == "" {
				item.HubOrigin = prev.HubOrigin
			}
			// A remote-published share stays this member's own for its whole
			// life; a stale snapshot that lost the flag must not demote the row.
			if prev.Mine {
				item.Mine = true
			}
			if prev.Admin {
				item.Admin = true
			}
			// A hub that does not know about notes omits the field. Keep the
			// notes this machine already stored. A list that left earlier
			// also must not cover a note that landed while it was in flight.
			if !item.NotesKnown || prev.NotesRevision > item.NotesRevision {
				item.Notes = prev.Notes
				item.NotesRevision = prev.NotesRevision
			}
			// An older Hub omits marks_revision. Keep the newer sequence this
			// machine already saw so a reader does not re-apply old marks.
			if prev.MarksRevision > item.MarksRevision {
				item.MarksRevision = prev.MarksRevision
			}
			if prev.ChatRevision > item.ChatRevision {
				item.ChatRevision = prev.ChatRevision
			}
		}
		// The Hub row has no local path. A dropped cloud row, or a probe
		// dismissal that this list just took back, still has the file.
		if strings.TrimSpace(item.Path) == "" {
			if path := strings.TrimSpace(knownPaths[item.Token]); fileCompanionLibraryFileExists(path) {
				item.Path = path
			}
		}
		if item.ShareURL == "" {
			item.ShareURL = strings.TrimRight(hub, "/") + "/hub/documents/shares/" + item.Token
		}
		kept = append(kept, item)
	}
	for token, prev := range previous {
		if seen[token] || dismissed[token] || revoked[token] || !fileCompanionLibraryFileExists(prev.Path) {
			continue
		}
		// A local group row missing from the list is already revoked by
		// membership. A foreign group row waits on the probe: the remote
		// snapshot going quiet only means the origin could not be read.
		if prev.Visibility == "group" && fileCompanionHubOrigin(prev.HubOrigin) == "" {
			continue
		}
		kept = append(kept, prev)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].OpenedAt > kept[j].OpenedAt
	})
	return trimFileCompanionLibrary(kept, fileCompanionLibraryCap)
}

// applyForeignCompanionShares copies a live snapshot from another Hub onto
// the row that was imported from that Hub. The home Hub list does not know
// this token. Notes and revision still have to move, or the reader never sees
// a new highlight or a host reply.
func applyForeignCompanionShares(items []FileCompanionLibraryItem, views map[string]fileCompanionShareView) []FileCompanionLibraryItem {
	if len(views) == 0 {
		return items
	}
	for i := range items {
		item := &items[i]
		if item.Source != "cloud" || item.HubOrigin == "" {
			continue
		}
		view, ok := views[item.Token]
		if !ok {
			continue
		}
		if name := strings.TrimSpace(view.Name); name != "" {
			item.Name = name
		}
		if owner := strings.TrimSpace(view.Owner); owner != "" {
			item.Owner = owner
		}
		if visibility := strings.TrimSpace(view.Visibility); visibility != "" {
			item.Visibility = visibility
		}
		if label := strings.TrimSpace(view.HostLabel); label != "" {
			item.HostLabel = label
		}
		// The single-share GET does not carry the member flags; only the
		// remote-shares list asserts them. Treat each as assert-only so a
		// metadata refresh cannot demote the row this member published.
		if view.Mine {
			item.Mine = true
		}
		if view.Mine || view.Admin {
			item.Admin = true
		}
		if view.Host != nil {
			item.Host = *view.Host
		}
		if view.PendingHost {
			item.PendingHost = true
			item.PendingHostLabel = view.PendingHostLabel
		}
		if view.ExpiresAt != "" {
			item.ExpiresAt = strings.TrimSpace(view.ExpiresAt)
			item.Permanent = false
		} else if view.Permanent {
			item.ExpiresAt = ""
			item.Permanent = true
		}
		if view.Revision > item.Revision {
			item.Revision = view.Revision
		}
		if view.MarksRevision != nil && *view.MarksRevision > item.MarksRevision {
			item.MarksRevision = *view.MarksRevision
		}
		if view.ChatRevision != nil && *view.ChatRevision > item.ChatRevision {
			item.ChatRevision = *view.ChatRevision
		}
		if view.Notes != nil {
			notes := notesFromCompanionShareView(view)
			revision := 0
			if view.NotesRevision != nil {
				revision = *view.NotesRevision
			}
			if revision >= item.NotesRevision {
				item.Notes = notes
				item.NotesRevision = revision
				item.NotesKnown = true
			}
		}
	}
	return items
}

func fileCompanionLibraryFileExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// reconcileFileCompanionLibrary keeps rows that appeared while a cloud pull
// was in flight, and applies the pulled path and revision onto those rows.
// dropped tokens are group shares the hub no longer returns.
func reconcileFileCompanionLibrary(fresh, snapshot []FileCompanionLibraryItem, dropped map[string]bool) []FileCompanionLibraryItem {
	snap := map[string]FileCompanionLibraryItem{}
	for _, item := range snapshot {
		if item.Source == "cloud" && item.Token != "" {
			snap[item.Token] = item
		}
	}
	seen := map[string]bool{}
	out := make([]FileCompanionLibraryItem, 0, len(fresh)+len(snap))
	for _, item := range fresh {
		if item.Source == "cloud" && item.Token != "" {
			if dropped[item.Token] {
				continue
			}
			if updated, ok := snap[item.Token]; ok {
				// A sync that finished while this refresh was still downloading
				// has the newer revision. The snapshot is the revision this
				// refresh fetched, and writing it back would hide that sync.
				if item.Revision > updated.Revision {
					if strings.TrimSpace(item.Path) == "" {
						item.Path = updated.Path
					}
					if updated.NotesRevision > item.NotesRevision {
						item.Notes = updated.Notes
						item.NotesRevision = updated.NotesRevision
					}
					// The changes feed can land a newer transcript while this
					// refresh's snapshot was still the previous one. A higher
					// document revision must not throw that counter away.
					if updated.ChatRevision > item.ChatRevision {
						item.ChatRevision = updated.ChatRevision
					}
					if updated.MarksRevision > item.MarksRevision {
						item.MarksRevision = updated.MarksRevision
					}
				} else {
					keptNotes := item.Notes
					keptRev := item.NotesRevision
					keptChat := item.ChatRevision
					keptMarks := item.MarksRevision
					keptPath := strings.TrimSpace(item.Path)
					sameRevision := item.Revision == updated.Revision
					item = updated
					// A higher snapshot revision is a pull that published, so
					// its path stays. At the same revision the fresh row already
					// has the file in use, and this refresh did not replace it.
					// An empty fresh path still takes the path this refresh published.
					if sameRevision && keptPath != "" {
						item.Path = keptPath
					} else if strings.TrimSpace(item.Path) == "" {
						item.Path = keptPath
					}
					if keptRev > item.NotesRevision {
						item.Notes = keptNotes
						item.NotesRevision = keptRev
					}
					if keptChat > item.ChatRevision {
						item.ChatRevision = keptChat
					}
					if keptMarks > item.MarksRevision {
						item.MarksRevision = keptMarks
					}
				}
			}
			if seen[item.Token] {
				continue
			}
			seen[item.Token] = true
		}
		out = append(out, item)
	}
	for token, item := range snap {
		if seen[token] || dropped[token] {
			continue
		}
		out = append(out, item)
		seen[token] = true
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].OpenedAt > out[j].OpenedAt
	})
	return trimFileCompanionLibrary(out, fileCompanionLibraryCap)
}

func trimFileCompanionLibrary(items []FileCompanionLibraryItem, cap int) []FileCompanionLibraryItem {
	if cap <= 0 || len(items) <= cap {
		return items
	}
	// Local projects and cloud projects each keep their own cap. A cloud
	// refresh must not evict a local project to make room for shares.
	localN, cloudN := 0, 0
	out := make([]FileCompanionLibraryItem, 0, len(items))
	for _, item := range items {
		if item.Source == "cloud" {
			if cloudN >= cap {
				continue
			}
			cloudN++
		} else {
			if localN >= cap {
				continue
			}
			localN++
		}
		out = append(out, item)
	}
	return out
}

// fileCompanionSameLibraryPath reports that two library paths are the same
// file. Drive-letter case and slash direction do not make a second project.
// The symlink check runs only when the cleaned spellings still differ.
func fileCompanionSameLibraryPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right || strings.EqualFold(left, right) {
		return true
	}
	return fileCompanionDedupeKey(left) == fileCompanionDedupeKey(right)
}

// fileCompanionCloudOwnsPath reports that a cloud project already names this
// file. That file is not also a local project.
func fileCompanionCloudOwnsPath(items []FileCompanionLibraryItem, path string) bool {
	for _, item := range items {
		if item.Source == "cloud" && fileCompanionSameLibraryPath(item.Path, path) {
			return true
		}
	}
	return false
}

func fileCompanionLocalOwnsPath(items []FileCompanionLibraryItem, path string) bool {
	id, _ := fileCompanionLocalIdentity(items, path)
	return id != ""
}

// fileCompanionLocalIdentity is the 本机项目 row for this file. The id is what
// 文档管理 uses to show that card when a share was the only row on screen.
func fileCompanionLocalIdentity(items []FileCompanionLibraryItem, path string) (id, name string) {
	for _, item := range items {
		if item.Source == "cloud" || !fileCompanionSameLibraryPath(item.Path, path) {
			continue
		}
		id = item.ID
		if id == "" {
			id = fileCompanionLibraryLocalID(path)
		}
		name = strings.TrimSpace(item.Name)
		if name == "" {
			name = filepath.Base(path)
		}
		return id, name
	}
	return "", ""
}

// fileCompanionInstalledShareCopy reports a file downloaded for a share.
// The owner's own paper lives outside that directory and stays a local
// project once the share is gone.
func fileCompanionInstalledShareCopy(a *App, path string) bool {
	if a == nil || strings.TrimSpace(path) == "" {
		return false
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files")
	return companionFigureInsideDir(root, path)
}

// fileCompanionLibraryPathKey is the identity fold uses. A path that cannot
// be resolved still matches another spelling of the same Windows file, so a
// locked or missing file does not stay listed twice.
func fileCompanionLibraryPathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := fileCompanionCanonicalPath(path); err == nil && resolved != "" {
		return resolved
	}
	cleaned := filepath.Clean(path)
	if filepath.VolumeName(cleaned) != "" || strings.HasPrefix(cleaned, `\\`) {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

// foldFileCompanionPromotedLocals drops a local row whose file a cloud
// project already names, and collapses local rows that are the same file.
// Each path is canonicalized once. The cloud row is the project; the file
// is not moved.
func foldFileCompanionPromotedLocals(items []FileCompanionLibraryItem) ([]FileCompanionLibraryItem, bool) {
	return foldFileCompanionPromotedLocalsLive(items, nil)
}

// foldFileCompanionPromotedLocalsLive is the same fold. liveTokens limits
// which cloud rows may retire a local row. Nil means every cloud row does.
// A share the Hub list did not return must not delete the local row: the
// probe may still say the link is only missing for this refresh.
func foldFileCompanionPromotedLocalsLive(items []FileCompanionLibraryItem, liveTokens map[string]bool) ([]FileCompanionLibraryItem, bool) {
	if len(items) == 0 {
		return items, false
	}
	keys := make([]string, len(items))
	owned := map[string]bool{}
	for i, item := range items {
		keys[i] = fileCompanionLibraryPathKey(item.Path)
		if item.Source != "cloud" || keys[i] == "" {
			continue
		}
		if liveTokens != nil && !liveTokens[item.Token] {
			continue
		}
		owned[keys[i]] = true
	}
	seenLocal := map[string]bool{}
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	changed := false
	for i, item := range items {
		if item.Source != "cloud" {
			key := keys[i]
			if key != "" && (owned[key] || seenLocal[key]) {
				changed = true
				continue
			}
			if key != "" {
				seenLocal[key] = true
			}
		}
		kept = append(kept, item)
	}
	if !changed {
		return items, false
	}
	return kept, true
}

// fileCompanionOwnLocalRow is the 本机项目 row for a file whose cloud project
// is going away. exceptToken is the share being retired and does not count as
// another owner of the file. A file another live share still names, or a
// local row that is already there, gets nothing.
//
// mine says the cloud row was this account's own share. The author's paper
// outside library-files comes back unchanged. The copy a refresh downloaded
// under library-files/<token> is also the owner's document — the original
// was missing or this is a second machine — so it returns to 本机项目 marked
// KeptShareCopy instead of being cleared with the share. A participant's
// copy is not the reader's document and still gets nothing.
func fileCompanionOwnLocalRow(a *App, items []FileCompanionLibraryItem, mine bool, exceptToken, path, name string, size int64) (FileCompanionLibraryItem, bool) {
	path = strings.TrimSpace(path)
	if a == nil || !fileCompanionShareTokenMatch.MatchString(exceptToken) {
		return FileCompanionLibraryItem{}, false
	}
	kept := ""
	pathExists := fileCompanionLibraryFileExists(path)
	if pathExists && !fileCompanionInstalledShareCopy(a, path) {
		kept = path
	} else if mine {
		// The author's own file is gone or the row never had one. The share
		// sidecar may still remember the original, and the refresh may have
		// left a downloaded copy — either is the owner's document.
		remembered := strings.TrimSpace(a.readFileCompanionSharePathsLocked()[exceptToken])
		rememberedOK := remembered != "" && fileCompanionLibraryFileExists(remembered)
		switch {
		case rememberedOK && !fileCompanionInstalledShareCopy(a, remembered):
			kept = remembered
		case pathExists:
			kept = path
		case rememberedOK:
			kept = remembered
		default:
			dir := filepath.Join(a.GetDataDir(), "file-companion", "library-files", exceptToken)
			kept = fileCompanionOneInstalledShareFile(dir)
		}
	}
	if kept == "" {
		return FileCompanionLibraryItem{}, false
	}
	path = kept
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		size = info.Size()
	}
	// One canonical key for this file. Each row is compared cheaply first so
	// a library refresh does not resolve every path while holding the lock.
	cleaned := filepath.Clean(path)
	key := ""
	resolved := false
	same := func(other string) bool {
		other = strings.TrimSpace(other)
		if other == "" {
			return false
		}
		if other == path || other == cleaned {
			return true
		}
		otherClean := filepath.Clean(other)
		if otherClean == cleaned || strings.EqualFold(otherClean, cleaned) {
			return true
		}
		if !resolved {
			key = fileCompanionDedupeKey(cleaned)
			resolved = true
		}
		return key != "" && fileCompanionDedupeKey(otherClean) == key
	}
	for _, item := range items {
		if item.Source == "cloud" && item.Token == exceptToken {
			continue
		}
		if same(item.Path) {
			return FileCompanionLibraryItem{}, false
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	return FileCompanionLibraryItem{
		ID:            fileCompanionLibraryLocalID(path),
		Name:          name,
		Path:          path,
		Source:        "local",
		OpenedAt:      time.Now().UTC().Format(time.RFC3339),
		Size:          size,
		KeptShareCopy: fileCompanionInstalledShareCopy(a, path),
	}, true
}

func (a *App) fileCompanionHubLogin() (string, string, error) {
	if a == nil {
		return "", "", errors.New("请先登录 MaClaw Hub")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", "", errors.New("请先登录 MaClaw Hub")
	}
	hub := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewer := strings.TrimSpace(cfg.RemoteViewerToken)
	if hub == "" || viewer == "" {
		return "", "", errors.New("请先登录 MaClaw Hub")
	}
	return hub, viewer, nil
}

func (a *App) fileCompanionReaderLabel() string {
	if a == nil {
		return ""
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.RemoteEmail)
}

// fileCompanionHubOrigin is scheme://host[:port] for an absolute http(s) URL.
// Default ports are dropped so a pasted link and the saved Hub URL compare equal.
func fileCompanionHubOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

// fileCompanionShareOrigin reports the Hub in a pasted share link when that
// Hub is not the one this machine is logged into. A bare token, a maclaw
// address, or a link on the logged-in Hub stays local.
func fileCompanionShareOrigin(link, homeHub string) (string, bool) {
	origin := fileCompanionHubOrigin(link)
	if origin == "" {
		return "", false
	}
	home := fileCompanionHubOrigin(homeHub)
	if home == "" || origin == home {
		return "", false
	}
	return origin, true
}

func fileCompanionSameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || left.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return fileCompanionURLPort(left) == fileCompanionURLPort(right)
}

func fileCompanionURLPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}

// fileCompanionDirectClient follows a redirect only within one origin. Scheme,
// host, and port must all match so credentials never cross services or downgrade
// from HTTPS to HTTP.
func fileCompanionDirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 || len(via) >= 3 || req == nil || req.URL == nil || req.URL.User != nil {
				return http.ErrUseLastResponse
			}
			if !fileCompanionSameOrigin(via[len(via)-1].URL, req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

const fileCompanionReaderGrantHeader = "X-Companion-Reader-Grant"

func fileCompanionShareAuthorize(req *http.Request, viewer, grant string) {
	if req == nil {
		return
	}
	if strings.TrimSpace(viewer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(viewer))
	}
	if fileCompanionReaderGrantMatch.MatchString(strings.TrimSpace(grant)) {
		req.Header.Set(fileCompanionReaderGrantHeader, strings.TrimSpace(grant))
	}
}

// fetchFileCompanionShareList also reports whether the Hub records host
// replies and whether reader notes wait for host review, plus the Hub's own
// clock. An older Hub omits both flags, and the owner machine then leaves the
// discussion to its owner instead of answering every note as if new. The
// server time keeps the discussion watermark on the same clock as the note
// timestamps; comparing a Hub-created note with a locally created install time
// can skip live notes when the clocks disagree. The watermark belongs to this
// response alone; the caller records it for its own poll.
func (a *App) fetchFileCompanionShareList(hub, viewer string) ([]FileCompanionLibraryItem, bool, bool, time.Time, fileCompanionShareWatermark, error) {
	var mark fileCompanionShareWatermark
	req, err := http.NewRequest(http.MethodGet, hub+"/api/mobile/documents/shares", nil)
	if err != nil {
		return nil, false, false, time.Time{}, mark, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, false, time.Time{}, mark, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, false, time.Time{}, mark, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	if etag := strings.Trim(resp.Header.Get("ETag"), `"`); etag != "" {
		if value, convErr := strconv.ParseUint(etag, 10, 64); convErr == nil {
			mark.seq = value
		}
	}
	var payload struct {
		Mine        []fileCompanionShareView `json:"mine"`
		Received    []fileCompanionShareView `json:"received"`
		NotesReply  bool                     `json:"notes_reply"`
		NotesReview bool                     `json:"notes_review"`
		ServerTime  string                   `json:"server_time"`
		RemoteSeq   uint64                   `json:"remote_seq"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, false, false, time.Time{}, mark, errors.New("云端分享记录无法读取")
	}
	mark.remoteSeq = payload.RemoteSeq
	serverTime, _ := time.Parse(time.RFC3339, strings.TrimSpace(payload.ServerTime))
	out := make([]FileCompanionLibraryItem, 0, len(payload.Mine)+len(payload.Received))
	out = append(out, libraryItemsFromShares(payload.Mine, true, hub)...)
	out = append(out, libraryItemsFromShares(payload.Received, false, hub)...)
	for _, view := range payload.Received {
		token := strings.TrimSpace(view.Token)
		grant := strings.TrimSpace(view.ReaderGrant)
		if fileCompanionShareTokenMatch.MatchString(token) && fileCompanionReaderGrantMatch.MatchString(grant) {
			_ = a.saveFileCompanionShareGrant(token, grant)
		}
	}
	return out, payload.NotesReply, payload.NotesReview, serverTime, mark, nil
}

func libraryItemsFromShares(views []fileCompanionShareView, mine bool, hub string) []FileCompanionLibraryItem {
	out := make([]FileCompanionLibraryItem, 0, len(views))
	for _, view := range views {
		token := strings.TrimSpace(view.Token)
		if !fileCompanionShareTokenMatch.MatchString(token) || view.Expired {
			continue
		}
		mine := mine || view.Mine
		host := mine
		if view.Host != nil {
			host = *view.Host
		}
		hostLabel := strings.TrimSpace(view.HostLabel)
		if hostLabel == "" {
			hostLabel = strings.TrimSpace(view.Owner)
		}
		item := FileCompanionLibraryItem{
			ID:               token,
			Token:            token,
			Name:             strings.TrimSpace(view.Name),
			Source:           "cloud",
			OpenedAt:         strings.TrimSpace(view.CreatedAt),
			Owner:            strings.TrimSpace(view.Owner),
			ShareURL:         strings.TrimSpace(view.URL),
			ExpiresAt:        strings.TrimSpace(view.ExpiresAt),
			Visibility:       strings.TrimSpace(view.Visibility),
			Size:             view.Size,
			Permanent:        view.Permanent || strings.TrimSpace(view.ExpiresAt) == "",
			Mine:             mine,
			Admin:            view.Admin || mine,
			Host:             host,
			HostLabel:        hostLabel,
			HostKnown:        true,
			PendingHost:      view.PendingHost,
			PendingHostLabel: strings.TrimSpace(view.PendingHostLabel),
			Revision:         view.Revision,
			Target:           strings.TrimSpace(view.Target),
			GroupID:          strings.TrimSpace(view.GroupID),
			Notes:            notesFromCompanionShareView(view),
			NotesKnown:       view.Notes != nil,
		}
		if view.NotesRevision != nil {
			item.NotesRevision = *view.NotesRevision
		}
		if view.MarksRevision != nil {
			item.MarksRevision = *view.MarksRevision
		}
		if view.ChatRevision != nil {
			item.ChatRevision = *view.ChatRevision
		}
		if item.Name == "" {
			item.Name = "document"
		}
		if item.Visibility == "" {
			item.Visibility = "public"
		}
		if item.ShareURL == "" {
			item.ShareURL = strings.TrimRight(hub, "/") + "/hub/documents/shares/" + token
		}
		origin := fileCompanionHubOrigin(view.HubOrigin)
		home := fileCompanionHubOrigin(hub)
		if origin != "" && origin != home {
			item.HubOrigin = origin
			if fileCompanionHubOrigin(item.ShareURL) != origin {
				item.ShareURL = origin + "/hub/documents/shares/" + token
			}
		}
		if item.OpenedAt == "" {
			item.OpenedAt = time.Now().UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return out
}

func notesFromCompanionShareView(view fileCompanionShareView) []FileCompanionShareNote {
	if view.Notes == nil {
		return nil
	}
	out := make([]FileCompanionShareNote, 0, len(*view.Notes))
	for _, note := range *view.Notes {
		text := strings.TrimSpace(note.Text)
		id := strings.TrimSpace(note.ID)
		if id == "" || text == "" {
			continue
		}
		created := strings.TrimSpace(note.CreatedAt)
		if created == "" {
			created = strings.TrimSpace(note.Created)
		}
		out = append(out, FileCompanionShareNote{
			ID:        id,
			Author:    strings.TrimSpace(note.Author),
			Text:      text,
			CreatedAt: created,
			Host:      note.Host,
			ReplyTo:   strings.TrimSpace(note.ReplyTo),
			Decision:  strings.TrimSpace(note.Decision),
		})
	}
	return out
}

func fileCompanionShareMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Message)
}

// ShareFileCompanionDocument uploads the open paper together with its
// highlights and chat, and returns a link. days 0 is permanent. A private
// share needs a password of 4 to 64 characters. person needs an email or
// phone. group needs a group id whose membership the caller already accepted.
func (a *App) ShareFileCompanionDocument(path, visibility, password, target, highlights string, days int) (FileCompanionShareResult, error) {
	if a == nil {
		return FileCompanionShareResult{}, errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	visibility = strings.TrimSpace(visibility)
	switch visibility {
	case "public", "private", "person", "group":
	default:
		return FileCompanionShareResult{}, errors.New("请选择分享对象")
	}
	password = strings.TrimSpace(password)
	target = strings.TrimSpace(target)
	switch visibility {
	case "private":
		n := len([]rune(password))
		if n < 4 || n > 64 {
			return FileCompanionShareResult{}, errors.New("分享密码需为 4–64 个字符")
		}
	case "person":
		password = ""
		target = normalizeFileCompanionContact(target)
		if target == "" {
			return FileCompanionShareResult{}, errors.New("请填写邮箱或手机号")
		}
	case "group":
		password = ""
		if !fileCompanionGroupIDMatch.MatchString(target) {
			return FileCompanionShareResult{}, errors.New("请选择群组")
		}
	default:
		password = ""
		target = ""
	}
	if days < 0 || days > 3660 {
		return FileCompanionShareResult{}, errors.New("分享时间不正确")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return FileCompanionShareResult{}, errors.New("文件无法读取")
	}
	if info.IsDir() {
		return FileCompanionShareResult{}, errors.New("不支持分享文件夹")
	}
	if info.Size() == 0 {
		return FileCompanionShareResult{}, errors.New("文件是空的")
	}
	if info.Size() > mobileDocumentOriginalMaxBytes {
		return FileCompanionShareResult{}, errors.New("文件过大")
	}
	// The paper's own bytes name the article. The zip below also carries the
	// highlights and the chat, so hashing it would make the same paper look
	// different after every annotation change.
	contentHash, err := fileCompanionDocumentSHA256(canonical)
	if err != nil {
		return FileCompanionShareResult{}, errors.New("文件无法读取")
	}
	zipPath, err := a.writeFileCompanionShareZip(canonical, highlights)
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	defer os.Remove(zipPath)
	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return FileCompanionShareResult{}, errors.New("无法保存分享")
	}
	if zipInfo.Size() > mobileDocumentOriginalMaxBytes {
		return FileCompanionShareResult{}, errors.New("文件过大")
	}
	file, err := os.Open(zipPath)
	if err != nil {
		return FileCompanionShareResult{}, errors.New("无法保存分享")
	}
	defer file.Close()
	name := filepath.Base(canonical)
	// A group that lives on another Hub takes the same upload through the
	// relay endpoint on this Hub: it forwards the body with the stored invite
	// secret, so the home login never reaches the origin.
	endpoint := hub + "/api/mobile/documents/shares"
	if visibility == "group" {
		if group, ok, groupErr := a.findFileCompanionRemoteGroup(target); groupErr == nil && ok && group.Remote {
			endpoint = hub + "/api/mobile/documents/groups/" + target + "/remote-share"
		}
	}
	view, err := postFileCompanionShareTo(endpoint, viewer, name, visibility, password, target, days, contentHash, file)
	if err != nil {
		if adopted, handled, adoptErr := a.adoptDuplicateFileCompanionShare(canonical, err, visibility, password, target, days, zipPath, hub, viewer); handled {
			return adopted, adoptErr
		}
		return FileCompanionShareResult{}, err
	}
	if grant := strings.TrimSpace(view.ReaderGrant); fileCompanionReaderGrantMatch.MatchString(grant) {
		_ = a.saveFileCompanionShareGrant(view.Token, grant)
	}
	item := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
	if len(item) == 0 {
		return FileCompanionShareResult{}, errors.New("云端没有返回分享链接")
	}
	item[0].Path = canonical
	item[0].OpenedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.upsertFileCompanionCloud(item[0]); err != nil {
		return FileCompanionShareResult{}, errors.New("文档列表无法保存")
	}
	return FileCompanionShareResult{
		URL:        item[0].ShareURL,
		Name:       item[0].Name,
		ExpiresAt:  item[0].ExpiresAt,
		Visibility: item[0].Visibility,
		Permanent:  item[0].Permanent,
		Token:      item[0].Token,
		Revision:   item[0].Revision,
	}, nil
}

// findFileCompanionRemoteGroup returns the group row for groupID when it is
// one this member joined on another Hub. A group that is not remote, or not
// found, answers found=false.
func (a *App) findFileCompanionRemoteGroup(groupID string) (FileCompanionGroup, bool, error) {
	groups, err := a.ListFileCompanionGroups()
	if err != nil {
		return FileCompanionGroup{}, false, err
	}
	for _, group := range groups {
		if group.ID == groupID {
			return group, true, nil
		}
	}
	return FileCompanionGroup{}, false, nil
}

// adoptDuplicateFileCompanionShare recovers the case where the Hub already
// holds a live link for this paper, scope, and owner. It applies the settings
// the person just chose, pushes the current project onto that link, and puts
// the cloud row back for this local file. handled is false for any error that
// is not a duplicate-with-token, so the caller keeps the original message.
func (a *App) adoptDuplicateFileCompanionShare(canonical string, shareErr error, visibility, password, target string, days int, zipPath, hub, viewer string) (FileCompanionShareResult, bool, error) {
	var status *fileCompanionShareStatus
	if !errors.As(shareErr, &status) || status == nil || status.status != http.StatusConflict {
		return FileCompanionShareResult{}, false, nil
	}
	token := strings.TrimSpace(status.token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return FileCompanionShareResult{}, false, nil
	}
	// A share that lives on another Hub keeps being edited through that Hub:
	// the grant is the member credential, and the row below must stay foreign.
	route := FileCompanionLibraryItem{Source: "cloud", Token: token, HubOrigin: status.origin}
	grant := strings.TrimSpace(status.grant)
	if fileCompanionReaderGrantMatch.MatchString(grant) {
		_ = a.saveFileCompanionShareGrant(token, grant)
	}
	// The link exists with this paper and scope; apply the password/expiry the
	// person just selected before pushing the newer project.
	if _, err := a.updateFileCompanionShareFor(hub, viewer, route, visibility, password, target, days); err != nil {
		return FileCompanionShareResult{}, true, err
	}
	file, err := os.Open(zipPath)
	if err != nil {
		return FileCompanionShareResult{}, true, errors.New("无法保存分享")
	}
	targetHub, auth, callGrant, foreign := a.fileCompanionShareCall(hub, viewer, route)
	if foreign && callGrant == "" {
		return FileCompanionShareResult{}, true, errors.New("请重新导入分享链接")
	}
	view, syncErr := postFileCompanionShareSync(targetHub, auth, callGrant, token, filepath.Base(canonical), file)
	_ = file.Close()
	if syncErr != nil {
		return FileCompanionShareResult{}, true, syncErr
	}
	if view.Token != token {
		return FileCompanionShareResult{}, true, errors.New("云端没有返回分享链接")
	}
	item := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
	if len(item) > 0 && status.origin != "" {
		item[0].HubOrigin = status.origin
	}
	if len(item) == 0 {
		return FileCompanionShareResult{}, true, errors.New("云端没有返回分享链接")
	}
	item[0].Path = canonical
	item[0].OpenedAt = time.Now().UTC().Format(time.RFC3339)
	// Upsert also clears a dismissal left by the old local-remove behavior; a
	// live share must not stay hidden from its owner.
	if err := a.upsertFileCompanionCloud(item[0]); err != nil {
		return FileCompanionShareResult{}, true, errors.New("文档列表无法保存")
	}
	return FileCompanionShareResult{
		URL:        item[0].ShareURL,
		Name:       item[0].Name,
		ExpiresAt:  item[0].ExpiresAt,
		Visibility: item[0].Visibility,
		Permanent:  item[0].Permanent,
		Token:      item[0].Token,
		Revision:   item[0].Revision,
	}, true, nil
}

// UpdateFileCompanionShare changes who can see one of this person's shares and
// how long it lasts. The document, its highlights, and the readers' notes stay
// as they are. An empty password keeps the one already on the link, and a
// negative days keeps the expiry the link already has. The Hub revokes old
// reader credentials when a link becomes private or its password changes.
func (a *App) UpdateFileCompanionShare(token, visibility, password, target string, days int) (FileCompanionShareResult, error) {
	if a == nil {
		return FileCompanionShareResult{}, errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return FileCompanionShareResult{}, errors.New("分享链接不存在")
	}
	visibility = strings.TrimSpace(visibility)
	switch visibility {
	case "public", "private", "person", "group":
	default:
		return FileCompanionShareResult{}, errors.New("请选择分享对象")
	}
	password = strings.TrimSpace(password)
	target = strings.TrimSpace(target)
	switch visibility {
	case "private":
		if n := len([]rune(password)); n != 0 && (n < 4 || n > 64) {
			return FileCompanionShareResult{}, errors.New("分享密码需为 4–64 个字符")
		}
	case "person":
		password = ""
		target = normalizeFileCompanionContact(target)
		if target == "" {
			return FileCompanionShareResult{}, errors.New("请填写邮箱或手机号")
		}
	case "group":
		password = ""
		if !fileCompanionGroupIDMatch.MatchString(target) {
			return FileCompanionShareResult{}, errors.New("请选择群组")
		}
	default:
		password = ""
		target = ""
	}
	if days > 3660 {
		return FileCompanionShareResult{}, errors.New("分享时间不正确")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	route, _ := a.fileCompanionCloudByToken(token)
	if route.Token == "" {
		route = FileCompanionLibraryItem{Source: "cloud", Token: token}
	}
	return a.updateFileCompanionShareFor(hub, viewer, route, visibility, password, target, days)
}

// updateFileCompanionShareFor PATCHes the share on the Hub that owns it. item
// carries HubOrigin when the share lives on another Hub; the member's grant
// is the credential there, not the home login.
func (a *App) updateFileCompanionShareFor(hub, viewer string, item FileCompanionLibraryItem, visibility, password, target string, days int) (FileCompanionShareResult, error) {
	token := strings.TrimSpace(item.Token)
	if token == "" {
		return FileCompanionShareResult{}, errors.New("分享链接不存在")
	}
	targetHub, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return FileCompanionShareResult{}, errors.New("请重新导入分享链接")
	}
	body := map[string]any{
		"visibility": visibility,
		"password":   password,
		"target":     target,
	}
	// A negative days is "leave the expiry alone". Sending 0 instead would
	// make an edit that only changes the audience turn a dated link permanent.
	if days >= 0 {
		body["days"] = days
	}
	payload, _ := json.Marshal(body)
	raw, err := a.fileCompanionHubJSONAs(http.MethodPatch, targetHub+"/api/mobile/documents/shares/"+token, auth, grant, payload, foreign)
	if err != nil {
		// An old hub has no route for this at all. Its plain 404 or 405 reads
		// better when the message says what to do about it.
		return FileCompanionShareResult{}, staleHubUpdateError(err)
	}
	var view fileCompanionShareView
	if json.Unmarshal(raw, &view) != nil || view.Token != token {
		// A reply for another share would write a second row under a token the
		// caller never asked about, and the edited link would look unchanged.
		return FileCompanionShareResult{}, errors.New("云端没有返回分享信息")
	}
	result := FileCompanionShareResult{
		URL:        strings.TrimSpace(view.URL),
		Name:       strings.TrimSpace(view.Name),
		ExpiresAt:  strings.TrimSpace(view.ExpiresAt),
		Visibility: strings.TrimSpace(view.Visibility),
		Permanent:  view.Permanent || strings.TrimSpace(view.ExpiresAt) == "",
		Token:      view.Token,
		Revision:   view.Revision,
	}
	if result.URL == "" {
		result.URL = strings.TrimRight(targetHub, "/") + "/hub/documents/shares/" + token
	}
	if view.HubOrigin == "" && foreign {
		view.HubOrigin = item.HubOrigin
	}
	rows := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
	if len(rows) == 0 {
		return result, nil
	}
	// Rewrite the row this machine already has. A row deleted while the edit
	// was in flight is not brought back.
	if _, err := a.updateFileCompanionCloudRow(rows[0]); err != nil {
		return FileCompanionShareResult{}, err
	}
	return result, nil
}

// staleHubUpdateError turns the answer an old hub gives for the 修改 route into
// one message the reader can act on. Anything else — including a hub that knows
// the route and refused — keeps the message it sent.
func staleHubUpdateError(err error) error {
	var status *fileCompanionShareStatus
	if !errors.As(err, &status) || status == nil {
		return err
	}
	if strings.TrimSpace(status.message) != "" {
		return err
	}
	switch status.status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return errors.New("云端还不支持修改分享，需要更新 Hub")
	default:
		return err
	}
}

// DeleteFileCompanionShare removes one of this person's links from the hub and
// drops the cloud row here. A hub that already lost the link counts as done;
// an old hub without the DELETE route is reported so the reader knows the link
// is still live.
func (a *App) DeleteFileCompanionShare(token string) (FileCompanionProjectDelete, error) {
	if a == nil {
		return FileCompanionProjectDelete{}, errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return FileCompanionProjectDelete{}, errors.New("分享链接不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionProjectDelete{}, err
	}
	item, _ := a.fileCompanionCloudByToken(token)
	item.Token = token
	targetHub, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return FileCompanionProjectDelete{}, errors.New("请重新导入分享链接")
	}
	_, err = a.fileCompanionHubJSONAs(http.MethodDelete, targetHub+"/api/mobile/documents/shares/"+token, auth, grant, nil, foreign)
	if err != nil {
		var status *fileCompanionShareStatus
		if !errors.As(err, &status) || status == nil {
			return FileCompanionProjectDelete{}, err
		}
		switch {
		case status.status == http.StatusNotFound && strings.TrimSpace(status.message) == "":
			return FileCompanionProjectDelete{}, errors.New("云端还不支持删除分享，需要更新 Hub")
		case status.status == http.StatusMethodNotAllowed || status.status == http.StatusNotImplemented:
			return FileCompanionProjectDelete{}, errors.New("云端还不支持删除分享，需要更新 Hub")
		case status.status != http.StatusNotFound:
			return FileCompanionProjectDelete{}, err
		}
		// The hub knows the route and says the link is gone; clear the row.
	}
	// Retire the token and drop the row in one critical section. This is the
	// reader's own delete, so a refresh that already fetched the hub list
	// must not revive the link. A list that started before the delete must
	// not keep the row or publish a pull that was still in flight.
	removed := a.retireFileCompanionShareReason(token, true)
	if removed.LibraryRev == 0 {
		removed.LibraryRev = fileCompanionLibraryRev.Load()
	}
	return removed, nil
}

func fileCompanionDocumentSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func postFileCompanionShare(hub, viewer, name, visibility, password, target string, days int, contentHash string, raw io.Reader) (fileCompanionShareView, error) {
	return postFileCompanionShareTo(hub+"/api/mobile/documents/shares", viewer, name, visibility, password, target, days, contentHash, raw)
}

// postFileCompanionShareTo uploads the share multipart to one endpoint. The
// remote-group relay uses it with the group path instead of the shares path.
func postFileCompanionShareTo(endpoint, viewer, name, visibility, password, target string, days int, contentHash string, raw io.Reader) (fileCompanionShareView, error) {
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		var writeErr error
		defer func() { _ = pipeWriter.CloseWithError(writeErr) }()
		if writeErr = writer.WriteField("filename", name); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("visibility", visibility); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("password", password); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("days", strconv.Itoa(days)); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("target", target); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("content_hash", contentHash); writeErr != nil {
			return
		}
		ext := filepath.Ext(name)
		if ext == "" {
			ext = ".bin"
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(
			`form-data; name="file"; filename="%s"; filename*=UTF-8''%s`,
			"upload"+ext,
			percentEncodeRFC5987(name),
		))
		header.Set("Content-Type", "application/octet-stream")
		part, err := writer.CreatePart(header)
		if err != nil {
			writeErr = err
			return
		}
		n, err := io.Copy(part, io.LimitReader(raw, mobileDocumentOriginalMaxBytes+1))
		if err != nil {
			writeErr = err
			return
		}
		if n > mobileDocumentOriginalMaxBytes {
			writeErr = errors.New("文件过大")
			return
		}
		writeErr = writer.Close()
	}()
	req, err := http.NewRequest(http.MethodPost, endpoint, pipeReader)
	if err != nil {
		_ = pipeWriter.CloseWithError(err)
		return fileCompanionShareView{}, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fileCompanionShareView{}, errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure := &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
		var payload struct {
			Token string                  `json:"token"`
			Share *fileCompanionShareView `json:"share"`
		}
		if json.Unmarshal(body, &payload) == nil {
			failure.token = strings.TrimSpace(payload.Token)
			if payload.Share != nil {
				failure.origin = fileCompanionHubOrigin(payload.Share.HubOrigin)
				failure.grant = strings.TrimSpace(payload.Share.ReaderGrant)
			}
		}
		return fileCompanionShareView{}, failure
	}
	var view fileCompanionShareView
	if json.Unmarshal(body, &view) != nil || !fileCompanionShareTokenMatch.MatchString(view.Token) {
		return fileCompanionShareView{}, errors.New("云端没有返回分享链接")
	}
	return view, nil
}

// fileCompanionNotificationLaunch is replaced in tests so opening a share does
// not start a real companion process.
var fileCompanionNotificationLaunch = func(a *App, paths []string) error {
	return a.LaunchFileCompanion(paths)
}

// TransferFileCompanionShareHost nominates a participant as the next host.
// Only the share admin may call it; the nominee must accept before the seat
// moves.
func (a *App) TransferFileCompanionShareHost(token, contact string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return errors.New("分享链接不存在")
	}
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return errors.New("请填写参与者邮箱或手机号")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	item, _ := a.fileCompanionCloudByToken(token)
	item.Token = token
	target, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return errors.New("请重新导入分享链接")
	}
	payload, _ := json.Marshal(map[string]string{"contact": contact})
	_, err = a.fileCompanionHubJSONAs(http.MethodPost, target+"/api/mobile/documents/shares/"+token+"/host", auth, grant, payload, foreign)
	return err
}

// RevokeFileCompanionShareHost cancels a pending nomination (action "cancel")
// or takes the host seat back to the admin (action "revoke"). The two are not
// interchangeable: a cancel that arrives after the nominee accepted must not
// unseat them.
func (a *App) RevokeFileCompanionShareHost(token, action string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return errors.New("分享链接不存在")
	}
	switch strings.TrimSpace(action) {
	case "cancel", "revoke":
	default:
		return errors.New("无法管理主持人")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	item, _ := a.fileCompanionCloudByToken(token)
	item.Token = token
	target, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return errors.New("请重新导入分享链接")
	}
	_, err = a.fileCompanionHubJSONAs(http.MethodDelete, target+"/api/mobile/documents/shares/"+token+"/host?action="+action, auth, grant, nil, foreign)
	return err
}

// AcceptFileCompanionShareHost accepts a pending host transfer.
func (a *App) AcceptFileCompanionShareHost(token string) error {
	return a.decideFileCompanionShareHost(token, "accept")
}

// DeclineFileCompanionShareHost declines a pending host transfer.
func (a *App) DeclineFileCompanionShareHost(token string) error {
	return a.decideFileCompanionShareHost(token, "decline")
}

func (a *App) decideFileCompanionShareHost(token, decision string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return errors.New("分享链接不存在")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return err
	}
	item, _ := a.fileCompanionCloudByToken(token)
	item.Token = token
	target, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return errors.New("请重新导入分享链接")
	}
	_, err = a.fileCompanionHubJSONAs(http.MethodPost, target+"/api/mobile/documents/shares/"+token+"/host/"+decision, auth, grant, nil, foreign)
	return err
}

// OpenFileCompanionShareFromNotification opens one shared document from a Hub
// notification. An already imported copy is opened as-is; otherwise the share
// is downloaded first (person and group notices need no password).
// LaunchFileCompanion starts the companion from the main window, and opens
// the file in this window when the caller is already the companion.
func (a *App) OpenFileCompanionShareFromNotification(token string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return "", errors.New("分享链接不存在")
	}
	fileCompanionLibraryMu.Lock()
	items, ok := a.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		return "", errors.New("文档列表无法读取")
	}
	for _, item := range items {
		if item.Source != "cloud" || item.Token != token {
			continue
		}
		if path := strings.TrimSpace(item.Path); path != "" && fileCompanionLibraryFileExists(path) {
			if err := fileCompanionNotificationLaunch(a, []string{path}); err != nil {
				return "", err
			}
			return path, nil
		}
	}
	// A share that lives on another Hub cannot import from the home Hub at
	// all. The link carries the origin so the import talks to the right Hub
	// with the stored reader grant instead of the home login.
	link := token
	if item, ok := a.fileCompanionCloudByToken(token); ok && fileCompanionHubOrigin(item.HubOrigin) != "" {
		link = strings.TrimRight(fileCompanionHubOrigin(item.HubOrigin), "/") + "/hub/documents/shares/" + token
	} else if !ok {
		if hub, viewer, loginErr := a.fileCompanionHubLogin(); loginErr == nil {
			if remote, _, _, _, _, fetchErr := a.fetchFileCompanionShareList(hub, viewer); fetchErr == nil {
				for _, row := range remote {
					if row.Token == token && fileCompanionHubOrigin(row.HubOrigin) != "" {
						link = strings.TrimRight(fileCompanionHubOrigin(row.HubOrigin), "/") + "/hub/documents/shares/" + token
						break
					}
				}
			}
		}
	}
	imported, err := a.ImportFileCompanionShare(link, "")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(imported.Path)
	if path == "" {
		return "", errors.New("文件未导入")
	}
	if err := fileCompanionNotificationLaunch(a, []string{path}); err != nil {
		return "", err
	}
	return path, nil
}

// ImportFileCompanionShare copies a share into this library. The link may be
// the Hub page, a maclaw address with a token query, or the bare token.
func (a *App) ImportFileCompanionShare(link, password string) (FileCompanionLibraryItem, error) {
	if a == nil {
		return FileCompanionLibraryItem{}, errors.New("app unavailable")
	}
	token, err := fileCompanionShareToken(link)
	if err != nil {
		return FileCompanionLibraryItem{}, err
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionLibraryItem{}, err
	}
	origin, foreign := fileCompanionShareOrigin(link, hub)
	target := hub
	auth := viewer
	grant := ""
	body := map[string]string{"password": strings.TrimSpace(password)}
	if foreign {
		// The home session is not a login on the Hub that owns the link, and
		// sending it there would hand that Hub the reader's credential.
		target = origin
		auth = ""
		grant = a.fileCompanionShareGrant(token)
		if label := a.fileCompanionReaderLabel(); label != "" {
			body["reader_label"] = label
		}
		if grant != "" {
			body["reader_grant"] = grant
		}
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, target+"/api/mobile/documents/shares/"+token+"/import", bytes.NewReader(payload))
	if err != nil {
		return FileCompanionLibraryItem{}, err
	}
	fileCompanionShareAuthorize(req, auth, grant)
	req.Header.Set("Content-Type", "application/json")
	// The body is the project itself. Leaving Accept-Encoding unset makes the
	// transport ask for gzip, and a proxy may buffer the whole file to compress it.
	req.Header.Set("Accept-Encoding", "identity")
	client := fileCompanionDirectClient(3 * time.Minute)
	resp, err := client.Do(req)
	if err != nil {
		if foreign {
			return FileCompanionLibraryItem{}, errors.New("暂时无法连接分享所在的 Hub")
		}
		return FileCompanionLibraryItem{}, errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return FileCompanionLibraryItem{}, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	name := fileCompanionLibrarySafeName(resp.Header.Get("X-Companion-Document-Name"))
	raw, err := io.ReadAll(io.LimitReader(resp.Body, mobileDocumentOriginalMaxBytes+1))
	if err != nil {
		return FileCompanionLibraryItem{}, errors.New("文件无法读取")
	}
	if int64(len(raw)) > mobileDocumentOriginalMaxBytes {
		return FileCompanionLibraryItem{}, errors.New("文件过大")
	}
	if len(raw) == 0 {
		return FileCompanionLibraryItem{}, errors.New("文件是空的")
	}
	expires := strings.TrimSpace(resp.Header.Get("X-Companion-Document-Expires"))
	visibility := strings.TrimSpace(resp.Header.Get("X-Companion-Document-Visibility"))
	if visibility == "" {
		visibility = "public"
	}
	revision, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Companion-Document-Revision")))
	shareURL := strings.TrimSpace(link)
	if !strings.HasPrefix(strings.ToLower(shareURL), "http://") && !strings.HasPrefix(strings.ToLower(shareURL), "https://") {
		shareURL = hub + "/hub/documents/shares/" + token
	}
	if fresh := strings.TrimSpace(resp.Header.Get(fileCompanionReaderGrantHeader)); fileCompanionReaderGrantMatch.MatchString(fresh) {
		grant = fresh
	}
	if foreign && grant != "" {
		if err := a.saveFileCompanionShareGrant(token, grant); err != nil {
			return FileCompanionLibraryItem{}, errors.New("文档列表无法保存")
		}
	}
	item := FileCompanionLibraryItem{
		ID:         token,
		Token:      token,
		Name:       name,
		Source:     "cloud",
		OpenedAt:   time.Now().UTC().Format(time.RFC3339),
		Owner:      strings.TrimSpace(resp.Header.Get("X-Companion-Document-Owner")),
		ShareURL:   shareURL,
		HubOrigin:  origin,
		ExpiresAt:  expires,
		Visibility: visibility,
		Size:       int64(len(raw)),
		Permanent:  expires == "",
		Mine:       false,
		Revision:   revision,
	}
	// Stage outside the lock. The claim publishes the file and the library row
	// together, so a refresh cannot sample the new generation and then replace
	// this import.
	stage, err := a.stageFileCompanionShare(token, name, raw)
	if err != nil {
		return FileCompanionLibraryItem{}, err
	}
	installed, err := a.commitStagedFileCompanionShare(stage, 0, true, &item)
	if err != nil {
		if installed.path != "" {
			return FileCompanionLibraryItem{}, errors.New("文档列表无法保存")
		}
		return FileCompanionLibraryItem{}, err
	}
	// This account just read the project. A password refusal remembered for
	// an earlier automatic install must not keep the row unimported after the
	// file is removed.
	fileCompanionShareUnblockPull(token)
	return item, nil
}

// updateFileCompanionCloudRow rewrites one existing cloud row in place and
// leaves everything else alone. A row that is gone stays gone: an edit that
// finishes after the reader deleted the project must not bring it back.
func (a *App) updateFileCompanionCloudRow(item FileCompanionLibraryItem) (bool, error) {
	if a == nil || item.Token == "" {
		return false, nil
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return false, errors.New("文档列表无法读取")
	}
	found := false
	for index, current := range items {
		if current.Source != "cloud" || current.Token != item.Token {
			continue
		}
		found = true
		// The row keeps the file this machine reads and the notes a reader
		// posted while the edit was in flight.
		item.Path = current.Path
		item.OpenedAt = current.OpenedAt
		if item.OpenedAt == "" {
			item.OpenedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if current.NotesRevision > item.NotesRevision {
			item.Notes = current.Notes
			item.NotesRevision = current.NotesRevision
		}
		items[index] = item
		break
	}
	if !found {
		return false, nil
	}
	if err := a.writeFileCompanionLibraryLocked(items); err != nil {
		return false, errors.New("文档列表无法保存")
	}
	return true, nil
}

func (a *App) upsertFileCompanionCloud(item FileCompanionLibraryItem) error {
	if a == nil || item.Token == "" {
		return nil
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	return a.upsertFileCompanionCloudLocked(item)
}

func (a *App) upsertFileCompanionCloudLocked(item FileCompanionLibraryItem) error {
	if a == nil || item.Token == "" {
		return nil
	}
	// A share the reader imports or publishes again is a project they want
	// back in 文档管理. Clear the deletion before the row is written: a row
	// stored while the token is still dismissed is dropped by the next list.
	if err := a.undismissFileCompanionShareLocked(item.Token); err != nil {
		return err
	}
	if strings.TrimSpace(item.Path) != "" {
		a.rememberFileCompanionSharePathLocked(item.Token, item.Path)
	}
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return errors.New("文档列表无法读取")
	}
	kept := make([]FileCompanionLibraryItem, 0, len(items)+1)
	for _, current := range items {
		if current.Source == "cloud" && current.Token == item.Token {
			// Sync and import replace the row with the snapshot taken before
			// the upload. A note that landed while that request was in flight
			// has the newer revision and has to stay.
			if current.Mine && !item.Mine {
				// Ownership is monotone: the share's owner never changes, but
				// a re-import response cannot say this row is the member's own
				// remote-published share. Keep the flag or the row loses its
				// host controls until the next list refresh restores them.
				item.Mine = true
				item.Admin = true
			}
			if current.NotesRevision > item.NotesRevision {
				item.Notes = current.Notes
				item.NotesRevision = current.NotesRevision
			}
			if current.MarksRevision > item.MarksRevision {
				item.MarksRevision = current.MarksRevision
			}
			if current.ChatRevision > item.ChatRevision {
				item.ChatRevision = current.ChatRevision
			}
			continue
		}
		kept = append(kept, current)
	}
	kept = append([]FileCompanionLibraryItem{item}, kept...)
	// The share is the project now. The local row for the same file is retired
	// with this write, so a cloud refresh cannot treat it as a second project.
	kept, _ = foldFileCompanionPromotedLocals(kept)
	return a.writeFileCompanionLibraryLocked(trimFileCompanionLibrary(kept, fileCompanionLibraryCap))
}

// retireFileCompanionShare removes a cloud row whose link is gone for good and
// marks the token dismissed under one lock. The dismissal is what stops an
// in-flight list from keeping the row and an in-flight pull from publishing it
// again; the token itself is dead, so the marker costs nothing after that.
// A probe dismissal can return when a hub list still names the share. The
// reader's own 取消分享 uses retireFileCompanionShareReason with byUser set,
// so that stale list cannot put the link back.
func (a *App) retireFileCompanionShare(token string) {
	a.retireFileCompanionShareReason(token, false)
}

func (a *App) retireFileCompanionShareReason(token string, byUser bool) FileCompanionProjectDelete {
	result := FileCompanionProjectDelete{ID: token}
	if a == nil || token == "" {
		return result
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	_, _ = a.dismissFileCompanionShareReasonLocked(token, byUser)
	a.forgetFileCompanionShareGrantLocked(token)
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		result.LibraryRev = fileCompanionLibraryRev.Load()
		return result
	}
	// Drop the cloud row and, when this was the last share of the owner's own
	// file, put that file back in one write. Two writes could lose the file
	// from the library if the second one failed.
	var ownPath, ownName string
	var ownSize int64
	found := false
	mine := false
	kept := make([]FileCompanionLibraryItem, 0, len(items)+1)
	for _, item := range items {
		if item.Source == "cloud" && item.Token == token {
			ownPath, ownName, ownSize, mine = item.Path, item.Name, item.Size, item.Mine
			found = true
			continue
		}
		kept = append(kept, item)
	}
	var restoredID, restoredPath, restoredName string
	var restoredSize int64
	if row, ok := fileCompanionOwnLocalRow(a, kept, mine, token, ownPath, ownName, ownSize); ok {
		kept = append([]FileCompanionLibraryItem{row}, kept...)
		kept = trimFileCompanionLibrary(kept, fileCompanionLibraryCap)
		found = true
		restoredID, restoredPath, restoredName, restoredSize = row.ID, row.Path, row.Name, row.Size
	} else if found && ownPath != "" && !fileCompanionCloudOwnsPath(kept, ownPath) {
		if id, name := fileCompanionLocalIdentity(kept, ownPath); id != "" {
			restoredID, restoredPath, restoredName, restoredSize = id, ownPath, name, ownSize
		}
	}
	if found {
		if err := a.writeFileCompanionLibraryLocked(kept); err != nil {
			restoredID = ""
		}
	}
	result.Path = restoredPath
	result.Name = restoredName
	result.LocalID = restoredID
	result.Size = restoredSize
	result.Restored = restoredID != ""
	result.LibraryRev = fileCompanionLibraryRev.Load()
	return result
}

// FileCompanionProjectDelete is what 文档管理 removed. Path is the document
// that stayed on disk. Cleared is set when this row was the last project
// using that path, so the window drops highlights and closes the tab.
// Promoted is set when the local row was already retired because that file
// is a cloud project; the cloud project stays. Restored is set when removing
// the share put the owner's own file back in 本机项目. LocalID is that row, so
// the list can show it while a refresh is still copying. Reading data stays
// until that local row is deleted.
type FileCompanionProjectDelete struct {
	ID         string `json:"id"`
	Path       string `json:"path,omitempty"`
	Name       string `json:"name,omitempty"`
	LocalID    string `json:"localId,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Cleared    bool   `json:"cleared"`
	Promoted   bool   `json:"promoted,omitempty"`
	Restored   bool   `json:"restored,omitempty"`
	LibraryRev uint64 `json:"libraryRev,omitempty"`
}

// DeleteFileCompanionProject removes one 文档管理 row and the companion data
// that belongs only to it: the reading record, highlights on disk, discussion,
// and generated notes. The source file is not removed.
//
// id is the row id or a cloud token. path is the file the screen is showing,
// which is the local project's identity when the row id was already retired.
// A local id whose file is now a cloud project does not delete that cloud
// project. A row that is already gone is the deleted state.
func (a *App) DeleteFileCompanionProject(id, path string) (FileCompanionProjectDelete, error) {
	empty := FileCompanionProjectDelete{}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	id = strings.TrimSpace(id)
	path = strings.TrimSpace(path)
	if id == "" && path == "" {
		return empty, errors.New("没有找到这个项目")
	}
	fileCompanionLibraryMu.Lock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		fileCompanionLibraryMu.Unlock()
		return empty, errors.New("文档列表无法读取")
	}
	// One pass. The id wins. The path is only the local project's identity
	// when that id is already gone, and it is canonicalized once.
	pathKey := ""
	shareID := fileCompanionShareTokenMatch.MatchString(id)
	if path != "" && !shareID {
		pathKey = fileCompanionDedupeKey(path)
	}
	foundIndex := -1
	pathIndex := -1
	for i := range items {
		if foundIndex < 0 && fileCompanionLibraryItemMatches(items[i], id) {
			foundIndex = i
			continue
		}
		if pathIndex < 0 && foundIndex < 0 && pathKey != "" && items[i].Source != "cloud" && fileCompanionDedupeKey(items[i].Path) == pathKey {
			pathIndex = i
		}
	}
	// A share id that is already gone must not delete the local project the
	// file just returned to. The path is only a fallback for a local id.
	if foundIndex < 0 {
		foundIndex = pathIndex
	}
	var found *FileCompanionLibraryItem
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	if foundIndex >= 0 {
		copy := items[foundIndex]
		found = &copy
		for i := range items {
			if i == foundIndex {
				continue
			}
			kept = append(kept, items[i])
		}
	}
	if found == nil {
		// The local project was retired when the file became a cloud project.
		// Leave the cloud row and its reading data in place.
		if path != "" && fileCompanionCloudOwnsPath(items, path) {
			rev := fileCompanionLibraryRev.Load()
			fileCompanionLibraryMu.Unlock()
			return FileCompanionProjectDelete{ID: id, Path: path, Promoted: true, LibraryRev: rev}, nil
		}
		// The share was already removed and the file is a local project.
		// Leave that row and its reading data in place.
		if path != "" && fileCompanionShareTokenMatch.MatchString(id) {
			localID, name := fileCompanionLocalIdentity(items, path)
			if localID != "" {
				rev := fileCompanionLibraryRev.Load()
				fileCompanionLibraryMu.Unlock()
				return FileCompanionProjectDelete{ID: id, Path: path, Name: name, LocalID: localID, Restored: true, LibraryRev: rev}, nil
			}
		}
		// The row is already gone. Finish removing companion data when no
		// project still uses the file, so a stale card can be deleted.
		cleared := path != "" && !fileCompanionLibraryUsesPath(items, path)
		if cleared {
			a.purgeFileCompanionProjectData(path)
		}
		rev := fileCompanionLibraryRev.Load()
		fileCompanionLibraryMu.Unlock()
		return FileCompanionProjectDelete{ID: id, Path: path, Cleared: cleared, LibraryRev: rev}, nil
	}
	// Hide a cloud share before the row is removed. A failed hide leaves the
	// row in place: the next list would otherwise download it again.
	hidShare := false
	if found.Source == "cloud" && fileCompanionShareTokenMatch.MatchString(found.Token) {
		added, err := a.dismissFileCompanionShareReasonLocked(found.Token, true)
		if err != nil {
			fileCompanionLibraryMu.Unlock()
			return empty, errors.New("文档列表无法保存")
		}
		hidShare = added
	}
	if foundPath := strings.TrimSpace(found.Path); foundPath != "" {
		path = foundPath
	}
	// Removing the share is not deleting the document. The owner's own file
	// goes back to 本机项目 in the same write, and its highlights stay. A
	// downloaded copy is not adopted. Another live share of the same file
	// keeps it a cloud project.
	restored := false
	localID := ""
	localSize := int64(0)
	if found.Source == "cloud" && found.Mine {
		if row, ok := fileCompanionOwnLocalRow(a, kept, true, found.Token, path, found.Name, found.Size); ok {
			kept = append([]FileCompanionLibraryItem{row}, kept...)
			kept = trimFileCompanionLibrary(kept, fileCompanionLibraryCap)
			restored = true
			localID = row.ID
			localSize = row.Size
		} else if path != "" && !fileCompanionCloudOwnsPath(kept, path) {
			// The screen hid this row while the share existed. It is already
			// the local project once that share is gone.
			if id, _ := fileCompanionLocalIdentity(kept, path); id != "" {
				restored = true
				localID = id
				localSize = found.Size
			}
		}
	}
	if err := a.writeFileCompanionLibraryLocked(kept); err != nil {
		if hidShare {
			_ = a.undismissFileCompanionShareLocked(found.Token)
		}
		fileCompanionLibraryMu.Unlock()
		return empty, errors.New("文档列表无法保存")
	}
	if found.Source == "cloud" && fileCompanionShareTokenMatch.MatchString(found.Token) {
		a.forgetFileCompanionSharePathLocked(found.Token)
	}
	sharedPath := path != "" && fileCompanionLibraryUsesPath(kept, path)
	result := FileCompanionProjectDelete{
		ID:         found.ID,
		Path:       path,
		Name:       found.Name,
		LocalID:    localID,
		Size:       localSize,
		Cleared:    path != "" && !sharedPath,
		Restored:   restored && localID != "",
		LibraryRev: fileCompanionLibraryRev.Load(),
	}
	// Remove the transcript before releasing the lock. Import and a share
	// pull publish that transcript under the same lock. Unlocking first lets
	// the publish land, and this purge then deletes the record just imported.
	if result.Cleared {
		a.purgeFileCompanionProjectData(path)
	}
	fileCompanionLibraryMu.Unlock()
	return result, nil
}

func fileCompanionLibraryItemMatches(item FileCompanionLibraryItem, id string) bool {
	if id == "" {
		return false
	}
	if item.ID == id {
		return true
	}
	return item.Source == "cloud" && item.Token != "" && item.Token == id
}

func (a *App) fileCompanionShareGrantPath() string {
	if a == nil {
		return ""
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return ""
	}
	return filepath.Join(root, "file-companion", "share-grants.json")
}

func (a *App) readFileCompanionShareGrantsLocked() map[string]string {
	out := map[string]string{}
	path := a.fileCompanionShareGrantPath()
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var stored map[string]string
	if json.Unmarshal(raw, &stored) != nil {
		return out
	}
	for token, grant := range stored {
		token = strings.TrimSpace(token)
		grant = strings.TrimSpace(grant)
		if fileCompanionShareTokenMatch.MatchString(token) && fileCompanionReaderGrantMatch.MatchString(grant) {
			out[token] = grant
		}
	}
	return out
}

func (a *App) writeFileCompanionShareGrantsLocked(grants map[string]string) error {
	path := a.fileCompanionShareGrantPath()
	if path == "" {
		return errors.New("library path is empty")
	}
	body, err := json.Marshal(grants)
	if err != nil {
		return err
	}
	return fileCompanionWriteAtomicFile(path, body, 0o600)
}

func (a *App) fileCompanionShareGrant(token string) string {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) {
		return ""
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	return a.readFileCompanionShareGrantsLocked()[token]
}

func (a *App) saveFileCompanionShareGrant(token, grant string) error {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) || !fileCompanionReaderGrantMatch.MatchString(grant) {
		return nil
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	grants := a.readFileCompanionShareGrantsLocked()
	if grants[token] == grant {
		return nil
	}
	grants[token] = grant
	return a.writeFileCompanionShareGrantsLocked(grants)
}

func (a *App) forgetFileCompanionShareGrantLocked(token string) {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) {
		return
	}
	grants := a.readFileCompanionShareGrantsLocked()
	if _, ok := grants[token]; !ok {
		return
	}
	delete(grants, token)
	_ = a.writeFileCompanionShareGrantsLocked(grants)
}

func (a *App) fileCompanionDismissedSharePath() string {
	if a == nil {
		return ""
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return ""
	}
	return filepath.Join(root, "file-companion", "dismissed-shares.json")
}

// fileCompanionDismissedShares separates a reader who removed the project in
// 文档管理 from a probe that decided the link was gone. The probe can be wrong
// for one refresh; the reader's delete must stay deleted when the Hub list
// still includes the share.
type fileCompanionDismissedShares struct {
	Tokens []string `json:"tokens"`
	User   []string `json:"user,omitempty"`
}

func (a *App) readDismissedSharesLocked() (map[string]bool, map[string]bool) {
	tokens := map[string]bool{}
	user := map[string]bool{}
	path := a.fileCompanionDismissedSharePath()
	if path == "" {
		return tokens, user
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return tokens, user
	}
	var stored fileCompanionDismissedShares
	if json.Unmarshal(raw, &stored) != nil {
		return tokens, user
	}
	for _, token := range stored.Tokens {
		token = strings.TrimSpace(token)
		if fileCompanionShareTokenMatch.MatchString(token) {
			tokens[token] = true
		}
	}
	for _, token := range stored.User {
		token = strings.TrimSpace(token)
		if fileCompanionShareTokenMatch.MatchString(token) {
			user[token] = true
			tokens[token] = true
		}
	}
	return tokens, user
}

func (a *App) readDismissedShareTokensLocked() map[string]bool {
	tokens, _ := a.readDismissedSharesLocked()
	return tokens
}

func (a *App) writeDismissedSharesLocked(tokens, user map[string]bool) error {
	path := a.fileCompanionDismissedSharePath()
	if path == "" {
		return errors.New("library path is empty")
	}
	list := make([]string, 0, len(tokens))
	for token := range tokens {
		if fileCompanionShareTokenMatch.MatchString(token) {
			list = append(list, token)
		}
	}
	sort.Strings(list)
	userList := make([]string, 0, len(user))
	for token := range user {
		if tokens[token] && fileCompanionShareTokenMatch.MatchString(token) {
			userList = append(userList, token)
		}
	}
	sort.Strings(userList)
	body, err := json.Marshal(fileCompanionDismissedShares{Tokens: list, User: userList})
	if err != nil {
		return err
	}
	return fileCompanionWriteAtomicFile(path, body, 0o600)
}

func (a *App) dismissFileCompanionShareLocked(token string) (bool, error) {
	return a.dismissFileCompanionShareReasonLocked(token, false)
}

func (a *App) dismissFileCompanionShareReasonLocked(token string, byUser bool) (bool, error) {
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return false, nil
	}
	tokens, user := a.readDismissedSharesLocked()
	changed := false
	if !tokens[token] {
		tokens[token] = true
		changed = true
	}
	if byUser && !user[token] {
		user[token] = true
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := a.writeDismissedSharesLocked(tokens, user); err != nil {
		return false, err
	}
	a.bumpFileCompanionShareEpochLocked(token)
	return true, nil
}

func (a *App) undismissFileCompanionShareLocked(token string) error {
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return nil
	}
	tokens, user := a.readDismissedSharesLocked()
	if !tokens[token] && !user[token] {
		return nil
	}
	delete(tokens, token)
	delete(user, token)
	if err := a.writeDismissedSharesLocked(tokens, user); err != nil {
		return err
	}
	a.bumpFileCompanionShareEpochLocked(token)
	return nil
}

func (a *App) fileCompanionSharePathsPath() string {
	if a == nil {
		return ""
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return ""
	}
	return filepath.Join(root, "file-companion", "share-paths.json")
}

func (a *App) readFileCompanionSharePathsLocked() map[string]string {
	out := map[string]string{}
	path := a.fileCompanionSharePathsPath()
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var stored struct {
		Paths map[string]string `json:"paths"`
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Paths == nil {
		return out
	}
	for token, filePath := range stored.Paths {
		token = strings.TrimSpace(token)
		filePath = strings.TrimSpace(filePath)
		if fileCompanionShareTokenMatch.MatchString(token) && filePath != "" {
			out[token] = filePath
		}
	}
	return out
}

func (a *App) writeFileCompanionSharePathsLocked(paths map[string]string) error {
	path := a.fileCompanionSharePathsPath()
	if path == "" {
		return errors.New("library path is empty")
	}
	if paths == nil {
		paths = map[string]string{}
	}
	body, err := json.Marshal(struct {
		Paths map[string]string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		return err
	}
	return fileCompanionWriteAtomicFile(path, body, 0o600)
}

func (a *App) rememberFileCompanionSharePathLocked(token, path string) {
	path = strings.TrimSpace(path)
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) || !fileCompanionLibraryFileExists(path) {
		return
	}
	if identity, err := fileCompanionCanonicalPath(path); err == nil && identity != "" {
		path = identity
	}
	a.rememberFileCompanionSharePathsLocked(map[string]string{token: path})
}

func (a *App) rememberFileCompanionSharePathsLocked(extra map[string]string) {
	if a == nil || len(extra) == 0 {
		return
	}
	paths := a.readFileCompanionSharePathsLocked()
	changed := false
	for token, path := range extra {
		token = strings.TrimSpace(token)
		path = strings.TrimSpace(path)
		if !fileCompanionShareTokenMatch.MatchString(token) || !fileCompanionLibraryFileExists(path) {
			continue
		}
		if identity, err := fileCompanionCanonicalPath(path); err == nil && identity != "" {
			path = identity
		}
		if paths[token] == path {
			continue
		}
		paths[token] = path
		changed = true
	}
	if changed {
		_ = a.writeFileCompanionSharePathsLocked(paths)
	}
}

func (a *App) forgetFileCompanionSharePathLocked(token string) {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) {
		return
	}
	paths := a.readFileCompanionSharePathsLocked()
	if _, ok := paths[token]; !ok {
		return
	}
	delete(paths, token)
	_ = a.writeFileCompanionSharePathsLocked(paths)
}

// fileCompanionKnownSharePathsLocked is the token-to-file map for this
// refresh. A path already stored on the cloud row or in the sidecar is
// enough. The library-files walk runs only when a listed or probe-dismissed
// share still has a download directory and no known file.
func (a *App) fileCompanionKnownSharePathsLocked(items, remote []FileCompanionLibraryItem, dismissed, user map[string]bool) map[string]string {
	known := map[string]string{}
	if a == nil {
		return known
	}
	for token, path := range a.readFileCompanionSharePathsLocked() {
		if fileCompanionLibraryFileExists(path) {
			known[token] = path
		}
	}
	for _, item := range items {
		if item.Source != "cloud" || !fileCompanionShareTokenMatch.MatchString(item.Token) {
			continue
		}
		if fileCompanionLibraryFileExists(item.Path) {
			known[item.Token] = item.Path
		}
	}
	if !a.fileCompanionShareIndexNeededLocked(remote, dismissed, user, known) {
		return known
	}
	for token, path := range a.fileCompanionSharePathIndexLocked() {
		if path == "" || fileCompanionLibraryFileExists(known[token]) {
			continue
		}
		known[token] = path
	}
	return known
}

// fileCompanionShareIndexNeededLocked reports that some share can only be
// found by reading library-files/<token>. The caller holds fileCompanionLibraryMu.
func (a *App) fileCompanionShareIndexNeededLocked(remote []FileCompanionLibraryItem, dismissed, user map[string]bool, known map[string]string) bool {
	if a == nil {
		return false
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files")
	missingDir := func(token string) bool {
		if user[token] || !fileCompanionShareTokenMatch.MatchString(token) || fileCompanionLibraryFileExists(known[token]) {
			return false
		}
		info, err := os.Stat(filepath.Join(root, token))
		return err == nil && info.IsDir()
	}
	for _, item := range remote {
		if missingDir(strings.TrimSpace(item.Token)) {
			return true
		}
	}
	for token := range dismissed {
		if missingDir(token) {
			return true
		}
	}
	return false
}

// fileCompanionSharePathIndexLocked is every token whose file is still on
// disk: the remembered path, or the single copy under library-files/<token>.
// The caller holds fileCompanionLibraryMu.
func (a *App) fileCompanionSharePathIndexLocked() map[string]string {
	out := map[string]string{}
	if a == nil {
		return out
	}
	for token, path := range a.readFileCompanionSharePathsLocked() {
		if fileCompanionLibraryFileExists(path) {
			out[token] = path
		}
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files")
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		token := entry.Name()
		if !entry.IsDir() || !fileCompanionShareTokenMatch.MatchString(token) {
			continue
		}
		if fileCompanionLibraryFileExists(out[token]) {
			continue
		}
		if path := fileCompanionOneInstalledShareFile(filepath.Join(root, token)); path != "" {
			out[token] = path
		}
	}
	return out
}

// fileCompanionOneInstalledShareFile is the downloaded copy in one share
// directory. Two documents is ambiguous, so the caller keeps looking at the
// remembered path instead of guessing.
func fileCompanionOneInstalledShareFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	found := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".bak") {
			continue
		}
		path := filepath.Join(dir, name)
		if !fileCompanionLibraryFileExists(path) {
			continue
		}
		if found != "" {
			return ""
		}
		found = path
	}
	if found == "" {
		return ""
	}
	if identity, err := fileCompanionCanonicalPath(found); err == nil && identity != "" {
		return identity
	}
	return found
}

// reviveProbeDismissedSharesLocked puts a live share back when this machine
// still has its file. A token the reader removed in 文档管理 stays dismissed.
// The caller holds fileCompanionLibraryMu. The returned map is the dismissal
// set the merge should use.
func (a *App) reviveProbeDismissedSharesLocked(remote []FileCompanionLibraryItem, dismissed, user map[string]bool, known map[string]string) map[string]bool {
	if a == nil || len(dismissed) == 0 {
		return dismissed
	}
	for _, item := range remote {
		token := strings.TrimSpace(item.Token)
		if !dismissed[token] || user[token] || !fileCompanionShareTokenMatch.MatchString(token) {
			continue
		}
		if !fileCompanionLibraryFileExists(known[token]) {
			continue
		}
		if err := a.undismissFileCompanionShareLocked(token); err != nil {
			continue
		}
		delete(dismissed, token)
	}
	return dismissed
}

// bumpFileCompanionShareEpochLocked invalidates a pull that already captured
// the previous generation. The caller holds fileCompanionLibraryMu.
func (a *App) bumpFileCompanionShareEpochLocked(token string) {
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return
	}
	fileCompanionShareEpoch[token]++
}

func (a *App) bumpFileCompanionShareEpoch(token string) {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) {
		return
	}
	fileCompanionLibraryMu.Lock()
	a.bumpFileCompanionShareEpochLocked(token)
	fileCompanionLibraryMu.Unlock()
}

// fileCompanionSharePullPermit is the generation a download must still match
// when it publishes, and whether that download should not start. Hidden
// shares are skipped. A row that already has this revision on disk is skipped
// too, so a pull cannot replace an import that landed before this token was
// reached. The caller must not hold fileCompanionLibraryMu.
func (a *App) fileCompanionSharePullPermit(item FileCompanionLibraryItem) (uint64, bool) {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(item.Token) {
		return 0, true
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	if a.readDismissedShareTokensLocked()[item.Token] {
		return 0, true
	}
	epoch := fileCompanionShareEpoch[item.Token]
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return epoch, false
	}
	for _, row := range items {
		if row.Source != "cloud" || row.Token != item.Token {
			continue
		}
		if row.Revision >= item.Revision && fileCompanionLibraryFileExists(row.Path) {
			return epoch, true
		}
		break
	}
	return epoch, false
}

// withoutDismissedShareUpdates drops downloads whose share was deleted before
// the refresh finished. The bytes stay where the commit left them: a pull that
// lost the race never published, and Delete purges when this row was the last
// user of the path.
func withoutDismissedShareUpdates(updates []FileCompanionShareUpdate, dismissed map[string]bool) []FileCompanionShareUpdate {
	if len(updates) == 0 || len(dismissed) == 0 {
		return updates
	}
	kept := make([]FileCompanionShareUpdate, 0, len(updates))
	for _, update := range updates {
		token := strings.TrimSpace(update.Token)
		if token != "" && dismissed[token] {
			continue
		}
		kept = append(kept, update)
	}
	return kept
}

// recordFileCompanionSharePathLocked stores the file a pull just published.
// The caller holds fileCompanionLibraryMu. A missing row is left alone: a new
// share is not in the library until the refresh finishes, and Delete cannot
// see it yet. The same path is not written again.
func (a *App) recordFileCompanionSharePathLocked(token, path string) error {
	path = strings.TrimSpace(path)
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) || path == "" {
		return nil
	}
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return errors.New("文档列表无法读取")
	}
	index := -1
	for i := range items {
		if items[i].Source == "cloud" && items[i].Token == token {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	previous := strings.TrimSpace(items[index].Path)
	if previous == path || (previous != "" && fileCompanionDedupeKey(previous) == fileCompanionDedupeKey(path)) {
		a.rememberFileCompanionSharePathLocked(token, previous)
		return nil
	}
	items[index].Path = path
	items, _ = foldFileCompanionPromotedLocals(items)
	if err := a.writeFileCompanionLibraryLocked(items); err != nil {
		return err
	}
	a.rememberFileCompanionSharePathLocked(token, path)
	// The row now names the new copy. A previous copy inside this share's
	// directory has its own transcript. Nothing else is using it, so leaving
	// it behind would survive a later delete of the new file.
	if previous == "" || fileCompanionLibraryUsesPath(items, previous) {
		return nil
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files", token)
	if !companionFigureInsideDir(root, previous) {
		return nil
	}
	a.purgeFileCompanionProjectData(previous)
	return nil
}

// omitOpenedShareCopies drops the local row that opening a received copy
// writes beside the cloud row. That row is the participant's open tab.
// Paths this refresh is not closing stay.
func omitOpenedShareCopies(items []FileCompanionLibraryItem, removed []string) []FileCompanionLibraryItem {
	if len(removed) == 0 {
		return items
	}
	gone := map[string]bool{}
	for _, path := range removed {
		if key := fileCompanionDedupeKey(path); key != "" {
			gone[key] = true
		}
	}
	if len(gone) == 0 {
		return items
	}
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	for _, item := range items {
		if item.Source == "local" {
			if key := fileCompanionDedupeKey(item.Path); key != "" && gone[key] {
				continue
			}
		}
		kept = append(kept, item)
	}
	return kept
}

func fileCompanionLibraryUsesPath(items []FileCompanionLibraryItem, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	// Cleaned spellings match without asking the filesystem. Symlink
	// resolution runs only for a row that still looks different, and the
	// query path is resolved once. Callers hold the library lock.
	cleaned := filepath.Clean(path)
	var key string
	resolved := false
	for _, item := range items {
		other := strings.TrimSpace(item.Path)
		if other == "" {
			continue
		}
		if other == path || other == cleaned {
			return true
		}
		otherClean := filepath.Clean(other)
		if otherClean == cleaned || strings.EqualFold(otherClean, cleaned) {
			return true
		}
		if !resolved {
			key = fileCompanionDedupeKey(cleaned)
			resolved = true
		}
		if key != "" && fileCompanionDedupeKey(otherClean) == key {
			return true
		}
	}
	return false
}

// fileCompanionProjectPurgeHook runs while fileCompanionLibraryMu is held,
// before the transcript is removed. Tests block here to prove a publish
// cannot install a replacement until this purge finishes.
var fileCompanionProjectPurgeHook func()

// purgeFileCompanionProjectData drops the transcript, drawings, and generated
// notes for one document. It does not remove the document itself, or a copy
// stored under library-files. The caller holds fileCompanionLibraryMu.
func (a *App) purgeFileCompanionProjectData(path string) {
	if a == nil {
		return
	}
	identity, err := fileCompanionCanonicalPath(path)
	if err != nil || strings.TrimSpace(identity) == "" {
		return
	}
	sessionID := fileCompanionSessionID(identity)
	if !fileCompanionSessionDirMatch.MatchString(sessionID) {
		return
	}
	if fileCompanionProjectPurgeHook != nil {
		fileCompanionProjectPurgeHook()
	}
	userID := "file-companion:" + sessionID
	a.fileCompanionCancelNote(sessionID)
	if a.isFileCompanionProcess() && a.imHandler != nil {
		_, _ = a.imHandler.RequestCancelSessionForUser(userID)
		side := filepath.Join(a.GetDataDir(), "file-companion", "sessions", sessionID+".json")
		_ = a.fileCompanionDropChat(userID, side)
	} else {
		a.removeFileCompanionSessionFile(sessionID)
	}
	a.removeFileCompanionSessionTree("figures", sessionID, path)
	a.removeFileCompanionSessionTree("results", sessionID, path)
	a.removeFileCompanionSessionTree("work", sessionID, path)
	_ = a.deleteCompanionReaderState(companionReaderDiscussionNotesKind, companionReaderPathKey(identity))
	logFileCompanion("project data cleared session=%s", sessionID)
}

func (a *App) removeFileCompanionSessionFile(sessionID string) {
	if a == nil || !fileCompanionSessionDirMatch.MatchString(sessionID) {
		return
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return
	}
	dir := filepath.Join(root, "file-companion", "sessions")
	side := filepath.Join(dir, sessionID+".json")
	if !companionFigureInsideDir(dir, side) {
		return
	}
	if err := os.Remove(side); err != nil && !os.IsNotExist(err) {
		logFileCompanion("project session remove failed session=%s", sessionID)
	}
}

func (a *App) removeFileCompanionSessionTree(kind, sessionID, source string) {
	if a == nil || !fileCompanionSessionDirMatch.MatchString(sessionID) {
		return
	}
	switch kind {
	case "figures", "results", "work":
	default:
		return
	}
	root := strings.TrimSpace(a.GetDataDir())
	if root == "" {
		return
	}
	parent := filepath.Join(root, "file-companion", kind)
	dir := filepath.Join(parent, sessionID)
	if !companionFigureInsideDir(parent, dir) {
		return
	}
	// A document that itself lives in this tree is the source file. Leave
	// the tree in place rather than remove the file the reader still has.
	if strings.TrimSpace(source) != "" {
		key := fileCompanionFigureKey(source)
		dirKey := fileCompanionFigureKey(dir)
		if key != "" && (key == dirKey || companionFigureInsideDir(dir, source)) {
			return
		}
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		logFileCompanion("project %s remove failed session=%s", kind, sessionID)
	}
}

func fileCompanionShareToken(link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", errors.New("请粘贴分享链接")
	}
	if fileCompanionShareTokenMatch.MatchString(link) {
		return link, nil
	}
	if parsed, err := url.Parse(link); err == nil {
		if token := strings.TrimSpace(parsed.Query().Get("token")); fileCompanionShareTokenMatch.MatchString(token) {
			return token, nil
		}
		base := strings.TrimSpace(parsed.Path)
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if fileCompanionShareTokenMatch.MatchString(base) {
			return base, nil
		}
	}
	if token := fileCompanionShareTokenFind.FindString(link); fileCompanionShareTokenMatch.MatchString(token) {
		return token, nil
	}
	return "", errors.New("不是伴读文档分享")
}

func fileCompanionLibrarySafeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	runes := []rune(name)
	if len(runes) > 180 {
		name = string(runes[:180])
	}
	if name == "" || name == "." || name == ".." {
		return "document"
	}
	return name
}
