export type CenterTab =
  | 'overview'
  | 'employees'
  | 'communications'
  | 'workflows'
  | 'knowledge'
  | 'packages'
  | 'models'
  | 'security'
  | 'delivery'
  | 'usage';

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
