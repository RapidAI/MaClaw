/**
 * Wails event name constants.
 *
 * These constants define the event names used for communication between
 * the Go backend and the TypeScript frontend via Wails EventsEmit/EventsOn.
 *
 * IMPORTANT: When adding or renaming events, update the corresponding
 * backend constants in:
 *   gui/events.go
 *
 * This file exists to prevent event name mismatches (the root cause of #98:
 * backend emitted "task-list-changed" but frontend listened for
 * "project-index:changed"). Using constants makes mismatches visible in
 * code review rather than silent runtime bugs.
 */

/** Emitted when ProjectIndex is updated (new project or activity change). */
export const EVENT_PROJECT_INDEX_CHANGED = "project-index:changed";

/** Companion event emitted together with EVENT_PROJECT_INDEX_CHANGED. */
export const EVENT_TASKS_CHANGED = "tasks:changed";

/** Emitted when a task is removed/archived and matching project tabs should close. */
export const EVENT_PROJECT_TASK_CLOSED = "project-task:closed";

/** Emitted before task-close when the task was permanently deleted. */
export const EVENT_PROJECT_TASK_DELETED = "project-task:deleted";

/** Emitted when the background update checker (startup delay + periodic re-check) finds a newer application release. */
export const EVENT_APP_UPDATE_AVAILABLE = "app-update-available";

/** Skill usage stats updated after a run. */
export const EVENT_SKILL_USAGE_UPDATED = "skill:usage_updated";

/** Skill self-repair completed (steps updated or disabled). */
export const EVENT_SKILL_REPAIRED = "skill:repaired";

/** Skill optimizer applied bounded edits. */
export const EVENT_SKILL_OPTIMIZED = "skill:optimized";

/** Nudge promoter created a new auto-discovered skill. */
export const EVENT_SKILL_AUTO_DISCOVERED = "skill:auto_discovered";

/** Skill execution failed (evolution pipeline observation). */
export const EVENT_SKILL_EXECUTION_FAILED = "skill:execution_failed";

/** Skill indexes refreshed after mutation (repair/install). */
export const EVENT_SKILL_INDEX_REFRESHED = "skill:index_refreshed";

/** Computer Use local observe (SoM text + element summary for operator UI). */
export const EVENT_COMPUTER_USE_OBSERVE = "computer-use:observe";

/** Computer Use action audit (click/type/key/…). */
export const EVENT_COMPUTER_USE_ACTION = "computer-use:action";

/** Computer Use operator control state (pause/resume/stop). */
export const EVENT_COMPUTER_USE_CONTROL = "computer-use:control";

/** Computer Use background warmup / self-check finished. */
export const EVENT_COMPUTER_USE_WARMUP = "computer-use:warmup";

/** Computer Use observe/pipeline error with operator guidance. */
export const EVENT_COMPUTER_USE_ERROR = "computer-use:error";
/** Log lifecycle: prune / delete / batch-delete (refresh file lists). */
export const EVENT_COMPUTER_USE_LOGS = "computer-use:logs";

/** Survey list/results should refresh after IM submit (or local mutation). Payload: { survey_id, event? }. */
export const EVENT_SURVEY_UPDATED = "survey-updated";

/** App config patched/saved (backend EventsEmit after PatchConfigFields). */
export const EVENT_CONFIG_CHANGED = "config-changed";

/** Alternate config broadcast used by some trays/legacy paths. */
export const EVENT_CONFIG_UPDATED = "config-updated";

/** Same-window optimistic config patch (CustomEvent on window). */
export const EVENT_MACLAW_CONFIG_CHANGED = "maclaw-config-changed";

/**
 * Same-window request to open/create a pure coding task
 * (local coding_dev or remote remote_coding_dev).
 * Dispatched from AI welcome cards; handled by SidebarTaskManagement.
 *
 * detail: {
 *   mode: 'coding_dev' | 'remote_coding_dev';
 *   name?: string;
 *   workingDir?: string;           // local coding workdir
 *   remote?: { host, port, user, password, workDir };
 *   autoCreate?: boolean;          // when true + valid env, create without dialog
 * }
 */
export const EVENT_OPEN_CREATE_CODING_TASK = "ai-open-create-coding-task";

/** Payload shape for EVENT_OPEN_CREATE_CODING_TASK. */
export type OpenCreateCodingTaskDetail = {
    mode?: "coding_dev" | "remote_coding_dev";
    name?: string;
    workingDir?: string;
    remote?: {
        host: string;
        port: number;
        user: string;
        password: string;
        workDir: string;
    };
    /** When true and required env is present, create the task immediately. */
    autoCreate?: boolean;
};
