package guiapp

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// computerUseExperienceDirForTest points lessons at a temp directory.
// Production leaves it empty and uses the MaClaw data directory.
var computerUseExperienceDirForTest string

func computerUseOpenExperience() {
	if testing.Testing() {
		if computerUseExperienceDirForTest == "" {
			computeruse.SetExperienceDir("")
			return
		}
		computeruse.SetExperienceDir(computerUseExperienceDirForTest)
		return
	}
	computeruse.SetExperienceDir(filepath.Join(corelib.MaclawDataDir(), "computer-use-experience"))
}

// computerUseRememberExperience stores one allowed lesson and returns the
// sentence that was written, or empty when the note was dropped.
func computerUseRememberExperience(app, note string, worked bool) string {
	computerUseOpenExperience()
	return computeruse.ExperienceRecord(app, note, worked)
}

func computerUseAppendExperience(msg, line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return msg
	}
	if strings.TrimSpace(msg) == "" {
		return "experience: " + line
	}
	return msg + "\nexperience: " + line
}

func computerUseExperiencePrompt(owner string) string {
	computerUseOpenExperience()
	goal := ""
	if st := snapshotComputerUseTaskState(owner); st != nil {
		goal = st.Goal
	}
	return computeruse.ExperiencePrompt(goal, nil)
}

// computerUseExperienceTrace is this task's focus / type / observe evidence.
// It is reset when a task begins and when the Computer Use session clears.
type computerUseExperienceTrace struct {
	focused  bool
	typed    bool
	observes int
}

var (
	computerUseTraceMu sync.Mutex
	computerUseTrace   computerUseExperienceTrace
)

func resetComputerUseExperienceTrace() {
	computerUseTraceMu.Lock()
	computerUseTrace = computerUseExperienceTrace{}
	computerUseTraceMu.Unlock()
}

func computerUseTraceFocus() {
	computerUseTraceMu.Lock()
	computerUseTrace.focused = true
	computerUseTrace.typed = false
	computerUseTraceMu.Unlock()
}

func computerUseTraceType() {
	computerUseTraceMu.Lock()
	if computerUseTrace.focused {
		computerUseTrace.typed = true
	}
	computerUseTraceMu.Unlock()
}

func computerUseTraceObserve() {
	computerUseTraceMu.Lock()
	computerUseTrace.observes++
	computerUseTraceMu.Unlock()
}

func computerUseTraceSnapshot() (focused, typed bool, observes int) {
	computerUseTraceMu.Lock()
	defer computerUseTraceMu.Unlock()
	return computerUseTrace.focused, computerUseTrace.typed, computerUseTrace.observes
}

// computerUseRememberVisionSearch records a search path when this vision
// turn focused an app and then typed, and the round has stayed short.
func computerUseRememberVisionSearch(window string) string {
	if !computerUseLLMSupportsVision() {
		return ""
	}
	focused, typed, observes := computerUseTraceSnapshot()
	note, ok := computeruse.ExperienceSearchProven(focused, typed, false, true, observes)
	if !ok {
		return ""
	}
	return computerUseRememberExperience(computeruse.ExperienceAppID(window), note, true)
}

// computerUseRememberFind records a find hit or miss. A hit always notes
// that on-screen text was found. The text-mode search-box lesson is added only
// when this round already focused, typed, and stayed under the observe cap.
// Vision mode records that lesson after focus and type, before the find hit.
func computerUseRememberFind(title, windowHint string, hit bool) string {
	app := computeruse.ExperienceAppID(title)
	if app == "" {
		app = computeruse.ExperienceAppID(windowHint)
	}
	var msg string
	if !hit {
		msg = computerUseAppendExperience("", computerUseRememberExperience(app, computeruse.NoteFindMissed, false))
	} else {
		msg = computerUseAppendExperience("", computerUseRememberExperience(app, computeruse.NoteFoundText, true))
		focused, typed, observes := computerUseTraceSnapshot()
		if note, ok := computeruse.ExperienceSearchProven(focused, typed, true, false, observes); ok {
			msg = computerUseAppendExperience(msg, computerUseRememberExperience(app, note, true))
		}
	}
	if msg == "" {
		return ""
	}
	return "\n" + msg
}
