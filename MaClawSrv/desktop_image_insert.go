package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Rich editors (articles, notes) take pictures through action=publish
// files=[...]: it uploads or pastes them, waits for each <img>, fills the
// text, clicks publish and reads the post back. ZN-2..ZN-6 instead spent
// 20+ minutes putting images in by hand (xclip image targets, page-script
// paste events, DataTransfer). Once the task asks to publish, those manual
// image inserts are refused and pointed at the one publish call.
var (
	desktopScriptImageInsertRe = regexp.MustCompile(`(?i)new\s+ClipboardEvent|new\s+DataTransfer|new\s+File\s*\(|insertImage|createElement\(\s*['"]img['"]|\binsertHTML\b[^;]*<img|['"]image/(png|jpe?g|gif|webp)['"]`)
	desktopBashImageClipRe     = regexp.MustCompile(`(?i)(xclip|xsel|wl-copy|copyq)[^|;&\n]*image/|(xclip|wl-copy)[^|;&\n]*\.(png|jpe?g|gif|webp)\b`)
	desktopImageExtRe          = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)\b|image/`)
)

const desktopManualImageRefusal = "refused: manual_image_insert — this task publishes, and pictures in a rich editor go through ONE call: action=publish with title, body (text, with the picture places marked or at the end) and files=[the image paths]. It uploads or pastes each picture, waits until it shows, clicks publish and reads the post back. Do not put images in by clipboard, xclip or page scripts. Call action=publish now"

// desktopManualImageInsert reports a step that puts an image into a page by
// hand instead of through the publish flow.
func desktopManualImageInsert(action string, args map[string]any) bool {
	switch action {
	case "batch":
		raw, _ := args["plan"].([]any)
		if len(raw) == 0 {
			raw, _ = args["steps"].([]any)
		}
		for _, r := range raw {
			if m, ok := r.(map[string]any); ok && desktopManualImageInsert(desktopArg(m, "action"), m) {
				return true
			}
		}
	case "script":
		return desktopScriptImageInsertRe.MatchString(desktopArg(args, "script"))
	case "bash":
		return desktopBashImageClipRe.MatchString(desktopArg(args, "command"))
	case "clipboard_set":
		for _, k := range []string{"file", "path", "mime", "type", "image"} {
			if desktopImageExtRe.MatchString(desktopArg(args, k)) {
				return true
			}
		}
	}
	return false
}

func desktopManualImageCheck(action string, args map[string]any, request string, published bool) error {
	if published || !desktopManualImageInsert(action, args) {
		return nil
	}
	if !desktopPublishIntentRe.MatchString(request) && !desktopPreapproveRe.MatchString(request) {
		return nil
	}
	return fmt.Errorf("%s", strings.TrimSpace(desktopManualImageRefusal))
}
