package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/botcatalog"
)

// BotCatalogItem is the Wails view of one Skills/MCP catalog row for a bot.
type BotCatalogItem = botcatalog.Item

type botCatalogPaths struct {
	mcpsDir        string
	skillsLockPath string
	dataDir        string
}

var (
	botCatalogMu    sync.Mutex
	botCatalogStore *botcatalog.AssignmentStore
)

func (a *App) resolveBotCatalogPaths() botCatalogPaths {
	dataDir := ""
	if a != nil {
		dataDir = strings.TrimSpace(a.GetDataDir())
	}
	if dataDir == "" && a != nil && strings.TrimSpace(a.testHomeDir) != "" {
		dataDir = filepath.Join(a.testHomeDir, ".maclaw", "data")
	}
	mcpsDir := strings.TrimSpace(os.Getenv("MACLAW_BOT_CATALOG_MCPS"))
	skillsLock := strings.TrimSpace(os.Getenv("MACLAW_BOT_CATALOG_SKILLS_LOCK"))
	if a != nil {
		if v := strings.TrimSpace(a.botCatalogMcpsDir); v != "" {
			mcpsDir = v
		}
		if v := strings.TrimSpace(a.botCatalogSkillsLock); v != "" {
			skillsLock = v
		}
	}
	if mcpsDir == "" || skillsLock == "" {
		// Prefer cwd (dev checkout), then executable sibling.
		candidates := []string{"."}
		if exe, err := os.Executable(); err == nil {
			candidates = append(candidates, filepath.Dir(exe))
		}
		for _, root := range candidates {
			if mcpsDir == "" {
				p := filepath.Join(root, "mcps")
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					mcpsDir = p
				}
			}
			if skillsLock == "" {
				p := filepath.Join(root, "skills-lock.json")
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					skillsLock = p
				}
			}
		}
	}
	return botCatalogPaths{mcpsDir: mcpsDir, skillsLockPath: skillsLock, dataDir: dataDir}
}

func (a *App) botCatalogAssignmentStore() (*botcatalog.AssignmentStore, error) {
	paths := a.resolveBotCatalogPaths()
	if paths.dataDir == "" {
		return nil, fmt.Errorf("data dir is required")
	}
	botCatalogMu.Lock()
	defer botCatalogMu.Unlock()
	if botCatalogStore != nil {
		return botCatalogStore, nil
	}
	store, err := botcatalog.OpenAssignments(paths.dataDir)
	if err != nil {
		return nil, err
	}
	botCatalogStore = store
	return store, nil
}

// ListBotCatalog returns a browsable Skills/MCP directory for one bot.
func (a *App) ListBotCatalog(botID, query string) ([]BotCatalogItem, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil {
		return nil, err
	}
	paths := a.resolveBotCatalogPaths()
	return botcatalog.LoadCatalog(paths.mcpsDir, paths.skillsLockPath, query, store, botID)
}

// InstallBotCatalogItem one-click installs a catalog item for a bot.
func (a *App) InstallBotCatalogItem(botID, kind, id string) error {
	botID = strings.TrimSpace(botID)
	id = strings.TrimSpace(id)
	if botID == "" || id == "" {
		return fmt.Errorf("bot id and item id are required")
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil {
		return err
	}
	return store.Install(botID, botcatalog.Kind(strings.TrimSpace(kind)), id)
}

// UninstallBotCatalogItem one-click uninstalls a catalog item for a bot.
func (a *App) UninstallBotCatalogItem(botID, kind, id string) error {
	botID = strings.TrimSpace(botID)
	id = strings.TrimSpace(id)
	if botID == "" || id == "" {
		return fmt.Errorf("bot id and item id are required")
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil {
		return err
	}
	return store.Uninstall(botID, botcatalog.Kind(strings.TrimSpace(kind)), id)
}
