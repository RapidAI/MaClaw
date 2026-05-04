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
                    <div style={{ backgroundColor: '#eef2ff', padding: '12px', borderRadius: '6px', marginBottom: '15px', border: '1px solid #e0e7ff' }}>
                        <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("currentVersion")}</div>
                        <div style={{ fontSize: '1rem', fontWeight: '600', color: '#4338ca', marginBottom: '12px' }}>v{appVersion}</div>
                        <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("latestVersion")}</div>
                        <div style={{ fontSize: '1rem', fontWeight: '600', color: '#059669' }}>{updateResult.latest_version}</div>
                    </div>

                    <div style={{ marginTop: '15px' }}>
                        {isDownloading ? (
                            <div style={{ width: '100%' }}>
                                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '8px', fontSize: '0.9rem' }}>
                                    <span>{t("downloading")}</span>
                                    <span>{downloadProgress}%</span>
                                </div>
                                <div style={{ width: '100%', height: '10px', backgroundColor: '#e2e8f0', borderRadius: '5px', overflow: 'hidden' }}>
                                    <div style={{ width: String(downloadProgress) + '%', height: '100%', backgroundColor: '#6366f1', transition: 'width 0.2s ease' }}></div>
                                </div>
                                <button
                                    className="btn-link"
                                    style={{ marginTop: '10px', color: '#ef4444' }}
                                    onClick={onCancelDownload}
                                >
                                    {t("cancelDownload")}
                                </button>
                            </div>
                        ) : installerPath ? (
                            <div style={{ textAlign: 'center', padding: '10px' }}>
                                <p style={{ color: '#059669', fontWeight: 'bold', marginBottom: '15px' }}>{t("downloadComplete")}</p>
                                <button className="btn-primary" style={{ width: '100%' }} onClick={onInstall}>
                                    {t("installNow")}
                                </button>
                            </div>
                        ) : (
                            <div>
                                {downloadError && (
                                    <div style={{ marginBottom: '10px' }}>
                                        <p style={{ color: '#ef4444', fontSize: '0.85rem', marginBottom: '5px' }}>{t("downloadError").replace("{error}", downloadError)}</p>
                                        <button className="btn-primary" style={{ width: '100%', backgroundColor: '#ef4444' }} onClick={onDownload}>
                                            {t("retry")}
                                        </button>
                                    </div>
                                )}
                                {!downloadError && (
                                    <>
                                        <p style={{ margin: '10px 0', fontSize: '0.9rem', color: '#374151' }}>{t("foundNewVersionMsg")}</p>
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
                <div style={{ backgroundColor: '#eef2ff', padding: '12px', borderRadius: '6px', border: '1px solid #e0e7ff' }}>
                    <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("currentVersion")}</div>
                    <div style={{ fontSize: '1rem', fontWeight: '600', color: '#4338ca', marginBottom: '12px' }}>v{appVersion}</div>
                    <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("latestVersion")}</div>
                    <div style={{ fontSize: '1rem', fontWeight: '600', color: '#059669', marginBottom: '12px' }}>{updateResult.latest_version}</div>
                    <p style={{ margin: '0', fontSize: '0.9rem', color: '#059669', fontWeight: '500' }}>??{t("isLatestVersion")}</p>
                </div>
            )}
            <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                <button className="btn-primary" disabled={isDownloading} onClick={onClose}>{t("close")}</button>
            </div>
        </div>
    </div>
);
