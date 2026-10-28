import type { AdminNotification } from "./useNotifications";

/**
 * A shared document is delivered as a Hub notification so a recipient who was
 * offline still sees it. The notice is not an invitation row: it stays in the
 * bell as a normal notice and the detail offers 打开.
 */
export const COMPANION_SHARE_SOURCE = "companion_share";

/**
 * A host-transfer request is actionable: the nominee accepts or declines in
 * the bell, and only acceptance moves the seat. The informational copy for
 * other participants uses the notice source.
 */
export const COMPANION_HOST_TRANSFER_SOURCE = "companion_host_transfer";

export function companionHostTransferToken(notice: AdminNotification): string {
    if (String(notice.source || "").trim() !== COMPANION_HOST_TRANSFER_SOURCE) return "";
    return String(notice.source_id || "").trim();
}

export function companionHostTransferErrorText(error: unknown, lang: string): string {
    const raw = error instanceof Error ? error.message : String(error || "");
    const text = raw.trim();
    const isZh = lang.startsWith("zh");
    if (text.includes("没有等待你处理") || text.includes("SHARE_FORBIDDEN")) {
        return isZh ? "这次主持人转移已失效。" : "This host transfer is no longer pending.";
    }
    // The nomination outlived the nominee's access: they left the group while
    // the request was waiting, so there is nothing to accept.
    if (text.includes("分享范围")) {
        return isZh ? "你已不在这份文档的分享范围里。" : "You no longer have access to this document.";
    }
    if (text.includes("已过期") || text.includes("expired")) {
        return isZh ? "这份分享已过期。" : "This share has expired.";
    }
    if (text.includes("分享链接不存在") || text.includes("not found")) {
        return isZh ? "分享已被撤回。" : "The share was withdrawn.";
    }
    return text || (isZh ? "处理失败，请稍后重试。" : "Couldn't handle this request. Try again later.");
}

export function companionShareToken(notice: AdminNotification): string {
    if (String(notice.source || "").trim() !== COMPANION_SHARE_SOURCE) return "";
    return String(notice.source_id || "").trim();
}

export function companionShareOpenErrorText(error: unknown, lang: string): string {
    const raw = error instanceof Error ? error.message : String(error || "");
    const text = raw.trim();
    const isZh = lang.startsWith("zh");
    if (text.includes("需要分享密码") || text.includes("password")) {
        return isZh
            ? "这份分享需要密码，请在文档管理的「导入分享」里输入密码打开。"
            : "This share needs a password. Open it from 文档管理 → 导入分享.";
    }
    if (text.includes("已过期") || text.includes("expired")) {
        return isZh ? "这份分享已过期。" : "This share has expired.";
    }
    if (text.includes("分享链接不存在") || text.includes("not found")) {
        return isZh ? "分享已被撤回。" : "The share was withdrawn.";
    }
    return text || (isZh ? "打不开这份分享，请稍后重试。" : "Couldn't open this share. Try again later.");
}
