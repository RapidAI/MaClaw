import type { TaskItem } from '../types';

export const quickTasks = ['写通知', '会议纪要', '周报总结', '整理表格', '异常上报', '生产日报'];

export const recentTasks: TaskItem[] = [
  {
    id: 'task-101',
    title: '整理今日生产异常',
    owner: '老陈',
    status: '处理中',
    updatedAt: '今天 15:20',
    description: '汇总产线异常并生成汇报摘要',
  },
  {
    id: 'task-102',
    title: '周会纪要整理',
    owner: '小迪',
    status: '已完成',
    updatedAt: '今天 11:40',
    description: '提炼会议结论和待办事项',
  },
  {
    id: 'task-103',
    title: '质检问题归类',
    owner: '小周',
    status: '待确认',
    updatedAt: '昨天 18:05',
    description: '按原因和影响范围整理质量问题',
  },
];
