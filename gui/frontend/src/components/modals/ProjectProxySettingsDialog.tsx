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
            <div className="modal-content project-proxy-modal">
                <div className="project-proxy-modal__header">
                    <h3>
                        {t("proxySettings")}
                    </h3>
                    <button className="modal-close" onClick={onClose}>&times;</button>
                </div>

                {config?.default_proxy_host && (
                    <div className="project-proxy-modal__default">
                        <label className="project-proxy-modal__check">
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
                            />
                            <span>{t("useDefaultProxy")} ({config.default_proxy_host}:{config.default_proxy_port})</span>
                        </label>
                    </div>
                )}

                <div className="project-proxy-modal__row project-proxy-modal__row--host">
                    <div className="project-proxy-modal__field">
                        <label className="form-label project-proxy-modal__label">{t("proxyHost")}</label>
                        <input type="text" className="form-input" spellCheck={false}
                            placeholder={t("proxyHostPlaceholder")}
                            value={selectedProject?.proxy_host || ''}
                            onChange={(e) => updateSelectedProject({ proxy_host: e.target.value })}
                        />
                    </div>
                    <div className="project-proxy-modal__field project-proxy-modal__field--port">
                        <label className="form-label project-proxy-modal__label">{t("proxyPort")}</label>
                        <input type="text" className="form-input" spellCheck={false}
                            placeholder={t("proxyPortPlaceholder")}
                            value={selectedProject?.proxy_port || ''}
                            onChange={(e) => updateSelectedProject({ proxy_port: e.target.value })}
                        />
                    </div>
                </div>

                <div className="project-proxy-modal__row">
                    <div className="project-proxy-modal__field">
                        <label className="form-label project-proxy-modal__label">{t("proxyUsername")}</label>
                        <input type="text" className="form-input" spellCheck={false} autoComplete="off"
                            value={selectedProject?.proxy_username || ''}
                            onChange={(e) => updateSelectedProject({ proxy_username: e.target.value })}
                        />
                    </div>
                    <div className="project-proxy-modal__field">
                        <label className="form-label project-proxy-modal__label">{t("proxyPassword")}</label>
                        <input type="password" className="form-input" autoComplete="new-password"
                            value={selectedProject?.proxy_password || ''}
                            onChange={(e) => updateSelectedProject({ proxy_password: e.target.value })}
                        />
                    </div>
                </div>

                <div className="project-proxy-modal__actions">
                    <button className="btn-secondary project-proxy-modal__button" onClick={onClose}>
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
                    }}>
                        {saveLabel}
                    </button>
                </div>
            </div>
        </div>
    );
};
