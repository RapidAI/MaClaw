// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { showConfirmMock, clipboardMock } = vi.hoisted(() => ({
    showConfirmMock: vi.fn().mockResolvedValue(true),
    clipboardMock: { writeText: vi.fn().mockResolvedValue(undefined) },
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelGitHubOAuth: vi.fn().mockResolvedValue(undefined),
    ConnectGitLab: vi.fn().mockResolvedValue({ connected: true, login: 'tux', name: 'Tux', origin: 'https://gitlab.com', scopes: 'api' }),
    DisconnectGitHub: vi.fn().mockResolvedValue(undefined),
    DisconnectGitLab: vi.fn().mockResolvedValue(undefined),
    GetGitHubConnection: vi.fn().mockResolvedValue({ connected: false, client_id: 'Iv1.b507a08c87ecfe98', custom_client_id: false }),
    GetGitLabConnection: vi.fn().mockResolvedValue({ connected: false }),
    GitHubAPIRequest: vi.fn().mockResolvedValue('{"resources":{"core":{"limit":5000,"remaining":4999},"search":{"limit":30,"remaining":29}}}'),
    GitHubCloneRepository: vi.fn().mockResolvedValue({ output: 'Cloning into repo...', dest_dir: '/data/github/repo', repo_url: 'https://github.com/o/repo.git' }),
    OpenGitLabTokenPage: vi.fn().mockResolvedValue(undefined),
    ShowItemInFolder: vi.fn().mockResolvedValue(undefined),
    StartGitHubOAuth: vi.fn().mockResolvedValue({ user_code: 'ABCD-1234', verification_uri: 'https://github.com/login/device', browser_opened: true }),
    WaitGitHubOAuth: vi.fn().mockResolvedValue({ connected: true, login: 'octocat', name: 'The Octocat', client_id: 'Iv1.b507a08c87ecfe98', custom_client_id: false }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: showConfirmMock }),
}));

import {
    CancelGitHubOAuth,
    ConnectGitLab,
    DisconnectGitHub,
    DisconnectGitLab,
    GetGitHubConnection,
    GetGitLabConnection,
    GitHubAPIRequest,
    GitHubCloneRepository,
    OpenGitLabTokenPage,
    ShowItemInFolder,
    StartGitHubOAuth,
    WaitGitHubOAuth,
} from '../../../../wailsjs/go/main/App';
import { GitHubSettingsPanel } from '../GitHubSettingsPanel';

const renderPanel = () => render(
    <GitHubSettingsPanel
        config={{ github_client_id: '' } as any}
        lang="zh-Hans"
        saveRemoteConfigField={vi.fn().mockResolvedValue(undefined)}
        showToastMessage={vi.fn()}
    />,
);

beforeEach(() => {
    vi.clearAllMocks();
    showConfirmMock.mockResolvedValue(true);
    Object.assign(navigator, { clipboard: clipboardMock });
});

describe('GitHubSettingsPanel', () => {
    it('shows the sign-in button while disconnected', async () => {
        renderPanel();
        await waitFor(() => expect(GetGitHubConnection).toHaveBeenCalledWith(false));
        expect(screen.getByText('使用 GitHub 登录')).toBeTruthy();
    });

    it('starts the device flow and shows the user code', async () => {
        renderPanel();
        await waitFor(() => expect(GetGitHubConnection).toHaveBeenCalled());
        fireEvent.click(screen.getByText('使用 GitHub 登录'));
        await waitFor(() => expect(screen.getByText('ABCD-1234')).toBeTruthy());
        expect(StartGitHubOAuth).toHaveBeenCalled();
        expect(WaitGitHubOAuth).toHaveBeenCalled();
    });

    it('cancels an in-progress login', async () => {
        vi.mocked(WaitGitHubOAuth).mockReturnValue(new Promise(() => {}));
        renderPanel();
        await waitFor(() => expect(GetGitHubConnection).toHaveBeenCalled());
        fireEvent.click(screen.getByText('使用 GitHub 登录'));
        await waitFor(() => expect(screen.getByText('ABCD-1234')).toBeTruthy());
        fireEvent.click(screen.getByText('取消'));
        expect(CancelGitHubOAuth).toHaveBeenCalled();
    });

    it('cancels a pending login when unmounted', async () => {
        vi.mocked(WaitGitHubOAuth).mockReturnValue(new Promise(() => {}));
        const { unmount } = renderPanel();
        await waitFor(() => expect(GetGitHubConnection).toHaveBeenCalled());
        fireEvent.click(screen.getByText('使用 GitHub 登录'));
        await waitFor(() => expect(screen.getByText('ABCD-1234')).toBeTruthy());
        unmount();
        expect(CancelGitHubOAuth).toHaveBeenCalled();
    });

    it('renders the connected account and disconnects after confirm', async () => {
        vi.mocked(GetGitHubConnection).mockResolvedValue({
            connected: true, login: 'octocat', name: 'The Octocat',
            html_url: 'https://github.com/octocat', scopes: 'read:user repo',
            client_id: 'Iv1.x', custom_client_id: false,
        } as any);
        vi.mocked(GetGitHubConnection).mockResolvedValueOnce({
            connected: true, login: 'octocat', name: 'The Octocat',
            html_url: 'https://github.com/octocat', scopes: 'read:user repo',
            client_id: 'Iv1.x', custom_client_id: false,
        } as any).mockResolvedValue({ connected: false, client_id: 'Iv1.x', custom_client_id: false } as any);
        renderPanel();
        await waitFor(() => expect(screen.getByText('@octocat')).toBeTruthy());
        expect(screen.getByText(/Scopes: read:user repo/)).toBeTruthy();
        fireEvent.click(screen.getByText('断开连接'));
        await waitFor(() => expect(DisconnectGitHub).toHaveBeenCalled());
        expect(showConfirmMock).toHaveBeenCalled();
    });

    it('tests the API via /rate_limit', async () => {
        vi.mocked(GetGitHubConnection).mockResolvedValue({
            connected: true, login: 'octocat', client_id: 'Iv1.x', custom_client_id: false,
        } as any);
        renderPanel();
        await waitFor(() => expect(screen.getByText('测试连接')).toBeTruthy());
        fireEvent.click(screen.getByText('测试连接'));
        await waitFor(() => expect(GitHubAPIRequest).toHaveBeenCalledWith('GET', '/rate_limit'));
        await waitFor(() => expect(screen.getByText(/core 4999\/5000/)).toBeTruthy());
    });

    it('connects GitLab from a pasted token and renders the account', async () => {
        renderPanel();
        await waitFor(() => expect(GetGitLabConnection).toHaveBeenCalledWith(false));
        fireEvent.click(screen.getByText('打开令牌页面'));
        expect(OpenGitLabTokenPage).toHaveBeenCalledWith('https://gitlab.com');
        fireEvent.change(screen.getByLabelText('访问令牌'), { target: { value: 'glpat-xyz' } });
        fireEvent.click(screen.getByText('连接 GitLab'));
        await waitFor(() => expect(ConnectGitLab).toHaveBeenCalledWith('https://gitlab.com', 'glpat-xyz'));
        await waitFor(() => expect(screen.getByText('@tux')).toBeTruthy());
        // The token field unmounts once connected — the paste never lingers.
        expect(screen.queryByLabelText('访问令牌')).toBeNull();
    });

    it('disconnects GitLab after confirm', async () => {
        vi.mocked(GetGitLabConnection).mockResolvedValue({
            connected: true, login: 'tux', name: 'Tux',
            html_url: 'https://gitlab.com/tux', origin: 'https://gitlab.com', scopes: 'api',
        } as any);
        renderPanel();
        await waitFor(() => expect(screen.getByText('@tux')).toBeTruthy());
        fireEvent.click(screen.getAllByText('断开连接').find((node) => node.closest('section')?.textContent?.includes('GitLab'))!);
        await waitFor(() => expect(DisconnectGitLab).toHaveBeenCalled());
        expect(showConfirmMock).toHaveBeenCalled();
    });

    it('clones a repository and offers show-in-folder', async () => {
        renderPanel();
        await waitFor(() => expect(GetGitHubConnection).toHaveBeenCalled());
        fireEvent.change(screen.getByPlaceholderText('仓库地址或 owner/repo'), { target: { value: 'octocat/Hello-World' } });
        fireEvent.click(screen.getByText('克隆仓库'));
        await waitFor(() => expect(GitHubCloneRepository).toHaveBeenCalledWith('octocat/Hello-World', ''));
        await waitFor(() => expect(screen.getByText('在文件夹中显示')).toBeTruthy());
        fireEvent.click(screen.getByText('在文件夹中显示'));
        expect(ShowItemInFolder).toHaveBeenCalledWith('/data/github/repo');
    });
});
