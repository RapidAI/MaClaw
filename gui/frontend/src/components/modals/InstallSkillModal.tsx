import type { Dispatch, SetStateAction } from 'react';
import { InstallSkill } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { InstallSkillList } from './InstallSkillList';
import { InstallLocationSelector } from './InstallLocationSelector';
import { InstallSkillFooter } from './InstallSkillFooter';

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
    const filteredSkills = installLocation === 'project'
        ? skills.filter(s => s.type !== 'address')
        : skills;

    const installSelectedSkills = async () => {
        setIsBatchInstalling(true);
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
        onClose();

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
                        onClick={() => { onClose(); switchTool('skills'); }}
                        style={{
                            background: 'none',
                            border: '1px solid var(--theme-border)',
                            borderRadius: '16px',
                            padding: '4px 10px',
                            fontSize: '0.8rem',
                            cursor: 'pointer',
                            color: 'var(--theme-primary)',
                            display: 'flex',
                            alignItems: 'center',
                            gap: '4px',
                            whiteSpace: 'nowrap',
                        }}
                        title={t("skills")}
                    >
                        {'\u{1F6E0}\uFE0F'} {t("skills")}
                    </button>

                    <button onClick={onClose} className="btn-close" style={{ marginLeft: 'auto' }}>&times;</button>
                </div>
                <InstallSkillList
                    filteredSkills={filteredSkills}
                    selectedSkillsToInstall={selectedSkillsToInstall}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    t={t}
                />
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
                />
            </div>
        </div>
    );
};
