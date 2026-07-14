import { describe, expect, it } from 'vitest';
import { downloadSourceName } from './UpdateModal';

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
});
