type ConfirmDialogProps = {
    title: string;
    message: string;
    t: (key: string) => string;
    onCancel: () => void;
    onConfirm: () => void;
};

export const ConfirmDialog = ({ title, message, t, onCancel, onConfirm }: ConfirmDialogProps) => (
    <div style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.6)',
        backdropFilter: 'blur(4px)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 10000,
        animation: 'fadeIn 0.2s ease-out'
    }}>
        <div style={{
            backgroundColor: 'var(--theme-surface)',
            borderRadius: '12px',
            padding: '24px',
            minWidth: '360px',
            maxWidth: '420px',
            boxShadow: '0 20px 60px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(0, 0, 0, 0.05)',
            border: '1px solid var(--theme-border)',
            animation: 'slideUp 0.3s ease-out',
            position: 'relative'
        }}>
            <div style={{
                width: '48px',
                height: '48px',
                borderRadius: '50%',
                backgroundColor: 'var(--theme-danger-bg)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                marginBottom: '16px',
                border: '2px solid color-mix(in srgb, var(--theme-danger) 22%, var(--theme-border))'
            }}>
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#ef4444" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="12"></line>
                    <line x1="12" y1="16" x2="12.01" y2="16"></line>
                </svg>
            </div>

            <h3 style={{
                margin: '0 0 8px 0',
                fontSize: '1.15rem',
                color: 'var(--theme-text-primary)',
                fontWeight: '700',
                letterSpacing: '-0.02em'
            }}>
                {title}
            </h3>

            <p style={{
                margin: '0 0 20px 0',
                color: 'var(--theme-text-secondary)',
                fontSize: '0.9rem',
                lineHeight: '1.5',
                fontWeight: '400'
            }}>
                {message}
            </p>

            <div style={{
                display: 'flex',
                justifyContent: 'flex-end',
                gap: '10px'
            }}>
                <button
                    onClick={onCancel}
                    style={{
                        padding: '8px 20px',
                        backgroundColor: 'var(--theme-surface-muted)',
                        color: 'var(--theme-text-primary)',
                        border: '1px solid var(--theme-border)',
                        borderRadius: '8px',
                        cursor: 'pointer',
                        fontSize: '0.875rem',
                        fontWeight: '600',
                        transition: 'all 0.2s',
                        boxShadow: '0 1px 2px rgba(0, 0, 0, 0.05)'
                    }}
                    onMouseEnter={(e) => {
                        e.currentTarget.style.backgroundColor = 'var(--theme-surface-muted)';
                        e.currentTarget.style.borderColor = 'var(--theme-border)';
                        e.currentTarget.style.transform = 'translateY(-1px)';
                        e.currentTarget.style.boxShadow = '0 2px 4px rgba(0, 0, 0, 0.1)';
                    }}
                    onMouseLeave={(e) => {
                        e.currentTarget.style.backgroundColor = 'var(--theme-surface-muted)';
                        e.currentTarget.style.borderColor = 'var(--theme-border)';
                        e.currentTarget.style.transform = 'translateY(0)';
                        e.currentTarget.style.boxShadow = '0 1px 2px rgba(0, 0, 0, 0.05)';
                    }}
                >
                    {t("cancel")}
                </button>
                <button
                    onClick={onConfirm}
                    style={{
                        padding: '8px 20px',
                        backgroundColor: 'var(--theme-danger)',
                        color: 'white',
                        border: 'none',
                        borderRadius: '8px',
                        cursor: 'pointer',
                        fontSize: '0.875rem',
                        fontWeight: '600',
                        transition: 'all 0.2s',
                        boxShadow: '0 2px 4px rgba(239, 68, 68, 0.3)'
                    }}
                    onMouseEnter={(e) => {
                        e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--theme-danger) 86%, #000)';
                        e.currentTarget.style.transform = 'translateY(-1px)';
                        e.currentTarget.style.boxShadow = '0 4px 8px rgba(239, 68, 68, 0.4)';
                    }}
                    onMouseLeave={(e) => {
                        e.currentTarget.style.backgroundColor = 'var(--theme-danger)';
                        e.currentTarget.style.transform = 'translateY(0)';
                        e.currentTarget.style.boxShadow = '0 2px 4px rgba(239, 68, 68, 0.3)';
                    }}
                >
                    {t("confirm")}
                </button>
            </div>
        </div>
    </div>
);
