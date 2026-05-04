import type { Dispatch, SetStateAction } from 'react';
import { SetChatFontSize } from '../../../wailsjs/go/main/App';
import { apiStoreProviders } from '../../config/apiStoreProviders';
import { ApiStoreProviderCard } from './ApiStoreProviderCard';

interface ApiStorePageProps {
    lang: string;
    t: (key: string) => string;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
}

export const ApiStorePage = ({ lang, t, chatFontSize, setChatFontSize }: ApiStorePageProps) => (
                        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>

                            <div style={{ flex: 1, overflowY: 'auto', padding: '20px', overflowX: 'hidden' }}>
                                <div style={{
                                    display: 'grid',
                                    gridTemplateColumns: 'repeat(4, 1fr)',
                                    gap: '12px',
                                    paddingBottom: '20px'
                                }}>
                                    {apiStoreProviders.map((provider) => (
                                        <ApiStoreProviderCard key={provider.name} provider={provider} t={t} />
                                    ))}
                                </div>
                                <div className="form-group" style={{ marginBottom: '16px' }}>
                                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '\u804a\u5929\u5b57\u4f53\u5927\u5c0f' : lang === 'zh-Hant' ? '\u804a\u5929\u5b57\u9ad4\u5927\u5c0f' : 'Chat Font Size'}</h4>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                                        <input type="range" min={12} max={24} step={1} value={chatFontSize}
                                            onChange={e => setChatFontSize(Number(e.target.value))}
                                            onPointerUp={async (e) => {
                                                const v = Number((e.currentTarget as HTMLInputElement).value);
                                                setChatFontSize(v);
                                                await SetChatFontSize(v).catch(() => {});
                                            }}
                                            style={{ flex: 1, accentColor: 'var(--theme-primary)' }} />
                                        <span style={{ fontSize: '0.78rem', color: 'var(--theme-text-secondary)', minWidth: '42px', textAlign: 'center' }}>{chatFontSize}px</span>
                                        <button onClick={() => { setChatFontSize(14); SetChatFontSize(14).catch(() => {}); }}
                                            style={{ fontSize: '0.72rem', padding: '3px 10px', cursor: 'pointer', background: 'var(--theme-surface-muted)', color: 'var(--theme-text-secondary)', border: '1px solid var(--theme-border)', borderRadius: 4 }}>
                                            {lang === 'zh-Hans' ? '\u91cd\u7f6e' : lang === 'zh-Hant' ? '\u91cd\u7f6e' : 'Reset'}
                                        </button>
                                    </div>
                                    <p style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '6px', marginBottom: 0 }}>
                                        {lang === 'zh-Hans' ? '\u72ec\u7acb\u8c03\u6574 AI \u52a9\u624b\u804a\u5929\u533a\u7684\u5b57\u4f53\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u54cd\u754c\u9762\u7f29\u653e\u3002' : lang === 'zh-Hant' ? '\u7368\u7acb\u8abf\u6574 AI \u52a9\u624b\u804a\u5929\u5340\u7684\u5b57\u9ad4\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u97ff\u4ecb\u9762\u7e2e\u653e\u3002' : 'Adjust the AI assistant chat area font size (12-24px) independently from UI zoom.'}
                                    </p>
                                </div>
                            </div>
                        </div>
);
