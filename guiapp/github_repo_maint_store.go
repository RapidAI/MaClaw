package guiapp

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	_ "modernc.org/sqlite"
)

// githubRepoMaintIntakeReady stays false until the intake service is
// initialized. Enabling a rule before that must not register a webhook.
var githubRepoMaintIntakeReady bool

// errRepoMaintRuleMissing is a deleted rule, not a transient database error.
var errRepoMaintRuleMissing = errors.New("规则不存在")

const (
	githubRepoMaintMaxRules = 20
	githubRepoMaintDBName   = "github-repo-maint.db"
)

// GitHub repo-maint event kinds. An empty command means that kind is off.
var githubRepoMaintKinds = []string{
	"issues.opened",
	"issues.closed",
	"issues.labeled",
	"issue_comment.created",
	"pull_request.opened",
	"pull_request.merged",
	"pull_request.review_requested",
	"pull_request_review.submitted",
	"push",
	"check_run.failure",
}

// GitHubRepoMaintRule is the Wails view of one maintenance rule.
// The HMAC secret is not a field: it lives in the credential store.
type GitHubRepoMaintRule struct {
	ID           string            `json:"id"`
	RepoURL      string            `json:"repo_url"`
	Owner        string            `json:"owner"`
	Repo         string            `json:"repo"`
	GhostDir     string            `json:"ghost_dir"`
	Enabled      bool              `json:"enabled"`
	PushBranch   string            `json:"push_branch"`
	Commands     map[string]string `json:"commands"`
	EnabledKinds map[string]bool   `json:"enabled_kinds"`
	HookReady    bool              `json:"hook_ready"`
	AuthBlocked  bool              `json:"auth_blocked"`
	LastError    string            `json:"last_error"`
	QueueDepth   int               `json:"queue_depth"`
	IntakeReady  bool              `json:"intake_ready"`
}

type repoMaintRuleRow struct {
	ID            string
	Owner         string
	Repo          string
	RepoURL       string
	GhostDir      string
	Enabled       bool
	PushBranch    string
	Commands      map[string]string
	Kinds         map[string]bool
	HookID        int64
	HookToken     string
	AuthBlocked   bool
	DeletePending bool
	ReplayMode    string
	ReplayNextURL string
	LastError     string
	UpdatedAt     string
}

var (
	repoMaintDBMu sync.Mutex
	repoMaintDBs  = map[string]*sql.DB{}
)

func repoMaintSecretProvider(ruleID string) string {
	return "github-repo-maint:" + strings.TrimSpace(ruleID)
}

func (a *App) repoMaintDB() (*sql.DB, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	a.ensureRepoMaintWorker()
	path := filepath.Join(a.GetDataDir(), githubRepoMaintDBName)
	repoMaintDBMu.Lock()
	defer repoMaintDBMu.Unlock()
	if db := repoMaintDBs[path]; db != nil {
		return db, nil
	}
	db, err := openRepoMaintDB(path)
	if err != nil {
		return nil, err
	}
	repoMaintDBs[path] = db
	return db, nil
}

func closeRepoMaintDB(path string) {
	repoMaintDBMu.Lock()
	db := repoMaintDBs[path]
	delete(repoMaintDBs, path)
	repoMaintDBMu.Unlock()
	if db != nil {
		_ = db.Close()
	}
}

func openRepoMaintDB(path string) (*sql.DB, error) {
	if err := mkdirAllParent(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	for _, stmt := range repoMaintSchemaStmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := ensureRepoMaintEventJSON(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ensureRepoMaintEventJSON adds the event payload column to a database
// created before jobs stored the event. CREATE TABLE IF NOT EXISTS does not
// add columns, and a restarted process needs the payload to finish the job.
func ensureRepoMaintEventJSON(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "event_json" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE jobs ADD COLUMN event_json TEXT NOT NULL DEFAULT ''`)
	return err
}

func (a *App) loadRepoMaintEventTask(ruleID, kind string) (string, bool, error) {
	db, err := a.repoMaintDB()
	if err != nil {
		return "", false, err
	}
	var path string
	err = db.QueryRow(`SELECT task_project_path FROM event_tasks WHERE rule_id=? AND event_kind=?`, ruleID, kind).Scan(&path)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	return path, true, nil
}

func (a *App) saveRepoMaintEventTask(ruleID, kind, projectPath string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO event_tasks (rule_id, event_kind, task_project_path, updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(rule_id, event_kind) DO UPDATE SET
			task_project_path=excluded.task_project_path,
			updated_at=excluded.updated_at`,
		ruleID, kind, projectPath, now)
	return err
}

func (a *App) clearRepoMaintEventTask(ruleID, kind string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM event_tasks WHERE rule_id=? AND event_kind=?`, ruleID, kind)
	return err
}

func mkdirAllParent(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

var repoMaintSchemaStmts = []string{
	`CREATE TABLE IF NOT EXISTS rules (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    ghost_dir TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    push_branch TEXT NOT NULL DEFAULT '',
    commands_json TEXT NOT NULL,
    kinds_json TEXT NOT NULL,
    hook_id INTEGER NOT NULL DEFAULT 0,
    hook_token TEXT NOT NULL DEFAULT '',
    auth_blocked INTEGER NOT NULL DEFAULT 0,
    delete_pending INTEGER NOT NULL DEFAULT 0,
    replay_mode TEXT NOT NULL DEFAULT 'none',
    replay_next_url TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS rules_repo ON rules(owner COLLATE NOCASE, repo COLLATE NOCASE)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS rules_ghost ON rules(ghost_dir)`,
	`CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL DEFAULT '',
    event_kind TEXT NOT NULL,
    object_id TEXT NOT NULL,
    ghost_dir TEXT NOT NULL,
    status TEXT NOT NULL,
    task_project_path TEXT NOT NULL DEFAULT '',
    session_user_id TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    event_json TEXT NOT NULL DEFAULT ''
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS jobs_delivery ON jobs(delivery_id) WHERE delivery_id <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS jobs_logical_open ON jobs(rule_id, event_kind, object_id) WHERE status IN ('queued', 'running', 'awaiting_user', 'comment_pending', 'succeeded', 'blocked_workspace')`,
	`CREATE INDEX IF NOT EXISTS jobs_dir ON jobs(ghost_dir, status, created_at)`,
	// One durable coding task per repository rule and event kind. The job row
	// records which task ran that delivery. This row is the identity that the
	// next delivery of the same kind continues. A restart clears the job's
	// task path; it must not clear this mapping.
	`CREATE TABLE IF NOT EXISTS event_tasks (
    rule_id TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    task_project_path TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (rule_id, event_kind)
)`,
}

func (a *App) ListGitHubRepoMaintRules() ([]GitHubRepoMaintRule, error) {
	db, err := a.repoMaintDB()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, owner, repo, repo_url, ghost_dir, enabled, push_branch, commands_json, kinds_json, hook_id, hook_token, auth_blocked, delete_pending, replay_mode, replay_next_url, last_error, updated_at FROM rules ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repoMaintRuleRow
	for rows.Next() {
		row, err := scanRepoMaintRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	depth, err := a.repoMaintQueueDepth(db)
	if err != nil {
		return nil, err
	}
	view := make([]GitHubRepoMaintRule, 0, len(out))
	for _, row := range out {
		view = append(view, row.public(depth[row.ID]))
	}
	if view == nil {
		view = []GitHubRepoMaintRule{}
	}
	return view, nil
}

func (row repoMaintRuleRow) public(depth int) GitHubRepoMaintRule {
	commands := row.Commands
	if commands == nil {
		commands = map[string]string{}
	}
	kinds := row.Kinds
	if kinds == nil {
		kinds = map[string]bool{}
	}
	return GitHubRepoMaintRule{
		ID:           row.ID,
		RepoURL:      row.RepoURL,
		Owner:        row.Owner,
		Repo:         row.Repo,
		GhostDir:     row.GhostDir,
		Enabled:      row.Enabled,
		PushBranch:   row.PushBranch,
		Commands:     commands,
		EnabledKinds: kinds,
		HookReady:    row.HookID > 0 && !row.DeletePending,
		AuthBlocked:  row.AuthBlocked,
		LastError:    row.LastError,
		QueueDepth:   depth,
		IntakeReady:  githubRepoMaintIntakeReady,
	}
}

type repoMaintScanner interface {
	Scan(dest ...any) error
}

func scanRepoMaintRule(s repoMaintScanner) (repoMaintRuleRow, error) {
	var row repoMaintRuleRow
	var enabled, authBlocked, deletePending int
	var commandsRaw, kindsRaw string
	if err := s.Scan(&row.ID, &row.Owner, &row.Repo, &row.RepoURL, &row.GhostDir, &enabled, &row.PushBranch, &commandsRaw, &kindsRaw, &row.HookID, &row.HookToken, &authBlocked, &deletePending, &row.ReplayMode, &row.ReplayNextURL, &row.LastError, &row.UpdatedAt); err != nil {
		return row, err
	}
	row.Enabled = enabled != 0
	row.AuthBlocked = authBlocked != 0
	row.DeletePending = deletePending != 0
	if err := json.Unmarshal([]byte(commandsRaw), &row.Commands); err != nil {
		return row, err
	}
	if err := json.Unmarshal([]byte(kindsRaw), &row.Kinds); err != nil {
		return row, err
	}
	if row.Commands == nil {
		row.Commands = map[string]string{}
	}
	if row.Kinds == nil {
		row.Kinds = map[string]bool{}
	}
	return row, nil
}

func (a *App) repoMaintQueueDepth(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT rule_id, COUNT(*) FROM jobs WHERE status IN ('queued','running','awaiting_user') GROUP BY rule_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (a *App) loadRepoMaintRule(id string) (repoMaintRuleRow, error) {
	db, err := a.repoMaintDB()
	if err != nil {
		return repoMaintRuleRow{}, err
	}
	row, err := scanRepoMaintRule(db.QueryRow(`SELECT id, owner, repo, repo_url, ghost_dir, enabled, push_branch, commands_json, kinds_json, hook_id, hook_token, auth_blocked, delete_pending, replay_mode, replay_next_url, last_error, updated_at FROM rules WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return repoMaintRuleRow{}, errRepoMaintRuleMissing
	}
	return row, err
}

// SaveGitHubRepoMaintRule validates and stores a draft or an edit.
// It does not change the master switch, and a rule that is off is not
// registered. When an enabled rule's event set or push branch changes, the
// local row is updated and then Hub and the GitHub hook are updated
// together. The hook PATCH repeats the same secret. If that update fails,
// the local row is restored to the previous set.
func (a *App) SaveGitHubRepoMaintRule(in GitHubRepoMaintRule) (GitHubRepoMaintRule, error) {
	owner, repo, repoURL, ghost, commands, kinds, branch, err := validateRepoMaintDraft(in)
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.Begin()
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	defer func() { _ = tx.Rollback() }()

	id := strings.TrimSpace(in.ID)
	var existing repoMaintRuleRow
	var syncSubscription bool
	var prior repoMaintRuleRow
	if id != "" {
		existing, err = scanRepoMaintRule(tx.QueryRow(`SELECT id, owner, repo, repo_url, ghost_dir, enabled, push_branch, commands_json, kinds_json, hook_id, hook_token, auth_blocked, delete_pending, replay_mode, replay_next_url, last_error, updated_at FROM rules WHERE id=?`, id))
		if err == sql.ErrNoRows {
			existing = repoMaintRuleRow{}
			err = nil
		}
		if err != nil {
			return GitHubRepoMaintRule{}, err
		}
	}
	if existing.Enabled && (existing.Owner != "" && (!strings.EqualFold(existing.Owner, owner) || !strings.EqualFold(existing.Repo, repo) || !sameGhostDir(existing.GhostDir, ghost))) {
		return GitHubRepoMaintRule{}, fmt.Errorf("请先关闭总开关，再修改仓库或目录")
	}
	if err := rejectDuplicateRepoMaint(tx, id, owner, repo, ghost); err != nil {
		return GitHubRepoMaintRule{}, err
	}
	// The remote update happens after this transaction commits. Refuse the
	// cases that cannot be patched before the local row changes, so a
	// failure does not leave a new event set that GitHub will never send.
	syncSubscription = existing.ID != "" && existing.Enabled && repoMaintSubscriptionChanged(existing, kinds, branch)
	if syncSubscription {
		if len(enabledRepoMaintKinds(kinds)) == 0 {
			return GitHubRepoMaintRule{}, fmt.Errorf("至少打开一个事件并填写命令")
		}
		if existing.HookID <= 0 {
			return GitHubRepoMaintRule{}, fmt.Errorf("GitHub hook 尚未挂上，无法更新事件")
		}
		if a.loadGitHubAccessToken() == "" {
			return GitHubRepoMaintRule{}, fmt.Errorf("GitHub 未连接")
		}
		if !oauth.GitHubScopesHaveRepo(a.githubConnectionStatus().Scopes) {
			return GitHubRepoMaintRule{}, fmt.Errorf("请重新连接 GitHub，令牌需要包含 repo")
		}
		if _, err := a.repoMaintHubIdentity(); err != nil {
			return GitHubRepoMaintRule{}, err
		}
		prior = existing
	}
	if existing.ID == "" {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM rules`).Scan(&n); err != nil {
			return GitHubRepoMaintRule{}, err
		}
		if n >= githubRepoMaintMaxRules {
			return GitHubRepoMaintRule{}, fmt.Errorf("最多保存 %d 条规则", githubRepoMaintMaxRules)
		}
		if id == "" {
			id, err = newRepoMaintID()
			if err != nil {
				return GitHubRepoMaintRule{}, err
			}
		}
		token, err := newRepoMaintToken()
		if err != nil {
			return GitHubRepoMaintRule{}, err
		}
		secret, err := newRepoMaintToken()
		if err != nil {
			return GitHubRepoMaintRule{}, err
		}
		if err := a.storeRepoMaintSecret(id, secret); err != nil {
			return GitHubRepoMaintRule{}, err
		}
		commandsJSON, kindsJSON, err := marshalRepoMaintMaps(commands, kinds)
		if err != nil {
			return GitHubRepoMaintRule{}, err
		}
		if _, err := tx.Exec(`INSERT INTO rules (
			id, owner, repo, repo_url, ghost_dir, enabled, push_branch, commands_json, kinds_json,
			hook_id, hook_token, auth_blocked, delete_pending, replay_mode, replay_next_url, last_error, updated_at
		) VALUES (?,?,?,?,?,0,?,?,?,0,?,0,0,'none','','',?)`,
			id, owner, repo, repoURL, ghost, branch, commandsJSON, kindsJSON, token, now); err != nil {
			return GitHubRepoMaintRule{}, err
		}
	} else {
		commandsJSON, kindsJSON, err := marshalRepoMaintMaps(commands, kinds)
		if err != nil {
			return GitHubRepoMaintRule{}, err
		}
		if _, err := tx.Exec(`UPDATE rules SET owner=?, repo=?, repo_url=?, ghost_dir=?, push_branch=?, commands_json=?, kinds_json=?, updated_at=? WHERE id=?`,
			owner, repo, repoURL, ghost, branch, commandsJSON, kindsJSON, now, existing.ID); err != nil {
			return GitHubRepoMaintRule{}, err
		}
		id = existing.ID
	}
	if err := tx.Commit(); err != nil {
		return GitHubRepoMaintRule{}, err
	}
	if syncSubscription {
		if err := a.syncEnabledRepoMaintSubscription(prior, kinds, branch); err != nil {
			if restErr := a.restoreRepoMaintSubscription(prior); restErr != nil {
				log.Printf("[github-repo-maint] restore subscription failed rule_id=%s err=%v", prior.ID, restErr)
			}
			return GitHubRepoMaintRule{}, err
		}
	}
	log.Printf("[github-repo-maint] rule saved rule_id=%s", id)
	a.requeueRepoMaintIfReady(id, ghost)
	return a.publicRepoMaintRule(id)
}

func validateRepoMaintDraft(in GitHubRepoMaintRule) (owner, repo, repoURL, ghost string, commands map[string]string, kinds map[string]bool, branch string, err error) {
	owner, repo, err = canonicalGitHubOwnerRepo(in.RepoURL)
	if err != nil {
		return "", "", "", "", nil, nil, "", err
	}
	repoURL = "https://github.com/" + owner + "/" + repo
	class, ghost, err := classifyGhostDir(in.GhostDir, owner, repo)
	if err != nil {
		return "", "", "", "", nil, nil, "", err
	}
	if class == ghostDirForeign {
		return "", "", "", "", nil, nil, "", fmt.Errorf("目录被其他内容占用，未写入")
	}
	commands, kinds, err = normalizeRepoMaintCommands(in)
	if err != nil {
		return "", "", "", "", nil, nil, "", err
	}
	branch, err = normalizePushBranch(in.PushBranch)
	if err != nil {
		return "", "", "", "", nil, nil, "", err
	}
	return owner, repo, repoURL, ghost, commands, kinds, branch, nil
}

func normalizeRepoMaintCommands(in GitHubRepoMaintRule) (map[string]string, map[string]bool, error) {
	allowed := map[string]bool{}
	for _, kind := range githubRepoMaintKinds {
		allowed[kind] = true
	}
	for kind := range in.Commands {
		if !allowed[kind] {
			return nil, nil, fmt.Errorf("未知事件 %s", kind)
		}
	}
	for kind := range in.EnabledKinds {
		if !allowed[kind] {
			return nil, nil, fmt.Errorf("未知事件 %s", kind)
		}
	}
	commands := map[string]string{}
	kinds := map[string]bool{}
	for _, kind := range githubRepoMaintKinds {
		cmd := ""
		if in.Commands != nil {
			cmd = strings.TrimSpace(in.Commands[kind])
		}
		on := in.EnabledKinds != nil && in.EnabledKinds[kind]
		if on && cmd == "" {
			return nil, nil, fmt.Errorf("事件 %s 的命令为空，无法打开", kind)
		}
		if cmd != "" {
			commands[kind] = cmd
		}
		kinds[kind] = on && cmd != ""
	}
	return commands, kinds, nil
}

func normalizePushBranch(raw string) (string, error) {
	branch := strings.TrimSpace(raw)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if branch == "" {
		return "", nil
	}
	if strings.HasPrefix(branch, "refs/") || strings.ContainsAny(branch, " \t~^:?*[\\") || strings.Contains(branch, "..") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".lock") {
		return "", fmt.Errorf("分支名无效")
	}
	return branch, nil
}

func rejectDuplicateRepoMaint(tx *sql.Tx, self, owner, repo, ghost string) error {
	var other string
	err := tx.QueryRow(`SELECT id FROM rules WHERE owner=? COLLATE NOCASE AND repo=? COLLATE NOCASE AND id<>?`, owner, repo, self).Scan(&other)
	if err == nil {
		return fmt.Errorf("同一仓库已经有一条规则")
	}
	if err != sql.ErrNoRows {
		return err
	}
	rows, err := tx.Query(`SELECT id, ghost_dir FROM rules WHERE id<>?`, self)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dir string
		if err := rows.Scan(&id, &dir); err != nil {
			return err
		}
		if dir == ghost || sameGhostDir(dir, ghost) {
			return fmt.Errorf("目录已被另一条规则使用")
		}
	}
	return rows.Err()
}

func sameGhostDir(a, b string) bool {
	if a == b {
		return true
	}
	return strings.EqualFold(a, b)
}

func repoMaintSubscriptionChanged(old repoMaintRuleRow, kinds map[string]bool, branch string) bool {
	if old.PushBranch != branch {
		return true
	}
	for _, kind := range githubRepoMaintKinds {
		if old.Kinds[kind] != kinds[kind] {
			return true
		}
	}
	return false
}

func (a *App) restoreRepoMaintSubscription(old repoMaintRuleRow) error {
	commandsJSON, kindsJSON, err := marshalRepoMaintMaps(old.Commands, old.Kinds)
	if err != nil {
		return err
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET push_branch=?, commands_json=?, kinds_json=?, updated_at=? WHERE id=?`,
		old.PushBranch, commandsJSON, kindsJSON, time.Now().UTC().Format(time.RFC3339Nano), old.ID)
	return err
}

func marshalRepoMaintMaps(commands map[string]string, kinds map[string]bool) (string, string, error) {
	if commands == nil {
		commands = map[string]string{}
	}
	if kinds == nil {
		kinds = map[string]bool{}
	}
	c, err := json.Marshal(commands)
	if err != nil {
		return "", "", err
	}
	k, err := json.Marshal(kinds)
	if err != nil {
		return "", "", err
	}
	return string(c), string(k), nil
}

func (a *App) storeRepoMaintSecret(ruleID, secret string) error {
	if a == nil || a.credentialStore == nil {
		return fmt.Errorf("credential store unavailable")
	}
	return a.credentialStore.Modify(repoMaintSecretProvider(ruleID), func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		next := &oauth.StoredCredential{Type: "oauth", AccessToken: secret}
		if old != nil {
			next = old
			next.AccessToken = secret
			next.Type = "oauth"
		}
		return next, nil
	})
}

func (a *App) repoMaintSecret(ruleID string) (string, error) {
	if a == nil || a.credentialStore == nil {
		return "", fmt.Errorf("credential store unavailable")
	}
	cred, err := a.credentialStore.Read(repoMaintSecretProvider(ruleID))
	if err != nil {
		return "", err
	}
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return "", fmt.Errorf("webhook secret 不存在")
	}
	return cred.AccessToken, nil
}

func (a *App) publicRepoMaintRule(id string) (GitHubRepoMaintRule, error) {
	row, err := a.loadRepoMaintRule(id)
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	depth, err := a.repoMaintQueueDepth(db)
	if err != nil {
		return GitHubRepoMaintRule{}, err
	}
	return row.public(depth[id]), nil
}

func newRepoMaintID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], nil
}

func newRepoMaintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func enabledRepoMaintKinds(kinds map[string]bool) []string {
	out := make([]string, 0, len(githubRepoMaintKinds))
	for _, kind := range githubRepoMaintKinds {
		if kinds[kind] {
			out = append(out, kind)
		}
	}
	return out
}

func githubHookEventsForKinds(kinds map[string]bool) []string {
	want := map[string]bool{}
	for _, kind := range enabledRepoMaintKinds(kinds) {
		switch kind {
		case "issues.opened", "issues.closed", "issues.labeled":
			want["issues"] = true
		case "issue_comment.created":
			want["issue_comment"] = true
		case "pull_request.opened", "pull_request.merged", "pull_request.review_requested":
			want["pull_request"] = true
		case "pull_request_review.submitted":
			want["pull_request_review"] = true
		case "push":
			want["push"] = true
		case "check_run.failure":
			want["check_run"] = true
		}
	}
	order := []string{"issues", "issue_comment", "pull_request", "pull_request_review", "push", "check_run"}
	out := make([]string, 0, len(want))
	for _, name := range order {
		if want[name] {
			out = append(out, name)
		}
	}
	return out
}
