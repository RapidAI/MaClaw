import type { LLMProvider } from "./LLMConfigPanelShared";

export const KNOWN_USER_AGENTS = [
    "openclaw",
    "Claude Code",
    "Cline",
    "OpenCode",
    "Roo Code",
    "Kilo Code",
    "Cursor",
    "Crush",
    "Goose",
    "claude code 2.0",
    "tigerclaw",
] as const;

const LEGACY_KNOWN_USER_AGENTS = ["opencode", "claude-code/2.0.0"] as const;

export const defaultAgentTypeForProvider = (provider?: LLMProvider | null) => {
    if (provider?.name === "CodeGen" && provider?.auth_type === "sso") return "tigerclaw";
    return "openclaw";
};

export const effectiveAgentType = (provider?: LLMProvider | null) => {
    const raw = (provider?.agent_type || "").trim();
    return raw || defaultAgentTypeForProvider(provider);
};

export const isKnownUserAgent = (agent: string) =>
    KNOWN_USER_AGENTS.some(known => known === agent) || LEGACY_KNOWN_USER_AGENTS.some(known => known === agent);

export const customAgentSeedForProvider = (provider?: LLMProvider | null) => {
    const current = effectiveAgentType(provider);
    if (!isKnownUserAgent(current)) return current;
    return "custom-client";
};

export const editableCustomAgentValue = (provider?: LLMProvider | null) => {
    const raw = provider?.agent_type ?? "";
    // Keep raw value (including empty string) so the controlled input reflects
    // exactly what's stored and allows full deletion without flickering.
    // Only fall back to the seed when agent_type is completely absent (undefined/null).
    if (provider?.agent_type == null) return customAgentSeedForProvider(provider);
    const trimmed = raw.trim();
    if (trimmed && isKnownUserAgent(trimmed)) return customAgentSeedForProvider(provider);
    return raw;
};

/** Called on blur / save — fills in the seed if the field was left empty. */
export const nextCustomAgentValue = (_provider: LLMProvider | null | undefined, value: string) => value;

export const commitCustomAgentValue = (provider: LLMProvider | null | undefined, value: string) => {
    return value.trim() ? value.trim() : customAgentSeedForProvider(provider);
};
