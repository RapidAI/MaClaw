import type { Dispatch, SetStateAction } from 'react';
import { LoadConfig, SaveConfig, SetDefaultLaunchMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type ProgrammingToolsSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    remoteToolMetadata: any[];
    toolProviders: Array<{ name: string; valid: boolean; builtin: boolean }>;
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
    SaveConfig(next);
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

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang, remoteToolMetadata, toolProviders }: ProgrammingToolsSettingsPanelProps) => {
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

            <div className="programming-tools-settings__default-grid">
                <div className="programming-tools-settings__field-card">
                    <h4>{textForLang(lang, 'Default Coding Tool', '默认编程工具', '預設編程工具')}</h4>
                    <select
                        className="form-input"
                        value={(config as any)?.default_tool || ''}
                        onChange={(e) => saveConfigPatch(config, setConfig, { default_tool: e.target.value, default_tool_provider: '' })}
                    >
                        <option value="">{textForLang(lang, 'Auto (Brand Default)', 'Auto（品牌默认）', 'Auto（品牌預設）')}</option>
                        {remoteToolMetadata.map((tool: any) => (
                            <option key={tool.name} value={tool.name} disabled={!tool.installed}>
                                {tool.display_name || tool.name}{!tool.installed ? textForLang(lang, ' (Not Installed)', '（未安装）', '（未安裝）') : ''}
                            </option>
                        ))}
                    </select>
                    <p>{textForLang(lang, 'Choose the default tool for MaClaw-created AI coding sessions. Auto uses the brand default.', '选择 MaClaw 自动创建 AI 编程会话时默认使用的工具。Auto 将使用品牌默认工具。', '選擇 MaClaw 自動建立 AI 程式會話時預設使用的工具。Auto 將使用品牌預設工具。')}</p>
                </div>

                {(config as any)?.default_tool ? (
                    <div className="programming-tools-settings__field-card">
                        <h4>{textForLang(lang, 'Default Provider', '默认服务商', '預設服務商')}</h4>
                        <select
                            className="form-input"
                            value={(config as any)?.default_tool_provider || ''}
                            onChange={(e) => saveConfigPatch(config, setConfig, { default_tool_provider: e.target.value })}
                        >
                            <option value="">{textForLang(lang, 'Auto (Auto Select)', 'Auto（自动选择）', 'Auto（自動選擇）')}</option>
                            {toolProviders.map((provider) => (<option key={provider.name} value={provider.name}>{provider.name}</option>))}
                        </select>
                        <p>{textForLang(lang, 'Choose the default provider for the selected tool. Auto picks the first available provider.', '选择默认工具使用的服务商。Auto 将自动选择第一个可用服务商。', '選擇預設工具使用的服務商。Auto 將自動選擇第一個可用服務商。')}</p>
                    </div>
                ) : null}
            </div>
        </div>
    );
};
