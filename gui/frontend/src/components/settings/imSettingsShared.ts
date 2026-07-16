import type { CSSProperties } from 'react';
import { localizeText } from '../../i18n';

export const textForLang = localizeText;

export const channelModeLabel = (lang: string) => textForLang(lang, 'Mode:', '\u901a\u9053\uff1a', '\u901a\u9053\uff1a');
export const restartLabel = (lang: string) => textForLang(lang, 'Restart', '\u91cd\u542f', '\u91cd\u555f');
export const watchLabel = (lang: string) => textForLang(lang, 'Watch', '\u76d1\u770b', '\u76e3\u770b');
/** Lansenger Follow entry (only when channel is connected). */
export const followLabel = (lang: string) => textForLang(lang, 'Follow', '\u5173\u6ce8', '\u95dc\u6ce8');
export const switchFailedLabel = (lang: string) => textForLang(lang, 'Switch failed', '\u5207\u6362\u5931\u8d25', '\u5207\u63db\u5931\u6557');

export const localModeOptions = (lang: string) => ([
    { value: true, label: textForLang(lang, 'Local', '\u5355\u673a', '\u55ae\u6a5f'), desc: textForLang(lang, 'Direct local LLM', '\u672c\u5730 LLM \u76f4\u8fde', '\u672c\u5730 LLM \u76f4\u9023') },
    { value: false, label: textForLang(lang, 'Remote', '\u591a\u673a', '\u591a\u6a5f'), desc: textForLang(lang, 'Via Hub', '\u901a\u8fc7 Hub \u8f6c\u53d1', '\u900f\u904e Hub \u8f49\u767c') },
]);

export const connectionStatusLabel = (status: string, lang: string) => {
    const labels: Record<string, string> = {
        connected: textForLang(lang, 'connected', '\u5df2\u8fde\u63a5', '\u5df2\u9023\u63a5'),
        connecting: textForLang(lang, 'connecting...', '\u8fde\u63a5\u4e2d...', '\u9023\u63a5\u4e2d...'),
        reconnecting: textForLang(lang, 'reconnecting...', '\u91cd\u8fde\u4e2d...', '\u91cd\u9023\u4e2d...'),
        paused: textForLang(lang, 'paused', '\u5df2\u6682\u505c', '\u5df2\u66ab\u505c'),
        error: textForLang(lang, 'error', '\u9519\u8bef', '\u932f\u8aa4'),
        disconnected: textForLang(lang, 'not connected', '\u672a\u8fde\u63a5', '\u672a\u9023\u63a5'),
    };
    // Text only — pair with ConnectionStatusBadge / StatusGlyph in UI, not emoji or ON/ERR prefixes.
    return labels[status] || labels.disconnected;
};

/** Map IM connection state to workbench StatusGlyph kind. */
export const connectionStatusGlyphKind = (
    status: string,
): 'ok' | 'error' | 'pending' | 'offline' => {
    if (status === 'connected') return 'ok';
    if (status === 'error') return 'error';
    if (status === 'connecting' || status === 'reconnecting' || status === 'paused') return 'pending';
    return 'offline';
};

export const pillButtonStyle = (active: boolean): CSSProperties => ({
    padding: '4px 14px',
    borderRadius: '14px',
    border: active ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
    background: active ? 'var(--theme-info-bg)' : 'transparent',
    color: active ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
    fontWeight: active ? 600 : 400,
    fontSize: '0.75rem',
    cursor: 'pointer',
    transition: 'all 0.15s',
});

export const connectionBadgeStyle = (status: string): CSSProperties => {
    const pending = status === 'connecting' || status === 'reconnecting' || status === 'paused';
    return {
        fontSize: '0.7rem',
        padding: '2px 8px',
        borderRadius: '10px',
        background: status === 'connected' ? 'var(--theme-success-bg)' : pending ? 'var(--theme-info-bg)' : 'var(--theme-danger-bg)',
        color: status === 'connected' ? 'var(--theme-success)' : pending ? 'var(--theme-primary)' : 'var(--theme-danger)',
    };
};
