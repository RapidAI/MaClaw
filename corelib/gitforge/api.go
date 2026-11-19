package gitforge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Client calls one forge's REST API. Token is sent as a header and is
// removed from any error text.
type Client struct {
	HTTP  *http.Client
	Token string
	// Bearer sends a GitLab OAuth token as Authorization: Bearer.
	// A personal access token keeps PRIVATE-TOKEN.
	Bearer bool
}

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: refuseRedirect}
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return fmt.Errorf("redirect refused")
}

// WhoAmI returns the login the token belongs to.
func (c *Client) WhoAmI(ctx context.Context, repo Repo) (string, error) {
	path := "/user"
	if repo.Service == GitLab {
		path = "/user"
	}
	body, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var user map[string]any
	if err := json.Unmarshal(body, &user); err != nil {
		return "", fmt.Errorf("%s user: %w", repo.Service, err)
	}
	login := jsonString(user, "login")
	if login == "" {
		login = jsonString(user, "username")
	}
	if login == "" {
		return "", fmt.Errorf("%s user: no login", repo.Service)
	}
	return repo.Service + " login=" + login, nil
}

// Visible lists the organizations and repositories this token can see.
// The caller uses it so a person can choose what the bot may touch.
func (c *Client) Visible(ctx context.Context, repo Repo) (string, error) {
	login, err := c.WhoAmI(ctx, repo)
	if err != nil {
		return "", err
	}
	var groupsPath, reposPath, groupKey, repoKey string
	if repo.Service == GitLab {
		groupsPath = "/groups?per_page=20&min_access_level=10"
		reposPath = "/projects?membership=true&simple=true&per_page=40&order_by=updated_at"
		groupKey, repoKey = "full_path", "path_with_namespace"
	} else {
		groupsPath = "/user/orgs?per_page=20"
		reposPath = "/user/repos?per_page=40&sort=updated&affiliation=owner,collaborator,organization_member"
		groupKey, repoKey = "login", "full_name"
	}
	groups, groupsErr := c.call(ctx, repo, http.MethodGet, groupsPath, nil)
	repos, err := c.call(ctx, repo, http.MethodGet, reposPath, nil)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", login)
	if groupsErr != nil {
		b.WriteString("organizations: (unavailable)\n")
	} else {
		fmt.Fprintf(&b, "organizations: %s\n", joinField(groups, groupKey))
	}
	names := splitField(repos, repoKey)
	if len(names) == 0 {
		b.WriteString("repositories: none\n")
	} else {
		b.WriteString("repositories:\n")
		for _, name := range names {
			fmt.Fprintf(&b, "%s\n", name)
		}
		if len(names) >= 40 {
			b.WriteString("list may be truncated\n")
		}
	}
	return b.String(), nil
}

func joinField(raw []byte, key string) string {
	names := splitField(raw, key)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func splitField(raw []byte, key string) []string {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		name := jsonString(row, key)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	return out
}

// Issues lists issues. state is open, closed, or all.
func (c *Client) Issues(ctx context.Context, repo Repo, state string, limit int) (string, error) {
	state = normalizeState(repo.Service, state)
	if limit < 1 || limit > 20 {
		limit = 15
	}
	var path string
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/issues?state=" + url.QueryEscape(state) + "&per_page=" + fmt.Sprint(limit) + "&order_by=updated_at"
	} else {
		path = "/repos/" + repo.Path + "/issues?state=" + url.QueryEscape(state) + "&per_page=" + fmt.Sprint(limit)
	}
	body, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", fmt.Errorf("%s issues: %w", repo.Service, err)
	}
	var b strings.Builder
	n := 0
	for _, row := range rows {
		if repo.Service != GitLab {
			if _, pr := row["pull_request"]; pr {
				continue
			}
		}
		n++
		num := jsonNumber(row, "number")
		if num == "" {
			num = jsonNumber(row, "iid")
		}
		title := oneLine(jsonString(row, "title"), 180)
		link := jsonString(row, "html_url")
		if link == "" {
			link = jsonString(row, "web_url")
		}
		updated := jsonString(row, "updated_at")
		fmt.Fprintf(&b, "#%s %s\n%s\nupdated %s\n\n", num, title, link, updated)
	}
	if n == 0 {
		return repo.Service + " " + repo.Path + " has no " + state + " issues", nil
	}
	return fmt.Sprintf("%s %s %d %s issues\n%s", repo.Service, repo.Path, n, state, b.String()), nil
}

// Issue returns one issue's title, state, and body.
func (c *Client) Issue(ctx context.Context, repo Repo, number string) (string, error) {
	number, err := issueNumber(number)
	if err != nil {
		return "", err
	}
	path := "/repos/" + repo.Path + "/issues/" + number
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/issues/" + number
	}
	body, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var row map[string]any
	if err := json.Unmarshal(body, &row); err != nil {
		return "", fmt.Errorf("%s issue: %w", repo.Service, err)
	}
	link := jsonString(row, "html_url")
	if link == "" {
		link = jsonString(row, "web_url")
	}
	text := keepLines(jsonString(row, "body"), 2000)
	if text == "" {
		text = keepLines(jsonString(row, "description"), 2000)
	}
	out := fmt.Sprintf("#%s %s\nstate %s\n%s\n%s", number, oneLine(jsonString(row, "title"), 180), jsonString(row, "state"), link, text)
	if notes := c.issueNotes(ctx, repo, number); notes != "" {
		out += "\n" + notes
	}
	return out, nil
}

func (c *Client) issueNotes(ctx context.Context, repo Repo, number string) string {
	path := "/repos/" + repo.Path + "/issues/" + number + "/comments?per_page=5&sort=created&direction=desc"
	author, bodyKey := "user", "body"
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/issues/" + number + "/notes?per_page=10&sort=desc"
		author = "author"
	}
	raw, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return ""
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, row := range rows {
		if sys, _ := row["system"].(bool); sys {
			continue
		}
		note := oneLine(jsonString(row, bodyKey), 400)
		if note == "" {
			continue
		}
		who := nestedString(row, author, "login")
		if who == "" {
			who = nestedString(row, author, "username")
		}
		if n == 0 {
			b.WriteString("notes:\n")
		}
		n++
		fmt.Fprintf(&b, "- %s: %s\n", who, note)
		if n >= 5 {
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func nestedString(row map[string]any, obj, key string) string {
	child, _ := row[obj].(map[string]any)
	return jsonString(child, key)
}

// Comment posts a note on an issue.
func (c *Client) Comment(ctx context.Context, repo Repo, number, text string) (string, error) {
	number, err := issueNumber(number)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 8000 {
		return "", fmt.Errorf("comment is empty or too long")
	}
	var path string
	var payload any
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/issues/" + number + "/notes"
		payload = map[string]string{"body": text}
	} else {
		path = "/repos/" + repo.Path + "/issues/" + number + "/comments"
		payload = map[string]string{"body": text}
	}
	if _, err := c.call(ctx, repo, http.MethodPost, path, payload); err != nil {
		return "", err
	}
	return "commented on " + repo.Web + " issue " + number, nil
}

// Runs lists recent GitHub Actions runs or GitLab pipelines.
// ref limits the list to that branch. An empty ref lists the latest runs.
func (c *Client) Runs(ctx context.Context, repo Repo, ref string) (string, error) {
	if err := safeRef(ref); err != nil {
		return "", err
	}
	path := "/repos/" + repo.Path + "/actions/runs?per_page=10"
	filter := "branch"
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/pipelines?per_page=10"
		filter = "ref"
	}
	if ref != "" {
		path += "&" + filter + "=" + url.QueryEscape(ref)
	}
	body, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	if repo.Service == GitLab {
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return "", fmt.Errorf("gitlab pipelines: %w", err)
		}
		return formatRuns(repo, rows, "id", "status", "ref", "web_url"), nil
	}
	var wrap struct {
		WorkflowRuns []map[string]any `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return "", fmt.Errorf("github actions: %w", err)
	}
	return formatRuns(repo, wrap.WorkflowRuns, "id", "status", "head_branch", "html_url"), nil
}

func formatRuns(repo Repo, rows []map[string]any, idKey, statusKey, refKey, urlKey string) string {
	if len(rows) == 0 {
		return repo.Service + " " + repo.Path + " has no pipeline runs"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s pipelines\n", repo.Service, repo.Path)
	for _, row := range rows {
		name := jsonString(row, "name")
		if name == "" {
			name = jsonString(row, "path")
		}
		fmt.Fprintf(&b, "#%s %s %s %s %s\n", jsonNumber(row, idKey), runState(row, statusKey), jsonString(row, refKey), name, jsonString(row, urlKey))
	}
	return b.String()
}

// runState is the result a person can act on. GitHub leaves status at
// "completed" and puts success or failure in conclusion.
func runState(row map[string]any, statusKey string) string {
	state := jsonString(row, statusKey)
	if state == "completed" {
		if conclusion := jsonString(row, "conclusion"); conclusion != "" {
			return conclusion
		}
	}
	return state
}

// Dispatch starts a GitHub workflow file or a GitLab pipeline on ref.
func (c *Client) Dispatch(ctx context.Context, repo Repo, workflow, ref string) (string, error) {
	if err := safeRef(ref); err != nil || ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	if repo.Service == GitLab {
		payload := map[string]string{"ref": ref}
		if _, err := c.call(ctx, repo, http.MethodPost, "/projects/"+url.PathEscape(repo.Path)+"/pipeline", payload); err != nil {
			return "", err
		}
		return "pipeline started on " + repo.Path + " ref " + ref, nil
	}
	workflow = strings.TrimPrefix(strings.TrimSpace(workflow), ".github/workflows/")
	if workflow == "" || workflow == "." || workflow == ".." || len(workflow) > 200 || strings.ContainsAny(workflow, "/\r\n\x00 '\"\\$`;|&<>") {
		return "", fmt.Errorf("workflow file is required, such as ci.yml")
	}
	payload := map[string]string{"ref": ref}
	path := "/repos/" + repo.Path + "/actions/workflows/" + url.PathEscape(workflow) + "/dispatches"
	if _, err := c.call(ctx, repo, http.MethodPost, path, payload); err != nil {
		return "", err
	}
	return "workflow " + workflow + " dispatched on " + repo.Path + " ref " + ref, nil
}

// PullRequest opens a GitHub pull request or a GitLab merge request.
func (c *Client) PullRequest(ctx context.Context, repo Repo, head, base, title, text string) (string, error) {
	if err := safeRef(head); err != nil || head == "" {
		return "", fmt.Errorf("head branch is required")
	}
	if err := safeRef(base); err != nil || base == "" {
		return "", fmt.Errorf("base branch is required")
	}
	base = TrackingBranch(base)
	if base == "" || safeRef(base) != nil {
		return "", fmt.Errorf("base branch is required")
	}
	title = oneLine(title, 200)
	if title == "" {
		return "", fmt.Errorf("pull request title is required")
	}
	text = strings.TrimSpace(text)
	if len(text) > 8000 {
		return "", fmt.Errorf("pull request body is too long")
	}
	if number, link, target, ok, err := c.findOpen(ctx, repo, head, base); err != nil {
		return "", err
	} else if ok {
		if target == "" {
			target = base
		}
		return "already open " + head + " → " + target + " " + link + " #" + number, nil
	}
	var path string
	var payload any
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/merge_requests"
		payload = map[string]string{"source_branch": head, "target_branch": base, "title": title, "description": text}
	} else {
		path = "/repos/" + repo.Path + "/pulls"
		payload = map[string]string{"head": head, "base": base, "title": title, "body": text}
	}
	body, err := c.call(ctx, repo, http.MethodPost, path, payload)
	if err != nil {
		return "", err
	}
	var row map[string]any
	_ = json.Unmarshal(body, &row)
	link := jsonString(row, "html_url")
	if link == "" {
		link = jsonString(row, "web_url")
	}
	if link == "" {
		link = repo.Web
	}
	return "opened " + head + " → " + base + " " + link, nil
}

// findOpen reports an existing pull or merge request for this head.
func (c *Client) findOpen(ctx context.Context, repo Repo, head, base string) (number, link, target string, ok bool, err error) {
	var path string
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/merge_requests?state=opened&source_branch=" + url.QueryEscape(head)
	} else {
		owner := repo.Path
		if i := strings.Index(owner, "/"); i > 0 {
			owner = owner[:i]
		}
		path = "/repos/" + repo.Path + "/pulls?state=open&head=" + url.QueryEscape(owner+":"+head)
	}
	body, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", "", "", false, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", "", "", false, fmt.Errorf("%s open requests: %w", repo.Service, err)
	}
	for _, row := range rows {
		gotHead, gotBase := nestedString(row, "head", "ref"), nestedString(row, "base", "ref")
		if repo.Service == GitLab {
			gotHead, gotBase = jsonString(row, "source_branch"), jsonString(row, "target_branch")
		}
		if gotHead != head || (base != "" && gotBase != "" && gotBase != base) {
			continue
		}
		number = jsonNumber(row, "number")
		if number == "" {
			number = jsonNumber(row, "iid")
		}
		link = jsonString(row, "html_url")
		if link == "" {
			link = jsonString(row, "web_url")
		}
		return number, link, gotBase, true, nil
	}
	return "", "", "", false, nil
}

// Merge lands an open pull request or merge request.
func (c *Client) Merge(ctx context.Context, repo Repo, number string) (string, error) {
	number, err := issueNumber(number)
	if err != nil {
		return "", fmt.Errorf("pull request number is required")
	}
	head, err := c.headSHA(ctx, repo, number)
	if err != nil {
		return "", err
	}
	var path string
	var payload any
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/merge_requests/" + number + "/merge"
		payload = map[string]string{"sha": head}
	} else {
		path = "/repos/" + repo.Path + "/pulls/" + number + "/merge"
		payload = map[string]string{"merge_method": "merge", "sha": head}
	}
	body, err := c.call(ctx, repo, http.MethodPut, path, payload)
	if err != nil {
		return "", err
	}
	var row map[string]any
	_ = json.Unmarshal(body, &row)
	if repo.Service != GitLab {
		merged, _ := row["merged"].(bool)
		if !merged {
			return "", fmt.Errorf("merge was not completed")
		}
	}
	link := jsonString(row, "html_url")
	if link == "" {
		link = jsonString(row, "web_url")
	}
	sha := jsonString(row, "sha")
	if sha == "" {
		sha = jsonString(row, "merge_commit_sha")
	}
	text := "merged #" + number
	if sha != "" {
		text += " " + sha
	}
	if link != "" {
		text += " " + link
	}
	return text, nil
}

// headSHA is the commit the merge must still be on. The forge rejects the
// merge when the branch has moved, instead of landing commits nobody reviewed.
func (c *Client) headSHA(ctx context.Context, repo Repo, number string) (string, error) {
	path := "/repos/" + repo.Path + "/pulls/" + number
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/merge_requests/" + number
	}
	raw, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return "", fmt.Errorf("%s request: %w", repo.Service, err)
	}
	sha := nestedString(row, "head", "sha")
	if repo.Service == GitLab {
		sha = jsonString(row, "sha")
	}
	if !commitSHA(sha) {
		return "", fmt.Errorf("pull request head is not a commit")
	}
	return sha, nil
}

func commitSHA(sha string) bool {
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

// requestMergeWait is how long to pause before reading a GitHub pull
// request again when mergeable is still null. Tests set it to 0.
var requestMergeWait = 800 * time.Millisecond

// Request reads one pull or merge request: whether it can merge, and the
// review notes a person left. Notes are omitted when that call fails.
func (c *Client) Request(ctx context.Context, repo Repo, number string) (string, error) {
	number, err := issueNumber(number)
	if err != nil {
		return "", fmt.Errorf("pull request number is required")
	}
	path := "/repos/" + repo.Path + "/pulls/" + number
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/merge_requests/" + number
	}
	raw, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return "", fmt.Errorf("%s request: %w", repo.Service, err)
	}
	if repo.Service != GitLab {
		if _, known := jsonBool(row, "mergeable"); !known {
			row = c.refreshMergeable(ctx, repo, path, row)
		}
	}
	title := oneLine(jsonString(row, "title"), 180)
	link := jsonString(row, "html_url")
	if link == "" {
		link = jsonString(row, "web_url")
	}
	head, base := nestedString(row, "head", "ref"), nestedString(row, "base", "ref")
	if repo.Service == GitLab {
		head, base = jsonString(row, "source_branch"), jsonString(row, "target_branch")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%s %s\nstate %s\n", number, title, jsonString(row, "state"))
	if draft, ok := jsonBool(row, "draft"); ok && draft {
		b.WriteString("draft\n")
	}
	if head != "" || base != "" {
		fmt.Fprintf(&b, "%s → %s\n", head, base)
	}
	fmt.Fprintf(&b, "%s\n%s\n", requestMergeLine(repo, row), link)
	if notes := c.requestNotes(ctx, repo, number); notes != "" {
		b.WriteString(notes)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c *Client) refreshMergeable(ctx context.Context, repo Repo, path string, row map[string]any) map[string]any {
	timer := time.NewTimer(requestMergeWait)
	select {
	case <-ctx.Done():
		timer.Stop()
		return row
	case <-timer.C:
	}
	raw, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return row
	}
	var again map[string]any
	if json.Unmarshal(raw, &again) != nil || len(again) == 0 {
		return row
	}
	return again
}

func requestMergeLine(repo Repo, row map[string]any) string {
	if repo.Service == GitLab {
		status := jsonString(row, "detailed_merge_status")
		if status == "" {
			status = jsonString(row, "merge_status")
		}
		if status == "" {
			status = "unknown"
		}
		return "merge " + status
	}
	state := jsonString(row, "mergeable_state")
	if state == "" {
		state = "unknown"
	}
	switch merged, ok := jsonBool(row, "mergeable"); {
	case !ok:
		return "mergeable unknown (" + state + ")"
	case merged:
		return "mergeable yes (" + state + ")"
	default:
		return "mergeable no (" + state + ")"
	}
}

func (c *Client) requestNotes(ctx context.Context, repo Repo, number string) string {
	if repo.Service == GitLab {
		return formatNotes("reviews", c.noteRows(ctx, repo, "/projects/"+url.PathEscape(repo.Path)+"/merge_requests/"+number+"/notes?per_page=10&sort=desc"))
	}
	reviews := c.noteRows(ctx, repo, "/repos/"+repo.Path+"/pulls/"+number+"/reviews?per_page=100")
	reverseRows(reviews)
	reviewText := formatNotes("reviews", reviews)
	lines := formatNotes("comments", c.noteRows(ctx, repo, "/repos/"+repo.Path+"/pulls/"+number+"/comments?per_page=5&sort=created&direction=desc"))
	return strings.TrimSpace(reviewText + "\n" + lines)
}

func reverseRows(rows []map[string]any) {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
}

func (c *Client) noteRows(ctx context.Context, repo Repo, path string) []map[string]any {
	raw, err := c.call(ctx, repo, http.MethodGet, path, nil)
	if err != nil {
		return nil
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	return rows
}

func formatNotes(title string, rows []map[string]any) string {
	var b strings.Builder
	n := 0
	for _, row := range rows {
		if sys, _ := row["system"].(bool); sys {
			continue
		}
		text := oneLine(jsonString(row, "body"), 400)
		state := jsonString(row, "state")
		if text == "" && state != "CHANGES_REQUESTED" && state != "APPROVED" {
			continue
		}
		who := nestedString(row, "user", "login")
		if who == "" {
			who = nestedString(row, "author", "username")
		}
		place := notePlace(row)
		if n == 0 {
			fmt.Fprintf(&b, "%s:\n", title)
		}
		n++
		if state != "" && state != "COMMENTED" && state != "commented" {
			fmt.Fprintf(&b, "- %s %s: %s%s\n", who, state, place, text)
		} else {
			fmt.Fprintf(&b, "- %s: %s%s\n", who, place, text)
		}
		if n >= 5 {
			break
		}
	}
	return b.String()
}

func notePlace(row map[string]any) string {
	path := jsonString(row, "path")
	line := jsonNumber(row, "line")
	if line == "" || line == "0" {
		line = jsonNumber(row, "original_line")
	}
	if path == "" {
		if pos, _ := row["position"].(map[string]any); pos != nil {
			path = jsonString(pos, "new_path")
			line = jsonNumber(pos, "new_line")
		}
	}
	if path == "" {
		return ""
	}
	if line != "" && line != "0" {
		return path + ":" + line + " "
	}
	return path + " "
}

func jsonBool(row map[string]any, key string) (bool, bool) {
	value, ok := row[key]
	if !ok || value == nil {
		return false, false
	}
	bit, ok := value.(bool)
	return bit, ok
}

// RunLog summarizes one pipeline and includes the tail of a failed job.
func (c *Client) RunLog(ctx context.Context, repo Repo, id string) (string, error) {
	id, err := issueNumber(id)
	if err != nil {
		return "", fmt.Errorf("run id is required")
	}
	if repo.Service == GitLab {
		return c.gitlabRunLog(ctx, repo, id)
	}
	body, err := c.call(ctx, repo, http.MethodGet, "/repos/"+repo.Path+"/actions/runs/"+id+"/jobs", nil)
	if err != nil {
		return "", err
	}
	var wrap struct {
		Jobs []map[string]any `json:"jobs"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&wrap); err != nil {
		return "", fmt.Errorf("github jobs: %w", err)
	}
	if len(wrap.Jobs) == 0 {
		return "github run " + id + " has no jobs", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "github run %s\n", id)
	logs := 0
	for _, job := range wrap.Jobs {
		fmt.Fprintf(&b, "%s %s\n", jsonString(job, "conclusion"), jsonString(job, "name"))
		steps, _ := job["steps"].([]any)
		failed := jsonString(job, "conclusion") == "failure"
		for _, step := range steps {
			row, _ := step.(map[string]any)
			if jsonString(row, "conclusion") == "failure" {
				failed = true
				fmt.Fprintf(&b, "  failed step: %s\n", jsonString(row, "name"))
			}
		}
		if !failed {
			continue
		}
		if checkID := checkRunID(job); checkID != "" {
			writeAnnotations(&b, c.noteRows(ctx, repo, "/repos/"+repo.Path+"/check-runs/"+checkID+"/annotations?per_page=5"))
		}
		if logs < 2 {
			if tail := c.githubJobLog(ctx, repo, numericID(jsonNumber(job, "id"), 20)); tail != "" {
				logs++
				fmt.Fprintf(&b, "  log tail:\n%s\n", tail)
			}
		}
	}
	return b.String(), nil
}

func checkRunID(job map[string]any) string {
	raw := strings.TrimRight(jsonString(job, "check_run_url"), "/")
	i := strings.LastIndex(raw, "/")
	if i < 0 {
		return ""
	}
	return numericID(raw[i+1:], 20)
}

func numericID(id string, max int) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > max || id[0] == '0' {
		return ""
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return id
}

// githubLogHostAllowed reports whether a redirect may be fetched for a
// job log. Tests replace it. The default refuses addresses and any host
// that is not GitHub's log storage.
var githubLogHostAllowed = githubLogHost

func githubLogHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if net.ParseIP(host) != nil {
		return false
	}
	switch {
	case strings.HasSuffix(host, ".blob.core.windows.net"):
		return true
	case host == "actions.githubusercontent.com" || strings.HasSuffix(host, ".actions.githubusercontent.com"):
		return true
	case host == "objects.githubusercontent.com" || strings.HasSuffix(host, ".objects.githubusercontent.com"):
		return true
	default:
		return false
	}
}

func allowGitHubLogURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	return githubLogHostAllowed(parsed.Hostname())
}

// githubLogClient fetches the signed log URL. It does not send the token
// and does not follow another redirect. Tests replace it.
var githubLogClient = func() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: refuseRedirect}
}

func (c *Client) noFollow() *http.Client {
	base := c.client()
	timeout := base.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: base.Transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// githubJobLog reads the tail of one failed job. The token is sent only to
// this repo's API. The signed URL it returns is fetched without the token
// and is never included in the text.
func (c *Client) githubJobLog(ctx context.Context, repo Repo, jobID string) string {
	if jobID == "" || repo.API == "" || strings.Contains(repo.API, "\n") || strings.Contains(repo.API, "\r") {
		return ""
	}
	endpoint := strings.TrimRight(repo.API, "/") + "/repos/" + repo.Path + "/actions/jobs/" + jobID + "/logs"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "MaClaw")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.noFollow().Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return ""
	}
	loc, locErr := resp.Location()
	if locErr != nil || resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == nil || !allowGitHubLogURL(loc.String()) {
		return ""
	}
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, loc.String(), nil)
	if err != nil {
		return ""
	}
	get.Header.Set("User-Agent", "MaClaw")
	get.Header.Set("Range", "bytes=-8000")
	body, err := jobLogClient().Do(get)
	if body != nil && body.Body != nil {
		defer body.Body.Close()
	}
	if err != nil || body == nil {
		return ""
	}
	if body.StatusCode != http.StatusOK && body.StatusCode != http.StatusPartialContent {
		return ""
	}
	text := scrub(tailText(body.Body, 4000, 1<<20), c.Token)
	if text == "" || strings.Contains(text, "\x00") {
		return ""
	}
	return text
}

// jobLogClient fetches the signed URL and at most one further redirect.
// The token is not on this client. A second redirect, or a host outside
// the log stores, stops the fetch.
func jobLogClient() *http.Client {
	base := githubLogClient()
	timeout := 20 * time.Second
	if base != nil && base.Timeout > 0 {
		timeout = base.Timeout
	}
	var transport http.RoundTripper
	if base != nil {
		transport = base.Transport
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 1 || req == nil || req.URL == nil || !allowGitHubLogURL(req.URL.String()) {
				return fmt.Errorf("redirect refused")
			}
			req.Header.Del("Authorization")
			req.Header.Del("Private-Token")
			return nil
		},
	}
}

func tailText(r io.Reader, keep, max int) string {
	if keep < 1 {
		keep = 4000
	}
	if max < keep {
		max = keep
	}
	buf := make([]byte, 0, keep)
	tmp := make([]byte, 8192)
	read := 0
	for read < max {
		n, err := r.Read(tmp)
		if n > 0 {
			read += n
			buf = append(buf, tmp[:n]...)
			if len(buf) > keep {
				buf = append([]byte(nil), buf[len(buf)-keep:]...)
			}
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(string(buf))
}

func writeAnnotations(b *strings.Builder, rows []map[string]any) {
	n := 0
	for _, row := range rows {
		msg := oneLine(jsonString(row, "message"), 300)
		if msg == "" {
			continue
		}
		place := jsonString(row, "path")
		line := jsonNumber(row, "start_line")
		if place != "" && line != "" && line != "0" {
			place += ":" + line
		}
		if place != "" {
			place += " "
		}
		fmt.Fprintf(b, "  %s%s\n", place, msg)
		n++
		if n >= 5 {
			break
		}
	}
}

func (c *Client) gitlabRunLog(ctx context.Context, repo Repo, id string) (string, error) {
	body, err := c.call(ctx, repo, http.MethodGet, "/projects/"+url.PathEscape(repo.Path)+"/pipelines/"+id+"/jobs", nil)
	if err != nil {
		return "", err
	}
	var jobs []map[string]any
	if err := json.Unmarshal(body, &jobs); err != nil {
		return "", fmt.Errorf("gitlab jobs: %w", err)
	}
	if len(jobs) == 0 {
		return "gitlab pipeline " + id + " has no jobs", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "gitlab pipeline %s\n", id)
	for _, job := range jobs {
		fmt.Fprintf(&b, "%s %s\n", jsonString(job, "status"), jsonString(job, "name"))
		if jsonString(job, "status") != "failed" {
			continue
		}
		jobID := jsonNumber(job, "id")
		trace, traceErr := c.call(ctx, repo, http.MethodGet, "/projects/"+url.PathEscape(repo.Path)+"/jobs/"+jobID+"/trace", nil)
		if traceErr != nil {
			fmt.Fprintf(&b, "  log: %s\n", traceErr.Error())
			continue
		}
		text := string(trace)
		if len(text) > 1500 {
			text = text[len(text)-1500:]
		}
		fmt.Fprintf(&b, "  log tail:\n%s\n", strings.TrimSpace(text))
	}
	return b.String(), nil
}

// Ship is a release that was created. Upload is the GitHub asset endpoint
// with the {?name,label} suffix removed. GitLab leaves it empty.
type Ship struct {
	Summary string
	Upload  string
}

// Release creates a release and returns the summary line.
func (c *Client) Release(ctx context.Context, repo Repo, tag, name, notes, ref string) (string, error) {
	ship, err := c.CreateRelease(ctx, repo, tag, name, notes, ref, "", "")
	if err != nil {
		return "", err
	}
	return ship.Summary, nil
}

// CreateRelease creates a release. assetURL is a GitLab package link.
func (c *Client) CreateRelease(ctx context.Context, repo Repo, tag, name, notes, ref, assetURL, assetName string) (Ship, error) {
	if err := safeRef(tag); err != nil || tag == "" {
		return Ship{}, fmt.Errorf("tag is required")
	}
	if err := safeRef(ref); err != nil {
		return Ship{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = tag
	}
	if len(name) > 200 || strings.ContainsAny(name, "\r\n\x00") {
		return Ship{}, fmt.Errorf("release name is invalid")
	}
	notes = strings.TrimSpace(notes)
	if len(notes) > 8000 {
		return Ship{}, fmt.Errorf("release notes are too long")
	}
	var path string
	var payload any
	if repo.Service == GitLab {
		path = "/projects/" + url.PathEscape(repo.Path) + "/releases"
		item := map[string]any{"tag_name": tag, "name": name, "description": notes}
		if ref != "" {
			item["ref"] = ref
		}
		if assetURL != "" && assetName != "" {
			item["assets"] = map[string]any{"links": []map[string]string{{"name": assetName, "url": assetURL}}}
		}
		payload = item
	} else {
		path = "/repos/" + repo.Path + "/releases"
		item := map[string]string{"tag_name": tag, "name": name, "body": notes}
		if ref != "" {
			item["target_commitish"] = ref
		}
		payload = item
	}
	body, err := c.call(ctx, repo, http.MethodPost, path, payload)
	if err != nil {
		return Ship{}, err
	}
	var row map[string]any
	_ = json.Unmarshal(body, &row)
	link := jsonString(row, "html_url")
	if link == "" {
		link = jsonString(row, "web_url")
	}
	if link == "" {
		link = repo.Web + "/releases/tag/" + tag
	}
	upload := jsonString(row, "upload_url")
	if i := strings.Index(upload, "{"); i >= 0 {
		upload = upload[:i]
	}
	summary := "released " + tag + " " + link
	if assetURL == "" {
		summary += "\nThe package file is still in the checkout. Pass asset= to attach it, or deliver it in this chat."
	}
	return Ship{Summary: summary, Upload: upload}, nil
}

func (c *Client) call(ctx context.Context, repo Repo, method, path string, payload any) ([]byte, error) {
	if strings.TrimSpace(c.Token) == "" {
		return nil, fmt.Errorf("missing %s", TokenEnv(repo.Service))
	}
	if repo.API == "" || strings.Contains(path, "://") {
		return nil, fmt.Errorf("api path is invalid")
	}
	endpoint := strings.TrimRight(repo.API, "/") + path
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw")
	req.Header.Set("Accept", "application/json")
	if repo.Service == GitHub || c.Bearer {
		if repo.Service == GitHub {
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		req.Header.Set("PRIVATE-TOKEN", c.Token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", repo.Service, scrub(err.Error(), c.Token))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, fmt.Errorf("%s: %s", repo.Service, scrub(err.Error(), c.Token))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s HTTP %d: %s", repo.Service, resp.StatusCode, scrub(shortBody(raw), c.Token))
	}
	return raw, nil
}

func shortBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	var wrap map[string]any
	if json.Unmarshal(raw, &wrap) == nil {
		if msg := jsonString(wrap, "message"); msg != "" {
			text = msg
		}
	}
	return oneLine(text, 300)
}

func scrub(text, token string) string {
	if token != "" {
		text = strings.ReplaceAll(text, token, "[set]")
	}
	return text
}

func normalizeState(service, state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "open", "opened":
		if service == GitLab {
			return "opened"
		}
		return "open"
	case "closed", "close":
		return "closed"
	case "all":
		return "all"
	default:
		if service == GitLab {
			return "opened"
		}
		return "open"
	}
}

func issueNumber(number string) (string, error) {
	number = strings.TrimSpace(number)
	if number == "" || len(number) > 12 {
		return "", fmt.Errorf("issue number is required")
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("issue number is required")
		}
	}
	if number[0] == '0' {
		return "", fmt.Errorf("issue number is required")
	}
	return number, nil
}

func jsonString(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return strings.TrimSpace(value)
}

func jsonNumber(row map[string]any, key string) string {
	switch value := row[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return fmt.Sprintf("%.0f", value)
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func keepLines(text string, max int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if i := strings.LastIndex(text[:cut], "\n"); i > cut/2 {
		cut = i
	}
	return strings.TrimSpace(text[:cut]) + "…"
}

func oneLine(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
