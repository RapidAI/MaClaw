package botregistry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Artifact is one file or text passed between bots through the main bot.
// Remote bot outputs land here; local files are referenced in place.
type Artifact struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	MIME      string    `json:"mime,omitempty"`
	From      string    `json:"from"`
	RunID     string    `json:"run_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Artifacts is a small on-disk store: <dir>/<id>/<name> plus index.json.
type Artifacts struct {
	mu  sync.Mutex
	dir string
}

// NewArtifacts opens (creating) the store under dir.
func NewArtifacts(dir string) (*Artifacts, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Artifacts{dir: dir}, nil
}

// Dir is the store root.
func (s *Artifacts) Dir() string { return s.dir }

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._ -]+`)

// SafeName strips path parts and odd characters from a file name.
func SafeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = unsafeName.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "artifact"
	}
	if len(name) > 120 {
		ext := filepath.Ext(name)
		name = name[:120-len(ext)] + ext
	}
	return name
}

// PutBytes stores data as a new artifact.
func (s *Artifacts) PutBytes(name, mime, from, runID string, data []byte) (Artifact, error) {
	if int64(len(data)) > TransferMax {
		return Artifact{}, fmt.Errorf("artifact is larger than %d MB", TransferMax>>20)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	name = SafeName(name)
	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Artifact{}, err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Artifact{}, err
	}
	sum := sha256.Sum256(data)
	a := Artifact{ID: id, Name: name, Path: path, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), MIME: mime, From: from, RunID: runID, CreatedAt: time.Now()}
	return a, s.appendLocked(a)
}

// PutFile records a local file in place (no copy) as an artifact.
func (s *Artifacts) PutFile(path, from string) (Artifact, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	if st.IsDir() {
		return Artifact{}, fmt.Errorf("%s is a directory", path)
	}
	if err := CheckOutboundPath(path, st.Size()); err != nil {
		return Artifact{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Artifact{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Artifact{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := Artifact{ID: newID(), Name: SafeName(filepath.Base(path)), Path: path, Size: st.Size(), SHA256: hex.EncodeToString(h.Sum(nil)), From: from, CreatedAt: time.Now()}
	return a, s.appendLocked(a)
}

// Get finds an artifact by id.
func (s *Artifacts) Get(id string) (Artifact, bool) {
	for _, a := range s.List() {
		if a.ID == strings.TrimSpace(id) {
			return a, true
		}
	}
	return Artifact{}, false
}

// List returns all artifacts, newest first.
func (s *Artifacts) List() []Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.readLocked()
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}

func (s *Artifacts) indexPath() string { return filepath.Join(s.dir, "index.json") }

func (s *Artifacts) readLocked() []Artifact {
	raw, err := os.ReadFile(s.indexPath())
	if err != nil {
		return []Artifact{}
	}
	var items []Artifact
	if json.Unmarshal(raw, &items) != nil {
		return []Artifact{}
	}
	return items
}

const artifactIndexMax = 500

func (s *Artifacts) appendLocked(a Artifact) error {
	items := append(s.readLocked(), a)
	if len(items) > artifactIndexMax {
		items = items[len(items)-artifactIndexMax:]
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath())
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "art_" + hex.EncodeToString(b[:])
}
