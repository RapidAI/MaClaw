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

// projectTabTranscriptPath is a per-owner side file. Tool checkpoints flush
// this file instead of rewriting the shared conversation store.
func projectTabTranscriptPath(storePath, userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return filepath.Join(filepath.Dir(storePath), "session-transcripts", hex.EncodeToString(sum[:])+".json")
}

// detachProjectTabTranscript moves a project-tab owner onto its own transcript
// file. Later calls with the same path are a no-op and do not reload disk
// over the live session.
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
