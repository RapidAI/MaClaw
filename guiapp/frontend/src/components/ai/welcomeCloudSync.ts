/**
 * Pure helpers for welcome-page Hub cloud sync (status parsing + error classification).
 * Kept free of React / Wails so unit tests can cover UX branching.
 */

/** localStorage: "1" = auto-upload local welcome backup after changes. */
export const WELCOME_CLOUD_AUTO_SYNC_KEY = "maclaw:welcome-cloud-auto-sync";

/** Debounce for auto-upload after local template/role/recent edits. */
export const WELCOME_CLOUD_AUTO_SYNC_DEBOUNCE_MS = 2_500;

export function loadWelcomeCloudAutoSync(): boolean {
    try {
        const v = localStorage.getItem(WELCOME_CLOUD_AUTO_SYNC_KEY);
        // Default off — opt-in so first-time users don't silently overwrite cloud.
        return v === "1" || v === "true";
    } catch {
        return false;
    }
}

export function saveWelcomeCloudAutoSync(enabled: boolean): void {
    try {
        if (enabled) localStorage.setItem(WELCOME_CLOUD_AUTO_SYNC_KEY, "1");
        else localStorage.removeItem(WELCOME_CLOUD_AUTO_SYNC_KEY);
    } catch { /* ignore */ }
}

/**
 * Whether auto-upload should run for this local/cloud snapshot.
 * Never auto-wipe a rich cloud backup with an empty local template list.
 */
export function shouldAutoPushWelcomeCloud(input: {
    autoSync: boolean;
    loggedIn: boolean;
    unsupported: boolean;
    busy: boolean;
    localTemplateCount: number;
    cloudHasDocument: boolean;
    cloudTemplateCount: number;
}): boolean {
    if (!input.autoSync || !input.loggedIn || input.unsupported || input.busy) return false;
    if (
        input.localTemplateCount === 0
        && input.cloudHasDocument
        && input.cloudTemplateCount > 0
    ) {
        return false;
    }
    return true;
}

/**
 * Stable fingerprint of local welcome data for auto-sync de-dupe.
 * Avoids re-uploading the same payload when unrelated state (e.g. login flag) flips.
 */
export function welcomeCloudLocalFingerprint(input: {
    templates: Array<{
        id?: string;
        title?: string;
        body?: string;
        agentMode?: string;
        sourceKey?: string;
        sourceTabId?: string;
        /** Non-secret coding env (password must not be fingerprinted for cloud). */
        codingEnv?: {
            workingDir?: string;
            remote?: { host?: string; port?: number; user?: string; workDir?: string; password?: string };
        };
    }>;
    userRole?: string;
    recent: Array<{ tabId?: string; textEn?: string; usedAt?: number }>;
}): string {
    const templates = (input.templates || []).map((t) => {
        const remote = t.codingEnv?.remote;
        // Fingerprint non-secret env only so host/workdir changes re-sync,
        // without embedding passwords in the fingerprint payload.
        const codingEnv = t.codingEnv
            ? {
                workingDir: t.codingEnv.workingDir || "",
                remote: remote
                    ? {
                        host: remote.host || "",
                        port: Number(remote.port) || 22,
                        user: remote.user || "",
                        workDir: remote.workDir || "",
                    }
                    : undefined,
            }
            : undefined;
        return {
            id: t.id || "",
            title: t.title || "",
            body: t.body || "",
            agentMode: t.agentMode || "",
            sourceKey: t.sourceKey || "",
            sourceTabId: t.sourceTabId || "",
            codingEnv,
        };
    });
    const recent = (input.recent || []).map((r) => ({
        tabId: r.tabId || "",
        textEn: r.textEn || "",
        usedAt: Number(r.usedAt) || 0,
    }));
    try {
        return JSON.stringify({
            role: input.userRole || "auto",
            templates,
            recent,
        });
    } catch {
        return `${templates.length}:${input.userRole || ""}:${recent.length}`;
    }
}

export type WelcomeCloudSyncStatus = {
    loggedIn: boolean;
    hasDocument: boolean;
    revision: string;
    templateCount: number;
    updatedAt: string;
    /** Hub is reachable/auth ok but missing welcome-sync routes. */
    unsupported: boolean;
    error: string;
};

export type WelcomeCloudErrorKind =
    | "conflict"
    | "empty"
    | "login"
    | "unsupported"
    | "invalid"
    | "generic";

/** Extract a human-readable message from Wails / Promise rejection values. */
export function welcomeCloudErrorMessage(err: unknown): string {
    if (err == null) return "";
    if (typeof err === "string") return err.trim();
    if (err instanceof Error) return (err.message || String(err)).trim();
    if (typeof err === "object") {
        const o = err as Record<string, unknown>;
        const msg = o.message ?? o.Message ?? o.error ?? o.Error;
        if (typeof msg === "string" && msg.trim()) return msg.trim();
        try {
            return JSON.stringify(err).slice(0, 200);
        } catch {
            return String(err);
        }
    }
    return String(err).trim();
}

export function classifyWelcomeCloudError(err: unknown): WelcomeCloudErrorKind {
    const msg = welcomeCloudErrorMessage(err).toLowerCase();
    if (!msg) return "generic";
    if (/cloud conflict|revision mismatch|updated on another device/.test(msg)) return "conflict";
    if (/no cloud welcome|welcome_sync_not_found/.test(msg)) return "empty";
    if (/upgrade hub|does not support welcome sync/.test(msg)) return "unsupported";
    if (/hub login|login required|viewer token|hub_url missing/.test(msg)) return "login";
    if (/invalid|not valid json|cloud document is invalid/.test(msg)) return "invalid";
    if (/not found/.test(msg) && /welcome|cloud|document/.test(msg)) return "empty";
    return "generic";
}

/** Normalize Hub/Wails status objects (snake_case or camelCase). */
export function parseWelcomeCloudStatus(
    status: Record<string, unknown> | null | undefined,
): WelcomeCloudSyncStatus {
    if (!status) {
        return {
            loggedIn: false,
            hasDocument: false,
            revision: "",
            templateCount: 0,
            updatedAt: "",
            unsupported: false,
            error: "",
        };
    }
    const error = String(status.error ?? status.Error ?? "").trim();
    const loggedIn = status.logged_in === true || status.loggedIn === true;
    const hasDocument = status.has_document === true || status.hasDocument === true;
    const revision = String(status.revision ?? "").trim();
    const templateCount = Number(status.template_count ?? status.templateCount ?? 0) || 0;
    const updatedAt = String(status.updated_at ?? status.updatedAt ?? "");
    const unsupported =
        /does not support|upgrade hub/i.test(error)
        || status.unsupported === true;
    return {
        // Keep loggedIn true when unsupported so UI can still show the upgrade chip.
        loggedIn,
        hasDocument: unsupported ? false : hasDocument,
        revision: unsupported ? "" : revision,
        templateCount: unsupported ? 0 : templateCount,
        updatedAt: unsupported ? "" : updatedAt,
        unsupported,
        error,
    };
}

export function welcomeCloudStatusLabel(
    status: Pick<WelcomeCloudSyncStatus, "loggedIn" | "hasDocument" | "templateCount" | "unsupported">,
    isZh: boolean,
): string | null {
    if (status.unsupported) {
        return isZh ? "需升级 Hub" : "Upgrade Hub";
    }
    if (!status.loggedIn) return null;
    if (status.hasDocument) {
        return isZh ? `云端 ${status.templateCount}` : `Cloud ${status.templateCount}`;
    }
    return isZh ? "云端空" : "Cloud empty";
}

export function welcomeCloudPayloadText(result: Record<string, unknown> | null | undefined): string {
    if (!result) return "";
    return String(result.payload_json ?? result.payloadJson ?? result.PayloadJSON ?? "").trim();
}

/** Short, human-readable updated-at for chip tooltips. */
export function formatWelcomeCloudUpdatedAt(raw: string, isZh: boolean): string {
    const s = (raw || "").trim();
    if (!s) return "";
    const d = new Date(s);
    if (Number.isNaN(d.getTime())) return s;
    try {
        return d.toLocaleString(isZh ? "zh-CN" : "en-US", {
            year: "numeric",
            month: "2-digit",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
        });
    } catch {
        return s;
    }
}

/**
 * Whether a just-pulled merge should push from storage (not React state).
 * Always true for the multi-step conflict resolver after a successful pull.
 */
export function shouldPushWelcomeFromStorageAfterPull(pulledOk: boolean): boolean {
    return pulledOk === true;
}

/** Multi-step conflict resolve phases for UI progress. */
export type WelcomeCloudConflictPhase = "idle" | "pulling" | "pushing";

export function welcomeCloudConflictPhaseLabel(
    phase: WelcomeCloudConflictPhase,
    isZh: boolean,
): string {
    switch (phase) {
        case "pulling":
            return isZh ? "① 正在拉取并合并云端…" : "① Pulling & merging cloud…";
        case "pushing":
            return isZh ? "② 正在上传合并结果…" : "② Uploading merged result…";
        default:
            return isZh
                ? "检测到云端冲突：建议先合并云端变更，再上传本地结果。"
                : "Cloud conflict: merge cloud changes, then upload the combined result.";
    }
}

export function welcomeCloudConflictResolveButtonLabel(
    phase: WelcomeCloudConflictPhase,
    isZh: boolean,
): string {
    if (phase === "pulling" || phase === "pushing") {
        return isZh ? "处理中…" : "Working…";
    }
    return isZh ? "拉取合并后再传" : "Merge then upload";
}

/** User-facing note for a classified cloud error (push/pull). */
export function welcomeCloudUserNote(
    kind: WelcomeCloudErrorKind,
    isZh: boolean,
    rawMessage = "",
    action: "push" | "pull" = "pull",
    /** When true, conflict text is for background auto-sync (no force-overwrite hint). */
    quiet = false,
): string {
    switch (kind) {
        case "conflict":
            if (quiet) {
                return isZh
                    ? "自动同步暂停：云端有更新。可用下方「拉取合并后再传」一键解决。"
                    : "Auto-sync paused: cloud has newer data. Use “Merge then upload” below.";
            }
            return isZh
                ? "云端有更新。可「拉取合并后再传」，或再次点击上传强制覆盖。"
                : "Cloud has newer data. Use “Merge then upload”, or push again to force overwrite.";
        case "empty":
            return isZh ? "云端暂无引导页备份" : "No cloud welcome backup yet";
        case "unsupported":
            return isZh ? "当前 Hub 版本不支持引导页云同步" : "This Hub build lacks welcome cloud sync";
        case "login":
            return isZh ? "请先登录 Hub 账号" : "Please sign in to Hub first";
        case "invalid":
            return isZh ? "云端数据无效" : "Cloud document is invalid";
        default: {
            const msg = (rawMessage || "").slice(0, 80);
            if (action === "push") {
                return isZh ? `上传失败：${msg || "未知错误"}` : `Upload failed: ${msg || "unknown error"}`;
            }
            return isZh ? `拉取失败：${msg || "未知错误"}` : `Pull failed: ${msg || "unknown error"}`;
        }
    }
}
