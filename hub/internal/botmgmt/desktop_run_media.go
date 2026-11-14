package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopRunMedia is the pictures and files of one finished run. The pin
// keeps the text. These bytes live in their own file so a later GET can
// hand them back after the process restarts.
const (
	desktopRunMediaTTL = 14 * 24 * time.Hour
	desktopRunMediaCap = 32 << 20
)

type desktopRunMedia struct {
	SavedAt time.Time    `json:"saved_at,omitempty"`
	Images  []ReplyImage `json:"images,omitempty"`
	Files   []ReplyFile  `json:"files,omitempty"`
}

func replyMediaID(data string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(data)))
	return "img-" + hex.EncodeToString(sum[:8])
}

func (s *Service) keepDesktopRunMedia(tenantID, userID, botID, runID string, images []ReplyImage, files []ReplyFile) error {
	if s == nil || (len(images) == 0 && len(files) == 0) {
		return nil
	}
	for i := range images {
		if strings.TrimSpace(images[i].ID) == "" {
			images[i].ID = replyMediaID(images[i].Data)
		}
	}
	dir := s.desktopRunMediaDir(tenantID, userID, botID)
	if dir == "" || !safeMediaID(runID) {
		return ErrInvalidInput
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(desktopRunMedia{SavedAt: time.Now(), Images: images, Files: files})
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, safeMediaName(runID)+".json")
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		log.Printf("[desktop-run-media] save bot=%s run=%s: %v", botID, runID, err)
		return err
	}
	s.pruneDesktopRunMedia(dir)
	return nil
}

// forgetDesktopRunMedia drops the pictures and files kept for one bot.
func (s *Service) forgetDesktopRunMedia(tenantID, userID, botID string) {
	dir := s.desktopRunMediaDir(tenantID, userID, botID)
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("[desktop-run-media] delete bot=%s: %v", botID, err)
	}
}

func (s *Service) pruneDesktopRunMedia(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type kept struct {
		path    string
		size    int64
		modTime time.Time
	}
	var files []kept
	var total int64
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		if !info.ModTime().IsZero() && now.Sub(info.ModTime()) > desktopRunMediaTTL {
			_ = os.Remove(full)
			continue
		}
		files = append(files, kept{path: full, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for len(files) > 0 && total > desktopRunMediaCap {
		_ = os.Remove(files[0].path)
		total -= files[0].size
		files = files[1:]
	}
}

func (s *Service) DesktopRunImages(tenantID, userID, botID, runID string) ([]ReplyImage, error) {
	media, err := s.loadDesktopRunMedia(tenantID, userID, botID, runID)
	if err != nil {
		return nil, err
	}
	if media.Images == nil {
		return []ReplyImage{}, nil
	}
	return media.Images, nil
}

func (s *Service) DesktopRunFiles(tenantID, userID, botID, runID string) ([]ReplyFile, error) {
	media, err := s.loadDesktopRunMedia(tenantID, userID, botID, runID)
	if err != nil {
		return nil, err
	}
	if media.Files == nil {
		return []ReplyFile{}, nil
	}
	return media.Files, nil
}

func (s *Service) loadDesktopRunMedia(tenantID, userID, botID, runID string) (desktopRunMedia, error) {
	dir := s.desktopRunMediaDir(tenantID, userID, botID)
	if dir == "" || !safeMediaID(runID) {
		return desktopRunMedia{}, ErrInvalidInput
	}
	raw, err := os.ReadFile(filepath.Join(dir, safeMediaName(runID)+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return desktopRunMedia{}, nil
		}
		return desktopRunMedia{}, err
	}
	var media desktopRunMedia
	if err := json.Unmarshal(raw, &media); err != nil {
		return desktopRunMedia{}, err
	}
	if !media.SavedAt.IsZero() && time.Since(media.SavedAt) > desktopRunMediaTTL {
		_ = os.Remove(filepath.Join(dir, safeMediaName(runID)+".json"))
		return desktopRunMedia{}, nil
	}
	return media, nil
}

func (s *Service) desktopRunMediaDir(tenantID, userID, botID string) string {
	if !safeMediaID(botID) {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)))
	return filepath.Join(s.mailFileDir(tenantID), "run-media", hex.EncodeToString(sum[:8]), safeMediaName(botID))
}

func safeMediaID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 80 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func safeMediaName(id string) string {
	return strings.TrimSpace(id)
}

// ReadDesktopContainerFile fetches one file from the bot desktop. read is
// the desktop file_bytes call. The path has to resolve inside that desktop's
// home, and the bytes have to fit the reply limit.
func (s *Service) ReadDesktopContainerFile(ctx context.Context, tenantID, userID, botID, rawPath string, read func(ctx context.Context, tenantID, desktopUser, path string) (string, error)) (name, mime, data string, err error) {
	resolved, err := DesktopContainerFilePath(rawPath)
	if err != nil {
		return "", "", "", err
	}
	if read == nil {
		return "", "", "", ErrSettingsUnavailable
	}
	desktopUser, err := s.desktopUserOf(tenantID, userID, botID)
	if err != nil {
		return "", "", "", err
	}
	encoded, err := read(ctx, tenantID, desktopUser, resolved)
	if err != nil {
		return "", "", "", err
	}
	if _, err := desktop.DecodeFileBytes(encoded); err != nil {
		return "", "", "", ErrInvalidInput
	}
	return path.Base(resolved), desktopFileMIME(resolved), encoded, nil
}

func desktopFileMIME(filePath string) string {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".cpp", ".go", ".py", ".md":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// DesktopContainerFilePath resolves a path the person asked to fetch from
// the bot desktop. Only the run work tree and the Desktop are accepted.
// A hidden directory is refused. The decision is the resolved path.
func DesktopContainerFilePath(raw string) (string, error) {
	resolved, err := desktop.DeliverableContainerPath(raw)
	if err != nil {
		return "", ErrInvalidInput
	}
	return resolved, nil
}
