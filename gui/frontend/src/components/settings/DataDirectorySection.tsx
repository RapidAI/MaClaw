import { useState, useEffect } from 'react';
import { SetDataDir } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type DataDirectorySectionProps = {
    config: main.AppConfig | null;
    setConfig: (c: main.AppConfig) => void;
    lang: string;
    showToastMessage: (message: string) => void;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export const DataDirectorySection = ({ config, setConfig, lang, showToastMessage }: DataDirectorySectionProps) => {
    const [dataDirInput, setDataDirInput] = useState(config?.data_dir || '');
    const [dataDirSaving, setDataDirSaving] = useState(false);

    useEffect(() => {
        if (config?.data_dir !== undefined) {
            setDataDirInput(config.data_dir || '');
        }
    }, [config?.data_dir]);

    const handleSaveDataDir = async () => {
        if (!config) return;
        setDataDirSaving(true);
        try {
            const errMsg = await SetDataDir(dataDirInput.trim());
            if (errMsg) {
                showToastMessage(errMsg);
            } else {
                const newConfig = new main.AppConfig({ ...config, data_dir: dataDirInput.trim() } as any);
                setConfig(newConfig);
                showToastMessage(textForLang(lang,
                    'Data directory updated. Please restart maclaw for the change to take effect.',
                    '数据目录已更新，请重启 maclaw 后生效。',
                    '資料目錄已更新，請重啟 maclaw 後生效。'
                ));
            }
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        } finally {
            setDataDirSaving(false);
        }
    };

    return (
        <div className="form-group" style={{ marginTop: '16px', borderTop: '1px solid var(--theme-border)', paddingTop: '16px' }}>
            <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                {textForLang(lang, 'Data Directory', '数据目录', '資料目錄')}
            </h4>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', maxWidth: '600px' }}>
                <input
                    className="form-input"
                    type="text"
                    style={{ flex: 1 }}
                    placeholder={textForLang(lang, 'Default: ~/.maclaw', '默认: ~/.maclaw', '預設: ~/.maclaw')}
                    value={dataDirInput}
                    onChange={(e) => setDataDirInput(e.target.value)}
                />
                <button
                    type="button"
                    onClick={handleSaveDataDir}
                    disabled={dataDirSaving}
                    style={{ border: '1px solid var(--theme-border)', background: 'var(--theme-primary)', color: '#fff', borderRadius: '4px', padding: '4px 12px', cursor: dataDirSaving ? 'not-allowed' : 'pointer', fontSize: '0.75rem', whiteSpace: 'nowrap', opacity: dataDirSaving ? 0.6 : 1 }}
                >
                    {dataDirSaving ? '...' : textForLang(lang, 'Save', '保存', '儲存')}
                </button>
            </div>
            <div style={{ marginTop: '6px', fontSize: '0.7rem', color: 'var(--theme-text-muted)', lineHeight: 1.5 }}>
                {textForLang(lang,
                    'Set a custom directory for all maclaw data (memories, logs, skills, etc.). config.json always stays at ~/.maclaw. Changes take effect after restart.',
                    '设置自定义数据目录（记忆、日志、技能等）。config.json 始终保留在 ~/.maclaw 下。修改后重启生效，数据将自动迁移。',
                    '設定自訂資料目錄（記憶、日誌、技能等）。config.json 始終保留在 ~/.maclaw 下。修改後重啟生效，資料將自動遷移。'
                )}
            </div>
        </div>
    );
};
