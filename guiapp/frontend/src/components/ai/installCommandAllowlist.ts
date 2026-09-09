/**
 * Shared install-command allowlist helpers for the AI assistant panel.
 *
 * Source of truth (also embedded by the Go backend via go:embed):
 *   ./installCommandAllowlist.json
 */
import allowlist from "./installCommandAllowlist.json";

export type InstallCommandAllowlist = typeof allowlist;

export const installCommandAllowlist: InstallCommandAllowlist = allowlist;

type NestedSpec = {
    aliases?: string[];
    actions?: string[];
};

function lowerSet(values: string[] | undefined): Set<string> {
    const set = new Set<string>();
    for (const v of values ?? []) {
        const key = v.trim().toLowerCase();
        if (key) set.add(key);
    }
    return set;
}

const META_ACTIONS = lowerSet(allowlist.meta_actions);

const COMMAND_ALIASES = new Map<string, string>();
const COMMAND_ACTIONS = new Map<string, Set<string>>();
/** nested[cmd][parentAction] → allowed sub-actions */
const COMMAND_NESTED = new Map<string, Map<string, Set<string>>>();

for (const [name, spec] of Object.entries(allowlist.commands)) {
    const canonical = name.trim().toLowerCase();
    COMMAND_ALIASES.set(canonical, canonical);
    for (const alias of spec.aliases ?? []) {
        const a = alias.trim().toLowerCase();
        if (a) COMMAND_ALIASES.set(a, canonical);
    }
    const acts = lowerSet(spec.actions);
    for (const m of META_ACTIONS) acts.add(m);

    const nestedRaw = "nested" in spec ? (spec.nested as Record<string, NestedSpec>) : undefined;
    if (nestedRaw) {
        const nested = new Map<string, Set<string>>();
        for (const [parent, value] of Object.entries(nestedRaw)) {
            const aliases = value?.aliases ?? [];
            const actions = value?.actions ?? [];
            const set = lowerSet(actions);
            for (const m of META_ACTIONS) set.add(m);
            const p = parent.trim().toLowerCase();
            // Nested parents (and aliases) are also top-level actions.
            acts.add(p);
            nested.set(p, set);
            for (const alias of aliases) {
                const a = alias.trim().toLowerCase();
                if (!a) continue;
                acts.add(a);
                nested.set(a, set);
            }
        }
        COMMAND_NESTED.set(canonical, nested);
    }

    COMMAND_ACTIONS.set(canonical, acts);
}

const BINARY_PREFIXES = lowerSet(
    (allowlist.binary_prefixes ?? []).map((p) => p.replace(/\.exe$/i, "")),
);

/** Regex matching a leading CLI binary token + space (e.g. `maclaw-tui `). */
export const INSTALL_BIN_PREFIX: RegExp = (() => {
    const parts = [...BINARY_PREFIXES]
        .map((p) => p.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
        .sort((a, b) => b.length - a.length);
    if (parts.length === 0) {
        return /^(?!)/; // never matches
    }
    return new RegExp(`^(?:${parts.join("|")})(?:\\.exe)?\\s+`, "i");
})();

export function normalizeInstallCommand(name: string): string {
    const key = name.trim().toLowerCase();
    return COMMAND_ALIASES.get(key) ?? key;
}

export function isKnownInstallCommand(name: string): boolean {
    return COMMAND_ALIASES.has(name.trim().toLowerCase());
}

export function isInstallMetaAction(action: string): boolean {
    return META_ACTIONS.has(action.trim().toLowerCase());
}

/** Whether action is a nested parent for cmd (e.g. plugin + marketplace). */
export function isInstallNestedParent(cmd: string, action: string): boolean {
    const nested = COMMAND_NESTED.get(normalizeInstallCommand(cmd));
    return !!nested?.has(action.trim().toLowerCase());
}

/**
 * Whether cmd+args is an allowed install slash action.
 * Mirrors Go installActionAllowed() from the same JSON allowlist.
 */
export function isInstallActionAllowed(cmd: string, args: string[]): boolean {
    if (!args.length) return false;
    const canonical = normalizeInstallCommand(cmd);
    const action = args[0].trim().toLowerCase();

    const acts = COMMAND_ACTIONS.get(canonical);
    if (!acts) return false;
    // Meta help is allowed only for known install commands.
    if (META_ACTIONS.has(action)) return true;
    if (!acts.has(action)) return false;

    const nested = COMMAND_NESTED.get(canonical);
    if (nested?.has(action)) {
        if (args.length === 1) return true; // bare parent shows usage
        const sub = args[1].trim().toLowerCase();
        return nested.get(action)!.has(sub);
    }
    return true;
}

/** Basename of a CLI token (strips path + .exe + surrounding quotes). */
export function installCLIBinaryBaseName(token: string): string {
    let t = token.trim().toLowerCase();
    if (
        (t.startsWith('"') && t.endsWith('"')) ||
        (t.startsWith("'") && t.endsWith("'"))
    ) {
        t = t.slice(1, -1);
    }
    t = t.replace(/\.exe$/i, "");
    const slash = Math.max(t.lastIndexOf("/"), t.lastIndexOf("\\"));
    if (slash >= 0) t = t.slice(slash + 1);
    return t.replace(/\.exe$/i, "").trim();
}

export function isInstallCLIBinaryPrefix(token: string): boolean {
    return BINARY_PREFIXES.has(installCLIBinaryBaseName(token));
}
