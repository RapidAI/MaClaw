package main

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Media files a run made or named (cover images, clips). While a run has
// any, every page action that could open a native file chooser is armed:
// Chrome then hands the chooser to CDP and the newest files are set on the
// exact input that asked, whatever opened it (script click, <label>, a real
// click). The OS dialog never appears.
var desktopMediaPathRe = regexp.MustCompile(`(/[^\s"'<>|;&()]+\.(?i:png|jpe?g|webp|gif|mp4|mov))`)

var desktopRunMedia sync.Map // run id -> []string

func desktopNoteMedia(scope agentruntime.Scope, action string, args map[string]any, out string, err error) {
	if err != nil || scope.RunID == "" {
		return
	}
	text := ""
	switch action {
	case "bash":
		text = desktopArg(args, "command") + "\n" + out
	case "file_write", "file_list":
		text = desktopArg(args, "path") + "\n" + out
	case "upload":
		text = strings.Join(desktopUploadFiles(args), " ")
	default:
		return
	}
	found := desktopMediaPathRe.FindAllString(text, -1)
	if len(found) == 0 {
		return
	}
	var list []string
	if v, ok := desktopRunMedia.Load(scope.RunID); ok {
		list = v.([]string)
	}
	for _, f := range found {
		if strings.HasPrefix(f, "/tmp/") && strings.Contains(f, "screenshot") {
			continue
		}
		kept := list[:0:0]
		for _, x := range list {
			if x != f {
				kept = append(kept, x)
			}
		}
		list = append(kept, f)
	}
	if len(list) > 9 {
		list = list[len(list)-9:]
	}
	desktopRunMedia.Store(scope.RunID, list)
}

func desktopPendingMedia(runID string) []string {
	if v, ok := desktopRunMedia.Load(runID); ok {
		return v.([]string)
	}
	return nil
}

// desktopArmChooserForRun arms interception before a page action of a run
// that has media; the newest file is offered (all of them to a multiple
// input would re-upload drafts).
// The returned func disarms after the action (a late chooser event gets a
// short grace), so the tab behaves normally once the step is over.
func desktopArmChooserForRun(scope agentruntime.Scope, action string, session *browser.BrowserAgentSession) func() {
	switch action {
	case "click", "script", "task_run", "skill_run", "press", "batch":
	default:
		return func() {}
	}
	media := desktopPendingMedia(scope.RunID)
	if len(media) == 0 || session == nil {
		return func() {}
	}
	session.Policy.AllowUpload = true
	if session.ArmFileChooser(media[len(media)-1:]) != nil {
		return func() {}
	}
	return func() {
		_, _ = session.WaitFileChooser(400 * time.Millisecond)
		session.DisarmFileChooser()
	}
}
