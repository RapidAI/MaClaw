package botmgmt

// bot_mail_files.go: files a bot shares in a message mid-run. The sender
// (MaClawSrv notify_main with paths) uploads the bytes with the message, Hub
// keeps them content-addressed next to the mailbox, and the receiver (the
// GUI main bot, or anything reading the mailbox) fetches them as soon as the
// message arrives, not only when the sender's run ends.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	mailFileMax      = 8 << 20 // per file, decoded
	mailFilesPerNote = 3
	mailFileKeep     = 8 * 24 * time.Hour // a little longer than the 7-day mailbox
)

// BotMailFile is one stored file on a message. SHA names the bytes on Hub.
type BotMailFile struct {
	Name string `json:"name"`
	MIME string `json:"mime,omitempty"`
	Size int    `json:"size"`
	SHA  string `json:"sha256"`
}

func cleanMailFiles(in []BotMailFile) []BotMailFile {
	out := []BotMailFile{}
	for _, f := range in {
		name := mailFileName(f.Name)
		if name == "" || len(f.SHA) != 64 || len(out) >= mailFilesPerNote {
			continue
		}
		out = append(out, BotMailFile{Name: name, MIME: strings.TrimSpace(f.MIME), Size: f.Size, SHA: strings.ToLower(f.SHA)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mailFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	return name
}

func (s *Service) mailFileDir(tenantID string) string {
	base := ""
	if s != nil {
		base = strings.TrimSpace(s.MailFileDir)
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "maclaw-bot-mail")
	}
	sum := sha256.Sum256([]byte("tenant:" + strings.TrimSpace(tenantID)))
	return filepath.Join(base, hex.EncodeToString(sum[:8]))
}

// StoreBotMailFile keeps one shared file and returns its message entry.
func (s *Service) StoreBotMailFile(tenantID, name, mime string, data []byte) (BotMailFile, error) {
	name = mailFileName(name)
	if name == "" || len(data) == 0 || len(data) > mailFileMax {
		return BotMailFile{}, ErrInvalidInput
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	dir := s.mailFileDir(tenantID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BotMailFile{}, err
	}
	s.pruneMailFiles(dir)
	dst := filepath.Join(dir, sha)
	if _, err := os.Stat(dst); err != nil {
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return BotMailFile{}, err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return BotMailFile{}, err
		}
	} else {
		now := time.Now()
		_ = os.Chtimes(dst, now, now)
	}
	return BotMailFile{Name: name, MIME: strings.TrimSpace(mime), Size: len(data), SHA: sha}, nil
}

func (s *Service) pruneMailFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cut := time.Now().Add(-mailFileKeep)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().Before(cut) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ReadBotMailFile returns a file on one of the user's messages. The note
// must belong to this user; botID, when set, must be its sender or target.
func (s *Service) ReadBotMailFile(ctx context.Context, tenantID, userID, botID, noteID, name string) (BotMailFile, []byte, error) {
	notes, err := s.ListBotCrossNotes(ctx, tenantID, userID)
	if err != nil {
		return BotMailFile{}, nil, err
	}
	for _, n := range notes {
		if n.ID != strings.TrimSpace(noteID) {
			continue
		}
		if botID != "" && n.From != botID && n.Target != botID {
			return BotMailFile{}, nil, ErrNotFound
		}
		for _, f := range n.Files {
			if f.Name != strings.TrimSpace(name) && f.SHA != strings.TrimSpace(name) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(s.mailFileDir(tenantID), f.SHA))
			if err != nil {
				return BotMailFile{}, nil, ErrNotFound
			}
			return f, data, nil
		}
	}
	return BotMailFile{}, nil, ErrNotFound
}
