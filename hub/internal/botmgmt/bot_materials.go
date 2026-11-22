package botmgmt

import (
	"context"
	"encoding/base64"
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
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// DesktopUploader is the optional upload channel of DesktopControl (Hub's
// desktop pool). It streams one material file into the owner's container.
type DesktopUploader interface {
	UploadFile(ctx context.Context, tenantID, userID, filePath string, body io.Reader, size int64) (string, error)
}

// DesktopFileWriter is the optional small-write channel of DesktopControl.
// It writes content (at most desktop.FileContentMax bytes) to one container
// path and creates parent directories.
type DesktopFileWriter interface {
	WriteFile(ctx context.Context, tenantID, userID, filePath, content string) (string, error)
}

// DesktopCommandRunner is the optional command channel of DesktopControl. It
// runs one short shell command inside a bot's container and returns its
// output. A relayed attachment uses it to place a file next to its task.
type DesktopCommandRunner interface {
	RunCommand(ctx context.Context, tenantID, userID, command string) (string, error)
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

// The id is also a staged file name segment and a lock key, so it must start
// with a letter or digit: a leading dash is an invalid Windows file name.
var materialUploadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$`)

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
	// A bot with no computer has no desktop folder. Staging the bytes and
	// then calling UploadFile would wake the owner's desktop.
	if botNone(bot) {
		return nil, ErrNoDesktop
	}
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
	_, putErr := up.UploadFile(desktoppool.WithComputer(ctx, bot.NodeID), tenantID, owner, target, src, have)
	_ = src.Close()
	if putErr != nil {
		return nil, explainDesktopErr(putErr)
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

// ---------------------------------------------------------------------------
// Remote-control attachments
//
// A file the person attaches to a task on another machine. The controller
// cannot write to that machine's disk; the Hub takes the chunks and places
// the file next to the task, inside the task directory:
//
//	<task dir>/attachments/<upload id>-<name>
//
// Two shapes exist:
//   - The controller is a bot that owns the task: wrote by that bot's own
//     container (WriteFile).
//   - The controller is a phone (no container): the bytes land in the
//     caller's own desktop under ~/.maclaw-attach/, then the controlled bot's
//     container copies that file next to the task (both sides are inside
//     containers; this Hub only hands over a path).
// ---------------------------------------------------------------------------

// attachmentsFolder is the per-task folder that holds remote attachments.
const attachmentsFolder = "attachments"

// AttachmentRef names where one remote attachment goes.
type AttachmentRef struct {
	GrantID  string
	UploadID string
	Name     string
	// Path is the task directory as the machine that runs the task sees it,
	// i.e. with container paths (/home/desktop/...) when a bot owns the task.
	Path string
	// BotID is the bot whose container holds the task; empty means the task
	// lives on a desktop this Hub cannot reach directly (a desktop app
	// reporting a host path), and the write is refused.
	BotID  string
	NodeID string
}

// AttachmentChunk is one piece of a file headed for [AttachmentRef].
type AttachmentChunk struct {
	Ref    AttachmentRef
	Offset int64
	Total  int64
	Data   []byte
}

// AttachmentStaged is the state after one chunk. Path is set once the whole
// file is in the task directory.
type AttachmentStaged struct {
	UploadID string `json:"upload_id"`
	Name     string `json:"name"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Done     bool   `json:"done"`
	Path     string `json:"path,omitempty"`
}

var attachmentGrantID = regexp.MustCompile(`^[A-Za-z0-9_-]{4,80}$`)

func sanitizeAttachmentGrantID(value string) string {
	value = strings.TrimSpace(value)
	if !attachmentGrantID.MatchString(value) {
		return ""
	}
	return value
}

func attachmentStageDir(root, tenantID, grantID string) string {
	return filepath.Join(root, "remote-attachments", safeStageKey(tenantID), safeStageKey(grantID))
}

func readAttachmentMeta(path string) (string, int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		return "", 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil || total <= 0 {
		return "", 0, false
	}
	return lines[0], total, true
}

func writeAttachmentMeta(path, name string, total int64) error {
	return os.WriteFile(path, []byte(name+"\n"+strconv.FormatInt(total, 10)+"\n"), 0o600)
}

// StageAttachmentChunk stages one chunk of a remote-control attachment and,
// on the last byte, places the whole file in the task's attachments folder.
//
// Identity and permission are the caller's job (it has the grant); this
// function guarantees that nothing outside the task directory can be
// written: the name goes through SafeMaterialName, the upload id must match
// the phone's format, and the target is always
// <Path>/attachments/<upload id>-<name>.
func (s *Service) StageAttachmentChunk(ctx context.Context, tenantID string, chunk AttachmentChunk) (*AttachmentStaged, error) {
	ref := chunk.Ref
	grantID := sanitizeAttachmentGrantID(ref.GrantID)
	if grantID == "" {
		return nil, fmt.Errorf("%w: grant id is invalid", ErrInvalidInput)
	}
	taskDir := strings.TrimSpace(ref.Path)
	if taskDir == "" || filepath.Base(taskDir) == "." || filepath.Base(taskDir) == string(filepath.Separator) {
		return nil, fmt.Errorf("%w: task directory is invalid", ErrInvalidInput)
	}
	if strings.TrimSpace(ref.BotID) == "" {
		// A host path from a desktop app cannot be written by this Hub: the
		// file would land outside the container that runs the task.
		return nil, fmt.Errorf("%w: the task is not on a computer this Hub can write to", ErrInvalidInput)
	}
	if !materialUploadID.MatchString(ref.UploadID) {
		return nil, fmt.Errorf("%w: upload id is invalid", ErrInvalidInput)
	}
	name := desktop.SafeMaterialName(ref.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: file name is invalid", ErrInvalidInput)
	}
	if chunk.Total <= 0 || chunk.Total > desktop.UploadMax {
		return nil, fmt.Errorf("%w: file must be 1 byte to %d MB", ErrInvalidInput, desktop.UploadMax>>20)
	}
	if len(chunk.Data) > desktop.UploadChunkMax || chunk.Offset < 0 || chunk.Offset+int64(len(chunk.Data)) > chunk.Total {
		return nil, fmt.Errorf("%w: chunk is out of range", ErrInvalidInput)
	}
	writer, ok := s.Desktop.(DesktopFileWriter)
	if !ok || writer == nil {
		return nil, fmt.Errorf("%w: desktop writes are unavailable", ErrSrv)
	}
	stored := ref.UploadID + "-" + name

	dir := attachmentStageDir(s.materialStageDir(), tenantID, grantID)
	unlock := lockMaterialUpload(dir + "/attachment-" + ref.UploadID)
	defer unlock.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	sweepMaterialStage(dir, time.Now())
	part := filepath.Join(dir, ref.UploadID+".part")
	meta := filepath.Join(dir, ref.UploadID+".meta")
	donePath := filepath.Join(dir, ref.UploadID+".done")
	// Same idempotency rule as a material upload: a retried last chunk must
	// not place the file twice.
	if finished, ok := readMaterialDone(donePath); ok && finished.Name == stored && finished.Total == chunk.Total {
		_ = os.Remove(part)
		_ = os.Remove(meta)
		return &AttachmentStaged{UploadID: ref.UploadID, Name: name, Received: finished.Received, Total: chunk.Total, Done: true, Path: finished.Path}, nil
	}
	if chunk.Offset == 0 {
		_ = os.Remove(meta)
		_ = os.Remove(donePath)
		if err := writeAttachmentMeta(meta, stored, chunk.Total); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
		}
	} else if pinned, total, ok := readAttachmentMeta(meta); !ok || pinned != stored || total != chunk.Total {
		return nil, fmt.Errorf("%w: upload changed", ErrInvalidInput)
	}
	have := int64(0)
	if chunk.Offset != 0 {
		if info, err := os.Stat(part); err == nil {
			have = info.Size()
		}
	}
	out := &AttachmentStaged{UploadID: ref.UploadID, Name: name, Received: have, Total: chunk.Total}
	switch {
	case chunk.Offset == 0:
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
		// Retry of the last chunk after a failed write.
		out.Received = have
	default:
		return out, fmt.Errorf("%w: expected offset %d", ErrInvalidInput, have)
	}
	raw, err := os.ReadFile(part)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	if int64(len(raw)) != chunk.Total {
		return nil, fmt.Errorf("%w: upload size changed", ErrInvalidInput)
	}
	// The bot's own file API is a base64 JSON string; a larger file needs a
	// stream. Refuse it with a clear message and drop the staged bytes.
	if int64(len(raw)) > desktop.FileContentMax {
		_ = os.Remove(part)
		_ = os.Remove(meta)
		return nil, fmt.Errorf("%w: file must be %d KB or less", ErrInvalidInput, desktop.FileContentMax>>10)
	}
	target := filepath.ToSlash(filepath.Join(taskDir, attachmentsFolder, stored))
	if err := s.placeAttachment(ctx, tenantID, ref, target, raw); err != nil {
		return nil, err
	}
	out.Done = true
	out.Path = target
	if err := writeMaterialDone(donePath, stored, target, have); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	_ = os.Remove(part)
	_ = os.Remove(meta)
	return out, nil
}

// placeAttachment writes the finished file into the bot's container that runs
// the task. Bytes travel as base64 inside one file call (the container's own
// API), and then a container copy moves it to the task directory; the copy is
// what keeps a large file from arriving twice over the JSON path.
func (s *Service) placeAttachment(ctx context.Context, tenantID string, ref AttachmentRef, target string, raw []byte) error {
	writer, ok := s.Desktop.(DesktopFileWriter)
	if !ok || writer == nil {
		return fmt.Errorf("%w: desktop writes are unavailable", ErrSrv)
	}
	botCtx := desktoppool.WithComputer(ctx, ref.NodeID)
	stored := filepath.Base(target)
	staging := desktop.ContainerHome + "/.maclaw-attach/" + safeStageKey(ref.GrantID) + "/" + stored
	if _, err := writer.WriteFile(botCtx, tenantID, ref.BotID, staging, base64.StdEncoding.EncodeToString(raw)); err != nil {
		return explainDesktopErr(err)
	}
	runner, ok := s.Desktop.(DesktopCommandRunner)
	if !ok || runner == nil {
		return fmt.Errorf("%w: desktop commands are unavailable", ErrSrv)
	}
	command, err := desktop.RenderAttachmentPlace(staging, target)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	argv := strings.Join(command, " ")
	if _, err := runner.RunCommand(botCtx, tenantID, ref.BotID, argv); err != nil {
		return explainDesktopErr(err)
	}
	return nil
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
	if len(lines) < 3 {
		return MaterialUpload{}, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
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
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		return "", 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return "", 0, false
	}
	return lines[0], total, true
}

func safeStageKey(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "_"
	}
	return out
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
