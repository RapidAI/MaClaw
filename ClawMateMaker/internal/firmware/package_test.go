package firmware

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawmatemaker/internal/partition"
)

const testELFSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestVerifyArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "ok.clawfw")
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","releaseVersion":"1","securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"mode":"app-only","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "images/app.bin": string(contents)})
	v, err := Verify(archive)
	if err != nil {
		t.Fatal(err)
	}
	if v.Manifest.PackageID != "bread-v1" || v.ArchiveSHA256 == "" {
		t.Fatalf("unexpected: %#v", v)
	}
}

func TestVerifyReleaseRejectsDuplicateJSONKeysAndMissingSecurityFields(t *testing.T) {
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	makeSigned := func(t *testing.T, manifest string) {
		t.Helper()
		archive := filepath.Join(t.TempDir(), "signed.clawfw")
		signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
		makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/app.bin": string(contents)})
		if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil {
			t.Fatal("ambiguous or incomplete signed manifest accepted")
		}
	}
	makeSigned(t, fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","mode":"app-only","securityBaseline":{"secureBoot":false,"secureBoot":true,"flashEncryption":false,"secureVersion":0},"files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:])))
	makeSigned(t, fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","mode":"app-only","securityBaseline":{"secureBoot":false,"flashEncryption":false},"files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:])))
}

func TestValidateReleaseManifestRequiresCanonicalELFSHA256(t *testing.T) {
	image := []byte("application firmware")
	table, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	manifest := completeReleaseManifest(image, table)
	var parsed Manifest
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.AppIdentity.ELFSHA256 = "SHA256:" + strings.ToUpper(testELFSHA256)
	if err := ValidateReleaseManifest(parsed, []byte(manifest)); err == nil {
		t.Fatal("non-canonical app ELF digest was accepted")
	}
	parsed.AppIdentity.ELFSHA256 = "not-a-digest"
	if err := ValidateReleaseManifest(parsed, []byte(manifest)); err == nil {
		t.Fatal("invalid app ELF digest was accepted")
	}
}

func TestValidateReleaseManifestRequiresSupportedChannel(t *testing.T) {
	image := []byte("application firmware")
	table, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	manifest := completeReleaseManifest(image, table)
	var parsed Manifest
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"", "dev", "stable ", "BETA"} {
		parsed.Channel = channel
		if err := ValidateReleaseManifest(parsed, []byte(manifest)); err == nil {
			t.Fatalf("unsupported channel %q was accepted", channel)
		}
	}
	parsed.Channel = ChannelBeta
	if err := ValidateReleaseManifest(parsed, []byte(manifest)); err != nil {
		t.Fatalf("supported beta channel rejected: %v", err)
	}
}

func TestVerifyReleaseRejectsManifestUTF8BOM(t *testing.T) {
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`\ufeff{"schemaVersion":1,"packageId":"bread-v1","securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"mode":"app-only","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bom.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/app.bin": string(contents)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil {
		t.Fatal("UTF-8 BOM manifest accepted")
	}
}
func TestVerifyReleaseRequiresValidSignature(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "signed.clawfw")
	contents := []byte("firmware bytes")
	table := []byte("partition table metadata")
	manifest := completeReleaseManifest(contents, table)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/app.bin": string(contents), "metadata/partition-table.bin": string(table)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRelease(archive, TrustStore{}); err == nil {
		t.Fatal("expected untrusted key rejection")
	}
}

func TestVerifyReleaseRejectsSignedIncompleteReleaseManifest(t *testing.T) {
	contents := []byte("firmware bytes")
	table := []byte("partition table metadata")
	manifest := strings.Replace(completeReleaseManifest(contents, table), `,"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]}`, "", 1)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "incomplete.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/app.bin": string(contents), "metadata/partition-table.bin": string(table)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil {
		t.Fatal("signed manifest without boot policy was accepted for installation")
	}
}

func TestVerifyReleaseRejectsFullImageWithDifferentPartitionTable(t *testing.T) {
	table := []byte("declared partition table")
	image := make([]byte, 0x8000+len(table))
	copy(image[0x8000:], []byte("different partition table"))
	imageSum := sha256.Sum256(image)
	tableSum := sha256.Sum256(table)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-full-v1","releaseVersion":"1","channel":"stable","board":{"id":"bread-compact-wifi-lcd-v1","profileHash":"catalog:bread-compact"},"chip":{"family":"esp32s3","flashBytes":16777216},"securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"layout":{"id":"layout-v1","fingerprint":"sha256:test","partitionTablePath":"metadata/partition-table.bin"},"mode":"full","appIdentity":{"projectName":"client","appVersion":"1","elfSha256":"%s","releaseSequence":1,"psramBytes":8388608},"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]},"files":[{"path":"images/full-flash.bin","size":%d,"sha256":"sha256:%s","offset":0,"region":"flash"},{"path":"metadata/partition-table.bin","size":%d,"sha256":"sha256:%s","region":"metadata"}]}`, testELFSHA256, len(image), hex.EncodeToString(imageSum[:]), len(table), hex.EncodeToString(tableSum[:]))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "mismatched-full.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/full-flash.bin": string(image), "metadata/partition-table.bin": string(table)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil {
		t.Fatal("full image with a different partition table was accepted")
	}
}

func TestVerifyReleaseRejectsSplitPartitionTableDifferentFromMetadata(t *testing.T) {
	table, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	wrongTable := append([]byte(nil), table...)
	wrongTable[0] ^= 1
	boot, offsetTable, app := uint64(0), uint64(0x8000), uint64(0x10000)
	bootRaw, appRaw := []byte("boot"), []byte("app")
	sum := func(raw []byte) string { value := sha256.Sum256(raw); return "sha256:" + hex.EncodeToString(value[:]) }
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"split-v1","releaseVersion":"1","channel":"stable","board":{"id":"bread-compact-wifi-lcd-v1","profileHash":"catalog:bread-compact"},"chip":{"family":"esp32s3","flashBytes":16777216},"securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"layout":{"id":"layout-v1","fingerprint":"sha256:test","partitionTablePath":"metadata/partition-table.bin"},"mode":"full","writeOrder":["app","partition-table","bootloader"],"appIdentity":{"projectName":"client","appVersion":"1","elfSha256":"%s","releaseSequence":1,"psramBytes":8388608},"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]},"files":[{"name":"bootloader","path":"images/bootloader.bin","size":%d,"sha256":"%s","offset":%d,"region":"bootloader"},{"name":"partition-table","path":"images/partition-table.bin","size":%d,"sha256":"%s","offset":%d,"region":"partition-table"},{"name":"app","path":"images/app.bin","size":%d,"sha256":"%s","offset":%d,"region":"app"},{"path":"metadata/partition-table.bin","size":%d,"sha256":"%s","region":"metadata"}]}`,
		testELFSHA256, len(bootRaw), sum(bootRaw), boot, len(wrongTable), sum(wrongTable), offsetTable, len(appRaw), sum(appRaw), app, len(table), sum(table))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bad-split.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/bootloader.bin": string(bootRaw), "images/partition-table.bin": string(wrongTable), "images/app.bin": string(appRaw), "metadata/partition-table.bin": string(table)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil {
		t.Fatal("split package with a mismatched partition table was accepted")
	}
}

func TestVerifyReleaseRejectsSplitImageOutsideDeclaredPartition(t *testing.T) {
	table, err := partition.Encode([]partition.Entry{
		{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000},
		{Label: "storage", Type: 1, Subtype: 0x82, Offset: 0x3b0000, Size: 0x300000},
	})
	if err != nil {
		t.Fatal(err)
	}
	boot, offsetTable, app, storage := uint64(0), uint64(0x8000), uint64(0x10000), uint64(0x6b0000)
	bootRaw, appRaw, storageRaw := []byte("boot"), []byte("app"), []byte("storage")
	sum := func(raw []byte) string { value := sha256.Sum256(raw); return "sha256:" + hex.EncodeToString(value[:]) }
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"split-outside-v1","releaseVersion":"1","channel":"stable","board":{"id":"bread-compact-wifi-lcd-v1","profileHash":"catalog:bread-compact"},"chip":{"family":"esp32s3","flashBytes":16777216},"securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"layout":{"id":"layout-v1","fingerprint":"%s","partitionTablePath":"metadata/partition-table.bin"},"mode":"full","recovery":{"powerLossBootable":false},"writeOrder":["storage","app","partition-table","bootloader"],"appIdentity":{"projectName":"client","appVersion":"1","elfSha256":"%s","releaseSequence":1,"psramBytes":8388608},"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]},"files":[{"name":"bootloader","path":"images/bootloader.bin","size":%d,"sha256":"%s","offset":%d,"region":"bootloader"},{"name":"partition-table","path":"images/partition-table.bin","size":%d,"sha256":"%s","offset":%d,"region":"partition-table"},{"name":"app","path":"images/app.bin","size":%d,"sha256":"%s","offset":%d,"region":"app"},{"name":"storage","path":"images/storage.bin","size":%d,"sha256":"%s","offset":%d,"region":"storage"},{"path":"metadata/partition-table.bin","size":%d,"sha256":"%s","region":"metadata"}]}`,
		partitionFingerprint(t, table), testELFSHA256,
		len(bootRaw), sum(bootRaw), boot,
		len(table), sum(table), offsetTable,
		len(appRaw), sum(appRaw), app,
		len(storageRaw), sum(storageRaw), storage,
		len(table), sum(table))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "outside-split.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{
		"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature),
		"images/bootloader.bin": string(bootRaw), "images/partition-table.bin": string(table), "images/app.bin": string(appRaw), "images/storage.bin": string(storageRaw), "metadata/partition-table.bin": string(table),
	})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil || !strings.Contains(err.Error(), "storage partition") {
		t.Fatalf("split image outside declared partition accepted: %v", err)
	}
}

func TestVerifyReleaseRequiresExplicitSafeRecoveryForSplitFullPackage(t *testing.T) {
	table, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	boot, offsetTable, app := uint64(0), uint64(0x8000), uint64(0x10000)
	bootRaw, appRaw := []byte("boot"), []byte("app")
	sum := func(raw []byte) string { value := sha256.Sum256(raw); return "sha256:" + hex.EncodeToString(value[:]) }
	base := fmt.Sprintf(`{"schemaVersion":1,"packageId":"split-recovery-v1","releaseVersion":"1","channel":"stable","board":{"id":"bread-compact-wifi-lcd-v1","profileHash":"catalog:bread-compact"},"chip":{"family":"esp32s3","flashBytes":16777216},"securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"layout":{"id":"layout-v1","fingerprint":"%s","partitionTablePath":"metadata/partition-table.bin"},"mode":"full","writeOrder":["app","partition-table","bootloader"],%%s"appIdentity":{"projectName":"client","appVersion":"1","elfSha256":"%s","releaseSequence":1,"psramBytes":8388608},"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]},"files":[{"name":"bootloader","path":"images/bootloader.bin","size":%d,"sha256":"%s","offset":%d,"region":"bootloader"},{"name":"partition-table","path":"images/partition-table.bin","size":%d,"sha256":"%s","offset":%d,"region":"partition-table"},{"name":"app","path":"images/app.bin","size":%d,"sha256":"%s","offset":%d,"region":"app"},{"path":"metadata/partition-table.bin","size":%d,"sha256":"%s","region":"metadata"}]}`,
		partitionFingerprint(t, table), testELFSHA256,
		len(bootRaw), sum(bootRaw), boot,
		len(table), sum(table), offsetTable,
		len(appRaw), sum(appRaw), app,
		len(table), sum(table))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(t *testing.T, recovery string, want string) {
		t.Helper()
		manifest := fmt.Sprintf(base, recovery)
		archive := filepath.Join(t.TempDir(), "split-recovery.clawfw")
		signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
		makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/bootloader.bin": string(bootRaw), "images/partition-table.bin": string(table), "images/app.bin": string(appRaw), "metadata/partition-table.bin": string(table)})
		if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("recovery=%s err=%v", recovery, err)
		}
	}
	verify(t, "", "recovery is required")
	verify(t, `"recovery":{"powerLossBootable":true},`, "powerLossBootable=false")
	manifest := fmt.Sprintf(base, `"recovery":{"powerLossBootable":false},`)
	archive := filepath.Join(t.TempDir(), "safe-split-recovery.clawfw")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/bootloader.bin": string(bootRaw), "images/partition-table.bin": string(table), "images/app.bin": string(appRaw), "metadata/partition-table.bin": string(table)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err != nil {
		t.Fatalf("safe explicit recovery package rejected: %v", err)
	}
}
func TestVerifyRejectsTraversalAndUnlisted(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.clawfw")
	makeZip(t, archive, map[string]string{"manifest.json": `{"schemaVersion":1,"packageId":"p","files":[{"path":"x.bin","size":1,"sha256":"sha256:00"}]}`, "../x.bin": "x"})
	if _, err := Verify(archive); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestVerifyRejectsAmbiguousFlashFileSpec(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad-spec.clawfw")
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":1,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "images/app.bin": string(contents)})
	if _, err := Verify(archive); err == nil {
		t.Fatal("unaligned flash image offset accepted")
	}
}

func TestVerifyRejectsOversizedFileSpec(t *testing.T) {
	offset := uint64(0x10000)
	if err := validateFileSpec(FileSpec{Path: "images/app.bin", Size: MaxFileBytes + 1, Offset: &offset, Region: "app"}); err == nil {
		t.Fatal("oversized image specification was accepted")
	}
}
func TestInstallPlanValidatesModeAndDataImpact(t *testing.T) {
	appOffset := uint64(0x10000)
	appOnly, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/app.bin", Region: "app", Offset: &appOffset}}})
	if err != nil || !appOnly.PreservesUserData || !appOnly.RequiresRecovery {
		t.Fatalf("plan=%+v err=%v", appOnly, err)
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/boot.bin", Region: "bootloader", Offset: &appOffset}}}); err == nil {
		t.Fatal("app-only non-app image accepted")
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/app.bin", Region: "app"}}}); err == nil {
		t.Fatal("app-only image without offset accepted")
	}
	fullOffset := uint64(0)
	full, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: []FileSpec{{Path: "images/full-flash.bin", Region: "flash", Offset: &fullOffset}}})
	if err != nil || full.PreservesUserData || !full.RequiresRecovery {
		t.Fatalf("plan=%+v err=%v", full, err)
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: []FileSpec{{Path: "images/full-flash.bin", Region: "flash", Offset: &appOffset}}}); err == nil {
		t.Fatal("full image with non-zero offset accepted")
	}
	if _, err := InstallPlanFor(Manifest{Mode: "factory-reset"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestInstallPlanRequiresSafeCompleteSplitWriteOrder(t *testing.T) {
	boot, table, app, storage := uint64(0), uint64(0x8000), uint64(0x10000), uint64(0x3b0000)
	images := []FileSpec{
		{Name: "bootloader", Path: "images/bootloader.bin", Region: "bootloader", Offset: &boot},
		{Name: "partition-table", Path: "images/partition-table.bin", Region: "partition-table", Offset: &table},
		{Name: "app", Path: "images/app.bin", Region: "app", Offset: &app},
		{Name: "storage", Path: "images/storage.bin", Region: "storage", Offset: &storage},
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: images, WriteOrder: []string{"storage", "app", "partition-table", "bootloader"}}); err != nil {
		t.Fatalf("safe split full plan rejected: %v", err)
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: images, WriteOrder: []string{"bootloader", "storage", "app", "partition-table"}}); err == nil {
		t.Fatal("bootloader was accepted before non-boot-critical images")
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: images, WriteOrder: []string{"storage", "app", "partition-table"}}); err == nil {
		t.Fatal("incomplete split write order was accepted")
	}
}

func TestInstallPlanRejectsUnsupportedSecurityBaseline(t *testing.T) {
	zero := uint64(0)
	for _, baseline := range []SecurityBaseline{
		{SecureBoot: true},
		{FlashEncryption: true},
		{SecureVersion: 1},
	} {
		if _, err := InstallPlanFor(Manifest{Mode: ModeFull, SecurityBaseline: baseline, Files: []FileSpec{{Path: "images/full.bin", Region: "flash", Offset: &zero}}}); err == nil {
			t.Fatalf("unsupported security baseline accepted: %#v", baseline)
		}
	}
	if err := ValidateSecurityBaseline(SecurityBaseline{}); err != nil {
		t.Fatalf("supported security baseline rejected: %v", err)
	}
}
func makeZip(t *testing.T, name string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for n, v := range entries {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func completeReleaseManifest(image, table []byte) string {
	imageSum := sha256.Sum256(image)
	tableSum := sha256.Sum256(table)
	return fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","releaseVersion":"1","channel":"stable","board":{"id":"bread-compact-wifi-lcd-v1","profileHash":"catalog:bread-compact"},"chip":{"family":"esp32s3","flashBytes":16777216},"securityBaseline":{"secureBoot":false,"flashEncryption":false,"secureVersion":0},"layout":{"id":"layout-v1","fingerprint":"sha256:test","partitionTablePath":"metadata/partition-table.bin"},"mode":"app-only","appIdentity":{"projectName":"client","appVersion":"1","elfSha256":"%s","releaseSequence":1,"psramBytes":8388608},"bootVerification":{"baud":115200,"timeoutSeconds":30,"requiredSelfTests":["local_ready"]},"files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"},{"path":"metadata/partition-table.bin","size":%d,"sha256":"sha256:%s","region":"metadata"}]}`, testELFSHA256, len(image), hex.EncodeToString(imageSum[:]), len(table), hex.EncodeToString(tableSum[:]))
}

func partitionFingerprint(t *testing.T, raw []byte) string {
	t.Helper()
	table, err := partition.Parse(raw, 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	return table.Fingerprint
}
