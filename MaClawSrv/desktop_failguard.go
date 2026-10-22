package main

import (
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Failure-class guard. The repeat guard only sees identical calls; a stuck
// model also varies the arguments while using the same broken method (a
// different script that the editor ignores again, another cross-origin
// navigate). The same intent class failing with the same error class twice
// in a run blocks that intent class for the rest of the run: the second
// failure carries a warning, later calls of that class are refused with
// guidance to switch interface or request a takeover.

const desktopFailClassLimit = 2

// desktopIntentClass groups calls that use the same method.
func desktopIntentClass(action string, args map[string]any) string {
	switch action {
	case "app_run", "computer":
		raw := args["steps"]
		if action == "computer" {
			raw = desktopComputerActions(args)
		}
		if desktopStepsInsertedText(raw) != "" {
			return action + ":text_input"
		}
		return action
	case "click":
		if desktopArg(args, "ref") != "" {
			return "click:ref"
		}
		return "click:text"
	}
	return action
}

// desktopErrorClass names the failure a step ended with, or "".
func desktopErrorClass(out string, err error) string {
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(out, browser.LoginWallMarker) || strings.Contains(msg, strings.ToLower(browser.LoginWallMarker)):
		return "login_wall"
	case strings.Contains(out, desktopNotAppliedMarker) || strings.Contains(msg, "not applied") || strings.Contains(msg, "未生效"):
		return "not_applied"
	case strings.Contains(msg, "cross-origin") || strings.Contains(msg, "cross_origin"):
		return "cross_origin_block"
	case err == nil:
		return ""
	case strings.Contains(msg, "rich_text_dom_write"):
		return "rich_text_dom_write"
	case strings.Contains(msg, "publish_needs_confirmation"):
		return "publish_needs_confirmation"
	case strings.Contains(msg, "page script failed"):
		return "script_error"
	case strings.Contains(msg, "is stale") || strings.Contains(msg, "not found") || strings.Contains(msg, "no element") ||
		strings.Contains(msg, "no match") || strings.Contains(msg, "找不到") || strings.Contains(msg, "未找到"):
		return "element_not_found"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "超时"):
		return "timeout"
	}
	return ""
}

// desktopFailClassCheck refuses a call whose intent class is blocked.
func desktopFailClassCheck(scope agentruntime.Scope, action string, args map[string]any) error {
	st := desktopRunFor(scope)
	if st == nil || desktopRepeatExempt[action] {
		return nil
	}
	intent := desktopIntentClass(action, args)
	st.mu.Lock()
	class := st.blocked[intent]
	st.mu.Unlock()
	if class == "" {
		return nil
	}
	log.Printf("[desktop-guard] refused intent=%s after error_class=%s x%d", intent, class, desktopFailClassLimit)
	return fmt.Errorf("refused: method %s already failed with %s %d times in this run, so it is blocked. %s", intent, class, desktopFailClassLimit, desktopFailClassAdvice(class))
}

// desktopFailClassRecord counts a finished call and returns a warning to add
// when it just blocked its intent class.
func desktopFailClassRecord(scope agentruntime.Scope, action string, args map[string]any, out string, err error) string {
	st := desktopRunFor(scope)
	if st == nil || desktopRepeatExempt[action] {
		return ""
	}
	class := desktopErrorClass(out, err)
	if class == "" {
		return ""
	}
	intent := desktopIntentClass(action, args)
	st.mu.Lock()
	if st.failures == nil {
		st.failures = map[string]int{}
		st.blocked = map[string]string{}
	}
	key := intent + "|" + class
	st.failures[key]++
	n := st.failures[key]
	if n >= desktopFailClassLimit {
		st.blocked[intent] = class
	}
	st.mu.Unlock()
	log.Printf("[desktop-guard] intent=%s error_class=%s count=%d", intent, class, n)
	if class == "login_wall" {
		// A wall is not fixed by retrying: ask for the person at once.
		return "\n\nLogin wall: " + desktopFailClassAdvice(class)
	}
	if n < desktopFailClassLimit {
		return ""
	}
	return fmt.Sprintf("\n\nMethod blocked: %s has now failed with %s %d times in this run. Further %s calls will be refused. %s", intent, class, n, intent, desktopFailClassAdvice(class))
}

func desktopFailClassAdvice(class string) string {
	switch class {
	case "not_applied":
		return "Switch interface: click into the editor and use real input (clipboard_set then app_run key ctrl+v, or app_run type), then verify with probe/screenshot — or call request_takeover."
	case "cross_origin_block":
		return "Switch interface: navigate explicitly to the target site in its own tab (navigate with the full URL), use the site's own link/button, or call request_takeover."
	case "element_not_found":
		return "Switch interface: probe again for fresh refs, use a screenshot and click by position (app_run/computer), or the accessibility tree — or call request_takeover."
	case "login_wall":
		return "The site shows a sign-in form, a captcha/安全验证 check, or stays blank. Do not wait or retry: call request_takeover so the person signs in or passes the check, then continue."
	case "timeout":
		return "Switch interface: take a screenshot to see the real state and use a different action, or call request_takeover."
	}
	return "Use a different method or call request_takeover."
}
