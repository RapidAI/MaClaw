import type { Metric, DashboardItem } from '../types';

export const metrics: Metric[] = [
  { label: '数字员工总数', value: '28', hint: '其中 21 个处于启用状态' },
  { label: '今日协作次数', value: '146', hint: '覆盖办公、生产、质量三类事务' },
  { label: '当前生效规则数', value: '32', hint: '包含安全、下发与模型路由规则' },
];

export const alerts: DashboardItem[] = [
  { title: '风险能力包待审核', description: '2 个能力包等待安全审核。', status: '待处理' },
  { title: '模型不可用告警', description: '一个备用模型链路检查失败。', status: '需关注' },
];

export const recentItems: DashboardItem[] = [
  { title: '最近活跃数字员工', description: '办公室 iWorker、运营 iWorker、数据 iWorker 在最近 1 小时内有任务处理记录。', status: '活跃' },
  { title: '最近新增能力包', description: '新增“周报汇总”和“异常归档”两个能力包。', status: '新增' },
  { title: '最近规则下发', description: '安全规则已下发到 18 个 iWorker 客户端。', status: '成功' },
];
