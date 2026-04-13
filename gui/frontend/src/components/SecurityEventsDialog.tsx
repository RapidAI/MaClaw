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

const riskLabel = (level: string, lang: string) => {
    const labels: Record<string, [string, string]> = {
        critical: ['严重', 'Critical'],
        high: ['高', 'High'],
        medium: ['中', 'Medium'],
        low: ['低', 'Low'],
    };
    const pair = labels[level] || [level, level];
    return lang.startsWith('zh') ? pair[0] : pair[1];
};

export function SecurityEventsDialog({ open, onClose, t }: Props) {
    const [events, setEvents] = useState<SecurityEvent[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const lang = document.documentElement.lang || 'en';

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

    const isZh = lang.startsWith('zh');

    return (
        <div className="modal-backdrop" onClick={onClose}>
            <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: '680px', maxHeight: '80vh', overflow: 'auto' }}>
                <div className="modal-header">
                    <h3 style={{ fontSize: '0.92rem', margin: 0 }}>🛡️ {isZh ? '安全事件' : 'Security Events'}</h3>
                    <button className="btn-close" onClick={onClose}>×</button>
                </div>
                <div className="modal-body" style={{ padding: '12px 16px' }}>
                    {loading && (
                        <p style={{ color: 'var(--theme-text-secondary)', fontSize: '0.8rem' }}>
                            {isZh ? '加载中...' : 'Loading...'}
                        </p>
                    )}
                    {!loading && error && (
                        <p style={{ color: '#e74c3c', fontSize: '0.8rem' }}>
                            {isZh ? '加载失败: ' : 'Failed: '}{error}
                        </p>
                    )}
                    {!loading && !error && events.length === 0 && (
                        <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--theme-text-secondary)' }}>
                            <div style={{ fontSize: '2rem', marginBottom: 8 }}>✅</div>
                            <p style={{ fontSize: '0.82rem', margin: 0 }}>
                                {isZh ? '一切安全，最近 7 天没有被拒绝的操作' : 'All clear — no denied operations in the last 7 days'}
                            </p>
                        </div>
                    )}
                    {!loading && !error && events.length > 0 && (
                        <>
                            <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', margin: '0 0 8px' }}>
                                {isZh
                                    ? `最近 7 天共 ${events.length} 条被拒绝的操作`
                                    : `${events.length} denied operation(s) in the last 7 days`}
                            </p>
                            <div style={{ overflowX: 'auto' }}>
                            <table style={{ width: '100%', fontSize: '0.78rem', borderCollapse: 'collapse', minWidth: 600 }}>
                                <thead>
                                    <tr style={{ borderBottom: '2px solid var(--theme-border)' }}>
                                        <th style={thStyle}>{isZh ? '时间' : 'Time'}</th>
                                        <th style={thStyle}>{isZh ? '工具/操作' : 'Tool'}</th>
                                        <th style={thStyle}>{isZh ? '目标' : 'Target'}</th>
                                        <th style={thStyle}>{isZh ? '远程 IP' : 'Remote IP'}</th>
                                        <th style={thStyle}>{isZh ? '风险' : 'Risk'}</th>
                                        <th style={thStyle}>{isZh ? '拒绝原因' : 'Reason'}</th>
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
                                                    {riskLabel(ev.risk_level, lang)}
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
