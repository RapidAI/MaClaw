package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	codingWorkbenchPlanMaxTasks = 6
	codingWorkbenchPlanMinTasks = 2
	// Six steps with 300-rune briefs already pass 2000 runes. The cut lands
	// on the last step, which is the one a credit stop still has to run.
	codingWorkbenchExecutionPlanPersistRunes = 4000
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

// codingOperationalAcceptance is the evidence contract for an operational turn.
// The classifier sets it once. Empty and launch keep the project run/build
// gate. Command means the work is a host action, so a successful non-probe
// command is the evidence. Scoring must not infer this from the user text.
type codingOperationalAcceptance string

const (
	codingOperationalAcceptanceLaunch  codingOperationalAcceptance = "launch"
	codingOperationalAcceptanceCommand codingOperationalAcceptance = "command"
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
	// Acceptance is meaningful only for operational. The quality gate reads it
	// and does not re-read the request. Omitted or unknown values stay launch,
	// so a missing field never grants the looser host-command contract.
	Acceptance codingOperationalAcceptance `json:"acceptance,omitempty"`
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
	case codingRequestInquiry:
		decision.NeedsPlan = false
		decision.Acceptance = ""
		return decision, true
	case codingRequestOperational:
		decision.NeedsPlan = false
		decision.Acceptance = normalizeCodingOperationalAcceptance(decision.Acceptance)
		return decision, true
	case codingRequestImplementation:
		decision.Acceptance = ""
		return decision, true
	default:
		return codingRequestDecision{}, false
	}
}

// normalizeCodingOperationalAcceptance refuses anything other than the host
// command contract. A malformed acceptance must not loosen the launch gate.
func normalizeCodingOperationalAcceptance(raw codingOperationalAcceptance) codingOperationalAcceptance {
	switch codingOperationalAcceptance(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case codingOperationalAcceptanceCommand:
		return codingOperationalAcceptanceCommand
	default:
		return codingOperationalAcceptanceLaunch
	}
}

const codingRequestClassifierSystemPrompt = `Classify the user's coding-workbench request by intent. Return JSON only.

Schema: {"kind":"inquiry|operational|implementation","needs_plan":true|false,"acceptance":"launch|command"}

inquiry: the user wants explanation, inspection, location, or an answer. It is read-only: never run commands or change files. Omit acceptance.
operational: the user wants work that may run commands but must not change project source files. Set acceptance to the evidence contract for that work:
- launch: run, build, test, or demonstrate an existing project.
- command: a host action that is not a project launch, such as installing or removing a package or tool, or checking server or host status.
implementation: the user asks to modify, create, fix, refactor, delete, or clear workspace files. Clearing or emptying the current project directory is implementation, never operational. Omit acceptance.
needs_plan is true for an implementation request that is more than one local edit: several files, a feature plus tests, UI plus logic, a richer rewrite, or more than one distinct deliverable. One-line typo, rename, or comment-only fixes stay needs_plan=false. Inquiry and operational always use needs_plan=false.
Questions about how a command works are inquiry, even when they mention build, test, run, or compile. Asking to run, build, test, or demonstrate the project is operational with acceptance launch. Asking to run a host tool, install or remove a package, or check the machine is operational with acceptance command. The verb alone does not choose the contract; judge what the user wants done.
When kind is operational, acceptance is required. When kind is not operational, omit acceptance. An omitted acceptance is treated as launch and does not cover a host action.
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
	if decision, ok := parseCodingRequestDecision(h.callLightweightLLMOnce(cfg, codingRequestClassifierSystemPrompt, userText, 15)); ok {
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

func codingSessionHasImplementationTrajectory(mem stickyCodingWorkbenchMemory) bool {
	return codingSessionHasWrittenPath(mem.FilesModified) || codingSessionHasWrittenPath(mem.FilesCreated)
}

func codingSessionHasWrittenPath(paths []string) bool {
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			return true
		}
	}
	return false
}

// applyCodingSessionImplementationPosture keeps the implementation tool
// contract for a session that has already written project files. The
// classifier sees only the latest utterance, so a follow-up in that session
// is labeled inquiry and the host then removes ssh_write_file, ssh_edit_file,
// and ordinary shell. The session record is the authority for that contract,
// not the wording of the new message. Operational turns stay operational: a
// run or build still must not be retargeted at source edits. A session with
// no written files, including one that only has a plan, can still be inquiry.
func applyCodingSessionImplementationPosture(decision codingRequestDecision, mem stickyCodingWorkbenchMemory) codingRequestDecision {
	if decision.Kind != codingRequestInquiry || !codingSessionHasImplementationTrajectory(mem) {
		return decision
	}
	decision.Kind = codingRequestImplementation
	decision.NeedsPlan = false
	decision.Acceptance = ""
	return decision
}

// codingSessionWriteContractNote is attached to an implementation turn whose
// session already wrote project files. The previous turn summary is carried
// forward verbatim, and a rejected inquiry used to describe that rejection as
// the environment. This note is the current tool contract.
const codingSessionWriteContractNote = "会话写合同：本会话已经写过项目文件。本轮可以使用 ssh_write_file、ssh_edit_file 和普通 ssh_bash，包括复合命令、git，以及运行已有构建结果。上一轮如果把环境说成只读，那只是当时的询问结果，不是当前限制。"

func codingSessionWriteContractContext(kind codingRequestKind, mem stickyCodingWorkbenchMemory) string {
	if kind != codingRequestImplementation || !codingSessionHasImplementationTrajectory(mem) {
		return ""
	}
	return codingSessionWriteContractNote
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
// inspection vocabulary — repository files and read-only host status — and
// rejects wrappers, builds, tests, package managers, redirects, and every
// command that could mutate the workspace.
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
	// Host-status programs. The plain forms only print state. Forms that
	// repeat, sync, follow, or write are checked below: free -s and free -c,
	// df --sync, vmstat/iostat/mpstat with a delay and no count, netstat -c,
	// findmnt --poll, top without a batch iteration, and ss --kill / --events /
	// --diag. dmesg -c/-n, sensors -s, pidstat -e, mount, ip, and sar -o stay
	// out: they clear, set, exec, or write.
	case "uptime", "ps", "pstree", "pgrep",
		"lscpu", "lsmem", "lsblk", "lsmod", "lspci", "lsusb",
		"w", "who":
		return true
	case "df":
		return codingInquiryDFReadOnly(args)
	case "free":
		return codingInquiryFreeExits(args)
	case "vmstat":
		return codingInquiryRepeatSamplesExit(codingInquirySampleVM, args)
	case "iostat":
		return codingInquiryRepeatSamplesExit(codingInquirySampleIO, args)
	case "mpstat":
		return codingInquiryRepeatSamplesExit(codingInquirySampleMP, args)
	case "netstat":
		return !codingInquiryNetstatContinuous(args)
	case "findmnt":
		return codingInquiryFindmntExits(args)
	case "top":
		return codingInquiryTopBatchExits(args)
	case "ss":
		// Kill closes sockets, -E follows events, and -D writes a diag file.
		// The gate lowercases first, so those short flags arrive as -k, -e,
		// and -d. The read-only names of those letters are refused with them.
		return !codingInquirySSUnsafe(args)
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

// codingInquirySSUnsafe reports ss forms that close sockets, follow events, or
// write a diag file. getopt_long accepts a unique abbreviation, so --k is
// --kill, --ev is --events, and --di is --diag. --ex is --extended and --dc
// is --dccp; those stay prints. Short e and d are refused with E and D
// because this gate has already lowercased the command.
func codingInquirySSUnsafe(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "" || arg == "--" {
			continue
		}
		lower := strings.ToLower(arg)
		name := codingInquiryLongName(lower)
		if codingInquiryLongExtends(name, "--kill") ||
			codingInquiryLongExtends(name, "--events") ||
			codingInquiryLongExtends(name, "--diag") {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "dekDEK") {
			return true
		}
	}
	return false
}

const (
	// A status inquiry may take a short finite sample. A delay with no count
	// repeats until the remote wait kills it.
	codingInquirySampleCountMax  = 30
	codingInquirySampleWindowMax = 120
)

type codingInquirySampleKind int

const (
	codingInquirySampleVM codingInquirySampleKind = iota
	codingInquirySampleIO
	codingInquirySampleMP
)

// codingInquiryRepeatSamplesExit allows one report, or delay+count that ends.
// vmstat and iostat treat a positive delay without a count as continuous.
// A lone 0 is the since-boot one-shot for iostat and mpstat: iostat forces
// its count to 1 when the interval is 0, and mpstat exits before the sample
// loop. vmstat rejects a delay below 1. iostat device names stay in front of
// the trailing interval.
func codingInquiryRepeatSamplesExit(kind codingInquirySampleKind, args []string) bool {
	head, nums, ok := codingInquirySamplePositionals(kind, args)
	if !ok {
		return false
	}
	if kind != codingInquirySampleIO && len(head) > 0 {
		return false
	}
	// -N <node> and -n (NUMA, no value) collapse after case folding. Any
	// integer after that flag might be an unbounded interval, so refuse it.
	if kind == codingInquirySampleMP && codingInquiryHasExactFlag(args, "-n", "--node") && len(head)+len(nums) > 0 {
		return false
	}
	switch len(nums) {
	case 0:
		return true
	case 1:
		return nums[0] == 0 && kind != codingInquirySampleVM
	default:
		return codingInquirySampleWindowOK(nums[0], nums[1])
	}
}

func codingInquirySampleWindowOK(interval, count int) bool {
	if count < 1 || count > codingInquirySampleCountMax || interval < 0 {
		return false
	}
	if interval == 0 || interval > codingInquirySampleWindowMax {
		return interval == 0
	}
	return interval*count <= codingInquirySampleWindowMax
}

// codingInquirySamplePositionals splits flag values from the trailing interval
// and count. A non-numeric token left over is a device name for iostat and a
// rejection for vmstat/mpstat. The command text is already lowercased.
func codingInquirySamplePositionals(kind codingInquirySampleKind, args []string) (head []string, nums []int, ok bool) {
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(normalizeShellCommandToken(args[i]))
		if arg == "" || arg == "--" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		name, _, inline := codingInquiryFlagParts(arg)
		if inline {
			continue
		}
		if codingInquirySampleFlagTakesValue(kind, name) && i+1 < len(args) {
			i++
			continue
		}
		if kind == codingInquirySampleIO && name == "-p" && i+1 < len(args) {
			next := strings.TrimSpace(normalizeShellCommandToken(args[i+1]))
			if _, isNum := codingInquiryNonNegInt(next); next != "" && !isNum {
				i++
			}
		}
	}
	i := len(positionals)
	for i > 0 && len(nums) < 2 {
		n, isNum := codingInquiryNonNegInt(positionals[i-1])
		if !isNum {
			break
		}
		nums = append([]int{n}, nums...)
		i--
	}
	for _, token := range positionals[:i] {
		if _, isNum := codingInquiryNonNegInt(token); isNum {
			return nil, nil, false
		}
	}
	return positionals[:i], nums, true
}

func codingInquirySampleFlagTakesValue(kind codingInquirySampleKind, name string) bool {
	switch kind {
	case codingInquirySampleVM:
		// --u / --un are --unit, and --p / --pa are --partition. No other
		// vmstat long option shares those prefixes. -S folds into -s, so a
		// separate unit argument stays on the positional path.
		return name == "-p" ||
			codingInquiryLongExtends(name, "--partition") ||
			codingInquiryLongExtends(name, "--unit")
	case codingInquirySampleIO:
		switch name {
		case "-o", "--output", "-g", "--group", "-j", "-f", "--dec":
			return true
		default:
			return false
		}
	case codingInquirySampleMP:
		// -P and -I. -N and the no-value -n flag fold together, so -n is not
		// treated as taking a value; see codingInquiryRepeatSamplesExit.
		switch name {
		case "-p", "-i", "-o", "--dec":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// codingInquiryLongName is the --option word without an inline "=value".
// A short flag returns "" so it cannot match a long option by accident.
func codingInquiryLongName(arg string) string {
	if !strings.HasPrefix(arg, "--") {
		return ""
	}
	if eq := strings.IndexByte(arg, '='); eq > 0 {
		return arg[:eq]
	}
	return arg
}

// codingInquiryLongExtends reports whether name is full or a getopt_long
// abbreviation of full. "--" itself is too short to be an abbreviation.
func codingInquiryLongExtends(name, full string) bool {
	return len(name) >= 3 && strings.HasPrefix(full, name)
}

// codingInquiryLongMatch returns the single long option that name abbreviates.
// An ambiguous prefix matches nothing, which is how getopt_long rejects it.
func codingInquiryLongMatch(name string, options []string) string {
	if len(name) < 3 || !strings.HasPrefix(name, "--") {
		return ""
	}
	found := ""
	for _, opt := range options {
		if strings.HasPrefix(opt, name) {
			if found != "" {
				return ""
			}
			found = opt
		}
	}
	return found
}

// codingInquiryDFReadOnly rejects df --sync, including --sy. That flag calls
// sync(2). df --si is a unit selector and stays allowed; it is not a prefix
// of --sync. --no-sync is the default and is not a prefix either.
func codingInquiryDFReadOnly(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "--" {
			break
		}
		if codingInquiryLongExtends(codingInquiryLongName(arg), "--sync") {
			return false
		}
	}
	return true
}

func codingInquiryFlagParts(arg string) (name, value string, inline bool) {
	if strings.HasPrefix(arg, "--") {
		if eq := strings.IndexByte(arg, '='); eq > 0 {
			return arg[:eq], arg[eq+1:], true
		}
	}
	return arg, "", false
}

func codingInquiryNonNegInt(token string) (int, bool) {
	if token == "" || token[0] < '0' || token[0] > '9' {
		return 0, false
	}
	n, err := strconv.Atoi(token)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// codingInquiryFreeExits allows one free report. -s repeats until -c stops
// it. -c without -s still repeats: procps free turns on the same repeat loop
// and sleeps its default of one second. A missing delay uses that default.
func codingInquiryFreeExits(args []string) bool {
	seconds := false
	delay, delayKnown := 0, false
	count := -1
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(normalizeShellCommandToken(args[i]))
		if arg == "" || arg == "--" {
			continue
		}
		switch {
		case strings.HasPrefix(arg, "--"):
			name, value, inline := codingInquiryFlagParts(arg)
			switch {
			case codingInquiryLongExtends(name, "--seconds"):
				// --si is a unit flag, not seconds. --sec is --seconds.
				seconds = true
				if inline {
					if n, ok := codingInquiryDelaySeconds(value); ok {
						delay, delayKnown = n, true
					}
				} else if i+1 < len(args) {
					if n, ok := codingInquiryDelaySeconds(strings.TrimSpace(normalizeShellCommandToken(args[i+1]))); ok {
						delay, delayKnown = n, true
						i++
					}
				}
			case codingInquiryLongExtends(name, "--count"):
				if inline {
					if n, ok := codingInquiryNonNegInt(value); ok {
						count = n
					}
				} else if i+1 < len(args) {
					if n, ok := codingInquiryNonNegInt(strings.TrimSpace(normalizeShellCommandToken(args[i+1]))); ok {
						count = n
						i++
					}
				}
			}
		case arg == "-s":
			seconds = true
			if i+1 < len(args) {
				if n, ok := codingInquiryDelaySeconds(strings.TrimSpace(normalizeShellCommandToken(args[i+1]))); ok {
					delay, delayKnown = n, true
					i++
				}
			}
		case arg == "-c":
			if i+1 < len(args) {
				if n, ok := codingInquiryNonNegInt(strings.TrimSpace(normalizeShellCommandToken(args[i+1]))); ok {
					count = n
					i++
				}
			}
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			body := arg[1:]
			for j := 0; j < len(body); j++ {
				switch body[j] {
				case 's':
					seconds = true
					rest := body[j+1:]
					if rest == "" && i+1 < len(args) {
						next := strings.TrimSpace(normalizeShellCommandToken(args[i+1]))
						if n, ok := codingInquiryDelaySeconds(next); ok {
							delay, delayKnown = n, true
							i++
						}
					} else if n, ok := codingInquiryDelaySeconds(rest); ok {
						delay, delayKnown = n, true
					}
					j = len(body)
				case 'c':
					rest := body[j+1:]
					if rest == "" && i+1 < len(args) {
						if n, ok := codingInquiryNonNegInt(strings.TrimSpace(normalizeShellCommandToken(args[i+1]))); ok {
							count = n
							i++
						}
					} else if n, ok := codingInquiryNonNegInt(rest); ok {
						count = n
					}
					j = len(body)
				}
			}
		}
	}
	if !seconds && count < 0 {
		return true
	}
	// A missing delay, a count with no -s, or a fractional delay still has
	// to fit the sample window. The ceiling of a fraction is what that
	// check sees. procps free uses one second when -s is absent.
	if !delayKnown {
		delay = 1
	}
	return codingInquirySampleWindowOK(delay, count)
}

func codingInquiryDelaySeconds(token string) (int, bool) {
	if n, ok := codingInquiryNonNegInt(token); ok {
		return n, true
	}
	if !codingInquiryFloatToken(token) {
		return 0, false
	}
	whole := 0
	for _, r := range token {
		if r == '.' {
			break
		}
		whole = whole*10 + int(r-'0')
	}
	return whole + 1, true
}

func codingInquiryHasExactFlag(args []string, names ...string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		name, _, _ := codingInquiryFlagParts(arg)
		for _, want := range names {
			if name == want {
				return true
			}
		}
	}
	return false
}

func codingInquiryFloatToken(token string) bool {
	token = strings.TrimSpace(normalizeShellCommandToken(token))
	if token == "" || token[0] < '0' || token[0] > '9' {
		return false
	}
	dot := false
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return dot
}

func codingInquiryNetstatContinuous(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "" || arg == "--" {
			continue
		}
		lower := strings.ToLower(arg)
		// --cont is --continuous. A short cluster containing c is the same flag.
		if codingInquiryLongExtends(codingInquiryLongName(lower), "--continuous") {
			return true
		}
		// -c is netstat's continuous flag. No other short flag uses c.
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "Cc") {
			return true
		}
	}
	return false
}

// codingInquiryFindmntExits rejects --poll. The gate lowercases -P (pairs,
// which prints once) together with -p (poll), so both are refused.
func codingInquiryFindmntExits(args []string) bool {
	for _, raw := range args {
		arg := strings.TrimSpace(normalizeShellCommandToken(raw))
		if arg == "" || arg == "--" {
			continue
		}
		lower := strings.ToLower(arg)
		// --po / --pol are --poll. --pairs does not share that prefix.
		if codingInquiryLongExtends(codingInquiryLongName(lower), "--poll") {
			return false
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "Pp") {
			return false
		}
	}
	return true
}

// codingInquiryTopLongNames are procps top's long options. --batch is the
// unique abbreviation of --batch-mode. A shared prefix such as --s matches
// more than one entry, so the lookup rejects it.
var codingInquiryTopLongNames = []string{
	"--accum-time-toggle",
	"--apply-defaults",
	"--batch-mode",
	"--cmdline-toggle",
	"--delay",
	"--filter-any-user",
	"--filter-only-euser",
	"--help",
	"--idle-toggle",
	"--iterations",
	"--list-fields",
	"--pid",
	"--scale-summary-mem",
	"--scale-task-mem",
	"--secure-mode",
	"--single-cpu-toggle",
	"--sort-override",
	"--threads-show",
	"--version",
	"--width",
}

// codingInquiryTopBatchExits allows the one-shot batch forms. Bare top and
// top -b do not exit. procps reads -bn1 as -b -n 1, and -n=1 the same as -n 1.
func codingInquiryTopBatchExits(args []string) bool {
	batch := false
	iterations := -1
	setIterations := func(token string) bool {
		token = strings.TrimPrefix(strings.TrimSpace(normalizeShellCommandToken(token)), "=")
		n, ok := codingInquiryNonNegInt(token)
		if !ok || n < 1 || n > 3 {
			return false
		}
		iterations = n
		return true
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(normalizeShellCommandToken(args[i]))
		if arg == "" || arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, inline := codingInquiryFlagParts(arg)
			switch codingInquiryLongMatch(name, codingInquiryTopLongNames) {
			case "--batch-mode":
				batch = true
			case "--iterations":
				token := value
				if !inline {
					if i+1 >= len(args) {
						return false
					}
					token = args[i+1]
					i++
				}
				if !setIterations(token) {
					return false
				}
			case "--delay", "--scale-summary-mem", "--scale-task-mem", "--sort-override",
				"--pid", "--filter-any-user", "--filter-only-euser", "--width":
				// Required arguments are consumed only when they do not look
				// like another flag. A flag-shaped argument makes top exit
				// with an error, and leaving it in place keeps -n visible.
				if !inline && i+1 < len(args) && !strings.HasPrefix(strings.TrimSpace(normalizeShellCommandToken(args[i+1])), "-") {
					i++
				}
			case "--accum-time-toggle", "--apply-defaults", "--cmdline-toggle", "--threads-show",
				"--help", "--idle-toggle", "--list-fields", "--secure-mode", "--single-cpu-toggle",
				"--version":
			default:
				return false
			}
			continue
		}
		switch {
		case arg == "-b":
			batch = true
		case arg == "-n":
			if i+1 >= len(args) || !setIterations(args[i+1]) {
				return false
			}
			i++
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			body := arg[1:]
			for j := 0; j < len(body); j++ {
				switch body[j] {
				case 'b':
					batch = true
				case 'n':
					rest := body[j+1:]
					if rest == "" {
						if i+1 >= len(args) || !setIterations(args[i+1]) {
							return false
						}
						i++
					} else if !setIterations(rest) {
						return false
					}
					j = len(body)
				case 'd', 'p', 'u', 'o', 'w', 'e':
					// These take a value. The rest of this cluster is that
					// value; otherwise the next argv is, when it is not a flag.
					if j == len(body)-1 && i+1 < len(args) && !strings.HasPrefix(strings.TrimSpace(normalizeShellCommandToken(args[i+1])), "-") {
						i++
					}
					j = len(body)
				}
			}
		default:
			return false
		}
	}
	return batch && iterations >= 1
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

// codingWorkbenchContinueCue is a resume after a stopped coding plan.
// "继续" stays out of parseCodingExecRetryCommand because workflow document
// confirmation also owns that word; here it only resumes a plan that already
// started and still has steps that did not pass.
//
// The checklist in StepStatuses is that plan once the agent has published it.
// An exact token list treated "继续呀" as a new request. The single-task path
// then deleted the checklist and replaced the understanding with a generic
// restatement, so the next turn started over. A continue head with no new
// requirement is the same resume. Residual work ("继续完善界面") is not.
func codingWorkbenchContinueCue(userText string) bool {
	if semanticBareContinueQuery(userText) {
		return true
	}
	switch parseCodingExecRetryCommand(userText) {
	case codingExecRetryActionResume, codingExecRetryActionFailed:
		return true
	default:
		return codingContinueUtteranceHasNoNewRequirement(userText)
	}
}

// codingContinueUtteranceHasNoNewRequirement reports a continue act whose
// payload is only politeness or a discourse particle. The unfinished
// checklist is the requirement. Anything left after the continue head is a
// new request and must not resume.
func codingContinueUtteranceHasNoNewRequirement(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(acpInnerUserRequest(userText)))
	text = strings.Trim(text, codingContinueTrimRunes)
	if text == "" {
		return false
	}
	text = peelCodingContinueFillers(text)
	for _, stem := range codingContinueStems {
		if !strings.HasPrefix(text, stem) {
			continue
		}
		if codingContinueRemainderEmpty(text[len(stem):]) {
			return true
		}
	}
	return false
}

// Longest first, so "继续执行" is the head of "继续执行呀" and is not split
// into "继续" plus a leftover requirement "执行".
var codingContinueStems = []string{
	"继续远程编码", "繼續遠程編碼", "继续远端编码", "繼續遠端編碼",
	"继续执行", "繼續執行", "继续编码", "繼續編碼", "继续任务", "繼續任務",
	"继续做", "繼續做", "接着做", "接著做",
	"continue execution", "continue coding", "resume coding", "keep going",
	"继续", "繼續", "接着", "接著", "continue",
}

const codingContinueTrimRunes = "。.!！?？~～、,，;；:：\"'“”‘’…⋯ \t"

// Leading fillers carry no requirement. Longest first so "那么" is not peeled
// down to "么".
var codingContinueFillers = []string{
	"那么", "那麼", "那就", "请你", "請你", "麻烦你", "麻煩你",
	"帮我", "幫我", "麻烦", "麻煩", "请", "請", "你就", "您", "你", "再", "那",
}

func peelCodingContinueFillers(text string) string {
	for i := 0; i < 4; i++ {
		trimmed := strings.Trim(text, codingContinueTrimRunes)
		next := trimmed
		for _, filler := range codingContinueFillers {
			if strings.HasPrefix(next, filler) {
				next = strings.Trim(strings.TrimPrefix(next, filler), codingContinueTrimRunes)
				break
			}
		}
		if next == trimmed {
			return trimmed
		}
		text = next
	}
	return strings.Trim(text, codingContinueTrimRunes)
}

// codingContinueParticles are discourse particles. They are matched only as a
// trailing suffix, so "下载" does not lose its "下" and "完善" stays a request.
var codingContinueParticles = []string{
	"一下子", "一下", "please", "thanks",
	"呀", "啊", "吧", "呢", "了", "哦", "喔", "噢", "哈", "嘛", "呐", "哇", "哟", "呦", "嘞", "咯",
	"做", "干", "幹", "下",
}

func codingContinueRemainderEmpty(rest string) bool {
	rest = strings.Trim(rest, codingContinueTrimRunes)
	for rest != "" {
		stripped := false
		for _, particle := range codingContinueParticles {
			if !strings.HasSuffix(rest, particle) {
				continue
			}
			rest = strings.Trim(strings.TrimSuffix(rest, particle), codingContinueTrimRunes)
			stripped = true
			break
		}
		if !stripped {
			return false
		}
	}
	return true
}

func codingPlanStepWasStarted(status string) bool {
	switch strings.TrimSpace(status) {
	case codingStepPassed, codingStepFailed, codingStepVerifyFail, codingStepSkipped, codingStepRunning:
		return true
	default:
		return false
	}
}

func codingPlanStepNeedsResume(status string) bool {
	return strings.TrimSpace(status) != codingStepPassed
}

// codingWorkbenchShouldResumeIncompletePlan reports whether this utterance
// should re-enter the current plan instead of planning a new turn.
func codingWorkbenchShouldResumeIncompletePlan(userText string, mem stickyCodingWorkbenchMemory) bool {
	_, ok := selectIncompleteCodingPlanTasks(userText, mem)
	return ok
}

// selectIncompleteCodingPlanTasks returns the not-yet-passed steps of a plan
// that has already started. An untouched pending approval and a fully passed
// plan are left alone.
func selectIncompleteCodingPlanTasks(userText string, mem stickyCodingWorkbenchMemory) ([]*v2.TaskItem, bool) {
	if mem.SkipNextPlan || !codingWorkbenchContinueCue(userText) {
		return nil, false
	}
	if len(mem.StepStatuses) == 0 {
		return nil, false
	}
	// PlanRunStarted survives the pending rewrite in reopen. Without it, a
	// continue that stops before the runner marks a step running looks like
	// an approval that never started, and the next continue deletes the list.
	started := mem.PlanRunStarted
	incomplete := make(map[int]codingWorkbenchStepStatus)
	for _, st := range mem.StepStatuses {
		if codingPlanStepWasStarted(st.Status) {
			started = true
		}
		if codingPlanStepNeedsResume(st.Status) {
			incomplete[st.Index] = st
		}
	}
	if !started || len(incomplete) == 0 {
		return nil, false
	}
	parsed := parseCodingWorkbenchPlan(mem.ExecutionPlan)
	byIndex := make(map[int]*v2.TaskItem, len(parsed))
	var ordered []*v2.TaskItem
	for _, task := range parsed {
		if task == nil || task.Index <= 0 {
			continue
		}
		byIndex[task.Index] = task
		ordered = append(ordered, task)
	}
	var selected []*v2.TaskItem
	if len(ordered) > 0 {
		for _, task := range ordered {
			if _, need := incomplete[task.Index]; need {
				selected = append(selected, task)
			}
		}
		for _, st := range mem.StepStatuses {
			if _, need := incomplete[st.Index]; !need {
				continue
			}
			if _, found := byIndex[st.Index]; found {
				continue
			}
			selected = append(selected, taskFromCodingStepStatus(st))
		}
	} else {
		for _, st := range mem.StepStatuses {
			if _, need := incomplete[st.Index]; need {
				selected = append(selected, taskFromCodingStepStatus(st))
			}
		}
	}
	if len(selected) == 0 {
		return nil, false
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i] == nil || selected[j] == nil {
			return selected[i] != nil
		}
		return selected[i].Index < selected[j].Index
	})
	return codingPlanTasksForResume(selected), true
}

func taskFromCodingStepStatus(st codingWorkbenchStepStatus) *v2.TaskItem {
	title := strings.TrimSpace(st.Title)
	if title == "" {
		title = fmt.Sprintf("T%d", st.Index)
	}
	// Summary is the last failure or skip note. It is not the step brief;
	// putting "insufficient credits" into the description makes the next
	// run treat the billing error as the work.
	return &v2.TaskItem{Index: st.Index, Title: title, Description: title}
}

// codingPlanTasksForResume keeps dependencies that are also being re-run and
// drops dependencies that already passed. Those passed steps are not in this
// subset, so leaving the edge in place would make TaskRunner skip the step.
func codingPlanTasksForResume(tasks []*v2.TaskItem) []*v2.TaskItem {
	out := cloneV2TaskItems(tasks)
	present := make(map[int]bool, len(out))
	for _, task := range out {
		if task != nil && task.Index > 0 {
			present[task.Index] = true
		}
	}
	for _, task := range out {
		if task == nil || len(task.DependsOn) == 0 {
			continue
		}
		kept := make([]int, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if present[dep] {
				kept = append(kept, dep)
			}
		}
		task.DependsOn = kept
	}
	return out
}

func (h *IMMessageHandler) reopenIncompleteCodingPlanSteps(userID string, mem stickyCodingWorkbenchMemory, execute []*v2.TaskItem) {
	if h == nil || len(execute) == 0 {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	want := make(map[int]struct{}, len(execute))
	for _, task := range execute {
		if task != nil && task.Index > 0 {
			want[task.Index] = struct{}{}
		}
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(stored *stickyCodingWorkbenchMemory) {
		if len(stored.StepStatuses) == 0 && len(mem.StepStatuses) > 0 {
			stored.StepStatuses = append([]codingWorkbenchStepStatus(nil), mem.StepStatuses...)
		}
		if strings.TrimSpace(stored.ExecutionPlan) == "" && strings.TrimSpace(mem.ExecutionPlan) != "" {
			stored.ExecutionPlan = mem.ExecutionPlan
		}
		// This reopen is itself the evidence the checklist already ran.
		stored.PlanRunStarted = true
		if codingGenericContinueRestatement(stored.RequirementRestatement) {
			stored.RequirementRestatement = truncateRunesForSubAgent(strings.TrimSpace(mem.RequirementRestatement), 400)
		}
		now := time.Now().Unix()
		for i := range stored.StepStatuses {
			if _, ok := want[stored.StepStatuses[i].Index]; !ok {
				continue
			}
			stored.StepStatuses[i].Status = codingStepPending
			stored.StepStatuses[i].Summary = ""
			stored.StepStatuses[i].VerifyCmd = ""
			stored.StepStatuses[i].VerifyOK = nil
			stored.StepStatuses[i].UpdatedUnix = now
		}
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

// codingPlanOutlineForPrompt is the full plan shown inside a resumed step.
// Execution may only include the unfinished tail, but the prompt still needs
// the original T index and plan length.
func codingPlanOutlineForPrompt(mem stickyCodingWorkbenchMemory, execute []*v2.TaskItem) (outline []*v2.TaskItem, total int) {
	parsed := parseCodingWorkbenchPlan(mem.ExecutionPlan)
	byIndex := make(map[int]*v2.TaskItem, len(parsed))
	for _, task := range parsed {
		if task != nil && task.Index > 0 {
			byIndex[task.Index] = task
		}
	}
	// Statuses are the checklist the user still sees. Parsed steps supply
	// the original brief when the heading survived; a truncated plan must
	// not drop the earlier titles from the resumed prompt.
	if len(mem.StepStatuses) > 0 {
		outline = make([]*v2.TaskItem, 0, len(mem.StepStatuses))
		seen := make(map[int]bool, len(mem.StepStatuses))
		for _, st := range mem.StepStatuses {
			seen[st.Index] = true
			if task, ok := byIndex[st.Index]; ok {
				outline = append(outline, task)
				continue
			}
			outline = append(outline, taskFromCodingStepStatus(st))
		}
		for _, task := range parsed {
			if task == nil || seen[task.Index] {
				continue
			}
			outline = append(outline, task)
		}
	} else if len(parsed) > 0 {
		outline = parsed
	} else {
		outline = execute
	}
	sort.SliceStable(outline, func(i, j int) bool {
		if outline[i] == nil || outline[j] == nil {
			return outline[i] != nil
		}
		return outline[i].Index < outline[j].Index
	})
	outline = codingPlanOutlineMarkPassed(outline, mem.StepStatuses)
	total = len(outline)
	if total < 1 {
		total = 1
	}
	return outline, total
}

// codingPlanOutlineMarkPassed labels steps that already passed. The resumed
// prompt lists the whole checklist; without this mark the model redoes them.
func codingPlanOutlineMarkPassed(outline []*v2.TaskItem, statuses []codingWorkbenchStepStatus) []*v2.TaskItem {
	if len(outline) == 0 || len(statuses) == 0 {
		return outline
	}
	passed := make(map[int]bool, len(statuses))
	for _, st := range statuses {
		if st.Index > 0 && strings.TrimSpace(st.Status) == codingStepPassed {
			passed[st.Index] = true
		}
	}
	if len(passed) == 0 {
		return outline
	}
	for i, task := range outline {
		if task == nil || !passed[task.Index] {
			continue
		}
		clone := *task
		title := strings.TrimSpace(clone.Title)
		if title == "" {
			title = fmt.Sprintf("T%d", clone.Index)
		}
		if !strings.Contains(title, "already done") {
			clone.Title = title + " (already done)"
		}
		outline[i] = &clone
	}
	return outline
}

// codingPlanReportedStepTotal is the plan length shown after a turn.
// A resume executes only the unfinished tail, but the checklist is still
// the original plan.
func codingPlanReportedStepTotal(planned bool, executing, recorded int) int {
	if planned && recorded > executing {
		return recorded
	}
	if executing > 0 {
		return executing
	}
	if recorded > 0 {
		return recorded
	}
	return 0
}

// codingPlanResumeStopNote replaces the previous step's own summary on a
// resume. That summary often says the project is already finished, or it
// repeats the credit error. Either one makes the next step no-op or spend
// another large prompt on text that is not the task.
func codingPlanResumeStopNote() string {
	return "The previous plan step stopped before the plan finished. Execute the current unfinished step. Ignore any earlier claim that the whole project is already done."
}

// codingPlanResumePromptMemory is a prompt-only copy. The stored plan and the
// last failure summary stay on disk; this copy just keeps them out of the
// next step's context because the step outline already carries the checklist.
func codingPlanResumePromptMemory(mem stickyCodingWorkbenchMemory) stickyCodingWorkbenchMemory {
	mem.ExecutionPlan = ""
	mem.LastSummary = codingPlanResumeStopNote()
	plan := strings.TrimSpace(mem.SessionPlan)
	rest := strings.TrimSpace(mem.RequirementRestatement)
	last := strings.TrimSpace(mem.LastUserText)
	// "继续" is how this turn was started. Leaving it as the previous request
	// makes the step look like a new one-line ask.
	if codingWorkbenchContinueCue(last) {
		mem.LastUserText = ""
		last = ""
	}
	// The short-follow-up restatement says "only the change you just named"
	// and that change is the word 继续. Drop it so the unfinished step runs.
	if codingGenericContinueRestatement(rest) {
		mem.RequirementRestatement = ""
		rest = ""
	}
	// Session plan is already the goal line. Repeating it as the restatement
	// and the previous request spends credits without adding a step brief.
	if plan != "" && rest == plan {
		mem.RequirementRestatement = ""
	}
	if plan != "" && last == plan {
		mem.LastUserText = ""
	}
	return mem
}

// codingGenericContinueRestatement is the host paraphrase of a short
// follow-up. It names no requirement. Using it as the resume goal makes the
// next step re-derive the work.
func codingGenericContinueRestatement(text string) bool {
	return strings.Contains(strings.TrimSpace(text), "上继续你这次提出的改动")
}

// codingPlanResumeGoal is the original request, not the word "继续".
func codingPlanResumeGoal(userText string, mem stickyCodingWorkbenchMemory) string {
	for _, candidate := range []string{mem.SessionPlan, mem.RequirementRestatement, mem.LastUserText} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || codingWorkbenchContinueCue(candidate) || codingGenericContinueRestatement(candidate) {
			continue
		}
		return candidate
	}
	return strings.TrimSpace(userText)
}

// codingPlanResumeUnderstanding replaces the host paraphrase of a bare
// continue. That sentence names no requirement, and the checklist UI shows
// it as 需求理解. The original goal takes its place. Clear it when no goal
// survived. A specific restatement is left as stored.
func codingPlanResumeUnderstanding(userText string, mem stickyCodingWorkbenchMemory) (text string, replace bool) {
	if !codingGenericContinueRestatement(mem.RequirementRestatement) {
		return "", false
	}
	goal := strings.TrimSpace(codingPlanResumeGoal(userText, mem))
	if goal == "" || goal == strings.TrimSpace(userText) || codingWorkbenchContinueCue(goal) || codingGenericContinueRestatement(goal) {
		return "", true
	}
	return goal, true
}

// codingChecklistResumePlanMarkdown records an agent checklist as the
// orchestrator plan. StepStatuses alone are not owned: the next todo_write
// is allowed to replace them, so a continue that just restored the list
// loses it again inside the resumed turn. A checklist shorter than the
// multi-step minimum stays a single task and may still mirror inner todos.
// Titles that are only the todo id are not briefs. applyTodoWrite stores a
// missing brief as that id, and owning those rows would block the todo_write
// that replaces "1" with the real steps.
func codingChecklistResumePlanMarkdown(userText string, mem stickyCodingWorkbenchMemory) string {
	if strings.TrimSpace(mem.ExecutionPlan) != "" || len(mem.StepStatuses) < codingWorkbenchPlanMinTasks {
		return ""
	}
	steps := append([]codingWorkbenchStepStatus(nil), mem.StepStatuses...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	tasks := make([]*v2.TaskItem, 0, len(steps))
	real := 0
	for _, st := range steps {
		title := strings.TrimSpace(st.Title)
		if st.Index <= 0 || title == "" {
			continue
		}
		if !codingStepTitleIsIdentityToken(title, st.Index) {
			real++
		}
		tasks = append(tasks, taskFromCodingStepStatus(st))
	}
	if real < codingWorkbenchPlanMinTasks {
		return ""
	}
	return formatCodingWorkbenchPlanMarkdown(codingPlanResumeGoal(userText, mem), tasks)
}

// codingStepTitleIsIdentityToken reports a checklist title that is only the
// todo id. A missing brief is stored as that id: the 1-based row, a T-label,
// or a numeric id that no longer matches the row after a merge.
func codingStepTitleIsIdentityToken(title string, index int) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true
	}
	if index > 0 {
		n := strconv.Itoa(index)
		if title == n || strings.EqualFold(title, "T"+n) {
			return true
		}
	}
	body := title
	if len(body) > 1 && (body[0] == 'T' || body[0] == 't') {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	// "继续" after a credit stop (or any hard stop) must keep the plan that
	// already ran and execute only the steps that did not pass. A new
	// single-task turn would wipe that checklist and start requirement
	// understanding over.
	if execute, ok := selectIncompleteCodingPlanTasks(userText, sessionMem); ok {
		log.Printf("[coding-plan] resume incomplete plan user=%s steps=%d", userID, len(execute))
		if md := codingChecklistResumePlanMarkdown(userText, sessionMem); md != "" {
			sessionMem.ExecutionPlan = md
		}
		if text, replace := codingPlanResumeUnderstanding(userText, sessionMem); replace {
			sessionMem.RequirementRestatement = text
		}
		h.reopenIncompleteCodingPlanSteps(userID, sessionMem, execute)
		if onProgress != nil {
			onProgress(fmt.Sprintf("继续未完成的计划：剩余 %d 步", len(execute)))
		}
		return execute, strings.TrimSpace(sessionMem.ExecutionPlan), true
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
		if ep := truncateRunesForSubAgent(strings.TrimSpace(executionPlan), codingWorkbenchExecutionPlanPersistRunes); ep != "" {
			mem.ExecutionPlan = ep
		}
		if seed := truncateRunesForSubAgent(strings.TrimSpace(sessionPlanIfEmpty), 800); seed != "" {
			if strings.TrimSpace(mem.SessionPlan) == "" {
				mem.SessionPlan = seed
			}
		}
	})
}
