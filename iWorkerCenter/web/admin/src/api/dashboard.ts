import { apiGet, apiPost } from './client';
import type { DashboardData, CenterStatus, CenterSettings, ExecutiveSkill, ExecutiveSkillResult } from '../types';

export function fetchDashboard(): Promise<DashboardData> {
  return apiGet<DashboardData>('/admin/executive/overview');
}

export function fetchExecutiveSkills(): Promise<{ skills: ExecutiveSkill[] }> {
  return apiGet<{ skills: ExecutiveSkill[] }>('/admin/executive/skills');
}

export function runExecutiveSkill(skillId: string): Promise<ExecutiveSkillResult> {
  return apiPost<ExecutiveSkillResult>('/admin/executive/skills/run', { skill_id: skillId });
}

export function recordManagementDecision(payload: {
  role_code: string;
  decision_type: 'review' | 'deferred';
  detail: string;
  display_time: string;
}): Promise<{ ok: boolean }> {
  return apiPost<{ ok: boolean }>('/admin/executive/management-decisions', payload);
}

export function confirmReturnToAutonomy(payload: {
  role_code: string;
  detail: string;
  display_time: string;
}): Promise<{ ok: boolean }> {
  return apiPost<{ ok: boolean }>('/admin/executive/autonomy-return', payload);
}

export function generateDepositionDrafts(payload: {
  role_code: string;
  action_title: string;
  detail: string;
}): Promise<{
  memory_id: string;
  capability_id: string;
  workflow_id?: string;
  memory_title: string;
  capability_name: string;
  workflow_name?: string;
}> {
  return apiPost('/admin/executive/deposition-drafts', payload);
}

export function publishDepositionRollout(payload: {
  role_code: string;
  workflow_id: string;
  detail: string;
}): Promise<{ ok: boolean }> {
  return apiPost('/admin/executive/deposition-rollout/publish', payload);
}

export function fetchCenterStatus(): Promise<CenterStatus> {
  return Promise.resolve({ status: 'unknown', provider_count: 0, config_path: '' } as CenterStatus);
}

export function fetchCenterSettings(): Promise<CenterSettings> {
  return Promise.resolve({ providers: [] } as CenterSettings);
}
