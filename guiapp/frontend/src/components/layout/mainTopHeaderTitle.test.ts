import { describe, expect, it } from 'vitest';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { utilitiesPageTitle } from '../../i18n/utilitiesLabels';

describe('getHeaderTitle', () => {
    const t = (key: string) => key;

    it('uses the shared utilities title for the utilities tab', () => {
        expect(getHeaderTitle('utilities', 'zh-Hans', t)).toBe(utilitiesPageTitle('zh-Hans'));
        expect(getHeaderTitle('utilities', 'zh-Hant', t)).toBe(utilitiesPageTitle('zh-Hant'));
        expect(getHeaderTitle('utilities', 'en', t)).toBe(utilitiesPageTitle('en'));
    });

    it('names the Knowledge page when the settings knowledge tab is active', () => {
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'knowledge')).toBe('知识库');
        expect(getHeaderTitle('settings', 'zh-Hant', t, false, 'knowledge')).toBe('知識庫');
        expect(getHeaderTitle('settings', 'en', t, false, 'knowledge')).toBe('Knowledge base');
    });

    it('names the LaTeX template library instead of falling through to About', () => {
        expect(getHeaderTitle('latex-templates', 'zh-Hans', t)).toBe('LaTeX 模板');
        expect(getHeaderTitle('latex-templates', 'zh-Hant', t)).toBe('LaTeX 模板');
        expect(getHeaderTitle('latex-templates', 'en', t)).toBe('LaTeX Templates');
    });

    it('names the bot workspace', () => {
        expect(getHeaderTitle('bots', 'zh-Hans', t)).toBe('Bot 管理');
        expect(getHeaderTitle('bots', 'en', t)).toBe('Bot management');
    });

    it('names the cloud drive on the files tab', () => {
        expect(getHeaderTitle('files', 'zh-Hans', t)).toBe('云盘');
        expect(getHeaderTitle('files', 'zh-Hant', t)).toBe('雲端硬碟');
        expect(getHeaderTitle('files', 'en', t)).toBe('Cloud drive');
    });

    it('keeps the global settings title for other settings tabs', () => {
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'general')).toBe('globalSettings');
        expect(getHeaderTitle('settings', 'zh-Hans', t)).toBe('globalSettings');
    });
});
