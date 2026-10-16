package codingagent

// reviewer_shell.go implements the read-only validation shell vocabulary for
// the reviewer coding role (review P2-1). A reviewer's allow-list may admit
// "bash" so it can run builds, tests, and git inspection, but that bash must
// never become a general write capability: the whitelist below admits only
// known read-only inspection commands and project validation entry points,
// and rejects output redirection, process substitution, heredocs, command
// substitution, and interpreters outright. Plain input redirection (< file)
// is allowed: it only opens a file for reading.
//
// Fourteenth hardening pass (token vs shell tokenization divergence): the
// gate matches raw tokens but the shell executes NORMALIZED ones. Quoting,
// $ expansion, brace expansion, and backslash escapes all rewrite words
// after this gate matches them, so the match inputs are mirrored here:
// every token is quote/escape-stripped exactly where the shell strips them,
// and any token the shell would still rewrite ($VAR, ${...}, $'...', {a,b})
// is rejected outright. Path-qualified heads (./git) are denied too —
// path.Base() on them would alias an arbitrary local binary to a vocabulary
// name.
//
// Scoping note: running project validation (go test, npm run, make, pytest)
// executes project-controlled code that can itself write files (TestMain,
// conftest.py, package.json scripts). That is inherent to validation and is
// NOT what this gate defends against. The gate defends against the MODEL
// using the reviewer's shell as a write/edit channel.
//
// Nineteenth pass (host-identity alignment): the gate models the host as
// `sh -c`/`bash -c`. That is EXACT on Unix and on the remote ssh transport
// (sh -lc), but on Windows the local dispatch (guiapp
// windowsCodingBashInvocation) routes by command shape: unquoted "||"/">nul"
// or MSVC compile patterns go to cmd.exe, unix-inspect inventory
// (ls/file/stat/cd) is rewritten into an equivalent PowerShell script by a
// pattern adapter, and everything else runs under PowerShell 5.1. The gate
// stays fail-closed across that divergence: cmd.exe %VAR% expansion is denied
// in combination with its only gate-survivable trigger (unquoted "||"); PS
// metacharacters ($ and backtick) are already denied; PS aliases for
// vocabulary heads (sort/diff/ps) diverge functionally but expose no
// write/exec channel; MSVC patterns are unreachable (cl/vcvars are not in the
// vocabulary); and the bash-only "FOO=x cmd" env-prefix syntax is
// allowed-but-inert on Windows (it fails to parse there), never a bypass.

import (
	"fmt"
	"path"
	"strings"
)

// reviewerShellReadOnlyHeads are single-token read-only programs. None of
// them accepts an in-band "write a file" action, and interpreters are
// deliberately absent (python/node/perl/awk can write via -c/flags).
var reviewerShellReadOnlyHeads = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"file": true, "stat": true, "du": true, "df": true,
	"grep": true, "rg": true, "diff": true, "sort": true, "uniq": true,
	"comm": true, "cmp": true, "echo": true, "printf": true,
	"which": true, "date": true, "uname": true, "ps": true,
	"basename": true, "dirname": true, "realpath": true, "readlink": true,
	"pwd": true, "id": true, "hostname": true, "tree": true,
	// Pure navigation: cd changes the working directory and cannot write or
	// execute anything by itself. Remote workflows habitually lead with
	// "cd <repo> && go test ./..." — denying cd would reject the segment
	// before the meaningful command is ever checked. Known residual
	// (documented, not denied): `cd <abs-dir> && <validation cmd>` runs that
	// command against a model-chosen directory — the compositional residual
	// of the go -C / npm --prefix / cargo --manifest-path denials, same
	// staging-requires-a-prior-write-outside-this-gate class as git -C.
	"cd": true,
	// find traversal/matching stays allowed; its write primaries
	// (-delete/-exec/...) are rejected in reviewerShellSegmentAllowed.
	"find":   true,
	"md5sum": true, "sha1sum": true, "sha256sum": true,
	"true": true, "false": true,
	// Project validation entry points. These run project recipes/scripts
	// (see the scoping note above) but expose no shell grammar of their own.
	"make":   true,
	"pytest": true,
}

// reviewerShellSubcommandHeads require the first non-flag argument to be one
// of the listed read-only subcommands.
var reviewerShellSubcommandHeads = map[string]map[string]bool{
	"git": {
		"status": true, "diff": true, "log": true, "show": true,
		"rev-parse": true, "describe": true, "blame": true, "shortlog": true,
		"ls-files": true, "ls-remote": true, "cat-file": true, "rev-list": true,
		"reflog": true, "count-objects": true, "grep": true,
		"merge-base": true, "cherry": true,
	},
	// build writes only the build cache outside the workspace source tree;
	// generate/fmt/mod/get/install write source or artifacts and stay denied.
	"go":    {"test": true, "vet": true, "build": true, "list": true, "doc": true, "version": true, "env": true},
	"cargo": {"test": true, "check": true},
	// npm run <script> executes project-defined scripts (trusted project
	// config); install/ci/publish and npx stay denied.
	"npm": {"test": true, "run": true},
}

// reviewerShellFindWriteFlags are find(1) primary actions that touch the
// filesystem. Plain traversal/matching remains allowed.
var reviewerShellFindWriteFlags = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true,
	"-ok": true, "-okdir": true,
	"-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true,
}

// reviewerShellDeniedSubFlags are per head/subcommand flags that turn an
// otherwise read-only validation form into a write or arbitrary-exec vector
// (hardening pass, 2026-09-20):
//   - go env -w/-u persist global go environment config;
//   - go build/test -o (and go test -c) emit artifacts at a model-chosen path;
//   - go test -coverprofile/-cpuprofile/... write profile files at a
//     model-chosen path; -outputdir relocates them;
//   - go test -exec / go vet -vettool run an arbitrary program;
//   - pytest --junitxml/--html/--resultlog write report files at a
//     model-chosen path.
//
// Flags are matched as exact token or "flag=" prefix, so "-count=1" is never
// confused with "-c".
var reviewerShellDeniedSubFlags = map[string]map[string]bool{
	"go/build": {"-o": true, "-toolexec": true, "-overlay": true, "-modfile": true},
	"go/test": {
		"-o": true, "-c": true, "-exec": true, "-outputdir": true,
		"-coverprofile": true, "-cpuprofile": true, "-memprofile": true,
		"-mutexprofile": true, "-blockprofile": true, "-trace": true,
		// seventeenth pass: -toolexec wraps EVERY toolchain invocation in a
		// model-chosen program (arbitrary exec), and -overlay substitutes
		// project source files with model-staged content at build time —
		// both compile staged code while the command still reads "go test"
		"-toolexec": true, "-overlay": true,
		// eighteenth pass: -modfile points go at an alternate go.mod whose
		// replace directives can swap project modules for staged code — the
		// token twin of the GOWORK/GOENV env denials
		"-modfile": true,
	},
	"go/vet": {"-vettool": true, "-cpuprofile": true, "-memprofile": true, "-trace": true, "-toolexec": true, "-overlay": true, "-modfile": true},
	"go/env": {"-w": true, "-u": true},
	"pytest": {"--junitxml": true, "--resultlog": true, "--html": true, "--update-snapshots": true, "--snapshot-update": true},
	// fifteenth pass (exec/config channels left in the token layer):
	//   - make -f/--file/--makefile runs a model-chosen makefile, i.e. recipes
	//     the model authored — arbitrary execution, not project config;
	//   - cargo --config accepts TOML inline, so build.rustc-wrapper /
	//     target.<triple>.runner hand cargo an arbitrary program to spawn;
	//   pytest denials additionally cover argparse prefix abbreviations
	//   (reviewerShellFlagDenied): "--junit=x" uniquely resolves to
	//   --junitxml, defeating exact-token denial.
	"make":        {"-f": true, "--file": true, "--makefile": true, "-I": true, "--include-dir": true, "--eval": true, "--load-plugins": true, "-t": true, "--touch": true},
	"cargo/test":  {"--config": true, "--manifest-path": true},
	"cargo/check": {"--config": true, "--manifest-path": true},
	// sixteenth pass (read-only heads' own write/exec flags, audited per
	// head after fifteen rounds focused on subcommand heads):
	//   - sort -o/--output overwrites a model-chosen path, and
	//     --compress-program executes the named program on temp files;
	//   - date -s/--set mutates the system clock;
	//   - rg --pre runs a model-chosen preprocessor command on every file;
	//   - make additionally gets --eval (makefile code the model authored,
	//     not project config), --load-plugins (dlopen of a model-chosen
	//     .so), and -I/--include-dir (resolve the project's include
	//     directives from a staged directory).
	"sort": {"-o": true, "--output": true, "--compress-program": true},
	"date": {"-s": true, "--set": true},
	"rg":   {"--pre": true},
	// eighteenth pass (remaining write channels of the bare read-only heads,
	// plus the flag twins of already-denied env):
	//   - tree -o writes the listing to a model-chosen file;
	//   - file -C/--compile writes a compiled magic database;
	//   - git --paginate (anywhere) and pre-subcommand -p spawn a pager —
	//     the flag twins of the denied PAGER/GIT_PAGER env (post-subcommand
	//     -p is git's patch flag and stays allowed);
	//   - go -workfile is the flag twin of the GOWORK env denial;
	//   - go test/build/vet -modfile points go at an alternate go.mod whose
	//     replace directives can swap project modules for staged code (the
	//     token twin of the GOWORK/GOENV denials).
	"tree": {"-o": true},
	"file": {"-C": true, "--compile": true},
	// seventeenth pass (flag twins of already-denied env / staged targets):
	//   - make -t/--touch touches every target's mtime in the workspace
	//     (the touch head is denied, but this form would bypass it);
	//   - cargo --manifest-path points cargo at a model-staged crate (the
	//     token twin of the --config denial);
	//   - go -C (see the go branch in reviewerShellSegmentAllowed) and
	//     -toolexec/-overlay above.
	// Known residual (documented, not denied): go test/build -ldflags=-extld
	// selects the cgo external linker — an exec channel that only fires on
	// cgo links; the flag is too common for version-injection builds to deny.
}

// reviewerShellAbbreviationHeads are programs whose long-option parsing
// accepts unambiguous abbreviations (GNU getopt_long: make/sort/date;
// python argparse: pytest). "sort --compress=x" resolves to
// --compress-program, so a token that is a strict prefix of a denied long
// flag must be refused too. Short flags never abbreviate; go's flag package,
// git, npm, cargo (clap, inference off), and rg do not abbreviate either.
var reviewerShellAbbreviationHeads = map[string]bool{
	"pytest": true, "make": true, "sort": true, "date": true,
}

// reviewerShellNpmWriteFlags are npm passthrough arguments (after "--") that
// flip a project script into write mode: the script itself is trusted project
// config, but the model-chosen arguments are not ("npm run lint -- --fix"
// makes eslint rewrite files; "npm test -- -u" regenerates snapshots). Both
// bare ("--fix") and value forms ("--fix=true") are matched.
var reviewerShellNpmWriteFlags = map[string]bool{
	"--fix": true, "--write": true, "--update": true, "--update-snapshots": true,
	"-u": true, "-i": true, "--in-place": true, "--save": true,
}

// reviewerShellNpmPassthroughAllowed rejects write-mode flags in the "--"
// passthrough tail of npm run/test. Value forms ("--fix=x", "--write=true")
// hit the same map: the name before "=" is what flips the script's mode.
func reviewerShellNpmPassthroughAllowed(segment string, fields []string) (bool, string) {
	for i, tok := range fields {
		if tok != "--" {
			continue
		}
		for _, arg := range fields[i+1:] {
			base := arg
			if eq := strings.Index(arg, "="); eq > 0 {
				base = arg[:eq]
			}
			if reviewerShellNpmWriteFlags[base] {
				return false, fmt.Sprintf("reviewer shell rejected %q: npm passthrough %s flips the script into write mode", segment, arg)
			}
		}
	}
	return true, ""
}

// reviewerShellNpmPrefixAllowed restricts --prefix (and its short alias -C,
// seventeenth pass) to project-relative paths. npm executes the package.json
// scripts of the prefix directory; an absolute or parent-escaping value lets
// the model point npm at a staged package (model-authored scripts), which is
// outside the project-config caveat.
func reviewerShellNpmPrefixAllowed(segment string, fields []string) (bool, string) {
	for i := 1; i < len(fields); i++ {
		tok := fields[i]
		var value string
		switch {
		case tok == "--prefix" || tok == "-C":
			if i+1 >= len(fields) {
				return false, fmt.Sprintf("reviewer shell rejected %q: %s needs a value", segment, tok)
			}
			value = fields[i+1]
			i++
		case strings.HasPrefix(tok, "--prefix="):
			value = tok[len("--prefix="):]
		case strings.HasPrefix(tok, "-C="):
			value = tok[len("-C="):]
		default:
			continue
		}
		if value != "." && !strings.HasPrefix(value, "./") {
			return false, fmt.Sprintf("reviewer shell rejected %q: --prefix may only point inside the project (./...)", segment)
		}
	}
	return true, ""
}

// reviewerShellGitConfigKeys allow-lists `git -c key=value` overrides. Config
// is NOT skipped blindly: keys like core.pager, core.fsmonitor, diff.external,
// or credential.helper make git itself spawn a program, so unknown or
// exec-capable keys fail closed. The listed keys only affect formatting of
// read-only output.
var reviewerShellGitConfigKeys = map[string]bool{
	"core.quotepath": true, "core.autocrlf": true, "core.safecrlf": true, "core.eol": true,
	"color.ui": true, "color.diff": true, "color.status": true,
	"diff.algorithm": true, "diff.noprefix": true, "diff.indentheuristic": true,
	"diff.context": true, "diff.renameLimit": true, "diff.mnemonicPrefix": true,
	"log.follow": true, "log.date": true, "log.abbrevCommit": true,
	"blame.date": true, "blame.coloring": true,
	"grep.lineNumber": true, "grep.patternType": true, "grep.fullName": true, "grep.column": true,
	"safe.directory": true,
}

// reviewerShellInvocationAllowed is the execution-time companion of admitting
// "bash" for a read-only reviewer role. It fails closed: every segment of the
// command (chain, pipe, and line separated) must be inside the vocabulary.
func reviewerShellInvocationAllowed(args map[string]interface{}) (bool, string) {
	raw, ok := args["command"]
	if !ok {
		return false, "reviewer shell command is missing"
	}
	command, _ := raw.(string)
	if strings.TrimSpace(command) == "" {
		return false, "reviewer shell command is empty"
	}
	// nineteenth pass: a command carrying an unquoted "||" dispatches to
	// cmd.exe on Windows hosts, where %VAR% percent expansion rewrites words
	// after this gate matches them — the % twin of the $ rejection. The model
	// cannot introduce environment variables on that path ("FOO=x cmd" is
	// bash-only syntax that fails on cmd.exe and PowerShell alike), so only
	// pre-existing vars can expand; the combination is denied fail-closed.
	// cmd.exe needs TWO percent signs to attempt an expansion, so a lone %
	// (printf "%s") stays allowed.
	if strings.Contains(stripReviewerShellQuotes(command), "||") && strings.Count(command, "%") >= 2 {
		return false, "reviewer shell rejected %VAR% expansion: an || chain dispatches to cmd.exe on Windows, where percent expansion rewrites words past this gate"
	}
	if ok, reason := reviewerShellValidationOrChainDenied(command); !ok {
		return false, reason
	}
	groups := splitReviewerShellPipeGroups(command)
	if ok, reason := reviewerShellValidationPipeDenied(groups); !ok {
		return false, reason
	}
	for _, segment := range groups {
		for _, part := range segment {
			if ok, reason := reviewerShellSegmentAllowed(part); !ok {
				return false, reason
			}
		}
	}
	return true, ""
}

// ReviewerShellGateAllows exposes the whitelist verdict for one reviewer bash
// command line. It exists so a host's cross-layer contract test can pin the
// layer agreement directly: every command shape the host's evidence gate
// treats as failure suppression — a validation command chained with "||",
// "&", or a non-verification "&&"/";" tail, or piped into another program —
// must already be refused here, at gate time, not merely flagged afterwards
// (twentieth pass: the nineteenth-pass alignment made this true; the export
// makes it testable against future vocabulary drift on either side).
func ReviewerShellGateAllows(args map[string]interface{}) (bool, string) {
	return reviewerShellInvocationAllowed(args)
}

// reviewerShellWordRewriteRisk walks one raw token the way the shell's word
// processing would, and reports the first construct that makes the token's
// post-expansion value differ from what a plain gate-side string match saw:
//   - unquoted or double-quoted $ expansion ($VAR, ${...}, $'...', $"..."):
//     "go test ${IFS}-coverprofile=x" delivers -coverprofile=x to go while
//     the gate matched a token that does not start with the flag;
//   - unquoted brace expansion {a,b}: bash rewrites one word into two.
//
// Single-quoted text, backslash escapes, and a $ that ends the word are
// literal in the shell too, so they stay allowed ("rg 'error$'" keeps
// working). Known residual: unquoted glob expansion (go build * can hand go
// a filename like "-o" if such a file exists in cwd) is documented, not
// denied — staging it requires a prior write outside this gate.
func reviewerShellWordRewriteRisk(tok string) string {
	inSingle, inDouble, escaped := false, false, false
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if escaped {
			escaped = false
			continue
		}
		switch {
		case c == '\\' && !inSingle:
			escaped = true
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '$' && !inSingle:
			if i == len(tok)-1 {
				break // a $ ending the word is literal
			}
			if inDouble && tok[i+1] == '"' {
				break // $ before the closing double quote is literal
			}
			return "shell expansion rewrites this word after the gate matches it"
		case c == '{' && !inSingle && !inDouble:
			if strings.Contains(tok[i+1:], ",") {
				return "brace expansion rewrites this word after the gate matches it"
			}
		}
	}
	return ""
}

// reviewerShellNormalizeToken removes the characters the shell itself removes
// during quote removal and escape processing, so gate-side matching operates
// on the same word the program will receive: "git" and \git both arrive as
// git. Divergences the shell would introduce BEYOND this (expansion, brace
// rewriting) are refused by reviewerShellWordRewriteRisk before this runs.
func reviewerShellNormalizeToken(tok string) string {
	tok = strings.ReplaceAll(tok, "'", "")
	tok = strings.ReplaceAll(tok, "\"", "")
	return strings.ReplaceAll(tok, "\\", "")
}

// reviewerShellNormalizedFields is the tokenization every segment-level check
// must use: shell-word splitting, a rewrite-risk refusal for words the shell
// would still transform, then quote/escape normalization. Tokens that
// normalize to empty are dropped (the shell drops empty words too).
func reviewerShellNormalizedFields(segment string) ([]string, bool) {
	raw := strings.Fields(segment)
	fields := make([]string, 0, len(raw))
	for _, tok := range raw {
		if reason := reviewerShellWordRewriteRisk(tok); reason != "" {
			return nil, false
		}
		if tok = reviewerShellNormalizeToken(tok); tok != "" {
			fields = append(fields, tok)
		}
	}
	return fields, true
}

// reviewerShellTimeoutCommandFields resolves the command a `timeout` wrapper
// runs. GNU timeout accepts flags before the duration: -k/-s/--kill-after/
// --signal consume a separate value, boolean -v/--foreground/--preserve-status
// and long=value forms carry their own. The old blind two-token strip
// over-denied "timeout -k 5 10 go test" (head resolved as "5") — seventeenth
// pass usability fix. Unknown flag shapes degrade fail-closed: whatever
// remains is still checked as a command segment. GNU getopt_long also accepts
// unambiguous abbreviations of the value-consuming long flags ("--kill" /
// "--s"), so their strict prefixes consume the value too — without this the
// duration resolved as the head and the wrapped command was over-denied
// (twenty-first pass availability fix).
func reviewerShellTimeoutCommandFields(fields []string) []string {
	i := 1
	for i < len(fields) && strings.HasPrefix(fields[i], "-") && fields[i] != "--" {
		i++
		switch tok := fields[i-1]; {
		case tok == "-k" || tok == "-s" || tok == "--kill-after" || tok == "--signal":
			i++ // these flags consume a separate value
		case !strings.Contains(tok, "=") &&
			(strings.HasPrefix("--kill-after", tok) || strings.HasPrefix("--signal", tok)):
			i++ // unambiguous GNU abbreviation of a value-consuming long flag
		}
	}
	if i >= len(fields) {
		return nil // no duration/command left
	}
	return fields[i+1:] // fields[i] is the duration; the command follows
}

// reviewerShellFdDupSentinel protects "2>&1" from the bare-& separator pass
// below and is restored as-is: stderr merges into stdout, so verification
// evidence still captures the full output.
const reviewerShellFdDupSentinel = "\x00FD\x00"

// reviewerShellFdDupErrSentinel shields "1>&2" from the & pass so the token
// reaches the segment check intact — where it is DENIED: stdout drained into
// stderr makes verification output look empty to the evidence capture.
const reviewerShellFdDupErrSentinel = "\x00FDE\x00"

// splitReviewerShellSegments breaks a command into independently checked
// segments. Quoted text is NOT respected on purpose: splitting inside a quote
// can only produce extra segments, and every segment must pass on its own, so
// the failure direction is always closed.
//
// Separators handled: newline, &&, ||, |, and BARE &. The last one is critical:
// the host runs the command via sh -c / bash -c, where "&" is a command
// separator (background), so "ls & rm -rf /" is TWO commands, not one.
// "&&"/"||" are replaced first so they are not double-split; "2>&1"/"1>&2"
// are shielded so the & pass does not tear the token apart — "2>&1" then
// survives the redirect check (evidence-safe plumbing) while "1>&2" is denied
// downstream (it drains verification output out of the captured stream).
// Any other use of & ("&>", "&>>" redirects, "|&") degrades into a segment
// whose redirect characters get rejected downstream — the closed direction.
func splitReviewerShellSegments(command string) []string {
	groups := splitReviewerShellPipeGroups(command)
	segments := make([]string, 0, 4)
	for _, group := range groups {
		segments = append(segments, group...)
	}
	return segments
}

// splitReviewerShellPipeGroups applies the same separator normalization as
// splitReviewerShellSegments but keeps each |-joined run together, so callers
// can reason about pipe topology (what is piped into what).
func splitReviewerShellPipeGroups(command string) [][]string {
	command = strings.ReplaceAll(command, "\r\n", ";")
	command = strings.ReplaceAll(command, "\n", ";")
	command = strings.ReplaceAll(command, "&&", ";")
	command = strings.ReplaceAll(command, "||", ";")
	// twenty-first pass: "|&" is bash's stdout+stderr pipe ("a |& b" ==
	// "a 2>&1 | b") — a PIPE, not two independent commands. Tearing it into
	// two single-segment groups let "go test ./... |& tail -20" escape the
	// validation-pipe denial (both halves pass segment checks on their own)
	// while the evidence gate flags the pipe post-execution; normalizing to
	// "|" keeps the pipe-group view intact, and the malicious-payload
	// separator matrix stays green because both halves are still checked.
	command = strings.ReplaceAll(command, "|&", "|")
	command = strings.ReplaceAll(command, "2>&1", reviewerShellFdDupSentinel)
	command = strings.ReplaceAll(command, "1>&2", reviewerShellFdDupErrSentinel)
	command = strings.ReplaceAll(command, "&", ";")
	command = strings.ReplaceAll(command, reviewerShellFdDupSentinel, "2>&1")
	command = strings.ReplaceAll(command, reviewerShellFdDupErrSentinel, "1>&2")
	groups := make([][]string, 0, 4)
	for _, semi := range strings.Split(command, ";") {
		group := make([]string, 0, 2)
		for _, pipe := range strings.Split(semi, "|") {
			segment := strings.TrimSpace(pipe)
			if segment != "" {
				group = append(group, segment)
			}
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// reviewerShellValidationOrChainDenied rejects chain shapes that mask a
// verification command's exit status, mirroring the evidence capture's
// post-execution verdicts (suppressesVerificationFailure) so the layers agree
// instead of running the command and then marking it (nineteenth pass; the
// pipe denial below started this alignment):
//   - "||" after a verification head: the chain reports the FALLBACK's status
//     when the verification fails ("go test ./... || echo ok" always looks
//     successful);
//   - "&" after a verification head: backgrounding detaches the status;
//   - "&&" or ";" after a verification head: allowed ONLY when the tail is
//     itself a verification command ("go test ./... && go vet ./..." chains
//     evidence) — a non-verification tail ("go test && git status") reports
//     the tail's status and is flagged by the evidence gate.
//
// A fallback BEFORE the verification ("git status || go test ./...") keeps
// the verification's real exit status and stays allowed.
//
// twenty-first pass: tokenization follows the host evidence gate's shell
// tokenizer, which carves "&"/"|"/";" out of words wherever they appear —
// "go test ./...||echo ok" chains in the shell exactly like the spaced form.
// Matching boundaries by exact strings.Fields token let the glued forms
// escape this denial while the evidence gate flagged them post-execution.
// "2>&1"/"1>&2" are fd duplications, not separators: shielded from the
// split (their verdicts live in the segment check — 2>&1 allowed, 1>&2
// denied) and restored afterwards.
func reviewerShellValidationOrChainDenied(command string) (bool, string) {
	fields := reviewerShellChainFields(command)
	type chainPart struct {
		segment  []string
		boundary string // "", "|", "||", "&&", "&", ";"
	}
	parts := make([]chainPart, 0, 3)
	var current []string
	for _, tok := range fields {
		switch tok {
		case "|", "||", "&&", "&", ";":
			parts = append(parts, chainPart{segment: current, boundary: tok})
			current = nil
		default:
			current = append(current, tok)
		}
	}
	parts = append(parts, chainPart{segment: current})
	sawVerification := false
	for i, part := range parts {
		if reviewerShellSegmentIsValidationHead(part.segment) {
			sawVerification = true
		}
		if !sawVerification || part.boundary == "" {
			continue
		}
		hasTail := i+1 < len(parts) && len(parts[i+1].segment) > 0
		if !hasTail {
			continue
		}
		switch part.boundary {
		case "||", "&":
			return false, fmt.Sprintf("reviewer shell rejected %q: %q after a verification command masks its exit status from verification evidence — run the verification bare", strings.Join(fields, " "), part.boundary)
		case "&&", ";":
			if !reviewerShellSegmentIsValidationHead(parts[i+1].segment) {
				return false, fmt.Sprintf("reviewer shell rejected %q: the %q tail after a verification command reports the tail's status, hiding the verification result — chain another verification command instead", strings.Join(fields, " "), part.boundary)
			}
		}
	}
	return true, ""
}

// reviewerShellChainFields tokenizes a command line the way the shell (and
// the host evidence gate's tokenizer) does for chain boundaries: "&", "|",
// and ";" are their own words wherever they appear, so the glued
// "go test ./...||echo ok" yields the same boundary sequence as the spaced
// form. Compound operators are spaced before their single-character parts
// ("&&"/"||" first), "|&" reduces to "|" (stderr-merge pipe — the trailing &
// belongs to the pipe, not to a background dispatch), and the fd
// duplications "2>&1"/"1>&2" are shielded from the split: they are plumbing
// words, and their allow/deny verdicts live in the segment check.
func reviewerShellChainFields(command string) []string {
	command = strings.ReplaceAll(command, "2>&1", "\x00FDIN\x00")
	command = strings.ReplaceAll(command, "1>&2", "\x00FDOUT\x00")
	command = strings.ReplaceAll(command, "|&", " | ")
	// compound operators are sentinel-protected BEFORE the single-character
	// pass spaces them out — otherwise "&&" becomes "& &" and "||" becomes
	// "| |" (two neutral boundaries), erasing the chain semantics.
	command = strings.ReplaceAll(command, "&&", "\x00OPA\x00")
	command = strings.ReplaceAll(command, "||", "\x00OPO\x00")
	command = strings.ReplaceAll(command, "&", " & ")
	command = strings.ReplaceAll(command, "|", " | ")
	command = strings.ReplaceAll(command, ";", " ; ")
	command = strings.ReplaceAll(command, "\x00OPA\x00", " && ")
	command = strings.ReplaceAll(command, "\x00OPO\x00", " || ")
	command = strings.ReplaceAll(command, "\x00FDIN\x00", "2>&1")
	command = strings.ReplaceAll(command, "\x00FDOUT\x00", "1>&2")
	return strings.Fields(command)
}

// reviewerShellSegmentIsValidationHead reports whether a raw segment resolves
// to a verification head: environment prefixes are skipped (and, per the head
// rules, would have been re-inspected), the timeout wrapper is unwrapped with
// the same flag-aware parser, and the resolved head is checked against the
// validation-pipe head set. Shared by the chain denial above.
func reviewerShellSegmentIsValidationHead(segment []string) bool {
	if len(segment) == 0 {
		return false
	}
	fields, _ := reviewerShellSkipEnvPrefix(segment)
	if len(fields) == 0 {
		return false
	}
	if path.Base(fields[0]) == "timeout" {
		if fields = reviewerShellTimeoutCommandFields(fields); len(fields) == 0 {
			return false
		}
	}
	return reviewerShellValidationPipeHead(path.Base(fields[0]), fields)
}

// stripReviewerShellQuotes removes quote characters the way the Windows
// dispatch's unquoted-syntax detector does before scanning for cmd.exe
// control operators: quoted "||" must not trigger the cmd.exe path.
func stripReviewerShellQuotes(command string) string {
	command = strings.ReplaceAll(command, "'", "")
	return strings.ReplaceAll(command, "\"", "")
}

// reviewerShellValidationPipeDenied rejects a validation command piped into
// another program. A pipeline's exit status is the LAST command's, so
// "go test ./... | tail -20" always reports success — the project's
// verification-evidence gate (suppressesVerificationFailure) treats exactly
// this shape as failure suppression and flags it after execution. Denying at
// the whitelist keeps the three layers (whitelist / high-risk classifier /
// evidence audit) agreeing instead of letting the command run and then be
// marked as a guardrail violation. Read-only heads piped (git diff | head)
// stay allowed: they are not verification evidence.
func reviewerShellValidationPipeDenied(groups [][]string) (bool, string) {
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		fields, expandSafe := reviewerShellNormalizedFields(group[0])
		if !expandSafe {
			// the rewrite-risk refusal in reviewerShellSegmentAllowed will
			// deny the command; nothing to add here
			continue
		}
		fields, _ = reviewerShellSkipEnvPrefix(fields)
		if len(fields) == 0 {
			continue
		}
		if path.Base(fields[0]) == "timeout" {
			if fields = reviewerShellTimeoutCommandFields(fields); len(fields) == 0 {
				continue
			}
		}
		head := path.Base(fields[0])
		if !reviewerShellValidationPipeHead(head, fields) {
			continue
		}
		return false, fmt.Sprintf("reviewer shell rejected %q: piping a verification command hides its exit status from verification evidence — run it bare", strings.Join(group, " | "))
	}
	return true, ""
}

// reviewerShellValidationPipeHead reports whether the leading command of a
// pipe group is one whose exit status feeds verification evidence. Only the
// validation forms the whitelist admits are covered; inspection subcommands
// (go env | grep, go list | wc -l) stay pipeable.
func reviewerShellValidationPipeHead(head string, fields []string) bool {
	switch head {
	case "make", "pytest":
		return true
	case "go":
		sub := reviewerShellSubcommand(fields, 6)
		return sub == "test" || sub == "vet" || sub == "build"
	case "cargo":
		sub := reviewerShellSubcommand(fields, 6)
		return sub == "test" || sub == "check"
	case "npm":
		sub := reviewerShellSubcommand(fields, 6)
		return sub == "test" || sub == "run"
	case "python", "python3":
		return len(fields) >= 3 && fields[1] == "-m" && (fields[2] == "pytest" || fields[2] == "unittest")
	}
	return false
}

func reviewerShellSegmentAllowed(segment string) (bool, string) {
	// fd duplication 2>&1 (stderr into stdout) is harmless plumbing and keeps
	// verification evidence complete, so it is stripped before the redirect
	// check. 1>&2 is the REVERSE direction: it drains stdout into stderr, so
	// verification output looks empty to the evidence capture — the evidence
	// gate (isShellVerificationOutputRedirectionToken) flags it, and this
	// whitelist stays aligned by denying it too.
	cleaned := strings.ReplaceAll(segment, "2>&1", "")
	if strings.Contains(cleaned, ">") {
		return false, fmt.Sprintf("reviewer shell rejected %q: output redirection is not allowed", segment)
	}
	if strings.Contains(cleaned, "<(") {
		return false, fmt.Sprintf("reviewer shell rejected %q: process substitution is not allowed", segment)
	}
	if strings.Contains(cleaned, "<<") {
		return false, fmt.Sprintf("reviewer shell rejected %q: heredoc is not allowed", segment)
	}
	if strings.Contains(cleaned, "$(") || strings.Contains(cleaned, "`") {
		return false, fmt.Sprintf("reviewer shell rejected %q: command substitution is not allowed", segment)
	}
	allFields, expandSafe := reviewerShellNormalizedFields(cleaned)
	if !expandSafe {
		return false, fmt.Sprintf("reviewer shell rejected %q: shell expansion rewrites the word after this gate matches it — write it literally", segment)
	}
	// bash's /dev/tcp and /dev/udp are not files: "reading" them opens a
	// network channel ("sort < /dev/tcp/host/port"), so the plain
	// input-redirection allowance (a pure file read) does not extend to
	// them. Both the spaced ("< /dev/tcp/h/p") and glued ("</dev/tcp/h/p")
	// token shapes, and an fd-prefixed "0<", reduce to the same scan —
	// twentieth pass.
	for i, tok := range allFields {
		if j := strings.Index(tok, "<"); j >= 0 {
			target := tok[j+1:]
			if target == "" && i+1 < len(allFields) {
				target = allFields[i+1] // spaced form: "<" and its file are separate tokens
			}
			if strings.HasPrefix(target, "/dev/tcp/") || strings.HasPrefix(target, "/dev/udp/") {
				return false, fmt.Sprintf("reviewer shell rejected %q: input redirection to /dev/tcp or /dev/udp opens a network channel, not a file read", segment)
			}
		}
	}
	fields, envDropped := reviewerShellSkipEnvPrefix(allFields)
	if len(fields) == 0 {
		return false, fmt.Sprintf("reviewer shell rejected %q: empty segment", segment)
	}
	if strings.ContainsAny(fields[0], "/\\") {
		return false, fmt.Sprintf("reviewer shell rejected %q: path-qualified programs are not in the reviewer vocabulary — call the tool by name", segment)
	}
	if path.Base(fields[0]) == "timeout" {
		fields = reviewerShellTimeoutCommandFields(fields)
		if len(fields) == 0 {
			return false, fmt.Sprintf("reviewer shell rejected %q: timeout needs a command", segment)
		}
	}
	head := path.Base(fields[0])
	if ok, reason := reviewerShellEnvPrefixAllowed(head, envDropped); !ok {
		return false, reason
	}
	if head == "git" {
		if ok, reason := reviewerShellGitFlagsAllowed(segment, fields); !ok {
			return false, reason
		}
	}
	if reviewerShellReadOnlyHeads[head] {
		if head == "find" {
			for _, tok := range fields[1:] {
				if reviewerShellFindWriteFlags[tok] {
					return false, fmt.Sprintf("reviewer shell rejected %q: find %s writes or executes", segment, tok)
				}
			}
		}
		// bare hostname prints the system name; any argument (including
		// -F/--file) MUTATES it — the sibling of the date -s denial
		// (eighteenth pass)
		if head == "hostname" && len(fields) > 1 {
			return false, fmt.Sprintf("reviewer shell rejected %q: hostname with arguments mutates the system hostname — only the bare printing form is allowed", segment)
		}
		// every read-only head with a per-head denial table gets its flags
		// checked (sixteenth pass: sort/date/rg joined pytest/make)
		if reviewerShellDeniedSubFlags[head] != nil && reviewerShellFlagDenied(head, fields, 1) {
			return false, fmt.Sprintf("reviewer shell rejected %q: %s uses a write/exec flag", segment, head)
		}
		return true, ""
	}
	if subcommands, ok := reviewerShellSubcommandHeads[head]; ok {
		sub := reviewerShellSubcommand(fields, 6)
		if sub == "" || !subcommands[sub] {
			return false, fmt.Sprintf("reviewer shell rejected %q: %s %s is not a read-only validation form", segment, head, sub)
		}
		if head == "go" {
			if ok, reason := reviewerShellGoModFlagAllowed(segment, fields); !ok {
				return false, reason
			}
			// go -C <dir> runs the whole command against a model-chosen
			// directory (a staged module): the twin of the GOWORK denial.
			// The subcommand extractor value-skips -C (shared with git -C),
			// so without this the sub would still resolve as "test" and the
			// command would pass — seventeenth pass. -workfile is the flag
			// twin of the GOWORK env denial (eighteenth pass). The
			// compositional twin `cd <dir> && go test` stays allowed (remote
			// workflows pin it — see the cd head comment); it is the same
			// documented residual class as git -C.
			for _, tok := range fields[1:] {
				if tok == "-C" || strings.HasPrefix(tok, "-C=") {
					return false, fmt.Sprintf("reviewer shell rejected %q: go -C targets a model-chosen directory", segment)
				}
				if tok == "-workfile" || strings.HasPrefix(tok, "-workfile=") {
					return false, fmt.Sprintf("reviewer shell rejected %q: go -workfile swaps the workspace for a model-chosen go.work", segment)
				}
			}
		}
		if reviewerShellFlagDenied(head+"/"+sub, fields, 1) {
			return false, fmt.Sprintf("reviewer shell rejected %q: %s %s uses a write/exec flag", segment, head, sub)
		}
		if head == "npm" {
			if ok, reason := reviewerShellNpmPrefixAllowed(segment, fields); !ok {
				return false, reason
			}
			if ok, reason := reviewerShellNpmPassthroughAllowed(segment, fields); !ok {
				return false, reason
			}
		}
		return true, ""
	}
	if head == "python" || head == "python3" {
		// the unittest form resolves no further head, so the dropped env must
		// be re-inspected here (PYTHONHOME/PYTHONPATH rules); the pytest form
		// re-checks again as "pytest" below
		if ok, reason := reviewerShellEnvPrefixAllowed(head, envDropped); !ok {
			return false, reason
		}
		if len(fields) >= 3 && fields[1] == "-m" && (fields[2] == "pytest" || fields[2] == "unittest") {
			if fields[2] == "pytest" {
				if reviewerShellFlagDenied("pytest", fields, 3) {
					return false, fmt.Sprintf("reviewer shell rejected %q: pytest report flags write files", segment)
				}
				// the resolved head is python, but the effective program is
				// pytest: re-check the dropped env against pytest's deny-list
				if ok, reason := reviewerShellEnvPrefixAllowed("pytest", envDropped); !ok {
					return false, reason
				}
			}
			return true, ""
		}
		return false, fmt.Sprintf("reviewer shell rejected %q: only python -m pytest/unittest is allowed", segment)
	}
	return false, fmt.Sprintf("reviewer shell rejected %q: %s is not in the reviewer validation vocabulary", segment, head)
}

// reviewerShellGitFlagsAllowed rejects git --output (diff machinery writes the
// result to a file), the pager/external-diff exec channels (-O[<pager>] /
// --open-files-in-pager force git grep to spawn a program; --ext-diff runs
// gitattributes-configured drivers that are off by default), and validates
// every `git -c key=value` against the config key allow-list. Note: plain
// `git diff` keeps textconv enabled by default — its drivers come from the
// project's committed .gitattributes, which sits under the same
// project-configured-code caveat as package.json scripts. Known residual:
// `git -C <dir>` stays allowed for remote workflows; a model-staged repo at
// that dir could carry its own textconv drivers (same class as the textconv
// caveat, mitigated for the env form via the GIT_DIR/GIT_WORK_TREE denial).
func reviewerShellGitFlagsAllowed(segment string, fields []string) (bool, string) {
	// -p means --paginate (spawn a pager) BEFORE the subcommand, but "show
	// the patch" AFTER it ("git log -p" is the bread-and-butter patch view).
	// --paginate is only meaningful pre-subcommand and errors post-subcommand,
	// so denying it anywhere is harmless (eighteenth pass: the flag twins of
	// the denied PAGER/GIT_PAGER env).
	subSeen := false
	for i := 1; i < len(fields); i++ {
		tok := fields[i]
		// --output[=file] writes the diff to a file; --output-indicator-*
		// are harmless formatting flags that share the prefix and stay
		// allowed (sixteenth pass refinement)
		// --git-dir/--work-tree are the flag twins of the denied GIT_DIR /
		// GIT_WORK_TREE env: they point git at a model-staged repository or
		// worktree (forged history for the evidence gate, crafted
		// .gitattributes drivers) — seventeenth pass
		if tok == "--git-dir" || strings.HasPrefix(tok, "--git-dir=") {
			return false, fmt.Sprintf("reviewer shell rejected %q: git --git-dir redirects the repository", segment)
		}
		if tok == "--work-tree" || strings.HasPrefix(tok, "--work-tree=") {
			return false, fmt.Sprintf("reviewer shell rejected %q: git --work-tree redirects the worktree", segment)
		}
		if tok == "--output" || strings.HasPrefix(tok, "--output=") {
			return false, fmt.Sprintf("reviewer shell rejected %q: git --output writes to a file", segment)
		}
		if tok == "--ext-diff" {
			return false, fmt.Sprintf("reviewer shell rejected %q: git --ext-diff executes external diff drivers", segment)
		}
		if strings.HasPrefix(tok, "--open-files-in-pager") || strings.HasPrefix(tok, "-O") {
			return false, fmt.Sprintf("reviewer shell rejected %q: git grep -O runs a pager program", segment)
		}
		if tok == "--paginate" {
			return false, fmt.Sprintf("reviewer shell rejected %q: git --paginate spawns a pager program", segment)
		}
		if !subSeen {
			if tok == "-p" {
				return false, fmt.Sprintf("reviewer shell rejected %q: pre-subcommand git -p is --paginate — it spawns a pager program", segment)
			}
			if !strings.HasPrefix(tok, "-") {
				subSeen = true
			}
		}
		if tok == "-c" {
			if i+1 >= len(fields) {
				return false, fmt.Sprintf("reviewer shell rejected %q: git -c needs key=value", segment)
			}
			value := fields[i+1]
			eq := strings.Index(value, "=")
			if eq <= 0 || !reviewerShellGitConfigKeys[value[:eq]] {
				return false, fmt.Sprintf("reviewer shell rejected %q: git -c %s is not a reviewer formatting key", segment, value)
			}
			i++
		}
	}
	return true, ""
}

// reviewerShellFlagDenied reports whether fields[start:] carries a denied flag
// for the given head/subcommand, either as the exact token or as "flag=value".
// For abbreviation heads (reviewerShellAbbreviationHeads) it also denies
// argparse/getopt prefix abbreviations: "--junit=x" uniquely resolves to
// --junitxml, so a token that is a strict prefix of a denied long flag must be
// refused too.
func reviewerShellFlagDenied(headSub string, fields []string, start int) bool {
	denied := reviewerShellDeniedSubFlags[headSub]
	if denied == nil {
		return false
	}
	abbrev := reviewerShellAbbreviationHeads[headSub]
	for _, tok := range fields[start:] {
		base := tok
		if eq := strings.Index(tok, "="); eq >= 0 {
			base = tok[:eq]
		}
		if denied[base] {
			return true
		}
		if abbrev && strings.HasPrefix(base, "--") {
			for flag := range denied {
				if len(base) < len(flag) && strings.HasPrefix(flag, base) {
					return true
				}
			}
		}
	}
	return false
}

// reviewerShellSkipEnvPrefix drops leading FOO=bar environment assignments and
// returns both the remaining fields and the dropped name=value tokens. The
// dropped tokens are NOT trusted blindly: heads with env-sensitive semantics
// (go, git) re-inspect them via reviewerShellEnvPrefixAllowed, because env
// variables like GOFLAGS and GIT_CONFIG_* can inject the very flags/config
// this gate denies at the token level.
func reviewerShellSkipEnvPrefix(fields []string) ([]string, []string) {
	dropped := make([]string, 0, 2)
	for len(fields) > 0 {
		eq := strings.Index(fields[0], "=")
		if eq <= 0 || !reviewerShellEnvName(fields[0][:eq]) {
			return fields, dropped
		}
		dropped = append(dropped, fields[0])
		fields = fields[1:]
	}
	return fields, dropped
}

// reviewerShellDeniedEnvNames are inline environment assignments that would
// bypass token-level checks for the given head:
//   - GOFLAGS injects flags wholesale into go commands ("GOFLAGS=build=-o=x"
//     defeats the -o denial; "GOFLAGS=-mod=mod" rewrites go.mod);
//   - git spawns programs via GIT_PAGER/PAGER (pager), GIT_SSH(_COMMAND) /
//     GIT_PROXY_COMMAND / *_ASKPASS (transport helpers), and GIT_EDITOR;
//     GIT_CONFIG_COUNT/KEY_n/VALUE_n inject arbitrary config, bypassing the
//     `git -c` key allow-list;
//   - cargo re-resolves its own toolchain via CARGO/RUSTC/RUSTC_WRAPPER;
//   - make runs recipe lines under SHELL and pre-loads makefiles from
//     MAKEFILES/MAKEFLAGS;
//   - npm picks the script shell via npm_config_script_shell and code via
//     NODE_OPTIONS;
//   - pytest re-injects options via PYTEST_ADDOPTS ("PYTEST_ADDOPTS=--junitxml=x"
//     defeats the --junitxml token denial) and plugins via PYTEST_PLUGINS.
var reviewerShellDeniedEnvNames = map[string]map[string]bool{
	// Keys are LOWERCASE: reviewerShellEnvPrefixAllowed looks names up
	// case-insensitively (Windows hosts resolve env names case-blind), so
	// every denied name must be stored lowercased.
	//
	// GOENV points the go command at an alternate env file, whose GOFLAGS
	// entry injects the very flags the GOFLAGS denial blocks (sixteenth
	// pass); GOWORK is handled separately in reviewerShellEnvPrefixAllowed
	// because its one safe direction ("off") must stay usable.
	//
	// twenty-first pass (the toolchain-selecting / staged-code channels the
	// first sweep missed): GOROOT relocates the entire go toolchain — the
	// PYTHONHOME twin, model code where the compiler should be; CC/CXX are
	// cgo's compiler selection and PKG_CONFIG its metadata program (both
	// execute a model-chosen binary when a cgo package builds); GOPATH and
	// GOMODCACHE relocate the module cache, so a staged cache swaps the
	// source of required modules — the env twin of the -modfile denial;
	// GOCOVERDIR redirects where test binaries write coverage files.
	"go": {"goflags": true, "goenv": true, "goroot": true, "gopath": true,
		"gomodcache": true, "gocoverdir": true, "cc": true, "cxx": true, "pkg_config": true},
	// git additionally relocates the repository wholesale via GIT_DIR /
	// GIT_WORK_TREE: pointing git at a model-staged repo (with a crafted
	// .gitattributes textconv driver) executes model-authored programs —
	// the env twin of `git -C`, which stays allowed for remote workflows
	// (see the residual note in reviewerShellGitFlagsAllowed). The object
	// store redirects (GIT_OBJECT_DIRECTORY / GIT_ALTERNATE_OBJECT_DIRECTORIES)
	// are denied too: staged objects forge the history that verification
	// evidence reads (sixteenth pass).
	"git": {"git_pager": true, "pager": true, "git_ssh": true, "git_ssh_command": true, "git_proxy_command": true, "git_askpass": true, "ssh_askpass": true, "git_editor": true, "git_dir": true, "git_work_tree": true, "git_object_directory": true, "git_alternate_object_directories": true},
	// RUSTFLAGS / CARGO_ENCODED_RUSTFLAGS reach rustc as arbitrary flags,
	// including -C linker=<program> — the env twin of the --config denial
	// (sixteenth pass).
	//
	// twenty-first pass: the cc crate compiles C sources with the model's
	// CC/CXX; CARGO_HOME relocates the config.toml that may set
	// build.rustc-wrapper (the env twin of the --config denial); and
	// RUSTUP_TOOLCHAIN / RUSTUP_HOME select or relocate the toolchain that
	// rustup dispatches cargo/rustc to.
	"cargo": {"cargo": true, "rustc": true, "rustc_wrapper": true, "rustflags": true, "cargo_encoded_rustflags": true,
		"cc": true, "cxx": true, "cargo_home": true, "rustup_toolchain": true, "rustup_home": true},
	// GNUMAKEFLAGS is GNU make's second MAKEFLAGS channel: it pre-loads
	// flags (-f/--file included) and would defeat the MAKEFLAGS denial
	// (sixteenth pass). MAKESTARTUP is the third channel: the startup
	// makefile is read before everything else (twenty-first pass).
	"make": {"shell": true, "makefiles": true, "makeflags": true, "gnumakeflags": true, "makestartup": true},
	// npm_config_prefix is the env twin of --prefix: it points npm at a
	// staged package's scripts (sixteenth pass).
	"npm":    {"node_options": true, "npm_config_script_shell": true, "npm_config_prefix": true},
	"pytest": {"pytest_addopts": true, "pytest_plugins": true},
	// twenty-first pass: ripgrep pre-loads flags from this config file,
	// including --pre — the per-file preprocessor exec channel the token
	// layer already denies.
	"rg": {"ripgrep_config_path": true},
}

// reviewerShellDeniedEnvGlobal are inline environment assignments denied for
// EVERY head: they redirect which program or code the OS/loader resolves, so
// they bypass the head allow-list itself, not just flag checks:
//   - PATH: "PATH=/tmp/evil git log" executes /tmp/evil/git;
//   - LD_PRELOAD / LD_LIBRARY_PATH / DYLD_INSERT_LIBRARIES: loader-level code
//     injection;
//   - BASH_ENV / ENV: sourced on startup when the host runs the command
//     through a shell.
//
// Keys are lowercase — see reviewerShellDeniedEnvNames.
var reviewerShellDeniedEnvGlobal = map[string]bool{
	"path": true, "ld_preload": true, "ld_library_path": true,
	"dyld_insert_libraries": true, "bash_env": true, "env": true, "ifs": true,
}

// reviewerShellEnvPrefixAllowed re-inspects the dropped FOO=bar tokens for the
// resolved head. GIT_CONFIG* is prefix-denied for git (the numbered
// GIT_CONFIG_KEY_n/VALUE_n family has no fixed name).
//
// Deny matching is CASE-INSENSITIVE (twenty-first pass): on Windows hosts
// node/npm resolve environment names case-insensitively, so the exact-match
// denial was defeated by "NPM_CONFIG_SCRIPT_SHELL=... npm test". Only deny
// lookups lowercase the name — allowed forms (GOOS, PYTHONPATH, GOWORK=off)
// keep their case and are never consulted here.
func reviewerShellEnvPrefixAllowed(head string, dropped []string) (bool, string) {
	denied := reviewerShellDeniedEnvNames[head]
	for _, tok := range dropped {
		eq := strings.Index(tok, "=")
		if eq <= 0 {
			continue
		}
		name := strings.ToLower(tok[:eq])
		if reviewerShellDeniedEnvGlobal[name] {
			return false, fmt.Sprintf("reviewer shell rejected inline env %s: it redirects program/loader resolution past the command allow-list", tok)
		}
		if denied != nil && denied[name] {
			return false, fmt.Sprintf("reviewer shell rejected inline env %s: it would bypass the flag/config gate for %s", tok, head)
		}
		if head == "git" && strings.HasPrefix(name, "git_config") {
			return false, fmt.Sprintf("reviewer shell rejected inline env %s: GIT_CONFIG_* injects git config past the -c allow-list", tok)
		}
		// GOWORK's one safe direction is "off"; any other value points the
		// go command at a model-staged go.work whose replace directives swap
		// project modules for staged code (sixteenth pass).
		if head == "go" && name == "gowork" {
			if value := tok[eq+1:]; value != "off" {
				return false, fmt.Sprintf("reviewer shell rejected inline env %s: GOWORK may only be disabled (off), not redirected", tok)
			}
		}
		// python resolves its stdlib and import path from these: PYTHONHOME
		// relocates the stdlib wholesale (code injection), and PYTHONPATH is
		// only tolerated for project-relative entries — the same
		// project-controlled-code caveat as the validation entry points.
		if head == "python" || head == "python3" || head == "pytest" {
			if name == "pythonhome" {
				return false, fmt.Sprintf("reviewer shell rejected inline env %s: PYTHONHOME redirects python's stdlib and can execute code", tok)
			}
			if name == "pythonpath" {
				for _, entry := range strings.Split(tok[eq+1:], ":") {
					if entry == "" || entry == "." || strings.HasPrefix(entry, "./") {
						continue
					}
					return false, fmt.Sprintf("reviewer shell rejected inline env %s: PYTHONPATH may only add project-relative entries (./...)", tok)
				}
			}
		}
	}
	return true, ""
}

// reviewerShellGoModFlagAllowed rejects -mod=mod for go subcommands: it lets
// the go command rewrite go.mod/go.sum in the source tree. "-mod mod" (separate
// value) and "-mod=mod" (joined) are both covered; readonly/vendor stay allowed.
func reviewerShellGoModFlagAllowed(segment string, fields []string) (bool, string) {
	for i := 1; i < len(fields); i++ {
		tok := fields[i]
		if tok == "-mod=mod" || (tok == "-mod" && i+1 < len(fields) && fields[i+1] == "mod") {
			return false, fmt.Sprintf("reviewer shell rejected %q: -mod=mod lets the go command rewrite go.mod/go.sum", segment)
		}
	}
	return true, ""
}

func reviewerShellEnvName(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return len(value) > 0
}

// reviewerShellSubcommand returns the first non-flag token after the program
// name, skipping the values of flags that take a separate argument (-C, -c).
// This keeps "git -C /repo status" and "git -c x=y log" working without
// admitting arbitrary subcommand spoofing.
func reviewerShellSubcommand(fields []string, maxIndex int) string {
	for i := 1; i < len(fields) && i <= maxIndex; i++ {
		tok := fields[i]
		switch tok {
		case "-C", "-c":
			i++ // the flag's value is the following token
			continue
		}
		if strings.HasPrefix(tok, "-") {
			continue
		}
		return tok
	}
	return ""
}
