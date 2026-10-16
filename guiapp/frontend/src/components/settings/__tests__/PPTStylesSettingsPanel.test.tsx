import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PPTStylesSettingsPanel } from '../PPTStylesSettingsPanel';

const list = vi.fn();
const generate = vi.fn();
const remove = vi.fn();

beforeEach(() => {
    list.mockReset();
    generate.mockReset();
    remove.mockReset();
    (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go = {
        main: {
            App: {
                ListPPTStyles: list,
                GeneratePPTStyle: generate,
                DeletePPTStyle: remove,
            },
        },
    };
});

describe('PPTStylesSettingsPanel', () => {
    it('shows style names before cover previews finish', async () => {
        let release: (url: string) => void = () => {};
        const preview = new Promise<string>((resolve) => { release = resolve; });
        (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App.ListPPTStyleChoices = vi.fn().mockResolvedValue([
            { id: 'warm', label: '温馨纪念', summary: '陶土色', builtin: true },
        ]);
        (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go.main.App.PPTStylePreview = vi.fn().mockReturnValue(preview);
        render(<PPTStylesSettingsPanel lang="zh-Hans" />);
        expect(await screen.findByText('温馨纪念')).toBeTruthy();
        expect(screen.queryByRole('img', { name: '温馨纪念 预览' })).toBeNull();
        release('data:image/png;base64,aa');
        expect(await screen.findByRole('img', { name: '温馨纪念 预览' })).toBeTruthy();
    });

    it('lists built-in and custom styles with previews', async () => {
        list.mockResolvedValue([
            { id: 'business', label: '商务汇报', summary: '深青配金色', builtin: true, preview_url: 'data:image/png;base64,aa' },
            { id: 'nebula', label: '星云', summary: '深空蓝紫', builtin: false, preview_url: 'data:image/png;base64,bb' },
        ]);
        render(<PPTStylesSettingsPanel lang="zh-Hans" />);
        expect(await screen.findByText('商务汇报')).toBeTruthy();
        expect(screen.getByText('星云')).toBeTruthy();
        expect(screen.getByRole('img', { name: '商务汇报 预览' })).toBeTruthy();
        expect(screen.getAllByRole('button', { name: '删除' })).toHaveLength(1);
    });

    it('generates a style from the description and can delete a custom one', async () => {
        list.mockResolvedValueOnce([]).mockResolvedValue([{ id: 'cyber', label: '赛博', summary: '洋红', builtin: false, preview_url: 'data:image/png;base64,cc' }]);
        generate.mockResolvedValue({ id: 'cyber', label: '赛博', summary: '洋红', builtin: false, preview_url: 'data:image/png;base64,cc' });
        remove.mockResolvedValue(undefined);
        render(<PPTStylesSettingsPanel lang="zh-Hans" />);
        await screen.findByLabelText('描述想要的风格');
        fireEvent.change(screen.getByLabelText('描述想要的风格'), { target: { value: '深色洋红霓虹' } });
        fireEvent.click(screen.getByRole('button', { name: '生成风格' }));
        await waitFor(() => expect(generate).toHaveBeenCalledWith('深色洋红霓虹', 'zh-Hans'));
        list.mockResolvedValue([{ id: 'cyber', label: '赛博', summary: '洋红', builtin: false, preview_url: 'data:image/png;base64,cc' }]);
        expect(await screen.findByText('赛博')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '删除' }));
        await waitFor(() => expect(remove).toHaveBeenCalledWith('cyber'));
        await waitFor(() => expect(screen.queryByText('赛博')).toBeNull());
    });
});
