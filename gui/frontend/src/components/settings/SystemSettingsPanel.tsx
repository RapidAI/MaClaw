import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { SystemDiagnosticsTable } from './SystemDiagnosticsTable';
import { DataDirectorySection } from './DataDirectorySection';
import { SystemTimeoutField } from './SystemTimeoutField';

type AudioDeviceOption = {
    deviceId: string;
    label: string;
};

type AudioDevicesState = {
    inputs: AudioDeviceOption[];
    outputs: AudioDeviceOption[];
    labelsAvailable: boolean;
    requestLabels: () => void;
};

type SystemSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    audioDevices: AudioDevicesState;
    saveRemoteConfigField: (patch: Record<string, any>) => void;
    showToastMessage: (message: string) => void;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const emptyValue = (lang: string, kind: 'inactive' | 'unset' | 'generated') => {
    if (kind === 'generated') return textForLang(lang, '(not generated)', '(\u672a\u751f\u6210)', '(\u672a\u751f\u6210)');
    if (kind === 'unset') return textForLang(lang, '(not set)', '(\u672a\u8bbe\u7f6e)', '(\u672a\u8a2d\u7f6e)');
    return textForLang(lang, '(not activated)', '(\u672a\u6fc0\u6d3b)', '(\u672a\u555f\u7528)');
};

export const SystemSettingsPanel = ({ config, setConfig, lang, audioDevices, saveRemoteConfigField, showToastMessage }: SystemSettingsPanelProps) => {
    const defaultHeartbeatSec = 30;

    const diagnostics: Array<[string, string]> = [
        ['Machine ID', config?.remote_machine_id || emptyValue(lang, 'inactive')],
        ['User ID', config?.remote_user_id || emptyValue(lang, 'inactive')],
        ['Client ID', config?.remote_client_id || emptyValue(lang, 'generated')],
        ['SN', config?.remote_sn || emptyValue(lang, 'inactive')],
        ['Hub URL', config?.remote_hub_url || emptyValue(lang, 'unset')],
        ['Email', config?.remote_email || emptyValue(lang, 'unset')],
        ['WeChat Mode', (config as any)?.weixin_local_mode === false ? textForLang(lang, 'Multi-device (Hub)', '\u591a\u673a (Hub)', '\u591a\u6a5f (Hub)') : textForLang(lang, 'Single-device (Local)', '\u5355\u673a (Local)', '\u55ae\u6a5f (Local)')],
    ];

    return (
        <div className="settings-panel system-settings-panel">
            <section className="system-settings-card">
                <h4>
                    {textForLang(lang, 'System Settings', '\u7cfb\u7edf\u8bbe\u7f6e', '\u7cfb\u7d71\u8a2d\u7f6e')}
                </h4>
                <div className="system-settings-number-grid">
                    <label className="system-settings-field">
                        <span>{textForLang(lang, 'Heartbeat Interval (sec)', '\u5fc3\u8df3\u95f4\u9694\uff08\u79d2\uff09', '\u5fc3\u8df3\u9593\u9694\uff08\u79d2\uff09')}</span>
                        <input
                            className="form-input"
                            type="number"
                            min={5}
                            step={1}
                            value={config?.remote_heartbeat_sec || defaultHeartbeatSec}
                            onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || defaultHeartbeatSec) })}
                            onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(5, Number(e.target.value || defaultHeartbeatSec)) })}
                        />
                    </label>
                    <label className="system-settings-field">
                        <span>{textForLang(lang, 'Screen Dim Timeout (min)', '\u606f\u5c4f\u7b49\u5f85\uff08\u5206\u949f\uff09', '\u606f\u5c4f\u7b49\u5f85\uff08\u5206\u9418\uff09')}</span>
                        <input
                            className="form-input"
                            type="number"
                            min={0}
                            step={1}
                            value={(config as any)?.screen_dim_timeout_min ?? 3}
                            onChange={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Number(e.target.value || 0) } as any)}
                            onBlur={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Math.max(0, Number(e.target.value || 0)) } as any)}
                            title={textForLang(lang, 'Minutes of inactivity before screen dims (0=disabled). Effective when workstation mode or screen-lock prevention is on.', '\u7a7a\u95f2\u591a\u5c11\u5206\u949f\u540e\u606f\u5c4f\uff080=\u5173\u95ed\uff09\uff1b\u5de5\u4f5c\u7ad9\u6a21\u5f0f\u6216\u9632\u9501\u5c4f\u65f6\u751f\u6548\u3002', '\u7a7a\u9592\u591a\u5c11\u5206\u9418\u5f8c\u606f\u5c4f\uff080=\u95dc\u9589\uff09\uff1b\u5de5\u4f5c\u7ad9\u6a21\u5f0f\u6216\u9632\u9396\u5c4f\u6642\u751f\u6548\u3002')}
                        />
                    </label>
                    <SystemTimeoutField label={textForLang(lang, 'Agent Response Timeout (sec)', 'Agent \u54cd\u5e94\u8d85\u65f6\uff08\u79d2\uff09', 'Agent \u56de\u61c9\u903e\u6642\uff08\u79d2\uff09')} value={(config as any)?.agent_response_timeout_sec || 600} fieldName="agent_response_timeout_sec" saveRemoteConfigField={saveRemoteConfigField} title={textForLang(lang, 'How long the AI assistant may stay silent before the foreground request is marked timed out. Default: 600 seconds. Range: 240-600 seconds.', 'AI \u52a9\u624b\u5728\u524d\u53f0\u8bf7\u6c42\u4e2d\u591a\u4e45\u6ca1\u6709 token \u6216\u8fdb\u5ea6\u540e\u5224\u5b9a\u8d85\u65f6\u3002\u9ed8\u8ba4\uff1a600 \u79d2\uff1b\u8303\u56f4\uff1a240-600 \u79d2\u3002', 'AI \u52a9\u624b\u5728\u524d\u53f0\u8acb\u6c42\u4e2d\u591a\u4e45\u6c92\u6709 token \u6216\u9032\u5ea6\u5f8c\u5224\u5b9a\u903e\u6642\u3002\u9810\u8a2d\uff1a600 \u79d2\uff1b\u7bc4\u570d\uff1a240-600 \u79d2\u3002')} />
                    <SystemTimeoutField label={textForLang(lang, 'LLM Request Timeout (sec)', 'LLM \u8bf7\u6c42\u8d85\u65f6\uff08\u79d2\uff09', 'LLM \u8acb\u6c42\u903e\u6642\uff08\u79d2\uff09')} value={(config as any)?.maclaw_llm_timeout_sec || 600} fieldName="maclaw_llm_timeout_sec" saveRemoteConfigField={saveRemoteConfigField} title={textForLang(lang, 'HTTP timeout for MaClaw LLM calls. Default: 600 seconds. Range: 240-600 seconds.', 'MaClaw LLM \u8c03\u7528\u7684 HTTP \u8d85\u65f6\u3002\u9ed8\u8ba4\uff1a600 \u79d2\uff1b\u8303\u56f4\uff1a240-600 \u79d2\u3002', 'MaClaw LLM \u8abf\u7528\u7684 HTTP \u903e\u6642\u3002\u9810\u8a2d\uff1a600 \u79d2\uff1b\u7bc4\u570d\uff1a240-600 \u79d2\u3002')} />
                </div>

                <label className="system-settings-option">
                        <input
                            type="checkbox"
                            aria-label={textForLang(lang, 'Workstation Mode', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f')}
                            checked={(config as any)?.workstation_mode === true}
                            onChange={(e) => {
                                saveRemoteConfigField({ workstation_mode: e.target.checked } as any);
                            }}
                        />
                        <span>
                            {textForLang(lang, 'Workstation Mode', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f')}
                        </span>
                    <small>
                        {textForLang(lang, 'Prevents sleep and screen lock while allowing display off. Useful for screenshot testing and debugging.', '\u9632\u6b62\u7cfb\u7edf\u7761\u7720\u548c\u9501\u5c4f\uff0c\u4f46\u5141\u8bb8\u5c4f\u5e55\u5728\u7a7a\u95f2\u540e\u5173\u95ed\uff0c\u9002\u5408\u622a\u56fe\u6d4b\u8bd5\u548c\u8c03\u8bd5\u3002', '\u9632\u6b62\u7cfb\u7d71\u7761\u7720\u548c\u9396\u5c4f\uff0c\u4f46\u5141\u8a31\u87a2\u5e55\u5728\u7a7a\u9592\u5f8c\u95dc\u9589\uff0c\u9069\u5408\u622a\u5716\u6e2c\u8a66\u548c\u9664\u932f\u3002')}
                    </small>
                </label>

                <div className="system-settings-audio-grid">
                    <label className="system-settings-field system-settings-field--select">
                        <span>
                            {textForLang(lang, 'Mic device', '\u9ed8\u8ba4\u5f55\u97f3\u8bbe\u5907', '\u9810\u8a2d\u9304\u97f3\u88dd\u7f6e')}
                        </span>
                        <select className="form-input" value={(config as any)?.audio_input_device_id || ''} onChange={(e) => saveRemoteConfigField({ audio_input_device_id: e.target.value } as any)}>
                            <option value="">{textForLang(lang, 'System Default', '\u7cfb\u7edf\u9ed8\u8ba4', '\u7cfb\u7d71\u9810\u8a2d')}</option>
                            {audioDevices.inputs.map(d => <option key={d.deviceId} value={d.deviceId}>{d.label}</option>)}
                        </select>
                    </label>
                    <label className="system-settings-field system-settings-field--select">
                        <span>
                            {textForLang(lang, 'Speaker device', '\u9ed8\u8ba4\u64ad\u653e\u8bbe\u5907', '\u9810\u8a2d\u64ad\u653e\u88dd\u7f6e')}
                        </span>
                        <select className="form-input" value={(config as any)?.audio_output_device_id || ''} onChange={(e) => saveRemoteConfigField({ audio_output_device_id: e.target.value } as any)}>
                            <option value="">{textForLang(lang, 'System Default', '\u7cfb\u7edf\u9ed8\u8ba4', '\u7cfb\u7d71\u9810\u8a2d')}</option>
                            {audioDevices.outputs.map(d => <option key={d.deviceId} value={d.deviceId}>{d.label}</option>)}
                        </select>
                    </label>
                </div>
                <div className="system-settings-help-row">
                    <span>{textForLang(lang, 'Select audio devices for AI assistant voice input and TTS playback.', '\u9009\u62e9 AI \u52a9\u624b\u5f55\u97f3\u8f93\u5165\u548c TTS \u64ad\u62a5\u4f7f\u7528\u7684\u97f3\u9891\u8bbe\u5907\u3002', '\u9078\u64c7 AI \u52a9\u624b\u9304\u97f3\u8f38\u5165\u548c TTS \u64ad\u5831\u4f7f\u7528\u7684\u97f3\u8a0a\u88dd\u7f6e\u3002')}</span>
                    {!audioDevices.labelsAvailable && (
                        <button type="button" onClick={audioDevices.requestLabels}>
                            {textForLang(lang, 'Show device names', '\u6388\u6743\u663e\u793a\u8bbe\u5907\u540d\u79f0', '\u6388\u6b0a\u986f\u793a\u88dd\u7f6e\u540d\u7a31')}
                        </button>
                    )}
                </div>
            </section>

            <DataDirectorySection config={config} setConfig={(c) => setConfig(c)} lang={lang} showToastMessage={showToastMessage} />

            <section className="system-settings-card system-settings-card--diagnostics">
                <h4>
                    {textForLang(lang, 'Diagnostics', '\u8bca\u65ad\u4fe1\u606f', '\u8a3a\u65b7\u8cc7\u8a0a')}
                </h4>
                <SystemDiagnosticsTable diagnostics={diagnostics} />
            </section>
        </div>
    );
};
