import { apiGet } from './client';
import type { IWorkerAgentInstance } from '../types';

export function listIWorkerAgentInstances(workerId?: string): Promise<IWorkerAgentInstance[]> {
  const query = workerId ? `?worker_id=${encodeURIComponent(workerId)}` : "";
  return apiGet<{ instances: IWorkerAgentInstance[] }>(`/admin/iworker/instances${query}`).then((d) => d.instances || []);
}
