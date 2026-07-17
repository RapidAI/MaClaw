package main

// refreshSkillIndexesAfterMutation invalidates local skill caches after a
// writeback that changes a skill definition on disk or in config.
// Delegates to App so IM/GUI/import paths share one invalidation policy.
func (h *IMMessageHandler) refreshSkillIndexesAfterMutation(skillName string) {
	if h == nil || h.app == nil {
		return
	}
	h.app.refreshSkillIndexesAfterMutation(skillName)
}
