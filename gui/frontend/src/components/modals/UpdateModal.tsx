type UpdateModalProps = {
    updateResult: any;
    appVersion: string;
    isDownloading: boolean;
    downloadProgress: number;
    installerPath: string;
    downloadError: string;
    t: (key: string) => string;
    onCancelDownload: () => void;
    onDownload: () => void;
    onInstall: () => void;
    onClose: () => void;
};

export const UpdateModal = ({
    updateResult,
    appVersion,
    isDownloading,
    downloadProgress,
    installerPath,
    downloadError,
    t,
    onCancelDownload,
    onDownload,
    onInstall,
    onClose,
}: UpdateModalProps) => (
    <div className="modal-overlay">
        <div className="modal-content" style={{ width: '400px', textAlign: 'left' }}>
            <h3>{t("foundNewVersion")}</h3>
            {updateResult.has_update ? (
                <>
                    <div style={{ backgroundColor: 'var(--theme-info-bg)', padding: '12px', borderRadius: '6px', marginBottom: '15px', border: '1px solid var(--theme-border)' }}>
                        <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-secondary)', marginBottom: '8px' }}>{t("currentVersion")}</div>
                        <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-primary)', marginBottom: '12px' }}>v{appVersion}</div>
                        <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-secondary)', marginBottom: '8px' }}>{t("latestVersion")}</div>
                        <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-success)' }}>{updateResult.latest_version}</div>
                    </div>

                    <div style={{ marginTop: '15px' }}>
                        {isDownloading ? (
                            <div style={{ width: '100%' }}>
                                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '8px', fontSize: '0.9rem' }}>
                                    <span>{t("downloading")}</span>
                                    <span>{downloadProgress}%</span>
                                </div>
                                <div style={{ width: '100%', height: '10px', backgroundColor: 'var(--theme-surface-muted)', borderRadius: '5px', overflow: 'hidden' }}>
                                    <div style={{ width: String(downloadProgress) + '%', height: '100%', backgroundColor: 'var(--theme-primary)', transition: 'width 0.2s ease' }}></div>
                                </div>
                                <button
                                    className="btn-link"
                                    style={{ marginTop: '10px', color: 'var(--theme-danger)' }}
                                    onClick={onCancelDownload}
                                >
                                    {t("cancelDownload")}
                                </button>
                            </div>
                        ) : installerPath ? (
                            <div style={{ textAlign: 'center', padding: '10px' }}>
                                <p style={{ color: 'var(--theme-success)', fontWeight: 'bold', marginBottom: '15px' }}>{t("downloadComplete")}</p>
                                <button className="btn-primary" style={{ width: '100%' }} onClick={onInstall}>
                                    {t("installNow")}
                                </button>
                            </div>
                        ) : (
                            <div>
                                {downloadError && (
                                    <div style={{ marginBottom: '10px' }}>
                                        <p style={{ color: 'var(--theme-danger)', fontSize: '0.85rem', marginBottom: '5px' }}>{t("downloadError").replace("{error}", downloadError)}</p>
                                        <button className="btn-primary" style={{ width: '100%', backgroundColor: 'var(--theme-danger)' }} onClick={onDownload}>
                                            {t("retry")}
                                        </button>
                                    </div>
                                )}
                                {!downloadError && (
                                    <>
                                        <p style={{ margin: '10px 0', fontSize: '0.9rem', color: 'var(--theme-text-primary)' }}>{t("foundNewVersionMsg")}</p>
                                        <button className="btn-primary" style={{ width: '100%' }} onClick={onDownload}>
                                            {t("downloadAndUpdate")}
                                        </button>
                                    </>
                                )}
                            </div>
                        )}
                    </div>
                </>
            ) : (
                <div style={{ backgroundColor: 'var(--theme-info-bg)', padding: '12px', borderRadius: '6px', border: '1px solid var(--theme-border)' }}>
                    <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-secondary)', marginBottom: '8px' }}>{t("currentVersion")}</div>
                    <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-primary)', marginBottom: '12px' }}>v{appVersion}</div>
                    <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-secondary)', marginBottom: '8px' }}>{t("latestVersion")}</div>
                    <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-success)', marginBottom: '12px' }}>{updateResult.latest_version}</div>
                    <p style={{ margin: '0', fontSize: '0.9rem', color: 'var(--theme-success)', fontWeight: '500' }}>{'\u2714\uFE0F ' + t("isLatestVersion")}</p>
                </div>
            )}
            <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                <button className="btn-primary" disabled={isDownloading} onClick={onClose}>{t("close")}</button>
            </div>
        </div>
    </div>
);
