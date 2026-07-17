package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyInstallIMCommandRequiresSlash(t *testing.T) {
	// Free-form chat must NOT be captured.
	for _, text := range []string{
		"skill is important",
		"mcp server looks good",
		"plugin system design",
		"please install skill pdf",
	} {
		if kind := classifyImmediateIMCommand(text); kind != imCommandUnknown {
			t.Fatalf("text %q classified as %v, want unknown", text, kind)
		}
	}
}

func TestClassifyInstallIMCommandSlash(t *testing.T) {
	cases := map[string]imCommandKind{
		"/skill":                              imCommandSkill,
		"/skill list":                         imCommandSkill,
		"/skill search pdf":                   imCommandSkill,
		"/skill install owner/repo":           imCommandSkill,
		"/mcp list":                           imCommandMCP,
		"/mcp install ida-pro-mcp@mrexodia":   imCommandMCP,
		"/plugin marketplace add a/b":         imCommandPlugin,
		"/plugin add ida-pro-mcp@mrexodia":    imCommandPlugin,
		"/plugin installed":                   imCommandPlugin,
		"/PLUGIN add x@y":                     imCommandPlugin,
		"maclaw-tui skill list":               imCommandSkill,
		"maclaw-tui plugin marketplace list":  imCommandPlugin,
	}
	for text, want := range cases {
		got := classifyImmediateIMCommand(text)
		if got != want {
			t.Fatalf("classifyImmediateIMCommand(%q)=%v want %v", text, got, want)
		}
	}
}

func TestClassifyInstallIMCommandRejectsUnknownActions(t *testing.T) {
	// Slash with unknown action should fall through to agent (or workbench), not install handler.
	for _, text := range []string{
		"/skill foo",
		"/mcp bar baz",
		"/plugin destroy all",
		"/plugin marketplace destroy",
	} {
		if kind := classifyInstallIMCommand(text); kind != imCommandUnknown {
			t.Fatalf("%q should not be install command, got %v", text, kind)
		}
	}
}

func TestClassifyInstallIMCommandHelpActions(t *testing.T) {
	for _, text := range []string{"/skill help", "/mcp --help", "/plugin -h", "/plugin marketplace help"} {
		if kind := classifyInstallIMCommand(text); kind == imCommandUnknown {
			t.Fatalf("%q should be install command", text)
		}
	}
}

func TestInstallActionAllowedMarketplace(t *testing.T) {
	if !installActionAllowed("plugin", []string{"marketplace"}) {
		t.Fatal("bare marketplace should be allowed")
	}
	if !installActionAllowed("plugin", []string{"marketplace", "add"}) {
		t.Fatal("marketplace add should be allowed")
	}
	if installActionAllowed("plugin", []string{"marketplace", "destroy"}) {
		t.Fatal("marketplace destroy should be rejected")
	}
}

func TestSplitInstallCommand(t *testing.T) {
	head, rest, ok := splitInstallCommand("/plugin add ida-pro-mcp@mrexodia")
	if !ok || head != "plugin" || len(rest) != 2 || rest[0] != "add" || rest[1] != "ida-pro-mcp@mrexodia" {
		t.Fatalf("got head=%q rest=%v ok=%v", head, rest, ok)
	}
	head, rest, ok = splitInstallCommand("maclaw-tui skill install acme/tool")
	if !ok || head != "skill" || len(rest) < 2 {
		t.Fatalf("got head=%q rest=%v ok=%v", head, rest, ok)
	}
}

func TestHandleInstallIMCommandPluginHelp(t *testing.T) {
	h := &IMMessageHandler{}
	resp := h.handleInstallIMCommand(imCommandPlugin, "/plugin", "zh")
	if resp == nil || resp.Text == "" {
		t.Fatal("expected help text")
	}
	if !strings.Contains(resp.Text, "marketplace") {
		t.Fatalf("help missing marketplace: %s", resp.Text)
	}
}

func TestHandleInstallIMCommandPluginMarketplaceList(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	// Ensure plugins path is writable.
	_ = os.MkdirAll(filepath.Join(dataDir, "plugins"), 0o755)

	h := &IMMessageHandler{}
	resp := h.handleInstallIMCommand(imCommandPlugin, "/plugin marketplace list", "en")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != "" {
		t.Fatalf("error: %s", resp.Error)
	}
	if !strings.Contains(resp.Text, "marketplace") && !strings.Contains(strings.ToLower(resp.Text), "no plugin") {
		t.Fatalf("unexpected text: %s", resp.Text)
	}
}

func TestHandleInstallIMCommandSkillList(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	h := &IMMessageHandler{}
	resp := h.handleInstallIMCommand(imCommandSkill, "/skill list", "zh")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != "" {
		// empty skills dir may still succeed with "No skills found"
		t.Fatalf("error: %s", resp.Error)
	}
	if resp.Text == "" {
		t.Fatal("empty text")
	}
}

func TestHandleInstallIMCommandUsageIsTextNotError(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	h := &IMMessageHandler{}
	// install without target → UsageError from CLI, should surface as Text guidance.
	resp := h.handleInstallIMCommand(imCommandSkill, "/skill install", "en")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != "" {
		t.Fatalf("usage should not be Error field: %s", resp.Error)
	}
	if resp.Text == "" || !strings.Contains(strings.ToLower(resp.Text), "install") {
		t.Fatalf("expected usage text, got %q", resp.Text)
	}
}

func TestIsInstallCLIBinaryPrefix(t *testing.T) {
	for _, tok := range []string{"maclaw-tui", "maclaw-tui.exe", "maclaw", "maclaw.exe", "tigerclaw-tui.exe"} {
		if !isInstallCLIBinaryPrefix(tok) {
			t.Fatalf("expected prefix match for %q", tok)
		}
	}
	if isInstallCLIBinaryPrefix("npm") || isInstallCLIBinaryPrefix("skill") {
		t.Fatal("false positive binary prefix")
	}
}

func TestHelpIncludesInstallCommands(t *testing.T) {
	text := localizedIMSlashHelpText("zh")
	for _, want := range []string{"/skill", "/mcp", "/plugin", "/summary"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %s:\n%s", want, text)
		}
	}
	// Regression: install block must end with newline so it does not glue to /summary.
	if strings.Contains(text, "marketplace/summary") || strings.Contains(text, "name@marketplace/summary") {
		t.Fatalf("install help glued to /summary:\n%s", text)
	}
	if !strings.Contains(text, "name@marketplace\n/summary") && !strings.Contains(text, "name@marketplace\n") {
		// At least ensure a newline separates marketplace example from whatever follows.
		idx := strings.Index(text, "name@marketplace")
		if idx < 0 {
			t.Fatalf("missing marketplace example:\n%s", text)
		}
		rest := text[idx+len("name@marketplace"):]
		if !strings.HasPrefix(rest, "\n") {
			t.Fatalf("expected newline after marketplace example, rest=%q", rest[:min(20, len(rest))])
		}
	}
}

func TestHandleImmediateInstallCommand(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	h := &IMMessageHandler{}
	resp, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: "u1", Platform: "desktop", Text: "/plugin", Lang: "en"},
		"/plugin",
		nil,
		nil,
	)
	if !handled || resp == nil {
		t.Fatalf("handled=%v resp=%v", handled, resp)
	}
	if !strings.Contains(resp.Text, "Plugin") && !strings.Contains(resp.Text, "plugin") {
		t.Fatalf("unexpected: %s", resp.Text)
	}
}

func TestHandleImmediateInstallCommandFromIMPlatform(t *testing.T) {
	// Weixin / QQ / Telegram share the same immediate-command path as desktop.
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	h := &IMMessageHandler{}
	for _, platform := range []string{"weixin", "qq", "telegram"} {
		resp, handled := h.handleImmediateIMCommand(
			IMUserMessage{UserID: "im-user", Platform: platform, Text: "/skill list", Lang: "zh"},
			"/skill list",
			nil,
			nil,
		)
		if !handled || resp == nil {
			t.Fatalf("platform %s: handled=%v resp=%v", platform, handled, resp)
		}
		if resp.Error != "" {
			t.Fatalf("platform %s: error %s", platform, resp.Error)
		}
		if resp.Text == "" {
			t.Fatalf("platform %s: empty text", platform)
		}
	}
}

func TestSplitInstallCommandFullwidthSlashAndBOM(t *testing.T) {
	// Chinese IM keyboards often emit fullwidth slash.
	head, rest, ok := splitInstallCommand("／skill list")
	if !ok || head != "skill" || len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("fullwidth: head=%q rest=%v ok=%v", head, rest, ok)
	}
	// BOM-prefixed paste.
	head, rest, ok = splitInstallCommand("\ufeff/mcp list")
	if !ok || head != "mcp" || len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("bom: head=%q rest=%v ok=%v", head, rest, ok)
	}
	if kind := classifyImmediateIMCommand("／plugin marketplace list"); kind != imCommandPlugin {
		t.Fatalf("classify fullwidth plugin = %v", kind)
	}
}

func TestSplitInstallCommandBinaryThenSlash(t *testing.T) {
	// Paste forms: maclaw-tui /skill list  and  maclaw-tui ／skill list
	// (slash after binary — previously failed because head was "/skill").
	for _, text := range []string{
		"maclaw-tui /skill list",
		"maclaw-tui ／skill list",
		"maclaw-tui.exe /plugin marketplace list",
	} {
		head, rest, ok := splitInstallCommand(text)
		if !ok {
			t.Fatalf("%q: split failed", text)
		}
		if head != "skill" && head != "plugin" {
			t.Fatalf("%q: head=%q", text, head)
		}
		if len(rest) < 1 {
			t.Fatalf("%q: rest=%v", text, rest)
		}
		if kind := classifyImmediateIMCommand(text); kind == imCommandUnknown {
			t.Fatalf("%q: not classified", text)
		}
	}
}

func TestInstallCommandFieldsQuoted(t *testing.T) {
	got := installCommandFields(`add --name "my server" --command "npx foo"`)
	want := []string{"add", "--name", "my server", "--command", "npx foo"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d got %q want %q", i, got[i], want[i])
		}
	}
	// Single quotes.
	got = installCommandFields(`search 'hello world'`)
	if len(got) != 2 || got[1] != "hello world" {
		t.Fatalf("single quotes: %v", got)
	}
}

func TestSplitInstallCommandQuotedAndPathBinary(t *testing.T) {
	head, rest, ok := splitInstallCommand(`/mcp add --name "my server" --url http://x`)
	if !ok || head != "mcp" {
		t.Fatalf("head=%q rest=%v ok=%v", head, rest, ok)
	}
	if len(rest) < 4 || rest[2] != "my server" {
		t.Fatalf("rest=%v", rest)
	}
	// Path basename binary prefix.
	for _, text := range []string{
		`C:\tools\maclaw-tui.exe skill list`,
		`/usr/local/bin/maclaw-tui skill list`,
		`"C:\Program Files\maclaw-tui.exe" skill list`,
	} {
		h, r, ok := splitInstallCommand(text)
		if !ok || h != "skill" || len(r) != 1 || r[0] != "list" {
			t.Fatalf("%q: head=%q rest=%v ok=%v", text, h, r, ok)
		}
	}
}

func TestInstallHelpKeepsSharedSkeletons(t *testing.T) {
	// Command paths must be identical across locales so docs/parity do not drift.
	en := localizedInstallCommandHelp("skill", "en")
	zh := localizedInstallCommandHelp("skill", "zh")
	for _, want := range []string{"/skill search", "/skill install", "/skill list", "/skill remove"} {
		if !strings.Contains(en, want) || !strings.Contains(zh, want) {
			t.Fatalf("missing %s in help\nen=%s\nzh=%s", want, en, zh)
		}
	}
	block := installSlashHelpBlock("zh")
	if !strings.HasSuffix(block, "\n") {
		t.Fatal("installSlashHelpBlock must end with newline")
	}
	if strings.Contains(block, "marketplace/summary") {
		t.Fatal("glued help block")
	}
}

func TestNestedMetaHelpDoesNotSwallowInstallTargets(t *testing.T) {
	// /plugin marketplace help → meta on nested parent → still install kind.
	if kind := classifyInstallIMCommand("/plugin marketplace help"); kind != imCommandPlugin {
		t.Fatalf("marketplace help kind=%v", kind)
	}
	// /skill install help must NOT be rewritten as help; install is a real action.
	if !installActionAllowed("skill", []string{"install", "help"}) {
		t.Fatal("skill install help should be allowed as install action")
	}
	if isInstallNestedParent("skill", "install") {
		t.Fatal("install is not a nested parent")
	}
	if !isInstallNestedParent("plugin", "marketplace") || !isInstallNestedParent("plugin", "market") {
		t.Fatal("marketplace/market should be nested parents")
	}
}

func TestHasInstallCLIRunner(t *testing.T) {
	for _, cmd := range []string{"skill", "mcp", "plugin", "skills", "plugins"} {
		if !hasInstallCLIRunner(cmd) {
			t.Fatalf("expected runner for %q", cmd)
		}
	}
	if hasInstallCLIRunner("nope") {
		t.Fatal("unexpected runner")
	}
}

func TestMergeInstallCLIText(t *testing.T) {
	if got := mergeInstallCLIText("err", "out"); got != "err\nout" {
		t.Fatalf("got %q", got)
	}
	if got := mergeInstallCLIText("err with out inside", "out"); got != "err with out inside" {
		t.Fatalf("dedupe got %q", got)
	}
	if got := mergeInstallCLIText("  ", "only-out"); got != "only-out" {
		t.Fatalf("empty err got %q", got)
	}
	if got := mergeInstallCLIText("only-err", ""); got != "only-err" {
		t.Fatalf("empty out got %q", got)
	}
}
