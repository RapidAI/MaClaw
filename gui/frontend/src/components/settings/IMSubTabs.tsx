import type { Dispatch, SetStateAction } from 'react';
import { pillButtonStyle, textForLang } from './imSettingsShared';

export type IMSubTab = 'qq' | 'telegram' | 'weixin' | 'lansenger' | 'thirdparty';

type IMSubTabsProps = {
    lang: string;
    imSubTab: IMSubTab;
    setImSubTab: Dispatch<SetStateAction<IMSubTab>>;
    showLansenger?: boolean;
};

export const imSubTabOptions = (lang: string, options: { showLansenger?: boolean } = {}): Array<{ key: IMSubTab; label: string }> => ([
    { key: 'qq', label: textForLang(lang, 'QQ Bot', '\u0051\u0051 \u673a\u5668\u4eba', '\u0051\u0051 \u6a5f\u5668\u4eba') },
    { key: 'telegram', label: 'Telegram Bot' },
    { key: 'weixin', label: textForLang(lang, 'WeChat', '\u5fae\u4fe1', '\u5fae\u4fe1') },
    ...(options.showLansenger ? [{ key: 'lansenger' as const, label: textForLang(lang, 'Lansenger', '\u84dd\u4fe1', '\u85cd\u4fe1') }] : []),
    { key: 'thirdparty', label: textForLang(lang, 'Third-party Access', '\u7b2c\u4e09\u65b9\u63a5\u5165', '\u7b2c\u4e09\u65b9\u63a5\u5165') },
]);

export const IMSubTabs = ({ lang, imSubTab, setImSubTab, showLansenger = false }: IMSubTabsProps) => (
    <div style={{ display: 'flex', gap: '6px', marginBottom: '16px', flexWrap: 'wrap' }}>
        {imSubTabOptions(lang, { showLansenger }).map((tab) => (
            <button
                key={tab.key}
                type="button"
                onClick={() => setImSubTab(tab.key)}
                style={pillButtonStyle(imSubTab === tab.key)}
            >
                {tab.label}
            </button>
        ))}
    </div>
);
