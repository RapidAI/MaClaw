import type { Colleague } from '../types';

export const colleagues: Colleague[] = [
  {
    id: 'office-iworker',
    name: 'Office iWorker',
    role: 'Document and communication partner',
    description: 'Handles notices, minutes, weekly reports, and structured written updates.',
    strengths: ['Notices', 'Minutes', 'Weekly reports', 'Email drafts'],
    tasks: ['Write a notice', 'Summarize a meeting', 'Prepare a weekly report', 'Draft an email'],
  },
  {
    id: 'data-iworker',
    name: 'Data iWorker',
    role: 'Data preparation partner',
    description: 'Cleans tables, summarizes datasets, and prepares analysis-ready notes.',
    strengths: ['Table cleanup', 'Data summary', 'Chart briefing'],
    tasks: ['Clean a table', 'Summarize data', 'Prepare a chart brief', 'Write analysis notes'],
  },
  {
    id: 'ops-iworker',
    name: 'Operations iWorker',
    role: 'Production and delivery partner',
    description: 'Prepares daily reports, handoff notes, and exception explanations.',
    strengths: ['Daily reports', 'Shift handoff', 'Exception summary'],
    tasks: ['Production daily report', 'Shift handoff note', 'Exception explanation', 'Escalation brief'],
  },
  {
    id: 'quality-iworker',
    name: 'Quality iWorker',
    role: 'Quality analysis partner',
    description: 'Classifies issues, explains causes, and prepares corrective-action drafts.',
    strengths: ['Issue classification', 'Cause analysis', 'Corrective actions'],
    tasks: ['Quality issue summary', 'Classify problems', 'Corrective-action draft', 'Root-cause analysis'],
  },
];
