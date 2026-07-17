import { describe, expect, it } from 'vitest';
import { downloadSourceName, failedUpdateResult, isRestorableStableResult } from './UpdateModal';

describe('downloadSourceName', () => {
    it('uses the first backend download candidate and preserves signed URL query values', () => {
        expect(downloadSourceName('https://cdn.example.com/update.exe?signature=a,b|https://github.com/RapidAI/MaClaw/releases/download/v1/update.exe'))
            .toBe('cdn.example.com');
    });

    it('maps known download hosts to their user-facing station names', () => {
        expect(downloadSourceName('github.com')).toBe('GitHub Releases');
        expect(downloadSourceName('https://assets-123.cos.ap-shanghai.myqcloud.com/update.exe')).toBe('Tencent Cloud COS');
        expect(downloadSourceName('https://downloads.example.r2.dev/update.exe')).toBe('Cloudflare R2');
    });

    it('returns an empty label when no download source is available', () => {
        expect(downloadSourceName('')).toBe('');
        expect(downloadSourceName(undefined)).toBe('');
    });

    it('splits on newlines the same way the backend combines candidate URLs', () => {
        expect(downloadSourceName('https://pub.example.r2.dev/latest/app.exe\nhttps://github.com/RapidAI/MaClaw/releases/download/v1/app.exe'))
            .toBe('Cloudflare R2');
    });
});

describe('isRestorableStableResult', () => {
    it('accepts stable and legacy payloads', () => {
        expect(isRestorableStableResult({ channel: 'stable', has_update: true })).toBe(true);
        expect(isRestorableStableResult({ has_update: false })).toBe(true);
    });

    it('rejects beta and failed payloads', () => {
        expect(isRestorableStableResult({ channel: 'beta' })).toBe(false);
        expect(isRestorableStableResult({ channel: 'stable', check_failed: true })).toBe(false);
        expect(isRestorableStableResult(null)).toBe(false);
    });
});

describe('failedUpdateResult', () => {
    it('marks the payload as a check failure', () => {
        const got = failedUpdateResult('network down');
        expect(got.check_failed).toBe(true);
        expect(got.has_update).toBe(false);
        expect(got.message).toBe('network down');
    });
});
