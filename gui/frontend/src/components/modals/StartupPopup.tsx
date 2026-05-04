import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { SaveConfig } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type StartupPopupProps = {
    config: main.AppConfig | null;
    setConfig: (config: main.AppConfig) => void;
    lang: string;
    t: (key: string) => string;
    onClose: () => void;
};

export const StartupPopup = ({ config, setConfig, lang, t, onClose }: StartupPopupProps) => (
    <div className="modal-overlay" style={{ backgroundColor: 'rgba(0, 0, 0, 0.4)', backdropFilter: 'blur(4px)' }}>
        <div className="modal-content" style={{
            width: '320px',
            textAlign: 'center',
            padding: 0,
            borderRadius: '16px',
            overflow: 'hidden',
            border: 'none',
            boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04)'
        }}>
            <div style={{
                background: 'linear-gradient(135deg, #eef2ff 0%, #e0e7ff 100%)',
                padding: '25px 20px',
                color: '#1e293b',
                position: 'relative',
                borderBottom: '1px solid #e2e8f0'
            }}>
                <button
                    className="modal-close"
                    onClick={onClose}
                    style={{ color: '#9ca3af', opacity: 0.8, top: '10px', right: '15px', zIndex: 10 }}
                >&times;</button>
                <div style={{
                    fontSize: '2.5rem',
                    marginBottom: '10px',
                    background: 'linear-gradient(135deg, #6366f1 0%, #8b5cf6 100%)',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    fontWeight: '900',
                    lineHeight: 1,
                    filter: 'drop-shadow(0 2px 4px rgba(59, 130, 246, 0.1))'
                }}>{`</>`}</div>
                <h3 style={{ margin: 0, color: '#0f172a', fontSize: '1.2rem', fontWeight: 'bold' }}>{t("startupTitle")}</h3>
                <p style={{
                    margin: '6px 0 0 0',
                    background: 'linear-gradient(135deg, #6366f1, #8b5cf6, #a855f7)',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    fontSize: '0.95rem',
                    fontWeight: '700'
                }}>
                    {t("slogan")}
                </p>
            </div>

            <div style={{ padding: '20px 25px' }}>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginBottom: '20px' }}>
                    <button
                        style={{
                            width: '100%',
                            padding: '10px',
                            borderRadius: '10px',
                            fontSize: '0.95rem',
                            fontWeight: '600',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            gap: '8px',
                            background: 'linear-gradient(135deg, #eef2ff, #e0e7ff)',
                            color: '#4338ca',
                            border: '1px solid #c7d2fe',
                            boxShadow: '0 2px 4px rgba(59, 130, 246, 0.1)',
                            cursor: 'pointer',
                            transition: 'all 0.2s'
                        }}
                        onClick={() => {
                            BrowserOpenURL("https://www.bilibili.com/video/BV1wmvoBnEF1");
                        }}
                    >
                        <span>??</span> {t("quickStart")}
                    </button>
                    <button
                        className="btn-link"
                        style={{
                            padding: '10px',
                            border: '1px solid #e2e8f0',
                            borderRadius: '10px',
                            fontSize: '0.95rem',
                            fontWeight: '500',
                            color: '#9ca3af',
                            backgroundColor: '#ffffff',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            gap: '8px',
                            boxShadow: '0 1px 2px rgba(0,0,0,0.05)'
                        }}
                        onClick={() => {
                            const manualUrl = (lang === 'zh-Hans' || lang === 'zh-Hant')
                                ? "https://github.com/rapidai/maclaw/blob/main/UserManual_CN.md"
                                : "https://github.com/rapidai/maclaw/blob/main/UserManual_EN.md";
                            BrowserOpenURL(manualUrl);
                        }}
                    >
                        <span>??</span> {t("manual")}
                    </button>
                </div>

                <div style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '8px'
                }}>
                    <label style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '6px',
                        cursor: 'pointer',
                        fontSize: '0.8rem',
                        color: '#94a3b8'
                    }}>
                        <input
                            type="checkbox"
                            checked={config?.hide_startup_popup || false}
                            style={{
                                width: '14px',
                                height: '14px',
                                cursor: 'pointer'
                            }}
                            onChange={(e) => {
                                if (config) {
                                    const newConfig = new main.AppConfig({ ...config, hide_startup_popup: e.target.checked });
                                    setConfig(newConfig);
                                    SaveConfig(newConfig);
                                }
                            }}
                        />
                        {t("dontShowAgain")}
                    </label>
                </div>
            </div>
        </div>
    </div>
);
