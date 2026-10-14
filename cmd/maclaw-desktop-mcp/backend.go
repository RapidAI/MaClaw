package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Identity is the desktop one MCP client may use, and what it may do there.
type Identity struct {
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	Scope        string `json:"scope"` // "read" or "control"
	AllowBash    bool   `json:"allow_bash"`
	AllowInstall bool   `json:"allow_install"`
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

// httpBackend talks to Hub (/api/v1/desktop-services/*, multi-tenant, the
// production path) or straight to one desktopd (/v1/desktops/*). The token
// is the service's desktop API token; it never leaves this process.
type httpBackend struct {
	base    string
	token   string
	hub     bool
	image   string
	client  *http.Client
	display string
}

func newHTTPBackend(kind, base, token, image string) (*httpBackend, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("backend URL and token are required")
	}
	if kind != "hub" && kind != "desktopd" {
		return nil, fmt.Errorf("backend must be hub or desktopd")
	}
	if image == "" {
		image = "maclaw-gui:2"
	}
	return &httpBackend{base: base, token: token, hub: kind == "hub", image: image, client: &http.Client{Timeout: 3 * time.Minute}, display: ":20"}, nil
}

func (b *httpBackend) path(name string) string {
	if b.hub {
		return b.base + "/api/v1/desktop-services/" + name
	}
	return b.base + "/v1/desktops/" + name
}

func (b *httpBackend) post(ctx context.Context, name string, body map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.path(name), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("desktop service unreachable: %v", err)
	}
	return resp, nil
}

func (b *httpBackend) postJSON(ctx context.Context, name string, body map[string]any, out any) error {
	resp, err := b.post(ctx, name, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
			Error   any    `json:"error"`
		}
		_ = json.Unmarshal(payload, &failure)
		message := failure.Message
		if text, ok := failure.Error.(string); ok && message == "" {
			message = text
		}
		if message == "" {
			message = strings.TrimSpace(string(payload))
			if len(message) > 300 {
				message = message[:300]
			}
		}
		return fmt.Errorf("desktop service %s failed (%d): %s", name, resp.StatusCode, message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("desktop service %s returned invalid JSON", name)
	}
	return nil
}

func (b *httpBackend) who(id Identity) map[string]any {
	return map[string]any{"tenant_id": id.TenantID, "user_id": id.UserID}
}

func (b *httpBackend) Screenshot(ctx context.Context, id Identity, opts desktop.ShotOptions) (desktop.Shot, error) {
	body := b.who(id)
	body["display"] = b.display
	body["max_width"], body["max_height"], body["format"] = opts.MaxWidth, opts.MaxHeight, opts.Format
	body["quality"], body["cursor"], body["marks"] = opts.Quality, opts.Cursor, opts.Marks
	if !opts.Region.Empty() {
		body["region"] = opts.Region
	}
	if b.hub {
		var out struct {
			Image        string             `json:"image_base64"`
			ScreenWidth  int                `json:"screen_width"`
			ScreenHeight int                `json:"screen_height"`
			ImageWidth   int                `json:"image_width"`
			ImageHeight  int                `json:"image_height"`
			Scale        float64            `json:"scale"`
			Region       desktop.ShotRegion `json:"region"`
			Cursor       []int              `json:"cursor"`
		}
		if err := b.postJSON(ctx, "screenshot", body, &out); err != nil {
			return desktop.Shot{}, err
		}
		data, err := base64.StdEncoding.DecodeString(out.Image)
		if err != nil || desktop.SniffImageMIME(data) == "" {
			return desktop.Shot{}, fmt.Errorf("desktop service returned an invalid screenshot")
		}
		if out.ScreenWidth == 0 {
			return desktop.ShotFromPNG(data)
		}
		shot := desktop.Shot{Data: data, MIME: desktop.SniffImageMIME(data), ScreenWidth: out.ScreenWidth, ScreenHeight: out.ScreenHeight,
			ImageWidth: out.ImageWidth, ImageHeight: out.ImageHeight, Scale: out.Scale, Region: out.Region}
		if len(out.Cursor) == 2 {
			shot.CursorX, shot.CursorY, shot.HasCursor = out.Cursor[0], out.Cursor[1], true
		}
		return shot, nil
	}
	resp, err := b.post(ctx, "screenshot", body)
	if err != nil {
		return desktop.Shot{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, desktop.MaxScreenshotBytes+1))
	if err != nil {
		return desktop.Shot{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return desktop.Shot{}, fmt.Errorf("desktop screenshot failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data[:min(len(data), 300)])))
	}
	return desktop.ParseShotHeaders(resp.Header.Get, data)
}

func (b *httpBackend) App(ctx context.Context, id Identity, argv []string) (string, error) {
	body := b.who(id)
	body["display"], body["args"] = b.display, argv
	var out struct {
		Output string `json:"output"`
	}
	err := b.postJSON(ctx, "app", body, &out)
	return out.Output, err
}

func (b *httpBackend) Ctl(ctx context.Context, id Identity, request map[string]any) (map[string]any, error) {
	body := b.who(id)
	body["display"], body["request"] = b.display, request
	var out map[string]any
	err := b.postJSON(ctx, "ctl", body, &out)
	return out, err
}

func (b *httpBackend) Open(ctx context.Context, id Identity, program string, args []string) (string, error) {
	body := b.who(id)
	body["display"], body["program"], body["args"] = b.display, program, args
	var out struct {
		Output string `json:"output"`
	}
	err := b.postJSON(ctx, "open", body, &out)
	return out.Output, err
}

func (b *httpBackend) File(ctx context.Context, id Identity, action, path, content string) (string, error) {
	body := b.who(id)
	body["action"], body["path"], body["content"] = action, path, content
	var out struct {
		Output string `json:"output"`
	}
	err := b.postJSON(ctx, "file", body, &out)
	return out.Output, err
}

func (b *httpBackend) Bash(ctx context.Context, id Identity, command string) (string, error) {
	body := b.who(id)
	body["command"] = command
	var out struct {
		Output string `json:"output"`
	}
	err := b.postJSON(ctx, "bash", body, &out)
	return out.Output, err
}

func (b *httpBackend) Install(ctx context.Context, id Identity, packages []string) (string, error) {
	body := b.who(id)
	body["packages"] = packages
	resp, err := b.post(ctx, "install", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Output string `json:"output"`
	}
	text := string(payload)
	if json.Unmarshal(payload, &out) == nil && out.Output != "" {
		text = out.Output
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return text, fmt.Errorf("install failed (%d)", resp.StatusCode)
	}
	return text, nil
}

func (b *httpBackend) Session(ctx context.Context, id Identity) (map[string]any, error) {
	body := b.who(id)
	if !b.hub {
		// desktopd needs the container shape; keep the deployment's.
		body["image"] = b.image
		for key, env := range map[string]string{"memory": "MACLAW_MCP_MEMORY", "cpus": "MACLAW_MCP_CPUS", "shm_size": "MACLAW_MCP_SHM_SIZE"} {
			if value := strings.TrimSpace(os.Getenv(env)); value != "" {
				body[key] = value
			}
		}
	}
	var out map[string]any
	err := b.postJSON(ctx, "session", body, &out)
	return out, err
}
