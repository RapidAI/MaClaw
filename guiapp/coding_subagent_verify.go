package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// ---------------------------------------------------------------------------
// Built-in Verify Loop for CodingSubAgent
//
// Codex-inspired improvement: after the main coding loop completes, if the
// model didn't run verification itself, automatically run the project's
// verification command. If verification fails, inject the error back into
// the SAME conversation context (no new SubAgent, no lost context) and let
// the model fix it. Maximum 2 verify-fix rounds to prevent infinite loops.
//
// Design principles:
// - Only triggers when model skipped verification (hasRunVerification=false)
// - Only triggers when files were actually modified (pure read tasks skip)
// - Reuses existing conversation (model sees its own edits + error output)
// - Max 2 rounds: verify→fix→verify→fix→give up
// - Project verify command is auto-detected from build system files
// ---------------------------------------------------------------------------

const (
	subAgentMaxVerifyFixRounds    = 2
	subAgentVerifyOutputMaxTokens = 2000
)

// detectProjectVerifyCommand scans the project root for build system files
// and returns an appropriate verification command. Returns "" if none detected.
func detectProjectVerifyCommand(projectPath string) string {
	if projectPath == "" {
		return ""
	}

	isWindows := strings.EqualFold(normalizedRemotePlatform(), "windows")

	// Check in priority order (most specific to least specific)
	checks := []struct {
		file    string
		command string
	}{
		{"go.mod", goVerifyCommand()},
		{"Cargo.toml", "cargo check"},
		{"package.json", detectNodeVerifyCommand(projectPath)},
		{"pyproject.toml", pythonVerifyCommand(isWindows)},
		{"CMakeLists.txt", detectCMakeVerifyCommand(projectPath)},
		{"Makefile", makeVerifyCommand(isWindows)},
	}

	for _, c := range checks {
		if c.command == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(projectPath, c.file)); err == nil {
			return c.command
		}
	}
	return ""
}

// goVerifyCommand short-circuits. A semicolon would keep going after a failed
// build, and a later successful go vet would hide that failure. The Windows
// coding shell rewrites && for PowerShell 5.1.
//
// This is the whole-module command. A task step must not run it as-is:
// scopeGoVerifyToTask narrows it to packages that task actually changed so
// historical findings in untouched packages do not fail the step.
func goVerifyCommand() string {
	return "go build ./... && go vet ./..."
}

const taskVerifyNoRelatedGoReason = "task changed no Go packages; unrelated historical build/vet findings ignored"

// verifyGateStderrBytes is how much Go build/vet stderr the step gate keeps
// before judging. The ordinary bash cap is 4KB, which can cut off a related
// diagnostic and leave only an unrelated vet nit in the prefix.
const verifyGateStderrBytes = 256 * 1024

const (
	// Printed between go build and go vet. A failure after the last
	// __MACLAW_BUILT__ and before __MACLAW_VETTED__ is vet output, so an
	// analyzer that is not on the phrase list is still historical when the
	// file is not part of this task. A later build failure prints VETTED
	// first and must not be washed out.
	verifyBuildOKMarker = "__MACLAW_BUILT__"
	verifyVetOKMarker   = "__MACLAW_VETTED__"
)

// taskGoPkg is one package the current task changed, relative to the module
// that owns it. moduleDir is "." for the project module. main is true when
// that package declares package main, so go build would write an executable
// into the working directory unless -o points somewhere else. needsBuild is
// false for a pure Go library: go vet already typechecks it, and go build
// would do that work a second time.
type taskGoPkg struct {
	moduleDir  string
	pkg        string
	main       bool
	needsBuild bool
}

// scopeGoVerifyToTask decides what a Go step gate is allowed to judge.
// Historical vet/build findings in packages this task did not change are
// ignored. A non-Go verify command is left unchanged. skip means there is
// no task-related Go code to verify, which is a pass, not a failure.
func scopeGoVerifyToTask(cmd, projectPath string, taskFiles []string) (scoped string, skip bool, reason string) {
	return scopeGoVerifyToTaskAt(cmd, projectPath, taskFiles, "")
}

// scopeGoVerifyToTaskAt is scopeGoVerifyToTask with a directory for main
// package executables. exeDir empty keeps `go build` writing into the
// working directory. A trailing slash makes -o a directory so several main
// packages can share it.
func scopeGoVerifyToTaskAt(cmd, projectPath string, taskFiles []string, exeDir string) (scoped string, skip bool, reason string) {
	if !isWholeModuleGoVerify(cmd) {
		return cmd, false, ""
	}
	pkgs := goPackagesForTask(projectPath, taskFiles)
	meta := taskGoModuleMeta(projectPath, taskFiles)
	if len(pkgs) == 0 && len(meta) == 0 {
		return "", true, taskVerifyNoRelatedGoReason
	}
	return formatTaskGoVerify(pkgs, meta, normalizeVerifyOutDir(exeDir)), false, ""
}

// localVerifyBuildOutDir is where a local step gate puts main-package
// executables. go build of package main otherwise drops them in the repo.
func localVerifyBuildOutDir() string {
	return normalizeVerifyOutDir(filepath.Join(os.TempDir(), "maclaw-verify"))
}

// remoteVerifyBuildOutDir is the same idea for a remote bash gate. The
// command is built on this machine and executed on the remote host.
const remoteVerifyBuildOutDir = "/tmp/maclaw-verify/"

func normalizeVerifyOutDir(exeDir string) string {
	exeDir = strings.TrimSpace(exeDir)
	if exeDir == "" {
		return ""
	}
	exeDir = filepath.ToSlash(exeDir)
	if !strings.HasSuffix(exeDir, "/") {
		exeDir += "/"
	}
	return exeDir
}

func isWholeModuleGoVerify(cmd string) bool {
	return strings.Join(strings.Fields(cmd), " ") == goVerifyCommand()
}

func formatTaskGoVerify(pkgs []taskGoPkg, metaModules []string, exeDir string) string {
	meta := make(map[string]bool, len(metaModules))
	grouped := map[string][]taskGoPkg{}
	var order []string
	addMod := func(mod string) {
		if _, ok := grouped[mod]; ok {
			return
		}
		grouped[mod] = nil
		order = append(order, mod)
	}
	for _, mod := range metaModules {
		meta[mod] = true
		addMod(mod)
	}
	for _, p := range pkgs {
		addMod(p.moduleDir)
		grouped[p.moduleDir] = appendTaskGoPkg(grouped[p.moduleDir], p)
	}
	sort.Strings(order)
	var parts []string
	for _, mod := range order {
		ps := grouped[mod]
		sort.Slice(ps, func(i, j int) bool { return ps[i].pkg < ps[j].pkg })
		if len(ps) > 0 {
			// Packages this task edited are the ones that must compile against
			// a go.mod edit. Building the rest of the module stops at the first
			// historical failure and never reaches those packages.
			// Pure libraries are typechecked by go vet. go build is reserved
			// for main packages (link, and do not drop the exe in the repo)
			// and for cgo or assembly, which vet does not link.
			var builds, mains, vetPkgs []string
			for _, p := range ps {
				vetPkgs = append(vetPkgs, p.pkg)
				if p.main && exeDir != "" {
					mains = append(mains, p.pkg)
					continue
				}
				if p.needsBuild || p.main {
					builds = append(builds, p.pkg)
				}
			}
			if len(builds) > 0 {
				parts = append(parts, goModuleCommand(mod, "build", builds))
			}
			if len(mains) > 0 {
				parts = append(parts, goModuleCommand(mod, "build", append([]string{"-o", exeDir}, mains...)))
			}
			sort.Strings(vetPkgs)
			parts = append(parts,
				"echo "+verifyBuildOKMarker,
				goModuleCommand(mod, "vet", vetPkgs),
				"echo "+verifyVetOKMarker,
			)
			continue
		}
		if meta[mod] {
			parts = append(parts, goModuleCommand(mod, "build", []string{"./..."}))
		}
	}
	return strings.Join(parts, " && ")
}

func appendTaskGoPkg(list []taskGoPkg, p taskGoPkg) []taskGoPkg {
	for i := range list {
		if list[i].pkg == p.pkg && list[i].moduleDir == p.moduleDir {
			if p.main {
				list[i].main = true
			}
			if p.needsBuild {
				list[i].needsBuild = true
			}
			return list
		}
	}
	return append(list, p)
}

func goModuleCommand(moduleDir, verb string, args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteGoArg(arg)
	}
	tail := "go " + verb + " " + strings.Join(quoted, " ")
	if moduleDir == "" || moduleDir == "." {
		return tail
	}
	return "go -C " + quoteGoArg(moduleDir) + " " + verb + " " + strings.Join(quoted, " ")
}

// quoteGoArg quotes a package or module path for both PowerShell and bash.
// Clean relative paths stay bare so the command matches go's own spelling.
func quoteGoArg(token string) string {
	if token == "" || strings.ContainsAny(token, " \t\"&;|$<>^`()\r\n") {
		return `"` + strings.ReplaceAll(token, `"`, "") + `"`
	}
	return token
}

func goPackagesForTask(projectPath string, files []string) []taskGoPkg {
	seen := map[string]bool{}
	cache := map[string]string{}
	var out []taskGoPkg
	for _, file := range files {
		rel := taskRelativePath(projectPath, file)
		if !isTaskGoInput(projectPath, rel) {
			continue
		}
		mod, pkg, ok := taskGoPackage(projectPath, rel, cache)
		if !ok {
			continue
		}
		key := mod + "\x00" + pkg
		if seen[key] {
			// The first file already scanned this directory. A later cgo or
			// assembly path still has to link, even when that file is not on
			// disk yet.
			if extForcesBuild(rel) {
				for i := range out {
					if out[i].moduleDir == mod && out[i].pkg == pkg {
						out[i].needsBuild = true
					}
				}
			}
			continue
		}
		seen[key] = true
		isMain := packageDeclaresMain(projectPath, rel)
		needsBuild := isMain || packageNeedsGoBuild(projectPath, rel)
		out = append(out, taskGoPkg{
			moduleDir:  mod,
			pkg:        pkg,
			main:       isMain,
			needsBuild: needsBuild,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].moduleDir != out[j].moduleDir {
			return out[i].moduleDir < out[j].moduleDir
		}
		return out[i].pkg < out[j].pkg
	})
	return out
}

// taskGoModuleMeta returns module directories whose go.mod or go.sum this
// task wrote. The directory of the manifest is the module; "." is the root.
func taskGoModuleMeta(projectPath string, files []string) []string {
	seen := map[string]bool{}
	var mods []string
	for _, file := range files {
		rel := taskRelativePath(projectPath, file)
		base := path.Base(rel)
		if rel == "" || (base != "go.mod" && base != "go.sum") {
			continue
		}
		mod := path.Dir(rel)
		if mod == "" || mod == "." {
			mod = "."
		}
		if seen[mod] {
			continue
		}
		seen[mod] = true
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	return mods
}

func taskGoPackage(projectPath, rel string, cache map[string]string) (moduleDir, pkg string, ok bool) {
	if !isGoPackageInput(rel) {
		return "", "", false
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	moduleDir = nearestGoModule(projectPath, dir, cache)
	if moduleDir != "." && moduleDir != "" {
		switch {
		case dir == moduleDir:
			pkg = "."
		case strings.HasPrefix(dir, moduleDir+"/"):
			pkg = "./" + strings.TrimPrefix(dir, moduleDir+"/")
		default:
			pkg = "./" + dir
		}
	} else {
		moduleDir = "."
		if dir == "" {
			pkg = "."
		} else {
			pkg = "./" + dir
		}
	}
	return moduleDir, pkg, true
}

func isGoPackageInput(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".go", ".s", ".c", ".h", ".cc", ".cpp", ".cxx", ".m", ".syso", ".swig", ".swigcxx":
		return rel != ""
	default:
		return false
	}
}

// packageNeedsGoBuild reports whether go vet is not enough. A missing
// project tree stays on go build. A pure Go library does not: vet
// typechecks it, including tests, and a second go build only repeats that.
func packageNeedsGoBuild(projectPath, rel string) bool {
	if projectPath == "" || rel == "" {
		return true
	}
	if _, err := os.Stat(projectPath); err != nil {
		return true
	}
	if extForcesBuild(rel) {
		return true
	}
	dir := path.Dir(rel)
	abs := projectPath
	if dir != "." && dir != "" {
		abs = filepath.Join(projectPath, filepath.FromSlash(dir))
	}
	for _, pattern := range []string{"*.s", "*.S", "*.c", "*.cc", "*.cpp", "*.cxx", "*.m", "*.syso", "*.swig", "*.swigcxx", "*.h"} {
		matches, err := filepath.Glob(filepath.Join(abs, pattern))
		if err != nil || len(matches) > 0 {
			return true
		}
	}
	matches, err := filepath.Glob(filepath.Join(abs, "*.go"))
	if err != nil {
		return true
	}
	for _, match := range matches {
		if fileImportsC(match) {
			return true
		}
	}
	return false
}

func extForcesBuild(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".s", ".c", ".h", ".cc", ".cpp", ".cxx", ".m", ".syso", ".swig", ".swigcxx":
		return true
	default:
		return false
	}
}

func fileImportsC(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), `"C"`)
}

// packageDeclaresMain reports whether rel belongs to a main package.
// A missing tree (the remote project is not on this machine) returns false
// so the command stays a plain go build instead of failing with
// "no main packages to build".
func packageDeclaresMain(projectPath, rel string) bool {
	if projectPath == "" || rel == "" {
		return false
	}
	if _, err := os.Stat(projectPath); err != nil {
		return false
	}
	dir := path.Dir(rel)
	abs := projectPath
	if dir != "." && dir != "" {
		abs = filepath.Join(projectPath, filepath.FromSlash(dir))
	}
	matches, err := filepath.Glob(filepath.Join(abs, "*.go"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		base := strings.ToLower(filepath.Base(match))
		// External tests are package foo_test. They do not make go build
		// emit a binary, and an ignored package main file must not either.
		if strings.HasSuffix(base, "_test.go") || fileBuildIgnored(match) {
			continue
		}
		if fileDeclaresMain(match) {
			return true
		}
	}
	return false
}

// fileBuildIgnored reports the conventional //go:build ignore header.
// That file is not part of `go build`, so it must not flip a library into
// a main package.
func fileBuildIgnored(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "/*") {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			return false
		}
		if constraintIsIgnore(line) {
			return true
		}
	}
	return false
}

func constraintIsIgnore(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 2 && fields[0] == "//go:build" && fields[1] == "ignore" {
		return true
	}
	if len(fields) == 2 && fields[0] == "//+build" && fields[1] == "ignore" {
		return true
	}
	return len(fields) == 3 && fields[0] == "//" && fields[1] == "+build" && fields[2] == "ignore"
}

// fileDeclaresMain reads the package clause. Comments and build tags before
// it are skipped. Anything else means this header is not package main.
func fileDeclaresMain(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	inBlock := false
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if inBlock {
			end := strings.Index(line, "*/")
			if end < 0 {
				continue
			}
			inBlock = false
			line = strings.TrimSpace(line[end+2:])
		}
		for line != "" {
			if strings.HasPrefix(line, "//") {
				line = ""
				break
			}
			if strings.HasPrefix(line, "/*") {
				end := strings.Index(line, "*/")
				if end < 0 {
					inBlock = true
					line = ""
					break
				}
				line = strings.TrimSpace(line[end+2:])
				continue
			}
			break
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "package" {
			return fields[1] == "main"
		}
		return false
	}
	return false
}

// isTaskGoInput is a Go file, or a cgo/asm file sitting next to one.
// A lone header under docs/ must not become `go build ./docs`.
func isTaskGoInput(projectPath, rel string) bool {
	if !isGoPackageInput(rel) {
		return false
	}
	if strings.EqualFold(path.Ext(rel), ".go") || projectPath == "" {
		return true
	}
	if _, err := os.Stat(projectPath); err != nil {
		return true
	}
	dir := path.Dir(rel)
	abs := projectPath
	if dir != "." && dir != "" {
		abs = filepath.Join(projectPath, filepath.FromSlash(dir))
	}
	matches, err := filepath.Glob(filepath.Join(abs, "*.go"))
	return err == nil && len(matches) > 0
}

// nearestGoModule walks from relDir up to the project root and returns the
// directory that contains go.mod. Missing trees are treated as the root
// module so a remote path that is not on this machine still scopes by file.
func nearestGoModule(projectPath, relDir string, cache map[string]string) string {
	key := relDir
	if key == "" {
		key = "."
	}
	if cache != nil {
		if mod, ok := cache[key]; ok {
			return mod
		}
	}
	mod := findGoModule(projectPath, relDir)
	if cache != nil {
		dir := key
		for {
			cache[dir] = mod
			if dir == "." || dir == mod {
				break
			}
			parent := path.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return mod
}

func findGoModule(projectPath, relDir string) string {
	if projectPath == "" {
		return "."
	}
	if _, err := os.Stat(projectPath); err != nil {
		return "."
	}
	dir := relDir
	if dir == "" {
		dir = "."
	}
	for {
		candidate := projectPath
		if dir != "." {
			candidate = filepath.Join(projectPath, filepath.FromSlash(dir))
		}
		info, err := os.Stat(filepath.Join(candidate, "go.mod"))
		if err == nil && !info.IsDir() {
			if dir == "." {
				return "."
			}
			return filepath.ToSlash(dir)
		}
		if dir == "." {
			return "."
		}
		parent := path.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func taskRelativePath(projectPath, file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	// Task audits on Windows store both slash and backslash paths.
	file = strings.ReplaceAll(file, `\`, `/`)
	cleaned := filepath.Clean(file)
	if projectPath != "" && filepath.IsAbs(cleaned) {
		base := filepath.Clean(projectPath)
		rel, err := filepath.Rel(base, cleaned)
		slashRel := ""
		if err == nil {
			slashRel = filepath.ToSlash(rel)
		}
		if err != nil || slashRel == ".." || strings.HasPrefix(slashRel, "../") {
			folded := foldUnderProject(base, cleaned)
			if folded == "" {
				return ""
			}
			cleaned = folded
		} else {
			cleaned = rel
		}
	}
	slash := filepath.ToSlash(cleaned)
	slash = strings.TrimPrefix(slash, "./")
	if slash == "" || slash == "." || strings.HasPrefix(slash, "../") || strings.HasPrefix(slash, "/") {
		return ""
	}
	return slash
}

// foldUnderProject matches a Windows path whose drive letter or directory
// case differs from the project root. filepath.Rel treats that as outside.
func foldUnderProject(project, file string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	root := strings.TrimRight(filepath.ToSlash(filepath.Clean(project)), "/")
	abs := filepath.ToSlash(filepath.Clean(file))
	if len(abs) < len(root) || !strings.EqualFold(abs[:len(root)], root) {
		return ""
	}
	if len(abs) == len(root) {
		return "."
	}
	if abs[len(root)] != '/' {
		return ""
	}
	return abs[len(root)+1:]
}

var goVerifyDiagRE = regexp.MustCompile(`(?m)^(.*\.go):(\d+):(?:\d+:)?[ \t]+(.+)$`)

// historicalGoVerifyOnly reports whether every diagnostic in a failed Go
// verify is a pre-existing, file-local vet finding outside the files this
// task changed. A compiler error in a package the task edited is still
// related, even when it is reported on a sibling file.
func historicalGoVerifyOnly(output, projectPath string, taskFiles []string) bool {
	if goVerifyLogTruncated(output) {
		// The missing tail can be the diagnostic that belongs to this task.
		return false
	}
	output = normalizeGoVerifyOutput(output)
	if goVerifyHasUnscopedFailure(output) {
		return false
	}
	// Same classifier as the fix prompt. A sibling printf nit must not be
	// treated as the error to fix, and a compile error must not be dropped
	// because its wording is missing from the vet phrase list.
	vetPhase := verifyFailureIsVet(output)
	changedFiles := taskVerifyPaths(projectPath, taskFiles)
	found := false
	for _, line := range strings.Split(output, "\n") {
		isDiag, related := goDiagRelated(line, projectPath, changedFiles, vetPhase)
		if !isDiag {
			continue
		}
		found = true
		if related {
			return false
		}
	}
	return found
}

func verifyFailureIsVet(output string) bool {
	built := strings.LastIndex(output, verifyBuildOKMarker)
	if built < 0 {
		return false
	}
	return built > strings.LastIndex(output, verifyVetOKMarker)
}

func isCompilerDiagnostic(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "undefined:") ||
		strings.Contains(m, "declared and not used") ||
		strings.Contains(m, "imported and not used") ||
		strings.Contains(m, "cannot use ") ||
		strings.Contains(m, "cannot convert") ||
		strings.Contains(m, "cannot find") ||
		strings.Contains(m, "not enough arguments") ||
		strings.Contains(m, "too many arguments") ||
		strings.Contains(m, "missing return") ||
		strings.Contains(m, "syntax error") ||
		strings.Contains(m, "invalid operation") ||
		strings.Contains(m, "assignment mismatch") ||
		strings.Contains(m, "multiple-value") ||
		strings.Contains(m, "redeclared") ||
		strings.Contains(m, "does not implement") ||
		strings.Contains(m, "cannot infer") ||
		strings.Contains(m, "unknown field") ||
		strings.Contains(m, "has no field or method") ||
		strings.Contains(m, "not a type") ||
		strings.Contains(m, "cannot refer to unexported") ||
		strings.Contains(m, "import cycle") ||
		strings.Contains(m, "could not import") ||
		strings.Contains(m, "no new variables") ||
		strings.Contains(m, "overflows") ||
		strings.Contains(m, "not enough return values") ||
		strings.Contains(m, "too many return values") ||
		strings.Contains(m, "cannot assign") ||
		strings.Contains(m, "cannot range over") ||
		strings.Contains(m, "cannot index") ||
		strings.Contains(m, "cannot slice") ||
		strings.Contains(m, "cannot call") ||
		strings.Contains(m, "is not a package") ||
		strings.HasPrefix(m, "expected ")
}

// goDiagRelated reports whether rawLine is a Go file diagnostic and whether
// this task has to answer it. vetPhase means go build already succeeded, so
// an unprefixed file:line line is an analyzer nit unless it is a compile error.
func goDiagRelated(rawLine, projectPath string, changed []string, vetPhase bool) (isDiag, related bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawLine), "[stderr]"))
	if raw == "" {
		return false, false
	}
	toolErr := lineIsVetToolError(raw)
	match := goVerifyDiagRE.FindStringSubmatch(stripGoVetPrefix(raw))
	if match == nil {
		return false, false
	}
	if toolErr || isCompilerDiagnostic(match[3]) {
		return true, true
	}
	rel := taskRelativePath(projectPath, match[1])
	if rel == "" || taskPathCovers(changed, rel) {
		return true, true
	}
	if vetPhase {
		return true, false
	}
	return true, !isFileLocalVetDiagnostic(match[3])
}

func goVerifyLogTruncated(output string) bool {
	return strings.Contains(output, "... (stderr truncated)") ||
		strings.Contains(output, "... (output truncated)")
}

// promoteRelatedGoDiagnostics moves diagnostics this task has to answer
// ahead of file-local vet nits in untouched files. The fix prompt keeps the
// head of the log, and that head used to be only the historical nits.
func promoteRelatedGoDiagnostics(output, projectPath string, taskFiles []string) string {
	changed := taskVerifyPaths(projectPath, taskFiles)
	vetPhase := verifyFailureIsVet(output)
	lines := strings.Split(output, "\n")
	related := make([]string, 0, len(lines))
	rest := make([]string, 0, len(lines))
	moved := false
	for _, line := range lines {
		isDiag, keep := goDiagRelated(line, projectPath, changed, vetPhase)
		if isDiag && keep {
			related = append(related, line)
			moved = true
			continue
		}
		rest = append(rest, line)
	}
	if !moved {
		return output
	}
	return strings.Join(append(related, rest...), "\n")
}

func lineIsVetToolError(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "vet.exe:") || strings.HasPrefix(lower, "vet:")
}

func stripGoVetPrefix(line string) string {
	lower := strings.ToLower(line)
	for _, prefix := range []string{"vet.exe:", "vet:"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return line
}

func normalizeGoVerifyOutput(output string) string {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "[stderr]"))
		if line == "" || strings.HasPrefix(strings.ToLower(line), "command exited with code") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// taskVerifyPaths lists files this task changed, plus the module-relative
// form go -C prints. Matching is exact so an unrelated other/cmd/srv does
// not count as datasrv/cmd/srv.
func taskVerifyPaths(projectPath string, taskFiles []string) []string {
	cache := map[string]string{}
	seen := map[string]bool{}
	var files []string
	add := func(value string) {
		if value == "" {
			return
		}
		key := value
		if runtime.GOOS == "windows" {
			key = strings.ToLower(value)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		files = append(files, value)
	}
	for _, file := range taskFiles {
		rel := taskRelativePath(projectPath, file)
		if !isTaskGoInput(projectPath, rel) {
			continue
		}
		add(rel)
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		mod := nearestGoModule(projectPath, dir, cache)
		if mod == "" || mod == "." || !strings.HasPrefix(rel, mod+"/") {
			continue
		}
		add(strings.TrimPrefix(rel, mod+"/"))
	}
	return files
}

// taskPathCovers reports whether diag is one of the recorded task paths.
// Windows path case is not significant.
func taskPathCovers(bases []string, diag string) bool {
	if diag == "" {
		return false
	}
	for _, base := range bases {
		if taskPathEqual(base, diag) {
			return true
		}
	}
	return false
}

func taskPathEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// goVerifyHasUnscopedFailure is a go toolchain failure that is not a
// file:line diagnostic. Those lines have no file to compare with the task,
// so they must not be washed out by a nearby historical vet nit.
func goVerifyHasUnscopedFailure(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		// The vet tool logs typecheck failures with its own name in front
		// (vet.exe: on Windows, vet: elsewhere). Analyzer nits are not
		// prefixed. A prefixed line is the package failing to typecheck,
		// including an untouched test file go build did not compile.
		if lineIsVetToolError(line) {
			return true
		}
		if goVerifyDiagRE.MatchString(line) {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "go:") ||
			strings.Contains(lower, "cannot find main module") ||
			strings.Contains(lower, "no go files") ||
			strings.Contains(lower, "build constraints exclude all") {
			return true
		}
	}
	return false
}

func isFileLocalVetDiagnostic(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "self-assignment of") ||
		strings.Contains(m, "should have signature") ||
		strings.Contains(m, "unreachable code") ||
		(strings.Contains(m, "result of ") && strings.Contains(m, "not used")) ||
		strings.Contains(m, "copies lock value") ||
		strings.Contains(m, "passes lock by value") ||
		strings.Contains(m, "misuse of unsafe") ||
		strings.Contains(m, "non-constant format") ||
		strings.Contains(m, "composite literal uses unkeyed") ||
		strings.Contains(m, "direct assignment to atomic") ||
		strings.Contains(m, "nil func value") ||
		strings.Contains(m, "possible nil pointer")
}

// acceptTaskVerifyResult turns a failed command into a pass when every
// diagnostic is historical and outside this task. The findings stay in the
// output so the step record still shows what was ignored.
func acceptTaskVerifyResult(ok bool, cmd, output, projectPath string, taskFiles []string, taskScoped bool) (bool, string, string, bool) {
	if taskScoped && !ok && historicalGoVerifyOnly(output, projectPath, taskFiles) {
		note := "ignored unrelated historical findings"
		if strings.TrimSpace(output) != "" {
			note += "\n" + output
		}
		return true, cmd, note, false
	}
	return ok, cmd, output, false
}

func pythonVerifyCommand(isWindows bool) string {
	if isWindows {
		return "python -m py_compile *.py"
	}
	return "python -m pytest --co -q 2>/dev/null || python -m py_compile *.py 2>/dev/null || true"
}

func makeVerifyCommand(isWindows bool) string {
	if isWindows {
		return "" // Makefile on Windows is unreliable
	}
	return "make -n check 2>/dev/null || make -n test 2>/dev/null || make -n build 2>/dev/null || true"
}

func detectNodeVerifyCommand(projectPath string) string {
	pkgPath := filepath.Join(projectPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	scripts, ok := pkg["scripts"].(map[string]interface{})
	if !ok {
		return ""
	}

	// Priority: typecheck > build > test (test may be slow)
	if _, ok := scripts["typecheck"]; ok {
		return "npm run typecheck"
	}
	if _, ok := scripts["build"]; ok {
		return "npm run build"
	}
	if _, ok := scripts["lint"]; ok {
		return "npm run lint"
	}
	return ""
}

// hostBuildTools are absolute executables the host can already run.
// Empty fields are tools this machine does not have.
type hostBuildTools struct {
	cmake   string
	ninja   string
	msbuild string
	make    string
}

var lookupHostBuildTools = currentHostBuildTools

func detectCMakeVerifyCommand(projectPath string) string {
	return selectCMakeVerifyCommand(projectPath, lookupHostBuildTools())
}

// ensureProjectVerifyCommand is the command the host is about to execute.
// Detection stays free of downloads: commit text and prompts also call
// detectProjectVerifyCommand. Installation happens only here, and only when
// a CMake build directory exists and no local builder can drive it.
func ensureProjectVerifyCommand(ctx context.Context, projectPath string) string {
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	if cmd := detectProjectVerifyCommand(projectPath); cmd != "" {
		return cmd
	}
	if cmakeVerifySkippedReason(projectPath) == "" {
		return ""
	}
	exe := ensurePrivateCMakeInstalled(ctx)
	if exe == "" {
		return ""
	}
	return selectCMakeVerifyCommand(projectPath, hostBuildTools{cmake: exe})
}

func cmakeVerifySkippedReason(projectPath string) string {
	if _, err := os.Stat(filepath.Join(projectPath, "CMakeLists.txt")); err != nil {
		return ""
	}
	buildDir := filepath.Join(projectPath, "build")
	info, err := os.Stat(buildDir)
	if err != nil || !info.IsDir() || !cmakeBuildDirConfigured(buildDir) {
		return ""
	}
	return "CMake build tree present, no cmake/ninja/msbuild available"
}

func cmakeBuildDirConfigured(buildDir string) bool {
	for _, name := range []string{"CMakeCache.txt", "build.ninja", "Makefile"} {
		info, err := os.Stat(filepath.Join(buildDir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return firstBuildSolution(buildDir) != ""
}

func currentHostBuildTools() hostBuildTools {
	if runtime.GOOS != "windows" {
		return hostBuildTools{
			cmake: existingExecutable(lookPathTool("cmake")),
			ninja: existingExecutable(lookPathTool("ninja")),
			make:  existingExecutable(lookPathTool("make")),
		}
	}
	tools := hostBuildTools{msbuild: detectWindowsMSBuild()}
	if dir := detectWindowsCMakeBin(); dir != "" {
		tools.cmake = filepath.Join(dir, "cmake.exe")
	}
	if dir := detectWindowsNinjaBin(); dir != "" {
		tools.ninja = filepath.Join(dir, "ninja.exe")
	}
	if dir := detectWindowsMinGWBin(); dir != "" {
		for _, name := range []string{"mingw32-make.exe", "make.exe"} {
			candidate := filepath.Join(dir, name)
			if existingExecutable(candidate) != "" {
				tools.make = candidate
				break
			}
		}
	}
	return tools
}

func lookPathTool(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// selectCMakeVerifyCommand picks a build command that does not depend on the
// GUI process PATH. A bare "cmake" is never returned: on other machines that
// becomes a PowerShell CommandNotFound and fails a step the agent already built.
func selectCMakeVerifyCommand(projectPath string, tools hostBuildTools) string {
	buildDir := filepath.Join(projectPath, "build")
	info, err := os.Stat(buildDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	cmakePath, makeProgram := readCMakeCacheTools(buildDir)
	if cmakePath != "" {
		return quoteVerifyExecutable(cmakePath) + " --build build"
	}
	if cmake := existingExecutable(tools.cmake); cmake != "" {
		return quoteVerifyExecutable(cmake) + " --build build"
	}
	if cmd := commandForCacheBuildTool(buildDir, makeProgram); cmd != "" {
		return cmd
	}
	if existingExecutable(filepath.Join(buildDir, "build.ninja")) != "" {
		if ninja := existingExecutable(tools.ninja); ninja != "" {
			return quoteVerifyExecutable(ninja) + " -C build"
		}
	}
	if sln := firstBuildSolution(buildDir); sln != "" {
		if msbuild := existingExecutable(tools.msbuild); msbuild != "" {
			return quoteVerifyExecutable(msbuild) + " " + shellQuoteVerifyToken(sln)
		}
	}
	if existingExecutable(filepath.Join(buildDir, "Makefile")) != "" {
		if makeExe := existingExecutable(tools.make); makeExe != "" {
			return quoteVerifyExecutable(makeExe) + " -C build"
		}
	}
	return ""
}

func commandForCacheBuildTool(buildDir, tool string) string {
	tool = existingExecutable(tool)
	if tool == "" {
		return ""
	}
	switch cmakeBuildToolKind(tool) {
	case "ninja":
		return quoteVerifyExecutable(tool) + " -C build"
	case "make":
		return quoteVerifyExecutable(tool) + " -C build"
	case "msbuild":
		if sln := firstBuildSolution(buildDir); sln != "" {
			return quoteVerifyExecutable(tool) + " " + shellQuoteVerifyToken(sln)
		}
	}
	return ""
}

func cmakeBuildToolKind(tool string) string {
	switch strings.ToLower(filepath.Base(tool)) {
	case "ninja", "ninja.exe":
		return "ninja"
	case "make", "make.exe", "gmake", "gmake.exe", "mingw32-make", "mingw32-make.exe":
		return "make"
	case "msbuild", "msbuild.exe":
		return "msbuild"
	default:
		return ""
	}
}

func firstBuildSolution(buildDir string) string {
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".sln") {
			return filepath.Join(buildDir, entry.Name())
		}
	}
	return ""
}

func existingExecutable(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return ""
	}
	return path
}

func readCMakeCacheTools(buildDir string) (cmakePath, makeProgram string) {
	data, err := os.ReadFile(filepath.Join(buildDir, "CMakeCache.txt"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(key, ":")
		if name != "CMAKE_COMMAND" && name != "CMAKE_MAKE_PROGRAM" {
			continue
		}
		value = existingExecutable(filepath.Clean(filepath.FromSlash(strings.Trim(strings.TrimSpace(value), `"'`))))
		if value == "" {
			continue
		}
		switch name {
		case "CMAKE_COMMAND":
			if cmakePath == "" {
				cmakePath = value
			}
		case "CMAKE_MAKE_PROGRAM":
			if makeProgram == "" {
				makeProgram = value
			}
		}
		if cmakePath != "" && makeProgram != "" {
			break
		}
	}
	return cmakePath, makeProgram
}

func shellQuoteVerifyToken(token string) string {
	if !strings.ContainsAny(token, " \t\"") {
		return token
	}
	return `"` + strings.ReplaceAll(token, `"`, "") + `"`
}

// quoteVerifyExecutable makes a path runnable by Windows PowerShell 5.1.
// A quoted program path is an expression unless the call operator is present,
// so "C:\Program Files\CMake\bin\cmake.exe" --build would not start cmake.
func quoteVerifyExecutable(exe string) string {
	// Keep the command itself free of a leading &. The shell tokenizer treats
	// & as a command boundary, which would hide cmake from every detector.
	// windowsCodingBashInvocation adds the PowerShell call operator at launch.
	return shellQuoteVerifyToken(exe)
}

// runPostLoopVerification executes one host-selected verification command and
// records it in the same audit ledger used by normal bash calls. Recovery uses
// it to obtain direct exit-status evidence without entering a mutating fix loop.
func (s *CodingSubAgent) runPostLoopVerification(
	cb *codingSubAgentCallbacks,
	result *agent.LoopResult,
	verifyCmd string,
	workingDir string,
	traj *TrajectoryRecorder,
) bool {
	if cb == nil || result == nil || strings.TrimSpace(verifyCmd) == "" || cb.ShouldStop() {
		return false
	}
	if strings.TrimSpace(workingDir) == "" {
		workingDir = s.projectPath
	}
	log.Printf("[coding-subagent-verify] running recovered verifier %q", verifyCmd)
	if s.onProgress != nil {
		s.onProgress("验证中: " + verifyCmd)
	}
	verifyArgs := fmt.Sprintf(`{"command":%q,"working_dir":%q,"timeout":60}`, verifyCmd, workingDir)
	verifyResult := cb.ExecuteTool("bash", verifyArgs)
	ledgerKnown, ledgerFailed := commandLedgerVerificationOutcome(cb, verifyCmd)
	verifyFailed := postLoopVerificationFailed(verifyResult, ledgerKnown, ledgerFailed)
	outcome := "succeeded"
	if verifyFailed {
		outcome = "failed"
	}
	recordSubAgentPostLoopVerify(traj, 1, verifyCmd, verifyArgs, verifyResult, outcome)
	result.ToolCalls++
	if verifyFailed {
		log.Printf("[coding-subagent-verify] recovered verification FAILED")
		return false
	}
	if s.onProgress != nil {
		s.onProgress("验证通过")
	}
	return true
}

// runPostLoopVerifyFixCycle runs automatic verification after the main coding
// loop completes. If verification fails, a fresh model loop repairs the code
// (up to maxRounds times). That loop does not reuse the main conversation.
// Counts, text, and errors are merged onto result. The returned entries are
// those model loops, in order. The host verification command is not one of
// them.
func (s *CodingSubAgent) runPostLoopVerifyFixCycle(
	cb *codingSubAgentCallbacks,
	result *agent.LoopResult,
	verifyCmd string,
	traj *TrajectoryRecorder,
	taskFiles []string,
) []agent.ConversationEntry {
	var modelLoops []agent.ConversationEntry
	for round := 0; round < subAgentMaxVerifyFixRounds; round++ {
		if cb.ShouldStop() {
			if result != nil && strings.TrimSpace(result.Error) == "" {
				result.Error = "cancelled"
			}
			return modelLoops
		}

		log.Printf("[coding-subagent-verify] round %d/%d: running %q", round+1, subAgentMaxVerifyFixRounds, verifyCmd)
		if s.onProgress != nil {
			s.onProgress(fmt.Sprintf("验证中 (round %d/%d): %s", round+1, subAgentMaxVerifyFixRounds, verifyCmd))
		}

		// Run verification command
		verifyArgs := fmt.Sprintf(`{"command":%q,"working_dir":%q,"timeout":60,"stderr_limit":%d}`, verifyCmd, s.projectPath, verifyGateStderrBytes)
		verifyResult := cb.ExecuteTool("bash", verifyArgs)
		ledgerKnown, ledgerFailed := commandLedgerVerificationOutcome(cb, verifyCmd)
		verifyFailed := postLoopVerificationFailed(verifyResult, ledgerKnown, ledgerFailed)
		if verifyFailed && historicalGoVerifyOnly(verifyResult, s.projectPath, taskFiles) {
			log.Printf("[coding-subagent-verify] round %d: ignored unrelated historical findings", round+1)
			verifyFailed = false
			cb.acceptHistoricalVerify(verifyCmd)
		}
		outcome := "succeeded"
		if verifyFailed {
			outcome = "failed"
		}
		recordSubAgentPostLoopVerify(traj, round+1, verifyCmd, verifyArgs, verifyResult, outcome)
		// Count automatic verify bash as a tool call for session accounting.
		result.ToolCalls++

		if !verifyFailed {
			log.Printf("[coding-subagent-verify] round %d: verification PASSED", round+1)
			if s.onProgress != nil {
				s.onProgress("验证通过")
			}
			return modelLoops
		}

		log.Printf("[coding-subagent-verify] round %d: verification FAILED, starting fix loop", round+1)

		// Verification failed — run a new agent loop with the fix prompt.
		// We don't reuse the conversation directly (type mismatch). Instead,
		// start a fresh mini-loop with a focused fix prompt that includes
		// the verification error. The SubAgent's system prompt provides
		// enough project context for the fix.
		truncatedOutput := truncateForSubAgentVerify(promoteRelatedGoDiagnostics(verifyResult, s.projectPath, taskFiles), subAgentVerifyOutputMaxTokens)
		fixPrompt := fmt.Sprintf(
			"验证命令 `%s` 执行失败。错误输出：\n```\n%s\n```\n\n请分析错误并修复代码使验证通过。只做最小必要修改。修复后不要重新运行验证命令（系统会自动运行）。",
			verifyCmd, truncatedOutput)

		// Run fix loop as a fresh RunLoop (shares system prompt + tools via cb).
		// History stays nil: this loop does not replay the main conversation.
		fixResult := agentruntime.RunAgentTurn(cb, fixPrompt, nil, s.httpClient)
		// Append fix-loop turns into the same trajectory session.
		appendSubAgentLoopResult(traj, fixResult, false)
		modelLoops = codingAgentResumeAppendLoop(modelLoops, fixResult.HistoryDelta)

		// Merge results
		result.Iterations += fixResult.Iterations
		result.ToolCalls += fixResult.ToolCalls
		result.Usage.Add(fixResult.Usage)
		if fixResult.Text != "" {
			result.Text = fixResult.Text
		}
		if fixResult.Error != "" {
			result.Error = fixResult.Error
		}
		if fixResult.HardExit {
			result.HardExit = true
		}
	}

	// Exhausted all fix rounds — surface failure so trajectory seals as error
	// and task status does not look like a clean success.
	log.Printf("[coding-subagent-verify] exhausted %d verify-fix rounds", subAgentMaxVerifyFixRounds)
	if s.onProgress != nil {
		s.onProgress(fmt.Sprintf("验证未通过（已尝试 %d 次修复）", subAgentMaxVerifyFixRounds))
	}
	if result != nil && strings.TrimSpace(result.Error) == "" {
		result.Error = fmt.Sprintf("post-loop verification failed after %d rounds (%s)", subAgentMaxVerifyFixRounds, verifyCmd)
	}
	return modelLoops
}

// isSubAgentVerificationFailure checks if bash output indicates a verification failure.
// Primary signal: exit code (SubAgent's bash tool includes exit status in output).
// Fallback: keyword detection for tools that don't report exit code.
func isSubAgentVerificationFailure(output string) bool {
	output = strings.TrimSpace(output)
	if output == "" || strings.HasPrefix(output, "(command completed with no output)") {
		return false // No output usually means success for build commands
	}

	// Primary: check for explicit exit code reporting from bash tool.
	// SubAgent's bash returns "exit status N" or "Exit Code: N" on failure.
	lower := strings.ToLower(output)
	if strings.Contains(lower, "exit status") || strings.Contains(lower, "exit code") {
		// "exit status 0" / "exit code: 0" = success
		if strings.Contains(lower, "exit status 0") || strings.Contains(lower, "exit code: 0") {
			return false
		}
		// Any non-zero exit code = failure
		return true
	}

	// Fallback: structural failure indicators (more specific than just "error")
	failureIndicators := []string{
		"fatal error",
		"compilation failed",
		"build failed",
		"test failed",
		"cannot find module",
		"cannot find package",
		"undefined:",
		"unresolved reference",
		"panic:",
		"segfault",
	}
	for _, indicator := range failureIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// postLoopVerificationFailed combines the tool's textual result with the
// command ledger. When the verifier appears in the ledger, its exit outcome
// is authoritative: successful tests may intentionally print phrases such as
// "exit status" or "build failed" as fixture data. Text remains a fallback
// only when no matching command result was recorded.
func postLoopVerificationFailed(output string, ledgerKnown, ledgerFailed bool) bool {
	if ledgerKnown {
		return ledgerFailed
	}
	return isSubAgentVerificationFailure(output)
}

// commandLedgerVerificationOutcome returns the final matching verifier result.
// Commands are searched newest-first so a successful retry supersedes an
// earlier failed attempt.
func commandLedgerVerificationOutcome(cb *codingSubAgentCallbacks, verifyCmd string) (known, failed bool) {
	if cb == nil {
		return false, false
	}
	want := normalizedSubAgentVerificationCommand(verifyCmd)
	if want == "" {
		return false, false
	}
	commands := cb.getCommandsRun()
	for i := len(commands) - 1; i >= 0; i-- {
		if normalizedSubAgentVerificationCommand(commands[i].Command) == want {
			return true, !commands[i].Succeeded
		}
	}
	return false, false
}

func normalizedSubAgentVerificationCommand(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}

// truncateForSubAgentVerify truncates verification output to fit in a fix prompt
// while preserving the most useful error information.
func truncateForSubAgentVerify(output string, maxTokens int) string {
	// Use the bash truncation logic which prioritizes error lines
	tokenEst := (len(output)*10 + 24) / 25
	if tokenEst <= maxTokens {
		return output
	}
	return truncateSubAgentBashResult(output)
}

// hasSubAgentSelfVerified checks whether the loop already produced a fresh,
// auditable verification result. It deliberately shares the quality gate's
// command classifier instead of keeping a smaller text-pattern list: otherwise
// the post-loop verifier can skip recovery for a command that the final audit
// will reject.
func hasSubAgentSelfVerified(cb *codingSubAgentCallbacks) bool {
	if cb == nil {
		return false
	}
	commands := cb.getCommandsRun()
	if len(commands) == 0 {
		return false
	}
	for i := len(commands) - 1; i >= 0; i-- {
		command := commands[i]
		if isUnsafeSubAgentVerificationCommand(command.Command) {
			return false
		}
		if isSubAgentVerificationCommand(command.Command) {
			return command.Succeeded
		}
	}
	return false
}
