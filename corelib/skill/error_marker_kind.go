package skill

import "strings"

func hasSkillRateLimitMarker(combined string) bool {
	// Phrase-first: OpenAI/Anthropic often emit rate_limit_exceeded without a bare "429".
	if strings.Contains(combined, "rate limit") ||
		strings.Contains(combined, "rate_limit") ||
		strings.Contains(combined, "too many requests") ||
		strings.Contains(combined, "frequency limit") ||
		strings.Contains(combined, "\u9891\u7387\u9650\u5236") {
		return true
	}
	// Bare status 429 with HTTP/error context (avoid matching unrelated numbers).
	return strings.Contains(combined, "429") &&
		(strings.Contains(combined, "http") ||
			strings.Contains(combined, "status") ||
			strings.Contains(combined, "error") ||
			strings.Contains(combined, "limit") ||
			strings.Contains(combined, "throttle"))
}

// hasSkillQuotaExceededMarker matches provider billing/plan quota exhaustion.
// combined is expected lowercased. Explicitly excludes filesystem disk quota.
func hasSkillQuotaExceededMarker(combined string) bool {
	if strings.Contains(combined, "disk quota") ||
		strings.Contains(combined, "filesystem quota") ||
		strings.Contains(combined, "disk space") {
		return false
	}
	return strings.Contains(combined, "insufficient_quota") ||
		strings.Contains(combined, "insufficient quota") ||
		strings.Contains(combined, "quota_exceeded") ||
		strings.Contains(combined, "quota exceeded") ||
		strings.Contains(combined, "exceeded your current quota") ||
		strings.Contains(combined, "exceeded your quota") ||
		strings.Contains(combined, "billing hard limit") ||
		strings.Contains(combined, "billing_not_active") ||
		(strings.Contains(combined, "credits") &&
			(strings.Contains(combined, "exhausted") ||
				strings.Contains(combined, "insufficient") ||
				strings.Contains(combined, "depleted") ||
				strings.Contains(combined, "no remaining"))) ||
		(strings.Contains(combined, "usage limit") && strings.Contains(combined, "exceed"))
}

func hasSkillAuthErrorMarker(combined string) bool {
	return (strings.Contains(combined, "401") && strings.Contains(combined, "unauthorized")) ||
		(strings.Contains(combined, "403") && strings.Contains(combined, "forbidden")) ||
		strings.Contains(combined, "permission denied") ||
		strings.Contains(combined, "access denied")
}

// hasSkillSessionNotFoundMarker matches browser/hub/remote session loss.
// combined is expected to already be lowercased by ClassifyStepError.
func hasSkillSessionNotFoundMarker(combined string) bool {
	return strings.Contains(combined, "session_not_found") ||
		strings.Contains(combined, "session not found") ||
		strings.Contains(combined, "session expired") ||
		strings.Contains(combined, "invalid session") ||
		strings.Contains(combined, "no such session")
}

func hasSkillMissingDependencyMarker(combined string) bool {
	return (strings.Contains(combined, "required python package") && strings.Contains(combined, "is not installed")) ||
		(strings.Contains(combined, "required node package") && strings.Contains(combined, "is not installed")) ||
		isMissingPythonPackageMessage(combined) ||
		isMissingNodeESMPackageMessage(combined) ||
		isMissingNodePackageMessage(combined)
}

func missingDependencyKindFromMessage(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "required python package"),
		strings.Contains(lower, "modulenotfounderror:"),
		strings.Contains(lower, "importerror: no module named"),
		strings.Contains(lower, "no module named "):
		return "python"
	case strings.Contains(lower, "required node package"),
		strings.Contains(lower, "err_module_not_found"),
		strings.Contains(lower, "cannot find module"):
		return "node"
	default:
		return ""
	}
}

func isLocalNodeModuleReference(combined string) bool {
	if !strings.Contains(combined, "cannot find module") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return isLocalModulePath(name)
}

func isLocalPythonModuleReference(combined string) bool {
	if !strings.Contains(combined, "no module named ") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	if name == "" {
		return false
	}
	return isLocalModulePath(name)
}

func isMissingPythonPackageMessage(combined string) bool {
	if !(strings.Contains(combined, "modulenotfounderror:") ||
		strings.Contains(combined, "importerror: no module named") ||
		strings.Contains(combined, "no module named ")) {
		return false
	}
	if isLocalPythonModuleReference(combined) {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	top := pythonTopLevelImportName(name)
	return top == "" || !pythonStdlibModules[top]
}

func isMissingNodePackageMessage(combined string) bool {
	if !strings.Contains(combined, "cannot find module") {
		return false
	}
	if isLocalNodeModuleReference(combined) {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return name == "" || nodePackageName(name) != ""
}

func isMissingNodeESMPackageMessage(combined string) bool {
	if !strings.Contains(combined, "err_module_not_found") || !strings.Contains(combined, "cannot find package") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return name == "" || nodePackageName(name) != ""
}
