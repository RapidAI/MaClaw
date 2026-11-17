package repomaint

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// normalizeWebhook maps one signed GitHub payload onto a RepoMaintEvent.
// keep is false when the event is not one this rule asked for. A push whose
// rule branch is empty and whose payload has no default_branch is kept;
// the desktop decides the branch with its own token.
func normalizeWebhook(rule ruleRow, githubEvent string, body []byte) (RepoMaintEvent, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return RepoMaintEvent{}, false
	}
	event := strings.TrimSpace(githubEvent)
	var ev RepoMaintEvent
	var ok bool
	switch event {
	case "issues":
		ev, ok = normalizeIssue(rule, payload)
	case "issue_comment":
		ev, ok = normalizeIssueComment(rule, payload)
	case "pull_request":
		ev, ok = normalizePullRequest(rule, payload)
	case "pull_request_review":
		ev, ok = normalizeReview(rule, payload)
	case "push":
		ev, ok = normalizePush(rule, payload)
	case "check_run":
		ev, ok = normalizeCheck(rule, payload)
	default:
		return RepoMaintEvent{}, false
	}
	if !ok || !kindEnabled(rule, ev.EventKind) || ev.ObjectID == "" {
		return RepoMaintEvent{}, false
	}
	ev.RuleID = rule.RuleID
	return ev, true
}

func kindEnabled(rule ruleRow, kind string) bool {
	for _, item := range rule.EventKinds {
		if item == kind {
			return true
		}
	}
	return false
}

func normalizeIssue(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	action := str(payload, "action")
	issue := obj(payload, "issue")
	if issue == nil {
		return RepoMaintEvent{}, false
	}
	number, ok := asInt(issue["number"])
	if !ok || number <= 0 {
		return RepoMaintEvent{}, false
	}
	var kind, objectID string
	switch action {
	case "opened":
		kind = kindIssuesOpened
		objectID = "issue:" + strconv.Itoa(number)
	case "closed":
		kind = kindIssuesClosed
		objectID = "issue:" + strconv.Itoa(number)
	case "labeled":
		labelName := str(obj(payload, "label"), "name")
		if labelName == "" {
			return RepoMaintEvent{}, false
		}
		kind = kindIssuesLabeled
		objectID = "issue:" + strconv.Itoa(number) + ":label:" + url.PathEscape(labelName)
	default:
		return RepoMaintEvent{}, false
	}
	owner, repo, branch := repoIdentity(obj(payload, "repository"))
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	body, runes := clipBody(str(issue, "body"))
	ev := baseEvent(kind, objectID, owner, repo, branch)
	ev.Number = number
	ev.Title = str(issue, "title")
	ev.Author = userLogin(issue["user"])
	ev.HTMLURL = str(issue, "html_url")
	ev.APIURL = str(issue, "url")
	ev.Labels = labelNames(issue["labels"])
	ev.Body = body
	ev.BodyRunes = runes
	return ev, true
}

func normalizeIssueComment(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	if str(payload, "action") != "created" {
		return RepoMaintEvent{}, false
	}
	comment := obj(payload, "comment")
	issue := obj(payload, "issue")
	if comment == nil || issue == nil {
		return RepoMaintEvent{}, false
	}
	commentID, ok := asInt64(comment["id"])
	if !ok || commentID <= 0 {
		return RepoMaintEvent{}, false
	}
	number, ok := asInt(issue["number"])
	if !ok || number <= 0 {
		return RepoMaintEvent{}, false
	}
	owner, repo, branch := repoIdentity(obj(payload, "repository"))
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	body, runes := clipBody(str(comment, "body"))
	ev := baseEvent(kindIssueComment, "comment:"+strconv.FormatInt(commentID, 10), owner, repo, branch)
	ev.Number = number
	ev.Title = str(issue, "title")
	ev.Author = userLogin(comment["user"])
	ev.HTMLURL = str(comment, "html_url")
	ev.APIURL = str(comment, "url")
	ev.CommentID = commentID
	ev.Body = body
	ev.BodyRunes = runes
	return ev, true
}

func normalizePullRequest(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	action := str(payload, "action")
	pr := obj(payload, "pull_request")
	if pr == nil {
		return RepoMaintEvent{}, false
	}
	number, ok := asInt(pr["number"])
	if !ok || number <= 0 {
		return RepoMaintEvent{}, false
	}
	var kind, objectID string
	switch action {
	case "opened":
		kind = kindPullOpened
		objectID = "pr:" + strconv.Itoa(number)
	case "closed":
		if !asBool(pr["merged"]) {
			return RepoMaintEvent{}, false
		}
		kind = kindPullMerged
		objectID = "pr:" + strconv.Itoa(number)
	case "review_requested":
		reviewer := reviewerName(payload)
		if reviewer == "" {
			return RepoMaintEvent{}, false
		}
		kind = kindReviewRequested
		objectID = "pr:" + strconv.Itoa(number) + ":reviewer:" + reviewer
	default:
		return RepoMaintEvent{}, false
	}
	owner, repo, branch := repoIdentity(obj(payload, "repository"))
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	body, runes := clipBody(str(pr, "body"))
	ev := baseEvent(kind, objectID, owner, repo, branch)
	ev.Number = number
	ev.Title = str(pr, "title")
	ev.Author = userLogin(pr["user"])
	ev.HTMLURL = str(pr, "html_url")
	ev.APIURL = str(pr, "url")
	ev.Draft = asBool(pr["draft"])
	if head := obj(pr, "head"); head != nil {
		ev.HeadRef = str(head, "ref")
		ev.SHA = str(head, "sha")
	}
	if base := obj(pr, "base"); base != nil {
		ev.BaseRef = str(base, "ref")
	}
	ev.Labels = labelNames(pr["labels"])
	ev.Body = body
	ev.BodyRunes = runes
	return ev, true
}

func normalizeReview(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	if str(payload, "action") != "submitted" {
		return RepoMaintEvent{}, false
	}
	review := obj(payload, "review")
	pr := obj(payload, "pull_request")
	if review == nil || pr == nil {
		return RepoMaintEvent{}, false
	}
	reviewID, ok := asInt64(review["id"])
	if !ok || reviewID <= 0 {
		return RepoMaintEvent{}, false
	}
	number, _ := asInt(pr["number"])
	owner, repo, branch := repoIdentity(obj(payload, "repository"))
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	body, runes := clipBody(str(review, "body"))
	ev := baseEvent(kindReviewSubmitted, "review:"+strconv.FormatInt(reviewID, 10), owner, repo, branch)
	ev.Number = number
	ev.Title = str(pr, "title")
	ev.Author = userLogin(review["user"])
	ev.HTMLURL = str(review, "html_url")
	ev.APIURL = str(review, "url")
	ev.ReviewID = reviewID
	ev.Body = body
	ev.BodyRunes = runes
	return ev, true
}

func normalizePush(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	ref := str(payload, "ref")
	const heads = "refs/heads/"
	if !strings.HasPrefix(ref, heads) || len(ref) == len(heads) {
		return RepoMaintEvent{}, false
	}
	branch := ref[len(heads):]
	sha := str(payload, "after")
	if sha == "" {
		return RepoMaintEvent{}, false
	}
	repoObj := obj(payload, "repository")
	owner, repo, defaultBranch := repoIdentity(repoObj)
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	want := strings.TrimSpace(rule.PushBranch)
	switch {
	case want != "":
		if branch != want {
			return RepoMaintEvent{}, false
		}
	case strings.TrimSpace(defaultBranch) == "":
		// Keep it. The desktop looks up the default branch with its token.
	default:
		if branch != defaultBranch {
			return RepoMaintEvent{}, false
		}
	}
	ev := baseEvent(kindPush, "commit:"+sha, owner, repo, defaultBranch)
	ev.Ref = ref
	ev.SHA = sha
	ev.Author = str(obj(payload, "pusher"), "name")
	if ev.Author == "" {
		ev.Author = str(payload, "pusher")
	}
	return ev, true
}

func normalizeCheck(rule ruleRow, payload map[string]any) (RepoMaintEvent, bool) {
	if str(payload, "action") != "completed" {
		return RepoMaintEvent{}, false
	}
	check := obj(payload, "check_run")
	if check == nil {
		return RepoMaintEvent{}, false
	}
	if str(check, "conclusion") != "failure" {
		return RepoMaintEvent{}, false
	}
	id, ok := asInt64(check["id"])
	if !ok || id <= 0 {
		return RepoMaintEvent{}, false
	}
	owner, repo, branch := repoIdentity(obj(payload, "repository"))
	if owner == "" {
		owner = rule.Owner
	}
	if repo == "" {
		repo = rule.Repo
	}
	ev := baseEvent(kindCheckFailure, "check_run:"+strconv.FormatInt(id, 10), owner, repo, branch)
	ev.CheckName = str(check, "name")
	ev.Conclusion = "failure"
	ev.SHA = str(check, "head_sha")
	ev.PRNumbers = unmergedPRNumbers(check["pull_requests"])
	return ev, true
}

func baseEvent(kind, objectID, owner, repo, defaultBranch string) RepoMaintEvent {
	return RepoMaintEvent{
		EventKind:     kind,
		ObjectID:      objectID,
		Owner:         owner,
		Repo:          repo,
		DefaultBranch: defaultBranch,
	}
}

func repoIdentity(repo map[string]any) (owner, name, defaultBranch string) {
	if repo == nil {
		return "", "", ""
	}
	name = str(repo, "name")
	defaultBranch = str(repo, "default_branch")
	switch raw := repo["owner"].(type) {
	case string:
		owner = raw
	case map[string]any:
		owner = str(raw, "login")
		if owner == "" {
			owner = str(raw, "name")
		}
	}
	if owner == "" || name == "" {
		if full := str(repo, "full_name"); strings.Contains(full, "/") {
			parts := strings.SplitN(full, "/", 2)
			if owner == "" {
				owner = parts[0]
			}
			if name == "" {
				name = parts[1]
			}
		}
	}
	return owner, name, defaultBranch
}

func reviewerName(payload map[string]any) string {
	if login := userLogin(payload["requested_reviewer"]); login != "" {
		return login
	}
	if team := obj(payload, "requested_team"); team != nil {
		if slug := str(team, "slug"); slug != "" {
			return slug
		}
	}
	return ""
}

func userLogin(v any) string {
	switch u := v.(type) {
	case string:
		return u
	case map[string]any:
		return str(u, "login")
	default:
		return ""
	}
}

func labelNames(v any) []string {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := ""
		switch n := item.(type) {
		case string:
			name = n
		case map[string]any:
			name = str(n, "name")
		}
		if name != "" {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func unmergedPRNumbers(v any) []int {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	var nums []int
	for _, item := range items {
		pr, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if merged, exists := pr["merged_at"]; exists && merged != nil {
			if text, isStr := merged.(string); !isStr || strings.TrimSpace(text) != "" {
				continue
			}
		}
		n, ok := asInt(pr["number"])
		if ok && n > 0 {
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		return nil
	}
	sort.Ints(nums)
	return nums
}

func clipBody(body string) (string, int) {
	runes := []rune(body)
	n := len(runes)
	if n > maxBodyRunes {
		return string(runes[:maxBodyRunes]), n
	}
	return body, n
}

func obj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func asInt(v any) (int, bool) {
	n, ok := asInt64(v)
	if !ok || n > int64(^uint(0)>>1) {
		return 0, false
	}
	return int(n), true
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i, true
		}
		f, ferr := n.Float64()
		if ferr != nil {
			return 0, false
		}
		return int64(f), true
	case float64:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
