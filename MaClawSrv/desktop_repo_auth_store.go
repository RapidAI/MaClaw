package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// desktopRepoAuthDir is the 0700 directory that keeps a bot's forge token
// and repository choice. Empty disables the disk copy; memory still works.
// main sets it from the service data root. Tests point it at a temp dir.
var desktopRepoAuthDir string

var repoAuthLoaded sync.Map

type repoAuthGrantFile struct {
	Pick  bool     `json:"pick,omitempty"`
	All   bool     `json:"all,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

type repoAuthFile struct {
	Secrets      map[string]string            `json:"secrets,omitempty"`
	GitLabBearer bool                         `json:"gitlab_bearer,omitempty"`
	Grants       map[string]repoAuthGrantFile `json:"grants,omitempty"`
}

func repoAuthSecret(name string) bool {
	switch name {
	case "GITHUB_TOKEN", "GITLAB_TOKEN", "GITLAB_HOST":
		return true
	default:
		return false
	}
}

func repoAuthTouched(values map[string]string) bool {
	for name := range values {
		if repoAuthSecret(name) {
			return true
		}
	}
	return false
}

func safeRepoAuthSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 80 || value == "." || value == ".." {
		return false
	}
	if strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

func repoAuthKey(tenant, bot string) string {
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(bot)
}

func repoAuthPath(tenant, bot string) (string, error) {
	root := strings.TrimSpace(desktopRepoAuthDir)
	if root == "" {
		return "", os.ErrNotExist
	}
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		tenant = "_"
	}
	bot = strings.TrimSpace(bot)
	if !safeRepoAuthSegment(tenant) || !safeRepoAuthSegment(bot) {
		return "", os.ErrInvalid
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(abs, tenant, bot+".json")
	rel, err := filepath.Rel(abs, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", os.ErrInvalid
	}
	return path, nil
}

func desktopNoteRepoTenant(botID, tenant string) {
	turn := desktopBotTurnOf(botID)
	tenant = strings.TrimSpace(tenant)
	if turn == nil || tenant == "" {
		return
	}
	turn.mu.Lock()
	if turn.tenant == "" {
		turn.tenant = tenant
	}
	turn.mu.Unlock()
}

// desktopEnsureRepoAuth loads the saved authorization once per process.
// A value already in memory is left as it is.
func desktopEnsureRepoAuth(tenant, bot string) {
	bot = strings.TrimSpace(bot)
	if bot == "" || strings.TrimSpace(desktopRepoAuthDir) == "" {
		return
	}
	key := repoAuthKey(tenant, bot)
	if _, ok := repoAuthLoaded.Load(key); ok {
		return
	}
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return
	}
	path, err := repoAuthPath(tenant, bot)
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.tenant == "" && strings.TrimSpace(tenant) != "" && safeRepoAuthSegment(tenant) {
		turn.tenant = strings.TrimSpace(tenant)
	}
	repoAuthLoaded.Store(key, true)
	if err != nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var file repoAuthFile
	if json.Unmarshal(raw, &file) != nil {
		return
	}
	if turn.secrets == nil {
		turn.secrets = map[string]string{}
	}
	for name, value := range file.Secrets {
		if repoAuthSecret(name) && strings.TrimSpace(value) != "" && turn.secrets[name] == "" {
			turn.secrets[name] = value
		}
	}
	if file.GitLabBearer {
		turn.gitlabBearer = true
	}
	for service, grant := range file.Grants {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if turn.repoPick[service] || turn.repoAll[service] || len(turn.repoPaths[service]) > 0 {
			continue
		}
		if turn.repoPick == nil {
			turn.repoPick = map[string]bool{}
		}
		if turn.repoAll == nil {
			turn.repoAll = map[string]bool{}
		}
		if turn.repoPaths == nil {
			turn.repoPaths = map[string]map[string]bool{}
		}
		turn.repoPick[service] = grant.Pick
		turn.repoAll[service] = grant.All
		if len(grant.Paths) > 0 && !grant.All {
			paths := map[string]bool{}
			for _, item := range grant.Paths {
				item = strings.ToLower(strings.TrimSpace(item))
				if item != "" {
					paths[item] = true
				}
			}
			turn.repoPaths[service] = paths
		}
	}
}

// desktopWriteRepoAuth writes the forge keys. The caller holds turn.mu.
// Other secrets in the same map are not written. Errors are dropped so a
// full disk cannot put a token into a chat message.
func desktopWriteRepoAuth(bot string, turn *desktopBotTurn) {
	if turn == nil {
		return
	}
	path, err := repoAuthPath(turn.tenant, bot)
	if err != nil {
		return
	}
	file := repoAuthFile{Secrets: map[string]string{}, Grants: map[string]repoAuthGrantFile{}}
	for name, value := range turn.secrets {
		if repoAuthSecret(name) && strings.TrimSpace(value) != "" {
			file.Secrets[name] = value
		}
	}
	file.GitLabBearer = turn.gitlabBearer
	seen := map[string]bool{}
	for service := range turn.repoPick {
		seen[service] = true
	}
	for service := range turn.repoAll {
		seen[service] = true
	}
	for service := range turn.repoPaths {
		seen[service] = true
	}
	for service := range seen {
		grant := repoAuthGrantFile{Pick: turn.repoPick[service], All: turn.repoAll[service]}
		for item := range turn.repoPaths[service] {
			grant.Paths = append(grant.Paths, item)
		}
		file.Grants[service] = grant
	}
	if len(file.Secrets) == 0 && !file.GitLabBearer && len(file.Grants) == 0 {
		return
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func desktopForgetRepoAuth(tenant, bot string) {
	desktopBotTurns.Delete(strings.TrimSpace(bot))
	repoAuthLoaded.Delete(repoAuthKey(tenant, bot))
}
