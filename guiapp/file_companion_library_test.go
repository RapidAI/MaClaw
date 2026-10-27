package guiapp

import (
	"archive/zip"
	"bytes"
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

	const importedToken = "cd_0123456789abcdef0123456789abcdef"
	fileCompanionLibraryMu.Lock()
	epochBefore := fileCompanionShareEpoch[importedToken]
	fileCompanionLibraryMu.Unlock()
	imported, err := app.ImportFileCompanionShare("https://hub.example/hub/documents/shares/"+importedToken, "secret")
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
	if _, err := app.DeleteFileCompanionProject(token); err != nil {
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
	deleted, err := app.DeleteFileCompanionProject(list.Items[0].ID)
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
	if _, err := app.DeleteFileCompanionProject(deleted.ID); err == nil {
		t.Fatal("second delete succeeded")
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
	deleted, err := app.DeleteFileCompanionProject(localID)
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
	deleted, err := app.DeleteFileCompanionProject(token)
	if err != nil || !deleted.Cleared {
		t.Fatalf("delete = %#v %v", deleted, err)
	}
	got, err := os.ReadFile(source)
	if err != nil || string(got) != string(body) {
		t.Fatalf("imported file = %q %v", got, err)
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatalf("derived paper = %v", err)
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var sawLocal bool
	for _, row := range list.Items {
		if row.Token == token || row.Path == source {
			t.Fatalf("dismissed share returned: %#v", list.Items)
		}
		if row.Source == "local" && row.Name == "local.md" {
			sawLocal = true
		}
	}
	if !sawLocal {
		t.Fatalf("local row lost: %#v", list.Items)
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
	if err := writeFileCompanionProject(zipPath, doc, json.RawMessage("[]"), []FileCompanionMessage{{Role: "assistant", Text: "restored chat"}}, paper, 1); err != nil {
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
			if _, err := app.DeleteFileCompanionProject(token); err != nil {
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
		out, callErr := app.DeleteFileCompanionProject(token)
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
		deleted, deleteErr = app.DeleteFileCompanionProject(token)
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
