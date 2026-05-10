//go:build darwin

package accessibility

import "strings"

type accessibilityErrorKind string

const (
	accessibilityErrorUnknown          accessibilityErrorKind = ""
	accessibilityErrorPermissionDenied accessibilityErrorKind = "permission_denied"
)

const accessibilityPermissionDeniedMessage = "accessibility permission denied: grant access in System Preferences > Privacy & Security > Accessibility"

func classifyAccessibilityErrorMessage(message string) accessibilityErrorKind {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "accessibility permission denied"),
		strings.Contains(message, "AXError"),
		strings.Contains(message, "kAXErrorCannotComplete"),
		strings.Contains(lower, "not trusted"),
		strings.Contains(lower, "accessibility"):
		return accessibilityErrorPermissionDenied
	default:
		return accessibilityErrorUnknown
	}
}

func isAccessibilityPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	return classifyAccessibilityErrorMessage(err.Error()) == accessibilityErrorPermissionDenied
}
