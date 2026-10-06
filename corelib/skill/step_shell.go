package skill

import "strings"

// Step shells a bash-action command can run under. The requirement checker
// and every runner must use ResolveStepShell so a token is treated as a PATH
// dependency only when that interpreter will look it up as an external program.
const (
	StepShellBash       = "bash"
	StepShellCmd        = "cmd"
	StepShellPowerShell = "powershell"
)

// ResolveStepShell chooses the interpreter for one bash-action command.
// preferredShell wins. Otherwise the command's own words decide: a PowerShell
// cmdlet cannot run in cmd.exe or bash, a Unix-only tool cannot run in
// cmd.exe, and cmd.exe already implements pipes, redirection, and && / ||.
// Treating those operators as "needs bash" sent Windows shell commands to the
// wrong interpreter and then failed the PATH precheck.
func ResolveStepShell(command, preferredShell, goos string) (string, string) {
	shell, reason, doubleQuoted, ok := stepShellWithoutWords(command, preferredShell, goos)
	if ok {
		return shell, reason
	}
	decided := finishStepShell(command, extractCommandWords(command), doubleQuoted)
	return decided.shell, decided.why
}

// classifyStepShell resolves the interpreter and returns the command words
// parsed for that decision. Requirement inference uses this so a step is
// tokenized once. Execution uses ResolveStepShell, which skips tokenization
// when a prefix or PowerShell here-string already decides the interpreter.
func classifyStepShell(command, preferredShell, goos string) (string, string, []string) {
	shell, reason, doubleQuoted, ok := stepShellWithoutWords(command, preferredShell, goos)
	if ok {
		return shell, reason, extractCommandWords(command)
	}
	words := extractCommandWords(command)
	decided := finishStepShell(command, words, doubleQuoted)
	return decided.shell, decided.why, words
}

type stepShellDecision struct {
	shell string
	why   string
}

// stepShellWithoutWords reports decisions that do not need command tokenization.
// doubleQuoted is set when the command was not decided, so the caller can use
// the scan it already paid for.
func stepShellWithoutWords(command, preferredShell, goos string) (shell, reason string, doubleQuoted, ok bool) {
	switch normalizeStepShellPreference(preferredShell) {
	case StepShellPowerShell:
		return StepShellPowerShell, "skill metadata preferred_shell=powershell", false, true
	case StepShellBash:
		return StepShellBash, "skill metadata preferred_shell=bash", false, true
	case StepShellCmd:
		return StepShellCmd, "skill metadata preferred_shell=cmd", false, true
	}
	if goos != "windows" {
		return StepShellBash, "non-windows default", false, true
	}
	if hasShebang(command) {
		return StepShellBash, "detected Unix-specific syntax in command", false, true
	}
	// A here-string is PowerShell even when the first word is python/node:
	// the here-string is the interpreter's argument. A bare $env: in an
	// interpreter command is source text and stays on cmd. export/source are
	// not decided here: a later cmdlet has to win.
	unquotedEnv, doubleQuoted, hereString := powershellSyntaxSignals(command)
	if hereString {
		return StepShellPowerShell, "detected PowerShell cmdlet or syntax", false, true
	}
	if unquotedEnv && !startsWithInterpreter(command) {
		return StepShellPowerShell, "detected PowerShell cmdlet or syntax", false, true
	}
	return "", "", doubleQuoted, false
}

func finishStepShell(command string, words []string, doubleQuotedEnv bool) stepShellDecision {
	syntax := scanShellSyntax(command)
	if commandNeedsPowerShell(command, words, doubleQuotedEnv, syntax) {
		return stepShellDecision{StepShellPowerShell, "detected PowerShell cmdlet or syntax"}
	}
	if commandNeedsUnixShell(command, words, syntax) {
		return stepShellDecision{StepShellBash, "detected Unix-specific syntax in command"}
	}
	return stepShellDecision{StepShellCmd, "default (cmd.exe)"}
}

func normalizeStepShellPreference(preferredShell string) string {
	switch strings.ToLower(strings.TrimSpace(preferredShell)) {
	case "powershell", "pwsh", "ps", "ps1":
		return StepShellPowerShell
	case "bash", "sh", "zsh":
		return StepShellBash
	case "cmd", "cmd.exe", "windows", "win_cmd":
		return StepShellCmd
	default:
		return ""
	}
}

// shellProvidesCommand reports that shell resolves name without PATH.
// POSIX builtins are handled separately so they stay non-dependencies on
// every host, matching the historical shellBuiltins set.
func shellProvidesCommand(name, shell string) bool {
	switch shell {
	case StepShellCmd:
		return cmdInternals[strings.ToLower(strings.TrimSpace(name))]
	case StepShellPowerShell:
		return powershellResolves(name)
	default:
		return false
	}
}

func powershellResolves(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if isPowerShellCmdlet(name) || powershellAliases[strings.ToLower(name)] || powershellNames[strings.ToLower(name)] || powershellKeywords[strings.ToLower(name)] {
		return true
	}
	// PowerShell aliases the common cmd internals (copy, dir, del, ...).
	return cmdInternals[strings.ToLower(name)]
}

func commandNeedsPowerShell(command string, words []string, doubleQuotedEnv bool, syntax shellSyntax) bool {
	// Syntax that the fast path already resolved is not scanned again.
	// doubleQuotedEnv is the leftover from that scan. An interpreter command
	// keeps "$env:" as source text, but a later Copy-Item still selects PowerShell.
	interpreter := startsWithInterpreter(command)
	if !interpreter && doubleQuotedEnv && !wordsIncludeInterpreter(words) {
		return true
	}
	if wordsIncludeUnixOnlyTool(words) && !wordsIncludeCmdlet(words) {
		return false
	}
	// A trailing backtick continues a PowerShell line. $($name) and $(Get-Date)
	// are PowerShell subexpressions; $(date) is bash and stays in shellSyntax.unix.
	if syntax.psBacktick || syntax.psSubexpr {
		return true
	}
	return wordsIncludeCmdlet(words) || wordsIncludePowerShellAlias(words)
}

func wordsIncludeCmdlet(words []string) bool {
	for _, word := range words {
		word = normalizeInferredCommandName(word)
		if isPowerShellCmdlet(word) || powershellAliases[strings.ToLower(word)] {
			return true
		}
	}
	return false
}

func wordsIncludePowerShellAlias(words []string) bool {
	for _, word := range words {
		if powershellNames[strings.ToLower(normalizeInferredCommandName(word))] {
			return true
		}
	}
	return false
}

// powershellSyntaxSignals reports PowerShell syntax outside of single quotes.
// Single quotes are literal in PowerShell; double quotes expand $env:.
func powershellSyntaxSignals(command string) (unquotedEnv, doubleQuotedEnv, hereString bool) {
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			if quote == '"' && strings.HasPrefix(command[i:], "$env:") {
				doubleQuotedEnv = true
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			continue
		}
		switch {
		case strings.HasPrefix(command[i:], "$env:"):
			unquotedEnv = true
		case hereStringOpenAt(command, i):
			hereString = true
		}
		// Either signal selects PowerShell by itself. Stop before a large
		// here-string body is scanned again.
		if unquotedEnv || hereString {
			return unquotedEnv, doubleQuotedEnv, hereString
		}
	}
	return unquotedEnv, doubleQuotedEnv, hereString
}

func startsWithInterpreter(command string) bool {
	command = skipLeadingShellComments(command)
	for _, prefix := range interpreterCommandPrefixes {
		if hasFoldPrefix(command, prefix) {
			return true
		}
	}
	return pythonLauncherPrefix(command)
}

// skipLeadingShellComments drops full-line comments so a later interpreter
// or command word is what the shell decision sees.
func skipLeadingShellComments(command string) string {
	for {
		command = strings.TrimLeft(command, " \t\r\n")
		if !strings.HasPrefix(command, "#") {
			return command
		}
		next := strings.IndexByte(command, '\n')
		if next < 0 {
			return ""
		}
		command = command[next+1:]
	}
}

// pythonLauncherPrefix matches python3.11 / pythonw, which the fixed prefix
// list cannot spell. The token must be the whole first word.
func pythonLauncherPrefix(command string) bool {
	i := 0
	for i < len(command) && command[i] != ' ' && command[i] != '\t' && command[i] != '\r' && command[i] != '\n' {
		i++
	}
	if i == 0 {
		return false
	}
	word := strings.TrimSuffix(strings.ToLower(command[:i]), ".exe")
	return isPythonCommand(word)
}

func hasShebang(command string) bool {
	return strings.HasPrefix(strings.TrimLeft(command, " \t"), "#!/")
}

func hasUnixShellPrefix(command string) bool {
	command = skipLeadingShellComments(command)
	return hasFoldPrefix(command, "export ") || hasFoldPrefix(command, "source ")
}

func hasFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func wordsIncludeInterpreter(words []string) bool {
	for _, word := range words {
		if isInterpreterWord(word) {
			return true
		}
	}
	return false
}

func isInterpreterWord(word string) bool {
	word = strings.ToLower(normalizeInferredCommandName(word))
	word = strings.TrimSuffix(word, ".exe")
	if isPythonCommand(word) {
		return true
	}
	switch word {
	case "node", "npm", "npx", "pip", "java", "pnpm", "cargo":
		return true
	default:
		return false
	}
}

func isPythonCommand(word string) bool {
	switch word {
	case "python", "pythonw", "py":
		return true
	}
	rest, ok := strings.CutPrefix(word, "python")
	if !ok || rest == "" || rest[0] < '0' || rest[0] > '9' {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// lineIsShellComment reports a line whose first non-space character is #.
func lineIsShellComment(line string) bool {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t', '\r':
			continue
		default:
			return line[i] == '#'
		}
	}
	return false
}

func quoteStateAfter(line string, quote byte) byte {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && quote == '"' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\\' && i+1 < len(line) {
			i++
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		}
	}
	return quote
}

func isPowerShellCmdlet(name string) bool {
	i := strings.IndexByte(name, '-')
	if i <= 0 || i >= len(name)-1 {
		return false
	}
	verb := strings.ToLower(name[:i])
	if !powershellVerbs[verb] {
		return false
	}
	for _, r := range name[i+1:] {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// commandNeedsUnixShell reports syntax or tools cmd.exe cannot execute.
// Pipes and && / || are not themselves Unix syntax: cmd.exe implements them.
// A Unix-only command word selects bash on its own.
func commandNeedsUnixShell(command string, words []string, syntax shellSyntax) bool {
	if hasUnixShellPrefix(command) {
		return true
	}
	// python/node stay on cmd when the rest is not a Unix tool. A leading
	// comment does not change that; cmd strips the comment line.
	if startsWithInterpreter(command) {
		return false
	}
	// A Unix tool needs bash even with no pipe. cmd.exe can run the operators
	// themselves, so a .py/.js path must not override that: `ls file.py` is
	// still ls. A # comment does not choose the shell: cmd strips those lines,
	// and PowerShell uses the same character.
	if wordsIncludeUnixOnlyTool(words) {
		return true
	}
	return syntax.unix
}

// shellSyntax is one quote-aware pass over bash and PowerShell markers.
// Quoted text is source, not syntax.
type shellSyntax struct {
	psBacktick bool
	psSubexpr  bool
	unix       bool
}

func scanShellSyntax(command string) shellSyntax {
	var out shellSyntax
	quote := byte(0)
	openTick := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			continue
		}
		if c == '`' {
			if !openTick && powershellBacktickAt(command, i) {
				out.psBacktick = true
			} else if !powershellBacktickAt(command, i) {
				out.unix = true
			}
			openTick = !openTick
			continue
		}
		if strings.HasPrefix(command[i:], "$(") {
			if powerShellSubexprAt(command, i) {
				out.psSubexpr = true
			} else {
				out.unix = true
			}
			continue
		}
		if strings.HasPrefix(command[i:], "~/") || strings.HasPrefix(command[i:], "<<") ||
			strings.HasPrefix(command[i:], "/*") || strings.HasPrefix(command[i:], "*/") {
			out.unix = true
		}
	}
	return out
}

func powerShellSubexprAt(command string, i int) bool {
	j := i + 2
	for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
		j++
	}
	if j >= len(command) {
		return false
	}
	if command[j] == '$' {
		return true
	}
	start := j
	for j < len(command) {
		c := command[j]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			j++
			continue
		}
		break
	}
	if j == start {
		return false
	}
	word := command[start:j]
	lower := strings.ToLower(word)
	return isPowerShellCmdlet(word) || powershellAliases[lower] || powershellNames[lower]
}

func powershellBacktickAt(command string, i int) bool {
	if i+1 >= len(command) {
		return true
	}
	switch command[i+1] {
	case '0', 'a', 'b', 'e', 'f', 'n', 'r', 't', 'u', 'v', '`', '"', '\'', '$':
		return true
	}
	j := i + 1
	for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
		j++
	}
	return j >= len(command) || command[j] == '\n' || command[j] == '\r'
}

func wordsIncludeUnixOnlyTool(words []string) bool {
	for _, word := range words {
		if unixOnlyCommands[strings.ToLower(normalizeInferredCommandName(word))] {
			return true
		}
	}
	return false
}

var interpreterCommandPrefixes = []string{
	"node ", "python ", "python.exe ", "python3 ", "py ", "py.exe ",
	"java ", "npm ", "pip ", "npx ", "go run ", "cargo run ", "pnpm ",
}

// powershellVerbs is the set of approved PowerShell verb prefixes. A token is
// a cmdlet only when it is Verb-Noun with one of these verbs, so names like
// docker-compose or xparse-cli stay external commands.
var powershellVerbs = map[string]bool{
	"add": true, "approve": true, "assert": true, "backup": true, "block": true,
	"build": true, "checkpoint": true, "clear": true, "close": true, "compare": true, "complete": true,
	"compress": true, "confirm": true, "connect": true, "convert": true, "convertfrom": true,
	"convertto": true, "copy": true, "debug": true, "deny": true, "deploy": true, "disable": true,
	"disconnect": true, "dismount": true, "edit": true, "enable": true, "enter": true,
	"exit": true, "expand": true, "export": true, "find": true, "format": true,
	"get": true, "grant": true, "group": true, "hide": true, "import": true,
	"initialize": true, "install": true, "invoke": true, "join": true, "limit": true,
	"lock": true, "measure": true, "merge": true, "mount": true, "move": true,
	"new": true, "open": true, "optimize": true, "out": true, "ping": true,
	"pop": true, "protect": true, "publish": true, "push": true, "read": true,
	"receive": true, "redo": true, "register": true, "remove": true, "rename": true,
	"repair": true, "request": true, "reset": true, "resize": true, "resolve": true,
	"restart": true, "restore": true, "resume": true, "revoke": true, "save": true,
	"search": true, "select": true, "send": true, "set": true, "show": true,
	"skip": true, "sort": true, "split": true, "start": true, "step": true, "stop": true,
	"submit": true, "suspend": true, "switch": true, "sync": true, "tee": true,
	"test": true, "trace": true, "unblock": true, "undo": true, "uninstall": true,
	"unlock": true, "unprotect": true, "unpublish": true, "unregister": true,
	"update": true, "use": true, "wait": true, "watch": true, "where": true,
	"write": true, "foreach": true,
}

// powershellAliases are short drive aliases that are not executables.
// Short collisions (gc, gi, ps, sc) are intentionally absent.
var powershellAliases = map[string]bool{
	"clc": true, "cli": true, "clv": true, "cvpa": true, "epcsv": true,
	"gci": true, "gcm": true, "gmo": true, "gwmi": true, "iex": true,
	"ipmo": true, "irm": true, "iwr": true, "nmo": true, "rmo": true,
	"saps": true, "sls": true,
}

// powershellNames select PowerShell. pwd is not a Unix-only tool: cmd.exe
// lacks it, and a following dir must not be sent to bash either.
var powershellNames = map[string]bool{
	"pwd": true,
}

// powershellKeywords are language words, not executables. They are not PATH
// dependencies once the shell is already PowerShell.
var powershellKeywords = map[string]bool{
	"foreach": true, "try": true, "catch": true, "finally": true,
	"while": true, "switch": true, "param": true,
}

// cmdInternals are commands cmd.exe resolves itself. They never appear on PATH.
var cmdInternals = map[string]bool{
	"assoc": true, "break": true, "call": true, "cd": true, "chdir": true,
	"cls": true, "color": true, "copy": true, "date": true, "del": true,
	"dir": true, "echo": true, "endlocal": true, "erase": true, "exit": true,
	"for": true, "ftype": true, "goto": true, "if": true, "md": true,
	"mkdir": true, "mklink": true, "move": true, "path": true, "pause": true,
	"popd": true, "prompt": true, "pushd": true, "rd": true, "rem": true,
	"ren": true, "rename": true, "rmdir": true, "set": true, "setlocal": true,
	"shift": true, "start": true, "time": true, "title": true, "type": true,
	"ver": true, "verify": true, "vol": true,
}

// unixOnlyCommands are tools cmd.exe does not provide. A pipeline that uses
// one of them still has to run under bash; a pipeline of cmd internals does not.
var unixOnlyCommands = map[string]bool{
	"awk": true, "basename": true, "cat": true, "chmod": true, "chown": true,
	"cp": true, "dirname": true, "egrep": true, "fgrep": true, "grep": true,
	"head": true, "ls": true, "mv": true, "rm": true,
	"sed": true, "tail": true, "tee": true, "touch": true, "uname": true,
	"uniq": true, "wc": true, "which": true, "xargs": true,
}
