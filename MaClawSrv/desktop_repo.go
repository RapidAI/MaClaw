package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/gitforge"
)

func init() {
	agentservice.DesktopRepo = desktopRepoCall
}

// desktopRepoHTTP is the forge API client. Tests replace its transport.
var desktopRepoHTTP = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("redirect refused")
	},
}

func desktopRepoCall(ctx context.Context, call agentservice.RepoCall) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	action := strings.ToLower(strings.TrimSpace(stringArg(call.Args, "action")))
	scope := agentruntime.Scope{
		TenantID:   strings.TrimSpace(call.TenantID),
		UserID:     desktop.BotDesktopUser(call.UserID, call.BotID),
		RunID:      strings.TrimSpace(call.RunID),
		SessionID:  strings.TrimSpace(call.SessionID),
		InstanceID: strings.TrimSpace(call.InstanceID),
	}
	desktopEnsureRepoAuth(scope.TenantID, desktopRepoBot(scope))
	if yes, ok := desktopbot.DangerAnswer(call.UserText); ok {
		prefix := strings.Join([]string{scope.TenantID, scope.UserID, scope.InstanceID}, "\x00") + "\x00"
		desktopbot.AnswerDanger(prefix, yes, desktopbot.DangerNow())
	}
	if strings.TrimSpace(call.Phase) == "plan" && !repoPlanAction(action) {
		return "", fmt.Errorf("%s", desktopbot.PlanPhaseNextStep)
	}
	switch action {
	case "connect":
		return desktopRepoConnect(ctx, scope, call.Args)
	case "repos":
		return desktopRepoRepos(ctx, scope, call.Args)
	case "grant":
		return desktopRepoGrant(scope, call.Args)
	case "whoami", "issues", "issue", "comment", "runs", "run_log", "request", "dispatch", "release", "merge":
		if action == "dispatch" || action == "release" || action == "merge" {
			if action == "release" {
				if call.Args == nil {
					call.Args = map[string]interface{}{}
				}
				if err := desktopRepoPinRelease(ctx, scope, call.Args); err != nil {
					return "", err
				}
			}
			if err := refuseDesktopDanger(ctx, scope, action, map[string]any{"command": repoDangerCommand(action, call.Args)}); err != nil {
				return "", err
			}
		}
		return desktopRepoAPI(ctx, scope, action, call.Args)
	case "pr":
		if err := refuseDesktopDanger(ctx, scope, "pr", map[string]any{"command": repoDangerCommand("pr", call.Args)}); err != nil {
			return "", err
		}
		return desktopRepoPR(ctx, scope, call.Args)
	case "clone", "pull", "status", "commit", "push", "branch", "diff", "log", "sync":
		if action == "push" {
			dest, destErr := desktopRepoDest(scope, stringArg(call.Args, "path"), "")
			if destErr != nil {
				return "", destErr
			}
			branch, branchErr := desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dest)+" rev-parse --abbrev-ref HEAD")
			if branchErr != nil {
				return "", branchErr
			}
			command, commandErr := repoPushCommand(branch)
			if commandErr != nil {
				return "", commandErr
			}
			if err := refuseDesktopDanger(ctx, scope, "bash", map[string]any{"command": command}); err != nil {
				return "", err
			}
			// The model can pass another ref. The push uses the branch just read.
			args := cloneArgs(call.Args)
			args["ref"] = branch
			call.Args = args
		}
		return desktopRepoGit(ctx, scope, action, call.Args)
	default:
		return "", fmt.Errorf("unknown repo action %s", action)
	}
}

func repoPlanAction(action string) bool {
	switch action {
	case "whoami", "issues", "issue", "status", "runs", "run_log", "request", "diff", "log", "connect", "repos", "grant":
		return true
	default:
		return false
	}
}

func repoPushCommand(branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("checkout is not on a branch")
	}
	if len(branch) > 200 || strings.Contains(branch, "..") || strings.ContainsAny(branch, "\r\n\x00 '\"\\$`;|&<>") || strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("branch name is invalid")
	}
	return "git push origin " + branch, nil
}

func repoDangerCommand(action string, args map[string]interface{}) string {
	repo := strings.TrimSpace(stringArg(args, "repo"))
	switch action {
	case "dispatch":
		ref := strings.TrimSpace(stringArg(args, "ref"))
		if ref == "" {
			ref = "the default branch"
		}
		return repo + " @ " + ref
	case "release":
		tag := strings.TrimSpace(stringArg(args, "tag"))
		if tag == "" {
			tag = "a tag"
		}
		target := strings.TrimSpace(stringArg(args, "ref"))
		if target == "" {
			return repo + " " + tag + " @ default branch"
		}
		if branch := strings.TrimSpace(stringArg(args, "release_branch")); repoBranchLabel(branch) {
			return repo + " " + tag + " @ " + branch + " " + target
		}
		return repo + " " + tag + " @ " + target
	case "pr":
		head := strings.TrimSpace(stringArg(args, "ref"))
		if head == "" {
			head = "this branch"
		}
		return repo + " " + head
	case "merge":
		number := strings.TrimSpace(stringArg(args, "number"))
		if number == "" {
			number = "the open request"
		}
		return repo + " #" + number
	default:
		return repo
	}
}

func desktopRepoSecrets(scope agentruntime.Scope) (github, gitlab, gitlabHost string) {
	all := desktopSecretsForScope(scope)
	if all == nil {
		return "", "", ""
	}
	return all["GITHUB_TOKEN"], all["GITLAB_TOKEN"], all["GITLAB_HOST"]
}

func desktopRepoMissingToken(service string) error {
	name := gitforge.TokenEnv(service)
	return fmt.Errorf("还没有授权 %s。调用 action=connect，把返回的链接发给这个人，让他在页面上确认组织和权限。不要让他粘贴 token，也不要自己打开网站。自建 GitLab 先保存 GITLAB_HOST=https://主机", name)
}

func desktopRepoToken(service, github, gitlab string) (string, error) {
	if service == gitforge.GitLab {
		if strings.TrimSpace(gitlab) == "" {
			return "", desktopRepoMissingToken(service)
		}
		return gitlab, nil
	}
	if strings.TrimSpace(github) == "" {
		return "", desktopRepoMissingToken(gitforge.GitHub)
	}
	return github, nil
}

func desktopRepoAPI(ctx context.Context, scope agentruntime.Scope, action string, args map[string]interface{}) (string, error) {
	github, gitlab, gitlabHost := desktopRepoSecrets(scope)
	host := strings.ToLower(strings.TrimSpace(stringArg(args, "host")))
	repoArg := strings.TrimSpace(stringArg(args, "repo"))
	var repo gitforge.Repo
	var err error
	if action == "whoami" && repoArg == "" {
		if host == "" {
			host = gitforge.GitHub
			if github == "" && gitlab != "" {
				host = gitforge.GitLab
			}
		}
		repo, err = gitforge.Parse("maclaw/whoami", host, gitlabHost)
	} else {
		repo, err = gitforge.Parse(repoArg, host, gitlabHost)
	}
	if err != nil {
		return "", err
	}
	token, err := desktopRepoToken(repo.Service, github, gitlab)
	if err != nil {
		return "", err
	}
	if action != "whoami" {
		if err := desktopRepoAdmit(scope, repo); err != nil {
			return "", err
		}
	}
	client := desktopRepoClient(scope, token, repo.Service)
	var text string
	switch action {
	case "whoami":
		text, err = client.WhoAmI(ctx, repo)
	case "issues":
		text, err = client.Issues(ctx, repo, stringArg(args, "state"), repoLimit(args))
	case "issue":
		text, err = client.Issue(ctx, repo, stringArg(args, "number"))
	case "request":
		text, err = client.Request(ctx, repo, stringArg(args, "number"))
	case "comment":
		text, err = client.Comment(ctx, repo, stringArg(args, "number"), stringArg(args, "body"))
	case "runs":
		text, err = client.Runs(ctx, repo, stringArg(args, "ref"))
	case "merge":
		text, err = client.Merge(ctx, repo, stringArg(args, "number"))
	case "run_log":
		text, err = client.RunLog(ctx, repo, stringArg(args, "number"))
	case "dispatch":
		text, err = client.Dispatch(ctx, repo, stringArg(args, "workflow"), stringArg(args, "ref"))
	case "release":
		text, err = desktopRepoRelease(ctx, scope, client, repo, args)
	default:
		err = fmt.Errorf("unknown repo action %s", action)
	}
	secrets := map[string]string{}
	if github != "" {
		secrets["GITHUB_TOKEN"] = github
	}
	if gitlab != "" {
		secrets["GITLAB_TOKEN"] = gitlab
	}
	if err != nil {
		return "", fmt.Errorf("%s", desktopScrubSecrets(err.Error(), secrets))
	}
	return desktopScrubSecrets(text, secrets), nil
}

func repoLimit(args map[string]interface{}) int {
	n := srvScheduleIntArg(args, "limit", 15)
	if n < 1 || n > 20 {
		return 15
	}
	return n
}

func desktopRepoGit(ctx context.Context, scope agentruntime.Scope, action string, args map[string]interface{}) (string, error) {
	github, gitlab, gitlabHost := desktopRepoSecrets(scope)
	host := strings.ToLower(strings.TrimSpace(stringArg(args, "host")))
	var repo gitforge.Repo
	var err error
	switch action {
	case "clone":
		repo, err = gitforge.Parse(stringArg(args, "repo"), host, gitlabHost)
		if err != nil {
			return "", err
		}
	case "pull", "push", "pushhead", "sync":
		dest, destErr := desktopRepoDest(scope, stringArg(args, "path"), "")
		if destErr != nil {
			return "", destErr
		}
		repo, err = desktopRepoOrigin(ctx, scope, dest, host, gitlabHost)
		if err != nil {
			return "", err
		}
		args = cloneArgs(args)
		args["path"] = dest
		if action == "sync" {
			base := stringArg(args, "base")
			if base == "" {
				base = stringArg(args, "ref")
			}
			if base == "" {
				line, lineErr := desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dest)+" symbolic-ref --short refs/remotes/origin/HEAD")
				if lineErr != nil || strings.TrimSpace(line) == "" {
					return "", fmt.Errorf("base branch is required")
				}
				base = strings.TrimPrefix(strings.TrimSpace(line), "origin/")
			}
			args["ref"] = base
		}
	}
	if err != nil {
		return "", err
	}
	tokenEnv := ""
	if action == "clone" || action == "pull" || action == "push" || action == "pushhead" || action == "sync" {
		if _, tokenErr := desktopRepoToken(repo.Service, github, gitlab); tokenErr != nil {
			return "", tokenErr
		}
		tokenEnv = gitforge.TokenEnv(repo.Service)
	}
	if repo.Path != "" {
		if err := desktopRepoAdmit(scope, repo); err != nil {
			return "", err
		}
	}
	name := "repo"
	if repo.Name != "" {
		name = repo.Name
	}
	dest, err := desktopRepoDest(scope, stringArg(args, "path"), name)
	if err != nil {
		return "", err
	}
	script, err := gitforge.Script(action, repo, dest, stringArg(args, "ref"), stringArg(args, "message"), tokenEnv)
	if err != nil {
		return "", err
	}
	var names []string
	if tokenEnv != "" {
		names = []string{tokenEnv}
	}
	text, err := desktopContainerBashSecrets(ctx, scope, script, names)
	secrets := map[string]string{}
	if github != "" {
		secrets["GITHUB_TOKEN"] = github
	}
	if gitlab != "" {
		secrets["GITLAB_TOKEN"] = gitlab
	}
	text = desktopScrubSecrets(text, secrets)
	if err != nil {
		if strings.TrimSpace(text) == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s", desktopScrubSecrets(text, secrets))
	}
	if action == "clone" || action == "pull" || action == "commit" || action == "push" || action == "branch" || action == "sync" {
		text = strings.TrimSpace(text) + "\ncheckout " + dest
	}
	return text, nil
}

func desktopRepoOrigin(ctx context.Context, scope agentruntime.Scope, dest, host, gitlabHost string) (gitforge.Repo, error) {
	text, err := desktopContainerBashSecrets(ctx, scope, "git -C "+bashSingleQuote(dest)+" remote get-url origin", nil)
	if err != nil {
		if strings.TrimSpace(text) == "" {
			text = err.Error()
		}
		return gitforge.Repo{}, fmt.Errorf("%s", strings.TrimSpace(text))
	}
	remote := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "(") {
			remote = line
		}
	}
	if remote == "" {
		return gitforge.Repo{}, fmt.Errorf("no origin in %s", dest)
	}
	return gitforge.Parse(remote, host, gitlabHost)
}

func desktopRepoDest(scope agentruntime.Scope, raw, name string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("path is empty")
		}
		root := desktopRunWorkDir(scope)
		if root == "" {
			_, bot := desktop.SplitDesktopUser(scope.UserID)
			if bot == "" {
				bot = "main"
			}
			root = path.Join("/home/desktop/work", desktopWorkdirSafe.ReplaceAllString(bot, "_"))
		}
		raw = path.Join(root, name)
	}
	return desktop.ContainerPath(raw)
}

func desktopRepoPinRelease(ctx context.Context, scope agentruntime.Scope, args map[string]interface{}) error {
	if strings.TrimSpace(stringArg(args, "ref")) != "" {
		return nil
	}
	dir, err := desktopRepoReleaseDir(scope, args)
	if err != nil || dir == "" {
		return err
	}
	sha, err := desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dir)+" rev-parse HEAD")
	if err != nil || !repoCommitSHA(sha) {
		return fmt.Errorf("release ref is required")
	}
	args["ref"] = sha
	branch, branchErr := desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dir)+" rev-parse --abbrev-ref HEAD")
	if branchErr == nil && repoBranchLabel(branch) {
		args["release_branch"] = branch
	}
	return nil
}

func desktopRepoReleaseDir(scope agentruntime.Scope, args map[string]interface{}) (string, error) {
	if strings.TrimSpace(stringArg(args, "path")) != "" {
		return desktopRepoDest(scope, stringArg(args, "path"), "")
	}
	asset := strings.TrimSpace(stringArg(args, "asset"))
	if asset == "" {
		return "", nil
	}
	file, _, err := desktopRepoAsset(asset)
	if err != nil {
		return "", err
	}
	dir := path.Dir(file)
	if dir == "" || dir == "." || dir == "/" {
		return "", fmt.Errorf("release ref is required")
	}
	return dir, nil
}

func repoCommitSHA(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, r := range sha {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func repoBranchLabel(branch string) bool {
	if branch == "" || branch == "HEAD" || len(branch) > 200 || strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, "\r\n\x00 '\"\\$`;|&<>") {
		return false
	}
	return true
}

func desktopRepoRelease(ctx context.Context, scope agentruntime.Scope, client *gitforge.Client, repo gitforge.Repo, args map[string]interface{}) (string, error) {
	tag := stringArg(args, "tag")
	asset := strings.TrimSpace(stringArg(args, "asset"))
	var assetURL, assetName, file string
	if asset != "" && repo.Service == gitforge.GitLab {
		var err error
		file, assetName, err = desktopRepoAsset(asset)
		if err != nil {
			return "", err
		}
		assetURL, err = gitforge.PackageURL(repo, tag, assetName)
		if err != nil {
			return "", err
		}
		if err = desktopRepoCurl(ctx, scope, repo, file, assetURL, assetName); err != nil {
			return "", err
		}
	}
	ship, err := client.CreateRelease(ctx, repo, tag, stringArg(args, "name"), stringArg(args, "body"), stringArg(args, "ref"), assetURL, assetName)
	if err != nil {
		return "", err
	}
	text := ship.Summary
	if asset != "" && repo.Service == gitforge.GitHub {
		file, assetName, err = desktopRepoAsset(asset)
		if err != nil {
			return text + "\nasset failed: " + err.Error(), nil
		}
		if strings.TrimSpace(ship.Upload) == "" {
			return text + "\nasset failed: the release has no upload url", nil
		}
		upload := ship.Upload + "?name=" + url.QueryEscape(assetName)
		if err = desktopRepoCurl(ctx, scope, repo, file, upload, assetName); err != nil {
			return text + "\nasset failed: " + err.Error(), nil
		}
		text += "\nattached " + assetName
	}
	if asset != "" && repo.Service == gitforge.GitLab {
		text += "\nattached " + assetName
	}
	return text, nil
}

func desktopRepoPR(ctx context.Context, scope agentruntime.Scope, args map[string]interface{}) (string, error) {
	dest, err := desktopRepoDest(scope, stringArg(args, "path"), "")
	if err != nil {
		return "", err
	}
	args = cloneArgs(args)
	args["path"] = dest
	if ref := stringArg(args, "ref"); ref != "" {
		if _, err = desktopRepoGit(ctx, scope, "switch", args); err != nil {
			return "", err
		}
	}
	head, err := desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dest)+" rev-parse --abbrev-ref HEAD")
	if err != nil {
		return "", err
	}
	if head == "" || head == "HEAD" {
		return "", fmt.Errorf("checkout is not on a branch")
	}
	base := stringArg(args, "base")
	if base == "" {
		base, err = desktopRepoGitLine(ctx, scope, "git -C "+bashSingleQuote(dest)+" symbolic-ref --short refs/remotes/origin/HEAD")
		if err != nil {
			return "", fmt.Errorf("base branch is required")
		}
		base = strings.TrimPrefix(base, "origin/")
	}
	base = gitforge.TrackingBranch(base)
	if head == base {
		return "", fmt.Errorf("checkout is on %s, which is the base branch. action=branch first. nothing was pushed", head)
	}
	repo, err := desktopRepoOrigin(ctx, scope, dest, strings.ToLower(stringArg(args, "host")), desktopRepoHost(scope))
	if err != nil {
		return "", err
	}
	if err := desktopRepoAdmit(scope, repo); err != nil {
		return "", err
	}
	pushed, err := desktopRepoGit(ctx, scope, "pushhead", args)
	if err != nil {
		return "", err
	}
	github, gitlab, _ := desktopRepoSecrets(scope)
	token, err := desktopRepoToken(repo.Service, github, gitlab)
	if err != nil {
		return "", err
	}
	title := stringArg(args, "name")
	if title == "" {
		title = stringArg(args, "message")
	}
	opened, err := desktopRepoClient(scope, token, repo.Service).PullRequest(ctx, repo, head, base, title, stringArg(args, "body"))
	if err != nil {
		return "", err
	}
	secrets := map[string]string{}
	if github != "" {
		secrets["GITHUB_TOKEN"] = github
	}
	if gitlab != "" {
		secrets["GITLAB_TOKEN"] = gitlab
	}
	return desktopScrubSecrets(strings.TrimSpace(pushed)+"\n"+opened, secrets), nil
}

func desktopRepoHost(scope agentruntime.Scope) string {
	_, _, host := desktopRepoSecrets(scope)
	return host
}

func desktopRepoGitLine(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	text, err := desktopContainerBashSecrets(ctx, scope, command, nil)
	if err != nil {
		if strings.TrimSpace(text) == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(text))
	}
	line := ""
	for _, part := range strings.Split(text, "\n") {
		part = strings.TrimSpace(part)
		if part != "" && !strings.HasPrefix(part, "(") {
			line = part
		}
	}
	return line, nil
}

func desktopRepoAsset(raw string) (string, string, error) {
	file, err := desktop.ContainerPath(raw)
	if err != nil {
		return "", "", err
	}
	name := path.Base(file)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", "", fmt.Errorf("asset name is invalid")
	}
	return file, name, nil
}

func desktopRepoCurl(ctx context.Context, scope agentruntime.Scope, repo gitforge.Repo, file, uploadURL, name string) error {
	if err := gitforge.AllowedUpload(repo, uploadURL); err != nil {
		return err
	}
	script, err := gitforge.UploadScript(repo.Service, gitforge.TokenEnv(repo.Service), file, uploadURL, name, repo.Service == gitforge.GitLab && desktopGitLabUsesBearer(desktopRepoBot(scope)))
	if err != nil {
		return err
	}
	text, err := desktopContainerBashSecrets(ctx, scope, script, []string{gitforge.TokenEnv(repo.Service)})
	github, gitlab, _ := desktopRepoSecrets(scope)
	secrets := map[string]string{}
	if github != "" {
		secrets["GITHUB_TOKEN"] = github
	}
	if gitlab != "" {
		secrets["GITLAB_TOKEN"] = gitlab
	}
	if err != nil {
		if strings.TrimSpace(text) == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s", desktopScrubSecrets(text, secrets))
	}
	return nil
}

func cloneArgs(args map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range args {
		out[key] = value
	}
	return out
}
