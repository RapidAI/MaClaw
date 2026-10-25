package guiapp

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const pendingReplyStoreVersion = 1

type pendingReplyDiskSnapshot struct {
	Version  int                                `json:"version"`
	Sessions map[string]pendingReplyDiskSession `json:"sessions"`
}

type pendingReplyDiskSession struct {
	Ask       *pendingAskUserState   `json:"ask,omitempty"`
	UserReply *pendingUserReplyState `json:"user_reply,omitempty"`
}

type pendingReplyDiskStore struct {
	path          string
	mu            sync.Mutex
	loadOnce      sync.Once
	snapshot      pendingReplyDiskSnapshot
	revision      uint64
	removedAskAt  map[string]time.Time
	removedUserAt map[string]time.Time
}

var pendingReplyDiskStores = struct {
	sync.Mutex
	byPath map[string]*pendingReplyDiskStore
}{byPath: map[string]*pendingReplyDiskStore{}}

func sharedPendingReplyDiskStore(path string) *pendingReplyDiskStore {
	pendingReplyDiskStores.Lock()
	defer pendingReplyDiskStores.Unlock()
	if store := pendingReplyDiskStores.byPath[path]; store != nil {
		return store
	}
	store := &pendingReplyDiskStore{
		path:          path,
		removedAskAt:  map[string]time.Time{},
		removedUserAt: map[string]time.Time{},
	}
	pendingReplyDiskStores.byPath[path] = store
	return store
}

func (h *IMMessageHandler) ensurePendingReplyStore() *pendingReplyDiskStore {
	if h == nil || h.app == nil {
		return nil
	}
	h.pendingReplyStoreOnce.Do(func() {
		path := filepath.Join(h.app.GetDataDir(), "pending_replies.json")
		store := sharedPendingReplyDiskStore(path)
		h.pendingReplyStore = store
		store.loadOnce.Do(func() {
			blob, err := os.ReadFile(path)
			if err != nil {
				if !os.IsNotExist(err) {
					log.Printf("[PendingReply] load failed: %v", err)
				}
				return
			}
			if err := json.Unmarshal(blob, &store.snapshot); err != nil {
				log.Printf("[PendingReply] parse failed: %v", err)
				store.snapshot = pendingReplyDiskSnapshot{}
			}
		})
		for userID, session := range store.snapshot.Sessions {
			if session.Ask != nil && pendingAskUserSafeToPersist(session.Ask) {
				h.pendingAskUser.Store(userID, session.Ask)
			}
			if session.UserReply != nil {
				h.pendingUserReply.Store(userID, session.UserReply)
			}
		}
		h.pendingReplyDiskRev = store.revision
	})
	return h.pendingReplyStore
}

func (h *IMMessageHandler) syncPendingReplyStore(store *pendingReplyDiskStore) {
	if h == nil || store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	h.syncPendingReplyStoreLocked(store)
}

func (h *IMMessageHandler) syncPendingReplyStoreLocked(store *pendingReplyDiskStore) {
	if h.pendingReplyDiskRev == store.revision {
		return
	}
	for userID, session := range store.snapshot.Sessions {
		if session.Ask != nil && pendingAskUserSafeToPersist(session.Ask) {
			removedAt := store.removedAskAt[userID]
			if !removedAt.IsZero() && !session.Ask.Timestamp.After(removedAt) {
				h.pendingAskUser.Delete(userID)
			} else if raw, ok := h.pendingAskUser.Load(userID); !ok {
				h.pendingAskUser.Store(userID, session.Ask)
			} else if current, _ := raw.(*pendingAskUserState); current == nil || current.Timestamp.Before(session.Ask.Timestamp) {
				h.pendingAskUser.Store(userID, session.Ask)
			}
		} else if removedAt := store.removedAskAt[userID]; !removedAt.IsZero() {
			if raw, ok := h.pendingAskUser.Load(userID); ok {
				if current, _ := raw.(*pendingAskUserState); current == nil || !current.Timestamp.After(removedAt) {
					h.pendingAskUser.Delete(userID)
				}
			}
		}
		if session.UserReply != nil {
			removedAt := store.removedUserAt[userID]
			if !removedAt.IsZero() && !session.UserReply.Timestamp.After(removedAt) {
				h.pendingUserReply.Delete(userID)
			} else if raw, ok := h.pendingUserReply.Load(userID); !ok {
				h.pendingUserReply.Store(userID, session.UserReply)
			} else if current, _ := raw.(*pendingUserReplyState); current == nil || current.Timestamp.Before(session.UserReply.Timestamp) {
				h.pendingUserReply.Store(userID, session.UserReply)
			}
		} else if removedAt := store.removedUserAt[userID]; !removedAt.IsZero() {
			if raw, ok := h.pendingUserReply.Load(userID); ok {
				if current, _ := raw.(*pendingUserReplyState); current == nil || !current.Timestamp.After(removedAt) {
					h.pendingUserReply.Delete(userID)
				}
			}
		}
	}
	for userID, removedAt := range store.removedAskAt {
		if raw, ok := h.pendingAskUser.Load(userID); ok {
			if current, _ := raw.(*pendingAskUserState); current == nil || !current.Timestamp.After(removedAt) {
				h.pendingAskUser.Delete(userID)
			}
		}
	}
	for userID, removedAt := range store.removedUserAt {
		if raw, ok := h.pendingUserReply.Load(userID); ok {
			if current, _ := raw.(*pendingUserReplyState); current == nil || !current.Timestamp.After(removedAt) {
				h.pendingUserReply.Delete(userID)
			}
		}
	}
	h.pendingReplyDiskRev = store.revision
}

func pendingAskUserSafeToPersist(state *pendingAskUserState) bool {
	return state != nil && state.MailboxDraftID == "" && state.MCPServerWrite == nil && state.CommitApproval == nil
}

func (h *IMMessageHandler) persistPendingReplies(userID string, removedAsk, removedUserReply bool) {
	store := h.ensurePendingReplyStore()
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	h.syncPendingReplyStoreLocked(store)
	snapshot := store.snapshot
	if snapshot.Sessions == nil {
		snapshot.Sessions = map[string]pendingReplyDiskSession{}
	}
	session := snapshot.Sessions[userID]
	now := time.Now()

	var ask *pendingAskUserState
	if raw, ok := h.pendingAskUser.Load(userID); ok {
		loaded, _ := raw.(*pendingAskUserState)
		if pendingAskUserSafeToPersist(loaded) {
			ask = loaded
		}
	}
	if removedAsk {
		ask = nil
		store.removedAskAt[userID] = now
	} else if ask != nil {
		if ask.Timestamp.IsZero() {
			ask.Timestamp = now
		}
		if removedAt := store.removedAskAt[userID]; !removedAt.IsZero() && !ask.Timestamp.After(removedAt) {
			ask = nil
			h.pendingAskUser.Delete(userID)
		} else {
			delete(store.removedAskAt, userID)
		}
	}
	session.Ask = ask

	var userReply *pendingUserReplyState
	if raw, ok := h.pendingUserReply.Load(userID); ok {
		userReply, _ = raw.(*pendingUserReplyState)
	}
	if removedUserReply {
		userReply = nil
		store.removedUserAt[userID] = now
	} else if userReply != nil {
		if userReply.Timestamp.IsZero() {
			userReply.Timestamp = now
		}
		if removedAt := store.removedUserAt[userID]; !removedAt.IsZero() && !userReply.Timestamp.After(removedAt) {
			userReply = nil
			h.pendingUserReply.Delete(userID)
		} else {
			delete(store.removedUserAt, userID)
		}
	}
	session.UserReply = userReply

	if session.Ask == nil && session.UserReply == nil {
		delete(snapshot.Sessions, userID)
	} else {
		snapshot.Sessions[userID] = session
	}
	snapshot.Version = pendingReplyStoreVersion
	store.snapshot = snapshot
	blob, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("[PendingReply] marshal failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		log.Printf("[PendingReply] mkdir failed: %v", err)
		return
	}
	tmp := store.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		log.Printf("[PendingReply] write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, store.path); err != nil {
		log.Printf("[PendingReply] rename failed: %v", err)
		return
	}
	store.revision++
	h.pendingReplyDiskRev = store.revision
}

func (h *IMMessageHandler) storePendingAskUser(userID string, state *pendingAskUserState) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	if state == nil {
		h.deletePendingAskUser(userID)
		return
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	h.pendingAskUser.Store(userID, state)
	h.persistPendingReplies(userID, false, false)
}

func (h *IMMessageHandler) deletePendingAskUser(userID string) {
	if h == nil {
		return
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	h.pendingAskUser.Delete(userID)
	h.persistPendingReplies(userID, true, false)
}

func (h *IMMessageHandler) takePendingAskUser(userID string) (any, bool) {
	if h == nil {
		return nil, false
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	raw, ok := h.pendingAskUser.LoadAndDelete(userID)
	if ok {
		h.persistPendingReplies(userID, true, false)
	}
	return raw, ok
}

func (h *IMMessageHandler) storePendingUserReply(userID string, state *pendingUserReplyState) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	if state == nil {
		h.deletePendingUserReply(userID)
		return
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	h.pendingUserReply.Store(userID, state)
	h.persistPendingReplies(userID, false, false)
}

func (h *IMMessageHandler) deletePendingUserReply(userID string) {
	if h == nil {
		return
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	h.pendingUserReply.Delete(userID)
	h.persistPendingReplies(userID, false, true)
}

func (h *IMMessageHandler) takePendingUserReply(userID string) (any, bool) {
	if h == nil {
		return nil, false
	}
	store := h.ensurePendingReplyStore()
	if store != nil {
		h.syncPendingReplyStore(store)
	}
	raw, ok := h.pendingUserReply.LoadAndDelete(userID)
	if ok {
		h.persistPendingReplies(userID, false, true)
	}
	return raw, ok
}
