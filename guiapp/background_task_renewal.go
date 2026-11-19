package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// backgroundTaskRenewalKey marks a tool running inside a background chat task.
// While the mark is set, bash and craft_tool replace their hard deadline with
// a stall window: output or process-tree CPU resets it, and a caller cancel
// still stops the process immediately. Housekeeping LoopKindBackground loops
// do not carry a chat-task id and stay on the hard deadline.
type backgroundTaskRenewalKey struct{}

type scriptProgressKey struct{}

func withBackgroundTaskRenewal(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, backgroundTaskRenewalKey{}, true)
}

func backgroundTaskRenews(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	renews, _ := ctx.Value(backgroundTaskRenewalKey{}).(bool)
	return renews
}

// markChatTaskToolContext enables renewal only for a background chat task.
// An empty id leaves the context unchanged, including a nil context.
func markChatTaskToolContext(ctx context.Context, chatTaskID string) context.Context {
	if strings.TrimSpace(chatTaskID) == "" {
		return ctx
	}
	return withBackgroundTaskRenewal(ctx)
}

func withScriptProgressCallback(ctx context.Context, cb coretool.ProgressCallback) context.Context {
	if ctx == nil || cb == nil {
		return ctx
	}
	return context.WithValue(ctx, scriptProgressKey{}, cb)
}

func scriptProgressFromContext(ctx context.Context) coretool.ProgressCallback {
	if ctx == nil {
		return nil
	}
	cb, _ := ctx.Value(scriptProgressKey{}).(coretool.ProgressCallback)
	return cb
}

func backgroundTaskRenewalNotice(running time.Duration) string {
	if running < 0 {
		running = 0
	}
	return fmt.Sprintf("正在执行：后台任务已续期（已运行 %s）", running.Round(time.Second))
}

func backgroundRenewOptions(quiet time.Duration, clock *coretool.ProgressClock, onProgress coretool.ProgressCallback) coretool.RenewalOptions {
	return coretool.RenewalOptions{
		QuietFor: quiet,
		Clock:    clock,
		OnRenew: func(running time.Duration) {
			log.Printf("[background-task] renew running=%s quiet=%s", running.Round(time.Second), quiet.Round(time.Second))
			if onProgress != nil {
				onProgress(backgroundTaskRenewalNotice(running))
			}
		},
	}
}
