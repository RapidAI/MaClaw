import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MATERIAL_MAX_BYTES, filesFromTransfer, materialsBlock, splitMaterials, uploadMaterial, withMaterials } from './desktopBotMaterials';
import { DesktopBotMaterialChips, DesktopBotMaterialStrip } from './DesktopBotMaterialStrip';

function fakeFile(bytes: number[], name: string) {
    const data = new Uint8Array(bytes);
    const make = (from: number, to: number): Blob & { name: string } => {
        const part = data.slice(from, to);
        return { name, size: part.length, slice: (a: number, b: number) => make(from + a, from + Math.min(b, part.length)), arrayBuffer: async () => part.buffer } as unknown as Blob & { name: string };
    };
    return make(0, data.length);
}

describe('uploadMaterial', () => {
    it('sends chunks in order and returns the remote path', async () => {
        const calls: Array<[number, number, string]> = [];
        const progress: number[] = [];
        const upload = vi.fn(async (_bot: string, _id: string, _name: string, offset: number, total: number, data: string) => {
            calls.push([offset, total, atob(data)]);
            return offset + atob(data).length >= total ? { done: true, path: '~/素材/助手/a.bin' } : { done: false };
        });
        const path = await uploadMaterial(fakeFile([97, 98, 99, 100, 101], 'a.bin'), 'bot', upload, sent => progress.push(sent), 'up-test-0001', 2);
        expect(path).toBe('~/素材/助手/a.bin');
        expect(calls).toEqual([[0, 5, 'ab'], [2, 5, 'cd'], [4, 5, 'e']]);
        expect(progress).toEqual([2, 4, 5]);
    });

    it('refuses oversize files and surfaces upload errors', async () => {
        const upload = vi.fn();
        const big = { name: 'v.mp4', size: MATERIAL_MAX_BYTES + 1, slice: () => fakeFile([], 'x') } as unknown as Blob & { name: string };
        await expect(uploadMaterial(big, 'bot', upload, () => undefined)).rejects.toThrow('too_large');
        expect(upload).not.toHaveBeenCalled();
        const failing = vi.fn(async () => { throw new Error('MaClawSrv 不可达'); });
        await expect(uploadMaterial(fakeFile([1], 'a'), 'bot', failing, () => undefined)).rejects.toThrow('不可达');
        expect(failing).toHaveBeenCalledTimes(3);
    });

    it('resumes from the server offset and stops when the chip is gone', async () => {
        const calls: number[] = [];
        const upload = vi.fn(async (_bot: string, _id: string, _name: string, offset: number) => {
            calls.push(offset);
            if (offset === 0) return { received: 2 };
            if (calls.filter(item => item === 2).length === 1) return { received: 2 };
            return offset + 2 >= 5 ? { done: true, path: '~/素材/助手/a.bin', received: 5 } : { received: offset + 2 };
        });
        const path = await uploadMaterial(fakeFile([97, 98, 99, 100, 101], 'a.bin'), 'bot', upload, () => undefined, 'up-test-0001', 2);
        expect(path).toBe('~/素材/助手/a.bin');
        expect(calls).toEqual([0, 2, 2, 4]);
        const stalled = vi.fn(async () => ({ received: 0 }));
        await expect(uploadMaterial(fakeFile([97, 98, 99], 'a.bin'), 'bot', stalled, () => undefined, 'up-test-0002', 2)).rejects.toThrow('stalled');
        const cancelled = vi.fn(async () => { throw new Error('cancelled'); });
        await expect(uploadMaterial(fakeFile([97], 'a.bin'), 'bot', cancelled, () => undefined)).rejects.toThrow('cancelled');
        expect(cancelled).toHaveBeenCalledTimes(1);
        const blocked = vi.fn();
        await expect(uploadMaterial(fakeFile([97], 'a.bin'), 'bot', blocked, () => undefined, 'up-test-0003', undefined, () => false)).rejects.toThrow('cancelled');
        expect(blocked).not.toHaveBeenCalled();
    });
});

describe('filesFromTransfer', () => {
    it('keeps a file that is only on the item list, once', () => {
        const file = new File(['png'], 'shot.png', { type: 'image/png' });
        const again = new File(['png'], 'shot.png', { type: 'image/png', lastModified: file.lastModified });
        const got = filesFromTransfer({
            items: [{ kind: 'file', getAsFile: () => file }, { kind: 'string', getAsFile: () => null }],
            files: [again],
        });
        expect(got).toEqual([file]);
        expect(filesFromTransfer({ files: { length: 0 }, items: [] })).toEqual([]);
    });
});

describe('materials block', () => {
    it('round-trips text and paths', () => {
        const content = withMaterials('用这些图发小红书', ['~/素材/助手/a.png', '~/素材/助手/b.mp4'], 'zh');
        expect(content).toBe('用这些图发小红书\n\n[素材]\n- ~/素材/助手/a.png\n- ~/素材/助手/b.mp4');
        expect(splitMaterials(content)).toEqual({ text: '用这些图发小红书', paths: ['~/素材/助手/a.png', '~/素材/助手/b.mp4'] });
    });
    it('fills in a sentence when only files are sent, and leaves plain text alone', () => {
        expect(withMaterials('  ', ['~/素材/x/a.png'], 'zh').startsWith('这些是给你的素材。\n\n[素材]')).toBe(true);
        expect(withMaterials('hi', [], 'en')).toBe('hi');
        expect(materialsBlock([])).toBe('');
        expect(splitMaterials('普通消息')).toEqual({ text: '普通消息', paths: [] });
        expect(splitMaterials('a\n[素材]\nnot a list')).toEqual({ text: 'a\n[素材]\nnot a list', paths: [] });
    });
});

describe('DesktopBotMaterialStrip', () => {
    afterEach(cleanup);
    it('shows progress, errors and done files, and attaches picked files', () => {
        const onAttach = vi.fn();
        const onRemove = vi.fn();
        render(<DesktopBotMaterialStrip lang="zh" onAttach={onAttach} onRemove={onRemove} items={[
            { id: '1', name: 'a.png', size: 100, sent: 50, status: 'uploading' },
            { id: '2', name: 'b.mp4', size: 300 * 1024 * 1024, sent: 0, status: 'error', error: 'too_large' },
            { id: '3', name: 'c.pdf', size: 2048, sent: 2048, status: 'done', path: '~/素材/x/c.pdf' },
        ]} />);
        const items = screen.getAllByTestId('desktop-bot-material');
        expect(screen.getByRole('list', { name: '已附加文件' })).toBeTruthy();
        expect(items[0].textContent).toContain('50%');
        expect(items[0].querySelector('.desktop-bot-materials__badge')?.textContent).toBe('PNG');
        expect(items[1].textContent).toContain('超过 200 MB');
        expect(items[2].textContent).toContain('2 KB');
        expect(items[2].querySelector('.desktop-bot-materials__meta')?.className).toContain('is-quiet');
        expect(items[0].querySelector('.desktop-bot-materials__meta')?.className).not.toContain('is-quiet');
        expect(items[2].getAttribute('title')).toContain('2 KB');
        expect(items[2].querySelector('.desktop-bot-materials__badge')?.textContent).toBe('PDF');
        const attach = screen.getByTestId('desktop-bot-attach');
        expect(attach.className).not.toContain('desktop-bot-chat__action');
        expect(attach.querySelector('svg')).toBeTruthy();
        fireEvent.click(screen.getAllByTestId('desktop-bot-material-remove')[1]);
        expect(onRemove).toHaveBeenCalledWith('2');
        const file = new File(['x'], 'pick.png', { type: 'image/png' });
        fireEvent.change(screen.getByTestId('desktop-bot-attach-input'), { target: { files: [file] } });
        expect(onAttach).toHaveBeenCalledWith([file]);
    });
    it('renders sent file chips by name', () => {
        render(<DesktopBotMaterialChips paths={['~/素材/x/封面.png']} />);
        expect(screen.getByTestId('desktop-bot-message-materials').textContent).toContain('封面.png');
    });
    it('keeps the paperclip as a wash icon and parks chips on their own row', () => {
        const fromFrontend = join(process.cwd(), 'src/components/bots/DesktopBotWorkspace.css');
        const cssPath = existsSync(fromFrontend) ? fromFrontend : join(process.cwd(), 'guiapp/frontend/src/components/bots/DesktopBotWorkspace.css');
        const css = readFileSync(cssPath, 'utf8');
        const attach = css.match(/\.desktop-bot-materials__attach\s*\{[^}]*\}/)?.[0] || '';
        expect(attach).toContain('width: 30px');
        expect(attach).toContain('height: 28px');
        expect(attach).toMatch(/background:\s*color-mix/);
        expect(attach).not.toMatch(/background:\s*var\(--mc-accent/);
        expect(css).toMatch(/\.desktop-bot-materials__strip\s*\{[^}]*flex:\s*1 0 100%/);
        expect(css).toMatch(/\.desktop-bot-materials__strip\s*\{[^}]*max-height:\s*120px/);
        expect(css).toMatch(/\.desktop-bot-materials__strip \.desktop-bot-materials__name\s*,[\s\S]*?clip:\s*rect/);
    });
    it('omits the chip row when nothing is attached', () => {
        render(<DesktopBotMaterialStrip lang="zh" onAttach={vi.fn()} onRemove={vi.fn()} items={[]} />);
        expect(screen.queryByRole('list', { name: '已附加文件' })).toBeNull();
        expect(screen.getByTestId('desktop-bot-attach')).toBeTruthy();
    });
});
