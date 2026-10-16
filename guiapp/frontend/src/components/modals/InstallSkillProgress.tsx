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

const skillInstallProgressTone = (phase?: string) => {
    if (phase === 'blocked' || phase === 'rejected') return 'danger';
    if (phase === 'done' || phase === 'scan-complete') return 'success';
    return 'primary';
};

export const InstallSkillProgress = ({ progress, isInstalling }: Props) => {
    if (!progress) return null;

    return (
        <div className="install-skill-progress" role="status" aria-live="polite">
            <div className="install-skill-progress__head">
                {isInstalling && !isTerminalSkillInstallPhase(progress.phase) && (
                    <div className="install-skill-progress__spinner" data-tone={skillInstallProgressTone(progress.phase)} />
                )}
                <div className="install-skill-progress__title">
                    {progress.skill ? progress.skill : 'Skill install'}
                    {progress.level ? ` - risk ${progress.level}` : ''}
                </div>
            </div>
            <div
                className="install-skill-progress__track"
                aria-hidden="true"
            >
                <div
                    className="install-skill-progress__bar"
                    data-tone={skillInstallProgressTone(progress.phase)}
                    style={{
                        width: String(progress.percent ?? 25) + '%',
                    }}
                />
            </div>
            <div className="install-skill-progress__status">
                {progress.status || progress.summary || 'Working...'}
            </div>
        </div>
    );
};
