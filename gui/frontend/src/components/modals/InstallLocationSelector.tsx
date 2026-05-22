import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';

// Styling for selector surface uses var(--theme-surface) in App.css.

type InstallLocation = 'user' | 'project';

type InstallLocationSelectorProps = {
    config: main.AppConfig;
    installLocation: InstallLocation;
    setInstallLocation: Dispatch<SetStateAction<InstallLocation>>;
    installProject: string;
    setInstallProject: Dispatch<SetStateAction<string>>;
    t: (key: string) => string;
};

export const InstallLocationSelector = ({
    config,
    installLocation,
    setInstallLocation,
    installProject,
    setInstallProject,
    t,
}: InstallLocationSelectorProps) => (
    <div className="install-location-selector">
        <span className="install-location-selector__title">{t("installLocation")}</span>
        <label className="install-location-selector__option" data-active={installLocation === 'user' ? 'true' : 'false'}>
            <input
                type="radio"
                name="installLocation"
                checked={installLocation === 'user'}
                onChange={() => setInstallLocation('user')}
            /> {t("userLocation")}
        </label>
        <label className="install-location-selector__option" data-active={installLocation === 'project' ? 'true' : 'false'}>
            <input
                type="radio"
                name="installLocation"
                checked={installLocation === 'project'}
                onChange={() => {
                    setInstallLocation('project');
                    if (config && config.current_project) {
                        setInstallProject(config.current_project);
                    }
                }}
            /> {t("projectLocation")}
        </label>
        {installLocation === 'project' && config?.projects && (
            <select
                className="install-location-selector__select"
                value={installProject}
                onChange={(e) => setInstallProject(e.target.value)}
            >
                {config.projects.map((proj: any) => (
                    <option key={proj.id} value={proj.id}>
                        {proj.name}
                    </option>
                ))}
            </select>
        )}
    </div>
);
