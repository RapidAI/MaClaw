package botmgmt

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// One mutex per upload id. A 200 MB desktop put must not block every other
// user's chunk append, and two chunks of the same id must not interleave.
var (
	materialLockMu sync.Mutex
	materialLocks  = map[string]*sync.Mutex{}
)

func lockMaterialUpload(key string) *sync.Mutex {
	materialLockMu.Lock()
	mu := materialLocks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		materialLocks[key] = mu
	}
	materialLockMu.Unlock()
	mu.Lock()
	return mu
}

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
	var peers []desktop.MaterialPeer
	for i, other := range rec.Bots {
		if i != index && other.OwnerUserID == userID {
			peers = append(peers, desktop.MaterialPeer{ID: other.ID, Name: other.Name})
		}
	}
	stored := desktop.MaterialStoredName(name, chunk.UploadID)
	if stored == "" {
		return nil, fmt.Errorf("%w: file name is invalid", ErrInvalidInput)
	}
	target, err := desktop.MaterialPath(desktop.MaterialsFolder(bot.ID, bot.Name, peers), stored)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	owner, _ := desktop.SplitDesktopUser(userID)

	dir := filepath.Join(s.materialStageDir(), safeStageKey(tenantID), safeStageKey(owner), safeStageKey(bot.ID))
	unlock := lockMaterialUpload(dir + "/" + chunk.UploadID)
	defer unlock.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	sweepMaterialStage(dir, time.Now())
	part := filepath.Join(dir, chunk.UploadID+".part")
	meta := filepath.Join(dir, chunk.UploadID+".meta")
	donePath := filepath.Join(dir, chunk.UploadID+".done")
	// The upload id is the idempotency key. A lost success response retries
	// the same chunk; answer with the path already on the desktop instead of
	// treating the missing part as a different upload.
	if finished, ok := readMaterialDone(donePath); ok && finished.Name == stored && finished.Total == chunk.Total {
		_ = os.Remove(part)
		_ = os.Remove(meta)
		finished.UploadID = chunk.UploadID
		return &finished, nil
	}
	if chunk.Offset == 0 {
		_ = os.Remove(meta)
		_ = os.Remove(donePath)
		if err := writeMaterialMeta(meta, stored, chunk.Total); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
		}
	} else if pinned, total, ok := readMaterialMeta(meta); !ok || pinned != stored || total != chunk.Total {
		return nil, fmt.Errorf("%w: upload changed", ErrInvalidInput)
	}
	have := int64(0)
	if chunk.Offset != 0 {
		if info, err := os.Stat(part); err == nil {
			have = info.Size()
		}
	}
	out := &MaterialUpload{UploadID: chunk.UploadID, Name: stored, Received: have, Total: chunk.Total}
	switch {
	case chunk.Offset == 0:
		// Truncate. Unlink-then-append leaves the previous bytes in place
		// when the name is still open or locked, and the new chunk is
		// written onto the end of the old file.
		wrote, err := replaceMaterialChunk(part, chunk.Data)
		if err != nil {
			return nil, err
		}
		have = wrote
		out.Received = have
		if have < chunk.Total {
			return out, nil
		}
	case chunk.Offset == have:
		wrote, err := appendMaterialChunk(part, chunk.Data)
		if err != nil {
			return nil, err
		}
		have += wrote
		out.Received = have
		if have < chunk.Total {
			return out, nil
		}
	case have == chunk.Total && chunk.Offset < have:
		// The desktop put failed after the bytes were staged. Retry that put
		// and leave the part in place until it succeeds.
		out.Received = have
	default:
		return out, fmt.Errorf("%w: expected offset %d", ErrInvalidInput, have)
	}
	up, ok := s.Desktop.(DesktopUploader)
	if !ok || up == nil {
		return nil, fmt.Errorf("%w: desktop uploads are unavailable", ErrSrv)
	}
	src, err := os.Open(part)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	_, putErr := up.UploadFile(ctx, tenantID, owner, target, src, have)
	_ = src.Close()
	if putErr != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, putErr.Error())
	}
	out.Done = true
	out.Path = desktop.MaterialDisplayPath(target)
	out.Name = stored
	if err := writeMaterialDone(donePath, stored, out.Path, have); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	_ = os.Remove(part)
	_ = os.Remove(meta)
	return out, nil
}

func appendMaterialChunk(part string, data []byte) (int64, error) {
	return writeMaterialChunk(part, data, false)
}

// replaceMaterialChunk is an offset-0 restart. The new bytes replace the
// part; they are not appended to whatever unlink left behind.
func replaceMaterialChunk(part string, data []byte) (int64, error) {
	return writeMaterialChunk(part, data, true)
}

func writeMaterialChunk(part string, data []byte, truncate bool) (int64, error) {
	flag := os.O_CREATE | os.O_WRONLY
	if truncate {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_APPEND
	}
	f, err := os.OpenFile(part, flag, 0o600)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	n, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil || n != len(data) {
		_ = os.Remove(part)
		return 0, fmt.Errorf("%w: staging failed", ErrSrv)
	}
	return int64(n), nil
}

func writeMaterialMeta(path, name string, total int64) error {
	return os.WriteFile(path, []byte(name+"\n"+strconv.FormatInt(total, 10)+"\n"), 0o600)
}

func writeMaterialDone(path, name, display string, total int64) error {
	body := name + "\n" + strconv.FormatInt(total, 10) + "\n" + display + "\n"
	return os.WriteFile(path, []byte(body), 0o600)
}

func readMaterialDone(path string) (MaterialUpload, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return MaterialUpload{}, false
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 || lines[0] == "" || lines[2] == "" {
		return MaterialUpload{}, false
	}
	total, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil {
		return MaterialUpload{}, false
	}
	return MaterialUpload{Name: lines[0], Total: total, Received: total, Done: true, Path: lines[2]}, true
}

func readMaterialMeta(path string) (string, int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	name, total, ok := strings.Cut(strings.TrimRight(string(raw), "\n"), "\n")
	if !ok || name == "" {
		return "", 0, false
	}
	n, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return name, n, true
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
