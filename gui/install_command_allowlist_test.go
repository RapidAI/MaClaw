package main

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/tui/commands"
)

func TestInstallCommandAllowlistLoaded(t *testing.T) {
	al, err := loadInstallCommandAllowlist()
	if err != nil {
		t.Fatalf("load allowlist: %v", err)
	}
	if al == nil {
		t.Fatal("nil allowlist")
	}
	for _, name := range []string{"skill", "mcp", "plugin"} {
		if _, ok := al.actions[name]; !ok {
			t.Fatalf("missing command %q", name)
		}
	}
	if normalizeInstallCmd("skills") != "skill" {
		t.Fatalf("skills alias = %q", normalizeInstallCmd("skills"))
	}
	if normalizeInstallCmd("plugins") != "plugin" {
		t.Fatalf("plugins alias = %q", normalizeInstallCmd("plugins"))
	}
	if !isKnownInstallCommand("skills") || !isKnownInstallCommand("mcp") {
		t.Fatal("isKnownInstallCommand failed")
	}
}

func TestInstallActionAllowedFromSharedAllowlist(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"skill", []string{"search"}, true},
		{"skills", []string{"install"}, true}, // alias
		{"skill", []string{"help"}, true},
		{"skill", []string{"--help"}, true},
		{"mcp", []string{"add"}, true},
		{"plugin", []string{"marketplace", "add"}, true},
		{"plugin", []string{"market", "list"}, true}, // nested alias
		{"plugin", []string{"marketplace"}, true},
		{"plugin", []string{"marketplace", "help"}, true},
		{"plugin", []string{"marketplace", "destroy"}, false},
		{"skill", []string{"foo"}, false},
		{"plugin", []string{"enable"}, false},
	}
	for _, tc := range cases {
		got := installActionAllowed(tc.cmd, tc.args)
		if got != tc.want {
			t.Fatalf("installActionAllowed(%q, %v)=%v want %v", tc.cmd, tc.args, got, tc.want)
		}
	}
}

func TestIsInstallCLIBinaryPrefixFromSharedAllowlist(t *testing.T) {
	for _, tok := range []string{
		"maclaw-tui", "maclaw-tui.exe", "maclaw", "tigerclaw-tui.exe",
		`C:\bin\maclaw-tui.exe`, `/usr/bin/maclaw-tui`, `"C:\Program Files\maclaw.exe"`,
	} {
		if !isInstallCLIBinaryPrefix(tok) {
			t.Fatalf("expected match %q", tok)
		}
	}
	if isInstallCLIBinaryPrefix("npm") || isInstallCLIBinaryPrefix("random-tui") {
		t.Fatal("false positive")
	}
}

func TestSplitInstallCommandUsesAllowlist(t *testing.T) {
	head, rest, ok := splitInstallCommand("/skills list")
	if !ok || head != "skills" || len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("got head=%q rest=%v ok=%v", head, rest, ok)
	}
	if _, _, ok := splitInstallCommand("/unknowncmd list"); ok {
		t.Fatal("unknown command should fail split")
	}
}

func TestFrontendBackendParityTable(t *testing.T) {
	// Shared decision table: keep in sync with installCommandAllowlist.test.ts
	type row struct {
		text string
		want bool // whether classifyImmediateIMCommand recognizes install
	}
	rows := []row{
		{"/skill list", true},
		{"/skills search pdf", true},
		{"/mcp install x@y", true},
		{"/plugin marketplace add a/b", true},
		{"/plugin market list", true},
		{"/plugin marketplace destroy", false},
		{"skill is important", false},
		{"maclaw-tui skill list", true},
		{"maclaw-tui /skill list", true},  // slash after binary
		{"maclaw-tui ／skill list", true}, // fullwidth slash after binary
		{`C:\bin\maclaw-tui.exe skill list`, true},
		{`/mcp add --name "my server"`, true},
		{"/skill foo", false},
		{"/skill install help", true}, // target id "help", not nested meta
		{"/plugin marketplace help", true},
	}
	for _, r := range rows {
		kind := classifyImmediateIMCommand(r.text)
		got := kind == imCommandSkill || kind == imCommandMCP || kind == imCommandPlugin || kind == imCommandInstall
		if got != r.want {
			t.Fatalf("classify(%q) install=%v want %v (kind=%v)", r.text, got, r.want, kind)
		}
	}
}

func TestNestedParentsAutoRegisteredAsActions(t *testing.T) {
	// marketplace/market come from nested parents, not only the actions array.
	if !installActionAllowed("plugin", []string{"marketplace", "add"}) {
		t.Fatal("marketplace add")
	}
	if !installActionAllowed("plugin", []string{"market", "refresh"}) {
		t.Fatal("market refresh via nested alias")
	}
}

func TestEveryAllowlistCommandHasCLIRunner(t *testing.T) {
	// Frontend JSON and Go embed share the same allowlist. Every command must
	// have a registered GUI runner so recognition and execution stay in sync.
	al, err := loadInstallCommandAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	for cmd := range al.actions {
		if !hasInstallCLIRunner(cmd) {
			t.Fatalf("allowlist command %q has no installCLIRunners entry", cmd)
		}
	}
	// Runners must not point at unknown commands either (typo guard).
	for cmd := range installCLIRunners {
		if _, ok := al.actions[cmd]; !ok {
			t.Fatalf("runner %q not present in allowlist actions", cmd)
		}
	}
}

// TestAllowlistActionsSubsetOfCLI ensures every chat-exposed action is
// actually accepted by the embedded CLI. Meta help flags are handled by the
// GUI before Run* and are exempt. Nested parents (marketplace) must exist as
// both top-level CLI actions and nested catalog keys.
func TestAllowlistActionsSubsetOfCLI(t *testing.T) {
	var raw installCommandAllowlistFile
	if err := json.Unmarshal(installCommandAllowlistJSON, &raw); err != nil {
		t.Fatalf("parse allowlist: %v", err)
	}
	meta := map[string]struct{}{}
	for _, m := range raw.MetaActions {
		meta[m] = struct{}{}
	}

	for cmd, spec := range raw.Commands {
		if _, ok := commands.InstallCLICatalog[cmd]; !ok {
			t.Fatalf("allowlist command %q missing from commands.InstallCLICatalog", cmd)
		}

		// Nested parent names (and aliases) are auto-registered as top-level
		// actions at load time — verify them against the CLI catalog first.
		for parent, nested := range spec.Nested {
			if !commands.InstallCLIHasNestedParent(cmd, parent) {
				t.Errorf("%s nested parent %q not in CLI catalog Nested", cmd, parent)
			}
			if !commands.InstallCLIHasAction(cmd, parent) {
				t.Errorf("%s nested parent %q not a top-level CLI action", cmd, parent)
			}
			for _, alias := range nested.Aliases {
				if !commands.InstallCLIHasNestedParent(cmd, alias) {
					t.Errorf("%s nested alias %q not in CLI catalog Nested", cmd, alias)
				}
				if !commands.InstallCLIHasAction(cmd, alias) {
					t.Errorf("%s nested alias %q not a top-level CLI action", cmd, alias)
				}
			}
			for _, sub := range nested.Actions {
				if _, isMeta := meta[sub]; isMeta {
					continue
				}
				if !commands.InstallCLIHasNestedAction(cmd, parent, sub) {
					t.Errorf("%s %s %s not accepted by CLI catalog", cmd, parent, sub)
				}
			}
		}

		for _, action := range spec.Actions {
			if _, isMeta := meta[action]; isMeta {
				continue
			}
			if !commands.InstallCLIHasAction(cmd, action) {
				t.Errorf("allowlist %s action %q not accepted by CLI (InstallCLICatalog)", cmd, action)
			}
		}
	}
}

// TestInstallCLIRunnersMatchCatalog keeps GUI runners and CLI catalog in sync.
// Every catalog command needs a runner; every runner must be a catalog command.
func TestInstallCLIRunnersMatchCatalog(t *testing.T) {
	for _, cmd := range commands.InstallCLICommandNames() {
		if !hasInstallCLIRunner(cmd) {
			t.Errorf("InstallCLICatalog command %q has no installCLIRunners entry", cmd)
		}
	}
	for cmd := range installCLIRunners {
		if _, ok := commands.InstallCLICatalog[cmd]; !ok {
			t.Errorf("installCLIRunners %q not in InstallCLICatalog", cmd)
		}
	}
}

