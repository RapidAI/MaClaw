import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { useRef, useId } from 'react';
import { LoadConfig, SetDefaultLaunchMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { getAllToolOptions } from '../../config/toolCatalog';
import { CodingKnowledgeSection } from './CodingKnowledgeSection';
import { cfgVal, saveConfigPatch } from './programmingToolsConfig';

type ProgrammingToolsSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
};

const textForLang = localizeText;

const saveLaunchMode = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    mode: 'local' | 'remote',
    versionRef: MutableRefObject<number>,
) => {
    if (!config) return;
    const myVersion = ++versionRef.current;
    const next = new main.AppConfig({
        ...config,
        default_launch_mode: mode,
        remote_enabled: mode === 'remote',
    } as any);
    setConfig(next);
    SetDefaultLaunchMode(mode).then(() => LoadConfig()).then((freshConfig) => {
        if (myVersion === versionRef.current) {
            setConfig(freshConfig);
        }
    }).catch((err) => {
        console.error('Failed to save launch mode:', err);
        if (myVersion === versionRef.current) {
            setConfig(config);
        }
    });
};

const visibleToolOptions = getAllToolOptions().filter(tool => tool.id !== 'claude');

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang }: ProgrammingToolsSettingsPanelProps) => {
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';
    const versionRef = useRef(0);
    const uid = useId();
    const patch = (p: Record<string, any>) => saveConfigPatch(config, setConfig, p, versionRef);
    const setMode = (m: 'local' | 'remote') => saveLaunchMode(config, setConfig, m, versionRef);
    // Mode B: default enabled when field is absent
    const acpHostEnabled = (config as any)?.acp_host_enabled !== false;
    const acpHostMirror = (config as any)?.acp_host_mirror_ui !== false;
    const acpHostPort = Number((config as any)?.acp_host_port || 0) || 0;

    return (
        <div className="settings-panel prog-tools">
            <section className="prog-tools__section" aria-labelledby={`${uid}-acp-heading`}>
                <h3 className="prog-tools__section-title" id={`${uid}-acp-heading`}>
                    {textForLang(lang, 'VS Code / ACP (Mode B)', 'VS Code / ACP（Mode B）', 'VS Code / ACP（Mode B）')}
                </h3>
                <div className="prog-tools__card">
                    <p className="prog-tools__hint" style={{ margin: '0 0 10px', opacity: 0.85, fontSize: '0.85rem' }}>
                        {textForLang(
                            lang,
                            'Single agent brain = GUI AI assistant. Bridge is thin ACP only. VS Code cwd = tool workspace (edits on disk). GUI must be running.',
                            '唯一大脑 = 桌面 AI 助手；bridge 仅做 ACP 转发。VS Code 打开的文件夹即工具工作区（改文件落盘）。须保持 GUI 运行。',
                            '唯一大腦 = 桌面 AI 助手；bridge 僅做 ACP 轉發。VS Code 開啟的資料夾即工具工作區（改檔落盤）。須保持 GUI 執行。',
                        )}
                    </p>
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-acp-enable-label`}>
                            {textForLang(lang, 'Enable ACP host', '启用 ACP Host', '啟用 ACP Host')}
                        </span>
                        <label className="prog-tools__switch" role="switch" aria-checked={acpHostEnabled} aria-labelledby={`${uid}-acp-enable-label`}>
                            <input
                                type="checkbox"
                                checked={acpHostEnabled}
                                onChange={(e) => patch({ acp_host_enabled: e.target.checked })}
                            />
                            <span className="prog-tools__switch-slider" aria-hidden="true" />
                        </label>
                    </div>
                    <div className="prog-tools__divider" aria-hidden="true" />
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-acp-mirror-label`}>
                            {textForLang(lang, 'Mirror to AI assistant UI', '同步到 AI 助手界面', '同步到 AI 助手介面')}
                        </span>
                        <label className="prog-tools__switch" role="switch" aria-checked={acpHostMirror} aria-labelledby={`${uid}-acp-mirror-label`}>
                            <input
                                type="checkbox"
                                checked={acpHostMirror}
                                onChange={(e) => patch({ acp_host_mirror_ui: e.target.checked })}
                            />
                            <span className="prog-tools__switch-slider" aria-hidden="true" />
                        </label>
                    </div>
                    <div className="prog-tools__divider" aria-hidden="true" />
                    <div className="prog-tools__field prog-tools__field--vertical">
                        <span className="prog-tools__field-label">
                            {textForLang(
                                lang,
                                'Port (0 = auto 18789 then ephemeral; >0 = strict, no fallback)',
                                '端口（0 = 优先 18789 再随机；>0 = 严格绑定，失败不降级）',
                                '連接埠（0 = 優先 18789 再隨機；>0 = 嚴格綁定，失敗不降級）',
                            )}
                        </span>
                        <input
                            type="number"
                            className="form-input"
                            min={0}
                            max={65535}
                            value={acpHostPort}
                            onChange={(e) => {
                                const n = parseInt(e.target.value, 10);
                                patch({ acp_host_port: Number.isFinite(n) ? n : 0 });
                            }}
                        />
                    </div>
                </div>
            </section>

            <section className="prog-tools__section" aria-labelledby={`${uid}-config-heading`}>
                <h3 className="prog-tools__section-title" id={`${uid}-config-heading`}>
                    {textForLang(lang, 'Tool Configuration', '工具配置', '工具配置')}
                </h3>
                <div className="prog-tools__card">
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-sidebar-label`}>
                            {textForLang(lang, 'Sidebar Entry', '侧边栏入口', '側邊欄入口')}
                        </span>
                        <label className="prog-tools__switch" role="switch" aria-checked={!!cfgVal(config, 'show_coding_tool_entry', false)} aria-labelledby={`${uid}-sidebar-label`}>
                            <input
                                type="checkbox"
                                checked={cfgVal(config, 'show_coding_tool_entry', false)}
                                onChange={(e) => patch({ show_coding_tool_entry: e.target.checked })}
                            />
                            <span className="prog-tools__switch-slider" aria-hidden="true" />
                        </label>
                    </div>
                    <div className="prog-tools__divider" aria-hidden="true" />
                    <div className="prog-tools__field prog-tools__field--vertical">
                        <span className="prog-tools__field-label">
                            {textForLang(lang, 'Enabled Tools', '启用的工具', '啟用的工具')}
                        </span>
                        <div className="prog-tools__tool-grid" role="group" aria-label={textForLang(lang, 'Enabled Tools', '启用的工具', '啟用的工具')}>
                            {visibleToolOptions.map((tool) => {
                                const key = `show_${tool.id}`;
                                const checked = cfgVal(config, key, true);
                                return (
                                    <label className="prog-tools__tool-chip" key={tool.id} data-active={checked}>
                                        <input
                                            type="checkbox"
                                            checked={checked}
                                            onChange={(e) => patch({ [key]: e.target.checked })}
                                            aria-label={tool.name}
                                        />
                                        <span className="prog-tools__tool-chip-label">{tool.name}</span>
                                    </label>
                                );
                            })}
                        </div>
                    </div>
                    <div className="prog-tools__divider" aria-hidden="true" />
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-mode-label`}>
                            {textForLang(lang, 'Default Mode', '默认模式', '預設模式')}
                        </span>
                        <div className="prog-tools__mode-toggle" role="group" aria-labelledby={`${uid}-mode-label`}>
                            <button className="prog-tools__mode-btn" data-active={launchMode === 'local'} aria-pressed={launchMode === 'local'} onClick={() => setMode('local')}>
                                {textForLang(lang, 'Local', '本地', '本機')}
                            </button>
                            <button className="prog-tools__mode-btn" data-active={launchMode === 'remote'} aria-pressed={launchMode === 'remote'} onClick={() => setMode('remote')}>
                                {textForLang(lang, 'Remote', '远程', '遠端')}
                            </button>
                        </div>
                    </div>
                </div>
            </section>
            <section className="prog-tools__section" aria-labelledby={`${uid}-kb-heading`}>
                <h3 className="prog-tools__section-title" id={`${uid}-kb-heading`}>
                    {textForLang(lang, 'Knowledge Base', '编程知识库', '程式知識庫')}
                </h3>
                <CodingKnowledgeSection config={config} setConfig={setConfig} lang={lang} versionRef={versionRef} />
            </section>
        </div>
    );
};
