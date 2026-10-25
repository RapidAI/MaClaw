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
)

// FileCompanionLibraryItem is one row in 文档管理.
type FileCompanionLibraryItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	Source     string `json:"source"`
	OpenedAt   string `json:"openedAt,omitempty"`
	Owner      string `json:"owner,omitempty"`
	ShareURL   string `json:"shareUrl,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Permanent  bool   `json:"permanent,omitempty"`
	Mine       bool   `json:"mine,omitempty"`
	Token      string `json:"token,omitempty"`
	Revision   int    `json:"revision,omitempty"`
	Target     string `json:"target,omitempty"`
	GroupID    string `json:"groupId,omitempty"`
	// Notes are other readers' discussion on this share. The host transcript
	// stays in Messages. NotesKnown is set when this list actually carried
	// the notes field, so an older hub does not wipe notes already stored.
	Notes         []FileCompanionShareNote `json:"notes,omitempty"`
	NotesRevision int                      `json:"notesRevision,omitempty"`
	NotesKnown    bool                     `json:"-"`
	// Highlights is set on an import that unpacked a companion project.
	// It is not stored in the library file.
	Highlights string `json:"highlights,omitempty"`
}

// FileCompanionShareNote is one reader's discussion on a shared paper.
type FileCompanionShareNote struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// FileCompanionLibraryList is the local history plus cloud shares.
type FileCompanionLibraryList struct {
	Items   []FileCompanionLibraryItem `json:"items"`
	Notice  string                     `json:"notice,omitempty"`
	Updates []FileCompanionShareUpdate `json:"updates,omitempty"`
	Removed []string                   `json:"removed,omitempty"`
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
// Highlights and messages replace what that path had on screen.
type FileCompanionShareUpdate struct {
	Path       string                 `json:"path"`
	Token      string                 `json:"token"`
	Name       string                 `json:"name"`
	Revision   int                    `json:"revision"`
	Highlights string                 `json:"highlights,omitempty"`
	Messages   []FileCompanionMessage `json:"messages"`
	Project    bool                   `json:"project"`
}

type fileCompanionShareView struct {
	Token         string                   `json:"token"`
	Name          string                   `json:"name"`
	Size          int64                    `json:"size"`
	Owner         string                   `json:"owner"`
	Visibility    string                   `json:"visibility"`
	Permanent     bool                     `json:"permanent"`
	Expired       bool                     `json:"expired"`
	URL           string                   `json:"url"`
	CreatedAt     string                   `json:"created_at"`
	ExpiresAt     string                   `json:"expires_at"`
	Revision      int                      `json:"revision"`
	Target        string                   `json:"target"`
	GroupID       string                   `json:"group_id"`
	Notes         *[]fileCompanionNoteWire `json:"notes"`
	NotesRevision *int                     `json:"notes_revision"`
}

type fileCompanionNoteWire struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Created   string `json:"createdAt"`
}

type fileCompanionShareStatus struct {
	status  int
	message string
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
	hub, viewer, loginErr := a.fileCompanionHubLogin()
	if loginErr != nil {
		notice = "登录后可以查看云端分享"
	} else {
		// One refresh at a time. A second open of 文档管理 waits, then sees
		// the revision the first refresh already stored, instead of downloading
		// the same project twice and writing an older library over the newer one.
		fileCompanionLibraryRefreshMu.Lock()
		remote, err := a.fetchFileCompanionShares(hub, viewer)
		if err != nil {
			fileCompanionLibraryRefreshMu.Unlock()
			var status *fileCompanionShareStatus
			if errors.As(err, &status) {
				notice = status.Error()
			} else {
				notice = "暂时无法连接云端，本机文档仍可查看"
			}
		} else {
			fileCompanionLibraryMu.Lock()
			fresh, freshOK := a.readFileCompanionLibraryLocked()
			if freshOK {
				items = fresh
			}
			previous := map[string]FileCompanionLibraryItem{}
			for _, item := range items {
				if item.Source == "cloud" && item.Token != "" {
					previous[item.Token] = item
				}
			}
			items = mergeFileCompanionShares(items, remote, hub)
			fileCompanionLibraryMu.Unlock()
			removedTokens := map[string]bool{}
			removed = a.dropRevokedGroupCopies(previous, remote, removedTokens)
			updates = a.pullNewerFileCompanionShares(hub, viewer, items, previous)
			fileCompanionLibraryMu.Lock()
			if fresh, freshOK = a.readFileCompanionLibraryLocked(); freshOK {
				items = reconcileFileCompanionLibrary(fresh, items, removedTokens)
			}
			_ = a.writeFileCompanionLibraryLocked(items)
			fileCompanionLibraryMu.Unlock()
			fileCompanionLibraryRefreshMu.Unlock()
		}
	}
	if items == nil {
		items = []FileCompanionLibraryItem{}
	}
	return FileCompanionLibraryList{Items: items, Notice: notice, Updates: updates, Removed: removed}, nil
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

func mergeFileCompanionShares(items, remote []FileCompanionLibraryItem, hub string) []FileCompanionLibraryItem {
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
		if item.Token == "" || seen[item.Token] {
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
		if item.ShareURL == "" {
			item.ShareURL = strings.TrimRight(hub, "/") + "/hub/documents/shares/" + item.Token
		}
		kept = append(kept, item)
	}
	for token, prev := range previous {
		if seen[token] || prev.Visibility == "group" || !fileCompanionLibraryFileExists(prev.Path) {
			continue
		}
		kept = append(kept, prev)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].OpenedAt > kept[j].OpenedAt
	})
	return trimFileCompanionLibrary(kept, fileCompanionLibraryCap)
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
					item = updated
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

func (a *App) fetchFileCompanionShares(hub, viewer string) ([]FileCompanionLibraryItem, error) {
	req, err := http.NewRequest(http.MethodGet, hub+"/api/mobile/documents/shares", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	var payload struct {
		Mine     []fileCompanionShareView `json:"mine"`
		Received []fileCompanionShareView `json:"received"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, errors.New("云端分享记录无法读取")
	}
	out := make([]FileCompanionLibraryItem, 0, len(payload.Mine)+len(payload.Received))
	out = append(out, libraryItemsFromShares(payload.Mine, true, hub)...)
	out = append(out, libraryItemsFromShares(payload.Received, false, hub)...)
	return out, nil
}

func libraryItemsFromShares(views []fileCompanionShareView, mine bool, hub string) []FileCompanionLibraryItem {
	out := make([]FileCompanionLibraryItem, 0, len(views))
	for _, view := range views {
		token := strings.TrimSpace(view.Token)
		if !fileCompanionShareTokenMatch.MatchString(token) || view.Expired {
			continue
		}
		item := FileCompanionLibraryItem{
			ID:         token,
			Token:      token,
			Name:       strings.TrimSpace(view.Name),
			Source:     "cloud",
			OpenedAt:   strings.TrimSpace(view.CreatedAt),
			Owner:      strings.TrimSpace(view.Owner),
			ShareURL:   strings.TrimSpace(view.URL),
			ExpiresAt:  strings.TrimSpace(view.ExpiresAt),
			Visibility: strings.TrimSpace(view.Visibility),
			Size:       view.Size,
			Permanent:  view.Permanent || strings.TrimSpace(view.ExpiresAt) == "",
			Mine:       mine,
			Revision:   view.Revision,
			Target:     strings.TrimSpace(view.Target),
			GroupID:    strings.TrimSpace(view.GroupID),
			Notes:      notesFromCompanionShareView(view),
			NotesKnown: view.Notes != nil,
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
	view, err := postFileCompanionShare(hub, viewer, name, visibility, password, target, days, file)
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	item := libraryItemsFromShares([]fileCompanionShareView{view}, true, hub)
	if len(item) == 0 {
		return FileCompanionShareResult{}, errors.New("云端没有返回分享链接")
	}
	item[0].Path = canonical
	item[0].OpenedAt = time.Now().UTC().Format(time.RFC3339)
	a.upsertFileCompanionCloud(item[0])
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

func postFileCompanionShare(hub, viewer, name, visibility, password, target string, days int, raw io.Reader) (fileCompanionShareView, error) {
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
		return fileCompanionShareView{}, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	var view fileCompanionShareView
	if json.Unmarshal(body, &view) != nil || !fileCompanionShareTokenMatch.MatchString(view.Token) {
		return fileCompanionShareView{}, errors.New("云端没有返回分享链接")
	}
	return view, nil
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
	payload, _ := json.Marshal(map[string]string{"password": strings.TrimSpace(password)})
	req, err := http.NewRequest(http.MethodPost, hub+"/api/mobile/documents/shares/"+token+"/import", bytes.NewReader(payload))
	if err != nil {
		return FileCompanionLibraryItem{}, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
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
	dest, highlights, _, project, err := a.materializeFileCompanionShare(token, name, raw)
	if err != nil {
		return FileCompanionLibraryItem{}, err
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
	item := FileCompanionLibraryItem{
		ID:         token,
		Token:      token,
		Name:       name,
		Path:       dest,
		Source:     "cloud",
		OpenedAt:   time.Now().UTC().Format(time.RFC3339),
		Owner:      strings.TrimSpace(resp.Header.Get("X-Companion-Document-Owner")),
		ShareURL:   shareURL,
		ExpiresAt:  expires,
		Visibility: visibility,
		Size:       int64(len(raw)),
		Permanent:  expires == "",
		Mine:       false,
		Revision:   revision,
	}
	if project {
		item.Highlights = string(highlights)
	}
	stored := item
	stored.Highlights = ""
	a.upsertFileCompanionCloud(stored)
	return item, nil
}

func (a *App) saveFileCompanionShareBody(token, name string, body io.Reader) (string, int64, error) {
	dir := filepath.Join(a.GetDataDir(), "file-companion", "library-files", token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	dest := filepath.Join(dir, name)
	tmp := dest + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	n, copyErr := io.Copy(file, io.LimitReader(body, mobileDocumentOriginalMaxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", 0, errors.New("文件无法读取")
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", 0, closeErr
	}
	if n > mobileDocumentOriginalMaxBytes {
		_ = os.Remove(tmp)
		return "", 0, errors.New("文件过大")
	}
	if n == 0 {
		_ = os.Remove(tmp)
		return "", 0, errors.New("文件是空的")
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return dest, n, nil
}

func (a *App) upsertFileCompanionCloud(item FileCompanionLibraryItem) {
	if a == nil || item.Token == "" {
		return
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return
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
	_ = a.writeFileCompanionLibraryLocked(trimFileCompanionLibrary(kept, fileCompanionLibraryCap))
}

// dropFileCompanionCloudToken removes one cloud row. The local file stays:
// an expired share is not the reader's own paper.
func (a *App) dropFileCompanionCloudToken(token string) {
	if a == nil || token == "" {
		return
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
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
