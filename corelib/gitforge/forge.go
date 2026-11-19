// Package gitforge is the GitHub and GitLab surface a desktop bot uses to
// develop: read issues, clone, and ship. Tokens stay in the caller's
// environment. This package never puts a credential in a URL or a command.
package gitforge

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	GitHub = "github"
	GitLab = "gitlab"
)

// Repo is one project on GitHub or GitLab. Clone has no userinfo.
type Repo struct {
	Service string
	Web     string
	API     string
	Clone   string
	Path    string
	Name    string
}

// Parse reads owner/name, group/sub/name, or an https URL. host is github
// or gitlab when the string has no host. gitlabHost is the saved
// self-hosted origin (https://gitlab.example.com); any other host is refused
// so a token is not sent there.
func Parse(repo, host, gitlabHost string) (Repo, error) {
	repo = strings.TrimSpace(repo)
	host = strings.ToLower(strings.TrimSpace(host))
	if repo == "" {
		return Repo{}, fmt.Errorf("repo is empty")
	}
	if strings.ContainsAny(repo, "\r\n\x00 ") || strings.Contains(repo, "@") {
		return Repo{}, fmt.Errorf("repo must be owner/name or an https URL without a token")
	}
	saved, err := parseGitLabHost(gitlabHost)
	if err != nil {
		return Repo{}, err
	}
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return parseURL(repo, saved)
	}
	switch host {
	case "", GitHub:
		return repoOn("https://github.com", "https://api.github.com", GitHub, repo, true)
	case GitLab:
		base := "https://gitlab.com"
		api := "https://gitlab.com/api/v4"
		if saved != nil {
			base = saved.base
			api = saved.api
		}
		return repoOn(base, api, GitLab, repo, false)
	default:
		return Repo{}, fmt.Errorf("host must be github or gitlab")
	}
}

type savedHost struct {
	base string
	api  string
	host string
}

func parseGitLabHost(raw string) (*savedHost, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("GITLAB_HOST must be an https origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("GITLAB_HOST must be an origin, not a project path")
	}
	host := strings.ToLower(parsed.Hostname())
	base := "https://" + host
	if port := parsed.Port(); port != "" {
		base += ":" + port
		host += ":" + port
	}
	return &savedHost{base: base, api: base + "/api/v4", host: host}, nil
}

func parseURL(raw string, saved *savedHost) (Repo, error) {
	if strings.HasPrefix(raw, "git@") {
		return Repo{}, fmt.Errorf("use an https URL, not ssh")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return Repo{}, fmt.Errorf("repo URL must be https and must not carry a token")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host += ":" + port
	}
	path := strings.Trim(parsed.EscapedPath(), "/")
	switch host {
	case "github.com", "www.github.com":
		return repoOn("https://github.com", "https://api.github.com", GitHub, path, true)
	case "gitlab.com", "www.gitlab.com":
		return repoOn("https://gitlab.com", "https://gitlab.com/api/v4", GitLab, path, false)
	default:
		if saved != nil && host == saved.host {
			return repoOn(saved.base, saved.api, GitLab, path, false)
		}
		return Repo{}, fmt.Errorf("repo host is not github.com, gitlab.com, or the saved GITLAB_HOST")
	}
}

func repoOn(webRoot, apiRoot, service, path string, github bool) (Repo, error) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" || strings.Contains(path, "..") || strings.ContainsAny(path, "\\?#") {
		return Repo{}, fmt.Errorf("repo path is invalid")
	}
	parts := strings.Split(path, "/")
	if github {
		if len(parts) != 2 || !gitName(parts[0]) || !gitName(parts[1]) {
			return Repo{}, fmt.Errorf("github repo must be owner/name")
		}
	} else if len(parts) < 2 {
		return Repo{}, fmt.Errorf("gitlab repo must be group/name")
	} else {
		for _, part := range parts {
			if !gitName(part) {
				return Repo{}, fmt.Errorf("repo path is invalid")
			}
		}
	}
	name := parts[len(parts)-1]
	return Repo{
		Service: service,
		Web:     webRoot + "/" + path,
		API:     apiRoot,
		Clone:   webRoot + "/" + path + ".git",
		Path:    path,
		Name:    name,
	}, nil
}

func gitName(part string) bool {
	if part == "" || part == "." || part == ".." || len(part) > 200 {
		return false
	}
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// TokenEnv is the secret name for this service.
func TokenEnv(service string) string {
	if service == GitLab {
		return "GITLAB_TOKEN"
	}
	return "GITHUB_TOKEN"
}

// AskUser is the git username that goes with a token. It is not the token.
func AskUser(service string) string {
	if service == GitLab {
		return "oauth2"
	}
	return "x-access-token"
}

// Script is a bash script that runs one git operation. tokenEnv is the
// variable name the askpass helper reads. The secret value is not in the script.
func Script(op string, repo Repo, dest, ref, message, tokenEnv string) (string, error) {
	op = strings.ToLower(strings.TrimSpace(op))
	if err := safeRef(ref); err != nil {
		return "", err
	}
	if strings.TrimSpace(dest) == "" || strings.ContainsAny(dest, "\r\n\x00") {
		return "", fmt.Errorf("path is empty")
	}
	qdest := shellQuote(dest)
	switch op {
	case "status":
		return "git -C " + qdest + " status --short --branch", nil
	case "commit":
		message = strings.TrimSpace(message)
		if message == "" || strings.ContainsAny(message, "\r\n\x00") || len(message) > 2000 {
			return "", fmt.Errorf("commit message is empty or invalid")
		}
		return "git -C " + qdest + " add -A || exit $?\n" + commitSecretGuard(qdest) + "git -C " + qdest + " -c user.email=maclaw-bot@local -c user.name=MaClaw commit -m " + shellQuote(message), nil
	case "clone":
		if repo.Clone == "" {
			return "", fmt.Errorf("repo is empty")
		}
		inner := "mkdir -p " + shellQuote(parentDir(dest)) + " && git clone"
		if ref != "" {
			inner += " --branch " + shellQuote(ref)
		}
		inner += " " + shellQuote(repo.Clone) + " " + qdest
		return askpass(repo.Service, tokenEnv, inner, "")
	case "pull":
		return askpass(repo.Service, tokenEnv, "git -C "+qdest+" pull --ff-only", "")
	case "push":
		// The confirmed command names this branch. A bare git push follows
		// the current upstream, which can be missing or a different branch.
		if ref == "" || ref == "HEAD" {
			return "", fmt.Errorf("branch name is required")
		}
		return askpass(repo.Service, tokenEnv, "git -C "+qdest+" push -u origin "+shellQuote(ref), "")
	case "pushhead":
		return askpass(repo.Service, tokenEnv, "git -C "+qdest+" push -u origin HEAD", "")
	case "sync":
		ref = TrackingBranch(ref)
		if ref == "" {
			return "", fmt.Errorf("base branch is required")
		}
		// A bare ref only updates FETCH_HEAD. The tracking ref has to be
		// named, or the merge below still sees the previous base.
		spec := "+refs/heads/" + ref + ":refs/remotes/origin/" + ref
		inner := "git -C " + qdest + " fetch origin " + shellQuote(spec) + " && git -C " + qdest + " -c user.email=maclaw-bot@local -c user.name=MaClaw merge --no-edit " + shellQuote("origin/"+ref)
		return askpass(repo.Service, tokenEnv, inner, "git -C "+qdest+" status --short")
	case "branch":
		if ref == "" {
			return "", fmt.Errorf("branch name is required")
		}
		quoted := shellQuote(ref)
		return "git -C " + qdest + " switch -c " + quoted + " || git -C " + qdest + " switch " + quoted, nil
	case "switch":
		if ref == "" {
			return "", fmt.Errorf("branch name is required")
		}
		return "git -C " + qdest + " switch " + shellQuote(ref), nil
	case "diff":
		return "git -C " + qdest + " diff --stat && git -C " + qdest + " diff", nil
	case "log":
		return "git -C " + qdest + " log -n 20 --oneline", nil
	default:
		return "", fmt.Errorf("unknown git operation %s", op)
	}
}

// commitSecretGuard drops secret-looking paths from the index and stops the
// commit. Example env files stay eligible. The checkout's other files stay
// staged so a later commit can proceed after those paths are gone.
func commitSecretGuard(qdest string) string {
	return `hit=""
while IFS= read -r -d '' name; do
  base="${name##*/}"
  low="${base,,}"
  pathlow="${name,,}"
  case "$low" in
    .env.example|.env.sample|.env.template|.env.dist) continue ;;
  esac
  block=0
  case "$low" in
    .env|.env.*|.maclaw-secret-*|id_rsa|id_dsa|id_ecdsa|id_ed25519|credentials|credentials.json|.npmrc|.pypirc|.netrc|keystore.jks|keystore.p12) block=1 ;;
    *.pem|*.key|*.p12|*.pfx|*.jks) block=1 ;;
  esac
  case "$pathlow" in
    .ssh/*|*/.ssh/*) block=1 ;;
  esac
  if [ "$block" = 1 ]; then
    git -C ` + qdest + ` reset -q -- "$name" || exit $?
    hit="${hit}
${name}"
  fi
done < <(git -C ` + qdest + ` diff --cached --name-only -z)
if [ -n "$hit" ]; then
  printf '%s\n' "refusing to commit secret files:${hit}"
  exit 1
fi
`
}

func askpass(service, tokenEnv, inner, onFail string) (string, error) {
	if tokenEnv != "GITHUB_TOKEN" && tokenEnv != "GITLAB_TOKEN" {
		return "", fmt.Errorf("token env is not a forge secret")
	}
	user := AskUser(service)
	helper := "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s\\n' " + shellQuote(user) + " ;;\n*) printf '%s\\n' \"$" + tokenEnv + "\" ;;\nesac\n"
	script := "ask=$(mktemp) && cat > \"$ask\" <<'MACLAW_ASK'\n" + helper + "MACLAW_ASK\nchmod 700 \"$ask\"\nGIT_ASKPASS=\"$ask\" GIT_TERMINAL_PROMPT=0 " + inner + "\nst=$?\nrm -f \"$ask\"\n"
	if onFail != "" {
		script += "if [ \"$st\" -ne 0 ]; then " + onFail + "; fi\n"
	}
	return script + "exit $st\n", nil
}

func parentDir(dest string) string {
	i := strings.LastIndex(dest, "/")
	if i <= 0 {
		return "/"
	}
	return dest[:i]
}

// TrackingBranch turns origin/main, refs/heads/main, and
// refs/remotes/origin/main into main. A branch whose own name starts with
// origin/ is left intact once those remote prefixes are gone.
func TrackingBranch(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "refs/remotes/origin/"):
		ref = strings.TrimPrefix(ref, "refs/remotes/origin/")
	case strings.HasPrefix(ref, "refs/heads/"):
		ref = strings.TrimPrefix(ref, "refs/heads/")
	default:
		ref = strings.TrimPrefix(ref, "origin/")
	}
	return ref
}

func safeRef(ref string) error {
	if ref == "" {
		return nil
	}
	if len(ref) > 200 || strings.Contains(ref, "..") || strings.ContainsAny(ref, "\r\n\x00 '\"\\$`;|&<>:") {
		return fmt.Errorf("ref is invalid")
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// PackageURL is the GitLab generic-package address for one release file.
func PackageURL(repo Repo, tag, name string) (string, error) {
	if repo.Service != GitLab || repo.API == "" {
		return "", fmt.Errorf("package upload is for gitlab")
	}
	if err := safeRef(tag); err != nil || tag == "" {
		return "", fmt.Errorf("tag is required")
	}
	if err := safeFileName(name); err != nil {
		return "", err
	}
	return strings.TrimRight(repo.API, "/") + "/projects/" + url.PathEscape(repo.Path) + "/packages/generic/release/" + url.PathEscape(tag) + "/" + url.PathEscape(name), nil
}

// UploadScript sends one file with curl. The token stays in a short-lived
// config file written from the environment. It is not part of the command.
// Files larger than 64MB are refused.
func UploadScript(service, tokenEnv, file, uploadURL, name string, bearer bool) (string, error) {
	if err := safeFileName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(file) == "" || strings.ContainsAny(file, "\r\n\x00") {
		return "", fmt.Errorf("asset path is empty")
	}
	if tokenEnv != "GITHUB_TOKEN" && tokenEnv != "GITLAB_TOKEN" {
		return "", fmt.Errorf("token env is not a forge secret")
	}
	header := "Authorization: Bearer ${" + tokenEnv + "}"
	method := "POST"
	if service == GitLab {
		method = "PUT"
		if !bearer {
			header = "PRIVATE-TOKEN: ${" + tokenEnv + "}"
		}
	}
	return "bytes=$(stat -c %s " + shellQuote(file) + ")\n" +
		"if [ \"$bytes\" -gt 67108864 ]; then echo 'package is larger than 64MB'; exit 1; fi\n" +
		"cfg=$(mktemp)\n" +
		"cat > \"$cfg\" <<EOF\nheader = \"" + header + "\"\nEOF\n" +
		"chmod 600 \"$cfg\"\n" +
		"curl --silent --show-error --fail --config \"$cfg\" -X " + method + " --header " + shellQuote("Content-Type: application/octet-stream") + " --data-binary @" + shellQuote(file) + " " + shellQuote(uploadURL) + "\n" +
		"st=$?\nrm -f \"$cfg\"\nexit $st\n", nil
}

func safeFileName(name string) error {
	if !gitName(name) || strings.Contains(name, "/") {
		return fmt.Errorf("asset name is invalid")
	}
	return nil
}

// AllowedUpload accepts only this forge's own upload host.
func AllowedUpload(repo Repo, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return fmt.Errorf("upload url is invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	if repo.Service == GitHub && host == "uploads.github.com" {
		return nil
	}
	api, err := url.Parse(repo.API)
	if err != nil || !strings.EqualFold(host, api.Hostname()) {
		return fmt.Errorf("upload host is not this forge")
	}
	return nil
}
