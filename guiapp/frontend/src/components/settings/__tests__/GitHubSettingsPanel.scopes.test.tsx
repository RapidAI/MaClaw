// @vitest-environment jsdom
// Scope-warning coverage for the GitHub card, kept in its own file so it
// cannot collide with concurrent edits to GitHubSettingsPanel.test.tsx.
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelGitHubOAuth: vi.fn().mockResolvedValue(undefined),
    ConnectGitLab: vi.fn().mockResolvedValue({ connected: true, login: 'tux', name: 'Tux', origin: 'https://gitlab.com', scopes: 'api' }),
    DisconnectGitHub: vi.fn().mockResolvedValue(undefined),
    DisconnectGitLab: vi.fn().mockResolvedValue(undefined),
    GetGitHubConnection: vi.fn(),
    GetGitLabConnection: vi.fn().mockResolvedValue({ connected: false }),
    GitHubAPIRequest: vi.fn().mockResolvedValue('{"resources":{"core":{"limit":5000,"remaining":4999},"search":{"limit":30,"remaining":29}}}'),
    GitHubCloneRepository: vi.fn().mockResolvedValue({ output: '', dest_dir: '' }),
    OpenGitLabTokenPage: vi.fn().mockResolvedValue(undefined),
    ShowItemInFolder: vi.fn().mockResolvedValue(undefined),
    StartGitHubOAuth: vi.fn().mockResolvedValue({ user_code: 'ABCD-1234', verification_uri: 'https://github.com/login/device', browser_opened: true }),
    WaitGitHubOAuth: vi.fn().mockResolvedValue({ connected: true, client_id: '', custom_client_id: false }),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: vi.fn().mockResolvedValue(true) }),
}));

import { GetGitHubConnection } from '../../../../wailsjs/go/main/App';
import { GitHubSettingsPanel } from '../GitHubSettingsPanel';

function renderPanel() {
    return render(
        <GitHubSettingsPanel
            config={null}
            lang="zh"
            saveRemoteConfigField={() => Promise.resolve({})}
            showToastMessage={() => {}}
        />,
    );
}

describe('GitHub settings scope warning', () => {
    beforeEach(() => {
        vi.mocked(GetGitHubConnection).mockReset();
    });

    afterEach(() => {
        cleanup();
    });

    it('tells the user a ghu_ GitHub App token needs a fresh OAuth login', async () => {
        vi.mocked(GetGitHubConnection).mockResolvedValue({
            connected: true, login: 'ada', name: 'Ada', scopes: '', github_app_token: true,
            client_id: '178c6fc778ccc68e1d6a', custom_client_id: false,
        });
        const { container } = renderPanel();
        await waitFor(() => expect(screen.getByText('@ada')).toBeTruthy());
        expect(container.textContent).toContain('GitHub App 令牌');
        expect(container.textContent).toContain('断开后重新登录');
    });

    it('points at the authorization checkbox for an OAuth token without repo', async () => {
        vi.mocked(GetGitHubConnection).mockResolvedValue({
            connected: true, login: 'ada', name: 'Ada', scopes: 'read:user',
            client_id: '178c6fc778ccc68e1d6a', custom_client_id: false,
        });
        const { container } = renderPanel();
        await waitFor(() => expect(screen.getByText('@ada')).toBeTruthy());
        expect(container.textContent).toContain('Scopes: read:user');
        expect(container.textContent).toContain('没有仓库权限');
        expect(container.textContent).toContain('Repositories');
    });

    it('stays quiet when the token carries the repo scope', async () => {
        vi.mocked(GetGitHubConnection).mockResolvedValue({
            connected: true, login: 'ada', name: 'Ada', scopes: 'read:user,repo',
            client_id: '178c6fc778ccc68e1d6a', custom_client_id: false,
        });
        const { container } = renderPanel();
        await waitFor(() => expect(screen.getByText('@ada')).toBeTruthy());
        expect(container.textContent).toContain('Scopes: read:user,repo');
        expect(container.textContent).not.toContain('没有仓库权限');
        expect(container.textContent).not.toContain('GitHub App 令牌');
    });
});
