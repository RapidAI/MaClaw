package jobs

import "testing"

func TestJournalRoundTripAndTamperDetection(t *testing.T) {
	root := t.TempDir()
	source := Journal{JobID: "job-a", PackageID: "package", PackageSHA256: "sha256:x", State: JournalPrepared}
	if err := WriteJournal(root, source); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJournal(root, "job-a")
	if err != nil || got.State != JournalPrepared {
		t.Fatalf("journal=%+v err=%v", got, err)
	}
	if err := WriteJournal(root, Journal{JobID: "../x", PackageID: "x"}); err == nil {
		t.Fatal("unsafe ID accepted")
	}
}

func TestListRecoveryRequiredExcludesOnlyVerifiedWrites(t *testing.T) {
	root := t.TempDir()
	for _, source := range []Journal{
		{JobID: "verified", PackageID: "ok", State: JournalVerified},
		{JobID: "prepared", PackageID: "pending", State: JournalPrepared},
		{JobID: "writing", PackageID: "interrupted", State: JournalWriting},
		{JobID: "failed", PackageID: "recovery", State: JournalRecoveryRequired, BootCriticalModified: true},
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
	if _, ok := seen["verified"]; ok || seen["prepared"].JobID != "" || seen["writing"].State != JournalRecoveryRequired || !seen["failed"].BootCriticalModified {
		t.Fatalf("unexpected recovery items: %+v", items)
	}
}
