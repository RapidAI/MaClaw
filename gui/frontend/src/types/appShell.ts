export interface RemoteCenterHubOption {
    hub_id: string;
    name: string;
    base_url: string;
    pwa_url?: string;
    visibility?: string;
    enrollment_mode?: string;
    status?: string;
}

export interface SidebarTokenUsageStat {
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
}

export interface SidebarHubCreditGrant {
    active?: boolean;
    credits_total?: number;
    credits_used?: number;
    credits_remaining?: number;
    starts_at?: string;
    expires_at?: string;
    Active?: boolean;
    CreditsTotal?: number;
    CreditsUsed?: number;
    CreditsRemaining?: number;
    StartsAt?: string;
    ExpiresAt?: string;
}

export interface SidebarHubServiceStatus {
    active?: boolean;
    Active?: boolean;
    active_grants?: SidebarHubCreditGrant[];
    ActiveGrants?: SidebarHubCreditGrant[];
    credit_grants?: SidebarHubCreditGrant[];
    CreditGrants?: SidebarHubCreditGrant[];
    credits_total?: number;
    CreditsTotal?: number;
    credits_used?: number;
    CreditsUsed?: number;
    credits_remaining?: number;
    CreditsRemaining?: number;
    credits_available?: number;
    CreditsAvailable?: number;
    tokens_per_credit?: number;
    TokensPerCredit?: number;
    nearest_expires_at?: string;
    NearestExpiresAt?: string;
    effective_expires_at?: string;
    EffectiveExpiresAt?: string;
    hub_llm_base_url?: string;
    HubLLMBaseURL?: string;
}

export interface SidebarHubCredits {
    authorized: boolean;
    total: number;
    used: number;
    remaining: number;
    tokensPerCredit: number;
    expiresAt: string;
    unlimited: boolean;
}

export interface SidebarLLMProviderSummary {
    name: string;
    url: string;
    isHubService: boolean;
}
