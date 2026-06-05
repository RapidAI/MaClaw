import { useRef, type Dispatch, type SetStateAction } from 'react';
import { SelectProjectDir, PatchConfigFields } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type ProjectManagerItemProps = {
    config: main.AppConfig;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
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
    const nameDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingNameRef = useRef<string>(project.name || '');

    const hasFlushedRef = useRef(false);

    const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const newName = e.target.value;
        pendingNameRef.current = newName;
        hasFlushedRef.current = false;

        // Immediately update UI state
        const newList = config.projects.map((p: any) => p.id === project.id ? { ...p, name: newName } : p);
        setConfig(new main.AppConfig({ ...config, projects: newList }));

        // Debounce the persist call to avoid saving on every keystroke
        if (nameDebounceRef.current) clearTimeout(nameDebounceRef.current);
        nameDebounceRef.current = setTimeout(() => {
            hasFlushedRef.current = true;
            PatchConfigFields({ projects: config.projects.map((p: any) => p.id === project.id ? { ...p, name: pendingNameRef.current } : p) })
                .catch((err) => console.error('Failed to save project name:', err));
        }, 500);
    };

    const flushNameSave = () => {
        if (nameDebounceRef.current) {
            clearTimeout(nameDebounceRef.current);
            nameDebounceRef.current = null;
        }
        if (hasFlushedRef.current) return;
        if (pendingNameRef.current === (project.name || '')) return;
        hasFlushedRef.current = true;
        PatchConfigFields({ projects: config.projects.map((p: any) => p.id === project.id ? { ...p, name: pendingNameRef.current } : p) })
            .catch((err) => console.error('Failed to save project name:', err));
    };

    const handleNameKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            flushNameSave();
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
            onBlur={flushNameSave}
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
                        const newConfig = new main.AppConfig({ ...config, projects: newList });
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
                        const newConfig = new main.AppConfig({ ...config, projects: newList });
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
