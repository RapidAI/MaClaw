package skill

// solidify.go implements the craft_tool → bash step promotion mechanism.
//
// Three-stage pipeline:
//   Stage 1 (Record):  After each successful craft_tool execution, record the
//                       script and compute its structural signature.
//   Stage 2 (Promote): When SuccessCount >= threshold AND all recorded scripts
//                       share the same structural signature, promote to bash.
//   Stage 3 (Revert):  If the promoted bash step fails, revert to craft_tool.
//
// The structural signature is the key mechanism that prevents premature
// promotion. SkVM's Code Solidification requires "code signature matching" —
// the LLM must produce structurally identical code across invocations before
// promotion. Without this, three different scripts that happen to succeed
// would be promoted, and the third (arbitrary) script would fail on the
// fourth invocation's different parameters.
//
// Signature computation: normalize the script by stripping comments, collapsing
// whitespace, and replacing string literals with placeholders. Two scripts
// with the same structure but different parameter values produce the same
// signature. Two scripts with different logic produce different signatures.

import (
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SolidificationThreshold is the number of consecutive successful craft_tool
// executions with matching structural signatures required before promotion.
const SolidificationThreshold = 3

// RecordCraftSuccess records a successful craft_tool execution.
// Returns true if the skill was modified (caller should persist).
//
// The script content is used to compute a structural signature. If the
// signature differs from the previous recording, SuccessCount resets to 1
// (the code structure changed, so the streak is broken).
func RecordCraftSuccess(skill *corelib.NLSkillEntry, stepIndex int, scriptPath, scriptContent, language string) bool {
	if skill == nil || stepIndex < 0 || stepIndex >= len(skill.Steps) {
		return false
	}
	if skill.Steps[stepIndex].Action != "craft_tool" {
		return false
	}

	sig := computeStructuralSignature(scriptContent, language)

	var candidate *corelib.SolidificationCandidate
	for i := range skill.SolidificationCandidates {
		if skill.SolidificationCandidates[i].StepIndex == stepIndex {
			candidate = &skill.SolidificationCandidates[i]
			break
		}
	}
	if candidate == nil {
		skill.SolidificationCandidates = append(skill.SolidificationCandidates, corelib.SolidificationCandidate{
			StepIndex: stepIndex,
		})
		candidate = &skill.SolidificationCandidates[len(skill.SolidificationCandidates)-1]
	}

	// Signature mismatch → code structure changed → reset streak.
	if candidate.Signature != "" && candidate.Signature != sig {
		log.Printf("[solidify] skill=%s step=%d signature changed (%s → %s), resetting streak",
			skill.Name, stepIndex, candidate.Signature[:8], sig[:8])
		candidate.SuccessCount = 0
	}

	candidate.ScriptPath = scriptPath
	candidate.Language = language
	candidate.Signature = sig
	candidate.SuccessCount++
	candidate.LastUsed = time.Now().Format(time.RFC3339)

	if len(candidate.ParamSlots) == 0 {
		candidate.ParamSlots = extractCraftParamSlots(skill.Steps[stepIndex])
	}

	log.Printf("[solidify] skill=%s step=%d success_count=%d/%d sig=%s",
		skill.Name, stepIndex, candidate.SuccessCount, SolidificationThreshold, sig[:8])

	return true
}

// TrySolidify checks if any craft_tool steps are ready for promotion.
// Returns true if the skill was modified.
func TrySolidify(skill *corelib.NLSkillEntry) bool {
	modified := false
	for _, candidate := range skill.SolidificationCandidates {
		if candidate.SuccessCount < SolidificationThreshold {
			continue
		}
		if candidate.StepIndex < 0 || candidate.StepIndex >= len(skill.Steps) {
			continue
		}
		step := &skill.Steps[candidate.StepIndex]
		if step.Action != "craft_tool" {
			continue
		}

		bashCmd := buildSolidifiedCommand(candidate)
		if bashCmd == "" {
			continue
		}

		originalStep := *step
		step.FallbackStep = &originalStep
		step.Action = "bash"
		step.Params = map[string]interface{}{"command": bashCmd}

		log.Printf("[solidify] PROMOTED skill=%s step=%d: craft_tool → bash (sig=%s, %d consecutive)",
			skill.Name, candidate.StepIndex, candidate.Signature[:8], candidate.SuccessCount)
		modified = true
	}
	return modified
}

// RevertSolidification reverts a promoted bash step back to craft_tool.
// Returns true if the skill was modified.
func RevertSolidification(skill *corelib.NLSkillEntry, stepIndex int) bool {
	if skill == nil || stepIndex < 0 || stepIndex >= len(skill.Steps) {
		return false
	}
	step := &skill.Steps[stepIndex]
	if step.FallbackStep == nil {
		return false
	}

	log.Printf("[solidify] REVERTED skill=%s step=%d: bash → %s",
		skill.Name, stepIndex, step.FallbackStep.Action)

	*step = *step.FallbackStep
	step.FallbackStep = nil
	ResetCandidate(skill, stepIndex)
	return true
}

// ResetCandidate resets the solidification candidate for a step.
func ResetCandidate(skill *corelib.NLSkillEntry, stepIndex int) {
	for i := range skill.SolidificationCandidates {
		if skill.SolidificationCandidates[i].StepIndex == stepIndex {
			skill.SolidificationCandidates[i].SuccessCount = 0
			skill.SolidificationCandidates[i].ScriptPath = ""
			skill.SolidificationCandidates[i].Signature = ""
			return
		}
	}
}

// HasFallbackStep returns true if the step has a fallback (was promoted).
func HasFallbackStep(skill *corelib.NLSkillEntry, stepIndex int) bool {
	if skill == nil || stepIndex < 0 || stepIndex >= len(skill.Steps) {
		return false
	}
	return skill.Steps[stepIndex].FallbackStep != nil
}

// --- Structural Signature ---

// computeStructuralSignature normalizes a script and hashes it.
// Two scripts with the same logic but different parameter values produce
// the same signature. Two scripts with different logic produce different ones.
//
// Normalization steps:
//  1. Strip comments (# for python/bash, // for JS)
//  2. Replace string literals with a placeholder
//  3. Collapse whitespace
//  4. SHA-256 hash
func computeStructuralSignature(script, language string) string {
	if script == "" {
		return "empty"
	}
	normalized := normalizeScript(script, language)
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash[:16]) // 128-bit, 32 hex chars
}

var (
	// Matches Python/bash single-line comments.
	hashCommentRe = regexp.MustCompile(`(?m)#[^\n]*$`)
	// Matches JS/TS single-line comments.
	slashCommentRe = regexp.MustCompile(`(?m)//[^\n]*$`)
	// Matches double-quoted strings (non-greedy).
	doubleQuoteRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	// Matches single-quoted strings (non-greedy).
	singleQuoteRe = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	// Matches consecutive whitespace.
	solidifyWhitespaceRe = regexp.MustCompile(`\s+`)
)

func normalizeScript(script, language string) string {
	s := script

	// Order matters: replace string literals BEFORE stripping comments.
	// If a string contains '#' (e.g., color codes "#ff0000"), the comment
	// regex would eat the rest of the line including the closing quote,
	// corrupting subsequent string replacement.
	s = doubleQuoteRe.ReplaceAllString(s, `"_"`)
	s = singleQuoteRe.ReplaceAllString(s, `'_'`)

	// Strip comments based on language.
	switch strings.ToLower(language) {
	case "python", "python3", "bash", "sh":
		s = hashCommentRe.ReplaceAllString(s, "")
	case "node", "javascript", "js":
		s = slashCommentRe.ReplaceAllString(s, "")
	default:
		s = hashCommentRe.ReplaceAllString(s, "")
		s = slashCommentRe.ReplaceAllString(s, "")
	}

	// Collapse whitespace.
	s = solidifyWhitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// --- Command building ---

func buildSolidifiedCommand(candidate corelib.SolidificationCandidate) string {
	if candidate.ScriptPath == "" {
		return ""
	}

	scriptPath := filepath.ToSlash(candidate.ScriptPath)
	quoted := scriptPath
	if strings.ContainsAny(scriptPath, " \t") {
		quoted = fmt.Sprintf(`"%s"`, scriptPath)
	}

	var cmd string
	switch strings.ToLower(candidate.Language) {
	case "python", "python3":
		cmd = "python " + quoted
	case "node", "javascript", "js":
		cmd = "node " + quoted
	case "bash", "sh":
		cmd = "bash " + quoted
	case "powershell", "ps1":
		cmd = "powershell -File " + quoted
	default:
		cmd = quoted
	}

	for _, slot := range candidate.ParamSlots {
		cmd += fmt.Sprintf(" {{%s}}", slot)
	}
	return cmd
}

func extractCraftParamSlots(step corelib.NLSkillStep) []string {
	var slots []string
	seen := make(map[string]bool)
	for _, key := range []string{"task", "instructions"} {
		text, _ := step.Params[key].(string)
		if text == "" {
			continue
		}
		for _, k := range ExtractPlaceholderKeys(text) {
			if !seen[k] {
				seen[k] = true
				slots = append(slots, k)
			}
		}
	}
	return slots
}
