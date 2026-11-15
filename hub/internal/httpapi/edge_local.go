package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

// issueLocalDataURL is the same-machine address for a screenshot or a file.
// Hub has already been asked, so it wakes the desktop and signs a short
// ticket. An empty URL means the bytes still travel the existing Hub path:
// a public desktop, a different node, or a node with no direct address.
func issueLocalDataURL(ctx context.Context, pool *desktoppool.Pool, tenantID, userID, action string) (string, error) {
	if pool == nil || edgeRegistry == nil || !edgenode.Enabled() {
		return "", nil
	}
	if skip, _ := ctx.Value(skipLocalKey{}).(bool); skip {
		return "", nil
	}
	caller := strings.TrimSpace(edgeCallerNode(ctx))
	if caller == "" {
		return "", nil
	}
	op := desktoppool.WakeScreenshot
	if action == "file" {
		op = desktoppool.WakeFile
	}
	server, err := pool.AuthorizeLocal(ctx, tenantID, userID, op)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(server.NodeID) == "" || server.NodeID != caller {
		return "", nil
	}
	node, err := edgeRegistry.Lookup(ctx, server.NodeID)
	if err != nil || strings.TrimSpace(node.DirectBase) == "" || strings.TrimSpace(node.DesktopdKey) == "" {
		return "", nil
	}
	ticket, err := edgelink.SignLocal(node.DesktopdKey, edgelink.LocalClaims{
		NodeID:   node.ID,
		TenantID: strings.TrimSpace(tenantID),
		UserID:   strings.TrimSpace(userID),
		Action:   action,
		Exp:      time.Now().Add(2 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(node.DirectBase, "/") + "/v1/desktops/local/" + ticket, nil
}
