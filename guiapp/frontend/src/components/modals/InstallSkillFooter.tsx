import type { Dispatch, SetStateAction } from 'react';
import { InstallDefaultMarketplace } from '../../../wailsjs/go/main/App';

// Styling for success action uses var(--theme-success) in App.css.

type InstallSkillFooterProps = {
    activeTool: string;
    selectedSkillsToInstall: string[];
    isBatchInstalling: boolean;
    isMarketplaceInstalling: boolean;
    setIsMarketplaceInstalling: Dispatch<SetStateAction<boolean>>;
    t: (key: string) => string;
    showToastMessage: (message: string, duration?: number) => void;
    onClose: () => void;
    onInstallSelected: () => void;
    closeDisabled?: boolean;
};

export const InstallSkillFooter = ({
    activeTool,
    selectedSkillsToInstall,
    isBatchInstalling,
    isMarketplaceInstalling,
    setIsMarketplaceInstalling,
    t,
    showToastMessage,
    onClose,
    onInstallSelected,
    closeDisabled = false,
}: InstallSkillFooterProps) => (
    <div className="modal-footer install-skill-footer">
        {activeTool === 'claude' ? (
            <button
                className="btn-link install-skill-footer__marketplace"
                data-loading={isMarketplaceInstalling ? 'true' : 'false'}
                disabled={isMarketplaceInstalling}
                onClick={async () => {
                    setIsMarketplaceInstalling(true);
                    try {
                        await InstallDefaultMarketplace();
                        showToastMessage('Marketplace installed successfully!');
                    } catch (err) {
                        showToastMessage('Error installing marketplace: ' + err);
                    } finally {
                        setIsMarketplaceInstalling(false);
                    }
                }}
            >
                {isMarketplaceInstalling && (
                    <div className="install-skill-footer__spinner install-skill-footer__spinner--primary" />
                )}
                {t("installDefaultMarketplace")}
            </button>
        ) : (
            <div />
        )}
        <div className="install-skill-footer__actions">
            <button className="btn-secondary" onClick={onClose} disabled={closeDisabled}>{t("cancel")}</button>
            <button
                className="btn-primary install-skill-footer__install"
                disabled={selectedSkillsToInstall.length === 0 || isBatchInstalling}
                onClick={onInstallSelected}
            >
                {isBatchInstalling && (
                    <div className="install-skill-footer__spinner install-skill-footer__spinner--light" />
                )}
                {t("install")}
            </button>
        </div>
    </div>
);
