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
	"time"
)

// The companion library is the reading history for 文档管理. It is not the
// open-set (files this window still has open) and not the mobile draft store.
// Local rows are recorded when a file actually opens. Cloud rows are shares
// this person created or imported.

const fileCompanionLibraryCap = 200

var (
	fileCompanionLibraryMu        sync.Mutex
	fileCompanionLibraryRefreshMu sync.Mutex
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

// FileCompanionLibraryItem is one row in 文档管理.
type FileCompanionLibraryItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
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
	// Highlights is set on an import that unpacked a companion project.
	// It is not stored in the library file.
	Highlights string `json:"highlights,omitempty"`
	// ChatMarks is the same import-only value for answer highlights. It is
	// absent from a legacy project, which leaves a reader's own marks alone.
	ChatMarks string `json:"chatMarks,omitempty"`
	// Annotations is the same import-only value for the host's placed notes.
	Annotations string `json:"annotations,omitempty"`
	// Drawings is the same import-only value for the host's freehand marks.
	Drawings string `json:"drawings,omitempty"`
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
}

// FileCompanionShareResult is the link created for the open file.
type FileCompanionShareResult struct {
	URL        string `json:"url"`
	Name       string `json:"name"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Visibility string `json:"visibility"`
	Permanent  bool   `json:"permanent"`
	Token      string `json:"token,omitempty"`
	Revision   int    `json:"revision,omitempty"`
}

// FileCompanionShareUpdate is a newer project the reader should show.
// Highlights and messages replace what that path had on screen. ChatMarks,
// Annotations, and Drawings are present only when the project carries them; a
// legacy project omits them and the reader's own marks and notes stay.
type FileCompanionShareUpdate struct {
	Path        string                 `json:"path"`
	Token       string                 `json:"token"`
	Name        string                 `json:"name"`
	Revision    int                    `json:"revision"`
	Highlights  string                 `json:"highlights,omitempty"`
	ChatMarks   *string                `json:"chatMarks,omitempty"`
	Annotations *string                `json:"annotations,omitempty"`
	Drawings    *string                `json:"drawings,omitempty"`
	Messages    []FileCompanionMessage `json:"messages"`
	Project     bool                   `json:"project"`
}

type fileCompanionShareView struct {
	Token string `json:"token"`
	Name  string `json:"name"`
	// Host/Admin are per-viewer flags on list rows. Host is a pointer so an
	// older Hub (field absent) can default to the owner.
	Host             *bool                    `json:"host,omitempty"`
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
	// desktop adopts it instead of leaving a duplicate dead end.
	token string
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
	items = upsertFileCompanionLocal(items, doc)
	_ = a.writeFileCompanionLibraryLocked(items)
}

func upsertFileCompanionLocal(items []FileCompanionLibraryItem, doc FileCompanionDocument) []FileCompanionLibraryItem {
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
		if item.Source == "local" && item.Path == doc.Path {
			if item.ID != "" {
				next.ID = item.ID
			}
			continue
		}
		kept = append(kept, item)
	}
	kept = append([]FileCompanionLibraryItem{next}, kept...)
	return trimFileCompanionLibrary(kept, fileCompanionLibraryCap)
}

func fileCompanionLibraryLocalID(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return "local:" + hex.EncodeToString(sum[:8])
}

func (a *App) readFileCompanionLibraryLocked() ([]FileCompanionLibraryItem, bool) {
	path := a.fileCompanionLibraryPath()
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []FileCompanionLibraryItem{}, true
		}
		return nil, false
	}
	var stored struct {
		Items []FileCompanionLibraryItem `json:"items"`
	}
	if json.Unmarshal(raw, &stored) != nil {
		return nil, false
	}
	if stored.Items == nil {
		return []FileCompanionLibraryItem{}, true
	}
	return stored.Items, true
}

func (a *App) writeFileCompanionLibraryLocked(items []FileCompanionLibraryItem) error {
	path := a.fileCompanionLibraryPath()
	if path == "" {
		return errors.New("library path is empty")
	}
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	body, err := json.Marshal(struct {
		Items []FileCompanionLibraryItem `json:"items"`
	}{Items: items})
	if err != nil {
		return err
	}
	return fileCompanionWriteAtomicFile(path, body, 0o600)
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
	items, changed := dropMissingFileCompanionLibrary(items)
	if len(items) > fileCompanionLibraryCap {
		items = trimFileCompanionLibrary(items, fileCompanionLibraryCap)
		changed = true
	}
	if changed {
		_ = a.writeFileCompanionLibraryLocked(items)
	}
	fileCompanionLibraryMu.Unlock()

	notice := ""
	var updates []FileCompanionShareUpdate
	var removed []string
	var removedHosts map[string]string
	hub, viewer, loginErr := a.fileCompanionHubLogin()
	if loginErr != nil {
		notice = "登录后可以查看云端分享"
	} else {
		// One refresh at a time. A second open of 文档管理 waits, then sees
		// the revision the first refresh already stored, instead of downloading
		// the same project twice and writing an older library over the newer one.
		fileCompanionLibraryRefreshMu.Lock()
		remote, notesReply, notesReview, _, err := a.fetchFileCompanionShareList(hub, viewer)
		if err != nil {
			fileCompanionLibraryRefreshMu.Unlock()
			var status *fileCompanionShareStatus
			if errors.As(err, &status) {
				notice = status.Error()
			} else {
				notice = "暂时无法连接云端，本机文档仍可查看"
			}
		} else {
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
			removed, purgePaths, removedHosts = a.dropRevokedFileCompanionShares(items, previous, remote, revoked, removedTokens, epochs)
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
				// The local row of a downloaded copy is the open tab. Drop it
				// with the paths this refresh is closing. A file this refresh
				// is not closing keeps its row.
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
			}
			_ = a.writeFileCompanionLibraryLocked(items)
			// A revoked received copy also loses its transcript, drawings,
			// generated notes, and result files. This runs while the library is
			// locked so a re-import cannot publish into the session being purged.
			for _, path := range purgePaths {
				if path != "" && !fileCompanionLibraryUsesPath(items, path) {
					a.purgeFileCompanionProjectData(path)
				}
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
	expiredNow := time.Now().UTC()
	for i := range items {
		if items[i].Source == "cloud" {
			items[i].Expired = fileCompanionShareExpiredAt(items[i].ExpiresAt, expiredNow)
		}
	}
	return FileCompanionLibraryList{Items: items, Notice: notice, Updates: updates, Removed: removed, RemovedHosts: removedHosts, NotesReply: a.fileCompanionNotesReply.Load(), NotesReview: a.fileCompanionNotesReview.Load()}, nil
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
			// A hub that does not know about notes omits the field. Keep the
			// notes this machine already stored. A list that left earlier
			// also must not cover a note that landed while it was in flight.
			if !item.NotesKnown || prev.NotesRevision > item.NotesRevision {
				item.Notes = prev.Notes
				item.NotesRevision = prev.NotesRevision
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
		if seen[token] || dismissed[token] || revoked[token] || prev.Visibility == "group" || !fileCompanionLibraryFileExists(prev.Path) {
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
				} else {
					keptNotes := item.Notes
					keptRev := item.NotesRevision
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
	if cap > 0 && len(items) > cap {
		return items[:cap]
	}
	return items
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

func fileCompanionSameSite(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) && left.Hostname() != ""
}

// fileCompanionDirectClient follows a redirect only when it stays on the same
// host. A share request must not carry a reader grant, or the home Hub token,
// across to some other site.
func fileCompanionDirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 || len(via) >= 3 || req == nil || req.URL == nil || req.URL.User != nil {
				return http.ErrUseLastResponse
			}
			if !fileCompanionSameSite(via[len(via)-1].URL, req.URL) {
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

func (a *App) fetchFileCompanionShares(hub, viewer string) ([]FileCompanionLibraryItem, error) {
	items, _, _, _, err := a.fetchFileCompanionShareList(hub, viewer)
	return items, err
}

// fetchFileCompanionShareList also reports whether the Hub records host
// replies and whether reader notes wait for host review, plus the Hub's own
// clock. An older Hub omits both flags, and the owner machine then leaves the
// discussion to its owner instead of answering every note as if new. The
// server time keeps the discussion watermark on the same clock as the note
// timestamps; comparing a Hub-created note with a locally created install time
// can skip live notes when the clocks disagree.
func (a *App) fetchFileCompanionShareList(hub, viewer string) ([]FileCompanionLibraryItem, bool, bool, time.Time, error) {
	req, err := http.NewRequest(http.MethodGet, hub+"/api/mobile/documents/shares", nil)
	if err != nil {
		return nil, false, false, time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, false, time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, false, time.Time{}, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	var payload struct {
		Mine        []fileCompanionShareView `json:"mine"`
		Received    []fileCompanionShareView `json:"received"`
		NotesReply  bool                     `json:"notes_reply"`
		NotesReview bool                     `json:"notes_review"`
		ServerTime  string                   `json:"server_time"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, false, false, time.Time{}, errors.New("云端分享记录无法读取")
	}
	serverTime, _ := time.Parse(time.RFC3339, strings.TrimSpace(payload.ServerTime))
	out := make([]FileCompanionLibraryItem, 0, len(payload.Mine)+len(payload.Received))
	out = append(out, libraryItemsFromShares(payload.Mine, true, hub)...)
	out = append(out, libraryItemsFromShares(payload.Received, false, hub)...)
	return out, payload.NotesReply, payload.NotesReview, serverTime, nil
}

func libraryItemsFromShares(views []fileCompanionShareView, mine bool, hub string) []FileCompanionLibraryItem {
	out := make([]FileCompanionLibraryItem, 0, len(views))
	for _, view := range views {
		token := strings.TrimSpace(view.Token)
		if !fileCompanionShareTokenMatch.MatchString(token) || view.Expired {
			continue
		}
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
		if item.Name == "" {
			item.Name = "document"
		}
		if item.Visibility == "" {
			item.Visibility = "public"
		}
		if item.ShareURL == "" {
			item.ShareURL = strings.TrimRight(hub, "/") + "/hub/documents/shares/" + token
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
// phone. group needs a group id the caller administers.
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
	view, err := postFileCompanionShare(hub, viewer, name, visibility, password, target, days, contentHash, file)
	if err != nil {
		if adopted, handled, adoptErr := a.adoptDuplicateFileCompanionShare(canonical, err, visibility, password, target, days, zipPath, hub, viewer); handled {
			return adopted, adoptErr
		}
		return FileCompanionShareResult{}, err
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
	// The link exists with this paper and scope; apply the password/expiry the
	// person just selected before pushing the newer project.
	if _, err := a.UpdateFileCompanionShare(token, visibility, password, target, days); err != nil {
		return FileCompanionShareResult{}, true, err
	}
	file, err := os.Open(zipPath)
	if err != nil {
		return FileCompanionShareResult{}, true, errors.New("无法保存分享")
	}
	view, syncErr := postFileCompanionShareSync(hub, viewer, token, filepath.Base(canonical), file)
	_ = file.Close()
	if syncErr != nil {
		return FileCompanionShareResult{}, true, syncErr
	}
	if view.Token != token {
		return FileCompanionShareResult{}, true, errors.New("云端没有返回分享链接")
	}
	item := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
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
// negative days keeps the expiry the link already has.
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
	raw, err := a.fileCompanionHubJSON(http.MethodPatch, hub+"/api/mobile/documents/shares/"+token, viewer, payload)
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
		result.URL = strings.TrimRight(hub, "/") + "/hub/documents/shares/" + token
	}
	item := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
	if len(item) == 0 {
		return result, nil
	}
	// Rewrite the row this machine already has. A row deleted while the edit
	// was in flight is not brought back.
	if _, err := a.updateFileCompanionCloudRow(item[0]); err != nil {
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
func (a *App) DeleteFileCompanionShare(token string) error {
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
	_, err = a.fileCompanionHubJSON(http.MethodDelete, hub+"/api/mobile/documents/shares/"+token, viewer, nil)
	if err != nil {
		var status *fileCompanionShareStatus
		if !errors.As(err, &status) || status == nil {
			return err
		}
		switch {
		case status.status == http.StatusNotFound && strings.TrimSpace(status.message) == "":
			return errors.New("云端还不支持删除分享，需要更新 Hub")
		case status.status == http.StatusMethodNotAllowed || status.status == http.StatusNotImplemented:
			return errors.New("云端还不支持删除分享，需要更新 Hub")
		case status.status != http.StatusNotFound:
			return err
		}
		// The hub knows the route and says the link is gone; clear the row.
	}
	// Retire the token and drop the row in one critical section. A list that
	// started before the delete must not keep the row (merge keeps a local
	// cloud row whose file exists) or publish a pull that was still in flight.
	a.retireFileCompanionShare(token)
	return nil
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
	req, err := http.NewRequest(http.MethodPost, hub+"/api/mobile/documents/shares", pipeReader)
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
			Token string `json:"token"`
		}
		if json.Unmarshal(body, &payload) == nil {
			failure.token = strings.TrimSpace(payload.Token)
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
	payload, _ := json.Marshal(map[string]string{"contact": contact})
	_, err = a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/shares/"+token+"/host", viewer, payload)
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
	_, err = a.fileCompanionHubJSON(http.MethodDelete, hub+"/api/mobile/documents/shares/"+token+"/host?action="+action, viewer, nil)
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
	_, err = a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/shares/"+token+"/host/"+decision, viewer, nil)
	return err
}

// OpenFileCompanionShareFromNotification opens one shared document from a Hub
// notification. An already imported copy is opened as-is; otherwise the share
// is downloaded first (person and group notices need no password). The caller
// is the main window, so LaunchFileCompanion starts or reveals the companion.
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
	imported, err := a.ImportFileCompanionShare(token, "")
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
			if current.NotesRevision > item.NotesRevision {
				item.Notes = current.Notes
				item.NotesRevision = current.NotesRevision
			}
			continue
		}
		kept = append(kept, current)
	}
	kept = append([]FileCompanionLibraryItem{item}, kept...)
	return a.writeFileCompanionLibraryLocked(trimFileCompanionLibrary(kept, fileCompanionLibraryCap))
}

// retireFileCompanionShare removes a cloud row whose link is gone for good and
// marks the token dismissed under one lock. The dismissal is what stops an
// in-flight list from keeping the row and an in-flight pull from publishing it
// again; the token itself is dead, so the marker costs nothing after that.
func (a *App) retireFileCompanionShare(token string) {
	if a == nil || token == "" {
		return
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	_, _ = a.dismissFileCompanionShareLocked(token)
	a.forgetFileCompanionShareGrantLocked(token)
	a.dropFileCompanionCloudTokenLocked(token)
}

// dropFileCompanionCloudTokenLocked removes one cloud row. The local file
// stays: a share that is gone is not the reader's own paper. The caller holds
// fileCompanionLibraryMu so a delete can retire the share and drop its row in
// one critical section.
func (a *App) dropFileCompanionCloudTokenLocked(token string) {
	if a == nil || token == "" {
		return
	}
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return
	}
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	changed := false
	for _, item := range items {
		if item.Source == "cloud" && item.Token == token {
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	if changed {
		_ = a.writeFileCompanionLibraryLocked(kept)
	}
}

// FileCompanionProjectDelete is what 文档管理 removed. Path is the document
// that stayed on disk. Cleared is set when this row was the last project
// using that path, so the window drops highlights and closes the tab.
type FileCompanionProjectDelete struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Name    string `json:"name,omitempty"`
	Cleared bool   `json:"cleared"`
}

// DeleteFileCompanionProject removes one 文档管理 row and the companion data
// that belongs only to it: the reading record, highlights on disk, discussion,
// and generated notes. The source file is not removed.
func (a *App) DeleteFileCompanionProject(id string) (FileCompanionProjectDelete, error) {
	empty := FileCompanionProjectDelete{}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return empty, errors.New("没有找到这个项目")
	}
	fileCompanionLibraryMu.Lock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		fileCompanionLibraryMu.Unlock()
		return empty, errors.New("文档列表无法读取")
	}
	var found *FileCompanionLibraryItem
	kept := make([]FileCompanionLibraryItem, 0, len(items))
	for i := range items {
		item := items[i]
		if found == nil && fileCompanionLibraryItemMatches(item, id) {
			copy := item
			found = &copy
			continue
		}
		kept = append(kept, item)
	}
	if found == nil {
		fileCompanionLibraryMu.Unlock()
		return empty, errors.New("没有找到这个项目")
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
	path := strings.TrimSpace(found.Path)
	sharedPath := path != "" && fileCompanionLibraryUsesPath(kept, path)
	result := FileCompanionProjectDelete{
		ID:      found.ID,
		Path:    path,
		Name:    found.Name,
		Cleared: path != "" && !sharedPath,
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
	key := fileCompanionDedupeKey(path)
	if strings.TrimSpace(path) == "" || key == "" {
		return false
	}
	for _, item := range items {
		other := strings.TrimSpace(item.Path)
		if other == "" {
			continue
		}
		if fileCompanionDedupeKey(other) == key {
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
