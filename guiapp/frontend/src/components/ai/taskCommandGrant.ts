/** Project path of the pure-coding tab on screen, or "" when that tab is not a coding task. */
export function visibleCodingProject(tab: { type?: string; agentMode?: string; projectPath?: string } | null | undefined): string {
    if (!tab || tab.type !== "project") return "";
    if (tab.agentMode !== "coding_dev" && tab.agentMode !== "remote_coding_dev") return "";
    return tab.projectPath || "";
}

/**
 * 「以后允许」 belongs to the coding tab that was visible when the dialog opened.
 * The approval event's project_path is the execution directory, so it is not the tab identity.
 */
export function commandGrantAppliesToVisibleTab(grantedOnProject: string, visibleProject: string): boolean {
    return grantedOnProject !== "" && grantedOnProject === visibleProject;
}
