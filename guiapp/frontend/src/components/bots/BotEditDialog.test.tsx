import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotEditDialog } from './BotEditDialog';

const labels = {
    title: '新建 Bot',
    name: '名称',
    description: '描述',
    save: '新建',
    cancel: '取消',
    close: '关闭',
};

describe('create bot computer choice', () => {
    it('sends a computer bot while the switch stays on', () => {
        const onSubmit = vi.fn();
        render(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                computer={{ label: '这个 bot 需要电脑', quota: '已用 1 / 3', locked: false }}
            />,
        );
        expect(screen.getByTestId('bot-create-quota').textContent).toBe('已用 1 / 3');
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true);
    });

    it('sends no computer when the switch is turned off', () => {
        const onSubmit = vi.fn();
        render(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                computer={{ label: '这个 bot 需要电脑', quota: '已用 1 / 3', locked: false }}
            />,
        );
        fireEvent.click(screen.getByTestId('bot-create-needs-computer'));
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), false);
    });

    it('keeps the switch off when the quota is already used up', () => {
        const onSubmit = vi.fn();
        render(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                computer={{ label: '这个 bot 需要电脑', quota: '已用 1 / 1', locked: true }}
            />,
        );
        const box = screen.getByTestId('bot-create-needs-computer') as HTMLInputElement;
        expect(box.checked).toBe(false);
        expect(box.disabled).toBe(true);
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), false);
    });

    it('does not ask about a computer while editing', () => {
        render(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={() => {}}
            />,
        );
        expect(screen.queryByTestId('bot-create-needs-computer')).toBeNull();
    });
});
