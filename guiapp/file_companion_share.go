package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func normalizeFileCompanionContact(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		if strings.ContainsAny(value, " \t") || strings.Count(value, "@") != 1 {
			return ""
		}
		local, domain, _ := strings.Cut(value, "@")
		if local == "" || !strings.Contains(domain, ".") || len(value) > 200 {
			return ""
		}
		return value
	}
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	phone := digits.String()
	if len(phone) < 6 || len(phone) > 15 {
		return ""
	}
	return phone
}

func (a *App) writeFileCompanionShareZip(source, highlightsJSON string) (string, error) {
	highlights, err := fileCompanionCleanHighlights(highlightsJSON)
	if err != nil {
		return "", err
	}
	messages := a.fileCompanionHistory(fileCompanionSessionID(source))
	paperPath, paperTurn := fileCompanionProjectPaper(messages)
	temp, err := os.CreateTemp("", "maclaw-share-*.zip")
	if err != nil {
		return "", errors.New("无法保存分享")
	}
	name := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return "", errors.New("无法保存分享")
	}
	if err := writeFileCompanionProject(name, source, highlights, messages, paperPath, paperTurn); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func (a *App) materializeFileCompanionShare(token, fallbackName string, raw []byte) (string, json.RawMessage, []FileCompanionMessage, bool, error) {
	if !fileCompanionShareTokenMatch.MatchString(token) {
		return "", nil, nil, false, errors.New("分享链接不存在")
	}
	manifest, document, paper, ok := readFileCompanionShareProject(raw)
	if !ok {
		dest, _, err := a.saveFileCompanionShareBody(token, fileCompanionLibrarySafeName(fallbackName), bytes.NewReader(raw))
		return dest, nil, nil, false, err
	}
	name := fileCompanionProjectSafeName(manifest.Name)
	if name == "" || name == "document" {
		name = fileCompanionLibrarySafeName(fallbackName)
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "library-files", token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, nil, true, err
	}
	dest := filepath.Join(dir, name)
	if err := fileCompanionWriteAtomicFile(dest, document, 0o644); err != nil {
		return "", nil, nil, true, err
	}
	identity, _, err := fileCompanionPathIdentity(dest)
	if err != nil {
		return "", nil, nil, true, errors.New("无法打开项目")
	}
	sessionID := fileCompanionSessionID(identity)
	if err := a.writeFileCompanionProjectSession(sessionID, manifest.Messages); err != nil {
		return "", nil, nil, true, err
	}
	if len(paper) > 0 && manifest.PaperTurn > 0 {
		_ = installFileCompanionSharePaper(a, sessionID, paper, manifest.PaperTurn)
	}
	highlights := manifest.Highlights
	if len(highlights) == 0 {
		highlights = json.RawMessage("[]")
	}
	return identity, highlights, manifest.Messages, true, nil
}

func readFileCompanionShareProject(raw []byte) (fileCompanionProjectManifest, []byte, []byte, bool) {
	var manifest fileCompanionProjectManifest
	if len(raw) < 4 || raw[0] != 'P' || raw[1] != 'K' {
		return manifest, nil, nil, false
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return manifest, nil, nil, false
	}
	var manifestFile, documentFile, paperFile *zip.File
	for _, file := range reader.File {
		kind, ok := fileCompanionProjectEntryKind(file.Name)
		if !ok {
			if strings.HasSuffix(file.Name, "/") {
				continue
			}
			return manifest, nil, nil, false
		}
		switch kind {
		case "manifest":
			if manifestFile != nil {
				return manifest, nil, nil, false
			}
			manifestFile = file
		case "document":
			if documentFile != nil {
				return manifest, nil, nil, false
			}
			documentFile = file
		case "paper":
			paperFile = file
		default:
			return manifest, nil, nil, false
		}
	}
	if manifestFile == nil || documentFile == nil {
		return manifest, nil, nil, false
	}
	manifest, err = readProjectManifest(manifestFile)
	if err != nil {
		return fileCompanionProjectManifest{}, nil, nil, false
	}
	document, err := readZipEntry(documentFile, fileCompanionProjectMaxDoc)
	if err != nil || len(document) == 0 {
		return fileCompanionProjectManifest{}, nil, nil, false
	}
	var paper []byte
	if paperFile != nil && manifest.PaperTurn > 0 {
		paper, _ = readZipEntry(paperFile, fileCompanionProjectMaxSidecar)
	}
	return manifest, document, paper, true
}

func readZipEntry(file *zip.File, limit int64) ([]byte, error) {
	if file == nil || file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("文件过大")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("文件过大")
	}
	return raw, nil
}

func installFileCompanionSharePaper(a *App, sessionID string, paper []byte, turn int) error {
	if a == nil || len(paper) == 0 {
		return nil
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	pdfPath := filepath.Join(dir, "论文解读.pdf")
	if err := fileCompanionWriteAtomicFile(pdfPath, paper, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(pdfPath), 0o644); err != nil {
		return err
	}
	if turn < 1 {
		turn = 1
	}
	return os.WriteFile(filepath.Join(dir, "latest-turn.txt"), []byte(itoaPositive(turn)), 0o644)
}

func (a *App) dropRevokedGroupCopies(previous map[string]FileCompanionLibraryItem, remote []FileCompanionLibraryItem, dropped map[string]bool) []string {
	seen := map[string]bool{}
	for _, item := range remote {
		if item.Token != "" {
			seen[item.Token] = true
		}
	}
	removed := make([]string, 0)
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files")
	for token, prev := range previous {
		if seen[token] || prev.Visibility != "group" || !fileCompanionShareTokenMatch.MatchString(token) {
			continue
		}
		dir := filepath.Join(root, token)
		// Only the downloaded copy lives in this directory. The owner's paper
		// is a different path, so losing the share must not close that tab.
		closeTab := companionFigureInsideDir(dir, prev.Path)
		_ = os.RemoveAll(dir)
		if dropped != nil {
			dropped[token] = true
		}
		if closeTab {
			removed = append(removed, strings.TrimSpace(prev.Path))
		}
	}
	return removed
}

func (a *App) pullNewerFileCompanionShares(hub, viewer string, items []FileCompanionLibraryItem, previous map[string]FileCompanionLibraryItem) []FileCompanionShareUpdate {
	updates := make([]FileCompanionShareUpdate, 0)
	for i := range items {
		item := &items[i]
		if item.Source != "cloud" || item.Mine || item.Token == "" {
			continue
		}
		if len(updates) >= 8 {
			break
		}
		prev := previous[item.Token]
		needs := item.Revision > prev.Revision
		if item.Path == "" && item.Visibility != "private" {
			needs = true
		}
		if !needs {
			continue
		}
		update, err := a.pullFileCompanionShare(hub, viewer, *item)
		if err != nil {
			continue
		}
		item.Path = update.Path
		item.Revision = update.Revision
		if update.Name != "" {
			item.Name = update.Name
		}
		updates = append(updates, update)
	}
	return updates
}

func (a *App) pullFileCompanionShare(hub, viewer string, item FileCompanionLibraryItem) (FileCompanionShareUpdate, error) {
	payload := []byte(`{"password":""}`)
	req, err := http.NewRequest(http.MethodPost, hub+"/api/mobile/documents/shares/"+item.Token+"/import", bytes.NewReader(payload))
	if err != nil {
		return FileCompanionShareUpdate{}, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return FileCompanionShareUpdate{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return FileCompanionShareUpdate{}, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, mobileDocumentOriginalMaxBytes+1))
	if err != nil || int64(len(raw)) > mobileDocumentOriginalMaxBytes || len(raw) == 0 {
		return FileCompanionShareUpdate{}, errors.New("文件无法读取")
	}
	name := fileCompanionLibrarySafeName(resp.Header.Get("X-Companion-Document-Name"))
	if name == "" || name == "document" {
		name = fileCompanionLibrarySafeName(item.Name)
	}
	path, highlights, messages, project, err := a.materializeFileCompanionShare(item.Token, name, raw)
	if err != nil {
		return FileCompanionShareUpdate{}, err
	}
	revision := item.Revision
	if parsed, convErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Companion-Document-Revision"))); convErr == nil {
		revision = parsed
	}
	update := FileCompanionShareUpdate{
		Path:     path,
		Token:    item.Token,
		Name:     name,
		Revision: revision,
		Project:  project,
	}
	if project {
		update.Highlights = string(highlights)
		if messages == nil {
			messages = []FileCompanionMessage{}
		}
		update.Messages = messages
	}
	return update, nil
}

// SyncFileCompanionShare uploads the open paper, highlights, and chat again.
// One file can belong to several shares. Each share is its own document, so
// the same zip is sent to every share this machine owns. One share's failure
// does not skip the others. A share the hub says is missing or expired is
// dropped, and the call succeeds when every share that can still take an
// update did. Any other failure is still reported.
func (a *App) SyncFileCompanionShare(path, highlights string) (FileCompanionShareResult, error) {
	if a == nil {
		return FileCompanionShareResult{}, errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionShareResult{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return FileCompanionShareResult{}, errors.New("文件无法读取")
	}
	shares := a.fileCompanionMineShares(canonical)
	if len(shares) == 0 {
		return FileCompanionShareResult{}, errors.New("还没有分享这份文档，无法同步更新")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionShareResult{}, err
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
	var last FileCompanionLibraryItem
	var lastView fileCompanionShareView
	var synced bool
	var realErr error
	var goneErr error
	for _, share := range shares {
		file, openErr := os.Open(zipPath)
		if openErr != nil {
			if realErr == nil {
				realErr = errors.New("无法保存分享")
			}
			break
		}
		view, postErr := postFileCompanionShareSync(hub, viewer, share.Token, filepath.Base(canonical), file)
		_ = file.Close()
		if postErr != nil {
			// A missing or expired share cannot take an update. Leave it out
			// so the next sync is not stuck on it. A 404 with no message is
			// an old hub and the row stays.
			if fileCompanionShareGone(postErr) {
				a.dropFileCompanionCloudToken(share.Token)
				if goneErr == nil {
					goneErr = postErr
				}
				continue
			}
			if realErr == nil {
				realErr = postErr
			}
			continue
		}
		share.Revision = view.Revision
		if view.URL != "" {
			share.ShareURL = view.URL
		}
		if view.Name != "" {
			share.Name = view.Name
		}
		share.Path = canonical
		share.Mine = true
		share.Source = "cloud"
		a.upsertFileCompanionCloud(share)
		last = share
		lastView = view
		synced = true
	}
	if realErr != nil {
		return FileCompanionShareResult{}, realErr
	}
	if !synced {
		if goneErr == nil {
			goneErr = errors.New("还没有分享这份文档，无法同步更新")
		}
		return FileCompanionShareResult{}, goneErr
	}
	result := libraryItemsFromShares([]fileCompanionShareView{lastView}, true, hub)
	url := last.ShareURL
	if len(result) > 0 && result[0].ShareURL != "" {
		url = result[0].ShareURL
	}
	return FileCompanionShareResult{
		URL:        url,
		Name:       last.Name,
		ExpiresAt:  last.ExpiresAt,
		Visibility: last.Visibility,
		Permanent:  last.Permanent,
		Token:      last.Token,
		Revision:   lastView.Revision,
	}, nil
}

// FileCompanionShareNotesResult is the note list after one reader's line is
// published. NotesRevision is the list the caller should show.
type FileCompanionShareNotesResult struct {
	Notes         []FileCompanionShareNote `json:"notes"`
	NotesRevision int                      `json:"notesRevision"`
}

// AddFileCompanionShareNote publishes one reader's discussion as a note.
// The host transcript is unchanged. Everyone who can see the share receives
// the note on the next library pull.
func (a *App) AddFileCompanionShareNote(path, text string) (FileCompanionShareNotesResult, error) {
	if a == nil {
		return FileCompanionShareNotesResult{}, errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	item, ok := a.fileCompanionShareByPath(canonical)
	if !ok {
		return FileCompanionShareNotesResult{}, errors.New("这份文档还没有分享，不能写批注")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return FileCompanionShareNotesResult{}, errors.New("无法保存批注")
	}
	body, err := a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/shares/"+item.Token+"/notes", viewer, payload)
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	notes, revision, err := decodeFileCompanionShareNotes(body)
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	if stored, storedRevision, ok := a.storeFileCompanionShareNotes(item.Token, notes, revision); ok {
		notes, revision = stored, storedRevision
	}
	return FileCompanionShareNotesResult{Notes: notes, NotesRevision: revision}, nil
}

// storeFileCompanionShareNotes writes notes onto the cloud row already in
// the library. The document revision and path stay as they are: a host sync
// can finish while this POST is in flight, and replacing the row captured
// before the POST would hide that sync. A row the hub already dropped is not
// put back. When the library already has a newer notes revision, that list
// is what the caller shows.
func (a *App) storeFileCompanionShareNotes(token string, notes []FileCompanionShareNote, revision int) ([]FileCompanionShareNote, int, bool) {
	if a == nil || token == "" {
		return nil, 0, false
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return nil, 0, false
	}
	for i := range items {
		if items[i].Source != "cloud" || items[i].Token != token {
			continue
		}
		if items[i].NotesRevision > revision {
			return cloneFileCompanionShareNotes(items[i].Notes), items[i].NotesRevision, true
		}
		items[i].Notes = cloneFileCompanionShareNotes(notes)
		items[i].NotesRevision = revision
		items[i].NotesKnown = true
		if err := a.writeFileCompanionLibraryLocked(items); err != nil {
			return nil, 0, false
		}
		return cloneFileCompanionShareNotes(items[i].Notes), revision, true
	}
	return nil, 0, false
}

func cloneFileCompanionShareNotes(notes []FileCompanionShareNote) []FileCompanionShareNote {
	out := make([]FileCompanionShareNote, len(notes))
	copy(out, notes)
	if out == nil {
		return []FileCompanionShareNote{}
	}
	return out
}

func decodeFileCompanionShareNotes(body []byte) ([]FileCompanionShareNote, int, error) {
	var payload struct {
		Notes         *[]fileCompanionNoteWire `json:"notes"`
		NotesRevision int                      `json:"notes_revision"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Notes == nil {
		return nil, 0, errors.New("云端批注无法读取")
	}
	notes := notesFromCompanionShareView(fileCompanionShareView{Notes: payload.Notes})
	if notes == nil {
		notes = []FileCompanionShareNote{}
	}
	return notes, payload.NotesRevision, nil
}

func (a *App) fileCompanionShareByPath(canonical string) (FileCompanionLibraryItem, bool) {
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return FileCompanionLibraryItem{}, false
	}
	canonical = strings.TrimSpace(canonical)
	for _, item := range items {
		if item.Source != "cloud" || item.Token == "" || !fileCompanionShareTokenMatch.MatchString(item.Token) {
			continue
		}
		if item.Path == canonical {
			return item, true
		}
		if identity, err := fileCompanionCanonicalPath(item.Path); err == nil && identity == canonical {
			return item, true
		}
	}
	return FileCompanionLibraryItem{}, false
}

// fileCompanionMineShares returns every cloud share this machine owns for the
// open file, newest first. A received copy of the same path is not owned.
// The caller uploads outside the library lock, so the returned rows are the
// snapshots from this read.
func (a *App) fileCompanionMineShares(canonical string) []FileCompanionLibraryItem {
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return nil
	}
	canonical = strings.TrimSpace(canonical)
	out := make([]FileCompanionLibraryItem, 0, 1)
	seen := map[string]bool{}
	for _, item := range items {
		if item.Source != "cloud" || !item.Mine || item.Token == "" || seen[item.Token] {
			continue
		}
		if item.Path != canonical {
			identity, err := fileCompanionCanonicalPath(item.Path)
			if err != nil || identity != canonical {
				continue
			}
		}
		seen[item.Token] = true
		out = append(out, item)
	}
	return out
}

// fileCompanionShareGone reports that the hub will not take another update
// for this token. A 404 without a message is an old hub, not a missing share.
func fileCompanionShareGone(err error) bool {
	var status *fileCompanionShareStatus
	if !errors.As(err, &status) || status == nil {
		return false
	}
	msg := strings.TrimSpace(status.message)
	switch status.status {
	case http.StatusGone:
		return msg != ""
	case http.StatusNotFound:
		return msg == "分享链接不存在"
	default:
		return false
	}
}

func postFileCompanionShareSync(hub, viewer, token, name string, raw io.Reader) (fileCompanionShareView, error) {
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		var writeErr error
		defer func() { _ = pipeWriter.CloseWithError(writeErr) }()
		if writeErr = writer.WriteField("filename", name); writeErr != nil {
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
	req, err := http.NewRequest(http.MethodPost, hub+"/api/mobile/documents/shares/"+token+"/sync", pipeReader)
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
