import { apiGet, apiPost } from './client';

export interface Memory {
  id: string;
  title: string;
  content: string;
  source: string;
  created_at: string;
}

export function listMemories(): Promise<Memory[]> {
  return apiGet<{ memories: Memory[] }>('/admin/memories').then(d => d.memories || []);
}

export function createMemory(data: { title: string; content: string; source?: string }): Promise<Memory> {
  return apiPost('/admin/memories', data);
}

export function runDedup(): Promise<{ removed: number; expired: number }> {
  return apiPost('/admin/memories/dedup');
}
