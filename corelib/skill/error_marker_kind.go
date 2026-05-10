package skill

import "strings"

func hasSkillRateLimitMarker(combined string) bool {
	return strings.Contains(combined, "429") &&
		(strings.Contains(combined, "rate limit") ||
			strings.Contains(combined, "too many requests") ||
			strings.Contains(combined, "frequency limit") ||
			strings.Contains(combined, "\u9891\u7387\u9650\u5236"))
}

func hasSkillAuthErrorMarker(combined string) bool {
	return (strings.Contains(combined, "401") && strings.Contains(combined, "unauthorized")) ||
		(strings.Contains(combined, "403") && strings.Contains(combined, "forbidden")) ||
		strings.Contains(combined, "permission denied") ||
		strings.Contains(combined, "access denied")
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
