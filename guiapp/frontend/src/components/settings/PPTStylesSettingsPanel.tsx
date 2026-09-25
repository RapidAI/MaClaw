import { useEffect, useRef, useState } from 'react';
import { localizedStyleLabel, localizedStyleSummary } from '../ai/pptStyleChoice';
import { localizeText } from '../../i18n';
import './PPTStylesSettingsPanel.css';

export type PPTStyleCard = {
    id: string;
    label: string;
    summary: string;
    keywords?: string[];
    builtin: boolean;
    cover_dark?: boolean;
    preview_url?: string;
};

type PPTStyleBridge = {
    ListPPTStyles?: () => Promise<PPTStyleCard[]>;
    ListPPTStyleChoices?: () => Promise<PPTStyleCard[]>;
    PPTStylePreview?: (id: string, lang: string) => Promise<string>;
    GeneratePPTStyle: (request: string, lang: string) => Promise<PPTStyleCard>;
    DeletePPTStyle: (id: string) => Promise<void>;
};

function pptStyleBridge(): PPTStyleBridge | null {
    const app = (window as unknown as { go?: { main?: { App?: PPTStyleBridge } } }).go?.main?.App;
    if (!app?.GeneratePPTStyle || !app.DeletePPTStyle) return null;
    if (!app.ListPPTStyleChoices && !app.ListPPTStyles) return null;
    return app;
}

export function PPTStylesSettingsPanel({ lang }: { lang: string }) {
    const t = (en: string, zhHans: string, zhHant: string) => localizeText(lang, en, zhHans, zhHant);
    const [styles, setStyles] = useState<PPTStyleCard[]>([]);
    const [request, setRequest] = useState('');
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const refreshTicket = useRef(0);
    const loadedOnce = useRef(false);
    const previewLang = useRef(lang);
    const stylesRef = useRef<PPTStyleCard[]>([]);
    stylesRef.current = styles;

    const refresh = async () => {
        const bridge = pptStyleBridge();
        if (!bridge) {
            setError(t('Style list is unavailable in this view.', '当前界面读不到风格列表。', '目前介面讀不到風格列表。'));
            setLoading(false);
            return;
        }
        const ticket = refreshTicket.current + 1;
        refreshTicket.current = ticket;
        if (!loadedOnce.current) setLoading(true);
        setError('');
        try {
            if (bridge.ListPPTStyleChoices) {
                const list = await bridge.ListPPTStyleChoices();
                if (ticket !== refreshTicket.current) return;
                const cards = Array.isArray(list) ? list : [];
                const keepCovers = previewLang.current === lang;
                previewLang.current = lang;
                setStyles((prev) => cards.map((card) => ({
                    ...card,
                    preview_url: keepCovers ? (card.preview_url || prev.find((item) => item.id === card.id)?.preview_url) : undefined,
                })));
                loadedOnce.current = true;
                setLoading(false);
                if (bridge.PPTStylePreview) {
                    const pending = cards.filter((card) => {
                        if (card.preview_url) return false;
                        if (!keepCovers) return true;
                        return !stylesRef.current.find((item) => item.id === card.id)?.preview_url;
                    });
                    let cursor = 0;
                    const worker = async () => {
                        while (cursor < pending.length) {
                            const card = pending[cursor];
                            cursor += 1;
                            if (!card || ticket !== refreshTicket.current) return;
                            try {
                                const url = await bridge.PPTStylePreview!(card.id, lang);
                                if (ticket !== refreshTicket.current) return;
                                setStyles((prev) => prev.map((item) => item.id === card.id ? { ...item, preview_url: url } : item));
                            } catch {
                                // The card stays visible without a cover.
                            }
                        }
                    };
                    await Promise.all([worker(), worker()]);
                }
                return;
            }
            const list = await bridge.ListPPTStyles!();
            if (ticket !== refreshTicket.current) return;
            setStyles(Array.isArray(list) ? list : []);
        } catch (err) {
            if (ticket !== refreshTicket.current) return;
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            if (ticket === refreshTicket.current) setLoading(false);
        }
    };

    useEffect(() => {
        void refresh();
    }, [lang]);

    const generate = async () => {
        const text = request.trim();
        if (!text || busy) return;
        const bridge = pptStyleBridge();
        if (!bridge) return;
        setBusy(true);
        setError('');
        try {
            const created = await bridge.GeneratePPTStyle(text, lang);
            setRequest('');
            setStyles((prev) => {
                const rest = prev.filter((item) => item.id !== created.id);
                return [...rest, created];
            });
            await refresh();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            setBusy(false);
        }
    };

    const remove = async (id: string) => {
        const bridge = pptStyleBridge();
        if (!bridge || busy) return;
        setBusy(true);
        setError('');
        try {
            await bridge.DeletePPTStyle(id);
            setStyles((prev) => prev.filter((item) => item.id !== id));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            setBusy(false);
        }
    };

    return (
        <section className="ppt-styles" data-testid="ppt-styles-panel">
            <header className="ppt-styles-head">
                <h2>{t('PPT Styles', 'PPT 风格', 'PPT 風格')}</h2>
                <p>
                    {t(
                        'These styles are available while making a deck. Name one, or let the purpose pick it. A generated style is saved here and can be cited the same way.',
                        '制作 PPT 时可以直接点名这些风格，也可以按用途自动选用。按描述生成的风格会保存在这里，之后同样可以引用。',
                        '製作 PPT 時可以直接點名這些風格，也可以按用途自動選用。按描述生成的風格會保存在這裡，之後同樣可以引用。',
                    )}
                </p>
            </header>

            {error ? <p className="ppt-styles-error" role="alert">{error}</p> : null}

            {loading && styles.length === 0 ? (
                <p className="ppt-styles-status" role="status">{t('Loading styles…', '正在加载风格…', '正在載入風格…')}</p>
            ) : (
                <ul className="ppt-styles-grid">
                    {styles.map((style) => (
                        <li key={style.id} className="ppt-style-card" data-testid="ppt-style-card">
                            {style.preview_url ? (
                                <img src={style.preview_url} alt={t(`${localizedStyleLabel(style, lang)} preview`, `${localizedStyleLabel(style, lang)} 预览`, `${localizedStyleLabel(style, lang)} 預覽`)} />
                            ) : (
                                <div className="ppt-style-missing" aria-hidden="true" />
                            )}
                            <div className="ppt-style-meta">
                                <div className="ppt-style-title-row">
                                    <strong>{localizedStyleLabel(style, lang)}</strong>
                                    <span className="ppt-style-badge">{style.builtin ? t('Built-in', '内置', '內置') : t('Custom', '自定义', '自訂')}</span>
                                </div>
                                <p>{localizedStyleSummary(style, lang)}</p>
                                <code>{style.id}</code>
                                {!style.builtin ? (
                                    <button type="button" className="btn-secondary ppt-style-delete" onClick={() => { void remove(style.id); }} disabled={busy}>
                                        {t('Delete', '删除', '刪除')}
                                    </button>
                                ) : null}
                            </div>
                        </li>
                    ))}
                </ul>
            )}

            <form
                className="ppt-styles-create"
                onSubmit={(event) => {
                    event.preventDefault();
                    void generate();
                }}
            >
                <label htmlFor="ppt-style-request">{t('Describe a style', '描述想要的风格', '描述想要的風格')}</label>
                <textarea
                    id="ppt-style-request"
                    value={request}
                    onChange={(event) => setRequest(event.target.value)}
                    rows={3}
                    placeholder={t(
                        'For example: a dark cyber deck with magenta neon and cool gray paper',
                        '例如：深色赛博风，洋红霓虹，内容页用冷灰',
                        '例如：深色賽博風，洋紅霓虹，內容頁用冷灰',
                    )}
                />
                <button type="submit" className="btn-primary" disabled={busy || request.trim() === ''}>
                    {busy ? t('Generating…', '正在生成…', '正在生成…') : t('Generate style', '生成风格', '生成風格')}
                </button>
            </form>
        </section>
    );
}
