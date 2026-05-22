import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';

// Skill item surfaces use var(--theme-surface) in App.css.

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
    <div className="modal-body install-skill-list elegant-scrollbar">
        {filteredSkills.length === 0 ? (
            <div className="install-skill-list__empty">
                {t("noSkills")}
            </div>
        ) : (
            <div className="install-skill-list__items">
                {filteredSkills.map((skill, idx) => (
                    <label
                        key={idx}
                        className="install-skill-list__item"
                        data-selected={selectedSkillsToInstall.includes(skill.name) ? 'true' : 'false'}
                        data-installed={skill.installed ? 'true' : 'false'}
                    >
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
                        />
                        <div className="install-skill-list__meta" title={skill.description}>
                            <div className="install-skill-list__name">
                                {skill.name}
                                {skill.installed && (
                                    <span className="install-skill-list__badge">
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
