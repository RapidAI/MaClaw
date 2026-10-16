package llmservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestRegistryHostsTokenBankAcceptsArrayOrMemberOnly(t *testing.T) {
	member := TokenBankMemberID("share-1", "gpt-4o")
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{{
		ID: "paid",
		Models: []llmpool.ModelConfig{{
			Name:            "gpt-4o",
			ProviderIDs:     []string{TokenBankArrayHigh},
			ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: member}},
		}},
	}}}
	if !RegistryHostsTokenBank(reg, "paid") {
		t.Fatal("array id in the service group must count as token bank")
	}
	memberOnly := &Registry{ServiceGroups: []llmpool.ServiceGroup{{
		ID:     "paid",
		Models: []llmpool.ModelConfig{{Name: "gpt-4o", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: member}}}},
	}}}
	if !RegistryHostsTokenBank(memberOnly, "paid") {
		t.Fatal("tbk_ member must count as token bank")
	}
	plain := &Registry{ServiceGroups: []llmpool.ServiceGroup{{
		ID:     "paid",
		Models: []llmpool.ModelConfig{{Name: "gpt-4o", ProviderIDs: []string{"openai"}}},
	}}}
	if RegistryHostsTokenBank(plain, "paid") {
		t.Fatal("a group with no token bank route must be rejected")
	}
	if RegistryHostsTokenBank(reg, "other") {
		t.Fatal("unknown group must be rejected")
	}
}

func TestRegistryHostsTokenBankOfficialHubEntryUsesComputeGroup(t *testing.T) {
	reg := &Registry{
		DefaultServiceGroupID: "redeem",
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "redeem",
				AccessPolicy: AccessPolicyGrantRequired,
				Kind:         llmpool.ServiceGroupKindDynamic,
				Models: []llmpool.ModelConfig{{
					Name:        "auto",
					ProviderIDs: []string{TokenBankArrayMid},
				}},
			},
			{
				ID:           llmpool.OfficialGroupID,
				AccessPolicy: AccessPolicyGrantRequired,
				Kind:         llmpool.ServiceGroupKindDynamic,
				Models: []llmpool.ModelConfig{{
					Name:        "auto",
					ProviderIDs: []string{"opencode-1"},
				}},
			},
		},
	}
	if !RegistryHostsTokenBank(reg, llmpool.HubOfficialServiceGroupID) {
		t.Fatal("official hub entry must follow the compute group that hosts token bank")
	}
	if RegistryHostsTokenBank(reg, llmpool.OfficialGroupID) {
		t.Fatal("a literal catalog group without a token bank route must be rejected")
	}
}

func TestRegistryHostsTokenBankOfficialHubEntryRejectsComputeGroupWithoutTokenBank(t *testing.T) {
	reg := &Registry{
		DefaultServiceGroupID: llmpool.OfficialGroupID,
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           llmpool.OfficialGroupID,
			AccessPolicy: AccessPolicyGrantRequired,
			Kind:         llmpool.ServiceGroupKindDynamic,
			Models: []llmpool.ModelConfig{{
				Name:        "auto",
				ProviderIDs: []string{"opencode-1"},
			}},
		}},
	}
	if RegistryHostsTokenBank(reg, llmpool.HubOfficialServiceGroupID) {
		t.Fatal("official entry whose compute group has no token bank route must be rejected")
	}
}

func TestRegistryHostsTokenBankOfficialHubEntryDoesNotBorrowAnotherGroup(t *testing.T) {
	reg := &Registry{
		DefaultServiceGroupID: llmpool.OfficialGroupID,
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           llmpool.OfficialGroupID,
				AccessPolicy: AccessPolicyGrantRequired,
				Kind:         llmpool.ServiceGroupKindDynamic,
				Models: []llmpool.ModelConfig{{
					Name:        "auto",
					ProviderIDs: []string{"opencode-1"},
				}},
			},
			{
				ID:           "redeem",
				AccessPolicy: AccessPolicyGrantRequired,
				Kind:         llmpool.ServiceGroupKindDynamic,
				Models: []llmpool.ModelConfig{{
					Name:        "auto",
					ProviderIDs: []string{TokenBankArrayMid},
				}},
			},
		},
	}
	if RegistryHostsTokenBank(reg, llmpool.HubOfficialServiceGroupID) {
		t.Fatal("official entry must follow the configured compute group, not a later group that hosts token bank")
	}
}
