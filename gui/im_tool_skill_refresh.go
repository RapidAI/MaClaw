package main

// refreshSkillIndexesAfterMutation invalidates local skill caches after a
// writeback that changes a skill definition on disk or in config.
func (h *IMMessageHandler) refreshSkillIndexesAfterMutation(skillName string) {
	if h == nil || h.app == nil {
		return
	}
	if exec := h.getSkillExecutor(); exec != nil {
		exec.invalidateSkillCache()
	}
	if h.app.toolRouter != nil {
		h.app.toolRouter.RefreshSkillIndex()
	}
	h.app.emitEvent("skill:index_refreshed", map[string]string{"skill": skillName})
}
