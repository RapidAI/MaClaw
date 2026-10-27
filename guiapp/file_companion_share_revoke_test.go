package guiapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func resetFileCompanionShareProbeCache() {
	fileCompanionShareProbe.mu.Lock()
	fileCompanionShareProbe.checked = map[string]time.Time{}
	fileCompanionShareProbe.mu.Unlock()
}

// seedReceivedShareCopy writes the downloaded copy and its transcript exactly
// where a real import puts them.
func seedReceivedShareCopy(t *testing.T, app *App, token string) (downloaded, sessionFile string) {
	t.Helper()
	dir := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	downloaded = filepath.Join(dir, "shared.pdf")
	if err := os.WriteFile(downloaded, []byte("%PDF-shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(downloaded)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	sessions := filepath.Join(app.GetDataDir(), "file-companion", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile = filepath.Join(sessions, sessionID+".json")
	if err := os.WriteFile(sessionFile, []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return downloaded, sessionFile
}

func newShareRevokeTestApp(t *testing.T, handler http.Handler) *App {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
}

func TestListFileCompanionLibraryDropsADeletedReceivedShare(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_66666666666666666666666666666666"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mobile/documents/shares" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[],"server_time":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
			return
		}
		if r.URL.Path == "/api/mobile/documents/shares/"+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"SHARE_NOT_FOUND","message":"分享链接不存在"}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: downloaded,
		Source: "cloud", Mine: false, Visibility: "public", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if item.Token == token {
			t.Fatalf("the revoked share is still listed: %+v", item)
		}
	}
	if len(listed.Removed) != 1 || !strings.EqualFold(listed.Removed[0], downloaded) {
		t.Fatalf("removed = %v", listed.Removed)
	}
	if _, err := os.Stat(filepath.Dir(downloaded)); !os.IsNotExist(err) {
		t.Fatalf("the downloaded copy is still present: %v", err)
	}
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Fatalf("the revoked transcript is still present: %v", err)
	}
	fileCompanionLibraryMu.Lock()
	dismissed := app.readDismissedShareTokensLocked()[token]
	fileCompanionLibraryMu.Unlock()
	if !dismissed {
		t.Fatal("the revoked token was not dismissed; a stale list could re-import it")
	}
}

func TestListFileCompanionLibraryKeepsAPathStillUsedByALocalRow(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[]}`))
			return
		}
		if r.URL.Path == "/api/mobile/documents/shares/"+token {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"SHARE_NOT_FOUND","message":"分享链接不存在"}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: downloaded,
		Source: "cloud", Mine: false, Visibility: "public", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	// The reader also opened that downloaded file as a local document. The
	// revoked share must not take the local row's file or its reader data.
	fileCompanionLibraryMu.Lock()
	items, _ := app.readFileCompanionLibraryLocked()
	items = append(items, FileCompanionLibraryItem{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
	})
	if err := app.writeFileCompanionLibraryLocked(items); err != nil {
		fileCompanionLibraryMu.Unlock()
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Unlock()
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	foundLocal := false
	for _, item := range listed.Items {
		if item.Token == token {
			t.Fatalf("the revoked cloud row is still listed: %+v", item)
		}
		if item.Source == "local" && strings.EqualFold(item.Path, downloaded) {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("the local row was dropped with the share: %+v", listed.Items)
	}
	if len(listed.Removed) != 0 {
		t.Fatalf("the shared path was closed while a local row still uses it: %v", listed.Removed)
	}
	if _, err := os.Stat(downloaded); err != nil {
		t.Fatalf("the local row's file was removed: %v", err)
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("the local row's transcript was purged: %v", err)
	}
}

func TestListFileCompanionLibraryKeepsAnExpiredReceivedShare(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_77777777777777777777777777777777"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[],"server_time":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
			return
		}
		if r.URL.Path == "/api/mobile/documents/shares/"+token {
			// An expired link still exists; the single-share endpoint does not
			// reject it, which is how the desktop tells expiry from deletion.
			_, _ = w.Write([]byte(`{"token":"` + token + `","name":"shared.pdf","owner":"o","permanent":false,"revision":1}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: downloaded,
		Source: "cloud", Mine: false, Visibility: "public", Revision: 1,
		ExpiresAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed.Items {
		if item.Token == token {
			found = true
		}
	}
	if !found || len(listed.Removed) != 0 {
		t.Fatalf("expired share was withdrawn: items=%+v removed=%v", listed.Items, listed.Removed)
	}
	if _, err := os.Stat(downloaded); err != nil {
		t.Fatalf("the expired copy was removed: %v", err)
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("the expired transcript was removed: %v", err)
	}
}

func TestListFileCompanionLibraryDropsAHostRowWithoutPurgingTheSource(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_88888888888888888888888888888888"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[],"server_time":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
			return
		}
		if r.URL.Path == "/api/mobile/documents/shares/"+token {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"SHARE_NOT_FOUND","message":"分享链接不存在"}`))
			return
		}
		http.NotFound(w, r)
	}))
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	sessions := filepath.Join(app.GetDataDir(), "file-companion", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(sessions, sessionID+".json")
	if err := os.WriteFile(sessionFile, []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "paper.pdf", Path: source,
		Source: "cloud", Mine: true, Visibility: "public", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if item.Token == token {
			t.Fatalf("the host's revoked row is still listed: %+v", item)
		}
	}
	if len(listed.Removed) != 0 {
		t.Fatalf("the host's own tab was closed: %v", listed.Removed)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("the host's source file was removed: %v", err)
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("the host's own transcript was purged: %v", err)
	}
}

func TestListFileCompanionLibraryKeepsARowWhenTheProbeIsUnsupported(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_99999999999999999999999999999999"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[]}`))
			return
		}
		// An older Hub has no single-link route. A plain 404 must not be read
		// as a deletion, or every received copy would be erased.
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: downloaded,
		Source: "cloud", Mine: false, Visibility: "public", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed.Items {
		if item.Token == token {
			found = true
		}
	}
	if !found || len(listed.Removed) != 0 {
		t.Fatalf("an unsupported probe withdrew the share: items=%+v removed=%v", listed.Items, listed.Removed)
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("the transcript was purged on an unsupported probe: %v", err)
	}
}
