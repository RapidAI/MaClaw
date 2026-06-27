import type { CSSProperties } from "react";
import { colors } from "./styles";
import { localizeHubServiceReason } from "../../utils/hubServiceI18n";

export interface LLMProvider {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol?: string; // "openai" (default) or "anthropic"
    context_length?: number; // max context tokens (0 = default 128k)
    is_custom?: boolean;
    auth_type?: string;
    agent_type?: string; // "openclaw" (default) or "claude_code"
    supports_vision?: boolean; // whether the model supports image input
    wire_api?: string; // "chat" (default), "responses", or "responses-ws"
}

export const NONE_PROVIDER = "__none__";
export const HUB_SERVICE_PROVIDER_NAME = "MaClaw\u5b98\u65b9"; // Must match Go hubServiceProviderName.
export const LLM_CONFIG_LOAD_TIMEOUT_MS = 5000;

/** Known OpenAI-compatible providers for quick-fill in custom provider config. */
export const KNOWN_OPENAI_ENDPOINTS: { name: string; url: string; model: string; context_length?: number; protocol?: string; agent_type?: string; wire_api?: string }[] = [
    { name: "OpenAI Official", url: "https://api.openai.com/v1", model: "gpt-5.4", context_length: 128000 },
    { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat", context_length: 128000 },
    { name: "\u667a\u8c31\u9f99\u867e", url: "https://open.bigmodel.cn/api/coding/paas/v4", model: "glm-5.1", context_length: 180000 },
    { name: "\u667a\u8c31\u7f16\u7a0b", url: "https://open.bigmodel.cn/api/anthropic", model: "glm-5.1", context_length: 180000, protocol: "anthropic", agent_type: "claude code 2.0" },
    { name: "Kimi (\u6708\u4e4b\u6697\u9762)", url: "https://api.kimi.com/coding/v1", model: "kimi-k2-thinking", context_length: 128000 },
    { name: "\u8baf\u98de\u661f\u8fb0", url: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", model: "astron-code-latest", context_length: 128000 },
    { name: "Doubao (\u8c46\u5305)", url: "https://ark.cn-beijing.volces.com/api/coding", model: "doubao-seed-code-preview-latest", context_length: 128000 },
    { name: "\u706b\u5c71\u5f15\u64ce Agent Plan", url: "https://ark.cn-beijing.volces.com/api/plan/v3", model: "glm-5.2", context_length: 128000, protocol: "openai", wire_api: "responses" },
    { name: "MiniMax", url: "https://api.minimaxi.com/v1", model: "MiniMax-M2.7", context_length: 128000 },
    { name: "\u817e\u8baf\u4e91", url: "https://api.lkeap.cloud.tencent.com/coding/v3", model: "glm-5", context_length: 128000 },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", model: "grok-3", context_length: 131072 },
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
