import type { Dispatch, SetStateAction } from 'react';
import { SaveConfig } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { SystemDiagnosticsTable } from './SystemDiagnosticsTable';
import { DataDirectorySection } from './DataDirectorySection';

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
        <div className="settings-panel">
            <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                    {textForLang(lang, 'System Settings', '\u7cfb\u7edf\u8bbe\u7f6e', '\u7cfb\u7d71\u8a2d\u7f6e')}
                </h4>
                <div style={{ display: 'flex', alignItems: 'center', gap: '16px', flexWrap: 'wrap' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                        <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>{textForLang(lang, 'Heartbeat Interval (sec)', '\u5fc3\u8df3\u95f4\u9694\uff08\u79d2\uff09', '\u5fc3\u8df3\u9593\u9694\uff08\u79d2\uff09')}</label>
                        <input
                            className="form-input"
                            type="number"
                            min={5}
                            step={1}
                            style={{ width: '70px' }}
                            value={config?.remote_heartbeat_sec || 10}
                            onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || 10) })}
                            onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(5, Number(e.target.value || 10)) })}
                        />
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                        <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>{textForLang(lang, 'Screen Dim Timeout (min)', '\u606f\u5c4f\u7b49\u5f85\uff08\u5206\u949f\uff09', '\u606f\u5c4f\u7b49\u5f85\uff08\u5206\u9418\uff09')}</label>
                        <input
                            className="form-input"
                            type="number"
                            min={0}
                            step={1}
                            style={{ width: '70px' }}
                            value={(config as any)?.screen_dim_timeout_min ?? 3}
                            onChange={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Number(e.target.value || 0) } as any)}
                            onBlur={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Math.max(0, Number(e.target.value || 0)) } as any)}
                            title={textForLang(lang, 'Minutes of inactivity before screen dims (0=disabled). Effective when workstation mode or screen-lock prevention is on.', '\u7a7a\u95f2\u591a\u5c11\u5206\u949f\u540e\u606f\u5c4f\uff080=\u5173\u95ed\uff09\uff1b\u5de5\u4f5c\u7ad9\u6a21\u5f0f\u6216\u9632\u9501\u5c4f\u65f6\u751f\u6548\u3002', '\u7a7a\u9592\u591a\u5c11\u5206\u9418\u5f8c\u606f\u5c4f\uff080=\u95dc\u9589\uff09\uff1b\u5de5\u4f5c\u7ad9\u6a21\u5f0f\u6216\u9632\u9396\u5c4f\u6642\u751f\u6548\u3002')}
                        />
                    </div>
                </div>

                <div style={{ marginTop: '12px', display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                        <input
                            type="checkbox"
                            checked={(config as any)?.workstation_mode === true}
                            onChange={async (e) => {
                                if (!config) return;
                                const prevConfig = config;
                                const newConfig = new main.AppConfig({ ...config, workstation_mode: e.target.checked } as any);
                                setConfig(newConfig);
                                try {
                                    await SaveConfig(newConfig);
                                } catch (err: any) {
                                    setConfig(prevConfig);
                                    showToastMessage(err?.message || String(err));
                                }
                            }}
                            style={{ width: '16px', height: '16px' }}
                        />
                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                            {textForLang(lang, 'Workstation Mode', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f', '\u5de5\u4f5c\u7ad9\u6a21\u5f0f')}
                        </span>
                    </label>
                    <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', lineHeight: 1.5 }}>
                        {textForLang(lang, 'Prevents sleep and screen lock while allowing display off. Useful for screenshot testing and debugging.', '\u9632\u6b62\u7cfb\u7edf\u7761\u7720\u548c\u9501\u5c4f\uff0c\u4f46\u5141\u8bb8\u5c4f\u5e55\u5728\u7a7a\u95f2\u540e\u5173\u95ed\uff0c\u9002\u5408\u622a\u56fe\u6d4b\u8bd5\u548c\u8c03\u8bd5\u3002', '\u9632\u6b62\u7cfb\u7d71\u7761\u7720\u548c\u9396\u5c4f\uff0c\u4f46\u5141\u8a31\u87a2\u5e55\u5728\u7a7a\u9592\u5f8c\u95dc\u9589\uff0c\u9069\u5408\u622a\u5716\u6e2c\u8a66\u548c\u9664\u932f\u3002')}
                    </span>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: '10px', marginTop: '12px', maxWidth: '760px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', minWidth: '90px' }}>
                            {textForLang(lang, 'Mic device', '\u9ed8\u8ba4\u5f55\u97f3\u8bbe\u5907', '\u9810\u8a2d\u9304\u97f3\u88dd\u7f6e')}
                        </label>
                        <select className="form-input" style={{ flex: 1, minWidth: 0 }} value={(config as any)?.audio_input_device_id || ''} onChange={(e) => saveRemoteConfigField({ audio_input_device_id: e.target.value } as any)}>
                            <option value="">{textForLang(lang, 'System Default', '\u7cfb\u7edf\u9ed8\u8ba4', '\u7cfb\u7d71\u9810\u8a2d')}</option>
                            {audioDevices.inputs.map(d => <option key={d.deviceId} value={d.deviceId}>{d.label}</option>)}
                        </select>
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', minWidth: '90px' }}>
                            {textForLang(lang, 'Speaker device', '\u9ed8\u8ba4\u64ad\u653e\u8bbe\u5907', '\u9810\u8a2d\u64ad\u653e\u88dd\u7f6e')}
                        </label>
                        <select className="form-input" style={{ flex: 1, minWidth: 0 }} value={(config as any)?.audio_output_device_id || ''} onChange={(e) => saveRemoteConfigField({ audio_output_device_id: e.target.value } as any)}>
                            <option value="">{textForLang(lang, 'System Default', '\u7cfb\u7edf\u9ed8\u8ba4', '\u7cfb\u7d71\u9810\u8a2d')}</option>
                            {audioDevices.outputs.map(d => <option key={d.deviceId} value={d.deviceId}>{d.label}</option>)}
                        </select>
                    </div>
                </div>
                <div style={{ marginTop: '8px', fontSize: '0.7rem', color: 'var(--theme-text-muted)', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                    <span>{textForLang(lang, 'Select audio devices for AI assistant voice input and TTS playback.', '\u9009\u62e9 AI \u52a9\u624b\u5f55\u97f3\u8f93\u5165\u548c TTS \u64ad\u62a5\u4f7f\u7528\u7684\u97f3\u9891\u8bbe\u5907\u3002', '\u9078\u64c7 AI \u52a9\u624b\u9304\u97f3\u8f38\u5165\u548c TTS \u64ad\u5831\u4f7f\u7528\u7684\u97f3\u8a0a\u88dd\u7f6e\u3002')}</span>
                    {!audioDevices.labelsAvailable && (
                        <button type="button" onClick={audioDevices.requestLabels} style={{ border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-primary)', borderRadius: '4px', padding: '2px 8px', cursor: 'pointer', fontSize: '0.68rem' }}>
                            {textForLang(lang, 'Show device names', '\u6388\u6743\u663e\u793a\u8bbe\u5907\u540d\u79f0', '\u6388\u6b0a\u986f\u793a\u88dd\u7f6e\u540d\u7a31')}
                        </button>
                    )}
                </div>
            </div>

            <DataDirectorySection config={config} setConfig={(c) => setConfig(c)} lang={lang} showToastMessage={showToastMessage} />

            <div className="form-group" style={{ marginTop: '16px', borderTop: '1px solid var(--theme-border)', paddingTop: '16px' }}>
                <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                    {textForLang(lang, 'Diagnostics', '\u8bca\u65ad\u4fe1\u606f', '\u8a3a\u65b7\u8cc7\u8a0a')}
                </h4>
                <SystemDiagnosticsTable diagnostics={diagnostics} />
            </div>
        </div>
    );
};
