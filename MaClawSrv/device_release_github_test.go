package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type srvReleaseRoundTripper func(*http.Request) (*http.Response, error)

func (f srvReleaseRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGitHubReleaseCatalogAcceptsOnlyCompleteSignedOfficialRelease(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "hub-release-test"
	assets := make(map[string][]byte)
	for _, profile := range srvOfficialFirmwareProfiles {
		assets[profile.assetName] = makeSrvSignedReleaseArchive(t, profile, "v8", 8, keyID, private, "payload-a")
	}
	client := srvReleaseTestClient(t, assets, "v8", 101)
	catalog := newSrvDeviceUpdateCatalog(t.TempDir())
	provider, err := newSrvGitHubReleaseCatalog(catalog, client, srvGitHubAPIBase, keyID, base64.StdEncoding.EncodeToString(public), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	document, ok := catalog.document()
	if !ok || document.SchemaVersion != srvDeviceUpdateCatalogSchemaVersion || document.Repository != srvOfficialFirmwareRepository || len(document.Releases) != 3 {
		t.Fatalf("unexpected trusted catalog: %#v", document)
	}
	for _, release := range document.Releases {
		if release.SourceAssetName == "" || release.SourceAssetID <= 0 || release.SourceAssetSize <= 0 || !validSrvSHA256(release.ArchiveSHA256) || release.ReleaseTag != "v8" || release.ReleaseSequence != 8 {
			t.Fatalf("release provenance is incomplete: %#v", release)
		}
	}
	identity := testFirmwareIdentity("esp32s3-github-catalog")
	identity.ReleaseSequence = 7
	if next, ok := catalog.latestFor(identity); !ok || next.ReleaseSequence != 8 || strings.Contains(next.NotesSummary, "https://") {
		t.Fatalf("catalog latest result: %#v, %v", next, ok)
	}
}

func TestGitHubReleaseCatalogRejectsPublishedAssetMutationWithoutReplacingSnapshot(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "hub-release-test"
	assets := make(map[string][]byte)
	for _, profile := range srvOfficialFirmwareProfiles {
		assets[profile.assetName] = makeSrvSignedReleaseArchive(t, profile, "v8", 8, keyID, private, "payload-a")
	}
	client := srvReleaseTestClient(t, assets, "v8", 101)
	catalog := newSrvDeviceUpdateCatalog(t.TempDir())
	provider, err := newSrvGitHubReleaseCatalog(catalog, client, srvGitHubAPIBase, keyID, base64.StdEncoding.EncodeToString(public), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, ok := catalog.document()
	if !ok {
		t.Fatal("missing first trusted snapshot")
	}
	assets[srvOfficialFirmwareProfiles[0].assetName] = makeSrvSignedReleaseArchive(t, srvOfficialFirmwareProfiles[0], "v8", 8, keyID, private, "payload-b")
	if err := provider.refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "mutation") {
		t.Fatalf("published asset mutation accepted: %v", err)
	}
	after, ok := catalog.document()
	if !ok || after.VerifiedAt != before.VerifiedAt || after.Releases[0].ArchiveSHA256 != before.Releases[0].ArchiveSHA256 {
		t.Fatalf("failed refresh replaced trusted snapshot: before=%#v after=%#v", before, after)
	}
}

func TestGitHubReleaseCatalogRejectsUnsignedOrIncompleteRelease(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "hub-release-test"
	assets := make(map[string][]byte)
	// All three items are required; a missing one must not mint a partial
	// catalog which would make profile visibility depend on release ordering.
	assets[srvOfficialFirmwareProfiles[0].assetName] = makeSrvSignedReleaseArchive(t, srvOfficialFirmwareProfiles[0], "v8", 8, keyID, private, "payload")
	provider, err := newSrvGitHubReleaseCatalog(newSrvDeviceUpdateCatalog(t.TempDir()), srvReleaseTestClient(t, assets, "v8", 101), srvGitHubAPIBase, keyID, base64.StdEncoding.EncodeToString(public), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "missing official asset") {
		t.Fatalf("incomplete official release accepted: %v", err)
	}
}

func srvReleaseTestClient(t *testing.T, assets map[string][]byte, tag string, releaseID int64) *http.Client {
	t.Helper()
	return &http.Client{Transport: srvReleaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == srvGitHubAPIBase+"/repos/RapidAI/MaClaw/releases/latest" {
			items := make([]srvGitHubReleaseAsset, 0, len(assets))
			for index, profile := range srvOfficialFirmwareProfiles {
				if body, ok := assets[profile.assetName]; ok {
					items = append(items, srvGitHubReleaseAsset{ID: int64(index + 1), Name: profile.assetName, Size: int64(len(body)), BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/" + tag + "/" + profile.assetName})
				}
			}
			body, err := json.Marshal(srvGitHubReleaseResponse{ID: releaseID, TagName: tag, PublishedAt: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), Assets: items})
			if err != nil {
				t.Fatal(err)
			}
			return srvReleaseHTTPResponse(request, http.StatusOK, body), nil
		}
		for name, body := range assets {
			if request.URL.String() == "https://github.com/RapidAI/MaClaw/releases/download/"+tag+"/"+name {
				return srvReleaseHTTPResponse(request, http.StatusOK, body), nil
			}
		}
		return srvReleaseHTTPResponse(request, http.StatusNotFound, nil), nil
	})}
}

func srvReleaseHTTPResponse(request *http.Request, status int, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}
}

func makeSrvSignedReleaseArchive(t *testing.T, profile srvOfficialFirmwareProfile, version string, sequence int64, keyID string, private ed25519.PrivateKey, payload string) []byte {
	t.Helper()
	boot, table, app := []byte("boot-"+payload), []byte("table-"+payload), []byte("app-"+payload)
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	offBoot, offTable, offApp := uint64(0), uint64(0x8000), uint64(0x10000)
	manifest := srvReleaseManifest{SchemaVersion: 1, PackageID: profile.profileID + "-" + version, ReleaseVersion: version, Board: srvReleaseManifestBoard{ID: profile.boardID, ProfileHash: "catalog:" + profile.profileID}, Chip: srvReleaseManifestChip{Family: "esp32s3", FlashBytes: 16 * 1024 * 1024}, SecurityBaseline: srvReleaseSecurityBase{}, Layout: srvReleaseManifestLayout{ID: "maclaw-s3-16m-factory-v2", Fingerprint: "sha256:layout", PartitionTablePath: "metadata/partition-table.bin"}, Mode: "full", WriteOrder: []string{"app", "partition-table", "bootloader"}, AppIdentity: srvReleaseAppIdentity{ProjectName: "maclaw-client", AppVersion: version, ELFSHA256: strings.Repeat("a", 64), ReleaseSequence: sequence, PSRAMBytes: 8 * 1024 * 1024}, BootVerification: srvReleaseBootPolicy{Baud: 115200, TimeoutSeconds: 30, RequiredSelfTests: []string{"local_ready"}}, Files: []srvReleaseManifestFile{{Name: "bootloader", Path: "images/bootloader.bin", Size: int64(len(boot)), SHA256: "sha256:" + digest(boot), Offset: &offBoot, Region: "bootloader"}, {Name: "partition-table", Path: "images/partition-table.bin", Size: int64(len(table)), SHA256: "sha256:" + digest(table), Offset: &offTable, Region: "partition-table"}, {Name: "app", Path: "images/app.bin", Size: int64(len(app)), SHA256: "sha256:" + digest(app), Offset: &offApp, Region: "app"}, {Path: "metadata/partition-table.bin", Size: int64(len(table)), SHA256: "sha256:" + digest(table), Region: "metadata"}}}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	rawSignature, err := json.Marshal(srvReleaseSignature{Algorithm: "ed25519", KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, rawManifest))})
	if err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	for name, raw := range map[string][]byte{"manifest.json": rawManifest, "manifest.sig": rawSignature, "images/bootloader.bin": boot, "images/partition-table.bin": table, "images/app.bin": app, "metadata/partition-table.bin": table} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}
