package llmservice

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/trae"
)

// vendorAuthKinds lists every account-login auth kind beside WorkBuddy. They
// all share the same lifecycle: the admin form starts a login, applies its
// credential once, and from then on only the rotating tokens are written back.
func vendorAuthKinds() map[string]string {
	return map[string]string{
		llmpool.ProviderAuthQoder:     llmpool.ProviderAuthQoder,
		llmpool.ProviderAuthTrae:      llmpool.ProviderAuthTrae,
		llmpool.ProviderAuthLobsterAI: llmpool.ProviderAuthLobsterAI,
	}
}

// clearVendorSecrets drops the account-login credential fields of every vendor
// except WorkBuddy, which has its own clearer.
func clearVendorSecrets(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.QoderSessionID = ""
	provider.TraeSessionID = ""
	provider.LobsterAISessionID = ""
	clearQoderSecrets(provider)
	clearTraeSecrets(provider)
	clearLobsterAISecrets(provider)
}

// normalizeProviderAuth keeps one auth kind per provider and never persists a
// login session id: the credential it names is applied once on create/update.
func normalizeProviderAuth(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.QoderSessionID = ""
	provider.TraeSessionID = ""
	provider.LobsterAISessionID = ""
	kind := strings.TrimSpace(provider.AuthKind)
	if _, ok := vendorAuthKinds()[kind]; !ok {
		if kind == llmpool.ProviderAuthWorkBuddy {
			normalizeWorkBuddyAuthKind(provider)
			return
		}
		// An unknown kind is a stale row or a hand-edited one; the provider then
		// behaves as a static key rather than failing every request.
		provider.AuthKind = ""
		clearVendorSecrets(provider)
		return
	}
	switch kind {
	case llmpool.ProviderAuthQoder:
		provider.QoderEdition = canonicalQoderEdition(provider.QoderEdition)
		if strings.TrimSpace(provider.QoderEdition) == "" {
			provider.QoderEdition = canonicalQoderEdition(existingQoderEdition(provider))
		}
	case llmpool.ProviderAuthTrae:
		provider.TraeEdition = canonicalTraeEdition(provider.TraeEdition)
		if strings.TrimSpace(provider.TraeEdition) == "" {
			provider.TraeEdition = canonicalTraeEdition(existingTraeEdition(provider))
		}
	}
}

// existingQoderEdition recovers the edition of a row whose edition field went
// missing, so a later save cannot silently re-point the provider at the wrong
// signing gateway.
func existingQoderEdition(provider *llmpool.ProviderConfig) string {
	if profile, ok := qoder.ProfileByName(provider.Name); ok {
		return profile.ID
	}
	return ""
}

// existingTraeEdition recovers the edition of a row whose edition field went
// missing, so chat requests keep carrying the right SOLO host and headers.
func existingTraeEdition(provider *llmpool.ProviderConfig) string {
	if profile, ok := trae.ProfileByName(provider.Name); ok {
		return profile.ID
	}
	return ""
}

// mergeVendorAuth carries an account-login credential across an update that
// did not resend it. Without this the admin form, which omits the tokens, would
// blank the credential of a Qoder, Trae or LobsterAI provider on every save.
// An omitted auth kind means the caller is not touching the login, so the
// existing vendor is adopted the same way WorkBuddy is. An explicit api_key
// selection must not adopt anything: the admin converted the row to a static
// key, and reviving the account would leave a dead credential behind.
func mergeVendorAuth(existing, incoming llmpool.ProviderConfig) llmpool.ProviderConfig {
	switch strings.TrimSpace(incoming.AuthKind) {
	case llmpool.ProviderAuthAPIKey:
		clearVendorSecrets(&incoming)
		return incoming
	case "":
		kind := strings.TrimSpace(existing.AuthKind)
		if _, ok := vendorAuthKinds()[kind]; !ok {
			return incoming
		}
		incoming.AuthKind = kind
		dropOtherVendorCredentials(&incoming, kind)
		switch kind {
		case llmpool.ProviderAuthQoder:
			mergeQoderCredential(existing, &incoming)
		case llmpool.ProviderAuthTrae:
			mergeTraeCredential(existing, &incoming)
		case llmpool.ProviderAuthLobsterAI:
			mergeLobsterAICredential(existing, &incoming)
		}
		return incoming
	case llmpool.ProviderAuthQoder:
		dropOtherVendorCredentials(&incoming, llmpool.ProviderAuthQoder)
		mergeQoderCredential(existing, &incoming)
	case llmpool.ProviderAuthTrae:
		dropOtherVendorCredentials(&incoming, llmpool.ProviderAuthTrae)
		mergeTraeCredential(existing, &incoming)
	case llmpool.ProviderAuthLobsterAI:
		dropOtherVendorCredentials(&incoming, llmpool.ProviderAuthLobsterAI)
		mergeLobsterAICredential(existing, &incoming)
	}
	return incoming
}

// mergeQoderCredential fills the Qoder account fields the caller omitted. A
// resent refresh token means the admin re-ran the login, so the stored values
// stay out of the way. The stored row must itself be Qoder: adopting the
// credential of a provider that just switched vendors would leave the previous
// account's access token on this row, and the chat request would fail against
// an upstream that never issued it.
func mergeQoderCredential(existing llmpool.ProviderConfig, incoming *llmpool.ProviderConfig) {
	if strings.TrimSpace(incoming.QoderRefreshToken) != "" {
		return
	}
	if strings.TrimSpace(existing.AuthKind) != llmpool.ProviderAuthQoder {
		return
	}
	incoming.QoderRefreshToken = existing.QoderRefreshToken
	incoming.QoderExpiresAt = existing.QoderExpiresAt
	incoming.QoderUserID = existing.QoderUserID
	incoming.QoderMachineID = existing.QoderMachineID
	if strings.TrimSpace(incoming.QoderEdition) == "" {
		incoming.QoderEdition = existing.QoderEdition
	}
	if strings.TrimSpace(incoming.APIKey) == "" {
		incoming.APIKey = existing.APIKey
	}
}

func mergeTraeCredential(existing llmpool.ProviderConfig, incoming *llmpool.ProviderConfig) {
	if strings.TrimSpace(incoming.TraeRefreshToken) != "" {
		return
	}
	if strings.TrimSpace(existing.AuthKind) != llmpool.ProviderAuthTrae {
		return
	}
	incoming.TraeRefreshToken = existing.TraeRefreshToken
	incoming.TraeExpiresAt = existing.TraeExpiresAt
	incoming.TraeUserID = existing.TraeUserID
	incoming.TraeMachineID = existing.TraeMachineID
	incoming.TraeDeviceID = existing.TraeDeviceID
	if strings.TrimSpace(incoming.TraeEdition) == "" {
		incoming.TraeEdition = existing.TraeEdition
	}
	if strings.TrimSpace(incoming.APIKey) == "" {
		incoming.APIKey = existing.APIKey
	}
}

func mergeLobsterAICredential(existing llmpool.ProviderConfig, incoming *llmpool.ProviderConfig) {
	if strings.TrimSpace(incoming.LobsterAIRefreshToken) != "" {
		return
	}
	if strings.TrimSpace(existing.AuthKind) != llmpool.ProviderAuthLobsterAI {
		return
	}
	incoming.LobsterAIRefreshToken = existing.LobsterAIRefreshToken
	incoming.LobsterAIExpiresAt = existing.LobsterAIExpiresAt
	incoming.LobsterAIUserID = existing.LobsterAIUserID
	incoming.LobsterAISessionUUID = existing.LobsterAISessionUUID
	incoming.LobsterAIFirstKeyfrom = existing.LobsterAIFirstKeyfrom
	if strings.TrimSpace(incoming.APIKey) == "" {
		incoming.APIKey = existing.APIKey
	}
}

// dropOtherVendorCredentials keeps only the credential of the keeper auth kind,
// so a provider switched from one account login to another does not keep the
// previous vendor's tokens. keep is the kind that survives; anything else is
// cleared.
func dropOtherVendorCredentials(provider *llmpool.ProviderConfig, keep string) {
	if provider == nil {
		return
	}
	keep = strings.TrimSpace(keep)
	if keep != llmpool.ProviderAuthQoder {
		provider.QoderSessionID = ""
		clearQoderSecrets(provider)
	}
	if keep != llmpool.ProviderAuthTrae {
		provider.TraeSessionID = ""
		clearTraeSecrets(provider)
	}
	if keep != llmpool.ProviderAuthLobsterAI {
		provider.LobsterAISessionID = ""
		clearLobsterAISecrets(provider)
	}
}
