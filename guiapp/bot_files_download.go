package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Large files a bot delivers stay on Hub's bot file store; the chat message
// is the file. This app downloads them over HTTP (Range, resume) into the
// bot's own folder (~/MaClaw/<bot> by default) so that message can open the
// copy. The save is not another transfer: the composer strip is only for a
// file the bot moves through the local relay.

// botStoredFile is Hub's answer for a stored file: a fresh signed path.
type botStoredFile struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
	BotID   string `json:"bot_id"`
	BotName string `json:"bot_name"`
	Data    string `json:"data"`
}

var botFileHTTP = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second, IdleConnTimeout: 90 * time.Second}}

// botFileFetcher downloads from Hub with this person's machine credential.
func (a *App) botFileFetcher() localrelay.Fetcher {
	return func(ctx context.Context, u string, offset int64) (*http.Response, error) {
		hubURL, token, err := a.getHubCredentials()
		if err != nil {
			return nil, err
		}
		machineID := ""
		if cfg, err := a.LoadConfig(); err == nil {
			machineID = strings.TrimSpace(groupDiscussionAgentID(cfg))
		}
		return localrelay.HTTPFetcher(botFileHTTP, hubURL, func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			if machineID != "" {
				r.Header.Set("X-Machine-ID", machineID)
			}
		})(ctx, u, offset)
	}
}

// botFileRefresh asks Hub for a new signed path for the person's own file.
func (a *App) botFileRefresh(ctx context.Context, ref string) (string, error) {
	var out botStoredFile
	if err := a.desktopBotCall(ctx, http.MethodPost, "/api/v1/bot-files/"+url.PathEscape(ref)+"/link", map[string]any{}, &out, 30*time.Second); err != nil {
		return "", err
	}
	return out.URL, nil
}

// localRelayAgentForFiles is the running relay agent, or one made only for
// chat-file downloads when the relay link has not started. That standby
// agent has no transfer reporter: a chat file is not a download chip.
func (a *App) localRelayAgentForFiles() *localrelay.Agent {
	localRelayState.mu.Lock()
	agent := localRelayState.agent
	localRelayState.mu.Unlock()
	if agent != nil {
		return agent
	}
	botFilesMu.Lock()
	defer botFilesMu.Unlock()
	if botFilesAgent == nil {
		botFilesAgent = &localrelay.Agent{
			Home:        localRelayHome(),
			Load:        loadLocalRelaySettings,
			Fetch:       a.botFileFetcher(),
			RefreshLink: a.botFileRefresh,
		}
	}
	return botFilesAgent
}

var (
	botFilesMu      sync.Mutex
	botFilesAgent   *localrelay.Agent
	botFilesStarted = map[string]time.Time{}
	botFilesGivenUp = map[string]bool{}
	botFilesCached  map[string]string
)

func botFilesIndexPath() string {
	return filepath.Join(maclawpath.DataDir(), "bot_files_saved.json")
}

// botFilesIndex is the saved-file index. The caller holds botFilesMu.
// The map is cached for this process; every write replaces it.
func botFilesIndex() map[string]string {
	if botFilesCached != nil {
		return botFilesCached
	}
	out := map[string]string{}
	raw, err := os.ReadFile(botFilesIndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			botFilesCached = out
		}
		return out
	}
	if json.Unmarshal(raw, &out) != nil {
		return map[string]string{}
	}
	botFilesCached = out
	return out
}

func botFilesRemember(ref, path string) {
	botFilesMu.Lock()
	defer botFilesMu.Unlock()
	idx := botFilesIndex()
	if idx[ref] == path {
		return
	}
	idx[ref] = path
	botFilesStore(idx)
}

func botFilesStore(idx map[string]string) {
	botFilesCached = idx
	raw, _ := json.MarshalIndent(idx, "", "  ")
	p := botFilesIndexPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// botFileSavedPath is where ref was saved before, if that file is still there.
func botFileSavedPath(ref string) string {
	botFilesMu.Lock()
	p := botFilesIndex()[ref]
	botFilesMu.Unlock()
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// mailFileInfo reads a message file's entry from Hub: inline data for a
// small file, or the store reference and a signed path for a large one.
func (a *App) mailFileInfo(ctx context.Context, noteID, name string) (botStoredFile, error) {
	var out botStoredFile
	err := a.desktopBotCall(ctx, http.MethodGet, "/api/v1/bots/main/inbox/"+url.PathEscape(noteID)+"/files/"+url.PathEscape(name), nil, &out, 60*time.Second)
	return out, err
}

// saveBotStoredFile downloads a stored file into its bot's folder (once).
func (a *App) saveBotStoredFile(ctx context.Context, f botStoredFile) (string, error) {
	if f.Ref == "" {
		return "", errors.New("not a stored file")
	}
	if p := botFileSavedPath(f.Ref); p != "" {
		rememberDeliveryName(f.BotID, f.Name, p)
		return p, nil
	}
	agent := a.localRelayAgentForFiles()
	path, err := agent.SaveDelivered(ctx, f.BotID, f.BotName, "", localrelay.FetchSpec{URL: f.URL, Ref: f.Ref, Name: f.Name, Size: f.Size, SHA256: f.SHA256})
	if err != nil {
		return "", err
	}
	botFilesRemember(f.Ref, path)
	rememberNewDelivery(f.BotID, f.Name, path)
	log.Printf("[bot-files] saved ref=%s bot=%s size=%d path=%s", f.Ref, f.BotID, f.Size, path)
	return path, nil
}

// rememberNewDelivery records a file that just finished saving. It is the
// copy that opens, even when an older file was touched more recently.
func rememberNewDelivery(botID, name, path string) {
	rememberDelivery(botID, name, path, true)
}

// rememberDeliveryName records a copy already on disk. An older file does
// not replace a newer one.
func rememberDeliveryName(botID, name, path string) {
	rememberDelivery(botID, name, path, false)
}

// rememberDelivery records where this delivery was saved. The key is the
// name the chat shows, not a uniquified local name. A bot id keeps one bot's
// file from opening for another bot that used the same name.
func rememberDelivery(botID, name, path string, force bool) {
	if strings.TrimSpace(path) == "" {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	base := filepath.Base(filepath.ToSlash(name))
	if base == "" || base == "." || base == "/" || base == `\` {
		return
	}
	keys := []string{"name:" + base}
	if botID = strings.TrimSpace(botID); botID != "" && botID != "human" {
		keys = append(keys, "name:"+botID+":"+base)
	}
	botFilesMu.Lock()
	defer botFilesMu.Unlock()
	idx := botFilesIndex()
	changed := false
	for _, key := range keys {
		if !deliveryReplaces(idx[key], path, force) {
			continue
		}
		idx[key] = path
		changed = true
	}
	if changed {
		botFilesStore(idx)
	}
}

// deliveryReplaces reports that path is the copy to open instead of current.
// An older file must not take the place of a newer one: the chat list is
// replayed on every refresh. force is set only for a file that just finished
// saving, so that copy opens even when the previous file was touched later.
func deliveryReplaces(current, path string, force bool) bool {
	if path == "" || current == path {
		return false
	}
	next, err := os.Stat(path)
	if err != nil || next.IsDir() {
		return false
	}
	if force || current == "" {
		return true
	}
	prev, err := os.Stat(current)
	if err != nil || prev.IsDir() {
		return true
	}
	return next.ModTime().After(prev.ModTime())
}

// cloudDesktopOpenPath reports a path on the bot's computer. A drive path
// on this computer is not one, even when the file is missing.
func cloudDesktopOpenPath(path string) bool {
	if filepath.VolumeName(path) != "" {
		return false
	}
	slash := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(slash, "/home/desktop/") || slash == "/home/desktop" {
		return true
	}
	for _, root := range []string{"/tmp/", "/var/", "/opt/", "/data/", "/workspace/", "/Volumes/", "/private/", "/mnt/", "/srv/", "/Users/"} {
		if strings.HasPrefix(slash, root) {
			return true
		}
	}
	return strings.HasPrefix(slash, "~/")
}

// deliveredFileForOpen is the local copy of a file the chat named. The
// requested path is often the cloud desktop path; the file the person can
// open is the one this app saved.
func deliveredFileForOpen(botID, requested string) string {
	botFilesMu.Lock()
	p := deliveryPath(botID, requested, botFilesIndex())
	botFilesMu.Unlock()
	return deliveredFileAt(p)
}

// mailFileSavedCopy is the file this machine already saved under name.
// rememberDelivery writes that name key for every saved mail file. Hub is
// how the bytes arrive; once they are here, a later Hub error is not a
// reason to tell the person the file is missing.
func mailFileSavedCopy(noteID, name string) (DesktopBotMailFileData, bool) {
	botFilesMu.Lock()
	idx := botFilesIndex()
	p := ""
	if key := mailNoteKey(noteID, name); key != "" {
		p = idx[key]
	}
	botFilesMu.Unlock()
	p = deliveredFileAt(p)
	if p == "" {
		// Saved before a note key existed. A unique name still has one copy.
		p = deliveredFileForOpen("", name)
	}
	if p == "" {
		return DesktopBotMailFileData{}, false
	}
	size := int64(0)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		size = info.Size()
	}
	return DesktopBotMailFileData{Name: filepath.Base(p), LocalPath: p, Size: size}, true
}

// mailNoteKey is one file on one said note. The name alone is shared by
// every later report that reused it, so a Hub miss would open the newest
// copy instead of the message the person clicked.
func mailNoteKey(noteID, name string) string {
	noteID = strings.TrimSpace(noteID)
	base := filepath.Base(filepath.ToSlash(strings.TrimSpace(name)))
	if noteID == "" || base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return "note:" + noteID + ":" + base
}

// rememberMailNote records which saved path belongs to this note.
func rememberMailNote(noteID, name, path string) {
	key := mailNoteKey(noteID, name)
	path = strings.TrimSpace(path)
	if key == "" || path == "" {
		return
	}
	botFilesMu.Lock()
	defer botFilesMu.Unlock()
	idx := botFilesIndex()
	if idx[key] == path {
		return
	}
	idx[key] = path
	botFilesStore(idx)
}

func deliveredFileNamed(botID, requested string, saved map[string]string) string {
	return deliveredFileAt(deliveryPath(botID, requested, saved))
}

func deliveryPath(botID, requested string, saved map[string]string) string {
	req := strings.TrimSpace(requested)
	if req == "" || strings.HasSuffix(req, "/") || strings.HasSuffix(req, `\`) {
		return ""
	}
	base := filepath.Base(filepath.ToSlash(req))
	if base == "" || base == "." || base == "/" || base == `\` {
		return ""
	}
	botID = strings.TrimSpace(botID)
	if botID != "" {
		// This bot's own copy only. Another bot's file of the same name
		// stays closed.
		return saved["name:"+botID+":"+base]
	}
	return saved["name:"+base]
}

func deliveredFileAt(p string) string {
	if p == "" {
		return ""
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return ""
	}
	return p
}

// autoSaveBotFiles starts the download of every stored file on these
// messages that is not saved yet. A failure is tried again a minute later;
// a file the server no longer has is not tried again.
func (a *App) autoSaveBotFiles(items []BotSaidItem) {
	// Newest message last in the list. Walk backwards so an older file of
	// the same name cannot replace the copy just saved.
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		for _, f := range item.Files {
			ref := strings.TrimSpace(f.Ref)
			if ref == "" {
				continue
			}
			if p := botFileSavedPath(ref); p != "" {
				rememberDeliveryName(item.From, f.Name, p)
				rememberMailNote(item.ID, f.Name, p)
				continue
			}
			botFilesMu.Lock()
			started, busy := botFilesStarted[ref]
			skip := botFilesGivenUp[ref] || (busy && time.Since(started) < time.Minute)
			if !skip {
				botFilesStarted[ref] = time.Now()
			}
			botFilesMu.Unlock()
			if skip {
				continue
			}
			noteID, name := item.ID, f.Name
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
				defer cancel()
				info, err := a.mailFileInfo(ctx, noteID, name)
				var path string
				if err == nil {
					path, err = a.saveBotStoredFile(ctx, info)
				}
				if err == nil {
					rememberMailNote(noteID, name, path)
				}
				if err != nil {
					code := localrelay.CodeOf(err)
					botFilesMu.Lock()
					if code == localrelay.CodeExpired || code == localrelay.CodeNotFound || strings.Contains(err.Error(), "BOT_FILE_EXPIRED") || strings.Contains(err.Error(), "BOT_FILE_NOT_FOUND") {
						botFilesGivenUp[ref] = true
					}
					botFilesMu.Unlock()
					log.Printf("[bot-files] auto save ref=%s: %v", ref, err)
				}
			}()
		}
	}
}

// fetchMailFileForChat is FetchDesktopBotMailFile's body. A stored file and
// an inlined mail file are both saved here and returned as a path. The
// webview does not carry the bytes back through the chat-attachment cap.
func (a *App) fetchMailFileForChat(noteID, name string) (DesktopBotMailFileData, error) {
	info, err := a.mailFileInfo(context.Background(), noteID, name)
	if err != nil {
		// Hub could not answer. A copy already saved for this name still opens.
		// The 08:16 weather PDF was on disk while preview showed Hub's error.
		if saved, ok := mailFileSavedCopy(noteID, name); ok {
			return saved, nil
		}
		return DesktopBotMailFileData{}, err
	}
	if info.Ref == "" {
		raw, err := base64.StdEncoding.DecodeString(info.Data)
		if err != nil || len(raw) == 0 {
			return DesktopBotMailFileData{}, fmt.Errorf("文件内容无效")
		}
		fileName := firstNonEmptyMB(info.Name, name)
		path, err := saveInlineMailFile(noteID, fileName, raw)
		if err != nil {
			return DesktopBotMailFileData{}, err
		}
		rememberNewDelivery(noteID, fileName, path)
		rememberMailNote(noteID, fileName, path)
		return DesktopBotMailFileData{Name: fileName, MIME: firstNonEmptyMB(info.MIME, "application/octet-stream"), LocalPath: path, Size: int64(len(raw))}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	path, err := a.saveBotStoredFile(ctx, info)
	if err != nil {
		if saved, ok := mailFileSavedCopy(noteID, firstNonEmptyMB(info.Name, name)); ok {
			return saved, nil
		}
		return DesktopBotMailFileData{}, err
	}
	rememberMailNote(noteID, firstNonEmptyMB(info.Name, name), path)
	return DesktopBotMailFileData{Name: filepath.Base(path), MIME: info.MIME, LocalPath: path, Size: info.Size}, nil
}
