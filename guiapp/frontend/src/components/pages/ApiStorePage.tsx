import type { Dispatch, SetStateAction } from 'react';
import { apiStoreProviders } from '../../config/apiStoreProviders';
import { localizeText } from '../../i18n';
import { useChatFontSizeCommit, type ChatFontSizeDraftRef } from '../settings/useChatFontSizeCommit';
import { ApiStoreProviderCard } from './ApiStoreProviderCard';

interface ApiStorePageProps {
    lang: string;
    t: (key: string) => string;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
    chatFontSizeDraftRef?: ChatFontSizeDraftRef;
}

export const ApiStorePage = ({ lang, t, chatFontSize, setChatFontSize, chatFontSizeDraftRef }: ApiStorePageProps) => {
    const { commit: commitChatFontSize, rangeHandlers: chatFontSizeHandlers } = useChatFontSizeCommit(chatFontSize, setChatFontSize, chatFontSizeDraftRef);
    return (
                        <div className="secondary-page-shell api-store-page" style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>

                            <div style={{ flex: 1, overflowY: 'auto', padding: '20px', overflowX: 'hidden' }}>
                                <div className="api-store-provider-grid" style={{
                                    gap: '12px',
                                    paddingBottom: '20px'
                                }}>
                                    {apiStoreProviders.map((provider) => (
                                        <ApiStoreProviderCard key={provider.name} provider={provider} t={t} />
                                    ))}
                                </div>
                                <div className="form-group" style={{ marginBottom: '16px' }}>
                                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{localizeText(lang, 'Chat Font Size', '聊天字体大小', '聊天字體大小')}</h4>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                                        <input type="range" min={12} max={24} step={1} value={chatFontSize}
                                            {...chatFontSizeHandlers}
                                            aria-label={localizeText(lang, 'Chat Font Size', '聊天字体大小', '聊天字體大小')}
                                            style={{ flex: 1, accentColor: 'var(--theme-primary)' }} />
                                        <span style={{ fontSize: '0.78rem', color: 'var(--theme-text-secondary)', minWidth: '42px', textAlign: 'center' }}>{chatFontSize}px</span>
                                        <button onClick={() => commitChatFontSize(14)}
                                            style={{ fontSize: '0.72rem', padding: '3px 10px', cursor: 'pointer', background: 'var(--theme-surface-muted)', color: 'var(--theme-text-secondary)', border: '1px solid var(--theme-border)', borderRadius: 4 }}>
                                            {localizeText(lang, 'Reset', '重置', '重置')}
                                        </button>
                                    </div>
                                    <p style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '6px', marginBottom: 0 }}>
                                        {localizeText(lang, 'Adjust the AI assistant chat area font size (12-24px) independently from UI zoom.', '独立调整 AI 助手聊天区的字体大小（12-24px），不影响界面缩放。', '獨立調整 AI 助手聊天區的字體大小（12-24px），不影響介面縮放。')}
                                    </p>
                                </div>
                            </div>
                        </div>
    );
};
