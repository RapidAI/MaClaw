import { apiGet, apiPost } from "./client";

export interface CollabTask {
  id: string;
  title: string;
  description: string;
  from_colleague_id: string;
  to_colleague_id: string;
  to_role_code: string;
  status: string;
  priority: number;
  result: string;
  created_at: string;
  updated_at: string;
}

export interface CollabEvent {
  id: string;
  task_id: string;
  event: string;
  actor_id: string;
  note: string;
  created_at: string;
}

export type RoutingStrategy = "least_loaded" | "primary_first";
export type ColleagueRuntimeState = "active" | "standby" | "unhealthy";

export interface CollaborationRoutingSettings {
  default_strategy: RoutingStrategy;
  role_strategies: Record<string, RoutingStrategy>;
  primary_colleague_by_role: Record<string, string>;
  runtime_state_by_colleague: Record<string, ColleagueRuntimeState>;
  last_heartbeat_by_colleague: Record<string, string>;
  heartbeat_timeout_seconds: number;
}

export interface CollaborationRoutingColleagueStatus {
  colleague_id: string;
  manual_state: ColleagueRuntimeState;
  effective_state: ColleagueRuntimeState;
  reason: string;
}

export interface CollaborationRoutingOverview {
  settings: CollaborationRoutingSettings;
  active_count: number;
  standby_count: number;
  unhealthy_count: number;
  status_by_colleague: Record<string, CollaborationRoutingColleagueStatus>;
}

export function listCollaborations(): Promise<CollabTask[]> {
  return apiGet<{ tasks: CollabTask[] }>("/admin/collaborations").then(
    (d) => d.tasks || [],
  );
}

export function getCollaborationEvents(taskId: string): Promise<CollabEvent[]> {
  return apiGet<{ events: CollabEvent[] }>(
    `/admin/collaborations/${taskId}/events`,
  ).then((d) => d.events || []);
}

export function getCollaborationRoutingSettings(): Promise<CollaborationRoutingOverview> {
  return apiGet<CollaborationRoutingOverview>("/admin/collaborations-settings");
}

export function saveCollaborationRoutingSettings(
  data: CollaborationRoutingSettings,
): Promise<{ status: string }> {
  return apiPost("/admin/collaborations-settings", data);
}

export function createCollaboration(data: {
  title: string;
  description?: string;
  from_colleague_id: string;
  to_colleague_id?: string;
  to_role_code?: string;
  priority?: number;
  source_type?: string;
  source_skill_id?: string;
  source_skill_title?: string;
  source_focus_title?: string;
  source_focus_role_code?: string;
}): Promise<CollabTask> {
  return apiPost("/admin/collaborations", data);
}

export function transitionCollaboration(
  taskId: string,
  action: "accept" | "start" | "complete" | "reject",
  body?: {
    actor_id?: string;
    result?: string;
    note?: string;
  },
): Promise<{ status: string }> {
  return apiPost(`/runtime/collaboration/${taskId}/${action}`, body || {});
}

export function executeRoleRoutingAction(data: {
  role_code: string;
  action: "promote_standby" | "prefer_primary" | "balance_load";
  actor_id?: string;
}): Promise<{ status: string; settings: CollaborationRoutingSettings }> {
  return apiPost("/admin/collaborations-settings/actions", data);
}
