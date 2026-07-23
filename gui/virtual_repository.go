package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"
)

const (
	virtualRepositoryManifestVersion  = 1
	virtualRepositoryDirName          = ".vrepo"
	virtualRepositoryManifestName     = "manifest.json"
	virtualRepositoryManifestMaxBytes = 4 * 1024 * 1024
	virtualRepositoryNameMaxLength    = 256
	virtualRepositoryFieldMaxLength   = 4096
	virtualRepositoryNodeMaxCount     = 10000
	virtualRepositoryStatsMaxEntries  = 1000000
)

var (
	virtualRepositoryStateMu      sync.Mutex
	virtualRepositoryKnownHostsMu sync.Mutex
	virtualRepositoryRemoteSaveMu sync.Mutex
)

type VirtualRepository struct {
	Version   int                      `json:"version"`
	ID        string                   `json:"id"`
	Name      string                   `json:"name"`
	RootPath  string                   `json:"root_path,omitempty"`
	Remote    *VirtualRepositoryRemote `json:"remote,omitempty"`
	Nodes     []VirtualRepositoryNode  `json:"nodes"`
	CreatedAt time.Time                `json:"created_at"`
	UpdatedAt time.Time                `json:"updated_at"`
}

type VirtualRepositoryRemote struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	User string `json:"user"`
}

type VirtualRepositoryNode struct {
	ID         string                    `json:"id"`
	ParentID   string                    `json:"parent_id,omitempty"`
	Name       string                    `json:"name"`
	Order      int                       `json:"order"`
	Repository *VirtualRepositoryBinding `json:"repository,omitempty"`
}

type VirtualRepositoryBinding struct {
	Kind         string `json:"kind"`
	RelativePath string `json:"relative_path"`
	Description  string `json:"description,omitempty"`
	RemoteURL    string `json:"remote_url,omitempty"`
	RefType      string `json:"ref_type,omitempty"`
	RefName      string `json:"ref_name,omitempty"`
	Enabled      bool   `json:"enabled"`
}

type virtualRepositoryIndex struct {
	Version int                           `json:"version"`
	Items   []virtualRepositoryIndexEntry `json:"items"`
}

type virtualRepositoryIndexEntry struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	RootPath   string                   `json:"root_path"`
	Remote     *VirtualRepositoryRemote `json:"remote,omitempty"`
	LastOpened time.Time                `json:"last_opened"`
}

func cloneVirtualRepositoryRemote(remote *VirtualRepositoryRemote) *VirtualRepositoryRemote {
	if remote == nil {
		return nil
	}
	copy := *remote
	return &copy
}

type virtualRepositoryLocalSettings struct {
	Version       int    `json:"version"`
	SVNExecutable string `json:"svn_executable,omitempty"`
}

func loadVirtualRepositoryLocalSettings(path string) (virtualRepositoryLocalSettings, error) {
	settings := virtualRepositoryLocalSettings{Version: 1}
	if err := readJSONFile(path, &settings); err != nil {
		return settings, err
	}
	if settings.Version != 1 {
		return settings, fmt.Errorf("unsupported virtual repository local settings version %d", settings.Version)
	}
	if len(settings.SVNExecutable) > virtualRepositoryFieldMaxLength || containsControlCharacter(settings.SVNExecutable) {
		return settings, errors.New("virtual repository SVN executable setting is invalid")
	}
	return settings, nil
}

type VCSClientStatus struct {
	Kind       string `json:"kind"`
	Available  bool   `json:"available"`
	Executable string `json:"executable,omitempty"`
	Version    string `json:"version,omitempty"`
	Source     string `json:"source,omitempty"`
	Error      string `json:"error,omitempty"`
}

type VirtualRepositoryNodeStatus struct {
	NodeID       string `json:"node_id"`
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	IsRepository bool   `json:"is_repository"`
	Branch       string `json:"branch,omitempty"`
	RemoteURL    string `json:"remote_url,omitempty"`
	Status       string `json:"status,omitempty"`
	Clean        bool   `json:"clean"`
	ErrorCode    string `json:"error_code,omitempty"`
	Error        string `json:"error,omitempty"`
}

type VirtualRepositoryDirectoryStats struct {
	Path      string `json:"path"`
	FileCount int64  `json:"file_count"`
	SizeBytes int64  `json:"size_bytes"`
}

func virtualRepositoryManifestPath(root string) string {
	return filepath.Join(root, virtualRepositoryDirName, virtualRepositoryManifestName)
}

func (a *App) virtualRepositoryStatePath(name string) string {
	return filepath.Join(a.getMaclawBaseDir(), name)
}

func cleanVirtualRepositoryRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("root directory is required")
	}
	if len(root) > virtualRepositoryFieldMaxLength || containsControlCharacter(root) {
		return "", errors.New("root directory path is invalid")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("root directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("root path is not a directory")
	}
	return filepath.Clean(abs), nil
}

func resolveVirtualRepositoryPath(root, relative string, mustExist bool) (string, error) {
	root, err := cleanVirtualRepositoryRoot(root)
	if err != nil {
		return "", err
	}
	relative = strings.TrimSpace(relative)
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("repository path must be a non-empty relative path")
	}
	if len(relative) > virtualRepositoryFieldMaxLength || containsControlCharacter(relative) {
		return "", errors.New("repository path is invalid")
	}
	cleanRel := filepath.Clean(relative)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", errors.New("repository path escapes the virtual repository root")
	}
	first := strings.Split(filepath.ToSlash(cleanRel), "/")[0]
	if strings.EqualFold(first, virtualRepositoryDirName) {
		return "", errors.New(".vrepo is reserved for virtual repository configuration")
	}
	target := filepath.Join(root, cleanRel)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("repository path escapes the virtual repository root")
	}
	if mustExist {
		info, statErr := os.Stat(target)
		if statErr != nil {
			return "", statErr
		}
		if !info.IsDir() {
			return "", errors.New("mapped path is not a directory")
		}
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			return "", fmt.Errorf("resolve virtual repository root links: %w", rootErr)
		}
		resolvedTarget, targetErr := filepath.EvalSymlinks(target)
		if targetErr != nil {
			return "", fmt.Errorf("resolve mapped path links: %w", targetErr)
		}
		realRel, relErr := filepath.Rel(resolvedRoot, resolvedTarget)
		if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return "", errors.New("mapped path escapes the root through a symbolic link")
		}
	} else {
		// Existing ancestors may contain a symlink even when the final directory
		// has not been created yet. Resolve the nearest existing ancestor so a
		// create request cannot escape the virtual root through that symlink.
		ancestor := target
		for {
			if _, statErr := os.Lstat(ancestor); statErr == nil {
				break
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				break
			}
			ancestor = parent
		}
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			return "", fmt.Errorf("resolve virtual repository root links: %w", rootErr)
		}
		resolvedAncestor, ancestorErr := filepath.EvalSymlinks(ancestor)
		if ancestorErr != nil {
			return "", fmt.Errorf("resolve mapped path ancestor links: %w", ancestorErr)
		}
		realRel, relErr := filepath.Rel(resolvedRoot, resolvedAncestor)
		if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return "", errors.New("mapped path escapes the root through a symbolic link")
		}
	}
	return filepath.Clean(target), nil
}

// deriveVirtualRepositoryMappingPaths keeps the persisted checkout path as an
// implementation detail. Users organize mappings exclusively by the virtual
// directory tree, so each mapping inherits its directory position from its
// parent chain and name.
func deriveVirtualRepositoryMappingPaths(v *VirtualRepository) {
	if v == nil {
		return
	}
	byID := make(map[string]*VirtualRepositoryNode, len(v.Nodes))
	for i := range v.Nodes {
		byID[strings.TrimSpace(v.Nodes[i].ID)] = &v.Nodes[i]
	}
	paths := make(map[string]string, len(v.Nodes))
	visiting := make(map[string]bool, len(v.Nodes))
	var pathFor func(string) string
	pathFor = func(id string) string {
		if value, ok := paths[id]; ok {
			return value
		}
		node := byID[id]
		if node == nil || visiting[id] {
			return ""
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return ""
		}
		visiting[id] = true
		parentID := strings.TrimSpace(node.ParentID)
		parentPath := ""
		if parentID != "" {
			parentPath = pathFor(parentID)
		}
		visiting[id] = false
		if parentID != "" && parentPath == "" {
			return ""
		}
		value := name
		if parentPath != "" {
			value = path.Join(parentPath, name)
		}
		paths[id] = value
		return value
	}
	for i := range v.Nodes {
		node := &v.Nodes[i]
		if node.Repository != nil {
			node.Repository.RelativePath = pathFor(strings.TrimSpace(node.ID))
		}
	}
}

func validateVirtualRepository(v *VirtualRepository) error {
	if v == nil {
		return errors.New("virtual repository is required")
	}
	v.ID = strings.TrimSpace(v.ID)
	if len(v.ID) > virtualRepositoryNameMaxLength || containsControlCharacter(v.ID) || strings.ContainsRune(v.ID, ':') {
		return errors.New("virtual repository id is invalid")
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return errors.New("virtual repository name is required")
	}
	if len(v.Name) > virtualRepositoryNameMaxLength {
		return errors.New("virtual repository name is too long")
	}
	if containsControlCharacter(v.Name) {
		return errors.New("virtual repository name must not contain control characters")
	}
	if v.Version == 0 {
		v.Version = virtualRepositoryManifestVersion
	}
	if v.Version != virtualRepositoryManifestVersion {
		return fmt.Errorf("unsupported .vrepo manifest version %d", v.Version)
	}
	if !v.CreatedAt.IsZero() && !v.UpdatedAt.IsZero() && v.UpdatedAt.Before(v.CreatedAt) {
		return errors.New("virtual repository updated_at must not be earlier than created_at")
	}
	if v.Remote != nil {
		v.Remote.Host = strings.TrimSpace(v.Remote.Host)
		v.Remote.User = strings.TrimSpace(v.Remote.User)
		v.RootPath = strings.TrimSpace(v.RootPath)
		if v.Remote.Host == "" || v.Remote.User == "" || v.RootPath == "" {
			return errors.New("remote host, username and root directory are required")
		}
		if len(v.Remote.User) > virtualRepositoryNameMaxLength || len(v.RootPath) > virtualRepositoryFieldMaxLength {
			return errors.New("remote connection field is too long")
		}
		if v.Remote.Port == 0 {
			v.Remote.Port = 22
		}
		if v.Remote.Port < 1 || v.Remote.Port > 65535 {
			return errors.New("remote SSH port must be between 1 and 65535")
		}
		if containsControlCharacter(v.Remote.Host) || containsControlCharacter(v.Remote.User) || containsControlCharacter(v.RootPath) {
			return errors.New("remote connection fields must not contain control characters")
		}
		if err := validateVirtualRepositorySSHHost(v.Remote.Host); err != nil {
			return err
		}
		if !strings.HasPrefix(v.RootPath, "/") || strings.Contains(v.RootPath, "\\") {
			return errors.New("remote root directory must be an absolute POSIX path")
		}
		v.RootPath = path.Clean(v.RootPath)
		if v.RootPath == "/" {
			return errors.New("remote root directory must not be the filesystem root")
		}
	} else if strings.TrimSpace(v.RootPath) != "" {
		root, err := cleanVirtualRepositoryRoot(v.RootPath)
		if err != nil {
			return err
		}
		v.RootPath = root
	}
	deriveVirtualRepositoryMappingPaths(v)
	if len(v.Nodes) > virtualRepositoryNodeMaxCount {
		return fmt.Errorf("virtual repository contains more than %d nodes", virtualRepositoryNodeMaxCount)
	}
	seen := make(map[string]struct{}, len(v.Nodes))
	parents := make(map[string]string, len(v.Nodes))
	nodeNames := make(map[string]string, len(v.Nodes))
	mappedNodes := make(map[string]string, len(v.Nodes))
	paths := map[string]string{}
	for i := range v.Nodes {
		n := &v.Nodes[i]
		n.ID = strings.TrimSpace(n.ID)
		n.Name = strings.TrimSpace(n.Name)
		n.ParentID = strings.TrimSpace(n.ParentID)
		if n.ID == "" || n.Name == "" {
			return errors.New("every node requires an id and name")
		}
		if len(n.ID) > virtualRepositoryNameMaxLength || len(n.ParentID) > virtualRepositoryNameMaxLength || len(n.Name) > virtualRepositoryNameMaxLength {
			return fmt.Errorf("node %q has an id or name that is too long", n.Name)
		}
		if containsControlCharacter(n.ID) || containsControlCharacter(n.ParentID) || containsControlCharacter(n.Name) || strings.ContainsRune(n.ID, ':') || strings.ContainsRune(n.ParentID, ':') {
			return fmt.Errorf("node %q contains an invalid id, parent id or name", n.Name)
		}
		if n.ParentID == n.ID {
			return fmt.Errorf("node %q cannot be its own parent", n.Name)
		}
		if strings.ContainsAny(n.Name, `/\\`) {
			return fmt.Errorf("node %q contains a path separator", n.Name)
		}
		if _, ok := seen[n.ID]; ok {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = struct{}{}
		parents[n.ID] = n.ParentID
		nodeNames[n.ID] = n.Name
		if n.Repository == nil {
			continue
		}
		mappedNodes[n.ID] = n.Name
		b := n.Repository
		b.Kind = strings.ToLower(strings.TrimSpace(b.Kind))
		if b.Kind != "git" && b.Kind != "svn" && b.Kind != "local" {
			return fmt.Errorf("node %q has unsupported kind %q", n.Name, b.Kind)
		}
		rawRelativePath := strings.TrimSpace(b.RelativePath)
		if v.Remote != nil {
			// Remote mappings follow the remote POSIX filesystem regardless of the
			// desktop OS. filepath.Clean on Windows treats a leading slash as a
			// rooted local path and can turn "/name" into "\\name", obscuring the
			// real validation error.
			if err := validateRemoteVirtualRepositoryRelativePath(rawRelativePath); err != nil {
				return fmt.Errorf("node %q: %w", n.Name, err)
			}
			b.RelativePath = path.Clean(rawRelativePath)
		} else {
			b.RelativePath = filepath.ToSlash(filepath.Clean(rawRelativePath))
		}
		if len(b.RelativePath) > virtualRepositoryFieldMaxLength {
			return fmt.Errorf("node %q repository path is too long", n.Name)
		}
		if containsControlCharacter(b.RelativePath) {
			return fmt.Errorf("node %q repository path contains control characters", n.Name)
		}
		if v.Remote == nil {
			if _, err := resolveVirtualRepositoryPath(v.RootPath, b.RelativePath, false); err != nil {
				return fmt.Errorf("node %q: %w", n.Name, err)
			}
		}
		pathKey := b.RelativePath
		if v.Remote == nil && goruntime.GOOS == "windows" {
			pathKey = strings.ToLower(pathKey)
		}
		if other, ok := paths[pathKey]; ok {
			return fmt.Errorf("nodes %q and %q map to the same path", other, n.Name)
		}
		paths[pathKey] = n.Name
		b.RemoteURL = strings.TrimSpace(b.RemoteURL)
		b.Description = strings.TrimSpace(b.Description)
		if len(b.Description) > virtualRepositoryFieldMaxLength || containsControlCharacter(b.Description) {
			return fmt.Errorf("node %q repository description is invalid", n.Name)
		}
		b.RefType = strings.ToLower(strings.TrimSpace(b.RefType))
		b.RefName = strings.TrimSpace(b.RefName)
		if b.Kind == "local" {
			b.RemoteURL = ""
			b.RefType, b.RefName = "", ""
		} else if b.RemoteURL == "" {
			return fmt.Errorf("node %q requires a repository URL", n.Name)
		} else if err := validateRepositoryRemoteURL(b.Kind, b.RemoteURL); err != nil {
			return fmt.Errorf("node %q: %w", n.Name, err)
		}
		if b.RefName == "" {
			// An empty ref always means the repository's default branch/path. Drop
			// a stale selector so the persisted manifest has one canonical form.
			b.RefType = ""
		}
		if err := validateVirtualRepositoryRef(b.Kind, b.RefType, b.RefName); err != nil {
			return fmt.Errorf("node %q: %w", n.Name, err)
		}
	}
	for _, n := range v.Nodes {
		if n.ParentID != "" {
			if _, ok := seen[n.ParentID]; !ok {
				return fmt.Errorf("node %q references a missing parent", n.Name)
			}
			if parentName, mapped := mappedNodes[n.ParentID]; mapped {
				return fmt.Errorf("node %q cannot be a child of mapped node %q", n.Name, parentName)
			}
		}
	}
	// Iterative three-color traversal validates all parent chains in O(nodes)
	// without allowing a hand-edited, deeply nested manifest to grow the Go stack.
	states := make(map[string]uint8, len(v.Nodes))
	for start := range parents {
		if states[start] == 2 {
			continue
		}
		chain := make([]string, 0, 8)
		for id := start; id != ""; id = parents[id] {
			if states[id] == 1 {
				return fmt.Errorf("node %q creates a parent cycle", nodeNames[id])
			}
			if states[id] == 2 {
				break
			}
			states[id] = 1
			chain = append(chain, id)
		}
		for _, id := range chain {
			states[id] = 2
		}
	}
	return nil
}

func validateVirtualRepositoryRef(kind, refType, refName string) error {
	if refType != "" && refType != "branch" && refType != "tag" {
		return errors.New("repository ref type must be branch or tag")
	}
	if refName == "" {
		return nil
	}
	if refType == "" {
		return errors.New("repository ref type is required when a branch or tag name is set")
	}
	if len(refName) > virtualRepositoryNameMaxLength || containsControlCharacter(refName) || strings.HasPrefix(refName, "-") {
		return errors.New("repository branch or tag name is invalid")
	}
	decoded, err := url.PathUnescape(refName)
	if err != nil {
		return errors.New("repository branch or tag name is invalid")
	}
	clean := path.Clean(strings.ReplaceAll(decoded, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsAny(refName, "?#") {
		return errors.New("repository branch or tag name is invalid")
	}
	for _, component := range strings.Split(strings.ReplaceAll(decoded, "\\", "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("repository branch or tag name is invalid")
		}
	}
	if kind == "git" && !validGitVirtualRepositoryRefName(refName) {
		return errors.New("Git branch or tag name is invalid")
	}
	return nil
}

func validGitVirtualRepositoryRefName(name string) bool {
	if name == "@" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, "/") || strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") || strings.ContainsAny(name, " ~^:?*[\\") {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func svnRepositoryURLForBinding(binding *VirtualRepositoryBinding) string {
	base := strings.TrimSuffix(sanitizeRepositoryRemoteURL(binding.RemoteURL), "/")
	if binding.RefName == "" {
		return base
	}
	segment := "branches"
	if binding.RefType == "tag" {
		segment = "tags"
	}
	return base + "/" + segment + "/" + strings.TrimPrefix(binding.RefName, "/")
}

func containsControlCharacter(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func virtualRepositoryLogError(err error) string {
	if err == nil {
		return ""
	}
	value := redactVCSOutput(err.Error())
	// Common Git/SVN diagnostics may echo a local or remote repository URL.
	// Logs only need the error category, so scrub URL/path-shaped operands too.
	value = virtualRepositoryLogURLPattern.ReplaceAllString(value, "[REPOSITORY_URL]")
	return truncateVirtualRepositoryDiagnostic(value, 512)
}

var virtualRepositoryLogURLPattern = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|[A-Za-z]:\\|/)[^\s"']+`)

func truncateVirtualRepositoryDiagnostic(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func unmarshalVirtualRepositoryInput(inputJSON, label string, dst any) error {
	if len(inputJSON) > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf("%s exceeds the %d-byte limit", label, virtualRepositoryManifestMaxBytes)
	}
	if strings.TrimSpace(inputJSON) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if err := json.Unmarshal([]byte(inputJSON), dst); err != nil {
		return fmt.Errorf("parse %s: %w", label, err)
	}
	return nil
}

func validateRepositoryRemoteURL(kind, value string) error {
	if len(value) > virtualRepositoryFieldMaxLength {
		return errors.New("repository URL is too long")
	}
	if containsControlCharacter(value) {
		return errors.New("repository URL must not contain control characters")
	}
	// Preserve Git's widely used SCP-like SSH syntax. url.Parse cannot parse it
	// as a URL because the colon separates the host from the remote path.
	if kind == "git" && strings.Contains(value, "@") && !strings.Contains(value, "://") {
		userHost, remotePath, ok := strings.Cut(value, ":")
		user, host, hasUser := strings.Cut(userHost, "@")
		if ok && hasUser && user != "" && remotePath != "" && !strings.ContainsAny(user+host+remotePath, " \t?#") && validateVirtualRepositorySSHHost(host) == nil {
			return nil
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}
	if parsed.Scheme == "" {
		return errors.New("repository URL requires a supported scheme or Git SCP-like SSH address")
	}
	scheme := strings.ToLower(parsed.Scheme)
	allowed := map[string]bool{"http": true, "https": true, "file": true}
	if kind == "git" {
		allowed["ssh"], allowed["git"] = true, true
	} else if kind == "svn" {
		allowed["svn"], allowed["svn+ssh"] = true, true
	}
	if !allowed[scheme] {
		return fmt.Errorf("repository URL scheme %q is not supported for %s", parsed.Scheme, kind)
	}
	if parsed.User != nil {
		return errors.New("repository URL must not contain a username or password")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("repository URL must not contain query parameters or a fragment")
	}
	if scheme == "file" {
		if parsed.Path == "" || (!strings.HasPrefix(parsed.Path, "/") && parsed.Opaque == "") {
			return errors.New("file repository URL must use an absolute path")
		}
		return nil
	}
	if parsed.Host == "" {
		return errors.New("repository URL requires a host")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return errors.New("repository URL requires a repository path")
	}
	return nil
}

func validateVirtualRepositorySSHHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/\\@?#[] \t") || strings.Contains(host, "://") {
		return errors.New("remote SSH host must be a hostname or IP address without a scheme, path or port")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if len(host) > 253 {
		return errors.New("remote SSH hostname is too long")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("remote SSH host is not a valid hostname")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return errors.New("remote SSH host is not a valid hostname")
			}
		}
	}
	return nil
}

func validateRemoteVirtualRepositoryRelativePath(relative string) error {
	relative = strings.TrimSpace(relative)
	if strings.HasPrefix(relative, "/") {
		return errors.New("remote repository path must be relative to the virtual repository root (for example, use \"maclaw2\" instead of \"/maclaw2\")")
	}
	if relative == "" || strings.Contains(relative, "\\") {
		return errors.New("remote repository path must be a non-empty POSIX relative path")
	}
	if len(relative) > virtualRepositoryFieldMaxLength || containsControlCharacter(relative) {
		return errors.New("remote repository path is invalid")
	}
	clean := path.Clean(relative)
	if clean != relative {
		return errors.New("remote repository path must be normalized and must not contain repeated separators, '.' or '..' components")
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("remote repository path escapes the virtual repository root")
	}
	if strings.EqualFold(strings.Split(clean, "/")[0], virtualRepositoryDirName) {
		return errors.New(".vrepo is reserved for virtual repository configuration")
	}
	return nil
}

func readVirtualRepository(root string) (*VirtualRepository, error) {
	root, err := cleanVirtualRepositoryRoot(root)
	if err != nil {
		return nil, err
	}
	manifestPath := virtualRepositoryManifestPath(root)
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, virtualRepositoryManifestMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > virtualRepositoryManifestMaxBytes {
		return nil, fmt.Errorf(".vrepo manifest exceeds %d bytes", virtualRepositoryManifestMaxBytes)
	}
	var repo VirtualRepository
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("parse .vrepo manifest: %w", err)
	}
	repo.RootPath = root
	if err := validateVirtualRepository(&repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

func writeVirtualRepository(repo *VirtualRepository) error {
	root, err := cleanVirtualRepositoryRoot(repo.RootPath)
	if err != nil {
		return err
	}
	repo.RootPath = root
	// Bound serialization before full semantic validation. This preserves the
	// most actionable error for oversized inputs and avoids spending time walking
	// a node graph that can never be persisted.
	probe := *repo
	probe.RootPath = ""
	probeData, marshalErr := json.Marshal(probe)
	if marshalErr != nil {
		return marshalErr
	}
	if len(probeData)+1 > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf(".vrepo manifest exceeds %d bytes", virtualRepositoryManifestMaxBytes)
	}
	if repo.ID == "" {
		repo.ID = "vrepo_" + uuid.NewString()
	}
	now := time.Now().UTC()
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	if err := validateVirtualRepository(repo); err != nil {
		return err
	}
	disk := *repo
	disk.RootPath = ""
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf(".vrepo manifest exceeds %d bytes", virtualRepositoryManifestMaxBytes)
	}
	return atomicWriteFile(virtualRepositoryManifestPath(root), append(data, '\n'))
}

func readJSONFile(path string, dst any) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, virtualRepositoryManifestMaxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf("JSON state file %q exceeds the %d-byte limit", filepath.Base(path), virtualRepositoryManifestMaxBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("JSON state file %q is empty", filepath.Base(path))
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse JSON state file %q: %w", filepath.Base(path), err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf("JSON state file %q exceeds the %d-byte limit", filepath.Base(path), virtualRepositoryManifestMaxBytes)
	}
	return atomicWriteFile(path, append(data, '\n'))
}

func (a *App) updateVirtualRepositoryIndex(repo *VirtualRepository) error {
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	return a.updateVirtualRepositoryIndexLocked(repo)
}

func (a *App) validateVirtualRepositoryIndexStateLocked() error {
	idx := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	if err := readJSONFile(a.virtualRepositoryStatePath("virtual-repositories-index.json"), &idx); err != nil {
		return err
	}
	return validateVirtualRepositoryIndex(&idx)
}

func (a *App) updateVirtualRepositoryIndexLocked(repo *VirtualRepository) error {
	path := a.virtualRepositoryStatePath("virtual-repositories-index.json")
	idx := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	if err := readJSONFile(path, &idx); err != nil {
		return err
	}
	if err := validateVirtualRepositoryIndex(&idx); err != nil {
		return err
	}
	next := make([]virtualRepositoryIndexEntry, 0, len(idx.Items)+1)
	for _, item := range idx.Items {
		sameLocation := sameVirtualRepositoryPath(item.RootPath, repo.RootPath)
		if item.Remote != nil || repo.Remote != nil {
			sameLocation = item.Remote != nil && repo.Remote != nil && remoteVirtualRepositoryHostID(item.Remote) == remoteVirtualRepositoryHostID(repo.Remote) && item.RootPath == repo.RootPath
		}
		if item.ID != repo.ID && !sameLocation {
			next = append(next, item)
		}
	}
	next = append(next, virtualRepositoryIndexEntry{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(repo.Remote), LastOpened: time.Now().UTC()})
	sort.Slice(next, func(i, j int) bool { return next[i].LastOpened.After(next[j].LastOpened) })
	idx.Items = next
	return writeJSONFile(path, idx)
}

func validateVirtualRepositoryIndex(index *virtualRepositoryIndex) error {
	if index.Version == 0 {
		index.Version = 1
	}
	if index.Version != 1 {
		return fmt.Errorf("unsupported virtual repository index version %d", index.Version)
	}
	seen := make(map[string]struct{}, len(index.Items))
	for _, item := range index.Items {
		id := strings.TrimSpace(item.ID)
		name := strings.TrimSpace(item.Name)
		rootPath := strings.TrimSpace(item.RootPath)
		if id == "" || name == "" || rootPath == "" {
			return errors.New("virtual repository index contains an incomplete entry")
		}
		if len(id) > virtualRepositoryNameMaxLength || len(name) > virtualRepositoryNameMaxLength || len(rootPath) > virtualRepositoryFieldMaxLength {
			return fmt.Errorf("virtual repository index entry %q contains a field that is too long", name)
		}
		if containsControlCharacter(id) || containsControlCharacter(name) || containsControlCharacter(rootPath) {
			return fmt.Errorf("virtual repository index entry %q contains control characters", name)
		}
		if item.ID != id || item.Name != name || item.RootPath != rootPath || strings.ContainsRune(id, ':') {
			return fmt.Errorf("virtual repository index entry %q contains a non-canonical field", name)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("virtual repository index contains duplicate id %q", id)
		}
		seen[id] = struct{}{}
		if item.Remote != nil {
			if err := validateVirtualRepositorySSHHost(item.Remote.Host); err != nil {
				return fmt.Errorf("virtual repository index entry %q: %w", name, err)
			}
			user := strings.TrimSpace(item.Remote.User)
			if item.Remote.Port < 0 || item.Remote.Port > 65535 || user == "" || len(user) > virtualRepositoryNameMaxLength || containsControlCharacter(user) || !strings.HasPrefix(rootPath, "/") || strings.Contains(rootPath, "\\") || path.Clean(rootPath) == "/" {
				return fmt.Errorf("virtual repository index entry %q has invalid remote connection fields", name)
			}
		}
	}
	return nil
}

func sameVirtualRepositoryPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (a *App) ListVirtualRepositories() (string, error) {
	idx := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	if err := readJSONFile(a.virtualRepositoryStatePath("virtual-repositories-index.json"), &idx); err != nil {
		return "", err
	}
	if err := validateVirtualRepositoryIndex(&idx); err != nil {
		return "", err
	}
	items := make([]any, 0, len(idx.Items))
	for _, item := range idx.Items {
		if item.Remote != nil {
			// Listing recent repositories must remain instant and must not fan out
			// SSH connections. The user explicitly opens a remote entry to connect.
			items = append(items, map[string]any{"id": item.ID, "name": item.Name, "root_path": item.RootPath, "remote": item.Remote, "available": true})
			continue
		}
		repo, err := readVirtualRepository(item.RootPath)
		if err != nil {
			items = append(items, map[string]any{"id": item.ID, "name": item.Name, "root_path": item.RootPath, "available": false, "error": err.Error()})
			continue
		}
		items = append(items, repo)
	}
	data, err := json.Marshal(items)
	return string(data), err
}

func (a *App) OpenVirtualRepository(root string) (string, error) {
	started := time.Now()
	repo, err := readVirtualRepository(root)
	if err != nil {
		log.Printf("[vrepo] open_local status=failed duration_ms=%d error=%q", time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	if err := a.updateVirtualRepositoryIndex(repo); err != nil {
		log.Printf("[vrepo] open_local repo=%q status=index_failed duration_ms=%d error=%q", repo.ID, time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	log.Printf("[vrepo] open_local repo=%q nodes=%d status=success duration_ms=%d", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds())
	data, err := json.Marshal(repo)
	return string(data), err
}

func (a *App) SaveVirtualRepository(inputJSON string) (string, error) {
	started := time.Now()
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	var repo VirtualRepository
	if err := unmarshalVirtualRepositoryInput(inputJSON, "virtual repository", &repo); err != nil {
		return "", err
	}
	if repo.Remote != nil {
		return "", errors.New("remote virtual repositories must be saved through SaveRemoteVirtualRepository")
	}
	// Validate machine-local state before changing the portable manifest. Since
	// this method holds virtualRepositoryStateMu, a later index update cannot
	// fail because another local save concurrently corrupted or replaced it.
	if err := a.validateVirtualRepositoryIndexStateLocked(); err != nil {
		return "", err
	}
	if strings.TrimSpace(repo.ID) == "" {
		root, err := cleanVirtualRepositoryRoot(repo.RootPath)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(virtualRepositoryManifestPath(root)); err == nil {
			return "", errors.New("this root already contains a virtual repository; open the existing repository instead of creating a new one")
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check existing virtual repository manifest: %w", err)
		}
	} else {
		current, err := readVirtualRepository(repo.RootPath)
		if err != nil {
			return "", fmt.Errorf("open existing virtual repository before saving: %w", err)
		}
		if current.ID != repo.ID {
			return "", errors.New("virtual repository id does not match the manifest at this root")
		}
		if repo.UpdatedAt.IsZero() {
			return "", errors.New("existing virtual repository revision is required; reopen it before saving")
		}
		if !repo.UpdatedAt.Equal(current.UpdatedAt) {
			return "", errors.New("virtual repository was modified by another window; reopen it before saving")
		}
	}
	if err := writeVirtualRepository(&repo); err != nil {
		log.Printf("[vrepo] save_local repo=%q nodes=%d status=manifest_failed duration_ms=%d error=%q", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	// Bindings are machine-local convenience state. A cleanup failure must not
	// turn a successfully persisted portable manifest into an apparent failure.
	// repositoryCredentialForNode validates the binding again before use.
	_ = a.pruneRepositoryCredentialBindingsLocked(&repo)
	if err := a.updateVirtualRepositoryIndexLocked(&repo); err != nil {
		log.Printf("[vrepo] save_local repo=%q nodes=%d status=index_failed duration_ms=%d error=%q", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	log.Printf("[vrepo] save_local repo=%q nodes=%d status=success duration_ms=%d", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds())
	data, err := json.Marshal(repo)
	if err == nil {
		a.clearVirtualRepositorySyncTombstone("repo", repo.ID)
		a.scheduleVirtualRepositorySync()
	}
	return string(data), err
}

func (a *App) DeleteVirtualRepository(id string) error {
	// A remote save deliberately releases virtualRepositoryStateMu while doing
	// SSH I/O, then reacquires it to update the recent index. Serialize deletion
	// with that full transaction so a delete cannot be undone by a late save.
	virtualRepositoryRemoteSaveMu.Lock()
	defer virtualRepositoryRemoteSaveMu.Unlock()
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("virtual repository id is required")
	}
	idx := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	path := a.virtualRepositoryStatePath("virtual-repositories-index.json")
	if err := readJSONFile(path, &idx); err != nil {
		return err
	}
	if err := validateVirtualRepositoryIndex(&idx); err != nil {
		return err
	}
	next := idx.Items[:0]
	found := false
	for _, item := range idx.Items {
		if item.ID != id {
			next = append(next, item)
		} else {
			found = true
		}
	}
	if !found {
		return errors.New("virtual repository was not found in recent repositories")
	}
	bindingFile, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return err
	}
	previousBindings := make(map[string]string, len(bindingFile.Bindings))
	for key, value := range bindingFile.Bindings {
		previousBindings[key] = value
	}
	prefix := id + ":"
	bindingsChanged := false
	removedBindings := []string{}
	for key := range bindingFile.Bindings {
		if strings.HasPrefix(key, prefix) {
			delete(bindingFile.Bindings, key)
			bindingsChanged = true
			removedBindings = append(removedBindings, key)
		}
	}
	bindingsPath := a.repositoryCredentialBindingsPath()
	if bindingsChanged {
		if err := writeJSONFile(bindingsPath, bindingFile); err != nil {
			return err
		}
	}
	idx.Items = next
	if err := writeJSONFile(path, idx); err != nil {
		if bindingsChanged {
			bindingFile.Bindings = previousBindings
			_ = writeJSONFile(bindingsPath, bindingFile)
		}
		return err
	}
	// Removing an index entry is also the point at which machine-local secrets
	// and node bindings become unreachable. The portable/local manifest and all
	// repository files remain untouched.
	_ = keyring.Delete(virtualRepositorySSHKeyringService, id)
	a.recordVirtualRepositorySyncTombstone("repo", id)
	// A remote repository's SSH password is scoped to the repository id. Keep a
	// matching tombstone so the encrypted Hub document drops the password too,
	// instead of retaining a secret for a repository the user deleted.
	a.recordVirtualRepositorySyncTombstone("ssh", id)
	for _, binding := range removedBindings {
		a.recordVirtualRepositorySyncTombstone("binding", binding)
	}
	a.scheduleVirtualRepositorySync()
	return nil
}

func (a *App) SelectVirtualRepositoryRoot(initialPath string) string {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Select Virtual Repository Root",
		DefaultDirectory:     strings.TrimSpace(initialPath),
		CanCreateDirectories: true,
	})
	if err != nil {
		return ""
	}
	return selection
}

func (a *App) CreateVirtualRepositoryDirectory(root, relativePath string) error {
	repo, err := readVirtualRepository(root)
	if err != nil {
		return err
	}
	binding := virtualRepositoryBindingByRelativePath(repo, relativePath)
	if binding == nil {
		return errors.New("mapped directory was not found in the saved virtual repository")
	}
	target, err := resolveVirtualRepositoryPath(repo.RootPath, binding.RelativePath, false)
	if err != nil {
		return err
	}
	return os.MkdirAll(target, 0o755)
}

func virtualRepositoryBindingByRelativePath(repo *VirtualRepository, relativePath string) *VirtualRepositoryBinding {
	if repo == nil {
		return nil
	}
	relativePath = strings.TrimSpace(relativePath)
	for i := range repo.Nodes {
		binding := repo.Nodes[i].Repository
		if binding != nil && binding.RelativePath == relativePath {
			return binding
		}
	}
	return nil
}

func (a *App) CheckoutVirtualRepositoryNode(repositoryID, nodeID string) error {
	repositoryID, nodeID = strings.TrimSpace(repositoryID), strings.TrimSpace(nodeID)
	if repositoryID == "" || nodeID == "" {
		return errors.New("virtual repository and node ids are required")
	}
	if len(repositoryID) > virtualRepositoryNameMaxLength || len(nodeID) > virtualRepositoryNameMaxLength || containsControlCharacter(repositoryID) || containsControlCharacter(nodeID) {
		return errors.New("virtual repository or node id is invalid")
	}
	items, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != repositoryID || item.Remote != nil {
			continue
		}
		repo, readErr := readVirtualRepository(item.RootPath)
		if readErr != nil {
			return readErr
		}
		return a.checkoutVirtualRepositoryNode(context.Background(), repo, nodeID)
	}
	return errors.New("local virtual repository was not found")
}

func (a *App) checkoutVirtualRepositoryNode(parent context.Context, repo *VirtualRepository, nodeID string) (resultErr error) {
	started := time.Now()
	kind, refType, refName := "", "", ""
	defer func() {
		status := "success"
		if resultErr != nil {
			status = "failed"
		}
		log.Printf("[vrepo] checkout_local repo=%q node=%q kind=%q ref_type=%q ref=%q status=%s duration_ms=%d error=%q", repo.ID, nodeID, kind, refType, refName, status, time.Since(started).Milliseconds(), virtualRepositoryLogError(resultErr))
	}()
	var node *VirtualRepositoryNode
	for i := range repo.Nodes {
		if repo.Nodes[i].ID == nodeID {
			node = &repo.Nodes[i]
			break
		}
	}
	if node == nil || node.Repository == nil || node.Repository.Kind == "local" || !node.Repository.Enabled {
		return errors.New("version-controlled virtual repository node was not found")
	}
	kind, refType, refName = node.Repository.Kind, node.Repository.RefType, node.Repository.RefName
	target, err := resolveVirtualRepositoryPath(repo.RootPath, node.Repository.RelativePath, false)
	if err != nil {
		return err
	}
	if entries, readErr := os.ReadDir(target); readErr == nil {
		if len(entries) != 0 {
			return errors.New("checkout target already exists and is not empty")
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	credential, secret, err := a.repositoryCredentialForNode(repo.ID, node.ID, node.Repository.Kind, node.Repository.RemoteURL)
	if err != nil {
		return err
	}
	if node.Repository.Kind == "git" {
		if credential != nil && (strings.ContainsAny(credential.Username, "\r\n") || strings.ContainsAny(secret, "\r\n")) {
			return &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("Git credential contains unsupported line breaks")}
		}
		args := []string{"clone"}
		if node.Repository.RefName != "" {
			args = append(args, "--branch", node.Repository.RefName, "--single-branch")
		}
		args = append(args, sanitizeRepositoryRemoteURL(node.Repository.RemoteURL), target)
		_, err = runGitVirtualRepositoryCheckout(ctx, args, credential, secret)
		return err
	}
	client := a.searchSVNClient(false)
	if !client.Available {
		return errors.New(client.Error)
	}
	auth, stdin, err := svnVirtualRepositoryAuth(ctx, client.Executable, credential, secret)
	if err != nil {
		return err
	}
	_, err = runVCSCommandInputEnv(ctx, client.Executable, "", nil, stdin, append([]string{"checkout", svnRepositoryURLForBinding(node.Repository), target}, auth...)...)
	return err
}

func runGitVirtualRepositoryCheckout(ctx context.Context, args []string, credential *RepositoryCredentialMetadata, secret string) (string, error) {
	extraEnv := []string{"GIT_TERMINAL_PROMPT=0"}
	cleanup := func() {}
	if credential != nil {
		askPass, err := createGitAskPassScript()
		if err != nil {
			return "", err
		}
		cleanup = askPass.cleanup
		extraEnv = append(extraEnv, "GIT_ASKPASS="+askPass.path, "GIT_ASKPASS_REQUIRE=force", "MACLAW_VREPO_GIT_USERNAME="+credential.Username, "MACLAW_VREPO_GIT_SECRET="+secret)
	}
	defer cleanup()
	return runVCSCommandEnv(ctx, "git", "", extraEnv, args...)
}

func (a *App) GetVirtualRepositoryDirectoryStats(root, relativePath string) (string, error) {
	target, err := resolveVirtualRepositoryPath(root, relativePath, true)
	if err != nil {
		return "", err
	}
	stats := VirtualRepositoryDirectoryStats{Path: target}
	visited := 0
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > virtualRepositoryStatsMaxEntries {
			return fmt.Errorf("directory contains more than %d entries; statistics were stopped", virtualRepositoryStatsMaxEntries)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		stats.FileCount++
		if info.Size() > 0 && stats.SizeBytes > math.MaxInt64-info.Size() {
			return errors.New("directory size exceeds the supported range")
		}
		stats.SizeBytes += info.Size()
		return nil
	})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(stats)
	return string(data), err
}

func (a *App) InspectVirtualRepository(root string) (string, error) {
	started := time.Now()
	repo, err := readVirtualRepository(root)
	if err != nil {
		log.Printf("[vrepo] inspect_local status=open_failed duration_ms=%d error=%q", time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	statuses := make([]VirtualRepositoryNodeStatus, 0)
	for _, node := range repo.Nodes {
		if node.Repository == nil || !node.Repository.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		statuses = append(statuses, a.inspectVirtualRepositoryNodeContext(ctx, repo.RootPath, node))
	}
	data, err := json.Marshal(statuses)
	errorsFound := 0
	for _, status := range statuses {
		if status.ErrorCode != "" {
			errorsFound++
		}
	}
	log.Printf("[vrepo] inspect_local repo=%q checked=%d errors=%d duration_ms=%d", repo.ID, len(statuses), errorsFound, time.Since(started).Milliseconds())
	return string(data), err
}

func (a *App) inspectVirtualRepositoryNode(root string, node VirtualRepositoryNode) VirtualRepositoryNodeStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.inspectVirtualRepositoryNodeContext(ctx, root, node)
}

func (a *App) inspectVirtualRepositoryNodeContext(ctx context.Context, root string, node VirtualRepositoryNode) VirtualRepositoryNodeStatus {
	b := node.Repository
	status := VirtualRepositoryNodeStatus{NodeID: node.ID, Kind: b.Kind}
	path, err := resolveVirtualRepositoryPath(root, b.RelativePath, true)
	if err != nil {
		target, targetErr := resolveVirtualRepositoryPath(root, b.RelativePath, false)
		_, lstatErr := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) && targetErr == nil && errors.Is(lstatErr, os.ErrNotExist) {
			status.ErrorCode = "not_checked_out"
		} else {
			status.ErrorCode = "path_invalid"
		}
		status.Error = err.Error()
		return status
	}
	status.Path = path
	status.Exists = true
	if b.Kind == "local" {
		status.IsRepository = true
		status.Clean = true
		status.Status = "local directory"
		return status
	}
	if b.Kind == "git" {
		inside, runErr := runVCSCommand(ctx, "git", path, "rev-parse", "--is-inside-work-tree")
		if runErr != nil || strings.TrimSpace(inside) != "true" {
			status.ErrorCode = "not_working_copy"
			status.Error = virtualRepositoryErrorText(runErr, "not a Git working tree")
			return status
		}
		status.IsRepository = true
		if b.RefType == "tag" && b.RefName != "" {
			status.Branch, err = runVCSCommand(ctx, "git", path, "describe", "--tags", "--exact-match")
			if err != nil {
				status.ErrorCode, status.Error = "ref_mismatch", fmt.Sprintf("configured tag %q is not checked out", b.RefName)
				return status
			}
		} else {
			status.Branch, err = runVCSCommand(ctx, "git", path, "symbolic-ref", "--quiet", "--short", "HEAD")
			if err != nil {
				status.ErrorCode, status.Error = "command_failed", err.Error()
				return status
			}
		}
		status.RemoteURL, err = runVCSCommand(ctx, "git", path, "remote", "get-url", "origin")
		if err != nil {
			status.ErrorCode, status.Error = "remote_unavailable", err.Error()
			return status
		}
		status.Status, err = runVCSCommand(ctx, "git", path, "status", "--short")
	} else {
		client := a.searchSVNClient(false)
		if !client.Available {
			status.ErrorCode = "client_not_found"
			status.Error = client.Error
			return status
		}
		_, runErr := runVCSCommand(ctx, client.Executable, path, "info", "--show-item", "wc-root")
		if runErr != nil {
			status.ErrorCode = "not_working_copy"
			status.Error = runErr.Error()
			return status
		}
		status.IsRepository = true
		status.RemoteURL, _ = runVCSCommand(ctx, client.Executable, path, "info", "--show-item", "url")
		status.Status, err = runVCSCommand(ctx, client.Executable, path, "status")
	}
	if err != nil {
		status.ErrorCode = "command_failed"
		status.Error = err.Error()
		return status
	}
	status.Branch = strings.TrimSpace(status.Branch)
	status.RemoteURL = sanitizeRepositoryRemoteURL(status.RemoteURL)
	expectedRemote := b.RemoteURL
	if b.Kind == "svn" {
		expectedRemote = svnRepositoryURLForBinding(b)
	}
	if expected := normalizeRepositoryRemoteURL(expectedRemote); expected != "" && normalizeRepositoryRemoteURL(status.RemoteURL) != expected {
		status.ErrorCode = "remote_mismatch"
		status.Error = fmt.Sprintf("configured repository URL does not match working copy remote (%s)", status.RemoteURL)
		return status
	}
	if b.Kind == "git" && b.RefName != "" && status.Branch != b.RefName {
		status.ErrorCode = "ref_mismatch"
		status.Error = fmt.Sprintf("configured %s %q is not checked out (current: %s)", b.RefType, b.RefName, status.Branch)
		return status
	}
	status.Status = strings.TrimSpace(status.Status)
	status.Clean = status.Status == ""
	return status
}

func normalizeRepositoryRemoteURL(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		value = parsed.String()
	}
	return strings.TrimSuffix(value, "/")
}

func sanitizeRepositoryRemoteURL(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimSuffix(parsed.String(), "/")
	}
	return value
}

func virtualRepositoryErrorText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func runVCSCommand(ctx context.Context, executable, dir string, args ...string) (string, error) {
	return runVCSCommandInputEnv(ctx, executable, dir, nil, "", args...)
}

func runVCSCommandEnv(ctx context.Context, executable, dir string, extraEnv []string, args ...string) (string, error) {
	return runVCSCommandInputEnv(ctx, executable, dir, extraEnv, "", args...)
}

func runVCSCommandInputEnv(ctx context.Context, executable, dir string, extraEnv []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout := &limitedVCSBuffer{limit: 512 * 1024}
	stderr := &limitedVCSBuffer{limit: 512 * 1024}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		message := redactVCSOutput(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return redactVCSOutput(stdout.String()), errors.New(message)
	}
	return redactVCSOutput(stdout.String()), nil
}

type limitedVCSBuffer struct {
	data      bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedVCSBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		if len(p) > remaining {
			b.truncated = true
			p = p[:remaining]
		}
		_, _ = b.data.Write(p)
	} else if len(p) > 0 {
		b.truncated = true
	}
	return original, nil
}

func (b *limitedVCSBuffer) String() string {
	value := b.data.String()
	if b.truncated {
		value += "\n[output truncated]"
	}
	return value
}

func redactVCSOutput(value string) string {
	value = strings.TrimSpace(value)
	value = vcsSecretFieldPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = vcsURLSecretQueryPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = vcsURLUserInfoPattern.ReplaceAllString(value, "$1[REDACTED]@")
	return value
}

var (
	vcsSecretFieldPattern    = regexp.MustCompile(`(?i)(password\s*[=:]\s*|passphrase\s*[=:]\s*|token\s*[=:]\s*|access_token\s*[=:]\s*|authorization\s*:\s*)[^\s&]+`)
	vcsURLSecretQueryPattern = regexp.MustCompile(`(?i)([?&](?:password|passphrase|token|access_token|auth|authorization)=)[^\s&#]+`)
	vcsURLUserInfoPattern    = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/@]+@`)
)

func (a *App) GetVCSClientStatus(kind string) (string, error) {
	var status VCSClientStatus
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "svn":
		status = a.searchSVNClient(false)
	case "git":
		status = validateVCSExecutable("git", "path", "git")
	default:
		return "", errors.New("unsupported VCS client kind")
	}
	data, err := json.Marshal(status)
	return string(data), err
}

func (a *App) SearchVCSClient(kind string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(kind), "svn") {
		return a.GetVCSClientStatus(kind)
	}
	status := a.searchSVNClient(true)
	data, err := json.Marshal(status)
	return string(data), err
}

func (a *App) SelectVCSClientExecutable(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "svn" && kind != "git" {
		return ""
	}
	name := kind
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select " + strings.ToUpper(kind) + " executable", Filters: []runtime.FileFilter{{DisplayName: name, Pattern: name}}})
	if err != nil {
		return ""
	}
	return selection
}

func (a *App) SetVCSClientExecutable(kind, executablePath string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(kind), "svn") {
		return "", errors.New("only SVN executable override is supported")
	}
	status := validateVCSExecutable(executablePath, "user", "svn")
	if !status.Available {
		return "", errors.New(status.Error)
	}
	path := a.virtualRepositoryStatePath("virtual-repository-local-settings.json")
	settings, err := loadVirtualRepositoryLocalSettings(path)
	if err != nil {
		return "", err
	}
	settings.SVNExecutable = status.Executable
	if err := writeJSONFile(path, settings); err != nil {
		return "", err
	}
	data, err := json.Marshal(status)
	return string(data), err
}

func (a *App) ResetVCSClientExecutable(kind string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(kind), "svn") {
		return "", errors.New("only SVN executable override is supported")
	}
	path := a.virtualRepositoryStatePath("virtual-repository-local-settings.json")
	settings, err := loadVirtualRepositoryLocalSettings(path)
	if err != nil {
		return "", err
	}
	settings.SVNExecutable = ""
	if err := writeJSONFile(path, settings); err != nil {
		return "", err
	}
	return a.SearchVCSClient("svn")
}

func (a *App) searchSVNClient(ignoreUser bool) VCSClientStatus {
	if !ignoreUser {
		settings, err := loadVirtualRepositoryLocalSettings(a.virtualRepositoryStatePath("virtual-repository-local-settings.json"))
		if err == nil && settings.SVNExecutable != "" {
			if status := validateVCSExecutable(settings.SVNExecutable, "user", "svn"); status.Available {
				return status
			}
		}
	}
	if status := validateVCSExecutable("svn", "path", "svn"); status.Available {
		return status
	}
	for _, candidate := range commonSVNExecutablePaths() {
		if status := validateVCSExecutable(candidate, "auto", "svn"); status.Available {
			return status
		}
	}
	return VCSClientStatus{Kind: "svn", Error: "SVN command line client was not found"}
}

func validateVCSExecutable(candidate, source, kind string) VCSClientStatus {
	status := VCSClientStatus{Kind: kind, Source: source}
	resolved, err := exec.LookPath(candidate)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	version, err := runVCSCommand(ctx, resolved, "", "--version", "--quiet")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Available = true
	status.Executable = resolved
	status.Version = strings.TrimSpace(version)
	return status
}

func commonSVNExecutablePaths() []string {
	if goruntime.GOOS != "windows" {
		return []string{"/usr/local/bin/svn", "/opt/homebrew/bin/svn", "/usr/bin/svn", "/snap/bin/svn"}
	}
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")}
	relatives := []string{
		filepath.Join("TortoiseSVN", "bin", "svn.exe"),
		filepath.Join("VisualSVN Server", "bin", "svn.exe"),
		filepath.Join("SlikSvn", "bin", "svn.exe"),
		filepath.Join("CollabNet", "Subversion Client", "svn.exe"),
	}
	var result []string
	seen := map[string]struct{}{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		for _, rel := range relatives {
			candidate := filepath.Join(root, rel)
			key := strings.ToLower(candidate)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, candidate)
			}
		}
	}
	return result
}
