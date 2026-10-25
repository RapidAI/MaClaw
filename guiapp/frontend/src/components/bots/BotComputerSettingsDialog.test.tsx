import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotComputerSettingsDialog, type BotComputerSettingsLabels } from './BotComputerSettingsDialog';

const action = (name: string) => ({
    name, button: name, description: [`${name}说明一`, `${name}说明二`],
    confirmTitle: `确定${name}？`, confirmButton: `我知道${name}的后果`, forceButton: `中断任务并${name}`,
    working: `正在${name}…`, done: `${name}完成`,
});

const labels: BotComputerSettingsLabels = {
    heading: '设置', title: '云电脑', shared: '所有 Bot 共用',
    actions: { restart: action('重启电脑'), reset: action('重置电脑') },
    resetDisabled: '管理员已关闭重置功能。',
    activeRuns: (n: number) => `${n} 个任务在运行`,
    busy: '有 Bot 正在执行任务', close: '关闭', cancel: '取消',
};

const base = { restart_allowed: true, reset_allowed: true, active_runs: 0 };

describe('BotComputerSettingsDialog', () => {
    it('shows each description under its button and again in the confirm step', () => {
        const onRun = vi.fn();
        render(<BotComputerSettingsDialog labels={labels} state={base} working={null} busy={null} notice="" onClose={() => {}} onRun={onRun} onCancelConfirm={() => {}} />);
        expect(screen.getByText('重启电脑说明一')).toBeTruthy();
        expect(screen.getByText('重置电脑说明二')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-restart'));
        expect(screen.getByTestId('bot-computer-confirm-restart')).toBeTruthy();
        expect(screen.getByText('重启电脑说明一')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-computer-confirm-restart-go'));
        expect(onRun).toHaveBeenCalledWith('restart', false);
    });

    it('reset needs the acknowledgement and is danger-styled', () => {
        const onRun = vi.fn();
        render(<BotComputerSettingsDialog labels={labels} state={{ ...base, active_runs: 2 }} working={null} busy={null} notice="" onClose={() => {}} onRun={onRun} onCancelConfirm={() => {}} />);
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

    it('a busy refusal offers a forced retry; disabled reset cannot be clicked', () => {
        const onRun = vi.fn();
        const { rerender } = render(<BotComputerSettingsDialog labels={labels} state={base} working={null} busy="restart" notice="" onClose={() => {}} onRun={onRun} onCancelConfirm={() => {}} />);
        expect(screen.getByTestId('bot-computer-busy')).toBeTruthy();
        fireEvent.click(screen.getByText('中断任务并重启电脑'));
        expect(onRun).toHaveBeenCalledWith('restart', true);
        rerender(<BotComputerSettingsDialog labels={labels} state={{ ...base, reset_allowed: false }} working={null} busy={null} notice="" onClose={() => {}} onRun={onRun} onCancelConfirm={() => {}} />);
        expect((screen.getByTestId('bot-computer-reset') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByText('管理员已关闭重置功能。')).toBeTruthy();
    });
});
