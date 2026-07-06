//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestQuoteWindowsCommandPath(t *testing.T) {
	got := quoteWindowsCommandPath(` C:\Program Files\TigerProxy\TigerProxy.exe `)
	want := `"C:\Program Files\TigerProxy\TigerProxy.exe"`
	if got != want {
		t.Fatalf("quoteWindowsCommandPath() = %q, want %q", got, want)
	}
}

func TestUnquoteWindowsCommandPath(t *testing.T) {
	cases := map[string]string{
		`"C:\Program Files\TigerProxy\TigerProxy.exe"`:          `C:\Program Files\TigerProxy\TigerProxy.exe`,
		`"C:\Program Files\TigerProxy\TigerProxy.exe" --hidden`: `C:\Program Files\TigerProxy\TigerProxy.exe`,
		`C:\TigerProxy\TigerProxy.exe --hidden`:                 `C:\TigerProxy\TigerProxy.exe`,
	}
	for in, want := range cases {
		if got := unquoteWindowsCommandPath(in); got != want {
			t.Fatalf("unquoteWindowsCommandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutoStartCommandStartsHidden(t *testing.T) {
	command, err := autoStartCommand()
	if err != nil {
		t.Fatalf("autoStartCommand: %v", err)
	}
	if got := unquoteWindowsCommandPath(command); got == "" {
		t.Fatalf("autoStartCommand executable path is empty: %q", command)
	}
	if !strings.HasSuffix(command, " --hidden") {
		t.Fatalf("autoStartCommand = %q, want --hidden suffix", command)
	}
	if !autoStartCommandMatchesExecutable(command) {
		t.Fatalf("autoStartCommandMatchesExecutable(%q) = false, want true", command)
	}
}

func TestAutoStartCommandMatchesExecutableRequiresHiddenArg(t *testing.T) {
	command, err := autoStartCommand()
	if err != nil {
		t.Fatalf("autoStartCommand: %v", err)
	}
	command = strings.TrimSuffix(command, " --hidden")
	if autoStartCommandMatchesExecutable(command) {
		t.Fatalf("autoStartCommandMatchesExecutable(%q) = true, want false without hidden arg", command)
	}
}
