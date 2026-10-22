import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { BotEditDialog } from './BotEditDialog';
import { BOT_COLORS, appearancesForUser, botNameTaken, defaultAppearance, nextAppearance, saveBotAppearance } from './botAppearance';

const labels = { title: 'New', name: 'Name', description: 'Desc', save: 'Save', cancel: 'Cancel', close: 'Close', nameTaken: 'taken' };

describe('bot appearance', () => {
    afterEach(() => { cleanup(); window.localStorage.clear(); });

    it('persists color and shape per user and bot', () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        expect(appearancesForUser('alice')).toEqual({ bot_1: { color: '#2563eb', shape: 'cloud' } });
        expect(appearancesForUser('bob')).toEqual({});
        saveBotAppearance('alice', 'bot_1', { color: 'red', shape: 'cloud' });
        expect(appearancesForUser('alice')).toEqual({});
    });

    it('picks an unused color next and a stable default', () => {
        expect(nextAppearance([{ color: BOT_COLORS[0], shape: 'circle' }]).color).toBe(BOT_COLORS[1]);
        expect(defaultAppearance('Bot 1')).toEqual(defaultAppearance('Bot 1'));
    });

    it('treats names case- and space-insensitively, except the bot itself', () => {
        const bots = [{ id: 'a', title: 'Writer  Bot' }];
        expect(botNameTaken('writer bot', bots)).toBe(true);
        expect(botNameTaken('writer bot', bots, 'a')).toBe(false);
        expect(botNameTaken('other', bots)).toBe(false);
    });

    it('dialog submits name, color and shape and blocks empty or taken names', () => {
        const onSubmit = vi.fn();
        render(<BotEditDialog initialTitle="Bot 2" initialDescription="" labels={labels} notice="" saving={false}
            onClose={() => {}} onSubmit={onSubmit} isNameTaken={name => name === 'Taken'}
            initialAppearance={{ color: BOT_COLORS[0], shape: 'circle' }} />);
        const input = screen.getByDisplayValue('Bot 2');
        fireEvent.change(input, { target: { value: 'Taken' } });
        expect(screen.getByTestId('bot-edit-name-taken')).toBeTruthy();
        expect((screen.getByTestId('bot-edit-save') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(input, { target: { value: '  ' } });
        expect((screen.getByTestId('bot-edit-save') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(input, { target: { value: '写手' } });
        fireEvent.click(screen.getByTestId('bot-edit-color-' + BOT_COLORS[3].slice(1)));
        fireEvent.click(screen.getByTestId('bot-edit-shape-leaf'));
        expect(screen.getByTestId('bot-edit-preview').getAttribute('data-shape')).toBe('leaf');
        fireEvent.click(screen.getByTestId('bot-edit-save'));
        expect(onSubmit).toHaveBeenCalledWith('写手', '', { color: BOT_COLORS[3], shape: 'leaf' });
    });
});
