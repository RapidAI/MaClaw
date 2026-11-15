package guiapp

import "github.com/RapidAI/CodeClaw/corelib/session"

func (h *IMMessageHandler) getSessionStore() *session.Store {
	if h == nil {
		return nil
	}
	store, release := h.app.acquireSessionStore()
	release()
	return store
}
