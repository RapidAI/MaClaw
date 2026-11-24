/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RemoteTaskArtifacts } from '../assistantTaskArtifacts';

const { openFileMock, showItemMock } = vi.hoisted(() => ({
    openFileMock: vi.fn().mockResolvedValue(undefined),
    showItemMock: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../../../../wailsjs/go/main/App', () => ({
    OpenFileOrShowInFolder: openFileMock,
    ShowItemInFolder: showItemMock,
    ExportLatexSourceBundle: vi.fn().mockResolvedValue(''),
    ExportTaskResultFile: vi.fn().mockResolvedValue(''),
    ImportMobileDocumentFromPath: vi.fn().mockResolvedValue({ id: 'draft-1' }),
}));

describe('RemoteTaskArtifacts', () => {
    it('lists the remote file by name without touching the local fs', () => {
        const onFetch = vi.fn();
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report.pdf']} messageId="m1" lang="zh-Hans" onFetch={onFetch} />);
        expect(screen.getByTestId('remote-task-result-card-m1')).toBeTruthy();
        expect(screen.getByText('report.pdf')).toBeTruthy();
        expect(onFetch).not.toHaveBeenCalled();
    });

    it('fetches the remote bytes on click and then offers the local actions', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\report.pdf');
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report-click.pdf']} messageId="m-click" lang="zh-Hans" onFetch={onFetch} />);
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
        await waitFor(() => expect(onFetch).toHaveBeenCalledWith('C:\\remote\\work\\report-click.pdf'));
        // Once the bytes are saved locally the card shows the local-artifact actions.
        await screen.findByTestId('remote-task-preview-btn');
        expect(screen.getByTestId('remote-task-view-btn')).toBeTruthy();
        expect(screen.getByTestId('remote-task-reveal-btn')).toBeTruthy();
        // The saved copy keeps the local path on the head link.
        expect(screen.getByTestId('remote-task-artifact').getAttribute('title')).toBe('C:\\Users\\me\\Downloads\\report.pdf');
        // Opening the saved copy must not fetch again.
        fireEvent.click(screen.getByTestId('remote-task-view-btn'));
        expect(onFetch).toHaveBeenCalledTimes(1);
    });

    it('fetches automatically without opening when autoFetch is set', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\auto.pdf');
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\auto.pdf']} messageId="m-auto" lang="zh-Hans" onFetch={onFetch} autoFetch />);
        await waitFor(() => expect(onFetch).toHaveBeenCalledWith('C:\\remote\\work\\auto.pdf'));
        // The saved copy swaps the fetch button for the local-artifact actions.
        await screen.findByTestId('remote-task-view-btn');
        expect(screen.queryByTestId('remote-task-fetch-btn')).toBeNull();
        expect(onFetch).toHaveBeenCalledTimes(1);
    });

    it('re-fetches when the saved local copy can no longer be opened', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\gone.pdf');
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\gone.pdf']} messageId="m-gone" lang="zh-Hans" onFetch={onFetch} />);
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
        await screen.findByTestId('remote-task-view-btn');
        // A stale copy that the shell can no longer open drops the cache
        // entry and pulls the remote bytes again.
        openFileMock.mockRejectedValueOnce(new Error('file gone'));
        fireEvent.click(screen.getByTestId('remote-task-view-btn'));
        await waitFor(() => expect(onFetch).toHaveBeenCalledTimes(2));
    });

    it('re-fetches and replays the reveal when the saved copy is gone', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\gone-reveal.pdf');
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\gone-reveal.pdf']} messageId="m-reveal" lang="zh-Hans" onFetch={onFetch} />);
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
        await screen.findByTestId('remote-task-reveal-btn');
        // A stale copy drops the cache entry, re-downloads, and replays the
        // reveal gesture on the fresh copy rather than failing silently.
        showItemMock.mockRejectedValueOnce(new Error('file gone'));
        fireEvent.click(screen.getByTestId('remote-task-reveal-btn'));
        await waitFor(() => expect(onFetch).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(showItemMock).toHaveBeenCalledWith('C:\\Users\\me\\Downloads\\gone-reveal.pdf'));
    });

    it('bounds concurrent auto-fetches and drains the queue', async () => {
        const resolvers: Array<(path: string) => void> = [];
        const onFetch = vi.fn().mockImplementation(() => new Promise<string>(resolve => { resolvers.push(resolve); }));
        render(<RemoteTaskArtifacts
            paths={['C:\\remote\\p1.pdf', 'C:\\remote\\p2.pdf', 'C:\\remote\\p3.pdf', 'C:\\remote\\p4.pdf', 'C:\\remote\\p5.pdf']}
            messageId="m-par"
            lang="zh-Hans"
            onFetch={onFetch}
            autoFetch
        />);
        // The fan-out cap starts three; the rest wait for a slot.
        await waitFor(() => expect(onFetch).toHaveBeenCalledTimes(3));
        resolvers[0]('C:\\Users\\me\\Downloads\\p1.pdf');
        await waitFor(() => expect(onFetch).toHaveBeenCalledTimes(4));
        resolvers.forEach(resolve => resolve('C:\\Users\\me\\Downloads\\rest.pdf'));
        await waitFor(() => expect(onFetch).toHaveBeenCalledTimes(5));
    });

    it('does not auto-fetch the same file twice across a remount', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\cached.pdf');
        const first = render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\cached.pdf']} messageId="m-cached" lang="zh-Hans" onFetch={onFetch} autoFetch />);
        await screen.findByTestId('remote-task-view-btn');
        first.unmount();
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\cached.pdf']} messageId="m-cached" lang="zh-Hans" onFetch={onFetch} autoFetch />);
        await screen.findByTestId('remote-task-view-btn');
        expect(onFetch).toHaveBeenCalledTimes(1);
    });

    it('shows the remote error and offers retry', async () => {
        const onFetch = vi.fn().mockRejectedValue(new Error('文件太大（6.0 MB），暂不支持回传'));
        render(<RemoteTaskArtifacts paths={['C:\\remote\\big.zip']} messageId="m2" lang="zh-Hans" onFetch={onFetch} />);
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
        const error = await screen.findByTestId('remote-task-fetch-error');
        expect(error.textContent).toContain('文件太大');
        await screen.findByText('重试');
    });

    it('does not auto-retry a failed fetch', async () => {
        const onFetch = vi.fn().mockRejectedValue(new Error('取回失败'));
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\fail.pdf']} messageId="m-fail" lang="zh-Hans" onFetch={onFetch} autoFetch />);
        await screen.findByTestId('remote-task-fetch-error');
        expect(onFetch).toHaveBeenCalledTimes(1);
    });

    it('opens the saved copy when a click lands during a background fetch', async () => {
        let resolveFetch: (path: string) => void = () => {};
        const onFetch = vi.fn().mockImplementation(() => new Promise<string>(resolve => { resolveFetch = resolve; }));
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\queued-open.pdf']} messageId="m-queued" lang="zh-Hans" onFetch={onFetch} autoFetch />);
        await waitFor(() => expect(onFetch).toHaveBeenCalledWith('C:\\remote\\work\\queued-open.pdf'));
        // The autoFetch download owns the row; a click during it must still
        // open the saved copy when it lands, not be swallowed.
        fireEvent.click(screen.getByTestId('remote-task-artifact'));
        resolveFetch('C:\\Users\\me\\Downloads\\queued-open.pdf');
        await screen.findByTestId('remote-task-view-btn');
        await waitFor(() => expect(openFileMock).toHaveBeenCalledWith('C:\\Users\\me\\Downloads\\queued-open.pdf'));
        expect(onFetch).toHaveBeenCalledTimes(1);
    });

    it('issues a single fetch while a request is still in flight', async () => {
        let resolveFetch: (path: string) => void = () => {};
        const onFetch = vi.fn().mockImplementation(() => new Promise<string>(resolve => { resolveFetch = resolve; }));
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report-inflight.pdf']} messageId="m4" lang="zh-Hans" onFetch={onFetch} />);
        const button = screen.getByTestId('remote-task-fetch-btn');
        fireEvent.click(button);
        fireEvent.click(button);
        expect(onFetch).toHaveBeenCalledTimes(1);
        resolveFetch('C:\\Users\\me\\Downloads\\report-inflight.pdf');
        await screen.findByTestId('remote-task-view-btn');
    });

    it('collapses the list past two artifacts like the local card', () => {
        const onFetch = vi.fn();
        render(<RemoteTaskArtifacts
            paths={['C:\\remote\\work\\a.pdf', 'C:\\remote\\work\\b.pdf', 'C:\\remote\\work\\c.pdf']}
            messageId="m-collapse"
            lang="zh-Hans"
            onFetch={onFetch}
        />);
        expect(screen.getByTestId('remote-task-artifact')).toBeTruthy();
        expect(screen.getByTestId('remote-task-artifact-1')).toBeTruthy();
        expect(screen.queryByTestId('remote-task-artifact-2')).toBeNull();
        fireEvent.click(screen.getByTestId('remote-task-artifacts-more'));
        expect(screen.getByTestId('remote-task-artifact-2')).toBeTruthy();
    });

    it('renders nothing when the turn produced no files', () => {
        const { container } = render(<RemoteTaskArtifacts paths={[]} messageId="m3" lang="zh-Hans" onFetch={vi.fn()} />);
        expect(container.firstChild).toBeNull();
    });
});
