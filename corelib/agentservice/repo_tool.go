package agentservice

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// RepoCall is one desktop-bot request to GitHub or GitLab. Args never carry
// a token. MaClawSrv reads GITHUB_TOKEN, GITLAB_TOKEN, and GITLAB_HOST from
// the bot secret store.
type RepoCall struct {
	TenantID   string
	UserID     string
	BotID      string
	RunID      string
	SessionID  string
	InstanceID string
	Phase      string
	UserText   string
	Args       map[string]interface{}
}

// DesktopRepo runs the repo tool. MaClawSrv installs it. A nil hook means
// this process is not the desktop-bot service.
var DesktopRepo func(ctx context.Context, call RepoCall) (string, error)

func (c *coreAgentCallbacks) executeRepo(args map[string]interface{}) string {
	if c == nil || !c.botUsesContainer() {
		return "Error: repo is the desktop bot's GitHub and GitLab tool"
	}
	if DesktopRepo == nil {
		return "Error: repo is not connected on this service"
	}
	tenant, user, bound := c.desktopPersonAccount()
	if !bound {
		return "Error: this bot's desktop is the person's cloud desktop, and that account is not bound"
	}
	phase := ""
	if c.messageMetadata != nil {
		phase = c.messageMetadata["bot_phase"]
	}
	text, err := DesktopRepo(c.parentContext(), RepoCall{
		TenantID:   tenant,
		UserID:     user,
		BotID:      c.botID,
		RunID:      c.runID,
		SessionID:  strings.TrimSpace(c.session.ID),
		InstanceID: strings.TrimSpace(c.instance.ID),
		Phase:      phase,
		UserText:   c.userText,
		Args:       args,
	})
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if text != "" {
			msg = strings.TrimSpace(text)
		}
		if !strings.HasPrefix(strings.ToLower(msg), "error:") {
			msg = "Error: " + msg
		}
		return msg
	}
	return text
}

func (c *coreAgentCallbacks) withRepoTool(tools []map[string]interface{}) []map[string]interface{} {
	if c == nil || DesktopRepo == nil || !c.botUsesContainer() {
		return tools
	}
	for _, tool := range tools {
		if nameOf(tool) == "repo" {
			return tools
		}
	}
	out := make([]map[string]interface{}, len(tools)+1)
	copy(out, tools)
	out[len(tools)] = repoToolDefinition()
	return out
}

func nameOf(tool map[string]interface{}) string {
	return tooldef.Name(tool)
}

func repoToolDefinition() map[string]interface{} {
	return functionToolDefinition("repo",
		"GitHub and GitLab for this desktop bot. Use it when the person asks you to develop, fix a bug, or ship. If it is not authorized, action=connect and send the person that link. They approve organizations and scopes on the provider page. Then action=repos, they choose repositories, and action=grant saves that choice. Never ask for or print a token. A finished authorization is kept for this bot across restarts. A self-hosted GitLab uses the saved GITLAB_HOST. Read: whoami, issues, issue, status, diff, log, runs, run_log, request. Work: clone, pull, branch, commit, comment. pr pushes the current branch and opens the pull or merge request, or keeps the one already open for that branch. runs with ref shows that branch's pipelines. request reads that pull or merge request: whether it can merge, and the review comments. sync fetches the base branch and merges it into the current branch without pushing. merge lands the open request. release with asset attaches a built file up to 64MB. push, pr, merge, dispatch, and release ask the person before they run. Reproduce and edit in the checkout with the desktop tool, then run the project's own tests. Do not browse the forge yourself. To keep watching issues, call bot_schedule.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "enum": []string{"connect", "repos", "grant", "whoami", "issues", "issue", "comment", "clone", "pull", "status", "diff", "log", "branch", "commit", "push", "sync", "pr", "request", "merge", "runs", "run_log", "dispatch", "release"}, "description": "what to do"},
				"host":     map[string]interface{}{"type": "string", "enum": []string{"github", "gitlab"}, "description": "github or gitlab. Required when repo is owner/name rather than a URL."},
				"repo":     map[string]interface{}{"type": "string", "description": "owner/name, group/sub/name, or an https URL. No token in the URL."},
				"path":     map[string]interface{}{"type": "string", "description": "Checkout directory inside this desktop. Empty on clone uses this run's project folder."},
				"ref":      map[string]interface{}{"type": "string", "description": "Branch or tag. branch creates it. pr switches to it before pushing. runs limits pipelines to it. clone and dispatch use it too. release without a ref tags the current checkout commit."},
				"base":     map[string]interface{}{"type": "string", "description": "pr: branch to merge into. sync: branch to fetch and merge into the current one. Empty uses the remote default."},
				"number":   map[string]interface{}{"type": "string", "description": "Issue number for issue and comment, the pull or merge request number for request and merge, or the pipeline id for run_log."},
				"body":     map[string]interface{}{"type": "string", "description": "comment text, or release notes when action is release."},
				"message":  map[string]interface{}{"type": "string", "description": "commit message. commit stops when a secret file such as .env or a private key would be included. .env.example is allowed."},
				"workflow": map[string]interface{}{"type": "string", "description": "GitHub workflow file, such as ci.yml. GitLab dispatch ignores it and starts a pipeline on ref."},
				"tag":      map[string]interface{}{"type": "string", "description": "Release tag, such as v1.2.0."},
				"name":     map[string]interface{}{"type": "string", "description": "Release title, or the pull request title. Defaults to the tag, or to message for a pull request."},
				"asset":    map[string]interface{}{"type": "string", "description": "release: built package file inside the checkout, up to 64MB. It is attached to the release."},
				"state":    map[string]interface{}{"type": "string", "enum": []string{"open", "closed", "all"}, "description": "issues filter. Default open."},
				"scopes":   map[string]interface{}{"type": "string", "description": "connect: space-separated scopes the person will approve. Empty uses the development set. GitHub: read:user, repo, public_repo, workflow, read:org. GitLab: read_user, api, read_api, read_repository, write_repository."},
				"repos":    map[string]interface{}{"type": "string", "description": "grant: owner/name list separated by commas, or * for every repository this authorization can see."},
			},
			"required": []string{"action"},
		},
	)
}
