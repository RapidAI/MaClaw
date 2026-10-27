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
	"sync/atomic"
	"time"
)

// errFileCompanionSharePullDropped means the share was dismissed or imported
// again while its bytes were still downloading. Nothing live was changed.
var errFileCompanionSharePullDropped = errors.New("file companion share pull dropped")

var fileCompanionPullStageSeq atomic.Uint64

// fileCompanionSharePublishHook runs while the library lock is held, after the
// publish decision and before any live file moves. Tests block here to prove
// that decision and the write stay in one critical section.
var fileCompanionSharePublishHook func()

// fileCompanionShareCommitHook runs after the published path is stored and the
// library lock is released. Tests delete from here. The transcript this publish
// just installed is then the path Delete reads.
var fileCompanionShareCommitHook func()

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

func fileCompanionShareDocumentName(fallback string, manifest fileCompanionProjectManifest, project bool) string {
	if !project {
		return fileCompanionLibrarySafeName(fallback)
	}
	name := fileCompanionProjectSafeName(manifest.Name)
	if name == "" || name == "document" {
		name = fileCompanionLibrarySafeName(fallback)
	}
	return name
}

// fileCompanionShareStage is one downloaded share kept off the live paths
// until commit. If the transcript cannot be installed, the previous document
// is put back.
type fileCompanionShareStage struct {
	dir        string
	token      string
	name       string
	document   string
	highlights json.RawMessage
	messages   []FileCompanionMessage
	paper      []byte
	paperTurn  int
	project    bool
}

type fileCompanionShareInstall struct {
	path       string
	highlights json.RawMessage
	messages   []FileCompanionMessage
	project    bool
}

func (a *App) fileCompanionPullStageRoot() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "pull-staging")
}

func (a *App) makeFileCompanionPullStageDir(token string) (string, error) {
	root := a.fileCompanionPullStageRoot()
	if root == "" || !fileCompanionShareTokenMatch.MatchString(token) {
		return "", errors.New("分享链接不存在")
	}
	dir := filepath.Join(root, token, strconv.FormatUint(fileCompanionPullStageSeq.Add(1), 36)+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if !companionFigureInsideDir(root, dir) {
		_ = os.RemoveAll(dir)
		return "", errors.New("无法打开项目")
	}
	return dir, nil
}

func (a *App) removeFileCompanionPullStage(dir string) {
	root := a.fileCompanionPullStageRoot()
	if dir == "" || root == "" || !companionFigureInsideDir(root, dir) {
		return
	}
	_ = os.RemoveAll(dir)
}

func (a *App) stageFileCompanionShare(token, fallbackName string, raw []byte) (*fileCompanionShareStage, error) {
	if a == nil || !fileCompanionShareTokenMatch.MatchString(token) {
		return nil, errors.New("分享链接不存在")
	}
	manifest, document, paper, project := readFileCompanionShareProject(raw)
	if !project {
		document = raw
	}
	if len(document) == 0 {
		return nil, errors.New("文件是空的")
	}
	dir, err := a.makeFileCompanionPullStageDir(token)
	if err != nil {
		return nil, err
	}
	name := fileCompanionShareDocumentName(fallbackName, manifest, project)
	path := filepath.Join(dir, name)
	if err := fileCompanionWriteAtomicFile(path, document, 0o644); err != nil {
		a.removeFileCompanionPullStage(dir)
		return nil, err
	}
	stage := &fileCompanionShareStage{
		dir:      dir,
		token:    token,
		name:     name,
		document: path,
		project:  project,
	}
	if project {
		stage.highlights = manifest.Highlights
		stage.messages = manifest.Messages
		stage.paper = paper
		stage.paperTurn = manifest.PaperTurn
	}
	return stage, nil
}

// stagedFilePublish is a live document replaced by a staged file. restore puts
// the previous bytes back. keep drops the backup after the transcript is in place.
type stagedFilePublish struct {
	dest   string
	backup string
	had    bool
}

func (p stagedFilePublish) restore() {
	if p.dest == "" {
		return
	}
	if p.had {
		_ = os.Remove(p.dest)
		_ = os.Rename(p.backup, p.dest)
		return
	}
	_ = os.Remove(p.dest)
}

func (p stagedFilePublish) keep() {
	if p.had {
		_ = os.Remove(p.backup)
	}
}

func publishFileCompanionStagedFile(src, dest string) (stagedFilePublish, error) {
	published := stagedFilePublish{dest: dest}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return stagedFilePublish{}, err
	}
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Rename(src, tmp); err != nil {
		return stagedFilePublish{}, err
	}
	backup := dest + ".bak"
	_ = os.Remove(backup)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			_ = os.Remove(tmp)
			return stagedFilePublish{}, err
		}
		published.backup = backup
		published.had = true
	} else if !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return stagedFilePublish{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		published.restore()
		_ = os.Remove(tmp)
		return stagedFilePublish{}, err
	}
	return published, nil
}

// publishStagedFileCompanionShareLocked installs a staged share. The caller
// holds fileCompanionLibraryMu. On failure the previous document is restored.
func (a *App) publishStagedFileCompanionShareLocked(stage *fileCompanionShareStage) (fileCompanionShareInstall, error) {
	if stage == nil || !fileCompanionShareTokenMatch.MatchString(stage.token) {
		return fileCompanionShareInstall{}, errors.New("分享链接不存在")
	}
	dest := filepath.Join(a.GetDataDir(), "file-companion", "library-files", stage.token, stage.name)
	published, err := publishFileCompanionStagedFile(stage.document, dest)
	if err != nil {
		return fileCompanionShareInstall{}, err
	}
	identity, _, err := fileCompanionPathIdentity(dest)
	if err != nil {
		published.restore()
		return fileCompanionShareInstall{}, errors.New("无法打开项目")
	}
	if stage.project {
		sessionID := fileCompanionSessionID(identity)
		if err := a.writeFileCompanionProjectSession(sessionID, stage.messages); err != nil {
			published.restore()
			return fileCompanionShareInstall{}, err
		}
		if len(stage.paper) > 0 && stage.paperTurn > 0 {
			_ = installFileCompanionSharePaper(a, sessionID, stage.paper, stage.paperTurn)
		}
	}
	published.keep()
	highlights := stage.highlights
	if stage.project && len(highlights) == 0 {
		highlights = json.RawMessage("[]")
	}
	return fileCompanionShareInstall{
		path:       identity,
		highlights: highlights,
		messages:   stage.messages,
		project:    stage.project,
	}, nil
}

// commitStagedFileCompanionShare publishes stage. A pull passes the generation
// it captured before the download and leaves store nil. An import passes
// claim so the generation moves and the library row is written before the
// lock is released. A pull cannot observe the new generation and still race
// that write.
func (a *App) commitStagedFileCompanionShare(stage *fileCompanionShareStage, epoch uint64, claim bool, store *FileCompanionLibraryItem) (fileCompanionShareInstall, error) {
	if stage == nil {
		return fileCompanionShareInstall{}, errors.New("无法打开项目")
	}
	defer a.removeFileCompanionPullStage(stage.dir)
	installed, err := a.commitLockedFileCompanionShare(stage, epoch, claim, store)
	if err == nil && fileCompanionShareCommitHook != nil {
		fileCompanionShareCommitHook()
	}
	return installed, err
}

func (a *App) commitLockedFileCompanionShare(stage *fileCompanionShareStage, epoch uint64, claim bool, store *FileCompanionLibraryItem) (fileCompanionShareInstall, error) {
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	if !fileCompanionShareTokenMatch.MatchString(stage.token) {
		return fileCompanionShareInstall{}, errors.New("分享链接不存在")
	}
	if claim {
		a.bumpFileCompanionShareEpochLocked(stage.token)
	} else if a.readDismissedShareTokensLocked()[stage.token] || fileCompanionShareEpoch[stage.token] != epoch {
		logFileCompanion("share pull dropped token=%s", stage.token)
		return fileCompanionShareInstall{}, errFileCompanionSharePullDropped
	}
	if fileCompanionSharePublishHook != nil {
		fileCompanionSharePublishHook()
	}
	installed, err := a.publishStagedFileCompanionShareLocked(stage)
	if err != nil {
		return installed, err
	}
	if store == nil {
		// The library row still has the path from before this download. Delete
		// reads that path to decide whether a transcript is left behind. Record
		// the published file before unlocking, or a delete in this gap sees an
		// empty path and keeps the transcript just installed.
		if err := a.recordFileCompanionSharePathLocked(stage.token, installed.path); err != nil {
			return installed, err
		}
		return installed, nil
	}
	store.Path = installed.path
	if installed.project {
		store.Highlights = string(installed.highlights)
	}
	saved := *store
	saved.Highlights = ""
	if err := a.upsertFileCompanionCloudLocked(saved); err != nil {
		return installed, err
	}
	return installed, nil
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

func (a *App) dropRevokedGroupCopies(previous map[string]FileCompanionLibraryItem, remote []FileCompanionLibraryItem, dropped map[string]bool, epochs map[string]uint64) []string {
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
		fileCompanionLibraryMu.Lock()
		// Import and delete move this generation while the hub list is still
		// downloading. The copy on disk belongs to that newer decision.
		if fileCompanionShareEpoch[token] != epochs[token] {
			fileCompanionLibraryMu.Unlock()
			continue
		}
		// Only the downloaded copy lives in this directory. The owner's paper
		// is a different path, so losing the share must not close that tab.
		closeTab := companionFigureInsideDir(dir, prev.Path)
		_ = os.RemoveAll(dir)
		fileCompanionLibraryMu.Unlock()
		if dropped != nil {
			dropped[token] = true
		}
		if closeTab {
			removed = append(removed, strings.TrimSpace(prev.Path))
		}
	}
	return removed
}

// withoutSupersededRevokePaths drops tab closes for shares an import took
// back while the refresh was still downloading other documents.
func (a *App) withoutSupersededRevokePaths(removed []string, superseded map[string]bool) []string {
	if a == nil || len(removed) == 0 || len(superseded) == 0 {
		return removed
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "library-files")
	kept := make([]string, 0, len(removed))
	for _, path := range removed {
		drop := false
		for token := range superseded {
			if !fileCompanionShareTokenMatch.MatchString(token) {
				continue
			}
			if companionFigureInsideDir(filepath.Join(root, token), path) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, path)
		}
	}
	return kept
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
		// Capture the generation before the download. Delete and import advance
		// it under the same lock as their live write, and the commit below
		// refuses to publish when it moved. A hidden share, or one this
		// machine already stored at this revision, is not downloaded.
		epoch, skip := a.fileCompanionSharePullPermit(*item)
		if skip {
			continue
		}
		prev := previous[item.Token]
		needs := item.Revision > prev.Revision
		if item.Path == "" && item.Visibility != "private" {
			needs = true
		}
		if !needs {
			continue
		}
		update, err := a.pullFileCompanionShare(hub, viewer, *item, epoch)
		if err != nil {
			// Merge already copied the hub revision onto this row. Saving that
			// revision makes the next refresh treat the failed download as
			// stored and skip it. Put the revision and path back to the copy
			// this machine had before the attempt. Notes stay on the row.
			item.Revision = prev.Revision
			if strings.TrimSpace(prev.Path) != "" {
				item.Path = prev.Path
			} else if !fileCompanionLibraryFileExists(item.Path) {
				item.Path = ""
			}
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

func (a *App) pullFileCompanionShare(hub, viewer string, item FileCompanionLibraryItem, epoch uint64) (FileCompanionShareUpdate, error) {
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
	stage, err := a.stageFileCompanionShare(item.Token, name, raw)
	if err != nil {
		return FileCompanionShareUpdate{}, err
	}
	installed, err := a.commitStagedFileCompanionShare(stage, epoch, false, nil)
	if err != nil {
		return FileCompanionShareUpdate{}, err
	}
	revision := item.Revision
	if parsed, convErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Companion-Document-Revision"))); convErr == nil {
		revision = parsed
	}
	update := FileCompanionShareUpdate{
		Path:     installed.path,
		Token:    item.Token,
		Name:     name,
		Revision: revision,
		Project:  installed.project,
	}
	if installed.project {
		update.Highlights = string(installed.highlights)
		messages := installed.messages
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
		if err := a.upsertFileCompanionCloud(share); err != nil {
			if realErr == nil {
				realErr = errors.New("文档列表无法保存")
			}
			continue
		}
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
	return a.addFileCompanionShareNoteByToken(item.Token, text, "")
}

// addFileCompanionShareNoteByToken publishes one note without needing the
// document open. replyTo is set only by the host's agent answering a reader
// note; an empty value is a reader's own discussion line.
func (a *App) addFileCompanionShareNoteByToken(token, text, replyTo string) (FileCompanionShareNotesResult, error) {
	if a == nil {
		return FileCompanionShareNotesResult{}, errors.New("app unavailable")
	}
	token = strings.TrimSpace(token)
	text = strings.TrimSpace(text)
	if token == "" || text == "" {
		return FileCompanionShareNotesResult{}, errors.New("无法保存批注")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	payload, err := json.Marshal(map[string]string{"text": text, "reply_to": strings.TrimSpace(replyTo)})
	if err != nil {
		return FileCompanionShareNotesResult{}, errors.New("无法保存批注")
	}
	body, err := a.fileCompanionHubJSON(http.MethodPost, hub+"/api/mobile/documents/shares/"+token+"/notes", viewer, payload)
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	notes, revision, err := decodeFileCompanionShareNotes(body)
	if err != nil {
		return FileCompanionShareNotesResult{}, err
	}
	if stored, storedRevision, ok := a.storeFileCompanionShareNotes(token, notes, revision); ok {
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
