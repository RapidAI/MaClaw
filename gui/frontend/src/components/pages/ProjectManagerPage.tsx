import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { ProjectManagerItem } from './ProjectManagerItem';

type ProjectSortMode = 'default' | 'name-asc' | 'name-desc' | 'path-asc' | 'path-desc';

interface ProjectManagerPageProps {
    config: main.AppConfig;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    t: (key: string) => string;
    projectSearchKeyword: string;
    setProjectSearchKeyword: Dispatch<SetStateAction<string>>;
    projectSortMode: ProjectSortMode;
    setProjectSortMode: Dispatch<SetStateAction<ProjectSortMode>>;
    filteredAndSortedProjects: any[];
    pagedProjects: any[];
    projectPageStartIndex: number;
    projectPageSize: number;
    safeProjectCurrentPage: number;
    totalProjectPages: number;
    setProjectCurrentPage: Dispatch<SetStateAction<number>>;
    selectedProjectForLaunch: string;
    setSelectedProjectForLaunch: Dispatch<SetStateAction<string>>;
}

export const ProjectManagerPage = ({
    config,
    setConfig,
    t,
    projectSearchKeyword,
    setProjectSearchKeyword,
    projectSortMode,
    setProjectSortMode,
    filteredAndSortedProjects,
    pagedProjects,
    projectPageStartIndex,
    projectPageSize,
    safeProjectCurrentPage,
    totalProjectPages,
    setProjectCurrentPage,
    selectedProjectForLaunch,
    setSelectedProjectForLaunch,
}: ProjectManagerPageProps) => (
                        <div className="project-manager-panel">
                            <div className="project-manager-toolbar">
                                <input
                                    type="text"
                                    className="form-input"
                                    value={projectSearchKeyword}
                                    onChange={(e) => setProjectSearchKeyword(e.target.value)}
                                    placeholder={t("projectSearchPlaceholder")}
                                    spellCheck={false}
                                    autoComplete="off"
                                />
                                <select
                                    className="form-input"
                                    value={projectSortMode}
                                    onChange={(e) => setProjectSortMode(e.target.value as 'default' | 'name-asc' | 'name-desc' | 'path-asc' | 'path-desc')}
                                >
                                    <option value="default">{t("projectSortDefault")}</option>
                                    <option value="name-asc">{t("projectSortNameAsc")}</option>
                                    <option value="name-desc">{t("projectSortNameDesc")}</option>
                                    <option value="path-asc">{t("projectSortPathAsc")}</option>
                                    <option value="path-desc">{t("projectSortPathDesc")}</option>
                                </select>
                            </div>

                            <div className="project-manager-summary">
                                {filteredAndSortedProjects.length > 0 ? (
                                    <span>
                                        {t("projectShowing")} {projectPageStartIndex + 1}-{Math.min(projectPageStartIndex + projectPageSize, filteredAndSortedProjects.length)} / {filteredAndSortedProjects.length} {t("projectTotal")}
                                    </span>
                                ) : (
                                    <span>{t("projectNoResults")}</span>
                                )}
                            </div>

                            <div className="project-manager-list elegant-scrollbar">
                                {pagedProjects.map((proj: any) => (
                                    <ProjectManagerItem
                                        key={proj.id}
                                        config={config}
                                        setConfig={setConfig}
                                        t={t}
                                        project={proj}
                                        selectedProjectForLaunch={selectedProjectForLaunch}
                                        setSelectedProjectForLaunch={setSelectedProjectForLaunch}
                                    />
                                ))}
                            </div>

                            {filteredAndSortedProjects.length > 0 && (
                                <div className="project-manager-pagination">
                                    <button
                                        className="btn-link"
                                        onClick={() => setProjectCurrentPage(Math.max(1, safeProjectCurrentPage - 1))}
                                        disabled={safeProjectCurrentPage <= 1}
                                    >
                                        {t("prevPage")}
                                    </button>
                                    <span>{safeProjectCurrentPage} / {totalProjectPages}</span>
                                    <button
                                        className="btn-link"
                                        onClick={() => setProjectCurrentPage(Math.min(totalProjectPages, safeProjectCurrentPage + 1))}
                                        disabled={safeProjectCurrentPage >= totalProjectPages}
                                    >
                                        {t("nextPage")}
                                    </button>
                                </div>
                            )}
                        </div>
);
