package main

import "strings"

const (
	desktopUserID   = "desktop-user"
	desktopPlatform = "desktop"
)

func trustedDesktopPrincipal(principalID string) bool {
	principalID = strings.TrimSpace(principalID)
	return principalID == desktopUserID || strings.HasPrefix(principalID, desktopUserID+":")
}
