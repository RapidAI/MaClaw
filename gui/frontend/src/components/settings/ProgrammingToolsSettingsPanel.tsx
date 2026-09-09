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
