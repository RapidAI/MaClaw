// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { IMAuditPanel } from '../IMAuditPanel';

const QueryIMAuditMessagesMock = vi.fn();
const GetIMAuditUsersMock = vi.fn();
const ExportIMAuditCSVMock = vi.fn();
const DeleteIMAuditMessagesBeforeMock = vi.fn();
const showConfirmMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    DeleteIMAuditMessagesBefore: (...args: unknown[]) => DeleteIMAuditMessagesBeforeMock(...args),
    ExportIMAuditCSV: (...args: unknown[]) => ExportIMAuditCSVMock(...args),
    GetIMAuditUsers: (...args: unknown[]) => GetIMAuditUsersMock(...args),
    OpenFileOrShowInFolder: vi.fn(),
    QueryIMAuditMessages: (...args: unknown[]) => QueryIMAuditMessagesMock(...args),
}));

vi.mock('../../../hooks/useSafeBackdropDismiss', () => ({
    useSafeBackdropDismiss: () => ({ backdropProps: {}, dialogProps: {} }),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: (...args: unknown[]) => showConfirmMock(...args) }),
}));

describe('IMAuditPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        GetIMAuditUsersMock.mockResolvedValue(['user-1']);
        ExportIMAuditCSVMock.mockResolvedValue('');
        DeleteIMAuditMessagesBeforeMock.mockResolvedValue(0);
        showConfirmMock.mockResolvedValue(false);
        QueryIMAuditMessagesMock.mockResolvedValue({
            messages: [{
                id: 1,
                timestamp: '2026-07-28T15:26:40Z',
                user_id: 'user-1',
                platform: 'lansenger',
                role: 'assistant',
                content: '## 天气预报\n\n| 时段 | 天气 |\n| --- | --- |\n| 白天 | 雷阵雨 |\n\n**注意安全**',
            }],
            total: 1,
            page: 1,
            page_size: 50,
        });
    });

    it('renders assistant messages as safe GFM, including headings and tables', async () => {
        const { container } = render(<IMAuditPanel platform="lansenger" lang="zh-Hans" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByRole('heading', { name: '天气预报' })).toBeTruthy());

        expect(container.querySelector('.im-audit-bubble table')).toBeTruthy();
        expect(screen.getByRole('columnheader', { name: '时段' })).toBeTruthy();
        expect(screen.getByText('雷阵雨')).toBeTruthy();
        expect(screen.getByText('注意安全').tagName).toBe('STRONG');
        expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(1);
    });

    it('keeps markup supplied in an audit message inert', async () => {
        QueryIMAuditMessagesMock.mockResolvedValue({
            messages: [{
                id: 2,
                timestamp: '2026-07-28T15:26:40Z',
                user_id: 'user-1',
                platform: 'lansenger',
                role: 'assistant',
                content: '<img src=x onerror=alert(1)>',
            }],
            total: 1,
            page: 1,
            page_size: 50,
        });

        const { container } = render(<IMAuditPanel platform="lansenger" lang="zh-Hans" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy());
        expect(container.querySelector('img')).toBeNull();
    });

    it('renders an unlabelled fenced code block as a block', async () => {
        QueryIMAuditMessagesMock.mockResolvedValue({
            messages: [{
                id: 11,
                timestamp: '2026-07-28T15:26:40Z',
                user_id: 'user-1',
                platform: 'lansenger',
                role: 'assistant',
                content: '```\nconst response = "ok";\n```',
            }],
            total: 1,
            page: 1,
            page_size: 50,
        });
        const { container } = render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByText('const response = "ok";')).toBeTruthy());
        expect(container.querySelector('pre > code.im-audit-inline-code')).toBeNull();
    });

    it('permits only safe external Markdown links', async () => {
        QueryIMAuditMessagesMock.mockResolvedValue({
            messages: [{
                id: 12,
                timestamp: '2026-07-28T15:26:40Z',
                user_id: 'user-1',
                platform: 'lansenger',
                role: 'assistant',
                content: '[web](https://example.com) [unsafe](javascript:alert(1)) [file](file:///C:/private.txt)',
            }],
            total: 1,
            page: 1,
            page_size: 50,
        });
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByRole('link', { name: 'web' }).getAttribute('href')).toBe('https://example.com'));
        expect(screen.queryByRole('link', { name: 'unsafe' })).toBeNull();
        expect(screen.queryByRole('link', { name: 'file' })).toBeNull();
    });

    it('normalizes malformed message records before rendering', async () => {
        QueryIMAuditMessagesMock.mockResolvedValue({
            Messages: [
                null,
                { ID: '7', Timestamp: '2026-07-28T15:26:40Z', UserID: 'alice', Platform: 'lansenger', Role: 'user', Content: 'safe item' },
                { ID: 8, Content: 42 },
                { id: 9, role: 'admin', content: 'unknown role' },
            ],
            Total: 'bad',
            Page: 0,
            PageSize: 'NaN',
        });
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByText('safe item')).toBeTruthy());
        expect(screen.getByText('unknown role')).toBeTruthy();
        expect(screen.queryByText('42')).toBeNull();
        expect(document.querySelector('[data-role="user"]')).toBeTruthy();
        expect(document.querySelector('[data-role="assistant"]')).toBeTruthy();
        expect(screen.getByText('0 total')).toBeTruthy();
    });

    it('ignores a slow response from a superseded query', async () => {
        let resolveStale: ((value: unknown) => void) | undefined;
        QueryIMAuditMessagesMock
            .mockImplementationOnce(() => Promise.resolve({ messages: [], total: 1, page: 1, page_size: 50 }))
            .mockImplementationOnce(() => new Promise((resolve) => { resolveStale = resolve; }))
            .mockResolvedValueOnce({
                messages: [{ id: 3, timestamp: '2026-07-28T15:26:40Z', user_id: 'new', platform: 'lansenger', role: 'assistant', content: 'new result' }],
                total: 1,
                page: 1,
                page_size: 50,
            });

        render(<IMAuditPanel platform="lansenger" lang="zh-Hans" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('combobox')).toBeTruthy());

        fireEvent.change(screen.getByRole('combobox'), { target: { value: 'user-1' } });
        await waitFor(() => expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(2));
        fireEvent.change(screen.getByPlaceholderText('搜索消息内容...'), { target: { value: 'new' } });
        fireEvent.click(screen.getByRole('button', { name: '搜索' }));
        await waitFor(() => expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(3));
        // Resolve the late user-filter response after the newer search owns the query.
        resolveStale?.({
            messages: [{ id: 4, timestamp: '2026-07-28T15:26:40Z', user_id: 'old', platform: 'lansenger', role: 'assistant', content: 'stale result' }],
            total: 1,
            page: 1,
            page_size: 50,
        });

        await waitFor(() => expect(screen.queryByText('stale result')).toBeNull());
        expect(screen.getByText('new result')).toBeTruthy();
    });

    it('reruns the current query when searching for the same term', async () => {
        const { container } = render(<IMAuditPanel platform="lansenger" lang="zh-Hans" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByPlaceholderText('搜索消息内容...')).toBeTruthy());

        const search = screen.getByRole('button', { name: '搜索' });
        // The initial page is supplied from the startup query. A deliberate search must still fetch.
        fireEvent.click(search);
        await waitFor(() => expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(2));
        expect(container.querySelector('.im-audit-list')?.getAttribute('aria-busy')).toBe('false');
    });

    it('does not reuse the startup first page after opening on a later page', async () => {
        QueryIMAuditMessagesMock
            .mockResolvedValueOnce({
                messages: [{ id: 1, timestamp: '2026-07-28T15:26:40Z', user_id: 'user-1', platform: 'lansenger', role: 'assistant', content: 'first page at startup' }],
                total: 51,
                page: 1,
                page_size: 50,
            })
            .mockResolvedValueOnce({
                messages: [{ id: 51, timestamp: '2026-07-28T15:26:41Z', user_id: 'user-1', platform: 'lansenger', role: 'assistant', content: 'latest startup page' }],
                total: 51,
                page: 2,
                page_size: 50,
            })
            .mockResolvedValueOnce({
                messages: [{ id: 52, timestamp: '2026-07-28T15:26:42Z', user_id: 'user-1', platform: 'lansenger', role: 'assistant', content: 'fresh empty search' }],
                total: 52,
                page: 1,
                page_size: 50,
            });
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        await waitFor(() => expect(screen.getByText('latest startup page')).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: 'Search' }));

        await waitFor(() => expect(screen.getByText('fresh empty search')).toBeTruthy());
        expect(screen.queryByText('first page at startup')).toBeNull();
        expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(3);
    });

    it('does not refetch messages merely because the interface language changes', async () => {
        const { rerender } = render(<IMAuditPanel platform="lansenger" lang="zh-Hans" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('heading', { name: '天气预报' })).toBeTruthy());

        rerender(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        expect(QueryIMAuditMessagesMock).toHaveBeenCalledTimes(1);
        expect(screen.getByText('1 total')).toBeTruthy();
    });

    it('does not render a settled startup request for a platform that was replaced', async () => {
        let resolvePreviousPlatform: ((value: unknown) => void) | undefined;
        QueryIMAuditMessagesMock
            .mockImplementationOnce(() => new Promise((resolve) => { resolvePreviousPlatform = resolve; }))
            .mockResolvedValueOnce({
                messages: [{ id: 20, timestamp: '2026-07-28T15:26:40Z', user_id: 'user-1', platform: 'telegram', role: 'assistant', content: 'telegram message' }],
                total: 1,
                page: 1,
                page_size: 50,
            });
        const { rerender } = render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);

        rerender(<IMAuditPanel platform="telegram" lang="en" onClose={vi.fn()} />);
        resolvePreviousPlatform?.({
            messages: [{ id: 19, timestamp: '2026-07-28T15:26:39Z', user_id: 'user-1', platform: 'lansenger', role: 'assistant', content: 'stale lansenger message' }],
            total: 1,
            page: 1,
            page_size: 50,
        });

        await waitFor(() => expect(screen.getByText('telegram message')).toBeTruthy());
        expect(screen.queryByText('stale lansenger message')).toBeNull();
    });

    it('does not expose backend error details while exporting', async () => {
        ExportIMAuditCSVMock.mockRejectedValue(new Error('C:\\private\\internal.csv'));
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Export CSV' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }));

        await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Failed to export CSV'));
        expect(screen.queryByText(/private\\internal/)).toBeNull();
    });

    it('treats a missing export path as an export failure', async () => {
        ExportIMAuditCSVMock.mockResolvedValue('');
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Export CSV' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }));

        await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Failed to export CSV'));
    });

    it('keeps loaded messages visible while a refresh is pending', async () => {
        let resolveRefresh: ((value: unknown) => void) | undefined;
        QueryIMAuditMessagesMock
            .mockResolvedValueOnce({
                messages: [{ id: 10, timestamp: '2026-07-28T15:26:40Z', user_id: 'user-1', platform: 'lansenger', role: 'assistant', content: 'existing message' }],
                total: 1,
                page: 1,
                page_size: 50,
            })
            .mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve; }));
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByText('existing message')).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
        await waitFor(() => expect(screen.getByText('Refreshing...')).toBeTruthy());
        expect(screen.getByText('existing message')).toBeTruthy();

        resolveRefresh?.({ messages: [], total: 0, page: 1, page_size: 50 });
        await waitFor(() => expect(screen.getByText('No messages found')).toBeTruthy());
    });

    it('prevents a duplicate cleanup while deletion is pending', async () => {
        let resolveDelete: ((value: unknown) => void) | undefined;
        showConfirmMock.mockResolvedValue(true);
        DeleteIMAuditMessagesBeforeMock.mockImplementation(() => new Promise((resolve) => { resolveDelete = resolve; }));
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Cleanup' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Cleanup' }));
        fireEvent.click(screen.getByRole('button', { name: /Delete records older than 30 days/ }));
        await waitFor(() => expect(DeleteIMAuditMessagesBeforeMock).toHaveBeenCalledTimes(1));
        expect(screen.getByRole('button', { name: 'Cleanup' }).hasAttribute('disabled')).toBe(true);

        resolveDelete?.(0);
        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Clean up audit messages' })).toBeNull());
    });

    it('prevents duplicate cleanup confirmations before the first confirmation resolves', async () => {
        let resolveConfirm: ((value: boolean) => void) | undefined;
        showConfirmMock.mockImplementation(() => new Promise<boolean>((resolve) => { resolveConfirm = resolve; }));
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Cleanup' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Cleanup' }));
        fireEvent.click(screen.getByRole('button', { name: /Delete records older than 30 days/ }));
        fireEvent.click(screen.getByRole('button', { name: /Delete records older than 30 days/ }));
        expect(showConfirmMock).toHaveBeenCalledTimes(1);

        resolveConfirm?.(false);
        await waitFor(() => expect(DeleteIMAuditMessagesBeforeMock).not.toHaveBeenCalled());
    });

    it('recovers when the cleanup confirmation cannot be opened', async () => {
        showConfirmMock.mockRejectedValue(new Error('dialog unavailable'));
        render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Cleanup' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Cleanup' }));
        fireEvent.click(screen.getByRole('button', { name: /Delete records older than 30 days/ }));

        await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Failed to clean up audit messages'));
        expect(DeleteIMAuditMessagesBeforeMock).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: 'Cleanup' }).hasAttribute('disabled')).toBe(false);
    });

    it('does not apply a completed cleanup to a platform opened afterwards', async () => {
        let resolveDelete: ((value: unknown) => void) | undefined;
        showConfirmMock.mockResolvedValue(true);
        DeleteIMAuditMessagesBeforeMock.mockImplementation(() => new Promise((resolve) => { resolveDelete = resolve; }));
        const { rerender } = render(<IMAuditPanel platform="lansenger" lang="en" onClose={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Cleanup' })).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: 'Cleanup' }));
        fireEvent.click(screen.getByRole('button', { name: /Delete records older than 30 days/ }));
        await waitFor(() => expect(DeleteIMAuditMessagesBeforeMock).toHaveBeenCalledTimes(1));
        rerender(<IMAuditPanel platform="telegram" lang="en" onClose={vi.fn()} />);
        resolveDelete?.(7);

        await waitFor(() => expect(screen.getByText('Telegram Message Watch')).toBeTruthy());
        expect(screen.queryByText('Deleted 7 records')).toBeNull();
        expect(screen.getByRole('button', { name: 'Cleanup' }).hasAttribute('disabled')).toBe(false);
    });
});
