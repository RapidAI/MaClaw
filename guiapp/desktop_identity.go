package guiapp

import "strings"

const (
	desktopUserID   = "desktop-user"
	desktopPlatform = "desktop"
)

func trustedDesktopPrincipal(principalID string) bool {
	principalID = strings.TrimSpace(principalID)
	return principalID == desktopUserID || strings.HasPrefix(principalID, desktopUserID+":")
}

// knowledgeOwnerScope is the knowledge-store identity for a principal.
// Desktop task sessions are one desktop user: writes stamp desktop-user, and
// reads also include historical desktop-user:<task> rows plus host-local rows
// whose owner is empty. Any other principal stays an exact owner match.
func knowledgeOwnerScope(principalID string) (owner string, lineage, includeEmpty bool) {
	principalID = strings.TrimSpace(principalID)
	if trustedDesktopPrincipal(principalID) {
		return desktopUserID, true, true
	}
	return principalID, false, false
}
