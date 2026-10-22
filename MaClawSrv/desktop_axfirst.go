package main

import (
	"context"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Accessibility-tree-first observation (MACLAW_DESKTOP_AX_FIRST, default on).
// A full screenshot also lists the focused window's interactive AT-SPI
// elements with ids, so app_run/computer can target {"id":N} instead of
// pixels; web pages keep probe refs. Pixels and the grounder stay the
// fallback. Off restores the screenshot-only observation for A/B runs.

const desktopAXFirstEnv = "MACLAW_DESKTOP_AX_FIRST"

const desktopAXFirstMaxNodes = 60

func desktopAXFirstEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopAXFirstEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// desktopAXFirstPrompt is the guidance added to execute turns.
func desktopAXFirstPrompt() string {
	if !desktopAXFirstEnabled() {
		return ""
	}
	return "Element-first: on web pages act by probe refs (click/type ref, or script by label); in other windows act by the accessibility ids that screenshot and app_probe list ({\"id\":N} in app_run/computer). Use pixel clicks only when no ref or id fits. "
}

// desktopAXFirstElements returns the element list to put before a full
// screenshot, or "" (flag off, nothing interactive, or the read failed).
func desktopAXFirstElements(ctx context.Context, scope agentruntime.Scope, display string) string {
	if !desktopAXFirstEnabled() {
		return ""
	}
	text, err := operateDesktopCtl(ctx, scope, display, "app_probe", map[string]any{"interactive_only": true, "max_nodes": desktopAXFirstMaxNodes})
	if err != nil {
		return ""
	}
	return desktopAXFirstBlock(text)
}

// desktopAXFirstBlock wraps an app_probe listing; a listing with no element
// ids adds nothing.
func desktopAXFirstBlock(probe string) string {
	probe = strings.TrimSpace(probe)
	if probe == "" || strings.Contains(probe, " 0 listed") || !strings.Contains(probe, "\n[") {
		return ""
	}
	return "Accessibility elements of the focused window (prefer {\"id\":N} over pixels):\n" + probe + "\n\n"
}
