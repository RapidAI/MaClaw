import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';

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
    <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: '12px',
        padding: '4px 12px',
        backgroundColor: 'var(--theme-surface)',
        border: '1px solid var(--theme-border)',
        borderRadius: '20px',
        fontSize: '0.8rem',
        marginLeft: '5px'
    }}>
        <span style={{ color: 'var(--theme-text-muted)', fontWeight: '500' }}>{t("installLocation")}</span>
        <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'user' ? 'var(--theme-success)' : 'var(--theme-text-secondary)', fontWeight: installLocation === 'user' ? 'bold' : 'normal' }}>
            <input
                type="radio"
                name="installLocation"
                checked={installLocation === 'user'}
                onChange={() => setInstallLocation('user')}
                style={{ margin: 0 }}
            /> {t("userLocation")}
        </label>
        <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'project' ? 'var(--theme-success)' : 'var(--theme-text-secondary)', fontWeight: installLocation === 'project' ? 'bold' : 'normal' }}>
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
                style={{ margin: 0 }}
            /> {t("projectLocation")}
        </label>
        {installLocation === 'project' && config?.projects && (
            <select
                value={installProject}
                onChange={(e) => setInstallProject(e.target.value)}
                style={{
                    padding: '2px 4px',
                    borderRadius: '4px',
                    border: '1px solid var(--theme-border)',
                    background: 'var(--theme-surface)',
                    color: 'var(--theme-text-primary)',
                    fontSize: '0.8rem',
                    maxWidth: '120px'
                }}
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
