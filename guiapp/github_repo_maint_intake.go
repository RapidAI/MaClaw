package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	githubRepoMaintIntakeReady = true
}

const (
	githubRepoMaintPlatform               = "github-repo-maint"
	githubRepoMaintMaxIterationExtensions = 3
	repoMaintReplayCap                    = 200
	repoMaintCommentPages                 = 3
)

var (
	githubRepoMaintQueueCap = 100
	errRepoMaintHoldDelivery = errors.New("repo maint delivery held")
	repoMaintMarkerRE        = regexp.MustCompile(`(?m)^<!-- maclaw-repo-maint: rule=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}) event=([a-z0-9_.]+) object=([A-Za-z0-9._:%-]+) -->$`)
	repoMaintWorkers         sync.Map
	repoMaintActiveUsers     sync.Map
	repoMaintPendingWork     sync.Map
	repoMaintBranchCache     sync.Map
)

// repoMaintEvent is the desktop copy of the Hub normalized payload.
// guiapp does not import hub/internal. object_id is already computed.
type repoMaintEvent struct {
	DeliveryID     string   `json:"delivery_id"`
	GitHubDelivery string   `json:"github_delivery"`
	RuleID         string   `json:"rule_id"`
	EventKind      string   `json:"event_kind"`
	ObjectID       string   `json:"object_id"`
	Owner          string   `json:"owner"`
	Repo           string   `json:"repo"`
	Number         int      `json:"number,omitempty"`
	Title          string   `json:"title,omitempty"`
	Author         string   `json:"author,omitempty"`
	HTMLURL        string   `json:"html_url,omitempty"`
	APIURL         string   `json:"api_url,omitempty"`
	HeadRef        string   `json:"head_ref,omitempty"`
	BaseRef        string   `json:"base_ref,omitempty"`
	Ref            string   `json:"ref,omitempty"`
	Draft          bool     `json:"draft,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Body           string   `json:"body,omitempty"`
	BodyRunes      int      `json:"body_runes,omitempty"`
	CommentID      int64    `json:"comment_id,omitempty"`
	ReviewID       int64    `json:"review_id,omitempty"`
	SHA            string   `json:"sha,omitempty"`
	CheckName      string   `json:"check_name,omitempty"`
	Conclusion     string   `json:"conclusion,omitempty"`
	DefaultBranch  string   `json:"default_branch,omitempty"`
	PRNumbers      []int    `json:"pr_numbers,omitempty"`
}

type repoMaintJobRow struct {
	ID              string
	RuleID          string
	DeliveryID      string
	EventKind       string
	ObjectID        string
	GhostDir        string
	Status          string
	TaskProjectPath string
	SessionUserID   string
	LastError       string
	CreatedAt       string
	UpdatedAt       string
}

type repoMaintPending struct {
	Event repoMaintEvent
}

type cachedDefaultBranch struct {
	Name string
	At   time.Time
}

func (a *App) ensureRepoMaintWorker() {
	if a == nil || testing.Testing() {
		return
	}
	if _, loaded := repoMaintWorkers.LoadOrStore(a, true); loaded {
		return
	}
	go func() {
		for {
			a.pumpRepoMaintOnce()
			time.Sleep(2 * time.Second)
		}
	}()
}

func (a *App) noteRepoMaintRuleError(id, msg string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET last_error=?, updated_at=? WHERE id=?`, msg, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (a *App) requeueRepoMaintIfReady(ruleID, ghost string) {
	_ = ghost
	rule, err := a.loadRepoMaintRule(ruleID)
	if err != nil {
		return
	}
	class, _, err := classifyGhostDir(rule.GhostDir, rule.Owner, rule.Repo)
	if err != nil || class != ghostDirReady {
		return
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE jobs SET status='queued', updated_at=? WHERE rule_id=? AND status='blocked_workspace'`, now, ruleID); err != nil {
		log.Printf("[github-repo-maint] requeue blocked failed rule_id=%s err=%v", ruleID, err)
	}
}

func (a *App) repoMaintGitHub(method, rawURL string, payload any) (int, []byte, http.Header, error) {
	status, body, hdr, err := a.doGitHubExchange(method, rawURL, payload)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid or revoked") {
		a.blockRepoMaintAuth()
	}
	return status, body, hdr, err
}

func (a *App) blockRepoMaintAuth() {
	if a == nil {
		return
	}
	path := filepath.Join(a.GetDataDir(), githubRepoMaintDBName)
	if _, err := os.Stat(path); err != nil {
		return
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE rules SET auth_blocked=1, updated_at=? WHERE enabled=1`, now); err != nil {
		log.Printf("[github-repo-maint] auth block failed err=%v", err)
		return
	}
	log.Printf("[github-repo-maint] token revoked; enabled rules marked auth_blocked")
	rows, err := db.Query(`SELECT id FROM rules WHERE enabled=1`)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	hub, hubErr := a.repoMaintHubIdentity()
	for _, id := range ids {
		if hubErr != nil {
			break
		}
		rule, err := a.loadRepoMaintRule(id)
		if err != nil {
			continue
		}
		secret, err := a.repoMaintSecret(id)
		if err != nil {
			continue
		}
		if err := a.putRepoMaintHubRule(hub, rule, secret, false, rule.HookID); err != nil {
			log.Printf("[github-repo-maint] hub disable after revoke failed rule_id=%s err=%v", id, err)
		}
	}
}

func (a *App) resyncRepoMaintAfterLogin() {
	if a == nil || !githubRepoMaintIntakeReady {
		return
	}
	path := filepath.Join(a.GetDataDir(), githubRepoMaintDBName)
	if _, err := os.Stat(path); err != nil {
		return
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.Exec(`UPDATE rules SET auth_blocked=0, updated_at=? WHERE enabled=1`, now)
	rows, err := db.Query(`SELECT id FROM rules WHERE enabled=1`)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	hub, hubErr := a.repoMaintHubIdentity()
	for _, id := range ids {
		rule, err := a.loadRepoMaintRule(id)
		if err != nil || hubErr != nil {
			continue
		}
		secret, err := a.repoMaintSecret(id)
		if err != nil {
			continue
		}
		if err := a.putRepoMaintHubRule(hub, rule, secret, true, rule.HookID); err != nil {
			log.Printf("[github-repo-maint] login resync hub failed rule_id=%s err=%v", id, err)
			continue
		}
		webhook, err := a.repoMaintWebhookURL(hub.BaseURL, rule.HookToken)
		if err != nil {
			continue
		}
		spec := gitHubHookSpec{URL: webhook, Secret: secret, Events: githubHookEventsForKinds(rule.Kinds), Active: true}
		if rule.HookID > 0 {
			if err := a.updateGitHubRepoHook(rule.Owner, rule.Repo, rule.HookID, spec); err != nil {
				log.Printf("[github-repo-maint] login resync hook failed rule_id=%s err=%v", id, err)
			}
			continue
		}
		created, err := a.createGitHubRepoHook(rule.Owner, rule.Repo, spec)
		if err != nil {
			log.Printf("[github-repo-maint] login resync hook create failed rule_id=%s err=%v", id, err)
			continue
		}
		_, _ = db.Exec(`UPDATE rules SET hook_id=?, updated_at=? WHERE id=?`, created, time.Now().UTC().Format(time.RFC3339Nano), id)
		_ = a.putRepoMaintHubRule(hub, rule, secret, true, created)
	}
}

func (a *App) acceptRepoMaintFrame(raw json.RawMessage) {
	var ev repoMaintEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		log.Printf("[github-repo-maint] frame decode failed")
		return
	}
	if err := a.acceptRepoMaintDelivery(ev); err != nil && !errors.Is(err, errRepoMaintHoldDelivery) {
		log.Printf("[github-repo-maint] accept failed delivery_id=%s err=%v", ev.DeliveryID, err)
	}
}

func (a *App) pullRepoMaintDeliveries() {
	if a == nil {
		return
	}
	hub, err := a.repoMaintHubIdentity()
	if err != nil {
		return
	}
	rawURL, err := repoMaintJoin(hub.BaseURL, "api", "github", "repo-maint", "deliveries")
	if err != nil {
		return
	}
	_, body, err := a.doRepoMaintHubJSON(hub, http.MethodGet, rawURL, nil)
	if err != nil {
		log.Printf("[github-repo-maint] pull deliveries failed err=%v", err)
		return
	}
	var wrap struct {
		Deliveries []struct {
			Payload repoMaintEvent `json:"payload"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return
	}
	for _, item := range wrap.Deliveries {
		if err := a.acceptRepoMaintDelivery(item.Payload); err != nil && !errors.Is(err, errRepoMaintHoldDelivery) {
			log.Printf("[github-repo-maint] pull accept failed delivery_id=%s err=%v", item.Payload.DeliveryID, err)
		}
	}
}

func (a *App) ackRepoMaintDelivery(deliveryID string) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" || a == nil {
		return
	}
	hub, err := a.repoMaintHubIdentity()
	if err != nil {
		log.Printf("[github-repo-maint] ack skipped delivery_id=%s err=%v", deliveryID, err)
		return
	}
	rawURL, err := repoMaintJoin(hub.BaseURL, "api", "github", "repo-maint", "deliveries", deliveryID, "ack")
	if err != nil {
		return
	}
	status, _, err := a.doRepoMaintHubJSON(hub, http.MethodPost, rawURL, map[string]any{})
	if err != nil {
		log.Printf("[github-repo-maint] ack failed delivery_id=%s http=%d err=%v", deliveryID, status, err)
		return
	}
	log.Printf("[github-repo-maint] ack delivery_id=%s http=%d", deliveryID, status)
}

// acceptRepoMaintDelivery records one Hub event and acks after the local
// transaction commits. errRepoMaintHoldDelivery means the desktop did not ack.
func (a *App) acceptRepoMaintDelivery(ev repoMaintEvent) error {
	ev.DeliveryID = strings.TrimSpace(ev.DeliveryID)
	ev.RuleID = strings.TrimSpace(ev.RuleID)
	ev.EventKind = strings.TrimSpace(ev.EventKind)
	ev.ObjectID = strings.TrimSpace(ev.ObjectID)
	if ev.DeliveryID == "" || ev.RuleID == "" || ev.ObjectID == "" {
		if ev.DeliveryID != "" {
			a.ackRepoMaintDelivery(ev.DeliveryID)
		}
		return nil
	}
	rule, err := a.loadRepoMaintRule(ev.RuleID)
	if err != nil {
		a.ackRepoMaintDelivery(ev.DeliveryID)
		return nil
	}
	if !strings.EqualFold(rule.Owner, ev.Owner) || !strings.EqualFold(rule.Repo, ev.Repo) {
		a.ackRepoMaintDelivery(ev.DeliveryID)
		return nil
	}
	if ev.EventKind == "push" {
		ok, hold := a.repoMaintPushAllowed(rule, &ev)
		if hold != nil {
			return hold
		}
		if !ok {
			a.ackRepoMaintDelivery(ev.DeliveryID)
			return nil
		}
	}
	if !rule.Enabled {
		if err := a.insertRepoMaintJob(rule, ev, ev.DeliveryID, "skipped_disabled", ""); err != nil && !repoMaintUnique(err) {
			return err
		}
		a.ackRepoMaintDelivery(ev.DeliveryID)
		return nil
	}
	if !repoMaintKindArmed(rule, ev.EventKind) {
		a.ackRepoMaintDelivery(ev.DeliveryID)
		return nil
	}
	if err := a.insertRepoMaintJob(rule, ev, ev.DeliveryID, "", ""); err != nil && !repoMaintUnique(err) {
		return err
	}
	a.ackRepoMaintDelivery(ev.DeliveryID)
	return nil
}

func (a *App) repoMaintPushAllowed(rule repoMaintRuleRow, ev *repoMaintEvent) (bool, error) {
	wanted := strings.TrimSpace(rule.PushBranch)
	if wanted == "" {
		wanted = strings.TrimSpace(ev.DefaultBranch)
	}
	if wanted == "" {
		branch, err := a.repoMaintDefaultBranch(rule.Owner, rule.Repo)
		if err != nil || branch == "" {
			return false, errRepoMaintHoldDelivery
		}
		wanted = branch
		ev.DefaultBranch = branch
	}
	ref := strings.TrimSpace(ev.Ref)
	return ref == "refs/heads/"+wanted || ref == wanted, nil
}

func (a *App) repoMaintDefaultBranch(owner, repo string) (string, error) {
	key := strings.ToLower(owner) + "/" + strings.ToLower(repo)
	if v, ok := repoMaintBranchCache.Load(key); ok {
		cached := v.(cachedDefaultBranch)
		if cached.Name != "" && time.Since(cached.At) < time.Hour {
			return cached.Name, nil
		}
	}
	rawURL, err := gitHubReposURL(owner, repo)
	if err != nil {
		return "", err
	}
	_, body, _, err := a.repoMaintGitHub(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	var doc struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	if doc.DefaultBranch == "" {
		return "", errRepoMaintHoldDelivery
	}
	repoMaintBranchCache.Store(key, cachedDefaultBranch{Name: doc.DefaultBranch, At: time.Now()})
	return doc.DefaultBranch, nil
}

func repoMaintKindArmed(rule repoMaintRuleRow, kind string) bool {
	if rule.Kinds == nil || !rule.Kinds[kind] {
		return false
	}
	return strings.TrimSpace(rule.Commands[kind]) != ""
}

func repoMaintUnique(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func (a *App) insertRepoMaintJob(rule repoMaintRuleRow, ev repoMaintEvent, deliveryID, forcedStatus, forcedError string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	status := forcedStatus
	last := forcedError
	if status == "" {
		var depth int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE rule_id=? AND status IN ('queued','running','awaiting_user')`, rule.ID).Scan(&depth); err != nil {
			return err
		}
		cap := githubRepoMaintQueueCap
		if cap < 1 {
			cap = 1
		}
		if depth >= cap {
			var existing int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE rule_id=? AND event_kind=? AND object_id=? AND status='failed' AND last_error='queue_full'`, rule.ID, ev.EventKind, ev.ObjectID).Scan(&existing); err != nil {
				return err
			}
			if existing > 0 {
				return nil
			}
			status = "failed"
			last = "queue_full"
		} else {
			status = "queued"
		}
	}
	id, err := newRepoMaintID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`INSERT INTO jobs (
		id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status,
		task_project_path, session_user_id, last_error, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,'','',?,?,?)`,
		id, rule.ID, deliveryID, ev.EventKind, ev.ObjectID, rule.GhostDir, status, last, now, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	repoMaintPendingWork.Store(id, repoMaintPending{Event: ev})
	if status == "failed" && last == "queue_full" {
		_ = a.noteRepoMaintRuleError(rule.ID, "queue_full")
		log.Printf("[github-repo-maint] queue_full rule_id=%s object=%s", rule.ID, ev.ObjectID)
	}
	log.Printf("[github-repo-maint] job %s rule_id=%s kind=%s object=%s", status, rule.ID, ev.EventKind, ev.ObjectID)
	return nil
}

func (a *App) replayGitHubRepoMaint(id string) error {
	rule, err := a.loadRepoMaintRule(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if !rule.Enabled {
		return fmt.Errorf("规则未启用")
	}
	budget := repoMaintReplayCap
	next := strings.TrimSpace(rule.ReplayNextURL)
	if next != "" {
		if _, err := repoMaintPinnedGitHubURL(next); err != nil {
			_ = a.setRepoMaintReplayNext(rule.ID, "")
			return fmt.Errorf("回放地址被拒绝")
		}
	}
	phase := repoMaintReplayPhase(next)
	if phase == "" || phase == "issues" {
		if rule.Kinds["issues.opened"] || next == "" || phase == "issues" {
			var err error
			next, budget, err = a.replayIssuePages(rule, next, budget)
			if err != nil {
				return err
			}
		}
	}
	if budget > 0 && (next == "" || repoMaintReplayPhase(next) == "pulls") {
		var err error
		next, budget, err = a.replayPullPages(rule, next, budget)
		if err != nil {
			return err
		}
	}
	if err := a.setRepoMaintReplayNext(rule.ID, next); err != nil {
		return err
	}
	log.Printf("[github-repo-maint] replay rule_id=%s remaining=%d next=%t", rule.ID, budget, next != "")
	return nil
}

func repoMaintReplayPhase(raw string) string {
	raw = strings.ToLower(raw)
	switch {
	case strings.Contains(raw, "/check-runs"):
		return "checks"
	case strings.Contains(raw, "/pulls"):
		return "pulls"
	case strings.Contains(raw, "/issues"):
		return "issues"
	default:
		return ""
	}
}

func (a *App) setRepoMaintReplayNext(id, next string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE rules SET replay_next_url=?, updated_at=? WHERE id=?`, next, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (a *App) replayIssuePages(rule repoMaintRuleRow, next string, budget int) (string, int, error) {
	if !rule.Kinds["issues.opened"] && repoMaintReplayPhase(next) != "issues" {
		return "", budget, nil
	}
	if !rule.Kinds["issues.opened"] {
		return "", budget, nil
	}
	page := next
	if repoMaintReplayPhase(page) != "issues" {
		var err error
		page, err = repoMaintListURL(rule.Owner, rule.Repo, []string{"issues"}, url.Values{"state": {"open"}, "per_page": {"100"}})
		if err != nil {
			return "", budget, err
		}
	}
	for budget > 0 && page != "" {
		body, link, err := a.repoMaintGetPage(page)
		if err != nil {
			return page, budget, err
		}
		var issues []repoMaintGHIssue
		if err := json.Unmarshal(body, &issues); err != nil {
			return "", budget, err
		}
		finished := true
		for _, issue := range issues {
			if budget <= 0 {
				finished = false
				break
			}
			budget--
			if issue.PullRequest != nil {
				continue
			}
			ev := repoMaintEvent{
				RuleID: rule.ID, EventKind: "issues.opened", ObjectID: "issue:" + itoa(issue.Number),
				Owner: rule.Owner, Repo: rule.Repo, Number: issue.Number, Title: issue.Title,
				Author: issue.User.Login, HTMLURL: issue.HTMLURL, Body: issue.Body,
			}
			if err := a.insertRepoMaintJob(rule, ev, "", "", ""); err != nil && !repoMaintUnique(err) {
				return page, budget, err
			}
		}
		if !finished {
			return page, 0, nil
		}
		page = link
	}
	if page != "" {
		return page, budget, nil
	}
	return "", budget, nil
}

func (a *App) replayPullPages(rule repoMaintRuleRow, next string, budget int) (string, int, error) {
	wantPR := rule.Kinds["pull_request.opened"]
	wantCheck := rule.Kinds["check_run.failure"]
	if !wantPR && !wantCheck && repoMaintReplayPhase(next) != "pulls" && repoMaintReplayPhase(next) != "checks" {
		return "", budget, nil
	}
	if !wantPR && !wantCheck {
		return "", budget, nil
	}
	page := next
	if repoMaintReplayPhase(page) == "checks" {
		body, _, err := a.repoMaintGetPage(page)
		if err != nil {
			return page, budget, err
		}
		budget = a.enqueueFailingChecks(rule, body, 0, budget)
		return "", budget, nil
	}
	if repoMaintReplayPhase(page) != "pulls" {
		var err error
		page, err = repoMaintListURL(rule.Owner, rule.Repo, []string{"pulls"}, url.Values{"state": {"open"}, "per_page": {"100"}})
		if err != nil {
			return "", budget, err
		}
	}
	for budget > 0 && page != "" {
		body, link, err := a.repoMaintGetPage(page)
		if err != nil {
			return page, budget, err
		}
		var pulls []repoMaintGHP
		if err := json.Unmarshal(body, &pulls); err != nil {
			return "", budget, err
		}
		finished := true
		for _, pr := range pulls {
			if wantPR {
				if budget <= 0 {
					finished = false
					break
				}
				budget--
				ev := repoMaintEvent{
					RuleID: rule.ID, EventKind: "pull_request.opened", ObjectID: "pr:" + itoa(pr.Number),
					Owner: rule.Owner, Repo: rule.Repo, Number: pr.Number, Title: pr.Title,
					Author: pr.User.Login, HTMLURL: pr.HTMLURL, HeadRef: pr.Head.Ref, BaseRef: pr.Base.Ref,
					SHA: pr.Head.SHA, Draft: pr.Draft,
				}
				if err := a.insertRepoMaintJob(rule, ev, "", "", ""); err != nil && !repoMaintUnique(err) {
					return page, budget, err
				}
			}
			if wantCheck && pr.Head.SHA != "" {
				checkURL, err := gitHubReposURL(rule.Owner, rule.Repo, "commits", pr.Head.SHA, "check-runs")
				if err != nil {
					return page, budget, err
				}
				checkBody, _, err := a.repoMaintGetPage(checkURL)
				if err != nil {
					return page, budget, err
				}
				before := budget
				budget = a.enqueueFailingChecks(rule, checkBody, pr.Number, budget)
				if budget <= 0 && before > 0 {
					finished = false
					break
				}
			}
		}
		if !finished {
			return page, 0, nil
		}
		page = link
	}
	if page != "" {
		return page, budget, nil
	}
	return "", budget, nil
}

func (a *App) enqueueFailingChecks(rule repoMaintRuleRow, body []byte, prNumber, budget int) int {
	var doc struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			HeadSHA    string `json:"head_sha"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return budget
	}
	for _, check := range doc.CheckRuns {
		if budget <= 0 {
			return 0
		}
		if check.Conclusion != "failure" {
			continue
		}
		budget--
		ev := repoMaintEvent{
			RuleID: rule.ID, EventKind: "check_run.failure", ObjectID: "check_run:" + strconv.FormatInt(check.ID, 10),
			Owner: rule.Owner, Repo: rule.Repo, SHA: check.HeadSHA, CheckName: check.Name, Conclusion: "failure",
		}
		if prNumber > 0 {
			ev.PRNumbers = []int{prNumber}
			ev.Number = prNumber
		}
		if err := a.insertRepoMaintJob(rule, ev, "", "", ""); err != nil && !repoMaintUnique(err) {
			log.Printf("[github-repo-maint] check enqueue failed rule_id=%s err=%v", rule.ID, err)
		}
	}
	return budget
}

type repoMaintGHIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

type repoMaintGHP struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func repoMaintListURL(owner, repo string, parts []string, query url.Values) (string, error) {
	raw, err := gitHubReposURL(owner, repo, parts...)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func repoMaintPinnedGitHubURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") || parsed.User != nil {
		return "", fmt.Errorf("回放地址被拒绝")
	}
	return parsed.String(), nil
}

func (a *App) repoMaintGetPage(raw string) ([]byte, string, error) {
	pinned, err := repoMaintPinnedGitHubURL(raw)
	if err != nil {
		return nil, "", err
	}
	_, body, hdr, err := a.repoMaintGitHub(http.MethodGet, pinned, nil)
	if err != nil {
		return nil, "", err
	}
	next := gitHubNextLink(hdr)
	if next == "" {
		return body, "", nil
	}
	if _, err := repoMaintPinnedGitHubURL(next); err != nil {
		return body, "", fmt.Errorf("回放地址被拒绝")
	}
	return body, next, nil
}

func gitHubNextLink(hdr http.Header) string {
	if hdr == nil {
		return ""
	}
	for _, part := range strings.Split(hdr.Get("Link"), ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) && !strings.Contains(part, `rel=next`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return strings.TrimSpace(part[start+1 : end])
		}
	}
	return ""
}

func (a *App) pumpRepoMaintOnce() {
	if a == nil {
		return
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return
	}
	a.refreshRepoMaintActiveUsers(db)
	a.retryRepoMaintComments(db)
	var running int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status='running'`).Scan(&running); err != nil {
		return
	}
	cap := repoMaintMaxParallel()
	if running >= cap {
		return
	}
	rows, err := db.Query(`SELECT id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at FROM jobs WHERE status='queued' ORDER BY created_at ASC`)
	if err != nil {
		return
	}
	defer rows.Close()
	var queued []repoMaintJobRow
	for rows.Next() {
		job, err := scanRepoMaintJob(rows)
		if err != nil {
			return
		}
		queued = append(queued, job)
	}
	busy := map[string]bool{}
	for _, job := range queued {
		if running >= cap {
			return
		}
		dir, normErr := normalizeGhostDir(job.GhostDir)
		if normErr != nil {
			dir = job.GhostDir
		}
		if busy[dir] {
			continue
		}
		var held int
		if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE ghost_dir=? AND status IN ('running','awaiting_user')`, job.GhostDir).Scan(&held); err != nil {
			return
		}
		if held > 0 {
			busy[dir] = true
			continue
		}
		busy[dir] = true
		if a.startRepoMaintJob(job) {
			running++
		}
	}
}

func repoMaintMaxParallel() int {
	n := 2
	if raw := strings.TrimSpace(os.Getenv("MACLAW_REPO_MAINT_MAX_PARALLEL")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			n = parsed
		}
	}
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

func (a *App) refreshRepoMaintActiveUsers(db *sql.DB) {
	rows, err := db.Query(`SELECT session_user_id, ghost_dir FROM jobs WHERE status IN ('running','awaiting_user') AND session_user_id<>''`)
	if err != nil {
		return
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var userID, dir string
		if err := rows.Scan(&userID, &dir); err == nil && userID != "" {
			seen[userID] = dir
		}
	}
	repoMaintActiveUsers.Range(func(key, _ any) bool {
		id, _ := key.(string)
		if _, ok := seen[id]; !ok {
			repoMaintActiveUsers.Delete(id)
		}
		return true
	})
	for id, dir := range seen {
		repoMaintActiveUsers.Store(id, dir)
	}
}

func scanRepoMaintJob(s interface{ Scan(...any) error }) (repoMaintJobRow, error) {
	var job repoMaintJobRow
	err := s.Scan(&job.ID, &job.RuleID, &job.DeliveryID, &job.EventKind, &job.ObjectID, &job.GhostDir, &job.Status, &job.TaskProjectPath, &job.SessionUserID, &job.LastError, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func (a *App) loadRepoMaintJob(id string) (repoMaintJobRow, error) {
	db, err := a.repoMaintDB()
	if err != nil {
		return repoMaintJobRow{}, err
	}
	return scanRepoMaintJob(db.QueryRow(`SELECT id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at FROM jobs WHERE id=?`, id))
}

func (a *App) repoMaintJobForUser(userID string) (repoMaintJobRow, bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" || a == nil {
		return repoMaintJobRow{}, false
	}
	db, err := a.repoMaintDB()
	if err != nil {
		return repoMaintJobRow{}, false
	}
	job, err := scanRepoMaintJob(db.QueryRow(`SELECT id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at FROM jobs WHERE session_user_id=? AND status IN ('running','awaiting_user') ORDER BY updated_at DESC LIMIT 1`, userID))
	if err != nil {
		return repoMaintJobRow{}, false
	}
	return job, true
}

func (a *App) setRepoMaintJobStatus(job repoMaintJobRow, status, taskPath, userID, last string) error {
	db, err := a.repoMaintDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE jobs SET status=?, task_project_path=?, session_user_id=?, last_error=?, updated_at=? WHERE id=?`,
		status, taskPath, userID, last, now, job.ID); err != nil {
		return err
	}
	if status == "running" || status == "awaiting_user" {
		if userID != "" {
			repoMaintActiveUsers.Store(userID, job.GhostDir)
		}
		return nil
	}
	if userID != "" {
		repoMaintActiveUsers.Delete(userID)
	}
	if job.SessionUserID != "" && job.SessionUserID != userID {
		repoMaintActiveUsers.Delete(job.SessionUserID)
	}
	return nil
}

func (a *App) repoMaintMarkerMatched(rule repoMaintRuleRow, ev repoMaintEvent) bool {
	number := repoMaintThreadNumber(ev)
	if number <= 0 || ev.EventKind == "push" {
		return false
	}
	rawURL, err := repoMaintListURL(rule.Owner, rule.Repo, []string{"issues", itoa(number), "comments"}, url.Values{
		"per_page": {"100"}, "sort": {"created"}, "direction": {"desc"},
	})
	if err != nil {
		return false
	}
	for page := 0; page < repoMaintCommentPages && rawURL != ""; page++ {
		body, next, err := a.repoMaintGetPage(rawURL)
		if err != nil {
			return false
		}
		var comments []struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(body, &comments); err != nil {
			return false
		}
		for _, comment := range comments {
			if repoMaintMarkerHits(comment.Body, rule.ID, ev.EventKind, ev.ObjectID) {
				return true
			}
		}
		rawURL = next
	}
	return false
}

func repoMaintMarkerHits(body, ruleID, kind, objectID string) bool {
	for _, match := range repoMaintMarkerRE.FindAllStringSubmatch(body, -1) {
		if len(match) == 4 && strings.EqualFold(match[1], ruleID) && match[2] == kind && match[3] == objectID {
			return true
		}
	}
	return false
}

func repoMaintThreadNumber(ev repoMaintEvent) int {
	switch ev.EventKind {
	case "push":
		return 0
	case "check_run.failure":
		return smallestPositive(ev.PRNumbers)
	default:
		if ev.Number > 0 {
			return ev.Number
		}
		return numberFromObjectID(ev.ObjectID)
	}
}

func smallestPositive(nums []int) int {
	best := 0
	for _, n := range nums {
		if n > 0 && (best == 0 || n < best) {
			best = n
		}
	}
	return best
}

func numberFromObjectID(objectID string) int {
	parts := strings.Split(objectID, ":")
	if len(parts) < 2 {
		return 0
	}
	switch parts[0] {
	case "issue", "pr":
		n, _ := strconv.Atoi(parts[1])
		return n
	default:
		return 0
	}
}

func (a *App) retryRepoMaintComments(db *sql.DB) {
	rows, err := db.Query(`SELECT id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at FROM jobs WHERE status='comment_pending'`)
	if err != nil {
		return
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		job, err := scanRepoMaintJob(rows)
		if err != nil {
			return
		}
		updated, err := time.Parse(time.RFC3339Nano, job.UpdatedAt)
		if err != nil {
			continue
		}
		if now.Sub(updated) < repoMaintCommentDelay(commentRetryCount(job.LastError)) {
			continue
		}
		a.postRepoMaintComment(job)
	}
}

func commentRetryCount(last string) int {
	const key = "comment_retry="
	idx := strings.Index(last, key)
	if idx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(last[idx+len(key):]))
	if n < 0 {
		return 0
	}
	return n
}

func repoMaintCommentDelay(n int) time.Duration {
	switch {
	case n <= 0:
		return time.Minute
	case n == 1:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

func (a *App) postRepoMaintComment(job repoMaintJobRow) {
	rule, err := a.loadRepoMaintRule(job.RuleID)
	if err != nil {
		return
	}
	pending, _ := repoMaintPendingWork.Load(job.ID)
	ev := repoMaintEvent{EventKind: job.EventKind, ObjectID: job.ObjectID, RuleID: job.RuleID}
	if work, ok := pending.(repoMaintPending); ok {
		ev = work.Event
	}
	if job.EventKind == "push" {
		_ = a.setRepoMaintJobStatus(job, "succeeded", job.TaskProjectPath, "", "")
		repoMaintPendingWork.Delete(job.ID)
		return
	}
	number := repoMaintThreadNumber(ev)
	if job.EventKind == "check_run.failure" && number <= 0 {
		number = a.smallestUnmergedPR(rule.Owner, rule.Repo, ev.SHA)
	}
	if number <= 0 {
		note := ""
		if job.EventKind == "check_run.failure" {
			note = "检查失败已在本地标记（没有关联的 PR）"
			log.Printf("[github-repo-maint] %s", note)
		}
		_ = a.setRepoMaintJobStatus(job, "succeeded", job.TaskProjectPath, "", note)
		repoMaintPendingWork.Delete(job.ID)
		return
	}
	title := repoMaintTaskTitle(a.repoMaintLang(), rule.Repo, ev)
	body := repoMaintCommentBody(a.repoMaintLang(), ev, title, rule.ID)
	rawURL, err := gitHubReposURL(rule.Owner, rule.Repo, "issues", itoa(number), "comments")
	if err != nil {
		return
	}
	_, _, _, err = a.repoMaintGitHub(http.MethodPost, rawURL, map[string]string{"body": body})
	if err != nil {
		next := commentRetryCount(job.LastError) + 1
		_ = a.setRepoMaintJobStatus(job, "comment_pending", job.TaskProjectPath, "", "comment_retry="+itoa(next))
		log.Printf("[github-repo-maint] comment_pending rule_id=%s object=%s", rule.ID, job.ObjectID)
		return
	}
	_ = a.setRepoMaintJobStatus(job, "succeeded", job.TaskProjectPath, "", "")
	repoMaintPendingWork.Delete(job.ID)
	log.Printf("[github-repo-maint] comment posted rule_id=%s object=%s", rule.ID, job.ObjectID)
}

func (a *App) smallestUnmergedPR(owner, repo, sha string) int {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return 0
	}
	rawURL, err := gitHubReposURL(owner, repo, "commits", sha, "pulls")
	if err != nil {
		return 0
	}
	_, body, _, err := a.repoMaintGitHub(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0
	}
	var pulls []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
	}
	if err := json.Unmarshal(body, &pulls); err != nil {
		return 0
	}
	best := 0
	for _, pr := range pulls {
		if pr.Merged || pr.State == "closed" || pr.Number <= 0 {
			continue
		}
		if best == 0 || pr.Number < best {
			best = pr.Number
		}
	}
	return best
}

func (a *App) repoMaintLang() string {
	lang := ""
	if a != nil {
		lang = a.CurrentLanguage
	}
	return repoMaintUILang(lang)
}

func repoMaintUILang(lang string) string {
	low := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case low == "":
		return "zh-Hans"
	case strings.HasPrefix(low, "zh-hant"), strings.HasPrefix(low, "zh-tw"), strings.HasPrefix(low, "zh-hk"):
		return "zh-Hant"
	case strings.HasPrefix(low, "zh"):
		return "zh-Hans"
	case strings.HasPrefix(low, "en"):
		return "en"
	default:
		return "en"
	}
}
