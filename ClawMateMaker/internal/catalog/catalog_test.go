package catalog

import (
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/flash"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficialProfileAssetNames(t *testing.T) {
	want := map[string]string{"echoear-2st": "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw", "bread-compact": "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw", "fangtang-4g": "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw"}
	for _, p := range Profiles() {
		if want[p.ID] != p.AssetName {
			t.Fatalf("%s asset = %q", p.ID, p.AssetName)
		}
		delete(want, p.ID)
	}
	if len(want) != 0 {
		t.Fatalf("profiles missing: %#v", want)
	}
}

func TestAssetLockSerializesAndHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firmware.clawfw")
	release, _, err := acquireAssetLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, waited, err := acquireAssetLock(ctx, path); err == nil || !waited {
		t.Fatalf("waited=%t err=%v", waited, err)
	}
}

func TestAssetLockReleaseAllowsNextWaiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firmware.clawfw")
	release, _, err := acquireAssetLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		next, waited, err := acquireAssetLock(context.Background(), path)
		if err == nil {
			if !waited {
				err = fmt.Errorf("second acquire did not observe existing lock")
			}
			next()
		}
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestCachedAssetRequiresExpectedDigest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "firmware.clawfw")
	data := []byte("firmware")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if _, _, ok := cachedAsset(p, int64(len(data)), "sha256:"+hex.EncodeToString(sum[:])); !ok {
		t.Fatal("valid cached asset rejected")
	}
	if _, _, ok := cachedAsset(p, int64(len(data)), "sha256:00"); ok {
		t.Fatal("wrong digest accepted")
	}
}
func TestProbeRecognitionDoesNotGuessBoard(t *testing.T) {
	r := RecognizeProbe(flash.ChipInfo{Chip: "Chip is ESP32-S3"}, flash.FlashInfo{})
	if r.Status != "requires_confirmation" || len(r.CandidateBoards) != 3 {
		t.Fatalf("unsafe recognition: %#v", r)
	}
}
func TestApplicationIdentityIsOnlyProbable(t *testing.T) {
	r := RecognizeApplicationIdentity("bread-compact-wifi-lcd-v1")
	if r.Status != "probable" || len(r.CandidateBoards) != 1 || r.CandidateBoards[0] != "bread-compact" {
		t.Fatalf("unexpected recognition: %#v", r)
	}
}
func TestLegacyApplicationIdentitySupportsMigrationOnly(t *testing.T) {
	r := RecognizeApplicationIdentityEvidence(device.AppIdentity{Protocol: 1, FirmwareTargetBoardID: "fangtang-4g-v1"})
	if r.Status != "probable" || len(r.CandidateBoards) != 1 || r.CandidateBoards[0] != "fangtang-4g" || !strings.Contains(r.Reason, "legacy") {
		t.Fatalf("unexpected legacy recognition: %#v", r)
	}
}
func TestGitHubReleaseURLAllowList(t *testing.T) {
	for _, raw := range []string{"https://github.com/RapidAI/CodeClaw/releases/download/v1/a.zip", "https://objects.githubusercontent.com/a"} {
		if !isGitHubReleaseURL(raw) {
			t.Fatalf("expected allowed: %s", raw)
		}
	}
	for _, raw := range []string{"http://github.com/a", "https://evil.example/a", "https://github.com.evil.example/a"} {
		if isGitHubReleaseURL(raw) {
			t.Fatalf("expected rejected: %s", raw)
		}
	}
}

func TestNewestStableReleaseWithAssetSkipsDraftAndPrerelease(t *testing.T) {
	const assetName = "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw"
	releases := []releaseResponse{
		{TagName: "v3-preview", Prerelease: true, Assets: []releaseAsset{{Name: assetName}}},
		{TagName: "v2-draft", Draft: true, Assets: []releaseAsset{{Name: assetName}}},
		{TagName: "v1-stable", Assets: []releaseAsset{{Name: assetName, Size: 42}}},
	}
	release, asset, ok := newestStableReleaseWithAsset(releases, assetName)
	if !ok || release.TagName != "v1-stable" || asset == nil || asset.Size != 42 {
		t.Fatalf("unexpected fallback selection: release=%#v asset=%#v ok=%t", release, asset, ok)
	}
}

func TestNewestStableReleaseWithAssetRequiresExactName(t *testing.T) {
	releases := []releaseResponse{{TagName: "v1", Assets: []releaseAsset{{Name: "lookalike.clawfw"}}}}
	if _, _, ok := newestStableReleaseWithAsset(releases, "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw"); ok {
		t.Fatal("lookalike asset was selected")
	}
}

func TestDownloadResumesTrustedPartialFile(t *testing.T) {
	data := []byte("signed firmware package bytes")
	var receivedRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRange = r.Header.Get("Range")
		if receivedRange != "bytes=7-" {
			t.Fatalf("Range = %q", receivedRange)
		}
		w.Header().Set("Content-Range", "bytes 7-"+itoa(int64(len(data)-1))+"/"+itoa(int64(len(data))))
		w.Header().Set("Content-Length", itoa(int64(len(data)-7)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[7:])
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "firmware.clawfw")
	if err := os.WriteFile(destination+".part", data[:7], 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{http: server.Client()}
	sum, size, err := c.download(context.Background(), server.URL, destination, int64(len(data)))
	if err != nil || size != int64(len(data)) || receivedRange != "bytes=7-" {
		t.Fatalf("sum=%q size=%d range=%q err=%v", sum, size, receivedRange, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("download=%q err=%v", got, err)
	}
	if _, err := os.Stat(destination + ".part"); !os.IsNotExist(err) {
		t.Fatal("partial file was not promoted")
	}
}

func TestDownloadRestartsWhenServerIgnoresRange(t *testing.T) {
	data := []byte("complete replacement package")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoa(int64(len(data))))
		_, _ = io.WriteString(w, string(data))
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "firmware.clawfw")
	if err := os.WriteFile(destination+".part", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{http: server.Client()}
	_, size, err := c.download(context.Background(), server.URL, destination, int64(len(data)))
	if err != nil || size != int64(len(data)) {
		t.Fatalf("size=%d err=%v", size, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("download=%q err=%v", got, err)
	}
}

func TestDownloadAcceptsChunkedResponseWithoutContentLength(t *testing.T) {
	data := []byte("chunked signed firmware package")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Transfer-Encoding", "chunked")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "firmware.clawfw")
	c := &Client{http: server.Client()}
	_, size, err := c.download(context.Background(), server.URL, destination, int64(len(data)))
	if err != nil || size != int64(len(data)) {
		t.Fatalf("size=%d err=%v", size, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("download=%q err=%v", got, err)
	}
}

func itoa(value int64) string { return fmt.Sprintf("%d", value) }
