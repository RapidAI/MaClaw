import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotRoomsDialog } from './BotRoomsDialog';
import type { CollabApi } from './botCollab';

const bots = [{ id: 'm', title: '主管', primary: true }, { id: 'a', title: '小A' }];

function fakeApi(): CollabApi {
    const room = { id: 'r1', name: '写作组', members: ['m', 'a'], attention: { a: 'mentions' }, created_at: '' };
    return {
        features: vi.fn(async () => ({ assignments: true, peer_review: true, say: true, rooms: true })),
        said: vi.fn(async () => [
            { id: '1', from: 'human', target: 'r1', kind: 'room', text: '@小A 写个开头', created_at: '' },
            { id: '2', from: 'a', target: 'r1', kind: 'say', text: '好的', created_at: '' },
        ]),
        assignments: vi.fn(async () => []),
        rooms: vi.fn(async () => [room]),
        saveRoom: vi.fn(async (_id, name, members, attention) => ({ ...room, id: 'r2', name, members, attention })),
        deleteRoom: vi.fn(async () => undefined),
        postRoom: vi.fn(async (_id, text) => ({ message: { id: '3', from: 'human', target: 'r1', kind: 'room', text, created_at: '' }, woken: ['a'] })),
    };
}

describe('BotRoomsDialog', () => {
    it('lists rooms, opens chat, shows answered and @autocomplete, sends', async () => {
        const api = fakeApi();
        render(<BotRoomsDialog bots={bots} api={api} onClose={() => {}} pollMs={60000} />);
        fireEvent.click(await screen.findByTestId('bot-room-open-r1'));
        await waitFor(() => expect(screen.getByTestId('bot-room-answered').textContent).toBe('已回答：小A'));
        const input = screen.getByTestId('bot-room-input') as HTMLTextAreaElement;
        fireEvent.change(input, { target: { value: '@小', selectionStart: 2 } });
        fireEvent.mouseDown(await screen.findByTestId('bot-room-mention-a'));
        expect(input.value).toContain('@小A');
        fireEvent.click(screen.getByTestId('bot-room-send'));
        await waitFor(() => expect(api.postRoom).toHaveBeenCalledWith('r1', expect.stringContaining('@小A')));
    });
    it('creates a room with members and attention', async () => {
        const api = fakeApi();
        render(<BotRoomsDialog bots={bots} api={api} onClose={() => {}} pollMs={60000} />);
        fireEvent.click(await screen.findByTestId('bot-room-new'));
        fireEvent.change(screen.getByTestId('bot-room-name'), { target: { value: '新群' } });
        fireEvent.change(screen.getByTestId('bot-room-attention-a'), { target: { value: 'all' } });
        fireEvent.click(screen.getByTestId('bot-room-save'));
        await waitFor(() => expect(api.saveRoom).toHaveBeenCalledWith('', '新群', ['m', 'a'], { m: 'all', a: 'all' }));
    });
});
