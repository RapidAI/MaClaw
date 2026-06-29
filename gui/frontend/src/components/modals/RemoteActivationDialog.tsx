import type { Dispatch, SetStateAction } from 'react';
import type { RemoteCenterHubOption } from '../../types/appShell';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

type RemoteActivationDraft = {
    hub_id: string;
    hub_url: string;
    hubcenter_url: string;
    email: string;
};

type RemoteActivationDialogProps = {
    draft: RemoteActivationDraft;
    setDraft: Dispatch<SetStateAction<RemoteActivationDraft>>;
    remoteCenterHubs: RemoteCenterHubOption[];
    loadingRemoteCenterHubs: boolean;
    remoteBusy?: string | null;
    t: (key: string) => string;
    onLoadRemoteHubs: () => void;
    onActivate: () => void;
    onClose: () => void;
};

export const RemoteActivationDialog = ({
    draft,
    setDraft,
    remoteCenterHubs,
    loadingRemoteCenterHubs,
    remoteBusy,
    t,
    onLoadRemoteHubs,
    onActivate,
    onClose,
}: RemoteActivationDialogProps) => {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    return (
    <div className="modal-overlay" {...backdropProps}>
        <div
            className="modal-content remote-activation-modal"
            {...dialogProps}
        >
            <div className="modal-header">
                <h3>{t("remoteActivationDialogTitle")}</h3>
                <button className="btn-close" onClick={onClose}>&times;</button>
            </div>
            <div className="modal-body remote-activation-modal__body elegant-scrollbar">
                <div className="remote-activation-modal__desc">
                    {t("remoteActivationDialogDesc")}
                </div>
                <div className="remote-activation-modal__grid">
                    <div>
                        <label className="form-label">{t("remoteHubCenterUrl")}</label>
                        <input
                            className="form-input"
                            value={draft.hubcenter_url}
                            onChange={(e) => setDraft((prev) => ({ ...prev, hubcenter_url: e.target.value }))}
                            placeholder="http://127.0.0.1:9388"
                            spellCheck={false}
                        />
                    </div>
                    <div>
                        <label className="form-label">{t("remoteEmail")}</label>
                        <input
                            className="form-input"
                            value={draft.email}
                            onChange={(e) => setDraft((prev) => ({ ...prev, email: e.target.value }))}
                            placeholder="name@example.com"
                            spellCheck={false}
                        />
                    </div>
                </div>
                <div className="remote-activation-modal__grid remote-activation-modal__grid--end">
                    <div>
                        <div className="remote-activation-modal__field-head">
                            <label className="form-label remote-activation-modal__field-label">{t("remoteSelectRegisteredHub")}</label>
                            <button
                                className="btn-secondary remote-activation-modal__load-button"
                                onClick={onLoadRemoteHubs}
                                disabled={loadingRemoteCenterHubs}
                            >
                                {loadingRemoteCenterHubs ? t("remoteLoadingRegisteredHubs") : t("remoteLoadRegisteredHubs")}
                            </button>
                        </div>
                        <select
                            className="form-select"
                            value={remoteCenterHubs.some((hub) => hub.hub_id === draft.hub_id || hub.base_url === draft.hub_url.trim()) ? (draft.hub_id || draft.hub_url.trim()) : ""}
                            onChange={(e) => {
                                const selected = remoteCenterHubs.find((hub) => hub.hub_id === e.target.value || hub.base_url === e.target.value);
                                setDraft((prev) => ({
                                    ...prev,
                                    hub_id: selected?.hub_id || "",
                                    hub_url: selected?.base_url || e.target.value,
                                }));
                            }}
                        >
                            <option value="">
                                {remoteCenterHubs.length > 0 ? t("remoteSelectRegisteredHub") : t("remoteNoRegisteredHubs")}
                            </option>
                            {remoteCenterHubs.map((hub) => (
                                <option key={hub.hub_id + '-' + hub.base_url} value={hub.hub_id || hub.base_url}>
                                    {hub.name ? hub.name + ' (' + hub.base_url + ')' : hub.base_url}
                                </option>
                            ))}
                        </select>
                    </div>
                    <div>
                        <label className="form-label">{t("remoteHubUrl")}</label>
                        <input
                            className="form-input"
                            value={draft.hub_url}
                            onChange={(e) => setDraft((prev) => ({ ...prev, hub_id: "", hub_url: e.target.value }))}
                            placeholder="https://hub.example.com"
                            spellCheck={false}
                        />
                    </div>
                </div>
                <div className="remote-activation-modal__hint">
                    {t("remoteHubManualOrSelect")}
                </div>
            </div>
            <div className="modal-footer remote-activation-modal__footer">
                <button className="btn-secondary" onClick={onClose}>{t("cancel")}</button>
                <button className="btn-primary" onClick={onActivate} disabled={remoteBusy === 'activate'}>
                    {remoteBusy === 'activate' ? t("remoteActivating") : t("remoteActivateAndLaunch")}
                </button>
            </div>
        </div>
    </div>
    );
};
