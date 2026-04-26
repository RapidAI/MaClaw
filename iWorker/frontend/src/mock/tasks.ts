import type { TaskItem } from '../types';

export const quickTasks = [
  'Customer follow-up brief',
  'Meeting decision summary',
  'Weekly operating report',
  'Data table cleanup',
  'Exception explanation',
  'Production daily report',
];

export const recentTasks: TaskItem[] = [
  {
    id: 'task-101',
    title: 'Summarize production exception',
    owner: 'Operations iWorker',
    status: 'In progress',
    updatedAt: 'Today 15:20',
    description: 'Collect production-line variance and produce an operating brief.',
  },
  {
    id: 'task-102',
    title: 'Prepare weekly meeting minutes',
    owner: 'Office iWorker',
    status: 'Done',
    updatedAt: 'Today 11:40',
    description: 'Extract decisions, blockers, and follow-up owners from the meeting record.',
  },
  {
    id: 'task-103',
    title: 'Classify quality issues',
    owner: 'Quality iWorker',
    status: 'Waiting review',
    updatedAt: 'Yesterday 18:05',
    description: 'Group quality issues by root cause and business impact.',
  },
];
