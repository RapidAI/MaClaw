import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MATERIAL_MAX_BYTES, materialsBlock, splitMaterials, uploadMaterial, withMaterials } from './desktopBotMaterials';
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
        expect(items[0].textContent).toContain('50%');
        expect(items[1].textContent).toContain('超过 200 MB');
        expect(items[2].textContent).toContain('2 KB');
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
});
