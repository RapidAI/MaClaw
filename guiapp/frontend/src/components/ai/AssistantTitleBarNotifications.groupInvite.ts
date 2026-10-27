import type { GroupInviteError } from "./CompanionGroupInvites";
import { localizeText } from "./aiAssistantI18n";
import type { AdminNotification } from "./useNotifications";

/**
 * A reading-group invitation is delivered as a Hub notification so an offline
 * invitee still receives it, but it is shown as an actionable invite row, not
 * as a plain notice. These are the notices that must not be listed twice.
 */
export const COMPANION_GROUP_INVITE_SOURCE = "companion_group_invite";

/**
 * The source id is "<group id>:<user id>". The group id is what an invite row
 * is keyed on, so a notice is only folded into a row when that row exists.
 */
export function groupInviteGroupID(notice: AdminNotification): string {
    if (notice.source !== COMPANION_GROUP_INVITE_SOURCE) return "";
    return String(notice.source_id || "").split(":")[0].trim();
}

export function groupInviteElsewhereText(error: GroupInviteError, lang: string): string {
    const name = error.name.trim();
    if (!name) return error.message;
    return localizeText(
        lang,
        `Couldn't handle “${name}”: ${error.message}`,
        `「${name}」未处理：${error.message}`,
        `「${name}」未處理：${error.message}`,
    );
}
