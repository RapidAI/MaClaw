package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func (a *App) memoryWriteContextForOwner(ownerID, source string) memory.WriteContext {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = desktopUserID
	}
	writeContext := memory.WriteContext{OwnerID: ownerID, Source: strings.TrimSpace(source)}
	if a != nil {
		writeContext.ProjectPath = strings.TrimSpace(a.EffectiveWorkingDirForOwner(ownerID))
	}
	return writeContext
}

func (h *IMMessageHandler) memoryWriteContextForUser(ownerID, source string) memory.WriteContext {
	if h == nil {
		return memory.WriteContext{OwnerID: strings.TrimSpace(ownerID), Source: strings.TrimSpace(source)}
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = desktopUserID
	}
	var writeContext memory.WriteContext
	if h.app != nil {
		writeContext = h.app.memoryWriteContextForOwner(ownerID, source)
	} else {
		writeContext = memory.WriteContext{OwnerID: ownerID, Source: strings.TrimSpace(source)}
	}
	if writeContext.ProjectPath == "" {
		writeContext.ProjectPath = projectPathFromUserID(ownerID)
	}
	if writeContext.ProjectPath == "" && ownerID == desktopUserID && h.contextResolver != nil {
		writeContext.ProjectPath, _ = h.contextResolver.ResolveProject()
	}
	writeContext.TurnID = h.currentLoopIDForUser(ownerID)
	if loopContext := h.runtimeLoopContextForOwner(ownerID); loopContext != nil {
		writeContext.SessionID = strings.TrimSpace(loopContext.SessionID)
		if writeContext.SessionID == "" {
			writeContext.SessionID = strings.TrimSpace(loopContext.ID)
		}
	}
	return writeContext
}
