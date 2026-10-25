package guiapp

import (
	"fmt"
	"testing"
	"time"
)

func TestPendingReplyStoreStressInterleavedHandlers(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const count = 100
	first := &IMMessageHandler{app: app}
	second := &IMMessageHandler{app: app}
	for i := 0; i < count; i++ {
		userID := fmt.Sprintf("stress-user-%03d", i)
		first.storePendingAskUser(userID, &pendingAskUserState{
			Question: "continue?", Timestamp: time.Now(),
		})
		if i%2 == 0 {
			second.ensurePendingReplyStore()
			if _, ok := second.takePendingAskUser(userID); !ok {
				t.Fatalf("handler 2 did not restore %s", userID)
			}
			first.storePendingUserReply(userID, &pendingUserReplyState{
				Question: "which model?", Timestamp: time.Now(),
			})
		}
	}

	reloaded := &IMMessageHandler{app: app}
	reloaded.ensurePendingReplyStore()
	for i := 0; i < count; i++ {
		userID := fmt.Sprintf("stress-user-%03d", i)
		_, hasAsk := reloaded.pendingAskUser.Load(userID)
		_, hasReply := reloaded.pendingUserReply.Load(userID)
		if i%2 == 0 {
			if hasAsk || !hasReply {
				t.Fatalf("user %s: ask=%v reply=%v", userID, hasAsk, hasReply)
			}
		} else if !hasAsk || hasReply {
			t.Fatalf("user %s: ask=%v reply=%v", userID, hasAsk, hasReply)
		}
	}
}
