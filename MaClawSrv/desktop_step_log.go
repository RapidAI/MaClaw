package main

import (
	"fmt"
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
