package codingagent

// reviewer_shell_test.go pins the reviewer read-only shell vocabulary: every
// command the reviewer may run must be a read-only inspection or project
// validation form, and every write channel (redirection, substitution,
// interpreters, mutating subcommands) must be rejected before execution.

import (
	"strconv"
	"strings"
	"testing"
)

func TestReviewerShellInvocationAllowed(t *testing.T) {
	allowed := []string{
		"ls -la",
		"cat go.mod",
		"head -50 main.go",
		"tail -20 build.log",
		"wc -l *.go",
		"rg pattern .",
		"grep -rn TODO .",
		"diff a.txt b.txt",
		"git status",
		"git diff",
		"git log --oneline -5",
		"git -C /repo diff",
		"git -c core.quotepath=false log",
		"git -c color.ui=never diff",
		"go test -count=1 ./...",
		"go test -run TestX ./pkg/...",
		"go env -json GOCACHE",
		"pytest --cov-report=term -q",
		"git rev-parse HEAD",
		"git blame main.go",
		"git ls-files",
		"git show HEAD~1",
		"go test ./...",
		"go vet ./...",
		"go build ./...",
		"go list ./...",
		"go env",
		"cargo test",
		"cargo check",
		"npm test",
		"npm run lint",
		"make test",
		"pytest -q",
		"python -m pytest -q",
		"python -m unittest discover",
		"python3 -m pytest tests/",
		"find . -name *.go",
		"find . -type f -name x",
		"timeout 60 go test ./...",
		"GOOS=linux go build ./...",
		"GOOS=linux GOARCH=amd64 go build ./...",
		"go build -mod=readonly ./...",
		"go test -mod=readonly ./...",
		"go vet -mod=vendor ./...",
		"git grep -n TODO .",
		"git diff --no-ext-diff",
		"go test ./... < /dev/null",
		"cat < go.mod",
		"grep -n TODO main.go < input.txt",
		"cd /repo && go test ./...",
		"cd /repo && git status && go vet ./...",
		"cargo test -- --nocapture",
		"make -j4 test",
		"npm test -- --run",
		"pytest -k foo -q",
		"git diff | head -50",
		"go env | grep GOOS",
		"go list ./... | wc -l",
		"git diff 2>&1 | tail -20",
		"go test ./... 2>&1 && go vet ./... 2>&1",
		"go test ./... && go vet ./...",
		"ls; git status",
		// fourteenth pass: quoted/escaped heads normalize to the same word the
		// shell will run, and trailing-/single-quoted $ is literal
		"\"git\" status",
		"\\git status",
		"rg 'error$'",
		"grep -n 'foo$' main.go",
		"rg \"err$\"",
		"rg '\\$PATH'",
		"grep '$' main.go",
		"PYTHONPATH=. python -m pytest",
		"PYTHONPATH=./pkg:. python -m pytest",
		// fifteenth pass: project-relative --prefix and unchanged make forms
		// stay available
		"npm test --prefix ./packages/foo",
		"make -C build test",
		"pytest -p no:cacheprovider -q",
		// sixteenth pass: the refined --output denial leaves git's harmless
		// --output-indicator-* flags working; plain sort/date/rg forms and
		// GOWORK=off keep their safe directions
		"git diff --output-indicator-new=# HEAD~1",
		"sort -r --output-delimiter=, list.txt",
		"date -u",
		"rg --pre-glob='*.md' 'TODO'",
		"GOWORK=off go test ./...",
		// seventeenth pass: timeout's own flags no longer over-deny the
		// wrapped command, and npm's project-relative -C alias works
		"timeout -k 5 10 go test ./...",
		"timeout --preserve-status 10 make test",
		"timeout --kill-after=5s 30s pytest -q",
		"npm -C ./packages/foo test",
		// eighteenth pass: bare read-only heads keep their printing forms,
		// post-subcommand -p stays the patch flag, and --no-pager works
		"tree /repo/src",
		"hostname",
		"file -b main.go",
		"git log -p -3",
		"git --no-pager log -p",
		// nineteenth pass: a fallback BEFORE the verification command keeps
		// its real exit status, and a && tail that is itself a verification
		// command chains evidence
		"git status || go test ./...",
		"git status || git diff --stat",
		"go test ./... && go vet ./...",
		// twentieth pass: plain input redirection stays a pure file read
		"sort < go.mod",
		"printf '%s %s' a b",
		// twenty-first pass: "|&" between read-only heads keeps its pipe
		// semantics, glued benign chains stay usable, and timeout's GNU
		// long-flag abbreviations resolve
		"git diff |& head -5",
		"git status|& cat",
		"ls&&git status",
		"ls;git status",
		"timeout --kill 5 10 go test ./...",
		"timeout --s KILL 30 make test",
	}
	for _, command := range allowed {
		if ok, reason := reviewerShellInvocationAllowed(map[string]interface{}{"command": command}); !ok {
			t.Errorf("allowed %q rejected: %s", command, reason)
		}
	}

	denied := []string{
		"rm -rf /",
		"echo hi > f",
		"echo hi >> f",
		"git diff > patch.txt",
		"git diff 1> patch.txt",
		"make x > log",
		"sed -i s/a/b/ file",
		"python -c 'import os'",
		"python -m pip install requests",
		"node -e 'fs.write'",
		"cat $(ls)",
		"echo `date`",
		"find . -delete",
		"find . -exec rm {} ;",
		"find . -fprint list.txt",
		"git commit -m w",
		"git push",
		"git checkout main",
		"git branch x",
		"git config user.name a",
		"go generate ./...",
		"go mod tidy",
		"npm install",
		"npx webpack",
		"touch f",
		"cp a b",
		"mv a b",
		"awk '{print}' f",
		"sh -c ls",
		"bash -c ls",
		"xargs rm",
		"sudo ls",
		"ls; rm -rf /",
		"go test ./... && git commit -m w",
		"timeout 60",
		// hardening pass (2026-09-20): per-subcommand write/exec flags
		"go env -w GOFLAGS=-mod=mod",
		"go env -u GOFLAGS",
		"go build -o /tmp/binary ./...",
		"go test -c ./...",
		"go test -exec touch ./...",
		"go vet -vettool=evil ./...",
		"go test -coverprofile=cov.out ./...",
		"go test -cpuprofile cpu.out ./...",
		"go test -trace tr.out ./...",
		"go test -outputdir /tmp ./...",
		"timeout 30 go test -coverprofile=c.out ./...",
		"git diff --output=patch.txt",
		"git log --output patch.txt",
		"git -c core.pager=touch log",
		"git -c core.fsmonitor=cmd status",
		"git -c some.key=v log",
		"pytest --junitxml=out.xml",
		"pytest --html=report.html",
		"python -m pytest --junitxml=out.xml",
		// second hardening pass: env-prefix injection + -mod
		"GOFLAGS=-mod=mod go build ./...",
		"GOFLAGS=build=-o=/tmp/x go build ./...",
		"GOFLAGS=-exec=x go test ./...",
		"timeout 60 go build -mod=mod ./...",
		"GIT_PAGER=touch git log",
		"PAGER=touch git log",
		"GIT_SSH_COMMAND=evil git ls-remote",
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.pager GIT_CONFIG_VALUE_0=touch git log",
		"go build -mod=mod ./...",
		"go test -mod mod ./...",
		// third hardening pass: program/loader resolution env
		"PATH=/tmp/evil git log",
		"PATH=/tmp/evil go test ./...",
		"LD_PRELOAD=/tmp/x.so git status",
		"LD_LIBRARY_PATH=/tmp/lib go build ./...",
		"BASH_ENV=/tmp/x go test ./...",
		"IFS=x git status",
		// fourth hardening pass: git pager/ext-diff channels + per-head toolchain env
		"git grep -Otouch TODO .",
		"git grep --open-files-in-pager=less TODO",
		"git diff --ext-diff",
		"CARGO=/tmp/evil cargo test",
		"RUSTC=/tmp/evil cargo check",
		"RUSTC_WRAPPER=/tmp/evil cargo test",
		"SHELL=/tmp/evil make test",
		"MAKEFLAGS=-f /tmp/x make",
		"MAKEFILES=/tmp/x make test",
		"NODE_OPTIONS=--require /tmp/x.js npm test",
		"npm_config_script_shell=/tmp/evil npm run lint",
		"PYTEST_ADDOPTS=--junitxml=x pytest -q",
		"PYTEST_ADDOPTS=--junitxml=x python -m pytest",
		"PYTEST_PLUGINS=evil_plugin pytest -q",
		// fifth pass (red team): bare & is a POSIX command separator
		"ls & rm -rf /",
		"echo a & touch f",
		"cat go.mod & git commit -m w",
		"ls &> f",
		"ls &>> f",
		"timeout 60 go test ./... & python -c 'x'",
		// fifth pass: model-chosen passthrough args flip scripts into write mode
		"npm run lint -- --fix",
		"npm test -- -u",
		"npm run format -- --write .",
		"pytest --update-snapshots",
		"python -m pytest --snapshot-update",
		// twelfth pass (layer consistency): input-side constructs that stay denied
		"grep a <(ls)",
		"diff <(sort a) <(sort b)",
		"cat << EOF",
		"cat <<EOF",
		// thirteenth pass (evidence alignment): piping a verifier hides its exit
		// status — the evidence gate flags it as failure suppression after
		// execution, so the whitelist denies it up front
		"go test ./... | tail -20",
		"go test ./... 2>&1 | tail -20",
		"make test | tail -5",
		"cargo test | head -20",
		"npm run lint | head -30",
		"pytest -q | tail",
		"python -m pytest -q | tail",
		"GOOS=linux go test ./... | cat",
		"timeout 30 go test ./... | tail -1",
		// 1>&2 drains stdout into stderr: verification output looks empty to
		// the evidence capture, so the evidence gate flags it — whitelist aligns
		"go test ./... 1>&2",
		// fourteenth pass (token vs shell tokenization divergence): the gate
		// matches raw tokens, the shell executes normalized ones — quoting,
		// $ expansion, brace expansion, and backslash escapes must not smuggle
		// denied flags past the token-level denial tables
		"git diff \"--output=x\"",
		"git diff \\--output=x",
		"go test \"-coverprofile=x\" ./...",
		"go build \"-o\" /tmp/bin",
		"go test \"-mod=mod\" ./...",
		"npm run lint -- \"--fix\"",
		"go test ${IFS}-coverprofile=x ./...",
		"go build ${IFS}-o x",
		"GOFLAGS=${X} go build ./...",
		"go test $PKG ./...",
		"go build $'-o' x",
		"git log ${HOME}",
		"git diff {--output=x,y}",
		// quoted/escaped validation head piped away: the evidence gate must
		// see through the quoting to the real head
		"'go' test ./... | tail -1",
		"\"go\" test ./... | tail -1",
		// path-qualified heads: path.Base would alias an arbitrary local
		// binary to a vocabulary name
		"./git status",
		"../go build ./...",
		"/bin/ls",
		// python stdlib/import-path env redirects
		"PYTHONHOME=/tmp/x python -m pytest",
		"PYTHONPATH=/tmp/x python -m pytest",
		"PYTHONHOME=/tmp/x python -m unittest",
		"PYTHONPATH=/tmp/x python -m unittest",
		// fifteenth pass (exec/config channels left in the token layer):
		// model-chosen makefile, cargo config-injected programs, pytest
		// argparse prefix abbreviations, npm value-form passthrough flags,
		// and a staged npm package via --prefix
		"make -f /tmp/evil.mk test",
		"make --file=evil.mk",
		"make --makefile /tmp/x.mk test",
		"cargo test --config build.rustc-wrapper=/tmp/evil",
		"cargo check --config /tmp/evil.toml",
		"pytest --junit=x.xml",
		"npm run lint -- --fix=x",
		"npm test -- --write=true",
		"npm test --prefix /tmp/staged",
		"npm run lint --prefix=/tmp/staged",
		"GIT_DIR=/tmp/staged git status",
		"GIT_WORK_TREE=/tmp/staged git diff",
		// sixteenth pass (read-only heads' own write/exec flags + getopt
		// abbreviation smuggling + per-head env twins): sort's overwrite and
		// compress-program channels, date's clock set, rg's preprocessor
		// exec, make's eval/include-path/plugin channels, GNU getopt
		// abbreviations resolving INTO denied flags, and the env twins of
		// token-level denials
		"sort -o /tmp/evil list.txt",
		"sort --output=/tmp/evil list.txt",
		"sort --compress-program=/tmp/evil list.txt",
		"sort --compress=/tmp/evil list.txt",
		"date -s 2026-01-01",
		"date --set=2026-01-01",
		"date --se=x",
		"rg --pre /tmp/evil 'TODO'",
		"rg --pre=/tmp/evil 'TODO'",
		"make --eval=X=1 test",
		"make --load-plugins=/tmp/evil.so test",
		"make --lo=/tmp/evil.so test",
		"make -I /tmp/staged test",
		"make --include-dir=/tmp/staged test",
		"GOENV=/tmp/envfile go test ./...",
		"GOWORK=/tmp/evil.work go test ./...",
		"GNUMAKEFLAGS=--eval=x make test",
		"RUSTFLAGS=-Clinker=/tmp/evil cargo test",
		"CARGO_ENCODED_RUSTFLAGS=x cargo test",
		"npm_config_prefix=/tmp/staged npm test",
		"GIT_OBJECT_DIRECTORY=/tmp/objects git log",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/tmp/alt git log",
		// seventeenth pass (flag twins of denied env / staged targets):
		// git's repository redirects, go's staged-directory and staged-code
		// channels, cargo's staged crate, npm's -C alias of --prefix, and
		// make touching every target's mtime
		"git --git-dir=/tmp/staged log",
		"git --git-dir /tmp/staged log",
		"git --work-tree=/tmp/staged diff",
		"git --work-tree /tmp/staged status",
		"go -C /tmp/staged test ./...",
		"go test -C /tmp/staged ./...",
		"go test -toolexec=/tmp/evil ./...",
		"go test -toolexec /tmp/evil ./...",
		"go build -toolexec=/tmp/evil ./...",
		"go vet -toolexec=/tmp/evil ./...",
		"go test -overlay=/tmp/evil.json ./...",
		"go build -overlay /tmp/evil.json ./...",
		"cargo test --manifest-path /tmp/staged/Cargo.toml",
		"cargo check --manifest-path=/tmp/staged/Cargo.toml",
		"npm -C /tmp/staged test",
		"npm run lint -C=/tmp/staged",
		"make -t all",
		"make --touch all",
		// eighteenth pass: tree/file/hostname write channels, git pager flags,
		// and go's workspace/module redirection flags
		"tree -o /tmp/out /repo",
		"file -C -m magic",
		"file --compile",
		"hostname evil",
		"hostname -F /etc/hostname",
		"git --paginate log",
		"git -p log",
		"git -c color.ui=always -p status",
		"go test -modfile=/tmp/staged/go.mod ./...",
		"go build -modfile /tmp/staged/go.mod ./...",
		"go vet -modfile=staged.mod ./...",
		"go -workfile /tmp/staged/go.work test ./...",
		"go -workfile=/tmp/x go test ./...",
		// nineteenth pass: "||"/"&" after a verification command mask its exit
		// status (same shapes the evidence gate flags); a "&&"/";" tail must
		// itself be a verification command; % pairs inside an || chain
		// dispatch to cmd.exe where percent expansion rewrites words
		"go test ./... || git status",
		"go test ./... || echo failed",
		"timeout 60 go test ./... || echo retry",
		"make test || echo failed",
		"go test ./... & echo bg",
		"go test ./... && git status",
		"go vet ./... ; echo done",
		"git status || cat %TEMP%\\notes",
		"go vet ./... || echo %PATH%",
		// twentieth pass: /dev/tcp and /dev/udp are network channels, not
		// files — the input-redirection allowance does not cover them; a
		// herestring is still a heredoc-class construct
		"sort < /dev/tcp/10.0.0.1/4444",
		"cat </dev/udp/host.example/53",
		"grep TODO < /dev/tcp/localhost/8080",
		"sort 0< /dev/tcp/10.0.0.1/4444",
		"sort <<< x",
		// twenty-first pass (layer alignment completion): "|&" is a pipe
		// ("a |& b" == "a 2>&1 | b") whose exit status is the tail's, and
		// glued operators chain in the shell exactly like the spaced forms —
		// the evidence gate flags both classes post-execution, so the
		// whitelist denies them up front
		"go test ./... |& tail -20",
		"go test ./...|& tail -5",
		"make test|& head -10",
		"timeout 30 go test ./... |& cat",
		"go test ./...||echo ok",
		"go test ./...&&git status",
		"go vet ./...;echo done",
		"go test ./...&echo bg",
		"timeout 60 go test ./...||echo retry",
		// twenty-first pass (remaining toolchain-selecting / staged-code env
		// channels): the go toolchain and cgo helper programs, cargo's
		// compiler selection and rustup dispatch, the third makefile channel,
		// ripgrep's config file, and Windows case variants of denied env
		"GOROOT=/tmp/evil go test ./...",
		"CC=/tmp/evil go build ./...",
		"CXX=/tmp/evil go test ./...",
		"PKG_CONFIG=/tmp/evil go test ./...",
		"GOPATH=/tmp/staged go test ./...",
		"GOMODCACHE=/tmp/staged go build ./...",
		"GOCOVERDIR=/tmp/cov go test ./...",
		"cc=/tmp/evil go build ./...",
		"CC=/tmp/evil cargo test",
		"CXX=/tmp/evil cargo check",
		"CARGO_HOME=/tmp/staged cargo test",
		"RUSTUP_TOOLCHAIN=/tmp/evil cargo test",
		"RUSTUP_HOME=/tmp/staged cargo test",
		"MAKESTARTUP=/tmp/evil.mk make test",
		"RIPGREP_CONFIG_PATH=/tmp/evil rg TODO",
		"ripgrep_config_path=/tmp/evil rg TODO",
		"NPM_CONFIG_SCRIPT_SHELL=/tmp/evil npm test",
	}
	for _, command := range denied {
		if ok, _ := reviewerShellInvocationAllowed(map[string]interface{}{"command": command}); ok {
			t.Errorf("denied %q unexpectedly allowed", command)
		}
	}

	if ok, _ := reviewerShellInvocationAllowed(map[string]interface{}{}); ok {
		t.Error("missing command must be denied")
	}
	if ok, _ := reviewerShellInvocationAllowed(map[string]interface{}{"command": "   "}); ok {
		t.Error("empty command must be denied")
	}
}

// TestReviewerShellSeparatorMatrix pins the gate's core invariant
// property-style: whatever joins two commands, a denied segment must never
// sneak through. Every separator the host shell (sh -c / bash -c) honors —
// including spacing, stacking, and quoting-free variations — must split the
// command into segments that are checked independently. A failure here means a
// separator class is missing from splitReviewerShellSegments, which invalidates
// the entire allow-list.
func TestReviewerShellSeparatorMatrix(t *testing.T) {
	evil := []string{"rm -rf /", "touch f", "echo x > f", "git commit -m w", "sh -c evil"}
	separators := []string{
		"&", " &", "& ", " & ", "&&", " && ", "&;&", "&|", "|&", "&&&",
		";", " ;", "; ", ";;", " ; | ; ", ";|", "|;",
		"|", " |", "| ", "||", " || ", "||;",
		"\n", "\n\n", "\n ", " \n", "\r\n", "\n&", "&\n", "\n;\n",
	}
	for _, sep := range separators {
		for _, e := range evil {
			command := "ls" + sep + e
			if ok, _ := reviewerShellInvocationAllowed(map[string]interface{}{"command": command}); ok {
				t.Errorf("separator %q lets %q through: %q allowed", strconv.Quote(sep), e, command)
			}
		}
	}
	// benign plumbing must keep working across representative joins
	fine := []string{
		"ls && git diff --stat",
		"ls; git diff --stat",
		"ls | git diff --stat",
		"git diff 2>&1 | tail -5",
		"go test ./... 2>&1 && go vet ./... 2>&1",
		"ls & pwd",
		"ls ; pwd | cat",
	}
	for _, command := range fine {
		if ok, _ := reviewerShellInvocationAllowed(map[string]interface{}{"command": command}); !ok {
			t.Errorf("benign command wrongly rejected: %q", command)
		}
	}
}

func TestReviewerShellToolPolicyGate(t *testing.T) {
	policy := ToolPolicy{
		Role:      RoleReviewer,
		Allowed:   map[string]bool{"bash": true},
		Normalize: func(name string) string { return name },
	}
	if ok, reason := policy.IsToolCallAllowed("bash", map[string]interface{}{"command": "go test ./..."}); !ok {
		t.Fatalf("reviewer go test rejected: %s", reason)
	}
	if ok, _ := policy.IsToolCallAllowed("bash", map[string]interface{}{"command": "rm -rf /"}); ok {
		t.Fatal("reviewer rm -rf must be denied")
	}
	if ok, _ := policy.IsToolCallAllowed("bash", map[string]interface{}{"command": "cat go.mod > copy.txt"}); ok {
		t.Fatal("reviewer redirect must be denied")
	}

	// ssh_bash is the remote transport twin of bash and must receive the
	// identical whitelist gate (review P2-1 remote completion).
	remote := ToolPolicy{
		Role:      RoleReviewer,
		Allowed:   map[string]bool{"ssh_bash": true},
		Normalize: func(name string) string { return strings.ToLower(strings.TrimSpace(name)) },
	}
	if ok, reason := remote.IsToolCallAllowed("ssh_bash", map[string]interface{}{"command": "cd /repo && go test ./...", "working_dir": "/repo"}); !ok {
		t.Fatalf("reviewer ssh_bash whitelisted validation rejected: %s", reason)
	}
	if ok, _ := remote.IsToolCallAllowed("ssh_bash", map[string]interface{}{"command": "rm -rf /"}); ok {
		t.Fatal("reviewer ssh_bash rm -rf must be denied")
	}
	if ok, _ := remote.IsToolCallAllowed("ssh_bash", map[string]interface{}{"command": "echo x > build.log"}); ok {
		t.Fatal("reviewer ssh_bash redirect must be denied")
	}
	if ok, _ := remote.IsToolCallAllowed("ssh_bash", map[string]interface{}{}); ok {
		t.Fatal("reviewer ssh_bash without command must fail closed")
	}

	explorer := ToolPolicy{
		Role:      RoleExplorer,
		Allowed:   map[string]bool{"Glob": true, "ripgrep": true},
		Normalize: func(name string) string { return name },
	}
	if ok, _ := explorer.IsToolCallAllowed("bash", map[string]interface{}{"command": "go test ./..."}); ok {
		t.Fatal("explorer bash must stay denied at the allow-list level")
	}

	// Fail closed: an inspection role with no allow-list admits nothing.
	empty := ToolPolicy{Role: RoleReviewer}
	if ok, _ := empty.IsToolCallAllowed("bash", map[string]interface{}{"command": "ls"}); ok {
		t.Fatal("nil allow-list must fail closed")
	}
}
