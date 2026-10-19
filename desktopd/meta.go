package desktopd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	// DefaultComputerDir is the host directory from section 7. computers/<tenant>/<userkey>/
	// is created at 0700 under it. DESKTOPD_COMPUTER_DIR overrides this.
	DefaultComputerDir       = "/data/soft/maclaw_desktopd"
	computerMetaStateRunning = "running"
	computerMetaStateAbsent  = "absent"
)

// ComputerMeta is the control-plane record for one user's machine.
// The gate token stays in the container registry. This file does not carry it.
// State is only running or absent: idle, sleeping, and waking are later stages.
type ComputerMeta struct {
	State        string      `json:"state"`
	Runtime      string      `json:"runtime"`
	SnapshotID   string      `json:"snapshot_id"`
	Generation   int         `json:"generation"`
	Host         string      `json:"host,omitempty"`
	LastActivity string      `json:"last_activity,omitempty"`
	Screens      []ScreenRow `json:"screens"`
}

// ScreenRow is one screen in the table. Screen 1 is display :20.
// Screens 2 and up are :21–:50, one per agent id. Agent is empty for the person.
type ScreenRow struct {
	Screen  int    `json:"screen"`
	Display string `json:"display"`
	Agent   string `json:"agent,omitempty"`
}

// ComputerDirFromEnv returns the host directory for meta.json.
// An empty value uses DefaultComputerDir. Tests leave Service.ComputerDir empty
// so they do not write this file.
func ComputerDirFromEnv(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultComputerDir
	}
	return raw
}

func (s *Service) computerDir() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.ComputerDir)
}

func singlePathSegment(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

func (s *Service) computerMetaPath(tenantID, userID string) (string, error) {
	root := s.computerDir()
	if root == "" {
		return "", errors.New("computer directory is unset")
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	key, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return "", err
	}
	if !singlePathSegment(spec.TenantID) || !desktop.ValidUserKey(key) {
		return "", fmt.Errorf("%w: computer path", ErrInvalid)
	}
	return filepath.Join(root, "computers", spec.TenantID, key, "meta.json"), nil
}

// ReadComputerMeta reads the host record. It does not call docker.
func (s *Service) ReadComputerMeta(tenantID, userID string) (ComputerMeta, error) {
	path, err := s.computerMetaPath(tenantID, userID)
	if err != nil {
		return ComputerMeta{}, err
	}
	if s != nil {
		s.metaMu.Lock()
		defer s.metaMu.Unlock()
	}
	return loadComputerMeta(path)
}

func loadComputerMeta(path string) (ComputerMeta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ComputerMeta{}, err
	}
	var meta ComputerMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ComputerMeta{}, err
	}
	if meta.Screens == nil {
		meta.Screens = []ScreenRow{}
	}
	return meta, nil
}

func (s *Service) updateComputerMeta(tenantID, userID string, fn func(*ComputerMeta)) {
	if s.computerDir() == "" {
		return
	}
	path, err := s.computerMetaPath(tenantID, userID)
	if err != nil {
		log.Printf("desktopd: computer meta path: %v", err)
		return
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	meta, err := loadComputerMeta(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("desktopd: computer meta read: %v", err)
			return
		}
		meta = ComputerMeta{Runtime: "docker", Screens: []ScreenRow{}}
	}
	if meta.Runtime == "" {
		meta.Runtime = "docker"
	}
	if meta.Screens == nil {
		meta.Screens = []ScreenRow{}
	}
	fn(&meta)
	if meta.State != computerMetaStateRunning && meta.State != computerMetaStateAbsent {
		return
	}
	meta.Runtime = "docker"
	meta.Screens = cleanScreens(meta.Screens)
	if err := writeComputerMeta(path, meta); err != nil {
		log.Printf("desktopd: computer meta write: %v", err)
	}
}

func cleanScreens(rows []ScreenRow) []ScreenRow {
	out := make([]ScreenRow, 0, len(rows))
	seen := map[int]bool{}
	for _, row := range rows {
		if row.Screen < 1 || seen[row.Screen] || !desktop.ValidDisplay(row.Display) {
			continue
		}
		seen[row.Screen] = true
		row.Agent = strings.TrimSpace(row.Agent)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Screen < out[j].Screen })
	return out
}

func writeComputerMeta(path string, meta ComputerMeta) error {
	if meta.Screens == nil {
		meta.Screens = []ScreenRow{}
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 0700 is the directory the doc asks for. The parent computers/ and tenant/
	// directories are the same. The file itself is 0600. A failure to chmod on
	// a platform that has no unix mode does not skip the write.
	for _, item := range []string{filepath.Dir(filepath.Dir(dir)), filepath.Dir(dir), dir} {
		_ = os.Chmod(item, 0o700)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func (s *Service) markComputerRunning(spec Spec) {
	s.updateComputerMeta(spec.TenantID, spec.UserID, func(meta *ComputerMeta) {
		meta.State = computerMetaStateRunning
		if host := s.advertiseHostOrEmpty(); host != "" {
			meta.Host = host
		}
	})
}

func (s *Service) markComputerAbsent(spec Spec) {
	s.updateComputerMeta(spec.TenantID, spec.UserID, func(meta *ComputerMeta) {
		meta.State = computerMetaStateAbsent
	})
}

// noteComputerActivity records that this user used the machine.
// File, bash, and http do not enter the container supervisor, so they never
// refreshed its last_used. The reaper reads this stamp with the same idle
// limit it already had. The limit is not shortened, and writing the stamp
// does not start the reaper.
func (s *Service) noteComputerActivity(tenantID, userID string) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return
	}
	stamp := s.now().UTC().Format(time.RFC3339Nano)
	s.updateComputerMeta(spec.TenantID, spec.UserID, func(meta *ComputerMeta) {
		if meta.State == "" {
			meta.State = computerMetaStateRunning
		}
		meta.LastActivity = stamp
		if host := s.advertiseHostOrEmpty(); host != "" && meta.Host == "" {
			meta.Host = host
		}
	})
}

// noteComputerScreen records a display the supervisor already returned.
// Screen 1 is the person's :20. A bot display is :21–:50 and keeps the agent id.
// The same agent replaces its own row. The token is not a field on the row.
func (s *Service) noteComputerScreen(spec Spec, display string) {
	index, ok := screenIndex(display)
	if !ok {
		return
	}
	row := ScreenRow{Screen: index, Display: strings.TrimSpace(display)}
	if spec.BotID != "" {
		if index == 1 {
			return
		}
		row.Agent = spec.BotID
	} else if index != 1 {
		return
	}
	s.updateComputerMeta(spec.TenantID, spec.UserID, func(meta *ComputerMeta) {
		if meta.State == "" {
			meta.State = computerMetaStateRunning
		}
		meta.Screens = upsertScreen(meta.Screens, row)
	})
}

func screenIndex(display string) (int, bool) {
	display = strings.TrimSpace(display)
	if !desktop.ValidDisplay(display) {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(display, ":"))
	if err != nil || number < 20 || number > 50 {
		return 0, false
	}
	return number - 19, true
}

func upsertScreen(rows []ScreenRow, row ScreenRow) []ScreenRow {
	for i, item := range rows {
		if item.Screen == row.Screen || (row.Agent != "" && item.Agent == row.Agent) {
			rows[i] = row
			return rows
		}
	}
	return append(rows, row)
}

// hostIdle is how long since this process saw file, bash, http, a screenshot,
// a key or click, ctl, or a CDP/noVNC session. ok is false when there is no stamp.
func (s *Service) hostIdle(tenantID, userID string) (time.Duration, bool) {
	meta, err := s.ReadComputerMeta(tenantID, userID)
	if err != nil || meta.LastActivity == "" {
		return 0, false
	}
	at, err := time.Parse(time.RFC3339Nano, meta.LastActivity)
	if err != nil {
		return 0, false
	}
	age := s.now().Sub(at)
	if age < 0 {
		age = 0
	}
	return age, true
}
