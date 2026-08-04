import { useEffect, useState } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';

type MigrationProgress = {
    phase: 'scanning' | 'copying' | 'done' | 'error';
    percent: number;
    currentFile: string;
    totalFiles: number;
    copiedFiles: number;
    error?: string;
};

/**
 * DataMigrationOverlay displays a full-screen progress overlay when maclaw
 * is migrating data to a new directory on startup.
 */
export const DataMigrationOverlay = () => {
    const [progress, setProgress] = useState<MigrationProgress | null>(null);
    const [visible, setVisible] = useState(false);

    useEffect(() => {
        const cancel = EventsOn('data-migration-progress', (p: MigrationProgress) => {
            setProgress(p);
            if (p.phase === 'scanning' || p.phase === 'copying') {
                setVisible(true);
            } else if (p.phase === 'done' || p.phase === 'error') {
                // Keep visible briefly so user sees completion.
                setTimeout(() => setVisible(false), 1500);
            }
        });
        return cancel;
    }, []);

    if (!visible || !progress) return null;

    const phaseText = () => {
        switch (progress.phase) {
            case 'scanning': return '正在扫描文件...';
            case 'copying': return `正在迁移数据 (${progress.copiedFiles}/${progress.totalFiles})`;
            case 'done': return '迁移完成 OK';
            case 'error': return `迁移出错: ${progress.error}`;
            default: return '';
        }
    };

    return (
        <div style={{
            position: 'fixed',
            inset: 0,
            zIndex: 99999,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(15, 23, 42, 0.88)',
            // Backdrop is a hardcoded dark scrim, so text must stay white —
            // --theme-on-primary flips to dark (#0f141b) under dark schemes and
            // would render dark-on-dark here.
            color: '#ffffff',
            fontFamily: 'system-ui, -apple-system, sans-serif',
        }}>
            <div style={{ textAlign: 'center', maxWidth: '480px', padding: '32px' }}>
                <h2 style={{ fontSize: '1.2rem', marginBottom: '16px', fontWeight: 500 }}>
                    数据目录迁移
                </h2>
                <p style={{ fontSize: '0.85rem', color: 'rgba(226, 232, 240, 0.78)', marginBottom: '24px' }}>
                    {phaseText()}
                </p>

                {/* Progress bar */}
                <div style={{
                    width: '100%',
                    height: '8px',
                    background: 'rgba(148, 163, 184, 0.24)',
                    borderRadius: '4px',
                    overflow: 'hidden',
                    marginBottom: '12px',
                }}>
                    <div style={{
                        width: `${Math.min(100, progress.percent)}%`,
                        height: '100%',
                        background: progress.phase === 'error' ? 'var(--theme-danger, #c43d34)' : progress.phase === 'done' ? 'var(--theme-success, #4f7f6f)' : 'var(--theme-primary, #2f5f98)',
                        borderRadius: '4px',
                        transition: 'width 0.3s ease',
                    }} />
                </div>

                {/* Current file */}
                {progress.phase === 'copying' && progress.currentFile && (
                    <p style={{ fontSize: '0.7rem', color: 'rgba(203, 213, 225, 0.72)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {progress.currentFile}
                    </p>
                )}

                {/* Percentage */}
                <p style={{ fontSize: '0.8rem', color: 'rgba(203, 213, 225, 0.82)', marginTop: '8px' }}>
                    {Math.round(progress.percent)}%
                </p>
            </div>
        </div>
    );
};
