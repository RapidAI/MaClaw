import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

type InstallLogModalProps = {
    envLogs: string[];
    t: (key: string) => string;
    onClose: () => void;
    onCopied: () => void;
    onSendLog: (hasError: boolean) => Promise<void> | void;
};

export const InstallLogModal = ({ envLogs, t, onClose, onCopied, onSendLog }: InstallLogModalProps) => {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    return (
    <div className="modal-overlay" {...backdropProps}>
        <div
            className="modal-content install-log-modal"
            {...dialogProps}
        >
            <div className="install-log-modal__header">
                <h3>{t("installLogTitle")}</h3>
                <button className="modal-close" onClick={onClose}>&times;</button>
            </div>
            <div className="elegant-scrollbar install-log-modal__body">
                {envLogs.length === 0 ? (
                    <div className="install-log-modal__empty">
                        {t("initializing")}
                    </div>
                ) : (
                    envLogs.map((log, index) => {
                        const isError = /error|failed/i.test(log);
                        return (
                            <div key={index} className="install-log-modal__line" data-error={isError ? 'true' : 'false'}>
                                {isError ? `** ${log}` : log}
                            </div>
                        );
                    })
                )}
            </div>
            <div className="install-log-modal__actions">
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
};
