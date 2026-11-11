// Package botfiles is Hub's store for files bots deliver that are too large
// to inline in a chat message. MaClawSrv (or the bot's cloud desktop, with a
// one-file upload token) streams the bytes in; the person's MaClaw app
// downloads them over HTTP with Range through a short-lived signed URL that
// also needs that person's own credential. Nothing here is public.
package botfiles

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults; each has an environment override (see FromEnv).
const (
	DefaultMaxFileBytes  int64 = 2 << 30  // MACLAW_HUB_BOT_FILE_MAX_MB
	DefaultMaxTotalBytes int64 = 20 << 30 // MACLAW_HUB_BOT_FILES_TOTAL_GB
	DefaultRetention           = 72 * time.Hour
	DefaultURLTTL              = time.Hour
	// An upload nobody finished is dropped after this long without bytes.
	StaleUploadAfter = 6 * time.Hour
)

var (
	ErrNotFound = errors.New("file not found")
	ErrExpired  = errors.New("file expired")
	ErrTooLarge = errors.New("file too large")
	ErrFull     = errors.New("file store is full")
	ErrOffset   = errors.New("upload offset does not match")
	ErrDenied   = errors.New("not allowed")
	ErrInvalid  = errors.New("invalid file")
)

// Meta is one stored file.
type Meta struct {
	ID         string    `json:"id"`
	Tenant     string    `json:"tenant"`
	Owner      string    `json:"owner"`
	BotID      string    `json:"bot_id,omitempty"`
	BotName    string    `json:"bot_name,omitempty"`
	Name       string    `json:"name"`
	MIME       string    `json:"mime,omitempty"`
	Size       int64     `json:"size"`
	Received   int64     `json:"received"`
	SHA256     string    `json:"sha256,omitempty"`
	Complete   bool      `json:"complete"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
	Expires    time.Time `json:"expires"`
	UploadHash string    `json:"upload_hash"`
	Downloads  int       `json:"downloads,omitempty"`
}

// Store keeps files under Dir/<id>/{meta.json,data}.
type Store struct {
	Dir           string
	MaxFileBytes  int64
	MaxTotalBytes int64
	Retention     time.Duration
	URLTTL        time.Duration
	Now           func() time.Time

	mu      sync.Mutex
	key     []byte
	writing map[string]bool
}

// FromEnv builds the store from the environment. dir is used when
// MACLAW_HUB_BOT_FILES_DIR is empty.
func FromEnv(dir string) *Store {
	s := &Store{Dir: dir}
	if v := strings.TrimSpace(os.Getenv("MACLAW_HUB_BOT_FILES_DIR")); v != "" {
		s.Dir = v
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("MACLAW_HUB_BOT_FILE_MAX_MB")), 10, 64); err == nil && n > 0 {
		s.MaxFileBytes = n << 20
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("MACLAW_HUB_BOT_FILES_TOTAL_GB")), 10, 64); err == nil && n > 0 {
		s.MaxTotalBytes = n << 30
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_HUB_BOT_FILES_KEEP_HOURS"))); err == nil && n > 0 {
		s.Retention = time.Duration(n) * time.Hour
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_HUB_BOT_FILE_URL_MINUTES"))); err == nil && n > 0 {
		s.URLTTL = time.Duration(n) * time.Minute
	}
	return s
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) maxFile() int64 {
	if s.MaxFileBytes > 0 {
		return s.MaxFileBytes
	}
	return DefaultMaxFileBytes
}

func (s *Store) maxTotal() int64 {
	if s.MaxTotalBytes > 0 {
		return s.MaxTotalBytes
	}
	return DefaultMaxTotalBytes
}

func (s *Store) retention() time.Duration {
	if s.Retention > 0 {
		return s.Retention
	}
	return DefaultRetention
}

// URLLifetime is how long a signed download URL stays valid.
func (s *Store) URLLifetime() time.Duration {
	if s.URLTTL > 0 {
		return s.URLTTL
	}
	return DefaultURLTTL
}

// MaxFile is the per-file cap in bytes.
func (s *Store) MaxFile() int64 { return s.maxFile() }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func validID(id string) bool {
	if len(id) != 26 || !strings.HasPrefix(id, "bf_") {
		return false
	}
	for _, c := range id[3:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) dirOf(id string) string { return filepath.Join(s.Dir, id) }

func (s *Store) readMeta(id string) (Meta, error) {
	if !validID(id) {
		return Meta{}, ErrNotFound
	}
	raw, err := os.ReadFile(filepath.Join(s.dirOf(id), "meta.json"))
	if err != nil {
		return Meta{}, ErrNotFound
	}
	var m Meta
	if json.Unmarshal(raw, &m) != nil {
		return Meta{}, ErrNotFound
	}
	return m, nil
}

func (s *Store) writeMeta(m Meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p := filepath.Join(s.dirOf(m.ID), "meta.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func cleanName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "." || name == "/" || name == ".." || name == "" {
		return ""
	}
	if r := []rune(name); len(r) > 180 {
		name = string(r[len(r)-180:])
	}
	return name
}

// Create reserves room for one file and returns its meta and upload token.
func (s *Store) Create(tenant, owner, botID, botName, name, mime string, size int64) (Meta, string, error) {
	name = cleanName(name)
	tenant, owner = strings.TrimSpace(tenant), strings.TrimSpace(owner)
	if name == "" || owner == "" || size < 0 {
		return Meta{}, "", ErrInvalid
	}
	if size > s.maxFile() {
		return Meta{}, "", fmt.Errorf("%w: %d bytes, the limit is %d bytes", ErrTooLarge, size, s.maxFile())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Meta{}, "", err
	}
	s.sweepLocked()
	if used := s.usedLocked(); used+size > s.maxTotal() {
		s.evictLocked(used + size - s.maxTotal())
		if s.usedLocked()+size > s.maxTotal() {
			return Meta{}, "", ErrFull
		}
	}
	now := s.now()
	tok := randomHex(32)
	m := Meta{ID: "bf_" + randomHex(12)[:23], Tenant: tenant, Owner: owner, BotID: strings.TrimSpace(botID), BotName: strings.TrimSpace(botName), Name: name, MIME: strings.TrimSpace(mime), Size: size, Created: now, Updated: now, Expires: now.Add(s.retention()), UploadHash: hashToken(tok)}
	if err := os.MkdirAll(s.dirOf(m.ID), 0o700); err != nil {
		return Meta{}, "", err
	}
	if err := os.WriteFile(filepath.Join(s.dirOf(m.ID), "data"), nil, 0o600); err != nil {
		return Meta{}, "", err
	}
	if size == 0 {
		sum := sha256.Sum256(nil)
		m.Complete, m.SHA256 = true, hex.EncodeToString(sum[:])
	}
	if err := s.writeMeta(m); err != nil {
		return Meta{}, "", err
	}
	return m, tok, nil
}

// CheckUpload returns the meta when tok is this file's upload token.
func (s *Store) CheckUpload(id, tok string) (Meta, error) {
	m, err := s.readMeta(id)
	if err != nil {
		return Meta{}, err
	}
	got := hashToken(strings.TrimSpace(tok))
	if subtle.ConstantTimeCompare([]byte(got), []byte(m.UploadHash)) != 1 {
		return Meta{}, ErrDenied
	}
	return m, nil
}

// Append writes body at offset (which must equal what was received so far)
// and completes the file when every byte is in. It returns the new meta.
func (s *Store) Append(id, tok string, offset int64, body io.Reader) (Meta, error) {
	m, err := s.CheckUpload(id, tok)
	if err != nil {
		return Meta{}, err
	}
	if m.Complete {
		return m, nil
	}
	s.mu.Lock()
	if s.writing == nil {
		s.writing = map[string]bool{}
	}
	if s.writing[id] {
		s.mu.Unlock()
		return m, fmt.Errorf("%w: another upload of this file is running", ErrOffset)
	}
	s.writing[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.writing, id)
		s.mu.Unlock()
	}()
	m, _ = s.readMeta(id)
	if offset != m.Received {
		return m, fmt.Errorf("%w: the server has %d bytes", ErrOffset, m.Received)
	}
	f, err := os.OpenFile(filepath.Join(s.dirOf(id), "data"), os.O_WRONLY, 0o600)
	if err != nil {
		return m, err
	}
	if err := f.Truncate(m.Received); err != nil {
		f.Close()
		return m, err
	}
	if _, err := f.Seek(m.Received, io.SeekStart); err != nil {
		f.Close()
		return m, err
	}
	// One byte more than allowed tells a too-long upload apart.
	n, cerr := io.Copy(f, io.LimitReader(body, m.Size-m.Received+1))
	if serr := f.Sync(); cerr == nil {
		cerr = serr
	}
	f.Close()
	if m.Received+n > m.Size {
		_ = os.Truncate(filepath.Join(s.dirOf(id), "data"), m.Received)
		return m, fmt.Errorf("%w: more bytes than the declared %d", ErrTooLarge, m.Size)
	}
	m.Received += n
	m.Updated = s.now()
	if m.Received == m.Size {
		sum, herr := fileSHA(filepath.Join(s.dirOf(id), "data"))
		if herr != nil {
			return m, herr
		}
		m.SHA256, m.Complete = sum, true
		m.Expires = s.now().Add(s.retention())
	}
	if err := s.writeMeta(m); err != nil {
		return m, err
	}
	return m, cerr
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Get returns a finished file of this owner.
func (s *Store) Get(tenant, owner, id string) (Meta, error) {
	m, err := s.readMeta(id)
	if err != nil {
		return Meta{}, err
	}
	if m.Tenant != strings.TrimSpace(tenant) || m.Owner != strings.TrimSpace(owner) {
		return Meta{}, ErrNotFound
	}
	if s.now().After(m.Expires) {
		return Meta{}, ErrExpired
	}
	if !m.Complete {
		return m, ErrNotFound
	}
	return m, nil
}

// Open opens a finished file's bytes. The caller closes it.
func (s *Store) Open(m Meta) (*os.File, error) {
	return os.Open(filepath.Join(s.dirOf(m.ID), "data"))
}

// NoteDownload counts one finished download.
func (s *Store) NoteDownload(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, err := s.readMeta(id); err == nil {
		m.Downloads++
		_ = s.writeMeta(m)
	}
}

func (s *Store) signKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key != nil {
		return s.key
	}
	p := filepath.Join(s.Dir, ".sign-key")
	if raw, err := os.ReadFile(p); err == nil && len(raw) >= 32 {
		s.key = raw
		return s.key
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	_ = os.MkdirAll(s.Dir, 0o700)
	_ = os.WriteFile(p, key, 0o600)
	s.key = key
	return key
}

func (s *Store) sig(m Meta, exp int64) string {
	mac := hmac.New(sha256.New, s.signKey())
	fmt.Fprintf(mac, "%s\n%s\n%s\n%d", m.ID, m.Tenant, m.Owner, exp)
	return hex.EncodeToString(mac.Sum(nil))[:40]
}

// DownloadPrefix starts the person's signed download path (relative to Hub).
const DownloadPrefix = "/api/v1/bot-files/"

// SignedPath returns a download path valid until the returned time.
func (s *Store) SignedPath(m Meta) (string, time.Time) {
	exp := s.now().Add(s.URLLifetime())
	if exp.After(m.Expires) {
		exp = m.Expires
	}
	e := exp.Unix()
	return fmt.Sprintf("%s%s?exp=%d&sig=%s", DownloadPrefix, m.ID, e, s.sig(m, e)), exp
}

// CheckSigned verifies a signed path's exp and sig for m.
func (s *Store) CheckSigned(m Meta, expText, sig string) error {
	exp, err := strconv.ParseInt(strings.TrimSpace(expText), 10, 64)
	if err != nil {
		return ErrDenied
	}
	want := s.sig(m, exp)
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.TrimSpace(sig))) != 1 {
		return ErrDenied
	}
	if s.now().Unix() > exp {
		return ErrExpired
	}
	return nil
}

func (s *Store) list() []Meta {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		if m, err := s.readMeta(e.Name()); err == nil {
			out = append(out, m)
		} else {
			// A folder without readable meta is debris.
			_ = os.RemoveAll(s.dirOf(e.Name()))
		}
	}
	return out
}

func (s *Store) usedLocked() int64 {
	var n int64
	for _, m := range s.list() {
		n += m.Size
	}
	return n
}

// Sweep removes expired files and stale unfinished uploads.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweepLocked()
}

func (s *Store) sweepLocked() int {
	now := s.now()
	removed := 0
	for _, m := range s.list() {
		if s.writing[m.ID] {
			continue
		}
		if now.After(m.Expires) || (!m.Complete && now.Sub(m.Updated) > StaleUploadAfter) {
			_ = os.RemoveAll(s.dirOf(m.ID))
			removed++
		}
	}
	return removed
}

// evictLocked frees at least need bytes, finished files oldest first.
func (s *Store) evictLocked(need int64) {
	all := s.list()
	sort.Slice(all, func(i, j int) bool { return all[i].Created.Before(all[j].Created) })
	for _, m := range all {
		if need <= 0 {
			return
		}
		if !m.Complete || s.writing[m.ID] {
			continue
		}
		_ = os.RemoveAll(s.dirOf(m.ID))
		need -= m.Size
	}
}

// StartSweeper sweeps every interval until stop is closed.
func (s *Store) StartSweeper(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.Sweep()
			}
		}
	}()
}
