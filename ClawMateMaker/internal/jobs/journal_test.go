package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalRoundTripAndTamperDetection(t *testing.T) {
	root := t.TempDir()
	images := testJournalImages(t)
	source := Journal{JobID: "job-a", PackageID: "package", PackageSHA256: "sha256:x", PlanSHA256: testJournalPlanHash(t, images), Images: images, State: JournalPrepared}
	if err := WriteJournal(root, source); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJournal(root, "job-a")
	if err != nil || got.State != JournalPrepared || len(got.Images) != 2 || got.PlanSHA256 != source.PlanSHA256 {
		t.Fatalf("journal=%+v err=%v", got, err)
	}
	if err := WriteJournal(root, Journal{JobID: "../x", PackageID: "x"}); err == nil {
		t.Fatal("unsafe ID accepted")
	}
	if err := WriteJournal(root, Journal{JobID: "unknown", PackageID: "x", State: "invented"}); err == nil {
		t.Fatal("unknown journal state accepted")
	}
}

func TestReadJournalRejectsUnknownButChecksummedState(t *testing.T) {
	root := t.TempDir()
	known := Journal{Schema: JournalSchema, JobID: "job-unknown", PackageID: "package", State: "invented", UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	known.Checksum = "sha256:" + hex.EncodeToString(sum[:])
	final, err := json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, known.JobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), final, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJournal(root, known.JobID); err == nil {
		t.Fatal("unknown but checksummed state accepted")
	}
}

func TestListRecoveryRequiredExcludesOnlyVerifiedWrites(t *testing.T) {
	root := t.TempDir()
	verifiedImages := testJournalImages(t)
	for index := range verifiedImages {
		verifiedImages[index].State = JournalImageReadbackVerified
	}
	for _, source := range []Journal{
		{JobID: "verified", PackageID: "ok", State: JournalVerified},
		{JobID: "prepared", PackageID: "pending", State: JournalPrepared},
		{JobID: "writing", PackageID: "interrupted", State: JournalWriting},
		{JobID: "failed", PackageID: "recovery", State: JournalRecoveryRequired, BootCriticalModified: true, PlanSHA256: testJournalPlanHash(t, verifiedImages), Images: verifiedImages, FlashVerified: true},
	} {
		if err := WriteJournal(root, source); err != nil {
			t.Fatal(err)
		}
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	seen := map[string]RecoveryItem{}
	for _, item := range items {
		seen[item.JobID] = item
	}
	if _, ok := seen["verified"]; ok || seen["prepared"].JobID != "" || seen["writing"].State != JournalRecoveryRequired || !seen["failed"].BootCriticalModified || !seen["failed"].FlashVerified {
		t.Fatalf("unexpected recovery items: %+v", items)
	}
}

func TestJournalEvidenceRejectsIncompleteOrModifiedPlan(t *testing.T) {
	root := t.TempDir()
	images := testJournalImages(t)
	images[0].State = JournalImageReadbackVerified
	images[1].State = JournalImageReadbackVerified
	journal := Journal{JobID: "complete", PackageID: "package", State: JournalRecoveryRequired, PlanSHA256: testJournalPlanHash(t, images), Images: images, FlashVerified: true}
	if err := WriteJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJournal(root, journal.JobID)
	if err != nil || !HasCompleteReadbackEvidence(got) {
		t.Fatalf("complete evidence was not accepted: journal=%+v err=%v", got, err)
	}
	journal.JobID = "incomplete"
	journal.Images[1].State = JournalImageWritten
	if err := WriteJournal(root, journal); err == nil {
		t.Fatal("flashVerified journal with incomplete evidence accepted")
	}
	journal.JobID = "modified"
	journal.Images[1].State = JournalImageReadbackVerified
	journal.PlanSHA256 = "sha256:modified"
	if err := WriteJournal(root, journal); err == nil {
		t.Fatal("modified plan hash accepted")
	}
}

func TestJournalLegacyFlashVerifiedFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := WriteJournal(root, Journal{JobID: "legacy", PackageID: "package", State: JournalRecoveryRequired, FlashVerified: true}); err == nil {
		t.Fatal("legacy boolean-only readback evidence accepted")
	}
}

func testJournalImages(t *testing.T) []JournalImage {
	t.Helper()
	return []JournalImage{
		{Name: "image-001", Region: "bootloader", Offset: 0, Size: 4096, SHA256: "sha256:aaaaaaaa", State: JournalImagePlanned},
		{Name: "image-002", Region: "factory", Offset: 0x10000, Size: 8192, SHA256: "sha256:bbbbbbbb", State: JournalImagePlanned},
	}
}

func testJournalPlanHash(t *testing.T, images []JournalImage) string {
	t.Helper()
	hash, err := JournalPlanSHA256(images)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestMarkRecoveryResolvedRequiresRecoveryState(t *testing.T) {
	root := t.TempDir()
	if err := WriteJournal(root, Journal{JobID: "interrupted", PackageID: "package", State: JournalRecoveryRequired}); err != nil {
		t.Fatal(err)
	}
	if err := MarkRecoveryResolved(root, "interrupted"); err != nil {
		t.Fatal(err)
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := WriteJournal(root, Journal{JobID: "prepared", PackageID: "package", State: JournalPrepared}); err != nil {
		t.Fatal(err)
	}
	if err := MarkRecoveryResolved(root, "prepared"); err == nil {
		t.Fatal("prepared job was incorrectly resolved")
	}
}

func TestListRecoveryRequiredIgnoresStrayInvalidDirectoryButBlocksWriteEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "unrelated"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated", "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "job-interrupted"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "job-interrupted", "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "job-interrupted", "events.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := ListRecoveryRequired(root)
	if err != nil || len(items) != 1 || items[0].JobID != "job-interrupted" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestListRecoveryRequiredRejectsFileAsRecoveryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jobs-file")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRecoveryRequired(root); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file recovery root error = %v", err)
	}
}
