package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type OpsCommandRisk string

const (
	OpsCommandRiskReadOnly OpsCommandRisk = "read_only"
	OpsCommandRiskMutating OpsCommandRisk = "mutating"
	OpsCommandRiskHigh     OpsCommandRisk = "high_risk"
)

type OpsCommandAssessment struct {
	Command string
	Risk    OpsCommandRisk
	Reason  string
}

type OpsApprovedCommand struct {
	Tool                string                 `json:"tool"`
	Action              string                 `json:"action,omitempty"`
	Target              string                 `json:"target,omitempty"`
	Command             string                 `json:"command"`
	RiskLevel           OpsRiskLevel           `json:"risk_level,omitempty"`
	ApprovalRequirement OpsApprovalRequirement `json:"approval_required,omitempty"`
}

type OpsRiskDecision string

const (
	OpsRiskDecisionUnknown          OpsRiskDecision = ""
	OpsRiskDecisionDocumentOnly     OpsRiskDecision = "document_only"
	OpsRiskDecisionPropose          OpsRiskDecision = "propose"
	OpsRiskDecisionApprovalRequired OpsRiskDecision = "approval_required"
	OpsRiskDecisionAutoExecute      OpsRiskDecision = "auto_execute"
	OpsRiskDecisionDeny             OpsRiskDecision = "deny"
)

type OpsRiskLevel string

const (
	OpsRiskLevelUnknown OpsRiskLevel = ""
	OpsRiskLevelL0      OpsRiskLevel = "L0"
	OpsRiskLevelL1      OpsRiskLevel = "L1"
	OpsRiskLevelL2      OpsRiskLevel = "L2"
	OpsRiskLevelL3      OpsRiskLevel = "L3"
	OpsRiskLevelL4      OpsRiskLevel = "L4"
)

type OpsApprovalRequirement string

const (
	OpsApprovalRequirementUnknown OpsApprovalRequirement = ""
	OpsApprovalRequirementNone    OpsApprovalRequirement = "none"
	OpsApprovalRequirementSingle  OpsApprovalRequirement = "single"
	OpsApprovalRequirementDouble  OpsApprovalRequirement = "double"
)

var opsHighRiskCommandPatterns = []struct {
	reason  string
	pattern *regexp.Regexp
}{
	{"recursive deletion at filesystem root is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)rm\s+[^;&|]*-[A-Za-z]*r[A-Za-z]*f[^;&|]*(\s+/(\s|$)|\s+/\*|\s+--no-preserve-root)`)},
	{"recursive deletion at filesystem root is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)rm\s+[^;&|]*(--recursive\b[^;&|]*--force\b|--force\b[^;&|]*--recursive\b)[^;&|]*(\s+/(\s|$)|\s+/\*|\s+--no-preserve-root)`)},
	{"filesystem formatting or wiping is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)(mkfs(\.[A-Za-z0-9_+-]+)?|wipefs|blkdiscard)\b`)},
	{"raw writes to block devices are not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)dd\s+[^;&|]*\bof=/dev/`)},
	{"volume removal is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)(pvremove|vgremove|lvremove)\b`)},
	{"host shutdown or reboot is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)(shutdown|reboot|halt|poweroff)\b`)},
	{"firewall flush/disable/reset is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)((iptables|ip6tables)\s+[^;&|]*(-F\b|--flush\b)|nft\s+flush\s+ruleset|ufw\s+(disable|reset)\b)`)},
	{"broad root permission changes are not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)(chmod|chown|chgrp)\s+[^;&|]*(-R\b|--recursive\b)[^;&|]*(\s+/(\s|$)|\s+/\*)`)},
	{"cluster-wide delete is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)kubectl\s+delete\s+[^;&|]*(\s+--all\b|\s+namespace\b|\s+ns\b)`)},
	{"terraform destroy is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)terraform\s+destroy\b`)},
	{"database drop/flush is not executable in ops-controlled mode", regexp.MustCompile(`(?i)\b(drop\s+database|drop\s+schema|flushall|flushdb)\b`)},
	{"destructive container pruning is not executable in ops-controlled mode", regexp.MustCompile(`(?i)(^|[;&|]\s*)docker\s+(system\s+prune\b[^;&|]*(\s--all\b|\s-[A-Za-z]*a[A-Za-z]*\b)|volume\s+prune\b)`)},
	{"fork bomb pattern is not executable in ops-controlled mode", regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;?\s*:`)},
}

var opsMutatingCommandPattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)(apt(-get)?\s+(install|remove|purge|upgrade|dist-upgrade)|yum\s+(install|remove|update)|dnf\s+(install|remove|update)|systemctl\s+(start|stop|restart|reload|enable|disable|daemon-reload)|service\s+\S+\s+(start|stop|restart|reload)|docker\s+(run|stop|start|restart|rm|exec|compose\s+(up|down|start|stop|restart|rm))|kubectl\s+(apply|patch|scale|rollout|delete|create|replace|set)|helm\s+(install|upgrade|rollback|uninstall)|iptables\s+(-A|-D|-I|-R|-P|-N|-X|-Z|--append|--delete|--insert|--replace|--policy|--new-chain|--delete-chain|--zero)|ip6tables\s+(-A|-D|-I|-R|-P|-N|-X|-Z|--append|--delete|--insert|--replace|--policy|--new-chain|--delete-chain|--zero)|nft\s+(add|delete|insert|replace|flush)|ufw\s+(allow|deny|reject|limit|delete|enable|disable|reload|reset)|firewall-cmd\s+.*(--add-|--remove-|--reload|--complete-reload|--runtime-to-permanent)|mount\s+|umount\s+|sysctl\s+-w\b|crontab\s+(-e|-r|[^-])|user(add|del|mod)\b|group(add|del|mod)\b|passwd\b|chpasswd\b|chage\b|gpasswd\b|kill\s+|pkill\b|killall\b|cp\s+|mv\s+|rm\s+|chmod\s+|chown\s+|mkdir\s+|touch\s+|tee\s+|ln\s+|install\s+|truncate\s+|rsync\s+|scp\s+|xargs\b|dd\s+[^;&|]*\bof=|curl\s+[^;&|]*\s-o\s+|wget\s+[^;&|]*\s-O\s+|sed\s+-i\b|perl\s+-pi\b|python[0-9.]*\s+-c\b|mysql\b|psql\b|redis-cli\b)`)

var opsCommandWrapperPattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+(?:-[A-Za-z]+\s+)*(?:--\s+)?|doas\s+|nohup\s+|command\s+|timeout\s+\S+\s+|(?:/usr/bin/)?env\s+(?:[A-Za-z_][A-Za-z0-9_]*=\S+\s+)*)`)
var opsSudoWrapperPattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)sudo\s+(?:(?:-[A-Za-z]*[ughpCT]|--(?:user|group|host|prompt|close-from|command-timeout))\s+\S+\s+|--(?:user|group|host|prompt|close-from|command-timeout)=\S+\s+|-[A-Za-z]+\s+|--\s+)*`)
var opsShellEvalPattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)(?:\S+/)?(?:ba|z|da|k)?sh\s+-[A-Za-z]*c[A-Za-z]*\s+['"]?`)
var opsExecutablePathPattern = regexp.MustCompile(`(^|[;&|]\s*)(?:\./|\.\./|/|\w:/)(?:[^\s;&|]+/)+([A-Za-z0-9_.+-]+)\b`)

// AssessOpsCommand classifies a shell command for controlled operations. The
// classifier is intentionally conservative only for catastrophic operations:
// mutating commands may still run in an approved ops execution phase, while
// high-risk commands must be turned into artifacts for human execution.
func AssessOpsCommand(command string) OpsCommandAssessment {
	command = strings.TrimSpace(command)
	if command == "" {
		return OpsCommandAssessment{Risk: OpsCommandRiskReadOnly, Reason: "empty command"}
	}
	classifiedCommand := stripOpsCommandWrappers(command)
	classifiedCommand = exposeOpsNestedShellCommandBoundaries(classifiedCommand)
	classifiedCommand = normalizeOpsExecutablePaths(classifiedCommand)
	if hasBroadSQLMutation(classifiedCommand) {
		return OpsCommandAssessment{Command: command, Risk: OpsCommandRiskHigh, Reason: "broad SQL mutation without WHERE is not executable in ops-controlled mode"}
	}
	for _, entry := range opsHighRiskCommandPatterns {
		if entry.pattern.MatchString(classifiedCommand) {
			return OpsCommandAssessment{Command: command, Risk: OpsCommandRiskHigh, Reason: entry.reason}
		}
	}
	if opsMutatingCommandPattern.MatchString(classifiedCommand) || hasShellWriteRedirection(classifiedCommand) {
		return OpsCommandAssessment{Command: command, Risk: OpsCommandRiskMutating, Reason: "command mutates system state and requires an approved controlled-execution phase"}
	}
	return OpsCommandAssessment{Command: command, Risk: OpsCommandRiskReadOnly, Reason: "command appears read-only"}
}

// ValidateToolCallByPolicy validates a concrete tool call under a workflow
// policy. Name-only filtering decides tool exposure; this function is the
// argument-level execution boundary.
func ValidateToolCallByPolicy(policy ToolFilterPolicy, name string, args map[string]interface{}) error {
	return ValidateToolCallByPolicyWithApproval(policy, name, args, nil)
}

// ValidateToolCallByPolicyWithApproval validates a concrete tool call and, when
// a risk-policy command manifest is available, requires executable shell
// commands to match that approved manifest exactly after whitespace
// normalization.
func ValidateToolCallByPolicyWithApproval(policy ToolFilterPolicy, name string, args map[string]interface{}, approved []OpsApprovedCommand) error {
	name = strings.TrimSpace(name)
	if !IsToolAllowedByPolicy(policy, name) {
		return fmt.Errorf("%s is not allowed by the current workflow tool policy", name)
	}
	if policy == ToolFilterDocOnly {
		return validateReadOnlyOpsToolCall(name, args)
	}
	if policy != ToolFilterOpsControlled {
		return nil
	}
	switch name {
	case "bash":
		command := stringArg(args, "command")
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("bash command is required by the current workflow tool policy")
		}
		if err := validateOpsCommand(command); err != nil {
			return err
		}
		return validateApprovedOpsCommand(approved, name, "", opsBashTarget(args), command)
	case "ssh":
		action := strings.TrimSpace(stringArg(args, "action"))
		switch action {
		case "connect":
			if initial := strings.TrimSpace(stringArg(args, "initial_command")); initial != "" {
				if err := validateOpsCommand(initial); err != nil {
					return err
				}
				if len(approved) == 0 {
					return fmt.Errorf("%s action %s changes operational state and requires an approved risk-policy allowed_commands manifest", name, action)
				}
				return validateApprovedOpsCommand(approved, name, action, opsSSHTarget(args), initial)
			}
			return validateApprovedOpsToolAction(approved, name, action, opsSSHTarget(args), "connect")
		case "exec", "exec_background":
			command := stringArg(args, "command")
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("ssh action %s requires a non-empty command", action)
			}
			if err := validateOpsCommand(command); err != nil {
				return err
			}
			return validateApprovedOpsCommand(approved, name, action, opsSSHTarget(args), command)
		case "upload":
			localPath := strings.TrimSpace(stringArg(args, "local_path"))
			remotePath := strings.TrimSpace(stringArg(args, "remote_path"))
			if localPath == "" || remotePath == "" {
				return fmt.Errorf("ssh action %s requires non-empty local_path and remote_path", action)
			}
			return validateApprovedOpsToolAction(approved, name, action, opsSSHTarget(args), fmt.Sprintf("%s -> %s", localPath, remotePath))
		case "download":
			remotePath := strings.TrimSpace(stringArg(args, "remote_path"))
			localPath := strings.TrimSpace(stringArg(args, "local_path"))
			if remotePath == "" || localPath == "" {
				return fmt.Errorf("ssh action %s requires non-empty remote_path and local_path", action)
			}
			return validateApprovedOpsToolAction(approved, name, action, opsSSHTarget(args), fmt.Sprintf("%s -> %s", remotePath, localPath))
		case "kill_task", "sudo_prepare", "close", "close_all":
			return validateApprovedOpsToolAction(approved, name, action, opsSSHActionTarget(args, action), opsSSHActionDescriptor(args))
		case "check_task", "list_tasks", "list":
			return nil
		default:
			return fmt.Errorf("ssh action %s is not allowed by the current workflow tool policy", displayOpsAction(action))
		}
	}
	return nil
}

func validateReadOnlyOpsToolCall(name string, args map[string]interface{}) error {
	switch strings.TrimSpace(name) {
	case "bash":
		command := stringArg(args, "command")
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("bash command is required before the ops risk-policy gate")
		}
		return validateReadOnlyOpsCommand(command)
	case "ssh":
		action := strings.TrimSpace(stringArg(args, "action"))
		switch action {
		case "", "connect":
			if initial := strings.TrimSpace(stringArg(args, "initial_command")); initial != "" {
				return validateReadOnlyOpsCommand(initial)
			}
		case "exec", "exec_background":
			command := stringArg(args, "command")
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("ssh action %s requires a non-empty command before the ops risk-policy gate", action)
			}
			return validateReadOnlyOpsCommand(command)
		case "upload", "download", "kill_task", "sudo_prepare", "close", "close_all":
			return fmt.Errorf("ssh action %s changes operational state or transfers data and is not allowed before the ops risk-policy gate", action)
		case "check_task", "list_tasks", "list":
			return nil
		default:
			return fmt.Errorf("ssh action %s is not allowed before the ops risk-policy gate", displayOpsAction(action))
		}
	}
	return nil
}

func validateReadOnlyOpsCommand(command string) error {
	assessment := AssessOpsCommand(command)
	switch assessment.Risk {
	case OpsCommandRiskHigh:
		return fmt.Errorf("%s; generate a reviewed runbook/script instead of executing it directly", assessment.Reason)
	case OpsCommandRiskMutating:
		return fmt.Errorf("command changes system state and is not allowed before the ops risk-policy gate")
	default:
		return nil
	}
}

// ExtractOpsApprovedCommands extracts the machine-readable allowed_commands
// block from a risk policy document. It intentionally supports a strict,
// simple YAML subset so the execution gate depends on explicit command
// manifests instead of broad natural-language interpretation.
func ExtractOpsApprovedCommands(policyText string) []OpsApprovedCommand {
	decision := ExtractOpsRiskDecision(policyText)
	riskLevel := ExtractOpsRiskLevel(policyText)
	approvalRequirement := ExtractOpsApprovalRequirement(policyText)
	if !OpsRiskDecisionAllowsExecution(decision, riskLevel, approvalRequirement) {
		return nil
	}
	lines := strings.Split(policyText, "\n")
	inBlock := false
	var out []OpsApprovedCommand
	var current *OpsApprovedCommand
	flush := func() {
		if current == nil {
			return
		}
		current.Tool = strings.TrimSpace(current.Tool)
		current.Action = strings.TrimSpace(current.Action)
		current.Target = strings.TrimSpace(current.Target)
		current.Command = strings.TrimSpace(current.Command)
		if current.Tool != "" && current.Command != "" && opsApprovedCommandExecutable(*current, decision, riskLevel, approvalRequirement) {
			current.RiskLevel = riskLevel
			current.ApprovalRequirement = approvalRequirement
			out = append(out, *current)
		}
		current = nil
	}

	for _, raw := range lines {
		withoutFence := strings.Trim(raw, "`")
		line := strings.TrimSpace(withoutFence)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !inBlock {
			if isAllowedCommandsHeader(line) {
				inBlock = true
			}
			continue
		}
		if hasNoYAMLIndent(withoutFence) && isTopLevelYAMLKey(line) {
			flush()
			break
		}
		if strings.HasPrefix(line, "-") {
			flush()
			current = &OpsApprovedCommand{}
			remainder := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if key, value, ok := splitYAMLScalar(remainder); ok {
				assignApprovedCommandField(current, key, value)
			}
			continue
		}
		if current == nil {
			continue
		}
		if key, value, ok := splitYAMLScalar(line); ok {
			assignApprovedCommandField(current, key, value)
		}
	}
	flush()
	return out
}

func isAllowedCommandsHeader(line string) bool {
	key, value, ok := splitYAMLScalar(line)
	return ok && key == "allowed_commands" && value == ""
}

func opsApprovedCommandExecutable(command OpsApprovedCommand, decision OpsRiskDecision, riskLevel OpsRiskLevel, approval OpsApprovalRequirement) bool {
	tool := strings.TrimSpace(command.Tool)
	action := strings.TrimSpace(command.Action)
	descriptor := strings.TrimSpace(command.Command)
	if tool == "" || descriptor == "" {
		return false
	}
	switch tool {
	case "bash":
		return opsCommandAssessmentExecutable(AssessOpsCommand(descriptor), decision)
	case "ssh":
		if requiresExplicitOpsTarget(tool, action) && strings.TrimSpace(command.Target) == "" {
			return false
		}
		switch action {
		case "exec", "exec_background":
			return opsCommandAssessmentExecutable(AssessOpsCommand(descriptor), decision)
		case "connect":
			if decision == OpsRiskDecisionAutoExecute {
				return false
			}
			if normalizeOpsCommand(descriptor) == "connect" {
				return true
			}
			return opsCommandAssessmentExecutable(AssessOpsCommand(descriptor), decision)
		case "upload", "download", "kill_task", "sudo_prepare", "close", "close_all":
			if decision == OpsRiskDecisionAutoExecute {
				return false
			}
			if action == "close_all" && (riskLevel != OpsRiskLevelL3 || approval != OpsApprovalRequirementDouble) {
				return false
			}
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func opsCommandAssessmentExecutable(assessment OpsCommandAssessment, decision OpsRiskDecision) bool {
	if assessment.Risk == OpsCommandRiskHigh {
		return false
	}
	if decision == OpsRiskDecisionAutoExecute {
		return assessment.Risk == OpsCommandRiskReadOnly
	}
	return true
}

func ExtractOpsRiskDecision(policyText string) OpsRiskDecision {
	for _, raw := range strings.Split(policyText, "\n") {
		line := strings.TrimSpace(strings.Trim(raw, "`"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitYAMLScalar(line)
		if !ok || key != "decision" {
			continue
		}
		decision := OpsRiskDecision(strings.ToLower(strings.TrimSpace(value)))
		switch decision {
		case OpsRiskDecisionDocumentOnly, OpsRiskDecisionPropose, OpsRiskDecisionApprovalRequired, OpsRiskDecisionAutoExecute, OpsRiskDecisionDeny:
			return decision
		default:
			return OpsRiskDecisionUnknown
		}
	}
	return OpsRiskDecisionUnknown
}

func ExtractOpsRiskLevel(policyText string) OpsRiskLevel {
	for _, raw := range strings.Split(policyText, "\n") {
		line := strings.TrimSpace(strings.Trim(raw, "`"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitYAMLScalar(line)
		if !ok || key != "risk_level" {
			continue
		}
		switch OpsRiskLevel(strings.ToUpper(strings.TrimSpace(value))) {
		case OpsRiskLevelL0, OpsRiskLevelL1, OpsRiskLevelL2, OpsRiskLevelL3, OpsRiskLevelL4:
			return OpsRiskLevel(strings.ToUpper(strings.TrimSpace(value)))
		default:
			return OpsRiskLevelUnknown
		}
	}
	return OpsRiskLevelUnknown
}

func ExtractOpsApprovalRequirement(policyText string) OpsApprovalRequirement {
	for _, raw := range strings.Split(policyText, "\n") {
		line := strings.TrimSpace(strings.Trim(raw, "`"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitYAMLScalar(line)
		if !ok || key != "approval_required" {
			continue
		}
		switch OpsApprovalRequirement(strings.ToLower(strings.TrimSpace(value))) {
		case OpsApprovalRequirementNone, OpsApprovalRequirementSingle, OpsApprovalRequirementDouble:
			return OpsApprovalRequirement(strings.ToLower(strings.TrimSpace(value)))
		default:
			return OpsApprovalRequirementUnknown
		}
	}
	return OpsApprovalRequirementUnknown
}

func OpsRiskDecisionAllowsExecution(decision OpsRiskDecision, riskLevel OpsRiskLevel, approval OpsApprovalRequirement) bool {
	if riskLevel == OpsRiskLevelUnknown || riskLevel == OpsRiskLevelL4 {
		return false
	}
	switch decision {
	case OpsRiskDecisionApprovalRequired:
		if approval == OpsApprovalRequirementUnknown || approval == OpsApprovalRequirementNone {
			return false
		}
		if riskLevel == OpsRiskLevelL3 {
			return approval == OpsApprovalRequirementDouble
		}
		return true
	case OpsRiskDecisionAutoExecute:
		return (approval == OpsApprovalRequirementNone) && (riskLevel == OpsRiskLevelL0 || riskLevel == OpsRiskLevelL1)
	default:
		return false
	}
}

func OpsApprovalDigest(policyText string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(policyText), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func validateOpsCommand(command string) error {
	assessment := AssessOpsCommand(command)
	if assessment.Risk == OpsCommandRiskHigh {
		return fmt.Errorf("%s; generate a reviewed runbook/script instead of executing it directly", assessment.Reason)
	}
	return nil
}

func validateApprovedOpsCommand(approved []OpsApprovedCommand, tool, action, target, command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	if len(approved) == 0 {
		assessment := AssessOpsCommand(command)
		if assessment.Risk == OpsCommandRiskMutating {
			return fmt.Errorf("%s command changes system state and requires an approved risk-policy allowed_commands manifest", tool)
		}
		return nil
	}
	tool = strings.TrimSpace(tool)
	action = strings.TrimSpace(action)
	target = normalizeOpsTarget(target)
	normalizedCommand := normalizeOpsCommand(command)
	missingActionForCommand := false
	missingTargetForCommand := false
	missingActualTargetForCommand := false
	for _, candidate := range approved {
		if strings.TrimSpace(candidate.Tool) != tool {
			continue
		}
		if requiresExplicitOpsAction(tool, action) && strings.TrimSpace(candidate.Action) == "" {
			if normalizeOpsCommand(candidate.Command) == normalizedCommand {
				missingActionForCommand = true
			}
			continue
		}
		if candidate.Action != "" && strings.TrimSpace(candidate.Action) != action {
			continue
		}
		if normalizeOpsCommand(candidate.Command) != normalizedCommand {
			continue
		}
		targetOK, targetReason := approvedTargetMatches(candidate.Target, target, requiresExplicitOpsTarget(tool, action))
		if !targetOK {
			if targetReason != "" {
				if strings.Contains(targetReason, "explicit target") {
					missingTargetForCommand = true
				} else if strings.Contains(targetReason, "execution target") {
					missingActualTargetForCommand = true
				}
			}
			continue
		}
		return nil
	}
	if missingActionForCommand {
		return fmt.Errorf("%s command requires an explicit action in the approved risk-policy allowed_commands manifest", tool)
	}
	if missingTargetForCommand {
		return fmt.Errorf("%s command requires an explicit target in the approved risk-policy allowed_commands manifest", tool)
	}
	if missingActualTargetForCommand {
		return fmt.Errorf("%s command requires an execution target such as session_id, label, or host", tool)
	}
	return fmt.Errorf("%s command is not present in the approved risk-policy allowed_commands manifest", tool)
}

func validateApprovedOpsToolAction(approved []OpsApprovedCommand, tool, action, target, descriptor string) error {
	descriptor = strings.TrimSpace(descriptor)
	if descriptor == "->" {
		descriptor = ""
	}
	if len(approved) == 0 {
		return fmt.Errorf("%s action %s changes operational state or transfers data and requires an approved risk-policy allowed_commands manifest", tool, action)
	}
	tool = strings.TrimSpace(tool)
	action = strings.TrimSpace(action)
	target = normalizeOpsTarget(target)
	normalizedDescriptor := normalizeOpsCommand(descriptor)
	if strings.TrimSpace(descriptor) == "" {
		return fmt.Errorf("%s action %s requires a non-empty action descriptor", tool, action)
	}
	missingTargetForAction := false
	missingActualTargetForAction := false
	for _, candidate := range approved {
		if strings.TrimSpace(candidate.Tool) != tool {
			continue
		}
		if strings.TrimSpace(candidate.Action) != action {
			continue
		}
		if normalizeOpsCommand(candidate.Command) != normalizedDescriptor {
			continue
		}
		if action == "close_all" && !candidateAllowsOpsCloseAll(candidate) {
			continue
		}
		targetOK, targetReason := approvedTargetMatches(candidate.Target, target, requiresExplicitOpsTarget(tool, action))
		if !targetOK {
			if targetReason != "" {
				if strings.Contains(targetReason, "explicit target") {
					missingTargetForAction = true
				} else if strings.Contains(targetReason, "execution target") {
					missingActualTargetForAction = true
				}
			}
			continue
		}
		return nil
	}
	if missingTargetForAction {
		return fmt.Errorf("%s action %s requires an explicit target in the approved risk-policy allowed_commands manifest", tool, action)
	}
	if missingActualTargetForAction {
		return fmt.Errorf("%s action %s requires an execution target such as session_id, label, or host", tool, action)
	}
	return fmt.Errorf("%s action %s is not present in the approved risk-policy allowed_commands manifest", tool, action)
}

func candidateAllowsOpsCloseAll(candidate OpsApprovedCommand) bool {
	return candidate.RiskLevel == OpsRiskLevelL3 && candidate.ApprovalRequirement == OpsApprovalRequirementDouble
}

func opsSSHActionDescriptor(args map[string]interface{}) string {
	action := strings.TrimSpace(stringArg(args, "action"))
	if taskID := strings.TrimSpace(stringArg(args, "task_id")); taskID != "" {
		return taskID
	}
	if action == "sudo_prepare" {
		return strings.TrimSpace(stringArg(args, "session_id"))
	}
	if action == "close_all" {
		return "all"
	}
	if action == "close" {
		return strings.TrimSpace(stringArg(args, "session_id"))
	}
	return ""
}

func opsSSHActionTarget(args map[string]interface{}, action string) string {
	switch strings.TrimSpace(action) {
	case "kill_task":
		return strings.TrimSpace(stringArg(args, "task_id"))
	case "sudo_prepare", "close":
		return strings.TrimSpace(stringArg(args, "session_id"))
	default:
		return opsSSHTarget(args)
	}
}

func opsBashTarget(args map[string]interface{}) string {
	return strings.TrimSpace(stringArg(args, "working_dir"))
}

func opsSSHTarget(args map[string]interface{}) string {
	if sessionID := strings.TrimSpace(stringArg(args, "session_id")); sessionID != "" {
		return sessionID
	}
	if label := strings.TrimSpace(stringArg(args, "label")); label != "" {
		return label
	}
	host := strings.TrimSpace(stringArg(args, "host"))
	user := strings.TrimSpace(stringArg(args, "user"))
	port := opsPortSuffix(args)
	if host != "" && user != "" {
		return user + "@" + host + port
	}
	if host != "" {
		return host + port
	}
	return ""
}

func opsPortSuffix(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	switch v := args["port"].(type) {
	case int:
		if v > 0 {
			return fmt.Sprintf(":%d", v)
		}
	case int64:
		if v > 0 {
			return fmt.Sprintf(":%d", v)
		}
	case float64:
		if v > 0 && v == float64(int64(v)) {
			return fmt.Sprintf(":%d", int64(v))
		}
	case string:
		v = strings.TrimSpace(v)
		if v != "" {
			return ":" + v
		}
	}
	return ""
}

func normalizeOpsCommand(command string) string {
	command = strings.TrimSpace(command)
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	pendingSpace := false
	for _, r := range command {
		if escaped {
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
			escaped = false
			continue
		}
		if inDouble && r == '\\' {
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		case ' ', '\t':
			if inSingle || inDouble {
				b.WriteRune(r)
			} else {
				pendingSpace = true
			}
		case '\n', '\r':
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		default:
			if pendingSpace && b.Len() > 0 {
				b.WriteRune(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeOpsTarget(target string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(target)), " ")
}

func requiresExplicitOpsTarget(tool, action string) bool {
	if strings.TrimSpace(tool) != "ssh" {
		return false
	}
	switch strings.TrimSpace(action) {
	case "connect", "exec", "exec_background", "upload", "download", "kill_task", "sudo_prepare", "close":
		return true
	default:
		return false
	}
}

func requiresExplicitOpsAction(tool, action string) bool {
	if strings.TrimSpace(tool) != "ssh" {
		return false
	}
	switch strings.TrimSpace(action) {
	case "connect", "exec", "exec_background":
		return true
	default:
		return false
	}
}

func displayOpsAction(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return "<empty>"
	}
	return action
}

func approvedTargetMatches(approvedTarget, actualTarget string, required bool) (bool, string) {
	approvedTarget = normalizeOpsTarget(approvedTarget)
	actualTarget = normalizeOpsTarget(actualTarget)
	if required && approvedTarget == "" {
		return false, "requires an explicit target in the approved risk-policy allowed_commands manifest"
	}
	if required && actualTarget == "" {
		return false, "requires an execution target such as session_id, label, or host"
	}
	if approvedTarget == "" {
		return true, ""
	}
	if approvedTarget != actualTarget {
		return false, ""
	}
	return true, ""
}

func stripOpsCommandWrappers(command string) string {
	previous := ""
	current := command
	for i := 0; i < 8 && current != previous; i++ {
		previous = current
		current = stripOneOpsCommandWrapper(current)
	}
	return current
}

func exposeOpsNestedShellCommandBoundaries(command string) string {
	command = opsShellEvalPattern.ReplaceAllString(command, "$1; ")
	replacer := strings.NewReplacer(
		"$(", "; ",
		"`", "; ",
		"(", "; ",
		")", " ",
	)
	return replacer.Replace(command)
}

func normalizeOpsExecutablePaths(command string) string {
	return opsExecutablePathPattern.ReplaceAllString(command, "$1$2")
}

func stripOneOpsCommandWrapper(command string) string {
	command = opsSudoWrapperPattern.ReplaceAllString(command, "$1")
	return opsCommandWrapperPattern.ReplaceAllStringFunc(command, func(match string) string {
		prefix := commandSeparatorPrefix(match)
		rest := strings.TrimSpace(strings.TrimPrefix(match, prefix))
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return prefix
		}
		switch strings.ToLower(fields[0]) {
		case "sudo":
			return prefix + strings.Join(stripSudoPrefixFields(fields), " ")
		default:
			return prefix
		}
	})
}

func stripSudoPrefixFields(fields []string) []string {
	i := 1
	for i < len(fields) {
		token := fields[i]
		if token == "--" {
			i++
			break
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			break
		}
		i++
		if sudoOptionConsumesValue(token) && i < len(fields) {
			i++
		}
	}
	if i >= len(fields) {
		return nil
	}
	return fields[i:]
}

func sudoOptionConsumesValue(token string) bool {
	if strings.Contains(token, "=") {
		return false
	}
	for _, option := range []string{"-u", "--user", "-g", "--group", "-h", "--host", "-p", "--prompt", "-C", "--close-from", "-T", "--command-timeout"} {
		if token == option {
			return true
		}
	}
	if strings.HasPrefix(token, "--user=") || strings.HasPrefix(token, "--group=") || strings.HasPrefix(token, "--host=") || strings.HasPrefix(token, "--prompt=") || strings.HasPrefix(token, "--close-from=") || strings.HasPrefix(token, "--command-timeout=") {
		return false
	}
	if strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") {
		for _, r := range token[1:] {
			if strings.ContainsRune("ughpCT", r) {
				return len(token) == 2
			}
		}
	}
	return false
}

func commandSeparatorPrefix(match string) string {
	trimmed := strings.TrimLeft(match, " \t")
	if trimmed == "" {
		return ""
	}
	switch trimmed[0] {
	case ';', '&', '|':
		return string(trimmed[0])
	default:
		return ""
	}
}

func hasBroadSQLMutation(command string) bool {
	for _, segment := range splitOpsCommandSegments(command) {
		normalized := strings.ToLower(strings.Join(strings.Fields(segment), " "))
		normalized = strings.NewReplacer(`"`, " ", `'`, " ", "`", " ").Replace(normalized)
		normalized = strings.Join(strings.Fields(normalized), " ")
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "truncate table ") {
			return true
		}
		if strings.Contains(normalized, "delete from ") && (!strings.Contains(normalized, " where ") || hasBroadSQLWhereClause(normalized)) {
			return true
		}
		if strings.Contains(normalized, " update ") || strings.HasPrefix(normalized, "update ") {
			if strings.Contains(normalized, " set ") && (!strings.Contains(normalized, " where ") || hasBroadSQLWhereClause(normalized)) {
				return true
			}
		}
	}
	return false
}

func hasBroadSQLWhereClause(normalized string) bool {
	whereIndex := strings.Index(normalized, " where ")
	if whereIndex < 0 {
		return false
	}
	where := " " + strings.TrimSpace(normalized[whereIndex+len(" where "):]) + " "
	whereCompact := strings.ReplaceAll(where, " ", "")
	return strings.HasPrefix(whereCompact, "1=1") ||
		strings.HasPrefix(whereCompact, "true") ||
		strings.Contains(whereCompact, "or1=1") ||
		strings.Contains(whereCompact, "ortrue")
}

func splitOpsCommandSegments(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		return r == ';' || r == '&' || r == '|'
	})
}

func hasShellWriteRedirection(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble && ch == '\\' {
			escaped = true
			continue
		}
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			continue
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			continue
		case '>':
			if inSingle || inDouble {
				continue
			}
			targetStart := i + 1
			if targetStart < len(command) && command[targetStart] == '>' {
				targetStart++
			}
			for targetStart < len(command) && (command[targetStart] == ' ' || command[targetStart] == '\t') {
				targetStart++
			}
			if targetStart >= len(command) || command[targetStart] == '&' {
				continue
			}
			targetEnd := targetStart
			for targetEnd < len(command) && !strings.ContainsRune("&;| \t\r\n", rune(command[targetEnd])) {
				targetEnd++
			}
			target := strings.Trim(strings.TrimSpace(command[targetStart:targetEnd]), `"'`)
			if target != "/dev/null" {
				return true
			}
			continue
		}
	}
	return false
}

func splitYAMLScalar(line string) (string, string, bool) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(stripYAMLLineComment(value))
	value = strings.Trim(value, `"'`)
	return key, value, key != ""
}

func stripYAMLLineComment(value string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if inDouble && r == '\\' {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return value[:i]
			}
		}
	}
	return value
}

func assignApprovedCommandField(command *OpsApprovedCommand, key, value string) {
	switch strings.TrimSpace(key) {
	case "tool":
		command.Tool = value
	case "action":
		command.Action = value
	case "target":
		command.Target = value
	case "command":
		command.Command = value
	}
}

func isTopLevelYAMLKey(line string) bool {
	if strings.HasPrefix(line, "-") {
		return false
	}
	key, _, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	return key != "" && !strings.ContainsAny(key, " \t")
}

func hasNoYAMLIndent(line string) bool {
	return line == strings.TrimLeft(line, " \t")
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}
