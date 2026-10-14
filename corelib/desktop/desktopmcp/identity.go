package desktopmcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type Identity struct {
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	Scope        string `json:"scope"` // "read" or "control"
	AllowBash    bool   `json:"allow_bash"`
	AllowInstall bool   `json:"allow_install"`
	// AnyUser lets an admin identity pick the desktop per call with the
	// user_id argument (and tenant_id when AnyTenant). Never set from a file.
	AnyUser   bool `json:"-"`
	AnyTenant bool `json:"-"`
}

// Control reports whether the identity may change the desktop.
func (id Identity) Control() bool { return id.Scope == "control" }

// Backend is the desktop service the MCP server forwards to.
type Backend interface {
	Screenshot(ctx context.Context, id Identity, opts desktop.ShotOptions) (desktop.Shot, error)
	App(ctx context.Context, id Identity, argv []string) (string, error)
	Ctl(ctx context.Context, id Identity, request map[string]any) (map[string]any, error)
	Open(ctx context.Context, id Identity, program string, args []string) (string, error)
	File(ctx context.Context, id Identity, action, path, content string) (string, error)
	Bash(ctx context.Context, id Identity, command string) (string, error)
	Install(ctx context.Context, id Identity, packages []string) (string, error)
	Session(ctx context.Context, id Identity) (map[string]any, error)
}

// target resolves the desktop an admin identity asked for. Others always get
// their own desktop; a user_id argument naming someone else is refused.
func (id Identity) target(args map[string]any) (Identity, error) {
	user, _ := args["user_id"].(string)
	tenant, _ := args["tenant_id"].(string)
	user, tenant = strings.TrimSpace(user), strings.TrimSpace(tenant)
	delete(args, "user_id")
	delete(args, "tenant_id")
	if id.AnyUser {
		if user != "" {
			if !desktop.ValidUserID(user) {
				return id, fmt.Errorf("invalid user_id")
			}
			id.UserID = user
		}
		if tenant != "" {
			if !id.AnyTenant && tenant != id.TenantID {
				return id, fmt.Errorf("this admin token is limited to tenant %s", id.TenantID)
			}
			id.TenantID = tenant
		}
		if id.UserID == "" {
			return id, fmt.Errorf("user_id is required: name the user whose desktop to use")
		}
		return id, nil
	}
	if (user != "" && user != id.UserID) || (tenant != "" && tenant != id.TenantID) {
		return id, fmt.Errorf("this token only reaches its own desktop")
	}
	if id.UserID == "" {
		return id, fmt.Errorf("this token has no desktop user")
	}
	return id, nil
}
