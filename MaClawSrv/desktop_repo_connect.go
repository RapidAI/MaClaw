package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/gitforge"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// A device-code login the person finishes in the browser. The device code
// stays in the poller. The model only receives the click link and the user code.
type repoAuthWait struct {
	mu     sync.Mutex
	link   string
	scopes string
	status string
	note   string
	cancel context.CancelFunc
}

var repoAuthWaits sync.Map

type repoDeviceStart func(ctx context.Context, service, scopes, gitlabHost string) (userCode, deviceCode string, interval int, err error)

type repoDevicePoll func(ctx context.Context, service, deviceCode, gitlabHost string, interval int) (string, error)

// Tests replace these. The defaults talk to GitHub and, when configured, GitLab.
var (
	desktopRepoStartDevice repoDeviceStart = desktopRepoStartDeviceLive
	desktopRepoPollDevice  repoDevicePoll  = desktopRepoPollDeviceLive
)

func desktopRepoAuthReset() {
	repoAuthWaits.Range(func(key, value any) bool {
		if wait, ok := value.(*repoAuthWait); ok && wait.cancel != nil {
			wait.cancel()
		}
		repoAuthWaits.Delete(key)
		return true
	})
}

func desktopRepoBot(scope agentruntime.Scope) string {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		bot = desktopBotOfInstance(scope.InstanceID)
	}
	if bot == "" {
		bot = "main"
	}
	return bot
}

func desktopRepoConnect(ctx context.Context, scope agentruntime.Scope, args map[string]interface{}) (string, error) {
	service := repoService(stringArg(args, "host"))
	scopes, err := gitforge.AuthorizeScopes(service, stringArg(args, "scopes"))
	if err != nil {
		return "", err
	}
	bot := desktopRepoBot(scope)
	if desktopRepoTokenOf(bot, service) != "" && strings.TrimSpace(stringArg(args, "scopes")) == "" {
		return desktopRepoConnectedText(bot, service), nil
	}
	key := bot + "\x00" + service
	if value, ok := repoAuthWaits.Load(key); ok {
		wait := value.(*repoAuthWait)
		wait.mu.Lock()
		same := wait.status == "pending" && wait.scopes == scopes && wait.link != ""
		link := wait.link
		wait.mu.Unlock()
		if same {
			return desktopRepoConnectText(service, scopes, link), nil
		}
	}
	gitlabHost := ""
	if service == gitforge.GitLab {
		_, _, gitlabHost = desktopRepoSecrets(scope)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	userCode, deviceCode, interval, err := desktopRepoStartDevice(ctx, service, scopes, gitlabHost)
	if err != nil && service == gitforge.GitHub && scopes != "read:user repo" {
		scopes = "read:user repo"
		userCode, deviceCode, interval, err = desktopRepoStartDevice(ctx, service, scopes, gitlabHost)
	}
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return "", fmt.Errorf("这台服务器还没有打开 GitLab 授权。GitHub 可以直接 action=connect。自建 GitLab 先保存 GITLAB_HOST")
		}
		return "", fmt.Errorf("授权没有开始")
	}
	link, err := gitforge.AuthorizeLink(service, gitlabHost, userCode)
	if err != nil {
		return "", err
	}
	pollCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	wait := &repoAuthWait{link: link, scopes: scopes, status: "pending", cancel: cancel}
	if previous, loaded := repoAuthWaits.Swap(key, wait); loaded {
		if old, ok := previous.(*repoAuthWait); ok && old.cancel != nil {
			old.cancel()
		}
	}
	go desktopRepoPoll(pollCtx, bot, service, gitlabHost, deviceCode, interval, wait)
	return desktopRepoConnectText(service, scopes, link), nil
}

func desktopRepoConnectText(service, scopes, link string) string {
	return "把这个链接发给这个人。他在页面上确认组织访问和这些权限：" + scopes + "\n" + link + "\n授权完成后让他回复一声。不要让他把 token 发到对话里，也不要自己打开这个网站。"
}

func desktopRepoConnectedText(bot, service string) string {
	if desktopRepoMustPick(bot, service) {
		return service + " 已经授权。调用 action=repos 把组织和仓库清单给这个人，他选定后用 action=grant。"
	}
	return service + " 已经授权。要改范围就再调用 action=repos，然后 action=grant。要换权限就带上 scopes 重新 action=connect。"
}

func desktopRepoPoll(ctx context.Context, bot, service, gitlabHost, deviceCode string, interval int, wait *repoAuthWait) {
	token, err := desktopRepoPollDevice(ctx, service, deviceCode, gitlabHost, interval)
	wait.mu.Lock()
	defer wait.mu.Unlock()
	if wait.status != "pending" {
		return
	}
	if err != nil {
		wait.status = "fail"
		wait.note = repoAuthFailText(err)
		return
	}
	name := gitforge.TokenEnv(service)
	desktopSaveBotSecret(bot, name, token)
	if service == gitforge.GitLab {
		desktopSetGitLabBearer(bot)
	}
	desktopMarkRepoPick(bot, service)
	wait.status = "ok"
	wait.note = service + " 授权已完成。调用 action=repos 列出组织和仓库，让这个人选定后 action=grant。"
}

func repoAuthFailText(err error) string {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	switch {
	case strings.Contains(msg, "denied"):
		return "这个人拒绝了授权。要再试就重新 action=connect。"
	case strings.Contains(msg, "expired"), strings.Contains(msg, "timed out"):
		return "授权页面过期了。重新 action=connect。"
	default:
		return "授权没有完成。重新 action=connect。"
	}
}

func desktopRepoRepos(ctx context.Context, scope agentruntime.Scope, args map[string]interface{}) (string, error) {
	service := repoService(stringArg(args, "host"))
	bot := desktopRepoBot(scope)
	token := desktopRepoTokenOf(bot, service)
	if token == "" {
		if note := desktopRepoAuthNote(bot, service); note != "" {
			return note, nil
		}
		return "", desktopRepoMissingToken(service)
	}
	repo, err := gitforge.Parse("maclaw/visible", service, desktopRepoHost(scope))
	if err != nil {
		return "", err
	}
	text, err := desktopRepoClient(scope, token, service).Visible(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("%s", desktopScrubSecrets(err.Error(), map[string]string{gitforge.TokenEnv(service): token}))
	}
	text = desktopScrubSecrets(text, map[string]string{gitforge.TokenEnv(service): token})
	return text + "选定后 action=grant，repos 填上面的名字，多个用逗号，或 * 表示这些全部。", nil
}

func desktopRepoGrant(scope agentruntime.Scope, args map[string]interface{}) (string, error) {
	service := repoService(stringArg(args, "host"))
	bot := desktopRepoBot(scope)
	if desktopRepoTokenOf(bot, service) == "" {
		return "", desktopRepoMissingToken(service)
	}
	raw := strings.TrimSpace(stringArg(args, "repos"))
	if raw == "" {
		return "", fmt.Errorf("repos is empty")
	}
	host := ""
	if service == gitforge.GitLab {
		host = desktopRepoHost(scope)
	}
	if raw == "*" {
		desktopSetRepoGrant(bot, service, true, nil)
		return "granted " + service + " all visible repositories", nil
	}
	paths := map[string]bool{}
	var names []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "*" {
			desktopSetRepoGrant(bot, service, true, nil)
			return "granted " + service + " all visible repositories", nil
		}
		repo, err := gitforge.Parse(part, service, host)
		if err != nil {
			return "", err
		}
		key := strings.ToLower(repo.Path)
		if paths[key] {
			continue
		}
		paths[key] = true
		names = append(names, repo.Path)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("repos is empty")
	}
	desktopSetRepoGrant(bot, service, false, paths)
	return "granted " + service + " " + strings.Join(names, ", "), nil
}

func desktopRepoAdmit(scope agentruntime.Scope, repo gitforge.Repo) error {
	if strings.TrimSpace(repo.Path) == "" {
		return nil
	}
	bot := desktopRepoBot(scope)
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return nil
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if !turn.repoPick[repo.Service] {
		if turn.repoAll[repo.Service] || len(turn.repoPaths[repo.Service]) > 0 {
			if turn.repoAll[repo.Service] || turn.repoPaths[repo.Service][strings.ToLower(repo.Path)] {
				return nil
			}
			return fmt.Errorf("仓库 %s 不在这个 bot 的授权范围里", repo.Path)
		}
		return nil
	}
	if turn.repoAll[repo.Service] || turn.repoPaths[repo.Service][strings.ToLower(repo.Path)] {
		return nil
	}
	if len(turn.repoPaths[repo.Service]) == 0 && !turn.repoAll[repo.Service] {
		return fmt.Errorf("授权还没有选定仓库。先把 action=repos 的清单给这个人，他选定后用 action=grant，repos 填 owner/name，多个用逗号，或 * 表示这次授权能看到的全部")
	}
	return fmt.Errorf("仓库 %s 不在这个 bot 的授权范围里", repo.Path)
}

func desktopRepoAuthNote(bot, service string) string {
	value, ok := repoAuthWaits.Load(bot + "\x00" + service)
	if !ok {
		return ""
	}
	wait := value.(*repoAuthWait)
	wait.mu.Lock()
	defer wait.mu.Unlock()
	switch wait.status {
	case "pending":
		return "还在等这个人打开链接并点授权。\n" + desktopRepoConnectText(service, wait.scopes, wait.link)
	case "ok":
		return wait.note
	case "fail":
		return wait.note
	default:
		return ""
	}
}

func desktopRepoClient(scope agentruntime.Scope, token, service string) *gitforge.Client {
	return &gitforge.Client{
		HTTP:   desktopRepoHTTP,
		Token:  token,
		Bearer: service == gitforge.GitLab && desktopGitLabUsesBearer(desktopRepoBot(scope)),
	}
}

func repoService(host string) string {
	if strings.EqualFold(strings.TrimSpace(host), gitforge.GitLab) {
		return gitforge.GitLab
	}
	return gitforge.GitHub
}

func desktopRepoTokenOf(bot, service string) string {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return ""
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	return strings.TrimSpace(turn.secrets[gitforge.TokenEnv(service)])
}

func desktopSaveBotSecret(bot, name, value string) {
	turn := desktopBotTurnOf(bot)
	if turn == nil || !desktopbot.ValidSecretName(name) || strings.TrimSpace(value) == "" {
		return
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.secrets == nil {
		turn.secrets = map[string]string{}
	}
	turn.secrets[name] = value
	if repoAuthSecret(name) {
		desktopWriteRepoAuth(bot, turn)
	}
}

func desktopSetGitLabBearer(bot string) {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return
	}
	turn.mu.Lock()
	turn.gitlabBearer = true
	desktopWriteRepoAuth(bot, turn)
	turn.mu.Unlock()
}

func desktopGitLabUsesBearer(bot string) bool {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return false
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	return turn.gitlabBearer
}

func desktopMarkRepoPick(bot, service string) {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.repoPick == nil {
		turn.repoPick = map[string]bool{}
	}
	turn.repoPick[service] = true
	delete(turn.repoAll, service)
	if turn.repoPaths != nil {
		delete(turn.repoPaths, service)
	}
	desktopWriteRepoAuth(bot, turn)
}

func desktopRepoMustPick(bot, service string) bool {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return false
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	return turn.repoPick[service] && !turn.repoAll[service] && len(turn.repoPaths[service]) == 0
}

func desktopSetRepoGrant(bot, service string, all bool, paths map[string]bool) {
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.repoPick == nil {
		turn.repoPick = map[string]bool{}
	}
	if turn.repoAll == nil {
		turn.repoAll = map[string]bool{}
	}
	if turn.repoPaths == nil {
		turn.repoPaths = map[string]map[string]bool{}
	}
	turn.repoPick[service] = false
	turn.repoAll[service] = all
	if all {
		delete(turn.repoPaths, service)
	} else {
		turn.repoPaths[service] = paths
	}
	desktopWriteRepoAuth(bot, turn)
}

func desktopRepoStartDeviceLive(ctx context.Context, service, scopes, gitlabHost string) (string, string, int, error) {
	if service == gitforge.GitLab {
		id := strings.TrimSpace(os.Getenv("MACLAW_GITLAB_CLIENT_ID"))
		if id == "" {
			return "", "", 0, fmt.Errorf("gitlab oauth client is not configured")
		}
		return gitforge.StartGitLabDevice(ctx, desktopRepoHTTP, gitlabHost, id, scopes)
	}
	resp, err := oauth.RequestGitHubDeviceCodeWith(ctx, oauth.GitHubAccountClientID, scopes)
	if err != nil {
		return "", "", 0, fmt.Errorf("github authorization did not start")
	}
	return resp.UserCode, resp.DeviceCode, resp.Interval, nil
}

func desktopRepoPollDeviceLive(ctx context.Context, service, deviceCode, gitlabHost string, interval int) (string, error) {
	if service == gitforge.GitLab {
		return gitforge.PollGitLabDevice(ctx, desktopRepoHTTP, gitlabHost, os.Getenv("MACLAW_GITLAB_CLIENT_ID"), os.Getenv("MACLAW_GITLAB_CLIENT_SECRET"), deviceCode, interval)
	}
	token, _, err := oauth.PollGitHubDeviceCodeWith(ctx, oauth.GitHubAccountClientID, deviceCode, interval)
	return token, err
}
