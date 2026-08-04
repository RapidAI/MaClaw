import { Dispatch, SetStateAction, useEffect, useState } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { CreateThirdPartyDevicePairing, GenerateHardwareWelcomeAudio, LoadConfig, RestartThirdPartyGateway, SelectHardwareWelcomeAudio, SendHardwareVolume, SendHardwareWelcomeAudio, SetThirdPartyGatewayLocalMode, StopThirdPartyGateway } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { channelModeLabel, textForLang, watchLabel } from './imSettingsShared';

type ThirdPartyAccessSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    showToastMessage: (message: string) => void;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    thirdPartyGatewayStatus: string;
    setThirdPartyGatewayStatus: Dispatch<SetStateAction<string>>;
    thirdPartyGatewayLocalMode: boolean;
    setThirdPartyGatewayLocalModeState: Dispatch<SetStateAction<boolean>>;
};

const gatewayStatusLabel = (status: string, lang: string) => ({
    connected: lang === 'en' ? 'Running' : '已启动', connecting: lang === 'en' ? 'Starting' : '启动中',
    disconnected: lang === 'en' ? 'Stopped' : '未连接', disabled: lang === 'en' ? 'Disabled' : '未启用', error: lang === 'en' ? 'Error' : '错误',
}[status] || status);

export const ThirdPartyAccessSettings = ({ config, setConfig, lang, saveRemoteConfigField, showToastMessage, setIMAuditPlatform, thirdPartyGatewayStatus, setThirdPartyGatewayStatus, thirdPartyGatewayLocalMode, setThirdPartyGatewayLocalModeState }: ThirdPartyAccessSettingsProps) => {
    const [pairing, setPairing] = useState<any>(null);
    const [welcomeText, setWelcomeText] = useState('');
    const [busy, setBusy] = useState(false);
    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    const enabled = Boolean((config as any)?.thirdparty_gateway_enabled);
    const volume = Number((config as any)?.hardware_volume ?? 70);
    useEffect(() => setWelcomeText(String((config as any)?.hardware_welcome_text || 'Hello, Maclaw')), [(config as any)?.hardware_welcome_text]);
    const refreshConfig = async () => setConfig(await LoadConfig() as any);
    const sendVolume = async (value: number) => { try { await SendHardwareVolume(value); await refreshConfig(); } catch (err: any) { showToastMessage(err?.message || String(err)); } };

    return <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">{isZh ? '开放本机 HTTP 消息接入端口，第三方软件主动连接 MaClaw，无需提供回调地址。' : 'Expose a local HTTP message gateway. Third-party software connects to MaClaw without a callback URL.'}</p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle"><input type="checkbox" aria-label={textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')} checked={enabled} onChange={async (e) => {
                const next = e.target.checked; const patch: any = { thirdparty_gateway_enabled: next };
                if (next && !String((config as any)?.thirdparty_gateway_token || '').trim()) { const bytes = new Uint8Array(32); window.crypto.getRandomValues(bytes); patch.thirdparty_gateway_token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join(''); }
                await saveRemoteConfigField(patch);
                if (next) { try { setThirdPartyGatewayStatus(await RestartThirdPartyGateway()); } catch (err: any) { showToastMessage(err?.message || String(err)); } } else { try { await StopThirdPartyGateway(); } catch {} setThirdPartyGatewayStatus('disconnected'); }
            }} /><span>{textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')}</span></label>
            <span className="im-settings-status" data-status={thirdPartyGatewayStatus}>{gatewayStatusLabel(thirdPartyGatewayStatus, lang)}</span>
            <button type="button" className="im-settings-button" disabled={!enabled} onClick={async () => { try { setThirdPartyGatewayStatus(await RestartThirdPartyGateway()); } catch (err: any) { showToastMessage(err?.message || String(err)); } }}>{textForLang(lang, 'Restart', '重启接口', '重啟介面')}</button>
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('thirdparty')}>{watchLabel(lang)}</button>
        </div>
        <div className="im-settings-mode-row"><span>{channelModeLabel(lang)}</span><div className="im-settings-segmented">{[{ value: true, label: isZh ? '单机' : 'Local', desc: isZh ? '本机 Agent 直接处理' : 'Handle with local Agent' }, { value: false, label: isZh ? '多机' : 'Hub', desc: isZh ? '通过 Hub 转发到在线设备' : 'Forward through Hub' }].map((opt) => <button key={String(opt.value)} type="button" aria-label={opt.desc} title={opt.desc} data-active={thirdPartyGatewayLocalMode === opt.value} onClick={() => {
            const previous = thirdPartyGatewayLocalMode; setThirdPartyGatewayLocalModeState(opt.value); SetThirdPartyGatewayLocalMode(opt.value).then(refreshConfig).catch((err: any) => { setThirdPartyGatewayLocalModeState(previous); showToastMessage(err?.message || err || '切换失败'); });
        }}>{opt.label}</button>)}</div></div>
        <div className="im-settings-grid im-settings-grid--gateway">
            <label className="im-settings-field"><span>Host</span><input type="text" value={(config as any)?.thirdparty_gateway_host || '127.0.0.1'} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_host: e.target.value })} placeholder="127.0.0.1" spellCheck={false} /></label>
            <label className="im-settings-field im-settings-field--port"><span>Port</span><input type="number" min={1} max={65535} value={(config as any)?.thirdparty_gateway_port || 18777} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_port: Number(e.target.value || 18777) })} /></label>
            <label className="im-settings-field im-settings-field--token"><span>Token</span><span className="im-settings-token-row"><input type="password" value={(config as any)?.thirdparty_gateway_token || ''} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_token: e.target.value })} placeholder="Bearer token" autoComplete="off" /><button type="button" className="im-settings-button im-settings-button--primary" onClick={async () => { const bytes = new Uint8Array(32); window.crypto.getRandomValues(bytes); await saveRemoteConfigField({ thirdparty_gateway_token: Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('') }); showToastMessage(isZh ? '已生成 Token' : 'Token generated'); }}>{isZh ? '生成 Token' : 'Generate Token'}</button></span></label>
        </div>
        <div className="im-settings-endpoint-row"><code>{`http://${(config as any)?.thirdparty_gateway_host || '127.0.0.1'}:${(config as any)?.thirdparty_gateway_port || 18777}/api/im-gateway/v1`}</code><button type="button" className="im-settings-button im-settings-button--primary" onClick={() => { const base = String((config as any)?.remote_hub_url || '').replace(/\/+$/, ''); BrowserOpenURL(base ? base + '/connector' : '/connector'); }}>{textForLang(lang, 'Open docs', '打开接入文档', '開啟接入文件')}</button></div>
        <div className="im-settings-hardware" aria-label={isZh ? '硬件配置' : 'Hardware configuration'}>
            <div className="im-settings-hardware__heading"><strong>{isZh ? '硬件配置' : 'Hardware configuration'}</strong><span>{isZh ? '码卡龙 ESP32 的配对、欢迎音频与扬声器设置。' : 'Pairing, welcome audio, and speaker settings for Macaron ESP32.'}</span></div>
            <div className="im-settings-hardware__pairing"><div><span>{isZh ? '配对码' : 'Pairing code'}</span>{pairing?.pairCode ? <strong className="im-settings-pair-code">{pairing.pairCode}</strong> : <small>{isZh ? '点击获取后，在硬件配网页输入。' : 'Get a code, then enter it in the hardware setup portal.'}</small>}</div><div className="im-settings-hardware__actions"><button type="button" className="im-settings-button im-settings-button--primary" disabled={!enabled} onClick={async () => { try { setPairing(await CreateThirdPartyDevicePairing()); showToastMessage(isZh ? '已生成配对码，有效期 30 分钟。' : 'Pairing code generated; valid for 30 minutes.'); } catch (err: any) { showToastMessage(err?.message || String(err)); } }}>{pairing?.pairCode ? (isZh ? '重新生成' : 'Regenerate') : (isZh ? '获取配对码' : 'Get code')}</button>{pairing?.gatewayURL && <code>{pairing.gatewayURL}</code>}</div></div>
            <div className="im-settings-hardware__volume"><label htmlFor="hardware-volume">{isZh ? '音量' : 'Volume'} <strong>{volume}%</strong></label><input id="hardware-volume" type="range" min={0} max={100} step={1} value={volume} disabled={!enabled} onChange={(e) => setConfig((current: any) => current ? { ...current, hardware_volume: Number(e.target.value) } : current)} onPointerUp={(e) => sendVolume(Number((e.target as HTMLInputElement).value))} onKeyUp={(e) => { if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) sendVolume(Number((e.target as HTMLInputElement).value)); }} /><small>{isZh ? '松开滑块后立即通过协议下发到已配对硬件。' : 'Sent to paired hardware when you release the slider.'}</small></div>
            <div className="im-settings-hardware__welcome"><div className="im-settings-hardware__welcome-title"><strong>{isZh ? 'Welcome 信息' : 'Welcome message'}</strong><label className="im-settings-toggle"><input type="checkbox" checked={Boolean((config as any)?.hardware_welcome_enabled)} onChange={(e) => saveRemoteConfigField({ hardware_welcome_enabled: e.target.checked })} /><span>{isZh ? '启用' : 'Enabled'}</span></label></div><p>{isZh ? '设备每次开机初始化完成后仅播放一次。输入文字生成 WAV，或选择 MP3、WAV、Ogg / Opus 自动转换为硬件可播放的 16 kHz 单声道 WAV。' : 'Plays once after each device boot finishes initializing. Generate WAV from text, or import MP3, WAV, Ogg / Opus and convert it to 16 kHz mono WAV.'}</p><div className="im-settings-hardware__welcome-controls"><textarea value={welcomeText} maxLength={80} onChange={(e) => setWelcomeText(e.target.value)} placeholder={isZh ? '例如：Hello, Maclaw' : 'For example: Hello, Maclaw'} /><button type="button" className="im-settings-button" disabled={busy || !welcomeText.trim()} onClick={async () => { setBusy(true); try { await GenerateHardwareWelcomeAudio(welcomeText); await refreshConfig(); showToastMessage(isZh ? '欢迎音频已生成。' : 'Welcome audio generated.'); } catch (err: any) { showToastMessage(err?.message || String(err)); } finally { setBusy(false); } }}>{isZh ? '生成音频' : 'Generate audio'}</button><button type="button" className="im-settings-button" disabled={busy} onClick={async () => { setBusy(true); try { await SelectHardwareWelcomeAudio(); await refreshConfig(); showToastMessage(isZh ? '欢迎音频已导入。' : 'Welcome audio imported.'); } catch (err: any) { showToastMessage(err?.message || String(err)); } finally { setBusy(false); } }}>{isZh ? '选择音频文件' : 'Choose audio'}</button><button type="button" className="im-settings-button im-settings-button--primary" disabled={busy || !(config as any)?.hardware_welcome_audio_path} onClick={async () => { setBusy(true); try { await SendHardwareWelcomeAudio(); showToastMessage(isZh ? '已发送测试播放。' : 'Test audio sent.'); } catch (err: any) { showToastMessage(err?.message || String(err)); } finally { setBusy(false); } }}>{isZh ? '测试播放' : 'Test playback'}</button></div>{(config as any)?.hardware_welcome_audio_path && <small className="im-settings-hardware__audio-status">{isZh ? '已准备硬件 WAV：' : 'Hardware WAV ready: '}{String((config as any).hardware_welcome_audio_path).split(/[\\/]/).pop()}</small>}</div>
        </div>
    </section>;
};
