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

const NO_STORE_ROWS = [
    { browser: 'chrome', state: 'failed', detail: 'no-store-url' },
    { browser: 'edge', state: 'failed', detail: 'no-store-url' },
    { browser: 'safari', state: 'absent' },
];

function install(overrides: Record<string, unknown> = {}) {
    const installSpy = vi.fn().mockResolvedValue({ ok: true, browsers: NO_STORE_ROWS });
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
                GetBrowserExtensionStatus: vi.fn().mockResolvedValue({ ok: true, browsers: NO_STORE_ROWS }),
                ...overrides,
            },
        },
    };
    return { installSpy, uninstallSpy };
}

/** First status read has nothing prepared; later polls return the installed folder. */
function statusFromNoStore(result: unknown) {
    return vi.fn()
        .mockResolvedValueOnce({ ok: true, browsers: NO_STORE_ROWS })
        .mockResolvedValue(result);
}

const originalClipboard = Object.getOwnPropertyDescriptor(Navigator.prototype, 'clipboard')
    ?? Object.getOwnPropertyDescriptor(navigator, 'clipboard');

/** Open the card and run the row install, leaving a load-unpacked folder on screen. */
async function openUnpacked(browser = 'edge') {
    fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
    await waitFor(() => expect(screen.getByTestId(`browser-extension-install-${browser}`)).toBeTruthy());
    fireEvent.click(screen.getByTestId(`browser-extension-install-${browser}`));
    await waitFor(() => expect(screen.getByTestId('browser-extension-unpacked')).toBeTruthy());
}

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

    it('reads status on open and registers only from the row button', async () => {
        const statusSpy = vi.fn().mockResolvedValue({ ok: true, browsers: NO_STORE_ROWS });
        const { installSpy, uninstallSpy } = install({ GetBrowserExtensionStatus: statusSpy });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(statusSpy).toHaveBeenCalledTimes(1));
        // Opening the card must not run a registration that launches a browser.
        expect(installSpy).not.toHaveBeenCalled();
        expect(screen.getByTestId('browser-extension-panel')).toBeTruthy();
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('点「安装」准备扩展目录，再按提示在浏览器里加载');
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('安装');
        expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('尚未安装');
        expect(screen.getByTestId('browser-extension-install-edge')).toBeTruthy();
        expect(screen.queryByTestId('browser-extension-row-safari')).toBeNull();
        expect(screen.queryByText(/download|下载页/i)).toBeNull();
        fireEvent.click(screen.getByTestId('browser-extension-install-chrome'));
        await waitFor(() => expect(installSpy).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('browser-extension-remove-chrome'));
        await waitFor(() => expect(uninstallSpy).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('未安装');
    });

    it('offers the unpacked install flow for Linux Chrome', async () => {
        const result = {
            ok: true,
            unpackedPath: '/home/me/.maclaw/data/file-companion/companion-paper',
            browsers: [
                { browser: 'chrome', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        const openPage = vi.fn().mockResolvedValue(undefined);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: vi.fn()
                .mockResolvedValueOnce({
                    ok: true,
                    browsers: [{ browser: 'chrome', state: 'failed', detail: 'linux-chrome' }],
                })
                .mockResolvedValue(result),
            ShowBrowserExtensionLoad: openPage,
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        fireEvent.click(screen.getByTestId('utilities-browser-extension-card'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-install-chrome')).toBeTruthy());
        expect(screen.getByTestId('browser-extension-row-chrome').textContent).toContain('Linux Chrome 无法静默安装');
        fireEvent.click(screen.getByTestId('browser-extension-install-chrome'));
        await waitFor(() => expect(screen.getByTestId('browser-extension-unpacked')).toBeTruthy());
        expect(openPage).toHaveBeenCalledWith('chrome');
    });

    it('shows the unpacked folder and opens the extensions page for that browser', async () => {
        const reveal = vi.fn().mockResolvedValue(undefined);
        const openPage = vi.fn().mockResolvedValue(undefined);
        const result = {
            ok: true,
            unpackedPath: 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
            RevealBrowserExtensionFolder: reveal,
            ShowBrowserExtensionLoad: openPage,
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
        expect(openPage).toHaveBeenCalledWith('edge');
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
        expect(screen.getByTestId('browser-extension-guide').textContent).toContain('已打开扩展页');
        expect(screen.getByTestId('browser-extension-show-folder').textContent).toContain('打开文件夹');
        fireEvent.click(screen.getByTestId('browser-extension-show-folder'));
        await waitFor(() => expect(reveal).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('browser-extension-install-edge'));
        await waitFor(() => expect(openPage).toHaveBeenCalledTimes(2));
    });

    it('does not claim the extensions page opened when the backend cannot open it', async () => {
        const result = {
            ok: true,
            unpackedPath: 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
        expect(screen.getByRole('alert').textContent).toContain('当前版本不能打开浏览器扩展页');
        expect(screen.queryByTestId('browser-extension-guide')).toBeNull();
    });

    it('polls until the browser reports the extension installed', async () => {
        const loading = {
            ok: true,
            unpackedPath: 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        const installed = {
            ok: true,
            unpackedPath: loading.unpackedPath,
            browsers: [
                { browser: 'edge', state: 'installed', detail: 'installed' },
            ],
        };
        const statusSpy = vi.fn()
            .mockResolvedValueOnce({ ok: true, browsers: NO_STORE_ROWS })
            .mockResolvedValue(installed);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(loading),
            GetBrowserExtensionStatus: statusSpy,
            ShowBrowserExtensionLoad: vi.fn().mockResolvedValue(undefined),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
        expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('未加载');
        await waitFor(() => expect(statusSpy).toHaveBeenCalledTimes(2), { timeout: 4000 });
        expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('已安装');
        expect(screen.queryByTestId('browser-extension-guide')).toBeNull();
    });

    it('does not let an older status poll overwrite a later uninstall', async () => {
        const loading = {
            ok: true,
            unpackedPath: 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        let resolveStale: (value: unknown) => void = () => {};
        const stale = new Promise((resolve) => { resolveStale = resolve; });
        const statusSpy = vi.fn()
            .mockResolvedValueOnce({ ok: true, browsers: NO_STORE_ROWS })
            .mockImplementationOnce(() => stale);
        const { uninstallSpy } = install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(loading),
            GetBrowserExtensionStatus: statusSpy,
            ShowBrowserExtensionLoad: vi.fn().mockResolvedValue(undefined),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
        await waitFor(() => expect(statusSpy).toHaveBeenCalledTimes(2), { timeout: 4000 });
        fireEvent.click(screen.getByTestId('browser-extension-remove-edge'));
        await waitFor(() => expect(uninstallSpy).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('未安装'));
        await act(async () => {
            resolveStale({
                ok: true,
                browsers: [{ browser: 'edge', state: 'installed', detail: 'installed' }],
            });
            await stale;
        });
        expect(screen.getByTestId('browser-extension-row-edge').textContent).toContain('未安装');
        expect(screen.getByTestId('browser-extension-row-edge').textContent).not.toContain('已安装');
    });

    it('copies the plugin directory beside the path, not its parent', async () => {
        const plugin = 'C:\\Users\\me\\.maclaw\\data\\file-companion\\companion-paper\\';
        const parent = 'C:\\Users\\me\\.maclaw\\data\\file-companion';
        vi.mocked(ClipboardSetText).mockReset();
        vi.mocked(ClipboardSetText).mockResolvedValue(true);
        const result = {
            ok: true,
            unpackedPath: plugin,
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
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
        const result = {
            ok: true,
            unpackedPath: plugin,
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        const uninstall = vi.fn().mockResolvedValue({
            ok: true,
            unpackedPath: plugin + '\\',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        });
        vi.mocked(ClipboardSetText).mockResolvedValue(true);
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
            UninstallBrowserExtensions: uninstall,
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
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
        const result = {
            ok: true,
            unpackedPath: plugin,
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
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
        const result = {
            ok: true,
            unpackedPath: plugin + '\\',
            browsers: [
                { browser: 'edge', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('edge');
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
        const result = {
            ok: true,
            unpackedPath: plugin,
            browsers: [
                { browser: 'chrome', state: 'needs-enable', detail: 'load-unpacked' },
            ],
        };
        install({
            InstallBrowserExtensions: vi.fn().mockResolvedValue(result),
            GetBrowserExtensionStatus: statusFromNoStore(result),
        });
        render(<UtilitiesPage lang="zh-Hans" mode="tools" />);
        await openUnpacked('chrome');
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
