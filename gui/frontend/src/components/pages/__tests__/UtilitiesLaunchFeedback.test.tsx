// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

const BrowserOpenURLMock = vi.fn();

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

/**
 * The launch handler prefers the wailsjs binding, which delegates to
 * window.go.main.App.LaunchVSCodeWithACP — stub that native endpoint.
 */
const stubNativeLaunch = (impl: () => Promise<unknown>) => {
    (window as any).go = { main: { App: { LaunchVSCodeWithACP: impl } } };
};

afterEach(() => {
    delete (window as any).go;
});

describe('UtilitiesPage VS Code launch feedback', () => {
    it('renders success with steps and warnings as structured lists', async () => {
        stubNativeLaunch(vi.fn().mockResolvedValue({
            ok: true,
            message: '已启动 VS Code',
            steps: ['Gateway 已开启', 'ACP Bridge 已配置'],
            warnings: ['端口 18789 被占用，已自动切换'],
        }));
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        const panel = await screen.findByTestId('utilities-vscode-hint');
        expect(panel.getAttribute('role')).toBe('status');
        expect(panel.className).toContain('utilities-launch-feedback--success');
        expect(screen.getByText('已启动 VS Code')).toBeTruthy();
        expect(screen.getByText('Gateway 已开启')).toBeTruthy();
        expect(screen.getByText('ACP Bridge 已配置')).toBeTruthy();
        expect(screen.getByText('端口 18789 被占用，已自动切换')).toBeTruthy();
    });

    it('renders launch failure as an alert with warnings', async () => {
        stubNativeLaunch(vi.fn().mockResolvedValue({
            ok: false,
            message: '启动失败',
            warnings: ['未检测到 VS Code 安装'],
        }));
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        const panel = await screen.findByTestId('utilities-vscode-hint');
        expect(panel.getAttribute('role')).toBe('alert');
        expect(panel.className).toContain('utilities-launch-feedback--error');
        expect(screen.getByText('启动失败')).toBeTruthy();
        expect(screen.getByText('未检测到 VS Code 安装')).toBeTruthy();
    });

    it('renders binding errors as an alert panel', async () => {
        stubNativeLaunch(vi.fn().mockRejectedValue(new Error('boom')));
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        const panel = await screen.findByTestId('utilities-vscode-hint');
        expect(panel.getAttribute('role')).toBe('alert');
        expect(screen.getByText('boom')).toBeTruthy();
    });

    it('dismisses the panel via the close button', async () => {
        stubNativeLaunch(vi.fn().mockResolvedValue({ ok: true, message: '已启动 VS Code', steps: [], warnings: [] }));
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        await screen.findByTestId('utilities-vscode-hint');
        fireEvent.click(screen.getByLabelText('关闭提示'));
        await waitFor(() => expect(screen.queryByTestId('utilities-vscode-hint')).toBeNull());
    });

    it('auto-dismisses success feedback after 12s', async () => {
        vi.useFakeTimers();
        try {
            stubNativeLaunch(vi.fn().mockResolvedValue({ ok: true, message: '已启动 VS Code', steps: [], warnings: [] }));
            render(<UtilitiesPage lang="zh-Hans" />);
            fireEvent.click(screen.getByTestId('utilities-vscode-card'));
            // Flush the async launch chain (dynamic import + stub promise).
            await act(async () => { await vi.advanceTimersByTimeAsync(0); });
            expect(screen.queryByTestId('utilities-vscode-hint')).toBeTruthy();
            await act(async () => { await vi.advanceTimersByTimeAsync(11999); });
            expect(screen.queryByTestId('utilities-vscode-hint')).toBeTruthy();
            await act(async () => { await vi.advanceTimersByTimeAsync(1); });
            expect(screen.queryByTestId('utilities-vscode-hint')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('keeps error feedback until dismissed (no auto-dismiss)', async () => {
        vi.useFakeTimers();
        try {
            stubNativeLaunch(vi.fn().mockResolvedValue({ ok: false, message: '启动失败', warnings: [] }));
            render(<UtilitiesPage lang="zh-Hans" />);
            fireEvent.click(screen.getByTestId('utilities-vscode-card'));
            await act(async () => { await vi.advanceTimersByTimeAsync(0); });
            expect(screen.queryByTestId('utilities-vscode-hint')).toBeTruthy();
            await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
            expect(screen.queryByTestId('utilities-vscode-hint')).toBeTruthy();
        } finally {
            vi.useRealTimers();
        }
    });
});

describe('UtilitiesPage VS Code missing install prompt', () => {
    it('prompts and opens the download page on confirm (ACP card)', async () => {
        BrowserOpenURLMock.mockClear();
        stubNativeLaunch(vi.fn().mockResolvedValue({
            ok: false,
            needVSCodeInstall: true,
            vscodeDownloadURL: 'https://code.visualstudio.com/Download',
        }));
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        expect(await screen.findByText('未检测到 VS Code')).toBeTruthy();
        fireEvent.click(screen.getByText('打开下载页'));
        expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://code.visualstudio.com/Download');
        await waitFor(() => expect(screen.queryByText('未检测到 VS Code')).toBeNull());
    });

    it('prompts from the extension card and closes without opening on cancel', async () => {
        BrowserOpenURLMock.mockClear();
        (window as any).go = {
            main: {
                App: {
                    LaunchVSCodeWithACPExtension: vi.fn().mockResolvedValue({
                        ok: false,
                        needVSCodeInstall: true,
                        vscodeDownloadURL: 'https://code.visualstudio.com/Download',
                    }),
                },
            },
        };
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-ext-card'));
        expect(await screen.findByText('未检测到 VS Code')).toBeTruthy();
        fireEvent.click(screen.getByText('取消'));
        expect(BrowserOpenURLMock).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.queryByText('未检测到 VS Code')).toBeNull());
    });
});

describe('UtilitiesPage VS Code extension card (first-party)', () => {
    const stubBoth = (implAcp: () => Promise<unknown>, implExt: () => Promise<unknown>) => {
        (window as any).go = {
            main: { App: { LaunchVSCodeWithACP: implAcp, LaunchVSCodeWithACPExtension: implExt } },
        };
    };

    it('dispatches to LaunchVSCodeWithACPExtension and renders success', async () => {
        const extFn = vi.fn().mockResolvedValue({
            ok: true,
            message: 'VS Code 已启动（扩展）',
            steps: ['ensured extension maclaw.maclaw-acp@0.1.0 (bottom panel)'],
            warnings: [],
        });
        stubBoth(vi.fn(), extFn);
        render(<UtilitiesPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTestId('utilities-vscode-ext-card'));
        const panel = await screen.findByTestId('utilities-vscode-hint');
        expect(extFn).toHaveBeenCalledTimes(1);
        expect(panel.className).toContain('utilities-launch-feedback--success');
        expect(screen.getByText('ensured extension maclaw.maclaw-acp@0.1.0 (bottom panel)')).toBeTruthy();
    });

    it('serializes both launch cards while one is in flight', async () => {
        let resolveLaunch: (value: unknown) => void = () => {};
        const acpFn = vi.fn().mockImplementation(
            () => new Promise((r) => { resolveLaunch = r; }),
        );
        const extFn = vi.fn().mockResolvedValue({ ok: true, message: 'ok', steps: [], warnings: [] });
        stubBoth(acpFn, extFn);
        render(<UtilitiesPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTestId('utilities-vscode-card'));
        // While the ACP launch is in flight, the extension card is disabled…
        await waitFor(() =>
            expect((screen.getByTestId('utilities-vscode-ext-card') as HTMLButtonElement).disabled).toBe(true));
        // …and clicking it must not start a concurrent launch.
        fireEvent.click(screen.getByTestId('utilities-vscode-ext-card'));
        expect(extFn).not.toHaveBeenCalled();

        resolveLaunch({ ok: true, message: 'done', steps: [], warnings: [] });
        await screen.findByTestId('utilities-vscode-hint');
        await waitFor(() =>
            expect((screen.getByTestId('utilities-vscode-ext-card') as HTMLButtonElement).disabled).toBe(false));
    });
});
