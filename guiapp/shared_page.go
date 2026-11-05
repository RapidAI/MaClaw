package guiapp

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/sharedpage"
)

// SharedPage is the Wails view of a co-editable output page.
type SharedPage = sharedpage.Page

var (
	sharedPageMu    sync.Mutex
	sharedPageStore *sharedpage.Store
)

func (a *App) sharedPages() (*sharedpage.Store, error) {
	dataDir := ""
	if a != nil {
		dataDir = strings.TrimSpace(a.GetDataDir())
		if dataDir == "" && strings.TrimSpace(a.testHomeDir) != "" {
			dataDir = a.testHomeDir
		}
	}
	if dataDir == "" {
		return nil, fmt.Errorf("data dir is required")
	}
	sharedPageMu.Lock()
	defer sharedPageMu.Unlock()
	if sharedPageStore != nil {
		return sharedPageStore, nil
	}
	store, err := sharedpage.Open(dataDir)
	if err != nil {
		return nil, err
	}
	sharedPageStore = store
	return sharedPageStore, nil
}

// CreateSharedPage starts a versioned shared output page.
func (a *App) CreateSharedPage(title, groupID, actor string) (*SharedPage, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	p, err := store.Create(title, groupID, actor)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetSharedPage returns one shared page.
func (a *App) GetSharedPage(id string) (*SharedPage, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	p, err := store.Get(id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListSharedPages lists shared pages (optional group filter).
func (a *App) ListSharedPages(groupID string) ([]SharedPage, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	return store.List(groupID), nil
}

// AppendSharedPageSection appends a section (user or bot actor).
func (a *App) AppendSharedPageSection(pageID, title, body, actor string) (*SharedPage, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	p, err := store.AppendSection(pageID, title, body, actor)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// EditSharedPageSection edits a section.
func (a *App) EditSharedPageSection(pageID, sectionID, title, body, actor string) (*SharedPage, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	p, err := store.EditSection(pageID, sectionID, title, body, actor)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteSharedPage removes a page.
func (a *App) DeleteSharedPage(id string) error {
	store, err := a.sharedPages()
	if err != nil {
		return err
	}
	return store.Delete(id)
}

// ListSharedPageTools returns tool descriptors bots can use for shared pages.
func (a *App) ListSharedPageTools() ([]map[string]any, error) {
	return sharedpage.ToolSpecs(), nil
}

// CallSharedPageTool executes a bot tool against the shared page store.
func (a *App) CallSharedPageTool(name string, args map[string]any) (any, error) {
	store, err := a.sharedPages()
	if err != nil {
		return nil, err
	}
	actor := sharedPageStringArg(args, "actor")
	if actor == "" {
		actor = "bot"
	}
	switch strings.TrimSpace(name) {
	case "shared_page_list":
		return store.List(sharedPageStringArg(args, "group_id")), nil
	case "shared_page_read":
		return store.Get(sharedPageStringArg(args, "page_id"))
	case "shared_page_append":
		return store.AppendSection(sharedPageStringArg(args, "page_id"), sharedPageStringArg(args, "title"), sharedPageStringArg(args, "body"), actor)
	case "shared_page_edit":
		return store.EditSection(sharedPageStringArg(args, "page_id"), sharedPageStringArg(args, "section_id"), sharedPageStringArg(args, "title"), sharedPageStringArg(args, "body"), actor)
	default:
		return nil, fmt.Errorf("unknown shared page tool %q", name)
	}
}

func sharedPageStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
