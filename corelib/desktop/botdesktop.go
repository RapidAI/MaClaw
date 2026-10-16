package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Per-bot desktops (desktop_mode=isolated).
//
// An isolated bot drives its own X display inside its owner's container. Every
// layer keeps working with one "desktop user" string: for an isolated bot it
// is the owner's user id, BotUserSep, and the bot id. MaClawSrv and Hub key
// their per-desktop state (runs, holds, views, handoffs) by that string, so
// two bots of one person never share a lock or a picture. Hub resolves the
// Docker service by the owner part, and desktopd splits it again: the
// container is the owner's, the display is the bot's.
const BotUserSep = "::desk::"

// BotDesktopModeIsolated is the per-bot setting that gives a bot its own display.
const (
	BotDesktopModeShared   = "shared"
	BotDesktopModeIsolated = "isolated"
)

var botIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

// ValidBotID reports whether a bot id can be part of a desktop user.
func ValidBotID(botID string) bool {
	return botIDPattern.MatchString(strings.TrimSpace(botID))
}

// BotDesktopUser is the desktop user of one isolated bot. An empty bot id is
// the owner's own (shared) desktop.
func BotDesktopUser(userID, botID string) string {
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return userID
	}
	if owner, _ := SplitDesktopUser(userID); owner != userID {
		userID = owner
	}
	return userID + BotUserSep + botID
}

// SplitDesktopUser returns the owner user id and the bot id ("" for the
// owner's own desktop). A malformed bot part is kept in the user id, which
// then fails ValidUserID-based checks downstream instead of silently
// opening the owner's desktop.
func SplitDesktopUser(desktopUser string) (string, string) {
	desktopUser = strings.TrimSpace(desktopUser)
	i := strings.Index(desktopUser, BotUserSep)
	if i < 0 {
		return desktopUser, ""
	}
	owner, bot := desktopUser[:i], desktopUser[i+len(BotUserSep):]
	if owner == "" || !ValidBotID(bot) {
		return desktopUser, ""
	}
	return owner, bot
}

// BotKey is the supervisor key of one bot display: the owner's UserKey
// followed by 16 hex digits of the bot id. The supervisor accepts it only in
// that owner's container.
func BotKey(tenantID, userID, botID string) (string, error) {
	owner, err := UserKey(tenantID, userID)
	if err != nil {
		return "", err
	}
	botID = strings.TrimSpace(botID)
	if !ValidBotID(botID) {
		return "", fmt.Errorf("%w: bot is invalid", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(botID))
	return owner + hex.EncodeToString(sum[:8]), nil
}

// DesktopKey is the supervisor key for a desktop user: the bot key for an
// isolated bot, otherwise the owner's UserKey.
func DesktopKey(tenantID, desktopUser string) (string, error) {
	owner, bot := SplitDesktopUser(desktopUser)
	if bot == "" {
		return UserKey(tenantID, owner)
	}
	return BotKey(tenantID, owner, bot)
}

var desktopKeyPattern = regexp.MustCompile(`^[0-9a-f]{16}([0-9a-f]{16})?$`)

// ValidDesktopKey accepts a user key or a bot key.
func ValidDesktopKey(key string) bool {
	return desktopKeyPattern.MatchString(key)
}
