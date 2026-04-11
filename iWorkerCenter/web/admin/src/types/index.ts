export type CenterTab =
  | 'overview'
  | 'employees'
  | 'communications'
  | 'workflows'
  | 'knowledge'
  | 'packages'
  | 'models'
  | 'compute'
  | 'security'
  | 'delivery'
  | 'usage'
  | 'im'
  | 'auth'
  | 'settings';

export interface Metric {
  label: string;
  value: string;
  hint: string;
}

export interface DashboardItem {
  title: string;
  description: string;
  status: string;
}

export interface DashboardData {
  metrics: Metric[];
  alerts: DashboardItem[];
  recent: DashboardItem[];
}

export interface CenterStatus {
  status: string;
  provider_count: number;
  config_path: string;
}

export interface CenterSettings {
  providers: unknown[];
  work_type_keywords?: Record<string, string[]>;
  work_type_tier?: Record<string, string>;
  role_provider_boost?: Record<string, string[]>;
}
