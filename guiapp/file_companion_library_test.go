package guiapp

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestFileCompanionLibraryRemembersOpensAndDropsMissing(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	first := filepath.Join(home, "a.md")
	second := filepath.Join(home, "b.md")
	if err := os.WriteFile(first, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(filepath.Join(home, "missing.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(filepath.Join(home, "dir")); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(first)
	if err != nil || opened.Error != "" {
		t.Fatalf("open a: %v %s", err, opened.Error)
	}
	again, err := app.FileCompanionOpen(first)
	if err != nil || again.Error != "" {
		t.Fatalf("reopen a: %v %s", err, again.Error)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Source != "local" || list.Items[0].Name != "a.md" {
		t.Fatalf("list = %#v notice %q", list.Items, list.Notice)
	}
	if list.Notice != "登录后可以查看云端分享" {
		t.Fatalf("notice = %q", list.Notice)
	}
	stored := filepath.Join(app.GetDataDir(), "file-companion", "library.json")
	raw, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(raw), list.Items[0].OpenedAt, "2000-01-01T00:00:00Z", 1)
	if err := os.WriteFile(stored, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(second); err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || list.Items[0].Name != "b.md" || list.Items[1].Name != "a.md" {
		t.Fatalf("order = %#v", list.Items)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "b.md" {
		t.Fatalf("after delete = %#v", list.Items)
	}

	items := make([]FileCompanionLibraryItem, 0, fileCompanionLibraryCap+1)
	for i := 0; i < fileCompanionLibraryCap+1; i++ {
		items = append(items, FileCompanionLibraryItem{
			ID:       "local:extra",
			Name:     "extra.md",
			Path:     second,
			Source:   "local",
			OpenedAt: "2020-01-01T00:00:00Z",
		})
	}
	items[0].OpenedAt = "2026-10-25T00:00:00Z"
	items[0].Name = "newest.md"
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked(items)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != fileCompanionLibraryCap {
		t.Fatalf("cap = %d", len(list.Items))
	}
	if list.Items[0].Name != "newest.md" {
		t.Fatalf("newest = %#v", list.Items[0])
	}
}

func TestFileCompanionLibraryShareAndImport(t *testing.T) {
	var postedName, postedVisibility, postedDays, postedPassword, postedContentHash string
	var postedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Errorf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares":
			media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.HasPrefix(media, "multipart/") {
				t.Errorf("content type %q: %v", r.Header.Get("Content-Type"), err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(8 << 20)
			if err != nil {
				t.Errorf("form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			postedName = form.Value["filename"][0]
			postedVisibility = form.Value["visibility"][0]
			postedDays = form.Value["days"][0]
			postedPassword = form.Value["password"][0]
			postedContentHash = form.Value["content_hash"][0]
			files := form.File["file"]
			if len(files) != 1 {
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			file, err := files[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			postedBody, _ = io.ReadAll(file)
			_ = file.Close()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"cd_0123456789abcdef0123456789abcdef","name":"论文.pdf","visibility":"private","permanent":false,"expires_at":"2026-11-01T00:00:00Z","url":"https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef","created_at":"2026-10-25T00:00:00Z","size":5}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/cd_0123456789abcdef0123456789abcdef/import":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"password":"secret"`) {
				t.Errorf("import body = %s", body)
			}
			w.Header().Set("X-Companion-Document-Name", "shared.pdf")
			w.Header().Set("X-Companion-Document-Owner", "other@example.com")
			w.Header().Set("X-Companion-Document-Expires", "")
			w.Header().Set("X-Companion-Document-Visibility", "public")
			_, _ = w.Write([]byte("%PDF-import"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[{"token":"cd_0123456789abcdef0123456789abcdef","name":"论文.pdf","visibility":"private","permanent":false,"expires_at":"2026-11-01T00:00:00Z","url":"https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5}],"received":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	app := &App{
		testHomeDir:      home,
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL + "/",
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(home, "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside.pdf")
	if err := os.WriteFile(outside, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ShareFileCompanionDocument(outside, "public", "", "", "", 0); err == nil || !strings.Contains(err.Error(), "path is outside the open file") {
		t.Fatalf("ungranted = %v", err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	shared, err := app.ShareFileCompanionDocument(source, "private", "secret", "", "", 90)
	if err != nil {
		t.Fatal(err)
	}
	if shared.URL != "https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef" || shared.Permanent || shared.Visibility != "private" {
		t.Fatalf("share = %#v", shared)
	}
	if postedVisibility != "private" || postedDays != "90" || postedPassword != "secret" || postedName != filepath.Base(source) {
		t.Fatalf("posted name=%q visibility=%q days=%q password=%q", postedName, postedVisibility, postedDays, postedPassword)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("%PDF-")))
	if postedContentHash != wantHash {
		t.Fatalf("posted content_hash = %q, want %q", postedContentHash, wantHash)
	}
	if err := companionShareBodyIsProject(postedBody, "%PDF-"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ShareFileCompanionDocument(source, "public", "", "", "", -1); err == nil || !strings.Contains(err.Error(), "分享时间不正确") {
		t.Fatalf("days = %v", err)
	}
	if _, err := app.ShareFileCompanionDocument(source, "private", "ab", "", "", 1); err == nil || !strings.Contains(err.Error(), "分享密码需为 4–64 个字符") {
		t.Fatalf("password = %v", err)
	}

	const importedToken = "cd_0123456789abcdef0123456789abcdef"
	fileCompanionLibraryMu.Lock()
	epochBefore := fileCompanionShareEpoch[importedToken]
	fileCompanionLibraryMu.Unlock()
	imported, err := app.ImportFileCompanionShare(server.URL+"/hub/documents/shares/"+importedToken, "secret")
	if err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	epochAfter := fileCompanionShareEpoch[importedToken]
	fileCompanionLibraryMu.Unlock()
	if epochAfter <= epochBefore {
		t.Fatalf("import left the share generation at %d", epochAfter)
	}
	body, err := os.ReadFile(imported.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "%PDF-import" || imported.Name != "shared.pdf" || imported.Source != "cloud" {
		t.Fatalf("imported = %#v body %q", imported, body)
	}
	if !strings.Contains(imported.Path, filepath.Join("library-files", "cd_0123456789abcdef0123456789abcdef")) {
		t.Fatalf("path = %s", imported.Path)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if list.Notice != "" {
		t.Fatalf("notice = %q", list.Notice)
	}
	var cloud FileCompanionLibraryItem
	for _, item := range list.Items {
		if item.Token == "cd_0123456789abcdef0123456789abcdef" && item.Path != "" {
			cloud = item
		}
	}
	if cloud.Path == "" || cloud.Name == "" {
		t.Fatalf("cloud row missing: %#v", list.Items)
	}

	if _, err := app.ImportFileCompanionShare("not a link", ""); err == nil || !strings.Contains(err.Error(), "不是伴读文档分享") {
		t.Fatalf("bad link = %v", err)
	}
	tokenOnly, err := fileCompanionShareToken("maclaw://import?token=cd_0123456789abcdef0123456789abcdef")
	if err != nil || tokenOnly != "cd_0123456789abcdef0123456789abcdef" {
		t.Fatalf("token = %q %v", tokenOnly, err)
	}

	anonymous := &App{testHomeDir: t.TempDir()}
	if _, err := anonymous.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	if _, err := anonymous.ShareFileCompanionDocument(source, "public", "", "", "", 0); err == nil || !strings.Contains(err.Error(), "请先登录 MaClaw Hub") {
		t.Fatalf("login = %v", err)
	}

	missingHub := httptest.NewServer(http.NotFoundHandler())
	defer missingHub.Close()
	stale := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      missingHub.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	staleFile := filepath.Join(stale.testHomeDir, "note.md")
	if err := os.WriteFile(staleFile, []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := stale.FileCompanionOpen(staleFile); err != nil {
		t.Fatal(err)
	}
	if _, err := stale.ShareFileCompanionDocument(staleFile, "public", "", "", "", 0); err == nil || !strings.Contains(err.Error(), "云端还没有文档分享，需要更新 Hub") {
		t.Fatalf("missing route = %v", err)
	}
	listed, err := stale.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if listed.Notice != "云端还没有文档分享，需要更新 Hub" || len(listed.Items) != 1 || listed.Items[0].Name != "note.md" {
		t.Fatalf("stale list = %#v notice %q", listed.Items, listed.Notice)
	}
}

func companionShareBodyIsProject(raw []byte, documentPrefix string) error {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	var manifest, document string
	for _, file := range reader.File {
		rc, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		buf, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return readErr
		}
		switch {
		case file.Name == "maclaw-companion.json":
			manifest = string(buf)
		case strings.HasPrefix(file.Name, "document/"):
			document = string(buf)
		}
	}
	if !strings.Contains(manifest, `"kind":"maclaw-file-companion"`) || !strings.HasPrefix(document, documentPrefix) {
		return errors.New("分享里不是伴读项目")
	}
	return nil
}

func TestFileCompanionLibraryMarksExpiredShares(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	localPath := filepath.Join(home, "a.md")
	if err := os.WriteFile(localPath, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	items := []FileCompanionLibraryItem{
		{ID: "cd_0123456789abcdef0123456789abcdef", Token: "cd_0123456789abcdef0123456789abcdef", Name: "past.pdf", Source: "cloud", ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339), OpenedAt: "2026-10-25T00:00:00Z"},
		{ID: "cd_abcdef0123456789abcdef0123456789", Token: "cd_abcdef0123456789abcdef0123456789", Name: "future.pdf", Source: "cloud", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), OpenedAt: "2026-10-25T00:00:00Z"},
		{ID: "cd_11111111111111111111111111111111", Token: "cd_11111111111111111111111111111111", Name: "permanent.pdf", Source: "cloud", Permanent: true, OpenedAt: "2026-10-25T00:00:00Z"},
		{ID: "local:a", Name: "a.md", Path: localPath, Source: "local", OpenedAt: "2026-10-25T00:00:00Z"},
	}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked(items)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string]bool{}
	for _, item := range list.Items {
		flags[item.Name] = item.Expired
	}
	if _, ok := flags["a.md"]; !ok {
		t.Fatalf("local row missing: %#v", list.Items)
	}
	if !flags["past.pdf"] || flags["future.pdf"] || flags["permanent.pdf"] || flags["a.md"] {
		t.Fatalf("expired flags = %#v", flags)
	}

	if fileCompanionShareExpiredAt("not a time", now) {
		t.Fatal("a malformed expiry must not unshare the project")
	}
	if !fileCompanionShareExpiredAt(now.Format(time.RFC3339), now) {
		t.Fatal("an expiry at the current instant is expired")
	}
}

func TestStampFileCompanionLibraryListDropsDeletedRowsAndKeepsExpiry(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	local := filepath.Join(home, "a.md")
	if err := os.WriteFile(local, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const past = "cd_0123456789abcdef0123456789abcdef"
	const gone = "cd_abcdef0123456789abcdef0123456789"
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{
		{ID: past, Token: past, Name: "past.pdf", Source: "cloud", ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339), OpenedAt: "2026-10-25T00:00:00Z"},
		{ID: "local:a", Name: "a.md", Path: local, Source: "local", OpenedAt: "2026-10-25T00:00:00Z"},
	})
	if err == nil {
		_, err = app.dismissFileCompanionShareReasonLocked(gone, true)
	}
	rev := fileCompanionLibraryRev.Load()
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	closed := filepath.Join(home, "closed.pdf")
	stamped := app.stampFileCompanionLibraryList(FileCompanionLibraryList{
		Items: []FileCompanionLibraryItem{
			{ID: past, Token: past, Name: "past.pdf", Source: "cloud", ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
			{ID: "local:a", Name: "a.md", Path: local, Source: "local"},
			{ID: "local:gone", Name: "gone.md", Path: filepath.Join(home, "gone.md"), Source: "local"},
			{ID: gone, Token: gone, Name: "dismissed.pdf", Source: "cloud"},
		},
		Updates: []FileCompanionShareUpdate{
			{Token: gone, Path: closed, Name: "dismissed.pdf"},
			{Token: past, Path: local, Name: "past.pdf"},
		},
		Removed:      []string{local, closed},
		RemovedHosts: map[string]string{local: "王老师", closed: "李老师"},
		LibraryRev:   1,
	})
	if stamped.LibraryRev < rev {
		t.Fatalf("stamped rev = %d, library rev = %d", stamped.LibraryRev, rev)
	}
	var sawPast, sawLocal bool
	for _, item := range stamped.Items {
		switch item.ID {
		case past:
			sawPast = true
			if !item.Expired {
				t.Fatal("expired share lost its flag when the event was stamped")
			}
		case "local:a":
			sawLocal = true
		default:
			t.Fatalf("stamped list kept a deleted row: %+v", item)
		}
	}
	if !sawPast || !sawLocal {
		t.Fatalf("stamped rows = %+v", stamped.Items)
	}
	if len(stamped.Updates) != 1 || stamped.Updates[0].Token != past {
		t.Fatalf("updates = %+v", stamped.Updates)
	}
	if len(stamped.Removed) != 1 || stamped.Removed[0] != closed {
		t.Fatalf("removed = %+v", stamped.Removed)
	}
	if stamped.RemovedHosts[closed] != "李老师" || stamped.RemovedHosts[local] != "" {
		t.Fatalf("removed hosts = %+v", stamped.RemovedHosts)
	}
}

func TestFileCompanionDeleteShareCallsHubAndDropsCloudRow(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			if deleted {
				_, _ = io.WriteString(w, `{"mine":[],"received":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"mine":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5}],"received":[]}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/mobile/documents/shares/"+token:
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("authorization = %q", got)
			}
			if deleted {
				// A second delete finds nothing. The hub says so with a
				// message, and the caller treats that as already done.
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"分享链接不存在"}`)
				return
			}
			deleted = true
			_, _ = io.WriteString(w, `{"deleted":"`+token+`"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list.Items {
		if item.Token == token {
			found = true
		}
	}
	if !found {
		t.Fatalf("cloud row missing before delete: %#v", list.Items)
	}

	if _, err := app.DeleteFileCompanionShare(token); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("hub did not receive DELETE")
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Token == token {
			t.Fatalf("cloud row survived delete: %#v", item)
		}
	}
	if _, err := app.DeleteFileCompanionShare(token); err != nil {
		t.Fatalf("missing link should be treated as deleted: %v", err)
	}
}

func TestFileCompanionDeleteShareReportsOldHub(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	_, err := app.DeleteFileCompanionShare("cd_0123456789abcdef0123456789abcdef")
	if err == nil || !strings.Contains(err.Error(), "云端还不支持删除分享，需要更新 Hub") {
		t.Fatalf("old hub = %v", err)
	}
}

// The four host-transfer bindings are the only way the desktop reaches those
// routes, and the hub reads the decision from the path and the cancel/revoke
// split from the query. A wiring slip would hand the seat to the wrong call.
func TestFileCompanionHostTransferBindingsCallTheHubRoutes(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	type call struct {
		method string
		path   string
		query  string
		body   string
	}
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		calls = append(calls, call{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: strings.TrimSpace(string(raw))})
		if strings.Contains(r.URL.Path, "/host") {
			_, _ = io.WriteString(w, `{"token":"`+token+`"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	if err := app.TransferFileCompanionShareHost(token, "reader@example.com"); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if err := app.RevokeFileCompanionShareHost(token, "cancel"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := app.RevokeFileCompanionShareHost(token, "revoke"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := app.AcceptFileCompanionShareHost(token); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := app.DeclineFileCompanionShareHost(token); err != nil {
		t.Fatalf("decline: %v", err)
	}
	prefix := "/api/mobile/documents/shares/" + token + "/host"
	want := []call{
		{method: http.MethodPost, path: prefix, body: `{"contact":"reader@example.com"}`},
		{method: http.MethodDelete, path: prefix, query: "action=cancel"},
		{method: http.MethodDelete, path: prefix, query: "action=revoke"},
		{method: http.MethodPost, path: prefix + "/accept"},
		{method: http.MethodPost, path: prefix + "/decline"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, expected := range want {
		if calls[i].method != expected.method || calls[i].path != expected.path || calls[i].query != expected.query || calls[i].body != expected.body {
			t.Fatalf("call %d = %+v, want %+v", i, calls[i], expected)
		}
	}
	// A garbage action must never reach the hub as a half-built request.
	if err := app.RevokeFileCompanionShareHost(token, "delete"); err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if len(calls) != len(want) {
		t.Fatalf("an unknown action still called the hub: %+v", calls[len(want):])
	}
}

func TestReconcileFileCompanionLibraryKeepsRowsOpenedDuringAPull(t *testing.T) {
	const kept = "cd_0123456789abcdef0123456789abcdef"
	const added = "cd_abcdef0123456789abcdef0123456789"
	fresh := []FileCompanionLibraryItem{
		{ID: "local:new", Source: "local", Path: "C:/new.md", OpenedAt: "2026-10-25T02:00:00Z"},
		{ID: kept, Token: kept, Source: "cloud", Path: "C:/old.pdf", OpenedAt: "2026-10-25T00:00:00Z", Visibility: "group"},
	}
	snapshot := []FileCompanionLibraryItem{
		{ID: kept, Token: kept, Source: "cloud", Path: "C:/new.pdf", Revision: 2, OpenedAt: "2026-10-25T00:00:00Z", Visibility: "group"},
		{ID: added, Token: added, Source: "cloud", Path: "C:/other.pdf", OpenedAt: "2026-10-25T01:00:00Z"},
	}
	got := reconcileFileCompanionLibrary(fresh, snapshot, map[string]bool{kept: true})
	if len(got) != 2 || got[0].Path != "C:/new.md" || got[1].Token != added {
		t.Fatalf("reconcile dropped a live row or kept a revoked group share: %+v", got)
	}
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	if len(got) != 3 || got[0].Path != "C:/new.md" || got[2].Path != "C:/new.pdf" || got[2].Revision != 2 {
		t.Fatalf("reconcile did not keep the pulled path: %+v", got)
	}
	fresh[1].Revision = 5
	fresh[1].Path = "C:/synced.pdf"
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	var synced FileCompanionLibraryItem
	for _, item := range got {
		if item.Token == kept {
			synced = item
		}
	}
	if synced.Revision != 5 || synced.Path != "C:/synced.pdf" {
		t.Fatalf("reconcile wrote an older refresh over a sync that finished during the pull: %+v", got)
	}
	fresh[1].NotesRevision = 4
	fresh[1].Notes = []FileCompanionShareNote{{ID: "cn_0123456789abcdef0123456789abcdef", Author: "reader@example.com", Text: "读者的讨论"}}
	snapshot[0].NotesRevision = 1
	snapshot[0].Notes = []FileCompanionShareNote{{ID: "cn_old", Text: "旧批注"}}
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	synced = FileCompanionLibraryItem{}
	for _, item := range got {
		if item.Token == kept {
			synced = item
		}
	}
	if synced.NotesRevision != 4 || len(synced.Notes) != 1 || synced.Notes[0].Text != "读者的讨论" {
		t.Fatalf("reconcile hid a newer note behind an older refresh: %+v", synced)
	}
	fresh[1].Revision = 2
	fresh[1].Path = "C:/imported.pdf"
	snapshot[0].Revision = 2
	snapshot[0].Path = ""
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	synced = FileCompanionLibraryItem{}
	for _, item := range got {
		if item.Token == kept {
			synced = item
		}
	}
	if synced.Revision != 2 || synced.Path != "C:/imported.pdf" {
		t.Fatalf("reconcile dropped a path imported during the refresh: %+v", synced)
	}
	snapshot[0].Path = "C:/stale.pdf"
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	synced = FileCompanionLibraryItem{}
	for _, item := range got {
		if item.Token == kept {
			synced = item
		}
	}
	if synced.Revision != 2 || synced.Path != "C:/imported.pdf" {
		t.Fatalf("reconcile put a refresh that did not publish over the imported file: %+v", synced)
	}
}

func TestUpsertFileCompanionCloudKeepsANewerNoteRevision(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	app := &App{testHomeDir: t.TempDir()}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Source: "cloud", Path: "C:/paper.pdf", Revision: 4,
		NotesRevision: 6, OpenedAt: "2026-10-25T00:00:00Z",
		Notes: []FileCompanionShareNote{{ID: "cn_new", Text: "后来的讨论"}},
	})
	// A sync that started before the note returns the old note list and the
	// new document revision.
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Source: "cloud", Path: "C:/paper.pdf", Revision: 8, Mine: true,
		NotesRevision: 2, OpenedAt: "2026-10-25T00:00:00Z",
		Notes: []FileCompanionShareNote{{ID: "cn_old", Text: "旧批注"}},
	})
	fileCompanionLibraryMu.Lock()
	items, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok || len(items) != 1 || items[0].Revision != 8 || items[0].NotesRevision != 6 || len(items[0].Notes) != 1 || items[0].Notes[0].Text != "后来的讨论" || !items[0].Mine {
		t.Fatalf("sync write covered a newer note: ok=%v items=%+v", ok, items)
	}
}

func TestMergeFileCompanionSharesKeepsANewerNoteRevision(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	paper := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(paper, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := []FileCompanionLibraryItem{{
		Token: token, Source: "cloud", Path: paper, Revision: 4,
		NotesRevision: 3, OpenedAt: "2026-10-25T00:00:00Z",
		Notes: []FileCompanionShareNote{{ID: "cn_new", Text: "后来的讨论"}},
	}}
	remote := []FileCompanionLibraryItem{{
		Token: token, Source: "cloud", Revision: 4, NotesKnown: true,
		NotesRevision: 1, OpenedAt: "2026-10-25T00:00:00Z",
		Notes: []FileCompanionShareNote{{ID: "cn_old", Text: "旧批注"}},
	}}
	got := mergeFileCompanionShares(local, remote, "https://hub.example", nil)
	if len(got) != 1 || got[0].Revision != 4 || got[0].Path != paper || got[0].NotesRevision != 3 || len(got[0].Notes) != 1 || got[0].Notes[0].Text != "后来的讨论" {
		t.Fatalf("merge covered a newer note with an older list: %+v", got)
	}
	remote[0].NotesKnown = false
	remote[0].Notes = nil
	remote[0].NotesRevision = 0
	got = mergeFileCompanionShares(local, remote, "https://hub.example", nil)
	if len(got) != 1 || got[0].NotesRevision != 3 || got[0].Notes[0].Text != "后来的讨论" {
		t.Fatalf("merge let a hub without notes wipe the stored list: %+v", got)
	}
}

func TestFileCompanionShareNotePostsTheReadersDiscussion(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	var app *App
	var openedPath string
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/mobile/documents/shares/"+token+"/notes" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "方法部分值得再看") {
			t.Errorf("note body = %s", body)
		}
		step++
		if app != nil && step == 1 {
			// A host sync finishes while this note is still uploading.
			_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
				ID: token, Token: token, Name: "论文.pdf", Path: openedPath,
				Source: "cloud", Visibility: "group", Mine: false, Revision: 9,
				NotesRevision: 5,
				Notes:         []FileCompanionShareNote{{ID: "cn_later", Author: "other@example.com", Text: "后来的讨论"}},
			})
		}
		if app != nil && step == 2 {
			fileCompanionLibraryMu.Lock()
			items, ok := app.readFileCompanionLibraryLocked()
			kept := make([]FileCompanionLibraryItem, 0, len(items))
			if ok {
				for _, item := range items {
					if item.Source == "cloud" && item.Token == token {
						continue
					}
					kept = append(kept, item)
				}
			}
			_ = app.writeFileCompanionLibraryLocked(kept)
			fileCompanionLibraryMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"notes_revision":1,"notes":[{"id":"cn_0123456789abcdef0123456789abcdef","author":"reader@example.com","text":"方法部分值得再看","created_at":"2026-10-25T00:00:00Z"}]}`)
	}))
	defer server.Close()
	app = &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	openedPath = opened.Path
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID:         token,
		Token:      token,
		Name:       "论文.pdf",
		Path:       opened.Path,
		Source:     "cloud",
		Visibility: "group",
		Mine:       false,
		Revision:   4,
	})
	notes, err := app.AddFileCompanionShareNote(opened.Path, "方法部分值得再看")
	if err != nil {
		t.Fatal(err)
	}
	if notes.NotesRevision != 5 || len(notes.Notes) != 1 || notes.Notes[0].Text != "后来的讨论" {
		t.Fatalf("stale note response covered a newer list: %#v", notes)
	}
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var stored FileCompanionLibraryItem
	for _, item := range listed.Items {
		if item.Token == token {
			stored = item
		}
	}
	if stored.Revision != 9 || stored.Path != opened.Path || stored.NotesRevision != 5 || len(stored.Notes) != 1 || stored.Notes[0].Text != "后来的讨论" {
		t.Fatalf("note write rolled the library row back: %+v", stored)
	}
	if _, err := app.AddFileCompanionShareNote(opened.Path, "方法部分值得再看"); err != nil {
		t.Fatal(err)
	}
	listed, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Items {
		if item.Token == token {
			t.Fatalf("a removed share was put back by the note write: %+v", item)
		}
	}
	if _, err := app.AddFileCompanionShareNote(filepath.Join(t.TempDir(), "missing.pdf"), "没有分享"); err == nil {
		t.Fatal("an unshared file accepted a note")
	}
}

func TestShareFileCompanionDocumentAdoptsAnExistingDuplicate(t *testing.T) {
	const token = "cd_22222222222222222222222222222222"
	var createdAt, patchedAt, syncedAt time.Time
	var created, patched, synced bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.Copy(io.Discard, r.Body)
			created = true
			createdAt = time.Now()
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"code":"SHARE_DUPLICATE","message":"这份文档已经以相同的范围分享过了，请先删除原分享，或修改原分享","token":"`+token+`","share":{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","revision":3,"size":5}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/mobile/documents/shares/"+token:
			patched = true
			patchedAt = time.Now()
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","revision":3,"size":5}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/sync":
			synced = true
			syncedAt = time.Now()
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","revision":9,"size":5}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-duplicate-recover"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.ShareFileCompanionDocument(source, "public", "", "", "[]", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !patched || !synced {
		t.Fatalf("calls created=%v patched=%v synced=%v", created, patched, synced)
	}
	if syncedAt.Before(createdAt) || !patchedAt.Before(syncedAt) {
		t.Fatalf("duplicate recovery order: create=%v patch=%v sync=%v", createdAt, patchedAt, syncedAt)
	}
	if result.Token != token || result.Revision != 9 {
		t.Fatalf("result = %+v", result)
	}
	fileCompanionLibraryMu.Lock()
	items, _ := app.readFileCompanionLibraryLocked()
	dismissed := app.readDismissedShareTokensLocked()[token]
	fileCompanionLibraryMu.Unlock()
	var row *FileCompanionLibraryItem
	for i := range items {
		if items[i].Source == "cloud" && items[i].Token == token {
			row = &items[i]
		}
	}
	if row == nil || !row.Mine || !strings.EqualFold(row.Path, opened.Path) {
		t.Fatalf("adopted row = %+v", row)
	}
	if dismissed {
		t.Fatal("the adopted share stayed dismissed")
	}
}

func TestSyncFileCompanionShareUpdatesEveryShareOfTheFile(t *testing.T) {
	const (
		newer    = "cd_0123456789abcdef0123456789abcdef"
		older    = "cd_abcdef0123456789abcdef0123456789"
		other    = "cd_fedcba9876543210fedcba9876543210"
		received = "cd_11111111111111111111111111111111"
	)
	var app *App
	var openedPath string
	var synced []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/sync") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/mobile/documents/shares/"), "/sync")
		if token != newer && token != older {
			http.Error(w, `{"message":"只有分享者可以同步更新"}`, http.StatusForbidden)
			return
		}
		synced = append(synced, token)
		if token == newer && app != nil {
			// A note lands on the other share while the first upload is in flight.
			_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
				ID: older, Token: older, Name: "论文.pdf", Path: openedPath,
				Source: "cloud", Visibility: "group", Mine: true, Revision: 4,
				NotesRevision: 7,
				Notes:         []FileCompanionShareNote{{ID: "cn_mid", Author: "reader@example.com", Text: "同步途中的批注"}},
			})
		}
		revision := 8
		if token == older {
			revision = 11
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":"%s","name":"论文.pdf","revision":%d,"url":"https://hub.example/s/%s","visibility":"group","permanent":true}`, token, revision, token)
	}))
	defer server.Close()
	app = &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	openedPath = opened.Path
	if _, err := app.SyncFileCompanionShare(opened.Path, "[]"); err == nil || !strings.Contains(err.Error(), "还没有分享这份文档") {
		t.Fatalf("sync of an unshared file: %v", err)
	}
	otherPath := filepath.Join(t.TempDir(), "other.pdf")
	if err := os.WriteFile(otherPath, []byte("%PDF-other"), 0o644); err != nil {
		t.Fatal(err)
	}
	olderPath := strings.ReplaceAll(opened.Path, `\`, `/`)
	if olderPath == opened.Path {
		olderPath = strings.ToUpper(opened.Path)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: other, Token: other, Name: "other.pdf", Path: otherPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 3,
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: received, Token: received, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "group", Mine: false, Revision: 4,
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: olderPath,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: newer, Token: newer, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 5,
		NotesRevision: 2,
		Notes:         []FileCompanionShareNote{{ID: "cn_new", Author: "a@example.com", Text: "第一份的批注"}},
	})
	result, err := app.SyncFileCompanionShare(opened.Path, "[]")
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != older || result.Revision != 11 {
		t.Fatalf("sync result = %+v, want the last share", result)
	}
	if strings.Join(synced, ",") != newer+","+older {
		t.Fatalf("synced tokens = %v", synced)
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("library unreadable")
	}
	got := map[string]FileCompanionLibraryItem{}
	for _, item := range stored {
		if item.Source == "cloud" && item.Token != "" {
			got[item.Token] = item
		}
	}
	if row := got[newer]; row.Revision != 8 || row.Path != opened.Path || row.NotesRevision != 2 || len(row.Notes) != 1 || row.Notes[0].Text != "第一份的批注" || row.ShareURL != "https://hub.example/s/"+newer {
		t.Fatalf("newer share was not updated on its own: %+v", row)
	}
	if row := got[older]; row.Revision != 11 || row.Path != opened.Path || !row.Mine || row.NotesRevision != 7 || len(row.Notes) != 1 || row.Notes[0].Text != "同步途中的批注" || row.ShareURL != "https://hub.example/s/"+older {
		t.Fatalf("older share missed the sync or lost the note that landed during it: %+v", row)
	}
	if row := got[other]; row.Revision != 3 || row.Path != otherPath {
		t.Fatalf("another file was synced: %+v", row)
	}
	if row := got[received]; row.Revision != 4 || row.Mine || row.Path != opened.Path {
		t.Fatalf("a received copy was synced: %+v", row)
	}
}

func TestSyncFileCompanionShareContinuesAfterOneShareFails(t *testing.T) {
	const (
		broken = "cd_0123456789abcdef0123456789abcdef"
		older  = "cd_abcdef0123456789abcdef0123456789"
	)
	var synced []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/sync") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/mobile/documents/shares/"), "/sync")
		synced = append(synced, token)
		w.Header().Set("Content-Type", "application/json")
		if token == broken {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"分享链接已过期"}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"token":"%s","name":"论文.pdf","revision":11,"url":"https://hub.example/s/%s","visibility":"group","permanent":true}`, token, token)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: broken, Token: broken, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 5,
		NotesRevision: 2,
		Notes:         []FileCompanionShareNote{{ID: "cn_new", Author: "a@example.com", Text: "第一份的批注"}},
	})
	_, err = app.SyncFileCompanionShare(opened.Path, "[]")
	if err == nil || !strings.Contains(err.Error(), "分享链接已过期") {
		t.Fatalf("sync error = %v", err)
	}
	if strings.Join(synced, ",") != broken+","+older {
		t.Fatalf("synced tokens = %v", synced)
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("library unreadable")
	}
	got := map[string]FileCompanionLibraryItem{}
	for _, item := range stored {
		if item.Source == "cloud" && item.Token != "" {
			got[item.Token] = item
		}
	}
	if row := got[broken]; row.Revision != 5 || row.NotesRevision != 2 || len(row.Notes) != 1 || row.Notes[0].Text != "第一份的批注" {
		t.Fatalf("failed share was rewritten: %+v", row)
	}
	if row := got[older]; row.Revision != 11 || row.NotesRevision != 6 || len(row.Notes) != 1 || row.Notes[0].Text != "后来的讨论" || row.ShareURL != "https://hub.example/s/"+older {
		t.Fatalf("later share did not update after an earlier failure: %+v", row)
	}
}

func TestFileCompanionShareStatusUsesTheHubMessage(t *testing.T) {
	empty := &fileCompanionShareStatus{status: http.StatusNotFound}
	if empty.Error() != "云端还没有文档分享，需要更新 Hub" {
		t.Fatalf("empty 404 = %q", empty.Error())
	}
	if fileCompanionShareGone(empty) {
		t.Fatal("a 404 with no message is an old hub")
	}
	missing := &fileCompanionShareStatus{status: http.StatusNotFound, message: "分享链接不存在"}
	if missing.Error() != "分享链接不存在" {
		t.Fatalf("missing share = %q", missing.Error())
	}
	if !fileCompanionShareGone(missing) {
		t.Fatal("a missing share should be dropped")
	}
	expired := &fileCompanionShareStatus{status: http.StatusGone, message: "分享链接已过期"}
	if expired.Error() != "分享链接已过期" || !fileCompanionShareGone(expired) {
		t.Fatalf("expired = %q gone %v", expired.Error(), fileCompanionShareGone(expired))
	}
	if fileCompanionShareGone(&fileCompanionShareStatus{status: http.StatusGone}) {
		t.Fatal("410 without a message stays")
	}
	forbidden := &fileCompanionShareStatus{status: http.StatusForbidden, message: "分享链接已过期"}
	if fileCompanionShareGone(forbidden) || forbidden.Error() != "分享链接已过期" {
		t.Fatal("403 is not a missing share")
	}
	if fileCompanionShareGone(errors.New("暂时无法连接云端")) {
		t.Fatal("a network error is not a missing share")
	}
}

// UpdateFileCompanionShare changes the link the reader already published: the
// same token comes back, the row keeps its local path, and a blank password
// leaves the stored one alone.
func TestUpdateFileCompanionShareChangesTheExistingLink(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	var patchedPath, patchedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5,"revision":1}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5}],"received":[]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/mobile/documents/shares/"+token:
			raw, _ := io.ReadAll(r.Body)
			patchedPath = r.URL.Path
			patchedBody = string(raw)
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"private","permanent":false,"expires_at":"2026-11-24T00:00:00Z","url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5,"revision":3}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	app := &App{
		testHomeDir:      home,
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(home, "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	// Publish first: the cloud row keeps the local path it was shared from,
	// and 修改 must not lose that path.
	if _, err := app.ShareFileCompanionDocument(source, "public", "", "", "[]", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}

	updated, err := app.UpdateFileCompanionShare(token, "private", "secret", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if patchedPath != "/api/mobile/documents/shares/"+token {
		t.Fatalf("patched %q", patchedPath)
	}
	if !strings.Contains(patchedBody, `"visibility":"private"`) || !strings.Contains(patchedBody, `"password":"secret"`) || !strings.Contains(patchedBody, `"days":30`) {
		t.Fatalf("body = %s", patchedBody)
	}
	if updated.Token != token || updated.Visibility != "private" || updated.Permanent || updated.ExpiresAt == "" {
		t.Fatalf("updated = %#v", updated)
	}

	row, ok := fileCompanionCloudRow(t, app, token)
	if !ok {
		t.Fatal("the cloud row disappeared")
	}
	canonical, canonErr := fileCompanionCanonicalPath(source)
	if canonErr != nil {
		t.Fatal(canonErr)
	}
	if row.Path != canonical || row.Visibility != "private" || row.ExpiresAt == "" {
		t.Fatalf("row = %#v", row)
	}

	// A blank password is how 修改 keeps the stored one; it still has to be
	// allowed through validation.
	if _, err := app.UpdateFileCompanionShare(token, "private", "", "", 0); err != nil {
		t.Fatalf("blank password = %v", err)
	}
	if _, err := app.UpdateFileCompanionShare(token, "private", "ab", "", 0); err == nil || !strings.Contains(err.Error(), "分享密码需为 4–64 个字符") {
		t.Fatalf("short password = %v", err)
	}
	if _, err := app.UpdateFileCompanionShare(token, "group", "", "not-a-group", 0); err == nil || !strings.Contains(err.Error(), "请选择群组") {
		t.Fatalf("group = %v", err)
	}
	if _, err := app.UpdateFileCompanionShare(token, "public", "", "", 3661); err == nil || !strings.Contains(err.Error(), "分享时间不正确") {
		t.Fatalf("days = %v", err)
	}
	// A negative days means "leave the expiry as it is", so the request must
	// not carry a days field at all.
	patchedBody = ""
	if _, err := app.UpdateFileCompanionShare(token, "public", "", "", -1); err != nil {
		t.Fatalf("keep expiry = %v", err)
	}
	if strings.Contains(patchedBody, `"days"`) {
		t.Fatalf("a kept expiry was sent as days: %s", patchedBody)
	}
	if _, err := app.UpdateFileCompanionShare("not-a-token", "public", "", "", 0); err == nil || !strings.Contains(err.Error(), "分享链接不存在") {
		t.Fatalf("token = %v", err)
	}
}

// An edit that finishes after the reader deleted the project must not put the
// row back into 文档管理.
func TestUpdateFileCompanionShareDoesNotRestoreADeletedRow(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5,"revision":1}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/mobile/documents/shares/"+token:
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"论文.pdf","visibility":"private","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":5,"revision":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	app := &App{
		testHomeDir:      home,
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(home, "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ShareFileCompanionDocument(source, "public", "", "", "[]", 0); err != nil {
		t.Fatal(err)
	}
	// The reader deletes the project while the edit is on its way to the hub.
	if _, err := app.DeleteFileCompanionProject(token, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UpdateFileCompanionShare(token, "private", "secret", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := fileCompanionCloudRow(t, app, token); ok {
		t.Fatal("the edit put the deleted share back into the library")
	}
}

func TestStaleHubUpdateErrorNamesTheMissingRoute(t *testing.T) {
	const staleHub = http.StatusNotFound
	// A 404 with no message is an old hub that has no 修改 route at all.
	for _, code := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		got := staleHubUpdateError(&fileCompanionShareStatus{status: code})
		if got.Error() != "云端还不支持修改分享，需要更新 Hub" {
			t.Fatalf("%d: %q", code, got.Error())
		}
	}
	// A hub that answered keeps its own message, even on a 404.
	missing := staleHubUpdateError(&fileCompanionShareStatus{status: staleHub, message: "分享链接不存在"})
	if missing.Error() != "分享链接不存在" {
		t.Fatalf("missing share = %q", missing.Error())
	}
	forbidden := staleHubUpdateError(&fileCompanionShareStatus{status: http.StatusForbidden, message: "只有分享者可以修改分享"})
	if forbidden.Error() != "只有分享者可以修改分享" {
		t.Fatalf("forbidden = %q", forbidden.Error())
	}
	offline := staleHubUpdateError(errors.New("暂时无法连接云端"))
	if offline.Error() != "暂时无法连接云端" {
		t.Fatalf("offline = %q", offline.Error())
	}
}

// fileCompanionCloudRow is the test's window onto the stored library: the
// production code only ever rewrites a row it already has.
func fileCompanionCloudRow(t *testing.T, app *App, token string) (FileCompanionLibraryItem, bool) {
	t.Helper()
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := app.readFileCompanionLibraryLocked()
	if !ok {
		return FileCompanionLibraryItem{}, false
	}
	for _, item := range items {
		if item.Source == "cloud" && item.Token == token {
			return item, true
		}
	}
	return FileCompanionLibraryItem{}, false
}

func TestSyncFileCompanionShareDropsAShareTheHubNoLongerKeeps(t *testing.T) {
	const (
		expired  = "cd_0123456789abcdef0123456789abcdef"
		missing  = "cd_abcdef0123456789abcdef0123456789"
		plain    = "cd_fedcba9876543210fedcba9876543210"
		older    = "cd_11111111111111111111111111111111"
		other    = "cd_22222222222222222222222222222222"
		received = "cd_33333333333333333333333333333333"
	)
	var synced []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/sync") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/mobile/documents/shares/"), "/sync")
		synced = append(synced, token)
		w.Header().Set("Content-Type", "application/json")
		switch token {
		case expired:
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, `{"message":"分享链接已过期"}`)
		case missing:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"分享链接不存在"}`)
		case plain:
			http.NotFound(w, r)
		default:
			_, _ = fmt.Fprintf(w, `{"token":"%s","name":"论文.pdf","revision":11,"url":"https://hub.example/s/%s","visibility":"group","permanent":true}`, token, token)
		}
	}))
	defer server.Close()
	newApp := func() *App {
		return &App{
			testHomeDir:      t.TempDir(),
			configCacheValid: true,
			configCache: corelib.AppConfig{
				RemoteHubURL:      server.URL,
				RemoteViewerToken: "viewer-token",
			},
		}
	}
	openPaper := func(app *App) (string, string) {
		t.Helper()
		source := filepath.Join(t.TempDir(), "论文.pdf")
		if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
			t.Fatal(err)
		}
		opened, err := app.FileCompanionOpen(source)
		if err != nil {
			t.Fatal(err)
		}
		return source, opened.Path
	}
	cloudRows := func(app *App) map[string]FileCompanionLibraryItem {
		t.Helper()
		fileCompanionLibraryMu.Lock()
		stored, ok := app.readFileCompanionLibraryLocked()
		fileCompanionLibraryMu.Unlock()
		if !ok {
			t.Fatal("library unreadable")
		}
		got := map[string]FileCompanionLibraryItem{}
		for _, item := range stored {
			if item.Source == "cloud" && item.Token != "" {
				got[item.Token] = item
			}
		}
		return got
	}
	paperRemains := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			t.Fatalf("paper = %v", err)
		}
	}

	app := newApp()
	source, openedPath := openPaper(app)
	otherPath := filepath.Join(t.TempDir(), "other.pdf")
	if err := os.WriteFile(otherPath, []byte("%PDF-other"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: other, Token: other, Name: "other.pdf", Path: otherPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 3,
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: received, Token: received, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: false, Revision: 4,
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: missing, Token: missing, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 5,
		NotesRevision: 2,
		Notes:         []FileCompanionShareNote{{ID: "cn_miss", Author: "a@example.com", Text: "已取消的批注"}},
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: expired, Token: expired, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 7,
		NotesRevision: 3,
		Notes:         []FileCompanionShareNote{{ID: "cn_exp", Author: "a@example.com", Text: "过期分享的批注"}},
	})
	synced = nil
	result, err := app.SyncFileCompanionShare(openedPath, "[]")
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != older || result.Revision != 11 {
		t.Fatalf("sync result = %+v", result)
	}
	if strings.Join(synced, ",") != expired+","+missing+","+older {
		t.Fatalf("synced tokens = %v", synced)
	}
	got := cloudRows(app)
	if _, ok := got[expired]; ok {
		t.Fatalf("expired share stayed: %+v", got[expired])
	}
	if _, ok := got[missing]; ok {
		t.Fatalf("missing share stayed: %+v", got[missing])
	}
	if row := got[older]; row.Revision != 11 || row.Path != openedPath || !row.Mine || row.NotesRevision != 6 || len(row.Notes) != 1 || row.Notes[0].Text != "后来的讨论" {
		t.Fatalf("surviving share = %+v", row)
	}
	if row := got[other]; row.Revision != 3 || row.Path != otherPath {
		t.Fatalf("another file was dropped: %+v", row)
	}
	if row := got[received]; row.Revision != 4 || row.Mine || row.Path != openedPath {
		t.Fatalf("received copy = %+v", row)
	}
	paperRemains(source)
	paperRemains(openedPath)

	stale := newApp()
	plainSource, plainPath := openPaper(stale)
	_ = stale.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: plainPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	_ = stale.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: plain, Token: plain, Name: "论文.pdf", Path: plainPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 5,
		NotesRevision: 2,
		Notes:         []FileCompanionShareNote{{ID: "cn_plain", Author: "a@example.com", Text: "第一份的批注"}},
	})
	synced = nil
	_, err = stale.SyncFileCompanionShare(plainPath, "[]")
	if err == nil || !strings.Contains(err.Error(), "云端还没有文档分享，需要更新 Hub") {
		t.Fatalf("old hub = %v", err)
	}
	if strings.Join(synced, ",") != plain+","+older {
		t.Fatalf("old hub synced tokens = %v", synced)
	}
	got = cloudRows(stale)
	if row := got[plain]; row.Revision != 5 || row.NotesRevision != 2 || len(row.Notes) != 1 || row.Notes[0].Text != "第一份的批注" {
		t.Fatalf("old hub dropped a share: %+v", row)
	}
	if row := got[older]; row.Revision != 11 || row.NotesRevision != 6 || len(row.Notes) != 1 || row.Notes[0].Text != "后来的讨论" {
		t.Fatalf("old hub skipped the other share: %+v", row)
	}
	paperRemains(plainSource)

	onlyExpired := newApp()
	expiredSource, expiredPath := openPaper(onlyExpired)
	_ = onlyExpired.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: expired, Token: expired, Name: "论文.pdf", Path: expiredPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 7,
	})
	synced = nil
	_, err = onlyExpired.SyncFileCompanionShare(expiredPath, "[]")
	if err == nil || !strings.Contains(err.Error(), "分享链接已过期") {
		t.Fatalf("only expired = %v", err)
	}
	if _, ok := cloudRows(onlyExpired)[expired]; ok {
		t.Fatal("expired share stayed when it was the only one")
	}
	paperRemains(expiredSource)
}

func TestDropRevokedGroupCopiesStaysInTheLibraryDir(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	root := filepath.Join(app.GetDataDir(), "file-companion", "library-files")
	token := "cd_0123456789abcdef0123456789abcdef"
	if err := os.MkdirAll(filepath.Join(root, token), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, token, "a.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "keep.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner := "cd_abcdef0123456789abcdef0123456789"
	paper := filepath.Join(home, "论文.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(root, token, "a.pdf")
	previous := map[string]FileCompanionLibraryItem{
		token:     {Token: token, Visibility: "group", Path: copyPath, Source: "cloud"},
		owner:     {Token: owner, Visibility: "group", Mine: true, Path: paper, Source: "cloud"},
		"../keep": {Token: "../keep", Visibility: "group", Path: outside, Source: "cloud"},
	}
	fileCompanionLibraryMu.Lock()
	epochs := map[string]uint64{
		token: fileCompanionShareEpoch[token],
		owner: fileCompanionShareEpoch[owner],
	}
	fileCompanionLibraryMu.Unlock()
	dropped := map[string]bool{}
	removed := app.dropRevokedGroupCopies(previous, nil, dropped, epochs)
	if _, err := os.Stat(filepath.Join(root, token)); !os.IsNotExist(err) {
		t.Fatalf("group copy still on disk: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("token escaped the library dir: %v", err)
	}
	if _, err := os.Stat(paper); err != nil {
		t.Fatalf("owner paper was removed: %v", err)
	}
	if !dropped[token] || !dropped[owner] || dropped["../keep"] || len(removed) != 1 || removed[0] != copyPath {
		t.Fatalf("dropped=%v removed=%v", dropped, removed)
	}
}

func TestDropRevokedGroupCopyKeepsAFileImportedDuringTheList(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	token := "cd_0123456789abcdef0123456789abcdef"
	copyPath := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "a.pdf")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := map[string]FileCompanionLibraryItem{
		token: {Token: token, Visibility: "group", Path: copyPath, Source: "cloud"},
	}
	app.bumpFileCompanionShareEpoch(token)
	if err := os.WriteFile(copyPath, []byte("%PDF-import"), 0o644); err != nil {
		t.Fatal(err)
	}
	dropped := map[string]bool{}
	removed := app.dropRevokedGroupCopies(previous, nil, dropped, map[string]uint64{token: 0})
	if got, err := os.ReadFile(copyPath); err != nil || string(got) != "%PDF-import" {
		t.Fatalf("import was removed with the revoked share: %q %v", got, err)
	}
	if dropped[token] || len(removed) != 0 {
		t.Fatalf("imported share was dropped: dropped=%v removed=%v", dropped, removed)
	}
}

func TestListKeepsAGroupShareImportedWhileAnotherDownloadRuns(t *testing.T) {
	const revoked = "cd_44444444444444444444444444444444"
	const other = "cd_55555555555555555555555555555555"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	copyPath := filepath.Join(app.GetDataDir(), "file-companion", "library-files", revoked, "a.pdf")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: revoked, Token: revoked, Name: "a.pdf", Path: identity, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Visibility: "group", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+other+`","name":"other.pdf","visibility":"public","permanent":true,"revision":2,"url":"https://hub.example/hub/documents/shares/`+other+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":8}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			app.bumpFileCompanionShareEpoch(revoked)
			if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(copyPath, []byte("%PDF-import"), 0o644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
				ID: revoked, Token: revoked, Name: "a.pdf", Path: identity, Source: "cloud",
				OpenedAt: "2026-10-25T00:00:02Z", Visibility: "group", Revision: 3,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("X-Companion-Document-Name", "other.pdf")
			w.Header().Set("X-Companion-Document-Revision", "2")
			_, _ = w.Write([]byte("%PDF-other"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, row := range list.Items {
		if row.Token == revoked {
			saw = true
			if row.Path != identity || row.Revision != 3 {
				t.Fatalf("imported row = %+v", row)
			}
		}
	}
	if !saw {
		t.Fatalf("import during the refresh was dropped: %+v", list.Items)
	}
	for _, path := range list.Removed {
		if path == identity {
			t.Fatalf("import during the refresh closed the tab: %+v", list.Removed)
		}
	}
	if got, err := os.ReadFile(copyPath); err != nil || string(got) != "%PDF-import" {
		t.Fatalf("imported copy = %q %v", got, err)
	}
}

func TestPullDropsTheTranscriptOfAReplacedShareCopy(t *testing.T) {
	const token = "cd_22222222222222222222222222222222"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	oldDest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "old.pdf")
	if err := os.MkdirAll(filepath.Dir(oldDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldDest, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldID, err := fileCompanionCanonicalPath(oldDest)
	if err != nil {
		t.Fatal(err)
	}
	oldSession := fileCompanionSessionID(oldID)
	oldSide := filepath.Join(app.GetDataDir(), "file-companion", "sessions", oldSession+".json")
	oldPaper := filepath.Join(app.GetDataDir(), "file-companion", "results", oldSession, "论文解读.pdf")
	if err := os.MkdirAll(filepath.Dir(oldSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldSide, []byte(`{"messages":["old"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPaper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPaper, []byte("%PDF-old-paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(home, "local.md")
	if err := os.WriteFile(local, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	localID, err := fileCompanionCanonicalPath(local)
	if err != nil {
		t.Fatal(err)
	}
	localSide := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(localID)+".json")
	if err := os.MkdirAll(filepath.Dir(localSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localSide, []byte(`{"messages":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{
		{ID: token, Token: token, Name: "old.pdf", Path: oldID, Source: "cloud", OpenedAt: "2026-10-25T00:00:00Z", Visibility: "public", Revision: 1},
		{ID: "local:" + localID, Name: "local.md", Path: localID, Source: "local", OpenedAt: "2026-10-25T00:00:01Z"},
	})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stage, err := app.stageFileCompanionShare(token, "new.pdf", []byte("%PDF-new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.commitStagedFileCompanionShare(stage, 0, false, nil); err != nil {
		t.Fatal(err)
	}
	newDest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "new.pdf")
	if got, err := os.ReadFile(newDest); err != nil || string(got) != "%PDF-new" {
		t.Fatalf("new copy = %q %v", got, err)
	}
	if got, err := os.ReadFile(oldDest); err != nil || string(got) != "%PDF-old" {
		t.Fatalf("old copy = %q %v", got, err)
	}
	if _, err := os.Stat(oldSide); !os.IsNotExist(err) {
		t.Fatalf("replaced copy kept its transcript: %v", err)
	}
	if _, err := os.Stat(oldPaper); !os.IsNotExist(err) {
		t.Fatalf("replaced copy kept its paper: %v", err)
	}
	if got, err := os.ReadFile(localSide); err != nil || string(got) != `{"messages":["keep"]}` {
		t.Fatalf("local transcript = %q %v", got, err)
	}
	fileCompanionLibraryMu.Lock()
	items, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("library unreadable")
	}
	var row FileCompanionLibraryItem
	for _, item := range items {
		if item.Token == token {
			row = item
		}
	}
	newID, err := fileCompanionCanonicalPath(newDest)
	if err != nil {
		t.Fatal(err)
	}
	if row.Path != newID {
		t.Fatalf("row path = %s, want %s", row.Path, newID)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestPullKeepsATranscriptOutsideTheShareCopy(t *testing.T) {
	const token = "cd_33333333333333333333333333333333"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	local := filepath.Join(home, "local.md")
	if err := os.WriteFile(local, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	localID, err := fileCompanionCanonicalPath(local)
	if err != nil {
		t.Fatal(err)
	}
	localSide := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(localID)+".json")
	if err := os.MkdirAll(filepath.Dir(localSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localSide, []byte(`{"messages":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "local.md", Path: localID, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stage, err := app.stageFileCompanionShare(token, "new.pdf", []byte("%PDF-new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.commitStagedFileCompanionShare(stage, 0, false, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(local); err != nil || string(got) != "local" {
		t.Fatalf("source = %q %v", got, err)
	}
	if got, err := os.ReadFile(localSide); err != nil || string(got) != `{"messages":["keep"]}` {
		t.Fatalf("source transcript = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestDeleteFileCompanionProjectKeepsSourceFile(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "信息安全数学基础.pdf")
	body := []byte("%PDF-source-keep")
	if err := os.WriteFile(source, body, 0o644); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(home, "other.pdf")
	if err := os.WriteFile(decoy, []byte("decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil || opened.Error != "" {
		t.Fatalf("open: %v %s", err, opened.Error)
	}
	identity, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	data := app.GetDataDir()
	side := filepath.Join(data, "file-companion", "sessions", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(`{"messages":["hi"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"figures", "results", "work"} {
		dir := filepath.Join(data, "file-companion", kind, sessionID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "related.txt"), []byte("related"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list: %v %#v", err, list.Items)
	}
	deleted, err := app.DeleteFileCompanionProject(list.Items[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Cleared || deleted.Path == "" {
		t.Fatalf("deleted = %#v", deleted)
	}
	got, err := os.ReadFile(source)
	if err != nil || string(got) != string(body) {
		t.Fatalf("source = %q err %v", got, err)
	}
	if decoyBody, err := os.ReadFile(decoy); err != nil || string(decoyBody) != "decoy" {
		t.Fatalf("decoy = %q err %v", decoyBody, err)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("session side = %v", err)
	}
	for _, kind := range []string{"figures", "results", "work"} {
		dir := filepath.Join(data, "file-companion", kind, sessionID)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s still present: %v", kind, err)
		}
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("after = %v %#v", err, list.Items)
	}
	if _, err := app.DeleteFileCompanionProject(deleted.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("reopen = %v %#v", err, list.Items)
	}
	got, err = os.ReadFile(source)
	if err != nil || string(got) != string(body) {
		t.Fatalf("source after reopen = %q err %v", got, err)
	}
}

func TestSharedDocumentBecomesCloudProject(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil || opened.Error != "" {
		t.Fatalf("open: %v %s", err, opened.Error)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 || list.Items[0].Source == "cloud" {
		t.Fatalf("local before share: %v %#v", err, list.Items)
	}
	localID := list.Items[0].ID
	const token = "cd_0123456789abcdef0123456789abcdef"
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "paper.pdf", Path: source, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: true, Visibility: "group", Size: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var sawCloud, sawLocal bool
	for _, item := range list.Items {
		if item.Source == "cloud" && item.Token == token {
			sawCloud = true
		}
		if item.Source != "cloud" && fileCompanionSameLibraryPath(item.Path, source) {
			sawLocal = true
		}
	}
	if !sawCloud || sawLocal {
		t.Fatalf("shared paper should be only a cloud project: %#v", list.Items)
	}
	deleted, err := app.DeleteFileCompanionProject(localID, source)
	if err != nil || deleted.Cleared {
		t.Fatalf("delete of retired local row = %#v %v", deleted, err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 || list.Items[0].Token != token {
		t.Fatalf("cloud project after local delete: %v %#v", err, list.Items)
	}
	app.retireFileCompanionShare(token)
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	sawCloud, sawLocal = false, false
	for _, item := range list.Items {
		if item.Token == token {
			sawCloud = true
		}
		if item.Source != "cloud" && fileCompanionSameLibraryPath(item.Path, source) {
			sawLocal = true
		}
	}
	if sawCloud || !sawLocal {
		t.Fatalf("owner file after unshare = %#v", list.Items)
	}
	if body, err := os.ReadFile(source); err != nil || string(body) != "%PDF-own" {
		t.Fatalf("source = %q %v", body, err)
	}
}

func TestRemovingOwnShareReturnsTheLocalProject(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(identity)+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(`{"messages":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	const first = "cd_0123456789abcdef0123456789abcdef"
	const second = "cd_abcdef0123456789abcdef0123456789"
	for _, token := range []string{first, second} {
		if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
			ID: token, Token: token, Name: "paper.pdf", Path: source, Source: "cloud",
			OpenedAt: "2026-10-25T00:00:00Z", Mine: true, Visibility: "public", Size: 8,
		}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := app.DeleteFileCompanionProject(first, "")
	if err != nil || removed.Restored || removed.Cleared {
		t.Fatalf("one remaining share = %#v %v", removed, err)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("reading data cleared while a share remains: %v", err)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Token != second {
		t.Fatalf("after first unshare = %#v", list.Items)
	}
	removed, err = app.DeleteFileCompanionProject(second, "")
	if err != nil || !removed.Restored || removed.Cleared {
		t.Fatalf("last unshare = %#v %v", removed, err)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("reading data cleared with the share: %v", err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 || list.Items[0].Source == "cloud" {
		t.Fatalf("local after unshare = %v %#v", err, list.Items)
	}
	again, err := app.DeleteFileCompanionProject(second, source)
	if err != nil || !again.Restored || again.Cleared {
		t.Fatalf("stale share delete = %#v %v", again, err)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("stale share delete cleared reading data: %v", err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 || list.Items[0].Source == "cloud" {
		t.Fatalf("local after stale share delete = %v %#v", err, list.Items)
	}
	deleted, err := app.DeleteFileCompanionProject(list.Items[0].ID, source)
	if err != nil || !deleted.Cleared || deleted.Restored {
		t.Fatalf("local delete = %#v %v", deleted, err)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("local delete left reading data: %v", err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("after local delete = %v %#v", err, list.Items)
	}
}

func TestRetiringOneShareOfAFileLeavesTheOtherCloudProject(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	const first = "cd_0123456789abcdef0123456789abcdef"
	const second = "cd_abcdef0123456789abcdef0123456789"
	for _, token := range []string{first, second} {
		if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
			ID: token, Token: token, Name: "paper.pdf", Path: source, Source: "cloud",
			OpenedAt: "2026-10-25T00:00:00Z", Mine: true, Visibility: "public",
		}); err != nil {
			t.Fatal(err)
		}
	}
	app.retireFileCompanionShare(first)
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var sawSecond, sawLocal bool
	for _, item := range list.Items {
		if item.Token == first {
			t.Fatalf("retired share returned: %#v", item)
		}
		if item.Token == second {
			sawSecond = true
		}
		if item.Source != "cloud" && fileCompanionSameLibraryPath(item.Path, source) {
			sawLocal = true
		}
	}
	if !sawSecond || sawLocal {
		t.Fatalf("the other share should remain the only project: %#v", list.Items)
	}
	stored := filepath.Join(app.GetDataDir(), "file-companion", "library.json")
	before, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("opening a cloud project rewrote the library")
	}
}

func TestListCollapsesLocalRowsOfTheSameFile(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-own"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other.pdf")
	if err := os.WriteFile(other, []byte("%PDF-other"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{
		{ID: "local:old", Name: "paper.pdf", Path: source, Source: "local", OpenedAt: "2026-10-25T00:00:00Z", Size: 8},
		{ID: "local:new", Name: "paper.pdf", Path: filepath.Clean(source), Source: "local", OpenedAt: "2026-10-26T00:00:00Z", Size: 8},
		{ID: "local:other", Name: "other.pdf", Path: other, Source: "local", OpenedAt: "2026-10-24T00:00:00Z", Size: 10},
	})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var paper, others int
	for _, item := range list.Items {
		if item.Source != "cloud" && fileCompanionSameLibraryPath(item.Path, source) {
			paper++
			if item.ID != "local:old" {
				t.Fatalf("kept the later duplicate: %#v", item)
			}
		}
		if fileCompanionSameLibraryPath(item.Path, other) {
			others++
		}
	}
	if paper != 1 || others != 1 {
		t.Fatalf("collapsed = %#v", list.Items)
	}
}

func TestDeleteFileCompanionProjectLeavesSharedPathData(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	source := filepath.Join(home, "a.md")
	if err := os.WriteFile(source, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list %#v %v", list.Items, err)
	}
	localID := list.Items[0].ID
	const token = "cd_0123456789abcdef0123456789abcdef"
	fileCompanionLibraryMu.Lock()
	items, _ := app.readFileCompanionLibraryLocked()
	items = append(items, FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.md", Path: source, Source: "cloud", OpenedAt: "2026-10-25T00:00:00Z",
	})
	err = app.writeFileCompanionLibraryLocked(items)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(identity)+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	deleted, err := app.DeleteFileCompanionProject(localID, "")
	if err != nil || deleted.Cleared {
		t.Fatalf("cleared shared path: %#v %v", deleted, err)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("shared transcript removed: %v", err)
	}
	if body, err := os.ReadFile(source); err != nil || string(body) != "alpha" {
		t.Fatalf("source = %q %v", body, err)
	}
}

// The owner's own share whose only copy is the download under
// library-files/<token> goes back to 本机项目 on delete; the share row is
// hidden and the file and its reading data stay.
func TestDeleteFileCompanionCloudProjectHidesShareAndKeepsFile(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares" {
			_, _ = io.WriteString(w, `{"mine":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"me@example.com","size":12}],"received":[]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	home := t.TempDir()
	app := &App{
		testHomeDir:      home,
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	dir := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "shared.pdf")
	body := []byte("%PDF-import")
	if err := os.WriteFile(source, body, 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Path: source, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: true, Visibility: "public",
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(app.GetDataDir(), "file-companion", "results", fileCompanionSessionID(identity), "论文解读.pdf")
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "local.md")
	if err := os.WriteFile(other, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(other); err != nil {
		t.Fatal(err)
	}
	deleted, err := app.DeleteFileCompanionProject(token, "")
	if err != nil || deleted.Cleared || !deleted.Restored {
		t.Fatalf("delete = %#v %v", deleted, err)
	}
	got, err := os.ReadFile(source)
	if err != nil || string(got) != string(body) {
		t.Fatalf("imported file = %q %v", got, err)
	}
	// The copy is the local project now; its reading data is not cleared.
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("derived paper = %v", err)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var sawLocal, sawKept bool
	for _, row := range list.Items {
		if row.Token == token {
			t.Fatalf("dismissed share returned: %#v", list.Items)
		}
		if row.Source == "local" && row.Name == "local.md" {
			sawLocal = true
		}
		if row.Source != "cloud" && fileCompanionSameLibraryPath(row.Path, source) {
			sawKept = true
			if !row.KeptShareCopy {
				t.Fatalf("kept copy row is not marked: %+v", row)
			}
		}
	}
	if !sawLocal {
		t.Fatalf("local row lost: %#v", list.Items)
	}
	if !sawKept {
		t.Fatalf("kept copy did not return to the library: %#v", list.Items)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Path: source, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: true, Visibility: "public",
	})
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var back bool
	for _, row := range list.Items {
		if row.Token == token {
			back = true
		}
	}
	if !back {
		t.Fatalf("share stayed hidden after import: %#v", list.Items)
	}
	got, err = os.ReadFile(source)
	if err != nil || string(got) != string(body) {
		t.Fatalf("file after restore = %q %v", got, err)
	}
}

func fileCompanionProjectZip(t *testing.T, dir string) []byte {
	t.Helper()
	doc := filepath.Join(dir, "shared.pdf")
	if err := os.WriteFile(doc, []byte("%PDF-pulled"), 0o644); err != nil {
		t.Fatal(err)
	}
	paper := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "project.zip")
	if err := writeFileCompanionProject(zipPath, doc, json.RawMessage("[]"), nil, nil, nil, nil, nil, []FileCompanionMessage{{Role: "assistant", Text: "restored chat"}}, paper, 1, nil, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fileCompanionPullDuringDeleteServer(t *testing.T, app *App, token string, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"revision":2,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":12}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			if _, err := app.DeleteFileCompanionProject(token, ""); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("X-Companion-Document-Name", "shared.pdf")
			w.Header().Set("X-Companion-Document-Revision", "2")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDeleteDuringSharePullDoesNotRestoreReadingData(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	server := fileCompanionPullDuringDeleteServer(t, app, token, fileCompanionProjectZip(t, home))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}

	local := filepath.Join(home, "local.md")
	if err := os.WriteFile(local, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(local); err != nil {
		t.Fatal(err)
	}
	localID, err := fileCompanionCanonicalPath(local)
	if err != nil {
		t.Fatal(err)
	}
	localSide := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(localID)+".json")
	if err := os.MkdirAll(filepath.Dir(localSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localSide, []byte(`{"messages":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 0 {
		t.Fatalf("deleted share returned an update: %#v", list.Updates)
	}
	for _, row := range list.Items {
		if row.Token == token {
			t.Fatalf("dismissed share returned: %#v", list.Items)
		}
	}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("pull wrote the document after delete: %v", err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
	if _, err := os.Stat(paper); !os.IsNotExist(err) {
		t.Fatalf("pulled paper stayed: %v", err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("pulled transcript stayed: %v", err)
	}
	assertNoFileCompanionPullStage(t, app)
	if kept, err := os.ReadFile(localSide); err != nil || string(kept) != `{"messages":["keep"]}` {
		t.Fatalf("local transcript = %q %v", kept, err)
	}
	if body, err := os.ReadFile(local); err != nil || string(body) != "local" {
		t.Fatalf("local source = %q %v", body, err)
	}
}

func TestDeleteDuringSharePullKeepsDataStillUsedByAnotherRow(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	server := fileCompanionPullDuringDeleteServer(t, app, token, fileCompanionProjectZip(t, home))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}

	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
	if err := os.MkdirAll(filepath.Dir(paper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paper, []byte("%PDF-kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(`{"messages":["keep-shared"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{
		{
			ID: "local:shared", Name: "shared.pdf", Path: dest, Source: "local",
			OpenedAt: "2026-10-25T00:00:01Z",
		},
		{
			ID: token, Token: token, Name: "shared.pdf", Path: "", Source: "cloud",
			OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
		},
	})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 0 {
		t.Fatalf("deleted share returned an update: %#v", list.Updates)
	}
	var sawLocal bool
	for _, row := range list.Items {
		if row.Token == token {
			t.Fatalf("dismissed share returned: %#v", list.Items)
		}
		if row.Source == "local" && row.Path == dest {
			sawLocal = true
		}
	}
	if !sawLocal {
		t.Fatalf("local row lost: %#v", list.Items)
	}
	if got, err := os.ReadFile(paper); err != nil || string(got) != "%PDF-kept" {
		t.Fatalf("shared paper = %q %v", got, err)
	}
	if got, err := os.ReadFile(side); err != nil || string(got) != `{"messages":["keep-shared"]}` {
		t.Fatalf("shared transcript = %q %v", got, err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "%PDF-old" {
		t.Fatalf("imported file = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func assertNoFileCompanionPullStage(t *testing.T, app *App) {
	t.Helper()
	root := filepath.Join(app.GetDataDir(), "file-companion", "pull-staging")
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			t.Errorf("staged share file remained: %s", path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestSharePullPublishesWhileTheShareStays(t *testing.T) {
	const token = "cd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	body := fileCompanionProjectZip(t, home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"revision":2,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":12}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			w.Header().Set("X-Companion-Document-Name", "shared.pdf")
			w.Header().Set("X-Companion-Document-Revision", "2")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 1 || !list.Updates[0].Project || list.Updates[0].Token != token {
		t.Fatalf("update = %#v", list.Updates)
	}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "%PDF-pulled" {
		t.Fatalf("imported file = %q %v", got, err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
	if got, err := os.ReadFile(paper); err != nil || string(got) != "%PDF-paper" {
		t.Fatalf("paper = %q %v", got, err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	transcript, err := os.ReadFile(side)
	if err != nil || !strings.Contains(string(transcript), "restored chat") {
		t.Fatalf("transcript = %q %v", transcript, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestSharePullDiscardsStagingWhenImportWins(t *testing.T) {
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	body := fileCompanionProjectZip(t, home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"revision":2,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":12}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			app.bumpFileCompanionShareEpoch(token)
			dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(dest, []byte("%PDF-import"), 0o644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			identity, err := fileCompanionCanonicalPath(dest)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			sessionID := fileCompanionSessionID(identity)
			paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
			if err := os.MkdirAll(filepath.Dir(paper), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(paper, []byte("%PDF-import-paper"), 0o644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
			if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(side, []byte(`{"messages":["import chat"]}`), 0o644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("X-Companion-Document-Name", "shared.pdf")
			w.Header().Set("X-Companion-Document-Revision", "2")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}

	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Path: dest, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 0 {
		t.Fatalf("stale pull returned an update: %#v", list.Updates)
	}
	var saw bool
	for _, row := range list.Items {
		if row.Token == token {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("imported share disappeared: %#v", list.Items)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-import" {
		t.Fatalf("imported file = %q %v", got, err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	if got, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")); err != nil || string(got) != "%PDF-import-paper" {
		t.Fatalf("paper = %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")); err != nil || string(got) != `{"messages":["import chat"]}` {
		t.Fatalf("transcript = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestImportPublishHoldsTheLibraryLock(t *testing.T) {
	const token = "cd_cccccccccccccccccccccccccccccccc"
	app := &App{testHomeDir: t.TempDir()}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	importStage, err := app.stageFileCompanionShare(token, "shared.pdf", []byte("%PDF-import"))
	if err != nil {
		t.Fatal(err)
	}
	pullStage, err := app.stageFileCompanionShare(token, "shared.pdf", []byte("%PDF-pulled"))
	if err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	epochBefore := fileCompanionShareEpoch[token]
	fileCompanionLibraryMu.Unlock()

	releaseCh := make(chan struct{})
	released := false
	release := func() {
		if !released {
			released = true
			close(releaseCh)
		}
	}
	defer release()
	entered := make(chan struct{})
	fileCompanionSharePublishHook = func() {
		close(entered)
		<-releaseCh
	}
	defer func() { fileCompanionSharePublishHook = nil }()

	errCh := make(chan error, 1)
	go func() {
		_, callErr := app.commitStagedFileCompanionShare(importStage, 0, true, nil)
		errCh <- callErr
	}()
	<-entered
	locked := fileCompanionLibraryMu.TryLock()
	if locked {
		fileCompanionLibraryMu.Unlock()
	}
	release()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	fileCompanionSharePublishHook = nil
	if locked {
		t.Fatal("import published without holding the library lock")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-import" {
		t.Fatalf("import file = %q %v", got, err)
	}
	if _, err := app.commitStagedFileCompanionShare(pullStage, epochBefore, false, nil); !errors.Is(err, errFileCompanionSharePullDropped) {
		t.Fatalf("stale pull = %v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-import" {
		t.Fatalf("pull replaced the import = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestSharePullSkipsARevisionAlreadyOnDisk(t *testing.T) {
	const token = "cd_dddddddddddddddddddddddddddddddd"
	app := &App{testHomeDir: t.TempDir()}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("%PDF-have"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Path: dest, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 4,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	item := FileCompanionLibraryItem{Token: token, Source: "cloud", Revision: 4}
	if _, skip := app.fileCompanionSharePullPermit(item); !skip {
		t.Fatal("current revision was downloaded again")
	}
	item.Revision = 5
	if _, skip := app.fileCompanionSharePullPermit(item); skip {
		t.Fatal("newer revision was skipped")
	}
	fileCompanionLibraryMu.Lock()
	_, err = app.dismissFileCompanionShareLocked(token)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	item.Revision = 9
	if _, skip := app.fileCompanionSharePullPermit(item); !skip {
		t.Fatal("dismissed share was downloaded")
	}
}

func TestDeletePurgesBeforeAConcurrentImportPublishes(t *testing.T) {
	const token = "cd_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(`{"messages":["old"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paper, []byte("%PDF-old-paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Path: identity, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stage, err := app.stageFileCompanionShare(token, "shared.pdf", fileCompanionProjectZip(t, home))
	if err != nil {
		t.Fatal(err)
	}

	releaseCh := make(chan struct{})
	released := false
	release := func() {
		if !released {
			released = true
			close(releaseCh)
		}
	}
	defer release()
	entered := make(chan struct{})
	fileCompanionProjectPurgeHook = func() {
		close(entered)
		<-releaseCh
	}
	defer func() { fileCompanionProjectPurgeHook = nil }()

	type deleteResult struct {
		out FileCompanionProjectDelete
		err error
	}
	delCh := make(chan deleteResult, 1)
	go func() {
		out, callErr := app.DeleteFileCompanionProject(token, "")
		delCh <- deleteResult{out: out, err: callErr}
	}()
	<-entered
	if got, err := os.ReadFile(side); err != nil || string(got) != `{"messages":["old"]}` {
		t.Fatalf("purge hook ran after the transcript was removed: %q %v", got, err)
	}
	locked := fileCompanionLibraryMu.TryLock()
	if locked {
		fileCompanionLibraryMu.Unlock()
	}
	item := FileCompanionLibraryItem{
		ID: token, Token: token, Name: "shared.pdf", Source: "cloud",
		OpenedAt: "2026-10-25T00:00:01Z", Visibility: "public", Revision: 2, Permanent: true,
	}
	commitErr := make(chan error, 1)
	go func() {
		_, callErr := app.commitStagedFileCompanionShare(stage, 0, true, &item)
		commitErr <- callErr
	}()
	release()
	deleted := <-delCh
	if deleted.err != nil {
		t.Fatal(deleted.err)
	}
	if !deleted.out.Cleared {
		t.Fatalf("delete did not clear the only row: %+v", deleted.out)
	}
	if err := <-commitErr; err != nil {
		t.Fatal(err)
	}
	fileCompanionProjectPurgeHook = nil
	if locked {
		t.Fatal("delete purged without holding the library lock")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-pulled" {
		t.Fatalf("imported file = %q %v", got, err)
	}
	if got, err := os.ReadFile(paper); err != nil || string(got) != "%PDF-paper" {
		t.Fatalf("imported paper = %q %v", got, err)
	}
	if got, err := os.ReadFile(side); err != nil || !strings.Contains(string(got), "restored chat") {
		t.Fatalf("imported transcript = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestSharePullKeepsThePreviousRevisionWhenTheDownloadFails(t *testing.T) {
	const token = "cd_ffffffffffffffffffffffffffffffff"
	app := &App{testHomeDir: t.TempDir()}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("%PDF-have"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"revision":7,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":12}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			http.Error(w, "unavailable", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Path: dest, Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 3,
		NotesRevision: 4,
		Notes:         []FileCompanionShareNote{{ID: "cn_keep", Text: "keep-note"}},
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 0 {
		t.Fatalf("failed download returned an update: %#v", list.Updates)
	}
	var row FileCompanionLibraryItem
	for _, item := range list.Items {
		if item.Token == token {
			row = item
		}
	}
	if row.Token != token || row.Revision != 3 || row.Path != dest || row.NotesRevision != 4 || len(row.Notes) != 1 || row.Notes[0].Text != "keep-note" {
		t.Fatalf("failed download stuck the hub revision: %+v", row)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-have" {
		t.Fatalf("file after failed download = %q %v", got, err)
	}
	if _, skip := app.fileCompanionSharePullPermit(FileCompanionLibraryItem{Token: token, Source: "cloud", Revision: 7}); skip {
		t.Fatal("failed download was treated as stored")
	}
}

func TestDeleteDuringSharePublishClearsTheInstalledTranscript(t *testing.T) {
	const token = "cd_11111111111111111111111111111111"
	home := t.TempDir()
	app := &App{testHomeDir: home}
	body := fileCompanionProjectZip(t, home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[{"token":"`+token+`","name":"shared.pdf","visibility":"public","permanent":true,"revision":2,"url":"https://hub.example/hub/documents/shares/`+token+`","created_at":"2026-10-25T00:00:00Z","owner":"other@example.com","size":12}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/import"):
			w.Header().Set("X-Companion-Document-Name", "shared.pdf")
			w.Header().Set("X-Companion-Document-Revision", "2")
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}

	local := filepath.Join(home, "local.md")
	if err := os.WriteFile(local, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(local); err != nil {
		t.Fatal(err)
	}
	localID, err := fileCompanionCanonicalPath(local)
	if err != nil {
		t.Fatal(err)
	}
	localSide := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(localID)+".json")
	if err := os.MkdirAll(filepath.Dir(localSide), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localSide, []byte(`{"messages":["keep"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{{
		ID: token, Token: token, Name: "shared.pdf", Source: "cloud",
		OpenedAt: "2026-10-25T00:00:00Z", Mine: false, Visibility: "public", Revision: 1,
	}})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	var deleted FileCompanionProjectDelete
	var deleteErr error
	fileCompanionShareCommitHook = func() {
		deleted, deleteErr = app.DeleteFileCompanionProject(token, "")
	}
	defer func() { fileCompanionShareCommitHook = nil }()

	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if !deleted.Cleared || strings.TrimSpace(deleted.Path) == "" {
		t.Fatalf("delete did not see the published file: %+v", deleted)
	}
	if len(list.Updates) != 0 {
		t.Fatalf("deleted share returned an update: %#v", list.Updates)
	}
	for _, row := range list.Items {
		if row.Token == token {
			t.Fatalf("dismissed share returned: %#v", list.Items)
		}
	}
	dest := filepath.Join(app.GetDataDir(), "file-companion", "library-files", token, "shared.pdf")
	if got, err := os.ReadFile(dest); err != nil || string(got) != "%PDF-pulled" {
		t.Fatalf("source copy = %q %v", got, err)
	}
	identity, err := fileCompanionCanonicalPath(dest)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(identity)
	paper := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "论文解读.pdf")
	if _, err := os.Stat(paper); !os.IsNotExist(err) {
		t.Fatalf("published paper stayed after delete: %v", err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("published transcript stayed after delete: %v", err)
	}
	if got, err := os.ReadFile(localSide); err != nil || string(got) != `{"messages":["keep"]}` {
		t.Fatalf("local transcript = %q %v", got, err)
	}
	assertNoFileCompanionPullStage(t, app)
}

func TestLibraryItemsFromSharesKeepsForeignHubOrigin(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	items := libraryItemsFromShares([]fileCompanionShareView{{
		Token:       token,
		Name:        "群组.pdf",
		Visibility:  "group",
		URL:         "http://home.example/hub/documents/shares/" + token,
		HubOrigin:   "https://origin.example/hub",
		ReaderGrant: "cgr_" + strings.Repeat("ab", 16),
	}}, false, "https://home.example")
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	if items[0].HubOrigin != "https://origin.example" {
		t.Fatalf("hub origin %q", items[0].HubOrigin)
	}
	if items[0].ShareURL != "https://origin.example/hub/documents/shares/"+token {
		t.Fatalf("share url %q", items[0].ShareURL)
	}
	fresh := items[0]
	fresh.HubOrigin = ""
	merged := mergeFileCompanionSharesWithRevoked([]FileCompanionLibraryItem{items[0]}, []FileCompanionLibraryItem{fresh}, "https://home.example", nil, nil, nil)
	if len(merged) != 1 || merged[0].HubOrigin != "https://origin.example" {
		t.Fatalf("merge dropped the foreign hub: %+v", merged)
	}
}

func TestFileCompanionShareOrigin(t *testing.T) {
	link := "https://other.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef"
	origin, foreign := fileCompanionShareOrigin(link, "https://home.example")
	if !foreign || origin != "https://other.example" {
		t.Fatalf("origin = %q foreign = %v", origin, foreign)
	}
	if _, foreign := fileCompanionShareOrigin("https://home.example:443/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", "https://home.example"); foreign {
		t.Fatal("default port should stay on the logged-in Hub")
	}
	if _, foreign := fileCompanionShareOrigin("cd_0123456789abcdef0123456789abcdef", "https://home.example"); foreign {
		t.Fatal("a bare token stays on the logged-in Hub")
	}
	if origin := fileCompanionHubOrigin("https://user:secret@other.example/hub/documents/shares/cd_x"); origin != "" {
		t.Fatalf("credentials in the link must not be a Hub origin: %s", origin)
	}
}

func TestImportFileCompanionShareUsesTheLinkHub(t *testing.T) {
	const token = "cd_abcdef0123456789abcdef0123456789"
	var homeImport, foreignAuth, foreignURL string
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = io.WriteString(w, `{"mine":[],"received":[],"notes_reply":true,"notes_review":true}`)
		case r.URL.Path == "/api/mobile/documents/shares/"+token:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"SHARE_NOT_FOUND","message":"分享链接不存在"}`)
		default:
			homeImport = r.Method + " " + r.URL.Path
			http.NotFound(w, r)
		}
	}))
	defer home.Close()
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/import":
			foreignAuth = r.Header.Get("Authorization")
			if r.Header.Get("X-Companion-Reader-Grant") != "" && foreignAuth != "" {
				t.Errorf("grant and home session were both sent")
			}
			w.Header().Set("X-Companion-Document-Name", "跨hub.pdf")
			w.Header().Set("X-Companion-Document-Owner", "owner@other.hub")
			w.Header().Set("X-Companion-Document-Visibility", "public")
			w.Header().Set("X-Companion-Document-Revision", "1")
			w.Header().Set("X-Companion-Reader-Grant", "cgr_0123456789abcdef0123456789abcdef")
			_, _ = w.Write([]byte("%PDF-foreign"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares/"+token:
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("probe sent home session %q", got)
			}
			_, _ = io.WriteString(w, `{"token":"`+token+`","name":"跨hub.pdf","owner":"owner@other.hub","visibility":"public","permanent":true,"revision":1,"url":"`+foreignURL+`/hub/documents/shares/`+token+`","notes":[{"id":"cn_1","author":"owner@other.hub","text":"主持人的批注","created_at":"2026-10-28T00:00:00Z"}],"notes_revision":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/notes":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("note sent home session %q", got)
			}
			if r.Header.Get("X-Companion-Reader-Grant") != "cgr_0123456789abcdef0123456789abcdef" {
				t.Errorf("note grant = %q", r.Header.Get("X-Companion-Reader-Grant"))
			}
			_, _ = io.WriteString(w, `{"notes":[{"id":"cn_2","author":"reader@example.com","text":"跨 hub 的问题","created_at":"2026-10-28T00:00:01Z"}],"notes_revision":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer foreign.Close()
	foreignURL = foreign.URL

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      home.URL,
			RemoteViewerToken: "home-secret",
			RemoteEmail:       "reader@example.com",
		},
	}
	imported, err := app.ImportFileCompanionShare(foreign.URL+"/hub/documents/shares/"+token, "")
	if err != nil {
		t.Fatal(err)
	}
	if homeImport != "" {
		t.Fatalf("home Hub was asked for the foreign share: %s", homeImport)
	}
	if foreignAuth != "" {
		t.Fatalf("foreign Hub received the home session: %s", foreignAuth)
	}
	if imported.HubOrigin != foreign.URL || imported.Name != "跨hub.pdf" {
		t.Fatalf("imported = %#v", imported)
	}
	if app.fileCompanionShareGrant(token) != "cgr_0123456789abcdef0123456789abcdef" {
		t.Fatalf("grant = %q", app.fileCompanionShareGrant(token))
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var row FileCompanionLibraryItem
	for _, item := range list.Items {
		if item.Token == token {
			row = item
		}
	}
	if row.Path == "" || row.HubOrigin != foreign.URL {
		t.Fatalf("list dropped the foreign share: %#v notice %q", list.Items, list.Notice)
	}
	if len(row.Notes) != 1 || row.Notes[0].Text != "主持人的批注" {
		t.Fatalf("notes = %#v", row.Notes)
	}
	noted, err := app.addFileCompanionShareNoteByToken(token, "跨 hub 的问题", "")
	if err != nil {
		t.Fatal(err)
	}
	if noted.NotesRevision != 2 || len(noted.Notes) != 1 || noted.Notes[0].Text != "跨 hub 的问题" {
		t.Fatalf("noted = %#v", noted)
	}
}

func TestSyncFileCompanionShareMarksPushesOnlyMarks(t *testing.T) {
	const (
		hostToken = "cd_0123456789abcdef0123456789abcdef"
		goneToken = "cd_abcdef0123456789abcdef0123456789"
	)
	var app *App
	var openedPath string
	var pushed []string
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/marks") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/mobile/documents/shares/"), "/marks")
		if token == goneToken {
			http.Error(w, `{"message":"分享链接不存在"}`, http.StatusNotFound)
			return
		}
		pushed = append(pushed, token)
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":"%s","revision":4,"marks_revision":7}`, token)
	}))
	defer server.Close()
	app = &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	openedPath = opened.Path
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: hostToken, Token: hostToken, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: true, Host: true, Revision: 4,
	})
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: goneToken, Token: goneToken, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: true, Host: true, Revision: 2,
	})
	marks := `{"highlights":[{"id":"m1","page":1}],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[]}`
	result, err := app.SyncFileCompanionShareMarks(openedPath, marks)
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed) != 1 || pushed[0] != hostToken {
		t.Fatalf("pushed = %v, want only the live host share", pushed)
	}
	if len(bodies) != 1 || bodies[0] != marks {
		t.Fatalf("marks body = %q", bodies)
	}
	if result.MarksRevision != 7 {
		t.Fatalf("marks revision = %d", result.MarksRevision)
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("library missing")
	}
	byToken := map[string]FileCompanionLibraryItem{}
	for _, item := range stored {
		byToken[item.Token] = item
	}
	if row := byToken[hostToken]; row.MarksRevision != 7 {
		t.Fatalf("stored marks revision = %+v", row)
	}
	if _, exists := byToken[goneToken]; exists {
		t.Fatalf("gone share was not retired: %+v", byToken[goneToken])
	}
}

func TestFetchFileCompanionShareMarksReadsTheBlob(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	marksBody := `{"highlights":[{"id":"m1","page":1}],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[{"id":"b1","page":2,"x":0.1,"y":0.2,"color":"yellow","name":"重点"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/marks" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer viewer-token" {
			http.Error(w, `{"message":"请先登录"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Companion-Marks-Revision", "9")
		_, _ = io.WriteString(w, marksBody)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "group", Mine: false, Revision: 4,
	})
	got, err := app.FetchFileCompanionShareMarks(opened.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Marks != marksBody || got.Revision != 9 {
		t.Fatalf("marks = %q revision = %d", got.Marks, got.Revision)
	}
}

func TestFetchFileCompanionShareMarksBeforeTheFileIsOpen(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	marksBody := `{"highlights":[{"id":"m1","page":1}],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/marks" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer viewer-token" {
			http.Error(w, `{"message":"请先登录"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Companion-Marks-Revision", "4")
		_, _ = io.WriteString(w, marksBody)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: canonical,
		Source: "cloud", Visibility: "group", Mine: true, Host: true, HostKnown: true, Revision: 2, MarksRevision: 4,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := app.FetchFileCompanionShareMarks(source)
	if err != nil {
		t.Fatal(err)
	}
	if got.Marks != marksBody || got.Revision != 4 {
		t.Fatalf("marks = %q revision = %d", got.Marks, got.Revision)
	}
}

func TestFetchFileCompanionShareMarksUsesTheOriginHub(t *testing.T) {
	const token = "cd_abcdef0123456789abcdef0123456789"
	const grant = "cgr_0123456789abcdef0123456789abcdef"
	marksBody := `{"highlights":[{"id":"m9","page":3}],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[]}`
	var homeHit string
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		homeHit = r.Method + " " + r.URL.Path
		http.NotFound(w, r)
	}))
	defer home.Close()
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/marks" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("origin received the home session %q", got)
		}
		if r.Header.Get("X-Companion-Reader-Grant") != grant {
			t.Errorf("grant = %q", r.Header.Get("X-Companion-Reader-Grant"))
		}
		w.Header().Set("X-Companion-Marks-Revision", "6")
		_, _ = io.WriteString(w, marksBody)
	}))
	defer foreign.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      home.URL,
			RemoteViewerToken: "home-secret",
		},
	}
	source := filepath.Join(t.TempDir(), "跨hub.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "跨hub.pdf", Path: canonical,
		Source: "cloud", Visibility: "group", Mine: false, HubOrigin: foreign.URL, Revision: 1, MarksRevision: 6,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveFileCompanionShareGrant(token, grant); err != nil {
		t.Fatal(err)
	}
	got, err := app.FetchFileCompanionShareMarks(source)
	if err != nil {
		t.Fatal(err)
	}
	if homeHit != "" {
		t.Fatalf("home hub was asked for the marks: %s", homeHit)
	}
	if got.Marks != marksBody || got.Revision != 6 {
		t.Fatalf("marks = %q revision = %d", got.Marks, got.Revision)
	}
}

func saveFileCompanionShareRefreshCheck(t *testing.T) {
	t.Helper()
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	saved := fileCompanionShareRefreshState{
		hub:         st.hub,
		seq:         st.seq,
		remoteSeq:   st.remoteSeq,
		lastFull:    st.lastFull,
		fullDone:    st.fullDone,
		unsupported: st.unsupported,
		hadLogin:    st.hadLogin,
	}
	st.mu.Unlock()
	t.Cleanup(func() {
		st.mu.Lock()
		st.hub = saved.hub
		st.seq = saved.seq
		st.remoteSeq = saved.remoteSeq
		st.lastFull = saved.lastFull
		st.fullDone = saved.fullDone
		st.unsupported = saved.unsupported
		st.hadLogin = saved.hadLogin
		st.mu.Unlock()
	})
}

func primeFileCompanionShareRefreshCheck(hub string, seq, remoteSeq uint64) {
	st := &fileCompanionShareRefreshCheck
	st.mu.Lock()
	st.reset(hub)
	st.fullDone = true
	st.hadLogin = true
	st.lastFull = time.Now()
	st.seq = seq
	st.remoteSeq = remoteSeq
	st.mu.Unlock()
}

func TestFileCompanionShareRefreshQuietKeepsTheNotice(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	app := &App{}
	const hub = "https://hub.example"
	fileCompanionShareRefreshCheck.mu.Lock()
	fileCompanionShareRefreshCheck.reset(hub)
	fileCompanionShareRefreshCheck.mu.Unlock()
	quiet, notice, _ := app.fileCompanionShareRefreshQuiet(hub, "viewer", false)
	if quiet || notice != "" {
		t.Fatalf("first signed-out refresh quiet=%v notice=%q", quiet, notice)
	}

	primeFileCompanionShareRefreshCheck(hub, 0, 0)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(hub, "viewer", false)
	if !quiet || notice != fileCompanionLibraryLoginNotice {
		t.Fatalf("signed-out quiet=%v notice=%q", quiet, notice)
	}

	changes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/documents/shares/changes" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"seq":7,"remote_seq":3}`)
	}))
	defer changes.Close()
	primeFileCompanionShareRefreshCheck(changes.URL, 7, 3)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(changes.URL, "viewer", true)
	if !quiet || notice != "" {
		t.Fatalf("unchanged quiet=%v notice=%q", quiet, notice)
	}
	primeFileCompanionShareRefreshCheck(changes.URL, 6, 3)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(changes.URL, "viewer", true)
	if quiet || notice != "" {
		t.Fatalf("changed sequence quiet=%v notice=%q", quiet, notice)
	}

	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()
	primeFileCompanionShareRefreshCheck(downURL, 1, 0)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(downURL, "viewer", true)
	if !quiet || notice != fileCompanionLibraryOfflineNotice {
		t.Fatalf("offline quiet=%v notice=%q", quiet, notice)
	}

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"请先登录 MaClaw Hub"}`)
	}))
	defer denied.Close()
	primeFileCompanionShareRefreshCheck(denied.URL, 1, 0)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(denied.URL, "viewer", true)
	if !quiet || notice != "请先登录 MaClaw Hub" {
		t.Fatalf("status quiet=%v notice=%q", quiet, notice)
	}

	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer old.Close()
	primeFileCompanionShareRefreshCheck(old.URL, 1, 0)
	quiet, notice, _ = app.fileCompanionShareRefreshQuiet(old.URL, "viewer", true)
	if quiet || notice != "" {
		t.Fatalf("old hub quiet=%v notice=%q", quiet, notice)
	}
	fileCompanionShareRefreshCheck.mu.Lock()
	unsupported := fileCompanionShareRefreshCheck.unsupported
	fileCompanionShareRefreshCheck.mu.Unlock()
	if !unsupported {
		t.Fatal("old hub was not remembered")
	}
}

func saveFileCompanionShareDiscussionSync(t *testing.T) {
	t.Helper()
	st := &fileCompanionShareDiscussionSyncState
	st.mu.Lock()
	var savedNotes map[string]fileCompanionDiscussionShare
	if st.notes != nil {
		savedNotes = make(map[string]fileCompanionDiscussionShare, len(st.notes))
		for token, share := range st.notes {
			savedNotes[token] = share
		}
	}
	saved := fileCompanionShareDiscussionSync{
		hub:         st.hub,
		seq:         st.seq,
		fullDone:    st.fullDone,
		unsupported: st.unsupported,
		hadLogin:    st.hadLogin,
		lastMine:    st.lastMine,
		notes:       savedNotes,
		backlog:     st.backlog,
	}
	st.mu.Unlock()
	t.Cleanup(func() {
		st.mu.Lock()
		st.hub = saved.hub
		st.seq = saved.seq
		st.fullDone = saved.fullDone
		st.unsupported = saved.unsupported
		st.hadLogin = saved.hadLogin
		st.lastMine = saved.lastMine
		st.notes = saved.notes
		st.backlog = saved.backlog
		st.mu.Unlock()
	})
}

// The library refresh and the discussion watcher poll the same share list.
// The host pushed a new chat line between their two fetches, so the watcher
// saw a newer ETag than the library did. Each poll must record the sequence
// of its own response: a shared watermark let the watcher's newer response
// cover the library's older one, the 5-second pre-check then read "nothing
// changed", and the participant waited for the 10-minute full refresh to see
// the host's line.
func TestFileCompanionShareListWatermarksStayPerPoller(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	saveFileCompanionShareDiscussionSync(t)
	const token = "cd_0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	seq := uint64(7)
	chatRevision := 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		currentSeq, currentChat := seq, chatRevision
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares":
			w.Header().Set("ETag", `"`+strconv.FormatUint(currentSeq, 10)+`"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"notes_review":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"chat_revision":%d,"created_at":"2026-10-25T00:00:00Z"}]}`, token, currentChat)
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":%d,"remote_seq":0,"shares":[]}`, currentSeq)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	hub := server.URL
	bump := func(nextSeq uint64, nextChat int) {
		mu.Lock()
		seq = nextSeq
		chatRevision = nextChat
		mu.Unlock()
	}

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      hub,
			RemoteViewerToken: "viewer-token",
		},
	}
	paper := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Revision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	fileCompanionShareRefreshCheck.mu.Lock()
	fileCompanionShareRefreshCheck.reset(hub)
	fileCompanionShareRefreshCheck.mu.Unlock()
	fileCompanionShareDiscussionSyncState.mu.Lock()
	fileCompanionShareDiscussionSyncState.reset(hub)
	fileCompanionShareDiscussionSyncState.mu.Unlock()

	// The library refresh fetches at seq 7. The host publishes a new chat
	// line and the discussion watcher fetches the newer list while that
	// refresh is still finishing; only then does the refresh record its
	// watermark.
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	bump(8, 4)
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	noteFileCompanionShareFullRefresh(hub)
	fileCompanionShareRefreshCheck.mu.Lock()
	librarySeq := fileCompanionShareRefreshCheck.seq
	fileCompanionShareRefreshCheck.mu.Unlock()
	fileCompanionShareDiscussionSyncState.mu.Lock()
	discussionSeq := fileCompanionShareDiscussionSyncState.seq
	fileCompanionShareDiscussionSyncState.mu.Unlock()
	if librarySeq != 7 {
		t.Fatalf("library watermark = %d, want the sequence of its own response (7)", librarySeq)
	}
	if discussionSeq != 8 {
		t.Fatalf("discussion watermark = %d, want 8", discussionSeq)
	}
	if quiet, notice, _ := app.fileCompanionShareRefreshQuiet(hub, "viewer-token", true); quiet {
		t.Fatalf("the host's new chat was swallowed by the watcher's watermark: quiet=%v notice=%q", quiet, notice)
	}
	// The refresh that pre-check asks for lands the new transcript sequence.
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	noteFileCompanionShareFullRefresh(hub)
	found := false
	for _, item := range listed.Items {
		if item.Token == token {
			found = true
			if item.ChatRevision != 4 {
				t.Fatalf("chat revision = %d, want 4", item.ChatRevision)
			}
		}
	}
	if !found {
		t.Fatalf("share row missing: %+v", listed.Items)
	}
	if quiet, notice, _ := app.fileCompanionShareRefreshQuiet(hub, "viewer-token", true); !quiet || notice != "" {
		t.Fatalf("caught-up refresh quiet=%v notice=%q", quiet, notice)
	}

	// The other direction: a library response must not cover the watcher's
	// watermark either, or a reader note waits for the next idle poll.
	bump(9, 5)
	if _, err := app.pollFileCompanionShareDiscussions(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	noteFileCompanionShareFullRefresh(hub)
	fileCompanionShareDiscussionSyncState.mu.Lock()
	discussionSeq = fileCompanionShareDiscussionSyncState.seq
	fileCompanionShareDiscussionSyncState.mu.Unlock()
	if discussionSeq != 9 {
		t.Fatalf("discussion watermark after a library refresh = %d, want 9", discussionSeq)
	}
}

// The discussion watcher answers notes. A host transcript moves the same
// global sequence and used to make every signed-in machine download the full
// share list, notes included, on the next tick.
func TestFileCompanionShareDiscussionChatSkipsTheList(t *testing.T) {
	saveFileCompanionShareDiscussionSync(t)
	savedFailures := fileCompanionShareDiscussionFailures
	savedReady := fileCompanionShareDiscussionReady
	fileCompanionShareDiscussionFailures = map[string]time.Time{}
	fileCompanionShareDiscussionReady = map[string]fileCompanionShareDiscussionReadyReply{}
	t.Cleanup(func() {
		fileCompanionShareDiscussionFailures = savedFailures
		fileCompanionShareDiscussionReady = savedReady
	})
	const token = "cd_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	var listCalls int
	var mu sync.Mutex
	seq := uint64(8)
	notes := 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentSeq, currentNotes := seq, notes
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":%d,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":%d,"marks_revision":1,"chat_revision":4,"visibility":"public","expires_at":"","target":"","group_id":"","host":false,"pending_host":false}]}`, currentSeq, token, currentNotes)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("ETag", `"`+strconv.FormatUint(currentSeq, 10)+`"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"notes_review":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":%d,"chat_revision":4,"created_at":"2026-10-25T00:00:00Z"}]}`, token, currentNotes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	st := &fileCompanionShareDiscussionSyncState
	st.mu.Lock()
	st.hub = server.URL
	st.seq = 7
	st.fullDone = true
	st.hadLogin = true
	st.lastMine = 1
	st.notes = map[string]fileCompanionDiscussionShare{token: {Notes: 2, HostKnown: true}}
	st.mu.Unlock()

	if got := app.pollFileCompanionShareDiscussionsQuiet(); got != 1 {
		t.Fatalf("chat-only poll count = %d, want the last hosted count", got)
	}
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls != 0 {
		t.Fatalf("chat-only change fetched the share list %d times", calls)
	}
	st.mu.Lock()
	gotSeq := st.seq
	st.mu.Unlock()
	if gotSeq != 8 {
		t.Fatalf("discussion sequence = %d, want 8", gotSeq)
	}

	st.mu.Lock()
	st.backlog = true
	st.mu.Unlock()
	if got := app.pollFileCompanionShareDiscussionsQuiet(); got != 0 {
		t.Fatalf("backlog poll count = %d, want the hosted shares in the list", got)
	}
	mu.Lock()
	calls = listCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("a capped note poll fetched the share list %d times", calls)
	}

	mu.Lock()
	seq = 9
	notes = 3
	mu.Unlock()
	if got := app.pollFileCompanionShareDiscussionsQuiet(); got != 0 {
		t.Fatalf("note poll count = %d, want the hosted shares in the list", got)
	}
	mu.Lock()
	calls = listCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("a new note fetched the share list %d times", calls)
	}
}

func TestFileCompanionShareNotesMoved(t *testing.T) {
	savedFailures := fileCompanionShareDiscussionFailures
	savedReady := fileCompanionShareDiscussionReady
	fileCompanionShareDiscussionFailures = map[string]time.Time{}
	fileCompanionShareDiscussionReady = map[string]fileCompanionShareDiscussionReadyReply{}
	t.Cleanup(func() {
		fileCompanionShareDiscussionFailures = savedFailures
		fileCompanionShareDiscussionReady = savedReady
	})
	const token = "cd_ffffffffffffffffffffffffffffffff"
	host := false
	known := map[string]fileCompanionDiscussionShare{token: {Notes: 2, HostKnown: true}}
	changes := fileCompanionShareChanges{HasShares: true, Shares: []fileCompanionShareChange{{
		Token: token, NotesRevision: 2, ChatRevision: 4, Host: &host,
	}}}
	if fileCompanionShareNotesMoved(changes, known) {
		t.Fatal("a transcript update was treated as a note change")
	}
	host = true
	if !fileCompanionShareNotesMoved(changes, known) {
		t.Fatal("a host transfer was not treated as a note-poll change")
	}
	host = false
	if fileCompanionShareDiscussionRetryDue(time.Now()) {
		t.Fatal("no failed answer was due")
	}
	fileCompanionShareDiscussionFailures[token] = time.Now().Add(-fileCompanionShareDiscussionFailureHold - time.Second)
	if fileCompanionShareDiscussionRetryDue(time.Now()) {
		t.Fatal("a model failure with no saved reply woke the share list")
	}
	fileCompanionShareDiscussionReady[token] = fileCompanionShareDiscussionReadyReply{
		Text: "已写好的回复", At: time.Now(),
	}
	if !fileCompanionShareDiscussionRetryDue(time.Now()) {
		t.Fatal("a saved reply was not due after the hold")
	}
}

func waitFileCompanionLibrarySync(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !fileCompanionLibrarySyncRunning.Load() && !fileCompanionShareLiveOnce.Load() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("library sync did not finish")
}

func libraryChatRevision(t *testing.T, app *App, token string) int {
	t.Helper()
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := app.readFileCompanionLibraryLocked()
	if !ok {
		t.Fatal("library unreadable")
	}
	for _, item := range items {
		if item.Token == token {
			return item.ChatRevision
		}
	}
	t.Fatalf("share %s missing from the library", token)
	return 0
}

// The host's new line only moves chat_revision. The participant's poll used to
// treat that as "fetch the whole share list, probe revocations, maybe download
// the project", so the open chat stayed on the old transcript until that work
// finished — or for the ten-minute reconcile, when a newer watermark had
// already been recorded. The changes feed already carries the counter. The
// poll has to publish it and let the open chat fetch the blob.
func TestFileCompanionShareChatPublishesWithoutTheFullList(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"visibility":"public","expires_at":"","target":"reader@example.com","group_id":"","host":false,"pending_host":false}]}`, token)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"created_at":"2026-10-25T00:00:00Z"}]}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Host: false, HostKnown: true, Revision: 1,
		NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 0)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls != 0 {
		t.Fatalf("chat-only change fetched the share list %d times", calls)
	}
	if got := libraryChatRevision(t, app, token); got != 4 {
		t.Fatalf("chat revision = %d, want 4", got)
	}
	fileCompanionShareRefreshCheck.mu.Lock()
	seq := fileCompanionShareRefreshCheck.seq
	fileCompanionShareRefreshCheck.mu.Unlock()
	if seq != 8 {
		t.Fatalf("watermark = %d, want 8", seq)
	}
	if quiet, notice, _ := app.fileCompanionShareRefreshQuiet(server.URL, "viewer-token", true); !quiet || notice != "" {
		t.Fatalf("after the counter landed quiet=%v notice=%q", quiet, notice)
	}
}

// A host line on a share that lives on another Hub used to move only
// remote_seq, so the participant still waited for the full list. The changes
// feed now carries that share's counters, and chat publishes from them.
func TestFileCompanionShareRemoteChatPublishesWithoutTheFullList(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_ffffffffffffffffffffffffffffffff"
	const origin = "https://other.example"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":5,"shares":[],"remote_shares":[{"token":%q,"hub_origin":%q,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4}]}`, token, origin)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":5,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"group","permanent":true,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"hub_origin":%q,"created_at":"2026-10-25T00:00:00Z"}]}`, token, origin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "group", HubOrigin: origin, Host: false, HostKnown: true,
		Revision: 1, NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 4)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls != 0 {
		t.Fatalf("cross-hub chat fetched the share list %d times", calls)
	}
	if got := libraryChatRevision(t, app, token); got != 4 {
		t.Fatalf("chat revision = %d, want 4", got)
	}
	fileCompanionShareRefreshCheck.mu.Lock()
	seq := fileCompanionShareRefreshCheck.seq
	remoteSeq := fileCompanionShareRefreshCheck.remoteSeq
	fileCompanionShareRefreshCheck.mu.Unlock()
	if seq != 8 || remoteSeq != 5 {
		t.Fatalf("watermark = %d/%d, want 8/5", seq, remoteSeq)
	}
}

// A foreign share the changes feed does not list (a public import, or a group
// past the snapshot cap) must not turn a home chat into a full refresh.
func TestFileCompanionShareChatSkipsWhenAForeignShareIsOutsideTheFeed(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const homeToken = "cd_11111111111111111111111111111111"
	const foreignToken = "cd_22222222222222222222222222222222"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":4,"shares":[{"token":%q,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"visibility":"public","expires_at":"","target":"","group_id":"","host":false,"pending_host":false}],"remote_shares":[]}`, homeToken)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":4,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"created_at":"2026-10-25T00:00:00Z"}]}`, homeToken)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	homePaper := filepath.Join(app.testHomeDir, "a.pdf")
	foreignPaper := filepath.Join(app.testHomeDir, "b.pdf")
	if err := os.WriteFile(homePaper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignPaper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: homeToken, Token: homeToken, Name: "a.pdf", Path: homePaper, Source: "cloud",
		Visibility: "public", Host: false, HostKnown: true, Revision: 1,
		NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: foreignToken, Token: foreignToken, Name: "b.pdf", Path: foreignPaper, Source: "cloud",
		Visibility: "public", HubOrigin: "https://other.example", Host: false, HostKnown: true,
		Revision: 1, NotesRevision: 1, MarksRevision: 1, ChatRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 4)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls != 0 {
		t.Fatalf("home chat fetched the share list %d times because a foreign share was outside the feed", calls)
	}
	if got := libraryChatRevision(t, app, homeToken); got != 4 {
		t.Fatalf("chat revision = %d, want 4", got)
	}
}

// A note or a new document still needs the share list. The transcript counter
// is written first, so the chat does not wait on that list.
func TestFileCompanionShareNotesStillRefreshTheList(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":3,"marks_revision":1,"chat_revision":4}]}`, token)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("ETag", `"8"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":3,"marks_revision":1,"chat_revision":4,"created_at":"2026-10-25T00:00:00Z"}]}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Revision: 1, NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 0)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls == 0 {
		t.Fatal("a new note did not refresh the share list")
	}
	if got := libraryChatRevision(t, app, token); got != 4 {
		t.Fatalf("chat revision = %d, want 4", got)
	}
}

// A project download can take long enough that the host publishes another
// line while it is still running. The next poll has to publish that line
// before the download returns.
func TestFileCompanionShareChatPublishesWhileARefreshIsBlocked(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	release := make(chan struct{})
	var closeRelease sync.Once
	var server *httptest.Server
	t.Cleanup(func() {
		closeRelease.Do(func() { close(release) })
		waitFileCompanionLibrarySync(t)
		if server != nil {
			server.Close()
		}
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_cccccccccccccccccccccccccccccccc"
	var mu sync.Mutex
	seq := uint64(2)
	chatRevision := 1
	notesRevision := 5
	entered := make(chan struct{})
	var once sync.Once
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		currentSeq, currentChat, currentNotes := seq, chatRevision, notesRevision
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":%d,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":%d,"marks_revision":0,"chat_revision":%d}]}`, currentSeq, token, currentNotes, currentChat)
		case "/api/mobile/documents/shares":
			once.Do(func() { close(entered) })
			<-release
			mu.Lock()
			currentSeq, currentChat, currentNotes = seq, chatRevision, notesRevision
			mu.Unlock()
			w.Header().Set("ETag", `"`+strconv.FormatUint(currentSeq, 10)+`"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":%d,"chat_revision":%d,"created_at":"2026-10-25T00:00:00Z"}]}`, token, currentNotes, currentChat)
		default:
			http.NotFound(w, r)
		}
	}))
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Revision: 1, NotesRevision: 0, ChatRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 1, 0)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("share list was not requested")
	}
	mu.Lock()
	seq = 3
	chatRevision = 2
	mu.Unlock()
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if libraryChatRevision(t, app, token) == 2 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("chat revision while the share list was blocked = %d, want 2", libraryChatRevision(t, app, token))
}

// A sequence bump that does not move the transcript still has to fetch the
// share list. Host transfer, audience, and expiry live in that list, and the
// counters alone cannot tell them from an unrelated sequence bump.
func TestFileCompanionShareSequenceWithoutChatStillRefreshes(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":3,"visibility":"public","expires_at":"","target":"","group_id":"","pending_host":false}]}`, token)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("ETag", `"8"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"public","permanent":true,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":3,"created_at":"2026-10-25T00:00:00Z"}]}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Revision: 1, NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 0)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls == 0 {
		t.Fatal("an unexplained sequence bump skipped the share list")
	}
}

// A host line that lands in the same snapshot as an audience edit publishes
// the transcript immediately and still fetches the share list.
func TestFileCompanionShareChatWithAudienceStillRefreshes(t *testing.T) {
	saveFileCompanionShareRefreshCheck(t)
	prevInline := fileCompanionLibraryListInline
	fileCompanionLibraryListInline = false
	t.Cleanup(func() {
		waitFileCompanionLibrarySync(t)
		fileCompanionLibraryListInline = prevInline
	})
	const token = "cd_ffffffffffffffffffffffffffffffff"
	var listCalls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mobile/documents/shares/changes":
			_, _ = fmt.Fprintf(w, `{"seq":8,"remote_seq":0,"shares":[{"token":%q,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"visibility":"private","expires_at":"","target":"","group_id":"","pending_host":false}]}`, token)
		case "/api/mobile/documents/shares":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("ETag", `"8"`)
			_, _ = fmt.Fprintf(w, `{"notes_reply":true,"remote_seq":0,"mine":[],"received":[{"token":%q,"name":"a.pdf","owner":"host@example.com","host":false,"visibility":"private","permanent":true,"revision":1,"notes_revision":2,"marks_revision":1,"chat_revision":4,"created_at":"2026-10-25T00:00:00Z"}]}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Revision: 1, NotesRevision: 2, MarksRevision: 1, ChatRevision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	primeFileCompanionShareRefreshCheck(server.URL, 7, 0)
	if _, err := app.ListFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	waitFileCompanionLibrarySync(t)
	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls == 0 {
		t.Fatal("an audience edit that arrived with a chat line skipped the share list")
	}
	if got := libraryChatRevision(t, app, token); got != 4 {
		t.Fatalf("chat revision = %d, want 4", got)
	}
}

func TestFileCompanionShareChangeNeedsList(t *testing.T) {
	item := FileCompanionLibraryItem{
		Visibility: "public", Revision: 1, NotesRevision: 2, ChatRevision: 3,
		HostKnown: true, Host: false,
	}
	row := fileCompanionShareChange{Revision: 1, NotesRevision: 2, ChatRevision: 4}
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("a chat counter was treated as a share-list change")
	}
	visibility := "public"
	expires := ""
	target := ""
	group := ""
	host := false
	pending := false
	row.Visibility = &visibility
	row.ExpiresAt = &expires
	row.Target = &target
	row.GroupID = &group
	row.Host = &host
	row.PendingHost = &pending
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("an unchanged share identity was treated as a share-list change")
	}
	private := "private"
	row.Visibility = &private
	if !fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("an audience edit was not treated as a share-list change")
	}
	row.Visibility = &visibility
	host = true
	if !fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("a host transfer was not treated as a share-list change")
	}
	item.HostKnown = false
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("an unknown host seat turned a chat line into a share-list change")
	}
	item.HostKnown = true
	host = false
	item.Admin = false
	readerTarget := "reader@example.com"
	row.Target = &readerTarget
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("a participant row treated the admin-only invite target as a change")
	}
	item.Admin = true
	if !fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("an admin row ignored an invite target edit")
	}
	item.ExpiresAt = "2026-11-10T00:00:00Z"
	sameInstant := "2026-11-10T08:00:00+08:00"
	row.ExpiresAt = &sameInstant
	row.Target = &target
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("the same expiry spelled with an offset was treated as a change")
	}
	blank := ""
	row.Visibility = &blank
	if fileCompanionShareChangeNeedsList(row, item) {
		t.Fatal("an empty audience on an older share was treated as a change from public")
	}
}

func TestReconcileFileCompanionLibraryKeepsNewerChat(t *testing.T) {
	const token = "cd_dddddddddddddddddddddddddddddddd"
	fresh := []FileCompanionLibraryItem{{
		ID: token, Token: token, Source: "cloud", Path: "C:/a.pdf",
		Revision: 1, ChatRevision: 5, MarksRevision: 3, OpenedAt: "2026-10-25T00:00:00Z",
	}}
	snapshot := []FileCompanionLibraryItem{{
		ID: token, Token: token, Source: "cloud", Path: "C:/a.pdf",
		Revision: 1, ChatRevision: 4, MarksRevision: 2, OpenedAt: "2026-10-25T00:00:00Z",
	}}
	got := reconcileFileCompanionLibrary(fresh, snapshot, nil)
	if len(got) != 1 || got[0].ChatRevision != 5 || got[0].MarksRevision != 3 {
		t.Fatalf("older snapshot replaced the live transcript: %+v", got)
	}
	fresh[0].Revision = 2
	snapshot[0].ChatRevision = 6
	snapshot[0].MarksRevision = 4
	got = reconcileFileCompanionLibrary(fresh, snapshot, nil)
	if len(got) != 1 || got[0].Revision != 2 || got[0].ChatRevision != 6 || got[0].MarksRevision != 4 {
		t.Fatalf("newer document revision dropped a newer transcript: %+v", got)
	}
}
