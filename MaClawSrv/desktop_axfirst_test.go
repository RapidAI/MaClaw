package main

import (
	"strings"
	"testing"
)

func TestDesktopAXFirstBlock(t *testing.T) {
	probe := "Accessible elements (1 window(s), 2 listed). Act on one...\n[1] push button \"OK\" screen 1,2 3x4\n[2] text \"Name\" screen 1,2 3x4"
	if b := desktopAXFirstBlock(probe); !strings.Contains(b, "prefer {\"id\":N}") || !strings.Contains(b, "[2] text") {
		t.Fatalf("block: %q", b)
	}
	if b := desktopAXFirstBlock("Accessible elements (1 window(s), 0 listed). Act on one..."); b != "" {
		t.Fatalf("empty listing must add nothing: %q", b)
	}
	if desktopAXFirstPrompt() == "" {
		t.Fatal("default on")
	}
	t.Setenv(desktopAXFirstEnv, "off")
	if desktopAXFirstEnabled() || desktopAXFirstPrompt() != "" {
		t.Fatal("flag off must disable")
	}
}
