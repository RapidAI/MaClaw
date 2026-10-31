package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolveFileToolPath(t *testing.T) {
	project := filepath.Clean("/tmp/project")
	got, err := ResolveFileToolPath("docs/a.md", func() string { return project })
	if err != nil {
		t.Fatalf("ResolveFileToolPath error: %v", err)
	}
	want := filepath.Join(project, "docs", "a.md")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveFileToolPath_Absolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x.txt")
	got, err := ResolveFileToolPath(abs, nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath error: %v", err)
	}
	if got != abs {
		t.Fatalf("path = %q, want %q", got, abs)
	}
}

func TestResolveFileToolPath_TildeHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	got, err := ResolveFileToolPath("~/.maclaw/workspace/note.md", nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath tilde error: %v", err)
	}
	want := filepath.Clean(filepath.Join(home, ".maclaw", "workspace", "note.md"))
	if got != want {
		t.Fatalf("tilde path = %q, want %q", got, want)
	}
	// ~otheruser must not be treated as home expansion.
	other, err := ResolveFileToolPath("~otheruser/docs", nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath ~otheruser error: %v", err)
	}
	if other == filepath.Join(home, "otheruser", "docs") || other == filepath.Clean(filepath.Join(home, "otheruser/docs")) {
		t.Fatalf("~otheruser was incorrectly expanded to home: %q", other)
	}
}

func TestResolveFileToolPath_EmptyUsesProjectDir(t *testing.T) {
	project := filepath.Clean("/tmp/project-root")
	got, err := ResolveFileToolPath("", func() string { return project })
	if err != nil {
		t.Fatalf("ResolveFileToolPath empty error: %v", err)
	}
	if got != project {
		t.Fatalf("empty path = %q, want project dir %q", got, project)
	}
}

func TestResolveFileToolPath_EmptyWithoutProjectErrors(t *testing.T) {
	if _, err := ResolveFileToolPath("", nil); err == nil {
		t.Fatal("expected error when empty path and no project resolver")
	}
}

func TestWriteTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	size, err := WriteTextFile(path, "hello", "overwrite")
	if err != nil {
		t.Fatalf("WriteTextFile overwrite error: %v", err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	if _, err := WriteTextFile(path, " world", "append"); err != nil {
		t.Fatalf("WriteTextFile append error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q, want %q", string(data), "hello world")
	}
}

func TestWriteTextFile_EmptyContentAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	size, err := WriteTextFile(path, "", "overwrite")
	if err != nil {
		t.Fatalf("WriteTextFile empty error: %v", err)
	}
	if size != 0 {
		t.Fatalf("size = %d, want 0", size)
	}
}

func TestWriteTextFile_InvalidMode(t *testing.T) {
	_, err := WriteTextFile(filepath.Join(t.TempDir(), "x.txt"), "a", "edit")
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestEditTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edit.txt")
	if err := os.WriteFile(path, []byte("a b a"), 0o644); err != nil {
		t.Fatalf("seed file error: %v", err)
	}
	res, err := EditTextFile(path, "a", "x", false)
	if err != nil {
		t.Fatalf("EditTextFile error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "x b a" {
		t.Fatalf("content = %q, want %q", string(data), "x b a")
	}
	res, err = EditTextFile(path, "a", "y", true)
	if err != nil {
		t.Fatalf("EditTextFile replaceAll error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "x b y" {
		t.Fatalf("content = %q, want %q", string(data), "x b y")
	}
}

func TestEditTextFileReadsLatestBytesUnderPathLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var closeRelease sync.Once
	finish := func() { closeRelease.Do(func() { close(release) }) }
	textFileMutationHook.Store(func(got string) {
		if got != path {
			return
		}
		once.Do(func() { close(entered) })
		<-release
	})
	t.Cleanup(func() {
		textFileMutationHook.Store((func(string))(nil))
		finish()
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := EditTextFile(path, "fresh", "FRESH", false)
		errCh <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("edit did not enter the file lock")
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finish()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "FRESH\n" {
		t.Fatalf("content = %q, want the edit applied to the bytes written while the lock was held", data)
	}
}

func TestConcurrentEditTextFileKeepsBothReplacements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "both.txt")
	for round := 0; round < 20; round++ {
		if err := os.WriteFile(path, []byte("left right\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := EditTextFile(path, "left", "LEFT", false)
			errCh <- err
		}()
		go func() {
			defer wg.Done()
			_, err := EditTextFile(path, "right", "RIGHT", false)
			errCh <- err
		}()
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "LEFT RIGHT\n" {
			t.Fatalf("round %d content = %q, want both replacements", round, data)
		}
	}
}

func TestTextFileWriteLockIsPerPath(t *testing.T) {
	dir := t.TempDir()
	held := filepath.Join(dir, "a.txt")
	other := filepath.Join(dir, "b.txt")
	heldLock := textFileWriteLock(held)
	heldLock.Lock()
	t.Cleanup(heldLock.Unlock)
	done := make(chan error, 1)
	go func() {
		_, err := WriteTextFile(other, "bee", "overwrite")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write of a different file waited on the other path lock")
	}
	data, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bee" {
		t.Fatalf("content = %q", data)
	}
}

func TestTextFileWriteLockKeyStableAcrossCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.go")
	before := textFileWriteLockKey(path)
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := textFileWriteLockKey(path); after != before {
		t.Fatalf("lock key changed after create: before %q after %q", before, after)
	}
	nested := filepath.Join(dir, "newsub", "file.go")
	beforeNested := textFileWriteLockKey(nested)
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if afterNested := textFileWriteLockKey(nested); afterNested != beforeNested {
		t.Fatalf("lock key changed after creating a parent: before %q after %q", beforeNested, afterNested)
	}
	if textFileWriteLock(path) != textFileWriteLock(path) {
		t.Fatal("same path did not keep its mutex")
	}
}

func TestTextFileWriteLockFollowsJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows junction")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Skip("junction unavailable: " + err.Error() + " " + string(out))
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	viaLink := filepath.Join(link, "child", "file.go")
	viaReal := filepath.Join(real, "child", "file.go")
	before := textFileWriteLockKey(viaLink)
	if got := textFileWriteLockKey(viaReal); got != before {
		t.Fatalf("junction and target did not share a key before create: link %q real %q", before, got)
	}
	if err := os.MkdirAll(filepath.Dir(viaLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(viaLink, []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := textFileWriteLockKey(viaLink); after != before {
		t.Fatalf("junction lock key changed after create: before %q after %q", before, after)
	}
	if got := textFileWriteLockKey(viaReal); got != before {
		t.Fatalf("junction and target diverged after create: link %q real %q", before, got)
	}
	held := textFileWriteLock(viaLink)
	held.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := WriteTextFile(viaReal, "joined", "overwrite")
		done <- err
	}()
	select {
	case err := <-done:
		held.Unlock()
		t.Fatalf("target path did not share the junction lock: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	held.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not finish after the junction lock was released")
	}
	data, err := os.ReadFile(viaLink)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "joined" {
		t.Fatalf("content = %q", data)
	}
}

func TestTextFileWriteLockFollowsNestedJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows junction")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	mid := filepath.Join(dir, "mid")
	link := filepath.Join(dir, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", mid, real).CombinedOutput(); err != nil {
		t.Skip("junction unavailable: " + err.Error() + " " + string(out))
	}
	t.Cleanup(func() { _ = os.Remove(mid) })
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, mid).CombinedOutput(); err != nil {
		t.Skip("junction unavailable: " + err.Error() + " " + string(out))
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	viaLink := filepath.Join(link, "child", "file.go")
	viaReal := filepath.Join(real, "child", "file.go")
	before := textFileWriteLockKey(viaLink)
	if got := textFileWriteLockKey(viaReal); got != before {
		t.Fatalf("nested junction key before create: link %q real %q", before, got)
	}
	if err := os.MkdirAll(filepath.Dir(viaReal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(viaReal, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := textFileWriteLockKey(viaLink); after != before {
		t.Fatalf("nested junction key changed after create: before %q after %q", before, after)
	}
	if got := textFileWriteLockKey(viaReal); got != before {
		t.Fatalf("nested junction key after create: link %q real %q", before, got)
	}
}

func TestRewriteTextFileKeepsBothPassageEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "both.txt")
	if err := os.WriteFile(path, []byte("alpha beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	replace := func(old, next string) error {
		_, err := RewriteTextFile(path, func(current string) (string, bool, error) {
			updated := strings.Replace(current, old, next, 1)
			if updated == current {
				return "", false, fmt.Errorf("missing %s", old)
			}
			return updated, true, nil
		})
		return err
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- replace("alpha", "ALPHA")
	}()
	go func() {
		defer wg.Done()
		errCh <- replace("beta", "BETA")
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ALPHA BETA\n" {
		t.Fatalf("content = %q, want both passage edits", data)
	}
}

func TestTextFileWriteLockFollowsExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink unavailable: " + err.Error())
	}
	if textFileWriteLock(target) != textFileWriteLock(link) {
		t.Fatal("symlink and target did not share a lock")
	}
}

func TestTextFileWriteLockKeyFoldsWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path case")
	}
	dir := t.TempDir()
	lower := filepath.Join(dir, "a.txt")
	upper := filepath.Join(dir, "A.txt")
	if textFileWriteLock(lower) != textFileWriteLock(upper) {
		t.Fatal("case-distinct windows paths did not share a lock")
	}
}

func TestEditTextFile_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edit.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("seed file error: %v", err)
	}
	if _, err := EditTextFile(path, "zzz", "x", false); err == nil {
		t.Fatal("expected not found error")
	}
}
