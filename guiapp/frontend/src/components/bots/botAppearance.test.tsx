import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { BotEditDialog } from './BotEditDialog';
import { BOT_COLORS, appearancesForUser, botNameTaken, captureBotLookRevisions, defaultAppearance, hubAppearance, hydrateBotLooks, looksToMigrate, markLookMigrated, mergeLooks, nextAppearance, resetBotLookCacheForTest, saveBotAppearance, setBotLookLoaderForTest, storeHubLooks } from './botAppearance';

const labels = { title: 'New', name: 'Name', description: 'Desc', save: 'Save', cancel: 'Cancel', close: 'Close', nameTaken: 'taken' };

describe('bot appearance', () => {
    afterEach(() => { cleanup(); window.localStorage.clear(); resetBotLookCacheForTest(); });

    it('persists color and shape per user and bot', () => {
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        expect(appearancesForUser('alice')).toEqual({ bot_1: { color: '#2563eb', shape: 'cloud' } });
        expect(appearancesForUser('bob')).toEqual({});
        saveBotAppearance('alice', 'bot_1', { color: 'red', shape: 'cloud' });
        expect(appearancesForUser('alice')).toEqual({});
        expect(localStorage.getItem('maclaw.desktopBotAppearance.v1')).toBeNull();
    });

    it('does not write the browser store when it is already full', () => {
        const proto = Object.getPrototypeOf(window.localStorage) as Storage;
        const setItem = proto.setItem;
        proto.setItem = () => { throw new DOMException("Failed to execute 'setItem' on 'Storage': Setting the value of 'maclaw.desktopBotAppearance.v1' exceeded the quota.", 'QuotaExceededError'); };
        try {
            expect(() => saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' })).not.toThrow();
            expect(() => markLookMigrated('alice', 'bot_1')).not.toThrow();
        } finally {
            proto.setItem = setItem;
        }
        expect(appearancesForUser('alice')).toEqual({ bot_1: { color: '#2563eb', shape: 'cloud' } });
        expect(looksToMigrate('alice', [{ id: 'bot_1' }])).toEqual([]);
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

describe('Hub-stored bot look', () => {
    beforeEach(() => { window.localStorage.clear(); resetBotLookCacheForTest(); });
    afterEach(() => resetBotLookCacheForTest());

    it('reads a valid Hub look and ignores missing or unknown fields', () => {
        expect(hubAppearance({ color: '#2563eb', shape: 'cloud' })).toEqual({ color: '#2563eb', shape: 'cloud' });
        expect(hubAppearance({})).toBeNull();
        expect(hubAppearance({ color: '#2563eb' })).toBeNull();
        expect(hubAppearance({ color: 'red', shape: 'cloud' })).toBeNull();
        expect(hubAppearance({ color: '#2563eb', shape: 'hexagon' })).toBeNull();
    });

    it('prefers the Hub look and falls back to local storage when Hub echoes none', () => {
        const local = { a: { color: '#16a34a', shape: 'leaf' as const }, b: { color: '#7c3aed', shape: 'pill' as const } };
        const merged = mergeLooks(local, [{ id: 'a', color: '#dc2626', shape: 'square' }, { id: 'b' }]);
        expect(merged.a).toEqual({ color: '#dc2626', shape: 'square' });
        expect(merged.b).toEqual(local.b);
    });

    it('keeps a look saved after the hub list was requested', () => {
        saveBotAppearance('alice', 'a', { color: '#16a34a', shape: 'leaf' });
        const since = captureBotLookRevisions('alice');
        const local = appearancesForUser('alice');
        expect(mergeLooks(local, [{ id: 'a', color: '#dc2626', shape: 'square' }], { userId: 'alice', revision: since }).a)
            .toEqual({ color: '#dc2626', shape: 'square' });
        saveBotAppearance('alice', 'a', { color: '#ca8a04', shape: 'pill' });
        expect(mergeLooks(appearancesForUser('alice'), [{ id: 'a', color: '#dc2626', shape: 'square' }], { userId: 'alice', revision: since }).a)
            .toEqual({ color: '#ca8a04', shape: 'pill' });
        saveBotAppearance('alice', 'a', null);
        expect(mergeLooks(appearancesForUser('alice'), [{ id: 'a', color: '#dc2626', shape: 'square' }], { userId: 'alice', revision: since }).a)
            .toBeUndefined();
    });

    it('stores a hub look and leaves one saved after that list alone', () => {
        const since = captureBotLookRevisions('alice');
        storeHubLooks('alice', [{ id: 'a', color: '#dc2626', shape: 'square' }, { id: 'b' }], { userId: 'alice', revision: since });
        expect(appearancesForUser('alice').a).toEqual({ color: '#dc2626', shape: 'square' });
        expect(appearancesForUser('alice').b).toBeUndefined();
        const listed = captureBotLookRevisions('alice');
        saveBotAppearance('alice', 'a', { color: '#16a34a', shape: 'leaf' });
        storeHubLooks('alice', [{ id: 'a', color: '#dc2626', shape: 'square' }], { userId: 'alice', revision: listed });
        expect(appearancesForUser('alice').a).toEqual({ color: '#16a34a', shape: 'leaf' });
    });

    it('migrates local looks only for bots without a Hub look, once', () => {
        saveBotAppearance('u1', 'a', { color: '#16a34a', shape: 'leaf' });
        saveBotAppearance('u1', 'b', { color: '#7c3aed', shape: 'pill' });
        const items = [{ id: 'a' }, { id: 'b', color: '#dc2626', shape: 'square' }, { id: 'c' }];
        expect(looksToMigrate('u1', items)).toEqual([{ id: 'a', look: { color: '#16a34a', shape: 'leaf' } }]);
        markLookMigrated('u1', 'a');
        expect(looksToMigrate('u1', items)).toEqual([]);
        expect(looksToMigrate('u2', items)).toEqual([]);
    });

    it('copies a changed look again and keeps the mark when the look is cleared or taken from Hub', async () => {
        const listed = [{ id: 'bot_1' }];
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        markLookMigrated('alice', 'bot_1');
        expect(looksToMigrate('alice', listed)).toEqual([]);
        saveBotAppearance('alice', 'bot_1', { color: '#2563eb', shape: 'cloud' });
        expect(looksToMigrate('alice', listed)).toEqual([]);

        saveBotAppearance('alice', 'bot_1', { color: '#16a34a', shape: 'leaf' });
        expect(looksToMigrate('alice', listed)).toEqual([{ id: 'bot_1', look: { color: '#16a34a', shape: 'leaf' } }]);

        markLookMigrated('alice', 'bot_1');
        const since = captureBotLookRevisions('alice');
        storeHubLooks('alice', [{ id: 'bot_1', color: '#dc2626', shape: 'square' }], { userId: 'alice', revision: since });
        expect(appearancesForUser('alice').bot_1).toEqual({ color: '#dc2626', shape: 'square' });
        expect(looksToMigrate('alice', listed)).toEqual([]);

        saveBotAppearance('alice', 'bot_1', { color: '#ca8a04', shape: 'pill' }, { keepMark: true });
        expect(looksToMigrate('alice', listed)).toEqual([]);

        saveBotAppearance('alice', 'bot_1', null);
        expect(looksToMigrate('alice', listed)).toEqual([]);
        saveBotAppearance('alice', 'bot_1', { color: '#ca8a04', shape: 'pill' }, { keepRevision: true });
        expect(appearancesForUser('alice').bot_1).toEqual({ color: '#ca8a04', shape: 'pill' });
        expect(looksToMigrate('alice', listed)).toEqual([]);
    });

    it('does not restore a copied mark after the look changes during load', async () => {
        let release: (file: { looks: Record<string, { color: string; shape: string }>; migrated: string[] }) => void = () => {};
        const pending = hydrateBotLooks('alice');
        saveBotAppearance('alice', 'bot_1', { color: '#16a34a', shape: 'leaf' });
        release({
            looks: { bot_1: { color: '#2563eb', shape: 'cloud' } },
            migrated: ['bot_1'],
        });
        expect(await pending).toEqual({ bot_1: { color: '#16a34a', shape: 'leaf' } });
        expect(looksToMigrate('alice', [{ id: 'bot_1' }])).toEqual([{ id: 'bot_1', look: { color: '#16a34a', shape: 'leaf' } }]);
    });

    it('imports a WebView document into the session and drops those keys after SQLite accepts them', async () => {
        localStorage.setItem('maclaw.desktopBotAppearance.v1', JSON.stringify({
            alice: { bot_1: { color: '#2563eb', shape: 'cloud' } },
        }));
        localStorage.setItem('maclaw.desktopBotAppearance.v1.migrated', JSON.stringify({ alice: ['bot_1'] }));
        const calls: string[][] = [];
        setBotLookLoaderForTest(async (userId, looksJSON, migratedJSON) => {
            calls.push([userId, looksJSON, migratedJSON]);
            return { looks: { bot_1: { color: '#2563eb', shape: 'cloud' } }, migrated: ['bot_1'] };
        });
        expect(await hydrateBotLooks('alice')).toEqual({ bot_1: { color: '#2563eb', shape: 'cloud' } });
        expect(calls[0][0]).toBe('alice');
        expect(calls[0][1]).toContain('#2563eb');
        expect(localStorage.getItem('maclaw.desktopBotAppearance.v1')).toBeNull();
        expect(localStorage.getItem('maclaw.desktopBotAppearance.v1.migrated')).toBeNull();
        expect(looksToMigrate('alice', [{ id: 'bot_1' }])).toEqual([]);
    });

    it('keeps a look saved during load and drops one cleared during load', async () => {
        let release: (file: { looks: Record<string, { color: string; shape: string }>; migrated: string[] }) => void = () => {};
        setBotLookLoaderForTest(() => new Promise(resolve => { release = resolve; }));
        const pending = hydrateBotLooks('alice');
        saveBotAppearance('alice', 'bot_keep', { color: '#16a34a', shape: 'leaf' });
        saveBotAppearance('alice', 'bot_drop', { color: '#2563eb', shape: 'cloud' });
        saveBotAppearance('alice', 'bot_drop', null);
        release({
            looks: {
                bot_keep: { color: '#111111', shape: 'blob' },
                bot_drop: { color: '#db2777', shape: 'pill' },
            },
            migrated: [],
        });
        expect(await pending).toEqual({ bot_keep: { color: '#16a34a', shape: 'leaf' } });
    });

    it('reads stored rows when the legacy import fails and keeps the WebView key', async () => {
        localStorage.setItem('maclaw.desktopBotAppearance.v1', JSON.stringify({
            alice: { bot_1: { color: '#111111', shape: 'blob' } },
        }));
        setBotLookLoaderForTest(async (_userId, looksJSON) => {
            if (looksJSON) throw new Error('import failed');
            return { looks: { bot_2: { color: '#2563eb', shape: 'cloud' } }, migrated: [] };
        });
        expect(await hydrateBotLooks('alice')).toEqual({
            bot_1: { color: '#111111', shape: 'blob' },
            bot_2: { color: '#2563eb', shape: 'cloud' },
        });
        expect(localStorage.getItem('maclaw.desktopBotAppearance.v1')).toContain('#111111');
    });

    it('keeps a WebView document when SQLite cannot take it', async () => {
        localStorage.setItem('maclaw.desktopBotAppearance.v1', JSON.stringify({
            alice: { a: { color: '#16a34a', shape: 'leaf' } },
        }));
        expect(await hydrateBotLooks('alice')).toEqual({ a: { color: '#16a34a', shape: 'leaf' } });
        expect(localStorage.getItem('maclaw.desktopBotAppearance.v1')).toContain('#16a34a');
    });
});
