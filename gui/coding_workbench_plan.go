package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	codingWorkbenchPlanMaxTasks = 6
	codingWorkbenchPlanMinTasks = 2
)

// codingRequestKind is the user-facing level of work expected from a coding
// environment.  It deliberately describes the request, not whether the
// workspace happens to be local or reached through SSH: both environments must
// make the same decision before they start spending time on a workflow.
type codingRequestKind string

const (
	codingRequestInquiry        codingRequestKind = "inquiry"
	codingRequestOperational    codingRequestKind = "operational"
	codingRequestImplementation codingRequestKind = "implementation"
)

func isValidCodingRequestKind(kind codingRequestKind) bool {
	switch kind {
	case codingRequestInquiry, codingRequestOperational, codingRequestImplementation:
		return true
	default:
		return false
	}
}

// codingRequestDecision is produced by a compact model classification before a
// coding turn starts.  It is intentionally intent-based: we do not infer a
// destructive execution mode from a phrase such as "build" or "test".
type codingRequestDecision struct {
	Kind      codingRequestKind `json:"kind"`
	NeedsPlan bool              `json:"needs_plan"`
}

// approvedCodingPlanDecision is intentionally not model-classified: a plan can
// only reach approval after an implementation turn established a concrete set
// of write-capable steps. Reclassifying its original text at execution time
// could incorrectly narrow the tool surface for work the user just approved.
func approvedCodingPlanDecision() codingRequestDecision {
	return codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true}
}

// normalizeCodingRequestDecision validates a propagated decision and enforces
// the invariant that only implementation work may have a planning boundary.
func normalizeCodingRequestDecision(decision codingRequestDecision) (codingRequestDecision, bool) {
	switch decision.Kind {
	case codingRequestInquiry, codingRequestOperational:
		decision.NeedsPlan = false
		return decision, true
	case codingRequestImplementation:
		return decision, true
	default:
		return codingRequestDecision{}, false
	}
}

const codingRequestClassifierSystemPrompt = `Classify the user's coding-workbench request by intent. Return JSON only.

Schema: {"kind":"inquiry|operational|implementation","needs_plan":true|false}

inquiry: the user wants explanation, inspection, location, or an answer. It is read-only: never run commands or change files.
operational: the user wants an existing project run, built, tested, or demonstrated, with no source change requested. It may run commands but must not change source files.
implementation: the user asks to modify, create, fix, refactor, delete, or clear workspace files. Clearing or emptying the current project directory is implementation, never operational.
needs_plan is true for an implementation request that is more than one local edit: several files, a feature plus tests, UI plus logic, a richer rewrite, or more than one distinct deliverable. One-line typo, rename, or comment-only fixes stay needs_plan=false.
Questions about how a command works are inquiry, even when they mention build, test, run, or compile. A question asking you to actually run something is operational.
Do not infer intent from isolated words; judge the complete request.`

func (h *IMMessageHandler) resolveCodingRequestDecision(userText string) codingRequestDecision {
	// An explicit workspace wipe is file mutation. Do not let the lightweight
	// classifier call it operational and send the agent to run an existing binary.
	if codingRequestLooksExplicitWorkspaceClear(normalizeCodingWorkspaceClearText(userText)) {
		return codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: false}
	}
	fallback := codingRequestDecision{
		Kind:      codingRequestImplementation,
		NeedsPlan: codingRequestNeedsPlanFallback(userText) || codingRequestLooksModeratelyComplex(userText),
	}
	if h == nil || h.client == nil || strings.TrimSpace(userText) == "" {
		return fallback
	}
	// Rewrite / multi-step asks already have a local plan floor. Waiting on the
	// classifier here only delays the first user-visible restatement.
	if codingRequestLooksModeratelyComplex(userText) {
		return fallback
	}
	cfg := h.getCodingLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getCodingLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fallback
	}
	if decision, ok := parseCodingRequestDecision(h.callLightweightLLMOnce(cfg, codingRequestClassifierSystemPrompt, userText, 5)); ok {
		return applyCodingRequestPlanFloor(decision, userText)
	}
	// A missing or malformed classifier answer must never grant a looser mode.
	// Defaulting to implementation preserves normal review/safety boundaries.
	return fallback
}

func applyCodingRequestPlanFloor(decision codingRequestDecision, userText string) codingRequestDecision {
	if decision.Kind == codingRequestImplementation && codingRequestLooksModeratelyComplex(userText) {
		decision.NeedsPlan = true
	}
	return decision
}

func codingRequestLooksModeratelyComplex(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || codingRequestLooksExplicitWorkspaceClear(userText) {
		return false
	}
	if numberedStepCount(userText) >= codingWorkbenchPlanMinTasks {
		return true
	}
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' {
			return -1
		}
		return r
	}, text)
	for _, marker := range []string{
		"然后", "並且", "并且", "同时", "同時", "以及", "再加上",
		"加测试", "加測試", "写测试", "寫測試", "andtest", "withtest", "unittests",
		"豪华", "豪華", "完整功能", "端到端", "end-to-end", "endtoend",
		"多文件", "多个文件", "severalfiles", "implementand", "实现并", "實現並",
		"图形界面", "图形版", "圖形界面", "界面版", "重写", "重寫", "rewrite",
		"移植到", "portto", "win32", "gdi",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	if strings.Contains(text, "and then") || strings.Contains(text, "plus test") || strings.Contains(text, "with tests") {
		return true
	}
	return false
}

func parseCodingRequestDecision(raw string) (codingRequestDecision, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return codingRequestDecision{}, false
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var decision codingRequestDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return codingRequestDecision{}, false
	}
	return normalizeCodingRequestDecision(decision)
}

// codingRequestNeedsPlanFallback is intentionally structural. It is used only
// when the lightweight classifier is unavailable; it never promotes a request
// to a more permissive read-only or operational mode based on keywords.
//
// It deliberately does not infer plan complexity from the message's wording.
// Planning is a potentially surprising confirmation boundary, so under a
// classifier outage only an explicit multi-step structure warrants one.
func codingRequestNeedsPlanFallback(userText string) bool {
	return numberedStepCount(strings.TrimSpace(userText)) >= codingWorkbenchPlanMinTasks
}

// isCodingInquiryTool / filterCodingInquiryTools keep the implementation
// agents from silently turning a question into a mutation.  The allow-list is
// intentionally small but includes CodeGraph-aware navigation and the normal
// read/search primitives.  It is used for both local and remote workbenches.
func isCodingInquiryTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "list_directory", "glob", "read_file", "search_files", "search_file", "bash",
		"ssh_read_file", "ssh_list_dir", "ssh_bash", "ssh_check_task",
		codeNavigationToolName, "coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return true
	default:
		return false
	}
}

func filterCodingInquiryTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if isCodingInquiryTool(name) {
			out = append(out, tool)
		}
	}
	return out
}

// isCodingOperationalTool is the local counterpart of the remote operational
// allow-list. A run/build/demo turn may inspect the existing project and run a
// command, but it must not silently grow into an implementation or planning
// workflow merely because it is executed locally.
func isCodingOperationalTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "glob", "ripgrep", "read_file", "list_directory", "bash", codeNavigationToolName,
		"coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return true
	default:
		return false
	}
}

func filterCodingOperationalTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if isCodingOperationalTool(name) {
			out = append(out, tool)
		}
	}
	return out
}

func isRemoteCodingInquiryTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_list_dir", "ssh_bash", "ssh_check_task", codeNavigationToolName,
		"coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return true
	default:
		return false
	}
}

// rejectCodingInquiryShellCommand provides the second half of the read-only
// inquiry boundary.  Keeping bash/ssh_bash available is useful for CodeGraph,
// git history, and targeted searches, but the tool allow-list alone cannot
// make an arbitrary shell command safe.  This deliberately permits a compact
// inspection vocabulary and rejects wrappers, builds, tests, package managers,
// redirects, and every command that could mutate the workspace.
func rejectCodingInquiryShellCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "a repository inquiry shell command must not be empty"
	}
	if codingInquiryShellCommandHasOutputRedirect(command) {
		return "shell output redirection is unavailable for a read-only repository inquiry"
	}
	if strings.Contains(command, "$(") || strings.Contains(command, "${") || strings.Contains(command, "`") || strings.Contains(command, "<(") {
		return "shell expansion is unavailable for a read-only repository inquiry"
	}
	_, segments := normalizeShellCommandSegments(command)
	if len(segments) == 0 {
		return "the shell command is unavailable for a read-only repository inquiry"
	}
	for _, segment := range segments {
		if !codingInquiryShellSegmentAllowed(segment) {
			return fmt.Sprintf("a repository inquiry runs only read-only inspection commands without approval: %s", command)
		}
	}
	return ""
}

func codingInquiryShellCommandHasOutputRedirect(command string) bool {
	for _, raw := range shellCommandFields(command) {
		token := strings.TrimSpace(normalizeShellCommandToken(raw))
		if token == "" || token == "2>&1" || token == "1>&2" || token == "2>&2" {
			continue
		}
		if strings.Contains(token, ">") {
			return true
		}
	}
	return false
}

func codingInquiryShellSegmentAllowed(segment []string) bool {
	segment = stripVerificationCommandPrefixes(segment)
	if len(segment) == 0 {
		return false
	}
	cmd := commandNameBase(segment[0])
	args := segment[1:]
	switch cmd {
	case "ls", "dir", "pwd", "cat", "head", "tail", "rg", "grep", "egrep", "fgrep", "ag", "ack",
		"wc", "sort", "uniq", "cut", "tr", "stat", "file", "readlink", "realpath", "basename", "dirname",
		"which", "type", "uname", "id", "whoami", "hostname", "nproc", "getconf", "arch", "tree", "du":
		return true
	case "find":
		for _, arg := range args {
			switch strings.ToLower(strings.TrimSpace(normalizeShellCommandToken(arg))) {
			case "-delete", "-exec", "-execdir", "-ok", "-okdir":
				return false
			}
		}
		return true
	case "test", "[":
		return true
	case "command":
		return len(args) >= 2 && (args[0] == "-v" || args[0] == "-V")
	case "git":
		return codingInquiryGitSubcommandAllowed(args)
	case "codegraph", "codegraph.cmd":
		return len(args) > 0 && (strings.EqualFold(args[0], "explore") || strings.EqualFold(args[0], "node"))
	default:
		return false
	}
}

// gitReadOnlySubcommands only ever report repository state, whatever arguments
// they are given, so they need no further inspection.  Some of them (ls-remote)
// contact a remote, which is still a read: it cannot change local or remote
// refs.
var gitReadOnlySubcommands = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true, "ls-files": true,
	"rev-parse": true, "blame": true, "grep": true, "cat-file": true,
	"ls-remote": true, "ls-tree": true, "rev-list": true, "describe": true,
	"shortlog": true, "whatchanged": true, "diff-tree": true, "diff-index": true,
	"for-each-ref": true, "merge-base": true, "name-rev": true, "check-ignore": true,
	"count-objects": true, "verify-commit": true, "verify-tag": true, "annotate": true,
	"show-ref": true, "show-branch": true,
}

// codingInquiryGitSubcommand returns the git subcommand (the first non-flag
// token) together with the arguments that follow it.
func codingInquiryGitSubcommand(args []string) (string, []string) {
	for i, arg := range args {
		arg = strings.TrimSpace(normalizeShellCommandToken(arg))
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return strings.ToLower(arg), args[i+1:]
	}
	return "", nil
}

func codingInquiryGitSubcommandAllowed(args []string) bool {
	sub, rest := codingInquiryGitSubcommand(args)
	if sub == "" {
		return false
	}
	if gitArgsHaveUnsafeOption(args) || gitHasConfigInjectionOption(args) {
		return false
	}
	switch sub {
	case "ls-remote":
		// ls-remote is the one read-only subcommand here that takes a URL, and
		// a `helper::payload` URL (ext::, fd::) hands a command line to git's
		// transport layer, so it may only name an ordinary remote.
		for _, raw := range rest {
			if gitTransportHelperURL(strings.TrimSpace(normalizeShellCommandToken(raw))) {
				return false
			}
		}
		return true
	case "grep":
		// `git grep -O<pager>` opens the matching files with an arbitrary
		// command.  The gate sees lowercased text, so the unrelated `-o` of
		// other subcommands cannot be told apart from `-O` and this check is
		// kept scoped to grep, which has no `-o` of its own.
		for _, raw := range rest {
			arg := strings.TrimSpace(normalizeShellCommandToken(raw))
			if strings.HasPrefix(arg, "-o") || gitFlagName(arg) == "--open-files-in-pager" {
				return false
			}
		}
		return true
	}
	if gitReadOnlySubcommands[sub] {
		return true
	}
	// branch/tag/remote report state in their listing form but create, delete,
	// or rewrite refs as soon as they are given a target, so they are admitted
	// only while their arguments stay in listing form.  Anything we cannot
	// classify fails closed.
	switch sub {
	case "branch":
		return gitBranchListingOnly(rest)
	case "tag":
		return gitTagListingOnly(rest)
	case "remote":
		return gitRemoteListingOnly(rest)
	}
	return false
}

// gitFlagName strips an inline `=value` so `--sort=x` matches `--sort`.
func gitFlagName(arg string) string {
	return strings.ToLower(strings.SplitN(arg, "=", 2)[0])
}

// gitArgsHaveUnsafeOption rejects options that turn an otherwise read-only git
// subcommand into a file write or a command execution.  `--output` writes a
// path without ever using a shell redirect, so the redirect guard cannot see
// it; `--upload-pack`/`--exec`/`--receive-pack` name a program for git to run;
// and `--exec-path` moves the directory git resolves its helper programs from,
// including the `git-remote-*` transport helpers.
func gitArgsHaveUnsafeOption(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		switch gitFlagName(arg) {
		case "--output", "--upload-pack", "--exec", "--receive-pack", "--exec-path":
			return true
		}
	}
	return false
}

// gitHasConfigInjectionOption reports whether a git command carries a
// pre-subcommand `-c key=value` or `--config-env`.  Both let a caller point git
// at an arbitrary pager, alias, or transport helper, which is command
// execution.  The scan stops at the subcommand so that `git log -c`, where `-c`
// merely asks for a combined diff, stays available.
//
// The shell text reaching this gate has already been lowercased, so `-c` and
// git's unrelated `-C <path>` cannot be told apart here and both are refused.
func gitHasConfigInjectionOption(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return false
		}
		switch gitFlagName(arg) {
		case "-c", "--config-env":
			return true
		}
	}
	return false
}

// gitTransportHelperURL reports whether an argument is a `helper::payload`
// remote URL.  git runs `git-remote-<helper>` for these, and the built-in ext
// and fd helpers treat the payload as a command line.  Callers must only apply
// this to arguments that are remote URLs: an ordinary search pattern such as
// `std::vector` has the same shape.
func gitTransportHelperURL(arg string) bool {
	idx := strings.Index(arg, "::")
	if idx <= 0 {
		return false
	}
	for _, r := range arg[:idx] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '+', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// gitRefFilterFlags select which refs to list.  They take a commit or pattern
// operand and force git into list mode, so a name following one of them is a
// filter argument rather than a ref being created.
var gitRefFilterFlags = map[string]bool{
	"--contains": true, "--no-contains": true,
	"--merged": true, "--no-merged": true, "--points-at": true,
}

// gitRefFormatValueFlags shape the listing output and take a separate operand
// that is never a ref name.
var gitRefFormatValueFlags = map[string]bool{"--sort": true, "--format": true}

// gitBranchDisplayFlags are safe on their own but do not put git into list
// mode, so they never license a positional argument.
var gitBranchDisplayFlags = map[string]bool{
	"-l": true, "-a": true, "--all": true, "-r": true, "--remotes": true,
	"-v": true, "-vv": true, "--verbose": true, "--show-current": true,
	"-i": true, "--ignore-case": true, "--color": true, "--no-color": true,
	"--column": true, "--no-column": true,
}

var gitTagDisplayFlags = map[string]bool{
	"-i": true, "--ignore-case": true, "--color": true, "--no-color": true,
	"--column": true, "--no-column": true,
}

// gitShortFlagCluster reports whether a single-dash token is a cluster of the
// listed safe short flags, so that `-av` is read as `-a -v`.  Digits pass so
// that git tag's `-n<num>` keeps working; they are never flags themselves.
func gitShortFlagCluster(arg string, safe string) bool {
	if len(arg) < 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") || strings.Contains(arg, "=") {
		return false
	}
	for _, r := range arg[1:] {
		if r >= '0' && r <= '9' {
			continue
		}
		if !strings.ContainsRune(safe, r) {
			return false
		}
	}
	return true
}

// gitFlagConsumesNext reports whether the token after a value-taking flag is
// that flag's operand.  An inline `=value` carries its own operand, and these
// flags take an optional value, so a following flag is not consumed.
func gitFlagConsumesNext(arg string, rest []string) bool {
	if strings.Contains(arg, "=") || len(rest) == 0 {
		return false
	}
	next := strings.TrimSpace(normalizeShellCommandToken(rest[0]))
	return next != "" && !strings.HasPrefix(next, "-")
}

func gitBranchListingOnly(args []string) bool {
	listing := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(normalizeShellCommandToken(args[i]))
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			// In list mode this is a shell pattern; otherwise it names a branch
			// to create, rename, or reset.
			if !listing {
				return false
			}
			continue
		}
		flag := gitFlagName(arg)
		switch {
		case flag == "--list":
			listing = true
		case gitRefFilterFlags[flag]:
			listing = true
			if gitFlagConsumesNext(arg, args[i+1:]) {
				i++
			}
		case gitRefFormatValueFlags[flag]:
			if gitFlagConsumesNext(arg, args[i+1:]) {
				i++
			}
		case gitBranchDisplayFlags[flag], gitShortFlagCluster(arg, "arvil"):
			// `-l` deliberately stays here rather than entering list mode:
			// before git 2.19 it meant --create-reflog, so `git branch -l name`
			// could still create a branch.  None of the clustered short flags
			// enter list mode either, so `-av` cannot license a branch name.
		default:
			return false
		}
	}
	return true
}

func gitTagListingOnly(args []string) bool {
	listing := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(normalizeShellCommandToken(args[i]))
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			// In list mode this is a pattern; otherwise it names a tag to create.
			if !listing {
				return false
			}
			continue
		}
		flag := gitFlagName(arg)
		switch {
		case flag == "-l" || flag == "--list":
			listing = true
		case gitRefFilterFlags[flag]:
			listing = true
			if gitFlagConsumesNext(arg, args[i+1:]) {
				i++
			}
		case gitRefFormatValueFlags[flag]:
			if gitFlagConsumesNext(arg, args[i+1:]) {
				i++
			}
		case gitTagDisplayFlags[flag]:
		case gitShortFlagCluster(arg, "iln"):
			// -n[<num>] prints annotation lines and, like -l, only exists in
			// list mode, so either one licenses a trailing pattern.
			if strings.ContainsAny(arg, "ln") {
				listing = true
			}
		default:
			return false
		}
	}
	return true
}

func gitRemoteListingOnly(args []string) bool {
	mode := ""
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			switch gitFlagName(arg) {
			case "-v", "--verbose", "-n", "--all", "--push":
				continue
			default:
				return false
			}
		}
		if gitTransportHelperURL(arg) {
			return false
		}
		if mode == "" {
			switch strings.ToLower(arg) {
			case "show", "get-url":
				mode = strings.ToLower(arg)
				continue
			default:
				return false
			}
		}
		// A trailing remote name for `remote show` / `remote get-url`.
	}
	return true
}

func filterRemoteCodingInquiryTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if isRemoteCodingInquiryTool(name) {
			out = append(out, tool)
		}
	}
	return out
}

// filterRemoteCodingOperationalTools keeps run/build/demo turns focused on the
// existing remote project.  Unlike an inquiry, ssh_bash remains available to
// launch or build the artifact; unlike an implementation, no write, planning,
// or local-extension tool can expand the request into code changes.
func filterRemoteCodingOperationalTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "ssh_read_file", "ssh_list_dir", "ssh_bash", "ssh_check_task", codeNavigationToolName,
			"coding_knowledge_search", "knowledge_search", "knowledge_image_search":
			out = append(out, tool)
		}
	}
	return out
}

func isRemoteCodingOperationalTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ssh_read_file", "ssh_list_dir", "ssh_bash", "ssh_check_task", codeNavigationToolName,
		"coding_knowledge_search", "knowledge_search", "knowledge_image_search":
		return true
	default:
		return false
	}
}

// rejectCodingOperationalShellCommand protects the direct run/build path from
// quietly becoming a file-management or dependency-install workflow. Build
// tools and existing project scripts are intentionally allowed: their normal
// generated output is part of execution, while direct source/config writes
// remain implementation work and must be requested explicitly.
func rejectCodingOperationalShellCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "a run/build/demo shell command must not be empty"
	}
	if codingInquiryShellCommandHasOutputRedirect(command) {
		return "shell output redirection is unavailable for a run/build/demo request"
	}
	// A normal launch/build may use ordinary environment variables, but command
	// and process substitution hide a second command from the task classifier.
	// They belong to an explicit implementation request where the user can see
	// and approve the wider scope.
	if strings.Contains(command, "$(") || strings.Contains(command, "`") || strings.Contains(command, "<(") {
		return "shell command/process substitution is unavailable for a run/build/demo request"
	}
	// Reuse the shared parser for direct/quoted interpreter snippets. The normal
	// coding path may ask for approval for one of these; operational turns must
	// reject them outright so `python -c`, `node -e`, or `sh -c` cannot convert a
	// simple run request into a source-edit request.
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if hasDisallowedShellFileMutation(normalized) {
		return "source-changing shell commands are unavailable for a run/build/demo request"
	}
	_, segments := normalizeShellCommandSegments(command)
	if len(segments) == 0 {
		return "the shell command is unavailable for a run/build/demo request"
	}
	for _, segment := range segments {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		cmd := commandNameBase(segment[0])
		args := segment[1:]
		switch cmd {
		case "rm", "rmdir", "del", "erase", "mv", "move", "cp", "copy", "mkdir", "md", "touch", "tee", "dd", "chmod", "chown", "install":
			return fmt.Sprintf("%s is unavailable for a run/build/demo request; ask for an implementation change instead", cmd)
		case "git":
			if !codingInquiryGitSubcommandAllowed(args) {
				sub, _ := codingInquiryGitSubcommand(args)
				if sub == "" {
					return "a git command needs a read-only subcommand for a run/build/demo request"
				}
				return fmt.Sprintf("git %s is not a read-only git subcommand, so a run/build/demo request does not run it without approval", sub)
			}
		case "sed", "perl":
			for _, arg := range args {
				if strings.EqualFold(strings.TrimSpace(arg), "-i") || strings.HasPrefix(strings.TrimSpace(arg), "-i") {
					return fmt.Sprintf("%s -i is unavailable for a run/build/demo request", cmd)
				}
			}
		case "npm", "pnpm", "yarn", "bun":
			if len(args) > 0 {
				switch strings.ToLower(strings.TrimSpace(args[0])) {
				case "install", "i", "add", "remove", "uninstall", "update":
					return fmt.Sprintf("%s %s is unavailable for a run/build/demo request", cmd, args[0])
				}
			}
		case "pip", "pip3":
			if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "install") {
				return fmt.Sprintf("%s install is unavailable for a run/build/demo request", cmd)
			}
		}
		if isOperationalSourceMutationCommand(cmd, args) {
			return fmt.Sprintf("%s is a source-generation or auto-fix command and is unavailable for a run/build/demo request", cmd)
		}
	}
	return ""
}

// isOperationalSourceMutationCommand covers known commands whose primary
// effect is rewriting tracked source/config. It complements the generic shell
// write guard; ordinary build output and running an existing application stay
// permitted. We intentionally do not try to infer effects of arbitrary app
// scripts: a request to run a program may legitimately write runtime data.
func isOperationalSourceMutationCommand(cmd string, args []string) bool {
	firstArg := ""
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(normalizeShellCommandToken(arg)))
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		firstArg = arg
		break
	}
	switch strings.ToLower(cmd) {
	case "go":
		return firstArg == "generate" || (firstArg == "mod" && operationalArgsContain(args, "tidy", "edit", "init"))
	case "cargo":
		if firstArg == "fix" || firstArg == "fmt" {
			return true
		}
		for _, arg := range args {
			if strings.EqualFold(strings.TrimSpace(normalizeShellCommandToken(arg)), "--fix") {
				return true
			}
		}
	case "rustfmt", "gofmt", "dartfmt", "swiftformat":
		return true
	case "prettier":
		return operationalArgsContain(args, "--write", "-w")
	case "protoc", "buf", "codegen", "openapi-generator", "swagger-codegen":
		return true
	case "npx", "pnpx", "yarnx":
		return firstArg == "prisma" || firstArg == "openapi-generator" || firstArg == "swagger-codegen"
	case "python", "python3", "py":
		return firstArg == "manage.py" && operationalArgsContain(args, "makemigrations", "migrate")
	case "django-admin", "flask":
		return firstArg == "makemigrations" || firstArg == "migrate"
	case "alembic":
		return firstArg == "revision"
	case "rails":
		return firstArg == "generate" || firstArg == "g"
	case "dotnet":
		return firstArg == "ef" && operationalArgsContain(args, "migrations")
	}
	return false
}

func operationalArgsContain(args []string, values ...string) bool {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(normalizeShellCommandToken(arg)))
		for _, value := range values {
			if arg == value {
				return true
			}
		}
	}
	return false
}

// shouldEnableCodingTDD is retained for workflow compatibility. The request
// decision is already made before execution; red/green is therefore only used
// when a multi-step plan explicitly enables it elsewhere.
func shouldEnableCodingTDD(userText string, planned bool, taskCount int) bool {
	return false
}

// sentenceDotCount counts '.' that look like sentence terminators, not version
// numbers (e.g. "go 1.22") or single-char extensions.
func sentenceDotCount(text string) int {
	n := 0
	runes := []rune(text)
	for i, r := range runes {
		if r != '.' {
			continue
		}
		// Digit on either side — likely version / decimal.
		if i > 0 && i+1 < len(runes) {
			prev, next := runes[i-1], runes[i+1]
			if prev >= '0' && prev <= '9' && next >= '0' && next <= '9' {
				continue
			}
		}
		n++
	}
	return n
}

// Digit / T-numbered steps only. Bare markdown bullets (- item) are NOT counted — they appear in ordinary "fix: - a - b" lists and would false-trigger multi-step plans.
var numberedStepLineRe = regexp.MustCompile(`(?m)^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：])\s+\S+`)

func numberedStepCount(text string) int {
	return len(numberedStepLineRe.FindAllString(text, -1))
}

// resolveCodingWorkbenchTasks returns the TaskItems to run for a pure-coding turn.
// Complex requests may be expanded into an ordered multi-step plan via LLM.
// Simple requests stay a single task. Planner failures fall back to single-task.
func (h *IMMessageHandler) resolveCodingWorkbenchTasks(
	userID, userText, projectPath string,
	sessionMem stickyCodingWorkbenchMemory,
	onProgress func(string),
	onToken func(string),
) (tasks []*v2.TaskItem, planMarkdown string, planned bool) {
	return h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		userText,
		projectPath,
		sessionMem,
		h.resolveCodingRequestDecision(userText),
		onProgress,
		onToken,
	)
}

// resolveCodingWorkbenchTasksWithDecision plans a request using the decision
// made by its root runner. Keeping the decision as an input is important: the
// planner and the subagent must act on the same interpretation of a turn.
func (h *IMMessageHandler) resolveCodingWorkbenchTasksWithDecision(
	userID, userText, projectPath string,
	sessionMem stickyCodingWorkbenchMemory,
	decision codingRequestDecision,
	onProgress func(string),
	onToken func(string),
) (tasks []*v2.TaskItem, planMarkdown string, planned bool) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		userText = "执行编程任务"
	}
	single := []*v2.TaskItem{{
		Index:       1,
		Title:       truncateRunesV2(userText, 80),
		Description: userText,
	}}
	fallbackSingle := func(reason string) ([]*v2.TaskItem, string, bool) {
		if reason != "" {
			log.Printf("[coding-plan] single-task path user=%s reason=%s", userID, reason)
		}
		// Drop stale multi-step plan so the banner does not show outdated steps.
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
		// A new direct task supersedes any unanswered plan from an earlier turn.
		// Leaving it behind would let a later /plan approve execute stale work.
		h.clearStickyPendingCodingPlan(userID)
		return single, "", false
	}
	// Plan mode off: never multi-step.
	planMode := normalizeCodingPlanMode(sessionMem.PlanMode)
	if planMode == codingPlanModeOff {
		return fallbackSingle("plan mode off")
	}
	// /plan skip: one-shot single-task for the next user request.
	if sessionMem.SkipNextPlan {
		if h != nil && userID != "" {
			h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
				mem.SkipNextPlan = false
			})
		}
		return fallbackSingle("skip next plan")
	}
	if !decision.NeedsPlan {
		return fallbackSingle("")
	}
	// Short follow-ups in an ongoing session usually mean "continue/fix", not replan.
	// A short rewrite ("改为图形界面版") is still a new product-level change.
	if sessionMem.TurnCount > 0 && utf8.RuneCountInString(userText) < 80 && numberedStepCount(userText) < 2 && !codingRequestLooksModeratelyComplex(userText) {
		return fallbackSingle("short follow-up")
	}

	// Prefer steps already written by the user (numbered / T1 list) — no LLM needed.
	if userPlan := extractUserProvidedCodingPlan(userText); len(userPlan) >= codingWorkbenchPlanMinTasks {
		tasks = userPlan
		log.Printf("[coding-plan] using user-provided steps user=%s steps=%d", userID, len(tasks))
	} else if codingRestatementFallbackIsSpecific(userText, sessionMem) {
		// Rewrite follow-ups already have a host restatement. Do not wait on a
		// planner LLM that usually falls back to the same two-step plan.
		tasks = defaultModerateCodingPlan(userText, sessionMem.RequirementRestatement)
		log.Printf("[coding-plan] host rewrite plan user=%s steps=%d", userID, len(tasks))
	} else {
		if onProgress != nil {
			onProgress("复杂编程任务：正在自动规划步骤…")
		}
		_, tasks = h.planCodingWorkbenchTasks(userID, userText, projectPath, sessionMem)
		if len(tasks) < codingWorkbenchPlanMinTasks && codingRequestLooksModeratelyComplex(userText) {
			tasks = defaultModerateCodingPlan(userText, sessionMem.RequirementRestatement)
			log.Printf("[coding-plan] host fallback moderate plan user=%s steps=%d", userID, len(tasks))
		}
		if len(tasks) < codingWorkbenchPlanMinTasks {
			return fallbackSingle(fmt.Sprintf("planner returned %d tasks", len(tasks)))
		}
	}
	if len(tasks) > codingWorkbenchPlanMaxTasks {
		tasks = tasks[:codingWorkbenchPlanMaxTasks]
	}
	goalText := codingPlanGoalText(userText, sessionMem.RequirementRestatement)
	tasks = finalizeCodingWorkbenchTasks(tasks, goalText)
	// Allow independent explore-only steps to run in parallel waves (TaskRunner MaxParallel).
	tasks = softenExploreOnlyPlanDeps(tasks)
	if len(tasks) < codingWorkbenchPlanMinTasks {
		return fallbackSingle("finalize dropped below min steps")
	}
	// Always rebuild markdown after finalize so indices/deps match execution.
	planMarkdown = formatCodingWorkbenchPlanMarkdown(goalText, tasks)
	// Single sticky write: execution plan + seed session goal when empty.
	if userID != "" {
		sessionSeed := ""
		if strings.TrimSpace(sessionMem.SessionPlan) == "" {
			sessionSeed = strings.TrimSpace(sessionMem.RequirementRestatement)
			if codingSessionContextLooksGeneric(sessionSeed) {
				sessionSeed = ""
			}
			if sessionSeed == "" && !codingSessionContextLooksGeneric(userText) && utf8.RuneCountInString(userText) >= 12 {
				sessionSeed = truncateRunesV2(userText, 400)
			}
		}
		h.persistCodingWorkbenchPlans(userID, planMarkdown, sessionSeed)
		// Seed step statuses as pending for live Todo UI.
		h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	}
	// Adaptive mode and explicit plan-first mode both stop here once a complex
	// request has a real multi-step plan.  This is the user control point: a
	// simple task executes directly, while a broad/risky task shows impact and
	// steps before it can mutate the workspace.  "off" remains the explicit
	// fast-execution override.
	if planMode == codingPlanModeAuto || planMode == codingPlanModeApprove {
		if userID != "" {
			h.storeStickyPendingCodingPlan(userID, userText, planMarkdown, tasks)
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("已规划 %d 个执行步骤，等待批准后执行", len(tasks)))
		}
		log.Printf("[coding-plan] multi-step plan awaiting approve user=%s steps=%d", userID, len(tasks))
		return tasks, planMarkdown, true
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("已规划 %d 个执行步骤，开始按计划实现", len(tasks)))
	}
	log.Printf("[coding-plan] multi-step plan user=%s steps=%d", userID, len(tasks))
	return tasks, planMarkdown, true
}

// extractUserProvidedCodingPlan parses an explicit multi-step list from the user
// message (numbered bullets or T1: headings) so we do not re-plan with the LLM.
func extractUserProvidedCodingPlan(userText string) []*v2.TaskItem {
	userText = strings.TrimSpace(userText)
	if userText == "" || numberedStepCount(userText) < codingWorkbenchPlanMinTasks {
		// Also allow T1/T2 headings without line-start numbered pattern.
		if tasks := sanitizeParsedCodingTasks(v2.ParseTaskList(userText)); len(tasks) >= codingWorkbenchPlanMinTasks {
			return tasks
		}
		return nil
	}
	if tasks := sanitizeParsedCodingTasks(v2.ParseTaskList(userText)); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks
	}
	// User-authored lists: require 1. / 2. or T1: (not bare "- bullet" lists).
	if tasks := parseCodingWorkbenchPlanNumbered(userText, false); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks
	}
	return nil
}

func (h *IMMessageHandler) planCodingWorkbenchTasks(
	userID, userText, projectPath string,
	sessionMem stickyCodingWorkbenchMemory,
) (planMarkdown string, tasks []*v2.TaskItem) {
	if h == nil {
		return "", nil
	}
	cfg := h.getCodingLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getCodingLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", nil
	}

	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("User request:\n")
	ctxBuilder.WriteString(truncateRunesV2(userText, 2000))
	ctxBuilder.WriteString("\n")
	if p := strings.TrimSpace(projectPath); p != "" {
		ctxBuilder.WriteString("\nProject path: ")
		ctxBuilder.WriteString(p)
		ctxBuilder.WriteString("\n")
	}
	if s := strings.TrimSpace(sessionMem.SessionPlan); s != "" {
		ctxBuilder.WriteString("\nSession goal:\n")
		ctxBuilder.WriteString(truncateRunesV2(s, 400))
		ctxBuilder.WriteString("\n")
	}
	if s := strings.TrimSpace(sessionMem.LastSummary); s != "" {
		ctxBuilder.WriteString("\nPrevious turn summary:\n")
		ctxBuilder.WriteString(truncateRunesV2(s, 500))
		ctxBuilder.WriteString("\n")
	}

	system := `You are a senior software engineering planner for a pure coding workbench.
Break complex coding requests into an ordered execution plan of concrete steps.

Rules:
- Output 2-6 steps only.
- Prefer JSON when possible (see schema). Markdown T1: headings are also accepted.
- Each step must be implementable by a coding agent with file/shell tools.
- Order steps so dependencies are satisfied (explore → implement → verify).
- Keep titles short (<= 40 chars). Descriptions actionable and specific.
- Do NOT write code. Planning only.
- If the request is already a single trivial change, return exactly one step.

Preferred JSON schema:
{"steps":[{"title":"...","description":"...","files":["relative/path.go"],"depends_on":[1]}]}
depends_on uses 1-based step indices and is optional.
files is mandatory for a write-capable step. It must list every intended
project-relative file; use a trailing slash only for an explicit directory
claim. Omit files for read-only exploration. Do not use absolute paths,
wildcards, or vague placeholders.

Alternatively Markdown:
### T1: title
描述: ...
### T2: title
描述: ...
依赖: T1
...`

	raw := h.callLightweightLLM(cfg, system, ctxBuilder.String(), 45)
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	tasks = parseCodingWorkbenchPlan(raw)
	if len(tasks) == 0 {
		return "", nil
	}
	return formatCodingWorkbenchPlanMarkdown(userText, tasks), tasks
}

type codingWorkbenchPlanJSON struct {
	Steps []codingWorkbenchPlanStepJSON `json:"steps"`
	Tasks []codingWorkbenchPlanStepJSON `json:"tasks"` // alias
}

type codingWorkbenchPlanStepJSON struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
	DependsOn   []int    `json:"depends_on"`
}

func parseCodingWorkbenchPlan(raw string) []*v2.TaskItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip fenced code if present.
	if i := strings.Index(raw, "```"); i >= 0 {
		rest := raw[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			raw = strings.TrimSpace(rest[:j])
		}
	}
	// Try JSON object / array.
	if tasks := parseCodingWorkbenchPlanJSON(raw); len(tasks) > 0 {
		return tasks
	}
	// Markdown / T1 list via shared parser.
	if tasks := v2.ParseTaskList(raw); len(tasks) > 0 {
		return sanitizeParsedCodingTasks(tasks)
	}
	// Fallback: numbered lines (allow bullets from LLM output).
	return parseCodingWorkbenchPlanNumbered(raw, true)
}

func parseCodingWorkbenchPlanJSON(raw string) []*v2.TaskItem {
	// Object with steps/tasks.
	var obj codingWorkbenchPlanJSON
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		steps := obj.Steps
		if len(steps) == 0 {
			steps = obj.Tasks
		}
		if len(steps) > 0 {
			return stepsJSONToTasks(steps)
		}
	}
	// Bare array.
	var arr []codingWorkbenchPlanStepJSON
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		return stepsJSONToTasks(arr)
	}
	// Find embedded JSON (only recurse when the slice is a proper substring).
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			sub := raw[i : j+1]
			if sub != raw {
				return parseCodingWorkbenchPlanJSON(sub)
			}
		}
	}
	if i := strings.Index(raw, "["); i >= 0 {
		if j := strings.LastIndex(raw, "]"); j > i {
			sub := raw[i : j+1]
			if sub != raw {
				return parseCodingWorkbenchPlanJSON(sub)
			}
		}
	}
	return nil
}

func stepsJSONToTasks(steps []codingWorkbenchPlanStepJSON) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(steps))
	for _, s := range steps {
		title := strings.TrimSpace(s.Title)
		desc := strings.TrimSpace(s.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = truncateRunesV2(desc, 40)
		}
		if desc == "" {
			desc = title
		}
		deps := make([]int, 0, len(s.DependsOn))
		for _, d := range s.DependsOn {
			if d > 0 && d <= len(steps) {
				deps = append(deps, d)
			}
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			Files:       append([]string(nil), s.Files...),
			DependsOn:   deps,
		})
	}
	return out
}

func sanitizeParsedCodingTasks(tasks []*v2.TaskItem) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		desc := strings.TrimSpace(t.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("步骤 %d", len(out)+1)
		}
		if desc == "" {
			desc = title
		}
		// Preserve DependsOn; finalizeCodingWorkbenchTasks reindexes/clamps.
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			Files:       t.Files,
			DependsOn:   append([]int(nil), t.DependsOn...),
		})
	}
	return out
}

// finalizeCodingWorkbenchTasks reindexes 1..N, clamps deps, injects overall
// request context, and chains sequential depends_on when the planner omitted them
// (so a failed early step skips later work in TaskRunner).
func finalizeCodingWorkbenchTasks(tasks []*v2.TaskItem, userText string) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		desc := strings.TrimSpace(t.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("步骤 %d", len(out)+1)
		}
		if desc == "" {
			desc = title
		}
		// Compact overall request footer (avoid duplicating the full user blob).
		overall := truncateRunesV2(strings.TrimSpace(userText), 400)
		if overall != "" && !strings.Contains(desc, overall) && !strings.Contains(desc, "## Overall request") {
			desc = desc + "\n\n## Overall request\n" + overall
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			Files:       append([]string(nil), t.Files...),
			DependsOn:   append([]int(nil), t.DependsOn...),
		})
	}
	n := len(out)
	if n == 0 {
		return out
	}
	// Remap/clamp depends_on to current 1..N indices; drop self-deps.
	anyDeps := false
	for _, t := range out {
		if len(t.DependsOn) > 0 {
			anyDeps = true
			break
		}
	}
	if !anyDeps && n >= 2 {
		// Default sequential chain: each step depends on its predecessor.
		for i := 1; i < n; i++ {
			out[i].DependsOn = []int{out[i-1].Index}
		}
	} else {
		for _, t := range out {
			if len(t.DependsOn) == 0 {
				continue
			}
			deps := make([]int, 0, len(t.DependsOn))
			seen := map[int]bool{}
			for _, d := range t.DependsOn {
				// Only earlier steps prevent cycles and forward dependencies.
				if d < 1 || d >= t.Index || d > n || seen[d] {
					continue
				}
				seen[d] = true
				deps = append(deps, d)
			}
			t.DependsOn = deps
		}
		// Steps with empty deps after the first still chain to previous so a
		// mid-plan failure cannot silently run independent later steps.
		for i := 1; i < n; i++ {
			if len(out[i].DependsOn) == 0 {
				out[i].DependsOn = []int{out[i-1].Index}
			}
		}
	}
	return out
}

// softenExploreOnlyPlanDeps removes sequential chain deps between consecutive
// explore/read-only steps so TaskRunner can schedule them in a parallel wave.
// Implement/verify steps keep their depends_on chain.
func softenExploreOnlyPlanDeps(tasks []*v2.TaskItem) []*v2.TaskItem {
	if len(tasks) < 2 {
		return tasks
	}
	isExplore := func(t *v2.TaskItem) bool {
		if t == nil {
			return false
		}
		// Prefer title only: Description often includes "## Overall request" with
		// implement/build words from the user goal that would false-negative.
		title := strings.ToLower(strings.TrimSpace(t.Title))
		desc := strings.ToLower(strings.TrimSpace(t.Description))
		if i := strings.Index(desc, "\n\n## overall request"); i >= 0 {
			desc = strings.TrimSpace(desc[:i])
		}
		blob := title + " " + desc
		// Exclude implement/verify keywords first.
		for _, kw := range []string{
			"implement", "实现", "编码", "fix", "修复", "write", "edit",
			"verify", "test", "build", "验证", "测试", "构建", "编译", "验收",
		} {
			if strings.Contains(blob, kw) {
				return false
			}
		}
		for _, kw := range []string{
			"explor", "探查", "定位", "map ", "read", "阅读", "survey", "定位代码",
			"了解", "分析现状", "inspect", "locate",
		} {
			if strings.Contains(blob, kw) {
				return true
			}
		}
		return false
	}
	for i := 1; i < len(tasks); i++ {
		if tasks[i] == nil || tasks[i-1] == nil {
			continue
		}
		if !isExplore(tasks[i]) || !isExplore(tasks[i-1]) {
			continue
		}
		// Only drop pure sequential single-dep on previous explore step.
		if len(tasks[i].DependsOn) == 1 && tasks[i].DependsOn[0] == tasks[i-1].Index {
			tasks[i].DependsOn = nil
		}
	}
	return tasks
}

// parseCodingWorkbenchPlanNumbered extracts ordered steps from numbered lines.
// allowBullets: LLM plans may use "- step"; user-authored plans should not
// (false) so ordinary bullet lists are not treated as execution plans.
func parseCodingWorkbenchPlanNumbered(raw string, allowBullets bool) []*v2.TaskItem {
	lines := strings.Split(raw, "\n")
	var out []*v2.TaskItem
	pat := `^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：])\s+(.+)$`
	if allowBullets {
		pat = `^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：]|[-*•])\s+(.+)$`
	}
	re := regexp.MustCompile(pat)
	for _, line := range lines {
		m := re.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		title := strings.TrimSpace(m[1])
		if title == "" {
			continue
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       truncateRunesV2(title, 80),
			Description: title,
		})
	}
	return out
}

func formatCodingWorkbenchPlanMarkdown(userText string, tasks []*v2.TaskItem) string {
	var b strings.Builder
	b.WriteString("**目标**: ")
	b.WriteString(truncateRunesV2(strings.TrimSpace(userText), 200))
	b.WriteString("\n\n")
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		b.WriteString(fmt.Sprintf("### T%d: %s\n", t.Index, title))
		if d := planStepDescriptionForDisplay(t.Description, title); d != "" {
			b.WriteString("描述: ")
			b.WriteString(d)
			b.WriteString("\n")
		}
		if len(t.Files) > 0 {
			b.WriteString("Files: ")
			b.WriteString(strings.Join(t.Files, ", "))
			b.WriteString("\n")
		}
		if len(t.DependsOn) > 0 {
			b.WriteString("依赖: ")
			for i, d := range t.DependsOn {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(fmt.Sprintf("T%d", d))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// planStepDescriptionForDisplay strips the injected Overall request footer so
// the auto-plan UI stays compact (execution still uses full Description).
func planStepDescriptionForDisplay(desc, title string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" || desc == title {
		return ""
	}
	if i := strings.Index(desc, "\n\n## Overall request"); i >= 0 {
		desc = strings.TrimSpace(desc[:i])
	}
	if desc == "" || desc == title {
		return ""
	}
	return truncateRunesV2(desc, 300)
}

// codingWorkbenchRunHeader summarizes TaskRunner outcomes using the user's
// requested activity, rather than the implementation detail that the coding
// workbench executed the turn. A repository inquiry must never be labelled as
// a completed code change.
func codingWorkbenchRunHeader(kind codingRequestKind, planned bool, stepCount int, results []v2.TaskRunResult) string {
	labels := codingWorkbenchLabelsForRequest(kind)
	if len(results) == 0 {
		return labels.incomplete
	}
	if !planned || stepCount <= 1 {
		if results[0].Status == v2.TaskFailed {
			return labels.incomplete
		}
		if results[0].Status == v2.TaskSkipped {
			return labels.skipped
		}
		return labels.complete
	}
	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case v2.TaskPassed:
			passed++
		case v2.TaskFailed:
			failed++
		case v2.TaskSkipped:
			skipped++
		}
	}
	switch {
	case passed == 0 && failed == 0 && skipped == 0:
		return labels.incomplete
	case failed == 0 && skipped == 0 && passed > 0:
		return fmt.Sprintf("%s (completed %d planned steps)", labels.complete, stepCount)
	case failed == 0 && skipped > 0 && passed > 0:
		return fmt.Sprintf("%s (%d/%d passed, %d skipped)", labels.partial, passed, stepCount, skipped)
	case passed == 0 && (failed > 0 || skipped > 0):
		return fmt.Sprintf("%s (%d planned steps, %d passed)", labels.incomplete, stepCount, passed)
	default:
		return fmt.Sprintf("%s (%d/%d passed, %d failed, %d skipped)", labels.partial, passed, stepCount, failed, skipped)
	}
}

type codingWorkbenchRunLabels struct {
	complete   string
	partial    string
	incomplete string
	skipped    string
}

func codingWorkbenchLabelsForRequest(kind codingRequestKind) codingWorkbenchRunLabels {
	switch kind {
	case codingRequestInquiry:
		return codingWorkbenchRunLabels{
			complete:   "Repository analysis complete",
			partial:    "Repository analysis partially complete",
			incomplete: "Repository analysis incomplete",
			skipped:    "Repository analysis cancelled or skipped",
		}
	case codingRequestOperational:
		return codingWorkbenchRunLabels{
			complete:   "Task complete",
			partial:    "Task partially complete",
			incomplete: "Task incomplete",
			skipped:    "Task cancelled or skipped",
		}
	default:
		return codingWorkbenchRunLabels{
			complete:   "Coding complete",
			partial:    "Coding partially complete",
			incomplete: "Coding incomplete",
			skipped:    "Coding cancelled or skipped",
		}
	}
}

// setStickyCodingExecutionPlan stores the multi-step plan for continuity/banner.
func (h *IMMessageHandler) setStickyCodingExecutionPlan(userID, plan string) {
	h.persistCodingWorkbenchPlans(userID, plan, "")
}

// clearStickyCodingExecutionPlan drops a stale multi-step plan (e.g. after a
// simple single-task turn so the UI banner does not keep showing old steps).
func (h *IMMessageHandler) clearStickyCodingExecutionPlan(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = ""
	})
}

// persistCodingWorkbenchPlans writes ExecutionPlan and optionally seeds SessionPlan
// in one sticky disk write.
func (h *IMMessageHandler) persistCodingWorkbenchPlans(userID, executionPlan, sessionPlanIfEmpty string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if ep := truncateRunesForSubAgent(strings.TrimSpace(executionPlan), 2000); ep != "" {
			mem.ExecutionPlan = ep
		}
		if seed := truncateRunesForSubAgent(strings.TrimSpace(sessionPlanIfEmpty), 800); seed != "" {
			if strings.TrimSpace(mem.SessionPlan) == "" {
				mem.SessionPlan = seed
			}
		}
	})
}
