package main

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// desktopReadCheckoutPage is the live page. Tests replace it.
var desktopReadCheckoutPage = func(session *browser.BrowserAgentSession) browser.CheckoutPage {
	if session == nil {
		return browser.CheckoutPage{}
	}
	return session.ReadCheckoutPage()
}

// desktopCheckoutElementAt is the control under a coordinate click.
// Tests replace it. Production asks the browser, then the same screen
// text a publish click already reads.
var desktopCheckoutElementAt = func(ctx context.Context, scope agentruntime.Scope, session *browser.BrowserAgentSession, display string, x, y float64) (string, string) {
	if session != nil {
		if name, text := session.ReadCheckoutElement(x, y); name != "" || text != "" {
			return name, text
		}
	}
	return desktopCheckoutOCR(ctx, scope, display, x, y)
}

func desktopCheckoutOwner(scope agentruntime.Scope) string {
	tenantID := strings.TrimSpace(scope.TenantID)
	userID := strings.TrimSpace(scope.UserID)
	switch {
	case tenantID == "" && userID == "":
		return ""
	case tenantID == "":
		return userID
	case userID == "":
		return tenantID
	default:
		return tenantID + ":" + userID
	}
}

// desktopHoldCheckout stops a click, an Enter, or an app step whose target
// is an order-submit or payment commit until the person has confirmed.
// The same decision covers every action type, so picking app_run instead of
// click does not skip it.
func desktopHoldCheckout(ctx context.Context, scope agentruntime.Scope, session *browser.BrowserAgentSession, display, action string, args map[string]any) (string, bool) {
	page := desktopReadCheckoutPage(session)
	targets := desktopCheckoutTargets(ctx, scope, session, display, action, args, page)
	if len(targets) == 0 {
		return "", false
	}
	pre := browser.PreselectedCartItems(scope.RunID, page.Selected)
	owner := desktopCheckoutOwner(scope)
	pageText := page.Context + "\n" + page.URL + "\n" + page.Focused
	spend := action == "app_run" || action == "computer"
	for _, target := range targets {
		if browser.IsCartAdd(target.Name, target.Text) {
			browser.NoteRunCartAdd(scope.RunID, browser.CartItemFrom(page.Context, firstCheckoutLabel(target)))
		}
		if !target.Commits() {
			continue
		}
		if browser.CheckoutReady(owner, pageText) {
			if spend {
				if err := browser.TakeCheckoutConfirmation(owner, pageText); err != nil {
					return browser.CheckoutConfirmQuestion(firstCheckoutLabel(target), pre), true
				}
			}
			return "", false
		}
		return browser.CheckoutConfirmQuestion(firstCheckoutLabel(target), pre), true
	}
	return "", false
}

func firstCheckoutLabel(target browser.CheckoutTarget) string {
	label := strings.TrimSpace(target.Name)
	if label == "" {
		label = strings.TrimSpace(target.Text)
	}
	return label
}

func desktopCheckoutPause(scope agentruntime.Scope, question string) (string, error) {
	if desktopUnattended(scope.InstanceID) {
		return "", errDesktopCheckoutUnattended
	}
	noteDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID)
	noteDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID, "payment_confirm")
	noteDesktopResume(scope)
	return agent.AskUserResultMarker(&agent.AskUserRequest{
		Question:  question,
		InputType: "text",
	}), nil
}

type checkoutErr string

func (e checkoutErr) Error() string { return string(e) }

var errDesktopCheckoutUnattended = checkoutErr("这次是自动任务，没有人在对话里确认下单或付款。")

func desktopCheckoutTargets(ctx context.Context, scope agentruntime.Scope, session *browser.BrowserAgentSession, display, action string, args map[string]any, page browser.CheckoutPage) []browser.CheckoutTarget {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "click":
		return []browser.CheckoutTarget{desktopClickTarget(session, args, page)}
	case "press":
		return []browser.CheckoutTarget{{
			Name: page.Focused, Context: page.Context, URL: page.URL,
			Key: desktopArg(args, "key"), Submits: true,
		}}
	case "type":
		raw, _ := args["text"].(string)
		if !strings.ContainsAny(raw, "\r\n") {
			return nil
		}
		return []browser.CheckoutTarget{{
			Name: page.Focused, Context: page.Context, URL: page.URL,
			Key: "Return", Submits: true,
		}}
	case "script", "skill_run":
		script := desktopArg(args, "script")
		if script == "" {
			script = desktopArg(args, "name")
		}
		return []browser.CheckoutTarget{{Name: script, Text: script, Context: page.Context, URL: page.URL, Submits: true}}
	case "task_run":
		return desktopTaskCheckoutTargets(session, args["steps"], page)
	case "app_run", "computer":
		raw := args["steps"]
		if action == "computer" {
			raw = desktopComputerActions(args)
		}
		return desktopAppCheckoutTargets(ctx, scope, session, display, raw, page)
	default:
		return nil
	}
}

func desktopClickTarget(session *browser.BrowserAgentSession, args map[string]any, page browser.CheckoutPage) browser.CheckoutTarget {
	text := desktopArg(args, "text")
	name := text
	role := ""
	if session != nil {
		if ref, err := session.ResolveRef(desktopArg(args, "snapshot_id"), desktopArg(args, "ref")); err == nil && ref != nil {
			if strings.TrimSpace(ref.Name) != "" {
				name = ref.Name
			}
			if strings.TrimSpace(ref.Text) != "" {
				text = ref.Text
			}
			role = ref.Role
		}
	}
	return browser.CheckoutTarget{
		Name: name, Text: text, Role: role,
		Context: page.Context, URL: page.URL, Submits: true,
	}
}

func desktopTaskCheckoutTargets(session *browser.BrowserAgentSession, raw any, page browser.CheckoutPage) []browser.CheckoutTarget {
	steps, err := desktopBrowserSteps(raw)
	if err != nil {
		return nil
	}
	var out []browser.CheckoutTarget
	for _, step := range steps {
		switch strings.ToLower(strings.TrimSpace(step.Action)) {
		case "click":
			target := desktopClickTarget(session, map[string]any{
				"text": step.Params["text"], "ref": step.Params["ref"], "snapshot_id": step.Params["snapshot_id"],
			}, page)
			if strings.TrimSpace(target.Name) == "" {
				target.Name = strings.TrimSpace(step.Params["name"])
			}
			out = append(out, target)
		case "press":
			key := step.Params["key"]
			if key == "" {
				key = step.Params["text"]
			}
			out = append(out, browser.CheckoutTarget{
				Name: page.Focused, Context: page.Context, URL: page.URL, Key: key, Submits: true,
			})
		case "type":
			if strings.ContainsAny(step.Params["text"], "\r\n") {
				out = append(out, browser.CheckoutTarget{
					Name: page.Focused, Context: page.Context, URL: page.URL, Key: "Return", Submits: true,
				})
			}
		}
	}
	return out
}

func desktopAppCheckoutTargets(ctx context.Context, scope agentruntime.Scope, session *browser.BrowserAgentSession, display string, raw any, page browser.CheckoutPage) []browser.CheckoutTarget {
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return nil
	}
	var out []browser.CheckoutTarget
	for _, step := range steps {
		switch step.Kind {
		case "click", "mouse_down":
			name, text := step.Name, step.Text
			if step.HasXY {
				if hitName, hitText := desktopCheckoutElementAt(ctx, scope, session, display, step.X, step.Y); hitName != "" || hitText != "" {
					name, text = hitName, hitText
				}
			}
			out = append(out, browser.CheckoutTarget{
				Name: name, Text: text, Context: page.Context, URL: page.URL, Submits: true,
			})
		case "key", "hold_key":
			for _, key := range step.Keys {
				out = append(out, browser.CheckoutTarget{
					Name: page.Focused, Context: page.Context, URL: page.URL, Key: key, Submits: true,
				})
			}
		case "type":
			// A type step in app_run sends the line and then Enter.
			out = append(out, browser.CheckoutTarget{
				Name: page.Focused, Context: page.Context, URL: page.URL, Key: "Return", Submits: true,
			})
		}
	}
	return out
}

// desktopCheckoutOCR reads the visible words under a coordinate click when
// the browser has no element there. The same screen read backs the publish
// guard. A missing reader means the point is not a labeled control.
func desktopCheckoutOCR(ctx context.Context, scope agentruntime.Scope, display string, x, y float64) (string, string) {
	if desktopOCRLines == nil {
		return "", ""
	}
	frame := desktopFrameFor(scope)
	_, opts := frame.get()
	shot, err := desktopGroundShot(ctx, scope, display, opts)
	if err != nil || len(shot.Data) == 0 {
		return "", ""
	}
	scale := shot.Scale
	if scale <= 0 {
		scale = 1
	}
	for _, p := range [][2]float64{{x, y}, {x * scale, y * scale}} {
		for _, line := range desktopOCRLines(ctx, shot.Data) {
			b := line.BBox
			if p[0] >= float64(b[0]-6) && p[0] <= float64(b[0]+b[2]+6) && p[1] >= float64(b[1]-6) && p[1] <= float64(b[1]+b[3]+6) {
				if strings.TrimSpace(line.Text) != "" {
					return line.Text, line.Text
				}
			}
		}
	}
	return "", ""
}
