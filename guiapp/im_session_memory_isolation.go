package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"path/filepath"
	"strings"
)

// isIsolatedAssistantSessionUserID identifies assistant conversations whose
// working context must never be mixed with another active conversation. Their
// only permitted cross-session input is a completed, archived experience
// selected explicitly by the memory layer.
func isIsolatedAssistantSessionUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	return isLansengerGroupConversationUserID(userID) ||
		isProjectTabUserID(userID) ||
		isACPAssistantSessionUserID(userID) ||
		isPetCompanionUser(userID) ||
		expertIDFromUserID(userID) != ""
}

// projectTabTranscriptPath is the stable per-owner detached key. SQLite stores
// this key as isolation metadata; legacy JSON mode uses it as a side-file path.
func projectTabTranscriptPath(storePath, userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return filepath.Join(filepath.Dir(storePath), "session-transcripts", hex.EncodeToString(sum[:])+".json")
}

// detachProjectTabTranscript marks a project-tab owner as detached. Later calls
// with the same key are a no-op and do not reload over the live session.
func (h *IMMessageHandler) detachProjectTabTranscript(userID string) {
	if h == nil || h.memory == nil || !isProjectTabUserID(userID) {
		return
	}
	store := h.memory.StorePath()
	if strings.TrimSpace(store) == "" {
		return
	}
	if err := h.memory.UseSeparateSessionFile(userID, projectTabTranscriptPath(store, userID)); err != nil {
		log.Printf("[conversation] detach project transcript failed user=%q err=%v", userID, err)
	}
}
