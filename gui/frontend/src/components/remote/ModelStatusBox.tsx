import { colors } from "./styles";

export function formatBytes(bytes: number) {
    if (bytes <= 0) return '0 B';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1024 / 1024).toFixed(1) + ' MB';
}

export function ModelStatusBox({ exists, downloading, size, progress, downloaded, total, error, onDownload, onRetry, accentColor, t }: {
    exists: boolean; downloading: boolean; size: number; progress: number;
    downloaded: number; total: number; error: string;
    onDownload: () => void; onRetry: () => void; accentColor: string;
    t: (en: string, zhHans: string, zhHant?: string) => string;
}) {
    return (
        <div style={{ background: colors.surface, border: `1px solid ${colors.border}`, borderRadius: 6, padding: '12px 14px' }}>
            {exists && !downloading && (
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: accentColor, fontSize: '1rem' }}>✓</span>
                    <span style={{ fontSize: '0.8rem', color: colors.text }}>{t('Model Ready', '模型已就绪')}</span>
                    <span style={{ fontSize: '0.74rem', color: colors.textMuted, marginLeft: 'auto' }}>{formatBytes(size)}</span>
                </div>
            )}
            {downloading && (
                <div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                        <span style={{ fontSize: '0.78rem', color: colors.text }}>{t('Downloading...', '正在下载模型...')}</span>
                        <span style={{ fontSize: '0.74rem', color: colors.textMuted }}>
                            {progress}% — {formatBytes(downloaded)} / {total > 0 ? formatBytes(total) : '?'}
                        </span>
                    </div>
                    <div style={{ width: '100%', height: 6, background: colors.border, borderRadius: 3, overflow: 'hidden' }}>
                        <div style={{ width: `${progress}%`, height: '100%', background: accentColor, borderRadius: 3, transition: 'width 0.3s ease' }} />
                    </div>
                </div>
            )}
            {!exists && !downloading && (
                <div>
                    <div style={{ fontSize: '0.78rem', color: colors.textSecondary, marginBottom: 8 }}>
                        {t('Model file not found. Download required.', '模型文件未找到，需要下载。')}
                    </div>
                    <button onClick={onDownload} style={{ padding: '6px 16px', fontSize: '0.78rem', background: accentColor, color: 'var(--theme-on-primary)', border: 'none', borderRadius: 4, cursor: 'pointer' }}>
                        {t('Download Model', '下载模型')}
                    </button>
                </div>
            )}
            {error && (
                <div style={{ marginTop: 8 }}>
                    <span style={{ fontSize: '0.76rem', color: 'var(--theme-danger)' }}>{t('Error: ', '错误：')}{error}</span>
                    {!downloading && (
                        <button onClick={onRetry} style={{ marginLeft: 10, padding: '4px 12px', fontSize: '0.74rem', background: colors.surface, color: colors.text, border: `1px solid ${colors.border}`, borderRadius: 4, cursor: 'pointer' }}>
                            {t('Retry', '重试')}
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
