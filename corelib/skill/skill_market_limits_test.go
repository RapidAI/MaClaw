package skill

import (
	"strings"
	"testing"
)

func TestSkillPackagePathHasRuntimeArtifact(t *testing.T) {
	cases := map[string]bool{
		"":                       false,
		"skill.yaml":             false,
		"scripts/run.py":         false,
		"assets/templates/a.svg": false,
		"node_modules":           true,
		"node_modules/x.js":      true,
		"pkg/node_modules/x.js":  true,
		".git/HEAD":              true,
		".venv/lib/site.py":      true,
		"venv/bin/python":        true,
		"__pycache__/a.pyc":      true,
		"upload_status.json":     true,
		"foo/upload_status.json": true,
		"quality_status.json":    true,
		"skill.yaml.bak":         true,
		"docs/readme.md":         false,
		"src/.cache/x":           true,
		"build.prev/x":           true,
		"__MACOSX/._foo":         true,
		".DS_Store":              true,
		"Thumbs.db":              true,
	}
	for path, want := range cases {
		if got := SkillPackagePathHasRuntimeArtifact(path); got != want {
			t.Errorf("SkillPackagePathHasRuntimeArtifact(%q)=%v want %v", path, got, want)
		}
	}
}

func TestSkillMarketLimitsConsistent(t *testing.T) {
	if MaxSkillMarketZipEntries < 3500 {
		t.Fatalf("MaxSkillMarketZipEntries=%d too low for multi-thousand asset packs", MaxSkillMarketZipEntries)
	}
	if MaxSkillMarketZipTotalBytes < MaxSkillMarketZipSingleFileBytes {
		t.Fatal("total limit must be >= single-file limit")
	}
	if MaxSkillMarketZipSingleFileBytes <= 0 || MaxSkillMarketZipTotalBytes <= 0 {
		t.Fatal("size limits must be positive")
	}
	// Download wire limit must exceed the old 5 MiB cap that blocked ppt-master.
	if MaxSkillPackageDownloadBytes <= 5<<20 {
		t.Fatalf("MaxSkillPackageDownloadBytes=%d too low for multi-asset skill install", MaxSkillPackageDownloadBytes)
	}
	if MaxSkillHubSearchJSONBytes <= 0 || MaxSkillHubSearchJSONBytes >= MaxSkillPackageDownloadBytes {
		t.Fatalf("search budget %d should be smaller than download budget %d", MaxSkillHubSearchJSONBytes, MaxSkillPackageDownloadBytes)
	}
}

func TestFormatSkillByteCount(t *testing.T) {
	if FormatSkillByteCount(500) != "500 B" {
		t.Fatalf("got %q", FormatSkillByteCount(500))
	}
	if got := FormatSkillByteCount(96 << 20); !strings.Contains(got, "MiB") && !strings.Contains(got, "M") {
		t.Fatalf("96 MiB format = %q", got)
	}
}

func TestCheckSkillPackageDownloadLimit(t *testing.T) {
	if err := CheckSkillPackageDownloadLimit(-1, 32); err != nil {
		t.Fatalf("unknown length should pass: %v", err)
	}
	if err := CheckSkillPackageDownloadLimit(32, 32); err != nil {
		t.Fatalf("exact limit should pass: %v", err)
	}
	if err := CheckSkillPackageDownloadLimit(33, 32); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestReadLimitedHTTPBody(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := ReadLimitedHTTPBody(strings.NewReader(body), int64(len(body)), 32)
	if err != nil {
		t.Fatalf("exact: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data mismatch")
	}
	if _, err := ReadLimitedHTTPBody(strings.NewReader(body+"!"), -1, 32); err == nil {
		t.Fatal("expected oversize body rejection")
	}
	if _, err := ReadLimitedHTTPBody(strings.NewReader("tiny"), 1000, 32); err == nil {
		t.Fatal("expected content-length rejection")
	}
	// Unknown Content-Length still enforces the byte cap after reading.
	if _, err := ReadLimitedHTTPBody(strings.NewReader(strings.Repeat("e", 100)), -1, 32); err == nil {
		t.Fatal("expected oversize body rejection even without Content-Length")
	}
	// Error-page style: CL ignored so a large declared length does not block
	// reading a small capped error body (first 32 bytes fit).
	data, err = ReadLimitedHTTPBody(strings.NewReader(strings.Repeat("e", 16)), 10<<20, 32)
	if err == nil {
		t.Fatal("expected content-length rejection when CL is known and huge")
	}
	_ = data
}
