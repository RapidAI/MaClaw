import { useEffect, useState } from 'react';
import { localizeText } from '../../i18n';
import { localizedStyleLabel, recommendPPTStyle, type PPTStyleChoice, type PPTStyleChoiceOption } from './pptStyleChoice';
import './PPTStyleChooser.css';

type ChoiceBridge = {
    ListPPTStyleChoices: () => Promise<PPTStyleChoiceOption[]>;
};

function choiceBridge(): ChoiceBridge | null {
    const app = (window as unknown as { go?: { main?: { App?: ChoiceBridge } } }).go?.main?.App;
    if (!app?.ListPPTStyleChoices) return null;
    return app;
}

export function PPTStyleChooser({
    lang,
    inputValue,
    onChoice,
}: {
    lang: string;
    inputValue: string;
    onChoice: (choice: PPTStyleChoice | null) => void;
}) {
    const t = (en: string, zhHans: string, zhHant: string) => localizeText(lang, en, zhHans, zhHant);
    const [styles, setStyles] = useState<PPTStyleChoiceOption[]>([]);
    const [manualId, setManualId] = useState<string | null>(null);
    const [error, setError] = useState('');

    useEffect(() => {
        const bridge = choiceBridge();
        if (!bridge) return;
        let cancelled = false;
        let ticket = 0;
        const load = () => {
            const mine = ++ticket;
            void bridge.ListPPTStyleChoices().then((list) => {
                if (cancelled || mine !== ticket) return;
                setStyles(Array.isArray(list) ? list.filter((item) => item?.id && item?.label) : []);
                setError('');
            }).catch((err: unknown) => {
                if (!cancelled && mine === ticket) setError(err instanceof Error ? err.message : String(err || ''));
            });
        };
        load();
        window.addEventListener('focus', load);
        return () => {
            cancelled = true;
            window.removeEventListener('focus', load);
        };
    }, []);

    const recommended = recommendPPTStyle(styles, inputValue);
    const selected = manualId && styles.some((item) => item.id === manualId) ? manualId : recommended;
    const matched = styles.some((item) => (item.keywords || []).some((keyword) => {
        const kw = String(keyword || "").trim().toLowerCase();
        return Array.from(kw).length >= 2 && inputValue.toLowerCase().includes(kw);
    }));

    useEffect(() => {
        const found = styles.find((item) => item.id === selected);
        onChoice(found ? { id: found.id, label: localizedStyleLabel(found, lang) } : null);
    }, [lang, onChoice, selected, styles]);

    if (styles.length === 0 && !error) return null;

    return (
        <section className="ppt-style-chooser" data-testid="ppt-style-chooser" aria-label={t('PPT style', 'PPT 风格', 'PPT 風格')}>
            <div className="ppt-style-chooser-head">
                <span>{manualId
                    ? t('This send uses the style you picked. Choose again or follow the purpose.', '这次将使用你选的风格，仍可改选，或改回跟随用途。', '這次將使用你選的風格，仍可改選，或改回跟隨用途。')
                    : matched
                        ? t('Recommended from your request. Pick another style if you want.', '已按用途推荐一套，也可以改选其它风格。', '已按用途推薦一套，也可以改選其它風格。')
                        : t('No purpose matched yet. Business is selected until you pick another.', '还没对上用途，先用商务汇报，也可以直接改选。', '還沒對上用途，先用商務匯報，也可以直接改選。')}</span>
                {manualId ? (
                    <button type="button" className="ppt-style-chooser-follow" onClick={() => setManualId(null)}>
                        {t('Follow purpose', '跟随用途', '跟隨用途')}
                    </button>
                ) : null}
            </div>
            {error ? <p className="ppt-style-chooser-error" role="alert">{error}</p> : null}
            <div className="ppt-style-chooser-row" role="radiogroup">
                {styles.map((style) => {
                    const active = style.id === selected;
                    const recommendedChip = style.id === recommended && matched;
                    return (
                        <button
                            key={style.id}
                            type="button"
                            role="radio"
                            aria-checked={active}
                            aria-label={recommendedChip ? `${localizedStyleLabel(style, lang)}，${t('Suggested', '推荐', '推薦')}` : localizedStyleLabel(style, lang)}
                            data-testid="ppt-style-choice"
                            className={`ppt-style-chip${active ? ' is-active' : ''}`}
                            onClick={() => {
                                if (manualId === null && style.id === recommended) return;
                                setManualId(style.id);
                            }}
                        >
                            <i style={{ background: style.accent ? `#${style.accent.replace('#', '')}` : 'var(--theme-primary, #2f6fbc)' }} aria-hidden="true" />
                            <span>{localizedStyleLabel(style, lang)}</span>
                            {recommendedChip ? <em>{t('Suggested', '推荐', '推薦')}</em> : null}
                        </button>
                    );
                })}
            </div>
        </section>
    );
}
