package browser

import (
	"fmt"
	"strings"
)

// Native key chords a publish strategy may press in an editor (runtime
// recovery parameters). Only caret movement, Enter and Escape: nothing that
// deletes, submits or types text.
var chordKeys = map[string]struct {
	code string
	vk   int
}{
	"End": {"End", 35}, "Home": {"Home", 36}, "Enter": {"Enter", 13},
	"ArrowDown": {"ArrowDown", 40}, "ArrowUp": {"ArrowUp", 38}, "ArrowLeft": {"ArrowLeft", 37}, "ArrowRight": {"ArrowRight", 39},
	"PageDown": {"PageDown", 34}, "PageUp": {"PageUp", 33}, "Escape": {"Escape", 27},
}

var chordCommands = map[string]string{
	"End": "moveToEndOfLine", "ctrl+End": "moveToEndOfDocument",
	"Home": "moveToBeginningOfLine", "ctrl+Home": "moveToBeginningOfDocument",
}

// ParseKeyChord reads "ctrl+End", "shift+ArrowDown", "Enter".
func ParseKeyChord(s string) (key string, mods int, err error) {
	parts := strings.Split(strings.TrimSpace(s), "+")
	key = parts[len(parts)-1]
	if _, ok := chordKeys[key]; !ok {
		return "", 0, fmt.Errorf("key %q is not allowed (End, Home, Enter, Arrow*, PageUp, PageDown, Escape with ctrl/shift/alt)", key)
	}
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "alt":
			mods |= 1
		case "ctrl", "control":
			mods |= 2
		case "shift":
			mods |= 8
		default:
			return "", 0, fmt.Errorf("modifier %q is not allowed", m)
		}
	}
	if key == "Enter" && mods&3 != 0 {
		// Ctrl/Alt+Enter submits forms on many sites: never a caret move.
		return "", 0, fmt.Errorf("%s is not allowed (it can submit)", s)
	}
	return key, mods, nil
}

// pressChord sends one chord as trusted key events.
func (p *publishRun) pressChord(chord string) error {
	key, mods, err := ParseKeyChord(chord)
	if err != nil {
		return err
	}
	k := chordKeys[key]
	var cmds []string
	norm := key
	if mods == 2 {
		norm = "ctrl+" + key
	}
	if c, ok := chordCommands[norm]; ok && (mods == 0 || mods == 2) {
		cmds = []string{c}
	}
	return p.key(key, k.code, k.vk, mods, cmds)
}
