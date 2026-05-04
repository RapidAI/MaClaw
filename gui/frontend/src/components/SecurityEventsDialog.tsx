import React, { useEffect, useState } from 'react';
import { QuerySecurityEvents } from '../../wailsjs/go/main/App';

interface SecurityEvent {
    time: string;
    tool_name: string;
    target: string;
    remote_ip: string;
    risk_level: string;
    reason: string;
}

type Props = {
    open: boolean;
    onClose: () => void;
    t: (key: string) => string;
};

const riskColor = (level: string) => {
    switch (level) {
        case 'critical': return '#e74c3c';
        case 'high': return '#e67e22';
        case 'medium': return '#f39c12';
        default: return '#95a5a6';
    }
};

const riskLabel = (level: string, t: (key: string) => string) => {
    const keyByLevel: Record<string, string> = {
        critical: 'securityRiskCritical',
        high: 'securityRiskHigh',
        medium: 'securityRiskMedium',
        low: 'securityRiskLow',
    };
    const key = keyByLevel[level] || 'securityRiskUnknown';
    const label = t(key);
    return label && label !== key ? label : (level || t('securityRiskUnknown'));
};

const formatText = (template: string, values: Record<string, string | number>) => {
    return Object.entries(values).reduce(
        (text, [key, value]) => text.replace(new RegExp('\\{' + key + '\\}', 'g'), String(value)),
        template,
    );
};

export function SecurityEventsDialog({ open, onClose, t }: Props) {
    const [events, setEvents] = useState<SecurityEvent[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');

    useEffect(() => {
        if (!open) return;
        setLoading(true);
        setError('');
        QuerySecurityEvents(7)
            .then((data: SecurityEvent[] | null) => setEvents(data || []))
            .catch((err: unknown) => {
                console.error('QuerySecurityEvents failed:', err);
                setError(String(err));
            })
            .finally(() => setLoading(false));
    }, [open]);

    if (!open) return null;

    const deniedSummary = formatText(t('securityEventsDeniedSummary'), { count: events.length });

    return (
        <div className="modal-backdrop" onClick={onClose}>
            <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: '680px', maxHeight: '80vh', overflow: 'auto' }}>
                <div className="modal-header">
                    <h3 style={{ fontSize: '0.92rem', margin: 0 }}>{"\u{1F6E1}\uFE0F"} {t('securityEvents')}</h3>
                    <button className="btn-close" onClick={onClose}>{"\u00d7"}</button>
                </div>
                <div className="modal-body" style={{ padding: '12px 16px' }}>
                    {loading && (
                        <p style={{ color: 'var(--theme-text-secondary)', fontSize: '0.8rem' }}>
                            {t('securityEventsLoading')}
                        </p>
                    )}
                    {!loading && error && (
                        <p style={{ color: '#e74c3c', fontSize: '0.8rem' }}>
                            {t('securityEventsLoadFailed')}{error}
                        </p>
                    )}
                    {!loading && !error && events.length === 0 && (
                        <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--theme-text-secondary)' }}>
                            <div style={{ fontSize: '2rem', marginBottom: 8 }}>{"\u2705"}</div>
                            <p style={{ fontSize: '0.82rem', margin: 0 }}>
                                {t('securityEventsAllClear')}
                            </p>
                        </div>
                    )}
                    {!loading && !error && events.length > 0 && (
                        <>
                            <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', margin: '0 0 8px' }}>
                                {deniedSummary}
                            </p>
                            <div style={{ overflowX: 'auto' }}>
                            <table style={{ width: '100%', fontSize: '0.78rem', borderCollapse: 'collapse', minWidth: 600 }}>
                                <thead>
                                    <tr style={{ borderBottom: '2px solid var(--theme-border)' }}>
                                        <th style={thStyle}>{t('securityEventsTime')}</th>
                                        <th style={thStyle}>{t('securityEventsTool')}</th>
                                        <th style={thStyle}>{t('securityEventsTarget')}</th>
                                        <th style={thStyle}>{t('securityEventsRemoteIp')}</th>
                                        <th style={thStyle}>{t('securityEventsRisk')}</th>
                                        <th style={thStyle}>{t('securityEventsReason')}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {events.map((ev, i) => (
                                        <tr key={i} style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                            <td style={tdStyle}>{ev.time}</td>
                                            <td style={{ ...tdStyle, fontWeight: 500 }}>{ev.tool_name}</td>
                                            <td style={{ ...tdStyle, maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={ev.target}>{ev.target}</td>
                                            <td style={tdStyle}>{ev.remote_ip}</td>
                                            <td style={tdStyle}>
                                                <span style={{
                                                    color: riskColor(ev.risk_level),
                                                    fontWeight: 600,
                                                    fontSize: '0.75rem',
                                                }}>
                                                    {riskLabel(ev.risk_level, t)}
                                                </span>
                                            </td>
                                            <td style={{ ...tdStyle, maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={ev.reason}>{ev.reason}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                            </div>
                        </>
                    )}
                </div>
                <div className="modal-footer">
                    <button className="btn-primary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={onClose}>
                        {t('close') || 'Close'}
                    </button>
                </div>
            </div>
        </div>
    );
}

const thStyle: React.CSSProperties = {
    padding: '6px 8px',
    textAlign: 'left',
    color: 'var(--theme-text-secondary)',
    fontWeight: 600,
    whiteSpace: 'nowrap',
};

const tdStyle: React.CSSProperties = {
    padding: '5px 8px',
    color: 'var(--theme-text-primary)',
};
