import { localizeText, normalizeLang } from '../i18n/langSelect';

const mnByEnglish: Record<string, string> = {
    'Current period credit limit is exhausted.': 'Одоогийн мөчлөгийн кредит дууссан.',
    'Grant credits are exhausted.': 'Олголтын кредит дууссан.',
    'Grant is not active yet.': 'Олголт хараахан идэвхжээгүй.',
    'Grant has expired.': 'Олголтын хугацаа дууссан.',
    'Hub access token is missing. Reconnect Hub and try again.': 'Hub хандалтын Token алга. Hub-д дахин холбогдож үзнэ үү.',
    'This redeem code has already been used.': 'Энэ идэвхжүүлэх кодыг аль хэдийн ашигласан.',
    'Redeem code can contain only letters or digits.': 'Идэвхжүүлэх код зөвхөн үсэг эсвэл цифр агуулна.',
    'The redeem code is invalid. Check the code and try again.': 'Идэвхжүүлэх код буруу. Кодоо шалгаад дахин оролдоно уу.',
    'Please enter a redeem code.': 'Идэвхжүүлэх код оруулна уу.',
    'Account email is missing. Complete Hub activation first.': 'Дансны имэйл алга. Эхлээд Hub идэвхжүүлэлтийг дуусгана уу.',
    'This code has no service group configured. Contact the administrator.': 'Энэ кодод үйлчилгээний бүлэг тохируулаагүй. Админтай холбогдоно уу.',
    'This code has no valid service group configured. Contact the administrator.': 'Энэ кодод хүчинтэй үйлчилгээний бүлэг тохируулаагүй. Админтай холбогдоно уу.',
    'Hub URL is not configured. Complete Hub activation first.': 'Hub URL тохируулаагүй. Эхлээд Hub идэвхжүүлэлтийг дуусгана уу.',
    'This reset voucher has already been used.': 'Энэ дахин тохируулах эрхийг аль хэдийн ашигласан.',
    'This reset voucher does not belong to the current account.': 'Энэ дахин тохируулах эрх одоогийн дансных биш.',
    'This reset voucher has expired.': 'Энэ дахин тохируулах эрхийн хугацаа дууссан.',
    'This reset voucher can be used only while the new-user limit card is still active.': 'Энэ эрхийг зөвхөн шинэ хэрэглэгчийн хязгаарын карт идэвхтэй үед ашиглана.',
    'This reset voucher was not found.': 'Энэ дахин тохируулах эрх олдсонгүй.',
    'Reset voucher could not be used. Please try again later.': 'Дахин тохируулах эрхийг ашиглаж чадсангүй. Дараа дахин оролдоно уу.',
    'Hub authorization has expired. Reconnect Hub and try again.': 'Hub зөвшөөрлийн хугацаа дууссан. Hub-д дахин холбогдож үзнэ үү.',
    'Hub tenant information is invalid. Complete Hub activation again.': 'Hub түрээслэгчийн мэдээлэл буруу. Hub идэвхжүүлэлтийг дахин дуусгана уу.',
    'Hub usage report is temporarily unavailable. Refresh later.': 'Hub хэрэглээний тайлан түр боломжгүй. Дараа шинэчилнэ үү.',
    'Redeem failed. Please try again later.': 'Идэвхжүүлэлт амжилтгүй. Дараа дахин оролдоно уу.',
    'Service status unavailable.': 'Үйлчилгээний төлөв боломжгүй.',
};

export function localizeByLang(lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans): string {
    if (normalizeLang(lang) === 'mn' && mnByEnglish[en]) return mnByEnglish[en];
    return localizeText(lang, en, zhHans, zhHant);
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
    ["voucher already used", "This reset voucher has already been used.", "该重置券已使用。", "該重置券已使用。"],
    ["voucher does not belong to this user", "This reset voucher does not belong to the current account.", "该重置券不属于当前账号。", "該重置券不屬於目前帳號。"],
    ["voucher expired", "This reset voucher has expired.", "该重置券已过期。", "該重置券已過期。"],
    ["no active new-user limit card", "This reset voucher can be used only while the new-user limit card is still active.", "当前没有有效的新用户限额卡，无法使用重置券。", "目前沒有有效的新用戶限額卡，無法使用重置券。"],
    ["voucher not found", "This reset voucher was not found.", "未找到该重置券。", "找不到該重置券。"],
    ["voucher redemption failed", "Reset voucher could not be used. Please try again later.", "重置券使用失败，请稍后重试。", "重置券使用失敗，請稍後重試。"],
];

const HUB_SERVICE_STATUS_ERROR_TRANSLATIONS: Array<[string, string, string, string?]> = [
    ["viewer token expired", "Hub authorization has expired. Reconnect Hub and try again.", "Hub 授权已过期，请重新连接 Hub 后重试。", "Hub 授權已過期，請重新連線 Hub 後重試。"],
    ["invalid tenant", "Hub tenant information is invalid. Complete Hub activation again.", "Hub 租户信息无效，请重新完成 Hub 激活。", "Hub 租戶資訊無效，請重新完成 Hub 啟用。"],
    ["usage report failed", "Hub usage report is temporarily unavailable. Refresh later.", "Hub 用量报告暂不可用，请稍后刷新。", "Hub 用量報告暫不可用，請稍後重新整理。"],
];

export function localizeHubServiceReason(reason: unknown, lang?: string): string {
    const raw = String(reason || "").trim().replace(/^Error:\s*/i, "");
    const normalized = raw.toLowerCase();
    const insufficientCredits = normalized.match(/insufficient\s+credits?(?:\s+for\s+this\s+request)?\s*:\s*need\s+([\d.,]+)\s+credits?\s*,\s*available\s+([\d.,]+)(?:\s+credits?)?(?:\s*\(\s*([\d.,]+)\s+held\s+by\s+in-flight\s+requests\s*\))?/i);
    if (insufficientCredits) {
        const [, need, available, held] = insufficientCredits;
        const heldEn = held ? ` (${held} held by in-flight requests)` : '';
        const heldZh = held ? `，其中 ${held} Credits 被在途请求冻结` : '';
        const heldZhHant = held ? `，其中 ${held} Credits 被在途請求凍結` : '';
        if (normalizeLang(lang) === 'mn') {
            const heldMn = held ? ` (${held} Credits замд байгаа хүсэлтэд түгжигдсэн)` : '';
            return `Энэ хүсэлтэд кредит хүрэлцэхгүй: ${need} кредит хэрэгтэй, боломжтой ${available} кредит${heldMn}.`;
        }
        return localizeByLang(
            lang,
            `Insufficient credits for this request: need ${need} credits, available ${available} credits${heldEn}.`,
            `本次请求额度不足：需要 ${need} Credits，当前可用 ${available} Credits${heldZh}。`,
            `本次請求額度不足：需要 ${need} Credits，目前可用 ${available} Credits${heldZhHant}。`,
        );
    }
    for (const [needle, en, zhHans, zhHant] of HUB_SERVICE_REASON_TRANSLATIONS) {
        if (normalized.includes(needle)) return localizeByLang(lang, en, zhHans, zhHant);
    }
    return raw;
}

function errorMessage(error: unknown): string {
    if (error == null) return "";
    if (typeof error === "string") return error.trim();
    if (error instanceof Error) return error.message.trim();
    if (typeof error === "object") {
        const message = (error as { message?: unknown }).message;
        if (typeof message === "string" && message.trim()) return message.trim();
    }
    const raw = String(error).trim();
    return raw === "[object Object]" ? "" : raw;
}

export function localizeHubServiceRedeemError(error: unknown, lang?: string, emptyFallback?: string): string {
    const raw = errorMessage(error).replace(/^Error:\s*/i, "");
    const normalized = raw.toLowerCase();
    for (const [needle, en, zhHans, zhHant] of HUB_SERVICE_REDEEM_ERROR_TRANSLATIONS) {
        if (normalized.includes(needle)) return localizeByLang(lang, en, zhHans, zhHant);
    }
    const lengthError = normalized.match(/redeem code must be (\d+) letters or digits/);
    if (lengthError) {
        const count = lengthError[1];
        if (normalizeLang(lang) === 'mn') return `Идэвхжүүлэх код ${count} үсэг эсвэл цифр байх ёстой.`;
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
        if (normalizeLang(lang) === 'mn') return `Идэвхжүүлэлт амжилтгүй: ${detail}`;
        return localizeByLang(lang, `Redeem failed: ${detail}`, `兑换失败：${detail}`, `兌換失敗：${detail}`);
    }
    const resetFailed = raw.match(/^reset voucher failed:\s*(.+)$/i);
    if (resetFailed) {
        const detail = resetFailed[1];
        if (normalizeLang(lang) === 'mn') return `Дахин тохируулах эрх амжилтгүй: ${detail}`;
        return localizeByLang(lang, `Reset voucher failed: ${detail}`, `重置券使用失败：${detail}`, `重置券使用失敗：${detail}`);
    }
    return raw || emptyFallback || localizeByLang(lang, "Redeem failed. Please try again later.", "兑换失败，请稍后重试。", "兌換失敗，請稍後重試。");
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
        if (normalizeLang(lang) === 'mn') return `Үйлчилгээний төлөв шалгах амжилтгүй: ${detail}`;
        return localizeByLang(lang, `Service status query failed: ${detail}`, `服务状态查询失败：${detail}`, `服務狀態查詢失敗：${detail}`);
    }
    return raw;
}

export type MnDurationUnit = 'second' | 'minute' | 'hour' | 'day';

/** Khalkha unit for a retry countdown. Callers keep their own rounding. */
export function mnDurationUnit(count: number, unit: MnDurationUnit): string {
    switch (unit) {
        case 'second': return `${count} секунд`;
        case 'minute': return `${count} минут`;
        case 'hour': return `${count} цаг`;
        case 'day': return `${count} өдөр`;
    }
}

/** "2 цаг 1 минут" → "ойролцоогоор 2 цаг 1 минутын дараа". Genitive attaches to the last unit. */
export function mnApproxAfter(duration: string): string {
    let phrase = duration;
    if (duration.endsWith('секунд') || duration.endsWith('минут')) phrase = `${duration}ын`;
    else if (duration.endsWith('цаг')) phrase = `${duration}ийн`;
    else if (duration.endsWith('өдөр')) phrase = `${duration.slice(0, -2)}рийн`;
    return `ойролцоогоор ${phrase} дараа`;
}
