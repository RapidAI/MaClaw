package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/fsnotify/fsnotify"
)

func TestKnowledgeFolderWatchImportsChangesAndRemoveStopsWatch(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	home := t.TempDir()
	app := &App{testHomeDir: home}
	t.Cleanup(app.stopKnowledgeFolderWatch)

	dataDir := app.GetDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.KnowledgeAddFolderWatch(dataDir); err == nil || !strings.Contains(err.Error(), "application data directory") {
		t.Fatalf("data dir error = %v", err)
	}

	root := filepath.Join(t.TempDir(), "notes")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "secret.md"), []byte("kfwsecretmoduletoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "not-a-folder.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.KnowledgeAddFolderWatch(filePath); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("file error = %v", err)
	}

	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if added.ID == "" || !pathEqual(added.Path, root) {
		t.Fatalf("added = %+v", added)
	}
	again, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != added.ID {
		t.Fatalf("duplicate id = %s, want %s", again.ID, added.ID)
	}
	if _, err := app.KnowledgeAddFolderWatch(nested); err == nil || !strings.Contains(err.Error(), "already covered by") {
		t.Fatalf("nested error = %v", err)
	}

	seen := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwsecretmoduletoken") {
		t.Fatalf("node_modules file was imported: %s", text)
	}

	note := filepath.Join(root, "note.md")
	if err := os.WriteFile(note, []byte("# Notes\n\nkfwalphatoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen = waitKnowledgeWatchText(t, app, added.ID, "kfwalphatoken", seen.LastImportAt)

	if err := os.WriteFile(note, []byte("# Notes\n\nkfwbetatoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwbetatoken", seen.LastImportAt)

	if err := os.WriteFile(filepath.Join(root, "skip.tmp"), []byte("kfwtmpsecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwtmpsecret") {
		t.Fatalf("temporary file was imported: %s", text)
	}

	if err := app.KnowledgeRemoveFolderWatch(added.ID); err != nil {
		t.Fatal(err)
	}
	items, err := app.KnowledgeListFolderWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("watches after remove = %+v", items)
	}
	if text := knowledgeWatchIndexedText(t, app); !strings.Contains(text, "kfwbetatoken") {
		t.Fatalf("remove deleted imported knowledge: %s", text)
	}

	gamma := filepath.Join(root, "gamma.md")
	if err := os.WriteFile(gamma, []byte("kfwgammatoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	app.importKnowledgeFolderWatchFile(gamma)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwgammatoken") {
		t.Fatalf("removed folder kept importing: %s", text)
	}
	if err := app.KnowledgeRemoveFolderWatch(added.ID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("second remove error = %v", err)
	}

	reloaded := &App{testHomeDir: home}
	t.Cleanup(reloaded.stopKnowledgeFolderWatch)
	reloadedItems, err := reloaded.KnowledgeListFolderWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedItems) != 0 {
		t.Fatalf("persisted watches = %+v", reloadedItems)
	}
}

func TestKnowledgeFolderWatchChildAttachFailureKeepsRoot(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})
	if _, _, err := app.attachKnowledgeFolderWatchDirs(added.ID, filepath.Join(root, "missing-child"), false); err == nil {
		t.Fatal("missing child attach succeeded")
	}
	if !app.knowledgeFolderWatchRootAttached(added.ID, root) {
		t.Fatal("failed child attach removed the watched root")
	}
	if err := os.WriteFile(filepath.Join(root, "still.md"), []byte("kfwkeepwatchtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwkeepwatchtoken", seen.LastImportAt)
}

func TestKnowledgeFolderWatchImportsNewSubdirectory(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})
	sub := filepath.Join(root, "chapter")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "page.md"), []byte("kfwsubtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwsubtoken", seen.LastImportAt)

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "secret.md"), []byte("kfwlatermoduletoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwlatermoduletoken") {
		t.Fatalf("node_modules change was imported: %s", text)
	}
}

func TestKnowledgeFolderWatchReclaimsDirectorySlots(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	previousMax := knowledgeFolderWatchMaxDirs
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	knowledgeFolderWatchMaxDirs = 2
	t.Cleanup(func() {
		knowledgeFolderWatchDebounce = previousDebounce
		knowledgeFolderWatchMaxDirs = previousMax
	})

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})

	childA := filepath.Join(root, "a")
	if err := os.MkdirAll(childA, 0o755); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchDir(t, app, added.ID, childA, true)

	childB := filepath.Join(root, "b")
	if err := os.MkdirAll(childB, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if app.knowledgeFolderWatchRootAttached(added.ID, childB) {
		t.Fatal("directory cap allowed another child")
	}

	app.forgetKnowledgeFolderWatchPath(childA)
	waitKnowledgeWatchDir(t, app, added.ID, childA, false)
	if !app.knowledgeFolderWatchRootAttached(added.ID, root) {
		t.Fatal("forgetting a child dropped the watched root")
	}

	childC := filepath.Join(root, "c")
	if err := os.MkdirAll(childC, 0o755); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchDir(t, app, added.ID, childC, true)
	if err := os.WriteFile(filepath.Join(childC, "page.md"), []byte("kfwreclaimtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwreclaimtoken", time.Time{})
}

func TestKnowledgeFolderWatchResumeMissingFolder(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.md"), []byte("kfwoldtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwoldtoken", time.Time{})

	app.forgetKnowledgeFolderWatchPath(root)
	missing := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return item.Status == knowledgeFolderWatchStatusMissing
	})
	if app.knowledgeFolderWatchRootAttached(added.ID, root) {
		t.Fatal("missing folder stayed attached")
	}
	if err := os.WriteFile(filepath.Join(root, "new.md"), []byte("kfwresumetoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumed, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != added.ID {
		t.Fatalf("resume id = %s, want %s", resumed.ID, added.ID)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwresumetoken", missing.LastImportAt)
	items, err := app.KnowledgeListFolderWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status == knowledgeFolderWatchStatusMissing {
		t.Fatalf("resumed watch = %+v", items)
	}
}

func TestKnowledgeFolderWatchDeleteBurstStillImports(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})

	const n = 80
	for i := 0; i < n; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("d%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if !app.knowledgeFolderWatchRootAttached(added.ID, root) {
		t.Fatal("root watch was not attached")
	}
	for i := 0; i < n; i++ {
		if err := os.RemoveAll(filepath.Join(root, fmt.Sprintf("d%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "after.md"), []byte("kfwbursttoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwbursttoken", seen.LastImportAt)
}

func TestKnowledgeFolderWatchSkipsDataDirInsideParent(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	previousBase := corelib.MaclawBaseDir()
	t.Cleanup(func() {
		knowledgeFolderWatchDebounce = previousDebounce
		corelib.SetMaclawBaseDir(previousBase)
	})

	home := t.TempDir()
	app := &App{testHomeDir: home}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	base := filepath.Join(root, "appdata")
	dataDir := filepath.Join(base, "data")
	if err := os.MkdirAll(filepath.Join(home, ".maclaw"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"data_dir": base})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".maclaw", "config.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if !pathEqual(app.GetDataDir(), dataDir) {
		t.Fatalf("data dir = %s, want %s", app.GetDataDir(), dataDir)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secret.md"), []byte("kfwdatadirtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.md"), []byte("kfwdataoktoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if app.knowledgeFolderWatchRootAttached(added.ID, dataDir) {
		t.Fatal("application data directory was watched")
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwdataoktoken", time.Time{})
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwdatadirtoken") {
		t.Fatalf("application data directory was imported: %s", text)
	}
}

func TestKnowledgeFolderWatchDirKeyFoldsWindowsCase(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("windows path identity")
	}
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})
	flipped := foldKnowledgeWatchCase(root)
	if flipped == root || !pathEqual(flipped, root) {
		t.Fatalf("flipped path = %q, root = %q", flipped, root)
	}
	st := app.folderWatch()
	st.mu.Lock()
	before := len(st.dirs)
	st.mu.Unlock()
	_, addedCount, err := app.attachKnowledgeFolderWatchDirs(added.ID, flipped, false)
	if err != nil {
		t.Fatal(err)
	}
	if addedCount != 0 {
		t.Fatalf("case variant added %d directories", addedCount)
	}
	st.mu.Lock()
	after := len(st.dirs)
	st.mu.Unlock()
	if after != before {
		t.Fatalf("watched directories = %d, want %d", after, before)
	}
	if err := os.WriteFile(filepath.Join(root, "case.md"), []byte("kfwcasetoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwcasetoken", seen.LastImportAt)
}

func TestKnowledgeFolderWatchFileReplaceSurvivesStaleForget(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(root, "note.md")
	if err := os.WriteFile(note, []byte("kfwbeforetoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeWatchText(t, app, added.ID, "kfwbeforetoken", time.Time{})

	if err := os.WriteFile(note, []byte("kfwreplacetoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen = waitKnowledgeWatchText(t, app, added.ID, "kfwreplacetoken", seen.LastImportAt)

	// The write event has already been imported. Schedule the same file again,
	// then run the forget that was queued while the path was missing. No later
	// filesystem event will put it back.
	app.scheduleKnowledgeFolderWatchImport(note)
	app.forgetKnowledgeFolderWatchPath(note)
	seen = waitKnowledgeWatchText(t, app, added.ID, "kfwreplacetoken", seen.LastImportAt)
	if seen.Status == knowledgeFolderWatchStatusMissing {
		t.Fatalf("file replace marked the folder missing: %+v", seen)
	}

	edited := filepath.Join(root, "edited.md")
	if err := os.WriteFile(edited, []byte("kfweditedold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen = waitKnowledgeWatchText(t, app, added.ID, "kfweditedold", seen.LastImportAt)
	tmp := edited + ".saving"
	if err := os.WriteFile(tmp, []byte("kfweditednew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(edited); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, edited); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfweditednew", seen.LastImportAt)
}

func TestKnowledgeFolderWatchRenameKeepsSiblingPrefix(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("windows fsnotify renames sibling watches that share a path prefix")
	}
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	doc := filepath.Join(root, "doc")
	doctor := filepath.Join(root, "doctor")
	if err := os.MkdirAll(doc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(doctor, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doctor, "keep.md"), []byte("kfwprefixold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen := waitKnowledgeWatchText(t, app, added.ID, "kfwprefixold", time.Time{})

	archive := filepath.Join(root, "archive")
	if err := os.Rename(doc, archive); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doctor, "next.md"), []byte("kfwprefixnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "moved.md"), []byte("kfwprefixmoved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwprefixnew", seen.LastImportAt)
	waitKnowledgeWatchText(t, app, added.ID, "kfwprefixmoved", seen.LastImportAt)
}

func TestKnowledgeFolderWatchOverflowReconcilesMissedFiles(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	waitKnowledgeFolderWatch(t, app, added.ID, func(item KnowledgeFolderWatch) bool {
		return !item.LastImportAt.IsZero()
	})

	st := app.folderWatch()
	st.watchMu.Lock()
	removeErr := st.watcher.Remove(root)
	st.watchMu.Unlock()
	if removeErr != nil {
		t.Fatal(removeErr)
	}
	missed := filepath.Join(root, "missed.md")
	if err := os.WriteFile(missed, []byte("kfwoverflowtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwoverflowtoken") {
		t.Fatal("missed file was imported through a removed watch")
	}
	app.handleKnowledgeFolderWatchError(errors.New("watcher read failed"))
	time.Sleep(300 * time.Millisecond)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwoverflowtoken") {
		t.Fatal("unrelated watcher error imported files")
	}
	app.handleKnowledgeFolderWatchError(fsnotify.ErrEventOverflow)
	waitKnowledgeWatchText(t, app, added.ID, "kfwoverflowtoken", time.Time{})
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "page.md"), []byte("kfwoverflowdirtoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	app.handleKnowledgeFolderWatchError(errors.New("Windows system assumed buffer larger than it is, events have likely been missed"))
	waitKnowledgeWatchText(t, app, added.ID, "kfwoverflowdirtoken", time.Time{})
	if err := os.WriteFile(filepath.Join(nested, "later.md"), []byte("kfwoverflowlater\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwoverflowlater", time.Time{})
}

func TestKnowledgeFolderWatchRecreatedDirRestoresWatch(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	child := filepath.Join(root, "chapter")
	section := filepath.Join(child, "section")
	if err := os.MkdirAll(section, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "old.md"), []byte("kfwreviveold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwreviveold", time.Time{})

	// One subtree watch covers chapter. Dropping that root watch is what a
	// missed remove leaves behind: the map still lists the folder.
	st := app.folderWatch()
	st.watchMu.Lock()
	removeErr := st.watcher.Remove(root)
	st.watchMu.Unlock()
	if removeErr != nil {
		t.Fatal(removeErr)
	}
	if err := os.WriteFile(filepath.Join(child, "page.md"), []byte("kfwrevivetoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(section, "deep.md"), []byte("kfwrevivedeep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwrevivetoken") || strings.Contains(text, "kfwrevivedeep") {
		t.Fatal("file was imported after the kernel watch was removed")
	}
	app.handleKnowledgeFolderWatchEvent(fsnotify.Event{Name: child, Op: fsnotify.Create})
	seen := waitKnowledgeWatchText(t, app, added.ID, "kfwrevivetoken", time.Time{})
	waitKnowledgeWatchText(t, app, added.ID, "kfwrevivedeep", time.Time{})
	if err := os.WriteFile(filepath.Join(child, "next.md"), []byte("kfwrevivenext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(section, "later.md"), []byte("kfwrevivelater\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwrevivenext", seen.LastImportAt)
	waitKnowledgeWatchText(t, app, added.ID, "kfwrevivelater", seen.LastImportAt)
}

func TestKnowledgeFolderWatchRenameReleasesOldDirectory(t *testing.T) {
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	chapter := filepath.Join(root, "chapter")
	section := filepath.Join(chapter, "section")
	if err := os.MkdirAll(section, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chapter, "old.md"), []byte("kfwrenameold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwrenameold", time.Time{})
	before := knowledgeWatchDirCount(app, added.ID)

	archive := filepath.Join(root, "archive")
	if err := os.Rename(chapter, archive); err != nil {
		t.Fatal(err)
	}
	if got := knowledgeWatchDirCount(app, added.ID); got != before {
		t.Fatalf("watch slots %d, was %d", got, before)
	}
	if err := os.WriteFile(filepath.Join(archive, "section", "page.md"), []byte("kfwrenametoken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwrenametoken", time.Time{})
}

func TestKnowledgeFolderWatchSplitsAroundSkippedDirectory(t *testing.T) {
	if !knowledgeFolderWatchSupportsRecurse() {
		t.Skip("subtree watches")
	}
	previousDebounce := knowledgeFolderWatchDebounce
	knowledgeFolderWatchDebounce = 40 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchDebounce = previousDebounce })

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.stopKnowledgeFolderWatch)
	root := filepath.Join(t.TempDir(), "notes")
	chapter := filepath.Join(root, "chapter")
	if err := os.MkdirAll(chapter, 0o755); err != nil {
		t.Fatal(err)
	}
	added, err := app.KnowledgeAddFolderWatch(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chapter, "old.md"), []byte("kfwsplitold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwsplitold", time.Time{})
	attached, recursive := knowledgeWatchDirMode(app, added.ID, root)
	if !attached || !recursive {
		t.Fatalf("root watch attached=%v recursive=%v", attached, recursive)
	}
	if childAttached, _ := knowledgeWatchDirMode(app, added.ID, chapter); childAttached {
		t.Fatal("clean subtree opened a child directory")
	}

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "secret.md"), []byte("kfwsplitsecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchDirMode(t, app, added.ID, root, true, false)
	waitKnowledgeWatchDir(t, app, added.ID, chapter, true)
	if modulesAttached, _ := knowledgeWatchDirMode(app, added.ID, filepath.Join(root, "node_modules")); modulesAttached {
		t.Fatal("node_modules was watched")
	}
	if text := knowledgeWatchIndexedText(t, app); strings.Contains(text, "kfwsplitsecret") {
		t.Fatalf("skipped directory was imported: %s", text)
	}
	if err := os.WriteFile(filepath.Join(chapter, "next.md"), []byte("kfwsplitnext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeWatchText(t, app, added.ID, "kfwsplitnext", time.Time{})
}

func TestKnowledgeFolderWatchProgressReportsCurrentFile(t *testing.T) {
	previous := knowledgeFolderWatchProgressMinInterval
	knowledgeFolderWatchProgressMinInterval = 300 * time.Millisecond
	t.Cleanup(func() { knowledgeFolderWatchProgressMinInterval = previous })

	app := &App{testHomeDir: t.TempDir()}
	st := app.folderWatch()
	t.Cleanup(func() {
		st.mu.Lock()
		for id, mark := range st.progressMark {
			if mark.timer != nil {
				mark.timer.Stop()
			}
			delete(st.progressMark, id)
		}
		st.mu.Unlock()
	})
	st.mu.Lock()
	st.started = true
	st.items = []KnowledgeFolderWatch{{
		ID:            "kfw_doc",
		Path:          filepath.Join(t.TempDir(), "missing-doc"),
		Status:        knowledgeFolderWatchStatusImporting,
		CurrentFile:   "stale.docx",
		TotalFiles:    99,
		ImportedFiles: 7,
	}}
	st.mu.Unlock()

	app.noteKnowledgeFolderWatchImportProgress("kfw_doc", knowledge.DirectoryImportResult{
		TotalFiles:     4,
		ProcessedFiles: 1,
		ImportedFiles:  1,
		CurrentFile:    "notes/chapter.docx",
		CurrentStep:    "parsing",
		StepProgress:   40,
	})
	got := knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentFile != "notes/chapter.docx" || got.TotalFiles != 4 || got.ProcessedFiles != 1 || got.CurrentStep != "parsing" || got.StepProgress != 40 || got.ImportedFiles != 1 {
		t.Fatalf("progress = %+v", got)
	}
	if _, err := os.Stat(app.knowledgeFolderWatchStorePath()); !os.IsNotExist(err) {
		t.Fatalf("progress tick persisted the watch file: %v", err)
	}

	app.noteKnowledgeFolderWatchImportProgress("kfw_doc", knowledge.DirectoryImportResult{
		TotalFiles:     4,
		ProcessedFiles: 2,
		ImportedFiles:  2,
		CurrentFile:    "notes/next.docx",
		CurrentStep:    "distilling",
		StepProgress:   10,
	})
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentFile != "notes/next.docx" || got.ProcessedFiles != 2 || got.CurrentStep != "distilling" {
		t.Fatalf("throttled progress = %+v", got)
	}

	app.noteKnowledgeFolderWatchScanProgress("kfw_doc", "walk", 6, 0, filepath.Join("doc", "scan.md"))
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentStep != "scanning" || got.TotalFiles != 0 || got.ProcessedFiles != 6 || got.CurrentFile != filepath.Join("doc", "scan.md") {
		t.Fatalf("scan progress = %+v", got)
	}
	app.noteKnowledgeFolderWatchScanProgress("kfw_doc", "hash", 2, 6, filepath.Join("doc", "hash.md"))
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentStep != "hashing" || got.TotalFiles != 6 || got.ProcessedFiles != 2 || got.CurrentFile != filepath.Join("doc", "hash.md") {
		t.Fatalf("hash progress = %+v", got)
	}

	root := t.TempDir()
	st.mu.Lock()
	st.items[0].Path = root
	st.mu.Unlock()
	app.noteKnowledgeFolderWatchScanProgress("kfw_doc", "walk", 4, 4, filepath.Join(root, "nested", "chapter.docx"))
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentStep != "scanning" || got.TotalFiles != 0 || got.ProcessedFiles != 4 || got.CurrentFile != filepath.Join("nested", "chapter.docx") {
		t.Fatalf("walk progress = %+v", got)
	}
	st.mu.Lock()
	st.items[0].Path = filepath.Join(t.TempDir(), "missing-doc")
	st.mu.Unlock()

	loaded := KnowledgeFolderWatch{Status: knowledgeFolderWatchStatusImporting, CurrentFile: "a.md", TotalFiles: 3, ImportedFiles: 2}
	normalizeLoadedKnowledgeFolderWatch(&loaded)
	if loaded.Status != knowledgeFolderWatchStatusWatching || loaded.CurrentFile != "" || loaded.TotalFiles != 0 || loaded.ImportedFiles != 2 {
		t.Fatalf("loaded watch = %+v", loaded)
	}

	app.noteKnowledgeFolderWatchImportProgress("kfw_other", knowledge.DirectoryImportResult{CurrentFile: "nope.md", TotalFiles: 1})
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentFile == "nope.md" {
		t.Fatalf("foreign progress applied: %+v", got)
	}

	st.mu.Lock()
	st.items[0].Status = knowledgeFolderWatchStatusWatching
	st.mu.Unlock()
	app.noteKnowledgeFolderWatchImportProgress("kfw_doc", knowledge.DirectoryImportResult{CurrentFile: "late.md", TotalFiles: 9, ProcessedFiles: 9})
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentFile == "late.md" || got.TotalFiles == 9 {
		t.Fatalf("late progress applied after import: %+v", got)
	}

	app.beginKnowledgeFolderWatchImport("kfw_doc")
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.Status != knowledgeFolderWatchStatusImporting || got.CurrentFile != "" || got.CurrentStep != "" || got.TotalFiles != 0 || got.ProcessedFiles != 0 || got.StepProgress != 0 {
		t.Fatalf("begin did not clear live progress: %+v", got)
	}
	if got.ImportedFiles != 2 {
		t.Fatalf("begin cleared the in-progress import summary: %+v", got)
	}
	if _, err := os.Stat(app.knowledgeFolderWatchStorePath()); !os.IsNotExist(err) {
		t.Fatalf("begin persisted the watch file: %v", err)
	}

	st.mu.Lock()
	st.items[0].CurrentFile = "scan.md"
	st.items[0].TotalFiles = 6
	st.items[0].ProcessedFiles = 6
	st.mu.Unlock()
	app.finishKnowledgeFolderWatchImport("kfw_doc", filepath.Join("doc", "scan.md"), knowledge.DirectoryImportResult{
		ImportedFiles:  2,
		SkippedFiles:   1,
		FailedFiles:    0,
		TotalFiles:     6,
		ProcessedFiles: 6,
		CurrentFile:    "scan.md",
	}, nil)
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.Status != knowledgeFolderWatchStatusMissing {
		t.Fatalf("status = %s, want missing", got.Status)
	}
	if got.CurrentFile != "" || got.CurrentStep != "" || got.TotalFiles != 0 || got.ProcessedFiles != 0 || got.StepProgress != 0 {
		t.Fatalf("finished progress not cleared: %+v", got)
	}
	if got.ImportedFiles != 2 || got.SkippedFiles != 1 || got.LastFile == "" {
		t.Fatalf("finished summary = %+v", got)
	}
	app.noteKnowledgeFolderWatchImportProgress("kfw_doc", knowledge.DirectoryImportResult{CurrentFile: "after.md", TotalFiles: 3})
	got = knowledgeWatchItem(t, app, "kfw_doc")
	if got.CurrentFile != "" || got.Status != knowledgeFolderWatchStatusMissing {
		t.Fatalf("progress after finish = %+v", got)
	}
}

func knowledgeWatchItem(t *testing.T, app *App, id string) KnowledgeFolderWatch {
	t.Helper()
	items, err := app.KnowledgeListFolderWatches()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("watch %s not found in %+v", id, items)
	return KnowledgeFolderWatch{}
}

func knowledgeWatchDirCount(app *App, id string) int {
	st := app.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	count := 0
	for _, entry := range st.dirs {
		if entry.id == id {
			count++
		}
	}
	return count
}

func knowledgeWatchDirMode(app *App, id, dir string) (bool, bool) {
	st := app.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	entry, ok := st.dirs[knowledgeFolderWatchDirKey(dir)]
	if !ok || entry.id != id {
		return false, false
	}
	return true, entry.recursive
}

func waitKnowledgeWatchDirMode(t *testing.T, app *App, id, dir string, wantAttached, wantRecursive bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		attached, recursive := knowledgeWatchDirMode(app, id, dir)
		if attached == wantAttached && recursive == wantRecursive {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	attached, recursive := knowledgeWatchDirMode(app, id, dir)
	t.Fatalf("dir %s attached=%v recursive=%v, want attached=%v recursive=%v", dir, attached, recursive, wantAttached, wantRecursive)
}

func foldKnowledgeWatchCase(path string) string {
	out := []rune(path)
	for i, ch := range out {
		switch {
		case ch >= 'a' && ch <= 'z':
			out[i] = ch - 'a' + 'A'
		case ch >= 'A' && ch <= 'Z':
			out[i] = ch - 'A' + 'a'
		}
	}
	return string(out)
}

func waitKnowledgeWatchDir(t *testing.T, app *App, id, dir string, want bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if app.knowledgeFolderWatchRootAttached(id, dir) == want {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("dir %s attached=%v, want %v", dir, app.knowledgeFolderWatchRootAttached(id, dir), want)
}

func waitKnowledgeWatchText(t *testing.T, app *App, id, token string, after time.Time) KnowledgeFolderWatch {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	var lastText string
	var last KnowledgeFolderWatch
	for time.Now().Before(deadline) {
		items, err := app.KnowledgeListFolderWatches()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID != id {
				continue
			}
			last = item
			if item.Status == knowledgeFolderWatchStatusImporting || !item.LastImportAt.After(after) {
				continue
			}
			lastText = knowledgeWatchIndexedText(t, app)
			if strings.Contains(lastText, token) {
				return item
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("indexed knowledge missing %s (watch=%+v): %s", token, last, lastText)
	return last
}

func waitKnowledgeFolderWatch(t *testing.T, app *App, id string, ready func(KnowledgeFolderWatch) bool) KnowledgeFolderWatch {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last KnowledgeFolderWatch
	for time.Now().Before(deadline) {
		items, err := app.KnowledgeListFolderWatches()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID != id {
				continue
			}
			last = item
			if ready(item) {
				return item
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("watch %s not ready: %+v", id, last)
	return last
}

func knowledgeWatchIndexedText(t *testing.T, app *App) string {
	t.Helper()
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sources, err := store.ListSources(context.Background(), knowledge.ListSourcesOptions{
		SearchScope: knowledge.SaveScopePersonal,
		Limit:       100,
	})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, source := range sources {
		text.WriteString(source.URI)
		text.WriteByte('\n')
		text.WriteString(source.Title)
		text.WriteByte('\n')
		nodes, err := store.ListNodesBySource(context.Background(), source.ID, 30)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range nodes {
			text.WriteString(node.Text)
			text.WriteByte('\n')
		}
	}
	return text.String()
}
