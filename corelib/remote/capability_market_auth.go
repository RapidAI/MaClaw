package remote

// CapabilityMarketAuthClient is the unified name for the capability market
// authentication client. It is a type alias for backward compatibility with
// existing code that uses SkillMarketAuthClient.
type CapabilityMarketAuthClient = SkillMarketAuthClient

// CapabilityMarketAuthResult is the unified name for the capability market
// authentication result. It is a type alias for backward compatibility.
type CapabilityMarketAuthResult = SkillMarketAuthResult

// NewCapabilityMarketAuthClient creates a new capability market auth client.
// This is the preferred constructor name; NewSkillMarketAuthClient is retained
// as a backward-compatible alias.
func NewCapabilityMarketAuthClient() *CapabilityMarketAuthClient {
	return NewSkillMarketAuthClient()
}
