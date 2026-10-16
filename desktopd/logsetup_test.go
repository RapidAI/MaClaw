package desktopd

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"4096", 4096},
		{"32MiB", 32 << 20},
		{"32M", 32 << 20},
		{"1KB", 1000},
		{"2K", 2 << 10},
	}
	for _, tc := range cases {
		got, err := parseByteSize(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("parseByteSize(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
	if _, err := parseByteSize("0"); err == nil {
		t.Fatal("zero size was accepted")
	}
	if _, err := parseByteSize("nope"); err == nil {
		t.Fatal("garbage size was accepted")
	}
}

func TestRotatingFileAppendsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "desktopd.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := newRotatingFile(path, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("more\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "kept\nmore\n" {
		t.Fatalf("open truncated or skipped the existing file: %q", body)
	}
}

func TestRotatingFileHonorsMaxFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktopd.log")
	w, err := newRotatingFile(path, 30, 2)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("0123456789abcdef0123456789abcd\n") // 33 bytes, over the limit
	for i := 0; i < 4; i++ {
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("newest rotated file: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatalf("oldest rotated file: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("kept more generations than maxFiles: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || len(current) == 0 {
		t.Fatalf("active log missing: %v %q", err, current)
	}
}

func TestConfigureProcessLogWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "desktopd.log")
	t.Setenv("DESKTOPD_LOG_FILE", path)
	t.Setenv("DESKTOPD_LOG_MAX_BYTES", "200")
	t.Setenv("DESKTOPD_LOG_MAX_FILES", "2")
	t.Setenv("DESKTOPD_LOG_STDERR", "0")
	prev := log.Writer()
	t.Cleanup(func() { log.SetOutput(prev) })
	if err := ConfigureProcessLog(); err != nil {
		t.Fatal(err)
	}
	log.Printf("hello from desktopd")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "hello from desktopd") {
		t.Fatalf("log file %q missing the line", body)
	}
	if !strings.Contains(string(body), "logging to "+path) {
		t.Fatalf("startup line missing: %s", body)
	}
}

func TestConfigureProcessLogOffLeavesStderr(t *testing.T) {
	t.Setenv("DESKTOPD_LOG_FILE", "off")
	prev := log.Writer()
	t.Cleanup(func() { log.SetOutput(prev) })
	if err := ConfigureProcessLog(); err != nil {
		t.Fatal(err)
	}
	if log.Writer() != prev {
		t.Fatal("DESKTOPD_LOG_FILE=off replaced the logger")
	}
}

func TestConfigureProcessLogRejectsBadSize(t *testing.T) {
	t.Setenv("DESKTOPD_LOG_FILE", filepath.Join(t.TempDir(), "desktopd.log"))
	t.Setenv("DESKTOPD_LOG_MAX_BYTES", "many")
	if err := ConfigureProcessLog(); err == nil {
		t.Fatal("invalid DESKTOPD_LOG_MAX_BYTES was accepted")
	}
}

func TestStderrTeeWritesBothAndSurvivesAFullDisk(t *testing.T) {
	var file, stderr bytes.Buffer
	w := stderrTee{file: &file, stderr: &stderr}
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	if file.String() != "line\n" || stderr.String() != "line\n" {
		t.Fatalf("file=%q stderr=%q", file.String(), stderr.String())
	}
	w = stderrTee{file: failWriter{}, stderr: &stderr}
	if _, err := w.Write([]byte("kept\n")); err == nil {
		t.Fatal("full disk was ignored")
	}
	if !strings.Contains(stderr.String(), "kept\n") {
		t.Fatalf("stderr lost the line when the file write failed: %q", stderr.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }
