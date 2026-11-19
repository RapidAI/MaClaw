package guiapp

// coding_plan_contract.go implements the frozen execution contract behind
// /plan approve (Voder's FrozenContract, scoped to this codebase's threat
// model): approving a plan compiles an immutable contract binding the run to
// the exact approved payload, its frozen write scope, the explicit input
// files the request named, and any labeled validation commands. The model
// cannot weaken it after approval; the host enforces it at dispatch and runs
// the validators itself at completion.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// codingExecutionContract is the immutable result of approving a pending
// plan. There is intentionally no setter or weakening API: the approved plan
// payload, the workspace, and the declared checks are fixed at approval.
type codingExecutionContract struct {
	// PlanDigest binds this run to the exact approved plan payload.
	PlanDigest string
	// ProjectPath is the approved write scope, frozen at approval time.
	ProjectPath string
	// RequiredFiles are the explicit input files the approved request named
	// (@path references and <<File>> markers). Model-generated plan markdown
	// can name them only through the deliberate <<File>> syntax — an @-path
	// in generated text is never promoted to a hard requirement.
	RequiredFiles []string
	// MissingRequiredFiles is the subset of RequiredFiles absent at compile
	// time. Non-empty blocks dispatch (fail closed).
	MissingRequiredFiles []string
	// Validators are labeled validation commands extracted from the approved
	// plan text. The host runs them after the plan completes; their result
	// is the only "verified" signal this path reports. No validators means
	// the outcome is reported unverified rather than failing compilation —
	// natural-language requirements must not force vacuous checks.
	Validators []string
	CompiledAt int64
}

var (
	planFileMarkerRe = regexp.MustCompile(`<<File name="([^"]{1,240})">>`)
	planAtRefRe      = regexp.MustCompile(`@([A-Za-z0-9_.\-/]+\.[A-Za-z0-9]{1,12})`)
	planValidatorRe  = regexp.MustCompile(`(?im)^\s*(?:验证命令|验证脚本|验收命令|check|checks|validation)\s*[:：]\s*(\S.{0,299})\s*$`)
)

const (
	planContractMaxRequiredFiles = 24
	planContractMaxValidators    = 3
	planContractValidatorTimeout = 300 // seconds per validator
)

// compileCodingExecutionContract freezes the approved plan into a dispatch
// contract. Compilation never partially succeeds: the contract records every
// detected problem at once (all missing inputs), like Voder's compile — a
// caller either dispatches a clean contract or reports the whole failure set.
func compileCodingExecutionContract(pending codingWorkbenchPendingPlan, projectPath string) codingExecutionContract {
	raw, _ := json.Marshal(pending)
	sum := sha256.Sum256(raw)
	contract := codingExecutionContract{
		PlanDigest:    fmt.Sprintf("%x", sum),
		ProjectPath:   strings.TrimSpace(projectPath),
		RequiredFiles: inferApprovedPlanRequiredFiles(pending),
		Validators:    inferApprovedPlanValidators(pending),
		CompiledAt:    time.Now().Unix(),
	}
	for _, ref := range contract.RequiredFiles {
		if !approvedPlanInputExists(contract.ProjectPath, ref) {
			contract.MissingRequiredFiles = append(contract.MissingRequiredFiles, ref)
		}
	}
	return contract
}

// inferApprovedPlanRequiredFiles extracts only explicit file references from
// the approved plan: @path refs and <<File>> markers in the user-authored
// request, and <<File>> markers in the generated markdown. It never guesses
// filenames from ordinary prose.
func inferApprovedPlanRequiredFiles(pending codingWorkbenchPendingPlan) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if !validApprovedPlanFileRef(name) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	// <<File>> markers are deliberate attachments in either source.
	for _, m := range planFileMarkerRe.FindAllStringSubmatch(pending.UserText, -1) {
		add(m[1])
	}
	for _, m := range planFileMarkerRe.FindAllStringSubmatch(pending.Markdown, -1) {
		add(m[1])
	}
	// @path refs only count in the user-authored request; generated markdown
	// routinely names files the plan itself will create.
	for _, idx := range planAtRefRe.FindAllStringSubmatchIndex(pending.UserText, -1) {
		if idx[0] > 0 && isPlanRefByte(pending.UserText[idx[0]-1]) {
			continue
		}
		if idx[1] < len(pending.UserText) && isPlanRefByte(pending.UserText[idx[1]]) {
			continue
		}
		add(pending.UserText[idx[2]:idx[3]])
	}
	if len(out) > planContractMaxRequiredFiles {
		out = out[:planContractMaxRequiredFiles]
	}
	return out
}

// validApprovedPlanFileRef is the lexical scope check for an explicit file
// reference: relative, forward-slash, no traversal, no device syntax.
func validApprovedPlanFileRef(name string) bool {
	if name == "" || len(name) > 240 {
		return false
	}
	// ':' covers drive letters (C:), alternate data streams, and device names.
	if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "~") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func isPlanRefByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func approvedPlanInputExists(projectPath, ref string) bool {
	if strings.TrimSpace(projectPath) == "" {
		return false
	}
	abs := filepath.Join(projectPath, filepath.FromSlash(ref))
	info, err := os.Stat(abs)
	return err == nil && !info.IsDir()
}

// inferApprovedPlanValidators extracts explicitly labeled validation
// commands. Only a labeled line counts — unlabeled code in a plan stays plan
// text, never an obligation the host runs on the user's workspace. Fenced
// code blocks are stripped first: a "验证命令:" line inside ```...``` is an
// example or document fragment, not a command the host may execute.
func inferApprovedPlanValidators(pending codingWorkbenchPendingPlan) []string {
	var out []string
	seen := map[string]bool{}
	for _, text := range []string{pending.UserText, pending.Markdown} {
		text = stripApprovedPlanFencedBlocks(text)
		for _, m := range planValidatorRe.FindAllStringSubmatch(text, -1) {
			cmd := strings.TrimSpace(m[1])
			if cmd == "" || seen[cmd] {
				continue
			}
			seen[cmd] = true
			out = append(out, cmd)
			if len(out) >= planContractMaxValidators {
				return out
			}
		}
	}
	return out
}

// stripApprovedPlanFencedBlocks removes ``` and ~~~ fenced regions from plan
// text so labeled lines inside quoted examples cannot become host-executed
// validators.
func stripApprovedPlanFencedBlocks(text string) string {
	var b strings.Builder
	inFence := false
	var fenceMarker string
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case !inFence && (strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~")):
			inFence = true
			fenceMarker = trim[:3]
		case inFence && strings.HasPrefix(trim, fenceMarker):
			inFence = false
		case !inFence:
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// storeApprovedPlanValidators freezes the contract's validators into sticky
// session memory so they survive session-round re-arms: a plan that exhausts
// one round's iteration budget and continues is verified at its terminal
// round, not abandoned mid-chain.
func (h *IMMessageHandler) storeApprovedPlanValidators(userID string, validators []string, projectPath string) {
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ApprovedPlanValidators = append([]string(nil), validators...)
		m.ApprovedPlanValidatorDir = projectPath
	})
}

func (h *IMMessageHandler) clearApprovedPlanValidators(userID string) {
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ApprovedPlanValidators = nil
		m.ApprovedPlanValidatorDir = ""
	})
}

// finishApprovedPlanValidators fires the stored validators at the approved
// plan's terminal round and appends the measured result to the response —
// the "fresh meta-audit before delivery" stage: the verified label is earned
// here, not asserted by the agent's summary. planTurn must report whether
// this turn actually executed the approved plan's tasks (first dispatch or
// a resuming continuation); an unrelated terminal turn drops the stale
// pending validators rather than stamping a verdict on the wrong work.
func (h *IMMessageHandler) finishApprovedPlanValidators(userID string, resp *IMAgentResponse, planTurn, continuing bool) *IMAgentResponse {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	validators := mem.ApprovedPlanValidators
	if len(validators) == 0 {
		return resp
	}
	if continuing || resp == nil || strings.TrimSpace(resp.Error) != "" {
		return resp
	}
	h.clearApprovedPlanValidators(userID)
	if !planTurn {
		return resp
	}
	// The frozen dir wins over mem.ProjectPath: approval bound these commands
	// to a specific workspace and a later turn may have re-rooted the session.
	dir := mem.ApprovedPlanValidatorDir
	if dir == "" {
		dir = mem.ProjectPath
	}
	return h.runApprovedPlanValidators(userID, dir, validators, resp)
}

// runApprovedPlanValidators executes the contract's validators itself —
// independently of what the model claims — after the approved plan finishes,
// and appends the measured result to the response. This is the "fresh
// meta-audit before delivery" stage: the verified label is earned here, not
// asserted by the agent's summary.
func (h *IMMessageHandler) runApprovedPlanValidators(userID, projectPath string, validators []string, resp *IMAgentResponse) *IMAgentResponse {
	if resp == nil || len(validators) == 0 {
		return resp
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(resp.Text, "\n"))
	if b.Len() == 0 {
		b.WriteString("—— 批准后验证（由主机独立执行）——\n")
	} else {
		b.WriteString("\n\n—— 批准后验证（由主机独立执行）——\n")
	}
	if strings.TrimSpace(projectPath) == "" {
		// Without a bound workspace the commands would run in the app's own
		// directory — never do that silently.
		b.WriteString("\n无法定位项目目录，验证命令未执行（结果未经验证）。")
		log.Printf("[coding-plan] approved-plan validators skipped: no project dir user=%s", userID)
		resp.Text = b.String()
		return resp
	}
	allPassed := true
	for _, cmd := range validators {
		result := executeCodingBashWithContext(nil, map[string]interface{}{
			"command":     cmd,
			"working_dir": projectPath,
			"timeout":     planContractValidatorTimeout,
		}, nil)
		passed := result.Kind == codingCommandResultOK && result.ExitCode == 0
		if !passed {
			allPassed = false
		}
		mark := "通过"
		if !passed {
			mark = fmt.Sprintf("未通过（exit=%d）", result.ExitCode)
		}
		fmt.Fprintf(&b, "\n`%s` → %s\n", cmd, mark)
		if tail := strings.TrimSpace(stripCodingCommandExitStatus(result.Text)); tail != "" {
			fmt.Fprintf(&b, "```\n%s\n```\n", truncateRunesForSubAgent(tail, 1200))
		}
	}
	if allPassed {
		b.WriteString("\n全部验证通过（已验证）。")
	} else {
		b.WriteString("\n存在未通过的验证命令（结果未经验证）。")
	}
	log.Printf("[coding-plan] approved-plan validators user=%s all_passed=%v", userID, allPassed)
	resp.Text = b.String()
	return resp
}
