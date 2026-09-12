// Re-export from unified i18n module for backward compatibility.
// New code should import from '../../i18n' directly.
export { localizeText } from '../../i18n';
import { localizeText } from '../../i18n';

/** English label for the fixed main AI assistant tab / panel chrome. */
export const LOCAL_ASSISTANT_TITLE_EN = "Default Task";
/** Chinese (Hans/Hant) label for the fixed main AI assistant tab / panel chrome. */
export const LOCAL_ASSISTANT_TITLE_ZH = "默认任务";

const OFFICIAL_SERVICE_MESSAGES: Array<[needle: string, en: string, zhHans: string, zhHant: string]> = [
    ["MaClaw 官方周期限流：当前周期额度已用尽", "MaClaw official service is period-limited: current-period credits are exhausted. Please try again later.", "MaClaw 官方周期限流：当前周期额度已用尽。", "MaClaw 官方週期限流：目前週期額度已用盡。"],
    ["MaClaw 官方额度已用尽", "MaClaw official credits are exhausted. Redeem more credits or switch to another provider.", "MaClaw 官方额度已用尽：请兑换额度或切换其他模型提供方。", "MaClaw 官方額度已用盡：請兌換額度或切換其他模型提供方。"],
    ["MaClaw 官方授权尚未生效", "MaClaw official authorization is not active yet. Please try again later.", "MaClaw 官方授权尚未生效，请稍后再试。", "MaClaw 官方授權尚未生效，請稍後再試。"],
    ["MaClaw 官方授权已过期", "MaClaw official authorization has expired. Redeem a new grant or switch to another provider.", "MaClaw 官方授权已过期：请兑换新的授权额度或切换其他模型提供方。", "MaClaw 官方授權已過期：請兌換新的授權額度或切換其他模型提供方。"],
    ["MaClaw 官方需要有效额度", "MaClaw official service requires active credits. Redeem credits or switch to another provider.", "MaClaw 官方需要有效额度：请兑换额度或切换其他模型提供方。", "MaClaw 官方需要有效額度：請兌換額度或切換其他模型提供方。"],
    ["MaClaw 官方服务商暂不可用", "MaClaw official provider is temporarily unavailable. Refresh Hub service status and try again.", "MaClaw 官方服务商暂不可用：请刷新 Hub 服务状态后重试。", "MaClaw 官方服務商暫不可用：請重新整理 Hub 服務狀態後重試。"],
];

/**
 * Localized title for the main AI assistant surface (local tab + panel chrome).
 * Uses normalizeLang so en-US / zh-CN / zh-TW resolve correctly.
 */
export function localAssistantTabTitle(lang?: string | null): string {
    return localizeText(lang, LOCAL_ASSISTANT_TITLE_EN, LOCAL_ASSISTANT_TITLE_ZH, LOCAL_ASSISTANT_TITLE_ZH);
}

/**
 * Localize backend errors shown in the assistant conversation.
 *
 * The Hub/LLM service returns these messages in English, even when the UI is
 * configured for Chinese. Keep the numeric values from the response while
 * translating the surrounding text so an error bubble follows the rest of
 * the assistant UI language.
 */
export function localizeAIAssistantError(error: unknown, lang?: string | null): string {
    const raw = error instanceof Error
        ? error.message.trim()
        : typeof error === "string"
            ? error.trim()
            : error && typeof error === "object" && typeof (error as { message?: unknown }).message === "string"
                ? String((error as { message: string }).message).trim()
                : String(error ?? "").trim();
    if (!raw) return "";

    // Hub currently emits a few user-facing service denials in Simplified
    // Chinese. They can reach an English UI directly (or wrapped by the LLM
    // failure prefix), so translate the complete actionable message here.
    const officialPeriodLimited = raw.match(/MaClaw\s*官方周期限流：当前周期额度已用尽，约\s*(.+?)\s*后恢复(?:；请刷新 Hub 服务状态)?。?/);
    if (officialPeriodLimited) {
        const retry = localizeHubRetry(officialPeriodLimited[1], lang);
        return localizeText(
            lang,
            `MaClaw official service is period-limited: current-period credits are exhausted; service resumes in about ${retry}.`,
            `MaClaw 官方周期限流：当前周期额度已用尽，约 ${officialPeriodLimited[1]} 后恢复。`,
            `MaClaw 官方週期限流：目前週期額度已用盡，約 ${localizeHubRetry(officialPeriodLimited[1], "zh-Hant")} 後恢復。`,
        );
    }
    for (const [needle, en, zhHans, zhHant] of OFFICIAL_SERVICE_MESSAGES) {
        if (raw.includes(needle)) return localizeText(lang, en, zhHans, zhHant);
    }

    // Normalize both backend wording variants so switching the UI language
    // also works for errors that were already localized by the service.
    const chineseInsufficientCredits = raw.match(
        /(?:额度不足|額度不足)[：:，,]?\s*(?:需要|需)\s*([\d.,]+)\s*(?:credits?|额度|額度)?[，,]\s*(?:当前可用|目前可用)\s*([\d.,]+)\s*(?:credits?|额度|額度)?/i,
    );
    if (chineseInsufficientCredits) {
        const [, need, available] = chineseInsufficientCredits;
        return localizeText(
            lang,
            `LLM call failed: insufficient credits for this request: need ${need} credits, available ${available}`,
            `LLM 调用失败：本次请求额度不足，需要 ${need} Credits，当前可用 ${available} Credits。`,
            `LLM 調用失敗：本次請求額度不足，需要 ${need} Credits，目前可用 ${available} Credits。`,
        );
    }

    const insufficientCredits = raw.match(
        /^(?:Error:\s*)?(?:LLM\s+(?:call\s+failed|调用失败|調用失敗))\s*[:：]\s*insufficient\s+credits?(?:\s+for\s+this\s+request)?\s*:\s*need\s+([\d.,]+)\s+credits?\s*,\s*available\s+([\d.,]+)(?:\s+credits?)?\s*[.!]?$/i,
    );
    if (insufficientCredits) {
        const [, need, available] = insufficientCredits;
        return localizeText(
            lang,
            `LLM call failed: insufficient credits for this request: need ${need} credits, available ${available}`,
            `LLM 调用失败：本次请求额度不足，需要 ${need} Credits，当前可用 ${available} Credits。`,
            `LLM 調用失敗：本次請求額度不足，需要 ${need} Credits，目前可用 ${available} Credits。`,
        );
    }

    const exhausted = raw.match(
        /^(?:Error:\s*)?(?:LLM\s+(?:call\s+failed|调用失败|調用失敗))\s*[:：]\s*(current\s+period\s+credit\s+limit\s+is\s+exhausted|(?:selected\s+model\s+)?grant\s+credits?\s+are\s+exhausted)\s*$/i,
    );
    if (exhausted) {
        const isPeriodLimit = exhausted[1].toLowerCase().startsWith("current period");
        return localizeText(
            lang,
            raw.replace(/^Error:\s*/i, ""),
            isPeriodLimit
                ? "LLM 调用失败：当前周期额度已用尽。"
                : "LLM 调用失败：当前授权额度已用尽。",
            isPeriodLimit
                ? "LLM 調用失敗：目前週期額度已用盡。"
                : "LLM 調用失敗：目前授權額度已用盡。",
        );
    }

    // The service may omit the detailed need/available amounts for some
    // providers. Localize that shorter form as well.
    const insufficient = raw.match(
        /^(?:Error:\s*)?(?:LLM\s+(?:call\s+failed|调用失败|調用失敗))\s*[:：]\s*insufficient\s+credits?\s*$/i,
    );
    if (insufficient) {
        return localizeText(
            lang,
            raw.replace(/^Error:\s*/i, ""),
            "LLM 调用失败：额度不足。",
            "LLM 調用失敗：額度不足。",
        );
    }

    // Keep the leading failure label in the selected language for other LLM
    // failures too (timeouts, HTTP failures, provider errors, and so on).
    // Details are retained verbatim unless they have a stable translation.
    const generic = raw.match(/^(?:Error:\s*)?LLM\s+(?:call\s+failed|调用失败|調用失敗)\s*[:：]\s*(.+)$/i);
    if (generic) {
        const detail = generic[1].trim();
        const normalizedDetail = detail.toLowerCase();
        let enDetail = detail;
        let zhHansDetail = detail;
        let zhHantDetail = detail;
        if (normalizedDetail.includes("timeout") || normalizedDetail.includes("timed out") || normalizedDetail.includes("deadline exceeded")) {
            zhHansDetail = "请求超时";
            zhHantDetail = "請求逾時";
        } else if (normalizedDetail.includes("service unavailable")) {
            zhHansDetail = detail.replace(/service unavailable/gi, "服务暂不可用");
            zhHantDetail = detail.replace(/service unavailable/gi, "服務暫不可用");
        } else if (normalizedDetail.includes("rate exceeded") || normalizedDetail.includes("rate limit")) {
            zhHansDetail = detail.replace(/(?:user request )?rate (?:exceeded|limit)/gi, "请求频率超限，请稍后重试");
            zhHantDetail = detail.replace(/(?:user request )?rate (?:exceeded|limit)/gi, "請求頻率超限，請稍後重試");
        } else if (normalizedDetail.includes("no active model service entitlement")) {
            zhHansDetail = detail.replace(/no active model service entitlement/gi, "当前没有可用的模型服务授权");
            zhHantDetail = detail.replace(/no active model service entitlement/gi, "目前沒有可用的模型服務授權");
        } else if (normalizedDetail.includes("current period credit limit is exhausted")) {
            zhHansDetail = detail.replace(/current period credit limit is exhausted/gi, "当前周期额度已用尽");
            zhHantDetail = detail.replace(/current period credit limit is exhausted/gi, "目前週期額度已用盡");
        } else if (normalizedDetail.includes("selected model grant credits are exhausted")) {
            zhHansDetail = detail.replace(/selected model grant credits are exhausted/gi, "当前授权额度已用尽");
            zhHantDetail = detail.replace(/selected model grant credits are exhausted/gi, "目前授權額度已用盡");
        } else if (detail.includes("当前周期额度已用尽") || detail.includes("目前週期額度已用盡")) {
            enDetail = "the current period credit limit is exhausted";
            zhHansDetail = "当前周期额度已用尽";
            zhHantDetail = "目前週期額度已用盡";
        } else if (detail.includes("额度已用尽") || detail.includes("額度已用盡")) {
            enDetail = "credits are exhausted";
            zhHansDetail = "额度已用尽";
            zhHantDetail = "額度已用盡";
        } else if (detail.includes("额度不足") || detail.includes("額度不足")) {
            enDetail = "insufficient credits";
            zhHansDetail = "额度不足";
            zhHantDetail = "額度不足";
        }
        return localizeText(lang, `LLM call failed: ${enDetail}`, `LLM 调用失败：${zhHansDetail}`, `LLM 調用失敗：${zhHantDetail}`);
    }

    return raw;
}

function localizeHubRetry(value: string, lang?: string | null): string {
    const raw = String(value || "").trim();
    const english = raw
        .replace(/(\d+(?:\.\d+)?)\s*小时/g, (_, count: string) => `${count} ${count === "1" ? "hour" : "hours"}`)
        .replace(/(\d+(?:\.\d+)?)\s*分钟/g, (_, count: string) => `${count} ${count === "1" ? "minute" : "minutes"}`)
        .replace(/(\d+(?:\.\d+)?)\s*秒/g, (_, count: string) => `${count} ${count === "1" ? "second" : "seconds"}`);
    const zhHant = raw.replace(/小时/g, "小時").replace(/分钟/g, "分鐘").replace(/约/g, "約");
    return localizeText(lang, english, raw, zhHant);
}
