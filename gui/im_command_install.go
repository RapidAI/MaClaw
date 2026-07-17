package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/tui/commands"
)

// Install / search slash commands for the AI assistant panel (and other IM surfaces).
//
// Supported (CLI-compatible):
//
//	/skill search <query>
//	/skill install <skill-id|owner/repo|github-url>
//	/skill list
//	/skill remove <name>
//	/mcp search <query>
//	/mcp install <id|owner/repo|name@marketplace>
//	/mcp list
//	/mcp remove <name>
//	/plugin marketplace add <owner/repo>
//	/plugin marketplace list|remove|refresh
//	/plugin search <query>
//	/plugin add <name@marketplace>
//	/plugin remove <name@marketplace>
//	/plugin installed|list|help
//
// Also accepts CLI-prefixed paste (maclaw-tui skill list) and fullwidth slash (／skill).
// Free-form chat without "/" / "／" or a known binary prefix is never classified.

func classifyInstallIMCommand(trimmed string) imCommandKind {
	head, args, ok := splitInstallCommand(trimmed)
	if !ok {
		return imCommandUnknown
	}
	cmd := normalizeInstallCmd(head)
	// Bare "/skill" / "/mcp" / "/plugin" (no args) is help.
	// With args, require a known action so free-form chat like
	// "skill is important" never hijacks the agent loop.
	if len(args) > 0 && !installActionAllowed(cmd, args) {
		return imCommandUnknown
	}
	// Every install command needs a registered runner (see installCLIRunners).
	if !hasInstallCLIRunner(cmd) {
		return imCommandUnknown
	}
	// Map well-known commands to dedicated kinds; other allowlisted+runnable
	// names use imCommandInstall without a new enum value.
	switch cmd {
	case "skill":
		return imCommandSkill
	case "mcp":
		return imCommandMCP
	case "plugin":
		return imCommandPlugin
	default:
		return imCommandInstall
	}
}

func splitInstallCommand(trimmed string) (head string, rest []string, ok bool) {
	trimmed = strings.TrimSpace(trimmed)
	// Strip BOM (some IM clients paste with U+FEFF).
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "", nil, false
	}
	// Fullwidth slash (／) is common on Chinese IM keyboards.
	hasSlash := false
	switch {
	case strings.HasPrefix(trimmed, "/"):
		hasSlash = true
		trimmed = strings.TrimSpace(trimmed[1:])
	case strings.HasPrefix(trimmed, "／"):
		hasSlash = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "／"))
	}
	// Quote-aware split so `/mcp add --name "my server"` keeps the name intact.
	fields := installCommandFields(trimmed)
	if len(fields) == 0 {
		return "", nil, false
	}
	hasBinary := false
	if isInstallCLIBinaryPrefix(fields[0]) {
		hasBinary = true
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return "", nil, false
	}
	// Free-form chat must use a leading slash or explicit CLI binary prefix.
	// Otherwise "skill is important" would be misclassified.
	if !hasSlash && !hasBinary {
		return "", nil, false
	}
	// After a binary prefix, users may still write `/skill` or `／skill`
	// (e.g. `maclaw-tui /skill list`). Peel that optional slash so the head
	// matches allowlist names (mirrors frontend isInstallCommandText).
	fields[0] = peelInstallCommandSlash(fields[0])
	if fields[0] == "" {
		return "", nil, false
	}
	head = strings.ToLower(fields[0])
	// Command names/aliases come solely from the shared allowlist JSON.
	if !isKnownInstallCommand(head) {
		return "", nil, false
	}
	return head, fields[1:], true
}

// peelInstallCommandSlash strips a leading ASCII or fullwidth slash from a token.
func peelInstallCommandSlash(tok string) string {
	tok = strings.TrimSpace(tok)
	switch {
	case strings.HasPrefix(tok, "/"):
		return strings.TrimSpace(tok[1:])
	case strings.HasPrefix(tok, "／"):
		return strings.TrimSpace(strings.TrimPrefix(tok, "／"))
	default:
		return tok
	}
}

// installCommandFields splits like strings.Fields but keeps quoted segments
// as single tokens (supports "double" and 'single' quotes; quotes are stripped).
func installCommandFields(s string) []string {
	var fields []string
	var b strings.Builder
	var quote rune // 0 when unquoted
	flush := func() {
		if b.Len() == 0 {
			return
		}
		fields = append(fields, b.String())
		b.Reset()
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return fields
}

func (h *IMMessageHandler) handleInstallIMCommand(kind imCommandKind, trimmed, lang string) *IMAgentResponse {
	_ = kind // classification already validated; re-parse for args.
	head, args, ok := splitInstallCommand(trimmed)
	if !ok {
		// Usage mistakes are user guidance, not hard failures.
		return &IMAgentResponse{Text: localizedInstallCommandUsage(lang)}
	}
	// Map skills/plugins aliases to CLI command names.
	cmd := normalizeInstallCmd(head)

	// Bare `/skill` / `/mcp` / `/plugin` → usage help.
	if len(args) == 0 {
		return &IMAgentResponse{Text: localizedInstallCommandHelp(cmd, lang)}
	}

	action := strings.ToLower(args[0])
	// Help aliases from shared meta_actions (e.g. /skill help, /mcp --help).
	if isInstallMetaAction(action) {
		return &IMAgentResponse{Text: localizedInstallCommandHelp(cmd, lang)}
	}
	// Nested parent help only: `/plugin marketplace help`.
	// Do NOT treat the second token as meta for install/add targets
	// (e.g. `/skill install help` must try to install id "help").
	if len(args) >= 2 && isInstallMetaAction(args[1]) && isInstallNestedParent(cmd, action) {
		return &IMAgentResponse{Text: localizedInstallCommandHelp(cmd, lang)}
	}

	// Guard dangerous/unknown bare actions for skill/mcp (list/search/install/remove only).
	if !installActionAllowed(cmd, args) {
		return &IMAgentResponse{Text: localizedInstallCommandUsage(lang) + "\n\n" + localizedInstallCommandHelp(cmd, lang)}
	}

	out, err := runInstallCLI(cmd, args)
	out = strings.TrimSpace(out)
	if err != nil {
		// flag -h / --help from embedded CLI → show our help, not a red error.
		if errors.Is(err, flag.ErrHelp) {
			if out != "" {
				return &IMAgentResponse{Text: localizedInstallCommandResultPrefix(lang, cmd) + out}
			}
			return &IMAgentResponse{Text: localizedInstallCommandHelp(cmd, lang)}
		}
		// UsageError is instructional (wrong args), not a system failure.
		var ue *commands.UsageError
		if errors.As(err, &ue) {
			return &IMAgentResponse{Text: localizedInstallCommandResultPrefix(lang, cmd) + mergeInstallCLIText(ue.Error(), out)}
		}
		return &IMAgentResponse{Error: mergeInstallCLIText(err.Error(), out)}
	}
	if out == "" {
		out = localizedInstallCommandOK(lang, cmd, action)
	}
	// Prefix so users see it was a local install command, not LLM prose.
	return &IMAgentResponse{Text: localizedInstallCommandResultPrefix(lang, cmd) + out}
}

// installCLIRunners maps canonical command names to embedded CLI runners.
// Keys must match commands.InstallCLICatalog (enforced by TestInstallCLIRunnersMatchCatalog).
// Classification can accept JSON-only names via imCommandInstall only when a
// runner is registered here.
var installCLIRunners = map[string]func([]string) (string, error){
	"skill":  commands.RunSkillCaptured,
	"mcp":    commands.RunMCPCaptured,
	"plugin": commands.RunPluginCaptured,
}

func hasInstallCLIRunner(cmd string) bool {
	_, ok := installCLIRunners[normalizeInstallCmd(cmd)]
	return ok
}

func runInstallCLI(cmd string, args []string) (string, error) {
	cmd = normalizeInstallCmd(cmd)
	run, ok := installCLIRunners[cmd]
	if !ok {
		return "", fmt.Errorf("install command %q is allowlisted but has no GUI runner yet", cmd)
	}
	return run(args)
}

// mergeInstallCLIText combines err text with captured CLI output without duplication.
func mergeInstallCLIText(errMsg, out string) string {
	msg := strings.TrimSpace(errMsg)
	out = strings.TrimSpace(out)
	if out == "" {
		return msg
	}
	if msg == "" {
		return out
	}
	if strings.Contains(msg, out) {
		return msg
	}
	return msg + "\n" + out
}

func localizedInstallCommandResultPrefix(lang, cmd string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("✓ /%s\n\n", cmd)
	case appLanguageZhHant:
		return fmt.Sprintf("✓ /%s 執行結果\n\n", cmd)
	default:
		return fmt.Sprintf("✓ /%s 执行结果\n\n", cmd)
	}
}

func localizedInstallCommandOK(lang, cmd, action string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("/%s %s completed.", cmd, action)
	case appLanguageZhHant:
		return fmt.Sprintf("/%s %s 已完成。", cmd, action)
	default:
		return fmt.Sprintf("/%s %s 已完成。", cmd, action)
	}
}

func localizedInstallCommandUsage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Install command usage: /skill|/mcp|/plugin <action> ..."
	case appLanguageZhHant:
		return "安裝命令用法：/skill|/mcp|/plugin <action> ..."
	default:
		return "安装命令用法：/skill|/mcp|/plugin <action> ..."
	}
}

type installHelpLines struct {
	title    string
	body     []string
	examples []string
	exLabel  string
}

// installHelpLocale holds the small set of localized tokens used by help text.
// Command skeletons stay shared so en/zh-Hans/zh-Hant cannot drift.
type installHelpLocale struct {
	exLabel string
	query   string // <query> / <关键词> / <關鍵詞>
	name    string // <name> / <名称> / <名稱>
	skillT  string
	mcpT    string
	pluginT string
}

func installHelpLocaleFor(lang string) installHelpLocale {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return installHelpLocale{
			exLabel: "Examples:",
			query:   "query",
			name:    "name",
			skillT:  "Skill commands:",
			mcpT:    "MCP commands:",
			pluginT: "Plugin marketplace (Codex-style):",
		}
	case appLanguageZhHant:
		return installHelpLocale{
			exLabel: "示例：",
			query:   "關鍵詞",
			name:    "名稱",
			skillT:  "Skill 命令：",
			mcpT:    "MCP 命令：",
			pluginT: "Plugin 市場（Codex 風格）：",
		}
	default: // zh-Hans
		return installHelpLocale{
			exLabel: "示例：",
			query:   "关键词",
			name:    "名称",
			skillT:  "Skill 命令：",
			mcpT:    "MCP 命令：",
			pluginT: "Plugin 市场（Codex 风格）：",
		}
	}
}

func localizedInstallCommandHelp(cmd, lang string) string {
	cmd = normalizeInstallCmd(cmd)
	loc := installHelpLocaleFor(lang)
	var h installHelpLines
	switch cmd {
	case "skill":
		h = installHelpLines{
			title: loc.skillT,
			body: []string{
				"/skill search <" + loc.query + ">",
				"/skill install <skill-id|owner/repo|github-url>",
				"/skill list",
				"/skill remove <" + loc.name + ">",
			},
			exLabel:  loc.exLabel,
			examples: []string{"/skill search pdf", "/skill install anthropics/skills"},
		}
	case "mcp":
		h = installHelpLines{
			title: loc.mcpT,
			body: []string{
				"/mcp search <" + loc.query + ">",
				"/mcp install <id|owner/repo|name@marketplace>",
				"/mcp list",
				"/mcp remove <" + loc.name + ">",
				"/mcp add --name <n> --command <cmd> | --url <endpoint>",
			},
			exLabel: loc.exLabel,
			examples: []string{
				"/mcp search jira",
				"/mcp install ida-pro-mcp@mrexodia",
				"/mcp install mrexodia/ida-pro-mcp",
			},
		}
	case "plugin":
		h = installHelpLines{
			title: loc.pluginT,
			body: []string{
				"/plugin marketplace add <owner/repo>",
				"/plugin marketplace list",
				"/plugin marketplace remove <" + loc.name + ">",
				"/plugin search <" + loc.query + ">",
				"/plugin add <name@marketplace>",
				"/plugin remove <name@marketplace>",
				"/plugin installed",
			},
			exLabel: loc.exLabel,
			examples: []string{
				"/plugin marketplace add mrexodia/codex-marketplace",
				"/plugin add ida-pro-mcp@mrexodia",
			},
		}
	default:
		// Unknown/JSON-only command without dedicated copy — generic usage only.
		return localizedInstallCommandUsage(lang)
	}
	return formatInstallHelp(h)
}

func formatInstallHelp(h installHelpLines) string {
	var b strings.Builder
	b.WriteString(h.title)
	b.WriteByte('\n')
	for _, line := range h.body {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(h.examples) > 0 {
		b.WriteString(h.exLabel)
		b.WriteByte('\n')
		for _, line := range h.examples {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func installSlashHelpBlock(lang string) string {
	// Always end with a trailing newline so concatenation with the next /help
	// line does not glue "marketplace/summary" into one token.
	// Shared command skeletons + localized gloss/example markers only.
	var skillGloss, mcpGloss, pluginGloss, eg string
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		skillGloss = "search/install skills"
		mcpGloss = "search/install MCP servers"
		pluginGloss = "Codex-style plugins"
		eg = "e.g. " // trailing space before command
	case appLanguageZhHant:
		skillGloss = "搜索/安裝 Skill"
		mcpGloss = "搜索/安裝 MCP"
		pluginGloss = "Codex 風格插件"
		eg = "例："
	default:
		skillGloss = "搜索/安装 Skill"
		mcpGloss = "搜索/安装 MCP"
		pluginGloss = "Codex 风格插件"
		eg = "例："
	}
	return "/skill search|install|list|remove - " + skillGloss + "\n" +
		"/mcp search|install|list|remove - " + mcpGloss + "\n" +
		"/plugin marketplace add|list ; /plugin add|remove name@market - " + pluginGloss + "\n" +
		"    " + eg + "/plugin marketplace add mrexodia/codex-marketplace\n" +
		"    " + eg + "/plugin add ida-pro-mcp@mrexodia\n" +
		"    " + eg + "/skill install owner/repo\n" +
		"    " + eg + "/mcp install name@marketplace\n"
}
