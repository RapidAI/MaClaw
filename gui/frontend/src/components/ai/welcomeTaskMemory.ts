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

/** Last-used coding environment (password never stored). */
export const WELCOME_CODING_ENV_KEY = "maclaw:welcome-coding-env";

export type WelcomeStoredCodingEnv = {
    workingDir?: string;
    remote?: {
        host: string;
        port: number;
        user: string;
        workDir: string;
    };
};

export function loadWelcomeCodingEnv(): WelcomeStoredCodingEnv {
    const raw = readJson<WelcomeStoredCodingEnv | null>(WELCOME_CODING_ENV_KEY, null);
    if (!raw || typeof raw !== "object") return {};
    const out: WelcomeStoredCodingEnv = {};
    if (typeof raw.workingDir === "string" && raw.workingDir.trim()) {
        out.workingDir = raw.workingDir.trim();
    }
    const r = raw.remote;
    if (r && typeof r === "object") {
        const host = String(r.host || "").trim();
        const user = String(r.user || "").trim();
        const workDir = String(r.workDir || "").trim();
        const port = Number(r.port) || 22;
        if (host || user || workDir) {
            out.remote = {
                host,
                user,
                workDir,
                port: port > 0 && port < 65536 ? port : 22,
            };
        }
    }
    return out;
}

export function saveWelcomeCodingEnv(env: WelcomeStoredCodingEnv): void {
    const prev = loadWelcomeCodingEnv();
    const next: WelcomeStoredCodingEnv = { ...prev };
    if (typeof env.workingDir === "string") {
        next.workingDir = env.workingDir.trim() || undefined;
    }
    if (env.remote) {
        next.remote = {
            host: env.remote.host.trim(),
            user: env.remote.user.trim(),
            workDir: env.remote.workDir.trim(),
            port: env.remote.port > 0 && env.remote.port < 65536 ? env.remote.port : 22,
        };
    }
    writeJson(WELCOME_CODING_ENV_KEY, next);
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
}): { templates: WelcomeCustomTemplate[]; saved: WelcomeCustomTemplate | null } {
    const title = (input.title || "").trim().slice(0, 80);
    const body = (input.body || "").trim().slice(0, 8000);
    if (!title || !body) {
        return { templates: loadWelcomeCustomTemplates(), saved: null };
    }

    const prev = loadWelcomeCustomTemplates();
    // Dedup by identical body (move to front / refresh metadata).
    const withoutDup = prev.filter((t) => t.body !== body);
    const entry: WelcomeCustomTemplate = {
        id: newCustomTemplateId(),
        title,
        body,
        sourceKey: input.sourceKey,
        sourceTabId: input.sourceTabId,
        agentMode: input.agentMode,
        createdAt: Date.now(),
        usedAt: Date.now(),
    };
    const templates = [entry, ...withoutDup].slice(0, WELCOME_CUSTOM_TEMPLATES_MAX);
    writeJson(WELCOME_CUSTOM_TEMPLATES_KEY, templates);
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
/** Older industry-tab key still read when packing a full backup. */
const WELCOME_SCENARIO_TAB_LEGACY_KEY = "maclaw:welcome-industry-tab";

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
    },
): WelcomeTemplatesExportPayload {
    const includeExtras = options?.includeExtras !== false;
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
        })),
    };
    if (includeExtras) {
        payload.userRole = options?.userRole ?? loadWelcomeUserRole();
        payload.recent = normalizeExportRecent(options?.recent ?? loadWelcomeRecentEntries());
        try {
            const tab =
                options?.lastScenarioTab
                ?? localStorage.getItem(WELCOME_SCENARIO_TAB_KEY)
                ?? localStorage.getItem(WELCOME_SCENARIO_TAB_LEGACY_KEY);
            if (tab) payload.lastScenarioTab = tab;
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
        items.push({
            title,
            body,
            sourceKey: typeof r.sourceKey === "string" ? r.sourceKey : undefined,
            sourceTabId: typeof r.sourceTabId === "string" ? r.sourceTabId : undefined,
            agentMode,
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
