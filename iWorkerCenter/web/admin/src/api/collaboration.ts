import { apiGet, apiPost } from './client';

export interface CollabTask {
  id: string;
  title: string;
  from_colleague: string;
  to_colleague: string;
  status: string;
  created_at: string;
}

export function listCollaborations(): Promise<CollabTask[]> {
  return apiGet<{ tasks: CollabTask[] }>('/admin/collaborations').then(d => d.tasks || []);
}

export function createCollaboration(data: { title: string; from_colleague: string; to_colleague: string }): Promise<CollabTask> {
  return apiPost('/admin/collaborations', data);
}
