package main

import (
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/project"
)

func (h *IMMessageHandler) toolProjectManage(args map[string]interface{}) string {
	actionText, _ := args["action"].(string)
	action := normalizeProjectToolAction(actionText)
	switch action {
	case projectToolActionCreate:
		name, _ := args["name"].(string)
		path, _ := args["path"].(string)
		res, err := project.Create(h.app, name, path)
		if err != nil {
			return fmt.Sprintf("创建项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": projectToolStatusCreated.String()})
		return string(data)
	case projectToolActionList:
		items, err := project.List(h.app)
		if err != nil {
			return fmt.Sprintf("加载配置失败: %v", err)
		}
		if len(items) == 0 {
			return "当前没有已配置的项目。请在桌面端添加项目。"
		}
		data, _ := json.Marshal(items)
		return string(data)
	case projectToolActionDelete:
		target, _ := args["target"].(string)
		res, err := project.Delete(h.app, target)
		if err != nil {
			return fmt.Sprintf("删除项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "status": projectToolStatusDeleted.String()})
		return string(data)
	case projectToolActionSwitch:
		target, _ := args["target"].(string)
		res, err := project.Switch(h.app, target)
		if err != nil {
			return fmt.Sprintf("切换项目失败: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": projectToolStatusSwitched.String()})
		return string(data)
	default:
		return fmt.Sprintf("未知 action: %s（支持 create/list/delete/switch）", action)
	}
}
