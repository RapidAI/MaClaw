package cloudworkspaceignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenBuiltinAndCloudignore(t *testing.T) {
	cases := []struct {
		path        string
		isDir       bool
		cloudignore string
		want        bool
		name        string
	}{
		{path: ".git", isDir: true, want: true, name: "vcs git dir"},
		{path: ".git/config", isDir: false, want: true, name: "vcs git file"},
		{path: "src/.git/HEAD", isDir: false, want: true, name: "nested git"},
		{path: ".hg", isDir: true, want: true, name: "vcs hg"},
		{path: ".svn/entries", isDir: false, want: true, name: "vcs svn"},
		{path: "node_modules/pkg/index.js", isDir: false, want: true, name: "deps node_modules"},
		{path: "vendor/foo.go", isDir: false, want: true, name: "deps vendor"},
		{path: ".venv/bin/python", isDir: false, want: true, name: "deps .venv"},
		{path: "venv/lib", isDir: true, want: true, name: "deps venv"},
		{path: "__pycache__/x.pyc", isDir: false, want: true, name: "deps pycache"},
		{path: ".pytest_cache/v", isDir: false, want: true, name: "deps pytest"},
		{path: ".mypy_cache/3.12", isDir: true, want: true, name: "deps mypy"},
		{path: "dist/app.js", isDir: false, want: true, name: "deps dist"},
		{path: "build/out", isDir: false, want: true, name: "deps build"},
		{path: "target/debug", isDir: true, want: true, name: "deps target"},
		{path: "out/x", isDir: false, want: true, name: "deps out"},
		{path: "bin/tool", isDir: false, want: true, name: "deps bin"},
		{path: "obj/Debug", isDir: true, want: true, name: "deps obj"},
		{path: ".next/cache", isDir: true, want: true, name: "deps next"},
		{path: ".turbo/x", isDir: false, want: true, name: "deps turbo"},
		{path: "coverage/lcov.info", isDir: false, want: true, name: "deps coverage"},
		{path: "tmp/x", isDir: false, want: true, name: "deps tmp"},
		{path: "temp/x", isDir: false, want: true, name: "deps temp"},
		{path: ".cache/x", isDir: false, want: true, name: "deps cache"},
		{path: ".idea/workspace.xml", isDir: false, want: true, name: "ide idea"},
		{path: ".vscode/settings.json", isDir: false, want: true, name: "ide vscode"},
		{path: ".maclaw/config.json", isDir: false, want: true, name: "product maclaw"},
		{path: ".maclaw-cloud/state", isDir: false, want: true, name: "product maclaw-cloud"},
		{path: "src/.maclaw/x", isDir: false, want: true, name: "nested product"},
		{path: ".env", isDir: false, want: true, name: "secret env"},
		{path: ".env.local", isDir: false, want: true, name: "secret env local"},
		{path: ".env.production", isDir: false, want: true, name: "secret env production"},
		{path: ".env.example", isDir: false, want: false, name: "env example kept"},
		{path: "dir/.env.example", isDir: false, want: false, name: "nested env example kept"},
		{path: "secret.pem", isDir: false, want: true, name: "secret pem"},
		{path: "tls.key", isDir: false, want: true, name: "secret key"},
		{path: "id_rsa", isDir: false, want: true, name: "secret id_rsa"},
		{path: "keys/id_dsa", isDir: false, want: true, name: "secret id_dsa"},
		{path: "id_ecdsa", isDir: false, want: true, name: "secret id_ecdsa"},
		{path: "id_ed25519", isDir: false, want: true, name: "secret id_ed25519"},
		{path: "credentials.json", isDir: false, want: true, name: "secret credentials"},
		{path: "pkg/credentials.json", isDir: false, want: true, name: "nested credentials"},
		{path: "disk.iso", isDir: false, want: true, name: "volume iso"},
		{path: "app.dmg", isDir: false, want: true, name: "volume dmg"},
		{path: "app.exe", isDir: false, want: false, name: "exe not ignored"},
		{path: "src/main.go", isDir: false, want: false, name: "source kept"},
		{path: "README.md", isDir: false, want: false, name: "readme kept"},
		{path: FileName, isDir: false, want: false, name: "cloudignore itself kept"},
		{path: "bin", isDir: false, want: false, name: "file named bin kept"},
		{path: "dist", isDir: true, want: true, name: "dist dir ignored"},

		{path: ".maclaw/x", isDir: false, cloudignore: "!.maclaw\n!.maclaw/**\n", want: true, name: "force skip maclaw"},
		{path: ".maclaw-cloud/x", isDir: false, cloudignore: "!.maclaw-cloud/\n!.maclaw-cloud/**\n", want: true, name: "force skip maclaw-cloud"},
		{path: ".env", isDir: false, cloudignore: "!.env\n", want: false, name: "user can un-ignore env"},

		{path: "secret.log", isDir: false, cloudignore: "*.log\n", want: true, name: "user glob"},
		{path: "keep.log", isDir: false, cloudignore: "*.log\n!keep.log\n", want: false, name: "user negation"},
		{path: "logs/a.txt", isDir: false, cloudignore: "/logs/\n", want: true, name: "anchored dir"},
		{path: "src/logs/a.txt", isDir: false, cloudignore: "/logs/\n", want: false, name: "anchored dir not nested"},
		{path: "foo/bar.tmp", isDir: false, cloudignore: "**/*.tmp\n", want: true, name: "double star"},
		{path: "bar.tmp", isDir: false, cloudignore: "**/*.tmp\n", want: true, name: "double star root"},
		{path: "generated/x.go", isDir: false, cloudignore: "/generated/\n", want: true, name: "root generated"},
		{path: "pkg/generated/x.go", isDir: false, cloudignore: "/generated/\n", want: false, name: "nested generated kept"},
		{path: "notes.txt", isDir: false, cloudignore: "# comment\n\n*.txt\n", want: true, name: "comments and blanks"},
		{path: "a/b/c.dat", isDir: false, cloudignore: "a/**/c.dat\n", want: true, name: "middle double star"},
		{path: `src\main.go`, isDir: false, want: false, name: "backslash separators"},
		{path: "", isDir: true, want: false, name: "workspace root"},
		{path: "../outside", isDir: false, want: true, name: "escape is skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldIgnore(tc.path, tc.isDir, tc.cloudignore)
			if got != tc.want {
				t.Fatalf("ShouldIgnore(%q, isDir=%v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
			}
		})
	}
}

func TestReadCloudignoreMissingAndPresent(t *testing.T) {
	root := t.TempDir()
	got, err := ReadCloudignore(root)
	if err != nil || got != "" {
		t.Fatalf("missing file: got %q err=%v", got, err)
	}
	body := "*.log\n"
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ReadCloudignore(root)
	if err != nil || got != body {
		t.Fatalf("present file: got %q err=%v", got, err)
	}
}

func TestMatcherReusable(t *testing.T) {
	m := NewMatcher("*.tmp\n!keep.tmp\n")
	if !m.ShouldIgnore("a.tmp", false) {
		t.Fatal("a.tmp should ignore")
	}
	if m.ShouldIgnore("keep.tmp", false) {
		t.Fatal("keep.tmp should not ignore")
	}
	if m.ShouldIgnore("a.go", false) {
		t.Fatal("a.go should not ignore")
	}
}
