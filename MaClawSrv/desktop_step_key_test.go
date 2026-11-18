package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopPermitIgnoresToolCallID(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot", RunID: "run-1"}
	args := map[string]any{
		"action":   "install",
		"packages": []any{"curl"},
		"url":      "https://example.test/pkg",
		"to":       "a@example.com",
		"text":     "hello body",
		"password": "super-secret",
	}
	first, err := desktopEffectPermit(scope, "install", "", args)
	if err != nil {
		t.Fatal(err)
	}
	second, err := desktopEffectPermit(scope, "install", "", args)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("permit changed without a new authorization: %#v %#v", first, second)
	}
	if first.Args == "" || first.Target == "" || first.Owner != "tenant/alice" || first.Account != "bot" {
		t.Fatalf("permit = %#v", first)
	}
	if strings.Contains(first.Args, "super-secret") || strings.Contains(first.Target, "super-secret") {
		t.Fatalf("secret entered the permit: %#v", first)
	}
	changed := map[string]any{"action": "install", "packages": []any{"curl"}, "url": "https://example.test/other", "to": "b@example.com", "text": "hello body"}
	other, err := desktopEffectPermit(scope, "install", "", changed)
	if err != nil {
		t.Fatal(err)
	}
	if other.Args == first.Args || other.Target == first.Target {
		t.Fatal("recipient and url were not part of the authorization")
	}
	incomplete := scope
	incomplete.RunID = ""
	if _, err := desktopEffectPermit(incomplete, "install", "", args); err == nil {
		t.Fatal("missing run was accepted")
	}
	if _, err := desktopEffectPermit(agentruntime.Scope{RunID: "run-1"}, "install", "", args); !errors.As(err, &desktopbot.ErrEffectIdentity{}) {
		t.Fatalf("missing owner err=%v", err)
	}
}
