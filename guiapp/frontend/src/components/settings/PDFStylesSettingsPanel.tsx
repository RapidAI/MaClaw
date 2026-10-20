import { useEffect, useRef, useState } from 'react';
import { localizeText } from '../../i18n';
import './PDFStylesSettingsPanel.css';

export type PDFStyleCard = {
    id: string;
    label: string;
    label_en?: string;
    label_hant?: string;
    summary: string;
    summary_en?: string;
    summary_hant?: string;
    motif: string;
    builtin: boolean;
    active: boolean;
    navy: string;
    accent: string;
    gold: string;
    ink: string;
};

type PDFStyleList = {
    styles?: PDFStyleCard[];
    warning?: string;
};

type PDFStyleBridge = {
    ListPDFStyles: () => Promise<PDFStyleList>;
    SetPDFStyle: (id: string) => Promise<void>;
    GeneratePDFStyle: (request: string) => Promise<PDFStyleCard>;
    DeletePDFStyle: (id: string) => Promise<void>;
};

function pdfStyleBridge(): PDFStyleBridge | null {
    const app = (window as unknown as { go?: { main?: { App?: PDFStyleBridge } } }).go?.main?.App;
    if (!app?.ListPDFStyles || !app.SetPDFStyle || !app.GeneratePDFStyle || !app.DeletePDFStyle) return null;
    return app;
}

function hex(value: string): string {
    const raw = String(value || '').replace('#', '').trim();
    return /^[0-9a-fA-F]{6}$/.test(raw) ? `#${raw}` : '#1C1917';
}

function styleText(style: PDFStyleCard, lang: string, field: 'label' | 'summary'): string {
    const code = String(lang || '').toLowerCase();
    if (code.startsWith('en') && style[`${field}_en`]) return style[`${field}_en`] || '';
    if ((code.includes('hant') || code.endsWith('-tw') || code.endsWith('-hk')) && style[`${field}_hant`]) {
        return style[`${field}_hant`] || '';
    }
    return style[field];
}

function motifName(motif: string, t: (en: string, zhHans: string, zhHant: string) => string): string {
    switch (motif) {
    case 'rail':
        return t('Rail', '立柱', '立柱');
    case 'frame':
        return t('Frame', '双框', '雙框');
    case 'band':
        return t('Band', '色带', '色帶');
    case 'ledger':
        return t('Ledger', '粗线', '粗線');
    default:
        return t('Masthead', '顶栏', '頂欄');
    }
}

function PDFStylePreview({ style, label }: { style: PDFStyleCard; label: string }) {
    const navy = hex(style.navy);
    const accent = hex(style.accent);
    const gold = hex(style.gold);
    const ink = hex(style.ink);
    const title = style.motif === 'frame' ? accent : style.motif === 'rail' ? ink : navy;
    return (
        <div className={`pdf-style-page motif-${style.motif || 'masthead'}`} aria-hidden="true">
            {style.motif === 'rail' ? <i className="pdf-style-rail" style={{ background: accent }} /> : null}
            {style.motif === 'frame' ? (
                <i className="pdf-style-frame" style={{ borderColor: gold }}>
                    <i style={{ borderColor: navy }} />
                </i>
            ) : null}
            {style.motif === 'band' ? (
                <>
                    <i className="pdf-style-band" style={{ background: navy }} />
                    <i className="pdf-style-notch" style={{ background: accent }} />
                </>
            ) : null}
            {style.motif === 'ledger' ? <i className="pdf-style-ledger" style={{ background: navy }} /> : null}
            {style.motif !== 'rail' && style.motif !== 'frame' && style.motif !== 'band' && style.motif !== 'ledger' ? (
                <i className="pdf-style-mast">
                    <i style={{ background: navy }} />
                    <i style={{ background: accent }} />
                </i>
            ) : null}
            <div className="pdf-style-copy">
                <strong style={{ color: title }}>{label}</strong>
                <i className="pdf-style-rule" style={{ background: style.motif === 'frame' || style.motif === 'ledger' ? gold : accent }} />
                <i className="pdf-style-line" />
                <i className="pdf-style-line short" />
                <span className="pdf-style-grid">
                    <b style={{ color: navy }}>A</b>
                    <b style={{ color: navy }}>B</b>
                    <s />
                    <s />
                </span>
            </div>
        </div>
    );
}

export function PDFStylesSettingsPanel({ lang }: { lang: string }) {
    const t = (en: string, zhHans: string, zhHant: string) => localizeText(lang, en, zhHans, zhHant);
    const [styles, setStyles] = useState<PDFStyleCard[]>([]);
    const [request, setRequest] = useState('');
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const refreshTicket = useRef(0);

    const refresh = async () => {
        const bridge = pdfStyleBridge();
        if (!bridge) {
            setError(t('Style list is unavailable in this view.', '当前界面读不到风格列表。', '目前介面讀不到風格列表。'));
            setLoading(false);
            return;
        }
        const ticket = refreshTicket.current + 1;
        refreshTicket.current = ticket;
        setError('');
        try {
            const list = await bridge.ListPDFStyles();
            if (ticket !== refreshTicket.current) return;
            setStyles(Array.isArray(list?.styles) ? list.styles : []);
            if (list?.warning) setError(list.warning);
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

    const choose = async (id: string) => {
        const bridge = pdfStyleBridge();
        if (!bridge || busy) return;
        setBusy(true);
        setError('');
        try {
            await bridge.SetPDFStyle(id);
            refreshTicket.current += 1;
            setStyles((prev) => prev.map((style) => ({ ...style, active: style.id === id })));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            setBusy(false);
        }
    };

    const generate = async () => {
        const text = request.trim();
        const bridge = pdfStyleBridge();
        if (!text || !bridge || busy) return;
        setBusy(true);
        setError('');
        try {
            const created = await bridge.GeneratePDFStyle(text);
            setRequest('');
            refreshTicket.current += 1;
            setStyles((prev) => {
                const rest = prev.filter((style) => style.id !== created.id);
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
        const bridge = pdfStyleBridge();
        if (!bridge || busy) return;
        setBusy(true);
        setError('');
        try {
            await bridge.DeletePDFStyle(id);
            refreshTicket.current += 1;
            setStyles((prev) => {
                const removed = prev.find((style) => style.id === id);
                const next = prev.filter((style) => style.id !== id);
                if (!removed?.active) return next;
                return next.map((style, index) => ({ ...style, active: index === 0 }));
            });
            await refresh();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            setBusy(false);
        }
    };

    return (
        <section className="pdf-styles" data-testid="pdf-styles-panel">
            <header className="pdf-styles-head">
                <h2>{t('PDF Styles', 'PDF 风格', 'PDF 風格')}</h2>
                <p>
                    {t(
                        'Every generated PDF uses the selected style. The first style, Gazette, is the default. The five built-in styles differ in color and layout. A description can add another style.',
                        '之后生成的 PDF 都使用这里选中的风格。默认是第一套「公报」。五套内置风格的色彩和版式各自不同。用一句话还可以再增加一套。',
                        '之後生成的 PDF 都使用這裡選中的風格。預設是第一套「公報」。五套內置風格的色彩和版式各自不同。用一句話還可以再增加一套。',
                    )}
                </p>
            </header>

            {error ? <p className="pdf-styles-error" role="alert">{error}</p> : null}

            {loading && styles.length === 0 ? (
                <p className="pdf-styles-status" role="status">{t('Loading styles…', '正在加载风格…', '正在載入風格…')}</p>
            ) : (
                <ul className="pdf-styles-grid">
                    {styles.map((style) => (
                        <li key={style.id} className={style.active ? 'pdf-style-card active' : 'pdf-style-card'} data-testid="pdf-style-card">
                            <button
                                type="button"
                                className="pdf-style-pick"
                                aria-pressed={style.active}
                                disabled={busy}
                                onClick={() => { void choose(style.id); }}
                            >
                                <PDFStylePreview style={style} label={styleText(style, lang, 'label')} />
                                <span className="pdf-style-meta">
                                    <span className="pdf-style-title-row">
                                        <strong>{styleText(style, lang, 'label')}</strong>
                                        <span className="pdf-style-badge">{style.active ? t('In use', '当前', '目前') : motifName(style.motif, t)}</span>
                                    </span>
                                    <span className="pdf-style-summary">{styleText(style, lang, 'summary')}</span>
                                </span>
                            </button>
                            {!style.builtin ? (
                                <button
                                    type="button"
                                    className="btn-secondary pdf-style-delete"
                                    aria-label={t(`Delete ${styleText(style, lang, 'label')}`, `删除${styleText(style, lang, 'label')}`, `刪除${styleText(style, lang, 'label')}`)}
                                    onClick={() => { void remove(style.id); }}
                                    disabled={busy}
                                >
                                    {t('Delete', '删除', '刪除')}
                                </button>
                            ) : null}
                        </li>
                    ))}
                </ul>
            )}

            <form
                className="pdf-styles-create"
                onSubmit={(event) => {
                    event.preventDefault();
                    void generate();
                }}
            >
                <label htmlFor="pdf-style-request">{t('Describe a style', '描述想要的风格', '描述想要的風格')}</label>
                <textarea
                    id="pdf-style-request"
                    value={request}
                    onChange={(event) => setRequest(event.target.value)}
                    rows={3}
                    placeholder={t(
                        'For example: a burgundy dossier with a gold double frame',
                        '例如：酒红案卷，金色双框，标题居中',
                        '例如：酒紅案卷，金色雙框，標題居中',
                    )}
                />
                <button type="submit" className="btn-primary" disabled={busy || request.trim() === ''}>
                    {busy ? t('Generating…', '正在生成…', '正在生成…') : t('Generate style', '生成风格', '生成風格')}
                </button>
            </form>
        </section>
    );
}
