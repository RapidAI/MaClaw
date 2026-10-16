package guiapp

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFileCompanionOpenPathIsCanonical(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	file := filepath.Join(dir, "Notes.md")
	if err := os.WriteFile(file, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(file)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := fileCompanionCanonicalPath(file)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Path != canon {
		t.Fatalf("path = %q, canonical %q", doc.Path, canon)
	}
	if doc.Name != filepath.Base(file) {
		t.Fatalf("name = %q", doc.Name)
	}
	missing := filepath.Join(dir, "Missing.TXT")
	miss, err := app.FileCompanionOpen(missing)
	if err != nil {
		t.Fatal(err)
	}
	if miss.Error != "file not found" {
		t.Fatalf("missing error = %q", miss.Error)
	}
	missCanon, err := fileCompanionCanonicalPath(missing)
	if err != nil {
		t.Fatal(err)
	}
	if miss.Path != missCanon {
		t.Fatalf("missing path = %q, canonical %q", miss.Path, missCanon)
	}
	if goruntime.GOOS != "windows" {
		return
	}
	again, err := app.FileCompanionOpen(strings.ToLower(file))
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != doc.Path || again.SessionID != doc.SessionID {
		t.Fatalf("case variant path %q session %q, open path %q session %q", again.Path, again.SessionID, doc.Path, doc.SessionID)
	}
}

func TestFileCompanionSaveRefusesSiblingAndHashMismatch(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	opened := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(opened, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(opened)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Editable || doc.Content != "alpha" || doc.LoadedHash == "" {
		t.Fatalf("open = %#v", doc)
	}
	sibling := filepath.Join(dir, "other.md")
	refused, err := app.FileCompanionSaveText(sibling, "nope", doc.LoadedHash)
	if err == nil || refused.Saved {
		t.Fatalf("sibling save = %#v err=%v", refused, err)
	}
	if _, statErr := os.Stat(sibling); !os.IsNotExist(statErr) {
		t.Fatalf("sibling was created: %v", statErr)
	}
	original, err := os.ReadFile(opened)
	if err != nil || string(original) != "alpha" {
		t.Fatalf("opened file changed: %q %v", original, err)
	}

	if err := os.WriteFile(opened, []byte("beta-on-disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(opened, future, future); err != nil {
		t.Fatal(err)
	}
	conflict, err := app.FileCompanionSaveText(opened, "from-editor", doc.LoadedHash)
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.Conflict || conflict.Saved || conflict.DiskHash == "" {
		t.Fatalf("conflict = %#v", conflict)
	}
	body, err := os.ReadFile(opened)
	if err != nil || string(body) != "beta-on-disk" {
		t.Fatalf("hash mismatch wrote the file: %q %v", body, err)
	}

	// Acknowledging the disk hash is the overwrite confirm. The editor text replaces the file.
	overwrite, err := app.FileCompanionSaveText(opened, "from-editor", conflict.DiskHash)
	if err != nil || !overwrite.Saved || overwrite.Conflict {
		t.Fatalf("overwrite = %#v err=%v", overwrite, err)
	}
	body, err = os.ReadFile(opened)
	if err != nil || string(body) != "from-editor" {
		t.Fatalf("overwrite body = %q %v", body, err)
	}
}

func TestFileCompanionSaveAcceptsStaleHashAfterOwnWrite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.FileCompanionSaveText(path, "beta", doc.LoadedHash)
	if err != nil || !saved.Saved || saved.Conflict {
		t.Fatalf("first save = %#v err=%v", saved, err)
	}
	// The editor still holds the hash from open. Disk matches the grant we just wrote.
	again, err := app.FileCompanionSaveText(path, "gamma", doc.LoadedHash)
	if err != nil || !again.Saved || again.Conflict {
		t.Fatalf("stale save = %#v err=%v", again, err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "gamma" {
		t.Fatalf("stale body = %q %v", body, err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	same, err := app.FileCompanionSaveText(path, "gamma", "stale-hash")
	if err != nil || !same.Saved || same.Conflict {
		t.Fatalf("same-content save = %#v err=%v", same, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("unchanged content was rewritten")
	}
}

func TestFileCompanionAppendKeepsSelection(t *testing.T) {
	app := &App{}
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("keep this sentence."), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionSelection(path, "this sentence.", -1)
	saved, err := app.FileCompanionAppendText(path, "这句话", doc.LoadedHash)
	if err != nil || !saved.Saved {
		t.Fatalf("append = %#v err=%v", saved, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep this sentence.\n\n这句话" {
		t.Fatalf("body = %q", body)
	}
	if !strings.Contains(string(body), "this sentence") {
		t.Fatal("selection was replaced")
	}

	doc, err = app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = app.FileCompanionAppendText(path, "tail", doc.LoadedHash)
	if err != nil || !saved.Saved {
		t.Fatalf("end append = %#v err=%v", saved, err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(body), "\n\ntail") {
		t.Fatalf("end append body = %q", body)
	}
}

func TestFileCompanionAppendUsesSelectionStart(t *testing.T) {
	const content = "alpha beta alpha"
	if got := fileCompanionAppendAfterSelection(content, "alpha", "译文", 11); got != "alpha beta alpha\n\n译文" {
		t.Fatalf("prefer 11 = %q", got)
	}
	if got := fileCompanionAppendAfterSelection(content, "alpha", "译文", 10); got != "alpha beta alpha\n\n译文" {
		t.Fatalf("prefer 10 = %q", got)
	}
	if got := fileCompanionAppendAfterSelection(content, "alpha", "译文", -1); got != "alpha\n\n译文 beta alpha" {
		t.Fatalf("prefer -1 = %q", got)
	}
	crlf := "alpha\r\nbeta\r\nalpha"
	if got := fileCompanionAppendAfterSelection(crlf, "alpha", "译文", 11); got != "alpha\r\nbeta\r\nalpha\r\n\r\n译文" {
		t.Fatalf("crlf prefer 11 = %q", got)
	}
	// UTF-16 offset 1 is the second Han character, not the second byte.
	if got := fileCompanionAppendAfterSelection("中中", "中", "译文", 1); got != "中中\n\n译文" {
		t.Fatalf("utf16 prefer 1 = %q", got)
	}
	if got := fileCompanionAppendAfterSelection("中中", "中", "译文", -1); got != "中\n\n译文中" {
		t.Fatalf("utf16 prefer -1 = %q", got)
	}
}

func TestFileCompanionAppendMatchesEditorNewlines(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("keep\r\nSEL\r\nTAIL"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	app.rememberFileCompanionSelection(path, "keep\nSEL", -1)
	saved, err := app.FileCompanionAppendText(path, "译文", doc.LoadedHash)
	if err != nil || !saved.Saved {
		t.Fatalf("append = %#v err=%v", saved, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep\r\nSEL\r\n\r\n译文\r\nTAIL" {
		t.Fatalf("body = %q", body)
	}
}

func TestFileCompanionOpenDirectoryDoesNotGrant(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	doc, err := app.FileCompanionOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Error == "" || doc.CanExport || app.fileCompanionGranted(dir) {
		t.Fatalf("directory doc = %#v granted=%v", doc, app.fileCompanionGranted(dir))
	}
}

func TestFileCompanionOpenReadOnlyKeepsText(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# Title\nkeep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Error != "" {
		t.Fatal(doc.Error)
	}
	if doc.Editable || doc.ReadOnlyReason != "permission" || !strings.Contains(doc.Content, "keep") {
		t.Fatalf("read-only doc = %#v", doc)
	}
	if _, err := app.FileCompanionSaveText(path, "changed", doc.LoadedHash); err == nil {
		t.Fatal("read-only save was accepted")
	}

	bigPath := filepath.Join(dir, "big.txt")
	body := strings.Repeat("A", codingWorkbenchBrowserMaxRunes+1)
	if err := os.WriteFile(bigPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	big, err := app.FileCompanionOpen(bigPath)
	if err != nil {
		t.Fatal(err)
	}
	if big.Editable || big.ReadOnlyReason != "too-large" || len(big.Content) != codingWorkbenchBrowserMaxRunes {
		t.Fatalf("too-large doc editable=%v reason=%q content=%d", big.Editable, big.ReadOnlyReason, len(big.Content))
	}
}

func TestFileCompanionOpenCappedRuneStaysReadOnly(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.txt")
	// One extra byte past the read cap splits a rune. The preview stays read-only
	// so a trimmed prefix cannot be written back over the rest of the file.
	body := strings.Repeat("😀", codingWorkbenchBrowserMaxRunes) + "\x80"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Error != "" || doc.Editable || doc.ReadOnlyReason != "too-large" {
		t.Fatalf("capped doc error=%q editable=%v reason=%q", doc.Error, doc.Editable, doc.ReadOnlyReason)
	}
	if !utf8.ValidString(doc.Content) || utf8.RuneCountInString(doc.Content) != codingWorkbenchBrowserMaxRunes {
		t.Fatalf("capped content runes=%d valid=%v", utf8.RuneCountInString(doc.Content), utf8.ValidString(doc.Content))
	}
	if _, err := app.FileCompanionSaveText(path, doc.Content, doc.LoadedHash); err == nil {
		t.Fatal("capped save was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatal("capped open changed the file")
	}
}

func TestFileCompanionOpenSkipsNonTextBody(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), "pic.png")
	body := []byte{0x89, 'P', 'N', 'G'}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Error != "" || doc.Content != "" || doc.LoadedHash != "" || doc.Editable || !doc.CanExport || doc.Size != 4 {
		t.Fatalf("png doc = %#v", doc)
	}
	if _, err := app.FileCompanionSaveText(path, "nope", doc.LoadedHash); err == nil {
		t.Fatal("png save was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("png bytes = %q", got)
	}
}
