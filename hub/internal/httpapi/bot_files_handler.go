package httpapi

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/botfiles"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// botFiles is Hub's store for large bot deliveries (see package botfiles).
var botFiles *botfiles.Store

// BotFilesDirName is the store's folder under Hub's data dir.
const BotFilesDirName = "bot-files"

// botFileStoreDir picks the store's folder. MACLAW_HUB_BOT_FILES_DIR wins
// (botfiles.FromEnv); then a sibling of an operator-set bot mail dir; then
// Hub's persistent runtime data dir (the SQLite database's folder, e.g.
// /data/soft/hub/data on papers), so stored files survive restarts. Only
// without any data dir does it fall back to a temp folder.
func botFileStoreDir(mailDir, dataDir string) string {
	if strings.TrimSpace(mailDir) != "" {
		return filepath.Join(filepath.Dir(filepath.Clean(mailDir)), "maclaw-bot-files")
	}
	if dataDir = strings.TrimSpace(dataDir); dataDir != "" {
		if abs, err := filepath.Abs(dataDir); err == nil {
			dataDir = abs
		}
		return filepath.Join(dataDir, BotFilesDirName)
	}
	return filepath.Join(os.TempDir(), "maclaw-bot-files")
}

// newBotFileStore builds the store and starts its sweeper.
func newBotFileStore(mailDir, dataDir string) *botfiles.Store {
	s := botfiles.FromEnv(botFileStoreDir(mailDir, dataDir))
	s.Sweep()
	s.StartSweeper(10*time.Minute, nil)
	log.Printf("[bot-files] store=%s max_file=%d MB", s.Dir, s.MaxFile()>>20)
	return s
}

func botFileOwner(tenant, user string) (string, string) {
	owner, _ := desktop.SplitDesktopUser(strings.TrimSpace(user))
	return store.NormalizeTenantID(tenant), owner
}

func writeBotFileError(w http.ResponseWriter, err error, received int64) {
	status, code := http.StatusInternalServerError, "BOT_FILE_FAILED"
	switch {
	case errors.Is(err, botfiles.ErrNotFound):
		status, code = http.StatusNotFound, "BOT_FILE_NOT_FOUND"
	case errors.Is(err, botfiles.ErrExpired):
		status, code = http.StatusGone, "BOT_FILE_EXPIRED"
	case errors.Is(err, botfiles.ErrTooLarge):
		status, code = http.StatusRequestEntityTooLarge, "BOT_FILE_TOO_LARGE"
	case errors.Is(err, botfiles.ErrFull):
		status, code = http.StatusInsufficientStorage, "BOT_FILE_STORE_FULL"
	case errors.Is(err, botfiles.ErrOffset):
		status, code = http.StatusConflict, "BOT_FILE_OFFSET"
	case errors.Is(err, botfiles.ErrDenied):
		status, code = http.StatusForbidden, "BOT_FILE_DENIED"
	case errors.Is(err, botfiles.ErrInvalid):
		status, code = http.StatusBadRequest, "BOT_FILE_INVALID"
	}
	writeJSON(w, status, map[string]any{"ok": false, "code": code, "error": err.Error(), "received": received})
}

func botFileInfo(m botfiles.Meta) map[string]any {
	path, exp := botFiles.SignedPath(m)
	return map[string]any{"ok": true, "ref": m.ID, "name": m.Name, "mime": m.MIME, "size": m.Size, "sha256": m.SHA256, "url": path, "url_expires": exp.UTC().Format(time.RFC3339), "expires": m.Expires.UTC().Format(time.RFC3339), "bot_id": m.BotID, "bot_name": m.BotName}
}

// PostBotFileCreateHandler POST /api/v1/desktop-services/files (desktop API
// token): reserve one file for a bot's owner. The answer's upload_token lets
// exactly that file be written, so the cloud desktop can stream it itself.
func PostBotFileCreateHandler(bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID   string `json:"tenant_id"`
			UserID     string `json:"user_id"`
			InstanceID string `json:"instance_id"`
			Name       string `json:"name"`
			MIME       string `json:"mime"`
			Size       int64  `json:"size"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "BOT_FILE_INVALID", "invalid file request")
			return
		}
		tenant, owner := botFileOwner(in.TenantID, in.UserID)
		_, isolated := desktop.SplitDesktopUser(strings.TrimSpace(in.UserID))
		botID, botName := strings.TrimSpace(in.InstanceID), ""
		if bots != nil {
			bot, err := bots.BotByInstance(r.Context(), in.TenantID, owner, in.InstanceID)
			if err != nil && isolated != "" {
				bot, err = bots.BotByInstance(r.Context(), in.TenantID, owner, isolated)
			}
			if err == nil {
				botID, botName = bot.ID, bot.Name
			}
		}
		ctype := strings.TrimSpace(in.MIME)
		if ctype == "" {
			ctype = mime.TypeByExtension(strings.ToLower(filepath.Ext(in.Name)))
		}
		m, tok, err := botFiles.Create(tenant, owner, botID, botName, in.Name, ctype, in.Size)
		if err != nil {
			writeBotFileError(w, err, 0)
			return
		}
		log.Printf("[bot-files] create id=%s user=%s bot=%s name=%q size=%d", m.ID, owner, botID, m.Name, m.Size)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": m.ID, "upload_token": tok, "upload_path": "/api/v1/desktop-services/files/" + m.ID + "/data", "max_bytes": botFiles.MaxFile(), "complete": m.Complete})
	}
}

// PutBotFileDataHandler PUT /api/v1/desktop-services/files/{id}/data?offset=N
// with X-Upload-Token: append bytes. A 409 carries "received", where a
// broken upload resumes.
func PutBotFileDataHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		tok := r.Header.Get("X-Upload-Token")
		offset, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("offset")), 10, 64)
		if err != nil {
			offset = 0
		}
		m, err := botFiles.Append(id, tok, offset, r.Body)
		if err != nil {
			writeBotFileError(w, err, m.Received)
			return
		}
		if m.Complete {
			log.Printf("[bot-files] complete id=%s size=%d sha=%s", m.ID, m.Size, m.SHA256)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": m.ID, "received": m.Received, "size": m.Size, "complete": m.Complete, "sha256": m.SHA256})
	}
}

// GetBotFileStatusHandler GET /api/v1/desktop-services/files/{id}/status
// with X-Upload-Token: how much the server has.
func GetBotFileStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := botFiles.CheckUpload(r.PathValue("id"), r.Header.Get("X-Upload-Token"))
		if err != nil {
			writeBotFileError(w, err, 0)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": m.ID, "received": m.Received, "size": m.Size, "complete": m.Complete, "sha256": m.SHA256})
	}
}

// PostBotFileLinkHandler POST /api/v1/desktop-services/files/{id}/link
// (desktop API token) {tenant_id, user_id}: a finished file's reference
// and a fresh signed download path for its owner.
func PostBotFileLinkHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "BOT_FILE_INVALID", "invalid link request")
			return
		}
		tenant, owner := botFileOwner(in.TenantID, in.UserID)
		m, err := botFiles.Get(tenant, owner, r.PathValue("id"))
		if err != nil {
			writeBotFileError(w, err, 0)
			return
		}
		writeJSON(w, http.StatusOK, botFileInfo(m))
	}
}

// GetBotFileHandler GET /api/v1/bot-files/{id}?exp=&sig=: the person's
// MaClaw app downloads a stored file. Both the signed, short-lived path and
// the person's own machine credential are required; Range is supported.
func GetBotFileHandler(identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		tenant, owner := botFileOwner(principal.TenantID, principal.UserID)
		m, err := botFiles.Get(tenant, owner, r.PathValue("id"))
		if err != nil {
			writeBotFileError(w, err, 0)
			return
		}
		if err := botFiles.CheckSigned(m, r.URL.Query().Get("exp"), r.URL.Query().Get("sig")); err != nil {
			if errors.Is(err, botfiles.ErrExpired) {
				// The link expired, not the file: the app asks for a new link.
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "BOT_FILE_LINK_EXPIRED", "error": "the download link expired; ask for a new one"})
				return
			}
			writeBotFileError(w, err, 0)
			return
		}
		f, err := botFiles.Open(m)
		if err != nil {
			writeBotFileError(w, botfiles.ErrNotFound, 0)
			return
		}
		defer f.Close()
		if m.MIME != "" {
			w.Header().Set("Content-Type", m.MIME)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", botFileNameEscape(m.Name)))
		w.Header().Set("X-Content-SHA256", m.SHA256)
		w.Header().Set("Cache-Control", "private, no-store")
		if r.Header.Get("Range") == "" {
			defer botFiles.NoteDownload(m.ID)
		}
		http.ServeContent(w, r, "", m.Created, f)
	}
}

// PostBotFileRefreshHandler POST /api/v1/bot-files/{id}/link: a new signed
// path for the person's own file, while the store still keeps it.
func PostBotFileRefreshHandler(identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		tenant, owner := botFileOwner(principal.TenantID, principal.UserID)
		m, err := botFiles.Get(tenant, owner, r.PathValue("id"))
		if err != nil {
			writeBotFileError(w, err, 0)
			return
		}
		writeJSON(w, http.StatusOK, botFileInfo(m))
	}
}

func botFileNameEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
