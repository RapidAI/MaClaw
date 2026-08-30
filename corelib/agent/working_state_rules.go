package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	errWorkingStateNil    = errors.New("working state is nil")
	errFocusRequired      = errors.New("focus label and fact are required")
	errLiveFull           = errors.New("live is full; swap explicitly")
	errSwapUnnamed        = errors.New("swap requires the outgoing label")
	errSwapMissing        = errors.New("swap target is not live")
	errPremiseMissing     = errors.New("settled label is not live")
	errSettledIncomplete  = errors.New("settled verifier and coverage are required")
	errOpenOnTrust        = errors.New("open cannot be written on trust")
	errUnknownRoundSignal = errors.New("unknown round signal")
)

var focusToolAllowlist = map[string]bool{
	"read_file":  true,
	"write_file": true,
	"edit_file":  true,
	"edit_lines": true,
}

var finishAllowlist = map[string]bool{
	"就这样":  true,
	"就这样吧": true,
	"不用了":  true,
}

// ExtractFocus returns a live item only for allowlisted file tools with a path.
func ExtractFocus(name, argsJSON string, outcome ToolExecutionOutcome) (FocusItem, bool) {
	if !focusToolAllowlist[strings.TrimSpace(name)] {
		return FocusItem{}, false
	}
	path := extractFocusPath(argsJSON)
	if path == "" {
		return FocusItem{}, false
	}
	label := NormalizeFocusLabel(path)
	if label == "" {
		return FocusItem{}, false
	}
	fact := clipRunes("path="+path+"；结果="+outcomeFact(outcome), workingStateFactMaxRunes)
	return FocusItem{Label: label, Fact: fact}, true
}

func extractFocusPath(argsJSON string) string {
	var obj map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &obj) == nil {
		for _, key := range []string{"path", "file_path"} {
			if s, ok := obj[key].(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
		// Valid JSON without a path must not scan "path" inside content.
		return ""
	}
	if p := strings.TrimSpace(extractJSONStringFieldFromRaw(argsJSON, "path")); p != "" {
		return p
	}
	return strings.TrimSpace(extractJSONStringFieldFromRaw(argsJSON, "file_path"))
}

// NormalizeFocusLabel cleans a path so the same file occupies one live slot.
func NormalizeFocusLabel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	if utf8.RuneCountInString(cleaned) > workingStateLabelMaxRunes {
		return clipRunesSuffix(cleaned, workingStateLabelMaxRunes)
	}
	return cleaned
}

// AdmitLive adds or updates a live item. A new label is rejected when live is full.
func AdmitLive(state *WorkingState, item FocusItem) error {
	if state == nil {
		return errWorkingStateNil
	}
	item.Label = strings.TrimSpace(item.Label)
	item.Fact = strings.TrimSpace(item.Fact)
	if item.Label == "" || item.Fact == "" {
		return errFocusRequired
	}
	for i, live := range state.Live {
		if live.Label == item.Label {
			state.Live[i] = item
			pointNextAtLabel(state, item.Label)
			state.touch()
			return nil
		}
	}
	if len(state.Live) >= workingStateMaxLive {
		return errLiveFull
	}
	state.Live = append(state.Live, item)
	pointNextAtLabel(state, item.Label)
	state.touch()
	return nil
}

// SwapLive replaces a named live slot.
func SwapLive(state *WorkingState, outLabel string, in FocusItem) error {
	if state == nil {
		return errWorkingStateNil
	}
	outLabel = strings.TrimSpace(outLabel)
	if outLabel == "" {
		return errSwapUnnamed
	}
	idx := -1
	for i, live := range state.Live {
		if live.Label == outLabel {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errSwapMissing
	}
	outgoing := state.Live[idx]
	state.Live = append(state.Live[:idx], state.Live[idx+1:]...)
	if err := AdmitLive(state, in); err != nil {
		restored := make([]FocusItem, 0, len(state.Live)+1)
		restored = append(restored, state.Live[:idx]...)
		restored = append(restored, outgoing)
		restored = append(restored, state.Live[idx:]...)
		state.Live = restored
		return err
	}
	return nil
}

// AdmitLiveEvictOldest is the only automatic eviction: it names Live[0].
func AdmitLiveEvictOldest(state *WorkingState, item FocusItem) error {
	if state == nil {
		return errWorkingStateNil
	}
	item.Label = strings.TrimSpace(item.Label)
	for _, live := range state.Live {
		if live.Label == item.Label {
			return AdmitLive(state, item)
		}
	}
	if len(state.Live) >= workingStateMaxLive {
		return SwapLive(state, state.Live[0].Label, item)
	}
	return AdmitLive(state, item)
}

// PremiseBeforeUse requires the label to already be live.
func PremiseBeforeUse(state *WorkingState, label string) error {
	if state == nil {
		return errWorkingStateNil
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return errPremiseMissing
	}
	for _, live := range state.Live {
		if live.Label == label {
			return nil
		}
	}
	return errPremiseMissing
}

// AdmitSettled records a verified claim that already occupies live.
func AdmitSettled(state *WorkingState, settled Settled) error {
	if err := PremiseBeforeUse(state, settled.Label); err != nil {
		return err
	}
	if strings.TrimSpace(settled.Verifier) == "" || strings.TrimSpace(settled.Coverage) == "" {
		return errSettledIncomplete
	}
	settled.Label = strings.TrimSpace(settled.Label)
	settled.Claim = strings.TrimSpace(settled.Claim)
	if settled.Claim == "" {
		settled.Claim = settled.Label
	}
	for i, existing := range state.Settled {
		if existing.Label == settled.Label {
			if strings.TrimSpace(settled.ID) == "" {
				settled.ID = existing.ID
			}
			// Move the refreshed claim to the end so a later third file
			// evicts the stale one, not the file just re-verified.
			kept := append(append([]Settled(nil), state.Settled[:i]...), state.Settled[i+1:]...)
			state.Settled = append(kept, settled)
			state.touch()
			return nil
		}
	}
	if strings.TrimSpace(settled.ID) == "" {
		settled.ID = nextSettledID(state)
	}
	state.Settled = append(state.Settled, settled)
	if len(state.Settled) > workingStateMaxSettled {
		state.Settled = append([]Settled(nil), state.Settled[len(state.Settled)-workingStateMaxSettled:]...)
	}
	state.touch()
	return nil
}

func nextSettledID(state *WorkingState) string {
	max := 0
	if state != nil {
		for _, item := range state.Settled {
			id := strings.TrimSpace(item.ID)
			if !strings.HasPrefix(id, "s") {
				continue
			}
			n, err := strconv.Atoi(id[1:])
			if err != nil || n <= max {
				continue
			}
			max = n
		}
	}
	return "s" + strconv.Itoa(max+1)
}

// CloseOpenOnTrust closes unclosed opens for toolName.
func CloseOpenOnTrust(state *WorkingState, toolName, settledID string) {
	if state == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return
	}
	if strings.TrimSpace(settledID) == "" {
		settledID = "trust:" + toolName
	}
	for i := range state.Open {
		if state.Open[i].ClosedBy == "" && state.Open[i].Tool == toolName {
			state.Open[i].ClosedBy = settledID
		}
	}
	state.touch()
}

// AddOpen records an unresolved item. Trust rejects it.
func AddOpen(state *WorkingState, item OpenItem) error {
	if state == nil {
		return errWorkingStateNil
	}
	if state.LastAction == ActionTrust {
		return errOpenOnTrust
	}
	item.Tool = strings.TrimSpace(item.Tool)
	item.Question = strings.TrimSpace(item.Question)
	item.SettleBy = strings.TrimSpace(item.SettleBy)
	if item.Question == "" && item.SettleBy == "" {
		return errFocusRequired
	}
	if UnclosedOpenCount(state) >= workingStateMaxOpen {
		return nil
	}
	state.Open = append(state.Open, item)
	state.touch()
	return nil
}

// UnclosedOpenCount returns still-open items.
func UnclosedOpenCount(state *WorkingState) int {
	if state == nil {
		return 0
	}
	n := 0
	for _, item := range state.Open {
		if strings.TrimSpace(item.ClosedBy) == "" {
			n++
		}
	}
	return n
}

// SelectAction is deterministic: first matching row wins.
func SelectAction(sig RoundSignal) (ControlAction, error) {
	switch {
	case sig.Kind == RoundToolOK:
		return ActionTrust, nil
	case isFailRound(sig.Kind) && sig.Prev == ActionEmpiric && sig.SameSigCount >= 2:
		return ActionSeekUser, nil
	case isFailRound(sig.Kind) && sig.OpenCount >= 2:
		return ActionSeekUser, nil
	case isFailRound(sig.Kind) && sig.SameSigCount <= 1:
		return ActionRetryDiagnose, nil
	case isFailRound(sig.Kind) && sig.SameSigCount == 2:
		return ActionReroute, nil
	case isFailRound(sig.Kind) && sig.SameSigCount >= 3:
		return ActionSeekUser, nil
	case sig.Kind == RoundLLMEmpty && sig.EmptyCount <= 1:
		return ActionEmpiric, nil
	case sig.Kind == RoundLLMEmpty:
		return ActionSeekUser, nil
	default:
		return "", errUnknownRoundSignal
	}
}

// ApplyControlAction writes LastAction, Next, and optional Open/Settled.
func ApplyControlAction(state *WorkingState, action ControlAction, sig RoundSignal) error {
	if state == nil {
		return errWorkingStateNil
	}
	state.LastAction = action
	state.touch()
	if action == ActionTrust {
		settledID := ""
		if label := strings.TrimSpace(sig.FocusLabel); label != "" {
			settled := Settled{
				Label:    label,
				Claim:    label,
				Verifier: strings.TrimSpace(sig.ToolName) + "+ok",
				Coverage: "this-turn " + label,
			}
			if err := AdmitSettled(state, settled); err == nil && len(state.Settled) > 0 {
				settledID = state.Settled[len(state.Settled)-1].ID
			}
		}
		CloseOpenOnTrust(state, sig.ToolName, settledID)
		state.Next = nextContinue(state, sig.ToolName, sig.FocusLabel)
		return nil
	}
	if action == ActionSeekUser && sig.OpenCount >= 2 {
		state.Next = nextSeekUser()
		return nil
	}
	if err := AddOpen(state, openForAction(action, sig)); err != nil {
		return err
	}
	state.Next = nextForAction(action, sig, state.Goal)
	return nil
}

// AccountToolSignature increments only on failure. Success resets the count.
func AccountToolSignature(state *WorkingState, name, argsJSON string, failed bool) int {
	if state == nil {
		return 0
	}
	sig := ToolCallSignature(name, argsJSON)
	if !failed {
		state.LastSig = sig
		state.SigCount = 0
		state.touch()
		return 0
	}
	if sig == state.LastSig {
		state.SigCount++
	} else {
		state.LastSig = sig
		state.SigCount = 1
	}
	state.touch()
	return state.SigCount
}

// ToolCallSignature is name + canonical JSON, or SHA-256 of raw args.
func ToolCallSignature(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	var decoded interface{}
	if json.Unmarshal([]byte(argsJSON), &decoded) == nil {
		if canon, err := json.Marshal(decoded); err == nil {
			return name + "\n" + string(canon)
		}
	}
	sum := sha256.Sum256([]byte(argsJSON))
	return name + "\n" + hex.EncodeToString(sum[:])
}

// ShouldBlockFinish implements the one-shot done-check.
func ShouldBlockFinish(state *WorkingState, userText string, attached bool) bool {
	if !attached || state == nil {
		return false
	}
	if finishAllowlist[strings.TrimSpace(userText)] {
		return false
	}
	if UnclosedOpenCount(state) == 0 {
		return false
	}
	if state.FinishNudges >= 1 {
		return false
	}
	return true
}

// ClearLiveAndOpen is the steer/replan mutation: keep Goal and Settled.
func ClearLiveAndOpen(state *WorkingState) {
	if state == nil {
		return
	}
	state.Live = nil
	kept := state.Open[:0]
	for _, item := range state.Open {
		if strings.TrimSpace(item.ClosedBy) != "" {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		state.Open = nil
	} else {
		state.Open = kept
	}
	// Next/LastAction must not keep pointing at a Live slot that was just dropped.
	// The one-shot finish nudge was about the opens we just dropped; a later
	// fail after steer still gets one done-check.
	state.Next = nextContinue(state, "", "")
	state.LastAction = ""
	state.FinishNudges = 0
	state.touch()
}

// SameSignatureForbidMessage is injected after the tool batch, not between results.
func SameSignatureForbidMessage(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "该工具"
	}
	return "[系统] 禁止再次使用相同参数调用 " + toolName + "。请换路或询问用户。"
}

// FinishNudgeMessage is the one-shot done-check prompt. It must not copy the section.
func FinishNudgeMessage() string {
	return "[系统] 还有未关闭问题，先核验或问用户。"
}

// AppendNextHint adds a single Next line to a user inject.
func AppendNextHint(prefix string, state *WorkingState) string {
	if state == nil {
		return prefix
	}
	next := strings.TrimSpace(state.Next)
	if next == "" {
		return prefix
	}
	if strings.TrimSpace(prefix) == "" {
		return "下一步：" + next
	}
	return strings.TrimRight(prefix, "\n") + "\n下一步：" + next
}

func pointNextAtLabel(state *WorkingState, label string) {
	if state == nil {
		return
	}
	state.Next = "用 " + label
}

func nextContinue(state *WorkingState, toolName, focusLabel string) string {
	if label := strings.TrimSpace(focusLabel); label != "" {
		return "按 " + label + " 继续"
	}
	if strings.TrimSpace(toolName) != "" && state != nil && strings.TrimSpace(state.Goal) != "" {
		if isDiscoveryProgressTool(toolName) {
			return "继续完成 " + state.Goal
		}
		return "根据工具 " + toolName + " 的 ok 继续完成 " + state.Goal
	}
	if state != nil && strings.TrimSpace(state.Goal) != "" {
		return "继续完成 " + state.Goal
	}
	return "继续当前任务"
}

func isDiscoveryProgressTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "discover_tool", "search_and_install_skill", "search_skill_hub", "list_mcp_tools":
		return true
	default:
		return false
	}
}

func nextSeekUser() string {
	return "询问用户后继续"
}

func nextForAction(action ControlAction, sig RoundSignal, goal string) string {
	tool := strings.TrimSpace(sig.ToolName)
	if tool == "" {
		tool = "工具"
	}
	switch action {
	case ActionRetryDiagnose:
		return "诊断 " + tool + " 失败原因并改范围"
	case ActionReroute:
		return "换参数或换工具，不要重复 " + tool
	case ActionEmpiric:
		return "列出不超过3个候选，并写核验步骤"
	case ActionSeekUser:
		return nextSeekUser()
	default:
		if strings.TrimSpace(goal) != "" {
			return "继续完成 " + goal
		}
		return "继续当前任务"
	}
}

func openForAction(action ControlAction, sig RoundSignal) OpenItem {
	tool := strings.TrimSpace(sig.ToolName)
	switch action {
	case ActionRetryDiagnose:
		return OpenItem{Tool: tool, Question: roundKindLabel(sig.Kind), SettleBy: "将改范围"}
	case ActionReroute:
		return OpenItem{Tool: tool, Question: "同签名再次失败", SettleBy: "换参或换工具"}
	case ActionEmpiric:
		return OpenItem{Tool: tool, Question: "无新约束", SettleBy: "有限候选并核验"}
	default:
		return OpenItem{Tool: tool, Question: "需要用户", SettleBy: openSettleByUserReply}
	}
}

func isFailRound(kind string) bool {
	return kind == RoundToolError || kind == RoundToolTimeout
}

func outcomeFact(outcome ToolExecutionOutcome) string {
	switch outcome {
	case ToolExecutionOutcomeOK:
		return "ok"
	case ToolExecutionOutcomeTimeout:
		return "timeout"
	default:
		return "error"
	}
}

func roundKindFromOutcome(outcome ToolExecutionOutcome) string {
	switch outcome {
	case ToolExecutionOutcomeOK:
		return RoundToolOK
	case ToolExecutionOutcomeTimeout:
		return RoundToolTimeout
	default:
		return RoundToolError
	}
}

func roundKindLabel(kind string) string {
	switch kind {
	case RoundToolTimeout:
		return "timeout"
	case RoundLLMEmpty:
		return "empty"
	default:
		return "error"
	}
}
