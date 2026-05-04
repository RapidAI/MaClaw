import type { Dispatch, SetStateAction } from 'react';
import type { RemoteCenterHubOption } from '../../types/appShell';

type RemoteActivationDraft = {
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
}: RemoteActivationDialogProps) => (
    <div className="modal-overlay" onClick={onClose}>
        <div className="modal-content" style={{ width: '640px', maxWidth: '94vw', maxHeight: '82vh', textAlign: 'left', display: 'flex', flexDirection: 'column' }} onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
                <h3>{t("remoteActivationDialogTitle")}</h3>
                <button className="btn-close" onClick={onClose}>&times;</button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '10px', overflowY: 'auto', paddingBottom: '10px' }}>
                <div style={{ fontSize: '0.82rem', color: '#64748b', lineHeight: 1.5 }}>
                    {t("remoteActivationDialogDesc")}
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: '10px' }}>
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
                <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: '10px', alignItems: 'end' }}>
                    <div>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', marginBottom: '6px' }}>
                            <label className="form-label" style={{ marginBottom: 0 }}>{t("remoteSelectRegisteredHub")}</label>
                            <button
                                className="btn-secondary"
                                onClick={onLoadRemoteHubs}
                                disabled={loadingRemoteCenterHubs}
                                style={{ minWidth: '112px', height: '30px', padding: '4px 10px', fontSize: '0.78rem', flexShrink: 0 }}
                            >
                                {loadingRemoteCenterHubs ? t("remoteLoadingRegisteredHubs") : t("remoteLoadRegisteredHubs")}
                            </button>
                        </div>
                        <select
                            className="form-select"
                            value={remoteCenterHubs.some((hub) => hub.base_url === draft.hub_url.trim()) ? draft.hub_url.trim() : ""}
                            onChange={(e) => setDraft((prev) => ({ ...prev, hub_url: e.target.value }))}
                        >
                            <option value="">
                                {remoteCenterHubs.length > 0 ? t("remoteSelectRegisteredHub") : t("remoteNoRegisteredHubs")}
                            </option>
                            {remoteCenterHubs.map((hub) => (
                                <option key={hub.hub_id + '-' + hub.base_url} value={hub.base_url}>
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
                            onChange={(e) => setDraft((prev) => ({ ...prev, hub_url: e.target.value }))}
                            placeholder="https://hub.example.com"
                            spellCheck={false}
                        />
                    </div>
                </div>
                <div style={{ fontSize: '0.79rem', color: '#64748b', lineHeight: 1.5 }}>
                    {t("remoteHubManualOrSelect")}
                </div>
            </div>
            <div className="modal-footer" style={{ marginTop: '0', flexShrink: 0 }}>
                <button className="btn-secondary" onClick={onClose}>{t("cancel")}</button>
                <button className="btn-primary" onClick={onActivate} disabled={remoteBusy === 'activate'}>
                    {remoteBusy === 'activate' ? t("remoteActivating") : t("remoteActivateAndLaunch")}
                </button>
            </div>
        </div>
    </div>
);
