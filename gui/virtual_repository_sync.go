package main

// Virtual repository cloud synchronization.  Definitions are portable (local
// absolute roots are never uploaded); passwords remain in the operating-system
// keyring locally and are only present inside the Hub's encrypted sync document.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

const virtualRepositorySyncVersion = 1

var virtualRepositoryCloudSyncMu sync.Mutex

var errVirtualRepositoryChangedDuringSync = errors.New("virtual repository changed while synchronizing")

type virtualRepositorySyncPackage struct {
	Version      int                                  `json:"version"`
	Repositories map[string]virtualRepositorySyncRepo `json:"repositories"`
	Credentials  map[string]virtualRepositorySyncCred `json:"credentials"`
	Bindings     map[string]string                    `json:"bindings"`
	SSHSecrets   map[string]string                    `json:"ssh_secrets"`
	Tombstones   map[string]time.Time                 `json:"tombstones,omitempty"`
}
type virtualRepositorySyncRepo struct {
	Repository VirtualRepository `json:"repository"`
	Location   string            `json:"location"` // local or remote
}
type virtualRepositorySyncCred struct {
	Metadata RepositoryCredentialMetadata `json:"metadata"`
	Secret   string                       `json:"secret,omitempty"`
}
type virtualRepositorySyncState struct {
	Version       int                  `json:"version"`
	CloudRevision string               `json:"cloud_revision,omitempty"`
	ItemHashes    map[string]string    `json:"item_hashes"`
	LastSyncedAt  time.Time            `json:"last_synced_at,omitempty"`
	Tombstones    map[string]time.Time `json:"tombstones,omitempty"`
}
type VirtualRepositorySyncConflict struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type VirtualRepositorySyncResult struct {
	Status        string                          `json:"status"`
	CloudRevision string                          `json:"cloud_revision,omitempty"`
	LastSyncedAt  time.Time                       `json:"last_synced_at,omitempty"`
	Conflicts     []VirtualRepositorySyncConflict `json:"conflicts,omitempty"`
	Message       string                          `json:"message,omitempty"`
}

func (a *App) virtualRepositorySyncStatePath() string {
	return a.virtualRepositoryStatePath("virtual-repository-sync-state.json")
}

// withVirtualRepositorySyncState serializes read-modify-write operations on
// the sync checkpoint. Repository saves, deletes, and the background sync run
// otherwise race here and can silently lose a newly created tombstone.
func (a *App) withVirtualRepositorySyncState(update func(*virtualRepositorySyncState) error) error {
	if a == nil || update == nil {
		return nil
	}
	a.virtualRepositorySyncStateMu.Lock()
	defer a.virtualRepositorySyncStateMu.Unlock()
	state, err := a.loadVirtualRepositorySyncState()
	if err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}
	return writeJSONFile(a.virtualRepositorySyncStatePath(), state)
}
func (a *App) loadVirtualRepositorySyncState() (virtualRepositorySyncState, error) {
	state := virtualRepositorySyncState{Version: virtualRepositorySyncVersion, ItemHashes: map[string]string{}}
	if err := readJSONFile(a.virtualRepositorySyncStatePath(), &state); err != nil {
		return state, err
	}
	if state.Version != virtualRepositorySyncVersion {
		return state, fmt.Errorf("unsupported virtual repository sync state version %d", state.Version)
	}
	if state.ItemHashes == nil {
		state.ItemHashes = map[string]string{}
	}
	if state.Tombstones == nil {
		state.Tombstones = map[string]time.Time{}
	}
	return state, nil
}

func virtualRepositorySyncHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func virtualRepositorySyncPackageHashes(pkg virtualRepositorySyncPackage) map[string]string {
	result := map[string]string{}
	for id, repo := range pkg.Repositories {
		result["repo:"+id] = virtualRepositorySyncHash(repo)
	}
	for id, credential := range pkg.Credentials {
		result["cred:"+id] = virtualRepositorySyncHash(credential)
	}
	for id, secret := range pkg.SSHSecrets {
		result["ssh:"+id] = virtualRepositorySyncHash(secret)
	}
	for id, deletedAt := range pkg.Tombstones {
		result["tombstone:"+id] = virtualRepositorySyncHash(deletedAt)
	}
	for key, value := range pkg.Bindings {
		result["binding:"+key] = virtualRepositorySyncHash(value)
	}
	return result
}

func virtualRepositorySyncTombstoneKey(kind, id string) string {
	return strings.TrimSpace(kind) + ":" + strings.TrimSpace(id)
}

// normalizedVirtualRepositorySyncTombstones keeps documents written by the
// first sync implementation compatible: its unqualified ids represented
// repository deletions. New documents qualify every deleted item so a
// repository, credential, and binding with coincidentally equal ids cannot
// erase each other.
func normalizedVirtualRepositorySyncTombstones(input map[string]time.Time) map[string]time.Time {
	result := make(map[string]time.Time, len(input))
	for key, deletedAt := range input {
		key = strings.TrimSpace(key)
		if key == "" || deletedAt.IsZero() {
			continue
		}
		if !strings.Contains(key, ":") {
			key = virtualRepositorySyncTombstoneKey("repo", key)
		}
		if existing, ok := result[key]; !ok || deletedAt.After(existing) {
			result[key] = deletedAt
		}
	}
	return result
}

// virtualRepositorySyncOptionalSecret distinguishes an intentionally absent
// keyring entry from a keyring outage. Treating every read failure as an empty
// password would upload incomplete data and could overwrite a working secret
// stored by another device.
func virtualRepositorySyncOptionalSecret(service, id string) (string, error) {
	secret, err := keyring.Get(service, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read sync secret from system keyring: %w", err)
	}
	return secret, nil
}

func (a *App) snapshotVirtualRepositorySyncPackage() (virtualRepositorySyncPackage, error) {
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{}, Credentials: map[string]virtualRepositorySyncCred{}, Bindings: map[string]string{}, SSHSecrets: map[string]string{}, Tombstones: map[string]time.Time{}}
	state, err := a.loadVirtualRepositorySyncState()
	if err != nil {
		return pkg, err
	}
	for id, deletedAt := range normalizedVirtualRepositorySyncTombstones(state.Tombstones) {
		pkg.Tombstones[id] = deletedAt
	}
	// Snapshot all machine-local files under their shared lock before doing any
	// remote I/O. The prior lock/unlock sequence could combine a new index with
	// an old credential/binding file (or vice versa), and upload a state that
	// never existed locally as one coherent revision.
	virtualRepositoryStateMu.Lock()
	items, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		virtualRepositoryStateMu.Unlock()
		return pkg, err
	}
	credentials, err := a.loadRepositoryCredentials()
	if err != nil {
		virtualRepositoryStateMu.Unlock()
		return pkg, err
	}
	bindings, err := a.loadRepositoryCredentialBindings()
	virtualRepositoryStateMu.Unlock()
	if err != nil {
		return pkg, err
	}
	for _, item := range items {
		var repo *VirtualRepository
		if item.Remote != nil {
			// Never hold the process-wide repository lock while SSH/SFTP can block.
			repo, err = a.readRemoteVirtualRepository(item, "", false)
		} else {
			virtualRepositoryStateMu.Lock()
			repo, err = readVirtualRepository(item.RootPath)
			virtualRepositoryStateMu.Unlock()
		}
		if err != nil {
			// Do not silently omit a repository. An unavailable remote machine is
			// transient; uploading a partial snapshot would make its repository
			// appear deleted on other devices. The automatic retry loop will try
			// again once the remote endpoint is reachable.
			return pkg, fmt.Errorf("read virtual repository %q for sync: %w", item.Name, err)
		}
		portable := *repo
		location := "local"
		if portable.Remote != nil {
			location = "remote"
		} else {
			portable.RootPath = ""
		}
		pkg.Repositories[portable.ID] = virtualRepositorySyncRepo{Repository: portable, Location: location}
		if portable.Remote != nil {
			secret, secretErr := virtualRepositorySyncOptionalSecret(virtualRepositorySSHKeyringService, portable.ID)
			if secretErr != nil {
				return pkg, secretErr
			}
			if secret != "" {
				pkg.SSHSecrets[portable.ID] = secret
			}
		}
	}
	for _, item := range credentials.Items {
		secret, secretErr := virtualRepositorySyncOptionalSecret(virtualRepositoryKeyringService, item.ID)
		if secretErr != nil {
			return pkg, secretErr
		}
		pkg.Credentials[item.ID] = virtualRepositorySyncCred{Metadata: item, Secret: secret}
	}
	for key, value := range bindings.Bindings {
		pkg.Bindings[key] = value
	}
	return pkg, nil
}

func (a *App) virtualRepositorySyncClient() (hubURL, token, machineID string, err error) {
	hubURL, token, err = a.getHubCredentials()
	if err != nil {
		return
	}
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		err = cfgErr
		return
	}
	machineID = strings.TrimSpace(cfg.RemoteMachineID)
	if machineID == "" {
		err = errors.New("machine id missing; register to Hub first")
	}
	return
}
func (a *App) virtualRepositorySyncRequest(ctx context.Context, method string, body any) ([]byte, http.Header, int, error) {
	hubURL, token, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		return nil, nil, 0, err
	}
	var reader io.Reader
	if body != nil {
		raw, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, nil, 0, marshalErr
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, hubURL+"/api/virtual-repositories/sync", reader)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if resp.StatusCode >= 300 {
		return data, resp.Header, resp.StatusCode, fmt.Errorf("hub returned %d", resp.StatusCode)
	}
	return data, resp.Header, resp.StatusCode, nil
}

// SyncVirtualRepositories is safe for a manual button and the background retry
// loop. resolutions maps a conflict key (for example repo:vrepo_x) to local,
// cloud, or copy. Without a resolution, conflicting items are left untouched.
func (a *App) SyncVirtualRepositories(resolutionsJSON string) (string, error) {
	virtualRepositoryCloudSyncMu.Lock()
	defer virtualRepositoryCloudSyncMu.Unlock()
	resolutions := map[string]string{}
	if strings.TrimSpace(resolutionsJSON) != "" {
		if err := json.Unmarshal([]byte(resolutionsJSON), &resolutions); err != nil {
			return "", errors.New("invalid sync conflict resolution")
		}
	}
	// Capture this before reading the local package. A save that overlaps the
	// snapshot or Hub request must force a new run, rather than allowing the
	// older package to overwrite the save during apply.
	localGeneration := a.virtualRepositorySyncGeneration.Load()
	local, err := a.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		return "", err
	}
	state, err := a.loadVirtualRepositorySyncState()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cloudData, headers, status, getErr := a.virtualRepositorySyncRequest(ctx, http.MethodGet, nil)
	if getErr != nil && status != http.StatusNotFound {
		return "", getErr
	}
	if status == http.StatusOK && len(cloudData) > 0 {
		// The Hub returns a small status view for a user who has not synced yet.
		// It is intentionally not treated as a malformed document.
		var cloudStatus struct {
			HasDocument bool `json:"has_document"`
		}
		if json.Unmarshal(cloudData, &cloudStatus) == nil && !cloudStatus.HasDocument {
			status = http.StatusNotFound
		}
	}
	if status == http.StatusOK && len(cloudData) > 0 {
		var cloud virtualRepositorySyncPackage
		if err := json.Unmarshal(cloudData, &cloud); err != nil || cloud.Version != virtualRepositorySyncVersion {
			return "", errors.New("Hub returned invalid virtual repository sync data")
		}
		merged, conflicts := mergeVirtualRepositorySyncPackages(local, cloud, state.ItemHashes, resolutions)
		if len(conflicts) > 0 {
			result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "conflict", CloudRevision: headers.Get("X-Virtual-Repository-Sync-Revision"), Conflicts: conflicts, Message: "Virtual repository changes need review"})
			return string(result), nil
		}
		local = merged
	}
	// A local change can be saved while the Hub GET is in flight. Preserve those
	// newer tombstones in the outgoing document so a stale snapshot cannot
	// resurrect an item that was just deleted on this machine.
	if err := a.withVirtualRepositorySyncState(func(current *virtualRepositorySyncState) error {
		for key, deletedAt := range normalizedVirtualRepositorySyncTombstones(current.Tombstones) {
			if existing, ok := local.Tombstones[key]; !ok || deletedAt.After(existing) {
				local.Tombstones[key] = deletedAt
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	if a.virtualRepositorySyncGeneration.Load() != localGeneration {
		return "", errVirtualRepositoryChangedDuringSync
	}
	ifMatch := strings.TrimSpace(headers.Get("X-Virtual-Repository-Sync-Revision"))
	if status == http.StatusNotFound {
		// Make the first write create-only. Without this precondition, two
		// machines that both see an empty Hub document can race and the second
		// upload silently replaces the first instead of surfacing a conflict.
		ifMatch = "*"
	}
	data, _, putStatus, putErr := a.virtualRepositorySyncRequest(ctx, http.MethodPut, map[string]any{"payload": local, "if_match_revision": ifMatch})
	if putErr != nil {
		if putStatus == http.StatusConflict {
			result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "conflict", Message: "Cloud data changed while syncing; try again"})
			return string(result), nil
		}
		return "", putErr
	}
	if a.virtualRepositorySyncGeneration.Load() != localGeneration {
		// The Hub now has the earlier merged snapshot, but applying it locally
		// would erase the newer local save. Leave the local state intact; that
		// save has already queued a follow-up sync which will reconcile it with
		// the just-accepted Hub revision.
		return "", errVirtualRepositoryChangedDuringSync
	}
	// Apply only after the Hub has accepted the merged document. Applying first
	// makes a failed optimistic write mutate this machine even though the merged
	// view was never durably shared; a later retry can then treat that mutation as
	// a local user change and create a misleading conflict.
	if err := a.applyVirtualRepositorySyncPackage(local); err != nil {
		return "", err
	}
	var view struct {
		Revision string `json:"revision"`
	}
	_ = json.Unmarshal(data, &view)
	if err := a.withVirtualRepositorySyncState(func(next *virtualRepositorySyncState) error {
		// Keep tombstones created while the Hub request was in flight. The next
		// automatic run will include them instead of accidentally reviving data.
		for key, deletedAt := range next.Tombstones {
			if current, exists := local.Tombstones[key]; !exists || deletedAt.After(current) {
				local.Tombstones[key] = deletedAt
			}
		}
		next.CloudRevision = view.Revision
		next.ItemHashes = virtualRepositorySyncPackageHashes(local)
		next.LastSyncedAt = time.Now().UTC()
		next.Tombstones = local.Tombstones
		state = *next
		return nil
	}); err != nil {
		return "", err
	}
	result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "success", CloudRevision: view.Revision, LastSyncedAt: state.LastSyncedAt})
	return string(result), nil
}

func mergeVirtualRepositorySyncPackages(local, cloud virtualRepositorySyncPackage, base map[string]string, resolutions map[string]string) (virtualRepositorySyncPackage, []VirtualRepositorySyncConflict) {
	result := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{}, Credentials: map[string]virtualRepositorySyncCred{}, Bindings: map[string]string{}, SSHSecrets: map[string]string{}, Tombstones: map[string]time.Time{}}
	conflicts := []VirtualRepositorySyncConflict{}
	// A repository copy gets a new repository id. Keep this mapping so its
	// repository-scoped secrets and bindings can follow it after the respective
	// collections have been merged below.
	localRepositoryCopies := map[string]string{}
	local.Tombstones = normalizedVirtualRepositorySyncTombstones(local.Tombstones)
	cloud.Tombstones = normalizedVirtualRepositorySyncTombstones(cloud.Tombstones)
	merge := func(prefix, kind string, left, right map[string]any, put func(string, any), name func(any) string) {
		keys := map[string]struct{}{}
		for key := range left {
			keys[key] = struct{}{}
		}
		for key := range right {
			keys[key] = struct{}{}
		}
		for key := range keys {
			l, leftOK := left[key]
			r, rightOK := right[key]
			id := prefix + key
			baseHash := base[id]
			leftHash, rightHash := virtualRepositorySyncHash(l), virtualRepositorySyncHash(r)
			if !leftOK {
				put(key, r)
				continue
			}
			if !rightOK {
				put(key, l)
				continue
			}
			if leftHash == rightHash {
				put(key, l)
				continue
			}
			localChanged, cloudChanged := leftHash != baseHash, rightHash != baseHash
			if baseHash == "" || (localChanged && cloudChanged) {
				choice := resolutions[id]
				if choice == "local" {
					put(key, l)
					continue
				}
				if choice == "cloud" {
					put(key, r)
					continue
				}
				if choice == "copy" && kind == "repository" {
					clone := l.(virtualRepositorySyncRepo)
					clone.Repository.ID = clone.Repository.ID + "-local-" + uuid.NewString()[:8]
					clone.Repository.Name += " (local copy)"
					put(key, r)
					put(clone.Repository.ID, clone)
					localRepositoryCopies[key] = clone.Repository.ID
					continue
				}
				conflicts = append(conflicts, VirtualRepositorySyncConflict{ID: id, Kind: kind, Name: name(l)})
				continue
			}
			if localChanged {
				put(key, l)
			} else {
				put(key, r)
			}
		}
	}
	mergeDeleted := func(tombstoneKind, displayKind, prefix string, localItems, cloudItems map[string]any, name func(any) string) (map[string]any, map[string]any, map[string]time.Time) {
		leftOut, rightOut, deleted := map[string]any{}, map[string]any{}, map[string]time.Time{}
		for id, value := range localItems {
			leftOut[id] = value
		}
		for id, value := range cloudItems {
			rightOut[id] = value
		}
		ids := map[string]struct{}{}
		for id := range localItems {
			ids[id] = struct{}{}
		}
		for id := range cloudItems {
			ids[id] = struct{}{}
		}
		for key := range local.Tombstones {
			if strings.HasPrefix(key, prefix) {
				ids[strings.TrimPrefix(key, prefix)] = struct{}{}
			}
		}
		for key := range cloud.Tombstones {
			if strings.HasPrefix(key, prefix) {
				ids[strings.TrimPrefix(key, prefix)] = struct{}{}
			}
		}
		for id := range ids {
			key := virtualRepositorySyncTombstoneKey(tombstoneKind, id)
			localDeleted, cloudDeleted := local.Tombstones[key], cloud.Tombstones[key]
			deletedAt := localDeleted
			if cloudDeleted.After(deletedAt) {
				deletedAt = cloudDeleted
			}
			left, leftOK := localItems[id]
			right, rightOK := cloudItems[id]
			if deletedAt.IsZero() {
				continue
			}
			baseHash := base[prefix+id]
			localChanged := leftOK && (baseHash == "" || virtualRepositorySyncHash(left) != baseHash)
			cloudChanged := rightOK && (baseHash == "" || virtualRepositorySyncHash(right) != baseHash)
			// Content on the same side as its tombstone predates the delete. Content
			// from the other side is concurrent only if it changed after the base.
			concurrent := (localDeleted.IsZero() && localChanged) || (cloudDeleted.IsZero() && cloudChanged)
			if concurrent {
				choice := resolutions[prefix+id]
				if choice == "local" {
					if leftOK {
						delete(rightOut, id)
					} else {
						delete(leftOut, id)
						delete(rightOut, id)
						deleted[key] = deletedAt
					}
					continue
				}
				if choice == "cloud" {
					if rightOK {
						delete(leftOut, id)
					} else {
						delete(leftOut, id)
						delete(rightOut, id)
						deleted[key] = deletedAt
					}
					continue
				}
				label := id
				if leftOK {
					label = name(left)
				} else if rightOK {
					label = name(right)
				}
				conflicts = append(conflicts, VirtualRepositorySyncConflict{ID: prefix + id, Kind: displayKind + " deletion", Name: label})
				continue
			}
			delete(leftOut, id)
			delete(rightOut, id)
			deleted[key] = deletedAt
		}
		return leftOut, rightOut, deleted
	}
	localRepos, cloudRepos := map[string]any{}, map[string]any{}
	for k, v := range local.Repositories {
		localRepos[k] = v
	}
	for k, v := range cloud.Repositories {
		cloudRepos[k] = v
	}
	localRepos, cloudRepos, repoTombstones := mergeDeleted("repo", "repository", "repo:", localRepos, cloudRepos, func(v any) string { return v.(virtualRepositorySyncRepo).Repository.Name })
	for key, deletedAt := range repoTombstones {
		result.Tombstones[key] = deletedAt
	}
	merge("repo:", "repository", localRepos, cloudRepos, func(k string, v any) { result.Repositories[k] = v.(virtualRepositorySyncRepo) }, func(v any) string { return v.(virtualRepositorySyncRepo).Repository.Name })
	// Early sync clients only recorded a repository tombstone. A repository
	// deletion must also prevent its SSH password and node bindings from being
	// revived by an older device that still has them. Upgrade those legacy
	// tombstones before merging repository-scoped data, so the next upload
	// persists the complete deletion intent.
	for key, deletedAt := range result.Tombstones {
		if !strings.HasPrefix(key, "repo:") {
			continue
		}
		repositoryID := strings.TrimPrefix(key, "repo:")
		sshKey := virtualRepositorySyncTombstoneKey("ssh", repositoryID)
		if existing, ok := local.Tombstones[sshKey]; !ok || deletedAt.After(existing) {
			local.Tombstones[sshKey] = deletedAt
		}
		if existing, ok := cloud.Tombstones[sshKey]; !ok || deletedAt.After(existing) {
			cloud.Tombstones[sshKey] = deletedAt
		}
		for bindingKey := range local.Bindings {
			if strings.HasPrefix(bindingKey, repositoryID+":") {
				key := virtualRepositorySyncTombstoneKey("binding", bindingKey)
				if existing, ok := local.Tombstones[key]; !ok || deletedAt.After(existing) {
					local.Tombstones[key] = deletedAt
				}
				if existing, ok := cloud.Tombstones[key]; !ok || deletedAt.After(existing) {
					cloud.Tombstones[key] = deletedAt
				}
			}
		}
		for bindingKey := range cloud.Bindings {
			if strings.HasPrefix(bindingKey, repositoryID+":") {
				key := virtualRepositorySyncTombstoneKey("binding", bindingKey)
				if existing, ok := local.Tombstones[key]; !ok || deletedAt.After(existing) {
					local.Tombstones[key] = deletedAt
				}
				if existing, ok := cloud.Tombstones[key]; !ok || deletedAt.After(existing) {
					cloud.Tombstones[key] = deletedAt
				}
			}
		}
	}
	localCreds, cloudCreds := map[string]any{}, map[string]any{}
	for k, v := range local.Credentials {
		localCreds[k] = v
	}
	for k, v := range cloud.Credentials {
		cloudCreds[k] = v
	}
	localCreds, cloudCreds, credentialTombstones := mergeDeleted("cred", "credential", "cred:", localCreds, cloudCreds, func(v any) string { return v.(virtualRepositorySyncCred).Metadata.Name })
	for key, deletedAt := range credentialTombstones {
		result.Tombstones[key] = deletedAt
	}
	merge("cred:", "credential", localCreds, cloudCreds, func(k string, v any) { result.Credentials[k] = v.(virtualRepositorySyncCred) }, func(v any) string { return v.(virtualRepositorySyncCred).Metadata.Name })
	localSSH, cloudSSH := map[string]any{}, map[string]any{}
	for k, v := range local.SSHSecrets {
		localSSH[k] = v
	}
	for k, v := range cloud.SSHSecrets {
		cloudSSH[k] = v
	}
	localSSH, cloudSSH, sshTombstones := mergeDeleted("ssh", "SSH password", "ssh:", localSSH, cloudSSH, func(any) string { return "SSH password" })
	for key, deletedAt := range sshTombstones {
		result.Tombstones[key] = deletedAt
	}
	merge("ssh:", "ssh password", localSSH, cloudSSH, func(k string, v any) { result.SSHSecrets[k] = v.(string) }, func(any) string { return "SSH password" })
	localBindings, cloudBindings := map[string]any{}, map[string]any{}
	for k, v := range local.Bindings {
		localBindings[k] = v
	}
	for k, v := range cloud.Bindings {
		cloudBindings[k] = v
	}
	localBindings, cloudBindings, bindingTombstones := mergeDeleted("binding", "credential binding", "binding:", localBindings, cloudBindings, func(any) string { return "Credential binding" })
	for key, deletedAt := range bindingTombstones {
		result.Tombstones[key] = deletedAt
	}
	merge("binding:", "credential binding", localBindings, cloudBindings, func(k string, v any) { result.Bindings[k] = v.(string) }, func(any) string { return "Credential binding" })
	for sourceID, copyID := range localRepositoryCopies {
		if secret, exists := local.SSHSecrets[sourceID]; exists {
			result.SSHSecrets[copyID] = secret
		}
		prefix := sourceID + ":"
		for key, credentialID := range local.Bindings {
			if !strings.HasPrefix(key, prefix) || !result.Tombstones[virtualRepositorySyncTombstoneKey("binding", key)].IsZero() {
				continue
			}
			if _, credentialExists := result.Credentials[credentialID]; !credentialExists {
				continue
			}
			copiedKey := copyID + ":" + strings.TrimPrefix(key, prefix)
			result.Bindings[copiedKey] = credentialID
		}
	}
	return result, conflicts
}

func (a *App) applyVirtualRepositorySyncPackage(pkg virtualRepositorySyncPackage) error {
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	pkg.Tombstones = normalizedVirtualRepositorySyncTombstones(pkg.Tombstones)
	deleted := func(kind, id string) bool {
		_, ok := pkg.Tombstones[virtualRepositorySyncTombstoneKey(kind, id)]
		return ok
	}
	for _, credential := range pkg.Credentials {
		if deleted("cred", credential.Metadata.ID) {
			continue
		}
		if credential.Metadata.ID == "" {
			continue
		}
		if credential.Secret != "" {
			if err := keyring.Set(virtualRepositoryKeyringService, credential.Metadata.ID, credential.Secret); err != nil {
				return fmt.Errorf("import credential into system keyring: %w", err)
			}
		}
	}
	credentials := repositoryCredentialFile{Version: 1, Items: make([]RepositoryCredentialMetadata, 0, len(pkg.Credentials))}
	for _, credential := range pkg.Credentials {
		if deleted("cred", credential.Metadata.ID) {
			continue
		}
		credentials.Items = append(credentials.Items, credential.Metadata)
	}
	sort.Slice(credentials.Items, func(i, j int) bool { return credentials.Items[i].ID < credentials.Items[j].ID })
	if err := writeJSONFile(a.repositoryCredentialPath(), credentials); err != nil {
		return err
	}
	bindings := make(map[string]string, len(pkg.Bindings))
	for key, credentialID := range pkg.Bindings {
		if !deleted("binding", key) && !deleted("cred", credentialID) {
			bindings[key] = credentialID
		}
	}
	if err := writeJSONFile(a.repositoryCredentialBindingsPath(), repositoryCredentialBindingFile{Version: 1, Bindings: bindings}); err != nil {
		return err
	}
	for key := range pkg.Tombstones {
		if strings.HasPrefix(key, "cred:") {
			_ = keyring.Delete(virtualRepositoryKeyringService, strings.TrimPrefix(key, "cred:"))
		}
	}
	for id, secret := range pkg.SSHSecrets {
		if secret != "" {
			if err := keyring.Set(virtualRepositorySSHKeyringService, id, secret); err != nil {
				return fmt.Errorf("import SSH password into system keyring: %w", err)
			}
		}
	}
	index, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return err
	}
	if len(pkg.Tombstones) > 0 {
		next := index[:0]
		for _, item := range index {
			if deleted("repo", item.ID) {
				_ = keyring.Delete(virtualRepositorySSHKeyringService, item.ID)
				continue
			}
			next = append(next, item)
		}
		index = next
	}
	for key := range pkg.Tombstones {
		if strings.HasPrefix(key, "ssh:") {
			_ = keyring.Delete(virtualRepositorySSHKeyringService, strings.TrimPrefix(key, "ssh:"))
		}
	}
	for _, synced := range pkg.Repositories {
		repo := synced.Repository
		if strings.TrimSpace(repo.ID) == "" {
			continue
		}
		if synced.Location == "local" {
			root := filepath.Join(a.getMaclawBaseDir(), "virtual-repository-sync", repo.ID)
			if err := os.MkdirAll(root, 0o755); err != nil {
				return err
			}
			repo.RootPath = root
			repo.Remote = nil
			if err := writeVirtualRepository(&repo); err != nil {
				return err
			}
		}
		found := false
		for i := range index {
			if index[i].ID == repo.ID {
				index[i] = virtualRepositoryIndexEntry{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(repo.Remote), LastOpened: time.Now().UTC()}
				found = true
				break
			}
		}
		if !found {
			index = append(index, virtualRepositoryIndexEntry{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(repo.Remote), LastOpened: time.Now().UTC()})
		}
	}
	sort.Slice(index, func(i, j int) bool { return index[i].LastOpened.After(index[j].LastOpened) })
	return writeJSONFile(a.virtualRepositoryStatePath("virtual-repositories-index.json"), virtualRepositoryIndex{Version: 1, Items: index})
}

func (a *App) recordVirtualRepositorySyncTombstone(kind, id string) {
	key := virtualRepositorySyncTombstoneKey(kind, id)
	if key == ":" {
		return
	}
	if err := a.withVirtualRepositorySyncState(func(state *virtualRepositorySyncState) error {
		if state.Tombstones == nil {
			state.Tombstones = map[string]time.Time{}
		}
		state.Tombstones[key] = time.Now().UTC()
		return nil
	}); err == nil {
		a.virtualRepositorySyncGeneration.Add(1)
	}
}

// clearVirtualRepositorySyncTombstone records an explicit re-creation or
// update of a previously deleted item. Without this, reusing a stable ID (for
// example reopening a repository after removing it from Recent) would leave a
// deletion marker that wins the next merge.
func (a *App) clearVirtualRepositorySyncTombstone(kind, id string) {
	key := virtualRepositorySyncTombstoneKey(kind, id)
	if key == ":" {
		return
	}
	if err := a.withVirtualRepositorySyncState(func(state *virtualRepositorySyncState) error {
		if state.Tombstones != nil {
			delete(state.Tombstones, key)
		}
		return nil
	}); err == nil {
		a.virtualRepositorySyncGeneration.Add(1)
	}
}

const (
	virtualRepositorySyncInitialDelay  = 2 * time.Second
	virtualRepositorySyncMaxRetryDelay = 5 * time.Minute
)

// scheduleVirtualRepositorySync coalesces saves made close together. A failed
// automatic sync stays local and is retried with capped exponential backoff;
// conflicts are deliberately left for the user to resolve from the UI.
func (a *App) scheduleVirtualRepositorySync() {
	a.scheduleVirtualRepositorySyncAfter(virtualRepositorySyncInitialDelay, 0)
}

func (a *App) scheduleVirtualRepositorySyncAfter(delay time.Duration, attempt int) {
	if a == nil {
		return
	}
	a.virtualRepositorySyncScheduleMu.Lock()
	if a.virtualRepositorySyncTimer != nil {
		a.virtualRepositorySyncTimer.Stop()
	}
	a.virtualRepositorySyncTimer = time.AfterFunc(delay, func() {
		a.runScheduledVirtualRepositorySync(attempt)
	})
	a.virtualRepositorySyncScheduleMu.Unlock()
}

func (a *App) runScheduledVirtualRepositorySync(attempt int) {
	raw, err := a.SyncVirtualRepositories("")
	if err == nil {
		var result VirtualRepositorySyncResult
		if json.Unmarshal([]byte(raw), &result) != nil || result.Status == "" {
			err = errors.New("virtual repository sync returned an invalid result")
		} else if result.Status == "conflict" {
			log.Printf("[vrepo-sync] automatic sync needs conflict resolution")
			return
		} else if result.Status != "success" {
			err = fmt.Errorf("virtual repository sync returned status %q", result.Status)
		}
	}
	if err == nil {
		return
	}
	nextDelay := virtualRepositorySyncInitialDelay << min(attempt, 8)
	if nextDelay > virtualRepositorySyncMaxRetryDelay {
		nextDelay = virtualRepositorySyncMaxRetryDelay
	}
	log.Printf("[vrepo-sync] automatic sync failed; retrying in %s: %v", nextDelay, err)
	a.scheduleVirtualRepositorySyncAfter(nextDelay, attempt+1)
}
