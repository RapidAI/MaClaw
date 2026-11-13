/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RemoteTaskArtifacts } from '../assistantTaskArtifacts';

describe('RemoteTaskArtifacts', () => {
    it('lists the remote file by name without touching the local fs', () => {
        const onFetch = vi.fn();
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report.pdf']} messageId="m1" lang="zh-Hans" onFetch={onFetch} />);
        expect(screen.getByTestId('remote-task-result-card-m1')).toBeTruthy();
        expect(screen.getByText('report.pdf')).toBeTruthy();
        expect(onFetch).not.toHaveBeenCalled();
    });

    it('fetches the remote bytes on click and then offers the saved copy', async () => {
        const onFetch = vi.fn().mockResolvedValue('C:\\Users\\me\\Downloads\\report.pdf');
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report.pdf']} messageId="m1" lang="zh-Hans" onFetch={onFetch} />);
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
        await waitFor(() => expect(onFetch).toHaveBeenCalledWith('C:\\remote\\work\\report.pdf'));
        // Once the bytes are saved locally the button switches to "open".
        await screen.findByText('打开');
        // A second click reopens the saved copy; it must not fetch again.
        fireEvent.click(screen.getByTestId('remote-task-fetch-btn'));
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

    it('issues a single fetch while a request is still in flight', async () => {
        let resolveFetch: (path: string) => void = () => {};
        const onFetch = vi.fn().mockImplementation(() => new Promise<string>(resolve => { resolveFetch = resolve; }));
        render(<RemoteTaskArtifacts paths={['C:\\remote\\work\\report.pdf']} messageId="m4" lang="zh-Hans" onFetch={onFetch} />);
        const button = screen.getByTestId('remote-task-fetch-btn');
        fireEvent.click(button);
        fireEvent.click(button);
        expect(onFetch).toHaveBeenCalledTimes(1);
        resolveFetch('C:\\Users\\me\\Downloads\\report.pdf');
        await screen.findByText('打开');
    });

    it('renders nothing when the turn produced no files', () => {
        const { container } = render(<RemoteTaskArtifacts paths={[]} messageId="m3" lang="zh-Hans" onFetch={vi.fn()} />);
        expect(container.firstChild).toBeNull();
    });
});
