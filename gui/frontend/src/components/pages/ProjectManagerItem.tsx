import { PatchConfigFields, SelectProjectDir } from '../../../wailsjs/go/main/App';
import { type Dispatch, type SetStateAction, useRef } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';

type ProjectManagerItemProps = {
    config: corelib.AppConfig;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    t: (key: string) => string;
    project: any;
    selectedProjectForLaunch: string;
    setSelectedProjectForLaunch: Dispatch<SetStateAction<string>>;
};

export const ProjectManagerItem = ({
    config,
    setConfig,
    t,
    project,
    selectedProjectForLaunch,
    setSelectedProjectForLaunch,
}: ProjectManagerItemProps) => {
    // The input has a clear "commit" signal: blur or Enter.
    // No debounce — it only introduces stale-closure and IME race conditions
    // for zero user-visible benefit (the input is short-lived and commit-on-blur).
    const dirtyRef = useRef(false);

    const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const newName = e.target.value;
        dirtyRef.current = true;
        const newList = config.projects.map((p: any) => p.id === project.id ? { ...p, name: newName } : p);
        setConfig(new corelib.AppConfig({ ...config, projects: newList }));
    };

    const commitName = () => {
        if (!dirtyRef.current) return;
        dirtyRef.current = false;
        // config.projects already contains the up-to-date name (set by handleNameChange).
        // Persist the current state directly — no transformation needed.
        PatchConfigFields({ projects: config.projects })
            .catch((err) => console.error('Failed to save project name:', err));
    };

    const handleNameKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            commitName();
            (e.target as HTMLInputElement).blur();
        }
    };

    return (
    <div key={project.id} className="project-manager-item">
        <input
            type="text"
            className="form-input"
            data-field="project-item-name"
            data-id={project.id}
            value={project.name || ''}
            onChange={handleNameChange}
            onKeyDown={handleNameKeyDown}
            onBlur={commitName}
            placeholder="Project"
            style={{ fontWeight: 'bold', border: 'none', padding: 0, fontSize: '0.9rem', width: '112px', flexShrink: 0, lineHeight: 1.1 }}
            spellCheck={false}
            autoComplete="off"
        />
        <div style={{ flex: 1, fontSize: '0.78rem', color: 'var(--theme-text-muted)', backgroundColor: 'var(--theme-surface-muted)', padding: '3px 6px', borderRadius: '4px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', lineHeight: 1.1 }}>
            {project.path}
        </div>

        <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexShrink: 0 }}>
            <button className="btn-link" onClick={() => {
                SelectProjectDir().then(dir => {
                    if (dir) {
                        const newList = config.projects.map((p: any) => p.id === project.id ? { ...p, path: dir } : p);
                        const newConfig = new corelib.AppConfig({ ...config, projects: newList });
                        setConfig(newConfig);
                        PatchConfigFields({ projects: newList }).catch((err) => console.error('Failed to save project path:', err));
                    }
                });
            }}>{t('change')}</button>

            <button
                style={{ color: 'var(--theme-danger)', background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.85rem' }}
                onClick={() => {
                    if (config.projects.length > 1) {
                        const newList = config.projects.filter((p: any) => p.id !== project.id);
                        const newConfig = new corelib.AppConfig({ ...config, projects: newList });
                        if (config.current_project === project.id) newConfig.current_project = newList[0].id;
                        if (selectedProjectForLaunch === project.id) setSelectedProjectForLaunch(newConfig.current_project);
                        setConfig(newConfig);
                        PatchConfigFields({ projects: newList, current_project: newConfig.current_project }).catch((err) => console.error('Failed to delete project:', err));
                    }
                }}
            >
                {t('delete')}
            </button>
        </div>
    </div>
    );
};
