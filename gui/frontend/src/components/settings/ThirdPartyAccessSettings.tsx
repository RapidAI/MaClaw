import { Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { CreateThirdPartyDevicePairing, LoadConfig, RestartThirdPartyGateway, SetThirdPartyGatewayLocalMode, StopThirdPartyGateway } from '../../../wailsjs/go/main/App';
import { corelib, main } from '../../../wailsjs/go/models';
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
    connected: lang === 'en' ? 'Running' : '\u5df2\u542f\u52a8',
    connecting: lang === 'en' ? 'Starting' : '\u542f\u52a8\u4e2d',
    disconnected: lang === 'en' ? 'Stopped' : '\u672a\u8fde\u63a5',
    disabled: lang === 'en' ? 'Disabled' : '\u672a\u542f\u7528',
    error: lang === 'en' ? 'Error' : '\u9519\u8bef',
}[status] || status);

export const ThirdPartyAccessSettings = ({
    config,
    setConfig,
    lang,
    saveRemoteConfigField,
    showToastMessage,
    setIMAuditPlatform,
    thirdPartyGatewayStatus,
    setThirdPartyGatewayStatus,
    thirdPartyGatewayLocalMode,
    setThirdPartyGatewayLocalModeState,
}: ThirdPartyAccessSettingsProps) => (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {lang === 'zh-Hans'
                ? '\u5f00\u653e\u672c\u673a HTTP \u6d88\u606f\u63a5\u5165\u7aef\u53e3\uff0c\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u4e3b\u52a8\u8fde\u63a5 MaClaw\uff0c\u65e0\u9700\u63d0\u4f9b\u56de\u8c03\u5730\u5740\u3002'
                : lang === 'zh-Hant'
                ? '\u958b\u653e\u672c\u6a5f HTTP \u6d88\u606f\u63a5\u5165\u7aef\u53e3\uff0c\u7b2c\u4e09\u65b9\u8edf\u9ad4\u4e3b\u52d5\u9023\u63a5 MaClaw\uff0c\u7121\u9700\u63d0\u4f9b\u56de\u8abf\u5730\u5740\u3002'
                : 'Expose a local HTTP message gateway. Third-party software connects to MaClaw without a callback URL.'}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input type="checkbox" aria-label={textForLang(lang, 'Enable third-party access', '\u5f00\u542f\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u63a5\u5165', '\u958b\u555f\u7b2c\u4e09\u65b9\u8edf\u9ad4\u63a5\u5165')} checked={(config as any)?.thirdparty_gateway_enabled || false} onChange={async (e) => {
                    const enabled = e.target.checked;
                    const patch: any = { thirdparty_gateway_enabled: enabled };
                    if (enabled && !String((config as any)?.thirdparty_gateway_token || '').trim()) {
                        const bytes = new Uint8Array(32);
                        window.crypto.getRandomValues(bytes);
                        patch.thirdparty_gateway_token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
                    }
                    await saveRemoteConfigField(patch);
                    if (enabled) {
                        try { const st = await RestartThirdPartyGateway(); setThirdPartyGatewayStatus(typeof st === 'string' ? st : 'disconnected'); }
                        catch (err: any) { showToastMessage(err?.message || String(err)); }
                    } else {
                        try { await StopThirdPartyGateway(); } catch {}
                        setThirdPartyGatewayStatus('disconnected');
                    }
                }} />
                <span>{textForLang(lang, 'Enable third-party access', '\u5f00\u542f\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u63a5\u5165', '\u958b\u555f\u7b2c\u4e09\u65b9\u8edf\u9ad4\u63a5\u5165')}</span>
            </label>
            <span className="im-settings-status" data-status={thirdPartyGatewayStatus}>{gatewayStatusLabel(thirdPartyGatewayStatus, lang)}</span>
            <button type="button" className="im-settings-button" disabled={!(config as any)?.thirdparty_gateway_enabled} onClick={async () => {
                try { const st = await RestartThirdPartyGateway(); setThirdPartyGatewayStatus(typeof st === 'string' ? st : 'disconnected'); }
                catch (e: any) { showToastMessage(e?.message || String(e)); }
            }}>
                {textForLang(lang, 'Restart', '\u91cd\u542f\u63a5\u53e3', '\u91cd\u555f\u4ecb\u9762')}
            </button>
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('thirdparty')}>
                {watchLabel(lang)}
            </button>
        </div>
        <div className="im-settings-mode-row">
            <span>{channelModeLabel(lang)}</span>
            <div className="im-settings-segmented">
                {[{ value: true, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u5355\u673a' : 'Local', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u672c\u673a Agent \u76f4\u63a5\u5904\u7406' : 'Handle with local Agent' }, { value: false, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u591a\u673a' : 'Hub', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u901a\u8fc7 Hub \u8f6c\u53d1\u5230\u5728\u7ebf\u8bbe\u5907' : 'Forward through Hub' }].map((opt) => (
                    <button key={String(opt.value)} type="button" aria-label={opt.desc} title={opt.desc} data-active={thirdPartyGatewayLocalMode === opt.value} onClick={() => {
                        const prev = thirdPartyGatewayLocalMode;
                        setThirdPartyGatewayLocalModeState(opt.value);
                        SetThirdPartyGatewayLocalMode(opt.value).then(() => { LoadConfig().then((c: any) => setConfig(c)).catch(() => {}); }).catch((err: any) => {
                            setThirdPartyGatewayLocalModeState(prev);
                            showToastMessage(err?.message || err || '\u5207\u6362\u5931\u8d25');
                        });
                    }}>{opt.label}</button>
                ))}
            </div>
        </div>
        <div className="im-settings-grid im-settings-grid--gateway">
            <label className="im-settings-field">
                <span>Host</span>
                <input type="text" value={(config as any)?.thirdparty_gateway_host || '127.0.0.1'} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_host: e.target.value } as any)} placeholder="127.0.0.1" spellCheck={false} />
            </label>
            <label className="im-settings-field im-settings-field--port">
                <span>Port</span>
                <input type="number" min={1} max={65535} value={(config as any)?.thirdparty_gateway_port || 18777} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_port: Number(e.target.value || 18777) } as any)} />
            </label>
            <label className="im-settings-field im-settings-field--token">
                <span>Token</span>
                <span className="im-settings-token-row">
                    <input type="password" value={(config as any)?.thirdparty_gateway_token || ''} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_token: e.target.value } as any)} placeholder="Bearer token" autoComplete="off" />
                    <button type="button" className="im-settings-button im-settings-button--primary" onClick={async () => {
                        const bytes = new Uint8Array(32);
                        window.crypto.getRandomValues(bytes);
                        const token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
                        await saveRemoteConfigField({ thirdparty_gateway_token: token } as any);
                        showToastMessage(lang === 'en' ? 'Token generated' : '\u5df2\u751f\u6210 Token');
                    }}>{lang === 'en' ? 'Generate Token' : '\u751f\u6210 Token'}</button>
                </span>
            </label>
        </div>
        <div className="im-settings-endpoint-row">
            <code>{`http://${(config as any)?.thirdparty_gateway_host || '127.0.0.1'}:${(config as any)?.thirdparty_gateway_port || 18777}/api/im-gateway/v1`}</code>
            <button type="button" className="im-settings-button" disabled={!(config as any)?.thirdparty_gateway_enabled} onClick={async () => {
                try {
                    const result: any = await CreateThirdPartyDevicePairing();
                    showToastMessage(lang === 'en'
                        ? `Pairing code: ${result?.pairCode}; ESP gateway: ${result?.gatewayURL} (valid for 30 minutes)`
                        : `硬件配对码：${result?.pairCode}；ESP 网关地址：${result?.gatewayURL}（30 分钟内有效）`);
                } catch (err: any) { showToastMessage(err?.message || String(err)); }
            }}>
                {lang === 'en' ? 'Pair device' : '\u786c\u4ef6\u914d\u5bf9'}
            </button>
            <button type="button" className="im-settings-button im-settings-button--primary" onClick={() => {
                const base = String((config as any)?.remote_hub_url || '').replace(/\/+$/, '');
                BrowserOpenURL(base ? base + '/connector' : '/connector');
            }}>{textForLang(lang, 'Open docs', '\u6253\u5f00\u63a5\u5165\u6587\u6863', '\u958b\u555f\u63a5\u5165\u6587\u4ef6')}</button>
        </div>
    </section>
);
