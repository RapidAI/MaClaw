package main

import (
	"strings"
)

// adaptWindowsUnixInspectCommand rewrites inspect-only Unix inventory
// (ls / file / stat) into PowerShell Get-Item before the host shell runs.
// Mixed build/test/delete commands are left untouched.
func adaptWindowsUnixInspectCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" || !subAgentUnixInspectOnlyCommand(command) {
		return "", false
	}
	var dirs []string
	var paths []string
	listCwd := false
	for _, segment := range shellCommandSegments(command) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "cd", "pushd":
			if p := firstUnixInspectPath(segment[1:]); p != "" {
				dirs = append(dirs, p)
			}
		case "ls", "ls.exe", "dir", "dir.exe":
			if found := unixInspectPaths(segment[1:]); len(found) > 0 {
				paths = append(paths, found...)
			} else {
				listCwd = true
			}
		case "file", "file.exe", "stat", "stat.exe":
			paths = append(paths, unixInspectPaths(segment[1:])...)
		}
	}
	dirs = uniquePreserveSubAgentStrings(dirs)
	paths = uniquePreserveSubAgentStrings(paths)
	if len(paths) == 0 && !listCwd && len(dirs) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, dir := range dirs {
		writeWindowsInspectChangeDirectory(&b, dir)
	}
	if listCwd {
		writeWindowsInspectCwdListing(&b)
	}
	for _, path := range paths {
		writeWindowsInspectPathListing(&b, path)
	}
	adapted := strings.TrimSpace(b.String())
	if adapted == "" || strings.EqualFold(strings.TrimSpace(command), adapted) {
		return "", false
	}
	return adapted, true
}

func writeWindowsInspectChangeDirectory(b *strings.Builder, dir string) {
	lit := powershellSingleQuote(dir)
	// Fail closed: a missing or file cd target must not inspect a sibling
	// left in the previous working directory.
	writeWindowsInspectMissingPathGuard(b, "-LiteralPath ", dir)
	b.WriteString("$dest = Get-Item -LiteralPath ")
	b.WriteString(lit)
	b.WriteByte('\n')
	b.WriteString("if ($null -eq $dest) { Write-Error ('Cannot find path ' + ")
	b.WriteString(lit)
	b.WriteString(" + ' because it does not exist.'); exit 1 }\n")
	b.WriteString("if (-not $dest.PSIsContainer) { Write-Error ('Cannot find path ' + ")
	b.WriteString(lit)
	b.WriteString(" + ' because it is not a directory.'); exit 1 }\n")
	b.WriteString("Set-Location -LiteralPath $dest.FullName\n")
}

func writeWindowsInspectCwdListing(b *strings.Builder) {
	b.WriteString("Get-ChildItem -Force | Select-Object Mode, LastWriteTime, Length, Name | Format-Table -AutoSize\n")
}

func writeWindowsInspectPathListing(b *strings.Builder, path string) {
	writeWindowsInspectMissingPathGuard(b, powershellPathParameter(path), path)
	b.WriteString("Get-Item ")
	b.WriteString(powershellPathParameter(path))
	b.WriteString(powershellSingleQuote(path))
	b.WriteString(" | ForEach-Object {\n")
	b.WriteString("  if ($_.PSIsContainer) {\n")
	b.WriteString("    '{0}  directory' -f $_.FullName\n")
	b.WriteString("    Get-ChildItem -LiteralPath $_.FullName -Force | Select-Object Mode, LastWriteTime, Length, Name | Format-Table -AutoSize\n")
	b.WriteString("  } else {\n")
	b.WriteString("    '{0}  {1} bytes  {2}' -f $_.FullName, $_.Length, $_.LastWriteTime\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
}

func writeWindowsInspectMissingPathGuard(b *strings.Builder, pathParam, path string) {
	lit := powershellSingleQuote(path)
	b.WriteString("if (-not (Test-Path ")
	b.WriteString(pathParam)
	b.WriteString(lit)
	b.WriteString(")) { Write-Error ('Cannot find path ' + ")
	b.WriteString(lit)
	b.WriteString(" + ' because it does not exist.'); exit 1 }\n")
}

func firstUnixInspectPath(args []string) string {
	if paths := unixInspectPaths(args); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func unixInspectPaths(args []string) []string {
	var paths []string
	for _, arg := range args {
		token := strings.TrimSpace(normalizeShellCommandToken(arg))
		if token == "" || token == "2>&1" || token == "--" {
			continue
		}
		if isShellVerificationOutputRedirectionToken(token) {
			continue
		}
		if unixInspectArgIsFlag(token) {
			continue
		}
		paths = append(paths, token)
	}
	return paths
}

// unixInspectArgIsFlag reports ls/stat/dir switches, not inspect targets.
// /usr and /home stay paths; /s and /b stay cmd.exe switches.
func unixInspectArgIsFlag(token string) bool {
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "-") || strings.HasPrefix(token, "%") {
		return true
	}
	if !strings.HasPrefix(token, "/") || strings.HasPrefix(token, "//") {
		return false
	}
	body := token[1:]
	if strings.ContainsAny(body, `/\.`) {
		return false
	}
	if i := strings.IndexByte(body, ':'); i >= 0 {
		body = body[:i]
	}
	if body == "" || len(body) > 2 {
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func powershellPathParameter(path string) string {
	if strings.ContainsAny(path, "*?") {
		return "-Path "
	}
	return "-LiteralPath "
}

func powershellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func uniquePreserveSubAgentStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func adaptWindowsPythonInspectCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" || !subAgentWindowsPythonInspectOnlyCommand(command) {
		return "", false
	}
	var paths []string
	listCwd := false
	// Scan the raw command. shellCommandFields + normalizeShellCommandToken
	// strip trailing ()'" from the -c script, which would hide the listed path.
	found := pythonInspectListPaths(command)
	for _, path := range found {
		if path == "" || path == "." || path == "./" {
			listCwd = true
			continue
		}
		paths = append(paths, path)
	}
	paths = uniquePreserveSubAgentStrings(paths)
	if len(paths) == 0 && !listCwd {
		if !pythonInspectListsCwd(command) {
			return "", false
		}
		listCwd = true
	}
	var b strings.Builder
	if listCwd {
		writeWindowsInspectCwdListing(&b)
	}
	for _, path := range paths {
		writeWindowsInspectPathListing(&b, path)
	}
	adapted := strings.TrimSpace(b.String())
	if adapted == "" {
		return "", false
	}
	return adapted, true
}

func subAgentWindowsPythonInspectOnlyCommand(command string) bool {
	segments := shellCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	sawInspect := false
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		switch cmd {
		case "echo":
			if !subAgentDiagnosticEchoSeparator(segment[1:]) {
				return false
			}
		case "python", "python.exe", "python3", "python3.exe", "py", "py.exe":
			script, ok := pythonInspectDashCScript(segment)
			if !ok || !pythonInspectScriptIsListDirOnly(script) {
				return false
			}
			sawInspect = true
		default:
			return false
		}
	}
	return sawInspect
}

func pythonInspectDashCScript(segment []string) (string, bool) {
	if len(segment) < 3 {
		return "", false
	}
	i := 1
	if commandNameBase(segment[0]) == "py" || commandNameBase(segment[0]) == "py.exe" {
		if i < len(segment) {
			flag := strings.TrimSpace(normalizeShellCommandToken(segment[i]))
			if flag == "-3" || strings.HasPrefix(flag, "-3.") {
				i++
			}
		}
	}
	for i < len(segment) {
		arg := strings.TrimSpace(normalizeShellCommandToken(segment[i]))
		if arg == "-c" {
			if i+1 >= len(segment) {
				return "", false
			}
			return strings.TrimSpace(normalizeShellCommandToken(segment[i+1])), true
		}
		if arg == "-m" {
			return "", false
		}
		if pythonInspectFlagTakesValue(arg) {
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "-") {
			i++
			continue
		}
		return "", false
	}
	return "", false
}

func pythonInspectScriptIsListDirOnly(script string) bool {
	compact := strings.ToLower(script)
	if !strings.Contains(compact, "listdir") && !strings.Contains(compact, "scandir") && !strings.Contains(compact, "iterdir") && !strings.Contains(compact, "getcwd") {
		return false
	}
	for _, banned := range []string{
		"open(", "write(", "writelines", "mkdir", "makedirs", "remove(", "unlink",
		"rmdir", "rename(", "subprocess", "os.system", "popen", "compile(",
		"exec(", "eval(", ".write_text", ".write_bytes", "dump(", "chmod", "symlink",
	} {
		if strings.Contains(compact, banned) {
			return false
		}
	}
	return true
}

func pythonInspectListPaths(script string) []string {
	var paths []string
	// listdir/scandir are case-insensitive. Do not scan generic path( —
	// that matches the suffix of os.path.abspath("...").
	paths = append(paths, extractPythonInspectCallPaths(script, []string{"listdir(", "scandir("}, true)...)
	paths = append(paths, extractPythonInspectCallPaths(script, []string{"Path("}, false)...)
	return uniquePreserveSubAgentStrings(paths)
}

func extractPythonInspectCallPaths(script string, funcs []string, foldCase bool) []string {
	haystack := script
	if foldCase {
		haystack = strings.ToLower(script)
	}
	var paths []string
	for _, fn := range funcs {
		needle := fn
		if foldCase {
			needle = strings.ToLower(fn)
		}
		start := 0
		for {
			rel := strings.Index(haystack[start:], needle)
			if rel < 0 {
				break
			}
			i := start + rel + len(needle)
			rest := strings.TrimSpace(script[i:])
			if strings.HasPrefix(rest, ")") {
				paths = append(paths, ".")
				start = i
				continue
			}
			if len(rest) >= 2 && (rest[0] == 'r' || rest[0] == 'R') && (rest[1] == '\'' || rest[1] == '"') {
				rest = rest[1:]
			}
			if rest == "" {
				break
			}
			q := rest[0]
			if q != '\'' && q != '"' {
				start = i
				continue
			}
			end := strings.IndexByte(rest[1:], q)
			if end < 0 {
				break
			}
			paths = append(paths, unescapePythonInspectPath(rest[1:1+end]))
			start = i + 1
		}
	}
	return paths
}

func unescapePythonInspectPath(path string) string {
	return strings.ReplaceAll(path, `\\`, `\`)
}

func pythonInspectFlagTakesValue(arg string) bool {
	switch arg {
	case "-X", "-W", "-Q", "--check-hash-based-pycs":
		return true
	}
	return false
}

func pythonInspectListsCwd(script string) bool {
	compact := strings.ToLower(strings.ReplaceAll(script, " ", ""))
	markers := []string{"listdir()", "listdir('.')", "listdir(\".\")", "scandir()", "scandir('.')", "scandir(\".\")", "getcwd"}
	for _, token := range markers {
		if strings.Contains(compact, strings.ReplaceAll(token, " ", "")) {
			return true
		}
	}
	return false
}
