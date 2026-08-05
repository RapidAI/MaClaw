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
