import { describe, expect, it } from 'vitest';
import { artifactFileSizes, collectBotArtifacts, formatArtifactSize, formatArtifactWhen } from './botFileArtifacts';

describe('collectBotArtifacts', () => {
    it('lists documents from replies and said notes, newest first, and skips pictures', () => {
        const at = (hour: number, minute: number) => new Date(2026, 10, 15, hour, minute).getTime();
        const rows = collectBotArtifacts({
            botId: 'b4',
            selfName: 'Bot 4',
            messages: [
                {
                    id: `m-${at(5, 7).toString(36)}-a`,
                    role: 'assistant',
                    // A later follow-up must not pull this file above the 05:19 report.
                    conversationAt: at(9, 0),
                    localPaths: ['C:\\Users\\alice\\MaClaw\\bot-files\\b4\\北京天气报告-0507.pdf', 'C:\\Users\\alice\\MaClaw\\bot-files\\b4\\shot.png'],
                },
                {
                    id: 'm2',
                    role: 'assistant',
                    files: [{ name: 'notes.md', mime: 'text/markdown', data: 'bm90ZQ==' }],
                },
                {
                    id: `m-${at(5, 30).toString(36)}-p`,
                    role: 'assistant',
                    pending: true,
                    localPaths: ['C:\\Users\\alice\\MaClaw\\bot-files\\b4\\pending.pdf'],
                },
                {
                    id: 'm3',
                    role: 'user',
                    localPaths: ['C:\\nope.pdf'],
                },
                {
                    id: 'm4',
                    role: 'assistant',
                    chat: true,
                    content: 'mybod 交来文件：写好了\n附件：简历.docx',
                    feed: { from: 'mybod', word: '交来文件', noteId: 'note_resume', stored: ['简历.docx'] },
                    requestId: 'note_resume:target',
                },
            ],
            said: [
                {
                    id: 'n1',
                    from: 'b4',
                    created_at: '2026-11-15T05:19:00',
                    files: [
                        { name: '北京天气报告-0515.pdf', mime: 'application/pdf', size: 43 * 1024 },
                        { name: 'snake.png', mime: 'image/png', size: 100 },
                    ],
                },
                {
                    id: 'note_resume',
                    from: 'b2',
                    created_at: '2026-11-15T05:10:00',
                    files: [{ name: '简历.docx', size: 1200 }],
                },
            ],
        });
        expect(rows.map(row => row.name)).toEqual([
            '北京天气报告-0515.pdf',
            '简历.docx',
            '北京天气报告-0507.pdf',
            'notes.md',
        ]);
        expect(rows[0].size).toBe(43 * 1024);
        expect(rows[0].source).toEqual({ kind: 'said', noteId: 'n1', botId: 'b4' });
        expect(rows.filter(row => row.name === '简历.docx')).toHaveLength(1);
        expect(rows.some(row => row.name === 'shot.png' || row.name === 'snake.png' || row.name === 'nope.pdf' || row.name === 'pending.pdf')).toBe(false);
        expect(rows.find(row => row.name === 'notes.md')?.size).toBe(4);
    });

    it('formats size and time for the table', () => {
        expect(formatArtifactSize(undefined)).toBe('');
        expect(formatArtifactSize(43 * 1024)).toBe('43 KB');
        expect(formatArtifactSize(512)).toBe('512 B');
        const ms = new Date(2026, 10, 15, 5, 19).getTime();
        expect(formatArtifactWhen(ms)).toBe('2026-11-15 05:19');
        expect(formatArtifactWhen(0)).toBe('');
    });

    it('keeps a failed size lookup retryable', async () => {
        const w = window as unknown as { go?: { main?: { App?: { DesktopBotArtifactSizes?: (paths: string[]) => Promise<number[]> } } } };
        try {
            w.go = { main: { App: { DesktopBotArtifactSizes: async () => { throw new Error('busy'); } } } };
            await expect(artifactFileSizes(['a.pdf'])).resolves.toBeNull();
            w.go = { main: { App: { DesktopBotArtifactSizes: async () => [-1, 12] } } };
            await expect(artifactFileSizes(['a.pdf', 'b.pdf'])).resolves.toEqual([-1, 12]);
        } finally {
            delete w.go;
        }
    });
});
