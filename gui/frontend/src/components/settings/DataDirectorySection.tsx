import { useState, useEffect } from 'react';
import { SetDataDir, SelectDataDir } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type DataDirectorySectionProps = {
    config: main.AppConfig | null;
    setConfig: (c: main.AppConfig) => void;
    lang: string;
    showToastMessage: (message: string) => void;
};

const textForLang = localizeText;

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

    const handleBrowseDataDir = async () => {
        try {
            const selected = await SelectDataDir();
            if (selected) {
                setDataDirInput(selected);
            }
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        }
    };

    return (
        <section className="system-settings-card data-directory-section">
            <h4>
                {textForLang(lang, 'Data Directory', '数据目录', '資料目錄')}
            </h4>
            <div className="data-directory-section__row">
                <input
                    className="form-input"
                    type="text"
                    placeholder={textForLang(lang, 'Default: ~/.maclaw', '默认: ~/.maclaw', '預設: ~/.maclaw')}
                    value={dataDirInput}
                    onChange={(e) => setDataDirInput(e.target.value)}
                />
                <button
                    type="button"
                    onClick={handleBrowseDataDir}
                    disabled={dataDirSaving}
                >
                    {textForLang(lang, 'Browse', '浏览', '瀏覽')}
                </button>
                <button
                    type="button"
                    onClick={handleSaveDataDir}
                    disabled={dataDirSaving}
                    className="data-directory-section__save"
                >
                    {dataDirSaving ? '...' : textForLang(lang, 'Save', '保存', '儲存')}
                </button>
            </div>
            <p>
                {textForLang(lang,
                    'Set a custom directory for all maclaw data (memories, logs, skills, etc.). config.json always stays at ~/.maclaw. Changes take effect after restart.',
                    '设置自定义数据目录（记忆、日志、技能等）。config.json 始终保留在 ~/.maclaw 下。修改后重启生效，数据将自动迁移。',
                    '設定自訂資料目錄（記憶、日誌、技能等）。config.json 始終保留在 ~/.maclaw 下。修改後重啟生效，資料將自動遷移。'
                )}
            </p>
        </section>
    );
};
