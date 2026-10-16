import type { ProjectSearchArtifact } from "./ProjectSceneDetailPanel";

export interface ProjectSearchItem {
    id: string;
    name: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    tags?: string[];
    last_activity?: string;
    entry_count?: number;
    pinned?: boolean;
    archived?: boolean;
    has_output?: boolean;
    source_urls?: string[];
    recent_artifacts?: ProjectSearchArtifact[];
}
