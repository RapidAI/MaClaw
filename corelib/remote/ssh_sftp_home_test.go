package remote

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJoinRemoteHomeMatchesSCP(t *testing.T) {
	got, err := joinRemoteHome("/home/gpu84", "~/out/../out/report.pdf")
	if err != nil || got != "/home/gpu84/out/report.pdf" {
		t.Fatalf("home file = %q err=%v", got, err)
	}
	got, err = joinRemoteHome("/home/gpu84/", "~")
	if err != nil || got != "/home/gpu84" {
		t.Fatalf("home directory = %q err=%v", got, err)
	}
	// "~/foo/../../etc/passwd" expands to /home/gpu84/foo/../../etc/passwd.
	got, err = joinRemoteHome("/home/gpu84", "~/foo/../../etc/passwd")
	if err != nil || got != "/home/etc/passwd" {
		t.Fatalf("parent of home = %q err=%v", got, err)
	}
	got, err = joinRemoteHome("/home/gpu84", "~/../etc/passwd")
	if err != nil || got != "/home/etc/passwd" {
		t.Fatalf("cleaned parent of home = %q err=%v", got, err)
	}
	if _, err = joinRemoteHome("gpu84", "~/out/a.pdf"); err == nil {
		t.Fatal("a relative home is not a login directory")
	}
	home, ok := absoluteRemoteHome("/home/gpu84/../gpu84")
	if !ok || home != "/home/gpu84" {
		t.Fatalf("cleaned home = %q ok=%v", home, ok)
	}
	if _, ok = absoluteRemoteHome(""); ok {
		t.Fatal("an empty home must not be used")
	}
	if _, ok = absoluteRemoteHome("relative"); ok {
		t.Fatal("a relative home must not be used")
	}
}

func TestResolveSFTPRemotePathUsesServerDirectory(t *testing.T) {
	prepared, err := prepareSFTPRemotePath("/home/gpu84/out/../out/a.pdf")
	if err != nil || prepared.needsHome {
		t.Fatalf("absolute prepare = %#v err=%v", prepared, err)
	}
	got, err := prepared.resolve(func() (string, error) {
		t.Fatal("an absolute path must not ask for the login directory")
		return "", nil
	})
	if err != nil || got != "/home/gpu84/out/a.pdf" {
		t.Fatalf("absolute = %q err=%v", got, err)
	}
	prepared, err = prepareSFTPRemotePath("~/out/a.pdf")
	if err != nil || !prepared.needsHome {
		t.Fatalf("tilde prepare = %#v err=%v", prepared, err)
	}
	if _, err = prepared.resolve(nil); err == nil || !strings.Contains(err.Error(), "remote home is unavailable") {
		t.Fatalf("tilde without a directory = %v", err)
	}
	got, err = prepared.resolve(func() (string, error) { return "/home/gpu84", nil })
	if err != nil || got != "/home/gpu84/out/a.pdf" {
		t.Fatalf("server directory = %q err=%v", got, err)
	}
	got, err = prepared.resolve(func() (string, error) { return "/", nil })
	if err != nil || got != "/out/a.pdf" {
		t.Fatalf("chroot directory = %q err=%v", got, err)
	}
	if _, err = prepareSFTPRemotePath("~gpu84/out/a.pdf"); err == nil || !strings.Contains(err.Error(), "another account") {
		t.Fatalf("~otheruser = %v", err)
	}
	if got := canonicalDownloadDir("/home/gpu84/out", func(string) (string, error) {
		return "/data/out", nil
	}); got != "/data/out" {
		t.Fatalf("symlink directory = %q", got)
	}
	if got := canonicalDownloadDir("/home/gpu84/out", func(string) (string, error) {
		return "", fmt.Errorf("unsupported")
	}); got != "/home/gpu84/out" {
		t.Fatalf("missing realpath = %q", got)
	}
	root, err := realDownloadRoot("/home/gpu84/out", func(string) (string, error) {
		return "/data/out", nil
	}, func(path string) (os.FileInfo, error) {
		if path != "/data/out" {
			t.Fatalf("lstat %s", path)
		}
		return fakeFileInfo{mode: os.ModeDir | 0o755, dir: true}, nil
	})
	if err != nil || root != "/data/out" {
		t.Fatalf("resolved directory = %q err=%v", root, err)
	}
	if _, err = realDownloadRoot("/home/gpu84/out", func(string) (string, error) {
		return "", fmt.Errorf("unsupported")
	}, func(string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink}, nil
	}); err == nil {
		t.Fatal("an unresolved directory symlink must not copy as an empty success")
	}
	if got := classifyWalkEntry(nil); got != walkMissing {
		t.Fatalf("nil entry = %v", got)
	}
	if got := classifyWalkEntry(fakeFileInfo{mode: os.ModeDir | 0o755, dir: true}); got != walkDir {
		t.Fatalf("dir entry = %v", got)
	}
	if got := classifyWalkEntry(fakeFileInfo{mode: 0o644}); got != walkFile {
		t.Fatalf("file entry = %v", got)
	}
	if got := classifyWalkEntry(fakeFileInfo{mode: os.ModeSymlink}); got != walkFollow {
		t.Fatalf("symlink entry = %v", got)
	}
	if got := classifyWalkEntry(fakeFileInfo{mode: os.ModeNamedPipe}); got != walkFollow {
		t.Fatalf("pipe entry = %v", got)
	}
}

type fakeFileInfo struct {
	mode os.FileMode
	dir  bool
}

func (f fakeFileInfo) Name() string       { return "entry" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }
