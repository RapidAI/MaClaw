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

func TestFileCompanionShareHostNameNamesThePersonToAsk(t *testing.T) {
	cases := []struct {
		name string
		item FileCompanionLibraryItem
		want string
	}{
		{"participant asks the current host", FileCompanionLibraryItem{HostLabel: "王老师", Owner: "管理员"}, "王老师"},
		{"legacy row falls back to the owner", FileCompanionLibraryItem{Owner: "管理员"}, "管理员"},
		{"host seat holder asks the admin", FileCompanionLibraryItem{HostKnown: true, Host: true, HostLabel: "me@example.com", Owner: "管理员"}, "管理员"},
		{"no label", FileCompanionLibraryItem{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileCompanionShareHostName(tc.item); got != tc.want {
				t.Fatalf("host name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOnlyRemovedFileCompanionHostsDropsPathsThatStayOpen(t *testing.T) {
	closed := `C:\library\closed.pdf`
	kept := `C:\library\kept.pdf`
	hosts := onlyRemovedFileCompanionHosts([]string{closed}, map[string]string{
		closed:                 "王老师",
		kept:                   "李老师",
		`C:\library\other.pdf`: "赵老师",
	})
	if len(hosts) != 1 || hosts[closed] != "王老师" {
		t.Fatalf("hosts = %v", hosts)
	}
	// A blank label is not a name; the window has its own generic notice.
	if got := onlyRemovedFileCompanionHosts([]string{closed}, map[string]string{closed: "  "}); got != nil {
		t.Fatalf("blank label kept: %v", got)
	}
	if got := onlyRemovedFileCompanionHosts(nil, map[string]string{closed: "王老师"}); got != nil {
		t.Fatalf("no closed path kept a label: %v", got)
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
		HostLabel: "王老师",
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
	if label := listed.RemovedHosts[listed.Removed[0]]; label != "王老师" {
		t.Fatalf("removed host = %q, want 王老师", label)
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

func TestListFileCompanionLibraryDropsAGroupShareTheHubNoLongerLists(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[],"server_time":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: downloaded,
		Source: "cloud", Mine: false, Visibility: "group", Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	items, _ := app.readFileCompanionLibraryLocked()
	items = append(items, FileCompanionLibraryItem{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
	})
	err := app.writeFileCompanionLibraryLocked(items)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if item.Token == token || (item.Source == "local" && strings.EqualFold(item.Path, downloaded)) {
			t.Fatalf("the deleted group share is still listed: %+v", item)
		}
	}
	if len(listed.Removed) != 1 || !strings.EqualFold(listed.Removed[0], downloaded) {
		t.Fatalf("removed = %v", listed.Removed)
	}
	if _, err := os.Stat(filepath.Dir(downloaded)); !os.IsNotExist(err) {
		t.Fatalf("the downloaded group copy is still present: %v", err)
	}
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Fatalf("the revoked group transcript is still present: %v", err)
	}
	fileCompanionLibraryMu.Lock()
	dismissed := app.readDismissedShareTokensLocked()[token]
	fileCompanionLibraryMu.Unlock()
	if dismissed {
		t.Fatal("a group token must stay available for a future re-invite")
	}
}

func TestListFileCompanionLibraryClosesAnOpenReceivedCopyWhenTheHostDeletesIt(t *testing.T) {
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
	// Opening the received copy writes a local row on the same file. That row
	// is the participant's open tab. The host deleting the share closes it
	// and removes the downloaded copy.
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
	for _, item := range listed.Items {
		if item.Token == token || (item.Source == "local" && strings.EqualFold(item.Path, downloaded)) {
			t.Fatalf("the open share stayed in the library: %+v", item)
		}
	}
	if len(listed.Removed) != 1 || !strings.EqualFold(listed.Removed[0], downloaded) {
		t.Fatalf("removed = %v, want the open copy", listed.Removed)
	}
	if _, err := os.Stat(downloaded); !os.IsNotExist(err) {
		t.Fatalf("the downloaded copy is still present: %v", err)
	}
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Fatalf("the revoked transcript is still present: %v", err)
	}
}

func TestListFileCompanionLibraryClosesADismissedOpenCopyOnTheNextList(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc"
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
	fileCompanionLibraryMu.Lock()
	if _, err := app.dismissFileCompanionShareLocked(token); err != nil {
		fileCompanionLibraryMu.Unlock()
		t.Fatal(err)
	}
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if strings.EqualFold(item.Path, downloaded) {
			t.Fatalf("the open copy stayed listed: %+v", item)
		}
	}
	if len(listed.Removed) != 1 || !strings.EqualFold(listed.Removed[0], downloaded) {
		t.Fatalf("removed = %v, want the open copy", listed.Removed)
	}
	if _, err := os.Stat(downloaded); !os.IsNotExist(err) {
		t.Fatalf("the downloaded copy is still present: %v", err)
	}
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Fatalf("the revoked transcript is still present: %v", err)
	}
}

func TestListFileCompanionLibraryClosesAGroupCopyLeftWithoutACloudRow(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_dededededededededededededededede"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	// A previous import in this process moves the counter. The leftover copy
	// was not part of this refresh's cloud snapshot, so that must not cancel
	// the close.
	app.bumpFileCompanionShareEpoch(token)
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if strings.EqualFold(item.Path, downloaded) {
			t.Fatalf("the open group copy stayed listed: %+v", item)
		}
	}
	if len(listed.Removed) != 1 || !strings.EqualFold(listed.Removed[0], downloaded) {
		t.Fatalf("removed = %v, want the open copy", listed.Removed)
	}
	if _, err := os.Stat(downloaded); !os.IsNotExist(err) {
		t.Fatalf("the downloaded group copy is still present: %v", err)
	}
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Fatalf("the group transcript is still present: %v", err)
	}
	fileCompanionLibraryMu.Lock()
	dismissed := app.readDismissedShareTokensLocked()[token]
	fileCompanionLibraryMu.Unlock()
	if dismissed {
		t.Fatal("a group token must stay available for a future re-invite")
	}
}

func TestListFileCompanionLibraryKeepsADismissedCopyWhenTheProbeIsUnsupported(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	app := newShareRevokeTestApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(`{"notes_reply":true,"mine":[],"received":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	downloaded, sessionFile := seedReceivedShareCopy(t, app, token)
	fileCompanionLibraryMu.Lock()
	if _, err := app.dismissFileCompanionShareLocked(token); err != nil {
		fileCompanionLibraryMu.Unlock()
		t.Fatal(err)
	}
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Removed) != 0 {
		t.Fatalf("an unsupported probe closed the tab: %v", listed.Removed)
	}
	foundLocal := false
	for _, item := range listed.Items {
		if item.Source == "local" && strings.EqualFold(item.Path, downloaded) {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("the open copy left the library: %+v", listed.Items)
	}
	if _, err := os.Stat(downloaded); err != nil {
		t.Fatalf("the downloaded copy was removed: %v", err)
	}
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("the transcript was purged: %v", err)
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
	fileCompanionLibraryMu.Lock()
	items, _ := app.readFileCompanionLibraryLocked()
	items = append(items, FileCompanionLibraryItem{
		ID: "local:paper", Name: "paper.pdf", Path: source, Source: "local",
	})
	err = app.writeFileCompanionLibraryLocked(items)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	foundLocal := false
	for _, item := range listed.Items {
		if item.Token == token {
			t.Fatalf("the host's revoked row is still listed: %+v", item)
		}
		if item.Source == "local" && strings.EqualFold(item.Path, source) {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("the host's own file left the library: %+v", listed.Items)
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

func shareListHandler(token string, mine bool) http.HandlerFunc {
	body := `{"notes_reply":true,"mine":[],"received":[{"token":"` + token + `","name":"shared.pdf","visibility":"public","permanent":true,"revision":1,"owner":"host@example.com","host":false,"created_at":"2026-10-25T00:00:00Z"}]}`
	if mine {
		body = `{"notes_reply":true,"mine":[{"token":"` + token + `","name":"paper.pdf","visibility":"public","permanent":true,"revision":1,"owner":"me@example.com","host":true,"created_at":"2026-10-25T00:00:00Z"}],"received":[]}`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}
}

func listedSharePath(t *testing.T, items []FileCompanionLibraryItem, token string) string {
	t.Helper()
	for _, item := range items {
		if item.Source == "cloud" && item.Token == token {
			return item.Path
		}
	}
	t.Fatalf("share %s was not listed: %+v", token, items)
	return ""
}

func TestListFileCompanionLibraryReattachesAnInstalledShareAfterTheCloudRowIsGone(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_12121212121212121212121212121212"
	app := newShareRevokeTestApp(t, shareListHandler(token, false))
	downloaded, _ := seedReceivedShareCopy(t, app, token)
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
		OpenedAt: "2026-10-30T00:00:00Z",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	got := listedSharePath(t, listed.Items, token)
	if fileCompanionDedupeKey(got) != fileCompanionDedupeKey(downloaded) {
		t.Fatalf("path = %s, want %s", got, downloaded)
	}
}

func TestListFileCompanionLibraryRestoresAProbeDismissedShareThatIsStillLive(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_34343434343434343434343434343434"
	app := newShareRevokeTestApp(t, shareListHandler(token, false))
	downloaded, _ := seedReceivedShareCopy(t, app, token)
	fileCompanionLibraryMu.Lock()
	if _, err := app.dismissFileCompanionShareLocked(token); err != nil {
		fileCompanionLibraryMu.Unlock()
		t.Fatal(err)
	}
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:shared", Name: "shared.pdf", Path: downloaded, Source: "local",
		OpenedAt: "2026-10-30T00:00:00Z",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	got := listedSharePath(t, listed.Items, token)
	if fileCompanionDedupeKey(got) != fileCompanionDedupeKey(downloaded) {
		t.Fatalf("path = %s, want %s", got, downloaded)
	}
	fileCompanionLibraryMu.Lock()
	dismissed := app.readDismissedShareTokensLocked()[token]
	fileCompanionLibraryMu.Unlock()
	if dismissed {
		t.Fatal("a live share stayed dismissed after the hub listed it again")
	}
}

func TestListFileCompanionLibraryReattachesARememberedHostFile(t *testing.T) {
	resetFileCompanionShareProbeCache()
	const token = "cd_56565656565656565656565656565656"
	app := newShareRevokeTestApp(t, shareListHandler(token, true))
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	app.rememberFileCompanionSharePathLocked(token, source)
	if _, err := app.dismissFileCompanionShareLocked(token); err != nil {
		fileCompanionLibraryMu.Unlock()
		t.Fatal(err)
	}
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: "local:paper", Name: "paper.pdf", Path: source, Source: "local",
		OpenedAt: "2026-10-30T00:00:00Z",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	got := listedSharePath(t, listed.Items, token)
	if fileCompanionDedupeKey(got) != fileCompanionDedupeKey(source) {
		t.Fatalf("path = %s, want %s", got, source)
	}
}
