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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

const (
	virtualRepositorySyncVersion         = 1
	virtualRepositorySyncResponseMaxSize = 3 << 20
)

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

// virtualRepositorySyncRemoteRepairError preserves the repository identity
// while adding user-readable sync context. The background scheduler later
// exposes only the ID to the UI; the wrapped cause remains available for logs
// and retry classification.
type virtualRepositorySyncRemoteRepairError struct {
	RepositoryID string
	Cause        error
}

func (e *virtualRepositorySyncRemoteRepairError) Error() string {
	if e == nil || e.Cause == nil {
		return "remote virtual repository needs connection repair"
	}
	return e.Cause.Error()
}

func (e *virtualRepositorySyncRemoteRepairError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func virtualRepositorySyncRepairRepositoryID(err error) string {
	var repair *virtualRepositorySyncRemoteRepairError
	if errors.As(err, &repair) {
		return strings.TrimSpace(repair.RepositoryID)
	}
	return ""
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
	// Reason distinguishes conflict shapes for UI/background handling without
	// parsing free-form Message text. "revision_race" means Hub if-match lost
	// after the bounded retry budget; item conflicts leave Reason empty and
	// populate Conflicts instead.
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

const virtualRepositorySyncReasonRevisionRace = "revision_race"

func virtualRepositorySyncRevisionRaceResult() string {
	result, _ := json.Marshal(VirtualRepositorySyncResult{
		Status:  "conflict",
		Reason:  virtualRepositorySyncReasonRevisionRace,
		Message: "Cloud data changed while syncing; try again",
	})
	return string(result)
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

// virtualRepositorySyncPackagesEqual deliberately normalizes absent maps before
// comparing. Older Hub documents omit empty collections while local snapshots
// initialize them. Those two JSON shapes describe the same sync state and must
// not trigger a pointless PUT/revision race.
func virtualRepositorySyncPackagesEqual(left, right virtualRepositorySyncPackage) bool {
	normalize := func(pkg *virtualRepositorySyncPackage) {
		if pkg.Repositories == nil {
			pkg.Repositories = map[string]virtualRepositorySyncRepo{}
		}
		if pkg.Credentials == nil {
			pkg.Credentials = map[string]virtualRepositorySyncCred{}
		}
		if pkg.Bindings == nil {
			pkg.Bindings = map[string]string{}
		}
		if pkg.SSHSecrets == nil {
			pkg.SSHSecrets = map[string]string{}
		}
		if pkg.Tombstones == nil {
			pkg.Tombstones = map[string]time.Time{}
		}
	}
	normalize(&left)
	normalize(&right)
	return virtualRepositorySyncHash(left) == virtualRepositorySyncHash(right)
}

// validateVirtualRepositorySyncRepositories binds each map key to its embedded
// repository ID before merge, hashing, or application. Without this guard a
// malformed cloud document could be merged under one ID and then applied under
// another, which makes conflict and tombstone handling ambiguous.
func validateVirtualRepositorySyncRepositories(repositories map[string]virtualRepositorySyncRepo) error {
	for rawKey, synced := range repositories {
		key := strings.TrimSpace(rawKey)
		if rawKey != key || key == "" || key != synced.Repository.ID {
			return errors.New("synchronized repository key does not match its repository id")
		}
	}
	return nil
}

// validateVirtualRepositorySyncPackage validates every value that can reach a
// local state file or system keyring. The Hub document is encrypted, but it is
// still external input from this machine's point of view: validate it before a
// partially-applied package can replace credentials or repository metadata.
func validateVirtualRepositorySyncPackage(pkg virtualRepositorySyncPackage) error {
	if pkg.Version != virtualRepositorySyncVersion {
		return fmt.Errorf("unsupported virtual repository sync package version %d", pkg.Version)
	}
	if err := validateVirtualRepositorySyncRepositories(pkg.Repositories); err != nil {
		return err
	}
	for id, synced := range pkg.Repositories {
		repo := cloneVirtualRepository(&synced.Repository)
		switch synced.Location {
		case "local":
			if repo.RootPath != "" || repo.Remote != nil {
				return fmt.Errorf("synchronized local virtual repository %q is not portable", id)
			}
			if err := validatePortableLocalVirtualRepositoryDefinition(repo, id, repo.Name); err != nil {
				return fmt.Errorf("synchronized local virtual repository %q: %w", id, err)
			}
		case "remote":
			if err := validateVirtualRepository(repo); err != nil || repo.Remote == nil {
				if err == nil {
					err = errors.New("remote connection is required")
				}
				return fmt.Errorf("synchronized remote virtual repository %q: %w", id, err)
			}
		default:
			return fmt.Errorf("synchronized virtual repository %q has unsupported location %q", id, synced.Location)
		}
	}

	credentialFile := repositoryCredentialFile{Version: 1, Items: make([]RepositoryCredentialMetadata, 0, len(pkg.Credentials))}
	for rawID, credential := range pkg.Credentials {
		metadata := credential.Metadata
		if rawID == "" || metadata.ID != rawID {
			return errors.New("synchronized credential key does not match credential metadata")
		}
		if len(credential.Secret) > virtualRepositoryFieldMaxLength || strings.ContainsAny(credential.Secret, "\r\n\x00") {
			return errors.New("synchronized credential secret is invalid")
		}
		credentialFile.Items = append(credentialFile.Items, metadata)
	}
	if err := validateRepositoryCredentialFile(&credentialFile); err != nil {
		return fmt.Errorf("synchronized credentials: %w", err)
	}

	for key, credentialID := range pkg.Bindings {
		if key != strings.TrimSpace(key) || credentialID != strings.TrimSpace(credentialID) || key == "" || credentialID == "" || len(key) > virtualRepositoryFieldMaxLength || len(credentialID) > virtualRepositoryNameMaxLength || containsControlCharacter(key) || containsControlCharacter(credentialID) {
			return errors.New("synchronized credential binding is invalid")
		}
		repositoryID, nodeID, ok := strings.Cut(key, ":")
		if !ok || repositoryID == "" || nodeID == "" || strings.ContainsRune(repositoryID, ':') || strings.ContainsRune(nodeID, ':') || len(repositoryID) > virtualRepositoryNameMaxLength || len(nodeID) > virtualRepositoryNameMaxLength || containsControlCharacter(repositoryID) || containsControlCharacter(nodeID) {
			return errors.New("synchronized credential binding key is invalid")
		}
		if _, exists := pkg.Credentials[credentialID]; !exists {
			return errors.New("synchronized credential binding references a missing credential")
		}
	}
	for id, secret := range pkg.SSHSecrets {
		if id != strings.TrimSpace(id) || id == "" || len(id) > virtualRepositoryNameMaxLength || containsControlCharacter(id) || strings.ContainsRune(id, ':') || len(secret) > virtualRepositoryFieldMaxLength || strings.ContainsAny(secret, "\r\n\x00") {
			return errors.New("synchronized SSH secret is invalid")
		}
	}
	for rawKey, deletedAt := range pkg.Tombstones {
		key := strings.TrimSpace(rawKey)
		if rawKey != key || key == "" || deletedAt.IsZero() {
			return errors.New("synchronized deletion marker is invalid")
		}
		kind, id, qualified := strings.Cut(key, ":")
		if !qualified {
			kind, id = "repo", key // legacy, unqualified repository tombstone
		}
		switch kind {
		case "repo", "ssh":
			if id == "" || len(id) > virtualRepositoryNameMaxLength || containsControlCharacter(id) || strings.ContainsRune(id, ':') {
				return errors.New("synchronized deletion marker is invalid")
			}
		case "cred":
			if id == "" || id != strings.TrimSpace(id) || len(id) > virtualRepositoryNameMaxLength || containsControlCharacter(id) {
				return errors.New("synchronized deletion marker is invalid")
			}
		case "binding":
			repositoryID, nodeID, ok := strings.Cut(id, ":")
			if !ok || repositoryID == "" || nodeID == "" || strings.ContainsRune(repositoryID, ':') || strings.ContainsRune(nodeID, ':') || len(repositoryID) > virtualRepositoryNameMaxLength || len(nodeID) > virtualRepositoryNameMaxLength || containsControlCharacter(repositoryID) || containsControlCharacter(nodeID) {
				return errors.New("synchronized deletion marker is invalid")
			}
		default:
			return errors.New("synchronized deletion marker has an unsupported kind")
		}
	}
	return nil
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
	// Definition cache is only needed for remote SSH repositories: local roots
	// are read from disk (or already carry an unbound portable definition).
	needsDefinitionCache := false
	for _, item := range items {
		if item.Remote != nil {
			needsDefinitionCache = true
			break
		}
	}
	var definitionCache virtualRepositoryDefinitionCache
	definitionCacheDirty := false
	if needsDefinitionCache {
		definitionCache, _ = a.loadVirtualRepositoryDefinitionCache()
	}
	liveRemoteIDs := map[string]struct{}{}
	for _, item := range items {
		var repo *VirtualRepository
		err = nil
		if item.Remote != nil {
			// Never hold the process-wide repository lock while SSH/SFTP can block.
			repo, err = a.readRemoteVirtualRepository(item, "", false)
			if err != nil {
				if cached := cachedVirtualRepositoryDefinitionFrom(definitionCache, item.ID, item); cached != nil {
					log.Printf("[vrepo-sync] remote repository %q unreachable; using cached definition: %v", item.Name, err)
					repo, err = cached, nil
				}
			}
		} else if item.Unbound {
			// A local definition received from another machine intentionally has
			// no root until the user binds it here. Keep that portable definition
			// in the outgoing package; treating it as a missing repository would
			// block every later sync or make the cloud copy disappear.
			if item.Definition == nil {
				return pkg, fmt.Errorf("local virtual repository %q is missing its portable definition", item.Name)
			}
			repo = cloneVirtualRepository(item.Definition)
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
			return pkg, &virtualRepositorySyncRemoteRepairError{
				RepositoryID: item.ID,
				Cause:        fmt.Errorf("read virtual repository %q for sync: %w", item.Name, err),
			}
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
			liveRemoteIDs[portable.ID] = struct{}{}
			if storeVirtualRepositoryDefinitionInCache(&definitionCache, &portable) {
				definitionCacheDirty = true
			}
			secret, secretErr := virtualRepositorySyncOptionalSecret(virtualRepositorySSHKeyringService, portable.ID)
			if secretErr != nil {
				return pkg, secretErr
			}
			if secret != "" {
				pkg.SSHSecrets[portable.ID] = secret
			}
		}
	}
	if needsDefinitionCache {
		if pruneVirtualRepositoryDefinitionCache(&definitionCache, liveRemoteIDs) {
			definitionCacheDirty = true
		}
		if definitionCacheDirty {
			_ = writeJSONFile(a.virtualRepositoryDefinitionCachePath(), definitionCache)
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
	if err := validateVirtualRepositorySyncPackage(pkg); err != nil {
		return pkg, err
	}
	return pkg, nil
}

func (a *App) virtualRepositorySyncClient() (hubURL, token, machineID string, err error) {
	// One LoadConfig: getHubCredentials used to re-read the file just for the
	// machine id, which doubled I/O on every GET/PUT of a multi-attempt sync.
	cfg, loadErr := a.LoadConfig()
	if loadErr != nil {
		return "", "", "", fmt.Errorf("load config: %w", loadErr)
	}
	hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token = strings.TrimSpace(cfg.RemoteMachineToken)
	machineID = strings.TrimSpace(cfg.RemoteMachineID)
	if hubURL == "" {
		return "", "", "", fmt.Errorf("Hub URL not configured")
	}
	if token == "" {
		return "", "", "", fmt.Errorf("Hub token not configured")
	}
	if machineID == "" {
		return "", "", "", errors.New("machine id missing; register to Hub first")
	}
	return hubURL, token, machineID, nil
}

func (a *App) virtualRepositorySyncRequest(ctx context.Context, hubURL, token, machineID, method string, body any) ([]byte, http.Header, int, error) {
	var reader io.Reader
	if body != nil {
		raw, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, nil, 0, marshalErr
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(hubURL, "/")+"/api/virtual-repositories/sync", reader)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Cap the transport timeout to the remaining sync budget so a single hung
	// attempt cannot outlive the overall deadline and starve later retries.
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining <= 0 {
			return nil, nil, 0, ctx.Err()
		} else if remaining < timeout {
			timeout = remaining
		}
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, virtualRepositorySyncResponseMaxSize+1))
	if readErr != nil {
		return data, resp.Header, resp.StatusCode, fmt.Errorf("read Hub response: %w", readErr)
	}
	if len(data) > virtualRepositorySyncResponseMaxSize {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("Hub response exceeds %d byte limit", virtualRepositorySyncResponseMaxSize)
	}
	if resp.StatusCode >= 300 {
		return data, resp.Header, resp.StatusCode, fmt.Errorf("hub returned %d", resp.StatusCode)
	}
	return data, resp.Header, resp.StatusCode, nil
}

// SyncVirtualRepositories is safe for a manual button and the background retry
// loop. resolutions maps a conflict key (for example repo:vrepo_x) to local,
// cloud, or copy. Without a resolution, conflicting items are left untouched.
func (a *App) SyncVirtualRepositories(resolutionsJSON string) (string, error) {
	return a.syncVirtualRepositoriesFromRequest(resolutionsJSON, false)
}

// syncScheduledVirtualRepositories lets the queued background job enter the
// shared cloud lock while manual callers remain rejected for the duration of
// that job.
func (a *App) syncScheduledVirtualRepositories() (string, error) {
	return a.syncVirtualRepositoriesFromRequest("", true)
}

func (a *App) syncVirtualRepositoriesFromRequest(resolutionsJSON string, backgroundJob bool) (string, error) {
	resolutions := map[string]string{}
	if strings.TrimSpace(resolutionsJSON) != "" {
		if err := json.Unmarshal([]byte(resolutionsJSON), &resolutions); err != nil {
			return "", errors.New("invalid sync conflict resolution")
		}
	}
	// The UI disables its button while an automatic run is queued or active. Keep
	// the same rule in the backend as a final guard: an event can still be in
	// flight when the user clicks. Acquire the schedule mutex before the shared
	// cloud lock so a newly queued automatic run cannot slip in between this
	// check and the manual request taking the lock.
	//
	// Manual sync may cancel a failed-retry wait (or a short debounce queue) so
	// the user is never stuck behind a multi-minute backoff. An already-running
	// automatic job still returns busy.
	//
	// Background jobs deliberately do NOT consult IsVirtualRepositoryBackgroundSyncPending
	// here: runScheduled claims that slot before entering, so a self-check would
	// always return busy and spin forever on "in progress" retries.
	a.virtualRepositorySyncScheduleMu.Lock()
	publishAfterUnlock := false
	if !backgroundJob {
		a.cancelVirtualRepositorySyncTimerLocked()
		if a.IsVirtualRepositoryBackgroundSyncPending() {
			a.virtualRepositorySyncScheduleMu.Unlock()
			result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "busy", Message: "Automatic virtual repository synchronization is in progress"})
			return string(result), nil
		}
		// Clear retry_wait/failed so the UI drops the error banner as soon as the
		// user takes over, not only after the Hub round-trip finishes.
		a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseIdle, "", 0, time.Time{})
		publishAfterUnlock = true
	}
	virtualRepositoryCloudSyncMu.Lock()
	a.virtualRepositorySyncScheduleMu.Unlock()
	if publishAfterUnlock {
		a.publishVirtualRepositoryBackgroundSyncStatus()
	}
	defer virtualRepositoryCloudSyncMu.Unlock()
	return a.syncVirtualRepositories(resolutions)
}

// virtualRepositorySyncMaxStaleAttempts is the number of full GET→merge→PUT
// cycles allowed when Hub returns 409 (another device wrote between our read
// and write). One extra attempt is not enough under multi-device churn; a few
// short restarts usually converge without surfacing a false "user conflict".
const virtualRepositorySyncMaxStaleAttempts = 5

// virtualRepositorySyncOverallTimeout budgets the entire sync, including
// stale-revision restarts, so the process-wide cloud lock cannot be held for
// unbounded time under sustained 409s.
const virtualRepositorySyncOverallTimeout = 60 * time.Second

// virtualRepositorySyncStaleBackoff returns the delay before retrying a Hub
// revision race. Kept as a function so tests can observe the schedule without
// re-implementing the exponential steps.
func virtualRepositorySyncStaleBackoff(attempt int) time.Duration {
	return time.Duration(50*(1<<min(attempt, 3))) * time.Millisecond
}

func isVirtualRepositorySyncRevisionRace(result VirtualRepositorySyncResult) bool {
	return result.Status == "conflict" && (result.Reason == virtualRepositorySyncReasonRevisionRace || len(result.Conflicts) == 0)
}

// advanceVirtualRepositorySyncCheckpoint records a Hub revision only after the
// corresponding package is known to be durable in the Hub. It deliberately
// does not apply the package: callers whose local snapshot already equals the
// cloud document need a pure checkpoint repair, not another write to local
// repository, credential, or keyring state.
func (a *App) advanceVirtualRepositorySyncCheckpoint(local virtualRepositorySyncPackage, revision string) (virtualRepositorySyncState, error) {
	// The caller owns local, but its maps are also read by the merge and retry
	// paths. Keep checkpoint reconciliation copy-on-write so retaining a newer
	// tombstone cannot mutate the package after it has been accepted by the Hub.
	checkpoint := local
	checkpoint.Tombstones = make(map[string]time.Time, len(local.Tombstones))
	for key, deletedAt := range local.Tombstones {
		checkpoint.Tombstones[key] = deletedAt
	}
	state := virtualRepositorySyncState{}
	if err := a.withVirtualRepositorySyncState(func(next *virtualRepositorySyncState) error {
		// Keep tombstones created while the Hub request was in flight. The next
		// automatic run will include them instead of accidentally reviving data.
		for key, deletedAt := range next.Tombstones {
			if current, exists := checkpoint.Tombstones[key]; !exists || deletedAt.After(current) {
				checkpoint.Tombstones[key] = deletedAt
			}
		}
		next.CloudRevision = revision
		next.ItemHashes = virtualRepositorySyncPackageHashes(checkpoint)
		next.LastSyncedAt = time.Now().UTC()
		next.Tombstones = checkpoint.Tombstones
		state = *next
		return nil
	}); err != nil {
		return virtualRepositorySyncState{}, err
	}
	return state, nil
}

// finishVirtualRepositorySync applies a package only after the Hub accepted
// it, then advances the local checkpoint to that exact Hub revision.
func (a *App) finishVirtualRepositorySync(local virtualRepositorySyncPackage, revision string) (virtualRepositorySyncState, error) {
	if err := a.applyVirtualRepositorySyncPackage(local); err != nil {
		return virtualRepositorySyncState{}, err
	}
	// Applying a cloud package changes the local snapshot. Treat it as a new
	// generation so an overlapping save or queued sync can never publish a
	// checkpoint for the pre-apply snapshot.
	a.virtualRepositorySyncGeneration.Add(1)
	return a.advanceVirtualRepositorySyncCheckpoint(local, revision)
}

// syncVirtualRepositories retries a stale Hub revision a few times under one
// overall deadline. A conflict caused only by another device finishing its
// sync between our GET and PUT is not a user-data conflict: a fresh GET can
// merge both changes. Resolutions from an earlier interactive pass are kept so
// a concurrent write after the user chose local/cloud/copy does not force them
// to re-answer.
func (a *App) syncVirtualRepositories(resolutions map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), virtualRepositorySyncOverallTimeout)
	defer cancel()

	hubURL, token, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		return "", err
	}

	var lastRevisionRace bool
	for attempt := 0; attempt < virtualRepositorySyncMaxStaleAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastRevisionRace {
				return virtualRepositorySyncRevisionRaceResult(), nil
			}
			return "", err
		}
		// Capture generation before reading the local package. A save that
		// overlaps the snapshot or Hub request must force a new run, rather
		// than allowing the older package to overwrite the save during apply.
		localGeneration := a.virtualRepositorySyncGeneration.Load()
		local, err := a.snapshotVirtualRepositorySyncPackage()
		if err != nil {
			return "", err
		}
		state, err := a.loadVirtualRepositorySyncState()
		if err != nil {
			return "", err
		}
		cloudData, headers, status, getErr := a.virtualRepositorySyncRequest(ctx, hubURL, token, machineID, http.MethodGet, nil)
		if getErr != nil && status != http.StatusNotFound {
			return "", getErr
		}
		if status == http.StatusOK && len(cloudData) > 0 {
			// The Hub returns a small status view for a user who has not synced yet.
			// It is intentionally not treated as a malformed document.
			var cloudStatus struct {
				HasDocument *bool `json:"has_document"`
			}
			if json.Unmarshal(cloudData, &cloudStatus) == nil && cloudStatus.HasDocument != nil && !*cloudStatus.HasDocument {
				status = http.StatusNotFound
			}
		}
		var cloud, merged virtualRepositorySyncPackage
		if status == http.StatusOK && len(cloudData) > 0 {
			if err := json.Unmarshal(cloudData, &cloud); err != nil || cloud.Version != virtualRepositorySyncVersion {
				return "", errors.New("Hub returned invalid virtual repository sync data")
			}
			if err := validateVirtualRepositorySyncPackage(cloud); err != nil {
				return "", fmt.Errorf("Hub returned invalid virtual repository sync data: %w", err)
			}
			var conflicts []VirtualRepositorySyncConflict
			merged, conflicts = mergeVirtualRepositorySyncPackages(local, cloud, state.ItemHashes, resolutions)
			if len(conflicts) > 0 {
				result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "conflict", CloudRevision: headers.Get("X-Virtual-Repository-Sync-Revision"), Conflicts: conflicts, Message: "Virtual repository changes need review"})
				return string(result), nil
			}
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
		ifMatch := ""
		if headers != nil {
			ifMatch = strings.TrimSpace(headers.Get("X-Virtual-Repository-Sync-Revision"))
		}
		if status == http.StatusNotFound {
			// Make the first write create-only. Without this precondition, two
			// machines that both see an empty Hub document can race and the second
			// upload silently replaces the first instead of surfacing a conflict.
			ifMatch = "*"
		} else if ifMatch == "" {
			// A successful GET with a document but no revision cannot safely be
			// followed by an optimistic write. Treat this as an incompatible or
			// malformed Hub response instead of silently degrading to an
			// unconditional overwrite.
			return "", errors.New("Hub returned virtual repository sync data without a revision")
		}
		// A stale local checkpoint is not itself a reason to write.  This is
		// common after a crash between a successful Hub write and checkpoint
		// persistence, or after upgrading from an earlier sync implementation.
		// Writing an already-converged document only changes the revision race
		// window and can make one desktop retry forever against a busy Hub.
		if status == http.StatusOK && virtualRepositorySyncPackagesEqual(local, cloud) {
			if a.virtualRepositorySyncGeneration.Load() != localGeneration {
				return "", errVirtualRepositoryChangedDuringSync
			}
			state, err = a.advanceVirtualRepositorySyncCheckpoint(local, ifMatch)
			if err != nil {
				return "", err
			}
			result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "success", CloudRevision: ifMatch, LastSyncedAt: state.LastSyncedAt})
			return string(result), nil
		}
		if status == http.StatusOK {
			local = merged
		}
		data, _, putStatus, putErr := a.virtualRepositorySyncRequest(ctx, hubURL, token, machineID, http.MethodPut, map[string]any{"payload": local, "if_match_revision": ifMatch})
		if putErr != nil {
			if putStatus == http.StatusConflict {
				lastRevisionRace = true
				if attempt+1 >= virtualRepositorySyncMaxStaleAttempts {
					break
				}
				// Brief backoff reduces livelock when two desktops retry in lockstep.
				// Interruptible so an overall deadline (or cancelled parent) can exit.
				delay := virtualRepositorySyncStaleBackoff(attempt)
				log.Printf("[vrepo-sync] hub revision race on attempt %d/%d; retrying in %s", attempt+1, virtualRepositorySyncMaxStaleAttempts, delay)
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return virtualRepositorySyncRevisionRaceResult(), nil
				case <-timer.C:
				}
				continue
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
		var view struct {
			HasDocument *bool  `json:"has_document"`
			Revision    string `json:"revision"`
		}
		if err := json.Unmarshal(data, &view); err != nil {
			return "", errors.New("Hub returned an invalid virtual repository sync response")
		}
		// PUT success responses are status views, not sync documents. Require the
		// explicit acknowledgement promised by the Hub contract before applying
		// credentials or repository definitions locally. A revision by itself is
		// not enough: a proxy or an older incompatible endpoint could otherwise
		// make the client commit data it cannot prove was stored remotely.
		if view.HasDocument == nil || !*view.HasDocument {
			return "", errors.New("Hub accepted virtual repository sync without confirming the stored document")
		}
		view.Revision = strings.TrimSpace(view.Revision)
		if view.Revision == "" {
			return "", errors.New("Hub accepted virtual repository sync without returning a revision")
		}
		// Apply only after the Hub has accepted the merged document. Applying first
		// makes a failed optimistic write mutate this machine even though the merged
		// view was never durably shared; a later retry can then treat that mutation as
		// a local user change and create a misleading conflict.
		state, err = a.finishVirtualRepositorySync(local, view.Revision)
		if err != nil {
			return "", err
		}
		result, _ := json.Marshal(VirtualRepositorySyncResult{Status: "success", CloudRevision: view.Revision, LastSyncedAt: state.LastSyncedAt})
		return string(result), nil
	}
	// Empty Conflicts + reason=revision_race marks a pure Hub if-match race
	// (not an item-level merge conflict). The UI and background scheduler use
	// that to decide whether to prompt the user or simply retry later.
	return virtualRepositorySyncRevisionRaceResult(), nil
}

func mergeVirtualRepositorySyncPackages(local, cloud virtualRepositorySyncPackage, base map[string]string, resolutions map[string]string) (virtualRepositorySyncPackage, []VirtualRepositorySyncConflict) {
	// Callers validate complete packages before persisting or applying them. Keep
	// merge permissive enough for its value-only inputs (and the conflict UI's
	// partial test fixtures), while still protecting the identity invariant that
	// merge itself depends on.
	if err := validateVirtualRepositorySyncRepositories(local.Repositories); err != nil {
		return virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{}, Credentials: map[string]virtualRepositorySyncCred{}, Bindings: map[string]string{}, SSHSecrets: map[string]string{}, Tombstones: map[string]time.Time{}}, []VirtualRepositorySyncConflict{{Kind: "repository", Name: err.Error()}}
	}
	if err := validateVirtualRepositorySyncRepositories(cloud.Repositories); err != nil {
		return virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{}, Credentials: map[string]virtualRepositorySyncCred{}, Bindings: map[string]string{}, SSHSecrets: map[string]string{}, Tombstones: map[string]time.Time{}}, []VirtualRepositorySyncConflict{{Kind: "repository", Name: err.Error()}}
	}
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
	// A credential delete invalidates every binding that references it. The
	// binding merge runs after credential merge, so without this final cleanup a
	// concurrent binding update can survive with a credential ID that no longer
	// exists, making the merged package fail validation after the Hub has already
	// accepted it.
	for key, credentialID := range result.Bindings {
		if _, exists := result.Credentials[credentialID]; exists {
			continue
		}
		deletedAt := result.Tombstones[virtualRepositorySyncTombstoneKey("cred", credentialID)]
		if deletedAt.IsZero() {
			// merge accepts partial fixtures for conflict presentation. Full
			// packages are validated before this point, so only a proven delete
			// may remove a binding here.
			continue
		}
		delete(result.Bindings, key)
		tombstoneKey := virtualRepositorySyncTombstoneKey("binding", key)
		if current := result.Tombstones[tombstoneKey]; current.Before(deletedAt) {
			result.Tombstones[tombstoneKey] = deletedAt
		}
	}
	return result, conflicts
}

func (a *App) applyVirtualRepositorySyncPackage(pkg virtualRepositorySyncPackage) error {
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	if err := validateVirtualRepositorySyncPackage(pkg); err != nil {
		return err
	}
	pkg.Tombstones = normalizedVirtualRepositorySyncTombstones(pkg.Tombstones)
	// Validate all file-backed state before mutating credentials or keyrings.
	// Otherwise a corrupt local index would leave a cloud package only partly
	// applied (new credentials persisted, but repository definitions rejected).
	if _, err := a.loadVirtualRepositoryIndexItems(); err != nil {
		return fmt.Errorf("load local virtual repository index before sync apply: %w", err)
	}
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
		// A tombstone is authoritative even if an older client (or a partially
		// merged document) still carries the repository definition. The index was
		// pruned above; never re-add that same repository during application.
		if deleted("repo", repo.ID) {
			continue
		}
		location := strings.TrimSpace(synced.Location)
		if location != "local" && location != "remote" {
			return fmt.Errorf("synchronized virtual repository %q has unsupported location %q", repo.Name, synced.Location)
		}
		unbound := location == "local"
		if unbound {
			// Never manufacture a local root for a synchronized definition. A
			// path is a machine binding, not portable repository data. This is
			// especially important when the source used a Windows drive that
			// does not exist on this device.
			repo.RootPath = ""
			repo.Remote = nil
			if err := validatePortableLocalVirtualRepositoryDefinition(&repo, repo.ID, repo.Name); err != nil {
				return fmt.Errorf("synchronized local virtual repository %q: %w", repo.Name, err)
			}
		} else {
			if err := validateVirtualRepository(&repo); err != nil || repo.Remote == nil {
				if err == nil {
					err = errors.New("remote connection is required")
				}
				return fmt.Errorf("synchronized remote virtual repository %q: %w", repo.Name, err)
			}
		}
		found := false
		for i := range index {
			if index[i].ID == repo.ID {
				// Preserve a usable local root on this machine. Cloud definitions do
				// not carry paths, so a successful local binding must not be erased
				// by a later synchronization. The local manifest is intentionally
				// retained too: overwriting it after the Hub write would claim a sync
				// applied content that may conflict with local work. The next merge
				// compares its portable snapshot and surfaces any real conflict.
				//
				// A non-empty path alone is not usable: a removable disk or Windows
				// drive may be absent. In that case retain the incoming portable
				// definition as unbound, otherwise a later snapshot would fail while
				// trying to read the old path and block all synchronization.
				if unbound && index[i].Remote == nil && !index[i].Unbound && strings.TrimSpace(index[i].RootPath) != "" {
					if existing, readErr := readVirtualRepository(index[i].RootPath); readErr == nil && existing.ID == repo.ID {
						found = true
						break
					}
				}
				entry := virtualRepositoryIndexEntry{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(repo.Remote), Unbound: unbound, LastOpened: time.Now().UTC()}
				if unbound {
					entry.Definition = cloneVirtualRepository(&repo)
				}
				index[i] = entry
				found = true
				break
			}
		}
		if !found {
			entry := virtualRepositoryIndexEntry{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(repo.Remote), Unbound: unbound, LastOpened: time.Now().UTC()}
			if unbound {
				entry.Definition = cloneVirtualRepository(&repo)
			}
			index = append(index, entry)
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
		a.markVirtualRepositorySyncMutation()
	}
}

// markVirtualRepositorySyncMutation invalidates snapshots that may be in
// flight while a local repository, credential, or remote manifest is changed.
// It is intentionally independent of the checkpoint lock: callers must mark
// the mutation as soon as it becomes durable, before any later bookkeeping
// (such as updating the recent-repository index) can give a concurrent sync a
// chance to apply an older package.
func (a *App) markVirtualRepositorySyncMutation() {
	if a != nil {
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
		a.markVirtualRepositorySyncMutation()
	}
}

const (
	virtualRepositorySyncInitialDelay    = 2 * time.Second
	virtualRepositorySyncMaxRetryDelay   = 5 * time.Minute
	virtualRepositorySyncMaxAutoAttempts = 5

	virtualRepositorySyncPhaseIdle      = "idle"
	virtualRepositorySyncPhaseQueued    = "queued"
	virtualRepositorySyncPhaseRunning   = "running"
	virtualRepositorySyncPhaseRetryWait = "retry_wait"
	virtualRepositorySyncPhaseFailed    = "failed"
	virtualRepositorySyncPhaseConflict  = "conflict"
)

// VirtualRepositoryBackgroundSyncStatus is exposed to the desktop UI so it can
// distinguish an active run from a failed-retry wait (or a permanent failure)
// instead of showing "syncing" for the entire backoff window.
type VirtualRepositoryBackgroundSyncStatus struct {
	Pending            bool   `json:"pending"`
	Phase              string `json:"phase"`
	Message            string `json:"message,omitempty"`
	RepairRepositoryID string `json:"repair_repository_id,omitempty"`
	Attempt            int    `json:"attempt,omitempty"`
	NextRetryAt        string `json:"next_retry_at,omitempty"`
}

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
	a.cancelVirtualRepositorySyncTimerLocked()
	// Debounce after a local edit (and the startup catch-up) blocks the manual
	// button for a short window so the queued job cannot race a click.
	a.adjustVirtualRepositoryBackgroundSyncPending(1)
	a.virtualRepositorySyncTimerBlocksUI = true
	a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseQueued, "", attempt, time.Time{})
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		a.runScheduledVirtualRepositorySync(timer, attempt, true)
	})
	a.virtualRepositorySyncTimer = timer
	a.virtualRepositorySyncScheduleMu.Unlock()
	a.publishVirtualRepositoryBackgroundSyncStatus()
}

// scheduleVirtualRepositorySyncRetryAfter queues a failed automatic retry only
// when a newer save has not already scheduled a fresh sync. A retry must never
// replace a user edit's short-delay sync with its own (possibly five-minute)
// backoff timer. Retry waits do not set pending: the UI stays interactive and
// shows the last error instead of "syncing".
func (a *App) scheduleVirtualRepositorySyncRetryAfter(delay time.Duration, attempt int, lastErr error) {
	if a == nil {
		return
	}
	message := ""
	repairRepositoryID := ""
	if lastErr != nil {
		message = strings.TrimSpace(lastErr.Error())
		repairRepositoryID = virtualRepositorySyncRepairRepositoryID(lastErr)
	}
	if message == "" {
		message = "automatic virtual repository synchronization failed"
	}
	a.virtualRepositorySyncScheduleMu.Lock()
	if a.virtualRepositorySyncTimer != nil {
		// A newer local edit already owns the timer. Leave phase/pending alone;
		// the caller still releases its own pending slot and publishes.
		a.virtualRepositorySyncScheduleMu.Unlock()
		return
	}
	if attempt >= virtualRepositorySyncMaxAutoAttempts {
		a.virtualRepositorySyncTimerBlocksUI = false
		a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseFailed, message, attempt, time.Time{}, repairRepositoryID)
		a.virtualRepositorySyncScheduleMu.Unlock()
		log.Printf("[vrepo-sync] automatic sync failed permanently (attempt %d): %s", attempt, message)
		return
	}
	nextRetry := time.Now().UTC().Add(delay)
	a.virtualRepositorySyncTimerBlocksUI = false
	a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseRetryWait, message, attempt, nextRetry, repairRepositoryID)
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		a.runScheduledVirtualRepositorySync(timer, attempt, false)
	})
	a.virtualRepositorySyncTimer = timer
	a.virtualRepositorySyncScheduleMu.Unlock()
	log.Printf("[vrepo-sync] automatic sync failed; retrying in %s (attempt %d/%d): %s", delay, attempt+1, virtualRepositorySyncMaxAutoAttempts, message)
}

func (a *App) runScheduledVirtualRepositorySync(timer *time.Timer, attempt int, timerHeldPending bool) {
	if a == nil {
		return
	}
	// A timer may fire just before a later save replaces it. Stop then returns
	// false, so the replacement cannot remove this timer's pending count. The
	// callback owns that decrement even when it has become stale; otherwise the
	// UI can remain permanently disabled after that narrow timing window.
	pendingHeld := timerHeldPending
	// On failure, callers set retry_wait/failed first, then releasePending(true)
	// so the UI never sees pending=false while phase is still "running".
	releasePending := func(publish bool) {
		if pendingHeld {
			pendingHeld = false
			a.adjustVirtualRepositoryBackgroundSyncPending(-1)
			if publish {
				a.publishVirtualRepositoryBackgroundSyncStatus()
			}
		}
	}
	// A timer that was replaced just as it fired is no longer the active
	// background job. Do not let it sync or touch the replacement timer.
	a.virtualRepositorySyncScheduleMu.Lock()
	if a.virtualRepositorySyncTimer != timer {
		a.virtualRepositorySyncScheduleMu.Unlock()
		releasePending(true)
		return
	}
	a.virtualRepositorySyncTimer = nil
	a.virtualRepositorySyncTimerBlocksUI = false
	if !timerHeldPending {
		// Retry-wait timers never claimed a UI-blocking slot. Claim one for the
		// active run so a workspace opened mid-sync still sees pending=true.
		a.adjustVirtualRepositoryBackgroundSyncPending(1)
		pendingHeld = true
	}
	a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseRunning, "", attempt, time.Time{})
	a.virtualRepositorySyncScheduleMu.Unlock()
	a.publishVirtualRepositoryBackgroundSyncStatus()

	raw, err := a.syncScheduledVirtualRepositories()
	if err == nil {
		var result VirtualRepositorySyncResult
		if json.Unmarshal([]byte(raw), &result) != nil || result.Status == "" {
			err = errors.New("virtual repository sync returned an invalid result")
		} else if result.Status == "conflict" {
			// Item-level merge conflicts need the user. A bare revision race is
			// transient multi-device churn: keep retrying with the same backoff
			// as other automatic failures.
			if isVirtualRepositorySyncRevisionRace(result) {
				err = errors.New(result.Message)
				if strings.TrimSpace(err.Error()) == "" {
					err = errors.New("cloud data changed while syncing")
				}
			} else {
				message := strings.TrimSpace(result.Message)
				if message == "" {
					message = "Virtual repository changes need review"
				}
				log.Printf("[vrepo-sync] automatic sync needs conflict resolution")
				a.virtualRepositorySyncScheduleMu.Lock()
				// Prefer a newer local-edit queue over an older conflict banner.
				if a.virtualRepositorySyncTimer == nil {
					a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseConflict, message, attempt, time.Time{})
				}
				a.virtualRepositorySyncScheduleMu.Unlock()
				releasePending(true)
				return
			}
		} else if result.Status != "success" {
			err = fmt.Errorf("virtual repository sync returned status %q", result.Status)
		}
	}
	if err == nil {
		a.virtualRepositorySyncScheduleMu.Lock()
		// A local edit may have queued a replacement while this run held the
		// cloud lock. Do not clobber that queued phase with idle.
		if a.virtualRepositorySyncTimer == nil {
			a.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseIdle, "", 0, time.Time{})
		}
		a.virtualRepositorySyncScheduleMu.Unlock()
		releasePending(true)
		return
	}
	// Set retry_wait/failed before releasing the UI-blocking pending slot so a
	// status poll cannot observe the inconsistent window pending=false+phase=running.
	// scheduleRetry does not claim pending; releasePending(true) emits one coherent event.
	if isVirtualRepositorySyncPermanentError(err) {
		// Permanent configuration errors cannot recover by waiting.
		a.scheduleVirtualRepositorySyncRetryAfter(0, virtualRepositorySyncMaxAutoAttempts, err)
		releasePending(true)
		return
	}
	nextDelay := virtualRepositorySyncInitialDelay << min(attempt, 8)
	if nextDelay > virtualRepositorySyncMaxRetryDelay {
		nextDelay = virtualRepositorySyncMaxRetryDelay
	}
	a.scheduleVirtualRepositorySyncRetryAfter(nextDelay, attempt+1, err)
	releasePending(true)
}

// isVirtualRepositorySyncPermanentError reports configuration problems that
// will not fix themselves without user action. Automatic backoff only wastes
// UI attention and log noise for these.
func isVirtualRepositorySyncPermanentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "hub url not configured") ||
		strings.Contains(msg, "hub token not configured") ||
		strings.Contains(msg, "machine id missing") ||
		strings.Contains(msg, "register to hub first")
}

// cancelVirtualRepositorySyncTimerLocked stops a queued debounce or retry-wait
// timer. Callers must hold virtualRepositorySyncScheduleMu. Active runs are not
// cancelled here — only their pending timer is.
func (a *App) cancelVirtualRepositorySyncTimerLocked() {
	if a == nil || a.virtualRepositorySyncTimer == nil {
		return
	}
	stopped := a.virtualRepositorySyncTimer.Stop()
	if stopped && a.virtualRepositorySyncTimerBlocksUI {
		a.adjustVirtualRepositoryBackgroundSyncPending(-1)
	}
	// When Stop returns false the callback is already running (or finished) and
	// owns any pending slot it claimed. Leave the counter alone.
	a.virtualRepositorySyncTimer = nil
	a.virtualRepositorySyncTimerBlocksUI = false
}

func (a *App) setVirtualRepositoryBackgroundSyncPhaseLocked(phase, message string, attempt int, nextRetryAt time.Time, repairRepositoryID ...string) {
	if a == nil {
		return
	}
	if phase == "" {
		phase = virtualRepositorySyncPhaseIdle
	}
	a.virtualRepositorySyncPhase = phase
	a.virtualRepositorySyncStatusMessage = strings.TrimSpace(message)
	a.virtualRepositorySyncRepairRepositoryID = ""
	if len(repairRepositoryID) > 0 {
		a.virtualRepositorySyncRepairRepositoryID = strings.TrimSpace(repairRepositoryID[0])
	}
	a.virtualRepositorySyncAttempt = attempt
	a.virtualRepositorySyncNextRetryAt = nextRetryAt
}

// IsVirtualRepositoryBackgroundSyncPending reports whether an automatic sync is
// in the short debounce queue or currently running. Failed-retry waits return
// false so the manual Sync button stays usable.
func (a *App) IsVirtualRepositoryBackgroundSyncPending() bool {
	return a != nil && a.virtualRepositoryBackgroundSyncs.Load() > 0
}

// GetVirtualRepositoryBackgroundSyncStatus returns the structured background
// sync UI state (phase, last error, next retry). Prefer this over the boolean
// helper when the frontend needs to show why sync is idle-but-not-ok.
func (a *App) GetVirtualRepositoryBackgroundSyncStatus() string {
	status := a.virtualRepositoryBackgroundSyncStatusSnapshot()
	data, err := json.Marshal(status)
	if err != nil {
		return `{"pending":false,"phase":"idle"}`
	}
	return string(data)
}

func (a *App) virtualRepositoryBackgroundSyncStatusSnapshot() VirtualRepositoryBackgroundSyncStatus {
	if a == nil {
		return VirtualRepositoryBackgroundSyncStatus{Phase: virtualRepositorySyncPhaseIdle}
	}
	pending := a.virtualRepositoryBackgroundSyncs.Load() > 0
	a.virtualRepositorySyncScheduleMu.Lock()
	defer a.virtualRepositorySyncScheduleMu.Unlock()
	phase := strings.TrimSpace(a.virtualRepositorySyncPhase)
	if phase == "" {
		if pending {
			phase = virtualRepositorySyncPhaseRunning
		} else {
			phase = virtualRepositorySyncPhaseIdle
		}
	}
	// Keep the public snapshot coherent across the tiny window where phase has
	// already moved to retry_wait/failed but the run has not yet dropped pending.
	// UI-blocking phases always report pending; wait/fail/conflict never do.
	switch phase {
	case virtualRepositorySyncPhaseQueued, virtualRepositorySyncPhaseRunning:
		pending = true
	case virtualRepositorySyncPhaseRetryWait, virtualRepositorySyncPhaseFailed, virtualRepositorySyncPhaseConflict:
		pending = false
	}
	status := VirtualRepositoryBackgroundSyncStatus{
		Pending:            pending,
		Phase:              phase,
		Message:            a.virtualRepositorySyncStatusMessage,
		RepairRepositoryID: a.virtualRepositorySyncRepairRepositoryID,
		Attempt:            a.virtualRepositorySyncAttempt,
	}
	if !a.virtualRepositorySyncNextRetryAt.IsZero() {
		status.NextRetryAt = a.virtualRepositorySyncNextRetryAt.UTC().Format(time.RFC3339)
	}
	return status
}

func (a *App) adjustVirtualRepositoryBackgroundSyncPending(delta int32) {
	if a == nil || delta == 0 {
		return
	}
	for {
		current := a.virtualRepositoryBackgroundSyncs.Load()
		next := current + delta
		if next < 0 {
			next = 0
		}
		if a.virtualRepositoryBackgroundSyncs.CompareAndSwap(current, next) {
			return
		}
	}
}

func (a *App) publishVirtualRepositoryBackgroundSyncStatus() {
	if a == nil {
		return
	}
	status := a.virtualRepositoryBackgroundSyncStatusSnapshot()
	a.emitEvent("virtual-repository:background-sync", status)
}

// setVirtualRepositoryBackgroundSyncPending adjusts the UI-blocking counter and
// publishes status. Kept for tests and call sites that only touch the counter.
func (a *App) setVirtualRepositoryBackgroundSyncPending(delta int32) {
	a.adjustVirtualRepositoryBackgroundSyncPending(delta)
	a.publishVirtualRepositoryBackgroundSyncStatus()
}

// virtualRepositoryDefinitionCache remembers the last portable definition that
// was successfully snapshotted. Remote hosts that are temporarily offline can
// still participate in Hub sync without looking deleted.
type virtualRepositoryDefinitionCache struct {
	Version int                          `json:"version"`
	Items   map[string]VirtualRepository `json:"items"`
}

func (a *App) virtualRepositoryDefinitionCachePath() string {
	return a.virtualRepositoryStatePath("virtual-repository-definition-cache.json")
}

func (a *App) loadVirtualRepositoryDefinitionCache() (virtualRepositoryDefinitionCache, error) {
	cache := virtualRepositoryDefinitionCache{Version: 1, Items: map[string]VirtualRepository{}}
	if a == nil {
		return cache, nil
	}
	if err := readJSONFile(a.virtualRepositoryDefinitionCachePath(), &cache); err != nil {
		return cache, err
	}
	if cache.Version != 1 {
		return virtualRepositoryDefinitionCache{Version: 1, Items: map[string]VirtualRepository{}}, fmt.Errorf("unsupported virtual repository definition cache version %d", cache.Version)
	}
	if cache.Items == nil {
		cache.Items = map[string]VirtualRepository{}
	}
	return cache, nil
}

func (a *App) rememberVirtualRepositoryDefinition(repo *VirtualRepository) {
	if a == nil || repo == nil || strings.TrimSpace(repo.ID) == "" {
		return
	}
	// Best-effort single-entry write for callers outside snapshot (tests, open paths).
	cache, err := a.loadVirtualRepositoryDefinitionCache()
	if err != nil {
		cache = virtualRepositoryDefinitionCache{Version: 1, Items: map[string]VirtualRepository{}}
	}
	if !storeVirtualRepositoryDefinitionInCache(&cache, repo) {
		return
	}
	_ = writeJSONFile(a.virtualRepositoryDefinitionCachePath(), cache)
}

// storeVirtualRepositoryDefinitionInCache updates cache in memory. Returns true
// when the entry changed so callers can flush once per snapshot instead of per repo.
func storeVirtualRepositoryDefinitionInCache(cache *virtualRepositoryDefinitionCache, repo *VirtualRepository) bool {
	if cache == nil || repo == nil || strings.TrimSpace(repo.ID) == "" || repo.Remote == nil {
		return false
	}
	if cache.Items == nil {
		cache.Items = map[string]VirtualRepository{}
	}
	if cache.Version == 0 {
		cache.Version = 1
	}
	portable := *repo
	// Avoid rewriting identical entries (common when a remote falls back to cache).
	if existing, ok := cache.Items[portable.ID]; ok && reflect.DeepEqual(existing, portable) {
		return false
	}
	cache.Items[portable.ID] = portable
	return true
}

// pruneVirtualRepositoryDefinitionCache drops entries for repositories that are
// no longer in the local index so a long-lived machine does not retain secrets
// of deleted remotes forever. Returns true when the map changed.
func pruneVirtualRepositoryDefinitionCache(cache *virtualRepositoryDefinitionCache, liveRemoteIDs map[string]struct{}) bool {
	if cache == nil || len(cache.Items) == 0 {
		return false
	}
	changed := false
	for id := range cache.Items {
		if _, ok := liveRemoteIDs[id]; ok {
			continue
		}
		delete(cache.Items, id)
		changed = true
	}
	return changed
}

func (a *App) cachedVirtualRepositoryDefinition(id string, item virtualRepositoryIndexEntry) *VirtualRepository {
	if a == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	cache, err := a.loadVirtualRepositoryDefinitionCache()
	if err != nil {
		return nil
	}
	return cachedVirtualRepositoryDefinitionFrom(cache, id, item)
}

func cachedVirtualRepositoryDefinitionFrom(cache virtualRepositoryDefinitionCache, id string, item virtualRepositoryIndexEntry) *VirtualRepository {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	cached, ok := cache.Items[id]
	if !ok || strings.TrimSpace(cached.ID) == "" || cached.ID != id {
		return nil
	}
	// Reject a cache entry that does not match the index location identity so a
	// re-bound or re-hosted repository cannot publish stale structure.
	if item.Remote != nil {
		if cached.Remote == nil || remoteVirtualRepositoryHostID(cached.Remote) != remoteVirtualRepositoryHostID(item.Remote) {
			return nil
		}
		if strings.TrimSpace(item.RootPath) != "" && strings.TrimSpace(cached.RootPath) != "" && item.RootPath != cached.RootPath {
			return nil
		}
		if strings.TrimSpace(item.Name) != "" {
			cached.Name = item.Name
		}
		cached.Remote = cloneVirtualRepositoryRemote(item.Remote)
		if strings.TrimSpace(item.RootPath) != "" {
			cached.RootPath = item.RootPath
		}
	} else if cached.Remote != nil {
		return nil
	}
	return cloneVirtualRepository(&cached)
}
