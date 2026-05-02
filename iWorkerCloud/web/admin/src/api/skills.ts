import { apiDelete, apiGet, apiPost, apiPut } from './client';

export interface CloudSkill {
  id: string;
  name: string;
  description: string;
  category?: string;
  version?: string;
  tags?: string[];
  risk_level?: string;
  status?: string;
  price?: number;
  author?: string;
  author_email?: string;
  source_center_id?: string;
  avg_rating?: number;
  download_count?: number;
  downloads?: number;
  package_format?: string;
  package_sha256?: string;
  package_size?: number;
  created_at?: string;
  updated_at?: string;
}

export interface SkillInput {
  id: string;
  name: string;
  description: string;
  category: string;
  version: string;
  tags: string[];
  risk_level: string;
  status: string;
  price?: number;
  author?: string;
  author_email?: string;
  package_format?: string;
  package_content_base64?: string;
}

export function listSkills(): Promise<CloudSkill[]> {
  return apiGet<{ skills: CloudSkill[] }>('/api/admin/skills').then(data => data.skills || []);
}

export function createSkill(input: SkillInput): Promise<CloudSkill> {
  return apiPost<CloudSkill>('/api/admin/skills', input);
}

export function updateSkill(id: string, input: SkillInput): Promise<CloudSkill> {
  return apiPut<CloudSkill>('/api/admin/skills/' + encodeURIComponent(id), input);
}

export function deleteSkill(id: string): Promise<void> {
  return apiDelete('/api/admin/skills/' + encodeURIComponent(id));
}
