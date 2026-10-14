package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// desktopStepLogKeys are the desktop tool arguments worth seeing in a log
// line. Free text (typed text, file content, scripts) is reduced to a length
// so user content and secrets never reach the log.
var desktopStepLogKeys = map[string]bool{
	"program": true, "path": true, "name": true, "ref": true, "url": true,
	"key": true, "packages": true, "window": true, "x": true, "y": true,
	"filter": true, "app": true,
}

var desktopStepLogLenOnly = map[string]bool{
	"text": true, "content": true, "script": true, "command": true,
	"steps": true, "actions": true, "old": true, "new": true, "value": true,
}

func desktopStepLogLine(user string, args map[string]any, out string, err error, took time.Duration) string {
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if !desktopActionAllowed(action) {
		// Never log a malformed action verbatim: models have leaked whole
		// tool-call markup (with the command text) into this field.
		action = fmt.Sprintf("invalid(%d shape=%q)", len(action), desktopActionShape(action))
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k != "action" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch {
		case desktopStepLogKeys[k]:
			v := fmt.Sprint(args[k])
			if k == "url" {
				if i := strings.IndexAny(v, "?#"); i >= 0 {
					v = v[:i] + "?…"
				}
			}
			parts = append(parts, k+"="+desktopStepClip(v, 60))
		case desktopStepLogLenOnly[k]:
			parts = append(parts, fmt.Sprintf("%s=<%d>", k, len(fmt.Sprint(args[k]))))
		default:
			parts = append(parts, k+"=…")
		}
	}
	result := "ok"
	if err != nil {
		result = "err=" + desktopStepClip(err.Error(), 120)
	} else if first := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]); first != "" {
		result = "ok first=" + desktopStepClip(first, 80)
	}
	return fmt.Sprintf("[desktop-step] user=%s action=%s args={%s} took=%s %s", user, action, strings.Join(parts, " "), took.Round(10*time.Millisecond), result)
}

func desktopStepClip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return fmt.Sprintf("%q", s)[1 : len(fmt.Sprintf("%q", s))-1]
}

// Leaked tool-call markup: some models put their own call encoding into the
// action field, e.g.
//
//	bash<tool_sep:ID>\n<arg_key:ID>command</arg_key:ID>\n<arg_value:ID>ls</arg_value:ID>
//
// recoverDesktopLeakedArgs turns that back into action=bash, command=ls so
// the call runs instead of failing with an invalid action.
var (
	desktopLeakSep   = regexp.MustCompile(`<tool_sep(?::[^>]*)?>`)
	desktopLeakPair  = regexp.MustCompile(`(?s)<arg_key(?::[^>]*)?>(.*?)</arg_key(?::[^>]*)?>\s*<arg_value(?::[^>]*)?>(.*?)(?:</arg_value(?::[^>]*)?>|$)`)
	desktopLeakStart = regexp.MustCompile(`<(?:tool_sep|arg_key|arg_value|tool_call)`)
)

func recoverDesktopLeakedArgs(args map[string]any) (map[string]any, bool) {
	raw, _ := args["action"].(string)
	if !desktopLeakStart.MatchString(raw) {
		return args, false
	}
	head := raw
	if loc := desktopLeakStart.FindStringIndex(raw); loc != nil {
		head = raw[:loc[0]]
	}
	action := strings.ToLower(strings.TrimSpace(head))
	if !desktopActionAllowed(action) {
		return args, false
	}
	out := make(map[string]any, len(args)+2)
	for k, v := range args {
		out[k] = v
	}
	out["action"] = action
	rest := desktopLeakSep.ReplaceAllString(raw[len(head):], "")
	for _, m := range desktopLeakPair.FindAllStringSubmatch(rest, -1) {
		key := strings.TrimSpace(m[1])
		if key == "" || key == "action" {
			continue
		}
		if _, set := out[key]; set {
			continue
		}
		out[key] = m[2]
	}
	return out, true
}

var desktopShapeWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// desktopActionShape keeps only the punctuation skeleton of a malformed
// action (words become w), so new leak formats can be recognised from the
// log without logging their text.
func desktopActionShape(action string) string {
	r := []rune(action)
	if len(r) > 80 {
		r = r[:80]
	}
	return desktopShapeWord.ReplaceAllString(string(r), "w")
}
