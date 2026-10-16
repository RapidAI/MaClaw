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
    [CODEGEN_LEGACY_USER_AGENT, CODEGEN_USER_AGENT],
]);

const knownUserAgentEqual = (known: string, agent: string) =>
    known.toLowerCase() === agent.toLowerCase();

export const defaultAgentTypeForProvider = (provider?: LLMProvider | null) => {
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

export const effectiveAgentType = (provider?: LLMProvider | null) => {
    const raw = (provider?.agent_type || "").trim();
    return raw || defaultAgentTypeForProvider(provider);
};

/** Map hidden legacy identities to the canonical visible chip value. */
export const selectableAgentType = (provider?: LLMProvider | null) => {
    const current = effectiveAgentType(provider);
    for (const [from, to] of DISPLAY_AGENT_ALIASES) {
        if (knownUserAgentEqual(from, current)) return to;
    }
    return current;
};

export const isKnownUserAgent = (agent: string) =>
    KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent)) ||
    LEGACY_KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent)) ||
    NON_SELECTABLE_KNOWN_USER_AGENTS.some(known => knownUserAgentEqual(known, agent));

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
