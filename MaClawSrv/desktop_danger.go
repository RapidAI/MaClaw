package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

type desktopComputerHelperKey struct{}

// desktopDangerReview is the other model's one look. Tests set it.
// A nil review is an unreadable verdict, which is not a pass.
// This process does not call a model itself.
var desktopDangerReview func(ctx context.Context, call desktopbot.DangerCall) (verdict string, ok bool)

func refuseDesktopDanger(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) error {
	call := desktopbot.DangerCall{
		Action:  action,
		Command: desktopDangerCommand(action, args),
		Helper:  desktopComputerHelper(ctx),
	}
	verdict, ok := "", false
	if desktopDangerReview != nil {
		verdict, ok = desktopDangerReview(ctx, call)
	}
	result := desktopbot.GateDanger(desktopbot.DangerInput{
		Call:      call,
		Verdict:   verdict,
		VerdictOK: ok,
		Key:       desktopDangerKey(scope, call),
		Now:       desktopbot.DangerNow(),
	})
	if result.Ask.Text != "" {
		desktopbot.NoteDangerAsk(desktopDangerAskKeys(scope), result.Ask)
	}
	if result.Run {
		return nil
	}
	return fmt.Errorf("%s", result.Error)
}

func desktopDangerCommand(action string, args map[string]any) string {
	if action != "install" {
		return desktopArg(args, "command")
	}
	names, err := desktop.PackageNames(args["packages"])
	if err != nil {
		return "install"
	}
	return "install " + strings.Join(names, " ")
}

func desktopComputerHelper(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	helper, _ := ctx.Value(desktopComputerHelperKey{}).(bool)
	return helper
}

func desktopDangerKey(scope agentruntime.Scope, call desktopbot.DangerCall) string {
	return strings.Join([]string{
		strings.TrimSpace(scope.TenantID),
		strings.TrimSpace(scope.UserID),
		strings.TrimSpace(scope.InstanceID),
		strings.ToLower(strings.TrimSpace(call.Action)),
		strings.TrimSpace(call.Command),
	}, "\x00")
}

func desktopDangerAskKeys(scope agentruntime.Scope) []string {
	return []string{
		strings.TrimSpace(scope.SessionID),
		strings.TrimSpace(scope.RunID),
		strings.TrimSpace(scope.InstanceID),
	}
}
