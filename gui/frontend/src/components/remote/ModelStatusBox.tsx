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
        <div className="model-status-box" style={{ ['--model-accent' as any]: accentColor }}>
            {exists && !downloading && (
                <div className="model-status-box__ready">
                    <span className="model-status-box__icon">OK</span>
                    <span className="model-status-box__label">{t('Model Ready', '模型已就绪')}</span>
                    <span className="model-status-box__size">{formatBytes(size)}</span>
                </div>
            )}
            {downloading && (
                <div>
                    <div className="model-status-box__progress-head">
                        <span className="model-status-box__label">{t('Downloading...', '正在下载模型...')}</span>
                        <span className="model-status-box__meta">
                            {progress}% — {formatBytes(downloaded)} / {total > 0 ? formatBytes(total) : '?'}
                        </span>
                    </div>
                    <div className="model-status-box__progress-track">
                        <div className="model-status-box__progress-fill" style={{ width: `${progress}%` }} />
                    </div>
                </div>
            )}
            {!exists && !downloading && (
                <div>
                    <div className="model-status-box__missing">
                        {t('Model file not found. Download required.', '模型文件未找到，需要下载。')}
                    </div>
                    <button onClick={onDownload} className="model-status-box__primary">
                        {t('Download Model', '下载模型')}
                    </button>
                </div>
            )}
            {error && (
                <div className="model-status-box__error">
                    <span>{t('Error: ', '错误：')}{error}</span>
                    {!downloading && (
                        <button onClick={onRetry} className="model-status-box__retry">
                            {t('Retry', '重试')}
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
