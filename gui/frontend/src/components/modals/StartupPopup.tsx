import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { SaveConfig } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

// Startup modal surface uses var(--theme-surface) in App.css.

type StartupPopupProps = {
    config: main.AppConfig | null;
    setConfig: (config: main.AppConfig) => void;
    lang: string;
    t: (key: string) => string;
    onClose: () => void;
};

export const StartupPopup = ({ config, setConfig, lang, t, onClose }: StartupPopupProps) => (
    <div className="modal-overlay startup-modal-overlay">
        <div className="modal-content startup-modal">
            <button className="modal-close startup-modal__close" onClick={onClose} aria-label={t("close")}>&times;</button>

            <div className="startup-modal__header">
                <div className="startup-modal__mark" aria-hidden="true">{`</>`}</div>
                <div className="startup-modal__title-block">
                    <h3>{t("startupTitle")}</h3>
                    <p>{t("slogan")}</p>
                </div>
            </div>

            <div className="startup-modal__body">
                <div className="startup-modal__quick-row" aria-hidden="true">
                    <span>{t("launch")}</span>
                    <span>{t("modelSettings")}</span>
                    <span>{t("manageProjects")}</span>
                </div>

                <div className="startup-modal__actions">
                    <button
                        type="button"
                        className="startup-modal__action startup-modal__action--primary"
                        data-legacy-icon="\u{1F3AC}"
                        onClick={() => {
                            BrowserOpenURL("https://www.bilibili.com/video/BV1wmvoBnEF1");
                        }}
                    >
                        <span className="startup-modal__action-index" aria-hidden="true">01</span>
                        <span>{t("quickStart")}</span>
                    </button>
                    <button
                        type="button"
                        className="startup-modal__action"
                        data-legacy-icon="\u{1F4D6}"
                        onClick={() => {
                            const manualUrl = (lang === 'zh-Hans' || lang === 'zh-Hant')
                                ? "https://github.com/rapidai/maclaw/blob/main/UserManual_CN.md"
                                : "https://github.com/rapidai/maclaw/blob/main/UserManual_EN.md";
                            BrowserOpenURL(manualUrl);
                        }}
                    >
                        <span className="startup-modal__action-index" aria-hidden="true">02</span>
                        <span>{t("manual")}</span>
                    </button>
                </div>

                <label className="startup-modal__option">
                    <input
                        type="checkbox"
                        checked={config?.hide_startup_popup || false}
                        onChange={(e) => {
                            if (config) {
                                const newConfig = new main.AppConfig({ ...config, hide_startup_popup: e.target.checked });
                                setConfig(newConfig);
                                SaveConfig(newConfig);
                            }
                        }}
                    />
                    <span>{t("dontShowAgain")}</span>
                </label>
            </div>
        </div>
    </div>
);
