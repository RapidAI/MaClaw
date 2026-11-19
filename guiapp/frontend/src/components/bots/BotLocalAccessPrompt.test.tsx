import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotLocalAccessPrompt } from './BotLocalAccessPrompt';

const { getPending, getAccess, saveAccess, answer } = vi.hoisted(() => ({
    getPending: vi.fn(),
    getAccess: vi.fn(),
    saveAccess: vi.fn(),
    answer: vi.fn(),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetPendingBotLocalAccessPrompts: () => getPending(),
    GetBotLocalAccess: () => getAccess(),
    SaveBotLocalAccess: (settings: unknown) => saveAccess(settings),
    AnswerBotLocalAccessPrompt: (id: string, choice: string) => answer(id, choice),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
}));

const prompt = {
    id: 'p1',
    bot_name: 'Bot 4',
    kind: 'write',
    target: 'C:\\Users\\ma139\\MaClaw\\Bot 4\\weather.pdf',
    seconds: 120,
};

describe('access prompt full permission', () => {
    beforeEach(() => {
        getPending.mockReset();
        getAccess.mockReset();
        saveAccess.mockReset();
        answer.mockReset();
        getPending.mockResolvedValue([prompt]);
        getAccess.mockResolvedValue({
            settings: {
                files_enabled: true,
                roots: [{ path: 'C:\\Users\\ma139\\MaClaw', write: true }],
                permission: 'ask',
                net_allow: [],
                approved: [],
            },
            connected: true,
            home: 'C:\\Users\\ma139',
        });
        saveAccess.mockResolvedValue({ settings: { permission: 'full' }, connected: true, home: '' });
    });

    it('switches to full access from the question and keeps the other settings', async () => {
        render(<BotLocalAccessPrompt lang="zh" />);
        fireEvent.click(await screen.findByTestId('bot-local-full'));
        await waitFor(() => expect(saveAccess).toHaveBeenCalled());
        expect(saveAccess.mock.calls[0][0]).toMatchObject({
            permission: 'full',
            files_enabled: true,
            roots: [{ path: 'C:\\Users\\ma139\\MaClaw', write: true }],
        });
        expect(answer).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.queryByTestId('bot-local-prompt')).toBeNull());
    });
});
