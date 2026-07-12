package agent

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// PromptProfile selects how much system-prompt policy to load for a turn.
// Light turns skip enterprise/coding/SSH/MCP bulk so simple Q&A stays cheap.
type PromptProfile string

const (
	// PromptProfileFull is the default complete agent policy.
	PromptProfileFull PromptProfile = "full"
	// PromptProfileLight trims coding workflows, long evidence rules, MCP/skill
	// catalogs, and SSH policy. Keeps identity, anti-hallucination, core
	// principles (short), and project path context.
	PromptProfileLight PromptProfile = "light"
)

// PromptProfileEnvKey forces the adaptive system-prompt profile when set.
// Values: light | full | auto (or empty = auto classify).
const PromptProfileEnvKey = "MACLAW_PROMPT_PROFILE"

// PromptLightRetryEnvKey controls in-loop light→full recovery after a light
// tool deny. Default is on; set 0|off|false to disable.
const PromptLightRetryEnvKey = "MACLAW_PROMPT_LIGHT_RETRY"

// PromptABPercentEnvKey enables quality A/B sampling: when classify would pick
// light, force full for N% of turns (sticky by user-text hash) so operators can
// compare answer quality without a second model call on every turn.
// Values: 0..100 (default 0 = off).
const PromptABPercentEnvKey = "MACLAW_PROMPT_AB_PERCENT"

// LightToolRetryEnabled reports whether RunLoop should attempt light→full
// upgrade + tool re-authorize after a light allowlist deny.
func LightToolRetryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(PromptLightRetryEnvKey))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// PromptABSamplePercent returns 0..100 quality A/B sample rate from env.
func PromptABSamplePercent() int {
	raw := strings.TrimSpace(os.Getenv(PromptABPercentEnvKey))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// ShouldABSampleFull reports whether this userText is in the A/B force-full
// sample. Sticky by FNV-1a hash of trimmed text so the same question keeps the
// same arm within a process lifetime / across restarts.
func ShouldABSampleFull(userText string) bool {
	pct := PromptABSamplePercent()
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.TrimSpace(userText)))
	return int(h.Sum32()%100) < pct
}

// IsQualityABReason reports whether a classify/resolve reason is a quality A/B
// force-full sample (for Turn chip prompt=full(ab)).
func IsQualityABReason(reason string) bool {
	return strings.Contains(reason, "quality A/B sample")
}

// IsSoftFullUpgradeReason reports preemptive SoftFullAgentIntent upgrade
// (Turn chip prompt=full(soft)).
func IsSoftFullUpgradeReason(reason string) bool {
	return strings.Contains(reason, "soft full-agent intent")
}

// NormalizePromptProfile maps free-form strings to a known profile.
func NormalizePromptProfile(s string) PromptProfile {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(PromptProfileLight), "compact", "minimal", "fast":
		return PromptProfileLight
	default:
		return PromptProfileFull
	}
}

// EnvPromptProfileOverride returns a forced profile when MACLAW_PROMPT_PROFILE
// is set to light/full. auto/empty means no override.
func EnvPromptProfileOverride() (PromptProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(PromptProfileEnvKey))) {
	case string(PromptProfileLight), "compact", "minimal", "fast":
		return PromptProfileLight, true
	case string(PromptProfileFull), "heavy", "complete":
		return PromptProfileFull, true
	default:
		return "", false
	}
}

// PromptProfileFromTask maps a ClassifyTurn task type to a prompt profile.
func PromptProfileFromTask(task llm.TaskType) PromptProfile {
	switch task {
	case llm.TaskFast, llm.TaskIntent, llm.TaskSummary:
		return PromptProfileLight
	default:
		return PromptProfileFull
	}
}

// PromptProfileFromUserText classifies the user text and returns a profile.
// Never calls a model — uses the same heuristics as turn model routing.
// Prefer ResolvePromptProfile so MACLAW_PROMPT_PROFILE overrides apply.
func PromptProfileFromUserText(userText string, hints llm.ClassifyHints) (PromptProfile, llm.ClassifyResult) {
	classified := llm.ClassifyTurn(userText, hints)
	return PromptProfileFromTask(classified.Task), classified
}

// ResolvePromptProfile applies env override first, then ClassifyTurn heuristics.
// When classify picks light but SoftFullAgentIntent still matches (terse ops/
// shell phrases ClassifyTurn missed), upgrades to full and records a light_upgrade.
// Optional quality A/B (MACLAW_PROMPT_AB_PERCENT) forces full on a sticky sample
// of light-eligible turns so operators can compare answer quality.
func ResolvePromptProfile(userText string, hints llm.ClassifyHints) (PromptProfile, llm.ClassifyResult) {
	if p, ok := EnvPromptProfileOverride(); ok {
		return p, llm.ClassifyResult{
			Task:   llm.TaskDefault,
			Reason: PromptProfileEnvKey + "=" + string(p),
		}
	}
	p, c := PromptProfileFromUserText(userText, hints)
	if p.IsLight() && SoftFullAgentIntent(userText) {
		RecordLightUpgrade("soft_full_agent_intent")
		return PromptProfileFull, llm.ClassifyResult{
			Task:   llm.TaskReasoning,
			Reason: "soft full-agent intent upgrade from light",
		}
	}
	if p.IsLight() {
		// Eligible for light — count for A/B denominator, then maybe force full.
		RecordABEligibleLight()
		if ShouldABSampleFull(userText) {
			RecordABSampleFull()
			return PromptProfileFull, llm.ClassifyResult{
				Task:   c.Task,
				Reason: "quality A/B sample force full (" + PromptABPercentEnvKey + "=" + strconv.Itoa(PromptABSamplePercent()) + "%)",
			}
		}
	}
	return p, c
}

// SoftFullAgentIntent detects terse ops/shell/file requests that often misroute
// to light (short TaskFast) but need full tools. Kept narrower than ClassifyTurn
// reasoning cues so casual chat stays light.
func SoftFullAgentIntent(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	// Bare / short shell-ish commands
	switch strings.TrimSpace(lower) {
	case "ls", "pwd", "df", "top", "htop", "whoami", "ps", "env", "uname", "id":
		return true
	}
	for _, cue := range softFullAgentCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

var softFullAgentCues = []string{
	"ls -", "ls ", " pwd", "df -", "ps aux", "ps -", "chmod ", "chown ",
	"ssh ", "scp ", "rsync ", "systemctl ", "journalctl ",
	"write_file", "read_file", "edit_file", "apply_patch",
	"在终端", "运行命令", "执行命令", "命令行里", "帮我跑", "帮我执行",
}

// IsLight reports whether the profile should use the trimmed prompt path.
func (p PromptProfile) IsLight() bool {
	return NormalizePromptProfile(string(p)) == PromptProfileLight
}
