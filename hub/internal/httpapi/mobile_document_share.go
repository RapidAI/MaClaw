package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Companion document shares are links to one file. A public link can be
// imported by anyone who can reach this Hub, including another tenant or a
// reader logged into a different Hub. A private link also needs the password.
// Person and group links stay on this Hub: the recipient has an account here.
// An empty expiry is permanent. The file bytes live in the mobile blob store,
// not in this JSON index.

const companionDocSharePathEnv = "MACLAW_COMPANION_DOC_SHARE_PATH"

// companionShareDuplicateMessage is what a reader sees when the same paper is
// already shared to the same audience. Deleting or editing that link is the
// way forward; a second link would only split the annotations.
const companionShareDuplicateMessage = "这份文档已经以相同的范围分享过了，请先删除原分享，或修改原分享"

// companionShareDuplicateUpdateMessage is the same rule seen from 修改: the
// link being edited is not the conflict, another link is.
const companionShareDuplicateUpdateMessage = "这份文档已经以相同的范围分享过了，请先删除另一份分享"

var companionShareTokenPattern = regexp.MustCompile(`^cd_[0-9a-f]{32}$`)

// A reader grant lets someone who is not a user of this Hub keep reading a
// public or private share: later downloads, notes, and the note list. It is
// not an account session and cannot sync, delete, or review.
const (
	companionReaderGrantHeader = "X-Companion-Reader-Grant"
	companionShareGuestCap     = 500
)

var companionReaderGrantPattern = regexp.MustCompile(`^cgr_[0-9a-f]{32}$`)

// companionShareGuest is one off-hub reader. GrantHash is sha256 of the secret
// the reader presents; Label is the name shown on their notes.
type companionShareGuest struct {
	GrantHash string    `json:"grant_hash"`
	Label     string    `json:"label"`
	SeenAt    time.Time `json:"seen_at"`
}

type companionDocShare struct {
	Token      string `json:"token"`
	TenantID   string `json:"tenant_id"`
	OwnerID    string `json:"owner_id"`
	OwnerLabel string `json:"owner_label"`
	// HostID is the current host when the admin transferred the seat. Empty
	// means the owner is the host. The owner keeps admin rights for the life of
	// the share, even after becoming a participant.
	HostID    string `json:"host_id,omitempty"`
	HostLabel string `json:"host_label,omitempty"`
	// A pending nomination waits for the named participant to accept.
	PendingHostID    string            `json:"pending_host_id,omitempty"`
	PendingHostLabel string            `json:"pending_host_label,omitempty"`
	PendingHostSince time.Time         `json:"pending_host_since,omitempty"`
	Name             string            `json:"name"`
	Size             int64             `json:"size"`
	BlobPath         string            `json:"blob_path"`
	Visibility       string            `json:"visibility"`
	PasswordSalt     string            `json:"password_salt,omitempty"`
	PasswordHash     string            `json:"password_hash,omitempty"`
	ExpiresAt        time.Time         `json:"expires_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	Importers        map[string]string `json:"importers,omitempty"`
	// Revision starts at 1. The owner bumps it when highlights or chat change.
	// The bytes are a companion project: the paper, the highlights, and the chat.
	Revision int `json:"revision,omitempty"`
	// Target is an email or phone for a person share, or the group id.
	Target string `json:"target,omitempty"`
	// GroupID is set when Visibility is group. Access follows accepted members.
	GroupID string `json:"group_id,omitempty"`
	// ContentHash identifies the paper itself, not the companion project zip.
	// It is what makes (paper, scope) unique per owner while a share is live.
	ContentHash string `json:"content_hash,omitempty"`
	// Guests imported a public or private link without an account on this Hub.
	// The raw grant stays with the reader; only its hash is stored here.
	Guests []companionShareGuest `json:"guests,omitempty"`
	// Notes are other readers' discussion. They are not the host transcript,
	// and a host sync of the paper must not drop them.
	Notes         []companionShareNote `json:"notes,omitempty"`
	NotesRevision int                  `json:"notes_revision,omitempty"`
}

// companionShareNote is one reader's discussion on a share. The host's own
// chat stays in the project. These notes are what everyone who can see the
// share reads.
//
// Host marks a note written by the share owner: the owner's own line, or the
// answer the owner's agent wrote for a reader note. ReplyTo is the reader note
// a host note answers, and it is how every machine learns that the note
// already received its one answer.
type companionShareNote struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id,omitempty"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Host      bool      `json:"host,omitempty"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	// Decision is the host's review of a reader note: pending before anyone
	// looks at it, approved before the host agent answers it, ignored when the
	// host chooses not to answer. Host notes have no decision: they are the
	// answer, not a question. Notes written before review existed carry no
	// value; an unanswered one is read as pending so the host still decides.
	Decision  string    `json:"decision,omitempty"`
	DecidedAt time.Time `json:"decided_at,omitempty"`
}

var companionDocShares = struct {
	sync.Mutex
	loaded bool
	items  map[string]companionDocShare
}{items: map[string]companionDocShare{}}

func companionDocShareStatePath() string {
	if p := strings.TrimSpace(os.Getenv(companionDocSharePathEnv)); p != "" {
		return p
	}
	state := mobileStatePath()
	if state == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(state), "companion-doc-shares.json")
}

func loadCompanionDocShares() {
	companionDocShares.Lock()
	defer companionDocShares.Unlock()
	if companionDocShares.loaded {
		return
	}
	companionDocShares.loaded = true
	if companionDocShares.items == nil {
		companionDocShares.items = map[string]companionDocShare{}
	}
	path := companionDocShareStatePath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored struct {
		Items map[string]companionDocShare `json:"items"`
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Items == nil {
		return
	}
	companionDocShares.items = stored.Items
}

// companionShareDisk orders index writes. A download copies the index and
// writes it after the file has already been sent. A newer snapshot must not
// lose to that older write. dirty means the last background write failed, so
// the next download saves the index again.
var companionShareDisk = struct {
	mu      sync.Mutex
	next    uint64
	written uint64
	dirty   bool
}{}

var companionSharePersistWG sync.WaitGroup

type companionShareIndexSnapshot struct {
	path  string
	gen   uint64
	items map[string]companionDocShare
}

func cloneCompanionDocShare(share companionDocShare) companionDocShare {
	if len(share.Guests) > 0 {
		guests := make([]companionShareGuest, len(share.Guests))
		copy(guests, share.Guests)
		share.Guests = guests
	}
	if len(share.Notes) > 0 {
		notes := make([]companionShareNote, len(share.Notes))
		copy(notes, share.Notes)
		share.Notes = notes
	}
	if len(share.Importers) > 0 {
		importers := make(map[string]string, len(share.Importers))
		for id, at := range share.Importers {
			importers[id] = at
		}
		share.Importers = importers
	}
	return share
}

// cloneCompanionShareIndexLocked copies the index for a background write.
// The caller holds companionDocShares. Nested slices are copied so a later
// edit can change the live index while this snapshot is marshaled.
func cloneCompanionShareIndexLocked() companionShareIndexSnapshot {
	path := companionDocShareStatePath()
	if path == "" {
		return companionShareIndexSnapshot{}
	}
	items := make(map[string]companionDocShare, len(companionDocShares.items))
	for token, share := range companionDocShares.items {
		items[token] = cloneCompanionDocShare(share)
	}
	companionShareDisk.mu.Lock()
	companionShareDisk.next++
	gen := companionShareDisk.next
	companionShareDisk.mu.Unlock()
	return companionShareIndexSnapshot{path: path, gen: gen, items: items}
}

func scheduleCompanionShareIndex(snap companionShareIndexSnapshot) {
	if snap.path == "" || snap.gen == 0 {
		return
	}
	companionSharePersistWG.Add(1)
	go func() {
		defer companionSharePersistWG.Done()
		body, err := json.Marshal(struct {
			Items map[string]companionDocShare `json:"items"`
		}{Items: snap.items})
		if err != nil {
			companionShareDisk.mu.Lock()
			companionShareDisk.dirty = true
			companionShareDisk.mu.Unlock()
			return
		}
		if err := writeCompanionShareSnapshot(snap.path, body, snap.gen); err != nil {
			if err := writeCompanionShareSnapshot(snap.path, body, snap.gen); err != nil {
				companionShareDisk.mu.Lock()
				companionShareDisk.dirty = true
				companionShareDisk.mu.Unlock()
			}
		}
	}()
}

// takeCompanionShareSnapshotLocked copies the index. The caller holds
// companionDocShares. The path is captured here so a later test can point the
// env var at a different file without this write following it.
func takeCompanionShareSnapshotLocked() (string, []byte, uint64, error) {
	path := companionDocShareStatePath()
	if path == "" {
		return "", nil, 0, nil
	}
	body, err := json.Marshal(struct {
		Items map[string]companionDocShare `json:"items"`
	}{Items: companionDocShares.items})
	if err != nil {
		return "", nil, 0, err
	}
	companionShareDisk.mu.Lock()
	companionShareDisk.next++
	gen := companionShareDisk.next
	companionShareDisk.mu.Unlock()
	return path, body, gen, nil
}

func writeCompanionShareSnapshot(path string, body []byte, gen uint64) error {
	if path == "" || gen == 0 {
		return nil
	}
	companionShareDisk.mu.Lock()
	defer companionShareDisk.mu.Unlock()
	if gen < companionShareDisk.written {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	companionShareDisk.written = gen
	if gen == companionShareDisk.next {
		companionShareDisk.dirty = false
	}
	return nil
}

func persistCompanionDocSharesLocked() error {
	path, body, gen, err := takeCompanionShareSnapshotLocked()
	if err != nil || path == "" {
		return err
	}
	return writeCompanionShareSnapshot(path, body, gen)
}

func (s companionDocShare) expired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt)
}

func newCompanionShareToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cd_" + hex.EncodeToString(buf), nil
}

// companionShareScopeKey is the audience half of the duplicate key. A private
// share's password is not part of it: sharing the same paper privately twice is
// a duplicate even when the password differs.
func companionShareScopeKey(visibility, target, groupID string) string {
	switch visibility {
	case "group":
		return "group:" + strings.TrimSpace(groupID)
	case "person":
		return "person:" + strings.TrimSpace(target)
	default:
		return visibility
	}
}

func normalizeCompanionContentHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return value
}

func companionShareContentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// companionShareDuplicateLocked returns the live share that already covers
// this (paper, scope), ignoring exceptToken so 修改 does not collide with
// itself. An expired share does not block a new one: the reader may publish
// it again. Legacy shares created before ContentHash existed do not match, so
// they never block a share this build makes.
func companionShareDuplicateLocked(ownerID, contentHash, visibility, target, groupID, exceptToken string, now time.Time) (companionDocShare, bool) {
	if contentHash == "" {
		return companionDocShare{}, false
	}
	scope := companionShareScopeKey(visibility, target, groupID)
	for _, share := range companionDocShares.items {
		if share.Token == exceptToken {
			continue
		}
		if share.OwnerID != ownerID || share.expired(now) {
			continue
		}
		if strings.TrimSpace(share.ContentHash) != contentHash {
			continue
		}
		if companionShareScopeKey(share.Visibility, share.Target, share.GroupID) != scope {
			continue
		}
		return share, true
	}
	return companionDocShare{}, false
}

func hashCompanionSharePassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + "\n" + password))
	return hex.EncodeToString(sum[:])
}

func companionSharePasswordOK(share companionDocShare, password string) bool {
	if share.Visibility != "private" {
		return true
	}
	password = strings.TrimSpace(password)
	if password == "" || share.PasswordHash == "" {
		return false
	}
	got := hashCompanionSharePassword(password, share.PasswordSalt)
	if len(got) != len(share.PasswordHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(share.PasswordHash)) == 1
}

// companionShareAllowsGuest is the audience that can be opened from another
// tenant or another Hub. Person and group shares name an account on this Hub.
func companionShareAllowsGuest(visibility string) bool {
	switch strings.TrimSpace(visibility) {
	case "", "public", "private":
		return true
	default:
		return false
	}
}

func newCompanionReaderGrant() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cgr_" + hex.EncodeToString(buf), nil
}

func hashCompanionReaderGrant(grant string) string {
	sum := sha256.Sum256([]byte(grant))
	return hex.EncodeToString(sum[:])
}

func cleanCompanionReaderLabel(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 80 {
		value = strings.TrimSpace(string(runes[:80]))
	}
	if value == "" {
		return "跨 Hub 读者"
	}
	return value
}

func companionReaderGrantHeaderValue(r *http.Request) string {
	if r == nil {
		return ""
	}
	grant := strings.TrimSpace(r.Header.Get(companionReaderGrantHeader))
	if companionReaderGrantPattern.MatchString(grant) {
		return grant
	}
	return ""
}

// companionShareGuestByGrant reports the stored label when grant is one this
// share issued. The hash compare does not branch on the secret itself.
func companionShareGuestByGrant(share companionDocShare, grant string) (string, string, bool) {
	if !companionReaderGrantPattern.MatchString(grant) {
		return "", "", false
	}
	hash := hashCompanionReaderGrant(grant)
	for _, guest := range share.Guests {
		if len(guest.GrantHash) != len(hash) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(guest.GrantHash), []byte(hash)) == 1 {
			label := strings.TrimSpace(guest.Label)
			if label == "" {
				label = "跨 Hub 读者"
			}
			return label, hash, true
		}
	}
	return "", "", false
}

// companionShareRememberGuest stores a new grant or refreshes one this share
// already knows. label is applied when the caller sent one; an empty label on
// a returning reader keeps the name already stored. The bool is true when the
// durable record changed (a new reader, a new name, or an eviction). A repeat
// download of the same reader only moves SeenAt in memory, so the import can
// send the file without rewriting the share index.
func companionShareRememberGuest(share companionDocShare, grant, label string) (companionDocShare, bool) {
	if !companionReaderGrantPattern.MatchString(grant) {
		return share, false
	}
	hash := hashCompanionReaderGrant(grant)
	now := time.Now().UTC()
	sent := strings.TrimSpace(label)
	cleaned := cleanCompanionReaderLabel(label)
	for i := range share.Guests {
		if len(share.Guests[i].GrantHash) != len(hash) || subtle.ConstantTimeCompare([]byte(share.Guests[i].GrantHash), []byte(hash)) != 1 {
			continue
		}
		changed := false
		if sent != "" {
			changed = share.Guests[i].Label != cleaned
		} else if strings.TrimSpace(share.Guests[i].Label) == "" {
			changed = true
		}
		if !changed {
			share.Guests[i].SeenAt = now
			return share, false
		}
		guests := make([]companionShareGuest, len(share.Guests))
		copy(guests, share.Guests)
		guests[i].Label = cleaned
		guests[i].SeenAt = now
		share.Guests = guests
		return share, true
	}
	guests := make([]companionShareGuest, len(share.Guests), len(share.Guests)+1)
	copy(guests, share.Guests)
	guests = append(guests, companionShareGuest{GrantHash: hash, Label: cleaned, SeenAt: now})
	for len(guests) > companionShareGuestCap {
		oldest := 0
		for i := 1; i < len(guests); i++ {
			if guests[i].SeenAt.Before(guests[oldest].SeenAt) {
				oldest = i
			}
		}
		guests = append(guests[:oldest], guests[oldest+1:]...)
	}
	share.Guests = guests
	return share, true
}

// companionViewerIfPresent accepts a bearer token when one was sent. A missing
// token is not an error: public and private links can be opened without an
// account on this Hub. A token that was sent and rejected still fails, so an
// expired login is not silently turned into a guest.
func companionViewerIfPresent(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, string, bool) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") || strings.TrimSpace(authz[len("bearer "):]) == "" {
		return nil, "", true
	}
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
		return nil, "", false
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
		return nil, "", false
	}
	return principal, ownerID, true
}

func companionShareOwnerLabel(principal *auth.ViewerPrincipal) string {
	if principal == nil {
		return "云端用户"
	}
	if email := strings.TrimSpace(principal.Email); email != "" {
		return email
	}
	if id := strings.TrimSpace(principal.UserID); id != "" {
		return id
	}
	return "云端用户"
}

// companionShareHostID is the seat that answers notes and syncs the project.
// Before any transfer, or after an admin revokes the seat, the owner holds it.
func companionShareHostID(share companionDocShare) string {
	if id := strings.TrimSpace(share.HostID); id != "" {
		return id
	}
	return strings.TrimSpace(share.OwnerID)
}

func companionShareHostLabel(share companionDocShare) string {
	if label := strings.TrimSpace(share.HostLabel); label != "" {
		return label
	}
	if label := strings.TrimSpace(share.OwnerLabel); label != "" {
		return label
	}
	return "云端用户"
}

func companionShareIsAdmin(share companionDocShare, userID string) bool {
	userID = strings.TrimSpace(userID)
	return userID != "" && strings.TrimSpace(share.OwnerID) == userID
}

func companionShareIsHost(share companionDocShare, userID string) bool {
	userID = strings.TrimSpace(userID)
	return userID != "" && companionShareHostID(share) == userID
}

func companionShareFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	runes := []rune(cleaned)
	if len(runes) > 180 {
		cleaned = string(runes[:180])
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "document"
	}
	return cleaned
}

func companionShareURL(r *http.Request, token string) string {
	path := "/hub/documents/shares/" + token
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return path
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	switch strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")) {
	case "https", "http":
		scheme = strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	}
	return scheme + "://" + host + path
}

func companionShareView(share companionDocShare, r *http.Request) map[string]any {
	now := time.Now().UTC()
	view := map[string]any{
		"token":          share.Token,
		"name":           share.Name,
		"size":           share.Size,
		"owner":          share.OwnerLabel,
		"host_label":     companionShareHostLabel(share),
		"visibility":     share.Visibility,
		"needs_password": share.Visibility == "private",
		"permanent":      share.ExpiresAt.IsZero(),
		"expired":        share.expired(now),
		"url":            companionShareURL(r, share.Token),
		"created_at":     share.CreatedAt.UTC().Format(time.RFC3339),
		"revision":       share.Revision,
	}
	if !share.ExpiresAt.IsZero() {
		view["expires_at"] = share.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return view
}

func companionShareByToken(token string) (companionDocShare, bool) {
	loadCompanionDocShares()
	companionDocShares.Lock()
	defer companionDocShares.Unlock()
	share, ok := companionDocShares.items[token]
	return share, ok
}

// CompanionDocumentSharesHandler lists the caller's shares and accepts a new one.
//
//	GET  /api/mobile/documents/shares
//	POST /api/mobile/documents/shares
func CompanionDocumentSharesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeCompanionShareList(w, r, identity, principal, ownerID)
		case http.MethodPost:
			createCompanionShare(w, r, principal, ownerID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		}
	}
}

func writeCompanionShareList(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, principal *auth.ViewerPrincipal, ownerID string) {
	contacts := companionViewerContactSet(identity, r.Context(), principal)
	groups := companionAcceptedGroupIDs(ownerID, contacts)
	loadCompanionDocShares()
	now := time.Now().UTC()
	mine := make([]map[string]any, 0)
	received := make([]map[string]any, 0)
	companionDocShares.Lock()
	for _, share := range companionDocShares.items {
		if share.expired(now) {
			continue
		}
		view := companionShareView(share, r)
		view["host"] = companionShareIsHost(share, ownerID)
		if companionShareIsAdmin(share, ownerID) {
			view["admin"] = true
			if strings.TrimSpace(share.PendingHostID) != "" {
				view["pending_host"] = true
				view["pending_host_label"] = strings.TrimSpace(share.PendingHostLabel)
			}
		}
		if share.OwnerID == ownerID {
			if share.Target != "" {
				view["target"] = share.Target
			}
			if share.GroupID != "" {
				view["group_id"] = share.GroupID
			}
			companionShareAttachNotes(view, share)
			mine = append(mine, view)
			continue
		}
		if companionShareReceived(share, ownerID, contacts, groups) {
			if share.GroupID != "" {
				view["group_id"] = share.GroupID
			}
			companionShareAttachNotes(view, share)
			received = append(received, view)
		}
	}
	companionDocShares.Unlock()
	// NotesReply tells the desktop that this Hub records host replies. An older
	// Hub omits it, and the owner's machine then does not run a discussion agent:
	// without reply_to every host line would look unanswered and loop.
	// ServerTime lets the owner's watermark use this Hub's clock. The two
	// machines can disagree by minutes, and comparing a note time from one
	// clock with an install time from the other silently skips live notes.
	writeJSON(w, http.StatusOK, map[string]any{
		"mine":         mine,
		"received":     received,
		"notes_reply":  true,
		"notes_review": true,
		"server_time":  now.UTC().Format(time.RFC3339),
	})
}

// writeCompanionShareDuplicate tells the desktop which live link already
// covers this paper and scope. The desktop can then adopt that link (and sync
// the current project onto it) instead of leaving an invisible share behind
// when a local row was removed in an older build.
func writeCompanionShareDuplicate(w http.ResponseWriter, r *http.Request, share companionDocShare) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":    "SHARE_DUPLICATE",
		"message": companionShareDuplicateMessage,
		"token":   share.Token,
		"share":   companionShareView(share, r),
	})
}

func createCompanionShare(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, ownerID string) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(mobileDocumentOriginalMaxBytes)+1<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "文件过大")
		return
	}
	visibility := strings.TrimSpace(r.FormValue("visibility"))
	switch visibility {
	case "public", "private", "person", "group":
	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择分享对象")
		return
	}
	password := strings.TrimSpace(r.FormValue("password"))
	target := ""
	groupID := ""
	switch visibility {
	case "private":
		n := len([]rune(password))
		if n < 4 || n > 64 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "分享密码需为 4–64 个字符")
			return
		}
	case "person":
		password = ""
		target = normalizeCompanionContact(r.FormValue("target"))
		if target == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写邮箱或手机号")
			return
		}
	case "group":
		password = ""
		groupID = strings.TrimSpace(r.FormValue("target"))
		if !companionGroupTokenPattern.MatchString(groupID) || !companionGroupIsAdmin(ownerID, groupID) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择群组")
			return
		}
		target = groupID
	default:
		password = ""
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.FormValue("days")))
	if err != nil || days < 0 || days > 3660 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "分享时间不正确")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择要分享的文件")
		return
	}
	defer file.Close()
	// The multipart filename is an ASCII fallback. The real name, including
	// Chinese, is the filename field the desktop sends beside the bytes.
	name := strings.TrimSpace(r.FormValue("filename"))
	if name == "" && header != nil {
		name = header.Filename
	}
	raw, err := readCompanionShareUpload(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	contentHash := normalizeCompanionContentHash(r.FormValue("content_hash"))
	if contentHash == "" {
		// An older desktop does not send the paper hash. The uploaded bytes are
		// then the only identity available; an exact retry still dedupes.
		contentHash = companionShareContentHash(raw)
	}
	now := time.Now().UTC()
	loadCompanionDocShares()
	companionDocShares.Lock()
	duplicateShare, duplicate := companionShareDuplicateLocked(ownerID, contentHash, visibility, target, groupID, "", now)
	companionDocShares.Unlock()
	if duplicate {
		go ensureCompanionShareNotification(duplicateShare)
		writeCompanionShareDuplicate(w, r, duplicateShare)
		return
	}
	token, err := newCompanionShareToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法创建分享")
		return
	}
	blobPath, err := mobileWriteDocumentBlob(ownerID, "share", token, raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享文件")
		return
	}
	share := companionDocShare{
		Token:       token,
		TenantID:    strings.TrimSpace(principal.TenantID),
		OwnerID:     ownerID,
		OwnerLabel:  companionShareOwnerLabel(principal),
		Name:        companionShareFileName(name),
		Size:        int64(len(raw)),
		BlobPath:    blobPath,
		Visibility:  visibility,
		CreatedAt:   now,
		Importers:   map[string]string{},
		Revision:    1,
		Target:      target,
		GroupID:     groupID,
		ContentHash: contentHash,
	}
	if days > 0 {
		share.ExpiresAt = now.Add(time.Duration(days) * 24 * time.Hour)
	}
	if password != "" {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法创建分享")
			return
		}
		share.PasswordSalt = hex.EncodeToString(salt)
		share.PasswordHash = hashCompanionSharePassword(password, share.PasswordSalt)
	}
	loadCompanionDocShares()
	companionDocShares.Lock()
	// The pre-check happened before the blob was written. Check once more with
	// the lock held so two requests that raced through it cannot both publish.
	if duplicateShare, duplicate = companionShareDuplicateLocked(ownerID, contentHash, visibility, target, groupID, "", now); duplicate {
		companionDocShares.Unlock()
		mobileDeleteDocumentBlob(blobPath)
		go ensureCompanionShareNotification(duplicateShare)
		writeCompanionShareDuplicate(w, r, duplicateShare)
		return
	}
	companionDocShares.items[token] = share
	err = persistCompanionDocSharesLocked()
	if err != nil {
		delete(companionDocShares.items, token)
		companionDocShares.Unlock()
		mobileDeleteDocumentBlob(blobPath)
		writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享")
		return
	}
	companionDocShares.Unlock()
	go refreshCompanionShareNotification(share)
	writeJSON(w, http.StatusOK, companionShareView(share, r))
}

func readCompanionShareUpload(file multipart.File) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(file, int64(mobileDocumentOriginalMaxBytes)+1))
	if err != nil {
		return nil, companionShareError("文件无法读取")
	}
	if int64(len(raw)) > int64(mobileDocumentOriginalMaxBytes) {
		return nil, companionShareError("文件过大")
	}
	if len(raw) == 0 {
		return nil, companionShareError("文件是空的")
	}
	return raw, nil
}

type companionShareError string

func (e companionShareError) Error() string { return string(e) }

// CompanionDocumentShareHandler returns public metadata for one link.
// The bytes and the password hash stay on the server.
//
//	GET /api/mobile/documents/shares/{token}
func CompanionDocumentShareHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, ok := companionDocShares.items[token]
		if ok {
			share.Notes = cloneCompanionShareNotes(share.Notes)
			share.Guests = append([]companionShareGuest(nil), share.Guests...)
		}
		companionDocShares.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		view := companionShareView(share, r)
		// Notes stay off the unauthenticated metadata response. A private link
		// would otherwise leak the discussion to anyone who has the address
		// but not the password. A grant issued at import is enough.
		if _, _, guest := companionShareGuestByGrant(share, companionReaderGrantHeaderValue(r)); guest && companionShareAllowsGuest(share.Visibility) {
			companionShareAttachNotes(view, share)
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// CompanionDocumentShareDeleteHandler removes one of the caller's links. The
// blob goes with it; copies readers already imported stay on their machines.
//
//	DELETE /api/mobile/documents/shares/{token}
func CompanionDocumentShareDeleteHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use DELETE")
			return
		}
		_, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, found := companionDocShares.items[token]
		if !found {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.OwnerID != ownerID {
			companionDocShares.Unlock()
			writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有分享者可以删除分享")
			return
		}
		delete(companionDocShares.items, token)
		if err := persistCompanionDocSharesLocked(); err != nil {
			companionDocShares.items[token] = share
			companionDocShares.Unlock()
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法删除分享")
			return
		}
		companionDocShares.Unlock()
		go revokeCompanionShareNotification(share.Token)
		go revokeCompanionHostTransferNotifications(share.Token)
		mobileDeleteDocumentBlob(share.BlobPath)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": token})
	}
}

// CompanionDocumentShareUpdateHandler changes who can see a share and how long
// it lasts. Only the owner may change it. The file, the revision, and the
// readers' notes stay as they are; a password that is left empty keeps the
// one already stored. A change that would leave two live links for the same
// paper and audience is refused, exactly like creating a duplicate.
//
//	PATCH /api/mobile/documents/shares/{token}
func CompanionDocumentShareUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		_, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		var body struct {
			Visibility string `json:"visibility"`
			Password   string `json:"password"`
			Target     string `json:"target"`
			Days       *int   `json:"days"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择分享对象")
			return
		}
		visibility := strings.TrimSpace(body.Visibility)
		switch visibility {
		case "public", "private", "person", "group":
		default:
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择分享对象")
			return
		}
		// Validate the audience before taking the share lock. The group check
		// takes the group lock, and holding one store's lock while waiting on
		// another is how two requests deadlock.
		password := strings.TrimSpace(body.Password)
		target := ""
		groupID := ""
		switch visibility {
		case "private":
			if password != "" {
				if n := len([]rune(password)); n < 4 || n > 64 {
					writeError(w, http.StatusBadRequest, "BAD_REQUEST", "分享密码需为 4–64 个字符")
					return
				}
			}
		case "person":
			password = ""
			target = normalizeCompanionContact(body.Target)
			if target == "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写邮箱或手机号")
				return
			}
		case "group":
			password = ""
			groupID = strings.TrimSpace(body.Target)
			if !companionGroupTokenPattern.MatchString(groupID) || !companionGroupIsAdmin(ownerID, groupID) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择群组")
				return
			}
			target = groupID
		default:
			password = ""
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, found := companionDocShares.items[token]
		if !found {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.OwnerID != ownerID {
			companionDocShares.Unlock()
			writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有分享者可以修改分享")
			return
		}
		if visibility == "private" && password == "" && share.PasswordHash == "" {
			// A private link with no password cannot be opened by anyone but
			// the people who already imported it. The owner has to send one
			// the first time they make the link private.
			companionDocShares.Unlock()
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "分享密码需为 4–64 个字符")
			return
		}
		previous := share
		// An absent days field leaves the expiry alone. A caller that only
		// changes the audience must not silently turn a dated link permanent.
		if body.Days != nil {
			days := *body.Days
			if days < 0 || days > 3660 {
				companionDocShares.Unlock()
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "分享时间不正确")
				return
			}
			if days > 0 {
				share.ExpiresAt = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
			} else {
				share.ExpiresAt = time.Time{}
			}
		}
		// A link that stays expired does not collide with anything. One that is
		// live after this edit must not duplicate another live link's scope.
		now := time.Now().UTC()
		if share.ExpiresAt.IsZero() || now.Before(share.ExpiresAt) {
			if _, duplicate := companionShareDuplicateLocked(ownerID, share.ContentHash, visibility, target, groupID, token, now); duplicate {
				companionDocShares.Unlock()
				writeError(w, http.StatusConflict, "SHARE_DUPLICATE", companionShareDuplicateUpdateMessage)
				return
			}
		}
		share.Visibility = visibility
		share.Target = target
		share.GroupID = groupID
		audienceChanged := companionShareAudienceChanged(previous, share)
		// Any audience edit withdraws a nomination, even when the nominee can
		// still open the new audience. The host seat is settled after the lock
		// is released, because that check takes the group lock.
		clearedPending := ""
		if audienceChanged && strings.TrimSpace(share.PendingHostID) != "" {
			clearedPending = strings.TrimSpace(share.PendingHostID)
			share.PendingHostID = ""
			share.PendingHostLabel = ""
			share.PendingHostSince = time.Time{}
		}
		if visibility == "private" {
			if password != "" {
				salt := make([]byte, 16)
				if _, err := rand.Read(salt); err != nil {
					companionDocShares.Unlock()
					writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享")
					return
				}
				share.PasswordSalt = hex.EncodeToString(salt)
				share.PasswordHash = hashCompanionSharePassword(password, share.PasswordSalt)
			}
		} else {
			share.PasswordSalt = ""
			share.PasswordHash = ""
		}
		companionDocShares.items[token] = share
		if err := persistCompanionDocSharesLocked(); err != nil {
			companionDocShares.items[token] = previous
			companionDocShares.Unlock()
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享")
			return
		}
		companionDocShares.Unlock()
		if audienceChanged {
			go refreshCompanionShareNotification(share)
			droppedPending, droppedHost, latest, _ := dropUnreachableCompanionHostSeats(r.Context(), identity, token, nil)
			if latest.Token != "" {
				share = latest
			}
			// A nomination that landed after this edit owns the status notice.
			// Publishing "已取消" on top of it would revoke that request.
			// A nomination that landed and cannot open is cleared by the drop
			// above; that person still needs the outcome.
			pendingCleared := clearedPending != "" || droppedPending != ""
			if strings.TrimSpace(share.PendingHostID) == "" && (pendingCleared || droppedHost != "") {
				go notifyCompanionHostTransferResult(share, companionShareAudienceHostNotice(share, pendingCleared, droppedHost != ""), companionHostSeatStampOf(share), clearedPending, droppedPending, droppedHost)
			}
		}
		view := companionShareView(share, r)
		// The editor is the admin. Echo the seat so the desktop row does not
		// treat a transferred host as this machine again: an absent host field
		// is read as "the owner still holds the seat".
		view["admin"] = true
		view["host"] = companionShareIsHost(share, ownerID)
		if strings.TrimSpace(share.PendingHostID) != "" {
			view["pending_host"] = true
			view["pending_host_label"] = strings.TrimSpace(share.PendingHostLabel)
		}
		// The owner is looking at their own link, so the target and the group
		// are part of what the edit dialog echoes back.
		if share.Target != "" {
			view["target"] = share.Target
		}
		if share.GroupID != "" {
			view["group_id"] = share.GroupID
		}
		companionShareAttachNotes(view, share)
		writeJSON(w, http.StatusOK, view)
	}
}

// CompanionDocumentShareImportHandler checks the password and returns the file.
// The caller is recorded so the file shows up in that person's library.
//
//	POST /api/mobile/documents/shares/{token}/import
func CompanionDocumentShareImportHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionViewerIfPresent(w, r, identity)
		if !ok {
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		var body struct {
			Password    string `json:"password"`
			ReaderLabel string `json:"reader_label"`
			ReaderGrant string `json:"reader_grant"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body)
		grant := companionReaderGrantHeaderValue(r)
		if grant == "" && companionReaderGrantPattern.MatchString(strings.TrimSpace(body.ReaderGrant)) {
			grant = strings.TrimSpace(body.ReaderGrant)
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, found := companionDocShares.items[token]
		if !found {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.expired(time.Now().UTC()) {
			companionDocShares.Unlock()
			writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
			return
		}
		issuedGrant := ""
		// durable is a new reader or a renamed one. A repeat download only
		// moves SeenAt in memory and does not rewrite the index.
		durable := false
		if ownerID != "" {
			contacts := companionViewerContactSet(identity, r.Context(), principal)
			groups := companionAcceptedGroupIDs(ownerID, contacts)
			status, message := companionShareReadAccess(share, ownerID, contacts, groups, body.Password)
			if status != 0 {
				companionDocShares.Unlock()
				code := "SHARE_FORBIDDEN"
				if status == http.StatusUnauthorized {
					code = "SHARE_PASSWORD"
				}
				if status == http.StatusForbidden && share.Visibility == "private" {
					code = "SHARE_PASSWORD"
				}
				writeError(w, status, code, message)
				return
			}
			if share.Importers == nil {
				share.Importers = map[string]string{}
			}
			if _, exists := share.Importers[ownerID]; !exists {
				cloned := make(map[string]string, len(share.Importers)+1)
				for id, at := range share.Importers {
					cloned[id] = at
				}
				cloned[ownerID] = time.Now().UTC().Format(time.RFC3339)
				share.Importers = cloned
				durable = true
			} else {
				share.Importers[ownerID] = time.Now().UTC().Format(time.RFC3339)
			}
		} else if !companionShareAllowsGuest(share.Visibility) {
			companionDocShares.Unlock()
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "指定分享和群组分享只能在文档所在的 Hub 打开")
			return
		} else {
			_, _, known := companionShareGuestByGrant(share, grant)
			if !known && share.Visibility == "private" && !companionSharePasswordOK(share, body.Password) {
				companionDocShares.Unlock()
				if strings.TrimSpace(body.Password) == "" {
					writeError(w, http.StatusUnauthorized, "SHARE_PASSWORD", "需要分享密码")
					return
				}
				writeError(w, http.StatusForbidden, "SHARE_PASSWORD", "分享密码不正确")
				return
			}
			if !known {
				var err error
				grant, err = newCompanionReaderGrant()
				if err != nil {
					companionDocShares.Unlock()
					writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法创建分享")
					return
				}
			}
			var changed bool
			share, changed = companionShareRememberGuest(share, grant, body.ReaderLabel)
			durable = changed
			issuedGrant = grant
		}
		companionDocShares.items[token] = share
		companionShareDisk.mu.Lock()
		if companionShareDisk.dirty {
			durable = true
		}
		companionShareDisk.mu.Unlock()
		var snap companionShareIndexSnapshot
		if durable {
			snap = cloneCompanionShareIndexLocked()
		}
		blobPath := share.BlobPath
		name := share.Name
		owner := share.OwnerLabel
		expires := ""
		if !share.ExpiresAt.IsZero() {
			expires = share.ExpiresAt.UTC().Format(time.RFC3339)
		}
		visibility := share.Visibility
		revision := share.Revision
		companionDocShares.Unlock()
		// The file is the part the reader is waiting on. The index copy is
		// already taken; marshaling and writing it continue after this
		// response so a small document is not stuck behind that rewrite.
		scheduleCompanionShareIndex(snap)
		file, size, err := mobileOpenDocumentBlob(blobPath)
		if err != nil || size <= 0 {
			if file != nil {
				_ = file.Close()
			}
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享文件已经不在")
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.Header().Set("Content-Disposition", companionShareDisposition(name))
		w.Header().Set("X-Companion-Document-Name", name)
		w.Header().Set("X-Companion-Document-Owner", owner)
		w.Header().Set("X-Companion-Document-Expires", expires)
		w.Header().Set("X-Companion-Document-Visibility", visibility)
		w.Header().Set("X-Companion-Document-Revision", strconv.Itoa(revision))
		if companionReaderGrantPattern.MatchString(issuedGrant) {
			w.Header().Set(companionReaderGrantHeader, issuedGrant)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, file)
	}
}

// companionShareSyncRejection is why this caller cannot write the project.
// A share that still exists after the host seat moved must stay a forbidden
// update. Calling it missing makes the desktop retire the row.
func companionShareSyncRejection(share companionDocShare, found bool, userID string, now time.Time) (status int, code, message string) {
	if !found {
		return http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在"
	}
	if share.expired(now) {
		return http.StatusGone, "SHARE_EXPIRED", "分享链接已过期"
	}
	if !companionShareIsHost(share, userID) {
		return http.StatusForbidden, "SHARE_FORBIDDEN", "只有主持人可以同步更新"
	}
	return 0, "", ""
}

// CompanionDocumentShareSyncHandler replaces the project and bumps the revision.
// Only the current host can push new highlights or chat.
//
//	POST /api/mobile/documents/shares/{token}/sync
func CompanionDocumentShareSyncHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, int64(mobileDocumentOriginalMaxBytes)+1<<20)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "文件过大")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择要分享的文件")
			return
		}
		defer file.Close()
		raw, err := readCompanionShareUpload(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, ok := companionDocShares.items[token]
		if !ok {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if status, code, message := companionShareSyncRejection(share, true, ownerID, time.Now().UTC()); status != 0 {
			companionDocShares.Unlock()
			writeError(w, status, code, message)
			return
		}
		companionDocShares.Unlock()
		blobPath, err := mobileWriteDocumentBlob(ownerID, "share", token, raw)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享文件")
			return
		}
		companionDocShares.Lock()
		share, ok = companionDocShares.items[token]
		if status, code, message := companionShareSyncRejection(share, ok, ownerID, time.Now().UTC()); status != 0 {
			companionDocShares.Unlock()
			// A delete that landed while this sync was writing must not leave
			// the bytes it just wrote behind as an orphan. Losing the host
			// seat is not a delete: the desktop retires a row only on 404.
			mobileDeleteDocumentBlob(blobPath)
			writeError(w, status, code, message)
			return
		}
		previous := share
		share.BlobPath = blobPath
		share.Size = int64(len(raw))
		share.Revision++
		companionDocShares.items[token] = share
		err = persistCompanionDocSharesLocked()
		if err != nil {
			companionDocShares.items[token] = previous
			companionDocShares.Unlock()
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存分享")
			return
		}
		companionDocShares.Unlock()
		view := companionShareView(share, r)
		// The owner is syncing their own link; keep the audience fields the
		// desktop row needs to prefill 修改 after the row is rewritten.
		if share.Target != "" {
			view["target"] = share.Target
		}
		if share.GroupID != "" {
			view["group_id"] = share.GroupID
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func companionShareAttachNotes(view map[string]any, share companionDocShare) {
	view["notes"] = companionShareNotesJSON(share.Notes, share)
	view["notes_revision"] = share.NotesRevision
}

// companionShareNoteIsHost reports whether a note came from the host seat. The
// note carries the seat it was written from, so a line the admin wrote before
// handing the seat over stays a host line and is not answered again. Notes
// written before Host existed only carry AuthorID; the owner was the host then,
// and an owner line counts as the host's only while the owner still holds the
// seat — after a transfer the admin's new lines are a reader's.
func companionShareNoteIsHost(note companionShareNote, share companionDocShare) bool {
	if note.Host {
		return true
	}
	if strings.TrimSpace(note.AuthorID) == "" {
		return false
	}
	if strings.TrimSpace(share.HostID) != "" {
		return false
	}
	return note.AuthorID == strings.TrimSpace(share.OwnerID)
}

func companionShareNoteByID(notes []companionShareNote, noteID string) (companionShareNote, bool) {
	for _, note := range notes {
		if note.ID == noteID {
			return note, true
		}
	}
	return companionShareNote{}, false
}

// companionShareNoteDecision is the review state of a reader note. A host note
// has none. An older note without a decision keeps its pre-review behavior
// only when it has already been answered; otherwise the host still reviews it.
func companionShareNoteDecision(note companionShareNote, share companionDocShare, answered map[string]bool) string {
	if companionShareNoteIsHost(note, share) {
		return ""
	}
	switch strings.TrimSpace(note.Decision) {
	case "pending", "approved", "ignored":
		return strings.TrimSpace(note.Decision)
	}
	if answered[note.ID] {
		return "approved"
	}
	return "pending"
}

func companionShareAnsweredNoteIDs(notes []companionShareNote, share companionDocShare) map[string]bool {
	answered := map[string]bool{}
	for _, note := range notes {
		if strings.TrimSpace(note.ReplyTo) == "" || !companionShareNoteIsHost(note, share) {
			continue
		}
		answered[strings.TrimSpace(note.ReplyTo)] = true
	}
	return answered
}

func companionShareNotesJSON(notes []companionShareNote, share companionDocShare) []map[string]any {
	answered := companionShareAnsweredNoteIDs(notes, share)
	out := make([]map[string]any, 0, len(notes))
	for _, note := range notes {
		if strings.TrimSpace(note.ID) == "" || strings.TrimSpace(note.Text) == "" {
			continue
		}
		row := map[string]any{
			"id":         note.ID,
			"author":     note.Author,
			"text":       note.Text,
			"created_at": note.CreatedAt.UTC().Format(time.RFC3339),
			"host":       companionShareNoteIsHost(note, share),
		}
		if note.ReplyTo != "" {
			row["reply_to"] = note.ReplyTo
		}
		if decision := companionShareNoteDecision(note, share, answered); decision != "" {
			row["decision"] = decision
		}
		if !note.DecidedAt.IsZero() {
			row["decided_at"] = note.DecidedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out
}

func cloneCompanionShareNotes(notes []companionShareNote) []companionShareNote {
	if len(notes) == 0 {
		return nil
	}
	out := make([]companionShareNote, len(notes))
	copy(out, notes)
	return out
}

func cleanCompanionShareNote(text string) (string, string) {
	text = strings.TrimSpace(text)
	text = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	n := len([]rune(text))
	if n == 0 {
		return "", "请填写批注"
	}
	if n > 2000 {
		return "", "批注过长"
	}
	return text, ""
}

func newCompanionShareNoteID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cn_" + hex.EncodeToString(buf), nil
}

// CompanionDocumentShareNotesHandler records one reader's discussion.
// The host transcript stays in the project. A note is visible to everyone
// who can already see the share, including the host.
//
//	POST /api/mobile/documents/shares/{token}/notes
func CompanionDocumentShareNotesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionViewerIfPresent(w, r, identity)
		if !ok {
			return
		}
		grant := companionReaderGrantHeaderValue(r)
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var body struct {
			Text    string `json:"text"`
			ReplyTo string `json:"reply_to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写批注")
			return
		}
		text, message := cleanCompanionShareNote(body.Text)
		if message != "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", message)
			return
		}
		replyTo := strings.TrimSpace(body.ReplyTo)
		noteID, err := newCompanionShareNoteID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存批注")
			return
		}
		var contacts map[string]struct{}
		var groups map[string]struct{}
		if ownerID != "" {
			contacts = companionViewerContactSet(identity, r.Context(), principal)
			groups = companionAcceptedGroupIDs(ownerID, contacts)
		}
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, found := companionDocShares.items[token]
		if !found {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.expired(time.Now().UTC()) {
			companionDocShares.Unlock()
			writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
			return
		}
		guestLabel := ""
		guestHash := ""
		if ownerID == "" {
			var guestOK bool
			guestLabel, guestHash, guestOK = companionShareGuestByGrant(share, grant)
			if !guestOK || !companionShareAllowsGuest(share.Visibility) {
				companionDocShares.Unlock()
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请重新导入分享链接")
				return
			}
		} else if status, accessMessage := companionShareReadAccess(share, ownerID, contacts, groups, ""); status != 0 {
			companionDocShares.Unlock()
			code := "SHARE_FORBIDDEN"
			if status == http.StatusUnauthorized {
				code = "UNAUTHORIZED"
			}
			writeError(w, status, code, accessMessage)
			return
		}
		previous := share
		previous.Notes = cloneCompanionShareNotes(share.Notes)
		hostID := companionShareHostID(share)
		isHost := ownerID != "" && hostID == ownerID
		if replyTo != "" {
			// Only the host answers a reader note. A reader's own line is a new
			// discussion entry, not an answer, and every answer names the note
			// it replies to so no machine answers the same note twice.
			if !isHost {
				companionDocShares.Unlock()
				writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有主持人可以回复批注")
				return
			}
			for _, existing := range share.Notes {
				if existing.ReplyTo != replyTo || !companionShareNoteIsHost(existing, share) {
					continue
				}
				// A second host machine, or a retry after a dropped response,
				// must not add a second answer to the same reader note. This
				// check runs before the target lookup: the question may already
				// have left the capped list while the answer is still there.
				notes := companionShareNotesJSON(share.Notes, share)
				revision := share.NotesRevision
				companionDocShares.Unlock()
				writeJSON(w, http.StatusOK, map[string]any{
					"notes":          notes,
					"notes_revision": revision,
				})
				return
			}
			target, found := companionShareNoteByID(share.Notes, replyTo)
			if !found {
				companionDocShares.Unlock()
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "被回复的批注不存在")
				return
			}
			if companionShareNoteIsHost(target, share) {
				companionDocShares.Unlock()
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "只能回复读者的批注")
				return
			}
			switch companionShareNoteDecision(target, share, companionShareAnsweredNoteIDs(share.Notes, share)) {
			case "pending":
				companionDocShares.Unlock()
				writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "批注还没有通过审核，不能回复")
				return
			case "ignored":
				companionDocShares.Unlock()
				writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "批注已被忽略，不能回复")
				return
			}
		}
		authorID := ownerID
		author := companionShareOwnerLabel(principal)
		if ownerID == "" {
			authorID = "guest:" + guestHash
			author = guestLabel
		}
		note := companionShareNote{
			ID:        noteID,
			AuthorID:  authorID,
			Author:    author,
			Text:      text,
			CreatedAt: time.Now().UTC(),
			Host:      isHost,
			ReplyTo:   replyTo,
		}
		if !isHost {
			// A reader line waits for the host. The host agent only picks up an
			// approved note, so an unsafe or off-topic line never reaches it.
			note.Decision = "pending"
		}
		share.Notes = append(cloneCompanionShareNotes(share.Notes), note)
		if len(share.Notes) > 80 {
			share.Notes = append([]companionShareNote(nil), share.Notes[len(share.Notes)-80:]...)
		}
		share.NotesRevision++
		companionDocShares.items[token] = share
		if err := persistCompanionDocSharesLocked(); err != nil {
			companionDocShares.items[token] = previous
			companionDocShares.Unlock()
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存批注")
			return
		}
		companionDocShares.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"notes":          companionShareNotesJSON(share.Notes, share),
			"notes_revision": share.NotesRevision,
		})
	}
}

func companionShareNoteDecisionFromBody(value string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "approve", "approved", "pass", "通过":
		return "approved", true
	case "ignore", "ignored", "dismiss", "忽略":
		return "ignored", true
	default:
		return "", false
	}
}

// CompanionDocumentShareNoteReviewHandler lets the host pass or ignore one
// reader note. Nothing reaches the host agent until the note is approved.
//
//	POST /api/mobile/documents/shares/{token}/notes/{noteID}/review
func CompanionDocumentShareNoteReviewHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		noteID := strings.TrimSpace(r.PathValue("noteID"))
		if noteID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "批注不存在")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var body struct {
			Decision string `json:"decision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择通过或忽略")
			return
		}
		decision, valid := companionShareNoteDecisionFromBody(body.Decision)
		if !valid {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择通过或忽略")
			return
		}
		contacts := companionViewerContactSet(identity, r.Context(), principal)
		groups := companionAcceptedGroupIDs(ownerID, contacts)
		loadCompanionDocShares()
		companionDocShares.Lock()
		share, found := companionDocShares.items[token]
		if !found {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.expired(time.Now().UTC()) {
			companionDocShares.Unlock()
			writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
			return
		}
		if status, accessMessage := companionShareReadAccess(share, ownerID, contacts, groups, ""); status != 0 {
			companionDocShares.Unlock()
			code := "SHARE_FORBIDDEN"
			if status == http.StatusUnauthorized {
				code = "UNAUTHORIZED"
			}
			writeError(w, status, code, accessMessage)
			return
		}
		if companionShareHostID(share) != ownerID {
			companionDocShares.Unlock()
			writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有主持人可以审核批注")
			return
		}
		index := -1
		for i := range share.Notes {
			if share.Notes[i].ID == noteID {
				index = i
				break
			}
		}
		if index < 0 {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "NOTE_NOT_FOUND", "批注不存在")
			return
		}
		target := share.Notes[index]
		if companionShareNoteIsHost(target, share) {
			companionDocShares.Unlock()
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "主持人自己的批注不需要审核")
			return
		}
		if companionShareAnsweredNoteIDs(share.Notes, share)[noteID] {
			companionDocShares.Unlock()
			writeError(w, http.StatusConflict, "NOTE_ANSWERED", "批注已经回答，不能再审核")
			return
		}
		if strings.TrimSpace(target.Decision) != decision {
			previous := share
			previous.Notes = cloneCompanionShareNotes(share.Notes)
			share.Notes[index].Decision = decision
			share.Notes[index].DecidedAt = time.Now().UTC()
			share.NotesRevision++
			companionDocShares.items[token] = share
			if err := persistCompanionDocSharesLocked(); err != nil {
				companionDocShares.items[token] = previous
				companionDocShares.Unlock()
				writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存审核结果")
				return
			}
		}
		notes := companionShareNotesJSON(share.Notes, share)
		revision := share.NotesRevision
		companionDocShares.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"notes":          notes,
			"notes_revision": revision,
		})
	}
}

func companionShareDisposition(name string) string {
	if name == "" {
		name = "document"
	}
	ascii := true
	for _, r := range name {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			ascii = false
			break
		}
	}
	if ascii {
		return `attachment; filename="` + name + `"`
	}
	return "attachment; filename*=UTF-8''" + urlPathEscape(name)
}

func urlPathEscape(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 128 && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			b.WriteRune(r)
			continue
		}
		buf := make([]byte, 4)
		n := copy(buf, []byte(string(r)))
		for _, c := range buf[:n] {
			b.WriteString("%")
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

// CompanionDocumentSharePageHandler is the page behind a share link.
//
//	GET /hub/documents/shares/{token}
func CompanionDocumentSharePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// A share page is a public URL. Keep it out of caches and referrers so a
		// private link's address does not leak through them.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !companionShareTokenPattern.MatchString(token) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(companionSharePage(companionSharePageError{
				Title:   "分享链接不存在",
				Message: "这个链接不是伴读文档分享。",
			})))
			return
		}
		share, ok := companionShareByToken(token)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(companionSharePage(companionSharePageError{
				Title:   "分享链接不存在",
				Message: "链接无效，或分享已经取消。",
			})))
			return
		}
		if share.expired(time.Now().UTC()) {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(companionSharePage(companionSharePageError{
				Title:   "分享链接已过期",
				Message: share.Name + " 的分享时间已经结束。",
			})))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(companionSharePage(companionSharePageData{
			Title:      share.Name,
			Name:       share.Name,
			OwnerLabel: share.OwnerLabel,
			SizeLabel:  companionShareSizeLabel(share.Size),
			When:       companionShareWhenLabel(share.ExpiresAt),
			Access:     companionShareAccessLabel(share.Visibility),
			Kind:       companionShareKindLabel(share.Name),
			Notes:      len(share.Notes),
			Revision:   share.Revision,
			CreatedAt:  share.CreatedAt,
		})))
	}
}

// companionSharePageData is what the share page renders. Fields are plain
// strings so the template stays a pure view.
type companionSharePageData struct {
	Title      string
	Name       string
	OwnerLabel string
	SizeLabel  string
	When       string
	Access     string
	Kind       string
	Notes      int
	Revision   int
	CreatedAt  time.Time
}

type companionSharePageError struct {
	Title   string
	Message string
}

func companionShareSizeLabel(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1024:
		return strconv.FormatInt(size, 10) + " B"
	case size < 1024*1024:
		return strconv.FormatInt(size/1024, 10) + " KB"
	default:
		return strconv.FormatFloat(float64(size)/(1024*1024), 'f', 1, 64) + " MB"
	}
}

func companionShareWhenLabel(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return "永久有效"
	}
	return "有效期至 " + expiresAt.UTC().Format("2006-01-02")
}

func companionShareAccessLabel(visibility string) string {
	switch visibility {
	case "private":
		return "私密分享"
	case "person":
		return "指定分享"
	case "group":
		return "群组分享"
	default:
		return "完全公开"
	}
}

// companionShareKindLabel reads the extension off the file name for the
// document badge. Unknown kinds degrade to a generic label.
func companionShareKindLabel(name string) string {
	base := strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	dot := strings.LastIndex(base, ".")
	if dot < 0 || dot == len(base)-1 {
		return "文档"
	}
	ext := strings.ToUpper(base[dot+1:])
	if len([]rune(ext)) > 5 {
		return "文档"
	}
	return ext
}

// companionSharePage renders the public share page. It is one self-contained
// document: the reader lands here from a chat or a mail client, so it must look
// finished without any other asset, and it must work at phone width.
func companionSharePage(data any) string {
	var title, inner string
	switch value := data.(type) {
	case companionSharePageError:
		title = value.Title
		inner = companionSharePageErrorBody(value)
	case companionSharePageData:
		title = value.Title
		inner = companionSharePageShareBody(value)
	default:
		title = "伴读文档分享"
	}
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<meta name="color-scheme" content="light dark">
<title>` + html.EscapeString(title) + ` · 伴读文档分享</title>
<style>
:root{
  --bg:#f4f6fa;--card:#ffffff;--text:#1b2430;--text-2:#55637a;--text-3:#7b8899;
  --border:#e2e8f1;--border-2:#d3dcea;--primary:#2f6fbc;--primary-soft:#eaf2fd;
  --ok:#1a7f37;--ok-soft:#eaf6ee;--radius:14px;
  --shadow:0 1px 2px rgba(23,43,77,.05),0 10px 30px -12px rgba(23,43,77,.16);
}
@media (prefers-color-scheme: dark){
  :root{
    --bg:#111823;--card:#18202c;--text:#e9eef6;--text-2:#a8b4c5;--text-3:#8492a5;
    --border:#26303e;--border-2:#33404f;--primary:#6aa6ee;--primary-soft:#1b2a3d;
    --ok:#69c98a;--ok-soft:#182c20;
    --shadow:0 1px 2px rgba(0,0,0,.3),0 12px 32px -14px rgba(0,0,0,.6);
  }
}
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{
  min-height:100vh;background:var(--bg);color:var(--text);
  font:15px/1.6 "Segoe UI","SF Pro Text","PingFang SC","Microsoft YaHei",system-ui,sans-serif;
  -webkit-font-smoothing:antialiased;
  display:flex;flex-direction:column;align-items:center;
  padding:32px 16px 40px;
}
.wrap{width:100%;max-width:620px}
.brand{display:flex;align-items:center;gap:8px;margin:0 0 18px 2px;color:var(--text-3);font-size:13px;font-weight:600}
.brand svg{width:18px;height:18px;color:var(--primary);flex:0 0 auto}
.card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);box-shadow:var(--shadow);overflow:hidden}
.head{padding:22px 24px 20px;border-bottom:1px solid var(--border)}
.kicker{display:flex;align-items:center;gap:8px;margin:0 0 10px;font-size:12px;font-weight:600;color:var(--text-3);letter-spacing:.03em}
.kicker .dot{width:4px;height:4px;border-radius:50%;background:currentColor;opacity:.5}
h1{margin:0;font-size:21px;line-height:1.35;font-weight:650;letter-spacing:-.01em;overflow-wrap:anywhere}
.meta{display:flex;flex-wrap:wrap;align-items:center;gap:6px;margin:12px 0 0}
.badge{display:inline-flex;align-items:center;gap:5px;height:24px;padding:0 9px;border-radius:999px;background:var(--bg);border:1px solid var(--border-2);color:var(--text-2);font-size:12px;font-weight:600;white-space:nowrap}
.badge.kind{background:var(--primary-soft);border-color:transparent;color:var(--primary)}
.badge.ok{background:var(--ok-soft);border-color:transparent;color:var(--ok)}
.body{padding:20px 24px}
dl.rows{margin:0}
.row{display:flex;gap:14px;padding:9px 0;border-bottom:1px solid var(--border);font-size:14px}
.row:last-child{border-bottom:0}
.row dt{flex:0 0 76px;margin:0;color:var(--text-3);font-weight:500}
.row dd{margin:0;color:var(--text);min-width:0;overflow-wrap:anywhere}
.owner{font-weight:600}
.steps{margin:18px 0 0;padding:0;list-style:none;counter-reset:step}
.steps li{position:relative;padding:0 0 0 34px;margin:0 0 12px;color:var(--text-2);font-size:14px}
.steps li:last-child{margin-bottom:0}
.steps li::before{counter-increment:step;content:counter(step);position:absolute;left:0;top:1px;width:22px;height:22px;border-radius:50%;background:var(--primary-soft);color:var(--primary);font-size:12px;font-weight:700;display:flex;align-items:center;justify-content:center}
.steps b{color:var(--text);font-weight:600}
.link{display:flex;align-items:center;gap:10px;margin:18px 0 0;padding:11px 12px;border:1px solid var(--border-2);border-radius:10px;background:var(--bg);font:500 12.5px/1.5 ui-monospace,"Cascadia Mono",Consolas,monospace;color:var(--text-2);overflow-wrap:anywhere;word-break:break-all}
.foot{padding:14px 24px 16px;border-top:1px solid var(--border);background:var(--bg);color:var(--text-3);font-size:12px}
.foot p{margin:0}
.error{text-align:center;padding:44px 24px}
.error .mark{width:52px;height:52px;margin:0 auto 16px;border-radius:50%;background:var(--primary-soft);color:var(--primary);display:flex;align-items:center;justify-content:center}
.error .mark svg{width:26px;height:26px}
.error h1{font-size:19px}
.error p{margin:10px 0 0;color:var(--text-2);font-size:14px;overflow-wrap:anywhere}
@media (max-width:480px){
  body{padding:20px 12px 28px}
  .head,.body,.foot{padding-left:18px;padding-right:18px}
  h1{font-size:19px}
  .row{flex-direction:column;gap:2px}
  .row dt{flex:none}
}
</style>
</head>
<body>
<div class="wrap">
  <p class="brand">
    <svg viewBox="0 0 80 80" aria-hidden="true" focusable="false"><path d="M14 58V22l26 25 26-25v36" fill="none" stroke="currentColor" stroke-width="10.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <span>伴读文档分享</span>
  </p>
  <main class="card">
` + inner + `
  </main>
</div>
</body>
</html>`
}

func companionSharePageShareBody(data companionSharePageData) string {
	access := `<span class="badge">` + html.EscapeString(data.Access) + `</span>`
	if data.Access == "完全公开" {
		access = `<span class="badge ok">` + html.EscapeString(data.Access) + `</span>`
	}
	rows := `<div class="row"><dt>分享者</dt><dd class="owner">` + html.EscapeString(data.OwnerLabel) + `</dd></div>`
	if data.SizeLabel != "" {
		rows += `<div class="row"><dt>大小</dt><dd>` + html.EscapeString(data.SizeLabel) + `</dd></div>`
	}
	rows += `<div class="row"><dt>有效期</dt><dd>` + html.EscapeString(data.When) + `</dd></div>`
	rows += `<div class="row"><dt>可见范围</dt><dd>` + html.EscapeString(data.Access) + `</dd></div>`
	if data.Notes > 0 {
		rows += `<div class="row"><dt>读者批注</dt><dd>` + strconv.Itoa(data.Notes) + ` 条讨论</dd></div>`
	}
	if data.Revision > 1 {
		rows += `<div class="row"><dt>版本</dt><dd>第 ` + strconv.Itoa(data.Revision) + ` 版，分享者已同步新标注</dd></div>`
	}
	if !data.CreatedAt.IsZero() {
		rows += `<div class="row"><dt>分享于</dt><dd>` + data.CreatedAt.UTC().Format("2006-01-02") + `</dd></div>`
	}
	return `<header class="head">
      <p class="kicker"><span>伴读项目</span><span class="dot"></span><span>文档 · 高亮 · 讨论</span></p>
      <h1>` + html.EscapeString(data.Name) + `</h1>
      <div class="meta">
        <span class="badge kind">` + html.EscapeString(data.Kind) + `</span>
        ` + access + `
        <span class="badge">` + html.EscapeString(data.When) + `</span>
      </div>
    </header>
    <section class="body">
      <dl class="rows">
        ` + rows + `
      </dl>
      <ol class="steps">
        <li>在 <b>MaClaw 伴读</b> 打开「<b>文档管理</b>」。</li>
        <li>点击「<b>导入分享</b>」，粘贴本页地址。</li>
        <li>导入后，文档、高亮和讨论会一起存进你的文档库。</li>
      </ol>
      <p class="link" id="share-link"></p>
    </section>
    <footer class="foot">
      <p>这份分享来自伴读，包含文档、你的高亮和聊天记录。</p>
    </footer>
    <script>
    (function(){
      var el = document.getElementById("share-link");
      if (!el) return;
      el.textContent = location.href;
      var copy = document.createElement("button");
      copy.type = "button";
      copy.textContent = "复制链接";
      copy.style.cssText = "margin-left:auto;flex:0 0 auto;height:26px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--card);color:var(--text-2);font:600 12px/1 'Segoe UI',system-ui,sans-serif;cursor:pointer";
      copy.addEventListener("click", function(){
        var done = function(){ copy.textContent = "已复制"; setTimeout(function(){ copy.textContent = "复制链接"; }, 1600); };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(location.href).then(done, function(){ copy.textContent = "请手动复制"; });
        } else {
          copy.textContent = "请手动复制";
        }
      });
      el.appendChild(copy);
    })();
    </script>`
}

func companionSharePageErrorBody(data companionSharePageError) string {
	return `<div class="error">
      <div class="mark">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="8.4"/><path d="M12 8.2v4.6"/><path d="M12 16.1v.1"/></svg>
      </div>
      <h1>` + html.EscapeString(data.Title) + `</h1>
      <p>` + html.EscapeString(data.Message) + `</p>
    </div>`
}
