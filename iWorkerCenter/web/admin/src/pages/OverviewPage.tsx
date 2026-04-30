import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { listAuditLogs } from '../api/audit';
import type { AuditLog } from '../api/audit';
import { createCollaboration, getCollaborationRoutingSettings, listCollaborations, transitionCollaboration } from '../api/collaboration';
import type { CollaborationRoutingOverview } from '../api/collaboration';
import { listColleagues } from '../api/colleagues';
import type { Colleague } from '../api/colleagues';
import { confirmReturnToAutonomy, fetchCenterStatus, fetchDashboard, fetchExecutiveSkills, generateDepositionDrafts, publishDepositionRollout, recordManagementDecision, runExecutiveSkill } from '../api/dashboard';
import { listRoles } from '../api/roles';
import type { Role } from '../api/roles';
import { listCapabilities } from '../api/capabilities';
import { listMemories } from '../api/memories';
import type { AssetNavigationTarget, CenterStatus, CenterTab, CommunicationsNavigationTarget, Metric, DashboardItem, ExecutiveAction, ExecutiveBoardFocus, ExecutiveBoardHistoryItem, ExecutiveBriefing, ExecutiveSkill, ExecutiveSkillResult, OverviewNavigationTarget } from '../types';

type BoardSignal = {
  title: string;
  priority?: number;
  tone: 'ok' | 'info' | 'warn';
  summary: string;
  detail: string;
  navigationTarget?: CommunicationsNavigationTarget;
};

type BoardHistoryItem = ExecutiveBoardHistoryItem & {
  id: string;
  title: string;
  detail: string;
  timestamp: string;
  tone: 'ok' | 'info' | 'warn';
  navigationTarget?: CommunicationsNavigationTarget;
  detailLines?: string[];
  isCluster?: boolean;
  clusterSkillTitle?: string;
  clusterFocusTitle?: string;
  clusterTaskTitle?: string;
  clusterRoleCode?: string;
  clusterExecutionStatus?: string;
  clusterExecutionResult?: string;
};

type BoardRoleItem = {
  roleCode: string;
  roleName: string;
  risk: 'stable' | 'watch' | 'critical';
  active: number;
  standby: number;
  unhealthy: number;
  openTaskCount: number;
  impactScore: number;
};

type RoleExecutionFeedback = {
  tone: 'ok' | 'info' | 'warn';
  message: string;
};

type ManagementDecisionRecord = {
  type: 'review' | 'deferred' | 'autonomy_return';
  detail: string;
  recordedAt: string;
  displayTime: string;
};
type OperatingModeCard = {
  id: string;
  eyebrow: string;
  title: string;
  tone: 'ok' | 'info' | 'warn';
  summary: string;
  detail: string;
  statLine: string[];
  primaryActionLabel?: string;
  secondaryActionLabel?: string;
  onPrimaryAction?: () => void;
  onSecondaryAction?: () => void;
  focusSection?: 'briefing' | 'coordination' | 'actions';
};

type InstitutionalizationProgressState = {
  phase: 'memory_review' | 'package_approval' | 'workflow_publish' | 'policy_monitoring' | 'inactive';
  tone: 'ok' | 'info' | 'warn';
  title: string;
  summary: string;
  detail: string;
  roleCode?: string;
  roleLabel?: string;
  statLine: string[];
  primaryActionLabel?: string;
  secondaryActionLabel?: string;
  onPrimaryAction?: () => void;
  onSecondaryAction?: () => void;
};

type ExecutiveAuditCluster = {
  id: string;
  skillId: string;
  skillTitle: string;
  focusTitle: string;
  taskId: string;
  taskTitle: string;
  roleCode: string;
  timestamp: string;
  tone: 'ok' | 'info' | 'warn';
  hasSkill: boolean;
  hasTask: boolean;
};

const extractAuditField = (text: string, field: string) => {
  const pattern = new RegExp(`${field}:\s*([^|]+)`, 'i');
  return text.match(pattern)?.[1]?.trim() || '';
};

const mergeTone = (current: 'ok' | 'info' | 'warn', next: 'ok' | 'info' | 'warn') => {
  if (current === 'warn' || next === 'warn') {
    return 'warn';
  }
  if (current === 'info' || next === 'info') {
    return 'info';
  }
  return 'ok';
};


const summarizeManagementDecisionState = (auditLogs: AuditLog[]) => {
  const latestByRole: Record<string, ManagementDecisionRecord> = {};

  auditLogs
    .filter((item) => item.work_type === 'management_decision' || item.work_type === 'management_autonomy_return')
    .forEach((item) => {
      const roleCode = extractAuditField(item.error_msg || '', 'role_code');
      const detail = extractAuditField(item.error_msg || '', 'detail');
      const decisionType = extractAuditField(item.error_msg || '', 'decision_type');
      const displayTime = extractAuditField(item.error_msg || '', 'display_time') || item.created_at;
      if (!roleCode || !detail || (decisionType !== 'review' && decisionType !== 'deferred' && decisionType !== 'autonomy_return')) {
        return;
      }
      const record: ManagementDecisionRecord = {
        type: decisionType,
        detail,
        recordedAt: item.created_at,
        displayTime,
      };
      const current = latestByRole[roleCode];
      if (!current || new Date(current.recordedAt).getTime() < new Date(record.recordedAt).getTime()) {
        latestByRole[roleCode] = record;
      }
    });

  return Object.entries(latestByRole).reduce((state, [roleCode, record]) => {
    if (record.type === 'review') {
      state.review[roleCode] = record;
    } else if (record.type === 'deferred') {
      state.deferred[roleCode] = record;
    }
    return state;
  }, {
    deferred: {} as Record<string, ManagementDecisionRecord>,
    review: {} as Record<string, ManagementDecisionRecord>,
  });
};

const decisionPriorityScore = (status: string) => {
  switch (status) {
    case 'rejected':
      return 0;
    case 'pending':
    case 'accepted':
      return 1;
    case 'in_progress':
      return 2;
    case 'done':
      return 3;
    default:
      return 4;
  }
};

const pickPriorityDecisionCluster = (history: BoardHistoryItem[]) => history
  .filter((item) => item.isCluster)
  .sort((a, b) => {
    const priorityDiff = decisionPriorityScore(a.clusterExecutionStatus || '') - decisionPriorityScore(b.clusterExecutionStatus || '');
    if (priorityDiff !== 0) {
      return priorityDiff;
    }
    return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
  })[0];

const buildClusteredExecutiveHistory = (
  auditLogs: AuditLog[],
  tasks: Array<{ id: string; status: string; result?: string }>,
): BoardHistoryItem[] => {
  const clusters = new Map<string, ExecutiveAuditCluster>();

  auditLogs
    .filter((item) => item.work_type === 'executive_skill' || item.work_type === 'executive_action_task')
    .forEach((item) => {
      const detail = item.error_msg || '';
      const skillId = extractAuditField(detail, 'skill_id') || item.request_id.replace(/^executive-skill-/, '').trim() || item.id;
      const roleCode = extractAuditField(detail, 'role_code');
      const focusTitle = extractAuditField(detail, 'focus');
      const taskId = extractAuditField(detail, 'task_id');
      const taskTitle = extractAuditField(detail, 'task');
      const derivedSkillTitle = item.summary
        .replace(/^Executive skill\s*/i, '')
        .replace(/\s*reviewed$/i, '')
        .replace(/^Task created from executive skill\s*/i, '')
        .trim();
      const tone = item.status === 'ok' ? 'ok' : 'warn';
      const current = clusters.get(skillId) || {
        id: `executive-cluster-${skillId}`,
        skillId,
        skillTitle: derivedSkillTitle,
        focusTitle: '',
        taskId: '',
        taskTitle: '',
        roleCode: '',
        timestamp: item.created_at,
        tone,
        hasSkill: false,
        hasTask: false,
      };

      current.skillTitle = current.skillTitle || derivedSkillTitle;
      current.focusTitle = current.focusTitle || focusTitle;
      current.taskId = current.taskId || taskId;
      current.taskTitle = current.taskTitle || taskTitle;
      current.roleCode = current.roleCode || roleCode;
      current.tone = mergeTone(current.tone, tone);
      if (new Date(item.created_at).getTime() > new Date(current.timestamp).getTime()) {
        current.timestamp = item.created_at;
      }
      if (item.work_type === 'executive_skill') {
        current.hasSkill = true;
      }
      if (item.work_type === 'executive_action_task') {
        current.hasTask = true;
      }
      clusters.set(skillId, current);
    });

  return Array.from(clusters.values()).map((cluster) => {
    const linkedTask = cluster.taskId
      ? tasks.find((task) => task.id === cluster.taskId)
      : null;
    const executionLine = linkedTask
      ? `Execution: ${linkedTask.status}${linkedTask.result ? ` | Result: ${linkedTask.result}` : ''}`
      : cluster.hasTask
        ? 'Execution: Task exists but current runtime status is not available in this view'
        : 'Execution: No task has been created from this decision yet';
    const tone = linkedTask?.status === 'rejected'
      ? 'warn'
      : linkedTask?.status === 'done'
        ? 'ok'
        : linkedTask?.status === 'in_progress' || linkedTask?.status === 'accepted' || linkedTask?.status === 'pending'
          ? 'info'
          : cluster.hasTask && cluster.tone === 'ok'
            ? 'ok'
            : cluster.tone;

    return {
      id: cluster.id,
      title: cluster.hasTask
        ? `${cluster.skillTitle || 'Executive skill'} dispatched an action`
        : `${cluster.skillTitle || 'Executive skill'} reviewed`,
      detail: cluster.hasTask
        ? `${cluster.focusTitle || 'An operating focus'} has already been converted into task ${cluster.taskTitle || 'execution'}.`
        : `${cluster.focusTitle || 'An operating focus'} was raised for management follow-through.`,
      detailLines: [
        `Skill: ${cluster.skillTitle || 'Executive skill'}`,
        `Focus: ${cluster.focusTitle || 'No explicit focus captured'}`,
        cluster.hasTask
          ? `Task: ${cluster.taskTitle || 'Execution handoff created'}`
          : 'Task: No organizational handoff has been created from this decision yet',
        `Role: ${cluster.roleCode || 'No direct role attached'}`,
        executionLine,
      ],
      timestamp: cluster.timestamp,
      tone,
      navigationTarget: cluster.taskId || cluster.roleCode
        ? {
          task_id: cluster.taskId || undefined,
          role_code: cluster.roleCode || undefined,
          source: cluster.hasTask ? 'skill_task_cluster' : 'skill_history',
        }
        : {
          source: cluster.hasTask ? 'skill_task_cluster' : 'skill_history',
        },
      isCluster: true,
      clusterSkillTitle: cluster.skillTitle || 'Executive skill',
      clusterFocusTitle: cluster.focusTitle || '',
      clusterTaskTitle: cluster.taskTitle || '',
      clusterRoleCode: cluster.roleCode || '',
      clusterExecutionStatus: linkedTask?.status || '',
      clusterExecutionResult: linkedTask?.result || '',
    };
  });
};

const pickLatestManagementDecision = (history: BoardHistoryItem[]) => history
  .find((item) => item.id.startsWith('management-'));

const historyRoleCode = (item: BoardHistoryItem | undefined) => {
  if (!item) {
    return '';
  }
  if (item.navigationTarget?.role_code) {
    return item.navigationTarget.role_code;
  }
  const roleLine = item.detailLines?.find((line) => line.startsWith('Role: '));
  return roleLine?.replace(/^Role:\s*/, '').trim() || '';
};

const managementDecisionType = (item: BoardHistoryItem | undefined) => {
  if (!item) {
    return '';
  }
  const typeLine = item.detailLines?.find((line) => line.startsWith('Decision type: '));
  return typeLine?.replace(/^Decision type:\s*/, '').trim() || '';
};

const historyExecutionStatus = (item: BoardHistoryItem | undefined) => {
  if (!item) {
    return '';
  }
  const statusLine = item.detailLines?.find((line) => line.startsWith('Execution status: '));
  return statusLine?.replace(/^Execution status:\s*/, '').trim() || item.clusterExecutionStatus || '';
};

const pickLatestAutonomyReturn = (history: BoardHistoryItem[]) => history
  .find((item) => item.id.startsWith('management-') && managementDecisionType(item) === 'autonomy_return');

const pickLatestStandardPublished = (history: BoardHistoryItem[]) => history
  .find((item) => item.id.startsWith('standard-'));

const pickLatestCapabilityApproved = (history: BoardHistoryItem[]) => history
  .find((item) => item.id.startsWith('capability-'));

const depositionDraftField = (item: BoardHistoryItem | undefined, prefix: string) => item?.detailLines?.find((line) => line.startsWith(prefix))?.replace(prefix, '').trim() || '';

const assetNavigationTargetForTab = (
  tab: 'knowledge' | 'packages' | 'workflows',
  item: ExecutiveAction,
  latestDraft: BoardHistoryItem | undefined,
): AssetNavigationTarget => {
  const roleCode = item.owner_role_code || historyRoleCode(latestDraft);
  const roleLabel = item.owner_role_label || item.owner || roleCode.toUpperCase();
  const draftName = tab === 'knowledge'
    ? `${roleLabel} recovery playbook`
    : tab === 'packages'
      ? `${roleLabel} recovery handling`
      : `${roleLabel} recovery deposition loop`;
  const draftID = tab === 'knowledge'
    ? depositionDraftField(latestDraft, 'Memory draft: ')
    : tab === 'packages'
      ? depositionDraftField(latestDraft, 'Capability draft: ')
      : depositionDraftField(latestDraft, 'Workflow draft: ');
  return {
    role_code: roleCode,
    role_label: roleLabel,
    draft_id: draftID && draftID !== 'not created' ? draftID : undefined,
    draft_name: draftName,
    source: 'overview_recovery_deposition',
  };
};

const assetNavigationTargetForRole = (
  tab: 'knowledge' | 'packages' | 'workflows',
  roleCode: string,
  roleLabel: string,
  latestDraft: BoardHistoryItem | undefined,
  source: string,
): AssetNavigationTarget => {
  const draftName = tab === 'knowledge'
    ? `${roleLabel} recovery playbook`
    : tab === 'packages'
      ? `${roleLabel} recovery handling`
      : `${roleLabel} recovery deposition loop`;
  const draftID = tab === 'knowledge'
    ? depositionDraftField(latestDraft, 'Memory draft: ')
    : tab === 'packages'
      ? depositionDraftField(latestDraft, 'Capability draft: ')
      : depositionDraftField(latestDraft, 'Workflow draft: ');
  return {
    role_code: roleCode,
    role_label: roleLabel,
    draft_id: draftID && draftID !== 'not created' ? draftID : undefined,
    draft_name: draftName,
    source,
  };
};

const latestDepositionDraftForRole = (history: BoardHistoryItem[], roleCode?: string) => history
  .filter((item) => item.id.startsWith('deposition-') && (!roleCode || historyRoleCode(item) === roleCode))
  .sort((left, right) => new Date(right.timestamp).getTime() - new Date(left.timestamp).getTime())[0];

const shouldPrioritizeManagementDecision = (
  latestCluster: BoardHistoryItem | undefined,
  latestManagement: BoardHistoryItem | undefined,
) => {
  if (!latestManagement) {
    return false;
  }
  if (!latestCluster) {
    return true;
  }
  return new Date(latestManagement.timestamp).getTime() >= new Date(latestCluster.timestamp).getTime();
};

const deriveDecisionBoardSummary = (
  dashboardSummary: string,
  history: BoardHistoryItem[],
) => {
  const latestCluster = pickPriorityDecisionCluster(history);
  const latestManagement = pickLatestManagementDecision(history);
  const latestStandard = pickLatestStandardPublished(history);
  if (latestStandard && (!latestManagement || new Date(latestStandard.timestamp).getTime() >= new Date(latestManagement.timestamp).getTime()) && (!latestCluster || new Date(latestStandard.timestamp).getTime() >= new Date(latestCluster.timestamp).getTime())) {
    const roleCode = historyRoleCode(latestStandard);
    const roleLabel = roleCode ? roleCode.toUpperCase() : 'the stabilized role';
    return `${roleLabel} is now running under a freshly published recovery standard. The next move is to observe live exceptions under policy instead of reopening direct management intervention.`;
  }
  if (shouldPrioritizeManagementDecision(latestCluster, latestManagement)) {
    const roleCode = historyRoleCode(latestManagement);
    const roleLabel = roleCode ? roleCode.toUpperCase() : 'the escalated role';
    switch (managementDecisionType(latestManagement)) {
      case 'autonomy_return':
        return `${roleLabel} has been cleared to return to autonomous execution. iWorkerCenter should keep the operating logic in system assets and only re-open management review if fresh variance appears.`;
      case 'deferred':
        return `Management has deferred direct intervention for ${roleLabel} until the next review window. The organization should keep running inside delegated policy while coordination risk is monitored closely.`;
      default:
        return `${roleLabel} is already under active management review. The next move is to clear blockers, adjust resources, or return the role to delegated execution.`;
    }
  }
  if (!latestCluster) {
    return dashboardSummary;
  }
  switch (latestCluster.clusterExecutionStatus) {
    case 'rejected':
      return `${latestCluster.clusterSkillTitle || 'An organizational decision'} has encountered execution resistance. ${latestCluster.clusterFocusTitle || 'The current focus'} should be reviewed immediately before more work queues behind it.`;
    case 'done':
      return `${latestCluster.clusterSkillTitle || 'An organizational decision'} has already produced a completed operating action. Leadership can now verify whether the result should settle into the system as a reusable rule or asset.`;
    case 'in_progress':
      return `${latestCluster.clusterSkillTitle || 'An organizational decision'} is now in active execution. Management should keep attention on delivery evidence instead of opening too many new fronts.`;
    case 'accepted':
    case 'pending':
      return `${latestCluster.clusterSkillTitle || 'An organizational decision'} has been translated into an execution task and is waiting to move further. The current focus is follow-through quality, not more diagnosis.`;
    default:
      return dashboardSummary;
  }
};

const deriveDecisionBoardFocus = (
  dashboardFocus: ExecutiveBoardFocus | null,
  history: BoardHistoryItem[],
) => {
  const latestCluster = pickPriorityDecisionCluster(history);
  const latestManagement = pickLatestManagementDecision(history);
  const latestStandard = pickLatestStandardPublished(history);
  if (latestStandard && (!latestManagement || new Date(latestStandard.timestamp).getTime() >= new Date(latestManagement.timestamp).getTime()) && (!latestCluster || new Date(latestStandard.timestamp).getTime() >= new Date(latestCluster.timestamp).getTime())) {
    const roleCode = historyRoleCode(latestStandard);
    if (!roleCode) {
      return dashboardFocus;
    }
    const roleLabel = roleCode.toUpperCase();
    return {
      title: `Monitor ${roleLabel} under policy`,
      summary: `${roleLabel} now has a published recovery standard inside the organization.`,
      description: 'The organization has already published the new standard. The next priority is to observe whether fresh exceptions are now absorbed by policy, workflow, and memory without another management loop.',
      status: 'ok',
      role_code: roleCode,
      role_label: roleLabel,
    };
  }
  if (shouldPrioritizeManagementDecision(latestCluster, latestManagement)) {
    const roleCode = historyRoleCode(latestManagement);
    if (!roleCode) {
      return dashboardFocus;
    }
    const roleLabel = roleCode.toUpperCase();
    switch (managementDecisionType(latestManagement)) {
      case 'autonomy_return':
        return {
          title: `Keep ${roleLabel} in delegated execution`,
          summary: `${roleLabel} has been confirmed back into autonomous coordination.`,
          description: 'Management has closed the exception loop for this role. The next priority is to keep execution inside policy, preserve the learning in system assets, and only escalate again if new operating risk appears.',
          status: 'ok',
          role_code: roleCode,
          role_label: roleLabel,
        };
      case 'deferred':
        return {
          title: `Review ${roleLabel} at next window`,
          summary: `${roleLabel} remains inside organizational observation until the deferred review window.`,
          description: 'Management explicitly deferred direct intervention. iWorkerCenter should keep the role inside delegated coordination, surface new evidence, and re-open escalation only if operating risk continues to rise.',
          status: 'info',
          role_code: roleCode,
          role_label: roleLabel,
        };
      default:
        return {
          title: `Hold management attention on ${roleLabel}`,
          summary: `${roleLabel} is already under active management review.`,
          description: 'Management has already taken this exception into direct review. The priority is now blocker removal, resource adjustment, and deciding when the role can safely return to delegated execution.',
          status: 'warn',
          role_code: roleCode,
          role_label: roleLabel,
        };
    }
  }
  if (!latestCluster || !latestCluster.clusterRoleCode) {
    return dashboardFocus;
  }

  const titleBase = latestCluster.clusterFocusTitle || latestCluster.clusterSkillTitle || 'Organizational follow-through';
  switch (latestCluster.clusterExecutionStatus) {
    case 'rejected':
      return {
        title: `Recover ${titleBase}`,
        summary: `${titleBase} has stalled in execution and now needs management intervention.`,
        description: latestCluster.clusterExecutionResult
          ? `The linked task was rejected with result: ${latestCluster.clusterExecutionResult}`
          : 'The linked handoff was rejected before completion. Review ownership, routing, and delegation criteria.',
        status: 'warn',
        role_code: latestCluster.clusterRoleCode,
        role_label: latestCluster.clusterRoleCode,
      };
    case 'done':
      return {
        title: `Institutionalize ${titleBase}`,
        summary: `${titleBase} has produced a completed execution result.`,
        description: latestCluster.clusterExecutionResult
          ? `The linked task completed with result: ${latestCluster.clusterExecutionResult}`
          : 'The linked handoff completed successfully. The next move is to capture the result as a reusable organizational asset.',
        status: 'ok',
        role_code: latestCluster.clusterRoleCode,
        role_label: latestCluster.clusterRoleCode,
      };
    case 'in_progress':
      return {
        title: `Monitor ${titleBase}`,
        summary: `${titleBase} is already being executed by the organization.`,
        description: 'Management should monitor evidence of progress and clear blockers before opening a new intervention thread.',
        status: 'info',
        role_code: latestCluster.clusterRoleCode,
        role_label: latestCluster.clusterRoleCode,
      };
    case 'accepted':
    case 'pending':
      return {
        title: `Push ${titleBase} into motion`,
        summary: `${titleBase} has been converted into a task but still needs stronger follow-through.`,
        description: 'The current management priority is to move the linked handoff from queue to active execution with clear ownership.',
        status: 'info',
        role_code: latestCluster.clusterRoleCode,
        role_label: latestCluster.clusterRoleCode,
      };
    default:
      return dashboardFocus;
  }
};

const resolveBoardFocus = (
  dashboardFocus: ExecutiveBoardFocus | null,
  skillResult: ExecutiveSkillResult | null,
): ExecutiveBoardFocus | null => skillResult?.focus || dashboardFocus;

const cloudHeartbeatTone = (status?: string): 'ok' | 'info' | 'warn' => {
  switch (status) {
    case 'online':
      return 'ok';
    case 'degraded':
    case 'waiting_for_credentials':
      return 'info';
    case 'error':
      return 'warn';
    default:
      return 'info';
  }
};

const badgeClass = (status: string) => {
  switch (status) {
    case 'warn':
      return 'badge warn';
    case 'ok':
      return 'badge ok';
    default:
      return 'badge info';
  }
};

const actionKey = (item: ExecutiveAction) => `${item.title}-${item.owner}-${item.owner_role_code}`;
const actionExecutionStatusLabel = (item: ExecutiveAction) => {
  if (!item.linked_task_status) {
    return '';
  }
  const resultSuffix = item.linked_task_result ? ` | ${item.linked_task_result}` : '';
  return `Task ${item.linked_task_status}${resultSuffix}`;
};

const actionExecutionTone = (item: ExecutiveAction) => (
  item.linked_task_status === 'rejected'
    ? 'warn'
    : item.linked_task_status === 'done' || item.linked_task_status === 'completed'
      ? 'ok'
      : 'info'
);

const actionHasLiveTask = (item: ExecutiveAction) => Boolean(item.linked_task_status);

const actionCanTransitionDirectly = (item: ExecutiveAction) => Boolean(item.linked_task_id && item.linked_task_status);

const taskTransitionOptions = (status?: string): Array<'accept' | 'start' | 'complete' | 'reject'> => {
  switch (status) {
    case 'pending':
      return ['accept', 'reject'];
    case 'accepted':
      return ['start', 'reject'];
    case 'in_progress':
      return ['complete', 'reject'];
    default:
      return [];
  }
};

const actionTransitionOptions = (item: ExecutiveAction): Array<'accept' | 'start' | 'complete' | 'reject'> => taskTransitionOptions(item.linked_task_status);

const actionTransitionLabel = (action: 'accept' | 'start' | 'complete' | 'reject') => {
  switch (action) {
    case 'accept':
      return 'Accept';
    case 'start':
      return 'Start';
    case 'complete':
      return 'Complete';
    case 'reject':
      return 'Reject';
  }
};

const resolveActionExecution = (item: ExecutiveAction, actions: ExecutiveAction[]) => actions.find((entry) => actionKey(entry) === actionKey(item)) || item;

const actionNavigationTarget = (item: ExecutiveAction): CommunicationsNavigationTarget => ({
  task_id: item.linked_task_id || undefined,
  role_code: item.owner_role_code || undefined,
  source: item.linked_task_id ? 'overview_action_task' : actionHasLiveTask(item) ? 'overview_action_execution' : 'overview_actions',
});

const actionOpenLabel = (item: ExecutiveAction) => {
  if (item.linked_task_id) {
    return 'Open Task';
  }
  if (actionHasLiveTask(item)) {
    return 'Open Execution';
  }
  return 'Open role workspace';
};

const actionCreateLabel = (item: ExecutiveAction, creating: boolean) => {
  if (actionHasLiveTask(item)) {
    return item.linked_task_id ? 'Task Linked' : 'In Execution';
  }
  return creating ? 'Creating...' : 'Create Task';
};

const actionCanGenerateDepositionDraft = (item: ExecutiveAction) => item.title.startsWith('Deposit ');
const actionIsDepositionReview = (item: ExecutiveAction) => item.title === 'Review deposition drafts for rollout';

const actionMatchesSkillRecommendation = (item: ExecutiveAction, result: ExecutiveSkillResult | null) => {
  if (!result) {
    return false;
  }
  return result.recommendations.some((entry) => actionKey(entry) === actionKey(item));
};


type BatchTransitionTarget = {
  taskId: string;
  title: string;
  roleCode?: string;
  sourceType: 'action' | 'history';
  sourceTitle?: string;
};

const collectBatchTransitionTargets = (
  actions: ExecutiveAction[],
  history: BoardHistoryItem[],
  status: string,
): BatchTransitionTarget[] => {
  const result: BatchTransitionTarget[] = [];
  const seen = new Set<string>();

  actions.forEach((item) => {
    if (!item.linked_task_id || item.linked_task_status !== status || seen.has(item.linked_task_id)) {
      return;
    }
    seen.add(item.linked_task_id);
    result.push({
      taskId: item.linked_task_id,
      title: item.title,
      roleCode: item.owner_role_code || undefined,
      sourceType: 'action',
      sourceTitle: item.title,
    });
  });

  history.forEach((item) => {
    const taskId = item.navigationTarget?.task_id;
    if (!item.isCluster || !taskId || item.clusterExecutionStatus !== status || seen.has(taskId)) {
      return;
    }
    seen.add(taskId);
    result.push({
      taskId,
      title: item.title,
      roleCode: item.clusterRoleCode || item.navigationTarget?.role_code || undefined,
      sourceType: 'history',
      sourceTitle: item.clusterFocusTitle || item.clusterSkillTitle || item.title,
    });
  });

  return result;
};

const batchTargetSourceLabel = (target: BatchTransitionTarget | null) => {
  if (!target) {
    return '';
  }
  const rolePart = target.roleCode ? ` | Role: ${target.roleCode}` : '';
  if (target.sourceType === 'history') {
    return `Source: Organization history | ${target.sourceTitle || target.title}${rolePart}`;
  }
  return `Source: Recommended action | ${target.sourceTitle || target.title}${rolePart}`;
};

const findBatchTargetForRole = (
  targets: BatchTransitionTarget[],
  roleCode?: string,
) => {
  if (!roleCode) {
    return null;
  }
  return targets.find((target) => target.roleCode === roleCode) || null;
};

const roleSuggestedAction = (
  roleItem: BoardRoleItem,
  pendingTarget: BatchTransitionTarget | null,
  acceptedTarget: BatchTransitionTarget | null,
) => {
  if (acceptedTarget) {
    return {
      action: 'start',
      tone: 'info',
      label: 'Suggested next step: Activate execution handoff',
      detail: `Activate organizational execution for ${acceptedTarget.title} under ${roleItem.roleCode}.`,
    };
  }
  if (pendingTarget) {
    return {
      action: 'accept',
      tone: 'warn',
      label: 'Suggested next step: Authorize delegation',
      detail: `Authorize ${pendingTarget.title} so the execution role can formally take ownership.`,
    };
  }
  if (roleItem.risk === 'critical' || roleItem.risk === 'watch') {
    return {
      action: 'open_communications',
      tone: roleItem.risk === 'critical' ? 'warn' : 'info',
      label: 'Suggested next step: Review execution blockers',
      detail: `No active organizational handoff is queued right now. Review live conversations and blockers under ${roleItem.roleCode}.`,
    };
  }
  return {
    action: 'open_top_target',
    tone: 'ok',
    label: 'Suggested next step: Keep organizational monitoring',
    detail: `${roleItem.roleCode} is currently stable enough to stay in organizational monitoring without immediate intervention.`,
  };
};

const roleFollowThroughHint = (
  feedback: RoleExecutionFeedback | undefined,
  suggestedAction: ReturnType<typeof roleSuggestedAction>,
) => {
  if (!feedback) {
    return '';
  }
  if (feedback.tone === 'warn') {
    return `Next organizational move: ${suggestedAction.detail}`;
  }
  switch (suggestedAction.action) {
    case 'start':
      return `Next organizational move: ${suggestedAction.detail}`;
    case 'open_communications':
      return `Next organizational move: ${suggestedAction.detail}`;
    case 'open_top_target':
      return 'Next organizational move: Keep this role under observation and inspect the current execution context when new variance appears.';
    default:
      return `Next organizational move: ${suggestedAction.detail}`;
  }
};

const roleBoardAttentionLabel = (suggestedAction: ReturnType<typeof roleSuggestedAction>) => {
  switch (suggestedAction.action) {
    case 'start':
      return 'Org attention';
    case 'accept':
      return 'Org attention';
    case 'open_communications':
      return 'Org attention';
    default:
      return '';
  }
};

const roleBoardSummaryClause = (
  roleItem: BoardRoleItem,
  suggestedAction: ReturnType<typeof roleSuggestedAction>,
  feedback: RoleExecutionFeedback | undefined,
) => {
  if (feedback) {
    return `${roleItem.roleCode} was just advanced`;
  }
  switch (suggestedAction.action) {
    case 'start':
      return `${roleItem.roleCode} should be started next`;
    case 'accept':
      return `${roleItem.roleCode} is waiting for acceptance`;
    case 'open_communications':
      return `${roleItem.roleCode} needs execution review`;
    default:
      return `${roleItem.roleCode} is in monitoring mode`;
  }
};
const normalizeValue = (value: string) => value.trim().toLowerCase().replace(/\s+/g, '-');

const roleLabelFromCode = (roleCode: string, roles: Role[]) => {
  const matchedRole = roles.find((role) => normalizeValue(role.code) === normalizeValue(roleCode));
  return matchedRole?.name || roleCode.toUpperCase();
};

const findRoleForAction = (item: ExecutiveAction, roles: Role[]) => {
  const requestedCode = normalizeValue(item.owner_role_code || '');
  const requestedLabel = normalizeValue(item.owner_role_label || item.owner || '');

  return roles.find((role) => normalizeValue(role.code) === requestedCode)
    || roles.find((role) => normalizeValue(role.name) === requestedLabel)
    || roles.find((role) => normalizeValue(role.code) === normalizeValue(item.owner || ''))
    || null;
};

const findRoleForRisk = (item: DashboardItem, roles: Role[]) => {
  if (item.role_code) {
    const matchedByCode = roles.find(
      (role) => normalizeValue(role.code) === normalizeValue(item.role_code || ''),
    );
    if (matchedByCode) {
      return matchedByCode;
    }
  }

  const haystack = normalizeValue(`${item.title} ${item.description}`);
  return roles.find((role) => {
    const code = normalizeValue(role.code);
    const name = normalizeValue(role.name);
    return (code && haystack.includes(code)) || (name && haystack.includes(name));
  }) || null;
};

const getSuggestedOwnerId = (item: ExecutiveAction, colleagues: Colleague[], roles: Role[]) => {
  const role = findRoleForAction(item, roles);
  if (role) {
    const matchedColleague = colleagues.find((colleague) => colleague.role_id === role.id || normalizeValue(colleague.role_code || '') === normalizeValue(role.code));
    if (matchedColleague) {
      return matchedColleague.id;
    }
  }

  const fallbackByLabel = colleagues.find((colleague) => normalizeValue(colleague.role_name || '') === normalizeValue(item.owner_role_label || item.owner || ''));
  return fallbackByLabel?.id || colleagues[0]?.id || '';
};

const describeRoute = (item: ExecutiveAction, colleagues: Colleague[], roles: Role[], selectedOwnerId: string) => {
  const role = findRoleForAction(item, roles);
  const colleague = colleagues.find((entry) => entry.id === selectedOwnerId);
  if (role && colleague) {
    return `Route ${role.code} -> ${colleague.name}`;
  }
  if (role) {
    return `Route ${role.code} is available, manual assignee selection is required.`;
  }
  if (colleague) {
    return `No matching role found, using ${colleague.name} as the current owner.`;
  }
  return 'No digital employee is currently available for this recommendation.';
};

const taskImpactWeight = (status: string) => {
  switch (status) {
    case 'in_progress':
      return 2;
    case 'pending':
    case 'accepted':
      return 1;
    default:
      return 0;
  }
};

const formatBoardTimestamp = (value: string) => {
  if (!value) {
    return 'Live view';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
};

const summarizeManagementDecisionHistory = (auditLogs: AuditLog[]): BoardHistoryItem[] => auditLogs
  .filter((item) => item.work_type === 'management_decision' || item.work_type === 'management_autonomy_return')
  .slice(0, 4)
  .map((item) => {
    const roleCode = extractAuditField(item.error_msg || '', 'role_code');
    const decisionType = extractAuditField(item.error_msg || '', 'decision_type');
    const detail = extractAuditField(item.error_msg || '', 'detail') || item.summary || 'Management intervention was recorded.';
    const displayTime = extractAuditField(item.error_msg || '', 'display_time') || item.created_at;
    const isDeferred = decisionType === 'deferred';
    const isAutonomyReturn = decisionType === 'autonomy_return' || item.work_type === 'management_autonomy_return';
    return {
      id: `management-${item.id}`,
      title: isAutonomyReturn
        ? 'Return to autonomy confirmed' + (roleCode ? ` for ${roleCode.toUpperCase()}` : '')
        : isDeferred
          ? 'Management follow-up deferred' + (roleCode ? ` for ${roleCode.toUpperCase()}` : '')
          : 'Management review opened' + (roleCode ? ` for ${roleCode.toUpperCase()}` : ''),
      detail,
      timestamp: item.created_at,
      tone: isAutonomyReturn ? 'ok' : isDeferred ? 'info' : 'warn',
      detailLines: [
        `Decision type: ${decisionType || 'review'}`,
        `Role: ${roleCode || 'No direct role attached'}`,
        `Recorded at: ${displayTime}`,
      ],
      navigationTarget: roleCode
        ? {
          role_code: roleCode,
          source: 'organization_history',
        }
        : {
          source: 'organization_history',
        },
    };
  });
const summarizeManagementRecoveryHistory = (
  managementDecisionEvents: BoardHistoryItem[],
  executiveEvents: BoardHistoryItem[],
): BoardHistoryItem[] => managementDecisionEvents.flatMap((decision) => {
  const roleCode = historyRoleCode(decision);
  if (!roleCode || managementDecisionType(decision) === 'autonomy_return') {
    return [];
  }
  const related = executiveEvents.find((item) => item.clusterRoleCode === roleCode && item.clusterExecutionStatus);
  if (!related) {
    return [];
  }
  let title = `Recovery action dispatched for ${roleCode.toUpperCase()}`;
  let tone: 'ok' | 'info' | 'warn' = 'info';
  let detail = `${related.clusterFocusTitle || related.clusterSkillTitle || roleCode.toUpperCase()} has been pushed back into organizational execution after management intervention.`;
  let nextStep = 'Track whether the dispatched recovery task returns the role to stable delegated operation.';
  switch (related.clusterExecutionStatus) {
    case 'in_progress':
      tone = 'ok';
      detail = `${related.clusterFocusTitle || related.clusterSkillTitle || roleCode.toUpperCase()} is already in active recovery execution after management intervention.`;
      break;
    case 'done':
      title = `Recovery completed for ${roleCode.toUpperCase()}`;
      tone = 'ok';
      detail = `${related.clusterFocusTitle || related.clusterSkillTitle || roleCode.toUpperCase()} has completed recovery execution and can now move back toward autonomous coordination.`;
      nextStep = 'Verify the result is stable, then remove the role from active management attention and deposit the learning into the system.';
      break;
    case 'rejected':
      title = `Recovery blocked for ${roleCode.toUpperCase()}`;
      tone = 'warn';
      detail = `${related.clusterFocusTitle || related.clusterSkillTitle || roleCode.toUpperCase()} failed to recover cleanly after management intervention and needs another decision loop.`;
      nextStep = 'Management should review routing, ownership, and acceptance criteria before retrying the recovery path.';
      break;
    default:
      break;
  }
  return [{
    id: `recovery-${decision.id}-${related.id}`,
    title,
    detail,
    timestamp: related.timestamp,
    tone,
    detailLines: [
      `Management decision: ${decision.title}`,
      `Execution status: ${related.clusterExecutionStatus || "unknown"}`,
      `Role: ${roleCode}`,
      `Next step: ${nextStep}`,
    ],
    navigationTarget: related.navigationTarget,
  }];
});
const summarizeBoardHistory = (
  tasks: Array<{
    id: string;
    title: string;
    status: string;
    to_role_code?: string;
    to_colleague_id?: string;
    updated_at: string;
    created_at: string;
    result?: string;
  }>,
  auditLogs: AuditLog[],
): BoardHistoryItem[] => {
  const taskEvents: BoardHistoryItem[] = tasks
    .filter((task) => task.status !== 'completed' && task.status !== 'done')
    .slice(0, 4)
    .map((task) => ({
      id: `task-${task.id}`,
      title: task.title,
      detail: task.to_role_code
        ? `Work is routed toward ${task.to_role_code} and is currently ${task.status}.`
        : `Task is currently ${task.status} and waiting on its assigned route.`,
      timestamp: task.updated_at || task.created_at,
      tone: task.status === 'rejected' ? 'warn' : task.status === 'in_progress' ? 'ok' : 'info',
      navigationTarget: {
        task_id: task.id,
        role_code: task.to_role_code || undefined,
        source: 'organization_history',
      },
    }));

  const routingEvents: BoardHistoryItem[] = auditLogs
    .filter((item) => item.work_type === 'role_routing_action')
    .slice(0, 4)
    .map((item) => ({
      id: `audit-${item.id}`,
      title: item.summary || 'Routing command',
      detail: item.error_msg
        ? item.error_msg.replace(/^before:\s*/s, 'Before: ').replace(/\nafter:\s*/s, ' | After: ')
        : 'Routing action executed from the organizational control surface.',
      timestamp: item.created_at,
      tone: item.status === 'ok' ? 'ok' : 'warn',
      navigationTarget: {
        source: 'organization_history',
      },
    }));

  const executiveEvents = buildClusteredExecutiveHistory(auditLogs, tasks);
  const managementDecisionEvents = summarizeManagementDecisionHistory(auditLogs);
  const managementRecoveryEvents = summarizeManagementRecoveryHistory(managementDecisionEvents, executiveEvents);

  return [...executiveEvents, ...managementRecoveryEvents, ...managementDecisionEvents, ...routingEvents, ...taskEvents]
    .sort((a, b) => {
      const aPriority = a.isCluster ? decisionPriorityScore(a.clusterExecutionStatus || '') : 5;
      const bPriority = b.isCluster ? decisionPriorityScore(b.clusterExecutionStatus || '') : 5;
      if (aPriority !== bPriority) {
        return aPriority - bPriority;
      }
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    })
    .slice(0, 6);
};

const summarizeBoardRoles = (
  tasks: Array<{ to_role_code?: string; status: string }>,
  roles: Role[],
  colleagues: Colleague[],
  routingOverview: CollaborationRoutingOverview | null,
): BoardRoleItem[] => {
  if (!routingOverview) {
    return [];
  }

  const workloadByRole = tasks.reduce<Record<string, { openTaskCount: number; impactScore: number }>>((acc, task) => {
    const roleCode = task.to_role_code || '';
    const weight = taskImpactWeight(task.status);
    if (!roleCode || weight === 0) {
      return acc;
    }
    const current = acc[roleCode] || { openTaskCount: 0, impactScore: 0 };
    current.openTaskCount += 1;
    current.impactScore += weight;
    acc[roleCode] = current;
    return acc;
  }, {});

  return roles
    .map((role) => {
      const members = colleagues.filter((colleague) => colleague.role_id === role.id || normalizeValue(colleague.role_code || '') === normalizeValue(role.code));
      const coverage = members.reduce(
        (acc, colleague) => {
          const state = routingOverview.status_by_colleague[colleague.id]?.effective_state || 'active';
          if (state === 'standby') acc.standby += 1;
          else if (state === 'unhealthy') acc.unhealthy += 1;
          else acc.active += 1;
          return acc;
        },
        { active: 0, standby: 0, unhealthy: 0 },
      );
      let risk: 'stable' | 'watch' | 'critical' = 'stable';
      if (members.length === 0 || coverage.active === 0) risk = 'critical';
      else if (coverage.active === 1 || coverage.unhealthy > 0) risk = 'watch';
      const workload = workloadByRole[role.code] || { openTaskCount: 0, impactScore: 0 };
      return {
        roleCode: role.code,
        roleName: role.name,
        risk,
        active: coverage.active,
        standby: coverage.standby,
        unhealthy: coverage.unhealthy,
        openTaskCount: workload.openTaskCount,
        impactScore: workload.impactScore,
      };
    })
    .sort((a, b) => {
      const rank = { critical: 0, watch: 1, stable: 2 };
      return rank[a.risk] - rank[b.risk]
        || b.impactScore - a.impactScore
        || b.openTaskCount - a.openTaskCount
        || a.roleName.localeCompare(b.roleName);
    })
    .slice(0, 6);
};

const compareBoardSignalPriority = (a: { priority?: number; tone: 'ok' | 'info' | 'warn'; title: string }, b: { priority?: number; tone: 'ok' | 'info' | 'warn'; title: string }) => {
  const priorityDiff = (a.priority ?? 999) - (b.priority ?? 999);
  if (priorityDiff !== 0) {
    return priorityDiff;
  }
  const toneRank = { warn: 0, info: 1, ok: 2 };
  const toneDiff = toneRank[a.tone] - toneRank[b.tone];
  if (toneDiff !== 0) {
    return toneDiff;
  }
  return a.title.localeCompare(b.title);
};
const dashboardItemToBoardSignal = (item: DashboardItem): BoardSignal => ({
  title: item.title,
  priority: item.signal_priority,
  tone: item.status === 'ok' ? 'ok' : item.status === 'warn' ? 'warn' : 'info',
  summary: item.description,
  detail: item.role_label
    ? `Primary owner: ${item.role_label}.`
    : 'Open the communications console for role-level follow-through.',
  navigationTarget: item.role_code
    ? {
      role_code: item.role_code,
      source: 'organization_briefing',
    }
    : undefined,
});

const compareBoardSignalPriorityFromDashboard = (a: DashboardItem, b: DashboardItem) => compareBoardSignalPriority(
  {
    title: a.title,
    priority: a.signal_priority,
    tone: a.status === 'ok' ? 'ok' : a.status === 'warn' ? 'warn' : 'info',
  },
  {
    title: b.title,
    priority: b.signal_priority,
    tone: b.status === 'ok' ? 'ok' : b.status === 'warn' ? 'warn' : 'info',
  },
);
const summarizeBoardSignals = (
  tasks: Array<{ to_role_code?: string; status: string }>,
  roles: Role[],
  colleagues: Colleague[],
  routingOverview: CollaborationRoutingOverview | null,
  auditLogs: AuditLog[],
): BoardSignal[] => {
  if (!routingOverview) {
    const signals: BoardSignal[] = [
      {
        title: 'Operating signal',
        tone: 'info',
        summary: 'Operational signals are warming up.',
        detail: 'Routing telemetry is not available yet, so the center is showing executive data only.',
      },
    ];

    return signals.sort(compareBoardSignalPriority);
  }

  const workloadByRole = tasks.reduce<Record<string, { openTaskCount: number; impactScore: number }>>((acc, task) => {
    const roleCode = task.to_role_code || '';
    const weight = taskImpactWeight(task.status);
    if (!roleCode || weight === 0) {
      return acc;
    }
    const current = acc[roleCode] || { openTaskCount: 0, impactScore: 0 };
    current.openTaskCount += 1;
    current.impactScore += weight;
    acc[roleCode] = current;
    return acc;
  }, {});

  const roleState = roles
    .map((role) => {
      const members = colleagues.filter((colleague) => colleague.role_id === role.id || normalizeValue(colleague.role_code || '') === normalizeValue(role.code));
      const coverage = members.reduce(
        (acc, colleague) => {
          const state = routingOverview.status_by_colleague[colleague.id]?.effective_state || 'active';
          if (state === 'standby') acc.standby += 1;
          else if (state === 'unhealthy') acc.unhealthy += 1;
          else acc.active += 1;
          return acc;
        },
        { active: 0, standby: 0, unhealthy: 0 },
      );
      const workload = workloadByRole[role.code] || { openTaskCount: 0, impactScore: 0 };
      let risk: 'stable' | 'watch' | 'critical' = 'stable';
      if (members.length === 0 || coverage.active === 0) risk = 'critical';
      else if (coverage.active === 1 || coverage.unhealthy > 0) risk = 'watch';
      return { role, coverage, workload, risk };
    })
    .sort((a, b) => {
      const rank = { critical: 0, watch: 1, stable: 2 };
      return rank[a.risk] - rank[b.risk]
        || b.workload.impactScore - a.workload.impactScore
        || b.workload.openTaskCount - a.workload.openTaskCount
        || a.role.name.localeCompare(b.role.name);
    });

  const criticalCount = roleState.filter((item) => item.risk === 'critical').length;
  const watchCount = roleState.filter((item) => item.risk === 'watch').length;
  const topRole = roleState[0];
  const openTasks = tasks.filter((task) => taskImpactWeight(task.status) > 0);
  const inFlightTasks = tasks.filter((task) => task.status === 'in_progress');
  const heartbeatTimeoutCount = Object.values(routingOverview.status_by_colleague).filter((item) => item.reason === 'heartbeat_timeout').length;
  const recentRoutingActions = auditLogs.filter((item) => item.work_type === 'role_routing_action').slice(0, 5);

  const signals: BoardSignal[] = [
    {
      title: 'Coverage posture',
      tone: criticalCount > 0 ? 'warn' : watchCount > 0 ? 'info' : 'ok',
      summary: criticalCount > 0
        ? `${criticalCount} role${criticalCount > 1 ? 's are' : ' is'} in critical coverage risk.`
        : watchCount > 0
          ? `${watchCount} role${watchCount > 1 ? 's are' : ' is'} thin but still serving.`
          : 'All observed roles have healthy active coverage.',
      detail: topRole
        ? `${topRole.role.name} is the current priority role with ${topRole.workload.openTaskCount} open task${topRole.workload.openTaskCount !== 1 ? 's' : ''} behind it.`
        : 'No role coverage data is currently available.',
      navigationTarget: topRole
        ? { role_code: topRole.role.code, source: 'organization_briefing' }
        : undefined,
    },
    {
      title: 'Business load',
      tone: openTasks.length > 0 ? 'info' : 'ok',
      summary: `${openTasks.length} open collaboration task${openTasks.length !== 1 ? 's are' : ' is'} currently live across the organization.`,
      detail: inFlightTasks.length > 0
        ? `${inFlightTasks.length} task${inFlightTasks.length !== 1 ? 's are' : ' is'} already in execution, so management intervention should favor roles carrying live work.`
        : 'No handoff is currently in active execution, so capacity moves can be made before work starts to queue harder.',
      navigationTarget: topRole
        ? { role_code: topRole.role.code, source: 'organization_briefing' }
        : undefined,
    },
    {
      title: 'Runtime health',
      tone: heartbeatTimeoutCount > 0 ? 'warn' : 'ok',
      summary: heartbeatTimeoutCount > 0
        ? `${heartbeatTimeoutCount} runtime node${heartbeatTimeoutCount !== 1 ? 's have' : ' has'} dropped out because of heartbeat policy.`
        : 'Runtime heartbeat and routing policy are currently aligned.',
      detail: recentRoutingActions.length > 0
        ? `Management has issued ${recentRoutingActions.length} routing action${recentRoutingActions.length !== 1 ? 's' : ''} recently, giving us a traceable organizational intervention history.`
        : 'No organizational routing action has been executed yet from the control surface.',
      navigationTarget: heartbeatTimeoutCount > 0 && topRole
        ? { role_code: topRole.role.code, source: 'organization_briefing' }
        : undefined,
    },
  ];

  return signals.sort(compareBoardSignalPriority);
};

type OverviewPageProps = {
  navigationTarget?: OverviewNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToCommunications: (target: CommunicationsNavigationTarget) => void;
  onNavigateToTab: (tab: CenterTab, target?: AssetNavigationTarget) => void;
};

export function OverviewPage({
  navigationTarget,
  onNavigationHandled,
  onNavigateToCommunications,
  onNavigateToTab,
}: OverviewPageProps) {
  const { t } = useTranslation();
  const [metrics, setMetrics] = useState<Metric[]>([]);
  const [centerStatus, setCenterStatus] = useState<CenterStatus | null>(null);
  const [briefing, setBriefing] = useState<ExecutiveBriefing | null>(null);
  const [risks, setRisks] = useState<DashboardItem[]>([]);
  const [actions, setActions] = useState<ExecutiveAction[]>([]);
  const [skills, setSkills] = useState<ExecutiveSkill[]>([]);
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [activeSkillId, setActiveSkillId] = useState<string>('');
  const [activeSkillResult, setActiveSkillResult] = useState<ExecutiveSkillResult | null>(null);
  const [skillLoading, setSkillLoading] = useState(false);
  const [skillError, setSkillError] = useState('');
  const [taskMessage, setTaskMessage] = useState('');
  const [creatingActionKey, setCreatingActionKey] = useState('');
  const [transitioningActionKey, setTransitioningActionKey] = useState('');
  const [depositionDraftKey, setDepositionDraftKey] = useState('');
  const [publishingWorkflowKey, setPublishingWorkflowKey] = useState('');
  const [batchTransitionKey, setBatchTransitionKey] = useState('');
  const [roleExecutionFeedback, setRoleExecutionFeedback] = useState<Record<string, RoleExecutionFeedback>>({});
  const [deferredManagementRoles, setDeferredManagementRoles] = useState<Record<string, ManagementDecisionRecord>>({});
  const [activeManagementReviews, setActiveManagementReviews] = useState<Record<string, ManagementDecisionRecord>>({});
  const [selectedOwners, setSelectedOwners] = useState<Record<string, string>>({});
  const [updatedAt, setUpdatedAt] = useState('');
  const [boardSignals, setBoardSignals] = useState<BoardSignal[]>([]);
  const [boardHistory, setBoardHistory] = useState<BoardHistoryItem[]>([]);
  const [boardRoles, setBoardRoles] = useState<BoardRoleItem[]>([]);
  const [boardSummary, setBoardSummary] = useState('');
  const [prioritySummary, setPrioritySummary] = useState('');
  const [boardFocus, setBoardFocus] = useState<ExecutiveBoardFocus | null>(null);
  const [priorityDecision, setPriorityDecision] = useState<ExecutiveBoardFocus | null>(null);
  const [boardUpdatedAt, setBoardUpdatedAt] = useState('');
  const [refreshingBoard, setRefreshingBoard] = useState(false);
  const [focusedOverviewRoleCode, setFocusedOverviewRoleCode] = useState('');
  const [expandedHistoryId, setExpandedHistoryId] = useState('');
  const [focusedOverviewSection, setFocusedOverviewSection] = useState<'briefing' | 'coordination' | 'actions' | ''>('');
  const briefingSectionRef = useRef<HTMLDivElement | null>(null);
  const coordinationSectionRef = useRef<HTMLDivElement | null>(null);
  const actionsSectionRef = useRef<HTMLDivElement | null>(null);

  const loadOverviewData = async (refreshMode = false) => {
    if (refreshMode) {
      setRefreshingBoard(true);
    }
    const [cols, roleList, caps, mems, dashboard, skillData, collabTasks, routingOverview, auditLogs, serviceStatus] = await Promise.all([
      listColleagues().catch(() => []),
      listRoles().catch(() => []),
      listCapabilities().catch(() => []),
      listMemories().catch(() => []),
      fetchDashboard().catch(() => null),
      fetchExecutiveSkills().catch(() => ({ skills: [] })),
      listCollaborations().catch(() => []),
      getCollaborationRoutingSettings().catch(() => null),
      listAuditLogs(12).catch(() => []),
      fetchCenterStatus().catch(() => null),
    ]);

    const dashboardMetrics = dashboard?.metrics || [];
    const baseMetrics: Metric[] = [
      { label: t('nav.employees'), value: String(cols.length), hint: `${cols.length}` },
      { label: t('nav.packages'), value: String(caps.length), hint: `${caps.length}` },
      { label: t('nav.knowledge'), value: String(mems.length), hint: `${mems.length}` },
    ];
    setCenterStatus(serviceStatus);
    setColleagues(cols);
    setRoles(roleList);
    setMetrics(dashboardMetrics.length > 0 ? [...baseMetrics, ...dashboardMetrics].slice(0, 4) : baseMetrics);
    setBriefing(dashboard?.briefing || null);
    setRisks(dashboard?.risks || dashboard?.alerts || []);
    setActions(dashboard?.actions || []);
    setUpdatedAt(dashboard?.updated_at || '');
    setBoardSummary(dashboard?.board_summary || '');
    setPrioritySummary(dashboard?.priority_summary || '');
    setBoardFocus(dashboard?.board_focus || null);
    setPriorityDecision(dashboard?.priority_decision || null);
    setSkills(skillData.skills || []);
    setBoardSignals(
      dashboard?.board_signals?.length
        ? [...dashboard.board_signals].sort(compareBoardSignalPriorityFromDashboard).map(dashboardItemToBoardSignal)
        : summarizeBoardSignals(collabTasks, roleList, cols, routingOverview, auditLogs),
    );
    setBoardHistory(dashboard?.board_history || summarizeBoardHistory(collabTasks, auditLogs));
    setBoardRoles(summarizeBoardRoles(collabTasks, roleList, cols, routingOverview));
    setBoardUpdatedAt(dashboard?.updated_at || new Date().toISOString());
    const managementDecisionState = summarizeManagementDecisionState(auditLogs);
    setDeferredManagementRoles(managementDecisionState.deferred);
    setActiveManagementReviews(managementDecisionState.review);
    if (cols.length > 0) {
      setSelectedOwners((prev) => {
        const next = { ...prev };
        (dashboard?.actions || []).forEach((item: ExecutiveAction) => {
          const key = actionKey(item);
          if (!next[key]) next[key] = getSuggestedOwnerId(item, cols, roleList);
        });
        return next;
      });
    }
    if (refreshMode) {
      setRefreshingBoard(false);
    }
  };

  useEffect(() => {
    void loadOverviewData();

    const timer = window.setInterval(() => {
      void loadOverviewData(true);
    }, 15000);

    return () => window.clearInterval(timer);
  }, [t]);

  useEffect(() => {
    if (!navigationTarget) {
      return;
    }
    if (navigationTarget.role_code) {
      setFocusedOverviewRoleCode(navigationTarget.role_code);
    }
    onNavigationHandled?.();
  }, [navigationTarget, onNavigationHandled]);

  useEffect(() => {
    if (skills.length > 0 && !activeSkillId) {
      void handleRunSkill(skills[0]);
    }
  }, [skills, activeSkillId]);

  useEffect(() => {
    if (colleagues.length > 0 && activeSkillResult) {
      setSelectedOwners((prev) => {
        const next = { ...prev };
        activeSkillResult.recommendations.forEach((item) => {
          const key = actionKey(item);
          if (!next[key]) next[key] = getSuggestedOwnerId(item, colleagues, roles);
        });
        return next;
      });
    }
  }, [colleagues, roles, activeSkillResult]);

  const defaultFromColleagueId = useMemo(() => colleagues[0]?.id || '', [colleagues]);
  const dynamicBoardSummary = useMemo(
    () => deriveDecisionBoardSummary(prioritySummary || boardSummary, boardHistory),
    [prioritySummary, boardSummary, boardHistory],
  );
  const effectiveBoardFocus = useMemo(
    () => resolveBoardFocus(deriveDecisionBoardFocus(priorityDecision || boardFocus, boardHistory), activeSkillResult),
    [priorityDecision, boardFocus, boardHistory, activeSkillResult],
  );
  const pendingBatchTargets = useMemo(
    () => collectBatchTransitionTargets(actions, boardHistory, 'pending'),
    [actions, boardHistory],
  );
  const acceptedBatchTargets = useMemo(
    () => collectBatchTransitionTargets(actions, boardHistory, 'accepted'),
    [actions, boardHistory],
  );
  const topPendingBatchTarget = pendingBatchTargets[0] || null;
  const topAcceptedBatchTarget = acceptedBatchTargets[0] || null;
  const topBoardRoles = useMemo(() => boardRoles
    .slice()
    .sort((left, right) => {
      const leftSuggested = roleSuggestedAction(
        left,
        findBatchTargetForRole(pendingBatchTargets, left.roleCode),
        findBatchTargetForRole(acceptedBatchTargets, left.roleCode),
      );
      const rightSuggested = roleSuggestedAction(
        right,
        findBatchTargetForRole(pendingBatchTargets, right.roleCode),
        findBatchTargetForRole(acceptedBatchTargets, right.roleCode),
      );
      const scoreRole = (roleItem: BoardRoleItem, suggested: ReturnType<typeof roleSuggestedAction>) => {
        let score = roleItem.impactScore * 100 + roleItem.openTaskCount * 10;
        if (roleItem.roleCode === focusedOverviewRoleCode) score += 10000;
        if (roleExecutionFeedback[roleItem.roleCode]) score += 5000;
        switch (suggested.action) {
          case 'start':
            score += 4000;
            break;
          case 'accept':
            score += 3000;
            break;
          case 'open_communications':
            score += 2000;
            break;
          default:
            score += 1000;
            break;
        }
        switch (roleItem.risk) {
          case 'critical':
            score += 700;
            break;
          case 'watch':
            score += 300;
            break;
          default:
            break;
        }
        return score;
      };
      return scoreRole(right, rightSuggested) - scoreRole(left, leftSuggested);
    })
    .slice(0, 3), [boardRoles, pendingBatchTargets, acceptedBatchTargets, focusedOverviewRoleCode, roleExecutionFeedback]);
  const managementRecoveryState = useMemo(() => {
    const activeReviewEntry = Object.entries(activeManagementReviews)
      .sort((left, right) => new Date(right[1].recordedAt).getTime() - new Date(left[1].recordedAt).getTime())[0];
    if (activeReviewEntry) {
      const [roleCode, record] = activeReviewEntry;
      const feedback = roleExecutionFeedback[roleCode];
      if (feedback) {
        return {
          phase: 'recovery_dispatched' as const,
          tone: feedback.tone === 'warn' ? 'warn' as const : 'info' as const,
          label: 'Recovery action dispatched',
          roleCode,
          summary: `${roleCode} has already received a recovery move after management review.`,
          detail: `${feedback.message} Management can now stay at oversight level while execution evidence comes back into the organization.`,
          nextStep: 'Keep watching for delivery evidence before returning the role fully to autonomous coordination.',
        };
      }
      return {
        phase: 'under_review' as const,
        tone: 'warn' as const,
        label: 'Management review in progress',
        roleCode,
        summary: `${roleCode} is currently under active management review.`,
        detail: record.detail,
        nextStep: 'The next step is to clear blockers, adjust resources, or issue a recovery handoff back into execution.',
      };
    }

    const deferredEntry = Object.entries(deferredManagementRoles)
      .sort((left, right) => new Date(right[1].recordedAt).getTime() - new Date(left[1].recordedAt).getTime())[0];
    if (deferredEntry) {
      const [roleCode, record] = deferredEntry;
      return {
        phase: 'deferred_monitoring' as const,
        tone: 'info' as const,
        label: 'Deferred until review window',
        roleCode,
        summary: `${roleCode} remains inside delegated observation until the next management review window.`,
        detail: record.detail,
        nextStep: 'iWorkerCenter should continue monitoring coordination signals and only re-escalate if the risk boundary rises again.',
      };
    }

    const latestAutonomyReturn = pickLatestAutonomyReturn(boardHistory);
    const latestCapabilityApproved = pickLatestCapabilityApproved(boardHistory);
    const latestStandard = pickLatestStandardPublished(boardHistory);

    if (latestStandard) {
      const roleCode = historyRoleCode(latestStandard);
      return {
        phase: 'policy_monitoring' as const,
        tone: 'ok' as const,
        label: 'Published standard is live',
        roleCode,
        summary: `${roleCode} is now running under an active published recovery standard.`,
        detail: latestStandard.detail,
        nextStep: 'Watch whether new exceptions are now absorbed by the published policy, workflow, and knowledge assets without another management loop.',
      };
    }

    if (latestCapabilityApproved) {
      const roleCode = historyRoleCode(latestCapabilityApproved);
      return {
        phase: 'workflow_rollout_pending' as const,
        tone: 'info' as const,
        label: 'Capability package approved',
        roleCode,
        summary: `${roleCode} has already been absorbed into an approved recovery capability package.`,
        detail: latestCapabilityApproved.detail,
        nextStep: 'Publish the workflow standard next so the approved capability becomes live organizational policy instead of a reviewed but not yet enforced asset.',
      };
    }

    if (latestAutonomyReturn) {
      const roleCode = historyRoleCode(latestAutonomyReturn);
      return {
        phase: 'institutionalizing' as const,
        tone: 'ok' as const,
        label: 'Institutionalize the recovery learning',
        roleCode,
        summary: `${roleCode} has already returned to autonomous execution.`,
        detail: latestAutonomyReturn.detail,
        nextStep: 'Convert the successful recovery path into reusable memories, workflow rules, and management skills so the next similar exception can stay inside the system.',
      };
    }

    const advancedEntry = Object.entries(roleExecutionFeedback)
      .sort((left, right) => {
        const leftFocused = left[0] === focusedOverviewRoleCode ? 1 : 0;
        const rightFocused = right[0] === focusedOverviewRoleCode ? 1 : 0;
        return rightFocused - leftFocused;
      })[0];
    if (advancedEntry) {
      const [roleCode, feedback] = advancedEntry;
      return {
        phase: 'returning_to_autonomy' as const,
        tone: feedback.tone === 'warn' ? 'info' as const : 'ok' as const,
        label: 'Returning to autonomous execution',
        roleCode,
        summary: `${roleCode} has moved back into delegated execution after intervention.`,
        detail: feedback.message,
        nextStep: 'If delivery evidence remains healthy, the role can fall back out of management attention and return to normal organizational rhythm.',
      };
    }

    return null;
  }, [activeManagementReviews, deferredManagementRoles, roleExecutionFeedback, focusedOverviewRoleCode, boardHistory]);

  const institutionalizationProgressState = useMemo<InstitutionalizationProgressState>(() => {
    const roleCode = managementRecoveryState?.roleCode || effectiveBoardFocus?.role_code || '';
    const roleLabel = roleCode ? roleLabelFromCode(roleCode, roles) : 'the organization';
    const latestDraft = roleCode ? latestDepositionDraftForRole(boardHistory, roleCode) : undefined;
    const memoryDraftID = depositionDraftField(latestDraft, 'Memory draft: ');
    const capabilityDraftID = depositionDraftField(latestDraft, 'Capability draft: ');
    const workflowDraftID = depositionDraftField(latestDraft, 'Workflow draft: ');
    const memoryReady = Boolean(memoryDraftID && memoryDraftID !== 'not created');
    const capabilityReady = Boolean(capabilityDraftID && capabilityDraftID !== 'not created');
    const workflowReady = Boolean(workflowDraftID && workflowDraftID !== 'not created');

    if (!roleCode) {
      return {
        phase: 'inactive',
        tone: 'ok',
        title: 'No active institutionalization loop',
        summary: 'The organization is not currently carrying a recovery case that needs to be deposited into memory, package, or workflow assets.',
        detail: 'When an iWorker or human-supported recovery path proves itself in live execution, iWorkerCenter should convert that result into durable organizational assets here.',
        statLine: [
          'Memory review waiting for next recovered case',
          'Package approval opens after memory is captured',
          'Workflow publish opens after package is approved',
        ],
      };
    }

    if (managementRecoveryState?.phase === 'policy_monitoring') {
      return {
        phase: 'policy_monitoring',
        tone: 'ok',
        title: `${roleLabel} is in policy monitoring`,
        summary: `${roleLabel} already has a published workflow standard, so the organization should now observe whether future variance is absorbed without another management exception loop.`,
        detail: 'This is the final institutional stage: the company has already converted recovery learning into live organizational policy and now validates that the operating system, not individual memory, carries continuity.',
        roleCode,
        roleLabel,
        statLine: [
          workflowReady ? 'Workflow draft already published into live policy' : 'Policy is live for this recovered role',
          capabilityReady ? 'Capability package exists beneath the published policy' : 'Capability package was approved before policy rollout',
          'Watch exceptions instead of reopening direct management control',
        ],
        primaryActionLabel: 'Open workflows',
        secondaryActionLabel: 'Open knowledge',
        onPrimaryAction: () => onNavigateToTab('workflows', assetNavigationTargetForRole('workflows', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
        onSecondaryAction: () => onNavigateToTab('knowledge', assetNavigationTargetForRole('knowledge', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
      };
    }

    if (managementRecoveryState?.phase === 'workflow_rollout_pending') {
      return {
        phase: 'workflow_publish',
        tone: 'info',
        title: `${roleLabel} is waiting for workflow publish`,
        summary: `${roleLabel} has already passed package approval. The remaining step is to publish the workflow standard so the approved capability becomes enforced organizational behavior.`,
        detail: 'At this stage, the organization has validated the capability package but has not yet pushed it into the live operating policy layer. Until publish happens, the learning is reviewed but not fully institutionalized.',
        roleCode,
        roleLabel,
        statLine: [
          memoryReady ? 'Memory draft is already captured' : 'Memory layer should already be captured before this stage',
          capabilityReady ? 'Capability package draft is ready for rollout' : 'Capability approval was recorded from board history',
          workflowReady ? 'Workflow draft is available to publish' : 'Workflow publication should be prepared next',
        ],
        primaryActionLabel: 'Open workflows',
        secondaryActionLabel: 'Open packages',
        onPrimaryAction: () => onNavigateToTab('workflows', assetNavigationTargetForRole('workflows', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
        onSecondaryAction: () => onNavigateToTab('packages', assetNavigationTargetForRole('packages', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
      };
    }

    if (managementRecoveryState?.phase === 'institutionalizing') {
      return {
        phase: 'memory_review',
        tone: 'info',
        title: `${roleLabel} is in memory review`,
        summary: `${roleLabel} has returned to autonomous execution. The organization should now capture the recovery path into reviewed memory so the next similar case starts from system knowledge instead of human recollection.`,
        detail: 'This is the first institutional step after recovery closes: deposit what worked, define the handling pattern, and create the base material that later becomes a capability package and a published workflow.',
        roleCode,
        roleLabel,
        statLine: [
          memoryReady ? 'Memory draft is already available for review' : 'Memory draft should be reviewed first',
          capabilityReady ? 'Capability draft can be prepared from approved memory' : 'Package approval follows memory review',
          workflowReady ? 'Workflow draft exists but should not skip review sequence' : 'Workflow publish comes after package approval',
        ],
        primaryActionLabel: 'Open knowledge',
        secondaryActionLabel: 'Open packages',
        onPrimaryAction: () => onNavigateToTab('knowledge', assetNavigationTargetForRole('knowledge', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
        onSecondaryAction: () => onNavigateToTab('packages', assetNavigationTargetForRole('packages', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
      };
    }

    if (managementRecoveryState?.phase === 'returning_to_autonomy') {
      return {
        phase: 'memory_review',
        tone: 'info',
        title: `${roleLabel} is approaching memory review`,
        summary: `${roleLabel} is still stabilizing after intervention. If execution remains healthy, this recovery path should be turned into reviewed memory next.`,
        detail: 'The organization should not freeze the lesson too early, but it also should not let a validated recovery disappear into individual worker experience. Watch for stable evidence, then start knowledge review.',
        roleCode,
        roleLabel,
        statLine: [
          'Recovery is back inside delegated execution',
          memoryReady ? 'A memory draft already exists for review' : 'Prepare memory review once recovery stabilizes',
          'Package and workflow layers should wait for confirmed repeatability',
        ],
        primaryActionLabel: 'Open knowledge',
        onPrimaryAction: () => onNavigateToTab('knowledge', assetNavigationTargetForRole('knowledge', roleCode, roleLabel, latestDraft, 'overview_institutionalization_progress')),
      };
    }

    return {
      phase: 'inactive',
      tone: managementRecoveryState?.tone || 'ok',
      title: managementRecoveryState?.label || 'Institutionalization not started',
      summary: managementRecoveryState?.summary || 'The organization is still handling the exception itself, so asset deposition has not started yet.',
      detail: managementRecoveryState?.nextStep || 'Institutionalization begins only after the organization has enough operating evidence to convert the case into durable system assets.',
      roleCode,
      roleLabel,
      statLine: [
        'Memory review starts after recovery is proven',
        'Package approval formalizes reusable capability',
        'Workflow publish turns approved capability into live policy',
      ],
      primaryActionLabel: 'Open communications',
      onPrimaryAction: () => onNavigateToCommunications({ role_code: roleCode, source: 'overview_institutionalization_progress' }),
    };
  }, [managementRecoveryState, effectiveBoardFocus, roles, boardHistory, onNavigateToTab, onNavigateToCommunications]);

  const boardSummaryDecision = useMemo(() => {
    if (topBoardRoles.length === 0) {
      return null;
    }

    const describedRoles = topBoardRoles.map((roleItem) => ({
      roleItem,
      pendingTarget: findBatchTargetForRole(pendingBatchTargets, roleItem.roleCode),
      acceptedTarget: findBatchTargetForRole(acceptedBatchTargets, roleItem.roleCode),
      feedback: roleExecutionFeedback[roleItem.roleCode],
    })).map((item) => ({
      ...item,
      suggestedAction: roleSuggestedAction(item.roleItem, item.pendingTarget, item.acceptedTarget),
    }));

    const firstToStart = describedRoles.find((item) => item.suggestedAction.action === 'start');
    const firstToAccept = describedRoles.find((item) => item.suggestedAction.action === 'accept');
    const firstToDrillDown = describedRoles.find((item) => item.suggestedAction.action === 'open_communications');
    const firstAdvanced = describedRoles.find((item) => item.feedback);
    const firstMonitoring = describedRoles.find((item) => item.suggestedAction.action === 'open_top_target');

    const parts: string[] = [];
    if (firstToStart) {
      parts.push(`Activate ${firstToStart.roleItem.roleCode} first`);
    } else if (firstToAccept) {
      parts.push(`Authorize ${firstToAccept.roleItem.roleCode} first`);
    } else if (firstToDrillDown) {
      parts.push(`Review ${firstToDrillDown.roleItem.roleCode} first`);
    }

    if (managementRecoveryState?.phase === 'recovery_dispatched') {
      parts.push(`${managementRecoveryState.roleCode} is already back in execution after management intervention`);
    } else if (managementRecoveryState?.phase === 'under_review') {
      parts.push(`${managementRecoveryState.roleCode} is still under active management review`);
    } else if (managementRecoveryState?.phase === 'deferred_monitoring') {
      parts.push(`${managementRecoveryState.roleCode} is being observed until the next management review window`);
    } else if (firstAdvanced) {
      parts.push(`${firstAdvanced.roleItem.roleCode} was just moved forward and should stay under management watch`);
    }

    if (firstToDrillDown && (!firstToStart || firstToDrillDown.roleItem.roleCode !== firstToStart.roleItem.roleCode)) {
      parts.push(`${firstToDrillDown.roleItem.roleCode} still needs execution review`);
    } else if (firstToAccept && (!firstToStart || firstToAccept.roleItem.roleCode !== firstToStart.roleItem.roleCode)) {
      parts.push(`${firstToAccept.roleItem.roleCode} is still waiting for delegation approval`);
    } else if (firstMonitoring) {
      parts.push(`${firstMonitoring.roleItem.roleCode} can stay in organizational observation`);
    }

    const primary = firstToStart || firstToAccept || firstToDrillDown || firstMonitoring || null;
    return {
      summary: `Organization summary: ${parts.join(' | ')}.`,
      primary,
    };
  }, [topBoardRoles, pendingBatchTargets, acceptedBatchTargets, roleExecutionFeedback, managementRecoveryState]);
  const executionBoardSummary = boardSummaryDecision?.summary || 'Organization summary: No priority role is currently demanding immediate intervention.';
  const autonomousOperationStatus = useMemo(() => {
    const describedRoles = topBoardRoles.map((roleItem) => ({
      roleItem,
      suggestedAction: roleSuggestedAction(
        roleItem,
        findBatchTargetForRole(pendingBatchTargets, roleItem.roleCode),
        findBatchTargetForRole(acceptedBatchTargets, roleItem.roleCode),
      ),
      feedback: roleExecutionFeedback[roleItem.roleCode],
    }));
    const escalationCount = describedRoles.filter((item) => item.suggestedAction.action === 'open_communications').length;
    const coordinationCount = describedRoles.filter((item) => item.suggestedAction.action === 'accept' || item.suggestedAction.action === 'start').length;
    const recentlyAdvancedCount = describedRoles.filter((item) => item.feedback).length;
    const managementReviewCount = describedRoles.filter((item) => item.suggestedAction.action === 'open_communications' || item.roleItem.risk === 'critical').length;
    const interventionRequired = Boolean(
      escalationCount > 0
      || priorityDecision?.status === 'warn'
      || topBoardRoles.some((item) => item.risk === 'critical'),
    );

    if (interventionRequired) {
      return {
        tone: 'warn',
        label: 'Management attention required',
        summary: `${escalationCount || 1} organizational escalation${escalationCount === 1 ? '' : 's'} currently need management review.`,
        detail: 'The organization is still running autonomously, but at least one high-impact issue has crossed the normal operating boundary.',
        managementReviewCount,
        delegatedCoordinationCount: coordinationCount,
      };
    }

    return {
      tone: 'ok',
      label: 'Autonomous operation running',
      summary: coordinationCount > 0
        ? `${coordinationCount} organizational handoff${coordinationCount === 1 ? '' : 's'} are currently being handled within policy.`
        : 'No management decision is currently required for the organization to keep running.',
      detail: recentlyAdvancedCount > 0
        ? `${recentlyAdvancedCount} role${recentlyAdvancedCount === 1 ? '' : 's'} were recently moved forward while the organization remained inside its delegation boundary.`
        : 'The current workload remains inside delegated operating rules, so iWorkerCenter can keep coordinating execution without waiting for management escalation.',
      managementReviewCount,
      delegatedCoordinationCount: coordinationCount,
    };
  }, [topBoardRoles, pendingBatchTargets, acceptedBatchTargets, roleExecutionFeedback, priorityDecision]);
  const autonomousEscalationTarget = useMemo(() => {
    const describedRoles = topBoardRoles.map((roleItem) => ({
      roleItem,
      pendingTarget: findBatchTargetForRole(pendingBatchTargets, roleItem.roleCode),
      acceptedTarget: findBatchTargetForRole(acceptedBatchTargets, roleItem.roleCode),
    })).map((item) => ({
      ...item,
      suggestedAction: roleSuggestedAction(item.roleItem, item.pendingTarget, item.acceptedTarget),
    }));
    return describedRoles.find((item) => item.suggestedAction.action === 'open_communications')
      || describedRoles.find((item) => item.roleItem.risk === 'critical')
      || null;
  }, [topBoardRoles, pendingBatchTargets, acceptedBatchTargets]);
  const autonomousEscalationReason = useMemo(() => {
    if (!autonomousEscalationTarget) {
      return '';
    }
    if (autonomousEscalationTarget.suggestedAction.action === 'open_communications') {
      return `${autonomousEscalationTarget.roleItem.roleCode} needs management review because execution blockers are no longer being cleared inside normal delegation.`;
    }
    if (autonomousEscalationTarget.roleItem.risk === 'critical') {
      return `${autonomousEscalationTarget.roleItem.roleCode} is at critical operating risk and needs management attention to restore healthy coverage.`;
    }
    if (autonomousEscalationTarget.acceptedTarget || autonomousEscalationTarget.pendingTarget) {
      return `${autonomousEscalationTarget.roleItem.roleCode} still carries a high-impact handoff that should not remain unattended.`;
    }
    return `${autonomousEscalationTarget.roleItem.roleCode} has crossed the normal operating boundary and now needs management review.`;
  }, [autonomousEscalationTarget]);
  const autonomousManagementMove = useMemo(() => {
    if (!autonomousEscalationTarget) {
      return null;
    }
    if (autonomousEscalationTarget.roleItem.risk === 'critical') {
      return {
        tone: 'warn',
        level: 'Immediate intervention',
        label: 'Recommended management move: Intervene',
        detail: `Restore operating coverage for ${autonomousEscalationTarget.roleItem.roleCode} before more execution risk accumulates.`,
      };
    }
    if (autonomousEscalationTarget.pendingTarget) {
      return {
        tone: 'info',
        level: 'Authorization needed',
        label: 'Recommended management move: Authorize',
        detail: `Confirm delegation for ${autonomousEscalationTarget.pendingTarget.title} so the role can keep moving within policy.`,
      };
    }
    if (autonomousEscalationTarget.suggestedAction.action === 'open_communications') {
      return {
        tone: 'info',
        level: 'Review only',
        label: 'Recommended management move: Review',
        detail: `Review blockers and unresolved coordination inside ${autonomousEscalationTarget.roleItem.roleCode} before changing goals or resources.`,
      };
    }
    return {
      tone: 'info',
      level: 'Review only',
      label: 'Recommended management move: Review',
      detail: `Open ${autonomousEscalationTarget.roleItem.roleCode} and verify whether escalation is still required.`,
    };
  }, [autonomousEscalationTarget]);
  const autonomousExpectedOutcome = useMemo(() => {
    if (!autonomousEscalationTarget || !autonomousManagementMove) {
      return '';
    }
    switch (autonomousManagementMove.level) {
      case 'Immediate intervention':
        return `${autonomousEscalationTarget.roleItem.roleCode} should return to healthy operating coverage and stop accumulating unmanaged execution risk.`;
      case 'Authorization needed':
        return `${autonomousEscalationTarget.roleItem.roleCode} should move the waiting handoff back into delegated execution without further escalation.`;
      default:
        return `${autonomousEscalationTarget.roleItem.roleCode} should either return to normal delegation or produce a clearer case for deeper intervention.`;
    }
  }, [autonomousEscalationTarget, autonomousManagementMove]);


  const recentManagementDecisions = useMemo(() => {
    const decisions = [
      ...Object.entries(activeManagementReviews).map(([roleCode, record]) => ({
        roleCode,
        tone: 'warn' as const,
        label: 'Taken into management review',
        detail: record.detail,
        displayTime: record.displayTime,
        recordedAt: record.recordedAt,
      })),
      ...Object.entries(deferredManagementRoles).map(([roleCode, record]) => ({
        roleCode,
        tone: 'info' as const,
        label: 'Deferred until next review',
        detail: record.detail,
        displayTime: record.displayTime,
        recordedAt: record.recordedAt,
      })),
    ];

    const targetRoleCode = autonomousEscalationTarget?.roleItem.roleCode || '';
    return decisions
      .sort((left, right) => {
        if (left.roleCode === targetRoleCode && right.roleCode !== targetRoleCode) {
          return -1;
        }
        if (right.roleCode === targetRoleCode && left.roleCode !== targetRoleCode) {
          return 1;
        }
        return new Date(right.recordedAt).getTime() - new Date(left.recordedAt).getTime();
      })
      .slice(0, 3);
  }, [autonomousEscalationTarget, activeManagementReviews, deferredManagementRoles]);


  const cloudHeartbeat = centerStatus?.cloud_heartbeat;
  const cloudTone = cloudHeartbeatTone(cloudHeartbeat?.status);
  const cloudStatusLabel = cloudHeartbeat?.status || (centerStatus ? 'not_configured' : 'unknown');

  const operatingModeCards = useMemo<OperatingModeCard[]>(() => {
    const inReviewCount = Object.keys(activeManagementReviews).length;
    const deferredCount = Object.keys(deferredManagementRoles).length;
    const depositionCount = boardHistory.filter((item) => item.id.startsWith('deposition-')).length;
    const capabilityCount = boardHistory.filter((item) => item.id.startsWith('capability-')).length;
    const standardCount = boardHistory.filter((item) => item.id.startsWith('standard-')).length;

    return [
      {
        id: 'delegated-ops',
        eyebrow: 'Default operating mode',
        title: autonomousOperationStatus.label,
        tone: autonomousOperationStatus.tone as OperatingModeCard['tone'],
        summary: autonomousOperationStatus.summary,
        detail: 'iWorkerCenter stays as the organizational runtime. Management only crosses into action when the system has reached a real decision boundary that cannot be cleared by delegated execution.',
        statLine: [
          `${boardRoles.length} roles in live operating view`,
          `${autonomousOperationStatus.delegatedCoordinationCount} delegated coordination path${autonomousOperationStatus.delegatedCoordinationCount === 1 ? '' : 's'} active`,
          `${autonomousOperationStatus.managementReviewCount} role${autonomousOperationStatus.managementReviewCount === 1 ? '' : 's'} above normal delegation`,
        ],
        primaryActionLabel: autonomousEscalationTarget ? `Open ${autonomousEscalationTarget.roleItem.roleCode}` : topBoardRoles[0] ? `Open ${topBoardRoles[0].roleCode}` : undefined,
        onPrimaryAction: autonomousEscalationTarget
          ? () => {
            setFocusedOverviewRoleCode(autonomousEscalationTarget.roleItem.roleCode);
            onNavigateToCommunications({ role_code: autonomousEscalationTarget.roleItem.roleCode, source: 'operating_mode_delegated' });
          }
          : topBoardRoles[0]
            ? () => {
              setFocusedOverviewRoleCode(topBoardRoles[0].roleCode);
              onNavigateToCommunications({ role_code: topBoardRoles[0].roleCode, source: 'operating_mode_delegated' });
            }
            : undefined,
      },
      {
        id: 'management-boundary',
        eyebrow: 'Decision boundary',
        title: autonomousEscalationTarget
          ? `Board decision needed for ${autonomousEscalationTarget.roleItem.roleCode}`
          : inReviewCount > 0
            ? 'Management exception loop is active'
            : deferredCount > 0
              ? 'Management has deferred fresh intervention'
              : 'No board intervention is currently required',
        tone: (autonomousEscalationTarget ? 'warn' : inReviewCount > 0 ? 'info' : deferredCount > 0 ? 'info' : 'ok') as OperatingModeCard['tone'],
        summary: autonomousEscalationTarget
          ? autonomousEscalationReason
          : inReviewCount > 0
            ? `${inReviewCount} role${inReviewCount === 1 ? '' : 's'} are still inside the management exception loop while execution evidence returns.`
            : deferredCount > 0
              ? `${deferredCount} role${deferredCount === 1 ? '' : 's'} are being observed until the next management review window.`
              : 'The board can stay in monitoring posture while the organization keeps running through delegated roles and published rules.',
        detail: autonomousEscalationTarget
          ? 'This layer exists for decisions, not execution. Clear the exception boundary, allocate direction, and then push the work back down into the organization.'
          : 'Use this layer only for exception handling, capital allocation, and policy choices. Routine execution should remain below, with iWorkers and human tools carrying the operating load.',
        statLine: [
          `${inReviewCount} active management review${inReviewCount === 1 ? '' : 's'}`,
          `${deferredCount} deferred review window${deferredCount === 1 ? '' : 's'}`,
          `${recentManagementDecisions.length} recent board-visible decision${recentManagementDecisions.length === 1 ? '' : 's'}`,
        ],
        primaryActionLabel: autonomousEscalationTarget ? 'Open decision workspace' : recentManagementDecisions[0] ? `Open ${recentManagementDecisions[0].roleCode}` : undefined,
        onPrimaryAction: autonomousEscalationTarget
          ? () => {
            setFocusedOverviewRoleCode(autonomousEscalationTarget.roleItem.roleCode);
            onNavigateToCommunications({ role_code: autonomousEscalationTarget.roleItem.roleCode, source: 'operating_mode_boundary' });
          }
          : recentManagementDecisions[0]
            ? () => {
              setFocusedOverviewRoleCode(recentManagementDecisions[0].roleCode);
              onNavigateToCommunications({ role_code: recentManagementDecisions[0].roleCode, source: 'operating_mode_boundary' });
            }
            : undefined,
      },
      {
        id: 'institutional-memory',
        eyebrow: 'Institutionalization',
        title: institutionalizationProgressState.title,
        tone: institutionalizationProgressState.tone,
        summary: institutionalizationProgressState.summary,
        detail: institutionalizationProgressState.detail,
        statLine: [
          `Current stage: ${institutionalizationProgressState.phase.replace('_', ' ')}`,
          `${depositionCount} draft deposition event${depositionCount === 1 ? '' : 's'}`,
          `${capabilityCount} capability approval event${capabilityCount === 1 ? '' : 's'}`,
          `${standardCount} workflow publication event${standardCount === 1 ? '' : 's'}`,
        ],
        focusSection: 'actions',
        primaryActionLabel: institutionalizationProgressState.primaryActionLabel,
        secondaryActionLabel: institutionalizationProgressState.secondaryActionLabel,
        onPrimaryAction: institutionalizationProgressState.onPrimaryAction,
        onSecondaryAction: institutionalizationProgressState.onSecondaryAction,
      },
    ];
  }, [
    managementRecoveryState,
    institutionalizationProgressState,
    effectiveBoardFocus,
    roles,
    activeManagementReviews,
    deferredManagementRoles,
    boardHistory,
    autonomousOperationStatus,
    boardRoles.length,
    topBoardRoles,
    autonomousEscalationTarget,
    autonomousEscalationReason,
    recentManagementDecisions,
    onNavigateToCommunications,
    onNavigateToTab,
  ]);

  const overviewSectionClassName = (section: 'briefing' | 'coordination' | 'actions') => focusedOverviewSection === section
    ? 'executive-section-highlight'
    : '';

  const handleAutonomousHandleNow = async () => {
    if (!autonomousEscalationTarget) {
      return;
    }
    const reviewStartedAt = new Date();
    const reviewDisplayTime = reviewStartedAt.toLocaleString('zh-CN', {
      hour12: false,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
    const roleCode = autonomousEscalationTarget.roleItem.roleCode;
    const detail = `Taken into management review at ${reviewDisplayTime}. The role is now under active management attention.`;
    try {
      await recordManagementDecision({
        role_code: roleCode,
        decision_type: 'review',
        detail,
        display_time: reviewDisplayTime,
      });
      await loadOverviewData(true);
      setFocusedOverviewRoleCode(roleCode);
      onNavigateToCommunications({
        role_code: roleCode,
        source: 'autonomous_escalation',
      });
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to record the management review decision.');
    }
  };

  const handleAutonomousDefer = async () => {
    if (!autonomousEscalationTarget) {
      return;
    }
    const deferredAt = new Date();
    const nextReviewAt = new Date(Date.now() + 24 * 60 * 60 * 1000).toLocaleString('zh-CN', {
      hour12: false,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
    const deferredDisplayTime = deferredAt.toLocaleString('zh-CN', {
      hour12: false,
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
    const roleCode = autonomousEscalationTarget.roleItem.roleCode;
    const detail = `Deferred until next review: ${nextReviewAt}. Revisit ${roleCode} if coordination risk continues to rise.`;
    try {
      await recordManagementDecision({
        role_code: roleCode,
        decision_type: 'deferred',
        detail,
        display_time: deferredDisplayTime,
      });
      await loadOverviewData(true);
      setFocusedOverviewRoleCode(roleCode);
      setTaskMessage(`Deferred management handling for ${roleCode}. The organization remains under observation until the next review window.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to record the deferred management decision.');
    }
  };

  const handleRunSkill = async (skill: ExecutiveSkill) => {
    try {
      setSkillLoading(true);
      setSkillError('');
      setTaskMessage('');
      setActiveSkillId(skill.id);
      const result = await runExecutiveSkill(skill.id);
      setActiveSkillResult(result);
      setFocusedOverviewRoleCode(result.focus.role_code || '');
      await loadOverviewData(true);
      if (colleagues.length > 0) {
        setSelectedOwners((prev) => {
          const next = { ...prev };
          result.recommendations.forEach((item) => {
            const key = actionKey(item);
            next[key] = prev[key] || getSuggestedOwnerId(item, colleagues, roles);
          });
          return next;
        });
      }
    } catch (error) {
      setSkillError(error instanceof Error ? error.message : 'Failed to run executive skill');
      setActiveSkillResult(null);
    } finally {
      setSkillLoading(false);
    }
  };

  const handleCreateTask = async (item: ExecutiveAction) => {
    const key = actionKey(item);
    const suggestedOwnerId = getSuggestedOwnerId(item, colleagues, roles) || defaultFromColleagueId;
    const selectedOwnerId = selectedOwners[key] || suggestedOwnerId;
    const roleCode = item.owner_role_code || findRoleForAction(item, roles)?.code || '';
    const route = describeRoute(item, colleagues, roles, selectedOwnerId);
    if (!defaultFromColleagueId || (!selectedOwnerId && !roleCode)) {
      setTaskMessage('No available execution role is ready to receive this organizational action.');
      return;
    }
    try {
      setCreatingActionKey(key);
      setTaskMessage('');
      const fromSkill = actionMatchesSkillRecommendation(item, activeSkillResult);
      await createCollaboration({
        title: item.title,
        description: item.description,
        from_colleague_id: defaultFromColleagueId,
        to_colleague_id: selectedOwnerId !== suggestedOwnerId ? selectedOwnerId : undefined,
        to_role_code: roleCode || undefined,
        priority: 80,
        source_type: fromSkill ? 'executive_skill' : undefined,
        source_skill_id: fromSkill ? activeSkillResult?.skill_id : undefined,
        source_skill_title: fromSkill ? activeSkillResult?.title : undefined,
        source_focus_title: fromSkill ? activeSkillResult?.focus.title : undefined,
        source_focus_role_code: fromSkill ? activeSkillResult?.focus.role_code : undefined,
      });
      await loadOverviewData(true);
      setTaskMessage(`Created an organizational handoff for ${item.title}. ${route}`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to create the organizational handoff.');
    } finally {
      setCreatingActionKey('');
    }
  };

  const handleTransitionTask = async (
    item: ExecutiveAction,
    action: 'accept' | 'start' | 'complete' | 'reject',
  ) => {
    if (!item.linked_task_id) {
      setTaskMessage('The linked organizational handoff is not available for direct coordination.');
      return;
    }

    const busyKey = `${actionKey(item)}-${action}`;
    try {
      setTransitioningActionKey(busyKey);
      setTaskMessage('');
      await transitionCollaboration(item.linked_task_id, action, {
        actor_id: item.owner_role_code || defaultFromColleagueId || 'org_console',
        result: action === 'complete' ? `Completed from organization overview for ${item.title}.` : undefined,
        note: `Updated from organization overview: ${action}`,
      });
      await loadOverviewData(true);
      setTaskMessage(`Organizational handoff ${actionTransitionLabel(action).toLowerCase()}ed for ${item.title}.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to update the organizational handoff.');
    } finally {
      setTransitioningActionKey('');
    }
  };

  const handleHistoryTransitionTask = async (
    item: BoardHistoryItem,
    action: 'accept' | 'start' | 'complete' | 'reject',
  ) => {
    const taskID = item.navigationTarget?.task_id;
    if (!taskID) {
      setTaskMessage('The linked organizational history item is not available for direct coordination.');
      return;
    }

    const busyKey = `${item.id}-${action}`;
    try {
      setTransitioningActionKey(busyKey);
      setTaskMessage('');
      await transitionCollaboration(taskID, action, {
        actor_id: item.clusterRoleCode || defaultFromColleagueId || 'org_console',
        result: action === 'complete' ? `Completed from organization history for ${item.title}.` : undefined,
        note: `Updated from organization history: ${action}`,
      });
      await loadOverviewData(true);
      setTaskMessage(`Organizational handoff ${actionTransitionLabel(action).toLowerCase()}ed from history for ${item.title}.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to update the organizational history handoff.');
    } finally {
      setTransitioningActionKey('');
    }
  };

  const handleBatchTransition = async (
    action: 'accept' | 'start',
    targets: BatchTransitionTarget[],
    batchKey: string,
  ) => {
    if (targets.length === 0) {
      setTaskMessage('No organization-linked handoff is currently waiting for this batch action.');
      return;
    }

    const singleRoleCode = targets.length === 1 ? targets[0].roleCode || '' : '';
    const singleTargetTitle = targets.length === 1 ? targets[0].title : '';

    try {
      setBatchTransitionKey(batchKey);
      setTaskMessage('');
      if (singleRoleCode) {
        setFocusedOverviewRoleCode(singleRoleCode);
      }
      for (const target of targets) {
        await transitionCollaboration(target.taskId, action, {
          actor_id: target.roleCode || defaultFromColleagueId || 'org_console_batch',
          note: `Updated from organization coordination: ${action}`,
        });
      }
      await loadOverviewData(true);
      if (singleRoleCode) {
        setRoleExecutionFeedback((prev) => ({
          ...prev,
          [singleRoleCode]: {
            tone: action === 'start' ? 'ok' : 'info',
            message: action === 'start'
              ? `${singleTargetTitle || 'The top handoff'} is now in active execution under ${singleRoleCode}.`
              : `${singleTargetTitle || 'The top handoff'} has been authorized and is ready for execution under ${singleRoleCode}.`,
          },
        }));
      }
      setTaskMessage(`${targets.length} organization-linked handoff${targets.length !== 1 ? 's' : ''} ${actionTransitionLabel(action).toLowerCase()}ed from organization coordination.`);
    } catch (error) {
      if (singleRoleCode) {
        setRoleExecutionFeedback((prev) => ({
          ...prev,
          [singleRoleCode]: {
            tone: 'warn',
            message: error instanceof Error ? error.message : `Failed to ${action} the current top handoff under ${singleRoleCode}.`,
          },
        }));
      }
      setTaskMessage(error instanceof Error ? error.message : 'Failed to run the organization coordination action.');
    } finally {
      setBatchTransitionKey('');
    }
  };

  const handleConfirmReturnToAutonomy = async (item: BoardHistoryItem) => {
    const roleCode = historyRoleCode(item);
    if (!roleCode) {
      setTaskMessage('The recovery record is missing a role code, so autonomy confirmation cannot be captured here.');
      return;
    }

    const displayTime = formatBoardTimestamp(item.timestamp);
    const detail = `Autonomy return confirmed at ${displayTime}. ${roleCode.toUpperCase()} can leave active management attention and continue inside delegated execution.`;

    try {
      setTransitioningActionKey(`${item.id}-autonomy-return`);
      setTaskMessage('');
      setFocusedOverviewRoleCode(roleCode);
      await confirmReturnToAutonomy({
        role_code: roleCode,
        detail,
        display_time: displayTime,
      });
      setRoleExecutionFeedback((prev) => {
        const next = { ...prev };
        delete next[roleCode];
        return next;
      });
      await loadOverviewData(true);
      setTaskMessage(`Confirmed that ${roleCode} can return to autonomous coordination after recovery completion.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to confirm the return to autonomous execution.');
    } finally {
      setTransitioningActionKey('');
    }
  };
  const handleGenerateDepositionDraft = async (item: ExecutiveAction) => {
    const key = actionKey(item);
    const roleCode = item.owner_role_code || historyRoleCode(latestDepositionDraftForRole(boardHistory, item.owner_role_code));
    if (!roleCode) {
      setTaskMessage('This deposition action is missing a role code, so draft generation cannot be started yet.');
      return;
    }

    try {
      setDepositionDraftKey(key);
      setTaskMessage('');
      setFocusedOverviewRoleCode(roleCode);
      await generateDepositionDrafts({
        role_code: roleCode,
        action_title: item.title,
        detail: item.description,
      });
      await loadOverviewData(true);
      setTaskMessage(`Generated deposition drafts for ${roleCode}. The next step is to review the memory, package, and workflow drafts before rollout.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to generate deposition drafts.');
    } finally {
      setDepositionDraftKey('');
    }
  };

  const handlePublishDepositionWorkflow = async (item: ExecutiveAction) => {
    const key = actionKey(item);
    const roleCode = item.owner_role_code;
    const draft = latestDepositionDraftForRole(boardHistory, roleCode);
    const workflowID = depositionDraftField(draft, 'Workflow draft: ');
    if (!roleCode || !workflowID || workflowID === 'not created') {
      setTaskMessage('No workflow draft is currently available to publish for this recovery standard.');
      return;
    }

    try {
      setPublishingWorkflowKey(key);
      setTaskMessage('');
      setFocusedOverviewRoleCode(roleCode);
      await publishDepositionRollout({
        role_code: roleCode,
        workflow_id: workflowID,
        detail: item.description,
      });
      await loadOverviewData(true);
      setTaskMessage(`Published the recovery workflow standard for ${roleCode}. The organization can now monitor new exceptions under policy.`);
    } catch (error) {
      setTaskMessage(error instanceof Error ? error.message : 'Failed to publish the workflow draft.');
    } finally {
      setPublishingWorkflowKey('');
    }
  };

  const handleBoardSummaryPrimaryAction = async () => {
    const primary = boardSummaryDecision?.primary;
    if (!primary) {
      return;
    }

    setFocusedOverviewRoleCode(primary.roleItem.roleCode);
    switch (primary.suggestedAction.action) {
      case 'start':
        if (primary.acceptedTarget) {
          await handleBatchTransition('start', [primary.acceptedTarget], `batch-start-role-${primary.roleItem.roleCode}`);
        }
        return;
      case 'accept':
        if (primary.pendingTarget) {
          await handleBatchTransition('accept', [primary.pendingTarget], `batch-accept-role-${primary.roleItem.roleCode}`);
        }
        return;
      case 'open_communications':
        onNavigateToCommunications({ role_code: primary.roleItem.roleCode, source: 'organization_summary_primary_action' });
        return;
      case 'open_top_target':
        if (primary.acceptedTarget || primary.pendingTarget) {
          const target = primary.acceptedTarget || primary.pendingTarget;
          if (target) {
            onNavigateToCommunications({
              task_id: target.taskId,
              role_code: primary.roleItem.roleCode,
              source: 'organization_summary_primary_action',
            });
            return;
          }
        }
        onNavigateToCommunications({ role_code: primary.roleItem.roleCode, source: 'organization_summary_primary_action' });
        return;
      default:
        return;
    }
  };

  return (
    <div className="center-page-stack">
      <section className="card section-card executive-hero soft">
        <div className="section-head">
          <div>
            <div className="mini light">{t('overview.executiveTag', { defaultValue: 'Executive Layer' })}</div>
            <h3>{t('overview.executiveTitle', { defaultValue: 'AI Native operating overview' })}</h3>
            <p>
              {briefing?.description || t('overview.executiveDesc', { defaultValue: 'Use iWorkerCenter as the organizational console that helps management see operations, understand risks, and push execution.' })}
            </p>
          </div>
          <span className={badgeClass(briefing?.status || 'info')}>
            {updatedAt || t('overview.live', { defaultValue: 'Live view' })}
          </span>
        </div>
        {briefing ? (
          <div className="executive-summary-row">
            <strong>{briefing.title}</strong>
          </div>
        ) : null}
      </section>

      <div className="metric-grid executive-metric-grid">
        {metrics.map((m) => (
          <MetricCard key={`${m.label}-${m.value}`} label={m.label} value={m.value} hint={m.hint} />
        ))}
      </div>

      <section className="card section-card soft">
        <div className="section-head">
          <div>
            <div className="mini light">Service Runtime</div>
            <h3>iWorkerCenter service status</h3>
            <p>This console is the management surface for the organization runtime. Customer operations stay in iWorkerCenter; iWorkerCloud only manages authorization, compute, skill entitlement, and platform connectivity.</p>
          </div>
          <span className={badgeClass(centerStatus?.status === 'ok' ? 'ok' : 'warn')}>{centerStatus?.status || 'unknown'}</span>
        </div>
        <div className="executive-action-row">
          <span className="badge info">{centerStatus?.runtime_type || 'service'} / {centerStatus?.product_kind || 'iworkercenter'} / {centerStatus?.admin_console || 'web_console'}</span>
          <span className="badge info">Runtime providers: {centerStatus?.provider_count ?? 0}</span>
          <span className="badge info">Provider mode: {centerStatus?.runtime_provider_mode || 'settings'}</span>
          <span className="badge info">Compute source: {centerStatus?.compute_source || 'settings'}</span>
          <span className={badgeClass(centerStatus?.compute_sync_status?.status === 'failure' ? 'warn' : 'ok')}>Compute sync: {centerStatus?.compute_sync_status?.status || 'pending'}</span>
          <span className="badge info">Cloud providers: {centerStatus?.cloud_provider_count ?? 0}</span>
          <span className={badgeClass(cloudTone)}>Cloud heartbeat: {cloudStatusLabel}</span>
          {cloudHeartbeat?.center_id ? <span className="badge info">Center ID: {cloudHeartbeat.center_id}</span> : null}
        </div>
        <div className="item-row">
          <strong>Center -&gt; Cloud connectivity</strong>
          <p>{cloudHeartbeat
            ? `Last success: ${formatBoardTimestamp(cloudHeartbeat.last_success_at || '')}. Consecutive failures: ${cloudHeartbeat.consecutive_failures || 0}.`
            : 'Cloud heartbeat monitor is not configured for this service process yet.'}</p>
          <p>{`Compute runtime: ${centerStatus?.runtime_provider_mode || 'settings'}, source: ${centerStatus?.compute_source || 'settings'}, permission: ${centerStatus?.compute_permission ? 'self-managed allowed' : 'cloud-managed'}.`}</p>
          {centerStatus?.compute_sync_status?.last_sync_at ? <p>{`Last compute sync: ${formatBoardTimestamp(centerStatus.compute_sync_status.last_sync_at)}.`}</p> : null}
          {centerStatus?.compute_sync_status?.error ? <p>{`Compute sync error: ${centerStatus.compute_sync_status.error}`}</p> : null}
          {cloudHeartbeat?.last_error ? <p>{`Last error: ${cloudHeartbeat.last_error}`}</p> : null}
        </div>
      </section>

      <div className="executive-operating-mode-grid">
        {operatingModeCards.map((card) => (
          <section
            key={card.id}
            className={`card section-card executive-operating-mode-card executive-operating-mode-card-${card.tone} ${focusedOverviewSection === card.focusSection ? 'executive-operating-mode-card-active' : ''}`}
            onClick={() => card.focusSection ? setFocusedOverviewSection(card.focusSection) : undefined}
          >
            <div className="executive-operating-mode-head">
              <div>
                <div className="mini light">{card.eyebrow}</div>
                <h3>{card.title}</h3>
              </div>
              <span className={badgeClass(card.tone)}>{card.tone === 'warn' ? 'Decision edge' : card.tone === 'info' ? 'Transition' : 'Stable'}</span>
            </div>
            <strong>{card.summary}</strong>
            <p>{card.detail}</p>
            <div className="executive-operating-mode-stats">
              {card.statLine.map((stat) => (
                <span key={stat}>{stat}</span>
              ))}
            </div>
            {(card.onPrimaryAction || card.onSecondaryAction) ? (
              <div className="executive-action-row">
                {card.onPrimaryAction && card.primaryActionLabel ? (
                  <button type="button" className="executive-assign-button" onClick={(event) => { event.stopPropagation(); card.onPrimaryAction?.(); }}>
                    {card.primaryActionLabel}
                  </button>
                ) : null}
                {card.onSecondaryAction && card.secondaryActionLabel ? (
                  <button type="button" className="executive-link-button" onClick={(event) => { event.stopPropagation(); card.onSecondaryAction?.(); }}>
                    {card.secondaryActionLabel}
                  </button>
                ) : null}
              </div>
            ) : null}
          </section>
        ))}
      </div>
      <SectionCard
        title={t('overview.boardTitle', { defaultValue: 'Organization briefing' })}
        desc={t('overview.boardDesc', { defaultValue: 'A live operating brief that rolls routing health, task pressure, and recent organization intervention history into one management view.' })}
      >
        <div className="item-row">
          <strong>Autonomous operation</strong>
          <span className={badgeClass(autonomousOperationStatus.tone)}>{autonomousOperationStatus.label}</span>
          <p>{autonomousOperationStatus.summary}</p>
          <p>{autonomousOperationStatus.detail}</p>
          <div className="executive-action-row">
            <span className={badgeClass(autonomousOperationStatus.managementReviewCount > 0 ? 'warn' : 'ok')}>
              {`Needs management review: ${autonomousOperationStatus.managementReviewCount}`}
            </span>
            <span className={badgeClass(autonomousOperationStatus.delegatedCoordinationCount > 0 ? 'info' : 'ok')}>
              {`Within delegated coordination: ${autonomousOperationStatus.delegatedCoordinationCount}`}
            </span>
          </div>
          {managementRecoveryState ? (
            <div className="item-row">
              <span className={badgeClass(managementRecoveryState.tone)}>{managementRecoveryState.label}</span>
              <p>{managementRecoveryState.summary}</p>
              <p>{managementRecoveryState.detail}</p>
              <p>{`Next step: ${managementRecoveryState.nextStep}`}</p>
            </div>
          ) : null}
          {autonomousEscalationTarget ? (
            <div className="item-row">
              <span className="badge warn">Why escalation</span>
              <p>{autonomousEscalationReason}</p>
              {autonomousManagementMove ? <span className={badgeClass(autonomousManagementMove.tone)}>{autonomousManagementMove.level}</span> : null}
              {autonomousManagementMove ? <span className={badgeClass(autonomousManagementMove.tone)}>{autonomousManagementMove.label}</span> : null}
              {autonomousManagementMove ? <p>{autonomousManagementMove.detail}</p> : null}
              {autonomousExpectedOutcome ? <p>{`Expected outcome if handled now: ${autonomousExpectedOutcome}`}</p> : null}
              {activeManagementReviews[autonomousEscalationTarget.roleItem.roleCode]?.detail ? <span className="badge warn">Taken into management review</span> : null}
              {activeManagementReviews[autonomousEscalationTarget.roleItem.roleCode]?.detail ? <p>{activeManagementReviews[autonomousEscalationTarget.roleItem.roleCode]?.detail}</p> : null}
              {deferredManagementRoles[autonomousEscalationTarget.roleItem.roleCode]?.detail ? <span className="badge info">Deferred until next review</span> : null}
              {deferredManagementRoles[autonomousEscalationTarget.roleItem.roleCode]?.detail ? <p>{deferredManagementRoles[autonomousEscalationTarget.roleItem.roleCode]?.detail}</p> : null}
              <div className="executive-action-row">
                <button
                  type="button"
                  className="executive-assign-button"
                  onClick={handleAutonomousHandleNow}
                >
                  Handle now: {autonomousEscalationTarget.roleItem.roleCode}
                </button>
                <button
                  type="button"
                  className="executive-link-button"
                  onClick={handleAutonomousDefer}
                >
                  Defer for now
                </button>
              </div>
            </div>
          ) : null}
          <div className="item-row">
            <strong>Institutionalization stage</strong>
            <span className={badgeClass(institutionalizationProgressState.tone)}>{institutionalizationProgressState.title}</span>
            <p>{institutionalizationProgressState.summary}</p>
            <p>{institutionalizationProgressState.detail}</p>
            <div className="executive-action-row">
              {institutionalizationProgressState.statLine.map((stat) => (
                <span key={stat} className="badge info">{stat}</span>
              ))}
            </div>
            {(institutionalizationProgressState.onPrimaryAction || institutionalizationProgressState.onSecondaryAction) ? (
              <div className="executive-action-row">
                {institutionalizationProgressState.onPrimaryAction && institutionalizationProgressState.primaryActionLabel ? (
                  <button
                    type="button"
                    className="executive-assign-button"
                    onClick={() => institutionalizationProgressState.onPrimaryAction?.()}
                  >
                    {institutionalizationProgressState.primaryActionLabel}
                  </button>
                ) : null}
                {institutionalizationProgressState.onSecondaryAction && institutionalizationProgressState.secondaryActionLabel ? (
                  <button
                    type="button"
                    className="executive-link-button"
                    onClick={() => institutionalizationProgressState.onSecondaryAction?.()}
                  >
                    {institutionalizationProgressState.secondaryActionLabel}
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>
        </div>

          {recentManagementDecisions.length > 0 ? (
            <div className="item-row">
              <span className="badge info">Recent management decisions</span>
              {recentManagementDecisions.map((item) => (
                <div key={`${item.roleCode}-${item.label}`} className="item-row">
                  <span className={badgeClass(item.tone)}>{`${item.roleCode} - ${item.label}`}</span>
                  <p>{item.detail}</p>
                  <p>{`Recorded at ${item.displayTime}`}</p>
                </div>
              ))}
            </div>
          ) : null}
        {dynamicBoardSummary ? (
          <div className="item-row">
            <strong>{t('overview.boardSummaryTitle', { defaultValue: 'Organization summary' })}</strong>
            <p>{dynamicBoardSummary}</p>
          </div>
        ) : null}
        <div className="executive-board-grid">
          {boardSignals.map((item) => (
            <div key={item.title} className="executive-board-card">
              <span className={badgeClass(item.tone)}>{item.title}</span>
              <strong>{item.summary}</strong>
              <p>{item.detail}</p>
              {item.navigationTarget ? (
                <button
                  type="button"
                  className="executive-link-button"
                  onClick={() => onNavigateToCommunications(item.navigationTarget!)}
                >
                  Open role workspace
                </button>
              ) : null}
            </div>
          ))}
          <div className="executive-board-card executive-board-card-focus">
            <span className={badgeClass(refreshingBoard ? 'warn' : effectiveBoardFocus?.status || 'info')}>
              {refreshingBoard ? 'Refreshing' : effectiveBoardFocus?.title || 'Operating rhythm'}
            </span>
            <strong>{effectiveBoardFocus?.summary || formatBoardTimestamp(boardUpdatedAt)}</strong>
            <p>
              {refreshingBoard
                ? 'The overview is pulling fresh operating data from routing, audit, and collaboration streams.'
                : effectiveBoardFocus?.description || 'The overview is now carrying live communications telemetry, so leadership can scan the organization before diving into the detailed operations console.'}
            </p>
            {activeSkillResult ? (
              <p>{t('overview.boardFocusMode', { defaultValue: 'This operating focus is currently being driven by the selected executive skill.' })}</p>
            ) : null}
            {effectiveBoardFocus?.role_code ? (
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToCommunications({
                  role_code: effectiveBoardFocus.role_code,
                  source: activeSkillResult ? 'skill_focus' : 'operating_focus',
                })}
              >
                Open role workspace
              </button>
            ) : null}
          </div>
        </div>
      </SectionCard>

      <SectionCard
        title={t('overview.drilldownTitle', { defaultValue: 'Role drill-down' })}
        desc={t('overview.drilldownDesc', { defaultValue: 'A role-level operating cut showing where coverage is thin and where live work is concentrating.' })}
      >
        <div className="executive-role-grid">
          {boardRoles.map((item) => (
            <div
              key={item.roleCode}
              className={`executive-role-card ${focusedOverviewRoleCode === item.roleCode ? 'executive-role-card-focused' : ''}`}
            >
              <div className="executive-role-head">
                <div>
                  <strong>{item.roleName}</strong>
                  <p>{item.roleCode}</p>
                </div>
                <span className={badgeClass(item.risk === 'critical' ? 'warn' : item.risk === 'watch' ? 'info' : 'ok')}>
                  {item.risk}
                </span>
              </div>
              <div className="executive-role-stats">
                <span>Active {item.active}</span>
                <span>Standby {item.standby}</span>
                <span>Unhealthy {item.unhealthy}</span>
                <span>Open tasks {item.openTaskCount}</span>
                <span>Impact {item.impactScore}</span>
              </div>
              <div className="executive-action-row">
                <button
                  type="button"
                  className="executive-link-button"
                  onClick={() => {
                    setFocusedOverviewRoleCode(item.roleCode);
                    onNavigateToCommunications({
                      role_code: item.roleCode,
                      source: 'overview_drilldown',
                    });
                  }}
                >
                  Open role workspace
                </button>
              </div>
            </div>
          ))}
        </div>
      </SectionCard>

      <div className="panel-grid executive-grid">
        <SectionCard
          title={t('overview.risksTitle', { defaultValue: 'Risks and signals' })}
          desc={t('overview.risksDesc', { defaultValue: 'The center should surface where the organization is still fragile or under-managed.' })}
        >
          <div className="item-list">
            {risks.map((item) => {
              const riskRole = findRoleForRisk(item, roles);
              return (
                <div key={item.title} className="item-row">
                  <strong>{item.title}</strong>
                  <p>{item.description}</p>
                  <span className={badgeClass(item.status)}>{item.status}</span>
                  {riskRole ? (
                    <div className="executive-action-row">
                      <button
                        type="button"
                        className="executive-link-button"
                        onClick={() => {
                          setFocusedOverviewRoleCode(riskRole.code);
                          onNavigateToCommunications({
                            role_code: riskRole.code,
                            source: 'overview_risk',
                          });
                        }}
                      >
                        Open role workspace
                      </button>
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        </SectionCard>

        <SectionCard
          title={t('overview.historyTitle', { defaultValue: 'Organization intervention history' })}
          desc={t('overview.historyDesc', { defaultValue: 'Recent management actions and execution movement, ordered as one operating timeline.' })}
        >
          <div className="executive-history-list">
            {boardHistory.length > 0 ? boardHistory.map((item) => {
              const historyStatus = historyExecutionStatus(item);
              const isRecoveryItem = item.id.startsWith('recovery-');
              const historyTransitionOptions = (item.isCluster || isRecoveryItem) ? taskTransitionOptions(historyStatus) : [];
              return (
                <div key={item.id} className="executive-history-item">
                  <span className={badgeClass(item.tone)}>{formatBoardTimestamp(item.timestamp)}</span>
                  <strong>{item.title}</strong>
                  <p>{item.detail}</p>
                  {item.detailLines?.length ? (
                    <div className="executive-action-row">
                      <button
                        type="button"
                        className="executive-link-button"
                        onClick={() => setExpandedHistoryId((current) => current === item.id ? '' : item.id)}
                      >
                        {expandedHistoryId === item.id ? 'Hide chain' : 'Show chain'}
                      </button>
                    </div>
                  ) : null}
                  {expandedHistoryId === item.id && item.detailLines?.length ? (
                    <div className="executive-history-chain">
                      {item.detailLines.map((line) => (
                        <span key={`${item.id}-${line}`}>{line}</span>
                      ))}
                    </div>
                  ) : null}
                  {item.navigationTarget?.task_id || item.navigationTarget?.role_code ? (
                    <button
                      type="button"
                      className="executive-link-button"
                      onClick={() => onNavigateToCommunications(item.navigationTarget!)}
                    >
                      Open role workspace
                    </button>
                  ) : null}
                  {(item.isCluster || isRecoveryItem) && item.navigationTarget?.task_id && historyTransitionOptions.length > 0 ? (
                    <div className="executive-action-row">
                      {historyTransitionOptions.map((transitionAction) => {
                        const busyKey = `${item.id}-${transitionAction}`;
                        return (
                          <button
                            key={busyKey}
                            type="button"
                            className="executive-assign-button"
                            disabled={transitioningActionKey === busyKey}
                            onClick={() => void handleHistoryTransitionTask(item, transitionAction)}
                          >
                            {transitioningActionKey === busyKey ? 'Updating...' : actionTransitionLabel(transitionAction)}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                  {isRecoveryItem && historyStatus === 'done' ? (
                    <div className="executive-action-row">
                      <button
                        type="button"
                        className="executive-link-button"
                        disabled={transitioningActionKey === `${item.id}-autonomy-return`}
                        onClick={() => void handleConfirmReturnToAutonomy(item)}
                      >
                        {transitioningActionKey === `${item.id}-autonomy-return` ? 'Updating...' : 'Confirm return to autonomy'}
                      </button>
                    </div>
                  ) : null}
                </div>
              );
            }) : (
              <div className="executive-history-item">
                <span className="badge info">Standby</span>
                <strong>No organization interventions recorded yet</strong>
                <p>New task creation and routing commands will appear here as the operating history builds up.</p>
              </div>
            )}
          </div>
        </SectionCard>
      </div>

      <div className="panel-grid executive-grid">
        <SectionCard
          title="Organization coordination"
          desc="Authorize delegation and activate queued work across the organization."
        >
          <div className="item-list">
            <div className="item-row">
              <strong>Organization attention summary</strong>
              <p>{executionBoardSummary}</p>
              <div className="executive-action-row">
                <button
                  type="button"
                  className="executive-assign-button"
                  disabled={!boardSummaryDecision?.primary || batchTransitionKey === `batch-start-role-${boardSummaryDecision?.primary?.roleItem.roleCode || ''}` || batchTransitionKey === `batch-accept-role-${boardSummaryDecision?.primary?.roleItem.roleCode || ''}`}
                  onClick={() => void handleBoardSummaryPrimaryAction()}
                >
                  {batchTransitionKey === `batch-start-role-${boardSummaryDecision?.primary?.roleItem.roleCode || ''}` || batchTransitionKey === `batch-accept-role-${boardSummaryDecision?.primary?.roleItem.roleCode || ''}`
                    ? 'Updating...'
                    : boardSummaryDecision?.primary?.suggestedAction.action === 'start'
                      ? `Authorize now: Activate ${boardSummaryDecision.primary.roleItem.roleCode}`
                      : boardSummaryDecision?.primary?.suggestedAction.action === 'accept'
                        ? `Authorize now: Delegate ${boardSummaryDecision.primary.roleItem.roleCode}`
                        : boardSummaryDecision?.primary?.suggestedAction.action === 'open_communications'
                          ? `Review now: ${boardSummaryDecision.primary.roleItem.roleCode}`
                          : boardSummaryDecision?.primary?.roleItem.roleCode
                            ? `Open workspace: ${boardSummaryDecision.primary.roleItem.roleCode}`
                            : 'Do it now'}
                </button>
              </div>
            </div>
            <div className="item-row">
              <strong>Queued organizational handoffs</strong>
              <p>{pendingBatchTargets.length} organization-linked handoff{pendingBatchTargets.length !== 1 ? 's are' : ' is'} waiting for delegation approval right now.</p>
              {topPendingBatchTarget ? <p>Top priority: {topPendingBatchTarget.title}</p> : null}
              {topPendingBatchTarget ? <p>{batchTargetSourceLabel(topPendingBatchTarget)}</p> : null}
              <div className="executive-action-row">
                <button
                  type="button"
                  className="executive-assign-button"
                  disabled={batchTransitionKey === 'batch-accept-top' || !topPendingBatchTarget}
                  onClick={() => topPendingBatchTarget ? void handleBatchTransition('accept', [topPendingBatchTarget], 'batch-accept-top') : undefined}
                >
                  {batchTransitionKey === 'batch-accept-top' ? 'Updating...' : 'Authorize top handoff'}
                </button>
                <button
                  type="button"
                  className="executive-assign-button"
                  disabled={batchTransitionKey === 'batch-accept' || pendingBatchTargets.length === 0}
                  onClick={() => void handleBatchTransition('accept', pendingBatchTargets, 'batch-accept')}
                >
                  {batchTransitionKey === 'batch-accept' ? 'Updating...' : 'Authorize queued handoffs'}
                </button>
              </div>
            </div>
            <div className="item-row">
              <strong>Ready execution handoffs</strong>
              <p>{acceptedBatchTargets.length} organization-linked handoff{acceptedBatchTargets.length !== 1 ? 's are' : ' is'} authorized and ready to move into active execution.</p>
              {topAcceptedBatchTarget ? <p>Top priority: {topAcceptedBatchTarget.title}</p> : null}
              {topAcceptedBatchTarget ? <p>{batchTargetSourceLabel(topAcceptedBatchTarget)}</p> : null}
              <div className="executive-action-row">
                <button
                  type="button"
                  className="executive-assign-button"
                  disabled={batchTransitionKey === 'batch-start-top' || !topAcceptedBatchTarget}
                  onClick={() => topAcceptedBatchTarget ? void handleBatchTransition('start', [topAcceptedBatchTarget], 'batch-start-top') : undefined}
                >
                  {batchTransitionKey === 'batch-start-top' ? 'Updating...' : 'Activate top handoff'}
                </button>
                <button
                  type="button"
                  className="executive-assign-button"
                  disabled={batchTransitionKey === 'batch-start' || acceptedBatchTargets.length === 0}
                  onClick={() => void handleBatchTransition('start', acceptedBatchTargets, 'batch-start')}
                >
                  {batchTransitionKey === 'batch-start' ? 'Updating...' : 'Activate authorized handoffs'}
                </button>
              </div>
            </div>
            {topBoardRoles.length > 0 ? topBoardRoles.map((roleItem) => {
              const rolePendingTarget = findBatchTargetForRole(pendingBatchTargets, roleItem.roleCode);
              const roleAcceptedTarget = findBatchTargetForRole(acceptedBatchTargets, roleItem.roleCode);
              const roleTopTarget = roleAcceptedTarget || rolePendingTarget;
              const suggestedAction = roleSuggestedAction(roleItem, rolePendingTarget, roleAcceptedTarget);
              return (
                <div key={roleItem.roleCode} className="item-row">
                  <strong>{roleItem.roleName}</strong>
                  <p>{roleItem.roleCode} is currently a top operating role in the organization.</p>
                  {roleExecutionFeedback[roleItem.roleCode] ? <span className="badge ok">Recently advanced</span> : null}
                  {roleExecutionFeedback[roleItem.roleCode] ? <p>This role is surfaced first because an organizational decision just changed its execution state.</p> : null}
                  {roleBoardAttentionLabel(suggestedAction) ? <span className={badgeClass(suggestedAction.tone)}>{roleBoardAttentionLabel(suggestedAction)}</span> : null}
                  {roleBoardAttentionLabel(suggestedAction) ? <p>This role still needs organizational follow-through before it can fall back to passive monitoring.</p> : null}
                  <span className={badgeClass(roleItem.risk === 'critical' ? 'warn' : roleItem.risk === 'watch' ? 'info' : 'ok')}>
                    {roleItem.risk}
                  </span>
                  <p>Basis: active {roleItem.active}, standby {roleItem.standby}, unhealthy {roleItem.unhealthy}, open tasks {roleItem.openTaskCount}, impact {roleItem.impactScore}.</p>
                  <span className={badgeClass(suggestedAction.tone)}>{suggestedAction.label}</span>
                  <p>{suggestedAction.detail}</p>
                  {rolePendingTarget ? <p>Queued target: {rolePendingTarget.title}</p> : <p>No pending organization-linked handoff is currently waiting under this role.</p>}
                  {rolePendingTarget ? <p>{batchTargetSourceLabel(rolePendingTarget)}</p> : null}
                  {roleAcceptedTarget ? <p>Ready target: {roleAcceptedTarget.title}</p> : <p>No accepted organization-linked handoff is currently waiting under this role.</p>}
                  {roleAcceptedTarget ? <p>{batchTargetSourceLabel(roleAcceptedTarget)}</p> : null}
                  <div className="executive-action-row">
                    <button
                      type="button"
                      className={suggestedAction.action === 'open_top_target' ? 'executive-assign-button' : 'executive-link-button'}
                      disabled={!roleTopTarget}
                      onClick={() => {
                        if (!roleTopTarget) {
                          return;
                        }
                        setFocusedOverviewRoleCode(roleItem.roleCode);
                        onNavigateToCommunications({
                          task_id: roleTopTarget.taskId,
                          role_code: roleItem.roleCode,
                          source: roleAcceptedTarget ? 'execution_controls_role_ready_target' : 'execution_controls_role_queued_target',
                        });
                      }}
                    >
                      {suggestedAction.action === 'open_top_target' ? 'Suggested: Open top handoff' : 'Open top handoff'}
                    </button>
                    <button
                      type="button"
                      className={suggestedAction.action === 'open_communications' ? 'executive-assign-button' : 'executive-link-button'}
                      onClick={() => {
                        setFocusedOverviewRoleCode(roleItem.roleCode);
                        onNavigateToCommunications({ role_code: roleItem.roleCode, source: 'execution_controls_role' });
                      }}
                    >
                      {suggestedAction.action === 'open_communications' ? 'Suggested: Open role workspace' : 'Open role workspace'}
                    </button>
                    <button
                      type="button"
                      className={suggestedAction.action === 'accept' ? 'executive-assign-button' : 'executive-link-button'}
                      disabled={batchTransitionKey === `batch-accept-role-${roleItem.roleCode}` || !rolePendingTarget}
                      onClick={() => rolePendingTarget ? void handleBatchTransition('accept', [rolePendingTarget], `batch-accept-role-${roleItem.roleCode}`) : undefined}
                    >
                      {batchTransitionKey === `batch-accept-role-${roleItem.roleCode}` ? 'Updating...' : suggestedAction.action === 'accept' ? `Suggested: Authorize top handoff for ${roleItem.roleCode}` : `Authorize top handoff for ${roleItem.roleCode}`}
                    </button>
                    <button
                      type="button"
                      className={suggestedAction.action === 'start' ? 'executive-assign-button' : 'executive-link-button'}
                      disabled={batchTransitionKey === `batch-start-role-${roleItem.roleCode}` || !roleAcceptedTarget}
                      onClick={() => roleAcceptedTarget ? void handleBatchTransition('start', [roleAcceptedTarget], `batch-start-role-${roleItem.roleCode}`) : undefined}
                    >
                      {batchTransitionKey === `batch-start-role-${roleItem.roleCode}` ? 'Updating...' : suggestedAction.action === 'start' ? `Suggested: Activate top handoff for ${roleItem.roleCode}` : `Activate top handoff for ${roleItem.roleCode}`}
                    </button>
                  </div>
                  {roleExecutionFeedback[roleItem.roleCode] ? (
                    <div className="item-row">
                      <span className={badgeClass(roleExecutionFeedback[roleItem.roleCode].tone)}>Org update</span>
                      <p>{roleExecutionFeedback[roleItem.roleCode].message}</p>
                      <p>{roleFollowThroughHint(roleExecutionFeedback[roleItem.roleCode], suggestedAction)}</p>
                    </div>
                  ) : null}
                </div>
              );
            }) : null}
          </div>
        </SectionCard>
      </div>

      <div className="panel-grid executive-grid">
        <SectionCard
          title={t('overview.actionsTitle', { defaultValue: 'Recommended actions' })}
          desc={t('overview.actionsDesc', { defaultValue: 'These are the management moves the system believes should be pushed next.' })}
        >
          <div className="item-list">
            {actions.map((item) => {
              const key = actionKey(item);
              const selectedOwnerId = selectedOwners[key] || getSuggestedOwnerId(item, colleagues, roles) || defaultFromColleagueId;
              const routeDescription = describeRoute(item, colleagues, roles, selectedOwnerId);
              const transitionOptions = actionTransitionOptions(item);
              const latestDraft = latestDepositionDraftForRole(boardHistory, item.owner_role_code);
              const workflowDraftID = depositionDraftField(latestDraft, 'Workflow draft: ');
              const canGenerateDepositionDraft = actionCanGenerateDepositionDraft(item);
              const isDepositionReview = actionIsDepositionReview(item);
              const hasDepositionDraft = Boolean(latestDraft);
              const canPublishWorkflowDraft = (isDepositionReview || canGenerateDepositionDraft) && workflowDraftID !== '' && workflowDraftID !== 'not created';
              return (
                <div key={key} className="item-row">
                  <strong>{item.title}</strong>
                  <p>{item.description}</p>
                  <span className="badge ok">{item.owner_role_label || item.owner}</span>
                  {actionHasLiveTask(item) ? <span className={badgeClass(actionExecutionTone(item))}>{actionExecutionStatusLabel(item)}</span> : null}
                  <p>{routeDescription}</p>
                  <div className="executive-action-row">
                    <button
                      type="button"
                      className="executive-link-button"
                      onClick={() => onNavigateToCommunications(actionNavigationTarget(item))}
                    >
                      {actionOpenLabel(item)}
                    </button>
                    {canGenerateDepositionDraft ? (
                      <>
                        <button
                          type="button"
                          className="executive-assign-button"
                          disabled={depositionDraftKey === key}
                          onClick={() => void handleGenerateDepositionDraft(item)}
                        >
                          {depositionDraftKey === key ? 'Generating...' : 'Generate draft set'}
                        </button>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('knowledge', assetNavigationTargetForTab('knowledge', item, latestDraft))}
                        >
                          Open knowledge
                        </button>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('packages', assetNavigationTargetForTab('packages', item, latestDraft))}
                        >
                          Open packages
                        </button>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('workflows', assetNavigationTargetForTab('workflows', item, latestDraft))}
                        >
                          Open workflows
                        </button>
                        <button
                          type="button"
                          className="executive-assign-button"
                          disabled={!hasDepositionDraft || !canPublishWorkflowDraft || publishingWorkflowKey === key}
                          onClick={() => void handlePublishDepositionWorkflow(item)}
                        >
                          {publishingWorkflowKey === key ? 'Publishing...' : 'Publish workflow draft'}
                        </button>
                      </>
                    ) : isDepositionReview ? (
                      <>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('knowledge', assetNavigationTargetForTab('knowledge', item, latestDraft))}
                        >
                          Open knowledge
                        </button>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('packages', assetNavigationTargetForTab('packages', item, latestDraft))}
                        >
                          Open packages
                        </button>
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToTab('workflows', assetNavigationTargetForTab('workflows', item, latestDraft))}
                        >
                          Open workflows
                        </button>
                        <button
                          type="button"
                          className="executive-assign-button"
                          disabled={!canPublishWorkflowDraft || publishingWorkflowKey === key}
                          onClick={() => void handlePublishDepositionWorkflow(item)}
                        >
                          {publishingWorkflowKey === key ? 'Publishing...' : 'Publish workflow draft'}
                        </button>
                      </>
                    ) : (
                      <>
                        <select
                          value={selectedOwnerId}
                          disabled={actionHasLiveTask(item)}
                          onChange={(event) => setSelectedOwners((prev) => ({ ...prev, [key]: event.target.value }))}
                        >
                          {colleagues.map((colleague) => (
                            <option key={colleague.id} value={colleague.id}>{colleague.name}</option>
                          ))}
                        </select>
                        <button
                          type="button"
                          className="executive-assign-button"
                          disabled={creatingActionKey === key || colleagues.length === 0 || actionHasLiveTask(item)}
                          onClick={() => void handleCreateTask(item)}
                        >
                          {actionCreateLabel(item, creatingActionKey === key)}
                        </button>
                      </>
                    )}
                  </div>
                  {actionCanTransitionDirectly(item) && transitionOptions.length > 0 ? (
                    <div className="executive-action-row">
                      {transitionOptions.map((transitionAction) => {
                        const busyKey = `${key}-${transitionAction}`;
                        return (
                          <button
                            key={busyKey}
                            type="button"
                            className="executive-assign-button"
                            disabled={transitioningActionKey === busyKey}
                            onClick={() => void handleTransitionTask(item, transitionAction)}
                          >
                            {transitioningActionKey === busyKey ? 'Updating...' : actionTransitionLabel(transitionAction)}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        </SectionCard>
      </div>

      <div className="panel-grid executive-grid executive-skills-layout">
        <SectionCard
          title={t('overview.skillsTitle', { defaultValue: 'Executive skills' })}
          desc={t('overview.skillsDesc', { defaultValue: 'Turn recurring management questions into callable capabilities instead of ad hoc reporting.' })}
        >
          <div className="executive-skill-list">
            {skills.map((skill) => (
              <button
                key={skill.id}
                type="button"
                className={`executive-skill-card ${skill.id === activeSkillId ? 'active' : ''}`}
                onClick={() => void handleRunSkill(skill)}
              >
                <strong>{skill.title}</strong>
                <span>{skill.question}</span>
                <p>{skill.description}</p>
              </button>
            ))}
          </div>
        </SectionCard>

        <SectionCard
          title={t('overview.skillResultTitle', { defaultValue: 'Skill output' })}
          desc={t('overview.skillResultDesc', { defaultValue: 'Each skill should produce a conclusion, supporting findings, and recommended actions.' })}
        >
          {skillLoading ? (
            <div className="item-row">
              <strong>{t('common.loading', { defaultValue: 'Loading...' })}</strong>
            </div>
          ) : skillError ? (
            <div className="item-row">
              <strong>{t('common.error', { defaultValue: 'Error' })}</strong>
              <p>{skillError}</p>
            </div>
          ) : activeSkillResult ? (
            <div className="executive-result">
              <div className="item-row">
                <strong>{activeSkillResult.title}</strong>
                <p>{activeSkillResult.summary}</p>
              </div>
              <div className="item-row">
                <strong>{t('overview.skillFocusTitle', { defaultValue: 'Skill focus' })}</strong>
                <span className={badgeClass(activeSkillResult.focus.status)}>{activeSkillResult.focus.title}</span>
                <p>{activeSkillResult.focus.summary}</p>
                <p>{activeSkillResult.focus.description}</p>
                {activeSkillResult.focus.role_code ? (
                  <div className="executive-action-row">
                    <button
                      type="button"
                      className="executive-link-button"
                      onClick={() => onNavigateToCommunications({
                        role_code: activeSkillResult.focus.role_code,
                        source: 'skill_focus',
                      })}
                    >
                      Open role workspace
                    </button>
                  </div>
                ) : null}
              </div>
              <div className="item-list">
                {activeSkillResult.findings.map((finding) => (
                  <div key={finding} className="item-row">
                    <strong>{t('overview.findingLabel', { defaultValue: 'Finding' })}</strong>
                    <p>{finding}</p>
                  </div>
                ))}
              </div>
              <div className="item-list">
                {activeSkillResult.recommendations.map((item) => {
                  const effectiveItem = resolveActionExecution(item, actions);
                  const key = actionKey(item);
                  const selectedOwnerId = selectedOwners[key] || getSuggestedOwnerId(effectiveItem, colleagues, roles) || defaultFromColleagueId;
                  const routeDescription = describeRoute(effectiveItem, colleagues, roles, selectedOwnerId);
                  const transitionOptions = actionTransitionOptions(effectiveItem);
                  return (
                    <div key={key} className="item-row">
                      <strong>{effectiveItem.title}</strong>
                      <p>{effectiveItem.description}</p>
                      <span className="badge ok">{effectiveItem.owner_role_label || effectiveItem.owner}</span>
                      {actionHasLiveTask(effectiveItem) ? <span className={badgeClass(actionExecutionTone(effectiveItem))}>{actionExecutionStatusLabel(effectiveItem)}</span> : null}
                      <p>{routeDescription}</p>
                      <div className="executive-action-row">
                        <button
                          type="button"
                          className="executive-link-button"
                          onClick={() => onNavigateToCommunications(actionNavigationTarget(effectiveItem))}
                        >
                          {actionOpenLabel(effectiveItem)}
                        </button>
                        <select
                          value={selectedOwnerId}
                          disabled={actionHasLiveTask(effectiveItem)}
                          onChange={(event) => setSelectedOwners((prev) => ({ ...prev, [key]: event.target.value }))}
                        >
                          {colleagues.map((colleague) => (
                            <option key={colleague.id} value={colleague.id}>{colleague.name}</option>
                          ))}
                        </select>
                        <button
                          type="button"
                          className="executive-assign-button"
                          disabled={creatingActionKey === key || colleagues.length === 0 || actionHasLiveTask(effectiveItem)}
                          onClick={() => void handleCreateTask(effectiveItem)}
                        >
                          {actionCreateLabel(effectiveItem, creatingActionKey === key)}
                        </button>
                      </div>
                      {actionCanTransitionDirectly(effectiveItem) && transitionOptions.length > 0 ? (
                        <div className="executive-action-row">
                          {transitionOptions.map((transitionAction) => {
                            const busyKey = `${key}-${transitionAction}`;
                            return (
                              <button
                                key={busyKey}
                                type="button"
                                className="executive-assign-button"
                                disabled={transitioningActionKey === busyKey}
                                onClick={() => void handleTransitionTask(effectiveItem, transitionAction)}
                              >
                                {transitioningActionKey === busyKey ? 'Updating...' : actionTransitionLabel(transitionAction)}
                              </button>
                            );
                          })}
                        </div>
                      ) : null}
                    </div>
                  );
                })}
              </div>
              {taskMessage ? (
                <div className="item-row">
                  <strong>Execution</strong>
                  <p>{taskMessage}</p>
                </div>
              ) : null}
            </div>
          ) : (
            <div className="item-row">
              <strong>{t('overview.skillEmptyTitle', { defaultValue: 'No skill selected' })}</strong>
              <p>{t('overview.skillEmptyDesc', { defaultValue: 'Pick one executive question from the left to see the structured output.' })}</p>
            </div>
          )}
        </SectionCard>
      </div>
    </div>
  );
}
















































































































