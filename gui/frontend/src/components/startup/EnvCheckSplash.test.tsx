// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EnvCheckSplash } from './EnvCheckSplash';
import { translations } from '../../i18n/appTranslations';

afterEach(() => {
    cleanup();
});

function renderSplash(overrides: Partial<ComponentProps<typeof EnvCheckSplash>> = {}) {
    const props: ComponentProps<typeof EnvCheckSplash> = {
        themeMode: 'light',
        nativeRounded: true,
        useCSSWindowCorners: false,
        isLegacyWindowsFrameless: false,
        t: (key) => translations['zh-Hans'][key] || translations.en[key] || key,
        envLogs: [],
        showLogs: false,
        isManualCheck: false,
        logEndRef: { current: null },
        onToggleLogs: vi.fn(),
        onDismiss: vi.fn(),
        onQuit: vi.fn(),
        ...overrides,
    };
    const result = render(<EnvCheckSplash {...props} />);
    return { ...result, props };
}

describe('EnvCheckSplash', () => {
    it('renders the MaClaw mark, preparing copy, and green progress track', () => {
        renderSplash();

        expect(screen.getByRole('img', { name: /MaClaw/i })).toBeTruthy();
        expect(screen.getByRole('heading', { name: '正在准备环境' })).toBeTruthy();
        expect(screen.getByText('正在准备运行环境，请稍候 — 就绪后将自动打开应用。')).toBeTruthy();
        expect(screen.getByRole('status').textContent).toBe('正在准备运行时...');
        expect(document.querySelector('.app-loading-progress__bar')).toBeTruthy();
        expect(document.querySelector('.app-loading-card')).toBeTruthy();
        const mark = screen.getByRole('img', { name: /MaClaw/i }) as HTMLImageElement;
        expect(mark.getAttribute('width')).toBe('192');
        expect(mark.getAttribute('height')).toBe('220');
    });

    it('shows the latest environment log as the status line', () => {
        renderSplash({ envLogs: ['Checking Node.js...', 'Installing Git...'] });
        expect(screen.getByRole('status').textContent).toBe('Installing Git...');
    });

    it('toggles the detail log and confirms before quitting startup setup', () => {
        const onToggleLogs = vi.fn();
        const onQuit = vi.fn();
        renderSplash({
            showLogs: true,
            envLogs: ['Checking Python environment...'],
            onToggleLogs,
            onQuit,
        });

        expect(screen.getByRole('textbox').textContent).toContain('Checking Python environment...');
        fireEvent.click(screen.getByRole('button', { name: '隐藏详情' }));
        expect(onToggleLogs).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByRole('button', { name: '退出程序' }));
        expect(onQuit).not.toHaveBeenCalled();
        expect(screen.getByRole('alertdialog').textContent).toContain('退出将导致环境安装不完整');
        expect(document.querySelector('.app-loading-log')).not.toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '否，继续安装' }));
        expect(onQuit).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole('button', { name: '退出程序' }));
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onQuit).not.toHaveBeenCalled();
        expect(screen.queryByRole('alertdialog')).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '退出程序' }));
        fireEvent.click(screen.getByRole('button', { name: '是的，退出' }));
        expect(onQuit).toHaveBeenCalledTimes(1);
    });

    it('keeps tab inside the quit warning', () => {
        renderSplash({ showLogs: true, envLogs: ['Checking Node.js...'] });
        fireEvent.click(screen.getByRole('button', { name: '退出程序' }));
        const confirm = screen.getByRole('button', { name: '是的，退出' });
        confirm.focus();
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('button', { name: '否，继续安装' }));
        fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(confirm);
    });

    it('keeps the log mounted while the quit warning is open', () => {
        renderSplash({
            showLogs: true,
            envLogs: ['Checking Node.js...'],
        });
        fireEvent.click(screen.getByRole('button', { name: '退出程序' }));
        expect((document.querySelector('.app-loading-prepare') as HTMLElement).hidden).toBe(true);
        expect((document.querySelector('.app-loading-log') as HTMLTextAreaElement).value).toContain('Checking Node.js...');
    });

    it('lets a manual check dismiss instead of quitting', () => {
        const onDismiss = vi.fn();
        renderSplash({ showLogs: true, isManualCheck: true, onDismiss });
        fireEvent.click(screen.getByRole('button', { name: '收起' }));
        expect(onDismiss).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole('button', { name: '退出程序' })).toBeNull();
    });

    it('keeps the English preparing copy for the screenshot-style splash', () => {
        renderSplash({
            t: (key) => translations.en[key] || key,
        });
        expect(screen.getByRole('heading', { name: 'Preparing Environment' })).toBeTruthy();
        expect(screen.getByText(/the app will open automatically when ready/i)).toBeTruthy();
        expect(screen.getByRole('status').textContent).toBe('Preparing runtime...');
    });

    it('keeps frameless window chrome attributes on the shell', () => {
        renderSplash({
            useCSSWindowCorners: false,
            isLegacyWindowsFrameless: true,
            nativeRounded: false,
        });
        const shell = document.querySelector('.app-loading-shell') as HTMLElement;
        expect(shell.getAttribute('data-css-window-corners')).toBe('false');
        expect(shell.getAttribute('data-windows-legacy-frameless')).toBe('true');
        expect(shell.getAttribute('data-native-rounded')).toBeNull();
    });
});
