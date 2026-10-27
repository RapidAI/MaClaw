package guiapp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readErrorLogFullScanReference(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var errors []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if classifyAppLogLine(line) != appLogLineError {
			continue
		}
		if len(line) > errorLogMaxLineBytes {
			line = line[:errorLogMaxLineBytes]
		}
		errors = append(errors, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(errors) > 500 {
		errors = errors[len(errors)-500:]
	}
	for i, j := 0, len(errors)-1; i < j; i, j = i+1, j-1 {
		errors[i], errors[j] = errors[j], errors[i]
	}
	return errors, nil
}

func TestReadErrorLogTailMatchesFullScan(t *testing.T) {
	var log strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&log, "ordinary log line %d\n", i)
	}
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&log, "request %d [ERROR] failure", i)
		if i < 599 {
			log.WriteByte('\n')
		}
	}

	path := filepath.Join(t.TempDir(), "maclaw.log")
	if err := os.WriteFile(path, []byte(log.String()), 0644); err != nil {
		t.Fatal(err)
	}
	want, err := readErrorLogFullScanReference(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readErrorLogTail(path, 500)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tail scan differs from full scan: got %d lines, want %d", len(got), len(want))
	}
	if len(got) != 500 || got[0] != "request 599 [ERROR] failure" || got[499] != "request 100 [ERROR] failure" {
		t.Fatalf("unexpected newest-first window: len=%d first=%q last=%q", len(got), got[0], got[len(got)-1])
	}
}

func TestReadErrorLogTailTruncatesLongLines(t *testing.T) {
	longLine := "begin " + strings.Repeat("x", 80*1024) + " [ERROR] after truncated prefix"
	data := "older [ERROR] failure\n" + longLine
	path := filepath.Join(t.TempDir(), "maclaw.log")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readErrorLogTail(path, 500)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{longLine[:errorLogMaxLineBytes], "older [ERROR] failure"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %d lines, want truncated long line followed by older error", len(got))
	}
}
