package main

import (
	"context"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// desktopProgressWriteBudget bounds one container write. A sleeping machine
// fails this write; the previous run copy stays.
const desktopProgressWriteBudget = 3 * time.Second

func init() {
	agentservice.CloudProgress = rewriteCloudProgress
}

// rewriteCloudProgress runs after each tool return of a cloud-desktop run.
// The container file is replaced. A failed write leaves the previous run copy
// and does not create a file in the GUI data directory.
func rewriteCloudProgress(ctx context.Context, tenantID, userID, taskID, toolName, result string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if desktopRemoteFile == nil && desktopHubClientFromEnv() == nil {
		return
	}
	id := desktopbot.SanitizeTaskID(taskID)
	prev, _ := desktopbot.RecallProgress(id)
	next := desktopbot.ApplyToolReturn(prev.Sections, toolName, result)
	body := desktopbot.RenderProgress(next)
	path := desktopbot.TaskProgressPath(id)
	writeCtx, cancel := context.WithTimeout(ctx, desktopProgressWriteBudget)
	defer cancel()
	scope := agentruntime.Scope{TenantID: strings.TrimSpace(tenantID), UserID: strings.TrimSpace(userID)}
	if _, err := desktopContainerFile(writeCtx, scope, "file_write", map[string]any{"path": path, "content": body}); err != nil {
		return
	}
	desktopbot.RememberProgress(id, next, path)
}
