package main

import "fmt"

// toolUninstallSkill handles manage_skill(action="uninstall").
// It removes the skill from config.json and deletes its on-disk directory.
func (h *IMMessageHandler) toolUninstallSkill(args map[string]interface{}) string {
	name := stringVal(args, "name")
	if name == "" {
		return "缺少 name 参数（要卸载的 Skill 名称）"
	}

	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}

	if err := exec.Delete(name); err != nil {
		return fmt.Sprintf("卸载失败: %v", err)
	}

	return fmt.Sprintf("✅ Skill '%s' 已卸载（配置和目录已清理）", name)
}
