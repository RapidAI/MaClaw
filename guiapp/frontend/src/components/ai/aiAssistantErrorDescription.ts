import { localizeText } from "../../i18n";

/** Structured breakdown of a localized assistant error for alert-style rendering. */
export interface AIAssistantErrorDescription {
    /** Short headline shown in bold (e.g. "额度不足"). */
    title: string;
    /** Supporting detail (numbers, HTTP status, provider message). */
    detail?: string;
    /** Optional actionable hint (e.g. "请稍后重试"). */
    hint?: string;
}

/**
 * Break a localized assistant error (see localizeAIAssistantError) into a
 * title/detail/hint triple so the UI can render a compact alert instead of a
 * long raw sentence. Unknown errors keep their full text as the title.
 */
export function describeAIAssistantError(localized: string, lang?: string | null): AIAssistantErrorDescription {
    const raw = String(localized || "").trim();
    if (!raw) return { title: "" };

    const creditHint = () => localizeText(
        lang,
        "Redeem more credits or switch providers, then try again.",
        "请兑换额度或切换模型提供方后重试。",
        "請兌換額度或切換模型提供方後重試。",
    );
    const retryHint = () => localizeText(lang, "Please try again later.", "请稍后重试。", "請稍後重試。");

    const creditNumbers = raw.match(
        /^(?:LLM call failed|LLM 调用失败|LLM 調用失敗)[:：]\s*(?:insufficient credits for this request:\s*need\s*([\d.,]+)\s*credits?\s*,\s*available\s*([\d.,]+)(?:\s*credits?)?(?:\s*\(\s*([\d.,]+)\s+held\s+by\s+in-flight\s+requests\s*\))?|(?:本次请求额度不足，需要|本次請求額度不足，需要)\s*([\d.,]+)\s*Credits?[，,]\s*(?:当前可用|目前可用)\s*([\d.,]+)\s*Credits?(?:\s*[，,]\s*其中\s*([\d.,]+)\s*Credits?\s*(?:被在途请求冻结|被在途請求凍結))?)[.。]?$/i,
    );
    if (creditNumbers) {
        const need = creditNumbers[1] ?? creditNumbers[4];
        const available = creditNumbers[2] ?? creditNumbers[5];
        const held = creditNumbers[3] ?? creditNumbers[6] ?? '';
        const heldEn = held ? ` (${held} held by in-flight requests)` : '';
        const heldZh = held ? `，其中 ${held} 被在途请求冻结` : '';
        return {
            title: localizeText(lang, "Insufficient credits", "额度不足", "額度不足"),
            detail: localizeText(
                lang,
                `This request needs ${need} credits, but only ${available} are available${heldEn}.`,
                `本次请求需要 ${need} Credits，当前可用 ${available} Credits${heldZh}。`,
                `本次請求需要 ${need} Credits，目前可用 ${available} Credits${held ? `，其中 ${held} 被在途請求凍結` : ''}。`,
            ),
            hint: creditHint(),
        };
    }

    const exhausted = raw.match(
        /^(?:LLM call failed|LLM 调用失败|LLM 調用失敗)[:：]\s*(current period credit limit is exhausted|selected model grant credits are exhausted|insufficient credits|当前周期额度已用尽。?|当前授权额度已用尽。?|目前週期額度已用盡。?|目前授權額度已用盡。?|额度不足。?|額度不足。?)$/i,
    );
    if (exhausted) {
        const phrase = exhausted[1].replace(/[.。]$/, "");
        const period = /period|周期|週期/i.test(phrase);
        const grant = /grant|授权|授權/i.test(phrase);
        return {
            title: localizeText(
                lang,
                period ? "Current-period credits are exhausted" : grant ? "Grant credits are exhausted" : "Insufficient credits",
                period ? "当前周期额度已用尽" : grant ? "当前授权额度已用尽" : "额度不足",
                period ? "目前週期額度已用盡" : grant ? "目前授權額度已用盡" : "額度不足",
            ),
            hint: creditHint(),
        };
    }

    // Hub official-service denials already read as "headline：actionable advice".
    // Split them so the advice doesn't render as one heavy bold sentence.
    const maclawSplit = raw.match(/^(MaClaw[^\s:：]*(?:\s+[^\s:：]+){0,5})\s*[:：]\s*(.+)$/);
    if (maclawSplit) {
        return { title: maclawSplit[1].trim(), detail: maclawSplit[2].trim() };
    }

    const llmSplit = raw.match(/^(LLM call failed|LLM 调用失败|LLM 調用失敗)[:：]\s*(.+)$/i);
    if (llmSplit) {
        const detail = llmSplit[2].trim();
        const bareTimeout = /^(timeout|timed out|请求超时|請求逾時)[.。]?$/i.test(detail);
        const isTimeout = bareTimeout || /timeout|timed out|deadline exceeded|请求超时|請求逾時/i.test(detail);
        return {
            title: isTimeout
                ? localizeText(lang, "Request timed out", "请求超时", "請求逾時")
                : llmSplit[1],
            detail: isTimeout && bareTimeout ? undefined : detail,
            hint: isTimeout ? retryHint() : undefined,
        };
    }

    return { title: raw };
}
