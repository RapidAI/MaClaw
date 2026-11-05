package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/sharedpage"
)

// registerSharedPageTools exposes shared_page_* in the live ToolRegistry so
// agent loops can call them like any other builtin tool.
func registerSharedPageTools(registry *ToolRegistry, app *App) {
	if registry == nil || app == nil {
		return
	}
	for _, spec := range sharedpage.ToolSpecs() {
		name, _ := spec["name"].(string)
		desc, _ := spec["description"].(string)
		params, _ := spec["parameters"].(map[string]any)
		props := map[string]interface{}{}
		var required []string
		if params != nil {
			if rawProps, ok := params["properties"].(map[string]any); ok {
				for k, v := range rawProps {
					props[k] = v
				}
			}
			if rawReq, ok := params["required"].([]string); ok {
				required = append(required, rawReq...)
			} else if rawReq, ok := params["required"].([]any); ok {
				for _, r := range rawReq {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
			}
		}
		toolName := name
		_ = registry.Register(RegisteredTool{
			Name:        toolName,
			Description: desc,
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"shared_page", "collaboration"},
			Priority:    0,
			Status:      RegToolAvailable,
			InputSchema: props,
			Required:    required,
			Source:      "builtin",
			Handler: func(args map[string]interface{}) string {
				return executeSharedPageTool(app, toolName, args)
			},
		})
	}
}

func executeSharedPageTool(app *App, name string, args map[string]interface{}) string {
	if app == nil {
		return "shared page tools unavailable"
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	out, err := app.CallSharedPageTool(name, args)
	if err != nil {
		return "error: " + err.Error()
	}
	// Best-effort Hub sync for mutations (append/edit) using existing report path.
	switch strings.TrimSpace(name) {
	case "shared_page_append", "shared_page_edit":
		_ = app.syncSharedPageToHub(sharedPageStringArg(args, "page_id"), sharedPageStringArg(args, "actor"))
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprint(out)
	}
	return string(raw)
}

// syncSharedPageToHub posts a compact page snapshot as a Hub report so other
// bots/devices can observe shared-page changes without a new Hub endpoint.
func (a *App) syncSharedPageToHub(pageID, actor string) error {
	pageID = strings.TrimSpace(pageID)
	if a == nil || pageID == "" {
		return nil
	}
	page, err := a.GetSharedPage(pageID)
	if err != nil || page == nil {
		return err
	}
	from := strings.TrimSpace(actor)
	if from == "" || from == "bot" || from == "router" || from == "user" {
		from = "main"
	}
	snapshot, _ := json.Marshal(map[string]any{
		"page_id":   page.ID,
		"title":     page.Title,
		"group_id":  page.GroupID,
		"version":   page.Version,
		"sections":  len(page.Sections),
		"updated_at": page.UpdatedAt,
	})
	_, err = a.postDesktopBotReport(from, "main", "shared_page", string(snapshot), "", nil)
	return err
}

// SyncSharedPageToHub is the Wails/manual entry for Hub snapshot sync.
func (a *App) SyncSharedPageToHub(pageID, actor string) error {
	return a.syncSharedPageToHub(pageID, actor)
}
