package main

import (
	"bytes"
	"context"
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
	"clawmatemaker/internal/flash"
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

func TestEmbeddedFrontendKeepsPortBoundDiagnostics(t *testing.T) {
	contents, err := assets.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Fatalf("read embedded frontend: %v", err)
	}
	for _, want := range []string{
		"AutoDetectPortFirmware",
		"ConfirmDetectedBoard",
		"Connected device details · {port}",
		"Firmware is not a product picker.",
		"const renderMatchedFirmware=()=>",
		"one serial port → one read-only identity → one immutable firmware result",
		"applyAutoDetectedItem=(item)=>",
		"filtering at this one boundary makes it impossible",
		"showDetectedHardware",
		"Hardware identity and package acquisition are separate facts.",
		"refresh:'重新获取固件'",
		"offline:'改用离线固件'",
		"FIRMWARE_PACKAGE_INVALID",
		"Nothing was written; get the official firmware again.",
		"channel.disabled=true",
		"channel.disabled=false",
		"retryHint:'当前官方包未就绪。",
		"showDetectedHardware(matched,item.reason)",
		"channel.removeAttribute('aria-disabled')",
		"const showRecoveryCandidates=",
		"recoveryCatalog:'恢复：确认实物板型'",
		"recoveryBoardHint:'设备无法提供可用于自动匹配的运行时身份。",
		"if(showRecoveryCandidates(item,port,generation))return false;",
		"const selectRecoveryBoard=async(candidate)=>",
		"api().ConfirmBoard(chosen.port,candidate.id,lastProbeJobID)",
		"option.onclick=()=>void selectRecoveryBoard(candidate)",
		"const resetPortPreparation=()=>",
		"identifyGeneration++;",
		"probe.__portFirstReset=true",
		"role=\"status\" aria-live=\"polite\" aria-atomic=\"true\"",
		"__busyIndicator",
		"selected-badge",
		"active?' selected is-selected-port'",
		".device.selected{border:2px solid var(--blue)",
		".device.selected .icon{background:var(--blue);color:#fff}",
		".device.selected:after{position:absolute;top:50%;right:14px",
		"chosen?.port===d.port",
		"identifyGeneration",
		"isCurrentPort",
		"detectedFirmware=firmware",
		"lastProbeJobID!==probeJobID",
		"__currentWriteOnly",
		"__currentWriteReset",
		"__currentWriteJobBinding",
		"activeFlashJobID=''",
		"JOB_CREATED",
		"Cloudflare R2",
		"Waveshare S3 Touch AMOLED 1.75C",
		"zh-TW",
	} {
		if !strings.Contains(string(contents), want) {
			t.Fatalf("embedded frontend lost required surface %q", want)
		}
	}
}
func TestAutoDetectPortFirmwareRejectsBlankPort(t *testing.T) {
	if _, err := NewApp().AutoDetectPortFirmware("  "); err == nil || !strings.Contains(err.Error(), "serial port is required") {
		t.Fatalf("blank port error = %v", err)
	}
}

func TestConfirmDetectedBoardRejectsProbeWithoutUniqueRuntimeIdentity(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:            jobID,
		Port:             "COM3",
		Status:           "succeeded",
		DeviceBinding:    "binding",
		FinishedAt:       time.Now().UTC(),
		BoardRecognition: catalog.Recognition{Status: "requires_confirmation", CandidateBoards: []string{"bread-compact", "echoear-2st"}},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "uniquely supported board identity") {
		t.Fatalf("unexpected automatic confirmation result: %v", err)
	}
}

func TestConfirmDetectedBoardRejectsWaveshareROMOnlyEvidence(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		Chip:          flash.ChipInfo{Chip: "ESP32-S3"},
		Flash:         flash.FlashInfo{SizeBytes: 32 * 1024 * 1024},
		// ROM capacity correctly identifies the only 32 MiB profile, but normal
		// flashing still requires the same fresh protocol:2 evidence as FlashJob.
		BoardRecognition: catalog.Recognition{Status: "probable", CandidateBoards: []string{"waveshare-amoled-1.75c"}},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "protocol:2") {
		t.Fatalf("ROM-only Waveshare evidence minted confirmation: %v", err)
	}
}

func TestConfirmDetectedBoardRejectsRuntimeIdentityWithProfileMismatch(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		AppIdentity: device.AppIdentity{
			Protocol:              device.ProtocolVersion,
			FirmwareTargetBoardID: "waveshare-s3-touch-amoled-1.75c-v1",
			Chip:                  "esp32s3",
			FlashBytes:            16 * 1024 * 1024,
		},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "uniquely supported board identity") {
		t.Fatalf("inconsistent runtime identity minted confirmation: %v", err)
	}
}

func TestConfirmDetectedBoardRejectsRuntimeIdentityThatContradictsROMFlash(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		Chip:          flash.ChipInfo{Chip: "ESP32-S3"},
		Flash:         flash.FlashInfo{SizeBytes: 16 * 1024 * 1024},
		AppIdentity: device.AppIdentity{
			Protocol:              device.ProtocolVersion,
			FirmwareTargetBoardID: "waveshare-s3-touch-amoled-1.75c-v1",
			Chip:                  "esp32s3",
			FlashBytes:            32 * 1024 * 1024,
		},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "uniquely supported board identity") {
		t.Fatalf("runtime identity contradicted ROM flash but minted confirmation: %v", err)
	}
}

func TestConfirmDetectedBoardRejectsLegacyRuntimeIdentity(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		Chip:          flash.ChipInfo{Chip: "ESP32-S3"},
		Flash:         flash.FlashInfo{SizeBytes: 16 * 1024 * 1024},
		AppIdentity: device.AppIdentity{
			Protocol:              1,
			FirmwareTargetBoardID: "fangtang-4g-v1",
			Chip:                  "esp32s3",
			FlashBytes:            16 * 1024 * 1024,
		},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "uniquely supported board identity") {
		t.Fatalf("legacy runtime identity minted confirmation: %v", err)
	}
}

func TestConfirmDetectedBoardRecomputesInsteadOfTrustingPersistedRecognition(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		Chip:          flash.ChipInfo{Chip: "ESP32-S3"},
		Flash:         flash.FlashInfo{SizeBytes: 16 * 1024 * 1024},
		// This stale/corrupt cache entry must not bypass live evidence: 16 MiB
		// ROM data alone cannot prove a specific one of the three 16 MiB boards.
		BoardRecognition: catalog.Recognition{Status: "probable", CandidateBoards: []string{"waveshare-amoled-1.75c"}},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "uniquely supported board identity") {
		t.Fatalf("persisted recognition bypassed raw ROM evidence: %v", err)
	}
}

func TestConfirmDetectedBoardRejectsExpiredOrFutureProbe(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })

	for _, finishedAt := range []time.Time{
		time.Now().UTC().Add(-boardConfirmationTTL - time.Second),
		time.Now().UTC().Add(time.Second),
	} {
		a := NewApp()
		a.logRoot = t.TempDir()
		jobID := "job-0123456789abcdef"
		probe := jobs.ProbeResult{
			JobID:         jobID,
			Port:          "COM3",
			Status:        "succeeded",
			DeviceBinding: "binding",
			FinishedAt:    finishedAt,
			BoardRecognition: catalog.Recognition{
				Status:          "probable",
				CandidateBoards: []string{"bread-compact"},
			},
		}
		writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteSummary(probe); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ConfirmDetectedBoard("COM3", jobID); err == nil || !strings.Contains(err.Error(), "probe evidence expired") {
			t.Fatalf("finishedAt=%v automatic confirmation error = %v", finishedAt, err)
		}
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

func TestAppInfoReportsInjectedBuildVersion(t *testing.T) {
	previous := buildVersion
	buildVersion = "1.2.3"
	t.Cleanup(func() { buildVersion = previous })
	if got := NewApp().GetAppInfo().Version; got != "1.2.3" {
		t.Fatalf("version = %q, want injected build version", got)
	}
}

func TestDialogCopyUsesReadableLocalizedNativeDialogText(t *testing.T) {
	for _, test := range []struct {
		locale      string
		importTitle string
		filter      string
		diagnostics string
	}{
		{"zh-CN", "选择已签名的 ClawMate 固件包", "ClawMate 固件包 (*.clawfw)", "选择诊断包保存文件夹"},
		{"zh-TW", "選擇已簽署的 ClawMate 韌體套件", "ClawMate 韌體套件 (*.clawfw)", "選擇診斷套件儲存資料夾"},
		{"en", "Choose a signed ClawMate firmware package", "ClawMate firmware package (*.clawfw)", "Choose a folder for the diagnostic bundle"},
	} {
		got := dialogCopyFor(test.locale)
		if got.importTitle != test.importTitle || got.firmwareFilter != test.filter || got.diagnosticsDirectoryTitle != test.diagnostics {
			t.Fatalf("locale %s copy = %#v", test.locale, got)
		}
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
	if _, err := a.ImportFirmwarePackage("bread-compact", "zh-CN"); err != errOfficialBuildRequired {
		t.Fatalf("developer import error = %v, want official build requirement", err)
	}
	if _, err := a.FlashFirmware("request-0123456789", "COM3", "bread-compact", "fwref-test", "boardref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer flash error = %v, want official build requirement", err)
	}
	if _, err := a.RecoverFirmware("request-0123456789", "job-0123456789abcdef", "COM3", "bread-compact", "fwref-test", "boardref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer recovery error = %v, want official build requirement", err)
	}
	if _, err := a.VerifyBoot("request-0123456789", "job-0123456789abcdef", "COM3", "fwref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer boot verification error = %v, want official build requirement", err)
	}
	if _, err := a.RetryJob("request-0123456789", "job-0123456789abcdef", "COM3", "bread-compact", "fwref-test", "boardref-test"); err != errOfficialBuildRequired {
		t.Fatalf("developer retry error = %v, want official build requirement", err)
	}
}

func TestNormalFlashFailsClosedWhenRecoveryStateCannotBeRead(t *testing.T) {
	a := NewApp()
	// Use a regular file as the recovery root. os.ReadDir must reject this
	// before the write path can inspect or consume any capability.
	a.logRoot = filepath.Join(t.TempDir(), "not-a-job-directory")
	if err := os.WriteFile(a.logRoot, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.requireNoRecovery(); err == nil || !strings.Contains(err.Error(), "inspect recovery state") {
		t.Fatalf("normal flash with unreadable recovery state = %v", err)
	}
}

func TestRetryJobRejectsRecoveryRequiredJobsBeforeConfirmationIsConsumed(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	if err := jobs.WriteJournal(a.logRoot, jobs.Journal{JobID: jobID, PackageID: "package", PackageSHA256: "sha256:deadbeef", State: jobs.JournalRecoveryRequired}); err != nil {
		t.Fatal(err)
	}
	w, err := logging.New(a.logRoot, jobID, "attempt-original", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSummary(map[string]any{"status": "recovery_required", "errorCode": "FLASH_WRITE_FAILED"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}
	_, err = a.RetryJob("request-0123456789", jobID, "COM3", "bread-compact", "fwref-test", "boardref-test")
	if err == nil || !strings.Contains(err.Error(), "explicit complete ROM recovery") {
		t.Fatalf("retry recovery job error = %v", err)
	}
	if _, ok := a.confirmations["boardref-test"]; !ok {
		t.Fatal("retry rejection consumed a physical board confirmation")
	}
}

func TestRetryJobRejectsReadbackVerifiedRecoveryInFavorOfVerifyBoot(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	images := []jobs.JournalImage{{Name: "image-001", Region: "app", Offset: 0x10000, Size: 4096, SHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: jobs.JournalImageReadbackVerified}}
	plan, err := jobs.JournalPlanSHA256(images)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.WriteJournal(a.logRoot, jobs.Journal{JobID: jobID, PackageID: "package", PackageSHA256: "sha256:deadbeef", PlanSHA256: plan, Images: images, State: jobs.JournalRecoveryRequired, FlashVerified: true}); err != nil {
		t.Fatal(err)
	}
	w, err := logging.New(a.logRoot, jobID, "attempt-original", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSummary(map[string]any{"status": "recovery_required", "errorCode": "BOOT_NOT_VERIFIED"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = a.RetryJob("request-0123456789", jobID, "COM3", "bread-compact", "fwref-test", "boardref-test")
	if err == nil || !strings.Contains(err.Error(), "use VerifyBoot") {
		t.Fatalf("retry with readback evidence error = %v", err)
	}
}

func TestRetryJobReturnsStoredResultForIdenticalRequest(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	requestID := "request-0123456789"
	a.writeRequests[requestID] = &writeRequest{
		fingerprint: "retry\x00COM3\x00bread-compact\x00fwref-test\x00boardref-test\x00job-0123456789abcdef",
		done:        make(chan struct{}),
		result:      jobs.FlashResult{JobID: "job-retry", Status: "succeeded"},
	}
	close(a.writeRequests[requestID].done)
	result, err := a.RetryJob(requestID, "job-0123456789abcdef", "COM3", "bread-compact", "fwref-test", "boardref-test")
	if err != nil || result.JobID != "job-retry" || result.Status != "succeeded" {
		t.Fatalf("stored retry result=%#v err=%v", result, err)
	}
	if _, err := a.RetryJob(requestID, "job-0123456789abcdef", "COM4", "bread-compact", "fwref-test", "boardref-test"); err == nil {
		t.Fatal("retry request ID reuse with different inputs was accepted")
	}
}

func TestVerifyBootRequiresReadbackVerifiedRecoveryJournal(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	if err := jobs.WriteJournal(a.logRoot, jobs.Journal{JobID: "job-0123456789abcdef", PackageID: "package", PackageSHA256: "sha256:deadbeef", State: jobs.JournalRecoveryRequired}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("package")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	a.verifiedPackage["fwref-test"] = verifiedPackage{path: path, boardID: "bread-compact", archiveSHA256: hex.EncodeToString(sum[:]), issuedAt: time.Now().UTC()}
	_, err := a.VerifyBoot("request-0123456789", "job-0123456789abcdef", "COM3", "fwref-test")
	if err == nil || !strings.Contains(err.Error(), "readback verification") {
		t.Fatalf("VerifyBoot error = %v, want readback-evidence rejection", err)
	}
}

func TestVerifyBootRetryReturnsStoredResultAfterRecoveryWasResolved(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	requestID := "request-0123456789"
	a.bootVerifies[requestID] = &bootVerifyRequest{
		fingerprint: "job-0123456789abcdef\x00COM3\x00fwref-test",
		done:        make(chan struct{}),
		result:      jobs.BootVerificationResult{JobID: "job-verified", Status: "succeeded", Attempts: 1},
	}
	close(a.bootVerifies[requestID].done)
	// No recovery journal or firmware capability is left after the original
	// successful call. The exact RPC retry must still return its stored result.
	result, err := a.VerifyBoot(requestID, "job-0123456789abcdef", "COM3", "fwref-test")
	if err != nil || result.Status != "succeeded" || result.JobID != "job-verified" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := a.VerifyBoot(requestID, "job-0123456789abcdef", "COM4", "fwref-test"); err == nil {
		t.Fatal("request ID reuse with a changed port was accepted")
	}
}

func TestOfficialBuildRejectsUnsupportedFirmwareChannel(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	if _, err := NewApp().GetLatestFirmwareForChannel("bread-compact", "dev"); err == nil {
		t.Fatal("unsupported firmware channel was accepted")
	}
}

func TestDialogCopyUsesSupportedLocaleAndDefaultsSafely(t *testing.T) {
	cases := []struct {
		locale      string
		importTitle string
		diagnostics string
	}{
		{locale: "zh-CN", importTitle: "选择已签名的 ClawMate 固件包", diagnostics: "选择诊断包保存文件夹"},
		{locale: "zh-TW", importTitle: "選擇已簽署的 ClawMate 韌體套件", diagnostics: "選擇診斷套件儲存資料夾"},
		{locale: "en", importTitle: "Choose a signed ClawMate firmware package", diagnostics: "Choose a folder for the diagnostic bundle"},
		{locale: "untrusted-locale", importTitle: "选择已签名的 ClawMate 固件包", diagnostics: "选择诊断包保存文件夹"},
	}
	for _, test := range cases {
		copy := dialogCopyFor(test.locale)
		if copy.importTitle != test.importTitle || copy.diagnosticsDirectoryTitle != test.diagnostics || copy.firmwareFilter == "" {
			t.Fatalf("locale %q copy=%+v", test.locale, copy)
		}
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

func TestAutoDetectionResultSerializesFirmwareUpdateWithoutHostPaths(t *testing.T) {
	result := AutoDetectionResult{Devices: []AutoDetectedDevice{{
		Device:   device.Candidate{Port: "COM4"},
		Status:   "firmware_ready",
		Firmware: &catalog.DownloadedRelease{BoardID: "bread-compact", FirmwareVersion: 13, Path: `C:\private\bread.clawfw`},
		Update:   catalog.CompareFirmwareVersions(12, 13),
	}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`C:\private`)) {
		t.Fatalf("auto-detection result leaked a host path: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"update":{"installedVersion":12,"availableVersion":13,"status":"upgrade_available"}`)) {
		t.Fatalf("auto-detection result did not expose the safe update decision: %s", encoded)
	}
}

func TestSingleAutoDetectionResultSerializesTopLevelFirmwareUpdate(t *testing.T) {
	result := AutoDetectionResult{
		Status: "firmware_ready",
		Update: catalog.CompareFirmwareVersions(12, 13),
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"update":{"installedVersion":12,"availableVersion":13,"status":"upgrade_available"}`)) {
		t.Fatalf("single auto-detection result omitted update: %s", encoded)
	}
}

func TestAppCompareFirmwareVersions(t *testing.T) {
	got := NewApp().CompareFirmwareVersions(8, 9)
	if got.Status != "upgrade_available" || got.InstalledVersion != 8 || got.AvailableVersion != 9 {
		t.Fatalf("unexpected firmware update decision: %+v", got)
	}
}

func TestWaveshareFlashPlanUsesItsExact32MiBProfileCapacity(t *testing.T) {
	profile, err := catalog.Profile("waveshare-amoled-1.75c")
	if err != nil {
		t.Fatal(err)
	}
	if profile.FlashBytes != 32*1024*1024 {
		t.Fatalf("Waveshare flash capacity = %d", profile.FlashBytes)
	}
	// flashReservedFirmware binds ExpectedFlashBytes directly from the profile;
	// retain this source-level guard so a new profile cannot silently inherit
	// the old 16 MiB default at the irreversible write boundary.
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "ExpectedFlashBytes: profile.FlashBytes") {
		t.Fatal("flash request does not use the selected profile flash capacity")
	}
}

func TestAutoDetectionResultExposesOnlySafeHostAccessEvidence(t *testing.T) {
	result := AutoDetectionResult{Devices: []AutoDetectedDevice{{
		Device: device.Candidate{Port: "COM4"},
		Access: device.HostAccess{Platform: "windows", Status: "blocked", Port: "COM4", DriverNeeded: true, AccessNeeded: true},
		Status: "access_blocked",
	}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"access"`, `"status":"blocked"`, `"driverNeeded":true`, `"accessNeeded":true`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("missing host access evidence %q in %s", expected, encoded)
		}
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
		Channel:       "stable",
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

func writeSuccessfulProbeEvidence(t *testing.T, a *App, jobID, port string, finishedAt time.Time) {
	t.Helper()
	dir := filepath.Join(a.logsPath(), jobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(jobs.ProbeResult{JobID: jobID, Port: port, Status: "succeeded", DeviceBinding: "3d7db9c93eb0c7c08860de80f196319679595ade1f572454d659f83d84f02556", Chip: flash.ChipInfo{Chip: "ESP32-S3"}, Flash: flash.FlashInfo{SizeBytes: 16 * 1024 * 1024}, StartedAt: finishedAt.Add(-time.Second), FinishedAt: finishedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmBoardRequiresFreshSuccessfulProbe(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	if _, err := a.ConfirmBoard("COM3", "bread-compact", "job-0123456789abcdef"); err == nil {
		t.Fatal("missing probe evidence minted a board confirmation")
	}
	writeSuccessfulProbeEvidence(t, a, "job-0123456789abcdef", "COM3", time.Now().UTC())
	confirmation, err := a.ConfirmBoard("COM3", "bread-compact", "job-0123456789abcdef")
	if err != nil || confirmation.ConfirmationRef == "" || confirmation.Port != "COM3" || confirmation.BoardID != "bread-compact" {
		t.Fatalf("confirmation=%#v err=%v", confirmation, err)
	}
	if stored := a.confirmations[confirmation.ConfirmationRef]; stored.deviceBinding == "" {
		t.Fatal("confirmation did not retain the probe device binding")
	}
}

func TestConfirmBoardRejectsFailedMismatchedOrExpiredProbe(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	dir := filepath.Join(a.logsPath(), jobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	failed, _ := json.Marshal(jobs.ProbeResult{JobID: jobID, Port: "COM3", Status: "failed", FinishedAt: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), failed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmBoard("COM3", "bread-compact", jobID); err == nil {
		t.Fatal("failed probe minted a board confirmation")
	}
	writeSuccessfulProbeEvidence(t, a, jobID, "COM4", time.Now().UTC())
	if _, err := a.ConfirmBoard("COM3", "bread-compact", jobID); err == nil {
		t.Fatal("mismatched probe port minted a board confirmation")
	}
	writeSuccessfulProbeEvidence(t, a, jobID, "COM3", time.Now().UTC().Add(-boardConfirmationTTL-time.Second))
	if _, err := a.ConfirmBoard("COM3", "bread-compact", jobID); err == nil {
		t.Fatal("expired probe minted a board confirmation")
	}
}

func TestConfirmBoardRejectsProbeWithoutROMIdentity(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	dir := filepath.Join(a.logsPath(), jobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(jobs.ProbeResult{JobID: jobID, Port: "COM3", Status: "succeeded", FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmBoard("COM3", "bread-compact", jobID); err == nil {
		t.Fatal("probe without ROM identity minted a board confirmation")
	}
}

func TestConfirmBoardRejectsManualSelectionThatContradictsROMCapacity(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	probe := jobs.ProbeResult{
		JobID:         jobID,
		Port:          "COM3",
		Status:        "succeeded",
		DeviceBinding: "binding",
		FinishedAt:    time.Now().UTC(),
		Chip:          flash.ChipInfo{Chip: "ESP32-S3"},
		Flash:         flash.FlashInfo{SizeBytes: 16 * 1024 * 1024},
	}
	writer, err := logging.New(a.logRoot, jobID, "attempt-probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteSummary(probe); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmBoard("COM3", "waveshare-amoled-1.75c", jobID); err == nil || !strings.Contains(err.Error(), "ROM evidence does not match") {
		t.Fatalf("manual 32 MiB profile selection on 16 MiB ROM was accepted: %v", err)
	}
}

func TestConsumeBoardConfirmationBindsAndConsumes(t *testing.T) {
	a := NewApp()
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "3d7db9c93eb0c7c08860de80f196319679595ade1f572454d659f83d84f02556", issuedAt: time.Now().UTC()}
	if _, err := a.consumeBoardConfirmation("boardref-test", "COM4", "bread-compact"); err == nil {
		t.Fatal("confirmation was accepted for a different port")
	}
	if _, err := a.consumeBoardConfirmation("boardref-test", "COM3", "echoear-2st"); err == nil {
		t.Fatal("confirmation was accepted for a different board")
	}
	confirmation, err := a.consumeBoardConfirmation("boardref-test", "COM3", "bread-compact")
	if err != nil || confirmation.deviceBinding != "3d7db9c93eb0c7c08860de80f196319679595ade1f572454d659f83d84f02556" {
		t.Fatalf("matching confirmation=%#v err=%v", confirmation, err)
	}
	if _, err := a.consumeBoardConfirmation("boardref-test", "COM3", "bread-compact"); err == nil {
		t.Fatal("confirmation was replayed")
	}
}

func TestConsumeBoardConfirmationRejectsExpiration(t *testing.T) {
	a := NewApp()
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", issuedAt: time.Now().UTC().Add(-boardConfirmationTTL - time.Second)}
	if _, err := a.consumeBoardConfirmation("boardref-test", "COM3", "bread-compact"); err == nil {
		t.Fatal("expired confirmation was accepted")
	}
}

func TestWriteCapabilitiesRejectFutureIssueTimes(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("package")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	a.verifiedPackage["fwref-test"] = verifiedPackage{
		path:          path,
		boardID:       "bread-compact",
		archiveSHA256: hex.EncodeToString(sum[:]),
		issuedAt:      time.Now().UTC().Add(time.Second),
	}
	if _, err := a.lookupVerifiedPackage("fwref-test"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("future package capability error = %v", err)
	}
	a.confirmations["boardref-test"] = boardConfirmation{
		port:     "COM3",
		boardID:  "bread-compact",
		issuedAt: time.Now().UTC().Add(time.Second),
	}
	if _, err := a.consumeBoardConfirmation("boardref-test", "COM3", "bread-compact"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("future board confirmation error = %v", err)
	}
}

func TestConsumeBoardConfirmationIsSingleUseUnderConcurrency(t *testing.T) {
	a := NewApp()
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := a.consumeBoardConfirmation("boardref-test", "COM3", "bread-compact")
			errs <- err
		}()
	}
	close(start)
	var succeeded int
	for range 2 {
		if err := <-errs; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("confirmation was consumed %d times, want exactly once", succeeded)
	}
}

func TestVerifyBootWaitsForRecoveryOperationLock(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.recoveryOperationMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := a.VerifyBoot("request-0123456789", "job-0123456789abcdef", "COM3", "fwref-test")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("VerifyBoot bypassed recovery operation lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	a.recoveryOperationMu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("invalid boot verification unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("VerifyBoot did not resume after recovery operation lock was released")
	}
}

func TestRecoveryStartJobWaitsForRecoveryOperationLock(t *testing.T) {
	a := NewApp()
	plan := &preparedFlashPlan{FlashPlan: FlashPlan{
		PlanID:    "plan-recovery-lock",
		PlanHash:  "hash",
		Recovery:  true,
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}, requestID: "request-0123456789"}
	a.plans[plan.PlanID] = plan
	a.recoveryOperationMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := a.StartJob(plan.PlanID, plan.PlanHash)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("recovery StartJob bypassed recovery operation lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	a.recoveryOperationMu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("incomplete recovery plan unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("recovery StartJob did not resume after recovery operation lock was released")
	}
}

func TestPrepareJobKeepsConfirmationUntilStartAndRejectsChangedPlan(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("package")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	a.verifiedPackage["fwref-test"] = verifiedPackage{path: path, boardID: "bread-compact", archiveSHA256: hex.EncodeToString(sum[:]), installPlan: "app-only", preservesUserData: true, requiresRecovery: true, issuedAt: time.Now().UTC()}
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}
	plan, err := a.PrepareJob("request-0123456789", "COM3", "bread-compact", "fwref-test", "boardref-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanID == "" || plan.PlanHash == "" || plan.Recovery || plan.Port != "COM3" || plan.InstallPlan != "app-only" || !plan.PreservesUserData || !plan.RequiresRecovery {
		t.Fatalf("plan=%#v", plan)
	}
	if _, ok := a.confirmations["boardref-test"]; !ok {
		t.Fatal("prepare must not consume board confirmation")
	}
	if _, err := a.StartJob(plan.PlanID, "changed"); err == nil {
		t.Fatal("changed plan hash was accepted")
	}
	if _, err := a.StartJob(plan.PlanID, plan.PlanHash); err == nil {
		t.Fatal("invalidated plan was accepted after changed hash attempt")
	}
}

func TestPrepareJobUsesVerifiedPackageInstallImpact(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })

	for _, test := range []struct {
		name              string
		installPlan       string
		preservesUserData bool
		requiresRecovery  bool
	}{
		{name: "app only", installPlan: "app-only", preservesUserData: true, requiresRecovery: true},
		{name: "full", installPlan: "full", preservesUserData: false, requiresRecovery: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := NewApp()
			path := filepath.Join(t.TempDir(), "release.clawfw")
			contents := []byte("package")
			if err := os.WriteFile(path, contents, 0600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(contents)
			a.verifiedPackage["fwref-test"] = verifiedPackage{
				path:              path,
				boardID:           "bread-compact",
				archiveSHA256:     hex.EncodeToString(sum[:]),
				installPlan:       test.installPlan,
				preservesUserData: test.preservesUserData,
				requiresRecovery:  test.requiresRecovery,
				issuedAt:          time.Now().UTC(),
			}
			a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}

			plan, err := a.PrepareJob("request-0123456789", "COM3", "bread-compact", "fwref-test", "boardref-test", "")
			if err != nil {
				t.Fatal(err)
			}
			if plan.InstallPlan != test.installPlan || plan.PreservesUserData != test.preservesUserData || plan.RequiresRecovery != test.requiresRecovery {
				t.Fatalf("plan impact = %#v, want mode=%q preserves=%t recovery=%t", plan, test.installPlan, test.preservesUserData, test.requiresRecovery)
			}
		})
	}
}

func TestStartJobRechecksRecoveryStateBeforeConfirmationIsConsumed(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })
	a := NewApp()
	a.logRoot = t.TempDir()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("package")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	a.verifiedPackage["fwref-test"] = verifiedPackage{path: path, boardID: "bread-compact", archiveSHA256: hex.EncodeToString(sum[:]), installPlan: "app-only", issuedAt: time.Now().UTC()}
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}
	plan, err := a.PrepareJob("request-0123456789", "COM3", "bread-compact", "fwref-test", "boardref-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.WriteJournal(a.logRoot, jobs.Journal{JobID: "job-0123456789abcdef", PackageID: "package", PackageSHA256: "sha256:deadbeef", State: jobs.JournalRecoveryRequired}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StartJob(plan.PlanID, plan.PlanHash); err == nil || !strings.Contains(err.Error(), "recovery is required") {
		t.Fatalf("start with newly-required recovery = %v", err)
	}
	if _, ok := a.confirmations["boardref-test"]; !ok {
		t.Fatal("new recovery lock consumed physical board confirmation")
	}
}

func TestStartJobKeepsConfirmationWhenPackageIsAlreadyReserved(t *testing.T) {
	previous := releaseBuild
	releaseBuild = "true"
	t.Cleanup(func() { releaseBuild = previous })

	a := NewApp()
	a.logRoot = t.TempDir()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("package")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	a.verifiedPackage["fwref-test"] = verifiedPackage{path: path, boardID: "bread-compact", archiveSHA256: hex.EncodeToString(sum[:]), installPlan: "app-only", issuedAt: time.Now().UTC()}
	a.confirmations["boardref-test"] = boardConfirmation{port: "COM3", boardID: "bread-compact", deviceBinding: "binding", issuedAt: time.Now().UTC()}
	plan, err := a.PrepareJob("request-0123456789", "COM3", "bread-compact", "fwref-test", "boardref-test", "")
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := a.reserveVerifiedPackage("fwref-test")
	if err != nil {
		t.Fatal(err)
	}
	defer release(false)

	if _, err := a.StartJob(plan.PlanID, plan.PlanHash); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("start with reserved package error = %v", err)
	}
	if _, ok := a.confirmations["boardref-test"]; !ok {
		t.Fatal("package reservation failure consumed physical board confirmation")
	}
}

func TestStartJobReturnsSameCompletedResultForPlanRetry(t *testing.T) {
	a := NewApp()
	plan := &preparedFlashPlan{FlashPlan: FlashPlan{PlanID: "plan-test", PlanHash: "hash", ExpiresAt: time.Now().UTC().Add(time.Minute)}, start: &planStart{done: make(chan struct{}), result: jobs.FlashResult{JobID: "job-0123456789abcdef", Status: "succeeded"}}}
	close(plan.start.done)
	a.plans[plan.PlanID] = plan
	result, err := a.StartJob(plan.PlanID, plan.PlanHash)
	if err != nil || result.Status != "succeeded" || result.JobID != "job-0123456789abcdef" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestStartJobWrongHashDoesNotInvalidateCorrectRetry(t *testing.T) {
	a := NewApp()
	plan := &preparedFlashPlan{FlashPlan: FlashPlan{PlanID: "plan-hash", PlanHash: "correct", ExpiresAt: time.Now().UTC().Add(time.Minute)}, start: &planStart{done: make(chan struct{}), result: jobs.FlashResult{Status: "succeeded"}}}
	close(plan.start.done)
	a.plans[plan.PlanID] = plan
	if _, err := a.StartJob(plan.PlanID, "wrong"); err == nil {
		t.Fatal("wrong plan hash accepted")
	}
	if result, err := a.StartJob(plan.PlanID, "correct"); err != nil || result.Status != "succeeded" {
		t.Fatalf("correct retry result=%#v err=%v", result, err)
	}
}

func TestStartJobRetrySurvivesPlanExpiryAfterItStarted(t *testing.T) {
	a := NewApp()
	plan := &preparedFlashPlan{FlashPlan: FlashPlan{PlanID: "plan-expired-running", PlanHash: "hash", ExpiresAt: time.Now().UTC().Add(-time.Second)}, start: &planStart{done: make(chan struct{}), result: jobs.FlashResult{Status: "succeeded"}}}
	close(plan.start.done)
	a.plans[plan.PlanID] = plan
	if result, err := a.StartJob(plan.PlanID, "hash"); err != nil || result.Status != "succeeded" {
		t.Fatalf("started plan retry result=%#v err=%v", result, err)
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

func TestGetJobLogPageFilteredRestrictsToStructuredLogFields(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(logging.Info, "download", "catalog", "MIRROR_SELECTED", "mirror.selected", "", nil)
	w.Event(logging.Error, "flash", "engine", "FLASH_VERIFY_FAILED", "flash.verify", "", nil)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := a.GetJobLogPageFiltered(jobID, 0, 10, logging.Filter{Severity: logging.Error, Stage: "flash", Component: "engine", Code: "FLASH_VERIFY_FAILED"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Code != "FLASH_VERIFY_FAILED" || page.Next != 2 {
		t.Fatalf("page=%#v", page)
	}
}

func TestGetJobLogPageFilteredRejectsInvalidFilter(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	if _, err := a.GetJobLogPageFiltered("job-0123456789abcdef", 0, 10, logging.Filter{Severity: logging.Severity("verbose")}); err == nil {
		t.Fatal("invalid severity filter was accepted")
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

func TestGetJobSnapshotReadsOnlyTheValidatedJob(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Event(logging.Info, "flash", "engine", "FLASH_TRANSFER_PROGRESS", "flash.transfer.progress", "", map[string]any{"transferredBytes": 12, "totalBytes": 20})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.GetJobSnapshot(jobID)
	if err != nil || snapshot.JobID != jobID || snapshot.LastEvent == nil || snapshot.LastEvent.Code != "FLASH_TRANSFER_PROGRESS" {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	if _, err := a.GetJobSnapshot("../" + jobID); err == nil {
		t.Fatal("unsafe snapshot job ID accepted")
	}
}

func TestRecoverFirmwareRejectsUnsafeRecoveryJobID(t *testing.T) {
	a := NewApp()
	if _, err := a.RecoverFirmware("request-0123456789", "../job-0123456789abcdef", "COM1", "bread-compact", "fwref-x", "boardref-test"); err == nil {
		t.Fatal("unsafe recovery job ID was accepted")
	}
}

func TestCancelJobOnlyCancelsTheMatchingActiveWrite(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	a.activeWrites["job-0123456789abcdef"] = activeWrite{cancel: cancel}
	if err := a.CancelJob("job-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("active job context was not cancelled")
	}
	if err := a.CancelJob("job-ffffffffffffffff"); err == nil {
		t.Fatal("unknown active job accepted")
	}
	if err := a.CancelJob("../job-0123456789abcdef"); err == nil {
		t.Fatal("unsafe job ID accepted")
	}
}

func TestPreventCloseWhileWritingCancelsOnlyWhenNoWriteIsActive(t *testing.T) {
	a := NewApp()
	if a.PreventCloseWhileWriting(context.Background()) {
		t.Fatal("idle application close was blocked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.activeWrites["job-0123456789abcdef"] = activeWrite{cancel: cancel}
	if !a.PreventCloseWhileWriting(context.Background()) {
		t.Fatal("active write did not block window close")
	}
	select {
	case <-ctx.Done():
		t.Fatal("close guard must not silently cancel; user must use explicit cancellation")
	default:
	}
	a.cancelActiveWrites()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown fallback did not cancel active write")
	}
}

func TestCancelJobIsIdempotentForPersistedTerminalJob(t *testing.T) {
	a := NewApp()
	a.logRoot = t.TempDir()
	jobID := "job-0123456789abcdef"
	w, err := logging.New(a.logRoot, jobID, "attempt-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSummary(map[string]any{"status": "failed", "errorCode": "JOB_CANCELLED"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.CancelJob(jobID); err != nil {
		t.Fatalf("terminal cancel retry = %v", err)
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
		result, _ := a.runWriteRequest(requestID, "flash", "COM3", "bread-compact", "fwref-a", "boardref-a", "", run)
		firstDone <- result
	}()
	<-started
	secondDone := make(chan jobs.FlashResult, 1)
	go func() {
		result, _ := a.runWriteRequest(requestID, "flash", "COM3", "bread-compact", "fwref-a", "boardref-a", "", run)
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
	_, err := a.runWriteRequest("request-0123456789", "flash", "COM3", "bread-compact", "fwref-a", "boardref-a", "", func() (jobs.FlashResult, error) { return jobs.FlashResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.runWriteRequest("request-0123456789", "flash", "COM4", "bread-compact", "fwref-a", "boardref-a", "", func() (jobs.FlashResult, error) { return jobs.FlashResult{}, nil }); err == nil {
		t.Fatal("request ID reuse with changed inputs was accepted")
	}
}

func TestRegisterVerifiedPackageRejectsUnverifiedResult(t *testing.T) {
	a := NewApp()
	if _, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Path: "firmware.clawfw", InstallStatus: "downloaded_unverified"}); err == nil {
		t.Fatal("unverified package was registered")
	}
}

func TestRegisterVerifiedPackageRejectsMissingOrUnsupportedChannel(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("verified firmware archive")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	base := catalog.DownloadedRelease{
		BoardID:       "bread-compact",
		Path:          path,
		InstallStatus: "verified_ready",
		SHA256:        hex.EncodeToString(sum[:]),
	}
	for _, channel := range []string{"", "dev"} {
		candidate := base
		candidate.Channel = channel
		if _, err := a.registerVerifiedPackage(candidate); err == nil {
			t.Fatalf("channel %q was accepted", channel)
		}
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
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Channel: "stable", Path: path, InstallStatus: "verified_ready", SHA256: hex.EncodeToString(sum[:])})
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
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Channel: "stable", Path: path, InstallStatus: "verified_ready", SHA256: hex.EncodeToString(sum[:])})
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

func TestReserveVerifiedPackageIsExclusiveAndReleasesAfterPrewriteFailure(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "release.clawfw")
	contents := []byte("archive")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	result, err := a.registerVerifiedPackage(catalog.DownloadedRelease{BoardID: "bread-compact", Channel: "stable", Path: path, InstallStatus: "verified_ready", SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := a.reserveVerifiedPackage(result.PackageRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.reserveVerifiedPackage(result.PackageRef); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("concurrent reservation error = %v", err)
	}
	release(false)
	if _, retryRelease, err := a.reserveVerifiedPackage(result.PackageRef); err != nil {
		t.Fatalf("pre-write failure did not release package capability: %v", err)
	} else {
		retryRelease(true)
	}
	if _, _, err := a.reserveVerifiedPackage(result.PackageRef); err == nil {
		t.Fatal("successful reservation did not revoke package capability")
	}
}
