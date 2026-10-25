package guiapp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticCapabilityExtension reports that a managed-plan failure is a missing
// built-in, not a safety refusal. The turn stays closed: the model may search
// and install a skill, or craft a one-off program, and nothing else is
// published. Policy denials, stale documents, ambiguous native documents, the
// workflow entry, and catalog-not-ready failures stay rejected.
func semanticCapabilityExtension(err error, userText string, attachments []MessageAttachment) (reason string, paths []string, ok bool) {
	if err == nil || errors.Is(err, errSemanticSessionCeilingSpent) || errors.Is(err, errSemanticAwaitingConfirmation) {
		return "", nil, false
	}
	if semanticCapabilityExtensionSafety(err) {
		return "", nil, false
	}
	if tool.IsTrustedInputMissingOrAmbiguous(err) {
		paths = semanticUnsupportedSelectedPaths(userText, attachments)
		if len(paths) == 0 {
			return "", nil, false
		}
		return "内置阅读器不支持这些文件", paths, true
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) {
		return "内置工具没有「" + string(unmapped.Label) + "」这项能力", nil, true
	}
	if strings.Contains(err.Error(), "no governed capability needs") {
		return "内置工具没有覆盖这项请求", nil, true
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) {
		if semanticUnmetOnlyBudget(unmet.Unmet) {
			return "", nil, false
		}
		return "内置工具没有覆盖这项请求", nil, true
	}
	return "", nil, false
}

// semanticCapabilityExtensionSafety is the refusal set that must not grow a
// skill or a crafted program. A missing built-in is not in this set.
func semanticCapabilityExtensionSafety(err error) bool {
	if semanticTrustedDocumentContextError(err) || semanticProducedDocumentError(err) {
		return true
	}
	if errors.Is(err, errSemanticGenerateDeliveryConflict) {
		return true
	}
	if semanticUnmetHasReason(err, "policy_denied") || semanticUnmetHasReason(err, "artifact_dependency_missing") {
		return true
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) && unmapped.Label == intent.LabelWorkflowTask {
		return true
	}
	var pinned semanticPinnedProviderUnavailableError
	if errors.As(err, &pinned) {
		return true
	}
	if semanticUnmetHasReason(err, tool.UnmetReasonPinnedProviderUnavailable) {
		return true
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) && semanticUnmetOnlyReasons(unmet.Unmet,
		tool.CatalogCoverageReasonNotReady,
		tool.CatalogCoverageReasonIncomplete,
		tool.CatalogCoverageReasonStale) {
		return true
	}
	return false
}

// semanticUnsupportedSelectedPaths is the current selection when every file
// is outside the native document reader. A native document in the same
// selection stays on the document plan. An image is not an unsupported book.
func semanticUnsupportedSelectedPaths(userText string, attachments []MessageAttachment) []string {
	paths := agent.SelectedLocalFilePathsFromPrompt(userText)
	if len(paths) > 0 {
		unsupported := make([]string, 0, len(paths))
		for _, path := range paths {
			if agent.IsDocumentFilePath(path) || agent.IsImageFilePath(path) {
				continue
			}
			unsupported = append(unsupported, path)
		}
		if len(unsupported) == 0 || len(unsupported) != len(paths) {
			return nil
		}
		return unsupported
	}
	var names []string
	sawNative := false
	for _, attachment := range attachments {
		name := strings.TrimSpace(attachment.FileName)
		if attachment.Type == "image" || agent.IsImageFilePath(name) {
			continue
		}
		if name == "" {
			continue
		}
		if _, _, ok := agent.DocumentAttachmentFormat(name, attachment.MimeType); ok || agent.IsDocumentFilePath(name) {
			sawNative = true
			continue
		}
		names = append(names, name)
	}
	if sawNative || len(names) == 0 {
		return nil
	}
	return names
}

func capabilityExtensionToolDefs(h *IMMessageHandler) []map[string]interface{} {
	if h == nil || h.registry == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, 2)
	for _, name := range []string{"search_and_install_skill", "craft_tool"} {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable {
			continue
		}
		out = append(out, registeredToolToDef(*registered))
	}
	return out
}

func capabilityExtensionPrompt(reason string, paths []string, tools []map[string]interface{}) string {
	have := map[string]bool{}
	for _, def := range tools {
		have[extractToolName(def)] = true
	}
	var b strings.Builder
	b.WriteString("## 内置能力未覆盖\n")
	b.WriteString("这一轮的内置工具不能直接完成请求。不要拒绝，也不要让用户改格式或换软件。\n")
	step := 1
	if have["search_and_install_skill"] {
		fmt.Fprintf(&b, "%d. 先调用 search_and_install_skill，query 用用户的原话，搜索并安装能完成这件事的 Skill。\n", step)
		step++
	}
	if have["craft_tool"] {
		fmt.Fprintf(&b, "%d. 没有可用 Skill，或 Skill 明确失败时，调用 craft_tool 编写一次性程序完成。task 里写清目标；如果下面列了文件，写上完整路径。\n", step)
	}
	b.WriteString("不要用脚本绕过 encrypted、malformed、source_changed、input_too_large、output_too_large。\n")
	if strings.TrimSpace(reason) != "" {
		b.WriteString("原因：")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	if len(paths) > 0 {
		b.WriteString("已选文件：\n")
		for _, path := range paths {
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
