package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const JournalSchema = 1

type JournalState string

const (
	JournalPrepared         JournalState = "prepared"
	JournalWriting          JournalState = "writing"
	JournalRecoveryRequired JournalState = "recovery_required"
	JournalVerified         JournalState = "verified"
)

type Journal struct {
	Schema               int          `json:"schema"`
	JobID                string       `json:"jobId"`
	PackageID            string       `json:"packageId"`
	PackageSHA256        string       `json:"packageSha256"`
	State                JournalState `json:"state"`
	BootCriticalModified bool         `json:"bootCriticalModified"`
	UpdatedAt            time.Time    `json:"updatedAt"`
	Checksum             string       `json:"checksum"`
}

// RecoveryItem is the safe subset of an unfinished write journal exposed to
// the UI. It deliberately contains no local package path or device identifier:
// after a restart the user must reconnect, probe and select a fresh signed
// complete package before any recovery write can be attempted.
type RecoveryItem struct {
	JobID                string       `json:"jobId"`
	PackageID            string       `json:"packageId"`
	State                JournalState `json:"state"`
	BootCriticalModified bool         `json:"bootCriticalModified"`
	UpdatedAt            time.Time    `json:"updatedAt"`
}

func journalPath(root, jobID string) string { return filepath.Join(root, jobID, "journal.json") }
func WriteJournal(root string, j Journal) error {
	if strings.Contains(j.JobID, "/") || strings.Contains(j.JobID, "\\") || j.JobID == "." || j.JobID == ".." {
		return errors.New("invalid journal id")
	}
	if j.Schema == 0 {
		j.Schema = JournalSchema
	}
	if j.Schema != JournalSchema || j.JobID == "" || j.PackageID == "" {
		return errors.New("invalid journal")
	}
	j.UpdatedAt = time.Now().UTC()
	j.Checksum = ""
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	j.Checksum = "sha256:" + hex.EncodeToString(sum[:])
	final, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(journalPath(root, j.JobID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	temp := journalPath(root, j.JobID) + ".tmp"
	if err := os.WriteFile(temp, append(final, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(temp, journalPath(root, j.JobID))
}
func ReadJournal(root, jobID string) (Journal, error) {
	if strings.Contains(jobID, "/") || strings.Contains(jobID, "\\") || jobID == "" {
		return Journal{}, errors.New("invalid journal id")
	}
	raw, err := os.ReadFile(journalPath(root, jobID))
	if err != nil {
		return Journal{}, err
	}
	var j Journal
	if err := json.Unmarshal(raw, &j); err != nil {
		return Journal{}, err
	}
	if j.Schema != JournalSchema || j.JobID != jobID || j.Checksum == "" {
		return Journal{}, errors.New("invalid journal schema or identity")
	}
	got := j.Checksum
	j.Checksum = ""
	canonical, err := json.Marshal(j)
	if err != nil {
		return Journal{}, err
	}
	sum := sha256.Sum256(canonical)
	if got != "sha256:"+hex.EncodeToString(sum[:]) {
		return Journal{}, fmt.Errorf("journal checksum mismatch")
	}
	return j, nil
}

// ListRecoveryRequired scans only direct job directories, validates every
// journal checksum, and returns any write that was not verified. Invalid or
// partially written journal files are reported as recovery-required too; this
// is fail-closed because their former write state cannot be established.
func ListRecoveryRequired(root string) ([]RecoveryItem, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]RecoveryItem, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := journalPath(root, entry.Name())
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		journal, err := ReadJournal(root, entry.Name())
		if err != nil {
			items = append(items, RecoveryItem{JobID: entry.Name(), State: JournalRecoveryRequired})
			continue
		}
		// Prepared journals precede every irreversible action. A crash during
		// package validation must not turn into a false recovery lock; only a
		// journal that reached the writing phase (or was explicitly marked for
		// recovery) blocks a new installation.
		if journal.State == JournalWriting || journal.State == JournalRecoveryRequired {
			items = append(items, RecoveryItem{JobID: journal.JobID, PackageID: journal.PackageID, State: JournalRecoveryRequired, BootCriticalModified: journal.BootCriticalModified, UpdatedAt: journal.UpdatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}
