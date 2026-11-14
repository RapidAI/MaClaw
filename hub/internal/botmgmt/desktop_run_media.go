package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopRunMedia is the pictures and files of one finished run. The pin
// keeps the text. These bytes live in their own file so a later GET can
// hand them back after the process restarts.
type desktopRunMedia struct {
	Images []ReplyImage `json:"images,omitempty"`
	Files  []ReplyFile  `json:"files,omitempty"`
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
	raw, err := json.Marshal(desktopRunMedia{Images: images, Files: files})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, safeMediaName(runID)+".json"), raw, 0o600)
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
	encoded, err := read(ctx, tenantID, s.desktopUserOf(tenantID, userID, botID), resolved)
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
// the bot desktop. Only a path inside that desktop's home is accepted.
// The decision is ContainerPath plus the home prefix, not a phrase in the reply.
func DesktopContainerFilePath(raw string) (string, error) {
	resolved, err := desktop.ContainerPath(raw)
	if err != nil {
		return "", err
	}
	home := desktop.ContainerHome
	if resolved != home && !strings.HasPrefix(resolved, home+"/") {
		return "", ErrInvalidInput
	}
	return resolved, nil
}
