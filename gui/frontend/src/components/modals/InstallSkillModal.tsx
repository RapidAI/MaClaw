import { useEffect, useState, type Dispatch, type SetStateAction } from 'react';
import { InstallSkill } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime';
import { InstallSkillList } from './InstallSkillList';
import { InstallLocationSelector } from './InstallLocationSelector';
import { InstallSkillFooter } from './InstallSkillFooter';
import { InstallSkillProgress, type SkillInstallProgress } from './InstallSkillProgress';

type InstallSkillModalProps = {
    config: main.AppConfig;
    skills: main.Skill[];
    activeTool: string;
    installLocation: 'user' | 'project';
    setInstallLocation: Dispatch<SetStateAction<'user' | 'project'>>;
    installProject: string;
    setInstallProject: Dispatch<SetStateAction<string>>;
    selectedSkillsToInstall: string[];
    setSelectedSkillsToInstall: Dispatch<SetStateAction<string[]>>;
    isBatchInstalling: boolean;
    setIsBatchInstalling: Dispatch<SetStateAction<boolean>>;
    isMarketplaceInstalling: boolean;
    setIsMarketplaceInstalling: Dispatch<SetStateAction<boolean>>;
    t: (key: string) => string;
    switchTool: (tool: string) => void;
    showToastMessage: (message: string, duration?: number) => void;
    onClose: () => void;
};

export const InstallSkillModal = ({
    config,
    skills,
    activeTool,
    installLocation,
    setInstallLocation,
    installProject,
    setInstallProject,
    selectedSkillsToInstall,
    setSelectedSkillsToInstall,
    isBatchInstalling,
    setIsBatchInstalling,
    isMarketplaceInstalling,
    setIsMarketplaceInstalling,
    t,
    switchTool,
    showToastMessage,
    onClose,
}: InstallSkillModalProps) => {
    const [installProgress, setInstallProgress] = useState<SkillInstallProgress | null>(null);

    useEffect(() => {
        const cleanup = EventsOn('skill-install-progress', (payload: any) => {
            if (!payload || typeof payload !== 'object') return;
            setInstallProgress({
                skill: typeof payload.skill === 'string' ? payload.skill : undefined,
                phase: typeof payload.phase === 'string' ? payload.phase : undefined,
                status: typeof payload.status === 'string' ? payload.status : undefined,
                level: typeof payload.level === 'string' ? payload.level : undefined,
                summary: typeof payload.summary === 'string' ? payload.summary : undefined,
                percent: typeof payload.percent === 'number' ? Math.max(0, Math.min(100, payload.percent)) : undefined,
            });
        });
        return cleanup;
    }, []);

    const filteredSkills = installLocation === 'project'
        ? skills.filter(s => s.type !== 'address')
        : skills;

    const installSelectedSkills = async () => {
        setIsBatchInstalling(true);
        setInstallProgress({ phase: 'queued', status: 'Preparing selected skills for installation.', percent: 5 });
        let successCount = 0;
        let failCount = 0;

        let targetProjectPath = '';
        if (installLocation === 'project') {
            const p = config?.projects?.find((proj: any) => proj.id === installProject);
            if (p) targetProjectPath = p.path;
        }

        for (const name of selectedSkillsToInstall) {
            const skill = skills.find(s => s.name === name);
            if (skill) {
                setInstallProgress({ skill: skill.name, phase: 'queued', status: 'Queued for install.', percent: 5 });
                const isGeminiOrCodex = activeTool?.toLowerCase() === 'gemini' || activeTool?.toLowerCase() === 'codex';
                if (isGeminiOrCodex && skill.type === 'address') {
                    console.warn('Skill ' + skill.name + ' is not supported for ' + activeTool);
                    failCount++;
                    continue;
                }

                try {
                    await InstallSkill(skill.name, skill.description, skill.type, skill.value, installLocation, targetProjectPath, activeTool);
                    successCount++;
                } catch (e) {
                    console.error(e);
                    failCount++;
                }
            }
        }

        setIsBatchInstalling(false);

        if (failCount > 0) {
            const isGeminiOrCodex = activeTool?.toLowerCase() === 'gemini' || activeTool?.toLowerCase() === 'codex';
            if (isGeminiOrCodex && selectedSkillsToInstall.some(name => skills.find(s => s.name === name)?.type === 'address')) {
                showToastMessage(t("skillZipOnlyError"));
            } else {
                showToastMessage(successCount + ' installed, ' + failCount + ' failed.');
            }
        } else {
            showToastMessage(successCount + ' skills installed successfully.');
        }

        setTimeout(() => {
            setInstallProgress(null);
            onClose();
        }, 600);
    };

    return (
        <div className="modal-overlay">
            <div className="modal-content" style={{ width: '500px', maxWidth: '95vw' }}>
                <div className="modal-header" style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' }}>
                    <h3 style={{ margin: 0, color: 'var(--theme-success)', whiteSpace: 'nowrap' }}>{t("selectSkillsToInstall")}</h3>

                    <InstallLocationSelector
                        config={config}
                        installLocation={installLocation}
                        setInstallLocation={setInstallLocation}
                        installProject={installProject}
                        setInstallProject={setInstallProject}
                        t={t}
                    />

                    <button
                        onClick={() => { if (!isBatchInstalling) { onClose(); switchTool('skills'); } }}
                        disabled={isBatchInstalling}
                        style={{
                            background: 'none',
                            border: '1px solid var(--theme-border)',
                            borderRadius: '16px',
                            padding: '4px 10px',
                            fontSize: '0.8rem',
                            cursor: 'pointer',
                            color: 'var(--theme-primary)',
                            opacity: isBatchInstalling ? 0.6 : 1,
                            display: 'flex',
                            alignItems: 'center',
                            gap: '4px',
                            whiteSpace: 'nowrap',
                        }}
                        title={t("skills")}
                    >
                        {'\u{1F6E0}\uFE0F'} {t("skills")}
                    </button>

                    <button onClick={onClose} disabled={isBatchInstalling} className="btn-close" style={{ marginLeft: 'auto', opacity: isBatchInstalling ? 0.6 : 1 }}>&times;</button>
                </div>
                <InstallSkillList
                    filteredSkills={filteredSkills}
                    selectedSkillsToInstall={selectedSkillsToInstall}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    t={t}
                />
                <InstallSkillProgress progress={installProgress} isInstalling={isBatchInstalling} />
                <InstallSkillFooter
                    activeTool={activeTool}
                    selectedSkillsToInstall={selectedSkillsToInstall}
                    isBatchInstalling={isBatchInstalling}
                    isMarketplaceInstalling={isMarketplaceInstalling}
                    setIsMarketplaceInstalling={setIsMarketplaceInstalling}
                    t={t}
                    showToastMessage={showToastMessage}
                    onClose={onClose}
                    onInstallSelected={installSelectedSkills}
                    closeDisabled={isBatchInstalling}
                />
            </div>
        </div>
    );
};
