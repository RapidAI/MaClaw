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

    it('lets the new bot pick one of the local servers', () => {
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
                computer={{
                    label: '这个 bot 需要电脑',
                    quota: '已用 5',
                    locked: false,
                    cloudLabel: '云端电脑',
                    localLabel: '本地电脑',
                    server: 'dn_a',
                    servers: [
                        { id: 'dn_a', label: 'znsoft@192.168.1.49' },
                        { id: 'dn_b', label: 'znsoft@192.168.1.50' },
                    ],
                }}
            />,
        );
        expect((screen.getByTestId('bot-create-server-dn_a') as HTMLInputElement).checked).toBe(true);
        fireEvent.click(screen.getByTestId('bot-create-server-dn_b'));
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true, 'dn_b');
        fireEvent.click(screen.getByTestId('bot-create-place-cloud'));
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenLastCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true, '');
        fireEvent.click(screen.getByTestId('bot-create-needs-computer'));
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenLastCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), false);
    });

    it('selects the cloud computer when the suggested server is not listed', () => {
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
                computer={{
                    label: '这个 bot 需要电脑',
                    quota: '',
                    locked: false,
                    cloudLabel: '云端电脑',
                    localLabel: '本地电脑',
                    server: 'dn_missing',
                    servers: [{ id: 'dn_a', label: 'znsoft@192.168.1.49' }],
                }}
            />,
        );
        expect((screen.getByTestId('bot-create-place-cloud') as HTMLInputElement).checked).toBe(true);
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true, '');
        fireEvent.click(screen.getByTestId('bot-create-place-local'));
        expect(screen.getByTestId('bot-create-local-only').textContent).toBe('znsoft@192.168.1.49');
        expect(screen.queryByTestId('bot-create-server-dn_a')).toBeNull();
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenLastCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true, 'dn_a');
    });

    it('does not create a cloud bot when the chosen local server disappeared', () => {
        const onSubmit = vi.fn();
        const computer = {
            label: '这个 bot 需要电脑',
            quota: '',
            locked: false,
            cloudLabel: '云端电脑',
            localLabel: '本地电脑',
            serverMissing: '请选择一台本地电脑',
            server: 'dn_a',
            servers: [
                { id: 'dn_a', label: 'znsoft@192.168.1.49' },
                { id: 'dn_b', label: 'znsoft@192.168.1.50' },
            ],
        };
        const { rerender } = render(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                computer={computer}
            />,
        );
        fireEvent.click(screen.getByTestId('bot-create-server-dn_b'));
        rerender(
            <BotEditDialog
                initialTitle="Bot 1"
                initialDescription=""
                labels={labels}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                computer={{
                    ...computer,
                    servers: [
                        { id: 'dn_c', label: 'znsoft@192.168.1.51' },
                        { id: 'dn_d', label: 'znsoft@192.168.1.52' },
                    ],
                }}
            />,
        );
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByTestId('bot-create-server-missing').textContent).toBe('请选择一台本地电脑');
        fireEvent.click(screen.getByTestId('bot-create-server-dn_c'));
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('Bot 1', '', expect.objectContaining({ color: expect.any(String) }), true, 'dn_c');
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
