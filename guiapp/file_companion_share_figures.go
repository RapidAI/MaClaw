package guiapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The chat transcript travels as JSON, so the images its bubbles refer to need
// their own channel. The host pushes each referenced figure once per share;
// a participant pulls it the first time a bubble shows it and mirrors the
// bytes under its own figures directory, so the next render is a local read.

// fileCompanionShareFigureNameMatch is the same name rule the Hub enforces.
var fileCompanionShareFigureNameMatch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// fileCompanionShareFigureSessionMatch is the session directory a figure path
// carries between file-companion/figures/ and the file name.
var fileCompanionShareFigureSessionMatch = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

const fileCompanionShareFigureMaxBytes = 8 << 20

// fileCompanionShareMessageFigurePaths is every image the transcript refers
// to, in first-seen order.
func fileCompanionShareMessageFigurePaths(messages []FileCompanionMessage) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, msg := range messages {
		for _, fig := range msg.Figures {
			path := strings.TrimSpace(fig.Path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

// fileCompanionShareFiguresLedger remembers which image bytes each share
// already holds, so a chat push only uploads the figures the transcript just
// introduced. It is a disk sidecar, not library state: losing it only costs
// one redundant upload.
type fileCompanionShareFiguresLedger struct {
	mu      sync.Mutex
	loaded  bool
	byToken map[string]map[string]string
	// unsupported marks shares whose Hub predates the figures endpoint. The
	// upload would fail on every chat push for every figure, which is the
	// expensive half of the retry, so this set is what stops it. It is
	// memory-only: a Hub that gains the endpoint gets another try next run.
	unsupported map[string]bool
}

var fileCompanionShareFigures fileCompanionShareFiguresLedger

func (a *App) fileCompanionShareFiguresLedgerPath() string {
	if a == nil || a.GetDataDir() == "" {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "share-figures.json")
}

func (a *App) fileCompanionShareFiguresLoadLocked() {
	st := &fileCompanionShareFigures
	if st.loaded {
		return
	}
	st.loaded = true
	st.byToken = map[string]map[string]string{}
	path := a.fileCompanionShareFiguresLedgerPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored struct {
		Shares map[string]map[string]string `json:"shares"`
	}
	if json.Unmarshal(raw, &stored) == nil && stored.Shares != nil {
		st.byToken = stored.Shares
	}
}

func (a *App) fileCompanionShareFiguresSaveLocked() {
	st := &fileCompanionShareFigures
	path := a.fileCompanionShareFiguresLedgerPath()
	if path == "" {
		return
	}
	body, err := json.Marshal(struct {
		Shares map[string]map[string]string `json:"shares"`
	}{Shares: st.byToken})
	if err != nil {
		return
	}
	_ = fileCompanionWriteAtomicFile(path, body, 0o600)
}

// fileCompanionShareFigureHashCache keys a figure file by (path, size, mtime)
// so a chat push does not decode and scale the same image on every line. The
// sha is kept, not the bytes: holding megabytes per figure in memory is worse
// than the occasional re-scale when a different share needs the upload.
type fileCompanionShareFigureHashEntry struct {
	size    int64
	modNano int64
	sha     string
}

var fileCompanionShareFigureHashCache sync.Map

const fileCompanionShareFigureHashCacheCap = 256

// fileCompanionShareFigureHash returns the sha of the display-scale bytes.
// The data comes back with it when this call had to scale, so an immediate
// upload does not decode the image a second time.
func fileCompanionShareFigureHash(canonical string, info os.FileInfo) (string, []byte, bool) {
	if cached, ok := fileCompanionShareFigureHashCache.Load(canonical); ok {
		entry := cached.(fileCompanionShareFigureHashEntry)
		if entry.size == info.Size() && entry.modNano == info.ModTime().UnixNano() {
			return entry.sha, nil, true
		}
	}
	data, _, err := companionFigureImage(canonical, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil || len(data) == 0 || len(data) > fileCompanionShareFigureMaxBytes {
		return "", nil, false
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	count := 0
	fileCompanionShareFigureHashCache.Range(func(_, _ any) bool {
		count++
		return count < fileCompanionShareFigureHashCacheCap
	})
	if count >= fileCompanionShareFigureHashCacheCap {
		fileCompanionShareFigureHashCache.Clear()
	}
	fileCompanionShareFigureHashCache.Store(canonical, fileCompanionShareFigureHashEntry{
		size:    info.Size(),
		modNano: info.ModTime().UnixNano(),
		sha:     sha,
	})
	return sha, data, true
}

// fileCompanionShareFigureBytes is the display-scale payload one file
// uploads. Called only after the hash says a share needs it.
func fileCompanionShareFigureBytes(canonical string) ([]byte, bool) {
	data, _, err := companionFigureImage(canonical, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil || len(data) == 0 || len(data) > fileCompanionShareFigureMaxBytes {
		return nil, false
	}
	return data, true
}

// fileCompanionShareFigureUploaded is the sha this share already has under
// name, or "" when it has none.
func (a *App) fileCompanionShareFigureUploaded(token, name string) string {
	st := &fileCompanionShareFigures
	st.mu.Lock()
	defer st.mu.Unlock()
	a.fileCompanionShareFiguresLoadLocked()
	return st.byToken[token][name]
}

func (a *App) fileCompanionShareFiguresUnsupported(token string) bool {
	st := &fileCompanionShareFigures
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.unsupported[token]
}

func (a *App) noteFileCompanionShareFiguresUnsupported(token string) {
	st := &fileCompanionShareFigures
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.unsupported == nil {
		st.unsupported = map[string]bool{}
	}
	st.unsupported[token] = true
}

func (a *App) noteFileCompanionShareFigureUploaded(token, name, sha string) {
	st := &fileCompanionShareFigures
	st.mu.Lock()
	defer st.mu.Unlock()
	a.fileCompanionShareFiguresLoadLocked()
	if st.byToken[token] == nil {
		st.byToken[token] = map[string]string{}
	}
	st.byToken[token][name] = sha
	a.fileCompanionShareFiguresSaveLocked()
}

func postFileCompanionShareFigure(target, viewer, grant, token, name string, data []byte) error {
	req, err := http.NewRequest(http.MethodPost, target+"/api/mobile/documents/shares/"+token+"/figures/"+url.PathEscape(name), bytes.NewReader(data))
	if err != nil {
		return err
	}
	fileCompanionShareAuthorize(req, viewer, grant)
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	return nil
}

// pushFileCompanionShareFigures uploads the images messages refer to that
// each share does not have yet. The display-scale copy goes over the wire,
// the same bytes the window would paint. Best-effort: callers log and move
// on, because the text half of the transcript already landed.
func (a *App) pushFileCompanionShareFigures(hub, viewer string, shares []FileCompanionLibraryItem, messages []FileCompanionMessage) {
	if a == nil || len(shares) == 0 || len(messages) == 0 {
		return
	}
	paths := fileCompanionShareMessageFigurePaths(messages)
	if len(paths) == 0 {
		return
	}
	// The hash and the scaled bytes are per file, not per share: one image
	// shared to three links uploads three times but is read and scaled once.
	dataByPath := map[string][]byte{}
	bytesFor := func(canonical string) ([]byte, bool) {
		data, ok := dataByPath[canonical]
		if !ok {
			data, ok = fileCompanionShareFigureBytes(canonical)
			if ok {
				dataByPath[canonical] = data
			}
		}
		return data, ok
	}
	for _, figPath := range paths {
		name := filepath.Base(figPath)
		if !fileCompanionShareFigureNameMatch.MatchString(name) || strings.Contains(name, "..") {
			continue
		}
		canonical, err := fileCompanionCanonicalPath(figPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		sha, hashed, ok := fileCompanionShareFigureHash(canonical, info)
		if !ok {
			continue
		}
		if hashed != nil {
			dataByPath[canonical] = hashed
		}
		for _, share := range shares {
			token := strings.TrimSpace(share.Token)
			if !fileCompanionShareTokenMatch.MatchString(token) || a.fileCompanionShareFiguresUnsupported(token) {
				continue
			}
			if a.fileCompanionShareFigureUploaded(token, name) == sha {
				continue
			}
			data, ok := bytesFor(canonical)
			if !ok {
				break
			}
			target, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, share)
			if foreign && grant == "" {
				continue
			}
			if err := postFileCompanionShareFigure(target, auth, grant, token, name, data); err != nil {
				if fileCompanionShareGone(err) {
					a.retireFileCompanionShare(token)
				}
				var status *fileCompanionShareStatus
				if errors.As(err, &status) && status != nil && strings.TrimSpace(status.message) == "" &&
					(status.status == http.StatusNotFound || status.status == http.StatusMethodNotAllowed) {
					a.noteFileCompanionShareFiguresUnsupported(token)
				}
				logFileCompanion("share figure push failed token=%s name=%s: %v", token, name, err)
				continue
			}
			a.noteFileCompanionShareFigureUploaded(token, name, sha)
		}
	}
}

// fileCompanionShareFigureTail reads the file-companion/figures/<session>/<name>
// tail out of a stored figure path. That tail is the only part of the host's
// absolute path that survives the move to a participant's machine.
func fileCompanionShareFigureTail(figurePath string) (string, string, bool) {
	p := strings.ReplaceAll(strings.TrimSpace(figurePath), "\\", "/")
	const marker = "/file-companion/figures/"
	idx := strings.LastIndex(p, marker)
	if idx < 0 {
		return "", "", false
	}
	parts := strings.Split(p[idx+len(marker):], "/")
	if len(parts) != 2 {
		return "", "", false
	}
	session, name := parts[0], parts[1]
	if !fileCompanionShareFigureSessionMatch.MatchString(session) {
		return "", "", false
	}
	if !fileCompanionShareFigureNameMatch.MatchString(name) || strings.Contains(name, "..") {
		return "", "", false
	}
	return session, name, true
}

func fileCompanionFigureDataURLFile(path string) (string, error) {
	data, mime, err := companionFigureImage(path, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// fileCompanionShareFigureMirror is where a fetched remote figure lives: the
// same session/name tail the transcript carries, rooted under this machine's
// data dir. A later render of the same bubble is a local stat, not a download.
func (a *App) fileCompanionShareFigureMirror(session, name string) string {
	if a == nil || a.GetDataDir() == "" {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "figures", session, name)
}

// FetchFileCompanionShareFigure serves one transcript image for a document.
// A local file wins first — the host's own bubble, or a figure this machine
// produced. A mirror of an earlier fetch comes next. Only then does it ask
// the share's Hub for the blob and mirror the answer onto disk.
func (a *App) FetchFileCompanionShareFigure(docPath, figurePath string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	figurePath = strings.TrimSpace(figurePath)
	if figurePath == "" || strings.ContainsRune(figurePath, 0) {
		return "", errFileCompanionNULPath
	}
	if info, err := os.Stat(figurePath); err == nil && !info.IsDir() {
		return a.FileCompanionFigureDataURL(figurePath)
	}
	session, name, ok := fileCompanionShareFigureTail(figurePath)
	if !ok {
		return "", errors.New("图片不可用")
	}
	mirror := a.fileCompanionShareFigureMirror(session, name)
	if mirror == "" {
		return "", errors.New("图片不可用")
	}
	if info, err := os.Stat(mirror); err == nil && !info.IsDir() && info.Size() > 0 {
		return fileCompanionFigureDataURLFile(mirror)
	}
	canonical, err := fileCompanionCanonicalPath(docPath)
	if err != nil {
		return "", errors.New("图片不可用")
	}
	item, ok := a.fileCompanionShareByPath(canonical)
	if !ok || item.Token == "" {
		return "", errors.New("这份文档还没有导入分享")
	}
	hub, viewer, err := a.fileCompanionHubLogin()
	if err != nil {
		return "", err
	}
	target, auth, grant, foreign := a.fileCompanionShareCall(hub, viewer, item)
	if foreign && grant == "" {
		return "", errors.New("请重新导入分享链接")
	}
	req, err := http.NewRequest(http.MethodGet, target+"/api/mobile/documents/shares/"+item.Token+"/figures/"+url.PathEscape(name), nil)
	if err != nil {
		return "", err
	}
	fileCompanionShareAuthorize(req, auth, grant)
	client := fileCompanionDirectClient(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("暂时无法连接云端")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, fileCompanionShareFigureMaxBytes+1))
	if err != nil || len(body) == 0 {
		return "", errors.New("图片不可用")
	}
	if len(body) > fileCompanionShareFigureMaxBytes {
		return "", errors.New("图片过大")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
	if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
		return "", err
	}
	if err := fileCompanionWriteAtomicFile(mirror, body, 0o644); err != nil {
		return "", err
	}
	return fileCompanionFigureDataURLFile(mirror)
}
