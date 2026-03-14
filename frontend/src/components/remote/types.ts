import { main } from "../../../wailsjs/go/models";

export type RemoteToolName = "claude" | "codex" | "opencode" | "iflow" | "kilo" | "kode";

export interface ImportantEventView {
    event_id?: string;
    session_id?: string;
    machine_id?: string;
    type?: string;
    severity?: string;
    title?: string;
    summary?: string;
    count?: number;
    grouped?: boolean;
    related_file?: string;
    command?: string;
    created_at?: number;
}

export interface RemoteSessionSummaryView {
    session_id?: string;
    machine_id?: string;
    tool?: string;
    title?: string;
    source?: string;
    status?: string;
    severity?: string;
    waiting_for_user?: boolean;
    current_task?: string;
    progress_summary?: string;
    last_result?: string;
    suggested_action?: string;
    important_files?: string[];
    last_command?: string;
    updated_at?: number;
}

export interface SessionPreviewView {
    session_id?: string;
    output_seq?: number;
    preview_lines?: string[];
    updated_at?: number;
}

export interface RemoteSessionView {
    id: string;
    tool: string;
    title: string;
    launch_source?: string;
    project_path: string;
    workspace_path?: string;
    workspace_root?: string;
    workspace_mode?: string;
    workspace_is_git?: boolean;
    model_id?: string;
    status?: string;
    pid?: number;
    summary?: RemoteSessionSummaryView;
    preview?: SessionPreviewView;
    events?: ImportantEventView[];
}

export interface RemoteToolReadinessView {
    tool?: string;
    ready?: boolean;
    remote_enabled?: boolean;
    tool_installed?: boolean;
    model_configured?: boolean;
    project_path?: string;
    tool_path?: string;
    command_path?: string;
    hub_url?: string;
    pty_supported?: boolean;
    pty_message?: string;
    selected_model?: string;
    selected_model_id?: string;
    issues?: string[];
    warnings?: string[];
}

export interface RemoteToolLaunchProbeView {
    tool?: string;
    supported?: boolean;
    ready?: boolean;
    message?: string;
    command_path?: string;
    project_path?: string;
}

export interface RemoteSmokeReportView {
    tool?: string;
    phase?: string;
    success?: boolean;
    last_updated?: string;
    recommended_next?: string;
    activation?: {
        email?: string;
        machine_id?: string;
        sn?: string;
        activated?: boolean;
    };
    pty_probe?: {
        supported?: boolean;
        message?: string;
    };
    launch_probe?: {
        ready?: boolean;
        message?: string;
        command_path?: string;
        project_path?: string;
    };
    started_session?: {
        id?: string;
        pid?: number;
        status?: string;
    };
    hub_visibility?: {
        verified?: boolean;
        machine_visible?: boolean;
        session_visible?: boolean;
        host_online?: boolean;
        message?: string;
    };
}

export interface RemoteToolMetadataView {
    name: string;
    display_name: string;
    binary_name?: string;
    default_title?: string;
    uses_openai_compat?: boolean;
    requires_session_config?: boolean;
    supports_proxy?: boolean;
    visible?: boolean;
    installed?: boolean;
    can_start?: boolean;
    tool_path?: string;
    unavailable_reason?: string;
    readiness_hint?: string;
    smoke_hint?: string;
}

export interface RemoteSuggestedAction {
    label: string;
    description: string;
    action: "install" | "activate" | "configure" | "reconnect" | "readiness" | "start";
}

export interface RemoteActivationStatus {
    activated?: boolean;
    email?: string;
    machine_id?: string;
    sn?: string;
}

export interface RemoteConnectionStatus {
    connected?: boolean;
    hub_url?: string;
}

export type RemoteSettingsConfig = main.AppConfig | null;

/**
 * Canonical set of session statuses that indicate the session is no longer
 * running.  This MUST be kept in sync with:
 *   - hub/internal/session/service.go  → terminalStatuses
 *   - hub/web/dist/_pwa_syntax_check.js → sessionClosed array
 */
export const TERMINAL_SESSION_STATUSES: ReadonlySet<string> = new Set([
    "stopped",
    "finished",
    "failed",
    "killed",
    "exited",
    "closed",
    "done",
    "error",
    "completed",
    "terminated",
]);
