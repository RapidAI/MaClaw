package guiapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingParallelTurn is one later implementation task that must not write the
// directory another live task already holds. A git repo gets a private
// worktree and a content merge. Anything else waits, then uses the primary.
type codingParallelTurn struct {
	handler              *IMMessageHandler
	userID               string
	taskPath             string
	primary              string
	execDir              string
	wt                   *codingWorkbenchWorktree
	baseSHA              string
	deferred             bool
	stayPut              bool
	waitPrimary          bool
	discardWorktree      bool
	remaining            int
	merged               bool
	children             int
	cleanupAfterChildren bool
	forgetAfterChildren  bool
	onProgress           func(string)
	runCtx               context.Context
	mu                   sync.Mutex
	childMu              sync.Mutex
}

var codingParallelTurns sync.Map // task path key -> *codingParallelTurn

func codingParallelTurnKey(taskPath string) string {
	return normalizeRecentTaskPathKey(taskPath)
}

func codingParallelTurnFor(taskPath string) *codingParallelTurn {
	value, ok := codingParallelTurns.Load(codingParallelTurnKey(taskPath))
	if !ok {
		return nil
	}
	turn, _ := value.(*codingParallelTurn)
	return turn
}

// codingParallelMappedDir returns the private checkout for a path inside the
// primary tree. A bound turn keeps that checkout after the earlier session
// leaves, until the merge finishes.
func codingParallelMappedDir(taskPath, dir string) (string, bool) {
	turn := codingParallelTurnFor(taskPath)
	if turn == nil {
		return "", false
	}
	original := dir
	dir = normalizeProjectSessionPath(dir)
	if dir == "" {
		return "", false
	}
	if turn.stayPut {
		return original, true
	}
	if turn.wt == nil || turn.wt.Path == "" {
		return "", false
	}
	if pathInsideWorkRoot(turn.wt.Path, dir) {
		return dir, true
	}
	root := turn.wt.GitRoot
	if root == "" {
		root = turn.primary
	}
	rel, inside := workRootRelative(root, dir)
	if !inside {
		return "", false
	}
	if rel == "." {
		// The caller asked for the git root. ProjectPath is only the
		// subdirectory the user opened; returning it would put a root file
		// inside that subdirectory, and the merge would land it there.
		return turn.wt.Path, true
	}
	// workRootRelative folds case for the lookup. The checkout is a real
	// tree, so the mapped path keeps the caller's spelling.
	mappedRel := filepath.FromSlash(rel)
	if preserved, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir)); err == nil && preserved != "." && !strings.HasPrefix(preserved, "..") {
		mappedRel = preserved
	}
	return filepath.Join(turn.wt.Path, mappedRel), true
}

// codingParallelProtectedRoot is the shared tree this turn must not write.
// Inquiry and a turn that stayed on the primary have no private checkout, so
// they have nothing to protect. A volume root is too wide to rewrite.
func codingParallelProtectedRoot(turn *codingParallelTurn) string {
	if turn == nil || turn.stayPut || turn.wt == nil || strings.TrimSpace(turn.wt.Path) == "" {
		return ""
	}
	root := strings.TrimSpace(turn.wt.GitRoot)
	if root == "" {
		root = strings.TrimSpace(turn.primary)
	}
	root = normalizeProjectSessionPath(root)
	if root == "" {
		return ""
	}
	rest := strings.TrimPrefix(root, filepath.VolumeName(root))
	rest = strings.Trim(rest, `/\`)
	if rest == "" {
		return ""
	}
	return root
}

// rewriteCommandOntoParallelCheckout maps absolute names of the shared tree
// inside a model shell command onto the bound checkout. mapPath returns the
// checkout path for one logical path. A name that is already the checkout,
// or outside the tree, stays as written. Separator style of each match is
// kept, so a forward-slash path and a doubled-backslash PowerShell literal
// both survive the rewrite.
func rewriteCommandOntoParallelCheckout(command, root string, mapPath func(string) (string, bool)) string {
	root = strings.TrimSpace(root)
	rootNorm := normalizeCodingRuntimeRef(root)
	if command == "" || rootNorm == "" || mapPath == nil {
		return command
	}
	volume := len(root) >= 2 && root[1] == ':'
	var b strings.Builder
	b.Grow(len(command) + 16)
	inSingle, inDouble := false, false
	for i := 0; i < len(command); {
		ch := command[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			b.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
			i++
			continue
		}
		if canStartAbsPath(command, i, volume) {
			if end, logical, ok := cutFlexiblePath(command, i, root, rootNorm, inSingle || inDouble); ok {
				if mapped, yes := mapPath(logical); yes && strings.TrimSpace(mapped) != "" {
					b.WriteString(restyleMappedPath(command[i:end], mapped))
					i = end
					continue
				}
			}
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}

// commandNamesProtectedRoot reports that command still contains the shared
// tree as a path prefix. Slashes of either direction collapse, so a spelling
// the rewriter missed still fails closed.
func commandNamesProtectedRoot(command, root string) bool {
	rootNorm := collapseSlashRuns(normalizeCodingRuntimeRef(root))
	if strings.TrimSpace(command) == "" || rootNorm == "" {
		return false
	}
	norm := collapseSlashRuns(normalizeCodingRuntimeRef(command))
	from := 0
	for from <= len(norm)-len(rootNorm) {
		at := strings.Index(norm[from:], rootNorm)
		if at < 0 {
			return false
		}
		at += from
		end := at + len(rootNorm)
		beforeOK := at == 0 || !isPathNameByte(norm[at-1])
		afterOK := end >= len(norm) || !isPathNameByte(norm[end])
		if beforeOK && afterOK {
			return true
		}
		from = at + 1
	}
	return false
}

func collapseSlashRuns(s string) string {
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func canStartAbsPath(command string, i int, volume bool) bool {
	if i < 0 || i >= len(command) {
		return false
	}
	if i > 0 && isPathNameChar(command[i-1], false) {
		return false
	}
	if volume {
		return i+1 < len(command) && command[i+1] == ':' && isASCIILetter(command[i])
	}
	return command[i] == '/' || command[i] == '\\'
}

func isASCIILetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// filesystemProviderPrefix is the PowerShell file-provider qualifier in front
// of a drive path. FileSystem::D:\Repo and the module-qualified form both
// address that drive path. A relative tail is not a provider path.
func filesystemProviderPrefix(s string) (int, bool) {
	const qualified = `Microsoft.PowerShell.Core\FileSystem::`
	const short = `FileSystem::`
	n := 0
	switch {
	case len(s) >= len(qualified) && strings.EqualFold(s[:len(qualified)], qualified):
		n = len(qualified)
	case len(s) >= len(short) && strings.EqualFold(s[:len(short)], short):
		n = len(short)
	default:
		return 0, false
	}
	rest := s[n:]
	if len(rest) < 3 || !isASCIILetter(rest[0]) || rest[1] != ':' {
		return 0, false
	}
	if rest[2] != '\\' && rest[2] != '/' {
		return 0, false
	}
	return n, true
}

func matchFlexibleRoot(command string, start int, rootNorm string) int {
	ci := start
	for ri := 0; ri < len(rootNorm); ri++ {
		if ci >= len(command) {
			return -1
		}
		want := rootNorm[ri]
		if want == '/' {
			next, ok := matchSeparatorRun(command, ci)
			if !ok {
				return -1
			}
			ci = next
			continue
		}
		if foldASCIIByte(command[ci]) != want {
			return -1
		}
		ci++
	}
	return ci
}

func matchSeparatorRun(command string, pos int) (int, bool) {
	if pos >= len(command) || (command[pos] != '/' && command[pos] != '\\') {
		return 0, false
	}
	for pos < len(command) && (command[pos] == '/' || command[pos] == '\\') {
		pos++
	}
	return pos, true
}

func foldASCIIByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// cutFlexiblePath reads one absolute path that begins with root. The logical
// path uses root's own spelling plus the suffix spelling from the command.
func cutFlexiblePath(command string, start int, root, rootNorm string, quoted bool) (end int, logical string, ok bool) {
	pos := matchFlexibleRoot(command, start, rootNorm)
	if pos < 0 {
		return 0, "", false
	}
	if pos < len(command) && isPathNameChar(command[pos], quoted) {
		return 0, "", false
	}
	var parts []string
	end = pos
	for end < len(command) {
		sepEnd, sepOK := matchSeparatorRun(command, end)
		if !sepOK {
			break
		}
		segStart := sepEnd
		segEnd := segStart
		for segEnd < len(command) && isPathNameChar(command[segEnd], quoted) {
			segEnd++
		}
		if segEnd == segStart {
			end = sepEnd
			break
		}
		parts = append(parts, command[segStart:segEnd])
		end = segEnd
	}
	logical = root
	if len(parts) > 0 {
		logical = filepath.Join(append([]string{root}, parts...)...)
	}
	return end, logical, true
}

func isPathNameChar(c byte, quoted bool) bool {
	switch c {
	case '/', '\\', '\'', '"', ';', '&', '|', '<', '>', '(', ')', '`', '^', '=', ',', '$', '*', '?', '[', ']', '{', '}', '!', ':':
		return false
	case ' ', '\t', '\r', '\n':
		return quoted
	default:
		return true
	}
}

func isPathNameByte(c byte) bool {
	return isPathNameChar(c, false)
}

func restyleMappedPath(originalSpan, mapped string) string {
	mapped = filepath.Clean(mapped)
	var styled string
	switch {
	case strings.Contains(originalSpan, `\\`):
		styled = strings.ReplaceAll(mapped, string(filepath.Separator), `\\`)
	case strings.Contains(originalSpan, "/") && !strings.Contains(originalSpan, `\`):
		styled = filepath.ToSlash(mapped)
	case filepath.Separator != '\\' && strings.Contains(originalSpan, `\`):
		styled = strings.ReplaceAll(mapped, string(filepath.Separator), `\`)
	default:
		styled = mapped
	}
	// Clean drops a trailing separator. A wildcard after D:\Repo\ must stay
	// D:\checkout\, not D:\checkout glued to *.go.
	switch {
	case strings.HasSuffix(originalSpan, `\\`):
		if !strings.HasSuffix(styled, `\\`) {
			styled += `\\`
		}
	case strings.HasSuffix(originalSpan, `\`):
		if !strings.HasSuffix(styled, `\`) {
			styled += `\`
		}
	case strings.HasSuffix(originalSpan, "/"):
		if !strings.HasSuffix(styled, "/") {
			styled += "/"
		}
	}
	return styled
}

// commandShellDir is the working directory a model command would have as
// PowerShell or cmd walks it. The process starts at base. A drive the
// process has not entered yet is at that drive's root, which is where
// D:repo\file resolves in a new shell whose cwd is on another drive.
// stack is the Push-Location / pushd stack. A child shell does not inherit
// it, and popd restores the pushed directory rather than the process start.
type commandShellDir struct {
	cur   string
	base  string
	per   map[string]string
	stack []shellDirSnap
}

type shellDirSnap struct {
	cur string
	per map[string]string
}

func newCommandShellDir(base string) *commandShellDir {
	s := &commandShellDir{cur: base, base: base, per: map[string]string{}}
	if vol := commandVolumeKey(base); vol != "" {
		s.per[vol] = base
	}
	return s
}

func (s *commandShellDir) snapshot() shellDirSnap {
	per := map[string]string{}
	if s != nil {
		for k, v := range s.per {
			per[k] = v
		}
	}
	cur := ""
	if s != nil {
		cur = s.cur
	}
	return shellDirSnap{cur: cur, per: per}
}

func (s *commandShellDir) restore(snap shellDirSnap) {
	if s == nil {
		return
	}
	s.cur = snap.cur
	s.per = snap.per
	if s.per == nil {
		s.per = map[string]string{}
	}
}

// pushThen remembers the current directory, then moves. Pop-Location
// returns to that directory, including its per-drive current directory.
func (s *commandShellDir) pushThen(path string) {
	if s == nil || strings.TrimSpace(path) == "" {
		return
	}
	s.stack = append(s.stack, s.snapshot())
	s.chdir(path)
}

func (s *commandShellDir) pop() {
	if s == nil || len(s.stack) == 0 {
		return
	}
	snap := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	s.restore(snap)
}

func (s *commandShellDir) chdir(path string) {
	if s == nil || strings.TrimSpace(path) == "" {
		return
	}
	s.cur = path
	if vol := commandVolumeKey(path); vol != "" {
		s.per[vol] = path
	}
}

// applyLocation moves to path. push records the previous directory on the
// default stack, which is what a later Pop-Location restores. A Push-Location
// that names another stack still moves, but a bare Pop-Location does not
// undo it: that pop targets the default stack only.
func (s *commandShellDir) applyLocation(path string, push bool, stack string) {
	if !push || strings.TrimSpace(stack) != "" {
		s.chdir(path)
		return
	}
	s.pushThen(path)
}

func (s *commandShellDir) dirOn(vol string) string {
	vol = strings.ToUpper(vol)
	if s != nil {
		if path, ok := s.per[vol]; ok && path != "" {
			return path
		}
	}
	if vol == "" {
		return ""
	}
	return vol + `\`
}

func commandVolumeKey(path string) string {
	if len(path) >= 2 && path[1] == ':' && isASCIILetter(path[0]) {
		return strings.ToUpper(path[:2])
	}
	return ""
}

// rewriteResolvedCommandPaths maps a shell path that lands in the shared
// tree onto the checkout. String prefix matching misses D:\other\..\Repo,
// a relative climb, a root-relative \Repo, and a drive-relative D:Repo
// from a cwd on another drive. mapPath reports the checkout path.
func rewriteResolvedCommandPaths(command, root, base string, mapPath func(string) (string, bool)) string {
	root = strings.TrimSpace(root)
	if command == "" || root == "" || mapPath == nil {
		return command
	}
	return walkShellCommand(command, newCommandShellDir(base), mapPath, shellParseModeForCommand(command))
}

// shellParseMode is whose quotes the walker honors. The desktop host is
// PowerShell unless the line is one cmd.exe actually runs. cmd does not
// treat ' as a quote, so & inside single quotes still separates commands.
// A nested cmd /c body uses cmd's rules even when the outer host is
// PowerShell. Invoke-Expression stays in the parent process and keeps
// the parent's rules.
type shellParseMode int

const (
	parsePowerShell shellParseMode = iota
	parseCmd
)

func shellParseModeForCommand(command string) shellParseMode {
	// The bool is the host choice. The rewritten text is for execution,
	// and this walk still has to see the model's original spelling.
	if _, ok := windowsCodingCmdLine(command); ok {
		return parseCmd
	}
	return parsePowerShell
}

func quoteIsReal(ch byte, mode shellParseMode) bool {
	return ch == '"' || (ch == '\'' && mode != parseCmd)
}

// walkShellCommand rewrites one shell script against cwd. A quoted
// cmd/powershell/bash payload is a child process: it starts at the parent's
// directory, and its cd does not move the parent. Invoke-Expression runs
// the quoted script in this process, so that cd is the parent's next cwd.
func walkShellCommand(command string, cwd *commandShellDir, mapPath func(string) (string, bool), mode shellParseMode) string {
	if command == "" || cwd == nil || mapPath == nil {
		return command
	}
	var b strings.Builder
	b.Grow(len(command) + 16)
	inSingle, inDouble := false, false
	atCmd, expectDir, expectPush, skipValue, nameNext := true, false, false, false, false
	stackName := ""
	for i := 0; i < len(command); {
		ch := command[i]
		if !inSingle && !inDouble && quoteIsReal(ch, mode) && atCmd {
			// & 'cd' ..\..\.. is a call, not a string. The quoted word is the
			// command, so the next argument still changes this process.
			// & 'call' is the same: call runs that next command here.
			if bodyStart, bodyEnd, close, ok := quotedPayloadBounds(command, i); ok {
				body := command[bodyStart:bodyEnd]
				switch commandLocationKind(body) {
				case "popd":
					cwd.pop()
					b.WriteString(command[i:close])
					i = close
					atCmd = false
					continue
				case "pushd":
					b.WriteString(command[i:close])
					i = close
					expectDir, expectPush = true, true
					stackName = ""
					nameNext = false
					atCmd = false
					continue
				case "chdir":
					b.WriteString(command[i:close])
					i = close
					expectDir, expectPush = true, false
					atCmd = false
					continue
				}
				if isCallToken(body) {
					b.WriteString(command[i:close])
					i = close
					continue
				}
			}
		}
		if !inSingle && !inDouble {
			if kind := quoteScriptKind(command, i, mode); kind != "" {
				bodyStart, bodyEnd, close, ok := quotedPayloadBounds(command, i)
				if ok {
					nested := nestedShellDir(command, i, cwd, mode)
					rewritten := walkShellCommand(command[bodyStart:bodyEnd], nested, mapPath, scriptParseMode(kind, mode))
					b.WriteByte(ch)
					b.WriteString(rewritten)
					if bodyEnd < len(command) {
						b.WriteByte(command[bodyEnd])
					}
					i = close
					atCmd, expectDir, expectPush, skipValue, nameNext = false, false, false, false, false
					stackName = ""
					continue
				}
			}
		}
		if ch == '\'' && !inDouble && mode != parseCmd {
			inSingle = !inSingle
			b.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
			i++
			continue
		}
		quoted := inSingle || inDouble
		if !quoted {
			if n := shellSepLen(command, i); n > 0 {
				b.WriteString(command[i : i+n])
				i += n
				atCmd, expectDir, expectPush, skipValue, nameNext = true, false, false, false, false
				stackName = ""
				continue
			}
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				b.WriteByte(ch)
				i++
				continue
			}
		}
		if !quoted && expectDir {
			end := readShellWord(command, i)
			if end > i {
				word := command[i:end]
				if rewritten, dir, mode := classifyAttachedParam(word, cwd, mapPath); mode != attachedNone {
					b.WriteString(rewritten)
					i = end
					if mode == attachedLocation {
						cwd.applyLocation(dir, expectPush, stackName)
						expectDir, expectPush, skipValue, nameNext = false, false, false, false
						stackName = ""
					}
					continue
				}
				switch {
				case skipValue:
					b.WriteString(word)
					i = end
					if nameNext {
						stackName = word
						nameNext = false
					}
					skipValue = false
					continue
				case isStackNameFlag(word):
					b.WriteString(word)
					i = end
					skipValue = true
					nameNext = true
					continue
				case isLocationValueFlag(word):
					b.WriteString(word)
					i = end
					skipValue = true
					continue
				case isChdirFlag(word):
					b.WriteString(word)
					i = end
					continue
				}
			}
		}
		if pathBoundary(command, i) && resolvedSpanStart(command, i, quoted) {
			end := cutResolvedSpan(command, i, quoted)
			// cd..\..\Repo and Module\Set-Location contain slashes, so they
			// look like paths. At the command position they are commands;
			// folding them into a path would miss the directory change.
			spanIsCommand := false
			// cmd runs cd after if/for and as @cd, so a glued form is still a
			// command when it is not the first word. Eating it as a path
			// would miss the directory change.
			if end > i && !quoted && (atCmd || (mode == parseCmd && !expectDir)) {
				token := command[i:end]
				if mode == parseCmd {
					token = cmdBareCommand(token)
				}
				if _, _, glued := splitGluedLocation(token); glued || commandLocationKind(token) != "" {
					spanIsCommand = true
				}
			}
			if end > i && !spanIsCommand {
				if replaced, dir, ok := mapResolvedSpan(command[i:end], cwd, mapPath); ok {
					b.WriteString(replaced)
					i = end
					atCmd = false
					if expectDir {
						cwd.applyLocation(dir, expectPush, stackName)
						expectDir, expectPush, skipValue, nameNext = false, false, false, false
						stackName = ""
					}
					continue
				}
			}
		}
		if quoted {
			b.WriteByte(ch)
			i++
			continue
		}
		end := readShellWord(command, i)
		if end <= i {
			b.WriteByte(ch)
			i++
			continue
		}
		word := command[i:end]
		// cmd's if/for/else run the later command in this process, and @
		// only turns echo off (@cd, @D:). PowerShell still requires the
		// first word: @cd there is not Set-Location.
		bare := word
		trackCmd := atCmd
		// A word after cd is the directory, even when that directory is
		// named call or D:.
		if mode == parseCmd && !expectDir {
			bare = cmdBareCommand(word)
			trackCmd = true
		}
		if trackCmd {
			if kind, path, glued := splitGluedLocation(bare); glued {
				replaced, dir, resolved := mapResolvedSpan(path, cwd, mapPath)
				if resolved {
					if replaced != path {
						b.WriteString(word[:len(word)-len(path)])
						b.WriteByte(' ')
						b.WriteString(replaced)
					} else {
						b.WriteString(word)
					}
					cwd.applyLocation(dir, kind == "pushd", "")
				} else {
					b.WriteString(word)
				}
				atCmd = false
				i = end
				continue
			}
		}
		switch {
		case trackCmd && isPopdToken(bare):
			cwd.pop()
			atCmd = false
		case trackCmd && isPushdToken(bare):
			expectDir, expectPush = true, true
			stackName = ""
			nameNext = false
			atCmd = false
		case trackCmd && isChdirToken(bare):
			expectDir, expectPush = true, false
			atCmd = false
		case trackCmd && isCallToken(bare):
			// cmd's call runs the next command in this process. Leaving
			// atCmd set is what lets call cd climb before the next path.
			atCmd = true
		case trackCmd && isDriveSwitch(bare):
			// Bare D: selects that drive's remembered directory. Later
			// names on this line resolve there; the token itself stays D:.
			// if exist x D: and @D: are the same switch.
			cwd.chdir(cwd.dirOn(shellCommandToken(bare)))
			atCmd = false
		case expectDir:
			if replaced, dir, ok := mapResolvedSpan(word, cwd, mapPath); ok {
				b.WriteString(replaced)
				cwd.applyLocation(dir, expectPush, stackName)
			} else {
				b.WriteString(word)
			}
			expectDir, expectPush, skipValue, nameNext = false, false, false, false
			stackName = ""
			atCmd = false
			i = end
			continue
		default:
			atCmd = false
			if replaced, ok := rewriteLooseShellWord(word, cwd, mapPath); ok {
				b.WriteString(replaced)
				i = end
				continue
			}
		}
		b.WriteString(word)
		i = end
	}
	return b.String()
}

// commandReachesProtectedRoot reports that some path in command still
// resolves into the shared tree. Rejection uses it after rewriting.
func commandReachesProtectedRoot(command, root, base string) bool {
	root = strings.TrimSpace(root)
	if strings.TrimSpace(command) == "" || root == "" {
		return false
	}
	hit := false
	rewriteResolvedCommandPaths(command, root, base, func(logical string) (string, bool) {
		if pathInsideWorkRoot(root, logical) {
			hit = true
		}
		return "", false
	})
	return hit
}

func mapResolvedSpan(span string, cwd *commandShellDir, mapPath func(string) (string, bool)) (replaced, dir string, ok bool) {
	if mapPath == nil {
		return "", "", false
	}
	// ^\ and `\ are one path to cmd and PowerShell. The raw span keeps the
	// escape, so Clean never sees the separator it hides. Prefer that reading
	// when it lands in the shared tree; a caret PowerShell would keep is
	// still rewritten, and the checkout is not the shared tree.
	if stripped, yes := unescapeShellSpan(span); yes {
		if replaced, dir, ok := mapShellReading(stripped, cwd, mapPath); ok {
			return replaced, dir, true
		}
	}
	if replaced, dir, ok := mapShellReading(span, cwd, mapPath); ok {
		return replaced, dir, true
	}
	logical, resolved := resolveShellSpan(span, cwd)
	if !resolved {
		return "", "", false
	}
	return span, logical, true
}

// mapShellReading maps one already-unescaped path. The PowerShell reading
// wins. /d/Repo is D:\d\Repo there and D:\Repo in nested bash, so the bash
// reading is used only when it is the protected tree.
func mapShellReading(span string, cwd *commandShellDir, mapPath func(string) (string, bool)) (replaced, dir string, ok bool) {
	logical, resolved := resolveShellSpan(span, cwd)
	if resolved {
		if mapped, yes := mappedCheckout(logical, mapPath); yes {
			return restyleMappedPath(span, mapped), mapped, true
		}
	}
	if alt, altOK := msysDrivePath(span); altOK && alt != logical {
		if mapped, yes := mappedCheckout(alt, mapPath); yes {
			return restyleMappedPath(span, mapped), mapped, true
		}
	}
	return "", "", false
}

// unescapeShellSpan removes unquoted cmd carets and PowerShell backticks.
// A line continuation drops the break. Any other escaped byte stays, so
// ^^ is one caret and ^\ is a separator. Quoted text never reaches this:
// the span cutter stops at the escape instead of taking it.
func unescapeShellSpan(span string) (string, bool) {
	if !strings.ContainsAny(span, "^`") {
		return span, false
	}
	var b strings.Builder
	b.Grow(len(span))
	changed := false
	for i := 0; i < len(span); {
		if next, keep, ok := shellEscapePair(span, i); ok {
			b.WriteString(keep)
			i = next
			changed = true
			continue
		}
		b.WriteByte(span[i])
		i++
	}
	if !changed {
		return span, false
	}
	return b.String(), true
}

// rewriteLooseShellWord rewrites one argument that is not a cd target.
// Copy-Item Repo and git -C Repo are paths once the process has climbed
// to the drive root. -LiteralPath:Repo is the same path glued to the
// parameter name. A trailing glob stays on the rewritten directory.
func rewriteLooseShellWord(word string, cwd *commandShellDir, mapPath func(string) (string, bool)) (string, bool) {
	if _, value, ok := splitAttachedParam(word); ok {
		replaced, _, resolved := mapResolvedSpan(value, cwd, mapPath)
		if resolved && replaced != value {
			return word[:len(word)-len(value)] + replaced, true
		}
		return "", false
	}
	pathPart, suffix := splitGlobSuffix(word)
	if pathPart == "" {
		return "", false
	}
	replaced, _, resolved := mapResolvedSpan(pathPart, cwd, mapPath)
	if resolved && replaced != pathPart {
		return replaced + suffix, true
	}
	return "", false
}

const (
	attachedNone = iota
	attachedLocation
	attachedOther
)

// classifyAttachedParam reads PowerShell's -Name:value form. -Path:Repo is
// one token, so a flag scan never sees Repo. A location parameter moves the
// process. Any other parameter keeps the search going so a later positional
// path is still the directory.
func classifyAttachedParam(word string, cwd *commandShellDir, mapPath func(string) (string, bool)) (rewritten, dir string, mode int) {
	name, value, ok := splitAttachedParam(word)
	if !ok {
		return "", "", attachedNone
	}
	replaced, landed, resolved := mapResolvedSpan(value, cwd, mapPath)
	if resolved && (replaced != value || isLocationPathParam(name)) {
		return word[:len(word)-len(value)] + replaced, landed, attachedLocation
	}
	return word, "", attachedOther
}

func splitAttachedParam(word string) (name, value string, ok bool) {
	if len(word) < 4 || (word[0] != '-' && word[0] != '/') || word[1] == '-' || word[1] == '/' {
		return "", "", false
	}
	colon := strings.IndexByte(word, ':')
	if colon <= 1 || colon >= len(word)-1 {
		return "", "", false
	}
	name = word[1:colon]
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isASCIILetter(c) && (c < '0' || c > '9') {
			return "", "", false
		}
	}
	return name, word[colon+1:], true
}

func isLocationPathParam(name string) bool {
	switch strings.ToLower(name) {
	case "path", "literalpath", "pspath", "lp":
		return true
	default:
		return false
	}
}

func splitGlobSuffix(word string) (pathPart, suffix string) {
	cut := strings.IndexAny(word, "*?[")
	if cut < 0 {
		return word, ""
	}
	if cut == 0 {
		return "", word
	}
	return word[:cut], word[cut:]
}

func mappedCheckout(logical string, mapPath func(string) (string, bool)) (string, bool) {
	mapped, yes := mapPath(logical)
	if !yes || strings.TrimSpace(mapped) == "" {
		return "", false
	}
	return mapped, true
}

// msysDrivePath reads /d/Repo or /d\Repo as D:\Repo. PowerShell itself
// treats that text as a root-relative name on the current drive.
func msysDrivePath(span string) (string, bool) {
	span = strings.TrimSpace(span)
	if len(span) < 4 || !isASCIILetter(span[1]) {
		return "", false
	}
	if (span[0] != '/' && span[0] != '\\') || (span[2] != '/' && span[2] != '\\') {
		return "", false
	}
	rest := strings.ReplaceAll(span[3:], "/", `\`)
	if strings.Trim(rest, `\`) == "" {
		return "", false
	}
	return filepath.Clean(strings.ToUpper(span[1:2]) + `:\` + rest), true
}

func resolveShellSpan(span string, cwd *commandShellDir) (string, bool) {
	span = strings.TrimSpace(span)
	if span == "" || cwd == nil {
		return "", false
	}
	// FileSystem::D:\Repo is a normal drive path. The qualifier is not a directory.
	if n, ok := filesystemProviderPrefix(span); ok {
		return resolveShellSpan(span[n:], cwd)
	}
	if len(span) >= 2 && span[1] == ':' && isASCIILetter(span[0]) {
		rest := span[2:]
		if rest == "" {
			// cd D: switches to that drive's remembered directory.
			return cwd.dirOn(span[:2]), true
		}
		if rest[0] == '/' || rest[0] == '\\' {
			return filepath.Clean(span), true
		}
		return filepath.Clean(filepath.Join(cwd.dirOn(span[:2]), rest)), true
	}
	if span[0] == '/' || span[0] == '\\' {
		if len(span) > 1 && (span[1] == '/' || span[1] == '\\') {
			return "", false
		}
		vol := commandVolumeKey(cwd.cur)
		if vol == "" {
			return "", false
		}
		// \Repo is absolute, so Join(vol, span) would drop the drive.
		rest := strings.TrimLeft(span, `/\`)
		return filepath.Clean(vol + `\` + rest), true
	}
	if cwd.cur == "" {
		return "", false
	}
	return filepath.Clean(filepath.Join(cwd.cur, span)), true
}

func pathBoundary(command string, i int) bool {
	if i <= 0 {
		return true
	}
	return !isPathNameChar(command[i-1], false)
}

func resolvedSpanStart(command string, i int, quoted bool) bool {
	end := cutResolvedSpan(command, i, quoted)
	if end <= i {
		return false
	}
	return relativeLikeStart(command, i) || driveLikeStart(command, i) || rootRelativeStart(command, i) || strings.ContainsAny(command[i:end], `/\`)
}

func relativeLikeStart(command string, i int) bool {
	if i >= len(command) || command[i] != '.' || i+1 >= len(command) {
		return false
	}
	next := command[i+1]
	if next == '/' || next == '\\' {
		return true
	}
	if next != '.' {
		return false
	}
	if i+2 >= len(command) {
		return true
	}
	third := command[i+2]
	return third == '/' || third == '\\'
}

func driveLikeStart(command string, i int) bool {
	if i+2 >= len(command) || !isASCIILetter(command[i]) || command[i+1] != ':' {
		return false
	}
	next := command[i+2]
	return next == '/' || next == '\\' || next == '.' || isPathNameChar(next, false)
}

func rootRelativeStart(command string, i int) bool {
	if i >= len(command) {
		return false
	}
	if command[i] != '/' && command[i] != '\\' {
		return false
	}
	if i+1 < len(command) && (command[i+1] == '/' || command[i+1] == '\\') {
		return false
	}
	if i+1 >= len(command) {
		return false
	}
	next := command[i+1]
	return next == '.' || isPathNameChar(next, false)
}

func cutResolvedSpan(command string, start int, quoted bool) int {
	// Microsoft.PowerShell.Core\FileSystem::D:\Repo is one path. Cutting at
	// the qualifier's colon would leave D:\Repo as a second word and miss it.
	if n, ok := filesystemProviderPrefix(command[start:]); ok {
		if end := cutResolvedSpan(command, start+n, quoted); end > start+n {
			return end
		}
	}
	end := start
	// ':' ends a name, except the colon in D:.
	if driveLikeStart(command, start) {
		end = start + 2
	}
	for end < len(command) {
		// An unquoted escape is part of this path. Quoted text keeps the
		// old split: a single-quoted caret is literal, and the following
		// separator is still its own span.
		if !quoted {
			if next, _, ok := shellEscapePair(command, end); ok {
				end = next
				continue
			}
		}
		c := command[end]
		if c == '/' || c == '\\' || isPathNameChar(c, quoted) {
			end++
			continue
		}
		break
	}
	return end
}

// shellEscapePair reads one unquoted ^ or ` escape. cmd and PowerShell both
// delete an escaped line break. A backtick keeps any other next byte.
// A caret keeps a path byte only: ^\ hides a separator, while ^& and ^"
// stay outside the span so the walker still sees the separator or quote.
func shellEscapePair(s string, i int) (next int, keep string, ok bool) {
	if i >= len(s) || i+1 >= len(s) {
		return i, "", false
	}
	esc := s[i]
	if esc != '^' && esc != '`' {
		return i, "", false
	}
	n := s[i+1]
	if n == '\n' {
		return i + 2, "", true
	}
	if n == '\r' {
		end := i + 2
		if end < len(s) && s[end] == '\n' {
			end++
		}
		return end, "", true
	}
	if esc == '^' && n != '/' && n != '\\' && !isPathNameChar(n, false) {
		return i, "", false
	}
	return i + 2, s[i+1 : i+2], true
}

func shellSepLen(command string, i int) int {
	switch command[i] {
	// '(' ')' group a command, so `(cd Repo)` still changes directory.
	// `cd (Repo)` does not: both PowerShell and cmd treat the parentheses
	// as a subexpression and leave the location where it was.
	// '{' '}' is a PowerShell block. if / foreach / ForEach-Object run it
	// in this process, and Set-Location sticks. A block that is only a
	// value is tracked the same way: a later relative name stays off the
	// shared tree.
	case ';', '(', ')', '{', '}':
		return 1
	case '\n':
		// `^` or a backtick at the end of the line continues it. The next
		// statement is still this one, which is how iex's script stays
		// attached across the break.
		if escapedLineBreak(command, i) {
			return 0
		}
		return 1
	case '&':
		if i > 0 && command[i-1] == '>' {
			return 0
		}
		if i+1 < len(command) && command[i+1] == '&' {
			return 2
		}
		return 1
	case '|':
		if i+1 < len(command) && command[i+1] == '|' {
			return 2
		}
		return 1
	default:
		return 0
	}
}

func readShellWord(command string, i int) int {
	end := i
	for end < len(command) && isShellWordChar(command[end]) {
		end++
	}
	return end
}

func isShellWordChar(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', ';', '&', '|', '<', '>', '(', ')', '\'', '"', '`':
		return false
	default:
		return true
	}
}

func shellCommandToken(token string) string {
	return strings.ToLower(strings.TrimSuffix(strings.Trim(token, `"'`), ".exe"))
}

func isChdirToken(token string) bool {
	return commandLocationKind(token) == "chdir"
}

func isPushdToken(token string) bool {
	return commandLocationKind(token) == "pushd"
}

func isPopdToken(token string) bool {
	return commandLocationKind(token) == "popd"
}

// isCallToken reports cmd's call builtin. call script.bat and call cd both
// stay in this process. A path such as .\call.exe is a program: only the
// bare name, after the .exe strip, counts.
func isCallToken(token string) bool {
	return shellCommandToken(token) == "call"
}

// cmdBareCommand strips one leading @. cmd treats that as "do not echo
// this command", so @cd and @D: are cd and D:. A second @ is part of the name.
func cmdBareCommand(word string) string {
	if len(word) > 1 && word[0] == '@' {
		return word[1:]
	}
	return word
}

// commandLocationKind reports a command that changes this process's
// directory. A module qualifier counts (Microsoft.PowerShell.Management\cd).
// A filesystem path does not: .\cd.exe is a program, not Set-Location.
// cd/d is cmd's switch glued to the name; the next word is the directory.
func commandLocationKind(token string) string {
	switch strings.ToLower(shellCommandToken(token)) {
	case "cd/d", "chdir/d":
		return "chdir"
	}
	switch moduleCommandLeaf(token) {
	case "cd", "chdir", "sl", "set-location":
		return "chdir"
	case "pushd", "push-location":
		return "pushd"
	case "popd", "pop-location":
		return "popd"
	default:
		return ""
	}
}

// moduleCommandLeaf is the command after a PowerShell module qualifier.
// Tokens with a drive, or a relative filesystem prefix, stay whole.
func moduleCommandLeaf(token string) string {
	token = shellCommandToken(token)
	if token == "" || strings.Contains(token, ":") || strings.HasPrefix(token, ".") || strings.HasPrefix(token, `\`) || strings.HasPrefix(token, "/") {
		return token
	}
	if i := strings.LastIndexAny(token, `\/`); i >= 0 {
		if leaf := token[i+1:]; leaf != "" {
			return leaf
		}
	}
	return token
}

// executableLeaf is the program name of a command or a path to that program.
// C:\Windows\System32\cmd.exe /c is still cmd.
func executableLeaf(token string) string {
	token = shellCommandToken(token)
	token = strings.Trim(token, `\/`)
	if i := strings.LastIndexAny(token, `\/`); i >= 0 {
		if leaf := token[i+1:]; leaf != "" {
			return leaf
		}
	}
	return token
}

// splitGluedLocation splits cmd's optional space: cd.., cd\, cd..\..\Repo,
// chdir.., pushd.., and cd/d..\. PowerShell itself only runs the exact
// forms cd.. and cd\. A nested cmd /c still runs the wider forms, so the
// walker tracks all of them. An absolute rewrite gains a space, because
// cdD:\checkout is not a command.
func splitGluedLocation(word string) (kind, path string, ok bool) {
	lower := strings.ToLower(word)
	prefix := ""
	switch {
	case strings.HasPrefix(lower, "chdir"):
		prefix = "chdir"
	case strings.HasPrefix(lower, "pushd"):
		prefix = "pushd"
	case strings.HasPrefix(lower, "cd"):
		prefix = "cd"
	default:
		return "", "", false
	}
	if len(word) <= len(prefix) {
		return "", "", false
	}
	rest := word[len(prefix):]
	if prefix != "pushd" && len(rest) >= 2 && rest[0] == '/' && (rest[1] == 'd' || rest[1] == 'D') {
		rest = rest[2:]
	}
	if rest == "" || (rest[0] != '\\' && rest[0] != '/' && rest[0] != '.') {
		return "", "", false
	}
	if prefix == "pushd" {
		return "pushd", rest, true
	}
	return "chdir", rest, true
}

func isDriveSwitch(token string) bool {
	token = shellCommandToken(token)
	return len(token) == 2 && isASCIILetter(token[0]) && token[1] == ':'
}

func isStackNameFlag(token string) bool {
	switch strings.ToLower(token) {
	case "-stackname", "-stack":
		return true
	default:
		return false
	}
}

// isLocationValueFlag is a Set-Location / Push-Location parameter that
// takes the next word as its value, not as the directory.
func isLocationValueFlag(token string) bool {
	switch strings.ToLower(token) {
	case "-erroraction", "-ea",
		"-warningaction", "-wa",
		"-informationaction", "-infa",
		"-errorvariable", "-ev",
		"-warningvariable", "-wv",
		"-informationvariable", "-iv",
		"-outvariable", "-ov",
		"-outbuffer", "-ob",
		"-pipelinevariable", "-pv":
		return true
	default:
		return false
	}
}

// quoteScriptKind is the shell that will run this quoted text. The executable
// does not have to be the first word: start, call, and Start-Process cmd.exe
// still run the quoted command. A quoted string that itself begins with cmd
// is the script forfiles /c passes through.
func quoteScriptKind(command string, quoteAt int, mode shellParseMode) string {
	if quoteAt <= 0 || quoteAt >= len(command) || !quoteIsReal(command[quoteAt], mode) {
		return ""
	}
	head := strings.TrimSpace(command[statementStartBefore(command, quoteAt, mode):quoteAt])
	if kind := processShellKind(head); kind != "" {
		return kind
	}
	bodyStart, bodyEnd, _, ok := quotedPayloadBounds(command, quoteAt)
	if !ok {
		return ""
	}
	kind := leadingProcessShell(command[bodyStart:bodyEnd])
	if kind == "" {
		return ""
	}
	// Inside cmd, the quoted text is a command line even when it starts
	// with powershell. cd later in that line still changes the process.
	if mode == parseCmd {
		return "cmd"
	}
	return kind
}

func scriptParseMode(kind string, parent shellParseMode) shellParseMode {
	switch kind {
	case "cmd":
		return parseCmd
	case "powershell", "bash":
		if parent == parseCmd {
			return parseCmd
		}
		return parsePowerShell
	default:
		return parent
	}
}

// processShellKind finds the shell whose quoted text follows head.
// A script switch (-Command, /c) wins. Otherwise any cmd/powershell/bash
// in the head still owns the quote, which is how Start-Process -ArgumentList
// passes a command line through.
func processShellKind(head string) string {
	words := shellHeadWords(head)
	script := ""
	seen := ""
	for start := 0; start < len(words); start++ {
		kind := shellWrapperKind(words[start])
		if kind == "" {
			continue
		}
		if script == "" && wrapperTailIsScript(kind, words[start+1:]) {
			script = kind
		}
		if seen == "" && kind != "inline" {
			seen = kind
		}
	}
	if script != "" {
		return script
	}
	return seen
}

func leadingProcessShell(command string) string {
	words := shellHeadWords(strings.TrimSpace(command))
	if len(words) == 0 {
		return ""
	}
	switch shellWrapperKind(words[0]) {
	case "cmd", "powershell", "bash":
		return shellWrapperKind(words[0])
	default:
		return ""
	}
}

func wrapperTailIsScript(kind string, tail []string) bool {
	for i := 0; i < len(tail); {
		w := tail[i]
		if isWrapperCommandFlag(kind, w) {
			return i == len(tail)-1
		}
		if takesWrapperOptionValue(kind, w) {
			// A following flag is another option, not this option's value.
			// iex -Verbose -ErrorAction Stop "..." must still see the quote.
			if i+1 < len(tail) && !wrapperFlagToken(tail[i+1]) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if w != "" && (w[0] == '-' || w[0] == '/') {
			i++
			continue
		}
		return false
	}
	// iex "script" has no -Command. powershell "script" is not a script.
	return kind == "inline"
}

func statementStartBefore(command string, at int, mode shellParseMode) int {
	start := 0
	inSingle, inDouble := false, false
	for i := 0; i < at && i < len(command); {
		ch := command[i]
		if ch == '\'' && !inDouble && mode != parseCmd {
			inSingle = !inSingle
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			i++
			continue
		}
		if !inSingle && !inDouble {
			if n := shellSepLen(command, i); n > 0 {
				start = i + n
				i += n
				continue
			}
		}
		i++
	}
	return start
}

func shellWrapperKind(token string) string {
	switch executableLeaf(token) {
	case "cmd":
		return "cmd"
	case "powershell", "pwsh":
		return "powershell"
	case "bash", "sh":
		return "bash"
	case "invoke-expression", "iex":
		// Same process. A child-shell copy would leave the next parent
		// command on the checkout after the script had climbed out of it.
		return "inline"
	default:
		return ""
	}
}

// nestedShellDir is the cwd the quoted payload runs in. A child starts at
// the parent's directory and is discarded. Invoke-Expression is the parent.
func nestedShellDir(command string, quoteAt int, parent *commandShellDir, mode shellParseMode) *commandShellDir {
	head := command[statementStartBefore(command, quoteAt, mode):quoteAt]
	words := shellHeadWords(head)
	if len(words) > 0 && shellWrapperKind(words[0]) == "inline" {
		return parent
	}
	if parent == nil {
		return newCommandShellDir("")
	}
	return newCommandShellDir(parent.cur)
}

func escapedLineBreak(command string, i int) bool {
	if i >= len(command) || command[i] != '\n' {
		return false
	}
	prev := i - 1
	if prev >= 0 && command[prev] == '\r' {
		prev--
	}
	return prev >= 0 && (command[prev] == '^' || command[prev] == '`')
}

func wrapperFlagToken(token string) bool {
	return token != "" && (token[0] == '-' || token[0] == '/')
}

// shellHeadWords splits a statement the way the walker reads words.
// strings.Fields keeps a trailing backtick on iex, so a continued
// iex<newline>"script" never matches Invoke-Expression.
func shellHeadWords(head string) []string {
	var words []string
	for i := 0; i < len(head); {
		c := head[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '`' || c == '^' {
			i++
			continue
		}
		start := i
		for i < len(head) && isShellWordChar(head[i]) {
			i++
		}
		if i == start {
			i++
			continue
		}
		words = append(words, head[start:i])
	}
	return words
}

func isWrapperCommandFlag(kind, token string) bool {
	w := strings.ToLower(strings.Trim(token, `"'`))
	switch kind {
	case "cmd":
		return w == "/c" || w == "/k"
	case "powershell":
		switch w {
		case "-command", "/command", "-c", "/c":
			return true
		}
	case "bash":
		return w == "-c" || w == "-lc" || w == "-cl"
	case "inline":
		return w == "-command" || w == "/command"
	}
	return false
}

func takesWrapperOptionValue(kind, token string) bool {
	w := strings.ToLower(strings.Trim(token, `"'`))
	switch kind {
	case "powershell":
		switch w {
		case "-executionpolicy", "-inputformat", "-outputformat", "-windowstyle",
			"-configurationname", "-workingdirectory", "-file", "-version":
			return true
		}
	case "bash":
		switch w {
		case "--rcfile", "--init-file":
			return true
		}
	case "inline":
		// -ErrorAction Stop still leaves the following quote as the script.
		// -Command itself is the script, so it is not a skipped value.
		if w == "-command" || w == "/command" || len(w) < 2 {
			return false
		}
		return w[0] == '-' || w[0] == '/'
	}
	return false
}

func quotedPayloadBounds(command string, open int) (bodyStart, bodyEnd, close int, ok bool) {
	if open < 0 || open >= len(command) {
		return 0, 0, 0, false
	}
	q := command[open]
	if q != '\'' && q != '"' {
		return 0, 0, 0, false
	}
	i := open + 1
	for i < len(command) {
		if command[i] != q {
			i++
			continue
		}
		if i+1 < len(command) && command[i+1] == q {
			i += 2
			continue
		}
		if q == '"' && i > open+1 && command[i-1] == '`' {
			i++
			continue
		}
		return open + 1, i, i + 1, true
	}
	return 0, 0, 0, false
}

func isChdirFlag(token string) bool {
	token = strings.ToLower(token)
	if token == "/d" || token == "--" {
		return true
	}
	return strings.HasPrefix(token, "-") && !strings.ContainsAny(token, `/\`)
}

// codingTurnPrimaryDir is the directory the task record says it edits.
// A detached checkout is an execution copy, not that directory.
func (a *App) codingTurnPrimaryDir(userID, presented string) string {
	presented = normalizeProjectSessionPath(presented)
	taskPath := projectPathFromSessionOwnerID(userID)
	if a == nil {
		return presented
	}
	if tagged := normalizeProjectSessionPath(a.codingTaskTaggedWorkRoot(taskPath)); tagged != "" && !isLocalCodingIdentityOrSandbox(taskPath, tagged) {
		return tagged
	}
	if claimed, ok := a.claimedWorkRoot(taskPath); ok {
		claimed = normalizeProjectSessionPath(claimed)
		if claimed != "" && !isLocalCodingIdentityOrSandbox(taskPath, claimed) {
			checkout := normalizeProjectSessionPath(a.exclusiveCheckoutDir(taskPath))
			if checkout == "" || !pathInsideWorkRoot(checkout, claimed) {
				return claimed
			}
		}
	}
	return presented
}

// codingParallelWorktreeCreate is the worktree allocator for a later
// implementation turn. Tests replace it to simulate a git lock.
var codingParallelWorktreeCreate = createCodingWorkbenchWorktree

// beginCodingParallelTurn decides where this local coding turn executes.
// Inquiry reads the primary tree. An operational turn waits when another
// session already holds it. An implementation turn takes a worktree when it
// can, and otherwise waits on the primary instead of writing beside the holder.
func (h *IMMessageHandler) beginCodingParallelTurn(userID, presented string, kind codingRequestKind, onProgress func(string)) *codingParallelTurn {
	if h == nil || h.app == nil {
		return nil
	}
	taskPath := normalizeProjectSessionPath(projectPathFromSessionOwnerID(userID))
	primary := h.app.codingTurnPrimaryDir(userID, presented)
	if taskPath == "" || primary == "" {
		return nil
	}
	holder, _ := h.app.earlierLiveWorkRootHolder(taskPath, primary)
	if holder == "" {
		return nil
	}
	turn := &codingParallelTurn{
		handler:    h,
		userID:     userID,
		taskPath:   taskPath,
		primary:    primary,
		execDir:    primary,
		onProgress: onProgress,
	}
	if kind == codingRequestInquiry {
		turn.stayPut = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		return turn
	}
	if kind != codingRequestImplementation {
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		emitCodingParallelWriterWait(onProgress, "同一目录上另一个任务还在使用，本任务等它结束后再开始")
		return turn
	}
	// Surface the possible wait before attempting an isolated checkout. The
	// attempt may have to wait on the holder's git metadata lock; without an
	// event the header keeps the previous "contacting the model" state while
	// the turn is actually queued behind another writer.
	emitCodingParallelWriterWait(onProgress, "同一目录上另一个任务还在写入，本任务正在尝试独立检出")
	wt, err := codingParallelWorktreeCreate(primary, 0, "parallel-task")
	if err != nil || wt == nil {
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		detail := "这个目录不是 git 仓库，本任务等另一个任务写完后再开始，避免互相覆盖"
		if err != nil {
			detail = "独立检出创建失败，本任务等另一个任务写完后再在主目录开始"
		}
		emitCodingParallelWriterWait(onProgress, detail)
		return turn
	}
	base, baseErr := codingParallelGitText(wt.Path, "rev-parse", "HEAD")
	if baseErr != nil || strings.TrimSpace(base) == "" {
		wt.cleanup(false)
		turn.stayPut = true
		turn.waitPrimary = true
		codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
		emitCodingParallelWriterWait(onProgress, "独立检出创建失败，本任务等另一个任务写完后再在主目录开始")
		return turn
	}
	turn.wt = wt
	turn.baseSHA = strings.TrimSpace(base)
	turn.execDir = wt.ProjectPath
	turn.deferred = true
	codingParallelTurns.Store(codingParallelTurnKey(taskPath), turn)
	codingParallelRememberExec(turn)
	// The isolated checkout is ready, so the turn is no longer waiting on the
	// holder. Move the header off waiting_writer before the runner emits its
	// own starting/tool events; otherwise the deferred path looks like it is
	// still queued behind the other task.
	emitCodingParallelWriterReady(onProgress)
	if onProgress != nil {
		onProgress("本任务在独立检出中执行，结束后合并回主目录")
	}
	return turn
}

func (t *codingParallelTurn) bindRunCtx(ctx context.Context) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.runCtx = ctx
	t.mu.Unlock()
}

func (t *codingParallelTurn) waitReady(ctx context.Context) error {
	if t == nil || !t.waitPrimary {
		return nil
	}
	return t.waitHolder(ctx)
}

func (t *codingParallelTurn) hasMerged() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.merged
}

// shellDir is where a hook or verify command of this turn runs. Those
// commands can write files. Before the merge they use the private checkout,
// so they do not write the directory the other task still holds. After the
// merge the combined bytes are on the primary and the checkout is about to
// be removed, so the command runs on the primary.
func (t *codingParallelTurn) shellDir(fallback string) string {
	if t == nil || !t.deferred {
		return fallback
	}
	if t.hasMerged() && strings.TrimSpace(t.primary) != "" {
		return t.primary
	}
	if strings.TrimSpace(t.execDir) != "" {
		return t.execDir
	}
	return fallback
}

// previewPaths points a finished merge at the primary tree. The worktree is
// where the turn wrote; the bytes a person opens are the ones that landed.
func (t *codingParallelTurn) previewPaths(execDir string, modified, created []string) (string, []string, []string) {
	if t == nil || !t.deferred || t.primary == "" || !t.hasMerged() {
		return execDir, modified, created
	}
	modified = t.remapLandedPaths(execDir, modified)
	created = t.remapLandedPaths(execDir, created)
	root := t.primary
	// A file beside the opened subdirectory is inside the git root. The
	// preview reads relative names from its root, so those names are made
	// absolute before the root widens. A path outside the git root does not
	// widen it: the subdirectory stays the root and its relative names stay
	// relative. A turn that only touched the subdirectory keeps that root.
	if t.wt != nil && previewPathsLeaveRoot(root, t.wt.GitRoot, modified, created) {
		modified = absolutizeUnder(t.primary, modified)
		created = absolutizeUnder(t.primary, created)
		root = t.wt.GitRoot
	}
	return root, modified, created
}

// previewPathsLeaveRoot reports a landed path that sits in the git repo and
// outside the opened subdirectory. A path outside the repo does not count.
func previewPathsLeaveRoot(opened, gitRoot string, groups ...[]string) bool {
	opened = filepath.Clean(strings.TrimSpace(opened))
	gitRoot = filepath.Clean(strings.TrimSpace(gitRoot))
	if opened == "" || opened == "." || gitRoot == "" || gitRoot == "." {
		return false
	}
	for _, group := range groups {
		for _, p := range group {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			full := p
			if !filepath.IsAbs(full) {
				full = filepath.Join(opened, filepath.FromSlash(full))
			}
			full = filepath.Clean(full)
			if !previewPathEscapes(opened, full) || previewPathEscapes(gitRoot, full) {
				continue
			}
			return true
		}
	}
	return false
}

func previewPathEscapes(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func absolutizeUnder(root string, paths []string) []string {
	if strings.TrimSpace(root) == "" || len(paths) == 0 {
		return paths
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || filepath.IsAbs(p) {
			out[i] = p
			continue
		}
		out[i] = filepath.Join(root, filepath.FromSlash(p))
	}
	return out
}

// remapLandedPaths rewrites checkout paths onto the tree the merge wrote.
// A file beside the opened subdirectory is still inside the repo, so the
// pair is the worktree root and the git root. The checkout-to-primary pair
// is only the fallback when those roots were not recorded.
func (t *codingParallelTurn) remapLandedPaths(execDir string, paths []string) []string {
	if t == nil || len(paths) == 0 {
		return paths
	}
	from, to := execDir, t.primary
	if from == "" {
		from = t.execDir
	}
	if t.wt != nil && strings.TrimSpace(t.wt.Path) != "" && strings.TrimSpace(t.wt.GitRoot) != "" {
		from = t.wt.Path
		to = t.wt.GitRoot
	}
	if from == "" || to == "" {
		return paths
	}
	return remapWorktreePaths(paths, from, to)
}

func (t *codingParallelTurn) runtimeOptions(task *TaskItem) *guiCodingRuntimeOptions {
	options := defaultGUICodingRuntimeOptions(task, t.primary)
	if !t.deferred {
		return options
	}
	options.PolicyProjectRoot = t.primary
	options.DeferredWorktree = true
	options.WorkspaceIsolated = true
	options.RequireFinalDiffGate = true
	options.FinalizeWriter = t.finalize
	return options
}

func (t *codingParallelTurn) noteSteps(n int) {
	if t == nil || !t.deferred {
		return
	}
	t.mu.Lock()
	t.remaining = n
	t.mu.Unlock()
}

func (t *codingParallelTurn) finalize(result *CodingSubAgentResult) (bool, error) {
	if t == nil || !t.deferred {
		return true, nil
	}
	t.mu.Lock()
	if t.remaining > 0 {
		t.remaining--
	}
	run := t.remaining <= 0 && !t.merged
	ctx := t.runCtx
	t.mu.Unlock()
	if !run {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return t.integrate(ctx, result)
}

func (t *codingParallelTurn) finish(ctx context.Context) {
	if t == nil {
		return
	}
	defer codingParallelTurns.Delete(codingParallelTurnKey(t.taskPath))
	if !t.deferred || t.wt == nil {
		return
	}
	t.mu.Lock()
	already := t.merged
	t.mu.Unlock()
	if !already {
		_, err := t.integrate(ctx, nil)
		if err != nil {
			t.mu.Lock()
			untouched := !t.merged
			t.mu.Unlock()
			// Cancel before any primary write drops the checkout. A conflict
			// or a file already written stays; deleting that checkout would
			// throw away the copy a person still has to review.
			if untouched && ctx != nil && ctx.Err() != nil {
				t.releaseExec(true)
				return
			}
			if untouched && t.handler != nil {
				t.handler.rememberCodingWorktree(t.userID, t.wt, "merge-failed")
			}
		}
	}
	t.mu.Lock()
	discard := t.discardWorktree
	t.mu.Unlock()
	// A clean merge removes the checkout. A detached read-only child is still
	// reading that directory, so the removal waits until the child returns.
	// A conflict keeps the directory for review and only drops the lookup.
	t.releaseExec(discard)
}

// codingParallelByExec finds the turn whose checkout a detached reader is
// still using. The tool map is removed when the turn returns; this one stays
// until the checkout is removed or deliberately kept.
var codingParallelByExec sync.Map

func codingParallelRememberExec(t *codingParallelTurn) {
	if t == nil || strings.TrimSpace(t.execDir) == "" {
		return
	}
	codingParallelByExec.Store(normalizeCodingRuntimeRef(t.execDir), t)
}

func codingParallelForgetExec(t *codingParallelTurn) {
	if t == nil || strings.TrimSpace(t.execDir) == "" {
		return
	}
	key := normalizeCodingRuntimeRef(t.execDir)
	if cur, ok := codingParallelByExec.Load(key); ok && cur == t {
		codingParallelByExec.Delete(key)
	}
}

// codingParallelTrackExec keeps the checkout alive for one detached read-only
// child. The child may use the project subdirectory, the worktree root, or a
// directory under that root. The returned function runs when the child returns.
func codingParallelTrackExec(projectPath string) func() {
	turn := codingParallelTurnForExec(projectPath)
	if turn == nil || turn.wt == nil {
		return func() {}
	}
	turn.childMu.Lock()
	turn.children++
	turn.childMu.Unlock()
	return turn.finishChild
}

// codingParallelTurnForExec finds the checkout a detached reader still uses.
// The ledger child is keyed by the project subdirectory. A reader of the
// worktree root, or of a directory under it, is the same checkout.
func codingParallelTurnForExec(projectPath string) *codingParallelTurn {
	if strings.TrimSpace(projectPath) == "" {
		return nil
	}
	if value, ok := codingParallelByExec.Load(normalizeCodingRuntimeRef(projectPath)); ok {
		turn, _ := value.(*codingParallelTurn)
		if turn != nil && turn.wt != nil {
			return turn
		}
	}
	var found *codingParallelTurn
	codingParallelByExec.Range(func(_, value any) bool {
		turn, _ := value.(*codingParallelTurn)
		if turn == nil || turn.wt == nil || strings.TrimSpace(turn.wt.Path) == "" {
			return true
		}
		if pathInsideWorkRoot(turn.wt.Path, projectPath) {
			found = turn
			return false
		}
		return true
	})
	return found
}

func (t *codingParallelTurn) releaseExec(deleteFiles bool) {
	if t == nil {
		return
	}
	t.childMu.Lock()
	if t.children > 0 {
		if deleteFiles {
			t.cleanupAfterChildren = true
		} else {
			t.forgetAfterChildren = true
		}
		t.childMu.Unlock()
		return
	}
	t.childMu.Unlock()
	codingParallelForgetExec(t)
	if deleteFiles && t.wt != nil {
		t.wt.cleanup(false)
	}
}

func (t *codingParallelTurn) finishChild() {
	if t == nil {
		return
	}
	t.childMu.Lock()
	if t.children > 0 {
		t.children--
	}
	left := t.children
	deleteFiles := left == 0 && t.cleanupAfterChildren
	forget := left == 0 && (t.cleanupAfterChildren || t.forgetAfterChildren)
	t.childMu.Unlock()
	if !forget {
		return
	}
	codingParallelForgetExec(t)
	if deleteFiles && t.wt != nil {
		t.wt.cleanup(false)
	}
}

func (t *codingParallelTurn) integrate(ctx context.Context, result *CodingSubAgentResult) (bool, error) {
	t.mu.Lock()
	if t.merged {
		t.mu.Unlock()
		return true, nil
	}
	t.merged = true
	wt := t.wt
	t.mu.Unlock()
	if wt == nil {
		return true, nil
	}
	if err := t.waitHolder(ctx); err != nil {
		t.mu.Lock()
		t.merged = false
		t.mu.Unlock()
		return false, err
	}
	summary, err := integrateCodingParallelWorktree(wt, t.baseSHA)
	if err != nil {
		// The diff failed before any primary write. Leave the merge unmarked
		// so finish can try once more. A failure after a file was written
		// stays marked: repeating the 3-way merge would apply that file twice.
		untouched := summary.Applied == 0 && len(summary.Conflicts) == 0
		if untouched {
			t.mu.Lock()
			t.merged = false
			t.mu.Unlock()
		} else if t.handler != nil {
			t.handler.rememberCodingWorktree(t.userID, wt, "merge-failed")
		}
		return false, err
	}
	if len(summary.Conflicts) > 0 {
		notice := fmt.Sprintf("自动合入 %d 个文件，还有 %d 个要人看", summary.Applied, len(summary.Conflicts))
		if t.onProgress != nil {
			t.onProgress(notice)
		}
		t.handler.storeStickyCodingConflict(t.userID, codingWorkbenchConflict{
			StepIndex:   wt.StepIndex,
			Branch:      wt.Branch,
			Path:        wt.Path,
			ProjectPath: wt.ProjectPath,
			GitRoot:     wt.GitRoot,
			MainProject: t.primary,
			Error:       fmt.Sprintf("%d 个文件无法自动合并", len(summary.Conflicts)),
			Kind:        "local_worktree",
			Files:       append([]string(nil), summary.Conflicts...),
		})
		wt.cleanup(true)
		if result != nil {
			result.FilesModified = summary.landedModified(result.FilesModified)
			result.FilesCreated = summary.landedModified(result.FilesCreated)
			t.rewriteLandedResultPaths(result)
		}
		return false, fmt.Errorf("%s", notice)
	}
	if t.onProgress != nil && summary.Applied > 0 {
		t.onProgress(fmt.Sprintf("已自动合入 %d 个文件", summary.Applied))
	}
	t.mu.Lock()
	t.discardWorktree = true
	t.mu.Unlock()
	t.rewriteLandedResultPaths(result)
	return true, nil
}

// rewriteLandedResultPaths points the turn's file list at the tree the merge
// wrote. An absolute checkout path would dangle once that checkout is removed.
func (t *codingParallelTurn) rewriteLandedResultPaths(result *CodingSubAgentResult) {
	if t == nil || result == nil {
		return
	}
	result.FilesModified = t.remapLandedPaths(t.execDir, result.FilesModified)
	result.FilesCreated = t.remapLandedPaths(t.execDir, result.FilesCreated)
}

func (t *codingParallelTurn) waitHolder(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if t == nil || t.handler == nil || t.handler.app == nil {
		return nil
	}
	waitLogged := false
	waitStart := time.Now()
	for {
		// A stop must win even when the other task has already left.
		// Otherwise finish writes the primary for a turn the person cancelled.
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		holder, _ := t.handler.app.earlierLiveWorkRootHolder(t.taskPath, t.primary)
		if holder == "" {
			if waitLogged {
				log.Printf("[work-root] task %q released: earlier writer done after %s", t.taskPath, time.Since(waitStart).Round(time.Second))
			}
			if ctx != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if !waitLogged {
			waitLogged = true
			log.Printf("[work-root] task %q waiting on earlier writer holding %q holder=%q", t.taskPath, t.primary, holder)
			// Re-announce the queued state when the wait actually begins. The
			// begin-time event can race a later progress snapshot; re-sending
			// here keeps waiting_writer on the header for the whole block.
			emitCodingParallelWriterWait(t.onProgress, "")
		}
		timer := time.NewTimer(500 * time.Millisecond)
		if ctx == nil {
			<-timer.C
			continue
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type codingParallelIntegrateSummary struct {
	Applied       int
	Conflicts     []string
	landed        map[string]bool
	roots         []string
	projectPrefix string
}

func (s codingParallelIntegrateSummary) landedModified(paths []string) []string {
	if len(s.landed) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if codingParallelPathLanded(s.landed, s.roots, s.projectPrefix, path) {
			out = append(out, path)
		}
	}
	return out
}

// codingParallelPathLanded reports whether path is one of the git-relative
// files that were written onto the primary. A bare file name is not enough:
// sub/other.txt and other.txt are different files.
func codingParallelPathLanded(landed map[string]bool, roots []string, projectPrefix, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
		if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return false
		}
		if codingParallelLandedKey(landed, rel) {
			return true
		}
		return projectPrefix != "" && codingParallelLandedKey(landed, projectPrefix+"/"+rel)
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if codingParallelLandedKey(landed, rel) {
			return true
		}
	}
	return false
}

func codingParallelLandedKey(landed map[string]bool, path string) bool {
	return landed[strings.ToLower(filepath.ToSlash(path))]
}

var codingParallelMergeLocks sync.Map

func codingParallelMergeLock(gitRoot string) func() {
	key := normalizeCodingRuntimeRef(gitRoot)
	value, _ := codingParallelMergeLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// integrateCodingParallelWorktree copies worktree edits onto the primary
// working tree. A file the primary did not touch is taken from the worktree.
// A file both sides changed is merged with git merge-file. Overlapping hunks,
// binary files, and a delete against an edit stay on the worktree.
func integrateCodingParallelWorktree(wt *codingWorkbenchWorktree, baseSHA string) (codingParallelIntegrateSummary, error) {
	var summary codingParallelIntegrateSummary
	if wt == nil || wt.Path == "" || strings.TrimSpace(baseSHA) == "" {
		return summary, fmt.Errorf("parallel worktree merge is missing its base")
	}
	root := wt.GitRoot
	if root == "" {
		root = wt.Path
	}
	unlock := codingParallelMergeLock(root)
	defer unlock()
	changes, err := codingParallelChangedFiles(wt.Path, baseSHA)
	if err != nil {
		return summary, err
	}
	summary.landed = map[string]bool{}
	summary.roots = []string{root, wt.Path}
	if wt.ProjectPath != "" && wt.ProjectPath != wt.Path {
		summary.roots = append(summary.roots, wt.ProjectPath)
	}
	if rel, err := filepath.Rel(wt.Path, wt.ProjectPath); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "" && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			summary.projectPrefix = rel
		}
	}
	for _, change := range changes {
		rel, ok := codingParallelRelPath(change.path)
		if !ok {
			log.Printf("[coding-parallel] skip worktree path outside the repository: %s", change.path)
			continue
		}
		change.path = rel
		applied, conflict, applyErr := applyCodingParallelFile(root, wt.Path, baseSHA, change)
		if applyErr != nil {
			// One file must not stop the others. Earlier files may already be
			// on the primary, so this merge cannot be run again from scratch.
			log.Printf("[coding-parallel] file %s stayed on the worktree: %v", change.path, applyErr)
			summary.Conflicts = append(summary.Conflicts, change.path)
			continue
		}
		if conflict {
			summary.Conflicts = append(summary.Conflicts, change.path)
			continue
		}
		if applied {
			summary.Applied++
			summary.landed[strings.ToLower(change.path)] = true
		}
	}
	return summary, nil
}

type codingParallelChange struct {
	path    string
	deleted bool
}

func codingParallelChangedFiles(worktree, baseSHA string) ([]codingParallelChange, error) {
	// Name-status must be git's own records. A configured external diff or
	// textconv can exit non-zero and drop the path list.
	raw, err := codingParallelGit(worktree, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("worktree diff: %w", err)
	}
	changes := parseCodingParallelNameStatus(raw)
	others, err := codingParallelGit(worktree, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("worktree untracked files: %w", err)
	}
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.path] = true
	}
	for _, path := range bytes.Split(others, []byte{0}) {
		rel := filepath.ToSlash(string(path))
		if rel == "" || seen[rel] {
			continue
		}
		changes = append(changes, codingParallelChange{path: rel})
	}
	return changes, nil
}

func parseCodingParallelNameStatus(raw []byte) []codingParallelChange {
	parts := bytes.Split(raw, []byte{0})
	var changes []codingParallelChange
	for i := 0; i < len(parts); {
		if len(parts[i]) == 0 {
			i++
			continue
		}
		status := string(parts[i])
		i++
		if i >= len(parts) {
			break
		}
		first := filepath.ToSlash(string(parts[i]))
		i++
		if first == "" {
			continue
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i >= len(parts) {
				break
			}
			second := filepath.ToSlash(string(parts[i]))
			i++
			// Rename removes the old path. Copy keeps it; deleting the source
			// would drop a file the worktree still has.
			if strings.HasPrefix(status, "R") {
				changes = append(changes, codingParallelChange{path: first, deleted: true})
			}
			if second != "" {
				changes = append(changes, codingParallelChange{path: second})
			}
			continue
		}
		changes = append(changes, codingParallelChange{
			path:    first,
			deleted: strings.HasPrefix(status, "D"),
		})
	}
	return changes
}

func applyCodingParallelFile(primaryRoot, worktree, baseSHA string, change codingParallelChange) (applied bool, conflict bool, err error) {
	rel := change.path
	primaryPath := filepath.Join(primaryRoot, filepath.FromSlash(rel))
	theirPath := filepath.Join(worktree, filepath.FromSlash(rel))
	// A symlink parent is not a normal directory. Writing or deleting through
	// it lands outside the tree the path string names.
	if !codingParallelWithinRoot(primaryRoot, primaryPath) || !codingParallelWithinRoot(worktree, theirPath) {
		return false, true, nil
	}
	base, baseOK, err := codingParallelBlob(primaryRoot, baseSHA, rel)
	if err != nil {
		return false, false, err
	}
	return applyCodingParallelFileOnce(primaryPath, theirPath, base, baseOK, change.deleted, true)
}

func applyCodingParallelFileOnce(primaryPath, theirPath string, base []byte, baseOK, deleted, allowRetry bool) (bool, bool, error) {
	var applied, conflict bool
	err := tool.WithTextFileMutation(primaryPath, func() error {
		if codingParallelIrregular(primaryPath) || codingParallelIrregular(theirPath) {
			// A directory or symlink is not a 3-way text merge. Keep the
			// primary bytes and let the remaining files land.
			conflict = true
			return nil
		}
		primary, primaryOK, readErr := readCodingParallelFile(primaryPath)
		if readErr != nil {
			return readErr
		}
		theirs, theirOK, readErr := readCodingParallelFile(theirPath)
		if readErr != nil {
			return readErr
		}
		if deleted || !theirOK {
			if !primaryOK {
				return nil
			}
			if baseOK && codingParallelSameText(primary, base) {
				if removeErr := codingParallelRemoveFile(primaryPath); removeErr != nil {
					return removeErr
				}
				applied = true
				return nil
			}
			conflict = true
			return nil
		}
		if primaryOK && codingParallelSameText(primary, theirs) {
			// Same text. A mode-only edit still has to land when the holder
			// left the bytes at the base: the worktree mode replaces it.
			// Once the holder has edited the text, keep the primary mode.
			if baseOK && codingParallelSameText(primary, base) {
				want := codingParallelPerm(theirPath)
				if want != codingParallelPerm(primaryPath) {
					if chmodErr := os.Chmod(primaryPath, want); chmodErr != nil {
						return chmodErr
					}
					applied = true
				}
			}
			return nil
		}
		if !primaryOK && !baseOK {
			if writeErr := writeCodingParallelFile(primaryPath, theirs, codingParallelPerm(theirPath)); writeErr != nil {
				return writeErr
			}
			applied = true
			return nil
		}
		if !primaryOK && baseOK {
			conflict = true
			return nil
		}
		if baseOK && codingParallelSameText(primary, base) {
			if writeErr := writeCodingParallelReplacing(primaryPath, theirs, codingParallelPerm(primaryPath)); writeErr != nil {
				return writeErr
			}
			applied = true
			return nil
		}
		if codingParallelBinary(base) || codingParallelBinary(primary) || codingParallelBinary(theirs) {
			conflict = true
			return nil
		}
		// The committed blob is LF. On Windows the working tree is often
		// CRLF, and merge-file would then conflict on every edited line.
		// Compare and merge the LF form, then write the primary's newline style.
		merged, clean, mergeErr := codingParallelMergeFile(codingParallelLF(base), codingParallelLF(primary), codingParallelLF(theirs))
		if mergeErr != nil {
			return mergeErr
		}
		if !clean {
			conflict = true
			return nil
		}
		if codingParallelUseCRLF(primary) {
			merged = codingParallelToCRLF(merged)
		}
		again, againOK, readErr := readCodingParallelFile(primaryPath)
		if readErr != nil {
			return readErr
		}
		if !againOK || !bytes.Equal(again, primary) {
			if allowRetry {
				return errCodingParallelRetry
			}
			conflict = true
			return nil
		}
		if writeErr := writeCodingParallelReplacing(primaryPath, merged, codingParallelPerm(primaryPath)); writeErr != nil {
			return writeErr
		}
		applied = true
		return nil
	})
	if errors.Is(err, errCodingParallelRetry) && allowRetry {
		return applyCodingParallelFileOnce(primaryPath, theirPath, base, baseOK, deleted, false)
	}
	return applied, conflict, err
}

var errCodingParallelRetry = errors.New("primary file changed during merge")

// codingParallelAllowWrite clears a read-only attribute. The merge has
// already decided this file is being replaced; a checkout bit must not turn
// that decision into a conflict.
func codingParallelAllowWrite(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o200 != 0 {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()|0o200)
}

func codingParallelRemoveFile(path string) error {
	if err := codingParallelAllowWrite(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeCodingParallelReplacing writes a file the merge is allowed to replace.
// An existing read-only file is made writable for the write, then restored to
// the mode the caller asked to keep.
func writeCodingParallelReplacing(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := codingParallelAllowWrite(path); err != nil {
		return err
	}
	if err := writeCodingParallelFile(path, content, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func writeCodingParallelFile(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, mode)
}

// codingParallelPerm is the permission bits to keep on a file this merge
// writes. A new file takes the worktree mode so an executable script stays
// executable. An existing file is passed its own mode; the create flag is
// what applies when the file is being created.
func codingParallelPerm(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0o644
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		return 0o644
	}
	return perm
}

func readCodingParallelFile(path string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return content, true, nil
}

func codingParallelSameText(left, right []byte) bool {
	return bytes.Equal(codingParallelLF(left), codingParallelLF(right))
}

func codingParallelLF(content []byte) []byte {
	if !bytes.Contains(content, []byte("\r\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

func codingParallelUseCRLF(content []byte) bool {
	if !bytes.Contains(content, []byte("\r\n")) {
		return false
	}
	rest := bytes.ReplaceAll(content, []byte("\r\n"), nil)
	return !bytes.Contains(rest, []byte("\n"))
}

func codingParallelToCRLF(content []byte) []byte {
	if bytes.Contains(content, []byte("\r\n")) || !bytes.Contains(content, []byte("\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
}

// codingParallelWithinRoot reports whether path stays inside root without
// following a symlink or a Windows junction. Missing tail components are
// allowed: the merge creates them. An existing link is not, because the
// write would follow it out of the tree.
func codingParallelWithinRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel == "." {
		return true
	}
	var names []string
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return false
		}
		names = append(names, part)
	}
	cur := root
	for i, part := range names {
		cur = filepath.Join(cur, part)
		info, statErr := os.Lstat(cur)
		if statErr != nil {
			// The merge creates a missing tail. Any other stat failure stays
			// closed: the path is not proven to be inside the tree.
			if errors.Is(statErr, os.ErrNotExist) {
				return true
			}
			return false
		}
		// A symlink or a Windows junction is not a real directory. Lstat
		// leaves ModeSymlink unset for a junction, but Readlink still
		// returns its target, and a write through that parent leaves the tree.
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if _, linkErr := os.Readlink(cur); linkErr == nil {
			return false
		}
		last := i == len(names)-1
		if !info.IsDir() && (!last || !info.Mode().IsRegular()) {
			return false
		}
	}
	return true
}

func codingParallelIrregular(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return !info.Mode().IsRegular()
}

func codingParallelRelPath(rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func codingParallelBinary(content []byte) bool {
	n := len(content)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(content[:n], 0) >= 0
}

func codingParallelBlob(repo, sha, rel string) ([]byte, bool, error) {
	// rev-parse --verify --quiet exits 1 with empty stderr both when the
	// path is absent and when the commit itself is missing. Confirm the
	// commit before treating that status as "not in the tree".
	spec := sha + ":" + rel
	if _, err := codingParallelGit(repo, "rev-parse", "--verify", "--quiet", spec); err != nil {
		if !codingParallelGitMissing(err) {
			return nil, false, err
		}
		if _, commitErr := codingParallelGit(repo, "rev-parse", "--verify", "--quiet", sha+"^{commit}"); commitErr != nil {
			return nil, false, commitErr
		}
		return nil, false, nil
	}
	out, err := codingParallelGit(repo, "cat-file", "blob", spec)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func codingParallelGitMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.TrimSpace(err.Error())
	return msg == "exit status 1" || strings.HasPrefix(msg, "exit status 1\n") || strings.HasPrefix(msg, "exit status 1 ")
}

func codingParallelMergeFile(base, ours, theirs []byte) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "maclaw-merge-file-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	basePath := filepath.Join(dir, "base")
	oursPath := filepath.Join(dir, "ours")
	theirPath := filepath.Join(dir, "theirs")
	if err := os.WriteFile(basePath, base, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(oursPath, ours, 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(theirPath, theirs, 0o644); err != nil {
		return nil, false, err
	}
	cmd := exec.Command("git", "merge-file", "-p", oursPath, basePath, theirPath)
	out, err := cmd.Output()
	if err == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return nil, false, nil
	}
	return nil, false, err
}

func codingParallelGit(dir string, args ...string) ([]byte, error) {
	// --no-pager and color.ui=false keep script output stable. A pager.diff
	// or color.ui=always config would otherwise stall the merge or paint
	// status letters the parser does not understand.
	cmd := exec.Command("git", append([]string{"--no-pager", "-C", dir, "-c", "color.ui=false"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"LC_ALL=C",
		"LANG=C",
	)
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(string(exitErr.Stderr))
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return nil, err
}

func codingParallelGitText(dir string, args ...string) (string, error) {
	out, err := codingParallelGit(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// codingSpawnCheckoutSnap is the deferred checkout before a worker runs.
// git stash create records tracked content without moving HEAD or the
// worktree. Untracked files are not in that commit, so their bytes are kept
// beside it. Rollback restores only the paths the worker changed.
type codingSpawnCheckoutSnap struct {
	dir       string
	gitRoot   string
	sha       string
	untracked map[string]codingSpawnUntrackedFile
}

type codingSpawnUntrackedFile struct {
	body   []byte
	link   string
	linked bool
}

type codingSpawnUntrackedDelta struct {
	path   string
	before codingSpawnUntrackedFile
	had    bool
}

func codingSpawnSnapshotCheckout(dir string) (*codingSpawnCheckoutSnap, error) {
	root, err := codingParallelGitText(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	sha, err := codingParallelGitText(root, "stash", "create")
	if err != nil {
		return nil, err
	}
	if sha == "" {
		sha, err = codingParallelGitText(root, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
	}
	untracked, err := codingSpawnReadUntracked(root)
	if err != nil {
		return nil, err
	}
	return &codingSpawnCheckoutSnap{dir: root, gitRoot: root, sha: sha, untracked: untracked}, nil
}

// codingSpawnProjectPaths rewrites repo-relative git names into the worker's
// project directory. A file outside that directory is not in the declared
// write-set, even when its repo-relative name matches a declared basename.
func codingSpawnProjectPaths(gitRoot, project string, changed []string) ([]string, error) {
	gitRoot = filepath.Clean(gitRoot)
	project = filepath.Clean(project)
	if project == "" || project == "." {
		project = gitRoot
	}
	out := make([]string, 0, len(changed))
	for _, rel := range changed {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		abs := filepath.Join(gitRoot, filepath.FromSlash(rel))
		projRel, err := filepath.Rel(project, abs)
		if err != nil {
			return nil, err
		}
		projRel = filepath.ToSlash(projRel)
		if projRel == ".." || strings.HasPrefix(projRel, "../") {
			return nil, fmt.Errorf("worktree changed undeclared path %q", rel)
		}
		out = append(out, projRel)
	}
	return out, nil
}

func codingSpawnSafeRel(rel string) (string, error) {
	cleaned, ok := codingParallelRelPath(rel)
	if !ok {
		return "", fmt.Errorf("checkout path %q escapes the repository", rel)
	}
	return cleaned, nil
}

func codingSpawnReadUntracked(root string) (map[string]codingSpawnUntrackedFile, error) {
	raw, err := codingParallelGit(root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	out := map[string]codingSpawnUntrackedFile{}
	for _, part := range bytes.Split(raw, []byte{0}) {
		rawRel := strings.TrimSpace(filepath.ToSlash(string(part)))
		if rawRel == "" {
			continue
		}
		rel, relErr := codingSpawnSafeRel(rawRel)
		if relErr != nil {
			return nil, relErr
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		// Parents must be real directories. The final entry may be a symlink;
		// that link is recorded and is not followed.
		parent := filepath.Dir(path)
		if filepath.Clean(parent) != filepath.Clean(root) && !codingParallelWithinRoot(root, parent) {
			return nil, fmt.Errorf("untracked path %s escapes the checkout", rel)
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return nil, linkErr
			}
			out[rel] = codingSpawnUntrackedFile{link: target, linked: true}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("untracked path %s is not a regular file", rel)
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		out[rel] = codingSpawnUntrackedFile{body: body}
	}
	return out, nil
}

func (s *codingSpawnCheckoutSnap) trackedChanges() ([]codingParallelChange, error) {
	if s == nil {
		return nil, fmt.Errorf("missing checkout snapshot")
	}
	raw, err := codingParallelGit(s.dir, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", s.sha)
	if err != nil {
		return nil, err
	}
	changes := parseCodingParallelNameStatus(raw)
	for i, change := range changes {
		rel, relErr := codingSpawnSafeRel(change.path)
		if relErr != nil {
			return nil, relErr
		}
		changes[i].path = rel
	}
	return changes, nil
}

func (s *codingSpawnCheckoutSnap) untrackedDeltas() ([]codingSpawnUntrackedDelta, error) {
	if s == nil {
		return nil, fmt.Errorf("missing checkout snapshot")
	}
	now, err := codingSpawnReadUntracked(s.dir)
	if err != nil {
		return nil, err
	}
	var deltas []codingSpawnUntrackedDelta
	seen := map[string]bool{}
	for path, before := range s.untracked {
		seen[path] = true
		after, has := now[path]
		if has && codingSpawnUntrackedSame(before, after) {
			continue
		}
		deltas = append(deltas, codingSpawnUntrackedDelta{path: path, before: before, had: true})
	}
	for path := range now {
		if seen[path] {
			continue
		}
		deltas = append(deltas, codingSpawnUntrackedDelta{path: path})
	}
	return deltas, nil
}

func codingSpawnUntrackedSame(left, right codingSpawnUntrackedFile) bool {
	if left.linked || right.linked {
		return left.linked && right.linked && left.link == right.link
	}
	return bytes.Equal(left.body, right.body)
}

func (s *codingSpawnCheckoutSnap) changedPaths() ([]string, error) {
	tracked, err := s.trackedChanges()
	if err != nil {
		return nil, err
	}
	deltas, err := s.untrackedDeltas()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(tracked)+len(deltas))
	for _, change := range tracked {
		if change.path != "" {
			paths = append(paths, change.path)
		}
	}
	for _, delta := range deltas {
		if delta.path != "" {
			paths = append(paths, delta.path)
		}
	}
	return paths, nil
}

func (s *codingSpawnCheckoutSnap) rollback() error {
	if s == nil {
		return fmt.Errorf("missing checkout snapshot")
	}
	tracked, trackErr := s.trackedChanges()
	deltas, deltaErr := s.untrackedDeltas()
	var errs []error
	if trackErr != nil {
		errs = append(errs, trackErr)
	}
	if deltaErr != nil {
		errs = append(errs, deltaErr)
	}
	for _, change := range tracked {
		if err := s.restoreTracked(change); err != nil {
			errs = append(errs, err)
		}
	}
	for _, delta := range deltas {
		if err := s.restoreUntracked(delta); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *codingSpawnCheckoutSnap) restoreTracked(change codingParallelChange) error {
	rel, err := codingSpawnSafeRel(change.path)
	if err != nil {
		return err
	}
	exists, err := codingSpawnBlobInCommit(s.dir, s.sha, rel)
	if err != nil {
		return err
	}
	if exists {
		_, err = codingParallelGit(s.dir, "checkout", s.sha, "--", rel)
		return err
	}
	return codingSpawnRemovePath(s.dir, rel)
}

func codingSpawnBlobInCommit(dir, sha, rel string) (bool, error) {
	_, err := codingParallelGit(dir, "rev-parse", "--verify", "--quiet", sha+":"+rel)
	if err == nil {
		return true, nil
	}
	if codingParallelGitMissing(err) {
		return false, nil
	}
	return false, err
}

func codingSpawnContainPath(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to touch %s outside the checkout", path)
	}
	parent := filepath.Dir(path)
	if parent != root && !codingParallelWithinRoot(root, parent) {
		return fmt.Errorf("refusing to touch %s outside the checkout", path)
	}
	return nil
}

func codingSpawnRemovePath(root, rel string) error {
	if _, err := codingParallelGit(root, "rm", "-f", "--ignore-unmatch", "--", rel); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := codingSpawnContainPath(root, path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *codingSpawnCheckoutSnap) restoreUntracked(delta codingSpawnUntrackedDelta) error {
	rel, err := codingSpawnSafeRel(delta.path)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := codingSpawnContainPath(s.dir, path); err != nil {
		return err
	}
	if !delta.had {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if delta.before.linked {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Symlink(delta.before.link, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, delta.before.body, 0o644)
}
