package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
// only names them. This app downloads them over HTTP (Range, resume) into
// the bot's own folder (~/MaClaw/<bot> by default), shows a chip with the
// progress, and remembers where each one was saved.

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

// localRelayAgentForFiles is the running relay agent, or one made for
// downloads when the relay link has not started yet.
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
			Notify:      func(ev localrelay.TransferEvent) { a.emitEvent(localRelayTransferEvent, ev) },
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
)

func botFilesIndexPath() string {
	return filepath.Join(maclawpath.DataDir(), "bot_files_saved.json")
}

func botFilesIndex() map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(botFilesIndexPath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func botFilesRemember(ref, path string) {
	botFilesMu.Lock()
	defer botFilesMu.Unlock()
	idx := botFilesIndex()
	idx[ref] = path
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
		return p, nil
	}
	agent := a.localRelayAgentForFiles()
	path, err := agent.SaveDelivered(ctx, f.BotID, f.BotName, "", localrelay.FetchSpec{URL: f.URL, Ref: f.Ref, Name: f.Name, Size: f.Size, SHA256: f.SHA256})
	if err != nil {
		return "", err
	}
	botFilesRemember(f.Ref, path)
	log.Printf("[bot-files] saved ref=%s bot=%s size=%d path=%s", f.Ref, f.BotID, f.Size, path)
	return path, nil
}

// autoSaveBotFiles starts the download of every stored file on these
// messages that is not saved yet. A failure is tried again a minute later;
// a file the server no longer has is not tried again.
func (a *App) autoSaveBotFiles(items []BotSaidItem) {
	for _, item := range items {
		for _, f := range item.Files {
			ref := strings.TrimSpace(f.Ref)
			if ref == "" {
				continue
			}
			botFilesMu.Lock()
			started, busy := botFilesStarted[ref]
			skip := botFilesGivenUp[ref] || (busy && time.Since(started) < time.Minute)
			if !skip {
				botFilesStarted[ref] = time.Now()
			}
			botFilesMu.Unlock()
			if skip || botFileSavedPath(ref) != "" {
				continue
			}
			noteID, name := item.ID, f.Name
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
				defer cancel()
				info, err := a.mailFileInfo(ctx, noteID, name)
				if err == nil {
					_, err = a.saveBotStoredFile(ctx, info)
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

// fetchMailFileForChat is FetchDesktopBotMailFile's body: a large stored
// file is saved on this computer and its path returned; a small one comes
// back as base64 like before.
func (a *App) fetchMailFileForChat(noteID, name string) (DesktopBotMailFileData, error) {
	info, err := a.mailFileInfo(context.Background(), noteID, name)
	if err != nil {
		return DesktopBotMailFileData{}, err
	}
	if info.Ref == "" {
		raw, err := base64.StdEncoding.DecodeString(info.Data)
		if err != nil {
			return DesktopBotMailFileData{}, err
		}
		return DesktopBotMailFileData{Name: firstNonEmptyMB(info.Name, name), MIME: firstNonEmptyMB(info.MIME, "application/octet-stream"), Data: base64.StdEncoding.EncodeToString(raw)}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	path, err := a.saveBotStoredFile(ctx, info)
	if err != nil {
		return DesktopBotMailFileData{}, err
	}
	return DesktopBotMailFileData{Name: filepath.Base(path), MIME: info.MIME, LocalPath: path, Size: info.Size}, nil
}
