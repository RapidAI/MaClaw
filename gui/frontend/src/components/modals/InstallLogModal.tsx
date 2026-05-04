type InstallLogModalProps = {
    envLogs: string[];
    t: (key: string) => string;
    onClose: () => void;
    onCopied: () => void;
    onSendLog: (hasError: boolean) => Promise<void> | void;
};

export const InstallLogModal = ({ envLogs, t, onClose, onCopied, onSendLog }: InstallLogModalProps) => (
    <div className="modal-overlay" onClick={onClose}>
        <div className="modal-content" style={{ width: '600px', maxWidth: '90vw' }} onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '15px' }}>
                <h3 style={{ margin: 0, color: '#6366f1' }}>{t("installLogTitle")}</h3>
                <button className="modal-close" onClick={onClose}>&times;</button>
            </div>
            <div
                className="elegant-scrollbar"
                style={{
                    backgroundColor: '#1e293b',
                    color: '#e2e8f0',
                    padding: '15px',
                    borderRadius: '8px',
                    height: '250px',
                    overflowY: 'auto',
                    fontFamily: 'monospace',
                    fontSize: '0.85rem',
                    whiteSpace: 'pre-wrap',
                    textAlign: 'left',
                    marginBottom: '15px'
                }}>
                {envLogs.length === 0 ? (
                    <div style={{ color: '#94a3b8', fontStyle: 'italic' }}>
                        {t("initializing")}
                    </div>
                ) : (
                    envLogs.map((log, index) => {
                        const isError = /error|failed/i.test(log);
                        return (
                            <div key={index} style={{
                                color: isError ? '#ef4444' : 'inherit',
                                marginBottom: '4px'
                            }}>
                                {isError ? `** ${log}` : log}
                            </div>
                        );
                    })
                )}
            </div>
            <div style={{
                display: 'flex',
                justifyContent: 'flex-end',
                gap: '10px'
            }}>
                <button
                    className="btn-link"
                    onClick={() => {
                        const logText = envLogs.join('\n');
                        navigator.clipboard.writeText(logText).then(onCopied);
                    }}
                >
                    {t("copyLog")}
                </button>
                <button
                    className="btn-link"
                    onClick={async () => {
                        console.log('Send log button clicked');
                        const hasError = envLogs.some(log => /error|failed/i.test(log));
                        await onSendLog(hasError);
                    }}
                >
                    {t("sendLog")}
                </button>
            </div>
        </div>
    </div>
);
