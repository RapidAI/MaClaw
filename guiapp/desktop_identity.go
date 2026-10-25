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
// lineage still reaches historical desktop-user:<task> rows. Empty owners are
// historical; opening the desktop library stamps them to desktop-user once.
// includeEmpty stays true for callers that have not moved to membership reads.
// Desktop search and list turn it off. Any other principal stays an exact match.
func knowledgeOwnerScope(principalID string) (owner string, lineage, includeEmpty bool) {
	principalID = strings.TrimSpace(principalID)
	if trustedDesktopPrincipal(principalID) {
		return desktopUserID, true, true
	}
	return principalID, false, false
}
