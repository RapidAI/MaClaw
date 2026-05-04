type ToolRepairStatus = {
    show: boolean;
    toolName: string;
    status: 'installing' | 'success' | 'failed';
    message: string;
};

type ToolRepairProgressDialogProps = {
    status: ToolRepairStatus;
    t: (key: string) => string;
    onClose: () => void;
};

export const ToolRepairProgressDialog = ({ status, t, onClose }: ToolRepairProgressDialogProps) => (
    <div className="modal-overlay" style={{ backgroundColor: 'rgba(0, 0, 0, 0.3)' }}>
        <div style={{
            backgroundColor: 'white',
            borderRadius: '16px',
            padding: '20px 28px',
            textAlign: 'center',
            boxShadow: '0 8px 32px rgba(0, 0, 0, 0.12)',
            minWidth: '220px',
            maxWidth: '280px'
        }}>
            {status.status === 'installing' && (
                <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                    <div style={{
                        width: '24px',
                        height: '24px',
                        border: '3px solid #e2e8f0',
                        borderTop: '3px solid #6366f1',
                        borderRadius: '50%',
                        animation: 'spin 0.8s linear infinite',
                        flexShrink: 0
                    }}></div>
                    <span style={{ color: '#475569', fontSize: '0.9rem', fontWeight: 500 }}>
                        {t("toolRepairInstalling").replace("{tool}", status.toolName)}
                    </span>
                </div>
            )}
            {status.status === 'success' && (
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                    <div style={{
                        width: '28px',
                        height: '28px',
                        backgroundColor: '#dcfce7',
                        borderRadius: '50%',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        flexShrink: 0
                    }}>
                        <span style={{ color: '#16a34a', fontSize: '16px' }}>&#10003;</span>
                    </div>
                    <span style={{ color: '#16a34a', fontSize: '0.9rem', fontWeight: 500 }}>
                        {t("toolRepairSuccess").replace("{tool}", status.toolName)}
                    </span>
                </div>
            )}
            {status.status === 'failed' && (
                <div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '12px' }}>
                        <div style={{
                            width: '28px',
                            height: '28px',
                            backgroundColor: '#fee2e2',
                            borderRadius: '50%',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            flexShrink: 0
                        }}>
                            <span style={{ color: '#dc2626', fontSize: '14px' }}>&#10005;</span>
                        </div>
                        <span style={{ color: '#dc2626', fontSize: '0.9rem', fontWeight: 500 }}>
                            {t("toolRepairFailed").replace("{tool}", status.toolName)}
                        </span>
                    </div>
                    <p style={{ color: '#6b7280', fontSize: '0.8rem', margin: '0 0 12px 0', wordBreak: 'break-word', textAlign: 'left' }}>
                        {status.message}
                    </p>
                    <button
                        style={{
                            backgroundColor: '#f1f5f9',
                            border: 'none',
                            borderRadius: '8px',
                            padding: '6px 16px',
                            fontSize: '0.85rem',
                            color: '#475569',
                            cursor: 'pointer',
                            fontWeight: 500
                        }}
                        onClick={onClose}
                    >
                        {t("close")}
                    </button>
                </div>
            )}
        </div>
    </div>
);
