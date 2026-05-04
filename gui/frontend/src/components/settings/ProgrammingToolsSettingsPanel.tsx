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
