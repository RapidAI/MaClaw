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

// JournalImageState describes the strongest durable evidence collected for a
// single immutable image range. These values intentionally say nothing about
// a local archive path or a device identifier, both of which must not be
// persisted in a browser-visible recovery record.
type JournalImageState string

const (
	JournalPrepared         JournalState = "prepared"
	JournalWriting          JournalState = "writing"
	JournalRecoveryRequired JournalState = "recovery_required"
	JournalVerified         JournalState = "verified"

	JournalImagePlanned          JournalImageState = "planned"
	JournalImageWritten          JournalImageState = "written"
	JournalImageReadbackVerified JournalImageState = "readback_verified"
)

// JournalImage is the path-free, immutable portion of a signed flash plan.
// Name is an opaque plan position (not the archive entry or local file name),
// so recovery diagnostics can be correlated without persisting local paths.
type JournalImage struct {
	Name   string            `json:"name"`
	Region string            `json:"region"`
	Offset uint64            `json:"offset"`
	Size   int64             `json:"size"`
	SHA256 string            `json:"sha256"`
	State  JournalImageState `json:"state"`
}

type Journal struct {
	Schema        int    `json:"schema"`
	JobID         string `json:"jobId"`
	PackageID     string `json:"packageId"`
	PackageSHA256 string `json:"packageSha256"`
	// PlanSHA256 hashes Images with their mutable State field excluded. It
	// makes an interrupted job's recovery evidence unambiguously refer to the
	// exact signed byte ranges that were approved before writing started.
	PlanSHA256           string         `json:"planSha256,omitempty"`
	Images               []JournalImage `json:"images,omitempty"`
	State                JournalState   `json:"state"`
	BootCriticalModified bool           `json:"bootCriticalModified"`
	// FlashVerified is written only after every planned image has passed ROM
	// readback verification. It is deliberately separate from State: a power
	// loss before this point still requires complete ROM recovery, while a
	// later application boot timeout can be retried without rewriting Flash.
	FlashVerified bool      `json:"flashVerified,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Checksum      string    `json:"checksum"`
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
	FlashVerified        bool         `json:"flashVerified"`
	UpdatedAt            time.Time    `json:"updatedAt" ts_type:"string"`
}

func journalPath(root, jobID string) string { return filepath.Join(root, jobID, "journal.json") }
func WriteJournal(root string, j Journal) error {
	if strings.Contains(j.JobID, "/") || strings.Contains(j.JobID, "\\") || j.JobID == "." || j.JobID == ".." {
		return errors.New("invalid journal id")
	}
	if j.Schema == 0 {
		j.Schema = JournalSchema
	}
	if j.Schema != JournalSchema || j.JobID == "" || j.PackageID == "" || !validJournalState(j.State) || !validJournalEvidence(j) {
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
	if j.Schema != JournalSchema || j.JobID != jobID || j.Checksum == "" || !validJournalState(j.State) || !validJournalEvidence(j) {
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

func validJournalState(state JournalState) bool {
	switch state {
	case JournalPrepared, JournalWriting, JournalRecoveryRequired, JournalVerified:
		return true
	default:
		return false
	}
}

func validJournalImageState(state JournalImageState) bool {
	switch state {
	case JournalImagePlanned, JournalImageWritten, JournalImageReadbackVerified:
		return true
	default:
		return false
	}
}

// JournalPlanSHA256 returns a deterministic digest of the immutable image
// facts. State is deliberately excluded so advancing write evidence cannot
// silently change the plan it proves.
func JournalPlanSHA256(images []JournalImage) (string, error) {
	immutable := make([]struct {
		Name   string `json:"name"`
		Region string `json:"region"`
		Offset uint64 `json:"offset"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}, len(images))
	for i, image := range images {
		immutable[i] = struct {
			Name   string `json:"name"`
			Region string `json:"region"`
			Offset uint64 `json:"offset"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		}{Name: image.Name, Region: image.Region, Offset: image.Offset, Size: image.Size, SHA256: image.SHA256}
	}
	raw, err := json.Marshal(immutable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// HasCompleteReadbackEvidence is the invariant required before a recovery
// item can offer a non-writing BOOT_STATUS retry. A boolean alone is not
// trusted because older/corrupt journals have no per-image proof.
func HasCompleteReadbackEvidence(j Journal) bool {
	if !j.FlashVerified || len(j.Images) == 0 || j.PlanSHA256 == "" {
		return false
	}
	for _, image := range j.Images {
		if image.State != JournalImageReadbackVerified {
			return false
		}
	}
	planSHA256, err := JournalPlanSHA256(j.Images)
	return err == nil && planSHA256 == j.PlanSHA256
}

func validJournalEvidence(j Journal) bool {
	if len(j.Images) == 0 {
		// Version-1 journals written before immutable image evidence existed
		// remain recoverable, but never qualify for a non-writing retry.
		return !j.FlashVerified && j.PlanSHA256 == ""
	}
	if j.PlanSHA256 == "" {
		return false
	}
	seen := make(map[string]struct{}, len(j.Images))
	for _, image := range j.Images {
		if image.Name == "" || image.Region == "" || image.Size <= 0 || image.SHA256 == "" || !validJournalImageState(image.State) {
			return false
		}
		if _, exists := seen[image.Name]; exists {
			return false
		}
		seen[image.Name] = struct{}{}
	}
	planSHA256, err := JournalPlanSHA256(j.Images)
	if err != nil || planSHA256 != j.PlanSHA256 {
		return false
	}
	return !j.FlashVerified || HasCompleteReadbackEvidence(j)
}

// ListRecoveryRequired scans only direct job directories, validates every
// journal checksum, and returns any write that was not verified. Invalid or
// partially written journal files are reported as recovery-required too; this
// is fail-closed because their former write state cannot be established.
func ListRecoveryRequired(root string) ([]RecoveryItem, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		// Windows may report ERROR_PATH_NOT_FOUND for ReadDir(file), which
		// os.IsNotExist classifies the same way as an absent directory. Verify
		// the root before treating the recovery store as empty; otherwise a bad
		// log-root configuration could silently bypass the recovery interlock.
		if info, statErr := os.Stat(root); statErr == nil && !info.IsDir() {
			return nil, fmt.Errorf("recovery log root is not a directory")
		}
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
			// Only application-created job directories can contribute a recovery
			// lock.  A stray or manually-created directory must not become a
			// permanent denial of service, while a corrupt journal for a genuine
			// write is still fail-closed.
			if isWriteJobDirectory(root, entry.Name()) {
				items = append(items, RecoveryItem{JobID: entry.Name(), State: JournalRecoveryRequired})
			}
			continue
		}
		// Prepared journals precede every irreversible action. A crash during
		// package validation must not turn into a false recovery lock; only a
		// journal that reached the writing phase (or was explicitly marked for
		// recovery) blocks a new installation.
		if journal.State == JournalWriting || journal.State == JournalRecoveryRequired {
			items = append(items, RecoveryItem{JobID: journal.JobID, PackageID: journal.PackageID, State: JournalRecoveryRequired, BootCriticalModified: journal.BootCriticalModified, FlashVerified: journal.FlashVerified, UpdatedAt: journal.UpdatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func isWriteJobDirectory(root, jobID string) bool {
	if !strings.HasPrefix(jobID, "job-") || strings.ContainsAny(jobID, `\\/:`) {
		return false
	}
	for _, name := range []string{"events.jsonl", "summary.json", "snapshot.json"} {
		info, err := os.Stat(filepath.Join(root, jobID, name))
		if err == nil && !info.IsDir() && info.Size() > 0 {
			return true
		}
	}
	return false
}

// MarkRecoveryResolved closes the recovery lock only after a separate full
// ROM recovery has reached verified boot. The original evidence journal is
// retained and is atomically moved to a terminal state rather than deleted.
func MarkRecoveryResolved(root, jobID string) error {
	journal, err := ReadJournal(root, jobID)
	if err != nil {
		return err
	}
	if journal.State != JournalWriting && journal.State != JournalRecoveryRequired {
		return fmt.Errorf("job %s is not awaiting recovery", jobID)
	}
	journal.State = JournalVerified
	return WriteJournal(root, journal)
}
