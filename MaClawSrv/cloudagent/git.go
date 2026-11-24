package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/gitforge"
)

const (
	commitTimeout  = 30 * time.Second
	pushTimeout    = 2 * time.Minute
	receiptTimeout = 30 * time.Second
	cloneTimeout   = 2 * time.Minute

	// pushApprovalArgs is the call shape ApplyCommitGate already treats as a
	// human git push. The push itself is not a shell command.
	pushApprovalArgs = `{"command":"git push"}`
)

// GitConnect is the coding-instance credential maclawsrv stores in git.json.
// Hub forwards it and does not keep the token.
type GitConnect struct {
	Service     string `json:"service"`
	Origin      string `json:"origin"`
	Repo        string `json:"repo"`
	Ref         string `json:"ref,omitempty"`
	Token       string `json:"token,omitempty"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	// Source marks how the token was provided. "account" means it came from
	// the signed-in GitHub account and may be refreshed by the client when
	// that account re-logs in; empty is a manually pasted token.
	Source string `json:"source,omitempty"`
}

// GitPublic is the git view without a token.
type GitPublic struct {
	Connected   bool   `json:"connected"`
	Service     string `json:"service,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	AuthorName  string `json:"author_name,omitempty"`
	AuthorEmail string `json:"author_email,omitempty"`
	Source      string `json:"source,omitempty"`
}

type gitSecret struct {
	Service     string `json:"service"`
	Origin      string `json:"origin"`
	Repo        string `json:"repo"`
	Clone       string `json:"clone"`
	Ref         string `json:"ref"`
	Token       string `json:"token"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	Source      string `json:"source,omitempty"`
}

func (rt *Runtime) ServeGitGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, rt.GitView(r.PathValue("id")))
}

func (rt *Runtime) ServeGitPut(w http.ResponseWriter, r *http.Request) {
	var in GitConnect
	if !decodeJSON(w, r, &in) {
		return
	}
	view, err := rt.ConnectGit(r.Context(), r.PathValue("id"), in)
	if err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (rt *Runtime) ServeGitDelete(w http.ResponseWriter, r *http.Request) {
	if err := rt.DisconnectGit(r.PathValue("id")); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GitView reports the connected repository. A missing file is not an error.
func (rt *Runtime) GitView(id string) GitPublic {
	inst, err := rt.instance(id)
	if err != nil || inst.meta.Kind != KindCoding {
		return GitPublic{}
	}
	secret, err := readGit(inst.dir)
	if err != nil {
		return GitPublic{}
	}
	return secret.public()
}

// ConnectGit stores a coding credential and checks out the one primary repo.
// A session instance is refused before any file is written.
func (rt *Runtime) ConnectGit(ctx context.Context, id string, in GitConnect) (GitPublic, error) {
	inst, err := rt.instance(id)
	if err != nil {
		return GitPublic{}, err
	}
	if inst.meta.Kind != KindCoding {
		return GitPublic{}, errSessionGit
	}
	secret, err := parseGitConnect(in)
	if err != nil {
		return GitPublic{}, err
	}
	if err := rt.checkout(ctx, inst, secret); err != nil {
		return GitPublic{}, err
	}
	if err := writeGit(inst.dir, secret); err != nil {
		return GitPublic{}, err
	}
	return secret.public(), nil
}

// DisconnectGit removes git.json. The checkout stays on disk.
func (rt *Runtime) DisconnectGit(id string) error {
	inst, err := rt.instance(id)
	if err != nil {
		return err
	}
	if inst.meta.Kind != KindCoding {
		return errSessionGit
	}
	err = os.Remove(gitPath(inst.dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func parseGitConnect(in GitConnect) (gitSecret, error) {
	in.Service = strings.TrimSpace(in.Service)
	in.Origin = strings.TrimSpace(in.Origin)
	in.Repo = strings.TrimSpace(in.Repo)
	in.Ref = strings.TrimSpace(in.Ref)
	in.Token = strings.TrimSpace(in.Token)
	in.AuthorName = strings.TrimSpace(in.AuthorName)
	in.AuthorEmail = strings.TrimSpace(in.AuthorEmail)
	if in.AuthorName == "" || in.AuthorEmail == "" || strings.Contains(in.AuthorEmail, " ") || !strings.Contains(in.AuthorEmail, "@") {
		return gitSecret{}, errors.New("请填写提交作者的姓名和邮箱")
	}
	if strings.EqualFold(in.AuthorEmail, "maclaw-bot@local") {
		return gitSecret{}, errors.New("提交不能使用 maclaw-bot@local")
	}
	if in.Token == "" {
		return gitSecret{}, errors.New("请填写访问令牌")
	}
	if err := safeRef(in.Ref); err != nil {
		return gitSecret{}, err
	}
	if in.Service != gitforge.GitHub && in.Service != gitforge.GitLab {
		return gitSecret{}, errors.New("代码库只支持 GitHub 或 GitLab")
	}
	if gitRejected(in.Repo) || gitRejected(in.Origin) {
		return gitSecret{}, errors.New("代码库地址只接受 HTTPS")
	}
	gitlabHost := ""
	if in.Service == gitforge.GitLab {
		gitlabHost = in.Origin
	}
	parsed, err := gitforge.Parse(in.Repo, in.Service, gitlabHost)
	if err != nil {
		return gitSecret{}, errors.New("代码库地址只接受 HTTPS")
	}
	if in.Service == gitforge.GitHub && in.Origin != "" {
		if gitRejected(in.Origin) || !strings.EqualFold(hostOf(in.Origin), "github.com") {
			return gitSecret{}, errors.New("GitHub 地址无效")
		}
	}
	origin := "https://github.com"
	if parsed.Service == gitforge.GitLab {
		origin = strings.TrimSuffix(parsed.Web, "/"+parsed.Path)
	}
	if in.Origin != "" && parsed.Service == gitforge.GitHub {
		origin = "https://github.com"
	}
	source := ""
	if strings.EqualFold(strings.TrimSpace(in.Source), "account") {
		source = "account"
	}
	return gitSecret{
		Service:     parsed.Service,
		Origin:      origin,
		Repo:        parsed.Path,
		Clone:       parsed.Clone,
		Ref:         in.Ref,
		Token:       in.Token,
		AuthorName:  in.AuthorName,
		AuthorEmail: in.AuthorEmail,
		Source:      source,
	}, nil
}

func gitRejected(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") || strings.HasPrefix(lower, "git@") || strings.Contains(lower, "git@")
}

func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.Trim(raw, "/")
	if i := strings.IndexAny(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func safeRef(ref string) error {
	if ref == "" {
		return nil
	}
	if len(ref) > 200 || strings.ContainsAny(ref, " \t\r\n\x00~^:?*[\\") || strings.Contains(ref, "..") || strings.HasPrefix(ref, "-") {
		return errors.New("分支名称无效")
	}
	return nil
}

func (s gitSecret) public() GitPublic {
	return GitPublic{
		Connected:   true,
		Service:     s.Service,
		Origin:      s.Origin,
		Repo:        s.Repo,
		Ref:         s.Ref,
		AuthorName:  s.AuthorName,
		AuthorEmail: s.AuthorEmail,
		Source:      s.Source,
	}
}

func gitPath(dir string) string {
	return filepath.Join(dir, "git.json")
}

// repoDirName is the last slash-separated segment. filepath.Base leaves
// "acme/demo" intact on Windows, where slash is not a separator.
func repoDirName(repo string) (string, error) {
	name := path.Base(strings.Trim(filepath.ToSlash(repo), "/"))
	if name == "." || name == ".." || name == "" || strings.ContainsAny(name, `/\`) {
		return "", errors.New("代码库名称无效")
	}
	return name, nil
}

func readGit(dir string) (gitSecret, error) {
	raw, err := os.ReadFile(gitPath(dir))
	if err != nil {
		return gitSecret{}, errors.New("还没有连接代码库")
	}
	var secret gitSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return gitSecret{}, err
	}
	if strings.TrimSpace(secret.Token) == "" || strings.TrimSpace(secret.Clone) == "" {
		return gitSecret{}, errors.New("代码库连接不完整")
	}
	return secret, nil
}

func writeGit(dir string, secret gitSecret) error {
	raw, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	return writeFile(gitPath(dir), raw, 0o600)
}

// noHooks pins hooks off for the srv's own git ops. File tools refuse .git
// paths, but a shell-scripted write could still land a hook — the srv's
// commit and push must never execute repo-authored code. The -c flag wins
// over any include.path a scripted config edit might add.
const noHooks = "core.hooksPath=" + os.DevNull

// checkout clones the one primary repository into workspace/<name>.
// A second repository is refused. The token is passed only through askpass.
func (rt *Runtime) checkout(ctx context.Context, inst *instance, secret gitSecret) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("这台机器没有 git")
	}
	workspace := filepath.Join(inst.dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	name, err := repoDirName(secret.Repo)
	if err != nil {
		return err
	}
	secrets := secretValues(inst.dir)
	if token := strings.TrimSpace(secret.Token); token != "" {
		secrets = append(secrets, token)
	}
	dest := filepath.Join(workspace, name)
	// The stored binding decides what "already connected" means. A scripted
	// .git directory cannot forge git.json (it lives outside the jail), so
	// fake checkouts demote to ordinary loose files and never block a
	// connect; a bound instance still refuses a second repository.
	if prev, err := readGit(inst.dir); err == nil {
		if prev.Repo != secret.Repo {
			return errors.New("这个实例已经有一个代码库")
		}
		if info, statErr := os.Stat(filepath.Join(dest, ".git")); statErr == nil && info.IsDir() {
			return setLocalIdentity(ctx, dest, secret, secrets)
		}
		// Bound but the checkout was lost — fall through and re-clone.
	}
	// A repo-less turn can leave a workspace directory named exactly like the
	// repository. It is not a checkout (those were caught above), so park it
	// aside and merge its contents back after the clone lands. The stash name
	// is unique: a stale .pre-checkout leftover must not block the rename.
	stash := ""
	if _, err := os.Stat(dest); err == nil {
		// A loose FILE named like the repo parks the same way — renaming
		// works for files too, and the sweep below carries it into the
		// clone instead of deleting it.
		stash = dest + ".pre-checkout-" + newID("")
		if err := os.Rename(dest, stash); err != nil {
			return errors.New("检出目录已经存在")
		}
	}
	restore := func() {
		_ = os.RemoveAll(dest)
		if stash != "" {
			_ = os.Rename(stash, dest)
		}
	}
	args := []string{"clone"}
	if secret.Ref != "" {
		args = append(args, "--branch", secret.Ref)
	}
	args = append(args, "--", secret.Clone, dest)
	if err := withAskpass(inst.dir, secret, secrets, func(env []string) error {
		out, runErr := gitRun(ctx, workspace, env, cloneTimeout, args...)
		if runErr != nil {
			return errors.New(scrubToken(secret, out+" "+runErr.Error()))
		}
		return nil
	}); err != nil {
		restore()
		return errors.New("检出代码库失败: " + err.Error())
	}
	if err := setLocalIdentity(ctx, dest, secret, secrets); err != nil {
		restore()
		return err
	}
	mergeWorkspaceLoose(workspace, dest, stash, inst.meta.ID)
	return nil
}

// mergeWorkspaceLoose folds pre-checkout work into the fresh clone. While no
// repository was connected the jail root was the workspace itself, so loose
// files sit at its top level; once the checkout lands the root moves into it
// and unmoved files would silently leave the jail. Entries move one by one;
// when a loose entry collides with clone content the clone keeps the name
// and the loose copy lands beside it as <name>.pre-checkout, so nothing the
// agent wrote is ever deleted or stranded outside the jail.
func mergeWorkspaceLoose(workspace, dest, stash, instanceID string) {
	move := func(from string) {
		// A bare gitdir never merges into the clone: at the top level it
		// would land beside the real .git and `git add -A` would sweep its
		// objects into the index as junk files. Left in place it instead
		// rides into the repo inside its stash dir as an embedded repo.
		if filepath.Base(from) == ".git" {
			return
		}
		target := filepath.Join(dest, filepath.Base(from))
		// os.Rename replaces an existing target on POSIX — check first so the
		// clone's own files win collisions on every platform.
		if _, err := os.Lstat(target); err == nil {
			target += ".pre-checkout"
			if _, err := os.Lstat(target); err == nil {
				slog.Warn("cloudagent checkout merge kept clone entry",
					slog.String("instance", instanceID), slog.String("path", filepath.Base(from)))
				return
			}
		}
		if err := os.Rename(from, target); err != nil {
			slog.Warn("cloudagent checkout merge skipped entry",
				slog.String("instance", instanceID), slog.String("path", filepath.Base(from)),
				slog.String("reason", err.Error()))
		}
	}
	if stash != "" {
		// A parked file has no entries to merge — the sweep below moves it
		// into the repo wholesale. Only directories get emptied by move.
		if info, err := os.Stat(stash); err == nil && info.IsDir() {
			if entries, err := os.ReadDir(stash); err == nil {
				for _, entry := range entries {
					move(filepath.Join(stash, entry.Name()))
				}
			}
			// Remove only when empty: colliding leftovers stay put and the
			// workspace sweep below carries them into the repo, not the void.
			_ = os.Remove(stash)
		}
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if filepath.Join(workspace, entry.Name()) == dest {
			continue
		}
		move(filepath.Join(workspace, entry.Name()))
	}
}

func setLocalIdentity(ctx context.Context, repo string, secret gitSecret, secrets []string) error {
	env := gitEnv(secrets)
	if _, err := gitRun(ctx, repo, env, commitTimeout, "config", "--local", "user.name", secret.AuthorName); err != nil {
		return errors.New("无法写入仓库作者")
	}
	if _, err := gitRun(ctx, repo, env, commitTimeout, "config", "--local", "user.email", secret.AuthorEmail); err != nil {
		return errors.New("无法写入仓库作者")
	}
	return nil
}

// Commit stages tracked changes and commits with the repo-local author.
// It does not push.
func (rt *Runtime) Commit(ctx context.Context, id, message string) (string, error) {
	inst, err := rt.instance(id)
	if err != nil {
		return "", err
	}
	if inst.meta.Kind != KindCoding {
		return "", errSessionGit
	}
	secret, err := readGit(inst.dir)
	if err != nil {
		return "", err
	}
	root, err := jailRoot(inst.dir, KindCoding)
	if err != nil {
		return "", err
	}
	return commitRepo(ctx, root, secret, message, secretValues(inst.dir))
}

func commitRepo(ctx context.Context, repo string, secret gitSecret, message string, secrets []string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" || strings.ContainsAny(message, "\r\n\x00") || len(message) > 2000 {
		return "", errors.New("提交说明无效")
	}
	if strings.EqualFold(secret.AuthorEmail, "maclaw-bot@local") {
		return "", errors.New("提交不能使用 maclaw-bot@local")
	}
	env := gitEnv(secrets)
	if err := setLocalIdentity(ctx, repo, secret, secrets); err != nil {
		return "", err
	}
	before, _ := gitRun(ctx, repo, env, commitTimeout, "rev-parse", "HEAD")
	before = strings.TrimSpace(before)
	// -A, not -u: files scaffolded before the repo was connected land in the
	// clone untracked, and the scaffold→connect→commit flow is the whole
	// point. rejectSecretIndex below un-stages anything secret-shaped.
	if _, err := gitRun(ctx, repo, env, commitTimeout, "add", "-A"); err != nil {
		return "", err
	}
	if err := rejectSecretIndex(ctx, repo, env); err != nil {
		return "", err
	}
	staged, err := gitRun(ctx, repo, env, commitTimeout, "diff", "--cached", "--name-only")
	if err != nil {
		return "", errors.New("host_repo_mutate_stage_unobserved")
	}
	if strings.TrimSpace(staged) == "" {
		return "", errors.New("host_repo_mutate_nothing_to_commit")
	}
	if _, err := gitRun(ctx, repo, env, commitTimeout, "-c", "user.name="+secret.AuthorName, "-c", "user.email="+secret.AuthorEmail, "-c", noHooks, "commit", "-m", message); err != nil {
		return "", err
	}
	digest, err := gitRun(ctx, repo, env, commitTimeout, "rev-parse", "HEAD")
	digest = strings.TrimSpace(digest)
	if err != nil || digest == "" || digest == before {
		return "", errors.New("host_repo_mutate_head_unobserved")
	}
	return "commit " + digest, nil
}

func rejectSecretIndex(ctx context.Context, repo string, env []string) error {
	raw, err := gitRun(ctx, repo, env, commitTimeout, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return errors.New("host_repo_mutate_stage_unobserved")
	}
	var blocked []string
	for _, name := range strings.Split(raw, "\x00") {
		name = strings.TrimSpace(name)
		if name == "" || !secretCommitPath(name) {
			continue
		}
		if _, err := gitRun(ctx, repo, env, commitTimeout, "reset", "-q", "--", name); err != nil {
			return err
		}
		blocked = append(blocked, name)
	}
	if len(blocked) > 0 {
		return errors.New("refusing to commit secret files: " + strings.Join(blocked, ", "))
	}
	return nil
}

func secretCommitPath(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case ".env.example", ".env.sample", ".env.template", ".env.dist":
		return false
	case ".env", "credentials", "credentials.json", ".npmrc", ".pypirc", ".netrc",
		"keystore.jks", "keystore.p12", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
		"git.json", "llm.json":
		return true
	}
	if strings.HasPrefix(base, ".env.") || strings.HasPrefix(base, ".maclaw-secret-") {
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".jks"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	low := strings.ToLower(filepath.ToSlash(name))
	return low == ".ssh" || strings.HasPrefix(low, ".ssh/") || strings.Contains(low, "/.ssh/")
}

// Push publishes the current branch only after the commit gate allows it.
// A nil gate refuses. An unknown receipt is not success.
func (rt *Runtime) Push(ctx context.Context, id, taskID string) (string, error) {
	inst, err := rt.instance(id)
	if err != nil {
		return "", err
	}
	if inst.meta.Kind != KindCoding {
		return "", errSessionGit
	}
	secret, err := readGit(inst.dir)
	if err != nil {
		return "", err
	}
	root, err := jailRoot(inst.dir, KindCoding)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(taskID) == "" {
		return "", errors.New("缺少任务")
	}
	return approvePush(ctx, newPushGate(inst, taskID, root), root, secret, secretValues(inst.dir))
}

func approvePush(ctx context.Context, host agent.LoopCallbacks, root string, secret gitSecret, secrets []string) (string, error) {
	if host == nil {
		return "", errNeedsPerson
	}
	res, blocked := agent.ApplyCommitGate(host, "bash", pushApprovalArgs)
	if blocked {
		if strings.TrimSpace(res.Result) == "" {
			return "", errNeedsPerson
		}
		return res.Result, errNeedsPerson
	}
	return pushRepo(ctx, root, secret, secrets)
}

func pushRepo(ctx context.Context, repo string, secret gitSecret, secrets []string) (string, error) {
	env := gitEnv(secrets)
	branch, err := gitRun(ctx, repo, env, commitTimeout, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if err != nil || branch == "" || branch == "HEAD" {
		return "", errors.New("host_repo_mutate_branch_unresolved")
	}
	local, err := gitRun(ctx, repo, env, commitTimeout, "rev-parse", "HEAD")
	local = strings.TrimSpace(local)
	if err != nil || local == "" {
		return "", errors.New("host_repo_mutate_head_unobserved")
	}
	upstream, upstreamErr := gitRun(ctx, repo, env, commitTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	remote, remoteBranch, ok := splitUpstream(upstream)
	if upstreamErr != nil || !ok {
		return "", errors.New("host_repo_mutate_upstream_unset")
	}
	// Push to the stored clone URL, never the configured remote name: a
	// shell `git remote set-url` must not be able to redirect the askpass
	// token to an arbitrary host.
	var pushErr error
	err = withAskpass(filepath.Dir(filepath.Dir(repo)), secret, secrets, func(authEnv []string) error {
		_, pushErr = gitRun(ctx, repo, authEnv, pushTimeout, "-c", noHooks, "push", "--", secret.Clone, "HEAD:refs/heads/"+remoteBranch)
		return nil
	})
	if err != nil {
		return "", err
	}
	var observed string
	var observeErr error
	err = withAskpass(filepath.Dir(filepath.Dir(repo)), secret, secrets, func(authEnv []string) error {
		observed, observeErr = gitRun(ctx, repo, authEnv, receiptTimeout, "ls-remote", "--exit-code", secret.Clone, "refs/heads/"+remoteBranch)
		return nil
	})
	if err != nil {
		return "", errors.New("host_repo_mutate_push_receipt_unknown")
	}
	fields := strings.Fields(strings.TrimSpace(observed))
	observed = ""
	if len(fields) > 0 {
		observed = fields[0]
	}
	switch {
	case observeErr != nil || observed == "":
		return "", errors.New("host_repo_mutate_push_receipt_unknown")
	case observed == local:
		return "push " + local + " -> " + remote + "/" + remoteBranch, nil
	case pushErr != nil:
		return "", errors.New("host_repo_mutate_push_rejected")
	default:
		return "", errors.New("host_repo_mutate_push_receipt_unknown")
	}
}

func scrubToken(secret gitSecret, text string) string {
	if secret.Token != "" {
		text = strings.ReplaceAll(text, secret.Token, "[secret]")
	}
	text = strings.TrimSpace(text)
	if len(text) > 500 {
		text = text[len(text)-500:]
	}
	return text
}

func splitUpstream(upstream string) (remote, branch string, ok bool) {
	upstream = strings.TrimSpace(upstream)
	remote, branch, found := strings.Cut(upstream, "/")
	remote, branch = strings.TrimSpace(remote), strings.TrimSpace(branch)
	if !found || remote == "" || branch == "" {
		return "", "", false
	}
	return remote, branch, true
}

func withAskpass(dir string, secret gitSecret, secrets []string, fn func(env []string) error) error {
	tokenEnv := gitforge.TokenEnv(secret.Service)
	script, err := writeAskpass(dir, secret.Service, tokenEnv)
	if err != nil {
		return err
	}
	defer os.Remove(script)
	env := gitEnv(secrets, "GIT_ASKPASS="+script, "GIT_ASKPASS_REQUIRE=force", tokenEnv+"="+secret.Token)
	return fn(env)
}

func writeAskpass(dir, service, tokenEnv string) (string, error) {
	if tokenEnv != "GITHUB_TOKEN" && tokenEnv != "GITLAB_TOKEN" {
		return "", errors.New("token env is not a forge secret")
	}
	user := gitforge.AskUser(service)
	if strings.ContainsAny(user, "\r\n\"%&|<>") {
		return "", errors.New("git 用户名无效")
	}
	name := "askpass-" + newID("")
	var body string
	if runtime.GOOS == "windows" {
		name += ".cmd"
		body = "@echo off\r\n" +
			"setlocal EnableExtensions\r\n" +
			"echo %~1 | findstr /I \"Username\" >nul\r\n" +
			"if not errorlevel 1 (\r\n" +
			"  echo " + user + "\r\n" +
			"  exit /b 0\r\n" +
			")\r\n" +
			"echo %" + tokenEnv + "%\r\n"
	} else {
		name += ".sh"
		body = "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s\\n' '" + user + "' ;;\n*) printf '%s\\n' \"$" + tokenEnv + "\" ;;\nesac\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func gitRun(ctx context.Context, dir string, env []string, timeout time.Duration, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	text := string(out)
	if runCtx.Err() != nil && ctx.Err() == nil {
		return text, runCtx.Err()
	}
	if err != nil {
		return text, err
	}
	return text, nil
}

func gitEnv(secrets []string, extra ...string) []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GITHUB_TOKEN": true, "GITLAB_TOKEN": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true,
		"MACLAW_ADMIN_SECRET": true,
	}
	var out []string
	ssh := false
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if drop[name] || (strings.Contains(strings.ToLower(name), "llm") && strings.Contains(strings.ToLower(name), "key")) {
			continue
		}
		if valueHasSecret(value, secrets) {
			continue
		}
		if strings.EqualFold(name, "GIT_SSH_COMMAND") {
			ssh = true
		}
		if strings.EqualFold(name, "GIT_TERMINAL_PROMPT") || strings.EqualFold(name, "GIT_ASKPASS") || strings.EqualFold(name, "GIT_ASKPASS_REQUIRE") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	if !ssh {
		out = append(out, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	out = clearCredentialHelper(out)
	return append(out, extra...)
}

// clearCredentialHelper drops the machine's saved GitHub or GitLab login.
// An empty credential.helper resets every earlier helper. A test that already
// set GIT_CONFIG_COUNT (the local insteadOf rewrite) keeps those entries.
func clearCredentialHelper(env []string) []string {
	count := 0
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if ok && name == "GIT_CONFIG_COUNT" {
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > count {
				count = n
			}
			continue
		}
		out = append(out, kv)
	}
	idx := strconv.Itoa(count)
	return append(out,
		"GIT_CONFIG_COUNT="+strconv.Itoa(count+1),
		"GIT_CONFIG_KEY_"+idx+"=credential.helper",
		"GIT_CONFIG_VALUE_"+idx+"=",
	)
}

// pushGate is the commit-approval host for one programming task.
// It does not resolve the ask inside the tool call.
type pushGate struct {
	inst *instance
	task string
	root string
}

func newPushGate(inst *instance, task, root string) *pushGate {
	return &pushGate{inst: inst, task: task, root: root}
}

func (g *pushGate) CommitWorkspaceRoot() string { return g.root }
func (g *pushGate) CommitTaskTempDir() string {
	return filepath.Join(g.inst.dir, "rollout")
}
func (g *pushGate) CommitChecklistOpen() bool { return false }
func (g *pushGate) CommitTaskKey() string     { return g.task }
func (g *pushGate) CommitAskPauses() bool     { return true }
func (g *pushGate) CommitLookup(fp string) (agent.CommitDecision, bool) {
	return g.inst.book.lookup(g.task, fp)
}
func (g *pushGate) CommitStore(fp string, decision agent.CommitDecision) error {
	return g.inst.book.storeDecision(g.task, fp, string(decision))
}
func (g *pushGate) CommitPrepareAsk(fp string) string {
	return g.inst.book.prepareAsk(g.task, fp)
}
func (g *pushGate) GetLLMConfig() corelib.MaclawLLMConfig { return corelib.MaclawLLMConfig{} }
func (g *pushGate) GetMaxIterations() int                 { return 30 }
func (g *pushGate) BuildSystemPrompt(string, bool) string { return "" }
func (g *pushGate) BuildTools(string) []map[string]interface{} {
	return nil
}
func (g *pushGate) ExecuteTool(string, string) string { return "" }
func (g *pushGate) OnToken(string)                    {}
func (g *pushGate) OnProgress(string)                 {}
func (g *pushGate) OnToolCall(string)                 {}
func (g *pushGate) OnToolResult(string)               {}
func (g *pushGate) ShouldStop() bool                  { return false }
