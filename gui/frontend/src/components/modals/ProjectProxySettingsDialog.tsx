import { SaveConfig } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type ProjectProxySettingsDialogProps = {
    config: main.AppConfig;
    selectedProjectForLaunch: string;
    setConfig: (config: main.AppConfig) => void;
    t: (key: string) => string;
    saveLabel: string;
    onClose: () => void;
};

export const ProjectProxySettingsDialog = ({
    config,
    selectedProjectForLaunch,
    setConfig,
    t,
    saveLabel,
    onClose,
}: ProjectProxySettingsDialogProps) => {
    const selectedProject = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
    const updateSelectedProject = (patch: Record<string, string>) => {
        if (!selectedProject) return;
        const newProjects = config.projects.map((p: any) =>
            p.id === selectedProject.id ? { ...p, ...patch } : p
        );
        setConfig(new main.AppConfig({ ...config, projects: newProjects }));
    };

    return (
        <div className="modal-overlay">
            <div className="modal-content" style={{ width: '540px', textAlign: 'left' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
                    <h3 style={{ margin: 0, color: '#6366f1' }}>
                        {t("proxySettings")}
                    </h3>
                    <button className="modal-close" onClick={onClose}>&times;</button>
                </div>

                {config?.default_proxy_host && (
                    <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#eef2ff', borderRadius: '6px', fontSize: '0.85rem' }}>
                        <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
                            <input
                                type="checkbox"
                                checked={!!selectedProject && !selectedProject.proxy_host}
                                onChange={(e) => {
                                    if (selectedProject && e.target.checked) {
                                        const newProjects = config.projects.map((p: any) =>
                                            p.id === selectedProject.id ? { ...p, proxy_host: '', proxy_port: '', proxy_username: '', proxy_password: '' } : p
                                        );
                                        const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                        setConfig(newConfig);
                                        SaveConfig(newConfig);
                                    }
                                }}
                                style={{ marginRight: '8px' }}
                            />
                            <span>{t("useDefaultProxy")} ({config.default_proxy_host}:{config.default_proxy_port})</span>
                        </label>
                    </div>
                )}

                <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                    <div style={{ flex: 1 }}>
                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyHost")}</label>
                        <input type="text" className="form-input" spellCheck={false}
                            placeholder={t("proxyHostPlaceholder")}
                            value={selectedProject?.proxy_host || ''}
                            onChange={(e) => updateSelectedProject({ proxy_host: e.target.value })}
                        />
                    </div>
                    <div style={{ width: '90px', flexShrink: 0 }}>
                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPort")}</label>
                        <input type="text" className="form-input" spellCheck={false}
                            placeholder={t("proxyPortPlaceholder")}
                            value={selectedProject?.proxy_port || ''}
                            onChange={(e) => updateSelectedProject({ proxy_port: e.target.value })}
                        />
                    </div>
                </div>

                <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                    <div style={{ flex: 1 }}>
                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyUsername")}</label>
                        <input type="text" className="form-input" spellCheck={false} autoComplete="off"
                            value={selectedProject?.proxy_username || ''}
                            onChange={(e) => updateSelectedProject({ proxy_username: e.target.value })}
                        />
                    </div>
                    <div style={{ flex: 1 }}>
                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPassword")}</label>
                        <input type="password" className="form-input" autoComplete="new-password"
                            value={selectedProject?.proxy_password || ''}
                            onChange={(e) => updateSelectedProject({ proxy_password: e.target.value })}
                        />
                    </div>
                </div>

                <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                    <button className="btn-secondary" onClick={onClose} style={{ padding: '8px 16px' }}>
                        {t("cancel")}
                    </button>
                    <button className="btn-primary" onClick={() => {
                        if (selectedProject && !selectedProject.use_proxy) {
                            const newProjects = config.projects.map((p: any) => p.id === selectedProject.id ? { ...p, use_proxy: true } : p);
                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                            setConfig(newConfig);
                            SaveConfig(newConfig);
                        } else {
                            SaveConfig(config);
                        }
                    }} style={{ padding: '8px 16px' }}>
                        {saveLabel}
                    </button>
                </div>
            </div>
        </div>
    );
};
