package catalog

import (
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/flash"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawmatemaker/internal/logging"
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

func TestValidateManifestBindingRejectsCrossBoardOrBroadPackage(t *testing.T) {
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifestBinding(profile, "bread-compact-wifi-lcd-v1", "catalog:bread-compact", "esp32s3", 16*1024*1024); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	for _, test := range []struct {
		name        string
		firmwareID  string
		profileHash string
		chip        string
		flash       int64
	}{
		{"cross board", "fangtang-4g-v1", "catalog:bread-compact", "esp32s3", 16 * 1024 * 1024},
		{"profile marker", "bread-compact-wifi-lcd-v1", "catalog:echoear-2st", "esp32s3", 16 * 1024 * 1024},
		{"chip", "bread-compact-wifi-lcd-v1", "catalog:bread-compact", "esp32", 16 * 1024 * 1024},
		{"flash", "bread-compact-wifi-lcd-v1", "catalog:bread-compact", "esp32s3", 8 * 1024 * 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateManifestBinding(profile, test.firmwareID, test.profileHash, test.chip, test.flash); err == nil {
				t.Fatal("invalid manifest binding accepted")
			}
		})
	}
}

func TestProfileForFirmwareBoardIDIsExactAndCaseInsensitive(t *testing.T) {
	profile, err := ProfileForFirmwareBoardID("BREAD-COMPACT-WIFI-LCD-V1")
	if err != nil || profile.ID != "bread-compact" {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
	if _, err := ProfileForFirmwareBoardID("not-a-board"); err == nil {
		t.Fatal("unknown firmware board target accepted")
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
	for _, raw := range []string{"https://github.com/RapidAI/MaClaw/releases/download/v1/a.zip", "https://objects.githubusercontent.com/a"} {
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

func TestApprovedDownloadURLRejectsExplicitPortsAndUserInfo(t *testing.T) {
	for _, raw := range []string{
		"https://github.com:443/RapidAI/MaClaw/releases/download/v1/a.clawfw",
		"https://user@github.com/RapidAI/MaClaw/releases/download/v1/a.clawfw",
		"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev:8443/latest/a.clawfw",
	} {
		if isApprovedDownloadURL(raw) {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
}

func TestReleaseChannelUsesExactBetaTopology(t *testing.T) {
	client, err := NewClientForChannel(t.TempDir(), BetaChannel)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.mirrors) != 2 || client.mirrors[0].url != "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/beta.json" || client.mirrors[1].url != "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/beta.json" {
		t.Fatalf("beta manifests = %#v", client.mirrors)
	}
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	item := mirrorManifestAsset{
		Name: profile.AssetName,
		URL:  "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/beta/" + profile.AssetName,
		URLs: []string{
			"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/beta/" + profile.AssetName,
			"https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/beta/" + profile.AssetName,
		},
	}
	if !validMirrorAssetTopology(item, profile.AssetName, BetaChannel) {
		t.Fatal("valid beta topology rejected")
	}
	if validMirrorAssetTopology(item, profile.AssetName, StableChannel) {
		t.Fatal("beta topology was accepted for stable")
	}
	if _, err := NewClientForChannel(t.TempDir(), ReleaseChannel("dev")); err == nil {
		t.Fatal("unapproved channel was accepted")
	}
}

func TestApprovedRedirectEnforcesHostAndHopLimit(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://github.com/RapidAI/MaClaw/releases/download/v1/a.clawfw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := approvedRedirect(request, make([]*http.Request, maxApprovedRedirects)); err == nil {
		t.Fatal("redirect hop limit was not enforced")
	}
	unsafe, err := http.NewRequest(http.MethodGet, "https://example.invalid/a.clawfw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := approvedRedirect(unsafe, nil); err == nil {
		t.Fatal("redirect outside allow-list was accepted")
	}
}

func TestMirrorAssetTopologyRequiresExactR2AndCOSPaths(t *testing.T) {
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	valid := mirrorManifestAsset{
		Name: profile.AssetName,
		URL:  cosLatestAssetBase + profile.AssetName,
		URLs: []string{r2LatestAssetBase + profile.AssetName, cosLatestAssetBase + profile.AssetName},
	}
	if !validMirrorAssetTopology(valid, profile.AssetName, StableChannel) {
		t.Fatal("valid R2/COS topology rejected")
	}
	for _, item := range []mirrorManifestAsset{
		{Name: profile.AssetName, URL: cosLatestAssetBase + profile.AssetName, URLs: []string{r2LatestAssetBase + "other.clawfw", cosLatestAssetBase + profile.AssetName}},
		{Name: profile.AssetName, URL: cosLatestAssetBase + profile.AssetName, URLs: []string{r2LatestAssetBase + profile.AssetName}},
		{Name: profile.AssetName, URL: r2LatestAssetBase + profile.AssetName, URLs: []string{r2LatestAssetBase + profile.AssetName, cosLatestAssetBase + profile.AssetName}},
	} {
		if validMirrorAssetTopology(item, profile.AssetName, StableChannel) {
			t.Fatalf("invalid mirror topology accepted: %#v", item)
		}
	}
}

func TestReleaseRepositoryUsesCanonicalAssetOwner(t *testing.T) {
	if Repository != "RapidAI/MaClaw" {
		t.Fatalf("release repository = %q, want canonical RapidAI/MaClaw", Repository)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDownloadLatestFallsBackToNewestStableExactAsset(t *testing.T) {
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed firmware package bytes")
	sum := sha256.Sum256(payload)
	assetURL := "https://github.com/RapidAI/MaClaw/releases/download/v8/" + profile.AssetName
	client := &Client{
		cacheDir: t.TempDir(),
		apiURL:   latestReleaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body []byte
			switch req.URL.String() {
			case latestReleaseURL:
				body, _ = json.Marshal(releaseResponse{TagName: "v9-without-firmware", PublishedAt: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)})
			case releasesURL:
				body, _ = json.Marshal([]releaseResponse{
					{TagName: "v9-preview", Prerelease: true, Assets: []releaseAsset{{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: assetURL}}},
					{TagName: "v8-stable", PublishedAt: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC), Assets: []releaseAsset{{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: assetURL, Digest: "sha256:" + hex.EncodeToString(sum[:])}}},
				})
			case assetURL:
				body = payload
			default:
				t.Fatalf("unexpected release request: %s", req.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
		})},
	}
	var events []logging.Event
	result, err := client.DownloadLatest(context.Background(), profile.ID, func(event logging.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseTag != "v8-stable" || result.AssetName != profile.AssetName || result.Size != int64(len(payload)) {
		t.Fatalf("unexpected download result: %#v", result)
	}
	if got, readErr := os.ReadFile(result.Path); readErr != nil || string(got) != string(payload) {
		t.Fatalf("cached payload=%q read error=%v", got, readErr)
	}
	codes := make(map[string]bool, len(events))
	for _, event := range events {
		codes[event.Code] = true
	}
	for _, want := range []string{"RELEASE_LATEST_MISSING_ASSET", "RELEASE_FALLBACK_SELECTED", "RELEASE_ASSET_SELECTED", "RELEASE_DOWNLOAD_COMPLETED"} {
		if !codes[want] {
			t.Fatalf("missing diagnostic event %s: %#v", want, events)
		}
	}
}

func TestDownloadLatestBetaUsesNewestPrereleaseExactAsset(t *testing.T) {
	profile, err := Profile("echoear-2st")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("beta signed firmware package bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	assetURL := "https://github.com/RapidAI/MaClaw/releases/download/v9-beta/" + profile.AssetName
	client, err := NewClientForChannel(t.TempDir(), BetaChannel)
	if err != nil {
		t.Fatal(err)
	}
	client.mirrors = nil
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.String() {
		case releasesURL:
			body, _ = json.Marshal([]releaseResponse{
				{TagName: "v10-stable", Prerelease: false, Assets: []releaseAsset{{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: assetURL, Digest: "sha256:" + digest}}},
				{TagName: "v9-beta", Prerelease: true, Assets: []releaseAsset{{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: assetURL, Digest: "sha256:" + digest}}},
			})
		case assetURL:
			if req.Method == http.MethodGet {
				body = payload
			}
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		header := make(http.Header)
		if len(body) > 0 {
			header.Set("Content-Length", itoa(int64(len(body))))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}
	result, err := client.DownloadLatest(context.Background(), profile.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseTag != "v9-beta" {
		t.Fatalf("beta release = %q, want v9-beta", result.ReleaseTag)
	}
}

func TestDownloadLatestFallsBackToTwoConsistentMirrorsWhenGitHubDiscoveryFails(t *testing.T) {
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("mirror-only signed firmware package bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	r2URL := "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/" + profile.AssetName
	cosURL := "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/" + profile.AssetName
	manifest := func() []byte {
		body, marshalErr := json.Marshal(mirrorManifest{Tag: "v9-mirror", Assets: map[string]mirrorManifestAsset{
			profile.AssetName: {Name: profile.AssetName, Size: int64(len(payload)), SHA256: "sha256:" + digest, URL: cosURL, URLs: []string{r2URL, cosURL}},
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	client := &Client{cacheDir: t.TempDir(), apiURL: latestReleaseURL, mirrors: []mirrorSource{{name: "r2", url: "https://r2.test/latest.json"}, {name: "cos", url: "https://cos.test/latest.json"}}}
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		status := http.StatusOK
		headers := make(http.Header)
		switch req.URL.String() {
		case latestReleaseURL:
			status = http.StatusServiceUnavailable
		case "https://r2.test/latest.json":
			body = manifest()
		case "https://cos.test/latest.json":
			body = manifest()
		case r2URL, cosURL:
			if req.Method == http.MethodGet {
				body = payload
			}
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		if len(body) > 0 {
			headers.Set("Content-Length", itoa(int64(len(body))))
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}
	var events []logging.Event
	result, err := client.DownloadLatest(context.Background(), profile.ID, func(event logging.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseTag != "v9-mirror" || result.GitHubDigest != "sha256:"+digest {
		t.Fatalf("unexpected mirror fallback result: %#v", result)
	}
	if got, readErr := os.ReadFile(result.Path); readErr != nil || string(got) != string(payload) {
		t.Fatalf("mirror fallback payload=%q err=%v", got, readErr)
	}
	codes := map[string]bool{}
	for _, event := range events {
		codes[event.Code] = true
	}
	for _, want := range []string{"RELEASE_GITHUB_DISCOVERY_FAILED", "RELEASE_MIRROR_DISCOVERY_SELECTED", "MIRROR_SELECTED", "RELEASE_DOWNLOAD_COMPLETED"} {
		if !codes[want] {
			t.Fatalf("missing fallback evidence %s: %#v", want, events)
		}
	}
}

func TestMirrorReleaseFallbackRequiresTwoMatchingManifests(t *testing.T) {
	profile, err := Profile("echoear-2st")
	if err != nil {
		t.Fatal(err)
	}
	manifest := func(tag, sha string) []byte {
		body, marshalErr := json.Marshal(mirrorManifest{Tag: tag, Assets: map[string]mirrorManifestAsset{
			profile.AssetName: {Name: profile.AssetName, Size: 42, SHA256: sha, URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/" + profile.AssetName}},
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.String() {
		case "https://r2.test/latest.json":
			body = manifest("v1", strings.Repeat("a", 64))
		case "https://cos.test/latest.json":
			body = manifest("v2", strings.Repeat("a", 64))
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}, mirrors: []mirrorSource{{name: "r2", url: "https://r2.test/latest.json"}, {name: "cos", url: "https://cos.test/latest.json"}}}
	if _, err := client.discoverReleaseFromMirrors(context.Background(), profile.AssetName, StableChannel, nil); err == nil {
		t.Fatal("conflicting mirror release manifests were accepted")
	}
}

func TestDiscoverDownloadCandidatesPrefersFastestWorkflowMirror(t *testing.T) {
	profile, err := Profile("bread-compact")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed firmware package bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	githubURL := "https://github.com/RapidAI/MaClaw/releases/download/v1/" + profile.AssetName
	r2URL := "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/" + profile.AssetName
	cosURL := "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/" + profile.AssetName
	manifest := func() []byte {
		body, marshalErr := json.Marshal(mirrorManifest{Tag: "v1", Assets: map[string]mirrorManifestAsset{
			profile.AssetName: {Name: profile.AssetName, Size: int64(len(payload)), SHA256: "sha256:" + digest, URL: cosURL, URLs: []string{r2URL, cosURL}},
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.String() {
		case "https://r2.test/latest.json":
			body = manifest()
		case "https://cos.test/latest.json":
			body = manifest()
		case githubURL:
			time.Sleep(25 * time.Millisecond)
		case r2URL:
		case cosURL:
			time.Sleep(10 * time.Millisecond)
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		header := make(http.Header)
		if len(body) > 0 {
			header.Set("Content-Length", itoa(int64(len(body))))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}, mirrors: []mirrorSource{{name: "r2", url: "https://r2.test/latest.json"}, {name: "cos", url: "https://cos.test/latest.json"}}}
	asset := &releaseAsset{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: githubURL, Digest: "sha256:" + digest}
	candidates := client.discoverDownloadCandidates(context.Background(), profile.AssetName, "v1", asset, nil)
	if len(candidates) != 3 || candidates[0].source != "r2" || candidates[0].url != r2URL {
		t.Fatalf("unexpected candidate order: %#v", candidates)
	}
}

func TestDiscoverDownloadCandidatesRejectsConflictingMirrorMetadata(t *testing.T) {
	profile, err := Profile("echoear-2st")
	if err != nil {
		t.Fatal(err)
	}
	githubURL := "https://github.com/RapidAI/MaClaw/releases/download/v1/" + profile.AssetName
	manifest := func(tag, sha string) []byte {
		body, marshalErr := json.Marshal(mirrorManifest{Tag: tag, Assets: map[string]mirrorManifestAsset{
			profile.AssetName: {Name: profile.AssetName, Size: 42, SHA256: sha, URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/" + profile.AssetName}},
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.String() {
		case "https://r2.test/latest.json":
			body = manifest("v1", strings.Repeat("a", 64))
		case "https://cos.test/latest.json":
			body = manifest("v1", strings.Repeat("b", 64))
		case githubURL:
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}, mirrors: []mirrorSource{{name: "r2", url: "https://r2.test/latest.json"}, {name: "cos", url: "https://cos.test/latest.json"}}}
	asset := &releaseAsset{Name: profile.AssetName, Size: 42, DownloadURL: githubURL, Digest: "sha256:" + strings.Repeat("a", 64)}
	candidates := client.discoverDownloadCandidates(context.Background(), profile.AssetName, "v1", asset, nil)
	if len(candidates) != 1 || candidates[0].source != "github" {
		t.Fatalf("conflicting mirrors were accepted: %#v", candidates)
	}
}

func TestDownloadLatestFallsBackWhenFastestMirrorTransferFails(t *testing.T) {
	profile, err := Profile("fangtang-4g")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed firmware package bytes")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	githubURL := "https://github.com/RapidAI/MaClaw/releases/download/v1/" + profile.AssetName
	r2URL := "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/" + profile.AssetName
	cosURL := "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/" + profile.AssetName
	manifest := func() []byte {
		body, marshalErr := json.Marshal(mirrorManifest{Tag: "v1", Assets: map[string]mirrorManifestAsset{
			profile.AssetName: {Name: profile.AssetName, Size: int64(len(payload)), SHA256: digest, URL: cosURL, URLs: []string{r2URL, cosURL}},
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	client := &Client{cacheDir: t.TempDir(), apiURL: latestReleaseURL, mirrors: []mirrorSource{{name: "r2", url: "https://r2.test/latest.json"}, {name: "cos", url: "https://cos.test/latest.json"}}}
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		status := http.StatusOK
		headers := make(http.Header)
		switch req.URL.String() {
		case latestReleaseURL:
			body, _ = json.Marshal(releaseResponse{TagName: "v1", Assets: []releaseAsset{{Name: profile.AssetName, Size: int64(len(payload)), DownloadURL: githubURL, Digest: "sha256:" + digest}}})
		case "https://r2.test/latest.json":
			body = manifest()
		case "https://cos.test/latest.json":
			body = manifest()
		case r2URL:
			if req.Method == http.MethodGet {
				status = http.StatusServiceUnavailable
			}
		case cosURL:
			if req.Method == http.MethodGet {
				body = payload
			} else {
				time.Sleep(10 * time.Millisecond)
			}
		case githubURL:
			if req.Method == http.MethodGet {
				body = payload
			} else {
				time.Sleep(20 * time.Millisecond)
			}
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		if len(body) > 0 {
			headers.Set("Content-Length", itoa(int64(len(body))))
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
	})}
	var events []logging.Event
	result, err := client.DownloadLatest(context.Background(), profile.ID, func(event logging.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := os.ReadFile(result.Path)
	if readErr != nil || string(got) != string(payload) {
		t.Fatalf("fallback package=%q err=%v", got, readErr)
	}
	for _, event := range events {
		if event.Code == "MIRROR_FALLBACK" && event.Fields["source"] == "cos" && event.Fields["failedSource"] == "r2" {
			return
		}
	}
	t.Fatalf("missing COS fallback event: %#v", events)
}

func TestApprovedDownloadURLRejectsUntrustedMirror(t *testing.T) {
	for _, raw := range []string{
		"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/firmware.clawfw",
		"https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/firmware.clawfw",
	} {
		if !isApprovedDownloadURL(raw) {
			t.Fatalf("approved mirror rejected: %s", raw)
		}
	}
	for _, raw := range []string{
		"http://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/firmware.clawfw",
		"https://evil.example/latest/firmware.clawfw",
		"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev.evil.example/latest/firmware.clawfw",
		"https://user@example.com/latest/firmware.clawfw",
	} {
		if isApprovedDownloadURL(raw) {
			t.Fatalf("untrusted mirror accepted: %s", raw)
		}
	}
}

func TestDownloadResumesTrustedPartialFile(t *testing.T) {
	data := []byte("signed firmware package bytes")
	var receivedRange string
	var receivedIfRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRange = r.Header.Get("Range")
		receivedIfRange = r.Header.Get("If-Range")
		if receivedRange != "bytes=7-" {
			t.Fatalf("Range = %q", receivedRange)
		}
		if receivedIfRange != `"firmware-v1"` {
			t.Fatalf("If-Range = %q", receivedIfRange)
		}
		w.Header().Set("ETag", `"firmware-v1"`)
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
	if err := writePartialIdentity(destination+".part", partialDownloadIdentity{URL: server.URL, ETag: `"firmware-v1"`}); err != nil {
		t.Fatal(err)
	}
	c := &Client{http: server.Client()}
	sum, size, err := c.download(context.Background(), server.URL, destination, int64(len(data)))
	if err != nil || size != int64(len(data)) || receivedRange != "bytes=7-" || receivedIfRange != `"firmware-v1"` {
		t.Fatalf("sum=%q size=%d range=%q if-range=%q err=%v", sum, size, receivedRange, receivedIfRange, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("download=%q err=%v", got, err)
	}
	if _, err := os.Stat(destination + ".part"); !os.IsNotExist(err) {
		t.Fatal("partial file was not promoted")
	}
	if _, err := os.Stat(destination + ".part.meta"); !os.IsNotExist(err) {
		t.Fatal("partial metadata was not removed after promotion")
	}
}

func TestDownloadRestartsWhenPartialIdentityIsMissing(t *testing.T) {
	data := []byte("complete replacement package")
	var receivedRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRange = r.Header.Get("Range")
		w.Header().Set("Content-Length", itoa(int64(len(data))))
		w.Header().Set("ETag", `"firmware-v2"`)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "firmware.clawfw")
	if err := os.WriteFile(destination+".part", []byte("unbound partial"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{http: server.Client()}
	_, size, err := c.download(context.Background(), server.URL, destination, int64(len(data)))
	if err != nil || size != int64(len(data)) || receivedRange != "" {
		t.Fatalf("size=%d range=%q err=%v", size, receivedRange, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("download=%q err=%v", got, err)
	}
}

func TestDownloadRestartsWhenPartialIdentityChanged(t *testing.T) {
	data := []byte("changed release object")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got == "" {
			w.Header().Set("ETag", `"firmware-v2"`)
			w.Header().Set("Content-Length", itoa(int64(len(data))))
			_, _ = w.Write(data)
			return
		} else if got != "bytes=7-" {
			t.Fatalf("Range = %q", got)
		}
		w.Header().Set("ETag", `"firmware-v2"`)
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
	if err := writePartialIdentity(destination+".part", partialDownloadIdentity{URL: server.URL, ETag: `"firmware-v1"`}); err != nil {
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

func TestCompareFirmwareVersions(t *testing.T) {
	for _, test := range []struct {
		name                 string
		installed, available int64
		want                 string
	}{
		{name: "upgrade available", installed: 12, available: 13, want: "upgrade_available"},
		{name: "up to date", installed: 13, available: 13, want: "up_to_date"},
		{name: "installed newer", installed: 14, available: 13, want: "installed_newer"},
		{name: "unknown without trusted version", installed: 0, available: 13, want: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := CompareFirmwareVersions(test.installed, test.available)
			if got.Status != test.want || got.InstalledVersion != test.installed || got.AvailableVersion != test.available {
				t.Fatalf("CompareFirmwareVersions(%d, %d) = %+v, want status %q", test.installed, test.available, got, test.want)
			}
		})
	}
}
