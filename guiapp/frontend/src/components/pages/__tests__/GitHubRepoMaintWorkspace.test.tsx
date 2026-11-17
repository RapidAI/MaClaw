// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

const connected = {
    connected: true,
    login: 'octocat',
    scopes: 'read:user repo',
    client_id: 'Iv1.example',
    custom_client_id: false,
};

const hub = {
    remote_hub_url: 'https://203.0.113.10',
    remote_machine_id: 'm1',
    remote_machine_token: 'machine-token',
};

const draftRule = {
    id: 'rule-1',
    repo_url: 'https://github.com/yucejade/jjquant-v2',
    owner: 'yucejade',
    repo: 'jjquant-v2',
    ghost_dir: 'D:/ghost',
    enabled: false,
    push_branch: '',
    commands: { 'issues.opened': '看一下议题' },
    enabled_kinds: { 'issues.opened': true },
    hook_ready: false,
    auth_blocked: false,
    last_error: '',
    queue_depth: 0,
    intake_ready: true,
};

function install(overrides: Record<string, unknown> = {}) {
    const app = {
        GetGitHubConnection: vi.fn().mockResolvedValue({ connected: false, client_id: 'Iv1.example', custom_client_id: false }),
        StartGitHubOAuth: vi.fn().mockResolvedValue({ user_code: 'ABCD-1234', verification_uri: 'https://github.com/login/device' }),
        WaitGitHubOAuth: vi.fn().mockResolvedValue(connected),
        LoadConfigForUI: vi.fn().mockResolvedValue(hub),
        ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([]),
        SaveGitHubRepoMaintRule: vi.fn(),
        SetGitHubRepoMaintEnabled: vi.fn().mockResolvedValue(undefined),
        ReplayGitHubRepoMaint: vi.fn().mockResolvedValue(undefined),
        GitHubAPIRequest: vi.fn().mockResolvedValue('{"private":false,"name":"jjquant-v2"}'),
        SelectWorkingDir: vi.fn().mockResolvedValue('D:/ghost'),
        GetACPHostStatus: vi.fn().mockRejectedValue(new Error('no backend')),
        ListExperts: vi.fn().mockResolvedValue('[]'),
        ...overrides,
    };
    (window as unknown as { go?: unknown }).go = { main: { App: app } };
    return app;
}

function openWorkspace(lang: string) {
    render(<UtilitiesPage lang={lang} mode="tools" />);
    fireEvent.click(screen.getByTestId('utilities-github-repo-maint-card'));
}

afterEach(() => {
    delete (window as unknown as { go?: unknown }).go;
});

describe('GitHub repository maintenance', () => {
    it('shows the tools card in zh-Hans, zh-Hant, and English', () => {
        install();
        const hans = render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        expect(screen.getByTestId('utilities-github-repo-maint-card').textContent).toContain('GitHub 仓库自动维护');
        hans.unmount();
        const hant = render(<UtilitiesPage lang="zh-Hant" mode="tools" />);
        expect(screen.getByTestId('utilities-github-repo-maint-card').textContent).toContain('GitHub 倉庫自動維護');
        expect(screen.getByTestId('utilities-github-repo-maint-card').textContent).toContain('進入');
        hant.unmount();
        const en = render(<UtilitiesPage lang="en" mode="tools" />);
        expect(screen.getByTestId('utilities-github-repo-maint-card').textContent).toContain('GitHub repository maintenance');
        expect(screen.getByTestId('utilities-github-repo-maint-card').textContent).toContain('Open');
        en.unmount();
    });

    it('opens a workspace that only takes back and the page language', async () => {
        install();
        openWorkspace('zh-Hans');
        expect(screen.getByTestId('github-repo-maint-workspace')).toBeTruthy();
        fireEvent.click(screen.getByTestId('github-repo-maint-back'));
        expect(screen.queryByTestId('github-repo-maint-workspace')).toBeNull();
        expect(screen.getByTestId('utilities-github-repo-maint-card')).toBeTruthy();
    });

    it('connects with the existing device-code login and has no token field', async () => {
        let finishLogin: (value: typeof connected) => void = () => {};
        const pending = new Promise<typeof connected>((resolve) => {
            finishLogin = resolve;
        });
        const app = install({ WaitGitHubOAuth: vi.fn().mockReturnValue(pending) });
        openWorkspace('en');
        fireEvent.click(screen.getByTestId('github-repo-maint-connect'));
        expect(await screen.findByTestId('github-repo-maint-user-code')).toHaveTextContent('ABCD-1234');
        expect(app.StartGitHubOAuth).toHaveBeenCalled();
        expect(app.WaitGitHubOAuth).toHaveBeenCalled();
        expect(document.querySelector('input[type="password"]')).toBeNull();
        expect(document.body.textContent || '').not.toContain('hook_secret');
        finishLogin(connected);
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-user-code')).toBeNull());
    });

    it('refuses to save an enabled event with an empty command', async () => {
        const app = install();
        openWorkspace('zh-Hans');
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: 'https://github.com/yucejade/jjquant-v2' } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: 'D:/ghost' } });
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        expect(await screen.findByTestId('github-repo-maint-error')).toHaveTextContent('命令');
        expect(app.SaveGitHubRepoMaintRule).not.toHaveBeenCalled();
    });

    it('asks before the first enable of a public repository, then offers new events or replay', async () => {
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-enable-rule-1'));
        expect(await screen.findByTestId('github-repo-maint-public-dialog')).toHaveTextContent('公开仓库');
        expect(app.SetGitHubRepoMaintEnabled).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('github-repo-maint-public-confirm'));
        expect(screen.getByTestId('github-repo-maint-choice-dialog')).toHaveTextContent('只处理之后的新事件');
        expect(screen.getByTestId('github-repo-maint-choice-dialog')).toHaveTextContent('回放未处理的');
        fireEvent.click(screen.getByTestId('github-repo-maint-choice-new'));
        await waitFor(() => expect(app.SetGitHubRepoMaintEnabled).toHaveBeenCalledWith('rule-1', true, 'new_only'));
        expect(document.body.textContent || '').not.toContain('hook_secret');
    });

    it('hides the master switch and replay while intake is not ready', async () => {
        install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([{ ...draftRule, intake_ready: false }]),
        });
        openWorkspace('zh-Hant');
        expect(await screen.findByTestId('github-repo-maint-rule-rule-1')).toBeTruthy();
        expect(screen.queryByTestId('github-repo-maint-enable-rule-1')).toBeNull();
        expect(screen.queryByTestId('github-repo-maint-replay-rule-1')).toBeNull();
        expect(screen.getByTestId('github-repo-maint-intake-off')).toHaveTextContent('接收尚未就緒');
    });
});
