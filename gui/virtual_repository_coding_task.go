package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/sync/singleflight"
)

var virtualRepositoryCodingTaskLaunches singleflight.Group

// VirtualRepositoryCodingTaskLaunch is returned to the frontend after a
// virtual repository has been converted into an armed pure-coding task.
type VirtualRepositoryCodingTaskLaunch struct {
	ProjectPath string `json:"project_path"`
	TaskTitle   string `json:"task_title"`
	AgentMode   string `json:"agent_mode"`
	RemoteHost  string `json:"remote_host,omitempty"`
}

// StartVirtualRepositoryCodingTask creates a task-management record and arms
// the corresponding local or remote coding environment. repositoryID is used
// for both modes so callers never get to supply an arbitrary execution path.
func (a *App) StartVirtualRepositoryCodingTask(repositoryID string) (VirtualRepositoryCodingTaskLaunch, error) {
	id := strings.TrimSpace(repositoryID)
	if id == "" {
		return VirtualRepositoryCodingTaskLaunch{}, errors.New("virtual repository ID is required")
	}
	// Wails requests are not guaranteed to come from one frontend instance.
	// Coalesce same-repository launches in the backend as the final guard against
	// duplicated task records and parallel SSH handshakes.
	launchKey := fmt.Sprintf("%p:%s", a, id)
	result, err, _ := virtualRepositoryCodingTaskLaunches.Do(launchKey, func() (any, error) {
		return a.startVirtualRepositoryCodingTask(id)
	})
	if err != nil {
		return VirtualRepositoryCodingTaskLaunch{}, err
	}
	return result.(VirtualRepositoryCodingTaskLaunch), nil
}

func (a *App) startVirtualRepositoryCodingTask(repositoryID string) (VirtualRepositoryCodingTaskLaunch, error) {
	item, err := a.virtualRepositoryIndexEntryByID(repositoryID)
	if err != nil {
		return VirtualRepositoryCodingTaskLaunch{}, err
	}
	var repo *VirtualRepository
	if item.Remote == nil {
		repo, err = readVirtualRepository(item.RootPath)
		if err != nil {
			return VirtualRepositoryCodingTaskLaunch{}, fmt.Errorf("open local virtual repository: %w", err)
		}
		if repo.ID != item.ID {
			return VirtualRepositoryCodingTaskLaunch{}, errors.New("virtual repository index no longer matches its manifest")
		}
	} else {
		// Check local trust material before touching the network. This makes a
		// missing-password or untrusted-host launch fail immediately and ensures
		// no task record can be created during an incomplete connection setup.
		password, passwordErr := keyring.Get(virtualRepositorySSHKeyringService, item.ID)
		if passwordErr != nil || strings.TrimSpace(password) == "" {
			return VirtualRepositoryCodingTaskLaunch{}, errors.New("SSH password is unavailable; open the virtual repository connection settings and save the password again")
		}
		knownHosts, knownHostsErr := a.loadVirtualRepositoryKnownHosts()
		if knownHostsErr != nil {
			return VirtualRepositoryCodingTaskLaunch{}, fmt.Errorf("load trusted SSH host key: %w", knownHostsErr)
		}
		fingerprint := knownHosts.Hosts[remoteVirtualRepositoryHostID(item.Remote)]
		if strings.TrimSpace(fingerprint) == "" {
			return VirtualRepositoryCodingTaskLaunch{}, errors.New("SSH host key is not trusted; test and trust the virtual repository connection first")
		}
		repo, err = a.readRemoteVirtualRepository(item, password, false)
		if err != nil {
			return VirtualRepositoryCodingTaskLaunch{}, fmt.Errorf("open remote virtual repository: %w", err)
		}
		return a.startRemoteVirtualRepositoryCodingTask(repo, password, fingerprint)
	}
	title := strings.TrimSpace(repo.Name)
	if title == "" {
		title = "Virtual repository"
	}
	if repo.Remote == nil {
		created := a.CreateTaskWithMode(title, repo.RootPath, "coding_dev")
		if strings.TrimSpace(created.ProjectPath) == "" {
			return VirtualRepositoryCodingTaskLaunch{}, errors.New("create local coding task failed")
		}
		if err := a.PrepareLocalCodingEnvironment(created.ProjectPath, repo.RootPath); err != nil {
			a.HideTask(created.ProjectPath)
			return VirtualRepositoryCodingTaskLaunch{}, fmt.Errorf("prepare local coding environment: %w", err)
		}
		return VirtualRepositoryCodingTaskLaunch{ProjectPath: created.ProjectPath, TaskTitle: title, AgentMode: "coding_dev"}, nil
	}
	return VirtualRepositoryCodingTaskLaunch{}, errors.New("invalid virtual repository location")
}

func (a *App) startRemoteVirtualRepositoryCodingTask(repo *VirtualRepository, password, fingerprint string) (VirtualRepositoryCodingTaskLaunch, error) {
	title := strings.TrimSpace(repo.Name)
	if title == "" {
		title = "Virtual repository"
	}
	created := a.CreateRemoteCodingTask(title, repo.Remote.Host, repo.Remote.User, repo.RootPath, repo.Remote.Port)
	if strings.TrimSpace(created.ProjectPath) == "" {
		return VirtualRepositoryCodingTaskLaunch{}, errors.New("create remote coding task failed")
	}
	if err := a.prepareRemoteCodingEnvironment(created.ProjectPath, repo.Remote.Host, repo.Remote.User, password, repo.RootPath, repo.Remote.Port, fingerprint); err != nil {
		return VirtualRepositoryCodingTaskLaunch{}, fmt.Errorf("prepare remote coding environment: %w", err)
	}
	return VirtualRepositoryCodingTaskLaunch{ProjectPath: created.ProjectPath, TaskTitle: title, AgentMode: "remote_coding_dev", RemoteHost: repo.Remote.Host}, nil
}

func (a *App) virtualRepositoryByIDForCodingTask(repositoryID string) (*VirtualRepository, error) {
	item, err := a.virtualRepositoryIndexEntryByID(repositoryID)
	if err != nil {
		return nil, err
	}
	if item.Remote != nil {
		return a.readRemoteVirtualRepository(item, "", false)
	}
	repo, err := readVirtualRepository(item.RootPath)
	if err != nil {
		return nil, fmt.Errorf("open local virtual repository: %w", err)
	}
	if repo.ID != item.ID {
		return nil, errors.New("virtual repository index no longer matches its manifest")
	}
	return repo, nil
}

func (a *App) virtualRepositoryIndexEntryByID(repositoryID string) (virtualRepositoryIndexEntry, error) {
	id := strings.TrimSpace(repositoryID)
	if id == "" {
		return virtualRepositoryIndexEntry{}, errors.New("virtual repository ID is required")
	}
	indexItems, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return virtualRepositoryIndexEntry{}, err
	}
	for _, item := range indexItems {
		if item.ID == id {
			return item, nil
		}
	}
	return virtualRepositoryIndexEntry{}, errors.New("virtual repository was not found in the recent list")
}
