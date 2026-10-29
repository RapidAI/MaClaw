package desktopd

// /mcp on desktopd: Streamable HTTP MCP for one screen, authenticated by that
// screen's gate token (the same token that already guards its CDP and noVNC
// tunnels). An agent that was handed a desktop session can drive exactly that
// screen with standard MCP tools: screenshot, click, type, key, scroll, bash,
// file read and write. The protocol and the tools are corelib's desktopmcp,
// the same server Hub mounts at its own /mcp; only the identity differs.

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/desktopmcp"
)

// gateScreen is the screen one gate token opens.
type gateScreen struct {
	TenantID string
	UserID   string
	BotID    string
	Display  string
	seen     time.Time
}

// gateScreens maps a gate token to its screen. OpenSession fills it; a
// desktopd restart empties it until the next session open, so a stale token
// is refused rather than guessed.
type gateScreens struct {
	mu sync.Mutex
	m  map[string]gateScreen
}

func (g *gateScreens) remember(token string, screen gateScreen) {
	token = strings.TrimSpace(token)
	if !desktop.ValidGateToken(token) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]gateScreen{}
	}
	screen.seen = time.Now()
	g.m[token] = screen
}

func (g *gateScreens) lookup(token string) (gateScreen, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return gateScreen{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for known, screen := range g.m {
		if subtle.ConstantTimeCompare([]byte(known), []byte(token)) == 1 {
			return screen, true
		}
	}
	return gateScreen{}, false
}

func (g *gateScreens) forget(tenantID, userID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, screen := range g.m {
		if screen.TenantID == tenantID && screen.UserID == userID {
			delete(g.m, token)
		}
	}
}

var desktopGates gateScreens

// rememberGate records the token ensure returned for this spec's screen.
func (s *Service) rememberGate(spec Spec, token, display string) {
	desktopGates.remember(token, gateScreen{TenantID: spec.TenantID, UserID: spec.UserID, BotID: spec.BotID, Display: display})
}

const mcpIdentityPrefix = "gate:"

// mcpAuth turns a gate token into a control identity for its own screen.
// bash is allowed (the holder already drives this screen's browser over
// CDP); install is not.
func mcpAuth(r *http.Request) (desktopmcp.Identity, bool) {
	token := desktopmcp.BearerToken(r)
	screen, ok := desktopGates.lookup(token)
	if !ok {
		return desktopmcp.Identity{}, false
	}
	user := screen.UserID
	if screen.BotID != "" {
		user = desktop.BotDesktopUser(screen.UserID, screen.BotID)
	}
	return desktopmcp.Identity{
		Name:      mcpIdentityPrefix + screen.Display + "|" + screen.BotID,
		TenantID:  screen.TenantID,
		UserID:    user,
		Scope:     "control",
		AllowBash: true,
	}, true
}

// mcpScreen reads the display and bot back out of the identity name.
func mcpScreen(id desktopmcp.Identity) (display, botID string) {
	rest := strings.TrimPrefix(id.Name, mcpIdentityPrefix)
	display, botID, _ = strings.Cut(rest, "|")
	if !desktop.ValidDisplay(display) {
		display = ""
	}
	return display, botID
}

// mcpBackend forwards MCP tool calls to this desktopd's own Service.
type mcpBackend struct{ svc *Service }

func (b mcpBackend) Screenshot(ctx context.Context, id desktopmcp.Identity, opts desktop.ShotOptions) (desktop.Shot, error) {
	display, _ := mcpScreen(id)
	shot, _, err := b.svc.CaptureShot(ctx, id.TenantID, id.UserID, display, "", opts)
	return shot, err
}

func (b mcpBackend) App(ctx context.Context, id desktopmcp.Identity, argv []string) (string, error) {
	display, _ := mcpScreen(id)
	return b.svc.App(ctx, id.TenantID, id.UserID, display, argv)
}

func (b mcpBackend) Ctl(ctx context.Context, id desktopmcp.Identity, request map[string]any) (map[string]any, error) {
	if op, _ := request["op"].(string); desktop.IsCtlViewOp(strings.TrimSpace(op)) {
		return nil, fmt.Errorf("%w: %s is Hub-only", ErrInvalid, op)
	}
	display, _ := mcpScreen(id)
	return b.svc.Ctl(ctx, id.TenantID, id.UserID, display, request)
}

func (b mcpBackend) Open(ctx context.Context, id desktopmcp.Identity, program string, args []string) (string, error) {
	display, _ := mcpScreen(id)
	return b.svc.Open(ctx, id.TenantID, id.UserID, display, program, args)
}

func (b mcpBackend) File(ctx context.Context, id desktopmcp.Identity, action, filePath, content string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "read":
		return b.svc.ReadFile(ctx, id.TenantID, id.UserID, filePath)
	case "bytes":
		return b.svc.ReadBytes(ctx, id.TenantID, id.UserID, filePath)
	case "write":
		return b.svc.WriteFile(ctx, id.TenantID, id.UserID, filePath, content)
	case "list":
		return b.svc.ListFile(ctx, id.TenantID, id.UserID, filePath)
	}
	return "", ErrInvalid
}

func (b mcpBackend) Bash(ctx context.Context, id desktopmcp.Identity, command string) (string, error) {
	return b.svc.Bash(ctx, id.TenantID, id.UserID, command)
}

func (b mcpBackend) Install(ctx context.Context, id desktopmcp.Identity, packages []string) (string, error) {
	return b.svc.Install(ctx, id.TenantID, id.UserID, packages)
}

func (b mcpBackend) Session(ctx context.Context, id desktopmcp.Identity) (map[string]any, error) {
	display, botID := mcpScreen(id)
	_ = botID // UserID already names the bot's screen
	s, err := b.svc.OpenSession(ctx, Spec{TenantID: id.TenantID, UserID: id.UserID})
	if err != nil {
		return nil, err
	}
	if display == "" {
		display = s.Display
	}
	return map[string]any{"display": display, "cdp_url": s.CDP}, nil
}

// mcpHandler serves /mcp. DESKTOPD_MCP=off turns it off;
// DESKTOPD_MCP_TAKEOVER=off lets the agent act while a person watches in noVNC
// (default: refuse control then, like Hub's /mcp).
func mcpHandler(svc *Service) http.Handler {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DESKTOPD_MCP")), "off") {
		return http.NotFoundHandler()
	}
	takeover := "viewer"
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DESKTOPD_MCP_TAKEOVER")), "off") {
		takeover = "off"
	}
	server := desktopmcp.New(mcpBackend{svc: svc}, desktopmcp.Options{Auth: mcpAuth, Takeover: takeover})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		server.ServeHTTP(w, r)
	})
}
