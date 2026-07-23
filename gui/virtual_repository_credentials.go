package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

const virtualRepositoryKeyringService = "MaClaw Virtual Repository"

type RepositoryCredentialMetadata struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Username  string    `json:"username"`
	Scope     string    `json:"scope,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type repositoryCredentialFile struct {
	Version int                            `json:"version"`
	Items   []RepositoryCredentialMetadata `json:"items"`
}

type saveRepositoryCredentialInput struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Scope    string `json:"scope,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

type repositoryCredentialBindingFile struct {
	Version  int               `json:"version"`
	Bindings map[string]string `json:"bindings"`
}

type repositoryCredentialDeleteResult struct {
	ID               string   `json:"id"`
	AffectedBindings []string `json:"affected_bindings"`
}

func validateRepositoryCredentialInput(input *saveRepositoryCredentialInput) error {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Username = strings.TrimSpace(input.Username)
	input.Scope = strings.TrimSpace(input.Scope)
	if input.Name == "" || input.Username == "" {
		return errors.New("credential name and username are required")
	}
	if input.Kind != "git" && input.Kind != "svn" {
		return errors.New("credential kind must be git or svn")
	}
	if len(input.ID) > virtualRepositoryNameMaxLength || len(input.Name) > virtualRepositoryNameMaxLength || len(input.Username) > virtualRepositoryNameMaxLength || len(input.Scope) > virtualRepositoryFieldMaxLength || len(input.Secret) > virtualRepositoryFieldMaxLength {
		return errors.New("credential field is too long")
	}
	if containsControlCharacter(input.ID) || containsControlCharacter(input.Name) || containsControlCharacter(input.Username) || containsControlCharacter(input.Scope) {
		return errors.New("credential id, name, username and scope must not contain control characters")
	}
	if strings.ContainsAny(input.Secret, "\r\n\x00") {
		return errors.New("credential password must not contain line breaks or NUL characters")
	}
	return nil
}

func (a *App) repositoryCredentialPath() string {
	return a.virtualRepositoryStatePath("virtual-repository-credentials.json")
}

func (a *App) repositoryCredentialBindingsPath() string {
	return a.virtualRepositoryStatePath("virtual-repository-bindings.json")
}

func validateRepositoryCredentialFile(file *repositoryCredentialFile) error {
	if file.Version != 1 {
		return fmt.Errorf("unsupported repository credential file version %d", file.Version)
	}
	seen := make(map[string]struct{}, len(file.Items))
	for _, item := range file.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Username) == "" {
			return errors.New("repository credential file contains an incomplete entry")
		}
		if item.Kind != "git" && item.Kind != "svn" {
			return fmt.Errorf("repository credential %q has an invalid kind", item.Name)
		}
		if len(item.ID) > virtualRepositoryNameMaxLength || len(item.Name) > virtualRepositoryNameMaxLength || len(item.Username) > virtualRepositoryNameMaxLength || len(item.Scope) > virtualRepositoryFieldMaxLength {
			return fmt.Errorf("repository credential %q contains a field that is too long", item.Name)
		}
		if containsControlCharacter(item.ID) || containsControlCharacter(item.Name) || containsControlCharacter(item.Username) || containsControlCharacter(item.Scope) {
			return fmt.Errorf("repository credential %q contains control characters", item.Name)
		}
		if _, exists := seen[item.ID]; exists {
			return fmt.Errorf("repository credential file contains duplicate id %q", item.ID)
		}
		if item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() || item.UpdatedAt.Before(item.CreatedAt) {
			return fmt.Errorf("repository credential %q contains invalid timestamps", item.Name)
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func (a *App) loadRepositoryCredentialBindings() (repositoryCredentialBindingFile, error) {
	file := repositoryCredentialBindingFile{Version: 1, Bindings: map[string]string{}}
	if err := readJSONFile(a.repositoryCredentialBindingsPath(), &file); err != nil {
		return file, err
	}
	if file.Version != 1 {
		return file, fmt.Errorf("unsupported repository credential binding file version %d", file.Version)
	}
	if file.Bindings == nil {
		file.Bindings = map[string]string{}
	}
	for key, credentialID := range file.Bindings {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(credentialID) == "" {
			return file, errors.New("repository credential binding file contains an incomplete entry")
		}
		if len(key) > virtualRepositoryFieldMaxLength || len(credentialID) > virtualRepositoryNameMaxLength || containsControlCharacter(key) || containsControlCharacter(credentialID) {
			return file, errors.New("repository credential binding file contains an invalid entry")
		}
	}
	return file, nil
}

// pruneRepositoryCredentialBindingsLocked removes machine-local bindings that
// no longer point at a Git/SVN node in the saved manifest. The caller must hold
// virtualRepositoryStateMu so manifest and binding updates cannot interleave.
func (a *App) pruneRepositoryCredentialBindingsLocked(repo *VirtualRepository) error {
	if repo == nil || strings.TrimSpace(repo.ID) == "" {
		return nil
	}
	bindings, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return err
	}
	path := a.repositoryCredentialBindingsPath()
	if len(bindings.Bindings) == 0 {
		return nil
	}
	valid := make(map[string]string, len(repo.Nodes))
	for _, node := range repo.Nodes {
		if node.Repository != nil && (node.Repository.Kind == "git" || node.Repository.Kind == "svn") {
			valid[node.ID] = node.Repository.Kind
		}
	}
	credentialKinds := map[string]string{}
	credentials, err := a.loadRepositoryCredentials()
	if err != nil {
		return err
	}
	for _, credential := range credentials.Items {
		credentialKinds[credential.ID] = credential.Kind
	}
	prefix := strings.TrimSpace(repo.ID) + ":"
	changed := false
	removed := make([]string, 0)
	for key, credentialID := range bindings.Bindings {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		nodeID := strings.TrimPrefix(key, prefix)
		kind, exists := valid[nodeID]
		if !exists || credentialKinds[credentialID] != kind {
			delete(bindings.Bindings, key)
			changed = true
			removed = append(removed, key)
		}
	}
	if !changed {
		return nil
	}
	if err := writeJSONFile(path, bindings); err != nil {
		return err
	}
	// The binding was removed because a repository edit made it invalid. It is
	// still a user-visible deletion and must be replicated, otherwise another
	// device can restore it from an older Hub document.
	for _, key := range removed {
		a.recordVirtualRepositorySyncTombstone("binding", key)
	}
	return nil
}

func (a *App) loadRepositoryCredentials() (repositoryCredentialFile, error) {
	file := repositoryCredentialFile{Version: 1, Items: []RepositoryCredentialMetadata{}}
	if err := readJSONFile(a.repositoryCredentialPath(), &file); err != nil {
		return file, err
	}
	if file.Items == nil {
		file.Items = []RepositoryCredentialMetadata{}
	}
	if err := validateRepositoryCredentialFile(&file); err != nil {
		return file, err
	}
	return file, nil
}

func (a *App) ListRepositoryCredentials(kind string) (string, error) {
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return "", err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	items := make([]RepositoryCredentialMetadata, 0, len(file.Items))
	for _, item := range file.Items {
		if kind == "" || item.Kind == kind {
			items = append(items, item)
		}
	}
	data, err := json.Marshal(items)
	return string(data), err
}

func (a *App) SaveRepositoryCredential(inputJSON string) (string, error) {
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	var input saveRepositoryCredentialInput
	if err := unmarshalVirtualRepositoryInput(inputJSON, "repository credential", &input); err != nil {
		return "", err
	}
	if err := validateRepositoryCredentialInput(&input); err != nil {
		return "", err
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	item := RepositoryCredentialMetadata{ID: strings.TrimSpace(input.ID), Name: input.Name, Kind: input.Kind, Username: input.Username, Scope: input.Scope, UpdatedAt: now}
	var previousSecret string
	hadPreviousSecret := false
	if item.ID == "" {
		if input.Secret == "" {
			return "", errors.New("password or token is required for a new credential")
		}
		item.ID = "vcred_" + uuid.NewString()
		item.CreatedAt = now
	} else {
		found := false
		for _, existing := range file.Items {
			if existing.ID == item.ID {
				if existing.Kind != item.Kind {
					return "", errors.New("credential type cannot be changed while editing; create a new credential instead")
				}
				item.CreatedAt = existing.CreatedAt
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("credential not found")
		}
		if input.Secret == "" {
			if _, keyringErr := keyring.Get(virtualRepositoryKeyringService, item.ID); keyringErr != nil {
				return "", errors.New("stored password is unavailable; enter it again before saving")
			}
		}
	}
	if input.Secret != "" {
		if existingSecret, getErr := keyring.Get(virtualRepositoryKeyringService, item.ID); getErr == nil {
			previousSecret, hadPreviousSecret = existingSecret, true
		}
		if err := keyring.Set(virtualRepositoryKeyringService, item.ID, input.Secret); err != nil {
			return "", fmt.Errorf("save credential in system keyring: %w", err)
		}
	}
	updated := false
	for i := range file.Items {
		if file.Items[i].ID == item.ID {
			file.Items[i] = item
			updated = true
			break
		}
	}
	if !updated {
		file.Items = append(file.Items, item)
	}
	if err := writeJSONFile(a.repositoryCredentialPath(), file); err != nil {
		if hadPreviousSecret {
			_ = keyring.Set(virtualRepositoryKeyringService, item.ID, previousSecret)
		} else if input.Secret != "" {
			_ = keyring.Delete(virtualRepositoryKeyringService, item.ID)
		}
		return "", err
	}
	data, err := json.Marshal(item)
	if err == nil {
		a.clearVirtualRepositorySyncTombstone("cred", item.ID)
		a.scheduleVirtualRepositorySync()
	}
	return string(data), err
}

func (a *App) DeleteRepositoryCredential(id string) (string, error) {
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("credential id is required")
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return "", err
	}
	found := false
	var deletedCredential RepositoryCredentialMetadata
	next := file.Items[:0]
	for _, item := range file.Items {
		if item.ID == id {
			found = true
			deletedCredential = item
			continue
		}
		next = append(next, item)
	}
	if !found {
		return "", errors.New("credential not found")
	}
	previousSecret, previousSecretErr := keyring.Get(virtualRepositoryKeyringService, id)
	hadPreviousSecret := previousSecretErr == nil
	bindings, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return "", err
	}
	previousBindings := repositoryCredentialBindingFile{Version: bindings.Version, Bindings: make(map[string]string, len(bindings.Bindings))}
	for key, value := range bindings.Bindings {
		previousBindings.Bindings[key] = value
	}
	affected := []string{}
	for key, credentialID := range bindings.Bindings {
		if credentialID == id {
			affected = append(affected, key)
			delete(bindings.Bindings, key)
		}
	}
	sort.Strings(affected)
	if err := keyring.Delete(virtualRepositoryKeyringService, id); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("delete credential from system keyring: %w", err)
	}
	file.Items = next
	if err := writeJSONFile(a.repositoryCredentialPath(), file); err != nil {
		if hadPreviousSecret {
			_ = keyring.Set(virtualRepositoryKeyringService, id, previousSecret)
		}
		return "", err
	}
	if err := writeJSONFile(a.repositoryCredentialBindingsPath(), bindings); err != nil {
		file.Items = append(file.Items, deletedCredential)
		_ = writeJSONFile(a.repositoryCredentialPath(), file)
		_ = writeJSONFile(a.repositoryCredentialBindingsPath(), previousBindings)
		if hadPreviousSecret {
			_ = keyring.Set(virtualRepositoryKeyringService, id, previousSecret)
		}
		return "", err
	}
	data, err := json.Marshal(repositoryCredentialDeleteResult{ID: id, AffectedBindings: affected})
	if err == nil {
		a.recordVirtualRepositorySyncTombstone("cred", id)
		for _, binding := range affected {
			a.recordVirtualRepositorySyncTombstone("binding", binding)
		}
		a.scheduleVirtualRepositorySync()
	}
	return string(data), err
}

func (a *App) SetRepositoryCredentialBinding(repositoryID, nodeID, credentialID string) error {
	repositoryID = strings.TrimSpace(repositoryID)
	nodeID = strings.TrimSpace(nodeID)
	credentialID = strings.TrimSpace(credentialID)
	if repositoryID == "" || nodeID == "" {
		return errors.New("repository id and node id are required")
	}
	var boundKind string
	stateLocked := false
	if credentialID != "" {
		var indexItem *virtualRepositoryIndexEntry
		indexItems, err := a.loadVirtualRepositoryIndexItems()
		if err != nil {
			return err
		}
		for _, candidate := range indexItems {
			if candidate.ID != repositoryID {
				continue
			}
			itemCopy := candidate
			indexItem = &itemCopy
			break
		}
		if indexItem == nil {
			return errors.New("virtual repository was not found")
		}
		var repo *VirtualRepository
		if indexItem.Remote != nil {
			// SSH may block for seconds. Do not hold the machine-local state lock
			// while loading a portable remote manifest.
			repo, err = a.readRemoteVirtualRepository(*indexItem, "", false)
		} else {
			virtualRepositoryStateMu.Lock()
			stateLocked = true
			repo, err = readVirtualRepository(indexItem.RootPath)
		}
		if err != nil {
			if stateLocked {
				virtualRepositoryStateMu.Unlock()
			}
			return err
		}
		for _, node := range repo.Nodes {
			if node.ID == nodeID && node.Repository != nil {
				boundKind = node.Repository.Kind
				break
			}
		}
		if boundKind == "" || boundKind == "local" {
			if stateLocked {
				virtualRepositoryStateMu.Unlock()
			}
			return errors.New("credential binding target is not a Git or SVN mapping")
		}
	}
	if !stateLocked {
		virtualRepositoryStateMu.Lock()
	}
	defer virtualRepositoryStateMu.Unlock()
	bindings, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return err
	}
	path := a.repositoryCredentialBindingsPath()
	key := repositoryID + ":" + nodeID
	if credentialID == "" {
		delete(bindings.Bindings, key)
	} else {
		found := false
		file, err := a.loadRepositoryCredentials()
		if err != nil {
			return err
		}
		for _, item := range file.Items {
			if item.ID == credentialID {
				if item.Kind != boundKind {
					return fmt.Errorf("%s credential cannot be bound to %s repository", item.Kind, boundKind)
				}
				found = true
				break
			}
		}
		if !found {
			return errors.New("credential not found")
		}
		bindings.Bindings[key] = credentialID
	}
	err = writeJSONFile(path, bindings)
	if err == nil {
		if credentialID == "" {
			a.recordVirtualRepositorySyncTombstone("binding", key)
		} else {
			a.clearVirtualRepositorySyncTombstone("binding", key)
		}
		a.scheduleVirtualRepositorySync()
	}
	return err
}

func (a *App) loadVirtualRepositoryIndexItems() ([]virtualRepositoryIndexEntry, error) {
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	if err := readJSONFile(a.virtualRepositoryStatePath("virtual-repositories-index.json"), &index); err != nil {
		return nil, err
	}
	if err := validateVirtualRepositoryIndex(&index); err != nil {
		return nil, err
	}
	return index.Items, nil
}

func (a *App) ListRepositoryCredentialBindings(repositoryID string) (string, error) {
	bindings, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return "", err
	}
	prefix := strings.TrimSpace(repositoryID) + ":"
	result := map[string]string{}
	for key, value := range bindings.Bindings {
		if strings.HasPrefix(key, prefix) {
			result[strings.TrimPrefix(key, prefix)] = value
		}
	}
	data, err := json.Marshal(result)
	return string(data), err
}

func (a *App) repositoryCredentialForNode(repositoryID, nodeID, repositoryKind, repositoryRemote string) (*RepositoryCredentialMetadata, string, error) {
	bindings, err := a.loadRepositoryCredentialBindings()
	if err != nil {
		return nil, "", err
	}
	credentialID := bindings.Bindings[strings.TrimSpace(repositoryID)+":"+strings.TrimSpace(nodeID)]
	if credentialID == "" {
		return nil, "", nil
	}
	file, err := a.loadRepositoryCredentials()
	if err != nil {
		return nil, "", err
	}
	for _, item := range file.Items {
		if item.ID != credentialID {
			continue
		}
		if item.Kind != strings.ToLower(strings.TrimSpace(repositoryKind)) {
			return nil, "", fmt.Errorf("bound %s credential cannot be used with %s repository", item.Kind, repositoryKind)
		}
		if !repositoryCredentialScopeAllowsRemote(item.Scope, repositoryRemote) {
			return nil, "", fmt.Errorf("credential scope %q does not allow repository host", item.Scope)
		}
		secret, err := keyring.Get(virtualRepositoryKeyringService, credentialID)
		if err != nil {
			return nil, "", fmt.Errorf("read credential from system keyring: %w", err)
		}
		copy := item
		return &copy, secret, nil
	}
	return nil, "", errors.New("bound credential metadata was not found")
}

// repositoryCredentialScopeAllowsRemote enforces scopes that are clearly host
// restrictions (URL, IP, localhost, DNS name, or host:port). Free-form values
// remain valid for SVN realm labels, which cannot be determined before the SVN
// client authenticates.
func repositoryCredentialScopeAllowsRemote(scope, remote string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return true
	}
	remoteHost := repositoryRemoteHostname(remote)
	if remoteHost == "" {
		return true
	}
	scopeHost := scope
	if parsed, parseErr := url.Parse(scope); parseErr == nil && parsed.Hostname() != "" {
		scopeHost = parsed.Hostname()
	} else if host, _, splitErr := net.SplitHostPort(scope); splitErr == nil {
		scopeHost = host
	}
	scopeHost = strings.Trim(strings.TrimSpace(scopeHost), "[]")
	isHostRestriction := strings.EqualFold(scopeHost, "localhost") || net.ParseIP(scopeHost) != nil || strings.Contains(scopeHost, ".") || strings.Contains(scope, ":")
	if !isHostRestriction {
		return true
	}
	return strings.EqualFold(scopeHost, remoteHost)
}

func repositoryRemoteHostname(remote string) string {
	remote = strings.TrimSpace(remote)
	if parsed, err := url.Parse(remote); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	// Git's SCP-like SSH syntax is not represented as a hierarchical URL by
	// net/url, so extract its host explicitly for credential-scope enforcement.
	if userHost, remotePath, ok := strings.Cut(remote, ":"); ok && remotePath != "" && !strings.Contains(remote, "://") {
		if _, host, hasUser := strings.Cut(userHost, "@"); hasUser && validateVirtualRepositorySSHHost(host) == nil {
			return host
		}
	}
	return ""
}

func repositoryCredentialFilesContainSecret(baseDir, secret string) (bool, error) {
	for _, name := range []string{"virtual-repository-credentials.json", "virtual-repository-bindings.json"} {
		data, err := os.ReadFile(filepath.Join(baseDir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if strings.Contains(string(data), secret) {
			return true, nil
		}
	}
	return false, nil
}
