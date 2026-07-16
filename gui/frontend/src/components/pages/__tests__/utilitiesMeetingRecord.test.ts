import { describe, expect, it } from 'vitest';
import {
    MEETING_RECORD_COMMAND_EN,
    MEETING_RECORD_COMMAND_ZH_HANS,
    MEETING_RECORD_COMMAND_ZH_HANT,
    formatMeetingRecordStamp,
    meetingRecordBaseTitle,
    meetingRecordCardDesc,
    meetingRecordCommand,
    meetingRecordFailMessage,
    meetingRecordTaskTitle,
} from '../utilitiesMeetingRecord';

describe('utilitiesMeetingRecord', () => {
    it('picks locale-appropriate recording commands (router markers)', () => {
        expect(meetingRecordCommand(undefined)).toBe(MEETING_RECORD_COMMAND_ZH_HANS);
        expect(meetingRecordCommand('zh-Hans')).toBe(MEETING_RECORD_COMMAND_ZH_HANS);
        expect(meetingRecordCommand('zh-Hant')).toBe(MEETING_RECORD_COMMAND_ZH_HANT);
        expect(meetingRecordCommand('en')).toBe(MEETING_RECORD_COMMAND_EN);
    });

    it('formats a stable local timestamp stamp with seconds', () => {
        const now = new Date(2026, 6, 17, 9, 5, 7); // month is 0-based
        expect(formatMeetingRecordStamp(now)).toBe('2026-07-17 09:05:07');
    });

    it('falls back when given an invalid date', () => {
        const stamp = formatMeetingRecordStamp(new Date(Number.NaN));
        // yyyy-mm-dd hh:mm:ss
        expect(stamp).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    });

    it('builds timestamped task titles per locale', () => {
        const now = new Date(2026, 6, 17, 15, 30, 12);
        expect(meetingRecordBaseTitle('zh-Hans')).toBe('会议记录');
        expect(meetingRecordTaskTitle('zh-Hans', now)).toBe('会议记录 2026-07-17 15:30:12');
        expect(meetingRecordTaskTitle('zh-Hant', now)).toBe('會議記錄 2026-07-17 15:30:12');
        expect(meetingRecordTaskTitle('en', now)).toBe('Meeting notes 2026-07-17 15:30:12');
    });

    it('picks locale-appropriate card descriptions', () => {
        expect(meetingRecordCardDesc('zh-Hans')).toContain('录制会议');
        expect(meetingRecordCardDesc('zh-Hant')).toContain('錄製會議');
        expect(meetingRecordCardDesc('en')).toMatch(/meeting recording/i);
    });

    it('picks locale-appropriate failure copy', () => {
        expect(meetingRecordFailMessage('zh-Hans')).toContain('无法开始');
        expect(meetingRecordFailMessage('zh-Hant')).toContain('無法開始');
        expect(meetingRecordFailMessage('en')).toMatch(/Could not start/i);
    });
});
