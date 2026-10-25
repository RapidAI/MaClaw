import { readFileSync } from 'node:fs';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotComputerSettingsDialog, type BotComputerSettingsLabels } from './BotComputerSettingsDialog';
import { BotSettingsTabs } from './BotNetworkSettingsDialog';

const action = (name: string, short: string) => ({
    name, button: `${name}（完整）`, short, description: [`${name}说明一`, `${name}说明二`],
    confirmTitle: `确定${name}？`, confirmButton: `我知道${name}的后果`, forceButton: `中断任务并${name}`,
    working: `正在${name}…`,
});

const labels: BotComputerSettingsLabels = {
    heading: '设置', title: '云电脑', shared: '所有 Bot 共用',
    actions: { restart: action('重启电脑', '重启'), reset: action('重置电脑', '重置') },
    resetDisabled: '管理员已关闭重置功能。',
    activeRuns: (n: number) => `${n} 个任务在运行`,
    busy: '有 Bot 正在执行任务', close: '关闭', cancel: '取消',
};

const base = { restart_allowed: true, reset_allowed: true, active_runs: 0 };

function renderDialog(overrides: Partial<Parameters<typeof BotComputerSettingsDialog>[0]> = {}) {
    const onRun = vi.fn();
    const onClose = vi.fn();
    const onCancelConfirm = vi.fn();
    const utils = render(
        <BotComputerSettingsDialog
            labels={labels}
            state={base}
            working={null}
            busy={null}
            notice=""
            tabs={<BotSettingsTabs active="computer" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={() => {}} />}
            onClose={onClose}
            onRun={onRun}
            onCancelConfirm={onCancelConfirm}
            {...overrides}
        />,
    );
    return { onRun, onClose, onCancelConfirm, ...utils };
}

describe('BotComputerSettingsDialog', () => {
    it('shows each description under its button and again in the confirm step', () => {
        const { onRun } = renderDialog();
        expect(screen.getByText('重启电脑说明一')).toBeTruthy();
        expect(screen.getByText('重置电脑说明二')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        expect(screen.getByTestId('bot-computer-confirm-restart')).toBeTruthy();
        expect(screen.getByText('重启电脑说明一')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-confirm-restart-go'));
        expect(onRun).toHaveBeenCalledWith('restart', false);
    });

    it('reset needs the acknowledgement and is danger-styled', () => {
        const { onRun } = renderDialog({ state: { ...base, active_runs: 2 } });
        expect(screen.getByTestId('bot-computer-reset').className).toContain('btn-danger');
        fireEvent.click(screen.getByTestId('bot-computer-reset'));
        expect(screen.getByText('2 个任务在运行')).toBeTruthy();
        const go = screen.getByTestId('bot-computer-confirm-reset-go') as HTMLButtonElement;
        expect(go.disabled).toBe(true);
        fireEvent.click(screen.getByTestId('bot-computer-reset-ack'));
        expect(go.disabled).toBe(false);
        fireEvent.click(go);
        expect(onRun).toHaveBeenCalledWith('reset', false);
    });

    // The result banner belongs to the action that just finished. Asking a new
    // question must not carry it along — the user would read it as an answer to
    // the question on screen.
    it('does not carry a previous result into a new question', () => {
        renderDialog({ notice: '已重启，浏览器会自动打开。', noticeTone: 'success' });
        expect(screen.getByTestId('bot-computer-notice')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        const question = screen.getByTestId('bot-computer-confirm-restart');
        expect(question.textContent).not.toContain('已重启');
    });

    it('does not carry a previous failure into a new question either', () => {
        renderDialog({ notice: '连接失败。' });
        fireEvent.click(screen.getByTestId('bot-computer-reset'));
        expect(screen.getByTestId('bot-computer-confirm-reset').textContent).not.toContain('连接失败');
    });

    // "A bot is working right now" is a decision to weigh, not a failure.
    it('warns about running bots instead of reporting an error', () => {
        renderDialog({ busy: 'restart' });
        const busy = screen.getByTestId('bot-computer-busy');
        expect(busy.className).toContain('bot-computer__warn');
        expect(busy.className).not.toContain('bot-llm-dialog__notice');
        expect(busy.getAttribute('role')).toBe('alert');
    });

    it('a busy refusal offers a forced retry; disabled reset cannot be clicked', () => {
        const { onRun, rerender } = renderDialog({ busy: 'restart' });
        expect(screen.getByTestId('bot-computer-busy')).toBeTruthy();
        fireEvent.click(screen.getByText('中断任务并重启电脑'));
        expect(onRun).toHaveBeenCalledWith('restart', true);
        rerender(
            <BotComputerSettingsDialog labels={labels} state={{ ...base, reset_allowed: false }} working={null} busy={null} notice="" onClose={() => {}} onRun={onRun} onCancelConfirm={() => {}} />,
        );
        expect((screen.getByTestId('bot-computer-reset') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByText('管理员已关闭重置功能。')).toBeTruthy();
    });

    // The two cards group an action; they are not radio options, so they must
    // not carry the selectable-card styling (pointer cursor, whole-card click).
    it('groups each action in a card that is not itself the control', () => {
        renderDialog();
        const restartCard = screen.getByTestId('bot-computer-restart').closest('.bot-computer__action') as HTMLElement;
        expect(restartCard).toBeTruthy();
        expect(restartCard.className).not.toContain('bot-llm-dialog__option');
        expect(restartCard.className).not.toContain('is-danger');
        const resetCard = screen.getByTestId('bot-computer-reset').closest('.bot-computer__action') as HTMLElement;
        expect(resetCard.className).toContain('is-danger');
        // The title names the action; the card button is the short verb.
        expect(within(restartCard).getByRole('heading').textContent).toBe('重启电脑');
        expect(screen.getByTestId('bot-computer-restart').textContent).toBe('重启');
        expect(screen.getByTestId('bot-computer-reset').textContent).toBe('重置');
    });

    it('falls back to the full label when no short one is given', () => {
        const longLabels: BotComputerSettingsLabels = {
            ...labels,
            actions: {
                restart: { ...labels.actions.restart, short: undefined },
                reset: { ...labels.actions.reset, short: undefined },
            },
        };
        render(<BotComputerSettingsDialog labels={longLabels} state={base} working={null} busy={null} notice="" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />);
        expect(screen.getByTestId('bot-computer-restart').textContent).toBe('重启电脑（完整）');
    });

    // "N tasks are running" and "restarting…" are information, not failures:
    // they must not borrow the red error banner.
    it('reports running work as information, not as an error', () => {
        renderDialog({ state: { ...base, active_runs: 3 } });
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        const runs = screen.getByText('3 个任务在运行');
        expect(runs.className).toContain('bot-computer__progress');
        expect(runs.className).not.toContain('bot-llm-dialog__notice');
    });

    it('keeps the shared-computer warning out of the error style', () => {
        renderDialog();
        const shared = screen.getByText('所有 Bot 共用');
        expect(shared.className).toContain('bot-computer__shared');
        expect(shared.className).not.toContain('bot-llm-dialog__notice');
    });

    // Same frame as the model and network tabs: header and tab strip share one
    // bordered block, so the three tabs do not jump between layouts.
    it('puts the header and the tab strip on the shared head block', () => {
        const { container } = renderDialog();
        const head = container.querySelector('.bot-llm-dialog__head');
        expect(head).toBeTruthy();
        expect(head?.querySelector('.custom-dialog__header')).toBeTruthy();
        expect(head?.querySelector('[data-testid="bot-settings-tabs"]')).toBeTruthy();
    });

    it('hides the tab strip while the confirm step is open', () => {
        renderDialog();
        expect(screen.getByTestId('bot-settings-tabs')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-reset'));
        expect(screen.queryByTestId('bot-settings-tabs')).toBeNull();
    });

    // Escape backs out one step at a time, and cannot abandon a running action.
    it('leaves the confirm step on Escape before closing the dialog', () => {
        const { onClose, onCancelConfirm } = renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        fireEvent.keyDown(screen.getByTestId('bot-computer-dialog'), { key: 'Escape' });
        expect(onCancelConfirm).toHaveBeenCalled();
        expect(onClose).not.toHaveBeenCalled();
        expect(screen.queryByTestId('bot-computer-confirm-restart')).toBeNull();
    });

    it('ignores Escape while the action is running', () => {
        const { onClose } = renderDialog({ working: 'restart' });
        fireEvent.keyDown(screen.getByTestId('bot-computer-dialog'), { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();
    });

    it('keeps focus inside the dialog, like the other two tabs', () => {
        const outside = document.createElement('button');
        document.body.appendChild(outside);
        renderDialog();
        const dialog = screen.getByTestId('bot-computer-dialog');
        expect(document.activeElement).toBe(dialog);
        outside.focus();
        expect(document.activeElement).toBe(dialog);
        outside.remove();
    });

    // Entering the confirm step replaces the body, which unmounts the button
    // that held focus. Focus used to fall to <body>, where neither Tab nor
    // Escape reached the dialog — a keyboard user was stuck.
    it('moves focus into the question it just asked', () => {
        renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        expect(document.activeElement).toBe(screen.getByTestId('bot-computer-confirm-restart-go'));

        cleanup();
        renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-reset'));
        expect(document.activeElement).toBe(screen.getByTestId('bot-computer-reset-ack'));
    });

    it('gives focus back to the button that asked when the question is dismissed', () => {
        renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-reset'));
        fireEvent.click(screen.getByText('取消'));
        expect(document.activeElement).toBe(screen.getByTestId('bot-computer-reset'));
    });

    // Starting an action is two commits: confirm closes (focus returns to the
    // opener) and then the running step paints, which unmounts that opener.
    // Comparing "is confirm open" missed the second commit and left focus on
    // <body> while the dialog was busy — no control was focusable at all.
    it('holds focus through the running step, where every control is disabled', () => {
        const { rerender } = renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        fireEvent.click(screen.getByTestId('bot-computer-confirm-restart-go'));
        rerender(
            <BotComputerSettingsDialog labels={labels} state={base} working="restart" busy={null} notice="" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />,
        );
        const dialog = screen.getByTestId('bot-computer-dialog');
        expect(document.activeElement).toBe(dialog);
        expect(dialog.contains(document.activeElement)).toBe(true);
    });

    // Landing back on the action button after success would invite a second
    // restart; the useful next step is closing the dialog.
    it('puts focus on Close after a successful action', () => {
        const { rerender } = renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        fireEvent.click(screen.getByTestId('bot-computer-confirm-restart-go'));
        rerender(
            <BotComputerSettingsDialog labels={labels} state={base} working="restart" busy={null} notice="" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />,
        );
        rerender(
            <BotComputerSettingsDialog labels={labels} state={base} working={null} busy={null} notice="已重启。" noticeTone="success" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />,
        );
        expect(document.activeElement).toBe(screen.getByText('关闭'));
    });

    it('returns focus into the dialog once the action finishes', () => {
        const { rerender } = renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        fireEvent.click(screen.getByTestId('bot-computer-confirm-restart-go'));
        rerender(
            <BotComputerSettingsDialog labels={labels} state={base} working="restart" busy={null} notice="" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />,
        );
        rerender(
            <BotComputerSettingsDialog labels={labels} state={base} working={null} busy={null} notice="已重启。" noticeTone="success" onClose={() => {}} onRun={() => {}} onCancelConfirm={() => {}} />,
        );
        const dialog = screen.getByTestId('bot-computer-dialog');
        expect(dialog.contains(document.activeElement)).toBe(true);
    });

    it('keeps Tab inside the dialog after the step swap', () => {
        renderDialog();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        const dialog = screen.getByTestId('bot-computer-dialog');
        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Tab' });
        expect(dialog.contains(document.activeElement)).toBe(true);
    });

    // A finished restart is a result, not a failure: it must not be painted
    // with the red error banner the failures use.
    it('reports a finished action with the success tone', () => {
        renderDialog({ notice: '已重启。', noticeTone: 'success' });
        const done = screen.getByTestId('bot-computer-notice');
        expect(done.className).toContain('bot-computer__done');
        expect(done.className).not.toContain('bot-llm-dialog__notice');
    });

    it('still reports a failure with the error tone', () => {
        renderDialog({ notice: '重启失败。' });
        const failed = screen.getByTestId('bot-computer-notice');
        expect(failed.className).toContain('bot-llm-dialog__notice');
        expect(failed.className).not.toContain('bot-computer__done');
    });
});

// Guards for the two defects this tab shipped with: a footer danger button the
// global shell repainted blue, and a head block that did not exist.
describe('bot computer stylesheet', () => {
    const fromFrontend = join(process.cwd(), 'src/components/bots/DesktopBotWorkspace.css');
    const cssPath = existsSync(fromFrontend) ? fromFrontend : join(process.cwd(), 'guiapp/frontend/src/components/bots/DesktopBotWorkspace.css');
    const css = readFileSync(cssPath, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');

    it('keeps the reset confirm button on the danger fill', () => {
        const rule = css.match(/\.modal-backdrop\.custom-dialog-backdrop \.bot-llm-dialog \.custom-dialog__footer \.btn-secondary\.btn-danger\s*\{([^}]*)\}/)?.[1] ?? '';
        expect(rule).toMatch(/background:\s*var\(--theme-danger/);
        // It must out-rank the (0,5,0) global rule that paints the footer blue.
        expect(css).toContain('.modal-backdrop.custom-dialog-backdrop .bot-llm-dialog .custom-dialog__footer .btn-secondary.btn-danger');
    });

    it('does not reuse the radio-card styling for the action cards', () => {
        expect(css).not.toMatch(/\.bot-computer__item\b/);
        expect(css).toMatch(/\.bot-computer__action\s*\{[^}]*border:/);
        expect(css).toMatch(/\.bot-computer__action\.is-danger\s*\{[^}]*--theme-danger/);
    });

    it('styles running work and the shared warning as information', () => {
        expect(css).toMatch(/\.bot-computer__progress\s*\{[^}]*--theme-primary/);
        expect(css).toMatch(/\.bot-computer__shared\s*\{[^}]*--theme-warning/);
        // No hardcoded light-only colors left in this block.
        expect(css).not.toMatch(/\.bot-computer__shared\s*\{[^}]*color:\s*#/);
    });
});
