package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGroupDiscussionAttachmentDownloadURLAddsSessionAndParticipant(t *testing.T) {
	got, attachmentID, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "/api/ve/files/file-1?x=1", "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("groupDiscussionAttachmentDownloadURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if u.String() != "https://hub.example/api/ve/files/download/file-1?participant_id=machine-1&session_id=disc-1" {
		t.Fatalf("download url = %q", u.String())
	}
	if attachmentID != "file-1" {
		t.Fatalf("attachment id = %q", attachmentID)
	}
}

func TestGroupDiscussionAttachmentDownloadURLRejectsExternalOrigin(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://evil.example/api/ve/files/file-1", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected external origin to be rejected")
	}
}

func TestGroupDiscussionAttachmentDownloadURLDropsUntrustedQueryParams(t *testing.T) {
	got, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/ve/files/file-1?redirect=https://evil.example&token=secret", "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("groupDiscussionAttachmentDownloadURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if u.Query().Get("redirect") != "" || u.Query().Get("token") != "" {
		t.Fatalf("untrusted query params preserved: %q", u.RawQuery)
	}
}

func TestGroupDiscussionAttachmentDownloadURLForcesCurrentDiscussion(t *testing.T) {
	got, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/ve/files/file-1?session_id=other&participant_id=other-machine", "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("groupDiscussionAttachmentDownloadURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if got := u.Query().Get("session_id"); got != "disc-1" {
		t.Fatalf("session_id = %q", got)
	}
	if got := u.Query().Get("participant_id"); got != "machine-1" {
		t.Fatalf("participant_id = %q", got)
	}
}

func TestGroupDiscussionAttachmentDownloadURLRejectsNonFileRelayPath(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/admin/export", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected non-file-relay path to be rejected")
	}
}
func TestGroupDiscussionAttachmentDownloadURLRejectsUploadEndpoint(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/ve/files/upload", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected upload endpoint to be rejected as an attachment download")
	}
}
func TestGroupDiscussionAttachmentDownloadURLRejectsAmbiguousFileID(t *testing.T) {
	cases := []string{
		"https://hub.example/api/ve/files/download/file-1/extra",
		"https://hub.example/api/ve/files/file%2F1",
	}
	for _, rawURL := range cases {
		_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", rawURL, "disc-1", "machine-1")
		if err == nil {
			t.Fatalf("expected ambiguous file url %q to be rejected", rawURL)
		}
	}
}

func TestSafeGroupDiscussionFilenameHandlesBothPathSeparators(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: `..\report?.txt`, want: "report_.txt"},
		{name: "../report?.txt", want: "report_.txt"},
		{name: `C:\tmp\report.pdf`, want: "report.pdf"},
		{name: "/tmp/report.pdf", want: "report.pdf"},
	} {
		if got := safeGroupDiscussionFilename(tc.name); got != tc.want {
			t.Fatalf("safeGroupDiscussionFilename(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGroupDiscussionDownloadAttachmentSavesUnderDiscussionDir(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		if r.URL.Path != "/api/ve/files/download/file-42" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("session_id"); got != "disc/one" {
			t.Fatalf("session_id = %q", got)
		}
		if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
			t.Fatalf("participant_id = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte("attachment body"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}

	result, err := app.GroupDiscussionDownloadAttachment("disc/one", "/api/ve/files/file-42", "../report?.txt")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if result.AttachmentID != "file-42" || result.Filename != "report_.txt" || result.SizeBytes != int64(len("attachment body")) {
		t.Fatalf("result = %+v", result)
	}
	wantDir := filepath.Join(app.GetDataDir(), "group-discussions", "disc_one", "attachments")
	if filepath.Dir(result.LocalPath) != wantDir {
		t.Fatalf("local dir = %q, want %q", filepath.Dir(result.LocalPath), wantDir)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "attachment body" {
		t.Fatalf("downloaded body = %q", string(data))
	}

	second, err := app.GroupDiscussionDownloadAttachment("disc/one", "/api/ve/files/file-42", "../report?.txt")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment cached: %v", err)
	}
	if second.LocalPath != result.LocalPath || second.SizeBytes != result.SizeBytes {
		t.Fatalf("cached result = %+v, want local path %q size %d", second, result.LocalPath, result.SizeBytes)
	}
	if hitCount != 1 {
		t.Fatalf("expected cached second download to avoid HTTP request, hits=%d", hitCount)
	}
}

func TestGroupDiscussionAttachmentPreviewDataURL(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := app.groupDiscussionAttachmentRoot("disc-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "photo.png")
	img := image.NewRGBA(image.Rect(0, 0, 320, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 80, A: 255})
		}
	}
	out, err := os.Create(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := app.GroupDiscussionAttachmentPreviewDataURL("disc-preview", localPath)
	if err != nil {
		t.Fatalf("GroupDiscussionAttachmentPreviewDataURL: %v", err)
	}
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatalf("preview data URL = %q", got)
	}
	jpegData, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatalf("decode thumbnail base64: %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("decode thumbnail image: %v", err)
	}
	if decoded.Bounds().Dx() != veImageAttachmentThumbnailMaxSide || decoded.Bounds().Dy() >= 120 {
		t.Fatalf("thumbnail size = %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestGroupDiscussionAttachmentPreviewDataURLRejectsOutsidePath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	outsidePath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(outsidePath, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GroupDiscussionAttachmentPreviewDataURL("disc-preview", outsidePath); err == nil {
		t.Fatal("expected preview outside discussion attachment directory to be rejected")
	}
}

func TestGroupDiscussionAttachmentPreviewDataURLRejectsSymlinkEscape(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := app.groupDiscussionAttachmentRoot("disc-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "photo.png")
	if err := os.WriteFile(outsidePath, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "linked-outside")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := app.GroupDiscussionAttachmentPreviewDataURL("disc-preview", filepath.Join(linkDir, "photo.png")); err == nil {
		t.Fatal("expected preview symlink escape to be rejected")
	}
}

func TestGroupDiscussionAttachmentPreviewDataURLRejectsNonImage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := app.groupDiscussionAttachmentRoot("disc-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(localPath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GroupDiscussionAttachmentPreviewDataURL("disc-preview", localPath); err == nil {
		t.Fatal("expected non-image preview to be rejected")
	}
}

func TestGroupDiscussionAttachmentPreviewDataURLRejectsInvalidImageBytes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := app.groupDiscussionAttachmentRoot("disc-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "not-image.png")
	if err := os.WriteFile(localPath, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GroupDiscussionAttachmentPreviewDataURL("disc-preview", localPath); err == nil {
		t.Fatal("expected invalid image bytes to be rejected")
	}
}

func TestGroupDiscussionDownloadAttachmentRejectsOversizedRemoteFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/download/file-big" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = io.CopyN(w, zeroReader{}, veFileAttachmentMaxSize+1)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}
	app.configCacheValid = true

	if _, err := app.GroupDiscussionDownloadAttachment("disc-big", "/api/ve/files/file-big", "big.bin"); err == nil {
		t.Fatal("expected oversized remote attachment to be rejected")
	}
	entries, err := os.ReadDir(app.groupDiscussionAttachmentRoot("disc-big"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("oversized partial file was not removed: %v", entries)
	}
}

func TestGroupDiscussionDownloadAttachmentIgnoresOversizedCachedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/download/file-big-cache" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("replacement"))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}
	app.configCacheValid = true
	dir := app.groupDiscussionAttachmentRoot("disc-big-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachedPath := filepath.Join(dir, "cached.bin")
	if err := writeZeroFile(cachedPath, veFileAttachmentMaxSize+1); err != nil {
		t.Fatalf("writeZeroFile: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(context.Background(), GroupDiscussionAttachmentRecord{AttachmentID: "file-big-cache", DiscussionID: "disc-big-cache", Filename: "cached.bin", HubURL: "/api/ve/files/file-big-cache", LocalPath: cachedPath, SizeBytes: veFileAttachmentMaxSize + 1, DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment: %v", err)
	}
	_ = store.Close()

	result, err := app.GroupDiscussionDownloadAttachment("disc-big-cache", "/api/ve/files/file-big-cache", "fresh.bin")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if result.LocalPath == cachedPath {
		t.Fatalf("oversized cached path reused: %q", result.LocalPath)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("downloaded data = %q err=%v", string(data), err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestGroupDiscussionDownloadAttachmentIgnoresCachedPathOutsideDiscussionDir(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		_, _ = w.Write([]byte("safe attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outsidePath, []byte("poisoned"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(context.Background(), GroupDiscussionAttachmentRecord{AttachmentID: "file-100", DiscussionID: "disc-safe", Filename: "safe.pdf", HubURL: "/api/ve/files/file-100", LocalPath: outsidePath, DownloadState: "downloaded"}); err != nil {
		_ = store.Close()
		t.Fatalf("UpsertDownloadedAttachment: %v", err)
	}
	_ = store.Close()

	result, err := app.GroupDiscussionDownloadAttachment("disc-safe", "/api/ve/files/file-100", "safe.pdf")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if hitCount != 1 {
		t.Fatalf("expected outside cached path to be ignored and refetched, hits=%d", hitCount)
	}
	if result.LocalPath == outsidePath {
		t.Fatalf("outside cached path was reused: %q", result.LocalPath)
	}
	if filepath.Dir(result.LocalPath) != app.groupDiscussionAttachmentRoot("disc-safe") {
		t.Fatalf("local dir = %q, want %q", filepath.Dir(result.LocalPath), app.groupDiscussionAttachmentRoot("disc-safe"))
	}
}

func TestGroupDiscussionDownloadAttachmentIgnoresCachedSymlinkEscape(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		_, _ = w.Write([]byte("fresh symlink-safe attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	dir := app.groupDiscussionAttachmentRoot("disc-symlink-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "cached.pdf")
	if err := os.WriteFile(outsidePath, []byte("poisoned"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "linked-outside")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cachedPath := filepath.Join(linkDir, "cached.pdf")
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(context.Background(), GroupDiscussionAttachmentRecord{AttachmentID: "file-symlink-cache", DiscussionID: "disc-symlink-cache", Filename: "safe.pdf", HubURL: "/api/ve/files/file-symlink-cache", LocalPath: cachedPath, DownloadState: "downloaded"}); err != nil {
		_ = store.Close()
		t.Fatalf("UpsertDownloadedAttachment: %v", err)
	}
	_ = store.Close()

	result, err := app.GroupDiscussionDownloadAttachment("disc-symlink-cache", "/api/ve/files/file-symlink-cache", "safe.pdf")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if hitCount != 1 {
		t.Fatalf("expected symlink cached path to be ignored and refetched, hits=%d", hitCount)
	}
	if result.LocalPath == cachedPath {
		t.Fatalf("symlink cached path was reused: %q", result.LocalPath)
	}
	data, err := os.ReadFile(outsidePath)
	if err != nil || string(data) != "poisoned" {
		t.Fatalf("outside data = %q err=%v", string(data), err)
	}
}

func TestGroupDiscussionDownloadAttachmentRejectsLocalSymlinkTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should not overwrite outside"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	dir := app.groupDiscussionAttachmentRoot("disc-local-symlink")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outsidePath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "file-local-symlink-safe.pdf")
	if err := os.Symlink(outsidePath, localPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := app.GroupDiscussionDownloadAttachment("disc-local-symlink", "/api/ve/files/file-local-symlink", "safe.pdf"); err == nil {
		t.Fatal("expected local symlink target to be rejected")
	}
	data, err := os.ReadFile(outsidePath)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("outside data = %q err=%v", string(data), err)
	}
}

func TestGroupDiscussionDownloadAttachmentAvoidsExistingRegularTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fresh body"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	dir := app.groupDiscussionAttachmentRoot("disc-existing-target")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "file-existing-safe.pdf")
	if err := os.WriteFile(localPath, []byte("stale body"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := app.GroupDiscussionDownloadAttachment("disc-existing-target", "/api/ve/files/file-existing", "safe.pdf")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if result.LocalPath == localPath {
		t.Fatalf("existing target path was overwritten: %q", result.LocalPath)
	}
	stale, err := os.ReadFile(localPath)
	if err != nil || string(stale) != "stale body" {
		t.Fatalf("stale target data = %q err=%v", string(stale), err)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil || string(data) != "fresh body" {
		t.Fatalf("new local data = %q err=%v", string(data), err)
	}
}

func TestGroupDiscussionCommitTempAttachmentDoesNotOverwriteRaceWinner(t *testing.T) {
	dir := t.TempDir()
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write([]byte("downloaded body")); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	claimedPath := filepath.Join(dir, "safe.pdf")
	if err := os.WriteFile(claimedPath, []byte("race winner"), 0o644); err != nil {
		t.Fatal(err)
	}

	localPath, localName, err := groupDiscussionCommitTempAttachment(tmpPath, dir, "safe.pdf")
	if err != nil {
		t.Fatalf("groupDiscussionCommitTempAttachment: %v", err)
	}
	if localPath == claimedPath || localName == "safe.pdf" {
		t.Fatalf("race winner target reused: path=%q name=%q", localPath, localName)
	}
	stale, err := os.ReadFile(claimedPath)
	if err != nil || string(stale) != "race winner" {
		t.Fatalf("race winner data = %q err=%v", string(stale), err)
	}
	data, err := os.ReadFile(localPath)
	if err != nil || string(data) != "downloaded body" {
		t.Fatalf("committed data = %q err=%v", string(data), err)
	}
}

func TestCopyTempAttachmentNoOverwriteKeepsExistingFile(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "download.tmp")
	if err := os.WriteFile(tmpPath, []byte("downloaded body"), 0o644); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(dir, "safe.pdf")
	if err := os.WriteFile(localPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyTempAttachmentNoOverwrite(tmpPath, localPath); !os.IsExist(err) {
		t.Fatalf("expected existing target error, got %v", err)
	}
	data, err := os.ReadFile(localPath)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("existing data = %q err=%v", string(data), err)
	}
	newPath := filepath.Join(dir, "safe (1).pdf")
	if err := copyTempAttachmentNoOverwrite(tmpPath, newPath); err != nil {
		t.Fatalf("copyTempAttachmentNoOverwrite: %v", err)
	}
	data, err = os.ReadFile(newPath)
	if err != nil || string(data) != "downloaded body" {
		t.Fatalf("new data = %q err=%v", string(data), err)
	}
}

func TestGroupDiscussionDownloadAttachmentRefetchesWhenCachedFileMissing(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		_, _ = w.Write([]byte("fresh attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	missingPath := filepath.Join(app.GetDataDir(), "group-discussions", "disc-missing", "attachments", "missing.pdf")
	if err := store.UpsertDownloadedAttachment(context.Background(), GroupDiscussionAttachmentRecord{AttachmentID: "file-99", DiscussionID: "disc-missing", Filename: "missing.pdf", HubURL: "/api/ve/files/file-99", LocalPath: missingPath, DownloadState: "downloaded"}); err != nil {
		_ = store.Close()
		t.Fatalf("UpsertDownloadedAttachment: %v", err)
	}
	_ = store.Close()

	result, err := app.GroupDiscussionDownloadAttachment("disc-missing", "/api/ve/files/file-99", "missing.pdf")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if hitCount != 1 {
		t.Fatalf("expected missing cached file to be refetched once, hits=%d", hitCount)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("read refetched attachment: %v", err)
	}
	if string(data) != "fresh attachment" {
		t.Fatalf("refetched body = %q", string(data))
	}
}

func TestGroupDiscussionDownloadAttachmentUsesRemoteClientIDFallback(t *testing.T) {
	var gotParticipantID string
	var gotMachineID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParticipantID = r.URL.Query().Get("participant_id")
		gotMachineID = r.Header.Get("X-Machine-ID")
		_, _ = w.Write([]byte("attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteClientID:     "client-fallback",
			RemoteMachineToken: "token-1",
		},
	}

	if _, err := app.GroupDiscussionDownloadAttachment("disc-1", "/api/ve/files/file-1", "report.txt"); err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if gotParticipantID != "client-fallback" {
		t.Fatalf("participant_id = %q, want client-fallback", gotParticipantID)
	}
	if gotMachineID != "client-fallback" {
		t.Fatalf("X-Machine-ID = %q, want client-fallback", gotMachineID)
	}
}
