import React, { useEffect, useState } from 'react';
import { GetMemoryHealth } from '../../wailsjs/go/main/App';

interface HealthReport {
    active_entries: number;
    max_capacity: number;
    capacity_percent: number;
    archived_entries: number;
    stale_entries: number;
    orphan_entries: number;
    no_embedding: number;
    no_hash: number;
    pinned_entries: number;
    embedder_active: boolean;
    category_counts: Record<string, number>;
    avg_access_count: number;
    oldest_entry: string;
    newest_entry: string;
    versioned_entries: number;
}

type Props = {
    open: boolean;
    onClose: () => void;
    t: (key: string) => string;
};

export function MemoryHealthDialog({ open, onClose, t }: Props) {
    const [report, setReport] = useState<HealthReport | null>(null);
    const [loading, setLoading] = useState(false);

    useEffect(() => {
        if (!open) return;
        setLoading(true);
        GetMemoryHealth()
            .then((r: HealthReport) => setReport(r))
            .catch((err: unknown) => {
                console.error('GetMemoryHealth failed:', err);
                setReport(null);
            })
            .finally(() => setLoading(false));
    }, [open]);

    if (!open) return null;

    const formatDate = (iso: string) => {
        if (!iso) return '-';
        try {
            return new Date(iso).toLocaleString();
        } catch {
            return iso;
        }
    };

    const capacityColor = (pct: number) => {
        if (pct >= 90) return '#e74c3c';
        if (pct >= 70) return '#f39c12';
        return '#27ae60';
    };

    return (
        <div className="modal-backdrop" onClick={onClose}>
            <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: '480px', maxHeight: '80vh', overflow: 'auto' }}>
                <div className="modal-header">
                    <h3 style={{ fontSize: '0.92rem', margin: 0 }}>🧠 {t('memoryHealthTitle')}</h3>
                    <button className="btn-close" onClick={onClose}>×</button>
                </div>
                <div className="modal-body" style={{ padding: '12px 16px' }}>
                    {loading && <p style={{ color: 'var(--theme-text-secondary)', fontSize: '0.8rem' }}>{t('loading')}...</p>}
                    {!loading && report && (
                        <>
                            <table style={{ width: '100%', fontSize: '0.8rem', borderCollapse: 'collapse' }}>
                                <tbody>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthCapacity')}</td>
                                        <td style={valueStyle}>
                                            <span style={{ color: capacityColor(report.capacity_percent), fontWeight: 600 }}>
                                                {report.active_entries} / {report.max_capacity}
                                            </span>
                                            <span style={{ marginLeft: 6, color: 'var(--theme-text-secondary)' }}>
                                                ({report.capacity_percent.toFixed(1)}%)
                                            </span>
                                        </td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthArchived')}</td>
                                        <td style={valueStyle}>{report.archived_entries}</td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthStale')}</td>
                                        <td style={valueStyle}>
                                            <span style={{ color: report.stale_entries > 0 ? '#f39c12' : 'inherit' }}>
                                                {report.stale_entries}
                                            </span>
                                        </td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthOrphan')}</td>
                                        <td style={valueStyle}>{report.orphan_entries}</td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthNoEmbed')}</td>
                                        <td style={valueStyle}>
                                            <span style={{ color: report.no_embedding > 0 ? '#e67e22' : 'inherit' }}>
                                                {report.no_embedding}
                                            </span>
                                        </td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthPinned')}</td>
                                        <td style={valueStyle}>{report.pinned_entries}</td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthVersioned')}</td>
                                        <td style={valueStyle}>{report.versioned_entries}</td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthEmbedder')}</td>
                                        <td style={valueStyle}>
                                            <span style={{ color: report.embedder_active ? '#27ae60' : '#e74c3c' }}>
                                                {report.embedder_active ? '✓ Active' : '✗ Inactive'}
                                            </span>
                                        </td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthAvgAccess')}</td>
                                        <td style={valueStyle}>{report.avg_access_count.toFixed(1)}</td>
                                    </tr>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <td style={labelStyle}>{t('memHealthOldest')}</td>
                                        <td style={valueStyle}>{formatDate(report.oldest_entry)}</td>
                                    </tr>
                                    <tr>
                                        <td style={labelStyle}>{t('memHealthNewest')}</td>
                                        <td style={valueStyle}>{formatDate(report.newest_entry)}</td>
                                    </tr>
                                </tbody>
                            </table>

                            {report.category_counts && Object.keys(report.category_counts).length > 0 && (
                                <>
                                    <h4 style={{ fontSize: '0.82rem', margin: '14px 0 6px', color: 'var(--theme-text-primary)' }}>
                                        {t('memHealthCategories')}
                                    </h4>
                                    <table style={{ width: '100%', fontSize: '0.78rem', borderCollapse: 'collapse' }}>
                                        <tbody>
                                            {Object.entries(report.category_counts)
                                                .sort(([, a], [, b]) => b - a)
                                                .map(([cat, count]) => (
                                                    <tr key={cat} style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                                        <td style={labelStyle}>{cat}</td>
                                                        <td style={valueStyle}>{count}</td>
                                                    </tr>
                                                ))}
                                        </tbody>
                                    </table>
                                </>
                            )}
                        </>
                    )}
                    {!loading && !report && (
                        <p style={{ color: 'var(--theme-text-secondary)', fontSize: '0.8rem' }}>
                            {t('memHealthUnavailable')}
                        </p>
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

const labelStyle: React.CSSProperties = {
    padding: '6px 8px',
    color: 'var(--theme-text-secondary)',
    whiteSpace: 'nowrap',
};

const valueStyle: React.CSSProperties = {
    padding: '6px 8px',
    textAlign: 'right',
    fontFamily: 'monospace',
};
