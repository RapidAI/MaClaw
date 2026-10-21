package botmgmt

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// DesktopUploader is the optional upload channel of DesktopControl (Hub's
// desktop pool). It streams one material file into the owner's container.
type DesktopUploader interface {
	UploadFile(ctx context.Context, tenantID, userID, filePath string, body io.Reader, size int64) (string, error)
}

// MaterialChunk is one piece of a material upload from the bot's owner.
// Offset is where Data starts; Total is the whole file size.
type MaterialChunk struct {
	UploadID string
	Name     string
	Offset   int64
	Total    int64
	Data     []byte
}

// MaterialUpload is the state after one chunk. Path is set (as ~/素材/…)
// once the last chunk is in and the file is on the desktop.
type MaterialUpload struct {
	UploadID string `json:"upload_id"`
	Name     string `json:"name"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Done     bool   `json:"done"`
	Path     string `json:"path,omitempty"`
}

var materialUploadID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// materialStageTTL drops a staged part nobody finished.
const materialStageTTL = time.Hour

var materialStageMu sync.Mutex

func (s *Service) materialStageDir() string {
	if s != nil && strings.TrimSpace(s.MaterialStageDir) != "" {
		return s.MaterialStageDir
	}
	return filepath.Join(os.TempDir(), "maclaw-bot-materials")
}

// UploadMaterialChunk stages one chunk of a file for the owner's bot and,
// on the last chunk, sends the whole file to ~/素材/<bot folder>/ on the
// owner's desktop. Only the owner may upload; any other bot is not found.
// The folder is derived here from the bot, never from the client.
func (s *Service) UploadMaterialChunk(ctx context.Context, tenantID, userID, botID string, chunk MaterialChunk) (*MaterialUpload, error) {
	userID = strings.TrimSpace(userID)
	if !materialUploadID.MatchString(chunk.UploadID) {
		return nil, fmt.Errorf("%w: upload id is invalid", ErrInvalidInput)
	}
	name := desktop.SafeMaterialName(chunk.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: file name is invalid", ErrInvalidInput)
	}
	if chunk.Total <= 0 || chunk.Total > desktop.UploadMax {
		return nil, fmt.Errorf("%w: file must be 1 byte to %d MB", ErrInvalidInput, desktop.UploadMax>>20)
	}
	if len(chunk.Data) > desktop.UploadChunkMax || chunk.Offset < 0 || chunk.Offset+int64(len(chunk.Data)) > chunk.Total {
		return nil, fmt.Errorf("%w: chunk is out of range", ErrInvalidInput)
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return nil, ErrNotFound
	}
	bot := rec.Bots[index]
	var others []string
	for i, other := range rec.Bots {
		if i != index && other.OwnerUserID == userID {
			others = append(others, other.Name)
		}
	}
	target, err := desktop.MaterialPath(desktop.MaterialsFolder(bot.ID, bot.Name, others), name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	owner, _ := desktop.SplitDesktopUser(userID)

	materialStageMu.Lock()
	defer materialStageMu.Unlock()
	dir := filepath.Join(s.materialStageDir(), safeStageKey(tenantID), safeStageKey(owner), safeStageKey(bot.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	sweepMaterialStage(dir, time.Now())
	part := filepath.Join(dir, chunk.UploadID+".part")
	have := int64(0)
	if info, err := os.Stat(part); err == nil {
		have = info.Size()
	}
	if chunk.Offset == 0 && have > 0 {
		have = 0 // a restart from zero replaces the stale part
		_ = os.Remove(part)
	}
	if chunk.Offset != have {
		return &MaterialUpload{UploadID: chunk.UploadID, Name: name, Received: have, Total: chunk.Total}, fmt.Errorf("%w: expected offset %d", ErrInvalidInput, have)
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	_, werr := f.Write(chunk.Data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(part)
		return nil, fmt.Errorf("%w: staging failed", ErrSrv)
	}
	have += int64(len(chunk.Data))
	out := &MaterialUpload{UploadID: chunk.UploadID, Name: name, Received: have, Total: chunk.Total}
	if have < chunk.Total {
		return out, nil
	}
	defer os.Remove(part)
	up, ok := s.Desktop.(DesktopUploader)
	if !ok || up == nil {
		return nil, fmt.Errorf("%w: desktop uploads are unavailable", ErrSrv)
	}
	src, err := os.Open(part)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	defer src.Close()
	if _, err := up.UploadFile(ctx, tenantID, owner, target, src, have); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	out.Done = true
	out.Path = desktop.MaterialDisplayPath(target)
	return out, nil
}

func safeStageKey(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "~%x", r)
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func sweepMaterialStage(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > materialStageTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
