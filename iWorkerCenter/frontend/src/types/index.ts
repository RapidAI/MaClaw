export type CenterTab =
  | 'overview'
  | 'bootstrap'
  | 'employees'
  | 'communications'
  | 'workflows'
  | 'knowledge'
  | 'packages'
  | 'models'
  | 'cloud'
  | 'security'
  | 'delivery'
  | 'usage'
  | 'im'
  | 'auth'
  | 'settings';

export type Metric = {
  label: string;
  value: string;
  hint: string;
};

export type DashboardItem = {
  title: string;
  description: string;
  status: string;
};
