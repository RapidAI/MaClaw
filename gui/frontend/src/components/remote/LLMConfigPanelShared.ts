import type { CSSProperties } from "react";
import { colors } from "./styles";
import { localizeHubServiceReason } from "../../utils/hubServiceI18n";

export interface LLMProvider {
	/** Stable backend identifier; provider name is display-only. */
    id?: string;
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string; // "openai" (default) or "anthropic"
    context_length?: number; // max context tokens (0 = default 128k)
    max_output_tokens?: number; // max output tokens per request (0 = system default 8192/16384)
    is_custom?: boolean;
	/** Set after this provider's current configuration passes Test & Save. */
	connection_test_passed?: boolean;
    auth_type?: string;
    refresh_token?: string;
    token_expires_at?: number;
    oauth_access_token?: string;
    agent_type?: string; // "openclaw" (default) or "claude_code"
    /** Set when this provider was imported from a local coding agent. */
    import_source?: string;
    /** Provider-specific model IDs, when discovered from the service. */
    models?: string[];
    /** Model IDs whose image-input capability has been confirmed. */
    vision_models?: string[];
    supports_vision?: boolean; // whether the model supports image input
    wire_api?: string; // "chat" (default), "responses", or "responses-ws"
}

export function isOpenCodeProvider(provider: Pick<LLMProvider, "name" | "agent_type"> | null | undefined) {
    if (!provider) return false;
    return provider.name === "OpenCode" || (provider.agent_type || "").trim() === "OpenCode";
}

/** OpenAI organization costs require an Admin API key, not ChatGPT/Codex or other OAuth tokens. */
export function canQueryOpenAIOrganizationCosts(
    provider?: Pick<LLMProvider, "name" | "url" | "key" | "auth_type"> | null,
): boolean {
    if (!provider) return false;
    const key = (provider.key || "").trim();
    if (!key.startsWith("sk-admin-")) return false;
    const authType = (provider.auth_type || "").trim().toLowerCase();
    if (authType === "oauth") return false;
    const url = (provider.url || "").trim();
    const urlLower = url.toLowerCase();
    if (urlLower.includes("chatgpt.com")) return false;
    const name = (provider.name || "").trim().toLowerCase();
    if (name === "openai" || name === "openai official") return true;
    try {
        const parsed = url.includes("://") ? url : `https://${url}`;
        return new URL(parsed).hostname.toLowerCase() === "api.openai.com";
    } catch {
        return urlLower.includes("api.openai.com");
    }
}

export const NONE_PROVIDER = "__none__";
export const HUB_SERVICE_PROVIDER_NAME = "MaClaw\u5b98\u65b9"; // Must match Go hubServiceProviderName.
export const LLM_CONFIG_LOAD_TIMEOUT_MS = 5000;

/** Known OpenAI-compatible providers for quick-fill in custom provider config. */
export const KNOWN_OPENAI_ENDPOINTS: { name: string; url: string; model: string; context_length?: number; protocol?: string; auth_type?: string; agent_type?: string; wire_api?: string }[] = [
    { name: "OpenAI Official", url: "https://api.openai.com/v1", model: "gpt-5.4", context_length: 128000 },
    { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat", context_length: 128000 },
    { name: "\u667a\u8c31\u9f99\u867e", url: "https://open.bigmodel.cn/api/coding/paas/v4", model: "glm-5.1", context_length: 180000 },
    { name: "\u667a\u8c31\u7f16\u7a0b", url: "https://open.bigmodel.cn/api/anthropic", model: "glm-5.3", context_length: 400000, protocol: "anthropic", agent_type: "claude code 2.0" },
    { name: "Kimi (\u6708\u4e4b\u6697\u9762)", url: "https://api.kimi.com/coding/v1", model: "kimi-k2-thinking", context_length: 128000 },
    { name: "\u8baf\u98de\u661f\u8fb0", url: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", model: "astron-code-latest", context_length: 128000 },
    { name: "Doubao (\u8c46\u5305)", url: "https://ark.cn-beijing.volces.com/api/coding", model: "doubao-seed-code-preview-latest", context_length: 128000 },
    { name: "\u706b\u5c71\u5f15\u64ce Agent Plan", url: "https://ark.cn-beijing.volces.com/api/plan/v3", model: "glm-5.2", context_length: 128000, protocol: "openai", wire_api: "responses" },
    { name: "MiniMax", url: "https://api.minimaxi.com/v1", model: "MiniMax-M2.7", context_length: 128000 },
    { name: "\u817e\u8baf\u4e91", url: "https://api.lkeap.cloud.tencent.com/coding/v3", model: "glm-5", context_length: 128000 },
    { name: "xAI-Grok", url: "https://api.x.ai/v1", model: "grok-4.6", context_length: 400000, auth_type: "oauth", wire_api: "responses" },
    { name: "OpenCode", url: "https://opencode.ai/zen/v1", model: "big-pickle", context_length: 128000, agent_type: "OpenCode" },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "openai/gpt-4o", context_length: 128000 },
    { name: "Together AI", url: "https://api.together.xyz/v1", model: "meta-llama/Llama-3-70b-chat-hf", context_length: 128000 },
    { name: "Groq", url: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile", context_length: 128000 },
    { name: "Perplexity", url: "https://api.perplexity.ai", model: "sonar-pro", context_length: 128000 },
    { name: "\u963f\u91cc\u4e91 (\u767e\u70bc)", url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen3.5-plus", context_length: 128000 },
    { name: "ChatFire", url: "https://api.chatfire.cn/v1", model: "gpt-4o", context_length: 128000 },
];

/* Hoisted style objects (avoid re-creation per render). */
export const inputStyle: CSSProperties = {
    width: "100%", padding: "7px 10px", fontSize: "0.8rem",
    border: `1px solid ${colors.border}`, borderRadius: 4,
    background: colors.surface, color: colors.text, boxSizing: "border-box",
};
export const labelStyle: CSSProperties = {
    fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block",
};
export const readonlyStyle: CSSProperties = {
    ...inputStyle, background: colors.bg, color: colors.textMuted, cursor: "default",
};

const secretLikeFieldRe = /(api[_-]?key|authorization|bearer|token|secret|password)\s*[:=]\s*\S+/gi;

function redactProviderTestError(text: string): string {
    return String(text || "").replace(secretLikeFieldRe, "[redacted]").trim();
}

function normalizeProviderTestErrorText(raw: string): string {
    let text = String(raw || "").trim();
    for (;;) {
        const next = text
            .replace(/^Error invoking '[^']+':\s*/i, "")
            .replace(/^Error:\s*/i, "")
            .trim();
        if (next === text) return text;
        text = next;
    }
}

function extractJSONObject(text: string): { type?: string; message?: string; error?: { type?: string; message?: string } } | null {
    const start = text.indexOf("{");
    if (start < 0) return null;
    let depth = 0;
    let inString = false;
    let escaped = false;
    for (let i = start; i < text.length; i++) {
        const ch = text[i];
        if (inString) {
            if (escaped) escaped = false;
            else if (ch === "\\") escaped = true;
            else if (ch === "\"") inString = false;
            continue;
        }
        if (ch === "\"") {
            inString = true;
            continue;
        }
        if (ch === "{") depth++;
        else if (ch === "}") {
            depth--;
            if (depth === 0) {
                try {
                    return JSON.parse(text.slice(start, i + 1)) as { type?: string; message?: string; error?: { type?: string; message?: string } };
                } catch {
                    return null;
                }
            }
        }
    }
    return null;
}

export function isProviderTestCancelMessage(raw: string): boolean {
    const text = normalizeProviderTestErrorText(raw).toLowerCase();
    return text === "cancelled" || text === "canceled" || text === "已取消";
}

export function formatProviderTestError(raw: string, t: (en: string, zhHans: string, zhHant?: string) => string): string {
    const text = normalizeProviderTestErrorText(raw);
    if (!text) return text;
    const lower = text.toLowerCase();
    if (lower.includes("free promotion has ended") || lower.includes("免费活动已结束")) {
        return t(
            "This model is no longer free. Choose another model, then Test & Save.",
            "该免费模型活动已结束。请更换其他模型后再检测并保存。",
        );
    }
    const parsed = extractJSONObject(text);
    if (parsed) {
        const type = String(parsed.type || parsed.error?.type || "");
        const message = redactProviderTestError(String(parsed.message || parsed.error?.message || ""));
        if (type.toLowerCase() === "modelerror" && message) {
            return t(
                `This model is unavailable: ${message}. Choose another model, then Test & Save.`,
                `当前模型不可用：${message}。请更换其他模型后再检测并保存。`,
            );
        }
        if (message) return message;
    }
    return redactProviderTestError(text);
}

export function formatProviderTestErrorOrFallback(raw: string, t: (en: string, zhHans: string, zhHant?: string) => string): string {
    return formatProviderTestError(raw, t) || t(
        "Connection failed. Check the API URL, key, and model.",
        "连接失败，请检查 API 地址、密钥和模型。",
    );
}

export function withTimeout<T>(promise: Promise<T>, timeoutMs: number, label: string): Promise<T> {
    return new Promise<T>((resolve, reject) => {
        const timer = window.setTimeout(() => {
            reject(new Error(`${label} timeout`));
        }, timeoutMs);
        promise.then(
            value => {
                window.clearTimeout(timer);
                resolve(value);
            },
            error => {
                window.clearTimeout(timer);
                reject(error);
            },
        );
    });
}
export function hubCreditGrants(status: HubLLMServiceStatus | null): HubLLMActiveGrant[] {
    const grants = (status?.credit_grants?.length ? status.credit_grants : status?.active_grants) || [];
    return grants.filter(grant => String(grant.source || "").trim().toLowerCase() !== "hubcenter_compute");
}

function hubRetrySeconds(grant?: HubLLMActiveGrant): number {
    if (!grant) return 0;
    let seconds = Number(grant.retry_after_seconds || 0);
    if ((!Number.isFinite(seconds) || seconds <= 0) && grant.retry_after_at) {
        const retryAt = new Date(grant.retry_after_at).getTime();
        if (Number.isFinite(retryAt)) seconds = Math.ceil((retryAt - Date.now()) / 1000);
    }
    return Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
}

function formatHubRetryDuration(seconds: number, lang?: string): string {
    const safeSeconds = Math.max(0, Math.ceil(Number(seconds || 0)));
    const zh = lang === "zh-Hans" || lang === "zh-Hant";
    if (safeSeconds < 60) return zh ? safeSeconds + " \u79d2" : safeSeconds + "s";
    const minutes = Math.ceil(safeSeconds / 60);
    if (minutes < 60) return zh ? minutes + " \u5206\u949f" : minutes + "m";
    const hours = Math.ceil(minutes / 60);
    if (hours < 24) return zh ? hours + " \u5c0f\u65f6" : hours + "h";
    const days = Math.ceil(hours / 24);
    return zh ? days + " \u5929" : days + "d";
}

export function hubOfficialStatus(status: HubLLMServiceStatus | null, lang: string | undefined, t: (en: string, zhHans: string, zhHant?: string) => string) {
    const grants = hubCreditGrants(status);
    const limited = grants.find(grant => String(grant.status || "").toLowerCase() === "period_limited");
    const queued = grants.find(grant => String(grant.status || "").toLowerCase() === "queued");
    const exhausted = grants.find(grant => String(grant.status || "").toLowerCase() === "exhausted");
    const expired = grants.find(grant => String(grant.status || "").toLowerCase() === "expired");
    const zh = lang === "zh-Hans" || lang === "zh-Hant";
    if (status?.active) {
        return { kind: "active" as const, label: t("Enabled", "\u5df2\u542f\u7528"), detail: "" };
    }
    if (limited) {
        const retry = hubRetrySeconds(limited);
        const retryText = retry > 0 ? formatHubRetryDuration(retry, lang) : "";
        return {
            kind: "limited" as const,
            label: t("Period limited", "\u5468\u671f\u9650\u989d"),
            detail: retryText
                ? (zh ? "\u5f53\u524d\u5468\u671f\u989d\u5ea6\u5df2\u7528\u5c3d\uff0c\u7ea6 " + retryText + " \u540e\u6062\u590d\u3002\u82e5\u5b98\u65b9\u8fd8\u6709\u5176\u5b83\u53ef\u7528\u901a\u9053\u4f1a\u81ea\u52a8\u5207\u6362\uff1b\u4e0d\u4f1a\u9759\u9ed8\u5207\u5230\u4f60\u7684\u79c1\u6709\u670d\u52a1\u5546\u3002" : "Current period quota is exhausted; recovers in about " + retryText + ". Routing switches automatically only to another available official route; it will not silently switch to your private provider.")
                : t("Current period quota is exhausted. Routing switches automatically only to another available official route; it will not silently switch to your private provider.", "\u5f53\u524d\u5468\u671f\u989d\u5ea6\u5df2\u7528\u5c3d\u3002\u82e5\u5b98\u65b9\u8fd8\u6709\u5176\u5b83\u53ef\u7528\u901a\u9053\u4f1a\u81ea\u52a8\u5207\u6362\uff1b\u4e0d\u4f1a\u9759\u9ed8\u5207\u5230\u4f60\u7684\u79c1\u6709\u670d\u52a1\u5546\u3002", "\u76ee\u524d\u9031\u671f\u984d\u5ea6\u5df2\u7528\u76e1\u3002\u82e5\u5b98\u65b9\u9084\u6709\u5176\u4ed6\u53ef\u7528\u901a\u9053\u6703\u81ea\u52d5\u5207\u63db\uff1b\u4e0d\u6703\u975c\u9ed8\u5207\u5230\u4f60\u7684\u79c1\u6709\u670d\u52d9\u5546\u3002"),
        };
    }
    if (queued) {
        const retry = hubRetrySeconds(queued);
        const retryText = retry > 0 ? formatHubRetryDuration(retry, lang) : "";
        return { kind: "queued" as const, label: t("Not active yet", "\u672a\u751f\u6548"), detail: retryText ? (zh ? "\u6388\u6743\u7ea6 " + retryText + " \u540e\u751f\u6548\u3002" : "Authorization starts in about " + retryText + ".") : t("Authorization is not active yet.", "\u6388\u6743\u5c1a\u672a\u751f\u6548\u3002", "\u6388\u6b0a\u5c1a\u672a\u751f\u6548\u3002") };
    }
    if (exhausted) {
        return { kind: "exhausted" as const, label: t("Credits exhausted", "\u989d\u5ea6\u5df2\u7528\u5c3d"), detail: t("Official credits are exhausted. You can redeem more credits or switch to another provider.", "\u5b98\u65b9\u989d\u5ea6\u5df2\u7528\u5c3d\u3002\u53ef\u4ee5\u5151\u6362\u66f4\u591a\u989d\u5ea6\uff0c\u6216\u5207\u6362\u5230\u5176\u5b83\u670d\u52a1\u5546\u3002", "\u5b98\u65b9\u984d\u5ea6\u5df2\u7528\u76e1\u3002\u53ef\u4ee5\u514c\u63db\u66f4\u591a\u984d\u5ea6\uff0c\u6216\u5207\u63db\u5230\u5176\u4ed6\u670d\u52d9\u5546\u3002") };
    }
    if (expired) {
        return { kind: "expired" as const, label: t("Grant expired", "\u6388\u6743\u5df2\u8fc7\u671f", "\u6388\u6b0a\u5df2\u904e\u671f"), detail: t("Official authorization has expired. You can redeem a new grant or switch to another provider.", "\u5b98\u65b9\u6388\u6743\u5df2\u8fc7\u671f\u3002\u53ef\u4ee5\u5151\u6362\u65b0\u7684\u6388\u6743\uff0c\u6216\u5207\u6362\u5230\u5176\u5b83\u670d\u52a1\u5546\u3002", "\u5b98\u65b9\u6388\u6b0a\u5df2\u904e\u671f\u3002\u53ef\u4ee5\u514c\u63db\u65b0\u7684\u6388\u6b0a\uff0c\u6216\u5207\u63db\u5230\u5176\u4ed6\u670d\u52d9\u5546\u3002") };
    }
    return { kind: "inactive" as const, label: t("Unavailable", "\u4e0d\u53ef\u7528"), detail: (status?.inactive_reasons || []).map(reason => localizeHubServiceReason(reason, lang)).filter(Boolean).join("; ") };
}


export interface HubLLMActiveGrant {
    id?: string;
    service_group_id?: string;
    source?: string;
    card_id?: string;
    card_order_id?: string;
    starts_at?: string;
    expires_at?: string;
    permanent?: boolean;
    rolling_five_hour?: boolean;
    active?: boolean;
    effective?: boolean;
    status?: string;
    status_reason?: string;
    credits_total?: number;
    credits_used?: number;
    credits_remaining?: number;
    credits_available?: number;
    retry_after_seconds?: number;
    retry_after_at?: string;
    period_limits?: {
        five_hour?: number;
        daily?: number;
        weekly?: number;
        monthly?: number;
    };
    period_usage?: {
        five_hour?: { window_start?: string; window_end?: string; credits_used?: number; rolling?: boolean };
        daily?: { window_start?: string; window_end?: string; credits_used?: number };
        weekly?: { window_start?: string; window_end?: string; credits_used?: number };
        monthly?: { window_start?: string; window_end?: string; credits_used?: number };
    };
}

export interface HubLLMServiceStatus {
    active?: boolean;
    skip_llm_config?: boolean;
    hub_llm_base_url?: string;
    available_models?: string[];
    default_model?: string;
    active_grants?: HubLLMActiveGrant[];
    credit_grants?: HubLLMActiveGrant[];
    inactive_reasons?: string[];
}
