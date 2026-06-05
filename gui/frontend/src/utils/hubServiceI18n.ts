export function localizeByLang(lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans): string {
    if (lang === "zh-Hans") return zhHans;
    if (lang === "zh-Hant") return zhHant;
    return en;
}

const HUB_SERVICE_REASON_TRANSLATIONS: Array<[string, string, string, string?]> = [
    ["current period credit limit is exhausted", "Current period credit limit is exhausted.", "当前周期额度已用尽。", "目前週期額度已用盡。"],
    ["grant credits are exhausted", "Grant credits are exhausted.", "授权额度已用尽。", "授權額度已用盡。"],
    ["grant is not active yet", "Grant is not active yet.", "授权尚未生效。", "授權尚未生效。"],
    ["grant has expired", "Grant has expired.", "授权已过期。", "授權已過期。"],
    ["hub access token is missing", "Hub access token is missing. Reconnect Hub and try again.", "Hub 访问令牌缺失，请重新连接 Hub 后重试。", "Hub 存取權杖缺失，請重新連線 Hub 後重試。"],
];

const HUB_SERVICE_REDEEM_ERROR_TRANSLATIONS: Array<[string, string, string, string?]> = [
    ["redeem code already used", "This redeem code has already been used.", "该兑换码已被使用。", "該兌換碼已被使用。"],
    ["redeem code must contain only letters or digits", "Redeem code can contain only letters or digits.", "兑换码只能包含字母或数字。", "兌換碼只能包含字母或數字。"],
    ["invalid redeem code", "The redeem code is invalid. Check the code and try again.", "兑换码无效，请核对后重试。", "兌換碼無效，請核對後重試。"],
    ["redeem code is required", "Please enter a redeem code.", "请输入兑换码。", "請輸入兌換碼。"],
    ["email is required", "Account email is missing. Complete Hub activation first.", "账号邮箱缺失，请先完成 Hub 激活。", "帳號信箱缺失，請先完成 Hub 啟用。"],
    ["redeem code has no service groups configured", "This code has no service group configured. Contact the administrator.", "该兑换码未配置服务组，请联系管理员处理。", "該兌換碼未配置服務組，請聯絡管理員處理。"],
    ["redeem code has no valid service groups configured", "This code has no valid service group configured. Contact the administrator.", "该兑换码没有有效服务组，请联系管理员处理。", "該兌換碼沒有有效服務組，請聯絡管理員處理。"],
    ["hub url is not configured", "Hub URL is not configured. Complete Hub activation first.", "Hub 地址未配置，请先完成 Hub 激活。", "Hub 位址未配置，請先完成 Hub 啟用。"],
    ["hub access token is missing", "Hub access token is missing. Reconnect Hub and try again.", "Hub 访问令牌缺失，请重新连接 Hub 后重试。", "Hub 存取權杖缺失，請重新連線 Hub 後重試。"],
];

const HUB_SERVICE_STATUS_ERROR_TRANSLATIONS: Array<[string, string, string, string?]> = [
    ["viewer token expired", "Hub authorization has expired. Reconnect Hub and try again.", "Hub 授权已过期，请重新连接 Hub 后重试。", "Hub 授權已過期，請重新連線 Hub 後重試。"],
    ["invalid tenant", "Hub tenant information is invalid. Complete Hub activation again.", "Hub 租户信息无效，请重新完成 Hub 激活。", "Hub 租戶資訊無效，請重新完成 Hub 啟用。"],
    ["usage report failed", "Hub usage report is temporarily unavailable. Refresh later.", "Hub 用量报告暂不可用，请稍后刷新。", "Hub 用量報告暫不可用，請稍後重新整理。"],
];

export function localizeHubServiceReason(reason: unknown, lang?: string): string {
    const raw = String(reason || "").trim().replace(/^Error:\s*/i, "");
    const normalized = raw.toLowerCase();
    for (const [needle, en, zhHans, zhHant] of HUB_SERVICE_REASON_TRANSLATIONS) {
        if (normalized.includes(needle)) return localizeByLang(lang, en, zhHans, zhHant);
    }
    return raw;
}

export function localizeHubServiceRedeemError(error: unknown, lang?: string): string {
    const raw = String(error || "").trim().replace(/^Error:\s*/i, "");
    const normalized = raw.toLowerCase();
    for (const [needle, en, zhHans, zhHant] of HUB_SERVICE_REDEEM_ERROR_TRANSLATIONS) {
        if (normalized.includes(needle)) return localizeByLang(lang, en, zhHans, zhHant);
    }
    const lengthError = normalized.match(/redeem code must be (\d+) letters or digits/);
    if (lengthError) {
        const count = lengthError[1];
        return localizeByLang(
            lang,
            `Redeem code must be ${count} letters or digits.`,
            `兑换码必须是 ${count} 位字母或数字。`,
            `兌換碼必須是 ${count} 位字母或數字。`,
        );
    }
    const redeemFailed = raw.match(/^redeem failed:\s*(.+)$/i);
    if (redeemFailed) {
        const detail = redeemFailed[1];
        return localizeByLang(lang, `Redeem failed: ${detail}`, `兑换失败：${detail}`, `兌換失敗：${detail}`);
    }
    return raw || localizeByLang(lang, "Redeem failed. Please try again later.", "兑换失败，请稍后重试。", "兌換失敗，請稍後重試。");
}

export function localizeHubServiceStatusError(error: unknown, lang?: string): string {
    const raw = String(error || "").trim().replace(/^Error:\s*/i, "");
    if (!raw) return localizeByLang(lang, "Service status unavailable.", "服务状态暂不可用。", "服務狀態暫不可用。");
    const reason = localizeHubServiceReason(raw, lang);
    if (reason !== raw) return reason;
    const normalized = raw.toLowerCase();
    for (const [needle, en, zhHans, zhHant] of HUB_SERVICE_STATUS_ERROR_TRANSLATIONS) {
        if (normalized.includes(needle)) return localizeByLang(lang, en, zhHans, zhHant);
    }
    const queryFailed = raw.match(/^(?:account status query failed|status query failed):\s*(.+)$/i);
    if (queryFailed) {
        const detail = queryFailed[1].trim();
        return localizeByLang(lang, `Service status query failed: ${detail}`, `服务状态查询失败：${detail}`, `服務狀態查詢失敗：${detail}`);
    }
    return raw;
}
