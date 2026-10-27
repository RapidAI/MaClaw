package httpapi

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readLogTailReference(path string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	lines := make([]string, 0, limit)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > limit {
			lines = append([]string(nil), lines[len(lines)-limit:]...)
		}
	}
	return lines
}

func TestReadLogTailMatchesFullScan(t *testing.T) {
	largeLine := strings.Repeat("x", 70*1024)
	largeLog := bytes.Repeat([]byte("prefix\n"), 20*1024)
	largeLog = append(largeLog, []byte("tail-1\n\ntail-2")...)
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty"},
		{name: "no trailing newline", data: []byte("first\n \nsecond")},
		{name: "trailing newline and whitespace", data: []byte(" first \n\t\nsecond\r\n")},
		{name: "larger than initial window", data: largeLog},
		{name: "oversized final line", data: []byte("before\n" + largeLine)},
		{name: "lines beyond initial window", data: append(bytes.Repeat([]byte("line\n"), 20000), []byte("last")...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "download.log")
			if err := os.WriteFile(path, tc.data, 0644); err != nil {
				t.Fatal(err)
			}
			for _, limit := range []int{1, 20, 120} {
				want := readLogTailReference(path, limit)
				if got := readLogTail(path, limit); !reflect.DeepEqual(got, want) {
					t.Fatalf("limit=%d got %d lines, want %d\ngot=%q\nwant=%q", limit, len(got), len(want), got, want)
				}
			}
		})
	}
}
