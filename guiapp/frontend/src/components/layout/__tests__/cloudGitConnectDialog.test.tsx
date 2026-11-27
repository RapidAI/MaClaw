// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CloudGitConnectDialog } from '../CloudGitConnectDialog';
import { OPEN_SETTINGS_EVENT } from '../../../utils/settingsNavigation';

function setBindings(bindings: Record<string, (...args: never[]) => Promise<unknown>>) {
    (window as unknown as { go?: unknown }).go = { main: { App: bindings } };
}

describe('CloudGitConnectDialog', () => {
    afterEach(() => {
        cleanup();
        delete (window as unknown as { go?: unknown }).go;
    });

    it('uses the connected GitHub account automatically and connects the selected repository without a UI token round-trip', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/app' }));
        const listRepos = vi.fn(async (service?: string) => service === 'github' ? [
            { name: 'acme/app', description: 'Application', private: true, default_branch: 'main', updated_at: new Date(Date.now() - 26 * 3600 * 1000).toISOString() },
            { name: 'octo/lib', description: 'Library', private: false, default_branch: 'main' },
        ] : []);
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true,
                login: 'ada',
                name: 'Ada Lovelace',
                email: '42+ada@users.noreply.github.com',
                repo_scope: true,
            })),
            CloudAgentGitRepositories: listRepos,
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);

        expect(await screen.findByText('@ada')).toBeTruthy();
        expect(screen.queryByText('用 GitHub 账号')).toBeNull();
        const repoRow = await screen.findByText('acme/app');
        expect(screen.getByText('1 天前更新')).toBeTruthy();
        fireEvent.click(repoRow);
        expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('acme/app');
        fireEvent.click(screen.getByText('连接'));

        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][0]).toBe('cag_1');
        expect(connect.mock.calls[0][1]).toMatchObject({
            service: 'github',
            origin: 'https://github.com',
            repo: 'acme/app',
            author_name: 'Ada Lovelace',
            author_email: '42+ada@users.noreply.github.com',
            source: 'account',
        });
        expect(connect.mock.calls[0][1].token).toBe('');
        expect(listRepos).toHaveBeenCalledWith('github');
    });

    it('filters the repository list as the user types', async () => {
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true,
                login: 'ada',
                name: 'Ada',
                email: 'ada@example.com',
                repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => [
                { name: 'acme/app', description: 'Application' },
                { name: 'octo/lib', description: 'Library' },
            ]),
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('acme/app')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'lib' } });
        await waitFor(() => expect(screen.queryByText('acme/app')).toBeNull());
        expect(screen.getByText('octo/lib')).toBeTruthy();

        fireEvent.click(screen.getByText('octo/lib'));
        fireEvent.click(screen.getByLabelText('仓库'));
        expect(screen.getByText('acme/app')).toBeTruthy();
        expect(screen.getByText('octo/lib')).toBeTruthy();
    });

    it('keeps a usable typed path when repository discovery fails', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo' }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true,
                login: 'ada',
                name: 'Ada',
                email: 'ada@example.com',
                repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => {
                throw new Error('GitHub 仓库列表获取失败: HTTP 403: API rate limit exceeded');
            }),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('无法自动读取仓库列表')).toBeTruthy();
        expect(screen.getByText(/API rate limit/)).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({ repo: 'acme/demo', source: 'account' });
    });

    it('falls back to the manual path when no settings account is connected', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo' }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({ connected: false })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('未连接账号')).toBeTruthy();
        fireEvent.click(screen.getByText('手动令牌或服务器设置'));
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        fireEvent.change(screen.getByLabelText('提交作者'), { target: { value: 'Ada' } });
        fireEvent.change(screen.getByLabelText('提交邮箱'), { target: { value: 'ada@example.com' } });
        fireEvent.change(screen.getByLabelText('访问令牌'), { target: { value: 'manual-token' } });
        fireEvent.click(screen.getByText('连接'));

        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({
            repo: 'acme/demo',
            author_name: 'Ada',
            author_email: 'ada@example.com',
            token: 'manual-token',
        });
        expect(connect.mock.calls[0][1].source).toBeUndefined();
    });

    it('does not submit an incomplete form through the form submit path', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({ connected: false })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: connect,
        });

        const { container } = render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('未连接账号')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        const form = container.querySelector('form');
        expect(form).toBeTruthy();
        fireEvent.submit(form as HTMLFormElement);
        expect(connect).not.toHaveBeenCalled();
    });

    it('preserves a manual token entered while the account lookup is still pending', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo' }));
        let resolveAccount: (value: Record<string, unknown>) => void = () => undefined;
        const accountPromise = new Promise<Record<string, unknown>>((resolve) => {
            resolveAccount = resolve;
        });
        setBindings({
            CloudAgentGitHubAccount: vi.fn(() => accountPromise),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(screen.getByText('手动令牌或服务器设置'));
        fireEvent.change(screen.getByLabelText('访问令牌'), { target: { value: 'manual-token' } });
        resolveAccount({
            connected: true,
            login: 'ada',
            name: 'Ada',
            email: 'ada@example.com',
            repo_scope: true,
        });

        expect(await screen.findByText('@ada')).toBeTruthy();
        expect((screen.getByLabelText('访问令牌') as HTMLInputElement).value).toBe('manual-token');
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({
            repo: 'acme/demo',
            author_name: 'Ada',
            author_email: 'ada@example.com',
            token: 'manual-token',
        });
        expect(connect.mock.calls[0][1].source).toBeUndefined();
    });

    it('reuses fetched repositories when switching between connected services', async () => {
        const listRepos = vi.fn(async (service?: string) => service === 'gitlab'
            ? [{ name: 'group/app', description: 'GitLab app' }]
            : [{ name: 'octo/app', description: 'GitHub app' }]);
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitLabAccount: vi.fn(async () => ({
                connected: true, login: 'tux', name: 'Tux', email: 'tux@example.com', origin: 'https://gitlab.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: listRepos,
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('octo/app')).toBeTruthy();
        fireEvent.click(screen.getByText('GitLab'));
        expect(await screen.findByText('group/app')).toBeTruthy();
        fireEvent.click(screen.getByText('GitHub'));
        expect(await screen.findByText('octo/app')).toBeTruthy();
        expect(listRepos).toHaveBeenCalledTimes(2);
    });

    it('explains missing repository permission and still allows an explicit manual token fallback', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo' }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true,
                login: 'ada',
                name: 'Ada',
                email: 'ada@example.com',
                repo_scope: false,
                github_app_token: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('账号权限不完整')).toBeTruthy();
        expect(screen.getByText(/GitHub App 令牌/)).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        expect((screen.getByText('连接') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.click(screen.getByText('手动令牌或服务器设置'));
        fireEvent.change(screen.getByLabelText('提交作者'), { target: { value: 'Ada' } });
        fireEvent.change(screen.getByLabelText('提交邮箱'), { target: { value: 'ada@example.com' } });
        fireEvent.change(screen.getByLabelText('访问令牌'), { target: { value: 'manual-token' } });
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({ repo: 'acme/demo', token: 'manual-token' });
        expect(connect.mock.calls[0][1].source).toBeUndefined();
    });

    it('retries repository discovery after a transient failure', async () => {
        let attempts = 0;
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => {
                attempts += 1;
                if (attempts === 1) throw new Error('temporary network failure');
                return [{ name: 'acme/app', description: 'Application' }];
            }),
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('无法自动读取仓库列表')).toBeTruthy();
        fireEvent.click(screen.getByText('重试'));
        expect(await screen.findByText('acme/app')).toBeTruthy();
        expect(attempts).toBe(2);
    });

    it('asks for missing account commit identity instead of submitting an incomplete account connect', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/demo' }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: '', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('@ada')).toBeTruthy();
        expect(screen.getByLabelText('提交邮箱')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'acme/demo' } });
        expect((screen.getByText('连接') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(screen.getByLabelText('提交邮箱'), { target: { value: 'ada@example.com' } });
        expect((screen.getByText('连接') as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({
            repo: 'acme/demo',
            author_name: 'Ada',
            author_email: 'ada@example.com',
            source: 'account',
        });
    });

    it('deep-links to Settings when no account is connected', async () => {
        const onClose = vi.fn();
        const events: Array<{ tab?: string }> = [];
        const handler = (event: Event) => events.push((event as CustomEvent<{ tab?: string }>).detail);
        window.addEventListener(OPEN_SETTINGS_EVENT, handler);
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({ connected: false })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        try {
            render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={onClose} />);
            expect(await screen.findByText('未连接账号')).toBeTruthy();
            fireEvent.click(screen.getByText('去设置登录'));
            expect(events).toEqual([{ tab: 'github' }]);
            expect(onClose).toHaveBeenCalledTimes(1);
        } finally {
            window.removeEventListener(OPEN_SETTINGS_EVENT, handler);
        }
    });

    it('opens on GitLab when only GitLab is connected', async () => {
        const listRepos = vi.fn(async (service?: string) => service === 'gitlab'
            ? [{ name: 'group/app', description: 'GitLab app' }]
            : []);
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({ connected: false })),
            CloudAgentGitLabAccount: vi.fn(async () => ({
                connected: true, login: 'tux', name: 'Tux', email: 'tux@example.com', origin: 'https://gitlab.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: listRepos,
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('@tux')).toBeTruthy();
        expect(await screen.findByText('group/app')).toBeTruthy();
        expect(listRepos).toHaveBeenCalledWith('gitlab');
    });

    it('clears a stale connect error when the repository input changes', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => { throw new Error('这个实例已经有一个代码库'); });
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => [{ name: 'acme/app' }, { name: 'octo/lib' }]),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByText('acme/app'));
        fireEvent.click(screen.getByText('连接'));
        expect(await screen.findByRole('alert')).toBeTruthy();
        expect(screen.getByText(/这个实例已经有一个代码库/)).toBeTruthy();

        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'octo/lib' } });
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('probe stands down when the user already switched service manually', async () => {
        let resolveGitHub: (value: Record<string, unknown>) => void = () => undefined;
        const githubPromise = new Promise<Record<string, unknown>>((resolve) => {
            resolveGitHub = resolve;
        });
        const listRepos = vi.fn(async (service?: string) => service === 'gitlab' ? [{ name: 'group/app' }] : []);
        setBindings({
            CloudAgentGitHubAccount: vi.fn(() => githubPromise),
            CloudAgentGitLabAccount: vi.fn(async () => ({
                connected: true, login: 'tux', name: 'Tux', email: 'tux@example.com', origin: 'https://gitlab.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: listRepos,
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        // The mount probe is parked on the pending GitHub account read while
        // the user clicks over to GitLab and types a repository path.
        fireEvent.click(screen.getByText('GitLab'));
        expect(await screen.findByText('group/app')).toBeTruthy();
        fireEvent.change(screen.getByLabelText('仓库'), { target: { value: 'group/app' } });

        // Resolving GitHub as unusable NOW satisfies the probe's switch
        // condition; the serviceRef guard must stand down instead of
        // switchService wiping the typed repository path.
        resolveGitHub({ connected: false });
        await waitFor(() => expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('group/app'));
        expect(screen.getByText('@tux')).toBeTruthy();
        expect(listRepos).toHaveBeenCalledTimes(1);
        expect(listRepos).toHaveBeenCalledWith('gitlab');
    });

    it('prefills the instance bound repository and notes the existing binding', async () => {
        const connect = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'acme/existing' }));
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => [{ name: 'acme/existing' }, { name: 'acme/other' }]),
            GetCloudAgentGit: vi.fn(async () => ({ connected: true, service: 'github', repo: 'acme/existing' })),
            ConnectCloudAgentGit: connect,
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('@ada')).toBeTruthy();
        expect((await screen.findByTestId('cloud-git-bound-note')).textContent).toContain('该实例已绑定 acme/existing');
        expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('acme/existing');
        expect((screen.getByText('连接') as HTMLButtonElement).disabled).toBe(false);

        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(connect).toHaveBeenCalledTimes(1));
        expect(connect.mock.calls[0][1]).toMatchObject({ service: 'github', repo: 'acme/existing', source: 'account' });
    });

    it('only notes a binding that belongs to the other service', async () => {
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => [{ name: 'acme/app' }]),
            GetCloudAgentGit: vi.fn(async () => ({ connected: true, service: 'gitlab', repo: 'group/app' })),
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('@ada')).toBeTruthy();
        expect((await screen.findByTestId('cloud-git-bound-note')).textContent).toContain('该实例已绑定 group/app');
        expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('');
    });

});

describe('CloudGitConnectDialog create-new mode', () => {
    afterEach(() => {
        cleanup();
        delete (window as unknown as { go?: unknown }).go;
    });

    function createBindings(overrides: Record<string, (...args: never[]) => Promise<unknown>> = {}) {
        return {
            CloudAgentGitHubAccount: vi.fn(async () => ({
                connected: true, login: 'ada', name: 'Ada', email: 'ada@example.com', repo_scope: true,
            })),
            CloudAgentGitRepositories: vi.fn(async () => []),
            GitPlatformCreateCredentials: vi.fn(async () => [
                { id: '', kind: 'account', login: 'ada', name: 'Ada' },
                { id: 'vcred_1', kind: 'credential', name: 'GH PAT', login: 'ada' },
            ]),
            GitPlatformNamespaces: vi.fn(async () => ({
                rows: [
                    { key: 'ada', label: 'ada', kind: 'personal' },
                    { key: 'acme', label: 'acme', kind: 'org' },
                ],
            })),
            GitPlatformRepoNameStatus: vi.fn(async (_service?: string, _cred?: string, _owner?: string, name?: string) => (
                name === 'taken' ? { available: false, reason: '该仓库已存在' } : { available: true, reason: '' }
            )),
            GitPlatformCreateRepo: vi.fn(async (_input?: any) => ({
                full_name: 'ada/fresh', clone_url: 'https://github.com/ada/fresh.git', default_branch: 'main', private: true,
            })),
            ConnectCloudAgentGit: vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'ada/fresh' })),
            GetCloudAgentGit: vi.fn(async () => ({ connected: false })),
            ...overrides,
        };
    }

    it('creates a repository in the personal namespace and binds it with the settings account', async () => {
        const bindings = createBindings();
        setBindings(bindings);

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-git-create-toggle'));
        fireEvent.change(await screen.findByTestId('cloud-git-create-name'), { target: { value: 'fresh' } });

        // The availability suggestion names owner/name once the probe returns;
        // wait for the OK verdict specifically (the checking phase renders the
        // same test id).
        await waitFor(() => {
            const status = screen.getByTestId('cloud-git-create-status');
            expect(status.className).toContain('grc-availability--ok');
        }, { timeout: 3000 });
        expect(screen.getByTestId('cloud-git-create-status').textContent).toContain('ada/fresh');
        fireEvent.click(screen.getByTestId('cloud-git-create-submit'));

        await waitFor(() => expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledTimes(1));
        expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledWith(expect.objectContaining({
            service: 'github', credential_id: undefined, owner: 'ada', namespace_kind: 'personal', name: 'fresh', private: true,
        }));
        await waitFor(() => expect(bindings.ConnectCloudAgentGit).toHaveBeenCalledTimes(1));
        expect(bindings.ConnectCloudAgentGit.mock.calls[0][1]).toMatchObject({
            service: 'github', repo: 'ada/fresh', source: 'account', credential_id: undefined,
        });
    });

    it('binds with the picked credential (source=credential) when a stored credential is selected', async () => {
        const bindings = createBindings();
        setBindings(bindings);

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-git-create-toggle'));
        const credentialSelect = await screen.findByTestId('cloud-git-create-credential') as HTMLSelectElement;
        fireEvent.change(credentialSelect, { target: { value: 'vcred_1' } });
        // A credential switch reloads namespaces before the name can be typed.
        await waitFor(() => expect(bindings.GitPlatformNamespaces).toHaveBeenLastCalledWith('github', 'vcred_1'));
        fireEvent.change(screen.getByTestId('cloud-git-create-name'), { target: { value: 'fresh' } });
        await waitFor(() => {
            const status = screen.getByTestId('cloud-git-create-status');
            expect(status.className).toContain('grc-availability--ok');
        }, { timeout: 3000 });
        fireEvent.click(screen.getByTestId('cloud-git-create-submit'));

        await waitFor(() => expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledTimes(1));
        expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledWith(expect.objectContaining({
            service: 'github', credential_id: 'vcred_1',
        }));
        await waitFor(() => expect(bindings.ConnectCloudAgentGit).toHaveBeenCalledTimes(1));
        expect(bindings.ConnectCloudAgentGit.mock.calls[0][1]).toMatchObject({
            repo: 'ada/fresh', source: 'credential', credential_id: 'vcred_1',
        });
    });

    it('blocks creation while the name is taken and shows the provider reason', async () => {
        const bindings = createBindings();
        setBindings(bindings);

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-git-create-toggle'));
        fireEvent.change(await screen.findByTestId('cloud-git-create-name'), { target: { value: 'taken' } });

        await waitFor(() => {
            const status = screen.getByTestId('cloud-git-create-status');
            expect(status.className).toContain('grc-availability--bad');
        }, { timeout: 3000 });
        expect(screen.getByTestId('cloud-git-create-status').textContent).toContain('该仓库已存在');
        expect((screen.getByTestId('cloud-git-create-submit') as HTMLButtonElement).disabled).toBe(true);
        expect(bindings.GitPlatformCreateRepo).not.toHaveBeenCalled();
    });

    it('explains a failed permission probe instead of silently hiding creation', async () => {
        setBindings(createBindings({
            GitPlatformCreateCredentials: vi.fn(async () => { throw new Error('credential store unreadable'); }),
        }));

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByTestId('cloud-git-create-error')).toBeTruthy();
        expect(screen.getByTestId('cloud-git-create-error').textContent).toContain('credential store unreadable');
        expect(screen.queryByTestId('cloud-git-create-toggle')).toBeNull();
        // The pick flow keeps working despite the failure.
        expect(await screen.findByText('@ada')).toBeTruthy();
    });

    it('does not switch service when the user already typed a repository path', async () => {
        // Only GitLab usable; the probe would normally switch — but the user
        // typed a path on GitHub within the probe window, and switching would
        // wipe their input.
        setBindings({
            CloudAgentGitHubAccount: vi.fn(async () => ({ connected: false })),
            CloudAgentGitLabAccount: vi.fn(async () => ({ connected: true, login: 'ada', name: 'Ada', email: 'a@e.com', repo_scope: true })),
            CloudAgentGitRepositories: vi.fn(async (service?: string) => service === 'gitlab' ? [] : []),
            ConnectCloudAgentGit: vi.fn(async () => ({ connected: true })),
        });
        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        const repoInput = screen.getByLabelText('仓库');
        fireEvent.change(repoInput, { target: { value: 'typed/repo' } });
        await new Promise((resolve) => setTimeout(resolve, 30));
        expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('typed/repo');
    });

    it('keeps the credential reference when a create+bind fails and retries the bind from the pick list', async () => {
        const bindings = createBindings();
        bindings.ConnectCloudAgentGit = vi.fn(async (_id?: string, _opts?: any) => ({ connected: true, repo: 'ada/fresh' }))
            .mockRejectedValueOnce(new Error('hub unreachable'));
        setBindings(bindings);

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-git-create-toggle'));
        // Create with the STORED credential so the retry must carry its
        // reference (a settings-account retry legitimately falls back to the
        // account source).
        fireEvent.change(await screen.findByTestId('cloud-git-create-credential'), { target: { value: 'vcred_1' } });
        await waitFor(() => expect((screen.getByTestId('cloud-git-create-namespace') as HTMLSelectElement).disabled).toBe(false));
        fireEvent.change(screen.getByTestId('cloud-git-create-name'), { target: { value: 'fresh' } });
        await waitFor(() => {
            const status = screen.getByTestId('cloud-git-create-status');
            expect(status.className).toContain('grc-availability--ok');
        }, { timeout: 3000 });
        fireEvent.click(screen.getByTestId('cloud-git-create-submit'));

        // The bind failure surfaces and the dialog falls back to the pick list
        // with the created repository prefilled.
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('hub unreachable'));
        expect((screen.getByLabelText('仓库') as HTMLInputElement).value).toBe('ada/fresh');

        // 连接 retries the BIND (not the creation) with the same credential
        // reference; the provider is never asked to create it again.
        expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByText('连接'));
        await waitFor(() => expect(bindings.ConnectCloudAgentGit).toHaveBeenCalledTimes(2));
        expect(bindings.ConnectCloudAgentGit.mock.calls[1][1]).toMatchObject({
            repo: 'ada/fresh', source: 'credential', credential_id: 'vcred_1',
        });
        expect(bindings.GitPlatformCreateRepo).toHaveBeenCalledTimes(1);
    });

    it('keeps creation usable and explains a partial list when the org enumeration fails', async () => {
        const bindings = createBindings();
        bindings.GitPlatformNamespaces = vi.fn(async () => ({
            rows: [{ key: 'ada', label: 'ada', kind: 'personal' }],
            org_warning: 'github /user/orgs: quota exhausted',
        }));
        setBindings(bindings);

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('cloud-git-create-toggle'));
        expect(await screen.findByTestId('cloud-git-create-ns-warning')).toBeTruthy();
        expect(screen.getByTestId('cloud-git-create-ns-warning').textContent).toContain('quota exhausted');
        // The personal namespace stays usable despite the failure.
        expect((screen.getByTestId('cloud-git-create-namespace') as HTMLSelectElement).value).toBe('ada');
    });

    it('hides the create toggle when no usable credential exists', async () => {
        setBindings(createBindings({ GitPlatformCreateCredentials: vi.fn(async () => []) }));

        render(<CloudGitConnectDialog lang="zh" instanceId="cag_1" onClose={() => {}} />);
        expect(await screen.findByText('@ada')).toBeTruthy();
        expect(screen.queryByTestId('cloud-git-create-toggle')).toBeNull();
    });
});
