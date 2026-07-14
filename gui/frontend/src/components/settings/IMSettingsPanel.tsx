import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { IMAuditPanel } from '../remote/IMAuditPanel';
import { ThirdPartyAccessSettings } from './ThirdPartyAccessSettings';
import { QQBotSettings } from './QQBotSettings';
import { TelegramBotSettings } from './TelegramBotSettings';
import { WeixinSettings } from './WeixinSettings';
import { LansengerSettings } from './LansengerSettings';
import { IMSubTabs, type IMSubTab } from './IMSubTabs';
import { IMProgressHintSettings } from './IMProgressHintSettings';

type IMSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imSubTab: IMSubTab;
    setImSubTab: Dispatch<SetStateAction<IMSubTab>>;
    imAuditPlatform: string | null;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    showToastMessage: (message: string) => void;
    qqBotStatus: string;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotLocalMode: boolean;
    setQQBotLocalModeState: Dispatch<SetStateAction<boolean>>;
    telegramStatus: string;
    setTelegramStatus: Dispatch<SetStateAction<string>>;
    telegramLocalMode: boolean;
    setTelegramLocalModeState: Dispatch<SetStateAction<boolean>>;
    weixinStatus: string;
    setWeixinStatus: Dispatch<SetStateAction<string>>;
    weixinLocalMode: boolean;
    setWeixinLocalModeState: Dispatch<SetStateAction<boolean>>;
    thirdPartyGatewayStatus: string;
    setThirdPartyGatewayStatus: Dispatch<SetStateAction<string>>;
    thirdPartyGatewayLocalMode: boolean;
    setThirdPartyGatewayLocalModeState: Dispatch<SetStateAction<boolean>>;
    showLansenger?: boolean;
    lansengerStatus: string;
    setLansengerStatus: Dispatch<SetStateAction<string>>;
    lansengerLocalMode: boolean;
    setLansengerLocalModeState: Dispatch<SetStateAction<boolean>>;
    weixinQRCode: string;
    setWeixinQRCode: Dispatch<SetStateAction<string>>;
    weixinQRLoading: boolean;
    setWeixinQRLoading: Dispatch<SetStateAction<boolean>>;
    weixinQRWaiting: boolean;
    setWeixinQRWaiting: Dispatch<SetStateAction<boolean>>;
    weixinQRError: string;
    setWeixinQRError: Dispatch<SetStateAction<string>>;
};

/** Parent mounts this panel only when the IM settings tab is active. */
export const IMSettingsPanel = ({
    config,
    setConfig,
    lang,
    imSubTab,
    setImSubTab,
    imAuditPlatform,
    setIMAuditPlatform,
    saveRemoteConfigField,
    showToastMessage,
    qqBotStatus,
    setQQBotStatus,
    qqBotLocalMode,
    setQQBotLocalModeState,
    telegramStatus,
    setTelegramStatus,
    telegramLocalMode,
    setTelegramLocalModeState,
    weixinStatus,
    setWeixinStatus,
    weixinLocalMode,
    setWeixinLocalModeState,
    thirdPartyGatewayStatus,
    setThirdPartyGatewayStatus,
    thirdPartyGatewayLocalMode,
    setThirdPartyGatewayLocalModeState,
    showLansenger = false,
    lansengerStatus,
    setLansengerStatus,
    lansengerLocalMode,
    setLansengerLocalModeState,
    weixinQRCode,
    setWeixinQRCode,
    weixinQRLoading,
    setWeixinQRLoading,
    weixinQRWaiting,
    setWeixinQRWaiting,
    weixinQRError,
    setWeixinQRError,
}: IMSettingsPanelProps) => (
    <div className="settings-content settings-panel im-settings-panel">
        <IMProgressHintSettings
            lang={lang}
            enabled={config?.im_progress_nudge_enabled !== false}
            onChange={(enabled) => saveRemoteConfigField({ im_progress_nudge_enabled: enabled })}
        />
        <IMSubTabs lang={lang} imSubTab={imSubTab} setImSubTab={setImSubTab} showLansenger={showLansenger} />

        {imSubTab === 'qq' && (
            <QQBotSettings
                config={config}
                setConfig={setConfig}
                lang={lang}
                saveRemoteConfigField={saveRemoteConfigField}
                qqBotStatus={qqBotStatus}
                setQQBotStatus={setQQBotStatus}
                qqBotLocalMode={qqBotLocalMode}
                setQQBotLocalModeState={setQQBotLocalModeState}
                setIMAuditPlatform={setIMAuditPlatform}
            />
        )}

        {imSubTab === 'telegram' && (
            <TelegramBotSettings
                config={config}
                setConfig={setConfig}
                lang={lang}
                saveRemoteConfigField={saveRemoteConfigField}
                telegramStatus={telegramStatus}
                setTelegramStatus={setTelegramStatus}
                telegramLocalMode={telegramLocalMode}
                setTelegramLocalModeState={setTelegramLocalModeState}
                setIMAuditPlatform={setIMAuditPlatform}
            />
        )}

        {imSubTab === 'weixin' && (
            <WeixinSettings
                config={config}
                setConfig={setConfig}
                lang={lang}
                weixinStatus={weixinStatus}
                setWeixinStatus={setWeixinStatus}
                weixinLocalMode={weixinLocalMode}
                setWeixinLocalModeState={setWeixinLocalModeState}
                setIMAuditPlatform={setIMAuditPlatform}
                weixinQRCode={weixinQRCode}
                setWeixinQRCode={setWeixinQRCode}
                weixinQRLoading={weixinQRLoading}
                setWeixinQRLoading={setWeixinQRLoading}
                weixinQRWaiting={weixinQRWaiting}
                setWeixinQRWaiting={setWeixinQRWaiting}
                weixinQRError={weixinQRError}
                setWeixinQRError={setWeixinQRError}
            />
        )}

        {showLansenger && imSubTab === 'lansenger' && (
            <LansengerSettings
                config={config}
                setConfig={setConfig}
                lang={lang}
                saveRemoteConfigField={saveRemoteConfigField}
                lansengerStatus={lansengerStatus}
                setLansengerStatus={setLansengerStatus}
                lansengerLocalMode={lansengerLocalMode}
                setLansengerLocalModeState={setLansengerLocalModeState}
                setIMAuditPlatform={setIMAuditPlatform}
            />
        )}

        {imSubTab === 'thirdparty' && (
            <ThirdPartyAccessSettings
                config={config}
                setConfig={setConfig}
                lang={lang}
                saveRemoteConfigField={saveRemoteConfigField}
                showToastMessage={showToastMessage}
                setIMAuditPlatform={setIMAuditPlatform}
                thirdPartyGatewayStatus={thirdPartyGatewayStatus}
                setThirdPartyGatewayStatus={setThirdPartyGatewayStatus}
                thirdPartyGatewayLocalMode={thirdPartyGatewayLocalMode}
                setThirdPartyGatewayLocalModeState={setThirdPartyGatewayLocalModeState}
            />
        )}

        {imAuditPlatform && (
            <IMAuditPanel
                platform={imAuditPlatform}
                onClose={() => setIMAuditPlatform(null)}
                lang={lang}
            />
        )}
    </div>
);
