package guiapp

import (
	"testing"
)

func TestCloudSrvRecordRoundTripAndLink(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.saveCloudSrvRecord(CloudSrvNodeRecord{
		Name: "srv", Host: "10.0.0.8", Port: 22, User: "root", HubURL: "https://hub.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	// Same machine saves update in place and keep the original CreatedAt.
	first, err := app.findCloudSrvRecord("10.0.0.8")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.saveCloudSrvRecord(CloudSrvNodeRecord{
		Name: "srv", Host: "10.0.0.8", Port: 22, User: "root", HubURL: "https://hub.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	records, err := app.loadCloudSrvRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d, want 1 (same machine must not duplicate)", len(records))
	}
	if records[0].CreatedAt != first.CreatedAt {
		t.Fatalf("CreatedAt changed on re-save")
	}

	// The enrolled node id links into the same record.
	app.noteCloudSrvNodeID(CloudSrvNodeRecord{Host: "10.0.0.8", Port: 22, User: "root"}, `{"node_id":"edge-9","hub_token":"x"}`)
	linked, err := app.findCloudSrvRecord("edge-9")
	if err != nil {
		t.Fatalf("lookup by node id failed: %v", err)
	}
	if linked.Host != "10.0.0.8" {
		t.Fatalf("linked record host=%q", linked.Host)
	}

	// Delete by node id removes only the target record.
	if err := app.deleteCloudSrvRecord("edge-9"); err != nil {
		t.Fatal(err)
	}
	records, err = app.loadCloudSrvRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records=%d after delete, want 0", len(records))
	}
}

func TestCloudSrvMachineLockSerializesJobs(t *testing.T) {
	key := cloudSrvMachineKey("10.0.0.8", 22, "root")
	if key != "10.0.0.8:22:root" {
		t.Fatalf("machine key=%q", key)
	}
	if !claimCloudSrvJob(key) {
		t.Fatal("first claim rejected")
	}
	if claimCloudSrvJob(key) {
		t.Fatal("second claim on the same machine was accepted")
	}
	// A different machine is not blocked by the first one.
	other := cloudSrvMachineKey("10.0.0.9", 22, "root")
	if !claimCloudSrvJob(other) {
		t.Fatal("a different machine was blocked by the first lock")
	}
	releaseCloudSrvJob(other)
	releaseCloudSrvJob(key)
	if !claimCloudSrvJob(key) {
		t.Fatal("claim after release was rejected")
	}
	releaseCloudSrvJob(key)
}

func TestFinishCloudSrvOnlyLinksNodeID(t *testing.T) {
	// envHasNonEmpty backs the hub-token check: an empty value must not pass.
	if envHasNonEmpty("MACLAW_DESKTOP_API_TOKEN=\nOTHER=1", "MACLAW_DESKTOP_API_TOKEN") {
		t.Fatal("empty token passed the check")
	}
	if !envHasNonEmpty("OTHER=1\nMACLAW_DESKTOP_API_TOKEN=tok", "MACLAW_DESKTOP_API_TOKEN") {
		t.Fatal("written token was not found")
	}
}
