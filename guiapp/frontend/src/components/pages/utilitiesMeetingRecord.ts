/**
 * Utilities → 会议记录 helpers.
 * Commands must match tool-router start-recording markers (corelib/tool/router.go).
 * The command stays the exact marker. Do not pass it through inline translation.
 */

import { localizeText, normalizeLang } from '../../i18n';

export const MEETING_RECORD_COMMAND_ZH_HANS = '录制会议';
export const MEETING_RECORD_COMMAND_ZH_HANT = '錄製會議';
export const MEETING_RECORD_COMMAND_EN = 'record the meeting';

/** Auto-sent agent message that triggers desktop meeting recording. */
export function meetingRecordCommand(lang?: string): string {
    const kind = normalizeLang(lang);
    if (kind === 'zh-Hant') return MEETING_RECORD_COMMAND_ZH_HANT;
    if (kind === 'zh-Hans') return MEETING_RECORD_COMMAND_ZH_HANS;
    return MEETING_RECORD_COMMAND_EN;
}

/** Base label without timestamp (for UI copy). */
export function meetingRecordBaseTitle(lang?: string): string {
    return localizeText(lang, 'Meeting notes', '会议记录', '會議記錄');
}

/** Compact local stamp so repeated sessions stay distinguishable in the task list. */
export function formatMeetingRecordStamp(now: Date = new Date()): string {
    if (Number.isNaN(now.getTime())) {
        now = new Date();
    }
    const y = now.getFullYear();
    const m = String(now.getMonth() + 1).padStart(2, '0');
    const d = String(now.getDate()).padStart(2, '0');
    const hh = String(now.getHours()).padStart(2, '0');
    const mm = String(now.getMinutes()).padStart(2, '0');
    const ss = String(now.getSeconds()).padStart(2, '0');
    // Include seconds so two starts in the same minute stay unique.
    return `${y}-${m}-${d} ${hh}:${mm}:${ss}`;
}

/**
 * Human-readable task / tab title for a new agent session.
 * Includes a local timestamp so each meeting starts a clearly labeled task.
 */
export function meetingRecordTaskTitle(lang?: string, now: Date = new Date()): string {
    return `${meetingRecordBaseTitle(lang)} ${formatMeetingRecordStamp(now)}`;
}

/** Card body description on the utilities home grid. */
export function meetingRecordCardDesc(lang?: string): string {
    return localizeText(
        lang,
        'Open a new agent session and start meeting recording',
        '打开新的 Agent 会话并开始录制会议',
        '打開新的 Agent 會話並開始錄製會議',
    );
}

export function meetingRecordFailMessage(lang?: string): string {
    return localizeText(
        lang,
        'Could not start meeting recording. Please try again.',
        '无法开始会议录制，请稍后重试。',
        '無法開始會議錄製，請稍後重試。',
    );
}
