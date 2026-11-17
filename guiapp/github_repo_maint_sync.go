package guiapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// errRepoMaintReplayNotReady is returned until intake owns replay.
// Enable still succeeds: the user's choice is stored on the rule.
var errRepoMaintReplayNotReady = errors.New("回放尚未就绪")

// newRepoMaintHubHTTPClient is replaced in tests. It does not follow
// redirects, so a 3xx cannot move the machine token to another host.
var newRepoMaintHubHTTPClient = defaultRepoMaintHubHTTPClient

func defaultRepoMaintHubHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// cloneGitHubRepoForMaint clones into a missing or empty ghost directory.
// Tests replace it so enable can be proven without a live GitHub clone.
var cloneGitHubRepoForMaint = func(a *App, repoURL, dest string) error {
	_, err := a.GitHubCloneRepository(repoURL, dest)
	return err
}

type repoMaintHubIdentity struct {
	BaseURL      *url.URL
	MachineID    string
	MachineToken string
}

func (a *App) repoMaintHubIdentity() (repoMaintHubIdentity, error) {
	if a == nil {
		return repoMaintHubIdentity{}, fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return repoMaintHubIdentity{}, err
	}
	return repoMaintHubIdentityFrom(cfg)
}

func repoMaintHubIdentityFrom(cfg corelib.AppConfig) (repoMaintHubIdentity, error) {
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	if machineID == "" || token == "" || strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return repoMaintHubIdentity{}, fmt.Errorf("未连接 Hub")
	}
	base, err := parsePublicHubURL(cfg.RemoteHubURL)
	if err != nil {
		return repoMaintHubIdentity{}, err
	}
	return repoMaintHubIdentity{BaseURL: base, MachineID: machineID, MachineToken: token}, nil
}

// parsePublicHubURL accepts only https endpoints whose host is not
// loopback, link-local, RFC1918, or IPv6 ULA. Names are resolved first.
func parsePublicHubURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("Hub 地址必须是公网 https")
	}
	if err := rejectPrivateHubHost(parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func rejectPrivateHubHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return fmt.Errorf("Hub 地址必须是公网 https")
	}
	if ip := net.ParseIP(host); ip != nil {
		if hubIPBlocked(ip) {
			return fmt.Errorf("Hub 地址必须是公网 https")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("Hub 地址必须是公网 https")
	}
	for _, ip := range ips {
		if hubIPBlocked(ip) {
			return fmt.Errorf("Hub 地址必须是公网 https")
		}
	}
	return nil
}

func hubIPBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func repoMaintJoin(base *url.URL, parts ...string) (string, error) {
	if base == nil {
		return "", fmt.Errorf("Hub 地址必须是公网 https")
	}
	u := *base
	u.RawQuery = ""
	u.Fragment = ""
	joined := u.JoinPath(parts...)
	if joined == nil || joined.Scheme != "https" || strings.Contains(joined.EscapedPath(), "//") {
		return "", fmt.Errorf("Hub 地址必须是公网 https")
	}
	return joined.String(), nil
}

func (a *App) repoMaintWebhookURL(base *url.URL, hookToken string) (string, error) {
	return repoMaintJoin(base, "api", "github", "repo-maint", "hooks", hookToken)
}

type repoMaintHubRuleBody struct {
	MachineID    string   `json:"machine_id"`
	Owner        string   `json:"owner"`
	Repo         string   `json:"repo"`
	HookToken    string   `json:"hook_token"`
	HookSecret   string   `json:"hook_secret"`
	GitHubHookID int64    `json:"github_hook_id"`
	Enabled      bool     `json:"enabled"`
	PushBranch   string   `json:"push_branch"`
	EventKinds   []string `json:"event_kinds"`
}

func (a *App) putRepoMaintHubRule(id repoMaintHubIdentity, rule repoMaintRuleRow, secret string, enabled bool, hookID int64) error {
	rawURL, err := repoMaintJoin(id.BaseURL, "api", "github", "repo-maint", "rules", rule.ID)
	if err != nil {
		return err
	}
	kinds := enabledRepoMaintKinds(rule.Kinds)
	body := repoMaintHubRuleBody{
		MachineID:    id.MachineID,
		Owner:        rule.Owner,
		Repo:         rule.Repo,
		HookToken:    rule.HookToken,
		HookSecret:   secret,
		GitHubHookID: hookID,
		Enabled:      enabled,
		PushBranch:   rule.PushBranch,
		EventKinds:   kinds,
	}
	status, _, err := a.doRepoMaintHubJSON(id, http.MethodPut, rawURL, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("hub rule: HTTP %d", status)
	}
	log.Printf("[github-repo-maint] hub upsert rule_id=%s enabled=%t http=%d", rule.ID, enabled, status)
	return nil
}

func (a *App) deleteRepoMaintHubRule(id repoMaintHubIdentity, ruleID string) error {
	rawURL, err := repoMaintJoin(id.BaseURL, "api", "github", "repo-maint", "rules", ruleID)
	if err != nil {
		return err
	}
	status, _, err := a.doRepoMaintHubJSON(id, http.MethodDelete, rawURL, nil)
	if status == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("hub rule: HTTP %d", status)
	}
	log.Printf("[github-repo-maint] hub delete rule_id=%s http=%d", ruleID, status)
	return nil
}

func (a *App) doRepoMaintHubJSON(id repoMaintHubIdentity, method, rawURL string, payload any) (int, []byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || !sameHubHost(parsed, id.BaseURL) {
		return 0, nil, fmt.Errorf("hub api: only the configured https hub is allowed")
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, parsed.String(), body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+id.MachineToken)
	req.Header.Set("X-Machine-ID", id.MachineID)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := newRepoMaintHubHTTPClient()
	if client == nil {
		client = defaultRepoMaintHubHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("hub api: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return resp.StatusCode, nil, fmt.Errorf("hub api: unexpected redirect (HTTP %d)", resp.StatusCode)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, respBody, fmt.Errorf("hub api: HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, respBody, nil
}

func sameHubHost(got, base *url.URL) bool {
	if got == nil || base == nil {
		return false
	}
	return strings.EqualFold(got.Hostname(), base.Hostname()) && got.Port() == base.Port()
}

// SetGitHubRepoMaintEnabled turns a rule on or off.
// replay is "new_only", "backfill", or empty when the rule was enabled before.
// Intake must be ready before the first enable, or this returns without
// calling Hub or GitHub.
func (a *App) SetGitHubRepoMaintEnabled(id string, enabled bool, replay string) error {
	id = strings.TrimSpace(id)
	rule, err := a.loadRepoMaintRule(id)
	if err != nil {
		return err
	}
	if rule.DeletePending {
		return fmt.Errorf("规则正在等待删除")
	}
	if !enabled {
		return a.disableRepoMaintRule(rule)
	}
	if !githubRepoMaintIntakeReady {
		return fmt.Errorf("接收尚未就绪")
	}
	if a.loadGitHubAccessToken() == "" {
		return fmt.Errorf("GitHub 未连接")
	}
	hub, err := a.repoMaintHubIdentity()
	if err != nil {
		return err
	}
	mode, err := normalizeReplayChoice(rule.ReplayMode, replay)
	if err != nil {
		return err
	}
	if len(enabledRepoMaintKinds(rule.Kinds)) == 0 {
		return fmt.Errorf("至少打开一个事件并填写命令")
	}
	secret, err := a.repoMaintSecret(rule.ID)
	if err != nil {
		return err
	}
	if err := a.ensureRepoMaintCheckout(&rule); err != nil {
		if !rule.Enabled {
			_ = a.touchRepoMaintError(rule.ID, err.Error())
		}
		return err
	}
	webhookURL, err := a.repoMaintWebhookURL(hub.BaseURL, rule.HookToken)
	if err != nil {
		return err
	}
	if err := a.putRepoMaintHubRule(hub, rule, secret, true, rule.HookID); err != nil {
		_ = a.touchRepoMaintError(rule.ID, err.Error())
		return err
	}
	spec := gitHubHookSpec{URL: webhookURL, Secret: secret, Events: githubHookEventsForKinds(rule.Kinds), Active: true}
	hookID := rule.HookID
	if hookID <= 0 {
		created, createErr := a.createGitHubRepoHook(rule.Owner, rule.Repo, spec)
		if createErr != nil {
			if rollErr := a.putRepoMaintHubRule(hub, rule, secret, false, 0); rollErr != nil {
				log.Printf("[github-repo-maint] hub rollback failed rule_id=%s err=%v", rule.ID, rollErr)
			}
			msg := "GitHub hook 创建失败，规则保持关闭"
			_ = a.touchRepoMaintError(rule.ID, msg)
			log.Printf("[github-repo-maint] hook create failed rule_id=%s", rule.ID)
			return fmt.Errorf("%s", msg)
		}
		hookID = created
	} else if err := a.updateGitHubRepoHook(rule.Owner, rule.Repo, hookID, spec); err != nil {
		if rollErr := a.putRepoMaintHubRule(hub, rule, secret, false, hookID); rollErr != nil {
			log.Printf("[github-repo-maint] hub rollback failed rule_id=%s err=%v", rule.ID, rollErr)
		}
		msg := "GitHub hook 更新失败，规则保持关闭"
		_ = a.touchRepoMaintError(rule.ID, msg)
		return fmt.Errorf("%s", msg)
	}
	if err := a.putRepoMaintHubRule(hub, rule, secret, true, hookID); err != nil {
		log.Printf("[github-repo-maint] hub hook id update failed rule_id=%s err=%v", rule.ID, err)
	}
	if err := a.markRepoMaintEnabled(rule.ID, hookID, mode); err != nil {
		return err
	}
	log.Printf("[github-repo-maint] rule enabled rule_id=%s hook_id=%d replay=%s", rule.ID, hookID, mode)
	if mode == "backfill" {
		if err := a.replayGitHubRepoMaint(rule.ID); err != nil && !errors.Is(err, errRepoMaintReplayNotReady) {
			// The hook is already live. A replay failure must not turn the rule off.
			_ = a.noteRepoMaintRuleError(rule.ID, err.Error())
		}
	}
	return nil
}

func (a *App) ensureRepoMaintCheckout(rule *repoMaintRuleRow) error {
	class, dir, err := classifyGhostDir(rule.GhostDir, rule.Owner, rule.Repo)
	if err != nil {
		return err
	}
	switch class {
	case ghostDirReady:
		return a.noteRepoMaintGhost(rule, dir)
	case ghostDirMissing, ghostDirEmpty:
		repoURL := "https://github.com/" + rule.Owner + "/" + rule.Repo
		if err := cloneGitHubRepoForMaint(a, repoURL, dir); err != nil {
			return err
		}
		class, dir, err = classifyGhostDir(dir, rule.Owner, rule.Repo)
		if err != nil {
			return err
		}
		if class != ghostDirReady {
			return fmt.Errorf("克隆完成后目录不是该仓库的检出")
		}
		return a.noteRepoMaintGhost(rule, dir)
	default:
		return fmt.Errorf("目录被其他内容占用，未覆盖")
	}
}

func (a *App) noteRepoMaintGhost(rule *repoMaintRuleRow, dir string) error {
	if dir == rule.GhostDir {
		return nil
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE rules SET ghost_dir=?, updated_at=? WHERE id=?`, dir, time.Now().UTC().Format(time.RFC3339Nano), rule.ID); err != nil {
		return err
	}
	rule.GhostDir = dir
	return nil
}

func normalizeReplayChoice(stored, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	switch requested {
	case "", "new_only", "backfill":
	default:
		return "", fmt.Errorf("请选择只处理新事件或回放未处理的")
	}
	if stored == "new_only" || stored == "backfill" {
		if requested == "" {
			return stored, nil
		}
		return requested, nil
	}
	if requested == "" {
		return "", fmt.Errorf("请选择只处理新事件或回放未处理的")
	}
	return requested, nil
}

func (a *App) disableRepoMaintRule(rule repoMaintRuleRow) error {
	if !rule.Enabled && rule.HookID <= 0 {
		return nil
	}
	hub, err := a.repoMaintHubIdentity()
	if err != nil {
		return err
	}
	secret, err := a.repoMaintSecret(rule.ID)
	if err != nil {
		return err
	}
	if err := a.putRepoMaintHubRule(hub, rule, secret, false, rule.HookID); err != nil {
		return err
	}
	if rule.HookID > 0 {
		spec := gitHubHookSpec{
			URL:    "",
			Secret: secret,
			Events: githubHookEventsForKinds(rule.Kinds),
			Active: false,
		}
		webhook, werr := a.repoMaintWebhookURL(hub.BaseURL, rule.HookToken)
		if werr != nil {
			return werr
		}
		spec.URL = webhook
		if err := a.updateGitHubRepoHook(rule.Owner, rule.Repo, rule.HookID, spec); err != nil {
			_ = a.markRepoMaintDisabled(rule.ID, err.Error())
			log.Printf("[github-repo-maint] hook deactivate failed rule_id=%s", rule.ID)
			return err
		}
	}
	if err := a.markRepoMaintDisabled(rule.ID, ""); err != nil {
		return err
	}
	log.Printf("[github-repo-maint] rule disabled rule_id=%s", rule.ID)
	return nil
}

func (a *App) markRepoMaintEnabled(id string, hookID int64, mode string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET enabled=1, hook_id=?, replay_mode=?, last_error='', auth_blocked=0, updated_at=? WHERE id=?`,
		hookID, mode, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (a *App) markRepoMaintDisabled(id, lastError string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE rules SET enabled=0, last_error=?, updated_at=? WHERE id=?`, lastError, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE jobs SET status='skipped_disabled', updated_at=? WHERE rule_id=? AND status='queued'`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) touchRepoMaintError(id, msg string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET enabled=0, last_error=?, updated_at=? WHERE id=?`, msg, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

// DeleteGitHubRepoMaintRule removes the Hub rule and the GitHub hook.
// A failed remote delete leaves the row marked delete_pending.
func (a *App) DeleteGitHubRepoMaintRule(id string) error {
	id = strings.TrimSpace(id)
	rule, err := a.loadRepoMaintRule(id)
	if err != nil {
		return err
	}
	hub, hubErr := a.repoMaintHubIdentity()
	needsRemote := rule.Enabled || rule.HookID > 0 || rule.ReplayMode == "new_only" || rule.ReplayMode == "backfill" || rule.LastError != "" || rule.DeletePending
	if needsRemote && hubErr != nil {
		_ = a.markRepoMaintDeletePending(id, hubErr.Error())
		return fmt.Errorf("删除未完成，将在下次重试")
	}
	if hubErr == nil && (needsRemote || rule.HookToken != "") {
		if err := a.deleteRepoMaintHubRule(hub, rule.ID); err != nil {
			_ = a.markRepoMaintDeletePending(id, err.Error())
			log.Printf("[github-repo-maint] delete pending rule_id=%s", id)
			return fmt.Errorf("删除未完成，将在下次重试")
		}
	}
	if rule.HookID > 0 {
		if err := a.deleteGitHubRepoHook(rule.Owner, rule.Repo, rule.HookID); err != nil {
			_ = a.markRepoMaintDeletePending(id, err.Error())
			log.Printf("[github-repo-maint] delete pending rule_id=%s", id)
			return fmt.Errorf("删除未完成，将在下次重试")
		}
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM rules WHERE id=?`, id); err != nil {
		return err
	}
	if a.credentialStore != nil {
		_ = a.credentialStore.Delete(repoMaintSecretProvider(id))
	}
	log.Printf("[github-repo-maint] rule deleted rule_id=%s", id)
	return nil
}

func (a *App) markRepoMaintDeletePending(id, msg string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET delete_pending=1, enabled=0, last_error=?, updated_at=? WHERE id=?`, msg, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

// ReplayGitHubRepoMaint replays the current open issues, unmerged PRs, and
// failing checks on those PRs. Intake fills this in; until then it errors.
func (a *App) ReplayGitHubRepoMaint(id string) error {
	if !githubRepoMaintIntakeReady {
		return fmt.Errorf("接收尚未就绪")
	}
	return a.replayGitHubRepoMaint(id)
}
