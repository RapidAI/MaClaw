import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';

type InstallSkillListProps = {
    filteredSkills: main.Skill[];
    selectedSkillsToInstall: string[];
    setSelectedSkillsToInstall: Dispatch<SetStateAction<string[]>>;
    t: (key: string) => string;
};

export const InstallSkillList = ({
    filteredSkills,
    selectedSkillsToInstall,
    setSelectedSkillsToInstall,
    t,
}: InstallSkillListProps) => (
    <div className="modal-body" style={{ maxHeight: '300px', overflowY: 'auto', padding: '10px 0' }}>
        {filteredSkills.length === 0 ? (
            <div style={{ textAlign: 'center', color: 'var(--theme-text-muted)', padding: '20px' }}>
                {t("noSkills")}
            </div>
        ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {filteredSkills.map((skill, idx) => (
                    <label key={idx} style={{
                        display: 'flex',
                        alignItems: 'center',
                        padding: '8px 12px',
                        border: '1px solid var(--theme-border)',
                        borderRadius: '6px',
                        cursor: skill.installed ? 'not-allowed' : 'pointer',
                        backgroundColor: selectedSkillsToInstall.includes(skill.name) ? 'color-mix(in srgb, var(--theme-success) 13%, var(--theme-surface))' : 'var(--theme-surface)',
                        opacity: skill.installed ? 0.5 : 1,
                        position: 'relative'
                    }}>
                        <input
                            type="checkbox"
                            checked={selectedSkillsToInstall.includes(skill.name)}
                            disabled={skill.installed}
                            onChange={(e) => {
                                if (e.target.checked) {
                                    setSelectedSkillsToInstall([...selectedSkillsToInstall, skill.name]);
                                } else {
                                    setSelectedSkillsToInstall(selectedSkillsToInstall.filter(n => n !== skill.name));
                                }
                            }}
                            style={{ marginRight: '10px' }}
                        />
                        <div style={{ flex: 1 }} title={skill.description}>
                            <div style={{ fontWeight: 'bold', fontSize: '0.9rem', color: 'var(--theme-text-primary)' }}>
                                {skill.name}
                                {skill.installed && (
                                    <span style={{
                                        marginLeft: '8px',
                                        fontSize: '0.75rem',
                                        color: 'var(--theme-success)',
                                        backgroundColor: 'var(--theme-success-bg)',
                                        padding: '2px 6px',
                                        borderRadius: '4px',
                                        fontWeight: 'normal'
                                    }}>
                                        {t("installed")}
                                    </span>
                                )}
                            </div>
                        </div>
                    </label>
                ))}
            </div>
        )}
    </div>
);
