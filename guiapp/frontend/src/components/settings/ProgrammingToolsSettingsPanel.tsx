import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { GetACPHostStatus, RestartACPHost } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { CodingKnowledgeSection } from './CodingKnowledgeSection';
import { cfgVal, saveConfigPatch } from './programmingToolsConfig';

type Props = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
};

const t = (lang: string, en: string, zh: string, hant = zh) => localizeText(lang, en, zh, hant);

/** Settings owned by the built-in coding SubAgent and its ACP bridge.
 * External CLI/editor tool registration intentionally does not belong here. */
export function ProgrammingToolsSettingsPanel({ config, setConfig, lang }: Props) {
    const versionRef = useRef(0);
    const [status, setStatus] = useState<Record<string, any> | null>(null);
    const [statusError, setStatusError] = useState('');
    const enabled = (cfgVal(config, 'acp_host_enabled', true) as boolean) !== false;
    const mirrorUI = (cfgVal(config, 'acp_host_mirror_ui', true) as boolean) !== false;
    const port = Number(cfgVal(config, 'acp_host_port', 0)) || 0;
    const qualityGate = cfgVal<boolean>(config, 'coding_quality_gate_enabled', false);

    const refresh = () => {
        void GetACPHostStatus().then((next) => { setStatus(next || null); setStatusError(''); })
            .catch((err) => setStatusError(String(err?.message || err || '')));
    };
    useEffect(() => { refresh(); }, []);

    const patch = (fields: Record<string, any>) => saveConfigPatch(config, setConfig, fields, versionRef);
    const restart = async () => {
        try { setStatus(await RestartACPHost()); setStatusError(''); }
        catch (err: any) { setStatusError(String(err?.message || err || '')); }
    };

    return (
        <div className="settings-content settings-content--stacked">
            <section className="prog-tools__card">
                <div className="prog-tools__section-title">{t(lang, 'Coding', '编程')}</div>
                <div className="prog-tools__field">
                    <div>
                        <label htmlFor="coding-quality-gate"><strong>{t(lang, 'Quality gate', '质量门', '質量門')}</strong></label>
                        <div className="settings-help-text">{t(lang, 'When on, a finished coding task fails if exploration, verification commands, or the git diff self-check do not pass, and a bug-fix edit waits for an accepted localization report. Off by default.', '开启后，编程任务结束时会检查探索、验证命令和 git diff，不通过会把任务判为失败；修复已有代码时，未通过定位报告也不能改文件。默认关闭。', '開啟後，程式任務結束時會檢查探索、驗證命令和 git diff，不通過會把任務判為失敗；修復既有程式時，未通過定位報告也不能改檔案。預設關閉。')}</div>
                    </div>
                    <input id="coding-quality-gate" type="checkbox" checked={qualityGate} onChange={(e) => patch({ coding_quality_gate_enabled: e.target.checked })} />
                </div>
            </section>
            <section className="prog-tools__card">
                <div className="prog-tools__section-title">{t(lang, 'Built-in coding agent', '内置编程子 Agent')}</div>
                <div className="prog-tools__field">
                    <div>
                        <strong>{t(lang, 'ACP bridge', 'ACP 编程桥接')}</strong>
                        <div className="settings-help-text">{t(lang, 'Allow VS Code ACP clients to use the built-in MaClaw coding agent.', '允许 VS Code ACP 客户端使用内置 MaClaw 编程子 Agent。')}</div>
                    </div>
                    <input type="checkbox" checked={enabled} onChange={(e) => patch({ acp_host_enabled: e.target.checked })} />
                </div>
                <div className="prog-tools__field">
                    <label className="prog-tools__field-label" htmlFor="acp-host-port">{t(lang, 'Port (0 = automatic)', '端口（0 = 自动）')}</label>
                    <input id="acp-host-port" className="form-input" type="number" min="0" max="65535" value={port} onChange={(e) => patch({ acp_host_port: Math.max(0, Math.min(65535, Number(e.target.value) || 0)) })} />
                </div>
                <div className="prog-tools__field">
                    <label className="prog-tools__field-label" htmlFor="acp-mirror-ui">{t(lang, 'Mirror activity to AI assistant', '同步活动到 AI 助手界面')}</label>
                    <input id="acp-mirror-ui" type="checkbox" checked={mirrorUI} onChange={(e) => patch({ acp_host_mirror_ui: e.target.checked })} />
                </div>
                <div className="prog-tools__field">
                    <span>{status?.ready ? t(lang, 'Running', '运行中') : t(lang, 'Stopped or not ready', '未运行或未就绪')}</span>
                    <button type="button" className="btn-secondary" onClick={() => void restart()}>{t(lang, 'Restart ACP', '重启 ACP')}</button>
                </div>
                {statusError && <div className="settings-error">{statusError}</div>}
            </section>
            <section>
                <div className="prog-tools__section-title">{t(lang, 'Coding knowledge base', '编程知识库')}</div>
                <CodingKnowledgeSection config={config} setConfig={setConfig} lang={lang} versionRef={versionRef} />
            </section>
        </div>
    );
}
