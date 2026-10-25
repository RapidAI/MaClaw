package guiapp

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	var postedName, postedVisibility, postedDays, postedPassword string
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
	if err := companionShareBodyIsProject(postedBody, "%PDF-"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ShareFileCompanionDocument(source, "public", "", "", "", -1); err == nil || !strings.Contains(err.Error(), "分享时间不正确") {
		t.Fatalf("days = %v", err)
	}
	if _, err := app.ShareFileCompanionDocument(source, "private", "ab", "", "", 1); err == nil || !strings.Contains(err.Error(), "分享密码需为 4–64 个字符") {
		t.Fatalf("password = %v", err)
	}

	imported, err := app.ImportFileCompanionShare("https://hub.example/hub/documents/shares/cd_0123456789abcdef0123456789abcdef", "secret")
	if err != nil {
		t.Fatal(err)
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
}

func TestUpsertFileCompanionCloudKeepsANewerNoteRevision(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	app := &App{testHomeDir: t.TempDir()}
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Source: "cloud", Path: "C:/paper.pdf", Revision: 4,
		NotesRevision: 6, OpenedAt: "2026-10-25T00:00:00Z",
		Notes: []FileCompanionShareNote{{ID: "cn_new", Text: "后来的讨论"}},
	})
	// A sync that started before the note returns the old note list and the
	// new document revision.
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	got := mergeFileCompanionShares(local, remote, "https://hub.example")
	if len(got) != 1 || got[0].Revision != 4 || got[0].Path != paper || got[0].NotesRevision != 3 || len(got[0].Notes) != 1 || got[0].Notes[0].Text != "后来的讨论" {
		t.Fatalf("merge covered a newer note with an older list: %+v", got)
	}
	remote[0].NotesKnown = false
	remote[0].Notes = nil
	remote[0].NotesRevision = 0
	got = mergeFileCompanionShares(local, remote, "https://hub.example")
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
			app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
			app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: other, Token: other, Name: "other.pdf", Path: otherPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 3,
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: received, Token: received, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "group", Mine: false, Revision: 4,
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: olderPath,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: other, Token: other, Name: "other.pdf", Path: otherPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 3,
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: received, Token: received, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: false, Revision: 4,
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "person", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: missing, Token: missing, Name: "论文.pdf", Path: openedPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 5,
		NotesRevision: 2,
		Notes:         []FileCompanionShareNote{{ID: "cn_miss", Author: "a@example.com", Text: "已取消的批注"}},
	})
	app.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	stale.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: older, Token: older, Name: "论文.pdf", Path: plainPath,
		Source: "cloud", Visibility: "group", Mine: true, Revision: 2,
		NotesRevision: 6,
		Notes:         []FileCompanionShareNote{{ID: "cn_old", Author: "reader@example.com", Text: "后来的讨论"}},
	})
	stale.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	onlyExpired.upsertFileCompanionCloud(FileCompanionLibraryItem{
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
	dropped := map[string]bool{}
	removed := app.dropRevokedGroupCopies(previous, nil, dropped)
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
