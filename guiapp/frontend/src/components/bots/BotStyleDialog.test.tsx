import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BotStyleDialog, type BotStyleApi } from './BotStyleDialog';

const view = (version: number, soul = '') => ({
    bot_id: 'b1',
    current: { version, soul, memory: '', parts: {} },
    history: version > 1 ? [{ version: version - 1, soul: 'old' }] : [],
    default_soul: 'DEFAULT SOUL',
    default_parts: { voice: 'default voice', quiet: 'default quiet' },
    base_changed: false,
    enabled: true,
});

describe('BotStyleDialog', () => {
    it('loads, saves the soul and part overrides, and restores a version', async () => {
        const api: BotStyleApi = {
            get: vi.fn().mockResolvedValue(view(1)),
            save: vi.fn().mockResolvedValue(view(2, '叫阿龙')),
            rollback: vi.fn().mockResolvedValue(view(3, 'old')),
        };
        render(<BotStyleDialog botId="b1" botTitle="A" lang="zh-Hans" onClose={() => {}} api={api} />);
        const soul = await screen.findByTestId('bot-style-soul');
        expect(soul.getAttribute('placeholder')).toBe('DEFAULT SOUL');
        fireEvent.change(soul, { target: { value: ' 叫阿龙 ' } });
        fireEvent.change(screen.getByTestId('bot-style-part-quiet'), { target: { value: '-' } });
        fireEvent.click(screen.getByTestId('bot-style-save'));
        await waitFor(() => expect(api.save).toHaveBeenCalledWith('b1', '叫阿龙', '', { quiet: '-' }));
        fireEvent.click(await screen.findByTestId('bot-style-restore-1'));
        await waitFor(() => expect(api.rollback).toHaveBeenCalledWith('b1', 1));
    });
});
