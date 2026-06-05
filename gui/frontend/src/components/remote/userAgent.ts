import type { LLMProvider } from "./LLMConfigPanelShared";

export const KNOWN_USER_AGENTS = ["openclaw", "claude-code/2.0.0", "tigerclaw"] as const;

export const defaultAgentTypeForProvider = (provider?: LLMProvider | null) => {
    if (provider?.name === "CodeGen" && provider?.auth_type === "sso") return "tigerclaw";
    return "openclaw";
};

export const effectiveAgentType = (provider?: LLMProvider | null) => {
    const raw = (provider?.agent_type || "").trim();
    return raw || defaultAgentTypeForProvider(provider);
};

export const isKnownUserAgent = (agent: string) => KNOWN_USER_AGENTS.some(known => known === agent);

export const customAgentSeedForProvider = (provider?: LLMProvider | null) => {
    const current = effectiveAgentType(provider);
    if (!isKnownUserAgent(current)) return current;
    return "custom-client";
};

export const editableCustomAgentValue = (provider?: LLMProvider | null) => {
    const raw = provider?.agent_type ?? "";
    const trimmed = raw.trim();
    if (trimmed && !isKnownUserAgent(trimmed)) return raw;
    return customAgentSeedForProvider(provider);
};

export const nextCustomAgentValue = (provider: LLMProvider | null | undefined, value: string) => {
    return value.trim() ? value : customAgentSeedForProvider(provider);
};
