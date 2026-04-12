import { apiGet, apiPost } from './client';

export interface WorkflowDef {
  id: string;
  name: string;
  description: string;
  status: string;
  steps: unknown[];
  created_at: string;
}

export interface WorkflowInstance {
  id: string;
  workflow_id: string;
  status: string;
  created_at: string;
}

export function listWorkflows(): Promise<WorkflowDef[]> {
  return apiGet<{ workflows: WorkflowDef[] }>('/admin/workflows').then(d => d.workflows || []);
}

export function createWorkflow(data: { name: string; description?: string; steps?: unknown[] }): Promise<WorkflowDef> {
  return apiPost('/admin/workflows', data);
}

export function publishWorkflow(id: string): Promise<void> {
  return apiPost(`/admin/workflows/${id}/publish`);
}

export function listWorkflowInstances(): Promise<WorkflowInstance[]> {
  return apiGet<{ instances: WorkflowInstance[] }>('/admin/workflow-instances').then(d => d.instances || []);
}

export function startWorkflowInstance(data: { workflow_id: string; input?: unknown }): Promise<WorkflowInstance> {
  return apiPost('/admin/workflow-instances', data);
}
