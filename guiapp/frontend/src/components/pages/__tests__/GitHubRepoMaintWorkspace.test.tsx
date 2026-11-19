// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
        CancelGitHubOAuth: vi.fn().mockResolvedValue(undefined),
        LoadConfigForUI: vi.fn().mockResolvedValue(hub),
        ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([]),
        SaveGitHubRepoMaintRule: vi.fn(),
        DeleteGitHubRepoMaintRule: vi.fn().mockResolvedValue(undefined),
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
    document.querySelectorAll('.app-viewport').forEach((element) => element.remove());
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
        expect((await screen.findByTestId('github-repo-maint-user-code')).textContent).toContain('ABCD-1234');
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
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: 'https://github.com/yucejade/jjquant-v2' } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: 'D:/ghost' } });
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        expect((await screen.findByTestId('github-repo-maint-editor')).textContent).toContain('打开的事件必须填写命令');
        expect(screen.queryByTestId('github-repo-maint-error')).toBeNull();
        expect(app.SaveGitHubRepoMaintRule).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('github-repo-maint-editor-cancel'));
        expect(screen.queryByTestId('github-repo-maint-error')).toBeNull();
    });

    it('asks before the first enable of a public repository, then offers new events or replay', async () => {
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-enable-rule-1'));
        expect((await screen.findByTestId('github-repo-maint-public-dialog')).textContent).toContain('公开仓库');
        expect(app.SetGitHubRepoMaintEnabled).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('github-repo-maint-public-confirm'));
        expect(screen.getByTestId('github-repo-maint-choice-dialog').textContent).toContain('只处理之后的新事件');
        expect(screen.getByTestId('github-repo-maint-choice-dialog').textContent).toContain('回放未处理的');
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
        expect(screen.getByTestId('github-repo-maint-intake-off').textContent).toContain('接收尚未就緒');
    });

    it('expands the command editor only for enabled events', async () => {
        install();
        openWorkspace('zh-Hans');
        expect(screen.queryByTestId('github-repo-maint-editor')).toBeNull();
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        expect(document.activeElement).toBe(screen.getByTestId('github-repo-maint-repo-url'));
        expect(screen.queryByTestId('github-repo-maint-command-issues.opened')).toBeNull();
        expect(screen.queryByTestId('github-repo-maint-command-push')).toBeNull();
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        expect(screen.getByTestId('github-repo-maint-command-issues.opened')).toBeTruthy();
        expect(screen.queryByTestId('github-repo-maint-command-push')).toBeNull();
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        expect(screen.queryByTestId('github-repo-maint-command-issues.opened')).toBeNull();
    });

    it('flags the offending event row when a command is missing and clears the flag on input', async () => {
        install();
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: 'https://github.com/yucejade/jjquant-v2' } });
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        const row = screen.getByTestId('github-repo-maint-kind-issues.opened').closest('.utilities-repomaint-event');
        expect(row?.className).toContain('utilities-repomaint-event--invalid');
        // The offending command editor takes focus and the error is repeated next
        // to the save button, where the failure actually happened.
        expect(document.activeElement).toBe(screen.getByTestId('github-repo-maint-command-issues.opened'));
        expect(screen.getByTestId('github-repo-maint-editor').textContent).toContain('打开的事件必须填写命令');
        fireEvent.change(screen.getByTestId('github-repo-maint-command-issues.opened'), { target: { value: 'maclaw 看一下' } });
        expect(row?.className).not.toContain('utilities-repomaint-event--invalid');
        expect(screen.getByTestId('github-repo-maint-editor').textContent).not.toContain('打开的事件必须填写命令');
    });

    it('opens a rule in an editor dialog and saves it as that same rule', async () => {
        const app = install({
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
            SaveGitHubRepoMaintRule: vi.fn().mockResolvedValue(draftRule),
        });
        openWorkspace('zh-Hans');
        expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull();
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        const dialog = await screen.findByTestId('github-repo-maint-editor-dialog');
        expect(dialog.getAttribute('role')).toBe('dialog');
        expect(screen.getByTestId('github-repo-maint-workspace').contains(dialog)).toBe(false);
        expect(document.body.contains(dialog)).toBe(true);
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).value).toBe(draftRule.repo_url);
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).readOnly).toBe(false);
        expect(screen.queryByTestId('github-repo-maint-identity-lock')).toBeNull();
        expect(screen.getByTestId('github-repo-maint-command-issues.opened')).toBeTruthy();
        expect(dialog.textContent).toContain('yucejade/jjquant-v2');
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        await waitFor(() => expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1));
        expect(app.SaveGitHubRepoMaintRule.mock.calls[0][0].id).toBe('rule-1');
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull());
    });

    it('cancel closes the editor dialog and a new rule starts blank', async () => {
        install({
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).value).toBe(draftRule.repo_url);
        fireEvent.click(screen.getByTestId('github-repo-maint-editor-cancel'));
        expect(screen.queryByTestId('github-repo-maint-editor')).toBeNull();
        fireEvent.click(screen.getByTestId('github-repo-maint-new'));
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).value).toBe('');
        expect(screen.getByTestId('github-repo-maint-editor').textContent).not.toContain('yucejade/jjquant-v2');
        expect(screen.queryByTestId('github-repo-maint-command-issues.opened')).toBeNull();
    });

    it('saves a second rule without the id of the rule already in the list', async () => {
        const second = {
            ...draftRule,
            id: 'rule-2',
            repo_url: 'https://github.com/octocat/hello',
            owner: 'octocat',
            repo: 'hello',
            ghost_dir: 'D:/hello',
            commands: {},
            enabled_kinds: {},
        };
        const app = install({
            ListGitHubRepoMaintRules: vi.fn()
                .mockResolvedValueOnce([])
                .mockResolvedValueOnce([draftRule])
                .mockResolvedValue([draftRule, second]),
            SaveGitHubRepoMaintRule: vi.fn()
                .mockResolvedValueOnce(draftRule)
                .mockResolvedValueOnce(second),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: draftRule.repo_url } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: draftRule.ghost_dir } });
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        await waitFor(() => expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1));
        expect(app.SaveGitHubRepoMaintRule.mock.calls[0][0].id).toBeUndefined();
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull());
        expect(await screen.findByTestId('github-repo-maint-rule-rule-1')).toBeTruthy();

        fireEvent.click(screen.getByTestId('github-repo-maint-new'));
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).value).toBe('');
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: second.repo_url } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: second.ghost_dir } });
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        await waitFor(() => expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(2));
        expect(app.SaveGitHubRepoMaintRule.mock.calls[1][0].id).toBeUndefined();
        expect(app.SaveGitHubRepoMaintRule.mock.calls[1][0].repo_url).toBe(second.repo_url);
        expect(await screen.findByTestId('github-repo-maint-rule-rule-2')).toBeTruthy();
        expect(screen.getByTestId('github-repo-maint-rule-rule-1')).toBeTruthy();
    });

    it('mounts the editor on the viewport, outside the scaled page', async () => {
        const viewport = document.createElement('div');
        viewport.className = 'app-viewport';
        const scaleLayer = document.createElement('div');
        scaleLayer.className = 'app-scale-layer';
        viewport.appendChild(scaleLayer);
        document.body.appendChild(viewport);
        install({ ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]) });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        const dialog = await screen.findByTestId('github-repo-maint-editor-dialog');
        expect(screen.getByTestId('github-repo-maint-workspace').contains(dialog)).toBe(false);
        expect(scaleLayer.contains(dialog)).toBe(false);
        expect(dialog.parentElement).toBe(viewport);
    });

    it('saves a new rule once when save is clicked twice before the request settles', async () => {
        let finishSave: (rule: typeof draftRule) => void = () => {};
        const pending = new Promise<typeof draftRule>((resolve) => {
            finishSave = resolve;
        });
        const app = install({
            ListGitHubRepoMaintRules: vi.fn()
                .mockResolvedValueOnce([])
                .mockResolvedValue([draftRule]),
            SaveGitHubRepoMaintRule: vi.fn().mockReturnValue(pending),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: draftRule.repo_url } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: draftRule.ghost_dir } });
        const saveButton = screen.getByTestId('github-repo-maint-save');
        fireEvent.click(saveButton);
        fireEvent.click(saveButton);
        expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
        expect(app.SaveGitHubRepoMaintRule.mock.calls[0][0].id).toBeUndefined();
        finishSave(draftRule);
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull());
        expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
    });

    it('returns focus to the opener and keeps Tab inside the editor', async () => {
        install({ ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]) });
        openWorkspace('zh-Hans');
        const edit = await screen.findByTestId('github-repo-maint-edit-rule-1');
        edit.focus();
        fireEvent.click(edit);
        const url = await screen.findByTestId('github-repo-maint-repo-url');
        expect(document.activeElement).toBe(url);
        expect(screen.getByTestId('github-repo-maint-workspace').hasAttribute('inert')).toBe(true);
        fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(screen.getByTestId('github-repo-maint-save'));
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(url);
        fireEvent.click(screen.getByTestId('github-repo-maint-editor-cancel'));
        expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull();
        expect(document.activeElement).toBe(edit);
        expect(screen.getByTestId('github-repo-maint-workspace').hasAttribute('inert')).toBe(false);
    });

    it('keeps the editor open when Escape is pressed while a save is in flight', async () => {
        let finishSave: (rule: typeof draftRule) => void = () => {};
        const pending = new Promise<typeof draftRule>((resolve) => {
            finishSave = resolve;
        });
        const app = install({
            ListGitHubRepoMaintRules: vi.fn()
                .mockResolvedValueOnce([])
                .mockResolvedValue([draftRule]),
            SaveGitHubRepoMaintRule: vi.fn().mockReturnValue(pending),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.change(screen.getByTestId('github-repo-maint-repo-url'), { target: { value: draftRule.repo_url } });
        fireEvent.change(screen.getByTestId('github-repo-maint-ghost-dir'), { target: { value: draftRule.ghost_dir } });
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.getByTestId('github-repo-maint-editor-dialog')).toBeTruthy();
        finishSave(draftRule);
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull());
        expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
    });

    it('closes the editor dialog with Escape without saving', async () => {
        const app = install({
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        expect(await screen.findByTestId('github-repo-maint-editor-dialog')).toBeTruthy();
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.queryByTestId('github-repo-maint-editor-dialog')).toBeNull();
        expect(app.SaveGitHubRepoMaintRule).not.toHaveBeenCalled();
    });

    it('disables a new rule once 20 are saved', async () => {
        const many = Array.from({ length: 20 }, (_, index) => ({
            ...draftRule,
            id: `rule-${index}`,
            repo: `repo-${index}`,
            ghost_dir: `D:/ghost-${index}`,
        }));
        install({ ListGitHubRepoMaintRules: vi.fn().mockResolvedValue(many) });
        openWorkspace('zh-Hans');
        await waitFor(() => expect((screen.getByTestId('github-repo-maint-new') as HTMLButtonElement).disabled).toBe(true));
        expect(screen.getByTestId('github-repo-maint-cap').textContent).toContain('20');
    });

    it('keeps the repository and directory fixed while an enabled rule is edited', async () => {
        const enabled = { ...draftRule, enabled: true, hook_ready: true };
        const app = install({
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([enabled]),
            SaveGitHubRepoMaintRule: vi.fn().mockResolvedValue(enabled),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        const url = screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement;
        const dir = screen.getByTestId('github-repo-maint-ghost-dir') as HTMLInputElement;
        expect(url.readOnly).toBe(true);
        expect(url.value).toBe(enabled.repo_url);
        expect(dir.readOnly).toBe(true);
        expect(dir.value).toBe(enabled.ghost_dir);
        expect((screen.getByTestId('github-repo-maint-pick-dir') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByTestId('github-repo-maint-identity-lock').textContent).toContain('请先关闭总开关');
        const command = screen.getByTestId('github-repo-maint-command-issues.opened');
        expect(document.activeElement).toBe(command);
        fireEvent.change(command, { target: { value: '继续处理这个议题' } });
        fireEvent.click(screen.getByTestId('github-repo-maint-save'));
        await waitFor(() => expect(app.SaveGitHubRepoMaintRule).toHaveBeenCalledTimes(1));
        const payload = app.SaveGitHubRepoMaintRule.mock.calls[0][0];
        expect(payload.id).toBe('rule-1');
        expect(payload.repo_url).toBe(enabled.repo_url);
        expect(payload.ghost_dir).toBe(enabled.ghost_dir);
        expect(payload.commands['issues.opened']).toBe('继续处理这个议题');
    });

    it('focuses the first event when an enabled rule has no command yet', async () => {
        const enabled = { ...draftRule, enabled: true, commands: {}, enabled_kinds: {} };
        install({ ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([enabled]) });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-edit-rule-1'));
        expect(document.activeElement).toBe(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        expect((screen.getByTestId('github-repo-maint-repo-url') as HTMLInputElement).readOnly).toBe(true);
        expect(screen.queryByTestId('github-repo-maint-command-issues.opened')).toBeNull();
    });

    it('checks repository privacy once when enable is clicked twice', async () => {
        let finishApi: (value: string) => void = () => {};
        const pending = new Promise<string>((resolve) => {
            finishApi = resolve;
        });
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
            GitHubAPIRequest: vi.fn().mockReturnValue(pending),
        });
        openWorkspace('zh-Hans');
        const box = await screen.findByTestId('github-repo-maint-enable-rule-1');
        act(() => {
            box.click();
            box.click();
        });
        expect(app.GitHubAPIRequest).toHaveBeenCalledTimes(1);
        expect(app.GitHubAPIRequest).toHaveBeenCalledWith('GET', '/repos/yucejade/jjquant-v2');
        expect(app.SetGitHubRepoMaintEnabled).not.toHaveBeenCalled();
        await act(async () => {
            finishApi('{"private":false}');
        });
        expect(await screen.findByTestId('github-repo-maint-public-dialog')).toBeTruthy();
        expect(app.SetGitHubRepoMaintEnabled).not.toHaveBeenCalled();
        expect(app.GitHubAPIRequest).toHaveBeenCalledTimes(1);
    });

    it('returns focus to the enable switch after the privacy check opens the public gate', async () => {
        let finishApi: (value: string) => void = () => {};
        const pending = new Promise<string>((resolve) => {
            finishApi = resolve;
        });
        install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
            GitHubAPIRequest: vi.fn().mockReturnValue(pending),
        });
        openWorkspace('zh-Hans');
        const box = await screen.findByTestId('github-repo-maint-enable-rule-1');
        box.focus();
        fireEvent.click(box);
        expect((box as HTMLInputElement).disabled).toBe(true);
        // A disabled focused control loses focus in the browser. jsdom keeps
        // it, so drop it here before the privacy request settles.
        box.blur();
        await act(async () => {
            finishApi('{"private":false}');
        });
        expect(await screen.findByTestId('github-repo-maint-public-dialog')).toBeTruthy();
        fireEvent.click(screen.getByTestId('github-repo-maint-public-cancel'));
        expect(document.activeElement).toBe(box);
    });

    it('closes the public-repository gate with Escape without enabling', async () => {
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-enable-rule-1'));
        expect(await screen.findByTestId('github-repo-maint-public-dialog')).toBeTruthy();
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.queryByTestId('github-repo-maint-public-dialog')).toBeNull();
        expect(app.SetGitHubRepoMaintEnabled).not.toHaveBeenCalled();
    });

    it('labels the command editor and tracks the armed count per event group', async () => {
        install();
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-new'));
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        const command = screen.getByTestId('github-repo-maint-command-issues.opened');
        expect(screen.getByLabelText('命令')).toBe(command);
        expect(screen.getByTestId('github-repo-maint-editor').textContent).toContain('1/4');
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-push'));
        expect(screen.getByLabelText('分支过滤')).toBe(screen.getByTestId('github-repo-maint-push-branch'));
        expect(screen.getByTestId('github-repo-maint-editor').textContent).toContain('1/6');
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-issues.opened'));
        fireEvent.click(screen.getByTestId('github-repo-maint-kind-push'));
        expect(screen.getByTestId('github-repo-maint-editor').textContent).toContain('0/4');
    });

    it('copies the device code to the clipboard from the login panel', async () => {
        let finishLogin: (value: typeof connected) => void = () => {};
        const pending = new Promise<typeof connected>((resolve) => {
            finishLogin = resolve;
        });
        install({ WaitGitHubOAuth: vi.fn().mockReturnValue(pending) });
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.assign(navigator, { clipboard: { writeText } });
        openWorkspace('en');
        fireEvent.click(screen.getByTestId('github-repo-maint-connect'));
        fireEvent.click(await screen.findByTestId('github-repo-maint-copy-code'));
        await waitFor(() => expect(writeText).toHaveBeenCalledWith('ABCD-1234'));
        expect(screen.getByTestId('github-repo-maint-copy-code').textContent).toBe('Copied');
        finishLogin(connected);
    });

    it('cancels a pending device-code login from the waiting panel', async () => {
        const app = install({
            WaitGitHubOAuth: vi.fn().mockReturnValue(new Promise(() => {})),
        });
        openWorkspace('en');
        fireEvent.click(screen.getByTestId('github-repo-maint-connect'));
        await screen.findByTestId('github-repo-maint-user-code');
        fireEvent.click(screen.getByTestId('github-repo-maint-cancel-connect'));
        await waitFor(() => expect(app.CancelGitHubOAuth).toHaveBeenCalled());
        expect(screen.queryByTestId('github-repo-maint-user-code')).toBeNull();
        // A cancelled login stays quiet: no failure banner appears.
        expect(screen.queryByTestId('github-repo-maint-error')).toBeNull();
    });

    it('clears the device-code panel when the login wait fails', async () => {
        install({
            WaitGitHubOAuth: vi.fn().mockRejectedValue(new Error('device flow failed')),
        });
        openWorkspace('en');
        fireEvent.click(screen.getByTestId('github-repo-maint-connect'));
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-user-code')).toBeNull());
        expect((await screen.findByTestId('github-repo-maint-error')).textContent).toContain('device flow failed');
    });

    it('deletes a rule through the confirmation dialog and refreshes the list', async () => {
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn()
                .mockResolvedValueOnce([draftRule])
                .mockResolvedValue([]),
            DeleteGitHubRepoMaintRule: vi.fn().mockResolvedValue(undefined),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-rule-1'));
        const dialog = await screen.findByTestId('github-repo-maint-delete-dialog');
        expect(dialog.textContent).toContain('jjquant-v2');
        expect(dialog.textContent).toContain('webhook');
        fireEvent.click(screen.getByTestId('github-repo-maint-delete-cancel'));
        expect(screen.queryByTestId('github-repo-maint-delete-dialog')).toBeNull();
        expect(app.DeleteGitHubRepoMaintRule).not.toHaveBeenCalled();
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-rule-1'));
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(screen.queryByTestId('github-repo-maint-delete-dialog')).toBeNull();
        expect(app.DeleteGitHubRepoMaintRule).not.toHaveBeenCalled();
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-rule-1'));
        fireEvent.click(screen.getByTestId('github-repo-maint-delete-confirm'));
        await waitFor(() => expect(app.DeleteGitHubRepoMaintRule).toHaveBeenCalledWith('rule-1'));
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-rule-rule-1')).toBeNull());
    });

    it('keeps the delete dialog until the first request settles when confirm is clicked twice', async () => {
        let finishDelete: () => void = () => {};
        const pending = new Promise<void>((resolve) => {
            finishDelete = resolve;
        });
        const app = install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn()
                .mockResolvedValueOnce([draftRule])
                .mockResolvedValue([]),
            DeleteGitHubRepoMaintRule: vi.fn().mockReturnValue(pending),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-rule-1'));
        const confirm = await screen.findByTestId('github-repo-maint-delete-confirm');
        fireEvent.click(confirm);
        fireEvent.click(confirm);
        expect(app.DeleteGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('github-repo-maint-delete-dialog')).toBeTruthy();
        finishDelete();
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-delete-dialog')).toBeNull());
        expect(app.DeleteGitHubRepoMaintRule).toHaveBeenCalledTimes(1);
    });

    it('closes the delete dialog and reports the retry notice when the backend delete fails', async () => {
        install({
            GetGitHubConnection: vi.fn().mockResolvedValue(connected),
            ListGitHubRepoMaintRules: vi.fn().mockResolvedValue([draftRule]),
            DeleteGitHubRepoMaintRule: vi.fn().mockRejectedValue(new Error('删除未完成，将在下次重试')),
        });
        openWorkspace('zh-Hans');
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-rule-1'));
        fireEvent.click(await screen.findByTestId('github-repo-maint-delete-confirm'));
        expect((await screen.findByTestId('github-repo-maint-error')).textContent).toContain('删除未完成');
        await waitFor(() => expect(screen.queryByTestId('github-repo-maint-delete-dialog')).toBeNull());
        // A pending delete keeps the rule listed so the retry can happen later.
        expect(screen.getByTestId('github-repo-maint-rule-rule-1')).toBeTruthy();
    });
});
