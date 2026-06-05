import type { Dispatch, SetStateAction } from 'react';
import { LoadConfig, PatchConfigFields, SetDefaultLaunchMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { getAllToolOptions } from '../../config/toolCatalog';

type ProgrammingToolsSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
) => {
    if (!config) return;
    const next = new main.AppConfig({ ...config, ...patch } as any);
    setConfig(next);
    PatchConfigFields(patch).then((saved) => {
        setConfig(new main.AppConfig(saved));
    }).catch((err) => {
        console.error('Failed to patch programming tool settings:', err);
        setConfig(config);
    });
};

const saveLaunchMode = async (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    mode: 'local' | 'remote',
) => {
    if (!config) return;
    const next = new main.AppConfig({
        ...config,
        default_launch_mode: mode,
        remote_enabled: mode === 'remote',
    } as any);
    setConfig(next);
    try {
        await SetDefaultLaunchMode(mode);
        const freshConfig = await LoadConfig();
        setConfig(freshConfig);
    } catch (err) {
        console.error('Failed to save launch mode:', err);
        setConfig(config);
    }
};

const visibleToolOptions = getAllToolOptions().filter(tool => tool.id !== 'claude');

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang }: ProgrammingToolsSettingsPanelProps) => {
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';

    return (
        <div className="settings-panel programming-tools-settings">
            <div className="programming-tools-settings__entry-card">
                <label className="programming-tools-settings__toggle">
                    <input
                        type="checkbox"
                        checked={!!(config as any)?.show_coding_tool_entry}
                        onChange={(e) => saveConfigPatch(config, setConfig, { show_coding_tool_entry: e.target.checked })}
                    />
                    <span>{textForLang(lang, 'Show coding tool entry in sidebar', '在侧边栏显示编程工具入口', '在側邊欄顯示程式工具入口')}</span>
                </label>
            </div>

            <div className="programming-tools-settings__visibility-card">
                <div className="programming-tools-settings__section-title">
                    {textForLang(lang, 'Visible Coding Tools', 'Visible Coding Tools', 'Visible Coding Tools')}
                </div>
                <div className="programming-tools-settings__visibility-grid">
                    {visibleToolOptions.map((tool) => {
                        const key = `show_${tool.id}`;
                        return (
                            <label className="programming-tools-settings__toggle" key={tool.id}>
                                <input
                                    type="checkbox"
                                    checked={(config as any)?.[key] !== false}
                                    onChange={(e) => saveConfigPatch(config, setConfig, { [key]: e.target.checked })}
                                />
                                <span>{tool.name}</span>
                            </label>
                        );
                    })}
                </div>
            </div>

            <div className="programming-tools-settings__launch-card">
                <div className="programming-tools-settings__section-title">
                    {textForLang(lang, 'Default Launch Mode', '默认启动模式', '預設啟動模式')}
                </div>
                <div className="programming-tools-settings__mode-options">
                    <label className="programming-tools-settings__mode-option" data-active={launchMode === 'local'}>
                        <input
                            type="radio"
                            name="launchMode"
                            checked={launchMode === 'local'}
                            onChange={() => { void saveLaunchMode(config, setConfig, 'local'); }}
                        />
                        <span>{textForLang(lang, 'Local', '本地', '本機')}</span>
                    </label>
                    <label className="programming-tools-settings__mode-option" data-active={launchMode === 'remote'}>
                        <input
                            type="radio"
                            name="launchMode"
                            checked={launchMode === 'remote'}
                            onChange={() => { void saveLaunchMode(config, setConfig, 'remote'); }}
                        />
                        <span>{textForLang(lang, 'Remote', '远程', '遠端')}</span>
                    </label>
                </div>
            </div>
        </div>
    );
};
