import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { IMAuditPanel } from '../remote/IMAuditPanel';
import { ThirdPartyAccessSettings } from './ThirdPartyAccessSettings';
import { QQBotSettings } from './QQBotSettings';
import { TelegramBotSettings } from './TelegramBotSettings';
import { WeixinSettings } from './WeixinSettings';
import { IMSubTabs, type IMSubTab } from './IMSubTabs';
type IMSettingsPanelProps = {
    settingsTab: string;
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imSubTab: IMSubTab;
    setImSubTab: Dispatch<SetStateAction<IMSubTab>>;
    imAuditPlatform: string | null;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    imAuditBtnStyle: CSSProperties;
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
    weixinQRCode: string;
    setWeixinQRCode: Dispatch<SetStateAction<string>>;
    weixinQRLoading: boolean;
    setWeixinQRLoading: Dispatch<SetStateAction<boolean>>;
    weixinQRWaiting: boolean;
    setWeixinQRWaiting: Dispatch<SetStateAction<boolean>>;
    weixinQRError: string;
    setWeixinQRError: Dispatch<SetStateAction<string>>;
};

export const IMSettingsPanel = ({
    settingsTab,
    config,
    setConfig,
    lang,
    imSubTab,
    setImSubTab,
    imAuditPlatform,
    setIMAuditPlatform,
    imAuditBtnStyle,
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
    weixinQRCode,
    setWeixinQRCode,
    weixinQRLoading,
    setWeixinQRLoading,
    weixinQRWaiting,
    setWeixinQRWaiting,
    weixinQRError,
    setWeixinQRError,
}: IMSettingsPanelProps) => (
                            <div className="settings-panel" style={{ display: settingsTab === 'im' ? 'block' : 'none' }}>
                                <IMSubTabs lang={lang} imSubTab={imSubTab} setImSubTab={setImSubTab} />

                                {/* QQ Bot tab */}
                                {imSubTab === 'qq' && (
                                    <QQBotSettings
                                        config={config}
                                        setConfig={setConfig}
                                        lang={lang}
                                        imAuditBtnStyle={imAuditBtnStyle}
                                        saveRemoteConfigField={saveRemoteConfigField}
                                        qqBotStatus={qqBotStatus}
                                        setQQBotStatus={setQQBotStatus}
                                        qqBotLocalMode={qqBotLocalMode}
                                        setQQBotLocalModeState={setQQBotLocalModeState}
                                        setIMAuditPlatform={setIMAuditPlatform}
                                    />
                                )}

                                {/* Telegram Bot tab */}
                                {imSubTab === 'telegram' && (
                                    <TelegramBotSettings
                                        config={config}
                                        setConfig={setConfig}
                                        lang={lang}
                                        imAuditBtnStyle={imAuditBtnStyle}
                                        saveRemoteConfigField={saveRemoteConfigField}
                                        telegramStatus={telegramStatus}
                                        setTelegramStatus={setTelegramStatus}
                                        telegramLocalMode={telegramLocalMode}
                                        setTelegramLocalModeState={setTelegramLocalModeState}
                                        setIMAuditPlatform={setIMAuditPlatform}
                                    />
                                )}

                                {/* WeChat tab */}
                                {imSubTab === 'weixin' && (
                                    <WeixinSettings
                                        config={config}
                                        setConfig={setConfig}
                                        lang={lang}
                                        imAuditBtnStyle={imAuditBtnStyle}
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

                                {imSubTab === 'thirdparty' && (
                                    <ThirdPartyAccessSettings
                                        config={config}
                                        setConfig={setConfig}
                                        lang={lang}
                                        imAuditBtnStyle={imAuditBtnStyle}
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
