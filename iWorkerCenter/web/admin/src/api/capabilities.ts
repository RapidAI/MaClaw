import { apiGet, apiPost, apiPut } from './client';

export interface Capability {
  id: string;
  name: string;
  description: string;
  status: string;
  created_at: string;
}

export function listCapabilities(): Promise<Capability[]> {
  return apiGet<{ capabilities: Capability[] }>('/admin/capabilities').then(d => d.capabilities || []);
}

export function createCapability(data: { name: string; description?: string }): Promise<Capability> {
  return apiPost('/admin/capabilities', data);
}

export function approveCapability(id: string): Promise<void> {
  return apiPost(`/admin/capabilities/${id}/approve`);
}

export function rejectCapability(id: string): Promise<void> {
  return apiPost(`/admin/capabilities/${id}/reject`);
}

export function bindCapability(capabilityID: string, colleagueID: string): Promise<void> {
  return apiPost(`/admin/capabilities/${capabilityID}/bind`, { colleague_id: colleagueID });
}


export interface MCPServer {
  id: string;
  name: string;
  description: string;
  server_type: 'http' | 'sse' | 'stdio' | string;
  endpoint: string;
  command?: string;
  args: string[];
  env_keys: string[];
  department_id: string;
  risk_level: string;
  status: 'enabled' | 'disabled' | string;
  installed_at: string;
  created_at?: string;
  updated_at?: string;
}

export type MCPServerInput = {
  name: string;
  description?: string;
  server_type: 'http' | 'sse' | 'stdio';
  endpoint?: string;
  command?: string;
  args?: string[];
  env_keys?: string[];
  department_id?: string;
  risk_level?: string;
  status?: 'enabled' | 'disabled';
};

export function listMCPServers(): Promise<MCPServer[]> {
  return apiGet<{ mcp_servers: MCPServer[] }>('/admin/mcp-servers').then(d => d.mcp_servers || []);
}

export function createMCPServer(data: MCPServerInput): Promise<MCPServer> {
  return apiPost<MCPServer>('/admin/mcp-servers', data);
}

export function updateMCPServer(id: string, data: MCPServerInput): Promise<MCPServer> {
  return apiPut<MCPServer>('/admin/mcp-servers/' + id, data);
}
