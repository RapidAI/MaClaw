package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/jobs"
	"clawmatemaker/internal/logging"
)

func TestAutoDetectionResultDoesNotExposePackagePath(t *testing.T) {
	result := AutoDetectionResult{
		Status: "firmware_ready",
		Firmware: &catalog.DownloadedRelease{
			BoardID: "bread-compact",
			Path:    `C:\\private\\firmware.clawfw`,
		},
	}
	if result.Firmware.Path == "" {
		t.Fatal("test precondition lost the internal path")
	}
	// DownloadedRelease.Path has json:"-"; retain this assertion here because
	// AutoDetectFirmware returns the type through the Wails boundary.
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), `C:\\private\\firmware.clawfw`) {
		t.Fatalf("auto detection result exposed private package path: %s", encoded)
	}
}

func TestDeveloperBuildReportsProbeOnly(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "false"
	t.Cleanup(func() { releaseBuild = previous })
	if !NewApp().GetAppInfo().ProbeOnly {
		t.Fatal("developer build must advertise that official package installation is unavailable")
	}
}

func TestOfficialBuildDoesNotReportProbeOnly(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	if NewApp().GetAppInfo().ProbeOnly {
		t.Fatal("official build must expose the full verified-installation flow")
	}
}

func TestDeveloperBuildRejectsInstallOperationsAtBackendBoundary(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "false"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	if _, err := a.GetLatestFirmware("bread-compact"); err != errOfficialBuildRequired {
		t.Fatalf("developer download error = %v, want official build requirement", err)
	}
	if _, err := a.ImportFirmwarePackage("bread-compact"); err != errOfficialBuildRequired {
		t.Fatalf("developer import error = %v, want official build requirement", err)
	}
	if _, err := a.FlashFirmware("request-0123456789", "COM3", "bread-compact", "fwref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer flash error = %v, want official build requirement", err)
	}
	if _, err := a.RecoverFirmware("request-0123456789", "job-0123456789abcdef", "COM3", "bread-compact", "fwref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer recovery error = %v, want official build requirement", err)
	}
}

func TestAutoDetectionResultDoesNotExposeBatchPackagePaths(t *testing.T) {
	result := AutoDetectionResult{Devices: []AutoDetectedDevice{{
		Device:   device.Candidate{Port: "COM4"},
		Status:   "firmware_ready",
		Firmware: &catalog.DownloadedRelease{BoardID: "bread-compact", Path: `C:\private\bread.clawfw`},
	}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `C:\private\bread.clawfw`) {
		t.Fatalf("batch auto detection result exposed private package path: %s", encoded)
	}
}

func TestRegisterVerifiedPackageHidesHostPath(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("verified firmware archive")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{
		BoardID:       "bread-compact",
		Path:          path,
		InstallStatus: "verified_ready",
		SHA256:        "sha256:" + hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageRef == "" {
		t.Fatal("missing opaque package reference")
	}
	if result.Path != path {
		t.Fatal("internal result must retain its path until Wails serialization")
	}
	stored, lookupErr := a.lookupVerifiedPackage(result.PackageRef)
	if lookupErr != nil || stored.path != path || stored.boardID != result.BoardID {
		t.Fatalf("stored capability = %#v, err=%v", stored, lookupErr)
	}
}

func TestGetJobLogPageRejectsUnsafeJobID(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	if _, err := a.GetJobLogPage("../job-0123456789abcdef", 0, 10); err == nil {
		t.Fatal("unsafe job ID was accepted")
	}
}

func TestGetJobLogPageReadsPersistedEvents(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(logging.Info, "probe", "engine", "STAGE_STARTED", "stage.started", "", nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := a.GetJobLogPage(jobID, 0, 10)
	if err != nil || len(page.Events) != 1 || page.Next != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestListRecentJobSummariesReadsSafeTerminalResult(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSummary(map[string]any{"jobId": jobID, "status": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := a.ListRecentJobSummaries()
	if err != nil || len(items) != 1 || items[0].JobID != jobID {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestListRecentJobSnapshotsRestoresTerminalState(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(logging.Error, "probe", "engine", "PROBE_FAILED", "probe.failed", "simulated failure", nil)
	if err := w.WriteSummary(map[string]any{"jobId": jobID, "status": "failed", "errorCode": "PROBE_FAILED", "errorMessage": "simulated failure"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := a.ListRecentJobSnapshots()
	if err != nil || len(items) != 1 || items[0].JobID != jobID || items[0].Status != "failed" || items[0].LatestSequence != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestRecoverFirmwareRejectsUnsafeRecoveryJobID(t *testing.T) {
	a := NewApp()
	if _, err := a.RecoverFirmware("request-0123456789", "../job-0123456789abcdef", "COM1", "bread-compact", "fwref-x"); err == nil {
		t.Fatal("unsafe recovery job ID was accepted")
	}
}

func TestRunWriteRequestReturnsOneExecutionForDuplicateRequestID(t *testing.T) {
	a := NewApp()
	const requestID = "request-0123456789"
	started := make(chan struct{})
	finish := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	run := func() (jobs.FlashResult, error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		close(started)
		<-finish
		return jobs.FlashResult{JobID: "job-0123456789abcdef", Status: "succeeded"}, nil
	}
	firstDone := make(chan jobs.FlashResult, 1)
	go func() {
		result, _ := a.runWriteRequest(requestID, "flash", "COM3", "bread-compact", "fwref-a", "", run)
		firstDone <- result
	}()
	<-started
	secondDone := make(chan jobs.FlashResult, 1)
	go func() {
		result, _ := a.runWriteRequest(requestID, "flash", "COM3", "bread-compact", "fwref-a", "", run)
		secondDone <- result
	}()
	close(finish)
	first, second := <-firstDone, <-secondDone
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 || first.JobID != second.JobID || first.Status != "succeeded" {
		t.Fatalf("calls=%d first=%#v second=%#v", calls, first, second)
	}
}

func TestRunWriteRequestRejectsDifferentInputsForReusedID(t *testing.T) {
	a := NewApp()
	_, err := a.runWriteRequest("request-0123456789", "flash", "COM3", "bread-compact", "fwref-a", "", func() (jobs.FlashResult, error) { return jobs.FlashResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.runWriteRequest("request-0123456789", "flash", "COM4", "bread-compact", "fwref-a", "", func() (jobs.FlashResult, error) { return jobs.FlashResult{}, nil }); err == nil {
		t.Fatal("request ID reuse with changed inputs was accepted")
	}
}

func TestRegisterVerifiedPackageRejectsUnverifiedResult(t *testing.T) {
	a := NewApp()
	if _, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Path: "firmware.clawfw", InstallStatus: "downloaded_unverified"}); err == nil {
		t.Fatal("unverified package was registered")
	}
}

func TestLookupVerifiedPackageRejectsChangedArchive(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("original archive")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Path: path, InstallStatus: "verified_ready", SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.lookupVerifiedPackage(result.PackageRef); err == nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("changed archive lookup error = %v", err)
	}
	if _, err := a.lookupVerifiedPackage(result.PackageRef); err == nil || !strings.Contains(err.Error(), "unknown, expired") {
		t.Fatalf("changed archive capability was not revoked: %v", err)
	}
}

func TestLookupVerifiedPackageRejectsExpiredReference(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("archive")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Path: path, InstallStatus: "verified_ready", SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	a.packageRefsMu.Lock()
	entry := a.verifiedPackage[result.PackageRef]
	entry.issuedAt = entry.issuedAt.Add(-verifiedPackageTTL - time.Second)
	a.verifiedPackage[result.PackageRef] = entry
	a.packageRefsMu.Unlock()
	if _, err := a.lookupVerifiedPackage(result.PackageRef); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired reference lookup error = %v", err)
	}
}
