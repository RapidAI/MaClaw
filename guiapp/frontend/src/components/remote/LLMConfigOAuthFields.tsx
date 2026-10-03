import { ImportCodexAuth } from '../../../wailsjs/go/main/App';
import { ClipboardSetText } from '../../../wailsjs/runtime';
import { colors } from './styles';
import { labelStyle, type LLMProvider } from './LLMConfigPanelShared';
import { isKimiCodeProvider, isWorkBuddyProvider } from './providerLogos';

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export async function copyKimiCodeUserCode(code: string): Promise<boolean> {
    const value = code.trim();
    if (!value) return false;
    try {
        return await ClipboardSetText(value) !== false;
    } catch {
        return false;
    }
}

function oauthSignInLabel(name: string, t: Translate): string {
    if (name === 'GitHub Copilot') return t('Sign in with GitHub', '\u4f7f\u7528 GitHub \u8d26\u53f7\u767b\u5f55');
    if (name === 'Anthropic') return t('Sign in with Claude.ai', '\u4f7f\u7528 Claude.ai \u8d26\u53f7\u767b\u5f55');
    if (name === 'xAI-Grok') return t('Sign in with xAI', '\u4f7f\u7528 xAI \u8d26\u53f7\u767b\u5f55');
    if (isWorkBuddyProvider(name)) return t('Sign in with WorkBuddy', '\u4f7f\u7528 WorkBuddy \u8d26\u53f7\u767b\u5f55');
    if (isKimiCodeProvider(name)) return t('Sign in with Kimi Code', '\u4f7f\u7528 Kimi Code \u8d26\u53f7\u767b\u5f55');
    return t('Sign in with OpenAI', '\u4f7f\u7528 OpenAI \u8d26\u53f7\u767b\u5f55');
}

export function LLMConfigOAuthFields({
    provider,
    oauthBusy,
    testFailed,
    hint,
    t,
    onLogin,
    onCancel,
    onImported,
    onImportError,
}: {
    provider: LLMProvider;
    oauthBusy: boolean;
    testFailed: boolean;
    hint?: string;
    t: Translate;
    onLogin: () => void;
    onCancel: () => void;
    onImported: (msg: string) => Promise<void> | void;
    onImportError: (error: unknown) => void;
}) {
    return (
        <div>
            <label style={labelStyle}>{t('Authentication', '\u8ba4\u8bc1\u65b9\u5f0f')}</label>
            {provider.key ? (
                <div style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '8px 12px', borderRadius: 4,
                    background: colors.successBg, border: `1px solid color-mix(in srgb, ${colors.success} 30%, transparent)`,
                }}>
                    <span style={{ fontSize: '0.76rem', color: colors.success, flex: 1 }}>
                        {t('OAuth authenticated', 'OAuth \u5df2\u8ba4\u8bc1')}
                    </span>
                    <button onClick={onLogin} disabled={oauthBusy} style={{
                        fontSize: '0.72rem', padding: '4px 12px', cursor: 'pointer',
                        background: 'transparent', color: 'var(--theme-primary)',
                        border: `1px solid ${colors.primary}`, borderRadius: 4,
                        opacity: oauthBusy ? 0.5 : 1,
                    }}>
                        {oauthBusy ? (provider.name === 'xAI-Grok' ? t('Waiting for browser authorization...', '\u7b49\u5f85\u6d4f\u89c8\u5668\u6388\u6743...') : t('Logging in...', '\u767b\u5f55\u4e2d...')) : t('Re-login', '\u91cd\u65b0\u767b\u5f55')}
                    </button>
                </div>
            ) : (
                <>
                    <button onClick={onLogin} disabled={oauthBusy} style={{
                        width: '100%', padding: '10px 0', fontSize: '0.8rem',
                        cursor: oauthBusy ? 'default' : 'pointer',
                        background: colors.primaryLight, color: colors.primaryDark,
                        border: `1px solid ${colors.primary}`, borderRadius: 4,
                        opacity: oauthBusy ? 0.6 : 1,
                    }}>
                        {oauthBusy
                            ? t('Waiting for browser authorization...', '\u7b49\u5f85\u6d4f\u89c8\u5668\u6388\u6743...')
                            : oauthSignInLabel(provider.name, t)}
                    </button>
                    {provider.name === 'OpenAI' && testFailed && !oauthBusy && (
                        <button onClick={async () => {
                            try {
                                const msg = await ImportCodexAuth();
                                await onImported(msg || '\u5df2\u4ece Codex \u5bfc\u5165');
                            } catch (e) {
                                onImportError(e);
                            }
                        }} style={{
                            width: '100%', padding: '8px 0', fontSize: '0.76rem',
                            cursor: 'pointer', marginTop: 6,
                            background: 'transparent', color: colors.primary,
                            border: `1px dashed ${colors.primary}`, borderRadius: 4,
                        }}>
                            {t('Import from Codex CLI (if already logged in)', '\u4ece Codex CLI \u5bfc\u5165\uff08\u5982\u5df2\u5728 Codex \u4e2d\u767b\u5f55\uff09')}
                        </button>
                    )}
                </>
            )}
            {oauthBusy && hint ? (
                <p style={{ fontSize: '0.76rem', color: colors.textSecondary, margin: '8px 0 0', lineHeight: 1.45, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                    {hint}
                </p>
            ) : null}
            {oauthBusy && (
                <button aria-label={t('Cancel OAuth login', '\u53d6\u6d88 OAuth \u767b\u5f55')} onClick={onCancel} style={{
                    width: '100%', padding: '8px 0', fontSize: '0.76rem',
                    cursor: 'pointer', marginTop: 6,
                    background: 'transparent', color: colors.textMuted,
                    border: `1px solid ${colors.border}`, borderRadius: 4,
                }}>
                    {t('Cancel', '\u53d6\u6d88')}
                </button>
            )}
        </div>
    );
}
