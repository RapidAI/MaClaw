package petpack

// Action sounds are host-synthesized tones. A pack names a built-in cue or
// writes a short explicit sequence. It cannot ship audio files, and the host
// refuses to play anything that failed ValidateRig.

const (
	minActionToneHz  = 200
	maxActionToneHz  = 2400
	minActionToneMs  = 12
	maxActionToneMs  = 180
	maxActionSoundMs = 420
)

// actionCues are distinct enough that a walk, a jump, and a bow do not share
// one chime. Frequencies stay in a desktop-speaker range: low enough to read
// as a land or a bow, high enough to read as a blink.
var actionCues = map[string][]RigTone{
	"step":      {{Hz: 740, Ms: 26}, {Hz: 620, Ms: 22}},
	"run":       {{Hz: 880, Ms: 16}, {Hz: 760, Ms: 14}, {Hz: 990, Ms: 16}},
	"jump":      {{Hz: 660, Ms: 36}, {Hz: 1180, Ms: 64}},
	"land":      {{Hz: 320, Ms: 34}, {Hz: 240, Ms: 28}},
	"bend":      {{Hz: 520, Ms: 48}, {Hz: 390, Ms: 64}},
	"stretch":   {{Hz: 784, Ms: 36}, {Hz: 988, Ms: 56}},
	"blink":     {{Hz: 1480, Ms: 16}},
	"greet":     {{Hz: 1046, Ms: 36}, {Hz: 1318, Ms: 44}, {Hz: 1568, Ms: 56}},
	"notice":    {{Hz: 1174, Ms: 28}, {Hz: 1396, Ms: 36}},
	"brace":     {{Hz: 349, Ms: 40}},
	"release":   {{Hz: 523, Ms: 28}, {Hz: 659, Ms: 36}},
	"focus":     {{Hz: 698, Ms: 32}, {Hz: 880, Ms: 44}},
	"celebrate": {{Hz: 784, Ms: 36}, {Hz: 1046, Ms: 36}, {Hz: 1318, Ms: 64}},
	"concern":   {{Hz: 466, Ms: 56}, {Hz: 392, Ms: 72}},
	"rest":      {{Hz: 494, Ms: 80}},
	"alert":     {{Hz: 880, Ms: 28}, {Hz: 880, Ms: 28}, {Hz: 1174, Ms: 48}},
	"speak":     {{Hz: 988, Ms: 24}, {Hz: 880, Ms: 22}},
	// sing is a short melody, distinct from the two-note speak blip. It is the
	// one action cue the Windows host still plays while the pet is speaking.
	"sing": {{Hz: 523, Ms: 80}, {Hz: 659, Ms: 70}, {Hz: 784, Ms: 80}, {Hz: 1046, Ms: 100}},
}

// ClampActionToneHz limits a host beep to the range ValidateRig accepts.
func ClampActionToneHz(hz float64) float64 {
	if hz < minActionToneHz {
		return minActionToneHz
	}
	if hz > maxActionToneHz {
		return maxActionToneHz
	}
	return hz
}

// IsActionCue reports whether name is a built-in per-action sound.
func IsActionCue(name string) bool {
	_, ok := actionCues[name]
	return ok
}

// ResolveActionSound returns the tones for a clip. An explicit sequence wins
// over a cue name. The result is a copy; callers can scale pitch without
// mutating the catalog.
func ResolveActionSound(cue string, explicit []RigTone) []RigTone {
	if len(explicit) > 0 {
		out := make([]RigTone, len(explicit))
		copy(out, explicit)
		return out
	}
	tones, ok := actionCues[cue]
	if !ok || len(tones) == 0 {
		return nil
	}
	out := make([]RigTone, len(tones))
	copy(out, tones)
	return out
}
