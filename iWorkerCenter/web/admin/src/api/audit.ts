import { apiGet } from "./client";

export interface AuditLog {
  id: string;
  request_id: string;
  provider_id: string;
  model: string;
  work_type: string;
  cost_tier: string;
  status: string;
  latency_ms: number;
  input_tokens: number;
  summary: string;
  error_msg: string;
  created_at: string;
}

export function listAuditLogs(limit = 20): Promise<AuditLog[]> {
  return apiGet<{ logs: AuditLog[] }>(`/admin/audit/logs?limit=${limit}`).then(
    (d) => d.logs || [],
  );
}
