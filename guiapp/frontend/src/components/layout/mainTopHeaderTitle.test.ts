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
});
