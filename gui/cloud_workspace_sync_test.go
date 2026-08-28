package main

import "testing"

func TestCloudWorkspaceSafeRelPath(t *testing.T) {
	okCases := []string{"a.txt", "src/main.go", "docs/readme.md"}
	for _, p := range okCases {
		got, ok := cloudWorkspaceSafeRelPath(p)
		if !ok || got != p {
			t.Fatalf("path %q ok=%v got=%q", p, ok, got)
		}
	}
	bad := []string{"", "../secret", "foo/../bar", "/abs", `C:\windows`, `a\b`, "foo//bar"}
	for _, p := range bad {
		if _, ok := cloudWorkspaceSafeRelPath(p); ok {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}
