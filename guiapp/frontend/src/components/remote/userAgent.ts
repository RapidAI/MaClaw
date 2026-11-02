import type { LLMProvider } from "./LLMConfigPanelShared";

export const CODEGEN_USER_AGENT = "QAgent";
const CODEGEN_LEGACY_USER_AGENT = "tigerclaw";

export const KNOWN_USER_AGENTS = [
    "openclaw",
    "Cline",
    "OpenCode",
    "Codex",
    "Roo Code",
    "Kilo Code",
    "Cursor",
    "Crush",
    "Goose",
    "claude code 2.0",
    CODEGEN_USER_AGENT,
] as const;

// Claude Code is kept as a recognized value for imported/previously saved
// providers, but is intentionally not rendered as a selectable chip.  The
// dedicated `claude code 2.0` identity below is the provider-facing option;
// showing both values in the GUI is confusing because they represent the
// same Claude client family.
const LEGACY_KNOWN_USER_AGENTS = ["opencode", "claude-code/2.0.0", CODEGEN_LEGACY_USER_AGENT] as const;
const NON_SELECTABLE_KNOWN_USER_AGENTS = ["Claude Code"] as const;
const DISPLAY_AGENT_ALIASES = new Map<string, string>([
    ["Claude Code", "claude code 2.0"],
    ["claude-code/2.0.0", "claude code 2.0"],
    [CODEGEN_LEGACY_USER_AGENT, CODEGEN_USER_AGENT],
]);

const knownUserAgentEqual = (known: string, agent: string) =>
    known.toLowerCase() === agent.toLowerCase();

type AgentIdentitySource = Pick<LLMProvider, "name"> & Partial<Pick<LLMProvider, "auth_type" | "import_source" | "agent_type">>;

export const defaultAgentTypeForProvider = (provider?: AgentIdentitySource | null) => {
    if (provider?.name === "CodeGen" && provider?.auth_type === "sso") return CODEGEN_USER_AGENT;
    if (provider?.name === "OpenCode") return "OpenCode";
    switch ((provider?.import_source || "").trim()) {
        case "codex":
            return "Codex";
        case "claude_code":
            return "Claude Code";
        case "opencode":
            return "OpenCode";
        default:
            return "openclaw";
    }
};

export const effectiveAgentType = (provider?: AgentIdentitySource | null) => {
    const raw = (provider?.agent_type || "").trim();
    return raw || defaultAgentTypeForProvider(provider);
};

/** Chip label for a stored identity. Known names match case-insensitively. */
export const canonicalAgentType = (agent: string) => {
    const trimmed = agent.trim();
    if (!trimmed) return "";
    for (const [from, to] of DISPLAY_AGENT_ALIASES) {
        if (knownUserAgentEqual(from, trimmed)) return to;
    }
    for (const known of KNOWN_USER_AGENTS) {
        if (knownUserAgentEqual(known, trimmed)) return known;
    }
    return trimmed;
};

/** Map hidden legacy identities to the canonical visible chip value. */
export const selectableAgentType = (provider?: LLMProvider | null) =>
    canonicalAgentType(effectiveAgentType(provider));

export const isKnownUserAgent = (agent: string) =>
    KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent)) ||
    LEGACY_KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent)) ||
    NON_SELECTABLE_KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent));

/** A saved identity the user typed, as opposed to a preset chip or an empty default. */
export const isExplicitCustomAgent = (provider?: LLMProvider | null) => {
    const raw = (provider?.agent_type ?? "").trim();
    if (!raw) return false;
    return !isKnownUserAgent(selectableAgentType(provider));
};

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

export const commitCustomAgentValue = (provider: LLMProvider | null | undefined, value: string) => {
    return value.trim() ? value.trim() : customAgentSeedForProvider(provider);
};

/** Trimmed custom identity already stored on the provider, or "" for a preset/default. */
export const explicitCustomAgentValue = (provider?: LLMProvider | null) => {
    const raw = (provider?.agent_type ?? "").trim();
    return raw && isExplicitCustomAgent(provider) ? raw : "";
};

/**
 * True when persisting `agent` would not change this provider's identity.
 * An empty agent_type already means the provider default. Writing that
 * default, a display alias, or only a different letter case must not
 * invalidate a passed connection test.
 */
export function agentIdentityUnchanged(provider: AgentIdentitySource | null | undefined, agent: string): boolean {
    const next = canonicalAgentType(agent);
    if (!next) return true;
    const stored = provider?.agent_type ?? "";
    return stored === next || canonicalAgentType(stored || effectiveAgentType(provider)) === next;
}

/** Copy the list with one provider's live User-Agent applied. Same list when nothing changed. */
export function withLiveAgentType<T extends AgentIdentitySource & { connection_test_passed?: boolean }>(
    providers: T[],
    index: number | null,
    agent: string,
): T[] {
    if (index === null || index < 0 || index >= providers.length) return providers;
    const current = providers[index];
    if (agentIdentityUnchanged(current, agent)) return providers;
    const next = canonicalAgentType(agent);
    return providers.map((provider, i) => i === index
        ? { ...provider, agent_type: next, connection_test_passed: false }
        : provider);
}
