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

    it('names every settings tab after the label its rail entry shows', () => {
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'knowledge')).toBe('知识库');
        expect(getHeaderTitle('settings', 'zh-Hant', t, false, 'knowledge')).toBe('知識庫');
        expect(getHeaderTitle('settings', 'en', t, false, 'knowledge')).toBe('Knowledge');
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'email')).toBe('邮箱');
        expect(getHeaderTitle('settings', 'zh-Hant', t, false, 'email')).toBe('郵箱');
        expect(getHeaderTitle('settings', 'en', t, false, 'email')).toBe('Email');
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'im')).toBe('IM');
        expect(getHeaderTitle('settings', 'en', t, false, 'im')).toBe('IM');
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'embedding')).toBe('AI 模型');
        expect(getHeaderTitle('settings', 'zh-Hant', t, false, 'embedding')).toBe('AI 模型');
        expect(getHeaderTitle('settings', 'en', t, false, 'embedding')).toBe('AI Model');
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'general')).toBe('通用设置');
        expect(getHeaderTitle('settings', 'zh-Hant', t, false, 'general')).toBe('通用設定');
        expect(getHeaderTitle('settings', 'en', t, false, 'general')).toBe('General');
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'llm')).toBe('大模型配置');
    });

    it('keeps the generic settings title for unknown settings tabs', () => {
        expect(getHeaderTitle('settings', 'zh-Hans', t, false, 'not-a-tab')).toBe('globalSettings');
        expect(getHeaderTitle('settings', 'zh-Hans', t)).toBe('globalSettings');
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
});
