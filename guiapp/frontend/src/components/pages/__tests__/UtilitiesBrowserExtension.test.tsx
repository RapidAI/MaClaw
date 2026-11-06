// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { UtilitiesPage } from '../UtilitiesPage';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
    BrowserOpenURL: vi.fn(),
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

describe('browser extension tool card', () => {
    afterEach(() => {
        delete (window as any).go;
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
