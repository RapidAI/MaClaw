/**
 * localStorage helpers for welcome-scenario tasks:
 * - last field values per task (by field label)
 * - recently used task keys
 */

import { SCENARIO_TABS, type WelcomePrompt } from "./welcomeScenarioTasks";

export const WELCOME_FIELD_VALUES_KEY = "maclaw:welcome-task-field-values";
export const WELCOME_RECENT_KEY = "maclaw:welcome-recent-tasks";
export const WELCOME_PREVIEW_OPEN_KEY = "maclaw:welcome-param-preview-open";
export const WELCOME_USER_ROLE_KEY = "maclaw:welcome-user-role";

export const WELCOME_RECENT_MAX = 4;

/** Workbench persona used to default the scenario tab. */
export type WelcomeUserRole =
    | "auto"
    | "dev"
    | "ops"
    | "business"
    | "research"
    | "writing"
    | "general";

export const WELCOME_USER_ROLES: WelcomeUserRole[] = [
    "auto",
    "dev",
    "ops",
    "business",
    "research",
    "writing",
    "general",
];

/** Default scenario tab id for each role. */
export const WELCOME_ROLE_DEFAULT_TAB: Record<WelcomeUserRole, string> = {
    auto: "business",
    dev: "dev",
    ops: "ops",
    business: "business",
    research: "research",
    writing: "writing",
    general: "business",
};

export function isWelcomeUserRole(value: string | null | undefined): value is WelcomeUserRole {
    return !!value && (WELCOME_USER_ROLES as string[]).includes(value);
}

export function loadWelcomeUserRole(): WelcomeUserRole {
    try {
        const raw = localStorage.getItem(WELCOME_USER_ROLE_KEY);
        if (isWelcomeUserRole(raw)) return raw;
    } catch { /* ignore */ }
    return "auto";
}

export function saveWelcomeUserRole(role: WelcomeUserRole): void {
    try {
        localStorage.setItem(WELCOME_USER_ROLE_KEY, role);
    } catch { /* ignore */ }
}

/**
 * Resolve the initial scenario tab:
 * 1) explicit last-used tab (if valid)
 * 2) role default (auto → most common recent tab, else business)
 */
export function resolveWelcomeDefaultTab(
    lastTab: string | null | undefined,
    role: WelcomeUserRole = loadWelcomeUserRole(),
    recent: WelcomeRecentEntry[] = loadWelcomeRecentEntries(),
): string {
    const validIds = new Set(SCENARIO_TABS.map((t) => t.id));
    if (lastTab && validIds.has(lastTab)) return lastTab;

    if (role === "auto") {
        // Prefer the tab that appears most in recent history.
        const counts = new Map<string, number>();
        for (const e of recent) {
            if (!validIds.has(e.tabId)) continue;
            counts.set(e.tabId, (counts.get(e.tabId) || 0) + 1);
        }
        let best = "";
        let bestN = 0;
        for (const [id, n] of counts) {
            if (n > bestN) {
                best = id;
                bestN = n;
            }
        }
        if (best) return best;
        return WELCOME_ROLE_DEFAULT_TAB.auto;
    }

    const preferred = WELCOME_ROLE_DEFAULT_TAB[role];
    return validIds.has(preferred) ? preferred : SCENARIO_TABS[0].id;
}

export type WelcomeRecentEntry = {
    /** Stable id: `${tabId}::${textEn}` */
    key: string;
    tabId: string;
    textEn: string;
    usedAt: number;
};

/** Field values keyed by field label (stable across template reordering). */
export type WelcomeFieldValuesByLabel = Record<string, string>;

export function welcomePromptKey(tabId: string, textEn: string): string {
    return `${tabId}::${textEn}`;
}

export function parseWelcomePromptKey(key: string): { tabId: string; textEn: string } | null {
    const sep = key.indexOf("::");
    if (sep <= 0) return null;
    const tabId = key.slice(0, sep);
    const textEn = key.slice(sep + 2);
    if (!tabId || !textEn) return null;
    return { tabId, textEn };
}

export function findWelcomePromptByKey(
    key: string,
): { tabId: string; prompt: WelcomePrompt } | null {
    const parsed = parseWelcomePromptKey(key);
    if (!parsed) return null;
    const tab = SCENARIO_TABS.find((t) => t.id === parsed.tabId);
    if (!tab) return null;
    const prompt = tab.prompts.find((p) => p.textEn === parsed.textEn);
    if (!prompt) return null;
    return { tabId: tab.id, prompt };
}

function readJson<T>(key: string, fallback: T): T {
    try {
        const raw = localStorage.getItem(key);
        if (!raw) return fallback;
        return JSON.parse(raw) as T;
    } catch {
        return fallback;
    }
}

function writeJson(key: string, value: unknown): void {
    try {
        localStorage.setItem(key, JSON.stringify(value));
    } catch {
        /* quota / private mode */
    }
}

export function loadWelcomeFieldValues(taskKey: string): WelcomeFieldValuesByLabel {
    if (!taskKey) return {};
    const all = readJson<Record<string, WelcomeFieldValuesByLabel>>(WELCOME_FIELD_VALUES_KEY, {});
    const saved = all[taskKey];
    return saved && typeof saved === "object" ? { ...saved } : {};
}

export function saveWelcomeFieldValues(taskKey: string, byLabel: WelcomeFieldValuesByLabel): void {
    if (!taskKey) return;
    const cleaned: WelcomeFieldValuesByLabel = {};
    for (const [label, value] of Object.entries(byLabel)) {
        const v = (value ?? "").trim();
        if (v) cleaned[label] = value;
    }
    const all = readJson<Record<string, WelcomeFieldValuesByLabel>>(WELCOME_FIELD_VALUES_KEY, {});
    // Drop then re-insert so this key becomes the newest (LRU-ish eviction).
    delete all[taskKey];
    if (Object.keys(cleaned).length > 0) {
        all[taskKey] = cleaned;
    }
    // Cap stored tasks to avoid unbounded growth (evict oldest keys first).
    const keys = Object.keys(all);
    if (keys.length > 40) {
        for (const k of keys.slice(0, keys.length - 40)) {
            delete all[k];
        }
    }
    writeJson(WELCOME_FIELD_VALUES_KEY, all);
}

export function loadWelcomeRecentEntries(): WelcomeRecentEntry[] {
    const items = readJson<WelcomeRecentEntry[]>(WELCOME_RECENT_KEY, []);
    if (!Array.isArray(items)) return [];
    return items
        .filter((e) => e && typeof e.key === "string" && typeof e.tabId === "string" && typeof e.textEn === "string")
        .slice(0, WELCOME_RECENT_MAX);
}

/** Push a task to the front of the recent list (deduped). Returns the new list. */
export function recordWelcomeRecent(tabId: string, textEn: string): WelcomeRecentEntry[] {
    const key = welcomePromptKey(tabId, textEn);
    const prev = loadWelcomeRecentEntries().filter((e) => e.key !== key);
    const next: WelcomeRecentEntry[] = [
        { key, tabId, textEn, usedAt: Date.now() },
        ...prev,
    ].slice(0, WELCOME_RECENT_MAX);
    writeJson(WELCOME_RECENT_KEY, next);
    return next;
}

/** Resolve recent entries to live prompt cards (drops deleted/renamed tasks). */
export function resolveWelcomeRecentPrompts(
    entries: WelcomeRecentEntry[] = loadWelcomeRecentEntries(),
): Array<{ tabId: string; prompt: WelcomePrompt; key: string }> {
    const out: Array<{ tabId: string; prompt: WelcomePrompt; key: string }> = [];
    for (const entry of entries) {
        const found = findWelcomePromptByKey(entry.key);
        if (found) {
            out.push({ tabId: found.tabId, prompt: found.prompt, key: entry.key });
        }
    }
    return out;
}

/**
 * Prefer custom templates over recent cards that originated from the same source.
 * Caps how many recent chips we show so the quick-access row stays short.
 */
export function filterWelcomeRecentForQuickAccess(
    recent: Array<{ tabId: string; prompt: WelcomePrompt; key: string }>,
    custom: WelcomeCustomTemplate[],
    maxRecent = 4,
): Array<{ tabId: string; prompt: WelcomePrompt; key: string }> {
    const sourceKeys = new Set(
        custom.map((c) => c.sourceKey).filter((k): k is string => !!k && k.length > 0),
    );
    const out: Array<{ tabId: string; prompt: WelcomePrompt; key: string }> = [];
    for (const item of recent) {
        if (sourceKeys.has(item.key)) continue;
        out.push(item);
        if (out.length >= maxRecent) break;
    }
    return out;
}

export function loadWelcomePreviewOpen(defaultOpen = false): boolean {
    try {
        const raw = localStorage.getItem(WELCOME_PREVIEW_OPEN_KEY);
        if (raw === "1") return true;
        if (raw === "0") return false;
    } catch { /* ignore */ }
    return defaultOpen;
}

export function saveWelcomePreviewOpen(open: boolean): void {
    try {
        localStorage.setItem(WELCOME_PREVIEW_OPEN_KEY, open ? "1" : "0");
    } catch { /* ignore */ }
}

/**
 * Last-used coding environment, kept in localStorage only.
 * Remote password may be stored for local convenience (never intended for cloud/share export).
 */
export const WELCOME_CODING_ENV_KEY = "maclaw:welcome-coding-env";

export type WelcomeStoredCodingEnv = {
    workingDir?: string;
    remote?: {
        host: string;
        port: number;
        user: string;
        workDir: string;
        /** Optional local-only SSH password. */
        password?: string;
    };
};

/** Cap local-stored SSH password length (localStorage safety). */
export const WELCOME_SSH_PASSWORD_MAX_LEN = 512;

/** Clamp SSH port to a valid TCP port; invalid values become 22. */
export function normalizeWelcomeSshPort(port: unknown): number {
    const n = Number(port);
    return Number.isFinite(n) && n > 0 && n < 65536 ? Math.trunc(n) : 22;
}

/** Clamp password for local storage (no interior trim — spaces may be significant). */
export function normalizeWelcomeSshPassword(password: unknown): string {
    if (typeof password !== "string" || !password) return "";
    return password.slice(0, WELCOME_SSH_PASSWORD_MAX_LEN);
}

/** Normalize a coding-env snapshot; drops empty/invalid values. Keeps non-empty password. */
export function normalizeWelcomeStoredCodingEnv(
    raw: unknown,
): WelcomeStoredCodingEnv | undefined {
    if (!raw || typeof raw !== "object") return undefined;
    const src = raw as Record<string, unknown>;
    const out: WelcomeStoredCodingEnv = {};
    if (typeof src.workingDir === "string" && src.workingDir.trim()) {
        out.workingDir = src.workingDir.trim();
    }
    const r = src.remote;
    if (r && typeof r === "object") {
        const remote = r as Record<string, unknown>;
        const host = String(remote.host || "").trim();
        const user = String(remote.user || "").trim();
        const workDir = String(remote.workDir || "").trim();
        const port = normalizeWelcomeSshPort(remote.port);
        const password = normalizeWelcomeSshPassword(remote.password);
        // Require at least one connection field; never store an orphan password alone.
        if (host || user || workDir) {
            out.remote = { host, user, workDir, port };
            // Attach password only when host or user is known (what the password unlocks).
            if (password && (host || user)) {
                out.remote.password = password;
            }
        }
    }
    if (!out.workingDir && !out.remote) return undefined;
    return out;
}

/** Drop password for portable export / cloud sync (localStorage keeps it). */
export function stripCodingEnvPassword(
    env: WelcomeStoredCodingEnv | undefined,
): WelcomeStoredCodingEnv | undefined {
    const n = normalizeWelcomeStoredCodingEnv(env);
    if (!n?.remote?.password) return n;
    const { password: _pw, ...remote } = n.remote;
    return { ...n, remote };
}

/**
 * Merge preferred env over fallback. Password from preferred wins;
 * if preferred remote has no password, reuse fallback password only when
 * host+user match (never attach credentials to a different server).
 */
export function mergeWelcomeStoredCodingEnv(
    preferred?: WelcomeStoredCodingEnv | null,
    fallback?: WelcomeStoredCodingEnv | null,
): WelcomeStoredCodingEnv | undefined {
    const a = normalizeWelcomeStoredCodingEnv(preferred);
    const b = normalizeWelcomeStoredCodingEnv(fallback);
    if (!a && !b) return undefined;
    const workingDir = a?.workingDir || b?.workingDir;
    let remote = a?.remote || b?.remote;
    if (
        a?.remote
        && !a.remote.password
        && b?.remote?.password
        && a.remote.host === b.remote.host
        && a.remote.user === b.remote.user
    ) {
        remote = { ...a.remote, password: b.remote.password };
    }
    return normalizeWelcomeStoredCodingEnv({ workingDir, remote });
}

/**
 * Resolve coding env when saving a custom template.
 * - `input` omitted → keep `previous` (e.g. post-send "save as template" offer)
 * - `input.remote.password` is a string (incl. "") → treat as explicit; "" clears
 * - `input.remote.password` omitted → merge password from previous when host+user match
 */
export function resolveWelcomeCodingEnvForSave(
    input?: WelcomeStoredCodingEnv | null,
    previous?: WelcomeStoredCodingEnv | null,
): WelcomeStoredCodingEnv | undefined {
    if (input == null) {
        return normalizeWelcomeStoredCodingEnv(previous);
    }
    const passwordExplicit = typeof input.remote?.password === "string";
    if (passwordExplicit) {
        // normalize drops empty password → explicit clear.
        return normalizeWelcomeStoredCodingEnv(input);
    }
    return mergeWelcomeStoredCodingEnv(input, previous);
}

export function loadWelcomeCodingEnv(): WelcomeStoredCodingEnv {
    const raw = readJson<WelcomeStoredCodingEnv | null>(WELCOME_CODING_ENV_KEY, null);
    return normalizeWelcomeStoredCodingEnv(raw) || {};
}

export function saveWelcomeCodingEnv(env: WelcomeStoredCodingEnv): void {
    const prev = loadWelcomeCodingEnv();
    const next: WelcomeStoredCodingEnv = { ...prev };
    if (typeof env.workingDir === "string") {
        next.workingDir = env.workingDir.trim() || undefined;
    }
    let vaultWrite: { host: string; user: string; password: string; port: number } | null = null;
    let vaultClear: { host: string; user: string; port: number } | null = null;
    if (env.remote) {
        const hasPasswordField = typeof env.remote.password === "string";
        // Build remote via normalize so host/port/user rules stay single-sourced.
        const normalized = normalizeWelcomeStoredCodingEnv({
            remote: {
                host: env.remote.host,
                port: env.remote.port,
                user: env.remote.user,
                workDir: env.remote.workDir,
                password: hasPasswordField && env.remote.password
                    ? env.remote.password
                    : undefined,
            },
        });
        if (!normalized?.remote) {
            // Invalid/empty remote payload — leave previous remote untouched.
        } else {
            const host = normalized.remote.host;
            const user = normalized.remote.user;
            next.remote = {
                host,
                user,
                workDir: normalized.remote.workDir,
                port: normalized.remote.port,
            };
            if (hasPasswordField && env.remote.password === "") {
                // Explicit clear — drop vault entry after last-used env is written.
                vaultClear = { host, user, port: next.remote.port };
            } else if (normalized.remote.password) {
                next.remote.password = normalized.remote.password;
            } else if (
                prev.remote?.password
                && sameRemoteSSHTarget(
                    prev.remote.host,
                    prev.remote.user,
                    prev.remote.port,
                    host,
                    user,
                    next.remote.port,
                )
            ) {
                // Partial update for the same host+user+port keeps the previous password.
                next.remote.password = prev.remote.password;
            }
            // Dual-write vault so reconnect can recall after last-used env rotates hosts.
            if (next.remote.password) {
                vaultWrite = {
                    host,
                    user,
                    password: next.remote.password,
                    port: next.remote.port,
                };
            }
        }
    }
    writeJson(WELCOME_CODING_ENV_KEY, next);
    // Vault updates after last-used env so a failed vault write never leaves env/vault inverted.
    if (vaultClear) {
        removeRemoteSSHPasswordVaultEntry(vaultClear.host, vaultClear.user, vaultClear.port);
    } else if (vaultWrite) {
        upsertRemoteSSHPasswordVaultEntry(
            vaultWrite.host,
            vaultWrite.user,
            vaultWrite.password,
            vaultWrite.port,
        );
    }
}

// --- Multi-host SSH password vault (local only; reconnect form recall) ---

/** localStorage map of `user@host:port` → password for remote coding reconnect. */
export const REMOTE_SSH_PASSWORD_VAULT_KEY = "maclaw:remote-ssh-passwords";
/** Soft cap so the vault cannot grow without bound across many one-off hosts. */
export const REMOTE_SSH_PASSWORD_VAULT_MAX = 40;

/** Stable vault key for a remote SSH identity (host is lowercased). */
export function remoteSSHPasswordVaultKey(host: string, user: string, port?: number): string {
    const h = String(host || "").trim().toLowerCase();
    const u = String(user || "").trim();
    const p = normalizeWelcomeSshPort(port);
    return `${u}@${h}:${p}`;
}

/** Host is case-insensitive; user is exact; port is normalized (0/invalid → 22). */
function sameRemoteSSHTarget(
    aHost: string,
    aUser: string,
    aPort: number | undefined,
    bHost: string,
    bUser: string,
    bPort: number | undefined,
): boolean {
    return (
        aHost.toLowerCase() === bHost.toLowerCase()
        && aUser === bUser
        && normalizeWelcomeSshPort(aPort) === normalizeWelcomeSshPort(bPort)
    );
}

function readRemoteSSHPasswordVault(): Record<string, string> {
    const raw = readJson<Record<string, unknown> | null>(REMOTE_SSH_PASSWORD_VAULT_KEY, null);
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) return {};
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(raw)) {
        const key = String(k || "").trim();
        const pw = normalizeWelcomeSshPassword(v);
        if (key && pw) out[key] = pw;
    }
    return out;
}

/** Insert/update one vault entry (LRU by re-append). No-op on empty identity/password. */
function upsertRemoteSSHPasswordVaultEntry(
    host: string,
    user: string,
    password: string,
    port?: number,
): void {
    const h = String(host || "").trim();
    const u = String(user || "").trim();
    const pw = normalizeWelcomeSshPassword(password);
    if (!h || !u || !pw) return;
    const key = remoteSSHPasswordVaultKey(h, u, port);
    const map = readRemoteSSHPasswordVault();
    // Move key to "most recent" by re-inserting last (object key order is insertion order).
    if (key in map) delete map[key];
    map[key] = pw;
    const keys = Object.keys(map);
    if (keys.length > REMOTE_SSH_PASSWORD_VAULT_MAX) {
        for (const d of keys.slice(0, keys.length - REMOTE_SSH_PASSWORD_VAULT_MAX)) {
            delete map[d];
        }
    }
    writeJson(REMOTE_SSH_PASSWORD_VAULT_KEY, map);
}

function removeRemoteSSHPasswordVaultEntry(host: string, user: string, port?: number): void {
    const h = String(host || "").trim();
    const u = String(user || "").trim();
    if (!h || !u) return;
    const key = remoteSSHPasswordVaultKey(h, u, port);
    const map = readRemoteSSHPasswordVault();
    if (!(key in map)) return;
    delete map[key];
    writeJson(REMOTE_SSH_PASSWORD_VAULT_KEY, map);
}

/**
 * Load a remembered SSH password for remote coding reconnect.
 * Prefers multi-host vault; falls back to last-used welcome coding env when
 * host+user+port match. Lazy-promotes welcome-env fallback into the vault.
 */
export function loadRemoteSSHPassword(host: string, user: string, port?: number): string {
    const h = String(host || "").trim();
    const u = String(user || "").trim();
    if (!h || !u) return "";
    const p = normalizeWelcomeSshPort(port);
    const fromVault = normalizeWelcomeSshPassword(
        readRemoteSSHPasswordVault()[remoteSSHPasswordVaultKey(h, u, p)],
    );
    if (fromVault) return fromVault;
    const env = loadWelcomeCodingEnv();
    const rh = String(env.remote?.host || "").trim();
    const ru = String(env.remote?.user || "").trim();
    const rp = env.remote?.port;
    // Port must match — never hand a :22 password to a :2222 reconnect.
    if (
        env.remote?.password
        && sameRemoteSSHTarget(rh, ru, rp, h, u, p)
    ) {
        const pw = normalizeWelcomeSshPassword(env.remote.password);
        if (pw) {
            // Promote legacy single-env password into multi-host vault.
            upsertRemoteSSHPasswordVaultEntry(h, u, pw, p);
            return pw;
        }
    }
    return "";
}

/**
 * Remember an SSH password for reconnect (localStorage only).
 * Always writes the multi-host vault; updates last-used welcome env only when
 * identity matches or last-used remote is unset (avoids clobbering another host).
 */
export function saveRemoteSSHPassword(
    host: string,
    user: string,
    password: string,
    port?: number,
    workDir?: string,
): void {
    const h = String(host || "").trim();
    const u = String(user || "").trim();
    const pw = normalizeWelcomeSshPassword(password);
    if (!h || !u || !pw) return;
    const p = normalizeWelcomeSshPort(port);
    upsertRemoteSSHPasswordVaultEntry(h, u, pw, p);

    const prev = loadWelcomeCodingEnv();
    const prevHost = String(prev.remote?.host || "").trim();
    const prevUser = String(prev.remote?.user || "").trim();
    const prevPort = prev.remote?.port;
    // Update last-used only for same target or when last-used remote is empty.
    // Same host+user on a different port still updates last-used (single slot).
    if (
        prev.remote
        && !sameRemoteSSHTarget(prevHost, prevUser, prevPort, h, u, p)
        && !(prevHost.toLowerCase() === h.toLowerCase() && prevUser === u)
    ) {
        // Different host/user is active in last-used env — vault-only is enough.
        return;
    }
    // Direct write of last-used env (skip saveWelcomeCodingEnv to avoid a second vault upsert).
    const next: WelcomeStoredCodingEnv = { ...prev };
    next.remote = {
        host: h,
        user: u,
        port: p,
        workDir: String(workDir || prev.remote?.workDir || "").trim(),
        password: pw,
    };
    writeJson(WELCOME_CODING_ENV_KEY, next);
}

/**
 * Remove a remembered password for one host+user+port.
 * Also strips password from last-used welcome env when the full target matches.
 */
export function clearRemoteSSHPassword(host: string, user: string, port?: number): void {
    const h = String(host || "").trim();
    const u = String(user || "").trim();
    if (!h || !u) return;
    const p = normalizeWelcomeSshPort(port);
    removeRemoteSSHPasswordVaultEntry(h, u, p);
    const prev = loadWelcomeCodingEnv();
    if (
        prev.remote?.password
        && sameRemoteSSHTarget(
            String(prev.remote.host || "").trim(),
            String(prev.remote.user || "").trim(),
            prev.remote.port,
            h,
            u,
            p,
        )
    ) {
        const { password: _drop, ...remote } = prev.remote;
        writeJson(WELCOME_CODING_ENV_KEY, { ...prev, remote });
    }
}

// --- Custom saved templates (user "save as favorite") ---

export const WELCOME_CUSTOM_TEMPLATES_KEY = "maclaw:welcome-custom-templates";
export const WELCOME_CUSTOM_TEMPLATES_MAX = 12;

export type WelcomeCustomTemplate = {
    id: string;
    /** Display title (usually the scenario card title). */
    title: string;
    /** Full prompt body (may still contain [placeholders]). */
    body: string;
    /** Origin scenario key when saved from a built-in card. */
    sourceKey?: string;
    sourceTabId?: string;
    agentMode?: "coding_dev" | "remote_coding_dev";
    /**
     * Coding environment captured when the user saved this as a favorite.
     * Stored in localStorage only (may include SSH password for local convenience).
     */
    codingEnv?: WelcomeStoredCodingEnv;
    createdAt: number;
    usedAt: number;
};

function newCustomTemplateId(): string {
    return `ct-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function loadWelcomeCustomTemplates(): WelcomeCustomTemplate[] {
    const items = readJson<WelcomeCustomTemplate[]>(WELCOME_CUSTOM_TEMPLATES_KEY, []);
    if (!Array.isArray(items)) return [];
    // Preserve array order (manual reorder + "touch moves to front"). Do not re-sort by usedAt.
    return items
        .filter((t) => t && typeof t.id === "string" && typeof t.title === "string" && typeof t.body === "string")
        .map((t) => ({
            id: t.id,
            title: String(t.title).slice(0, 80),
            body: String(t.body).slice(0, 8000),
            sourceKey: typeof t.sourceKey === "string" ? t.sourceKey : undefined,
            sourceTabId: typeof t.sourceTabId === "string" ? t.sourceTabId : undefined,
            agentMode:
                t.agentMode === "coding_dev" || t.agentMode === "remote_coding_dev"
                    ? t.agentMode
                    : undefined,
            codingEnv: normalizeWelcomeStoredCodingEnv(t.codingEnv),
            createdAt: Number(t.createdAt) || 0,
            usedAt: Number(t.usedAt) || 0,
        }))
        .slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
}

export function saveWelcomeCustomTemplate(input: {
    title: string;
    body: string;
    sourceKey?: string;
    sourceTabId?: string;
    agentMode?: "coding_dev" | "remote_coding_dev";
    codingEnv?: WelcomeStoredCodingEnv;
}): { templates: WelcomeCustomTemplate[]; saved: WelcomeCustomTemplate | null } {
    const title = (input.title || "").trim().slice(0, 80);
    const body = (input.body || "").trim().slice(0, 8000);
    if (!title || !body) {
        return { templates: loadWelcomeCustomTemplates(), saved: null };
    }

    const prev = loadWelcomeCustomTemplates();
    // Dedup by identical body (move to front / refresh metadata). Prefer latest codingEnv.
    const prevSame = prev.find((t) => t.body === body);
    const withoutDup = prev.filter((t) => t.body !== body);
    const codingEnv = resolveWelcomeCodingEnvForSave(input.codingEnv, prevSame?.codingEnv);
    const passwordExplicitClear =
        typeof input.codingEnv?.remote?.password === "string"
        && input.codingEnv.remote.password === "";
    const entry: WelcomeCustomTemplate = {
        // Reuse id so open-by-id chips and task keys stay stable across re-saves.
        id: prevSame?.id || newCustomTemplateId(),
        title,
        body,
        sourceKey: input.sourceKey ?? prevSame?.sourceKey,
        sourceTabId: input.sourceTabId ?? prevSame?.sourceTabId,
        agentMode: input.agentMode ?? prevSame?.agentMode,
        codingEnv,
        createdAt: prevSame?.createdAt || Date.now(),
        usedAt: Date.now(),
    };
    const templates = [entry, ...withoutDup].slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
    writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, templates);
    // Also refresh the global last-used env so other coding cards benefit.
    if (codingEnv) {
        if (passwordExplicitClear && codingEnv.remote) {
            // Propagate explicit password clear to the global last-used env.
            saveWelcomeCodingEnv({
                remote: { ...codingEnv.remote, password: "" },
            });
        } else {
            saveWelcomeCodingEnv(codingEnv);
        }
    }
    return { templates, saved: entry };
}

export function touchWelcomeCustomTemplate(id: string): WelcomeCustomTemplate[] {
    const prev = loadWelcomeCustomTemplates();
    const target = prev.find((t) => t.id === id);
    if (!target) return prev;
    // Always move the touched template to the front (stable even when timestamps collide).
    const updated: WelcomeCustomTemplate = { ...target, usedAt: Date.now() };
    const next = [updated, ...prev.filter((t) => t.id !== id)];
    writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, next);
    return next;
}

export function deleteWelcomeCustomTemplate(id: string): WelcomeCustomTemplate[] {
    const next = loadWelcomeCustomTemplates().filter((t) => t.id !== id);
    writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, next);
    return next;
}

/** Rename a custom template title (empty title is ignored). */
export function renameWelcomeCustomTemplate(id: string, title: string): WelcomeCustomTemplate[] {
    const nextTitle = (title || "").trim().slice(0, 80);
    const prev = loadWelcomeCustomTemplates();
    if (!nextTitle) return prev;
    let changed = false;
    const next = prev.map((t) => {
        if (t.id !== id) return t;
        if (t.title === nextTitle) return t;
        changed = true;
        return { ...t, title: nextTitle };
    });
    if (changed) writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, next);
    return next;
}

/**
 * Swap a custom template one step left ("up") or right ("down") in the saved list.
 * Out-of-range moves are no-ops.
 */
export function moveWelcomeCustomTemplate(
    id: string,
    direction: "up" | "down",
): WelcomeCustomTemplate[] {
    const prev = loadWelcomeCustomTemplates();
    const i = prev.findIndex((t) => t.id === id);
    if (i < 0) return prev;
    const j = direction === "up" ? i - 1 : i + 1;
    if (j < 0 || j >= prev.length) return prev;
    const next = prev.slice();
    const tmp = next[i];
    next[i] = next[j];
    next[j] = tmp;
    writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, next);
    return next;
}

// --- Export / import (local JSON backup + optional Hub cloud sync) ---

export const WELCOME_TEMPLATES_EXPORT_KIND = "maclaw-welcome-custom-templates";
/** v1 = templates only; v2+ may include role / recent / last tab. */
export const WELCOME_TEMPLATES_EXPORT_VERSION = 2;

/** Last known Hub welcome-sync revision (for optimistic concurrency on push). */
export const WELCOME_CLOUD_REVISION_KEY = "maclaw:welcome-cloud-revision";

export function loadWelcomeCloudRevision(): string {
    try {
        return localStorage.getItem(WELCOME_CLOUD_REVISION_KEY) || "";
    } catch {
        return "";
    }
}

export function saveWelcomeCloudRevision(revision: string): void {
    try {
        const rev = (revision || "").trim();
        if (!rev) {
            localStorage.removeItem(WELCOME_CLOUD_REVISION_KEY);
        } else {
            localStorage.setItem(WELCOME_CLOUD_REVISION_KEY, rev);
        }
    } catch { /* ignore */ }
}

export const WELCOME_SCENARIO_TAB_KEY = "maclaw:welcome-scenario-tab";

/** Portable export shape (template ids are regenerated on import). */
export type WelcomeTemplatesExportPayload = {
    version: number;
    kind: typeof WELCOME_TEMPLATES_EXPORT_KIND;
    exportedAt: string;
    templates: Array<{
        title: string;
        body: string;
        sourceKey?: string;
        sourceTabId?: string;
        agentMode?: "coding_dev" | "remote_coding_dev";
        /** Coding env; portable/cloud export strips password by default. */
        codingEnv?: WelcomeStoredCodingEnv;
    }>;
    /** Optional v2 full-backup fields */
    userRole?: WelcomeUserRole;
    recent?: Array<{ tabId: string; textEn: string; usedAt?: number }>;
    lastScenarioTab?: string;
};

export type WelcomeTemplatesImportResult = {
    templates: WelcomeCustomTemplate[];
    added: number;
    skipped: number;
    /** Whether role / recent / last tab were applied from the file. */
    restoredExtras: boolean;
    error?: string;
};

export type WelcomeTemplatesImportOptions = {
    mode?: "merge" | "replace";
    /** Restore userRole / recent / lastScenarioTab when present (default true). */
    restoreExtras?: boolean;
};

export type WelcomeTemplatesImportPreviewItem = {
    title: string;
    body: string;
    /** First line / short snippet for UI. */
    snippet: string;
};

export type WelcomeTemplatesImportPreview = {
    mode: "merge" | "replace";
    /** Original file text (pass through to apply). */
    raw: string;
    toAdd: WelcomeTemplatesImportPreviewItem[];
    toSkip: Array<WelcomeTemplatesImportPreviewItem & { reason: "duplicate" | "overflow" }>;
    extras: {
        userRole?: WelcomeUserRole;
        recentCount: number;
        lastScenarioTab?: string;
    };
    /** True when file carries any restore-able extras. */
    hasExtras: boolean;
};

function templateSnippet(body: string, max = 72): string {
    const one = (body || "").replace(/\s+/g, " ").trim();
    return one.length <= max ? one : `${one.slice(0, max)}…`;
}

function toPreviewItem(
    item: WelcomeTemplatesExportPayload["templates"][number],
): WelcomeTemplatesImportPreviewItem {
    return {
        title: item.title,
        body: item.body,
        snippet: templateSnippet(item.body),
    };
}

/**
 * Dry-run import: compute what would be added/skipped without writing storage.
 */
export function previewWelcomeTemplatesImport(
    raw: string,
    mode: "merge" | "replace" = "merge",
    existing: WelcomeCustomTemplate[] = loadWelcomeCustomTemplates(),
): { ok: true; preview: WelcomeTemplatesImportPreview } | { ok: false; error: string } {
    const parsed = parseWelcomeTemplatesImport(raw);
    if (!parsed.ok) return { ok: false, error: parsed.error };

    const toAdd: WelcomeTemplatesImportPreviewItem[] = [];
    const toSkip: Array<WelcomeTemplatesImportPreviewItem & { reason: "duplicate" | "overflow" }> = [];

    if (mode === "replace") {
        const capped = parsed.items.slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
        for (const item of capped) toAdd.push(toPreviewItem(item));
        for (const item of parsed.items.slice(WELCOME_CUSTOM_TEMPLATES_MAX)) {
            toSkip.push({ ...toPreviewItem(item), reason: "overflow" });
        }
    } else {
        const existingBodies = new Set(existing.map((t) => t.body));
        const accepted: WelcomeTemplatesExportPayload["templates"] = [];
        for (const item of parsed.items) {
            if (existingBodies.has(item.body)) {
                toSkip.push({ ...toPreviewItem(item), reason: "duplicate" });
                continue;
            }
            existingBodies.add(item.body);
            accepted.push(item);
        }
        const room = Math.max(0, WELCOME_CUSTOM_TEMPLATES_MAX - existing.length);
        for (const item of accepted.slice(0, room)) toAdd.push(toPreviewItem(item));
        for (const item of accepted.slice(room)) {
            toSkip.push({ ...toPreviewItem(item), reason: "overflow" });
        }
    }

    const extras = {
        userRole: parsed.userRole,
        recentCount: parsed.recent?.length ?? 0,
        lastScenarioTab: parsed.lastScenarioTab,
    };
    const hasExtras = !!(extras.userRole || extras.recentCount > 0 || extras.lastScenarioTab);

    return {
        ok: true,
        preview: {
            mode,
            raw,
            toAdd,
            toSkip,
            extras,
            hasExtras,
        },
    };
}

function normalizeExportRecent(
    entries: WelcomeRecentEntry[],
): NonNullable<WelcomeTemplatesExportPayload["recent"]> {
    return entries
        .filter((e) => e && typeof e.tabId === "string" && typeof e.textEn === "string")
        .slice(0, WELCOME_RECENT_MAX)
        .map((e) => ({
            tabId: e.tabId,
            textEn: e.textEn,
            usedAt: Number(e.usedAt) || 0,
        }));
}

/** Build a JSON-serializable export payload (templates + optional full-backup extras). */
export function buildWelcomeTemplatesExport(
    templates: WelcomeCustomTemplate[] = loadWelcomeCustomTemplates(),
    options?: {
        includeExtras?: boolean;
        userRole?: WelcomeUserRole;
        recent?: WelcomeRecentEntry[];
        lastScenarioTab?: string | null;
        /**
         * When true, include SSH passwords in the export file.
         * Default false so cloud sync / shared JSON never carry secrets.
         * LocalStorage templates still keep passwords independently.
         */
        includePasswords?: boolean;
    },
): WelcomeTemplatesExportPayload {
    const includeExtras = options?.includeExtras !== false;
    const includePasswords = options?.includePasswords === true;
    const payload: WelcomeTemplatesExportPayload = {
        version: WELCOME_TEMPLATES_EXPORT_VERSION,
        kind: WELCOME_TEMPLATES_EXPORT_KIND,
        exportedAt: new Date().toISOString(),
        templates: templates.map((t) => ({
            title: t.title,
            body: t.body,
            sourceKey: t.sourceKey,
            sourceTabId: t.sourceTabId,
            agentMode: t.agentMode,
            codingEnv: includePasswords
                ? t.codingEnv
                : stripCodingEnvPassword(t.codingEnv),
        })),
    };
    if (includeExtras) {
        payload.userRole = options?.userRole ?? loadWelcomeUserRole();
        payload.recent = normalizeExportRecent(options?.recent ?? loadWelcomeRecentEntries());
        try {
            const tab = options?.lastScenarioTab ?? localStorage.getItem(WELCOME_SCENARIO_TAB_KEY);
            if (tab) payload.lastScenarioTab = tab;
            // Drop retired pre-scenario key if still present on this profile.
            localStorage.removeItem("maclaw:welcome-industry-tab");
        } catch { /* ignore */ }
    }
    return payload;
}

export function stringifyWelcomeTemplatesExport(
    templates: WelcomeCustomTemplate[] = loadWelcomeCustomTemplates(),
    options?: Parameters<typeof buildWelcomeTemplatesExport>[1],
): string {
    return `${JSON.stringify(buildWelcomeTemplatesExport(templates, options), null, 2)}\n`;
}

/**
 * Parse and validate an export JSON string.
 * Accepts our payload shape, or a bare array of {title, body}.
 */
export function parseWelcomeTemplatesImport(raw: string): {
    ok: true;
    items: WelcomeTemplatesExportPayload["templates"];
    userRole?: WelcomeUserRole;
    recent?: WelcomeRecentEntry[];
    lastScenarioTab?: string;
} | { ok: false; error: string } {
    let data: unknown;
    try {
        data = JSON.parse(raw);
    } catch {
        return { ok: false, error: "invalid_json" };
    }

    let list: unknown[] = [];
    let userRole: WelcomeUserRole | undefined;
    let recent: WelcomeRecentEntry[] | undefined;
    let lastScenarioTab: string | undefined;

    if (Array.isArray(data)) {
        list = data;
    } else if (data && typeof data === "object") {
        const obj = data as Record<string, unknown>;
        if (obj.kind && obj.kind !== WELCOME_TEMPLATES_EXPORT_KIND) {
            return { ok: false, error: "unknown_kind" };
        }
        if (!Array.isArray(obj.templates)) {
            return { ok: false, error: "missing_templates" };
        }
        list = obj.templates;
        if (isWelcomeUserRole(String(obj.userRole || ""))) {
            userRole = obj.userRole as WelcomeUserRole;
        }
        if (typeof obj.lastScenarioTab === "string" && obj.lastScenarioTab.trim()) {
            lastScenarioTab = obj.lastScenarioTab.trim();
        }
        if (Array.isArray(obj.recent)) {
            const now = Date.now();
            recent = obj.recent
                .map((row, index) => {
                    if (!row || typeof row !== "object") return null;
                    const r = row as Record<string, unknown>;
                    const tabId = String(r.tabId || "").trim();
                    const textEn = String(r.textEn || "").trim();
                    if (!tabId || !textEn) return null;
                    return {
                        key: welcomePromptKey(tabId, textEn),
                        tabId,
                        textEn,
                        usedAt: Number(r.usedAt) || now - index,
                    } as WelcomeRecentEntry;
                })
                .filter((e): e is WelcomeRecentEntry => !!e)
                .slice(0, WELCOME_RECENT_MAX);
        }
    } else {
        return { ok: false, error: "invalid_shape" };
    }

    const items: WelcomeTemplatesExportPayload["templates"] = [];
    for (const row of list) {
        if (!row || typeof row !== "object") continue;
        const r = row as Record<string, unknown>;
        const title = String(r.title || "").trim().slice(0, 80);
        const body = String(r.body || "").trim().slice(0, 8000);
        if (!title || !body) continue;
        const agentMode =
            r.agentMode === "coding_dev" || r.agentMode === "remote_coding_dev"
                ? r.agentMode
                : undefined;
        const codingEnv = normalizeWelcomeStoredCodingEnv(r.codingEnv);
        items.push({
            title,
            body,
            sourceKey: typeof r.sourceKey === "string" ? r.sourceKey : undefined,
            sourceTabId: typeof r.sourceTabId === "string" ? r.sourceTabId : undefined,
            agentMode,
            codingEnv,
        });
    }
    // Allow extras-only restore? Require at least templates for a valid file.
    if (items.length === 0) {
        return { ok: false, error: "empty" };
    }
    return { ok: true, items, userRole, recent, lastScenarioTab };
}

function applyImportedExtras(parsed: {
    userRole?: WelcomeUserRole;
    recent?: WelcomeRecentEntry[];
    lastScenarioTab?: string;
}): boolean {
    let applied = false;
    if (parsed.userRole) {
        saveWelcomeUserRole(parsed.userRole);
        applied = true;
    }
    if (parsed.recent && parsed.recent.length > 0) {
        writeJson(WELCOME_RECENT_KEY, parsed.recent.slice(0, WELCOME_RECENT_MAX));
        applied = true;
    }
    if (parsed.lastScenarioTab) {
        try {
            localStorage.setItem(WELCOME_SCENARIO_TAB_KEY, parsed.lastScenarioTab);
            applied = true;
        } catch { /* ignore */ }
    }
    return applied;
}

/**
 * Merge or replace local templates with imported items.
 * Optionally restores role / recent / last tab from v2+ backups.
 */
export function importWelcomeCustomTemplates(
    raw: string,
    modeOrOptions: "merge" | "replace" | WelcomeTemplatesImportOptions = "merge",
): WelcomeTemplatesImportResult {
    const options: WelcomeTemplatesImportOptions =
        typeof modeOrOptions === "string"
            ? { mode: modeOrOptions, restoreExtras: true }
            : { mode: "merge", restoreExtras: true, ...modeOrOptions };
    const mode = options.mode ?? "merge";
    const restoreExtras = options.restoreExtras !== false;

    const parsed = parseWelcomeTemplatesImport(raw);
    if (!parsed.ok) {
        return {
            templates: loadWelcomeCustomTemplates(),
            added: 0,
            skipped: 0,
            restoredExtras: false,
            error: parsed.error,
        };
    }

    const now = Date.now();
    const incoming: WelcomeCustomTemplate[] = parsed.items.map((item, index) => ({
        id: newCustomTemplateId(),
        title: item.title,
        body: item.body,
        sourceKey: item.sourceKey,
        sourceTabId: item.sourceTabId,
        agentMode: item.agentMode,
        codingEnv: item.codingEnv,
        // Preserve relative order from the file (first in file → front).
        createdAt: now + (parsed.items.length - index),
        usedAt: now + (parsed.items.length - index),
    }));

    let templates: WelcomeCustomTemplate[];
    let added = 0;
    let skipped = 0;

    if (mode === "replace") {
        templates = incoming.slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
        added = templates.length;
        skipped = Math.max(0, incoming.length - templates.length);
        writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, templates);
    } else {
        const prev = loadWelcomeCustomTemplates();
        const existingBodies = new Set(prev.map((t) => t.body));
        const mergedNew: WelcomeCustomTemplate[] = [];
        for (const item of incoming) {
            if (existingBodies.has(item.body)) {
                skipped += 1;
                continue;
            }
            existingBodies.add(item.body);
            mergedNew.push(item);
            added += 1;
        }
        templates = [...mergedNew, ...prev].slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
        const overflow = mergedNew.length + prev.length - templates.length;
        if (overflow > 0) skipped += overflow;
        writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, templates);
    }

    const restoredExtras = restoreExtras
        ? applyImportedExtras({
            userRole: parsed.userRole,
            recent: parsed.recent,
            lastScenarioTab: parsed.lastScenarioTab,
        })
        : false;

    return { templates, added, skipped, restoredExtras };
}

/** Suggested download filename for export. */
export function welcomeTemplatesExportFilename(now = new Date()): string {
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, "0");
    const d = String(now.getDate()).padStart(2, "0");
    return `maclaw-welcome-backup-${y}${m}${d}.json`;
}

/** Convert a custom template into a WelcomePrompt-shaped card for openPrompt. */
export function customTemplateToWelcomePrompt(t: WelcomeCustomTemplate): WelcomePrompt {
    return {
        text: t.title,
        textEn: t.title,
        desc: t.body.slice(0, 60) + (t.body.length > 60 ? "…" : ""),
        descEn: t.body.slice(0, 60) + (t.body.length > 60 ? "…" : ""),
        icon: "spark",
        template: t.body,
        templateEn: t.body,
        agentMode: t.agentMode,
    };
}

/** Normalize prompt body the same way save does (trim + cap). */
export function normalizeWelcomeTemplateBody(body: string): string {
    return (body || "").trim().slice(0, 8000);
}

/** True when this body is already stored as a custom template. */
export function isWelcomeTemplateBodySaved(
    body: string,
    templates: WelcomeCustomTemplate[] = loadWelcomeCustomTemplates(),
): boolean {
    const normalized = normalizeWelcomeTemplateBody(body);
    if (!normalized) return false;
    return templates.some((t) => t.body === normalized);
}

/**
 * Whether we should surface a post-send "save as template" offer.
 * Skip empty, short, already-saved, or coding-agent payloads.
 */
export function shouldOfferWelcomeTemplateSave(input: {
    body: string;
    title?: string;
    agentMode?: string | null;
    templates?: WelcomeCustomTemplate[];
}): boolean {
    if (input.agentMode === "coding_dev" || input.agentMode === "remote_coding_dev") {
        return false;
    }
    const body = normalizeWelcomeTemplateBody(input.body);
    if (body.length < 24) return false;
    if (!(input.title || "").trim()) return false;
    return !isWelcomeTemplateBodySaved(body, input.templates);
}

/** Payload for the post-send save offer banner. */
export type WelcomeTemplateSaveOffer = {
    title: string;
    body: string;
};
