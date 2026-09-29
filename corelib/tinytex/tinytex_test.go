package tinytex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDownloadURLsUseOfficialTinyTeX0Asset(t *testing.T) {
	china, err := DownloadURLs("windows", "amd64", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(china) < 2 {
		t.Fatalf("urls = %#v", china)
	}
	if !strings.Contains(china[0], "ghproxy.net/https://github.com/rstudio/tinytex-releases/releases/download/daily/TinyTeX-0-windows.exe") {
		t.Fatalf("china first url = %s", china[0])
	}
	last := china[len(china)-1]
	if last != OfficialBase+"/TinyTeX-0-windows.exe" {
		t.Fatalf("china official url = %s", last)
	}

	direct, err := DownloadURLs("windows", "amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	if direct[0] != OfficialBase+"/TinyTeX-0-windows.exe" {
		t.Fatalf("direct first url = %s", direct[0])
	}
	for _, u := range append(china, direct...) {
		if strings.Contains(u, "TinyTeX-1") || strings.Contains(u, "TinyTeX-2") || strings.HasSuffix(u, "/TinyTeX-windows.exe") {
			t.Fatalf("unexpected bundle in %s", u)
		}
	}
}

func TestCTANRepositoriesPreferChinaMirrorsWhenAsked(t *testing.T) {
	cn := CTANRepositories(true)
	if !strings.Contains(cn[0], "mirrors.tuna.tsinghua.edu.cn") {
		t.Fatalf("china repo = %s", cn[0])
	}
	if !strings.Contains(cn[len(cn)-1], "mirror.ctan.org") {
		t.Fatalf("fallback = %s", cn[len(cn)-1])
	}
	en := CTANRepositories(false)
	if !strings.Contains(en[0], "mirror.ctan.org") {
		t.Fatalf("official first = %s", en[0])
	}
}

func TestArchiveOKRejectsHTMLAndAcceptsMagic(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "TinyTeX-0-windows.exe")
	if err := os.WriteFile(htmlPath, []byte("<html>not an exe</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveOK(htmlPath); err == nil {
		t.Fatal("html payload was accepted")
	}

	exePath := filepath.Join(dir, "ok.exe")
	payload := make([]byte, minArchiveBytes+8)
	payload[0], payload[1] = 'M', 'Z'
	if err := os.WriteFile(exePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveOK(exePath); err != nil {
		t.Fatal(err)
	}

	xzPath := filepath.Join(dir, "ok.tar.xz")
	xz := make([]byte, minArchiveBytes+8)
	copy(xz, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00})
	if err := os.WriteFile(xzPath, xz, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveOK(xzPath); err != nil {
		t.Fatal(err)
	}
}

func TestFindDistRootAndVerifiedStamp(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "TinyTeX")
	if err := os.MkdirAll(filepath.Join(dist, "bin", "windows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	engineName := "xelatex"
	if runtime.GOOS == "windows" {
		engineName = "xelatex.exe"
	}
	if err := os.WriteFile(filepath.Join(dist, "bin", "windows", engineName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := FindDistRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if found != dist {
		t.Fatalf("dist = %s", found)
	}
	if !EnginePresent(found) {
		t.Fatal("engine should be present")
	}
	if ArticlePresent(found) || Verified(found) {
		t.Fatal("scheme-small is not installed yet")
	}

	article := ArticleClassPath(found)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(article, []byte("\\documentclass"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Verified(found) {
		t.Fatal("article.cls without a version stamp is not verified")
	}
	if err := WriteStamp(found, "XeTeX 3.141592653 (TeX Live 2025)\nsecond line\n"); err != nil {
		t.Fatal(err)
	}
	if !Verified(found) {
		t.Fatal("expected verified scheme-small tree")
	}
	ver, ok := ReadStamp(found)
	if !ok || ver != "XeTeX 3.141592653 (TeX Live 2025)" {
		t.Fatalf("stamp = %q ok=%v", ver, ok)
	}
	if Scheme != "scheme-small" || Bundle != "TinyTeX-0" {
		t.Fatalf("scheme=%s bundle=%s", Scheme, Bundle)
	}
}
