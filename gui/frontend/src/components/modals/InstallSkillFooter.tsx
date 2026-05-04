import type { Dispatch, SetStateAction } from 'react';
import { InstallDefaultMarketplace } from '../../../wailsjs/go/main/App';

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
}: InstallSkillFooterProps) => (
    <div className="modal-footer" style={{ marginTop: '15px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        {activeTool === 'claude' ? (
            <button
                className="btn-link"
                style={{ color: 'var(--theme-primary)', fontSize: '0.85rem', padding: '4px 15px', display: 'flex', alignItems: 'center', gap: '6px', opacity: isMarketplaceInstalling ? 0.6 : 1, minWidth: '120px', justifyContent: 'center' }}
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
                    <div style={{ width: '12px', height: '12px', border: '2px solid var(--theme-primary)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
                )}
                {t("installDefaultMarketplace")}
            </button>
        ) : (
            <div></div>
        )}
        <div style={{ display: 'flex', gap: '10px' }}>
            <button className="btn-secondary" onClick={onClose}>{t("cancel")}</button>
            <button
                className="btn-primary"
                style={{ backgroundColor: 'var(--theme-success)', borderColor: 'var(--theme-success)', display: 'flex', alignItems: 'center', gap: '6px', opacity: (selectedSkillsToInstall.length === 0 || isBatchInstalling) ? 0.6 : 1 }}
                disabled={selectedSkillsToInstall.length === 0 || isBatchInstalling}
                onClick={onInstallSelected}
            >
                {isBatchInstalling && (
                    <div style={{ width: '12px', height: '12px', border: '2px solid white', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
                )}
                {t("install")}
            </button>
        </div>
    </div>
);
