// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CloudGitConnectDialog } from '../CloudGitConnectDialog';

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
            { name: 'acme/app', description: 'Application', private: true, default_branch: 'main' },
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

});
