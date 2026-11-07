// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';
import { ClipboardSetText } from '../../../../wailsjs/runtime';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
    BrowserOpenURL: vi.fn(),
    ClipboardSetText: vi.fn().mockResolvedValue(true),
}));

function install(overrides: Record<string, unknown> = {}) {
    const installSpy = vi.fn().mockResolvedValue({
        ok: true,
        browsers: [
            { browser: 'chrome', state: 'failed', detail: 'no-store-url' },
            { browser: 'edge', state: 'not-installed', detail: 'not-installed' },
            { browser: 'safari', state: 'absent' },
        ],
    });
    const uninstallSpy = vi.fn().mockResolvedValue({
        ok: true,
        browsers: [
            { browser: 'chrome', state: 'not-installed', detail: 'not-installed' },
            { browser: 'edge', state: 'not-installed', detail: 'not-installed' },
        ],
    });
    (window as any).go = {
        main: {
            App: {
                ListExperts: vi.fn().mockResolvedValue('[]'),
                GetACPHostStatus: vi.fn().mockRejectedValue(new Error('no backend')),
                InstallBrowserExtensions: installSpy,
                UninstallBrowserExtensions: uninstallSpy,
                GetBrowserExtensionStatus: vi.fn().mockResolvedValue({ ok: true, browsers: [] }),
                ...overrides,
            },
        },
    };
    return { installSpy, uninstallSpy };
}

const originalClipboard = Object.getOwnPropertyDescriptor(Navigator.prototype, 'clipboard')
    ?? Object.getOwnPropertyDescriptor(navigator, 'clipboard');

describe('browser extension tool card', () => {
    afterEach(() => {
        delete (window as any).go;
        vi.useRealTimers();
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText).mockResolvedValue(true);
        if (originalClipboard) {
            Object.defineProperty(navigator, 'clipboard', originalClipboard);
        } else {
            delete (navigator as any).clipboard;
        }
    });

    it('shows the card on tools and combined, and hides it on experts', async () => {
        install();
        const { rerender } = render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        expect(screen.getByTestId('utilities-browser-extension-card')).toBeTruthy();
        expect(screen.getByText('浏览器插件')).toBeTruthy();
        rerender(<UtilitiesPage lang="zh-Hans" mode="combined" />);
        expect(screen.getByTestId('utilities-browser-extension-card')).toBeTruthy();
        rerender(<UtilitiesPage lang="zh-Hans" mode="experts" />);
        await waitFor(() => expect(screen.queryByTestId('utilities-browser-extension-card')).toBeNull());
    });

    it('registers installed browsers and does not draw an absent Safari row', async () => {
        const { installSpy, uninstallSpy } = install();
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(installSpy).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId('browser-extension-panel')).toBeTruthy();
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('当前构建没有商店更新地址，扩展不会自动装上');
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('安装');
        expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('未安装');
        expect(screen.queryByTestId('browser-extension-install-edge')).toBeNull();
        expect(screen.queryByTestId('browser-extension-row-safari')).toBeNull();
        expect(screen.queryByText(/download|下载页/i)).toBeNull();
        fireEvent.click(screen.getByTestId('browser-extension-install-chrome'));
        await waitFor(() => expect(installSpy).toHaveBeenCalledTimes(2));
        fireEvent.click(screen.getByTestId('browser-extension-remove-chrome'));
        await waitFor(() => expect(uninstallSpy).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('未安装');
    });

    it('shows the unpacked folder when the browser must load it', async () => {
        const reveal = vi.fn().mockResolvedValue(undefined);
        const openPage = vi.fn().mockResolvedValue(undefined);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper',
                browsers: [
                    { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
            RevealBrowserExtensionFolder: reveal,
            ShowBrowserExtensionLoad: openPage,
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-unpacked')).toBeTruthy());
        const row = screen.getByTestId('browser-extension-row-edge').textContent || '';
        expect(row).toContain('开发者模式');
        expect(row).toContain('保持开启');
        expect(row).toContain('Load unpacked');
        expect(row).toContain('加载已解压的扩展程序');
        expect(row).toContain('加载解压缩');
        expect(row).not.toContain('加载未打包');
        expect(row).toContain('未加载');
        expect(row).not.toContain('待确认');
        expect(screen.getByTestId('browser-extension-install-edge').textContent).toContain('打开扩展页');
        expect(screen.getByTestId('browser-extension-unpacked').textContent).toContain('companion-paper');
        expect(screen.getByTestId('browser-extension-show-folder').textContent).toContain('打开文件夹');
        fireEvent.click(screen.getByTestId('browser-extension-show-folder'));
        await waitFor(() => expect(reveal).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('browser-extension-install-edge'));
        await waitFor(() => expect(openPage).toHaveBeenCalledWith('edge'));
    });

    it('copies the plugin directory beside the path, not its parent', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper\\';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText).mockResolvedValue(true);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: plugin,
                browsers: [
                    { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-copy-path')).toBeTruthy());
        const copyBtn = screen.getByTestId('browser-extension-copy-path');
        const path = copyBtn.parentElement?.querySelector('.browser-ext__path');
        expect(path?.nextElementSibling).toBe(copyBtn);
        expect(path?.textContent).toContain('companion-paper');
        expect(copyBtn.textContent).toContain('复制');
        fireEvent.click(copyBtn);
        const expected = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper';
        await waitFor(() => expect(ClipboardSetText).toHaveBeenCalledWith(expected));
        expect(vi.mocked(ClipboardSetText).mock.calls[0][0]).not.toBe(parent);
        expect(String(vi.mocked(ClipboardSetText).mock.calls[0][0]).endsWith('\\companion-paper')).toBe(true);
        await waitFor(() => expect(copyBtn.textContent).toBe('已复制'));
        expect(path?.textContent).toBe(expected);
    });

    it('does not restore the copied label after a newer folder result', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        const uninstall = vi.fn().mockResolvedValue({
            ok: true,
            unpackedPath: plugin + '\\',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        });
        vi.mocked(ClipboardSetText).mockResolvedValue(true);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: plugin,
                browsers: [
                    { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
            UninstallBrowserExtensions: uninstall,
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-copy-path')).toBeTruthy());
        vi.useFakeTimers();
        fireEvent.click(screen.getByTestId('browser-extension-copy-path'));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(screen.getByTestId('browser-extension-copy-path').textContent).toBe('已复制');
        expect(vi.mocked(ClipboardSetText).mock.calls[0][0]).toBe(plugin);
        expect(vi.mocked(ClipboardSetText).mock.calls[0][0]).not.toBe(parent);
        fireEvent.click(screen.getByTestId('browser-extension-remove-edge'));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(screen.getByTestId('browser-extension-copy-path').textContent).toBe('复制');
        expect(screen.getByTestId('browser-extension-unpacked').textContent).toContain(plugin);
        await act(async () => {
            vi.advanceTimersByTime(2500);
        });
        expect(screen.getByTestId('browser-extension-copy-path').textContent).toBe('复制');
    });

    it('keeps the later plugin-directory copy when an older attempt fails', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        let resolveFirst: (value: boolean) => void = () => {};
        const first = new Promise<boolean>((resolve) => { resolveFirst = resolve; });
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText)
            .mockImplementationOnce(() => first)
            .mockResolvedValueOnce(true);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: plugin,
                browsers: [
                    { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-copy-path')).toBeTruthy());
        const copyBtn = screen.getByTestId('browser-extension-copy-path');
        fireEvent.click(copyBtn);
        fireEvent.click(copyBtn);
        await waitFor(() => expect(copyBtn.textContent).toBe('已复制'));
        expect(vi.mocked(ClipboardSetText).mock.calls.map((call) => call[0])).toEqual([plugin, plugin]);
        expect(vi.mocked(ClipboardSetText).mock.calls.some((call) => call[0] === parent)).toBe(false);
        await act(async () => {
            resolveFirst(false);
            await first;
        });
        expect(writeText).not.toHaveBeenCalled();
        expect(screen.queryByRole('alert')).toBeNull();
        expect(copyBtn.textContent).toBe('已复制');
    });

    it('does not let an older success hide a later failure to copy the plugin directory', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        let resolveFirst: (value: boolean) => void = () => {};
        const first = new Promise<boolean>((resolve) => { resolveFirst = resolve; });
        const writeText = vi.fn().mockRejectedValue(new Error('denied'));
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText)
            .mockImplementationOnce(() => first)
            .mockResolvedValueOnce(false);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: plugin + '\\',
                browsers: [
                    { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-copy-path')).toBeTruthy());
        const copyBtn = screen.getByTestId('browser-extension-copy-path');
        fireEvent.click(copyBtn);
        fireEvent.click(copyBtn);
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('无法复制插件目录'));
        expect(copyBtn.textContent).toBe('复制');
        expect(writeText).toHaveBeenCalledTimes(1);
        expect(writeText).toHaveBeenCalledWith(plugin);
        expect(writeText.mock.calls.some((call) => call[0] === parent)).toBe(false);
        await act(async () => {
            resolveFirst(true);
            await first;
        });
        expect(screen.getByRole('alert').textContent).toContain('无法复制插件目录');
        expect(copyBtn.textContent).toBe('复制');
        expect(vi.mocked(ClipboardSetText).mock.calls.map((call) => call[0])).toEqual([plugin, plugin]);
    });

    it('does not mark the plugin directory copied when both clipboards reject it', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText).mockResolvedValue(false);
        const writeText = vi.fn().mockRejectedValue(new Error('denied'));
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue({
                ok: true,
                unpackedPath: plugin,
                browsers: [
                    { browser: 'chrome', state: 'needs-enable', detail: 'load-unpacked' },
                ],
            }),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-copy-path')).toBeTruthy());
        fireEvent.click(screen.getByTestId('browser-extension-copy-path'));
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('无法复制插件目录'));
        expect(writeText).toHaveBeenCalledWith(plugin);
        expect(writeText.mock.calls.some((call) => call[0] === parent)).toBe(false);
        expect(screen.getByTestId('browser-extension-copy-path').textContent).toContain('复制');
        expect(screen.getByTestId('browser-extension-copy-path').textContent).not.toContain('已复制');
    });

    it('returns to the tool grid when the professional-features entry changes', async () => {
        install();
        const { rerender } = render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-panel')).toBeTruthy());
        rerender(<UtilitiesPage lang="zh-Hans" mode="experts" />);
        await waitFor(() => expect(screen.queryByTestId('browser-extension-panel')).toBeNull());
        rerender(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        expect(screen.getByTestId('utilities-browser-extension-card')).toBeTruthy();
        expect(screen.queryByTestId('browser-extension-panel')).toBeNull();
    });
});
