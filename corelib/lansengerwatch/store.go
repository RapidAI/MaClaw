package lansengerwatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store persists watch jobs, rosters, and transcript files under baseDir/lansenger_watch.
type Store struct {
	mu      sync.Mutex
	baseDir string
}

// NewStore creates a store rooted at maclawBase/lansenger_watch.
func NewStore(maclawBase string) *Store {
	return &Store{baseDir: filepath.Join(strings.TrimSpace(maclawBase), "lansenger_watch")}
}

// Root returns the store directory.
func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

func (s *Store) configPath() string {
	return filepath.Join(s.baseDir, ConfigFileName)
}

func (s *Store) rosterPath(groupID string) string {
	safe := sanitizeFilePart(groupID)
	return filepath.Join(s.baseDir, RosterDirName, safe+".json")
}

func (s *Store) jobLogDir(jobID string) string {
	return filepath.Join(s.baseDir, LogsDirName, sanitizeFilePart(jobID))
}

// EnsureReady creates the store directory tree.
func (s *Store) EnsureReady() error {
	if s == nil {
		return fmt.Errorf("lansengerwatch: nil store")
	}
	for _, d := range []string{
		s.baseDir,
		filepath.Join(s.baseDir, RosterDirName),
		filepath.Join(s.baseDir, LogsDirName),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// LoadConfig reads jobs; missing file yields empty config.
func (s *Store) LoadConfig() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadConfigLocked()
}

func (s *Store) loadConfigLocked() (Config, error) {
	if err := s.EnsureReady(); err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Version: 1, Jobs: []Job{}}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("lansengerwatch: parse config: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Jobs == nil {
		cfg.Jobs = []Job{}
	}
	return cfg, nil
}

// SaveConfig writes the full config document.
func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveConfigLocked(cfg)
}

func (s *Store) saveConfigLocked(cfg Config) error {
	if err := s.EnsureReady(); err != nil {
		return err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Jobs == nil {
		cfg.Jobs = []Job{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.configPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.configPath())
}

// ListJobs returns a copy of jobs.
func (s *Store) ListJobs() ([]Job, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(cfg.Jobs))
	copy(out, cfg.Jobs)
	return out, nil
}

// GetJob returns a job by id.
func (s *Store) GetJob(id string) (Job, bool, error) {
	id = strings.TrimSpace(id)
	cfg, err := s.LoadConfig()
	if err != nil {
		return Job{}, false, err
	}
	for _, j := range cfg.Jobs {
		if j.ID == id {
			return j, true, nil
		}
	}
	return Job{}, false, nil
}

// UpsertJob creates or updates a job (assigns id/timestamps when needed).
func (s *Store) UpsertJob(job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.loadConfigLocked()
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	job.GroupID = strings.TrimSpace(job.GroupID)
	job.Name = strings.TrimSpace(job.Name)
	if job.Name == "" {
		job.Name = "盯人任务"
	}
	if job.GroupID == "" {
		return Job{}, fmt.Errorf("group_id is required")
	}
	job.TargetStaffIDs = normalizeIDList(job.TargetStaffIDs)
	job.ForwardChannels = NormalizeForwardChannels(job.ForwardChannels)
	// Drop deprecated people-based forward list from persisted config.
	job.ForwardStaffIDs = nil
	job.KeywordScope = NormalizeKeywordScope(job.KeywordScope)
	if job.TargetNames == nil {
		job.TargetNames = map[string]string{}
	}
	for i := range job.Keywords {
		job.Keywords[i].ID = strings.TrimSpace(job.Keywords[i].ID)
		if job.Keywords[i].ID == "" {
			job.Keywords[i].ID = uuid.NewString()
		}
		job.Keywords[i].Keywords = normalizeKeywordList(job.Keywords[i].Keywords)
		if job.Keywords[i].CLITimeoutSec < 0 {
			job.Keywords[i].CLITimeoutSec = 0
		}
		if job.Keywords[i].CLITimeoutSec > MaxCLITimeoutSec {
			job.Keywords[i].CLITimeoutSec = MaxCLITimeoutSec
		}
	}

	if strings.TrimSpace(job.ID) == "" {
		job.ID = uuid.NewString()
		job.CreatedAt = now
		job.UpdatedAt = now
		cfg.Jobs = append(cfg.Jobs, job)
	} else {
		found := false
		for i, existing := range cfg.Jobs {
			if existing.ID == job.ID {
				if existing.CreatedAt.IsZero() {
					job.CreatedAt = now
				} else {
					job.CreatedAt = existing.CreatedAt
				}
				job.UpdatedAt = now
				cfg.Jobs[i] = job
				found = true
				break
			}
		}
		if !found {
			job.CreatedAt = now
			job.UpdatedAt = now
			cfg.Jobs = append(cfg.Jobs, job)
		}
	}
	if err := s.saveConfigLocked(cfg); err != nil {
		return Job{}, err
	}
	return job, nil
}

// DeleteJob removes a job by id (logs retained).
func (s *Store) DeleteJob(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.loadConfigLocked()
	if err != nil {
		return err
	}
	out := cfg.Jobs[:0]
	for _, j := range cfg.Jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	cfg.Jobs = out
	return s.saveConfigLocked(cfg)
}

// LoadRoster returns members learned/added for a group.
func (s *Store) LoadRoster(groupID string) (GroupRoster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRosterLocked(groupID)
}

func (s *Store) loadRosterLocked(groupID string) (GroupRoster, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return GroupRoster{}, fmt.Errorf("group_id required")
	}
	if err := s.EnsureReady(); err != nil {
		return GroupRoster{}, err
	}
	data, err := os.ReadFile(s.rosterPath(groupID))
	if err != nil {
		if os.IsNotExist(err) {
			return GroupRoster{GroupID: groupID, Members: []Member{}}, nil
		}
		return GroupRoster{}, err
	}
	var r GroupRoster
	if err := json.Unmarshal(data, &r); err != nil {
		return GroupRoster{}, err
	}
	r.GroupID = groupID
	if r.Members == nil {
		r.Members = []Member{}
	}
	return r, nil
}

// rosterTouchMinInterval avoids rewriting roster JSON on every chat line.
const rosterTouchMinInterval = 5 * time.Minute

// NoteMember upserts a roster entry from a live message or manual add.
// Skips disk write when the member already exists with the same name and was
// seen recently (manual adds always flush).
func (s *Store) NoteMember(groupID, groupName, staffID, name, source string) error {
	groupID = strings.TrimSpace(groupID)
	staffID = NormalizeStaffID(staffID)
	if groupID == "" || staffID == "" {
		return nil
	}
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)
	manual := source == "manual"

	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.loadRosterLocked(groupID)
	if err != nil {
		return err
	}
	metaChanged := false
	if gn := strings.TrimSpace(groupName); gn != "" && gn != r.GroupName {
		r.GroupName = gn
		metaChanged = true
	}
	now := time.Now().UTC()
	for i := range r.Members {
		if NormalizeStaffID(r.Members[i].StaffID) != staffID {
			continue
		}
		changed := metaChanged
		if name != "" && name != r.Members[i].Name {
			r.Members[i].Name = name
			changed = true
		}
		if source != "" && source != r.Members[i].Source && (manual || r.Members[i].Source == "") {
			r.Members[i].Source = source
			changed = true
		}
		// Only persist if name/meta changed, manual add, or last-seen is stale.
		stale := r.Members[i].LastSeenAt.IsZero() || now.Sub(r.Members[i].LastSeenAt) >= rosterTouchMinInterval
		if !changed && !stale && !manual {
			return nil
		}
		r.Members[i].LastSeenAt = now
		r.UpdatedAt = now
		return s.saveRosterLocked(r)
	}
	r.Members = append(r.Members, Member{
		StaffID:    staffID,
		Name:       name,
		LastSeenAt: now,
		Source:     source,
	})
	r.UpdatedAt = now
	return s.saveRosterLocked(r)
}

func (s *Store) saveRosterLocked(r GroupRoster) error {
	if err := s.EnsureReady(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := s.rosterPath(r.GroupID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AppendTranscript appends a line to the job's daily log file. kind is "all" or "keyword".
func (s *Store) AppendTranscript(jobID, kind, line string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "all"
	}
	if jobID == "" {
		return "", fmt.Errorf("job id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.jobLogDir(jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	day := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, day+"_"+sanitizeFilePart(kind)+".txt")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return "", err
	}
	return path, nil
}

// ListTranscriptFiles returns log file paths for a job (newest first by basename).
func (s *Store) ListTranscriptFiles(jobID string) ([]string, error) {
	dir := s.jobLogDir(strings.TrimSpace(jobID))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".txt") {
			names = append(names, name)
		}
	}
	// YYYY-MM-DD_*.txt sorts reverse-lexicographically as newest-first.
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(dir, name))
	}
	return out, nil
}

// maxTranscriptReadBytes caps UI/API reads of a single log file (~2 MiB).
const maxTranscriptReadBytes = 2 << 20

// ReadTranscriptFile reads a log file (must be under store root).
func (s *Store) ReadTranscriptFile(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	root := filepath.Clean(s.baseDir)
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside watch store")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Read at most max+1 to detect truncation without loading unbounded files.
	data, err := io.ReadAll(io.LimitReader(f, maxTranscriptReadBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTranscriptReadBytes {
		return string(data[:maxTranscriptReadBytes]) + "\n…(truncated, file exceeds 2MB)…\n", nil
	}
	return string(data), nil
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func normalizeIDList(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = NormalizeStaffID(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizeKeywordList(kws []string) []string {
	out := make([]string, 0, len(kws))
	for _, k := range kws {
		k = strings.TrimSpace(k)
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}
