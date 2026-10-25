package guiapp

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkPendingReplyStorePersist(b *testing.B) {
	app := &App{testHomeDir: b.TempDir()}
	h := &IMMessageHandler{app: app}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		userID := fmt.Sprintf("bench-user-%d", i%20)
		h.storePendingAskUser(userID, &pendingAskUserState{
			Question: "continue?", Options: []string{"yes", "no"}, Timestamp: time.Now(),
		})
		if _, ok := h.takePendingAskUser(userID); !ok {
			b.Fatal("stored pending ask was not found")
		}
	}
}
