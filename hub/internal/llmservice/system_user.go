package llmservice

import (
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	// SystemLLMUserEmail is the stable Hub account that service-group API keys
	// authenticate and record usage as. It is listed as a fixed user in usage
	// stats and is exempt from new-user welcome benefits.
	SystemLLMUserEmail    = "sys_user"
	systemLLMUserIDPrefix = "sys_user_"
)

// SystemLLMUserID is the tenant-scoped primary key for the sys_user account.
func SystemLLMUserID(tenantID string) string {
	return systemLLMUserIDPrefix + store.NormalizeTenantID(tenantID)
}

// IsSystemLLMUser reports whether the identity is the Hub service-group API
// key account. Email matching is enough because usage reports key by email;
// the ID prefix covers lookups that only have a user id.
func IsSystemLLMUser(userID, email string) bool {
	if strings.EqualFold(strings.TrimSpace(email), SystemLLMUserEmail) {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(userID), systemLLMUserIDPrefix)
}
