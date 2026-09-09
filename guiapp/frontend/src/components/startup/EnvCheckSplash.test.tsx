// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EnvCheckSplash } from './EnvCheckSplash';
import { translations } from '../../i18n/appTranslations';

afterEach(() => {
    cleanup();
    document.getElementById('maclaw-boot-splash')?.remove();
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
        expect(screen.getByRole('heading', { name: '环境准备中' })).toBeTruthy();
        expect(screen.getByText('正在准备运行环境，请稍候，完成后自动进入主界面。')).toBeTruthy();
        expect(screen.getByRole('status').textContent).toBe('Preparing runtime...');
        expect(document.querySelector('.app-loading-progress__bar')).toBeTruthy();
        expect(document.querySelector('.app-loading-card')).toBeTruthy();
        expect(document.querySelector('.app-loading-backdrop')).toBeTruthy();
        const mark = screen.getByRole('img', { name: /MaClaw/i }) as HTMLImageElement;
        expect(mark.getAttribute('width')).toBe('192');
        expect(mark.getAttribute('height')).toBe('220');
    });

    it('fills the compact window with large Node, Git, and Python runtime tiles', () => {
        renderSplash();

        expect(screen.getByText('Node.js')).toBeTruthy();
        expect(screen.getByText('Git')).toBeTruthy();
        expect(screen.getByText('Python')).toBeTruthy();
        expect(screen.getByText('JavaScript 运行时')).toBeTruthy();
        expect(screen.getByText('代码版本管理')).toBeTruthy();
        expect(screen.getByText('Python 运行时')).toBeTruthy();
        expect(document.querySelectorAll('.app-loading-runtime')).toHaveLength(3);
        expect(document.querySelectorAll('.app-loading-runtime__icon')).toHaveLength(3);
        expect(document.querySelectorAll('.app-loading-runtime__state--pending')).toHaveLength(3);
        expect((document.querySelector('.app-loading-progress__bar') as HTMLElement).style.width).toBe('8%');
    });

    it('highlights the runtime that matches the latest environment log', () => {
        const { rerender, props } = renderSplash({ envLogs: ['[2/4] Checking Node.js...'] });
        expect(document.querySelector('.app-loading-runtime--node')?.getAttribute('aria-current')).toBe('step');
        expect(document.querySelector('.app-loading-runtime--git')?.getAttribute('aria-current')).toBeNull();

        rerender(<EnvCheckSplash {...props} envLogs={['[2/4] Checking Node.js...', '[3/4] Checking Git...']} />);
        expect(document.querySelector('.app-loading-runtime--git')?.getAttribute('aria-current')).toBe('step');
        expect(document.querySelector('.app-loading-runtime--node')?.getAttribute('aria-current')).toBeNull();
        expect(document.querySelector('.app-loading-runtime--node')?.classList.contains('is-done')).toBe(true);
        expect(screen.getByText('已就绪')).toBeTruthy();
        expect(screen.getByText('进行中')).toBeTruthy();
        expect((document.querySelector('.app-loading-progress__bar') as HTMLElement).style.width).toBe('62%');
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

        expect(screen.getByRole('textbox', { name: '安装日志' }).textContent).toContain('Checking Python environment...');
        expect((document.querySelector('.app-loading-runtimes') as HTMLElement).hidden).toBe(true);
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
        expect((document.querySelector('.app-loading-prepare') as HTMLElement).hidden).toBe(false);
        expect(document.querySelector('.app-loading-card--quit')).toBeTruthy();
        expect(document.querySelector('.app-loading-content')?.hasAttribute('inert')).toBe(true);
        expect(document.querySelector('.app-loading-quit')).toBeTruthy();
        expect((document.querySelector('.app-loading-log') as HTMLTextAreaElement).value).toContain('Checking Node.js...');
    });

    it('does not mark Git ready when the log jumps from Node.js to Python', () => {
        renderSplash({
            envLogs: ['Checking Node.js...', 'Checking Python environment...'],
        });
        expect(document.querySelector('.app-loading-runtime--python')?.classList.contains('is-active')).toBe(true);
        expect(document.querySelector('.app-loading-runtime--node')?.classList.contains('is-done')).toBe(true);
        expect(document.querySelector('.app-loading-runtime--git')?.classList.contains('is-done')).toBe(false);
        expect(document.querySelector('.app-loading-runtime--git')?.classList.contains('is-active')).toBe(false);
    });

    it('marks unseen runtimes skipped after the environment check completes', () => {
        renderSplash({
            envLogs: ['Checking Python environment...', '— Base environment check complete.'],
        });
        expect(document.querySelector('.app-loading-runtime--python')?.classList.contains('is-done')).toBe(true);
        expect(document.querySelector('.app-loading-runtime--node')?.classList.contains('is-skipped')).toBe(true);
        expect(document.querySelector('.app-loading-runtime--git')?.classList.contains('is-skipped')).toBe(true);
        expect(screen.getAllByText('已跳过')).toHaveLength(2);
    });

    it('gives the detail log the tile row instead of crushing it into the hero', () => {
        renderSplash({
            showLogs: true,
            envLogs: ['Checking Node.js...'],
        });
        const log = document.querySelector('.app-loading-log');
        const hero = document.querySelector('.app-loading-hero');
        expect(log).toBeTruthy();
        expect(hero?.contains(log)).toBe(false);
        expect(document.querySelector('.app-loading-content')?.contains(log)).toBe(true);
        expect(screen.getByRole('status')).toBeTruthy();
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

    it('does not own HTML boot-splash removal (main.tsx hands it off after React renders)', () => {
        const boot = document.createElement('div');
        boot.id = 'maclaw-boot-splash';
        document.body.appendChild(boot);
        renderSplash();
        expect(document.querySelector('.app-loading-card')).toBeTruthy();
        expect(document.getElementById('maclaw-boot-splash')).toBeTruthy();
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
