package guiapp

// Virtual repository machine/root mappings. A repository is reachable through
// one or more (machine, root) pairs: device-private local roots and portable
// remote SSH endpoints. Mappings are machine coordinates, so they persist in
// the desktop index and (for remote SSH mappings only) the Hub sync package,
// never in the .vrepo manifest.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

type virtualRepositoryMappingRequest struct {
	RepositoryID string                   `json:"repository_id"`
	Mapping      VirtualRepositoryMapping `json:"mapping"`
	Password     string                   `json:"password,omitempty"`
	TrustHostKey bool                     `json:"trust_host_key,omitempty"`
}

type virtualRepositoryMappingConnectionRequest struct {
	RepositoryID string                   `json:"repository_id,omitempty"`
	MappingID    string                   `json:"mapping_id,omitempty"`
	Mapping      VirtualRepositoryMapping `json:"mapping"`
	Password     string                   `json:"password,omitempty"`
	TrustHostKey bool                     `json:"trust_host_key,omitempty"`
}

func validateVirtualRepositoryMappingRepositoryID(id string) (string, error) {
	id = strings.TrimSpace(id)
	// '/' is the composite-key separator (<repository id>/<mapping id>) used by
	// mapping-scoped keyring entries and sync tombstones.
	if id == "" || len(id) > virtualRepositoryNameMaxLength || containsControlCharacter(id) || strings.ContainsAny(id, `:/`) {
		return "", errors.New("virtual repository id is invalid")
	}
	return id, nil
}

func parseVirtualRepositoryMappingRequest(inputJSON string) (virtualRepositoryMappingRequest, error) {
	var request virtualRepositoryMappingRequest
	if err := unmarshalVirtualRepositoryInput(inputJSON, "virtual repository mapping", &request); err != nil {
		return request, err
	}
	id, err := validateVirtualRepositoryMappingRepositoryID(request.RepositoryID)
	if err != nil {
		return request, err
	}
	request.RepositoryID = id
	if len(request.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(request.Password, "\r\n\x00") {
		return request, errors.New("virtual repository mapping password is invalid")
	}
	return request, nil
}

// normalizeVirtualRepositoryMappingInput canonicalizes one client-supplied
// mapping and fills in a generated id and default label.
func normalizeVirtualRepositoryMappingInput(mapping *VirtualRepositoryMapping) error {
	mapping.ID = strings.TrimSpace(mapping.ID)
	if mapping.ID == "" {
		mapping.ID = newVirtualRepositoryMappingID()
	}
	mapping.Label = strings.TrimSpace(mapping.Label)
	mapping.Kind = strings.ToLower(strings.TrimSpace(mapping.Kind))
	if mapping.Label == "" {
		if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
			mapping.Label = strings.TrimSpace(mapping.Host)
		}
		if mapping.Label == "" && mapping.Kind == virtualRepositoryMappingKindLocal {
			mapping.Label = "Local"
		}
	}
	mapping.LastStatus, mapping.LastError, mapping.LastCheckedAt = "", "", nil
	canonical := []VirtualRepositoryMapping{*mapping}
	if err := validateVirtualRepositoryMappings(canonical); err != nil {
		return err
	}
	*mapping = canonical[0]
	return nil
}

// applyVirtualRepositoryMappingLocation re-points the index entry location
// (RootPath/Remote) at the given default mapping. This keeps the legacy
// per-entry location fields an exact mirror of the default mapping.
func applyVirtualRepositoryMappingLocation(item *virtualRepositoryIndexEntry, mapping VirtualRepositoryMapping) {
	item.RootPath = mapping.RootPath
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
		item.Remote = virtualRepositoryMappingRemote(&mapping)
	} else {
		item.Remote = nil
	}
	item.Unbound, item.Definition = false, nil
}

// ensureLocalVirtualRepositoryMappingRoot verifies that a local mapping root
// either already contains this repository's manifest or can be initialized
// from the current definition. It never initializes a non-empty directory.
func (a *App) ensureLocalVirtualRepositoryMappingRoot(item virtualRepositoryIndexEntry, root string) error {
	existing, readErr := readVirtualRepository(root)
	switch {
	case readErr == nil:
		if existing.ID != item.ID {
			return errors.New("the selected directory contains a different virtual repository")
		}
		return nil
	case os.IsNotExist(readErr):
		if _, rootErr := os.Stat(root); os.IsNotExist(rootErr) {
			if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
				return fmt.Errorf("create selected root directory: %w", mkErr)
			}
		}
		if _, manifestErr := os.Stat(virtualRepositoryManifestPath(root)); manifestErr != nil && !os.IsNotExist(manifestErr) {
			return fmt.Errorf("inspect selected root directory: %w", manifestErr)
		}
		entries, dirErr := os.ReadDir(root)
		if dirErr != nil {
			return fmt.Errorf("inspect selected root directory: %w", dirErr)
		}
		if !virtualRepositoryRootAllowsInitialization(entries) {
			return errors.New("the selected directory is not empty; choose an empty directory or one containing this virtual repository")
		}
		definition, defErr := a.virtualRepositoryDefinitionForNewLocalRoot(item, root)
		if defErr != nil {
			return defErr
		}
		if definition == nil {
			return errVirtualRepositoryMappingRootUninitialized
		}
		definition.RootPath, definition.Remote, definition.Mappings = root, nil, nil
		if err := writeVirtualRepository(definition); err != nil {
			return fmt.Errorf("initialize selected root directory: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("read selected root directory: %w", readErr)
	}
}

func virtualRepositoryRootAllowsInitialization(entries []os.DirEntry) bool {
	for _, entry := range entries {
		name := entry.Name()
		if name == virtualRepositoryDirName || virtualRepositoryBenignEmptyRootName(name) {
			continue
		}
		return false
	}
	return true
}

func virtualRepositoryBenignEmptyRootName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "desktop.ini", "thumbs.db", ".ds_store":
		return true
	default:
		return false
	}
}

func prepareLocalVirtualRepositoryMappingRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root != "" {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
				return "", fmt.Errorf("create selected root directory: %w", mkErr)
			}
		}
	}
	return cleanVirtualRepositoryRoot(root)
}

var errVirtualRepositoryMappingRootUninitialized = errors.New("the selected directory does not contain this virtual repository; bind it through the repository workspace first")

// virtualRepositoryDefinitionForNewLocalRoot finds a portable definition that
// can be written into an empty local mapping root. Remote-default repositories
// initialize from another local mapping, the definition cache, or a live SSH
// read — they must not require a prior BindVirtualRepositoryRoot.
func (a *App) virtualRepositoryDefinitionForNewLocalRoot(item virtualRepositoryIndexEntry, root string) (*VirtualRepository, error) {
	if item.Unbound && item.Definition != nil {
		return cloneVirtualRepository(item.Definition), nil
	}
	var lastLocalErr error
	tryLocal := func(path string) *VirtualRepository {
		if strings.TrimSpace(path) == "" || sameVirtualRepositoryPath(path, root) {
			return nil
		}
		current, err := readVirtualRepository(path)
		if err != nil {
			if lastLocalErr == nil {
				lastLocalErr = err
			}
			return nil
		}
		if current.ID != item.ID {
			return nil
		}
		return cloneVirtualRepository(current)
	}
	if item.Remote == nil {
		if definition := tryLocal(item.RootPath); definition != nil {
			return definition, nil
		}
	}
	for _, mapping := range virtualRepositoryIndexEntryMappings(item) {
		if mapping.Kind != virtualRepositoryMappingKindLocal {
			continue
		}
		if definition := tryLocal(mapping.RootPath); definition != nil {
			return definition, nil
		}
	}
	if cached := a.cachedVirtualRepositoryDefinition(item.ID, item); cached != nil {
		return cached, nil
	}
	if item.Remote != nil {
		mapping, mappingErr := resolveVirtualRepositoryMapping(item, "")
		view := item
		secretKey := item.ID
		if mappingErr == nil && mapping != nil && mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
			view = virtualRepositoryIndexEntryForMapping(item, *mapping)
			secretKey = virtualRepositoryMappingSecretKey(item.ID, mapping.ID)
		}
		repo, err := a.readRemoteVirtualRepositoryWithKey(view, secretKey, "", false)
		if err != nil {
			return nil, fmt.Errorf("cannot copy the remote virtual repository definition into the selected directory: %w", err)
		}
		if repo != nil && repo.ID == item.ID {
			a.rememberVirtualRepositoryDefinition(repo)
			return cloneVirtualRepository(repo), nil
		}
	}
	if lastLocalErr != nil {
		return nil, fmt.Errorf("read current local definition before initializing the new root: %w", lastLocalErr)
	}
	return nil, errVirtualRepositoryMappingRootUninitialized
}

// updateVirtualRepositoryEntryMappingsLocked runs one mapping mutation against
// the stored index entry and persists the index. Callers hold
// virtualRepositoryStateMu. touchLastOpened is false for background bookkeeping
// (for example connection-status write-back) that must not reorder the recent
// list. The returned list is the entry's mapping view after the mutation.
func (a *App) updateVirtualRepositoryEntryMappingsLocked(repositoryID string, touchLastOpened bool, mutate func(item *virtualRepositoryIndexEntry) error) ([]VirtualRepositoryMapping, error) {
	indexPath := a.virtualRepositoryStatePath("virtual-repositories-index.json")
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}
	if err := readJSONFile(indexPath, &index); err != nil {
		return nil, err
	}
	if err := validateVirtualRepositoryIndex(&index); err != nil {
		return nil, err
	}
	for i := range index.Items {
		if index.Items[i].ID != repositoryID {
			continue
		}
		item := &index.Items[i]
		// Resolve legacy locations into the mapping model before mutating so the
		// stored entry always carries the complete list afterwards.
		item.Mappings = virtualRepositoryIndexEntryMappings(*item)
		if err := mutate(item); err != nil {
			return nil, err
		}
		if err := validateVirtualRepositoryMappings(item.Mappings); err != nil {
			return nil, err
		}
		if touchLastOpened {
			item.LastOpened = time.Now().UTC()
		}
		if err := writeJSONFile(indexPath, index); err != nil {
			return nil, err
		}
		return cloneVirtualRepositoryMappings(item.Mappings), nil
	}
	return nil, errors.New("virtual repository was not found in recent repositories")
}

// verifyRemoteMappingForUnboundRepository gates the unbound→remote conversion.
// A synchronized portable definition must not be pinned to an unverified SSH
// endpoint: the remote host must answer and its manifest must carry the same
// repository id before the entry's portable definition is dropped.
func (a *App) verifyRemoteMappingForUnboundRepository(item virtualRepositoryIndexEntry, mapping VirtualRepositoryMapping, password string, trustHostKey bool) error {
	view := virtualRepositoryIndexEntryForMapping(item, mapping)
	repo, err := a.readRemoteVirtualRepositoryWithKey(view, virtualRepositoryMappingSecretKey(item.ID, mapping.ID), password, trustHostKey)
	if err != nil {
		return fmt.Errorf("verify the remote manifest before replacing the portable definition (test and trust the connection first): %w", err)
	}
	if repo.ID != item.ID {
		return errors.New("the remote root contains a different virtual repository")
	}
	return nil
}

// mirrorDefaultRemoteMappingPassword keeps the mapping-scoped keyring entry of
// the default remote mapping in step with the legacy repository-scoped key
// written by SaveRemoteVirtualRepository and its repair flow, matching the
// dual-write behavior of the mapping Add/Update/Test paths.
func (a *App) mirrorDefaultRemoteMappingPassword(repositoryID, password string) {
	if strings.TrimSpace(password) == "" {
		return
	}
	items, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return
	}
	for _, item := range items {
		if item.ID != repositoryID {
			continue
		}
		for _, mapping := range virtualRepositoryIndexEntryMappings(item) {
			if !mapping.IsDefault || mapping.Kind != virtualRepositoryMappingKindRemoteSSH {
				continue
			}
			if scopedKey := virtualRepositoryMappingSecretKey(repositoryID, mapping.ID); scopedKey != repositoryID {
				if err := keyring.Set(virtualRepositorySSHKeyringService, scopedKey, password); err != nil {
					log.Printf("[vrepo] mirror default mapping password to scoped key failed for repo %q: %v", repositoryID, err)
				}
			}
		}
		return
	}
}

func marshalVirtualRepositoryMappings(mappings []VirtualRepositoryMapping) (string, error) {
	if mappings == nil {
		mappings = []VirtualRepositoryMapping{}
	}
	data, err := json.Marshal(mappings)
	return string(data), err
}

// resolveVirtualRepositoryMapping picks the target mapping for an execution
// entry point: the requested mapping, or the default when mappingID is empty.
// An unbound repository has no legacy location; its stored mappings (for
// example synchronized remote SSH endpoints) are still resolvable.
func resolveVirtualRepositoryMapping(item virtualRepositoryIndexEntry, mappingID string) (*VirtualRepositoryMapping, error) {
	mappings := virtualRepositoryIndexEntryMappings(item)
	mappingID = strings.TrimSpace(mappingID)
	if mappingID != "" {
		mapping := virtualRepositoryMappingByID(mappings, mappingID)
		if mapping == nil {
			return nil, errors.New("virtual repository mapping was not found")
		}
		return mapping, nil
	}
	if mapping := virtualRepositoryDefaultMapping(mappings); mapping != nil {
		return mapping, nil
	}
	return nil, errors.New("virtual repository has no machine mapping on this device; bind a local root or add a remote mapping first")
}

// ListVirtualRepositoryMappings returns this device's mappings for one
// repository. Local mappings report a live root check; remote SSH mappings
// report the persisted result of the last explicit connection test (listing
// must never fan out SSH connections).
func (a *App) ListVirtualRepositoryMappings(repositoryID string) (string, error) {
	id, err := validateVirtualRepositoryMappingRepositoryID(repositoryID)
	if err != nil {
		return "", err
	}
	item, err := a.virtualRepositoryIndexEntryByID(id)
	if err != nil {
		return "", err
	}
	mappings := virtualRepositoryIndexEntryMappings(item)
	now := time.Now().UTC()
	for i := range mappings {
		if mappings[i].Kind != virtualRepositoryMappingKindLocal {
			continue
		}
		if info, statErr := os.Stat(mappings[i].RootPath); statErr == nil && info.IsDir() {
			mappings[i].LastStatus, mappings[i].LastError, mappings[i].LastCheckedAt = "ok", "", &now
		} else {
			mappings[i].LastStatus = "error"
			if statErr != nil {
				mappings[i].LastError = statErr.Error()
			} else {
				mappings[i].LastError = "root path is not a directory"
			}
			mappings[i].LastCheckedAt = &now
		}
	}
	return marshalVirtualRepositoryMappings(mappings)
}

// AddVirtualRepositoryMapping attaches a new (machine, root) pair to a
// repository. An empty (or missing) local directory is initialized from the
// current definition; remote SSH mappings store their password in the system
// keyring when one is supplied.
func (a *App) AddVirtualRepositoryMapping(inputJSON string) (string, error) {
	request, err := parseVirtualRepositoryMappingRequest(inputJSON)
	if err != nil {
		return "", err
	}
	mapping := request.Mapping
	if err := normalizeVirtualRepositoryMappingInput(&mapping); err != nil {
		return "", err
	}
	if mapping.Kind == virtualRepositoryMappingKindLocal {
		root, err := prepareLocalVirtualRepositoryMappingRoot(mapping.RootPath)
		if err != nil {
			return "", fmt.Errorf("open selected root directory: %w", err)
		}
		mapping.RootPath = root
	}

	item, err := a.virtualRepositoryIndexEntryByID(request.RepositoryID)
	if err != nil {
		return "", err
	}
	if mapping.Kind == virtualRepositoryMappingKindLocal {
		if item.Unbound {
			return "", errors.New("bind the repository to a local root with BindVirtualRepositoryRoot instead")
		}
		if err := a.ensureLocalVirtualRepositoryMappingRoot(item, mapping.RootPath); err != nil {
			return "", err
		}
	}
	becomesDefault := len(virtualRepositoryIndexEntryMappings(item)) == 0 || mapping.IsDefault
	if item.Unbound && mapping.Kind == virtualRepositoryMappingKindRemoteSSH && becomesDefault {
		// The new mapping would replace the portable definition as the entry
		// location. Verify the endpoint and its manifest before dropping it.
		if err := a.verifyRemoteMappingForUnboundRepository(item, mapping, request.Password, request.TrustHostKey); err != nil {
			return "", err
		}
	}
	// Store the password before the index references the mapping: a keyring
	// failure must not leave a saved mapping that cannot be dialed.
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH && strings.TrimSpace(request.Password) != "" {
		if err := keyring.Set(virtualRepositorySSHKeyringService, virtualRepositoryMappingSecretKey(item.ID, mapping.ID), request.Password); err != nil {
			return "", fmt.Errorf("save SSH password in system keyring: %w", err)
		}
		if becomesDefault {
			if err := keyring.Set(virtualRepositorySSHKeyringService, item.ID, request.Password); err != nil {
				return "", fmt.Errorf("save SSH password in system keyring: %w", err)
			}
		}
	}

	virtualRepositoryStateMu.Lock()
	mappings, err := a.updateVirtualRepositoryEntryMappingsLocked(request.RepositoryID, true, func(entry *virtualRepositoryIndexEntry) error {
		if virtualRepositoryMappingByID(entry.Mappings, mapping.ID) != nil {
			return errors.New("virtual repository mapping id already exists")
		}
		if len(entry.Mappings) == 0 {
			mapping.IsDefault = true
		}
		if mapping.IsDefault {
			for i := range entry.Mappings {
				entry.Mappings[i].IsDefault = false
			}
			applyVirtualRepositoryMappingLocation(entry, mapping)
		}
		entry.Mappings = append(entry.Mappings, mapping)
		return nil
	})
	virtualRepositoryStateMu.Unlock()
	if err != nil {
		return "", err
	}
	a.markVirtualRepositorySyncMutation()
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
		a.scheduleVirtualRepositorySync()
	}
	return marshalVirtualRepositoryMappings(mappings)
}

// UpdateVirtualRepositoryMapping edits the label and coordinates of one stored
// mapping. Changing the coordinates of the default mapping moves the
// repository location and therefore still requires MigrateVirtualRepositoryRoot.
func (a *App) UpdateVirtualRepositoryMapping(inputJSON string) (string, error) {
	request, err := parseVirtualRepositoryMappingRequest(inputJSON)
	if err != nil {
		return "", err
	}
	mapping := request.Mapping
	mapping.ID = strings.TrimSpace(mapping.ID)
	if mapping.ID == "" {
		return "", errors.New("virtual repository mapping id is required")
	}
	if err := normalizeVirtualRepositoryMappingInput(&mapping); err != nil {
		return "", err
	}
	if mapping.Kind == virtualRepositoryMappingKindLocal {
		root, err := prepareLocalVirtualRepositoryMappingRoot(mapping.RootPath)
		if err != nil {
			return "", fmt.Errorf("open selected root directory: %w", err)
		}
		mapping.RootPath = root
	}

	virtualRepositoryStateMu.Lock()
	item, err := a.virtualRepositoryIndexEntryByID(request.RepositoryID)
	if err != nil {
		virtualRepositoryStateMu.Unlock()
		return "", err
	}
	storedMapping := virtualRepositoryMappingByID(virtualRepositoryIndexEntryMappings(item), mapping.ID)
	if mapping.Kind == virtualRepositoryMappingKindLocal {
		if storedMapping == nil {
			virtualRepositoryStateMu.Unlock()
			return "", errors.New("virtual repository mapping was not found")
		}
		if !sameVirtualRepositoryPath(storedMapping.RootPath, mapping.RootPath) {
			if item.Unbound {
				virtualRepositoryStateMu.Unlock()
				return "", errors.New("bind the repository to a local root with BindVirtualRepositoryRoot instead")
			}
			if err := a.ensureLocalVirtualRepositoryMappingRoot(item, mapping.RootPath); err != nil {
				virtualRepositoryStateMu.Unlock()
				return "", err
			}
		}
	}
	virtualRepositoryStateMu.Unlock()
	// Store the password before the index references the updated mapping: a
	// keyring failure must not leave a saved mapping that cannot be dialed. A
	// default mapping's password is mirrored to the legacy repository key.
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH && strings.TrimSpace(request.Password) != "" {
		if storedMapping == nil {
			return "", errors.New("virtual repository mapping was not found")
		}
		effectiveDefault := storedMapping.IsDefault || mapping.IsDefault
		if err := keyring.Set(virtualRepositorySSHKeyringService, virtualRepositoryMappingSecretKey(item.ID, mapping.ID), request.Password); err != nil {
			return "", fmt.Errorf("save SSH password in system keyring: %w", err)
		}
		if effectiveDefault {
			if err := keyring.Set(virtualRepositorySSHKeyringService, item.ID, request.Password); err != nil {
				return "", fmt.Errorf("save SSH password in system keyring: %w", err)
			}
		}
	}
	virtualRepositoryStateMu.Lock()
	mappings, err := a.updateVirtualRepositoryEntryMappingsLocked(request.RepositoryID, true, func(entry *virtualRepositoryIndexEntry) error {
		stored := virtualRepositoryMappingByID(entry.Mappings, mapping.ID)
		if stored == nil {
			return errors.New("virtual repository mapping was not found")
		}
		if stored.Kind != mapping.Kind {
			return errors.New("changing a mapping's kind is not supported; remove it and add a new mapping")
		}
		if stored.IsDefault {
			// The default mapping mirrors the entry location. Moving it is a root
			// migration, and it cannot lose its default flag through an edit.
			locationChanged := stored.RootPath != mapping.RootPath
			if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
				locationChanged = locationChanged || remoteVirtualRepositoryHostID(virtualRepositoryMappingRemote(stored)) != remoteVirtualRepositoryHostID(virtualRepositoryMappingRemote(&mapping)) || stored.User != mapping.User
			}
			if locationChanged {
				return errors.New("changing the default mapping's location requires MigrateVirtualRepositoryRoot")
			}
			mapping.IsDefault = true
		}
		mapping.LastStatus, mapping.LastError, mapping.LastCheckedAt = stored.LastStatus, stored.LastError, stored.LastCheckedAt
		if mapping.IsDefault {
			for i := range entry.Mappings {
				entry.Mappings[i].IsDefault = false
			}
			applyVirtualRepositoryMappingLocation(entry, mapping)
		}
		*stored = mapping
		return nil
	})
	virtualRepositoryStateMu.Unlock()
	if err != nil {
		return "", err
	}
	a.markVirtualRepositorySyncMutation()
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
		a.scheduleVirtualRepositorySync()
	}
	return marshalVirtualRepositoryMappings(mappings)
}

// RemoveVirtualRepositoryMapping detaches one mapping. Removing the default
// mapping promotes the first remaining mapping and re-mirrors the entry
// location; removing the only mapping is rejected (delete the repository or
// unbind it instead).
func (a *App) RemoveVirtualRepositoryMapping(repositoryID, mappingID string) error {
	id, err := validateVirtualRepositoryMappingRepositoryID(repositoryID)
	if err != nil {
		return err
	}
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" || len(mappingID) > virtualRepositoryNameMaxLength || containsControlCharacter(mappingID) || strings.ContainsAny(mappingID, `:/`) {
		return errors.New("virtual repository mapping id is invalid")
	}
	virtualRepositoryStateMu.Lock()
	var removed *VirtualRepositoryMapping
	var promoted *VirtualRepositoryMapping
	_, err = a.updateVirtualRepositoryEntryMappingsLocked(id, true, func(entry *virtualRepositoryIndexEntry) error {
		target := virtualRepositoryMappingByID(entry.Mappings, mappingID)
		if target == nil {
			return errors.New("virtual repository mapping was not found")
		}
		if len(entry.Mappings) == 1 {
			return errors.New("cannot remove the only mapping of a virtual repository")
		}
		removedCopy := *target
		removed = &removedCopy
		remaining := make([]VirtualRepositoryMapping, 0, len(entry.Mappings)-1)
		for _, m := range entry.Mappings {
			if m.ID != mappingID {
				remaining = append(remaining, m)
			}
		}
		entry.Mappings = remaining
		if removed.IsDefault {
			entry.Mappings[0].IsDefault = true
			applyVirtualRepositoryMappingLocation(entry, entry.Mappings[0])
			promotedCopy := entry.Mappings[0]
			promoted = &promotedCopy
		}
		return nil
	})
	virtualRepositoryStateMu.Unlock()
	if err != nil {
		return err
	}
	if removed != nil && removed.Kind == virtualRepositoryMappingKindRemoteSSH {
		a.recordVirtualRepositorySyncTombstone("vmap", id+"/"+mappingID)
		if secretKey := virtualRepositoryMappingSecretKey(id, mappingID); secretKey != id {
			_ = keyring.Delete(virtualRepositorySSHKeyringService, secretKey)
			a.recordVirtualRepositorySyncTombstone("ssh", secretKey)
		}
		a.scheduleVirtualRepositorySync()
	}
	if removed != nil && removed.IsDefault {
		// The removed mapping was the entry location, so the bare repository key
		// held its SSH password. Re-point that key at the promoted default (or
		// drop it) so no dial inherits a password from a detached endpoint.
		if promoted != nil && promoted.Kind == virtualRepositoryMappingKindRemoteSSH {
			if scopedKey := virtualRepositoryMappingSecretKey(id, promoted.ID); scopedKey != id {
				if secret, getErr := keyring.Get(virtualRepositorySSHKeyringService, scopedKey); getErr == nil && secret != "" {
					if err := keyring.Set(virtualRepositorySSHKeyringService, id, secret); err != nil {
						return fmt.Errorf("mirror promoted mapping password in system keyring: %w", err)
					}
				} else {
					_ = keyring.Delete(virtualRepositorySSHKeyringService, id)
				}
			}
		} else {
			_ = keyring.Delete(virtualRepositorySSHKeyringService, id)
			a.recordVirtualRepositorySyncTombstone("ssh", id)
		}
	}
	return nil
}

// SetDefaultVirtualRepositoryMapping marks one mapping as the repository's
// default and re-mirrors the entry location. For a remote SSH default the
// mapping's keyring password is mirrored to the legacy repository-scoped key
// so the existing dial paths keep working; when the new default has no stored
// password, the stale repository-scoped secret is deleted instead of being
// left pointing at the previous endpoint.
func (a *App) SetDefaultVirtualRepositoryMapping(repositoryID, mappingID string) (string, error) {
	id, err := validateVirtualRepositoryMappingRepositoryID(repositoryID)
	if err != nil {
		return "", err
	}
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" {
		return "", errors.New("virtual repository mapping id is required")
	}
	item, err := a.virtualRepositoryIndexEntryByID(id)
	if err != nil {
		return "", err
	}
	target := virtualRepositoryMappingByID(virtualRepositoryIndexEntryMappings(item), mappingID)
	if target == nil {
		return "", errors.New("virtual repository mapping was not found")
	}
	if item.Unbound {
		if target.Kind == virtualRepositoryMappingKindLocal {
			return "", errors.New("bind the repository to the local root with BindVirtualRepositoryRoot instead")
		}
		// The conversion drops the portable definition; verify the endpoint and
		// its manifest first.
		if err := a.verifyRemoteMappingForUnboundRepository(item, *target, "", false); err != nil {
			return "", err
		}
	}
	// Adjust keyring material before the index flips the default, so a keyring
	// failure never leaves the repository pointing at a default it cannot dial.
	if target.Kind == virtualRepositoryMappingKindRemoteSSH {
		if scopedKey := virtualRepositoryMappingSecretKey(id, mappingID); scopedKey != id {
			if secret, getErr := keyring.Get(virtualRepositorySSHKeyringService, scopedKey); getErr == nil && secret != "" {
				if err := keyring.Set(virtualRepositorySSHKeyringService, id, secret); err != nil {
					return "", fmt.Errorf("mirror SSH password to repository keyring key: %w", err)
				}
			} else {
				_ = keyring.Delete(virtualRepositorySSHKeyringService, id)
			}
		}
	} else {
		_ = keyring.Delete(virtualRepositorySSHKeyringService, id)
	}
	virtualRepositoryStateMu.Lock()
	mappings, err := a.updateVirtualRepositoryEntryMappingsLocked(id, true, func(entry *virtualRepositoryIndexEntry) error {
		stored := virtualRepositoryMappingByID(entry.Mappings, mappingID)
		if stored == nil {
			return errors.New("virtual repository mapping was not found")
		}
		for i := range entry.Mappings {
			entry.Mappings[i].IsDefault = entry.Mappings[i].ID == mappingID
		}
		applyVirtualRepositoryMappingLocation(entry, *stored)
		return nil
	})
	virtualRepositoryStateMu.Unlock()
	if err != nil {
		return "", err
	}
	a.markVirtualRepositorySyncMutation()
	if target.Kind == virtualRepositoryMappingKindRemoteSSH {
		a.scheduleVirtualRepositorySync()
	}
	return marshalVirtualRepositoryMappings(mappings)
}

// TestVirtualRepositoryMappingConnection probes one mapping. Remote SSH
// mappings reuse the standard connection test (host-key pinning and keyring
// password flow included); local mappings verify that the root directory
// exists. A successful remote test with a supplied password stores it under
// the mapping's keyring key. The outcome is recorded on the stored mapping.
func (a *App) TestVirtualRepositoryMappingConnection(inputJSON string) (string, error) {
	var request virtualRepositoryMappingConnectionRequest
	if err := unmarshalVirtualRepositoryInput(inputJSON, "virtual repository mapping connection", &request); err != nil {
		return "", err
	}
	if request.RepositoryID != "" {
		id, err := validateVirtualRepositoryMappingRepositoryID(request.RepositoryID)
		if err != nil {
			return "", err
		}
		request.RepositoryID = id
	}
	if len(request.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(request.Password, "\r\n\x00") {
		return "", errors.New("virtual repository mapping password is invalid")
	}
	mapping := request.Mapping
	if err := normalizeVirtualRepositoryMappingInput(&mapping); err != nil {
		return "", err
	}
	mappingID := strings.TrimSpace(request.MappingID)
	if mappingID == "" {
		mappingID = mapping.ID
	}

	var status RemoteVirtualRepositoryConnectionStatus
	if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
		secretKey := virtualRepositoryMappingSecretKey(request.RepositoryID, mappingID)
		connectionInput := remoteVirtualRepositoryConnectionInput{
			RepositoryID: secretKey,
			Remote:       virtualRepositoryMappingRemote(&mapping),
			RootPath:     mapping.RootPath,
			Password:     request.Password,
			TrustHostKey: request.TrustHostKey,
		}
		raw, marshalErr := json.Marshal(connectionInput)
		if marshalErr != nil {
			return "", marshalErr
		}
		statusJSON, testErr := a.TestRemoteVirtualRepositoryConnection(string(raw))
		if testErr != nil {
			return "", testErr
		}
		if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
			return "", fmt.Errorf("decode remote connection status: %w", err)
		}
		if request.RepositoryID != "" && strings.TrimSpace(request.Password) != "" && status.Connected && status.HostKeyTrusted && status.RootExists && status.ErrorCode == "" {
			if err := keyring.Set(virtualRepositorySSHKeyringService, secretKey, request.Password); err != nil {
				return "", fmt.Errorf("save SSH password in system keyring: %w", err)
			}
			if secretKey != request.RepositoryID && a.virtualRepositoryMappingIsDefault(request.RepositoryID, mappingID) {
				if err := keyring.Set(virtualRepositorySSHKeyringService, request.RepositoryID, request.Password); err != nil {
					return "", fmt.Errorf("save SSH password in system keyring: %w", err)
				}
			}
		}
	} else {
		if _, err := cleanVirtualRepositoryRoot(mapping.RootPath); err != nil {
			status.ErrorCode, status.Error = "root_not_found", err.Error()
		} else {
			status.Connected, status.RootExists = true, true
		}
	}
	if request.RepositoryID != "" && mappingID != "" {
		a.recordVirtualRepositoryMappingStatus(request.RepositoryID, mappingID, status)
	}
	data, err := json.Marshal(status)
	return string(data), err
}

func (a *App) virtualRepositoryMappingIsDefault(repositoryID, mappingID string) bool {
	item, err := a.virtualRepositoryIndexEntryByID(repositoryID)
	if err != nil {
		return false
	}
	mapping := virtualRepositoryMappingByID(virtualRepositoryIndexEntryMappings(item), mappingID)
	return mapping != nil && mapping.IsDefault
}

// recordVirtualRepositoryMappingStatus persists the last connection-test
// outcome on a stored mapping. It is best-effort: a repository that was
// removed concurrently simply has nothing to update.
func (a *App) recordVirtualRepositoryMappingStatus(repositoryID, mappingID string, status RemoteVirtualRepositoryConnectionStatus) {
	lastStatus, lastError := "ok", ""
	if !status.Connected || !status.RootExists || status.ErrorCode != "" {
		lastStatus = "error"
		lastError = status.Error
	}
	now := time.Now().UTC()
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	_, _ = a.updateVirtualRepositoryEntryMappingsLocked(repositoryID, false, func(entry *virtualRepositoryIndexEntry) error {
		mapping := virtualRepositoryMappingByID(entry.Mappings, mappingID)
		if mapping == nil {
			return errors.New("virtual repository mapping was not found")
		}
		mapping.LastStatus, mapping.LastError, mapping.LastCheckedAt = lastStatus, lastError, &now
		return nil
	})
}
