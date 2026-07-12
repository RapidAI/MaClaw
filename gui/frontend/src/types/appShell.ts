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
    cached_input_tokens?: number;
    cache_write_tokens?: number;
    requests?: number;
    cached_requests?: number;
    local_cache_requests?: number;
    local_cache_hits?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
    CachedInputTokens?: number;
    CacheWriteTokens?: number;
    Requests?: number;
    CachedRequests?: number;
    LocalCacheRequests?: number;
    LocalCacheHits?: number;
}

export interface SidebarCurrentProviderTokenUsage {
    provider: string;
    isHubService: boolean;
    input: number;
    output: number;
    total: number;
    cachedInput?: number;
    cacheWrite?: number;
    requests?: number;
    cachedRequests?: number;
    localCacheRequests?: number;
    localCacheHits?: number;
}

export interface SidebarHubPeriodLimits {
    five_hour?: number;
    daily?: number;
    weekly?: number;
    monthly?: number;
    FiveHour?: number;
    Daily?: number;
    Weekly?: number;
    Monthly?: number;
}

export interface SidebarHubCreditGrant {
    source?: string;
    active?: boolean;
    effective?: boolean;
    status?: string;
    status_reason?: string;
    credits_total?: number;
    credits_used?: number;
    credits_remaining?: number;
    credits_available?: number;
    period_limits?: SidebarHubPeriodLimits;
    retry_after_seconds?: number;
    retry_after_at?: string;
    starts_at?: string;
    expires_at?: string;
    Active?: boolean;
    Effective?: boolean;
    Status?: string;
    StatusReason?: string;
    CreditsTotal?: number;
    CreditsUsed?: number;
    CreditsRemaining?: number;
    CreditsAvailable?: number;
    PeriodLimits?: SidebarHubPeriodLimits;
    RetryAfterSeconds?: number;
    RetryAfterAt?: string;
    StartsAt?: string;
    ExpiresAt?: string;
    Source?: string;
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
    serviceActive?: boolean;
    total: number;
    used: number;
    /** Lifetime account remaining (includes queued metered balances). */
    remaining: number;
    /**
     * Currently spendable credits for the active period/route window.
     * May be lower than `remaining` when period limits apply.
     */
    available: number;
    /** True when available is meaningfully lower than remaining (show both). */
    showPeriodAvailable: boolean;
    tokensPerCredit: number;
    expiresAt: string;
    unlimited: boolean;
    status: string;
    retryAfterSeconds: number;
    retryAfterAt: string;
}

export interface SidebarCreditDisplayFormatters {
    formatSidebarTokens: (value: number) => string;
    formatSidebarHubExpiry: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubTotalCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubUsedCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarCredit: (value: number) => string;
}

export interface SidebarLLMProviderSummary {
    name: string;
    url: string;
    isHubService: boolean;
    /** True when the provider has enough configuration to be usable (URL + key, or hub service). */
    configured?: boolean;
}
