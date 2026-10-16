import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LatexTemplateLibraryPage } from '../LatexTemplateLibraryPage';

const bindings: Record<string, unknown> = {};

vi.mock('../../../utils/wailsAppModule', () => ({
    getWailsAppModule: async () => bindings,
}));

const LIBRARY = {
    categories: [
        { id: 'conference', name: '会议', builtin: true },
        { id: 'journal', name: '期刊', builtin: true },
        { id: 'thesis', name: '毕业论文', builtin: true },
        { id: 'other', name: '其它', builtin: true },
    ],
    templates: [
        { id: 'blank', name: '空白模板', description: '从最小骨架开始', category_id: 'other', author: 'MaClaw', version: '1.0.0', file_count: 1 },
        { id: 'tpl-ieee', name: 'IEEE 会议模板', description: '双栏', category_id: 'conference', source: 'hub', version: 'v1.2', file_count: 3 },
        { id: 'tpl-local', name: '本地模板', category_id: 'thesis', source: 'local', author: '张三', share_status: 'pending', share_note: '缺少样式文件' },
    ],
};

beforeEach(() => {
    for (const key of Object.keys(bindings)) delete bindings[key];
    bindings.ListLatexTemplates = vi.fn().mockResolvedValue(JSON.stringify(LIBRARY));
    bindings.SyncLatexTemplateShares = vi.fn().mockResolvedValue(undefined);
    bindings.SyncLatexTemplatesFromHub = vi.fn().mockResolvedValue('{"added":2,"failed":0}');
    bindings.ShareLatexTemplate = vi.fn().mockResolvedValue('{"id":"hub_1","status":"pending"}');
});

afterEach(cleanup);

describe('LatexTemplateLibraryPage', () => {
    it('groups templates into the four product categories', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-section-conference');
        expect(screen.getByTestId('latex-template-section-thesis')).toBeTruthy();
        expect(screen.queryByTestId('latex-template-section-journal')).toBeNull();
        expect(screen.queryByTestId('latex-template-section-other')).toBeNull();
        expect(screen.getByTestId('latex-template-card-tpl-ieee').textContent).toContain('IEEE 会议模板');
        expect(screen.getByTestId('latex-template-count').textContent).toBe('3');
    });

    it('refreshes moderation state before listing', async () => {
        // Otherwise a submitted template would keep showing a stale badge.
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-local');
        expect(bindings.SyncLatexTemplateShares).toHaveBeenCalled();
    });

    it('still renders the library when the moderation refresh is unavailable', async () => {
        delete bindings.SyncLatexTemplateShares;
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByTestId('latex-template-card-tpl-ieee')).toBeTruthy();
    });

    it('reports a load failure instead of rendering an empty library', async () => {
        bindings.ListLatexTemplates = vi.fn().mockRejectedValue(new Error('backend unavailable'));
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByText(/backend unavailable/)).toBeTruthy();
    });

    it('hands the picked template to its caller', async () => {
        const onUseTemplate = vi.fn();
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={onUseTemplate} />);
        fireEvent.click(await screen.findByTestId('latex-template-use-tpl-ieee'));
        expect(onUseTemplate).toHaveBeenCalledWith(expect.objectContaining({ id: 'tpl-ieee' }));
    });

    it('shares a template and reports the review state', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        fireEvent.click(await screen.findByTestId('latex-template-share-button-tpl-local'));
        await waitFor(() => expect(bindings.ShareLatexTemplate).toHaveBeenCalledWith('tpl-local'));
        expect(await screen.findByText(/等待管理员审核/)).toBeTruthy();
    });

    it('says plainly when a sync pass left templates behind', async () => {
        // One pass is bounded, so the count must not imply the whole catalogue
        // was fetched.
        bindings.SyncLatexTemplatesFromHub = vi.fn().mockResolvedValue('{"added":2,"remaining":7}');
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        fireEvent.click(screen.getByTestId('latex-template-sync'));
        expect(await screen.findByText(/还有 7 个模板待同步/)).toBeTruthy();
    });

    it('never offers share or remove for the blank option', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        const blankCard = await screen.findByTestId('latex-template-card-blank');
        expect(screen.queryByTestId('latex-template-share-button-blank')).toBeNull();
        expect(screen.queryByTestId('latex-template-remove-blank')).toBeNull();
        expect(screen.getByTestId('latex-template-use-blank').textContent).toContain('从空白开始');
        expect(blankCard.textContent).toContain('从最小骨架开始');
        expect(blankCard.textContent).not.toContain('MaClaw');
        expect(screen.getByTestId('latex-template-page').querySelector('[data-testid^="latex-template-card-"]')?.getAttribute('data-testid')).toBe('latex-template-card-blank');
    });

    it('keeps source, version, and the moderation note readable', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        const hub = await screen.findByTestId('latex-template-card-tpl-ieee');
        const source = hub.querySelector('.latex-template-card__tag');
        expect(source?.textContent).toBe('中心');
        expect(source?.getAttribute('title')).toBe('来自模板中心');
        expect(screen.queryByTestId('latex-template-share-button-tpl-ieee')).toBeNull();
        expect(screen.getByTestId('latex-template-remove-tpl-ieee')).toBeTruthy();
        expect(hub.textContent).toContain('v1.2');
        expect(hub.textContent).not.toContain('vv1.2');

        const local = screen.getByTestId('latex-template-card-tpl-local');
        expect(screen.getByTestId('latex-template-share-tpl-local').getAttribute('data-share-status')).toBe('pending');
        expect(local.textContent).toContain('备注：缺少样式文件');
        expect(local.querySelector('[title="作者：张三"]')?.textContent).toBe('张三');
        expect(local.querySelector('[title="作者：张三"]')?.getAttribute('aria-label')).toBe('作者：张三');
    });

    it('keeps use available while a refresh is still in flight', async () => {
        let release: (value: string) => void = () => {};
        const gate = new Promise<string>((resolve) => { release = resolve; });
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-use-tpl-ieee');
        bindings.ListLatexTemplates = vi.fn().mockReturnValue(gate);
        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        await waitFor(() => expect((screen.getByTestId('latex-template-import') as HTMLButtonElement).disabled).toBe(true));
        expect((screen.getByTestId('latex-template-use-tpl-ieee') as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByTestId('latex-template-share-button-tpl-local') as HTMLButtonElement).disabled).toBe(true);
        release(JSON.stringify(LIBRARY));
        await waitFor(() => expect((screen.getByTestId('latex-template-import') as HTMLButtonElement).disabled).toBe(false));
    });

    it('keeps the current list when a refresh fails', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        bindings.ListLatexTemplates = vi.fn().mockRejectedValue(new Error('offline'));
        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        expect(await screen.findByText(/offline/)).toBeTruthy();
        expect(screen.getByTestId('latex-template-card-tpl-ieee')).toBeTruthy();
    });

    it('keeps the newer listing when an older refresh finishes later', async () => {
        let releaseFirst: (value: string) => void = () => {};
        let releaseSecond: (value: string) => void = () => {};
        const first = new Promise<string>((resolve) => { releaseFirst = resolve; });
        const second = new Promise<string>((resolve) => { releaseSecond = resolve; });
        let calls = 0;
        bindings.ListLatexTemplates = vi.fn(() => {
            calls += 1;
            return calls === 1 ? first : second;
        });
        const newer = {
            categories: LIBRARY.categories,
            templates: [
                { id: 'blank', name: '空白模板', category_id: 'other' },
                { id: 'tpl-new', name: '新模板', category_id: 'conference', source: 'hub' },
            ],
        };
        const { rerender } = render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await waitFor(() => expect(bindings.ListLatexTemplates).toHaveBeenCalledTimes(1));
        rerender(<LatexTemplateLibraryPage lang="en" onUseTemplate={vi.fn()} />);
        await waitFor(() => expect(bindings.ListLatexTemplates).toHaveBeenCalledTimes(2));
        releaseSecond(JSON.stringify(newer));
        await screen.findByTestId('latex-template-card-tpl-new');
        releaseFirst(JSON.stringify(LIBRARY));
        await waitFor(() => expect((screen.getByTestId('latex-template-import') as HTMLButtonElement).disabled).toBe(false));
        expect(screen.queryByTestId('latex-template-card-tpl-ieee')).toBeNull();
    });

    it('keeps the current list when a refresh payload is not JSON', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        bindings.ListLatexTemplates = vi.fn().mockResolvedValue('not json');
        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        expect(await screen.findByText(/invalid library/)).toBeTruthy();
        expect(screen.getByTestId('latex-template-card-tpl-ieee')).toBeTruthy();
    });

    it('says the library is empty when nothing is installed', async () => {
        bindings.ListLatexTemplates = vi.fn().mockResolvedValue('{"categories":[],"templates":[]}');
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByText(/还没有模板/)).toBeTruthy();
    });

    it('shows 20 cards a page and keeps the rest on the next page', async () => {
        const templates = [
            { id: 'blank', name: '空白模板', category_id: 'other' },
            ...Array.from({ length: 21 }, (_, index) => ({
                id: `tpl-${index + 1}`,
                name: `模板 ${index + 1}`,
                category_id: 'conference',
                source: 'hub',
            })),
        ];
        bindings.ListLatexTemplates = vi.fn().mockResolvedValue(JSON.stringify({
            categories: LIBRARY.categories,
            templates,
        }));
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByTestId('latex-template-card-tpl-1')).toBeTruthy();
        expect(screen.getByTestId('latex-template-card-tpl-20')).toBeTruthy();
        expect(screen.queryByTestId('latex-template-card-tpl-21')).toBeNull();
        expect(screen.getByTestId('latex-template-pager-conference').textContent).toContain('1 / 2');
        fireEvent.click(screen.getByTestId('latex-template-page-next-conference'));
        expect(await screen.findByTestId('latex-template-card-tpl-21')).toBeTruthy();
        expect(screen.queryByTestId('latex-template-card-tpl-1')).toBeNull();
        expect(screen.getByTestId('latex-template-pager-conference').textContent).toContain('2 / 2');
        fireEvent.click(screen.getByTestId('latex-template-page-prev-conference'));
        expect(await screen.findByTestId('latex-template-card-tpl-1')).toBeTruthy();
        expect(screen.queryByTestId('latex-template-card-tpl-21')).toBeNull();
    });

    it('omits the pager when a category fits on one page', async () => {
        const templates = Array.from({ length: 20 }, (_, index) => ({
            id: `tpl-${index + 1}`,
            name: `模板 ${index + 1}`,
            category_id: 'conference',
            source: 'hub',
        }));
        bindings.ListLatexTemplates = vi.fn().mockResolvedValue(JSON.stringify({
            categories: LIBRARY.categories,
            templates,
        }));
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByTestId('latex-template-card-tpl-20')).toBeTruthy();
        expect(screen.queryByTestId('latex-template-pager-conference')).toBeNull();
    });

    it('shows installed templates while the hub catalogue is still downloading', async () => {
        let release: (value: string) => void = () => {};
        bindings.SyncLatexTemplatesFromHub = vi.fn(() => new Promise<string>((resolve) => { release = resolve; }));
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        expect(await screen.findByTestId('latex-template-card-tpl-ieee')).toBeTruthy();
        expect((screen.getByTestId('latex-template-use-tpl-ieee') as HTMLButtonElement).disabled).toBe(false);
        release('{"added":0}');
    });

    it('joins an opening catalogue download instead of starting a second one', async () => {
        let release: (value: string) => void = () => {};
        bindings.SyncLatexTemplatesFromHub = vi.fn(() => new Promise<string>((resolve) => { release = resolve; }));
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        await waitFor(() => expect(bindings.SyncLatexTemplatesFromHub).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('latex-template-sync'));
        expect(bindings.SyncLatexTemplatesFromHub).toHaveBeenCalledTimes(1);
        release('{"added":1,"remaining":3}');
        expect(await screen.findByText(/还有 3 个模板待同步/)).toBeTruthy();
    });

    it('downloads the catalogue once per sync click', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        // The opening visit downloads once and, when packs were added, lists again.
        await waitFor(() => expect(bindings.ListLatexTemplates).toHaveBeenCalledTimes(2));
        const before = vi.mocked(bindings.SyncLatexTemplatesFromHub as ReturnType<typeof vi.fn>).mock.calls.length;
        fireEvent.click(screen.getByTestId('latex-template-sync'));
        expect(await screen.findByText(/已同步 2 个新模板/)).toBeTruthy();
        const after = vi.mocked(bindings.SyncLatexTemplatesFromHub as ReturnType<typeof vi.fn>).mock.calls.length;
        expect(after - before).toBe(1);
    });

    it('refreshes the local library without downloading the catalogue again', async () => {
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        await screen.findByTestId('latex-template-card-tpl-ieee');
        await waitFor(() => expect(bindings.SyncLatexTemplatesFromHub).toHaveBeenCalled());
        const before = vi.mocked(bindings.SyncLatexTemplatesFromHub as ReturnType<typeof vi.fn>).mock.calls.length;
        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        await waitFor(() => expect((screen.getByTestId('latex-template-import') as HTMLButtonElement).disabled).toBe(false));
        expect(vi.mocked(bindings.SyncLatexTemplatesFromHub as ReturnType<typeof vi.fn>).mock.calls.length).toBe(before);
    });

    it('confirms before removing a template', async () => {
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        render(<LatexTemplateLibraryPage lang="zh-Hans" onUseTemplate={vi.fn()} />);
        fireEvent.click(await screen.findByTestId('latex-template-remove-tpl-local'));
        expect(confirm).toHaveBeenCalled();
        expect(bindings.DeleteLatexTemplate).toBeUndefined();
        confirm.mockRestore();
    });
});
