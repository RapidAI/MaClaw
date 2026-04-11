import { apiGet, apiPut } from './client';

export interface IMConfig {
  feishu?: {
    app_id: string;
    app_secret: string;
    verification_token?: string;
    encrypt_key?: string;
    enabled?: boolean;
  };
  dingtalk?: {
    app_key: string;
    app_secret: string;
    robot_code?: string;
    enabled?: boolean;
  };
  wecom?: {
    corp_id: string;
    corp_secret?: string;
    secret?: string;
    agent_id: string;
    token?: string;
    aes_key?: string;
    enabled?: boolean;
  };
}

export function loadIMConfig(): Promise<IMConfig> {
  return apiGet('/admin/im-config');
}

export function saveIMConfig(config: IMConfig): Promise<void> {
  return apiPut('/admin/im-config', config);
}
