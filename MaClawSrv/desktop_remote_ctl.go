package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopRemoteShot replaces the Hub screenshot with options in tests.
var desktopRemoteShot func(ctx context.Context, tenantID, userID, display string, opts desktop.ShotOptions) (desktop.Shot, error)

// desktopRemoteCtl replaces the Hub control request in tests.
var desktopRemoteCtl func(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error)

// screenshotWith asks Hub for a screenshot with options. The answer carries
// the screen size and scale; an older Hub that ignores the options returns a
// full PNG, which is then the identity frame.
func (c *desktopHubClient) screenshotWith(ctx context.Context, tenantID, userID, display, name string, opts desktop.ShotOptions) (desktop.Shot, string, error) {
	var out struct {
		MIME         string             `json:"mime"`
		Image        string             `json:"image_base64"`
		Saved        string             `json:"saved_path"`
		ScreenWidth  int                `json:"screen_width"`
		ScreenHeight int                `json:"screen_height"`
		ImageWidth   int                `json:"image_width"`
		ImageHeight  int                `json:"image_height"`
		Scale        float64            `json:"scale"`
		Region       desktop.ShotRegion `json:"region"`
		Cursor       []int              `json:"cursor"`
	}
	body := map[string]any{
		"tenant_id": tenantID, "user_id": userID, "display": display,
		"max_width": opts.MaxWidth, "max_height": opts.MaxHeight, "format": opts.Format,
		"quality": opts.Quality, "cursor": opts.Cursor, "marks": opts.Marks,
	}
	if !opts.Region.Empty() {
		body["region"] = opts.Region
	}
	if strings.TrimSpace(name) != "" {
		body["name"] = strings.TrimSpace(name)
	}
	if err := c.postLimit(ctx, "/api/v1/desktop-services/screenshot", body, &out, desktop.MaxScreenshotBytes*2); err != nil {
		return desktop.Shot{}, "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.Image))
	if err != nil || len(data) > desktop.MaxScreenshotBytes || desktop.SniffImageMIME(data) == "" {
		return desktop.Shot{}, "", fmt.Errorf("desktop service returned an invalid screenshot")
	}
	var shot desktop.Shot
	if out.ScreenWidth > 0 && out.ImageWidth > 0 && out.Scale > 0 {
		shot = desktop.Shot{
			Data: data, MIME: desktop.SniffImageMIME(data),
			ScreenWidth: out.ScreenWidth, ScreenHeight: out.ScreenHeight,
			ImageWidth: out.ImageWidth, ImageHeight: out.ImageHeight,
			Scale: out.Scale, Region: out.Region,
		}
		if len(out.Cursor) == 2 {
			shot.CursorX, shot.CursorY, shot.HasCursor = out.Cursor[0], out.Cursor[1], true
		}
	} else {
		if !opts.Plain() {
			return desktop.Shot{}, "", fmt.Errorf("the desktop service does not support screenshot options yet")
		}
		if shot, err = desktop.ShotFromPNG(data); err != nil {
			return desktop.Shot{}, "", err
		}
	}
	saved := strings.TrimSpace(out.Saved)
	if strings.TrimSpace(name) == "" || saved != desktop.DesktopShotPath(name) {
		saved = ""
	}
	return shot, saved, nil
}

// Ctl runs one desktop control request through Hub.
func (c *desktopHubClient) Ctl(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
	var out map[string]any
	if err := c.postLimit(ctx, "/api/v1/desktop-services/ctl", map[string]any{
		"tenant_id": tenantID, "user_id": userID, "display": display, "request": request,
	}, &out, 8<<20); err != nil {
		return nil, err
	}
	return out, nil
}

// desktopShotLocal captures with options from the local GUI container.
func desktopShotLocal(display string, opts desktop.ShotOptions) (desktop.Shot, error) {
	script, err := desktop.ShotScript("", opts)
	if err != nil {
		return desktop.Shot{}, err
	}
	if !desktop.ValidDisplay(display) {
		return desktop.Shot{}, fmt.Errorf("desktop display is invalid")
	}
	out, err := exec.Command("docker", "exec", "-e", "DISPLAY="+display, desktopContainerName(), "sh", "-c", script).Output()
	if err != nil {
		return desktop.Shot{}, fmt.Errorf("desktop screenshot failed: %v", err)
	}
	return desktop.DecodeShot(string(out), opts)
}

// desktopCtlLocal runs a control request in the local GUI container.
func desktopCtlLocal(display string, request map[string]any) (map[string]any, error) {
	tail, err := desktop.CtlArgs(request)
	if err != nil {
		return nil, err
	}
	if !desktop.ValidDisplay(display) {
		return nil, fmt.Errorf("desktop display is invalid")
	}
	out, err := exec.Command("docker", append([]string{"exec", "-e", "DISPLAY=" + display, desktopContainerName()}, tail...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("desktop control failed: %v", err)
	}
	return desktop.DecodeCtl(string(out))
}
