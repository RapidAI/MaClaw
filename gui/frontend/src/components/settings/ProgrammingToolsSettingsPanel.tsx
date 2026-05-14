import type { Dispatch, SetStateAction } from 'react';
import { SaveConfig } from '../../../wailsjs/go/main/App';
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

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang, remoteToolMetadata, toolProviders }: ProgrammingToolsSettingsPanelProps) => (
    <div className="settings-panel">
        {/* Show/hide coding tool entry in sidebar */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px', padding: '8px 12px', borderRadius: '8px', background: 'color-mix(in srgb, var(--theme-primary) 6%, transparent)', border: '1px solid color-mix(in srgb, var(--theme-primary) 15%, transparent)' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text)', margin: 0 }}>
                <input
                    type="checkbox"
                    checked={!!(config as any)?.show_coding_tool_entry}
                    onChange={(e) => saveConfigPatch(config, setConfig, { show_coding_tool_entry: e.target.checked })}
                />
                {textForLang(lang, 'Show coding tool entry in sidebar', '在侧边栏显示编程工具入口', '在側邊欄顯示程式工具入口')}
            </label>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '16px' }}>
            <label style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem', color: 'var(--theme-text)' }}>
                {textForLang(lang, 'Default Launch Mode', '\u9ed8\u8ba4\u542f\u52a8\u6a21\u5f0f', '\u9810\u8a2d\u555f\u52d5\u6a21\u5f0f')}
            </label>
            <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input
                    type="radio"
                    name="launchMode"
                    checked={!config?.default_launch_mode || config.default_launch_mode === 'local'}
                    onChange={() => saveConfigPatch(config, setConfig, { default_launch_mode: 'local', remote_enabled: false })}
                />
                {textForLang(lang, 'Local', '\u672c\u5730', '\u672c\u6a5f')}
            </label>
            <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input
                    type="radio"
                    name="launchMode"
                    checked={config?.default_launch_mode === 'remote'}
                    onChange={() => saveConfigPatch(config, setConfig, { default_launch_mode: 'remote', remote_enabled: true })}
                />
                {textForLang(lang, 'Remote', '\u8fdc\u7a0b', '\u9060\u7aef')}
            </label>
        </div>
        <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
            <div style={{ display: 'flex', gap: '24px', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                <div style={{ flex: '1 1 0', minWidth: '180px', maxWidth: config?.default_tool ? undefined : '320px' }}>
                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '8px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                        {textForLang(lang, 'Default Coding Tool', '\u9ed8\u8ba4\u7f16\u7a0b\u5de5\u5177', '\u9810\u8a2d\u7de8\u7a0b\u5de5\u5177')}
                    </h4>
                    <select
                        className="form-input"
                        value={(config as any)?.default_tool || ''}
                        onChange={(e) => saveConfigPatch(config, setConfig, { default_tool: e.target.value, default_tool_provider: '' })}
                        style={{ width: '100%', fontSize: '0.8rem', padding: '4px 8px', height: '30px' }}
                    >
                        <option value="">{textForLang(lang, 'Auto (Brand Default)', 'Auto (\u54c1\u724c\u9ed8\u8ba4)', 'Auto (\u54c1\u724c\u9810\u8a2d)')}</option>
                        {remoteToolMetadata.map((tool: any) => (
                            <option key={tool.name} value={tool.name} disabled={!tool.installed}>
                                {tool.display_name || tool.name}{!tool.installed ? textForLang(lang, ' (Not Installed)', ' (\u672a\u5b89\u88c5)', ' (\u672a\u5b89\u88dd)') : ''}
                            </option>
                        ))}
                    </select>
                    <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginTop: '6px' }}>
                        {textForLang(lang, 'Choose the default tool for MaClaw-created AI coding sessions. Auto uses the brand default.', '\u9009\u62e9 MaClaw \u81ea\u52a8\u521b\u5efa AI \u7f16\u7a0b\u4f1a\u8bdd\u65f6\u9ed8\u8ba4\u4f7f\u7528\u7684\u5de5\u5177\u3002Auto \u5c06\u4f7f\u7528\u54c1\u724c\u9ed8\u8ba4\u5de5\u5177\u3002', '\u9078\u64c7 MaClaw \u81ea\u52d5\u5efa\u7acb AI \u7de8\u7a0b\u6703\u8a71\u6642\u9810\u8a2d\u4f7f\u7528\u7684\u5de5\u5177\u3002Auto \u5c07\u4f7f\u7528\u54c1\u724c\u9810\u8a2d\u5de5\u5177\u3002')}
                    </p>
                </div>

                {(config as any)?.default_tool ? (
                    <div style={{ flex: '1 1 0', minWidth: '180px' }}>
                        <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '8px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                            {textForLang(lang, 'Default Provider', '\u9ed8\u8ba4\u670d\u52a1\u5546', '\u9810\u8a2d\u670d\u52d9\u5546')}
                        </h4>
                        <select
                            className="form-input"
                            value={(config as any)?.default_tool_provider || ''}
                            onChange={(e) => saveConfigPatch(config, setConfig, { default_tool_provider: e.target.value })}
                            style={{ width: '100%', fontSize: '0.8rem', padding: '4px 8px', height: '30px' }}
                        >
                            <option value="">{textForLang(lang, 'Auto (Auto Select)', 'Auto (\u81ea\u52a8\u9009\u62e9)', 'Auto (\u81ea\u52d5\u9078\u64c7)')}</option>
                            {toolProviders.map((provider) => (<option key={provider.name} value={provider.name}>{provider.name}</option>))}
                        </select>
                        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginTop: '6px' }}>
                            {textForLang(lang, 'Choose the default provider for the selected tool. Auto picks the first available provider.', '\u9009\u62e9\u9ed8\u8ba4\u5de5\u5177\u4f7f\u7528\u7684\u670d\u52a1\u5546\u3002Auto \u5c06\u81ea\u52a8\u9009\u62e9\u7b2c\u4e00\u4e2a\u53ef\u7528\u670d\u52a1\u5546\u3002', '\u9078\u64c7\u9810\u8a2d\u5de5\u5177\u4f7f\u7528\u7684\u670d\u52d9\u5546\u3002Auto \u5c07\u81ea\u52d5\u9078\u64c7\u7b2c\u4e00\u500b\u53ef\u7528\u670d\u52d9\u5546\u3002')}
                        </p>
                    </div>
                ) : null}
            </div>
        </div>
    </div>
);
