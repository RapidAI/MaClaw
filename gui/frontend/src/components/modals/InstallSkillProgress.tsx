export type SkillInstallProgress = {
    skill?: string;
    phase?: string;
    status?: string;
    level?: string;
    summary?: string;
    percent?: number;
};

type Props = {
    progress: SkillInstallProgress | null;
    isInstalling: boolean;
};

const isTerminalSkillInstallPhase = (phase?: string) =>
    phase === 'done' || phase === 'scan-complete' || phase === 'blocked' || phase === 'rejected';

const skillInstallProgressColor = (phase?: string) => {
    if (phase === 'blocked' || phase === 'rejected') return 'var(--theme-danger)';
    if (phase === 'done' || phase === 'scan-complete') return 'var(--theme-success)';
    return 'var(--theme-primary)';
};

export const InstallSkillProgress = ({ progress, isInstalling }: Props) => {
    if (!progress) return null;

    return (
        <div
            role="status"
            aria-live="polite"
            style={{
                marginTop: '12px',
                padding: '10px 12px',
                border: '1px solid var(--theme-border)',
                borderRadius: '8px',
                background: 'var(--theme-bg-secondary)',
                color: 'var(--theme-text)',
                display: 'grid',
                gap: '8px',
            }}
        >
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', minWidth: 0 }}>
                {isInstalling && !isTerminalSkillInstallPhase(progress.phase) && (
                    <div style={{ width: '14px', height: '14px', border: `2px solid ${skillInstallProgressColor(progress.phase)}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite', flex: '0 0 auto' }} />
                )}
                <div style={{ minWidth: 0, fontSize: '0.86rem', fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {progress.skill ? progress.skill : 'Skill install'}
                    {progress.level ? ` - risk ${progress.level}` : ''}
                </div>
            </div>
            <div
                style={{
                    height: '4px',
                    borderRadius: '999px',
                    background: 'var(--theme-border)',
                    overflow: 'hidden',
                }}
                aria-hidden="true"
            >
                <div
                    style={{
                        width: String(progress.percent ?? 25) + '%',
                        height: '100%',
                        background: skillInstallProgressColor(progress.phase),
                        transition: 'width 0.25s ease',
                    }}
                />
            </div>
            <div style={{ fontSize: '0.82rem', color: 'var(--theme-text-secondary)', lineHeight: 1.4 }}>
                {progress.status || progress.summary || 'Working...'}
            </div>
        </div>
    );
};
