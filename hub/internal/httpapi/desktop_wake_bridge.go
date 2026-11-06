package httpapi

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

func botmgmtDesktopWakeReason(ctx context.Context) string {
	return botmgmt.DesktopWakeReasonFrom(ctx)
}
