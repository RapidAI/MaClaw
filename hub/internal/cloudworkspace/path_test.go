package cloudworkspace

import "testing"

func TestValidateManifestPath(t *testing.T) {
	ok, err := ValidateManifestPath("src/a.go")
	if err != nil || ok != "src/a.go" {
		t.Fatalf("got %q err=%v", ok, err)
	}
	for _, p := range []string{
		"",
		"/etc/passwd",
		"../secret",
		"foo/../bar",
		`foo\bar`,
		"C:/windows",
		"node_modules/x",
		".maclaw-cloud/state.json",
		"foo//bar",
		"./x",
		"CON.txt",
		"folder/trailing.",
		"folder/trailing ",
	} {
		if _, err := ValidateManifestPath(p); err != ErrInvalidPath {
			t.Fatalf("path %q err=%v", p, err)
		}
	}
}

func TestNormalizeEntriesRejectsPortablePathCollisions(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, entries := range [][]ManifestEntry{
		{{Path: "README.md", SHA256: sha}, {Path: "readme.md", SHA256: sha}},
		{{Path: "cafe\u0301.txt", SHA256: sha}, {Path: "caf\u00e9.txt", SHA256: sha}},
	} {
		if _, err := normalizeEntries(entries); err != ErrInvalidPath {
			t.Fatalf("entries=%+v err=%v, want ErrInvalidPath", entries, err)
		}
	}
}

func TestValidSHA256Hex(t *testing.T) {
	if ValidSHA256Hex("abc") {
		t.Fatal("short")
	}
	if ValidSHA256Hex("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatal("uppercase")
	}
	if !ValidSHA256Hex("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatal("lowercase hex")
	}
}
