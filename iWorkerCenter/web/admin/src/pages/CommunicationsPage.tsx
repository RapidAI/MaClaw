import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { SectionCard } from "../components/cards/SectionCard";
import {
  executeRoleRoutingAction,
  getCollaborationEvents,
  getCollaborationRoutingSettings,
  listCollaborations,
  saveCollaborationRoutingSettings,
  transitionCollaboration,
} from "../api/collaboration";
import type {
  ColleagueRuntimeState,
  CollabEvent,
  CollabTask,
  CollaborationRoutingColleagueStatus,
  CollaborationRoutingOverview,
  CollaborationRoutingSettings,
  RoutingStrategy,
} from "../api/collaboration";
import { listAuditLogs } from "../api/audit";
import type { AuditLog } from "../api/audit";
import { listColleagues } from "../api/colleagues";
import type { Colleague } from "../api/colleagues";
import { listRoles } from "../api/roles";
import type { Role } from "../api/roles";
import type { CommunicationsNavigationTarget, OverviewNavigationTarget } from "../types";

type RoleRiskLevel = "stable" | "watch" | "critical";
type RoleActionKind = "promote_standby" | "prefer_primary" | "balance_load";

type RoleHealthSummary = {
  role: Role;
  total: number;
  active: number;
  standby: number;
  unhealthy: number;
  openTaskCount: number;
  impactScore: number;
  priorityScore: number;
  risk: RoleRiskLevel;
  message: string;
  standbyCandidateID: string;
  activeCandidateID: string;
  recommendedAction: RoleActionKind;
  recommendationReason: string;
};

type BoardBriefingItem = {
  title: string;
  tone: "ok" | "info" | "warn";
  summary: string;
  action: string;
};

const statusClass = (status: string) => {
  switch (status) {
    case "pending":
      return "badge warn";
    case "accepted":
    case "in_progress":
      return "badge info";
    case "completed":
    case "done":
      return "badge ok";
    case "rejected":
      return "badge warn";
    default:
      return "badge info";
  }
};

const transitionActions: Record<
  string,
  Array<"accept" | "start" | "complete" | "reject">
> = {
  pending: ["accept", "reject"],
  accepted: ["start", "reject"],
  in_progress: ["complete", "reject"],
};

const actionLabel = (action: "accept" | "start" | "complete" | "reject") => {
  switch (action) {
    case "accept":
      return "Accept";
    case "start":
      return "Start";
    case "complete":
      return "Complete";
    case "reject":
      return "Reject";
  }
};

const routeLabel = (task: CollabTask) => {
  if (task.to_role_code && task.to_colleague_id) {
    return `${task.to_role_code} -> ${task.to_colleague_id}`;
  }
  return task.to_colleague_id || task.to_role_code || "-";
};

const navigationTaskPriority = (task: CollabTask, roleCode?: string) => {
  let score = 0;
  if (roleCode && task.to_role_code === roleCode) {
    score += 100;
  }
  switch (task.status) {
    case "in_progress":
      score += 30;
      break;
    case "accepted":
      score += 20;
      break;
    case "pending":
      score += 10;
      break;
    default:
      break;
  }
  const updatedAt = new Date(task.updated_at || task.created_at).getTime();
  return score * 1_000_000_000_000 + (Number.isNaN(updatedAt) ? 0 : updatedAt);
};

const pickNavigationTask = (
  tasks: CollabTask[],
  target: CommunicationsNavigationTarget,
) => {
  if (target.task_id) {
    const exactTask = tasks.find((task) => task.id === target.task_id);
    if (exactTask) {
      return exactTask;
    }
  }

  if (target.role_code) {
    return [...tasks]
      .filter((task) => task.to_role_code === target.role_code)
      .sort(
        (left, right) =>
          navigationTaskPriority(right, target.role_code) -
          navigationTaskPriority(left, target.role_code),
      )[0] || null;
  }

  return null;
};

const defaultRoutingSettings = (): CollaborationRoutingSettings => ({
  default_strategy: "least_loaded",
  role_strategies: {},
  primary_colleague_by_role: {},
  runtime_state_by_colleague: {},
  last_heartbeat_by_colleague: {},
  heartbeat_timeout_seconds: 90,
});

const defaultRoutingOverview = (): CollaborationRoutingOverview => ({
  settings: defaultRoutingSettings(),
  active_count: 0,
  standby_count: 0,
  unhealthy_count: 0,
  status_by_colleague: {},
});

const runtimeStateLabel = (state: ColleagueRuntimeState) => {
  switch (state) {
    case "standby":
      return "Standby";
    case "unhealthy":
      return "Unhealthy";
    default:
      return "Active";
  }
};

const runtimeStateBadgeClass = (state: ColleagueRuntimeState) => {
  switch (state) {
    case "standby":
      return statusClass("pending");
    case "unhealthy":
      return statusClass("rejected");
    default:
      return statusClass("completed");
  }
};

const effectiveReasonLabel = (reason: string) => {
  switch (reason) {
    case "manual_standby":
      return "Placed in standby manually";
    case "manual_unhealthy":
      return "Taken out of service manually";
    case "heartbeat_timeout":
      return "Heartbeat timeout triggered failover";
    case "heartbeat_healthy":
      return "Heartbeat is healthy";
    default:
      return "Manually active";
  }
};

const formatTimestamp = (value?: string) => {
  if (!value) {
    return "not available";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
};

const riskBadgeClass = (risk: RoleRiskLevel) => {
  switch (risk) {
    case "critical":
      return "badge warn";
    case "watch":
      return "badge info";
    default:
      return "badge ok";
  }
};

const riskLabel = (risk: RoleRiskLevel) => {
  switch (risk) {
    case "critical":
      return "Critical";
    case "watch":
      return "Watch";
    default:
      return "Stable";
  }
};

const roleActionLabel = (action: RoleActionKind) => {
  switch (action) {
    case "promote_standby":
      return "Promote Standby";
    case "prefer_primary":
      return "Prefer Primary";
    default:
      return "Balance Load";
  }
};

const taskImpactWeight = (status: string) => {
  switch (status) {
    case "in_progress":
      return 2;
    case "pending":
    case "accepted":
      return 1;
    default:
      return 0;
  }
};

const riskPriorityWeight = (risk: RoleRiskLevel) => {
  switch (risk) {
    case "critical":
      return 300;
    case "watch":
      return 150;
    default:
      return 0;
  }
};

const parseRoutingAuditDetail = (value: string) => {
  if (!value) {
    return { before: "", after: "", raw: "" };
  }
  const match = value.match(/^before:\s*(.*)\nafter:\s*(.*)$/s);
  if (!match) {
    return { before: "", after: "", raw: value };
  }
  return {
    before: match[1].trim(),
    after: match[2].trim(),
    raw: "",
  };
};

type CommunicationsPageProps = {
  navigationTarget?: CommunicationsNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target: OverviewNavigationTarget) => void;
};

export function CommunicationsPage({
  navigationTarget,
  onNavigationHandled,
  onNavigateToOverview,
}: CommunicationsPageProps) {
  const { t } = useTranslation();
  const [tasks, setTasks] = useState<CollabTask[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState("");
  const [events, setEvents] = useState<CollabEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [eventLoading, setEventLoading] = useState(false);
  const [busyAction, setBusyAction] = useState("");
  const [message, setMessage] = useState("");
  const [roles, setRoles] = useState<Role[]>([]);
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [routingOverview, setRoutingOverview] =
    useState<CollaborationRoutingOverview>(defaultRoutingOverview());
  const [routingSettings, setRoutingSettings] =
    useState<CollaborationRoutingSettings>(defaultRoutingSettings());
  const [routingLoading, setRoutingLoading] = useState(true);
  const [routingMessage, setRoutingMessage] = useState("");
  const [savingRouting, setSavingRouting] = useState(false);
  const [batchExecuting, setBatchExecuting] = useState(false);
  const [refreshingRuntime, setRefreshingRuntime] = useState(false);
  const [lastRuntimeRefreshAt, setLastRuntimeRefreshAt] = useState("");
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [focusedRoleCode, setFocusedRoleCode] = useState('');

  const loadTasks = async (preferredTaskId?: string) => {
    const items = await listCollaborations();
    setTasks(items);
    const nextTaskId = preferredTaskId || selectedTaskId || items[0]?.id || "";
    setSelectedTaskId(nextTaskId);
    return nextTaskId;
  };

  const loadEvents = async (taskId: string) => {
    if (!taskId) {
      setEvents([]);
      return;
    }
    setEventLoading(true);
    try {
      const items = await getCollaborationEvents(taskId);
      setEvents(items);
    } finally {
      setEventLoading(false);
    }
  };

  const loadRouting = async () => {
    const [roleList, colleagueList, overview, logs] = await Promise.all([
      listRoles().catch(() => []),
      listColleagues().catch(() => []),
      getCollaborationRoutingSettings().catch(() => defaultRoutingOverview()),
      listAuditLogs(30).catch(() => []),
    ]);
    setRoles(roleList);
    setColleagues(colleagueList);
    setRoutingOverview(overview);
    setAuditLogs(
      logs.filter((item) => item.work_type === "role_routing_action"),
    );
    setRoutingSettings({
      default_strategy: overview.settings.default_strategy || "least_loaded",
      role_strategies: overview.settings.role_strategies || {},
      primary_colleague_by_role:
        overview.settings.primary_colleague_by_role || {},
      runtime_state_by_colleague:
        overview.settings.runtime_state_by_colleague || {},
      last_heartbeat_by_colleague:
        overview.settings.last_heartbeat_by_colleague || {},
      heartbeat_timeout_seconds:
        overview.settings.heartbeat_timeout_seconds || 90,
    });
    setLastRuntimeRefreshAt(new Date().toISOString());
    setRoutingLoading(false);
  };

  useEffect(() => {
    loadTasks()
      .then((taskId) => loadEvents(taskId))
      .catch(() => {})
      .finally(() => setLoading(false));

    void loadRouting();
  }, []);

  useEffect(() => {
    if (selectedTaskId) {
      void loadEvents(selectedTaskId);
    }
  }, [selectedTaskId]);

  useEffect(() => {
    if (!navigationTarget) {
      return;
    }

    const preferredTask = pickNavigationTask(tasks, navigationTarget);

    if (preferredTask) {
      setSelectedTaskId(preferredTask.id);
      if (navigationTarget.task_id && preferredTask.id === navigationTarget.task_id) {
        setMessage("Focused from overview on the selected collaboration task.");
      } else if (navigationTarget.role_code) {
        setMessage(
          `Focused from ${navigationTarget.source || "overview"} on the highest-priority task for role ${navigationTarget.role_code}.`,
        );
      }
    } else if (navigationTarget.task_id) {
      setMessage("The linked task is not available in the current communications view.");
    }

    if (navigationTarget.role_code) {
      setFocusedRoleCode(navigationTarget.role_code);
      setRoutingMessage(
        `Focused from ${navigationTarget.source || "overview"} on role ${navigationTarget.role_code}.`,
      );
    }

    onNavigationHandled?.();
  }, [navigationTarget, onNavigationHandled, tasks]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setRefreshingRuntime(true);
      void loadRouting().finally(() => setRefreshingRuntime(false));
    }, 15000);

    return () => window.clearInterval(timer);
  }, []);

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedTaskId) || null,
    [tasks, selectedTaskId],
  );

  const handleTransition = async (
    action: "accept" | "start" | "complete" | "reject",
  ) => {
    if (!selectedTask) {
      return;
    }
    try {
      setBusyAction(action);
      setMessage("");
      await transitionCollaboration(selectedTask.id, action, {
        actor_id:
          selectedTask.to_colleague_id || selectedTask.from_colleague_id,
        note: `Updated from communications panel: ${action}`,
      });
      const currentTaskId = await loadTasks(selectedTask.id);
      await loadEvents(currentTaskId);
      setMessage(`Task ${actionLabel(action).toLowerCase()}ed successfully.`);
    } catch (error) {
      setMessage(
        error instanceof Error ? error.message : "Task transition failed.",
      );
    } finally {
      setBusyAction("");
    }
  };

  const handleSaveRouting = async () => {
    try {
      setSavingRouting(true);
      setRoutingMessage("");
      await saveCollaborationRoutingSettings(routingSettings);
      await loadRouting();
      setRoutingMessage("Routing strategy saved.");
    } catch (error) {
      setRoutingMessage(
        error instanceof Error
          ? error.message
          : "Failed to save routing strategy.",
      );
    } finally {
      setSavingRouting(false);
    }
  };

  const handleRefreshRuntime = async () => {
    try {
      setRefreshingRuntime(true);
      setRoutingMessage("");
      await loadRouting();
      setRoutingMessage("Runtime view refreshed.");
    } catch (error) {
      setRoutingMessage(
        error instanceof Error
          ? error.message
          : "Failed to refresh runtime view.",
      );
    } finally {
      setRefreshingRuntime(false);
    }
  };

  const roleColleagues = (role: Role) =>
    colleagues.filter(
      (colleague) =>
        colleague.role_id === role.id || colleague.role_code === role.code,
    );

  const colleagueStatus = (
    colleagueID: string,
  ): CollaborationRoutingColleagueStatus =>
    routingOverview.status_by_colleague[colleagueID] || {
      colleague_id: colleagueID,
      manual_state:
        routingSettings.runtime_state_by_colleague[colleagueID] || "active",
      effective_state:
        routingSettings.runtime_state_by_colleague[colleagueID] || "active",
      reason: "manual_active",
    };

  const manualStateForColleague = (
    colleagueID: string,
  ): ColleagueRuntimeState =>
    routingSettings.runtime_state_by_colleague[colleagueID] || "active";
  const lastHeartbeatForColleague = (colleagueID: string) =>
    routingSettings.last_heartbeat_by_colleague[colleagueID] || "";
  const heartbeatTimeoutCount = Object.values(
    routingOverview.status_by_colleague,
  ).filter((status) => status.reason === "heartbeat_timeout").length;
  const lastRefreshLabel = formatTimestamp(lastRuntimeRefreshAt);

  const workloadByRole = useMemo(() => {
    const result: Record<
      string,
      { openTaskCount: number; impactScore: number }
    > = {};
    for (const task of tasks) {
      const roleCode = task.to_role_code;
      const weight = taskImpactWeight(task.status);
      if (!roleCode || weight === 0) {
        continue;
      }
      if (!result[roleCode]) {
        result[roleCode] = { openTaskCount: 0, impactScore: 0 };
      }
      result[roleCode].openTaskCount += 1;
      result[roleCode].impactScore += weight;
    }
    return result;
  }, [tasks]);

  const roleHealthSummaries = useMemo<RoleHealthSummary[]>(() => {
    return roles
      .map((role) => {
        const members = roleColleagues(role);
        const counts = members.reduce(
          (acc, colleague) => {
            const state = colleagueStatus(colleague.id).effective_state;
            if (state === "standby") {
              acc.standby += 1;
              if (!acc.standbyCandidateID) {
                acc.standbyCandidateID = colleague.id;
              }
            } else if (state === "unhealthy") {
              acc.unhealthy += 1;
            } else {
              acc.active += 1;
              if (!acc.activeCandidateID) {
                acc.activeCandidateID = colleague.id;
              }
            }
            return acc;
          },
          {
            active: 0,
            standby: 0,
            unhealthy: 0,
            standbyCandidateID: "",
            activeCandidateID: "",
          },
        );

        let risk: RoleRiskLevel = "stable";
        let message = "Role has enough active coverage for normal routing.";
        let recommendedAction: RoleActionKind = "balance_load";
        let recommendationReason =
          "Capacity is healthy, so distributed routing remains the best default.";
        const workload = workloadByRole[role.code] || {
          openTaskCount: 0,
          impactScore: 0,
        };

        if (members.length === 0 || counts.active === 0) {
          risk = "critical";
          message =
            counts.standby > 0
              ? "No active operator remains. This role is surviving on standby only."
              : "No healthy active operator remains for this role.";
          recommendedAction =
            counts.standby > 0 ? "promote_standby" : "prefer_primary";
          recommendationReason =
            counts.standby > 0
              ? "A standby operator is available, so promoting it is the fastest way to restore active capacity."
              : "No standby is available, so primary-first routing protects the healthiest remaining route definition.";
        } else if (counts.active === 1 || counts.unhealthy > 0) {
          risk = "watch";
          if (counts.active === 1) {
            message =
              "Only one active operator remains. This role is close to single-point failure.";
            recommendedAction =
              counts.standby > 0 ? "promote_standby" : "prefer_primary";
            recommendationReason =
              counts.standby > 0
                ? "A standby exists and should be promoted before the role falls into a hard single point of failure."
                : "Primary-first routing should lock the current active operator as the preferred path while capacity is thin.";
          } else {
            message =
              "At least one operator is degraded. Capacity is reduced but still serving.";
            recommendedAction = "balance_load";
            recommendationReason =
              "Work should stay distributed across the remaining healthy operators while the degraded node is isolated.";
          }
        }

        return {
          role,
          total: members.length,
          active: counts.active,
          standby: counts.standby,
          unhealthy: counts.unhealthy,
          openTaskCount: workload.openTaskCount,
          impactScore: workload.impactScore,
          priorityScore:
            riskPriorityWeight(risk) +
            workload.impactScore * 10 +
            workload.openTaskCount,
          risk,
          message,
          standbyCandidateID: counts.standbyCandidateID,
          activeCandidateID: counts.activeCandidateID,
          recommendedAction,
          recommendationReason,
        };
      })
      .sort((a, b) => {
        const order: Record<RoleRiskLevel, number> = {
          critical: 0,
          watch: 1,
          stable: 2,
        };
        return (
          order[a.risk] - order[b.risk] ||
          b.impactScore - a.impactScore ||
          b.openTaskCount - a.openTaskCount ||
          a.role.name.localeCompare(b.role.name)
        );
      });
  }, [roles, colleagues, routingOverview, routingSettings, workloadByRole]);

  const criticalRoleCount = roleHealthSummaries.filter(
    (item) => item.risk === "critical",
  ).length;
  const watchRoleCount = roleHealthSummaries.filter(
    (item) => item.risk === "watch",
  ).length;

  const topPriorityRoles = roleHealthSummaries
    .filter((item) => item.risk !== "stable")
    .slice(0, 3);
  const highestImpactRole = roleHealthSummaries.find(
    (item) => item.impactScore > 0,
  );
  const openTaskCount = tasks.filter(
    (task) => taskImpactWeight(task.status) > 0,
  ).length;
  const inProgressTaskCount = tasks.filter(
    (task) => task.status === "in_progress",
  ).length;
  const boardBriefing = useMemo<BoardBriefingItem[]>(() => {
    const items: BoardBriefingItem[] = [];

    if (criticalRoleCount > 0) {
      const topCriticalRole = topPriorityRoles[0];
      items.push({
        title: "Coverage risk",
        tone: "warn",
        summary: topCriticalRole
          ? `${topCriticalRole.role.name} is the sharpest operational risk with ${topCriticalRole.openTaskCount} open task${topCriticalRole.openTaskCount > 1 ? "s" : ""} already waiting on this role.`
          : `${criticalRoleCount} role${criticalRoleCount > 1 ? "s are" : " is"} now in critical coverage risk.`,
        action: topCriticalRole
          ? `Execute ${roleActionLabel(topCriticalRole.recommendedAction)} first on ${topCriticalRole.role.name}.`
          : "Restore active capacity or promote standby immediately.",
      });
    } else {
      items.push({
        title: "Coverage posture",
        tone: watchRoleCount > 0 ? "info" : "ok",
        summary:
          watchRoleCount > 0
            ? `${watchRoleCount} role${watchRoleCount > 1 ? "s are" : " is"} operating with thin redundancy but no role is yet in hard failure.`
            : "All roles currently have healthy active coverage under the present routing policy.",
        action:
          watchRoleCount > 0
            ? "Use the queue below to harden thin roles before they become single-point failures."
            : "Maintain the current routing policy and keep standby warm.",
      });
    }

    items.push({
      title: "Business load",
      tone: highestImpactRole ? "info" : "ok",
      summary: highestImpactRole
        ? `${openTaskCount} open collaboration task${openTaskCount > 1 ? "s are" : " is"} live, with the heaviest load concentrated on ${highestImpactRole.role.name}.`
        : "No open collaboration load is currently pressuring a specific role.",
      action: highestImpactRole
        ? `Protect ${highestImpactRole.role.name} capacity first because it carries impact score ${highestImpactRole.impactScore}.`
        : "No workload rebalancing is needed right now.",
    });

    items.push({
      title: "Runtime discipline",
      tone: heartbeatTimeoutCount > 0 ? "warn" : "ok",
      summary:
        heartbeatTimeoutCount > 0
          ? `${heartbeatTimeoutCount} runtime node${heartbeatTimeoutCount > 1 ? "s have" : " has"} already fallen out of routing because heartbeat policy marked it unhealthy.`
          : "Heartbeat policy is currently aligned with live runtime health.",
      action:
        heartbeatTimeoutCount > 0
          ? "Inspect the affected seat, then decide whether to restore it or keep the role on promoted standby."
          : "Keep heartbeat thresholds as-is and refresh periodically for drift.",
    });

    return items;
  }, [
    criticalRoleCount,
    heartbeatTimeoutCount,
    highestImpactRole,
    openTaskCount,
    topPriorityRoles,
    watchRoleCount,
  ]);

  const applyRoleAction = async (
    summary: RoleHealthSummary,
    action: RoleActionKind,
  ) => {
    try {
      setSavingRouting(true);
      setRoutingMessage("");
      await executeRoleRoutingAction({
        role_code: summary.role.code,
        action,
        actor_id: "board_console",
      });
      await loadRouting();
      if (action === "promote_standby") {
        setRoutingMessage(
          `Standby promotion executed for ${summary.role.name}.`,
        );
      } else if (action === "prefer_primary") {
        setRoutingMessage(
          `Primary-first routing executed for ${summary.role.name}.`,
        );
      } else {
        setRoutingMessage(
          `Least-loaded routing executed for ${summary.role.name}.`,
        );
      }
    } catch (error) {
      setRoutingMessage(
        error instanceof Error
          ? error.message
          : "Failed to execute role action.",
      );
    } finally {
      setSavingRouting(false);
    }
  };

  const executePriorityQueue = async () => {
    if (topPriorityRoles.length === 0) {
      setRoutingMessage(
        "No elevated role risks require batch action right now.",
      );
      return;
    }
    try {
      setBatchExecuting(true);
      setRoutingMessage("");
      for (const item of topPriorityRoles) {
        await executeRoleRoutingAction({
          role_code: item.role.code,
          action: item.recommendedAction,
          actor_id: "board_console_batch",
        });
      }
      await loadRouting();
      setRoutingMessage(
        `Executed ${topPriorityRoles.length} prioritized board action${topPriorityRoles.length > 1 ? "s" : ""}.`,
      );
    } catch (error) {
      setRoutingMessage(
        error instanceof Error
          ? error.message
          : "Failed to execute prioritized actions.",
      );
    } finally {
      setBatchExecuting(false);
    }
  };

  const recentRoutingActions = auditLogs.slice(0, 8);

  const summaryCards = [
    {
      label: "Active routing",
      value: routingOverview.active_count,
      desc: "Receiving work now",
      tone: "ok",
    },
    {
      label: "Standby pool",
      value: routingOverview.standby_count,
      desc: "Ready for takeover",
      tone: "info",
    },
    {
      label: "Out of service",
      value: routingOverview.unhealthy_count,
      desc: "Removed from routing",
      tone: "warn",
    },
    {
      label: "Timeout alerts",
      value: heartbeatTimeoutCount,
      desc: "Auto-failed by heartbeat",
      tone: heartbeatTimeoutCount > 0 ? "warn" : "info",
    },
  ];

  return (
    <div className="center-page-stack">
      <div className="panel-grid communications-layout">
        <SectionCard
          title={t("nav.communications")}
          desc={loading ? t("common.loading") : `${tasks.length} tasks`}
        >
          <div className="item-list">
            {tasks.map((task) => (
              <button
                key={task.id}
                type="button"
                className={`communication-task-card ${task.id === selectedTaskId ? "active" : ""}`}
                onClick={() => setSelectedTaskId(task.id)}
              >
                <div className="communication-task-head">
                  <strong>{task.title}</strong>
                  <span className={statusClass(task.status)}>
                    {task.status}
                  </span>
                </div>
                <p>{task.description || "No description"}</p>
                <span className="communication-task-meta">
                  {task.from_colleague_id} {" -> "} {routeLabel(task)}
                </span>
              </button>
            ))}
          </div>
        </SectionCard>

        <SectionCard
          title="Execution Timeline"
          desc={selectedTask ? selectedTask.title : "Select a task to inspect"}
        >
          {selectedTask ? (
            <div className="communications-detail">
              <div className="item-row">
                <strong>{selectedTask.title}</strong>
                <p>{selectedTask.description || "No description"}</p>
                <span className={statusClass(selectedTask.status)}>
                  {selectedTask.status}
                </span>
              </div>

              <div className="communication-meta-grid">
                <div className="item-row">
                  <strong>From</strong>
                  <p>{selectedTask.from_colleague_id}</p>
                </div>
                <div className="item-row">
                  <strong>Route</strong>
                  <p>{routeLabel(selectedTask)}</p>
                </div>
              </div>

              <div className="executive-action-row">
                {(transitionActions[selectedTask.status] || []).map(
                  (action) => (
                    <button
                      key={action}
                      type="button"
                      className="executive-assign-button"
                      disabled={busyAction === action}
                      onClick={() => void handleTransition(action)}
                    >
                      {busyAction === action
                        ? "Updating..."
                        : actionLabel(action)}
                    </button>
                  ),
                )}
              </div>

              {message ? (
                <div className="item-row">
                  <strong>Update</strong>
                  <p>{message}</p>
                </div>
              ) : null}

              <div className="item-list">
                {eventLoading ? (
                  <div className="item-row">
                    <strong>{t("common.loading")}</strong>
                  </div>
                ) : events.length > 0 ? (
                  events.map((eventItem) => (
                    <div key={eventItem.id} className="item-row">
                      <strong>{eventItem.event}</strong>
                      <p>{eventItem.note || "No note"}</p>
                      <span className="communication-task-meta">
                        {eventItem.actor_id || "-"} at {eventItem.created_at}
                      </span>
                    </div>
                  ))
                ) : (
                  <div className="item-row">
                    <strong>No events yet</strong>
                    <p>
                      The task has been created but no follow-up events are
                      recorded yet.
                    </p>
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="item-row">
              <strong>No task selected</strong>
              <p>
                Select a collaboration task from the left to inspect status and
                execution events.
              </p>
            </div>
          )}
        </SectionCard>
      </div>

      <div className="item-row communications-context-row">
        <strong>Navigation context</strong>
        <p>Use this shortcut to return to the board view while preserving the current role context.</p>
        <div className="executive-action-row">
          <button
            type="button"
            className="executive-link-button"
            onClick={() =>
              onNavigateToOverview?.({
                role_code: focusedRoleCode || selectedTask?.to_role_code || undefined,
                source: "communications",
              })
            }
          >
            Back to Overview
          </button>
          {focusedRoleCode ? (
            <span className="communications-runtime-refresh-label">Focused role: {focusedRoleCode}</span>
          ) : null}
        </div>
      </div>

      <SectionCard
        title="Role Routing Strategy"
        desc={
          routingLoading
            ? "Loading routing settings..."
            : "Operate the digital workforce like a live service: define policy, set standby, and inspect the state the center is actually using."
        }
      >
        <div className="item-list">
          <div className="communications-runtime-grid">
            {summaryCards.map((card) => (
              <div key={card.label} className="communications-runtime-card">
                <span className={`badge ${card.tone}`}>{card.label}</span>
                <strong>{card.value}</strong>
                <p>{card.desc}</p>
              </div>
            ))}
            <div className="communications-runtime-card">
              <span className="badge info">Heartbeat window</span>
              <strong>{routingSettings.heartbeat_timeout_seconds}s</strong>
              <p>Timeout threshold before automatic failover.</p>
            </div>
          </div>

          <div
            className={`item-row ${heartbeatTimeoutCount > 0 || criticalRoleCount > 0 ? "communications-alert-row" : ""}`}
          >
            <strong>Operations signal</strong>
            <p>
              {criticalRoleCount > 0
                ? `${criticalRoleCount} role${criticalRoleCount > 1 ? "s are" : " is"} currently in critical coverage risk. The board should add active capacity or promote standby immediately.${highestImpactRole ? ` Highest live business impact: ${highestImpactRole.role.name} with ${highestImpactRole.openTaskCount} open task${highestImpactRole.openTaskCount > 1 ? "s" : ""}.` : ""}`
                : heartbeatTimeoutCount > 0
                  ? `${heartbeatTimeoutCount} runtime node${heartbeatTimeoutCount > 1 ? "s are" : " is"} currently degraded by heartbeat timeout and already excluded from active routing.`
                  : highestImpactRole
                    ? `All observed runtime nodes are within heartbeat policy, and no role is currently in critical coverage risk. Highest live workload remains on ${highestImpactRole.role.name} with ${highestImpactRole.openTaskCount} open task${highestImpactRole.openTaskCount > 1 ? "s" : ""}.`
                    : "All observed runtime nodes are within heartbeat policy, and no role is currently in critical coverage risk."}
            </p>
            <div className="executive-action-row">
              <span
                className={`badge ${criticalRoleCount > 0 || heartbeatTimeoutCount > 0 ? "warn" : "ok"}`}
              >
                {criticalRoleCount > 0 || heartbeatTimeoutCount > 0
                  ? "Attention needed"
                  : "Stable now"}
              </span>
              <span className="communications-runtime-refresh-label">
                Critical roles: {criticalRoleCount} | Watch roles:{" "}
                {watchRoleCount}
              </span>
              <span className="communications-runtime-refresh-label">
                Last refresh: {lastRefreshLabel}
              </span>
              <button
                type="button"
                className="executive-assign-button"
                disabled={refreshingRuntime}
                onClick={() => void handleRefreshRuntime()}
              >
                {refreshingRuntime ? "Refreshing..." : "Refresh Runtime"}
              </button>
            </div>
          </div>

          <div className="item-row communications-briefing-row">
            <div>
              <strong>Board briefing</strong>
              <p>
                A compact operating brief for the CEO and board: where the
                organization is exposed, where business load is accumulating,
                and what intervention should happen next.
              </p>
            </div>
            <div className="communications-briefing-grid">
              {boardBriefing.map((item) => (
                <div key={item.title} className="communications-briefing-card">
                  <span className={`badge ${item.tone}`}>{item.title}</span>
                  <strong>{item.summary}</strong>
                  <p>{item.action}</p>
                </div>
              ))}
              <div className="communications-briefing-card communications-briefing-card-accent">
                <span className="badge info">Immediate focus</span>
                <strong>
                  {topPriorityRoles[0]
                    ? `${topPriorityRoles[0].role.name} should be the next board intervention target.`
                    : "No urgent intervention target is queued right now."}
                </strong>
                <p>
                  {topPriorityRoles[0]
                    ? `${roleActionLabel(topPriorityRoles[0].recommendedAction)} is the fastest stabilizing move, while ${inProgressTaskCount} task${inProgressTaskCount > 1 ? "s are" : " is"} already in execution across the organization.`
                    : `The digital workforce is operating within policy, with ${inProgressTaskCount} task${inProgressTaskCount > 1 ? "s" : ""} currently in progress.`}
                </p>
              </div>
            </div>
          </div>

          <div className="item-row">
            <strong>Priority action queue</strong>
            <p>
              The board queue ranks the highest-risk roles first and recommends
              the fastest stabilizing move for each one.
            </p>
            <div className="communications-priority-list">
              {topPriorityRoles.length > 0 ? (
                topPriorityRoles.map((item, index) => (
                  <div
                    key={item.role.id}
                    className="communications-priority-item"
                  >
                    <strong>
                      {index + 1}. {item.role.name}
                    </strong>
                    <span className={riskBadgeClass(item.risk)}>
                      {riskLabel(item.risk)}
                    </span>
                    <p>
                      Recommended: {roleActionLabel(item.recommendedAction)}
                    </p>
                    <div className="communications-priority-metrics">
                      <span>Open tasks {item.openTaskCount}</span>
                      <span>Impact score {item.impactScore}</span>
                      <span>Priority score {item.priorityScore}</span>
                    </div>
                    <span className="communication-task-meta">
                      {item.recommendationReason}
                    </span>
                  </div>
                ))
              ) : (
                <div className="communications-priority-item">
                  <strong>No queued role actions</strong>
                  <p>
                    All roles are currently stable enough to operate without
                    prioritized intervention.
                  </p>
                </div>
              )}
            </div>
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-assign-button"
                disabled={batchExecuting || topPriorityRoles.length === 0}
                onClick={() => void executePriorityQueue()}
              >
                {batchExecuting
                  ? "Executing queue..."
                  : "Execute Top Recommendations"}
              </button>
            </div>
          </div>

          <div className="item-row">
            <strong>Role capacity watch</strong>
            <p>
              This board rolls individual employee states into role-level
              service health, so management can see where capacity is stable,
              thin, or already relying on standby.
            </p>
          </div>

          <div className="communications-role-grid">
            {roleHealthSummaries.map((summary) => (
              <div
                key={summary.role.id}
                className={`communications-role-card communications-role-card-${summary.risk} ${focusedRoleCode === summary.role.code ? "communications-role-card-focused" : ""}`}
              >
                <div className="communications-role-head">
                  <div>
                    <strong>{summary.role.name}</strong>
                    <p>{summary.role.code}</p>
                  </div>
                  <div className="communications-role-head-badges">
                    {focusedRoleCode === summary.role.code ? (
                      <span className="badge info">Focused</span>
                    ) : null}
                    <span className={riskBadgeClass(summary.risk)}>
                      {riskLabel(summary.risk)}
                    </span>
                  </div>
                </div>
                <div className="communications-role-stats">
                  <span>Active {summary.active}</span>
                  <span>Standby {summary.standby}</span>
                  <span>Unhealthy {summary.unhealthy}</span>
                  <span>Open tasks {summary.openTaskCount}</span>
                  <span>Impact {summary.impactScore}</span>
                  <span>Total {summary.total}</span>
                </div>
                <p className="communications-role-message">{summary.message}</p>
                <div className="communications-role-recommendation">
                  <span className="badge info">Recommended</span>
                  <strong>{roleActionLabel(summary.recommendedAction)}</strong>
                  <p>{summary.recommendationReason}</p>
                </div>
                <div className="communications-role-actions">
                  <button
                    type="button"
                    className={`executive-assign-button communications-role-action ${summary.recommendedAction === "promote_standby" ? "communications-role-action-recommended" : ""}`}
                    disabled={!summary.standbyCandidateID}
                    onClick={() =>
                      void applyRoleAction(summary, "promote_standby")
                    }
                  >
                    Promote Standby
                  </button>
                  <button
                    type="button"
                    className={`communications-secondary-button ${summary.recommendedAction === "prefer_primary" ? "communications-role-action-recommended" : ""}`}
                    disabled={
                      !summary.activeCandidateID && !summary.standbyCandidateID
                    }
                    onClick={() =>
                      void applyRoleAction(summary, "prefer_primary")
                    }
                  >
                    Prefer Primary
                  </button>
                  <button
                    type="button"
                    className={`communications-secondary-button ${summary.recommendedAction === "balance_load" ? "communications-role-action-recommended" : ""}`}
                    onClick={() =>
                      void applyRoleAction(summary, "balance_load")
                    }
                  >
                    Balance Load
                  </button>
                </div>
              </div>
            ))}
          </div>

          <div className="item-row">
            <strong>Global policy</strong>
            <p>
              Default routing decides how the center dispatches work inside the
              same role, while heartbeat keeps execution routing aligned with
              node health.
            </p>
            <div className="executive-action-row">
              <label>
                Default strategy
                <select
                  value={routingSettings.default_strategy}
                  onChange={(event) =>
                    setRoutingSettings((prev) => ({
                      ...prev,
                      default_strategy: event.target.value as RoutingStrategy,
                    }))
                  }
                >
                  <option value="least_loaded">least_loaded</option>
                  <option value="primary_first">primary_first</option>
                </select>
              </label>
              <label>
                Heartbeat timeout (s)
                <input
                  type="number"
                  min={10}
                  step={10}
                  value={routingSettings.heartbeat_timeout_seconds}
                  onChange={(event) =>
                    setRoutingSettings((prev) => ({
                      ...prev,
                      heartbeat_timeout_seconds:
                        Number(event.target.value) || 90,
                    }))
                  }
                />
              </label>
              <button
                type="button"
                className="executive-assign-button"
                disabled={savingRouting || batchExecuting}
                onClick={() => void handleSaveRouting()}
              >
                {savingRouting ? "Saving..." : "Save Strategy"}
              </button>
            </div>
          </div>

          {roles.map((role) => {
            const candidates = roleColleagues(role);
            const roleStrategy =
              routingSettings.role_strategies[role.code] ||
              routingSettings.default_strategy;
            return (
              <div key={role.id} className="item-row">
                <strong>{role.name}</strong>
                <p>{role.code}</p>
                <div className="executive-action-row">
                  <select
                    value={roleStrategy}
                    onChange={(event) =>
                      setRoutingSettings((prev) => ({
                        ...prev,
                        role_strategies: {
                          ...prev.role_strategies,
                          [role.code]: event.target.value as RoutingStrategy,
                        },
                      }))
                    }
                  >
                    <option value="least_loaded">least_loaded</option>
                    <option value="primary_first">primary_first</option>
                  </select>
                  <select
                    value={
                      routingSettings.primary_colleague_by_role[role.code] || ""
                    }
                    onChange={(event) =>
                      setRoutingSettings((prev) => ({
                        ...prev,
                        primary_colleague_by_role: {
                          ...prev.primary_colleague_by_role,
                          [role.code]: event.target.value,
                        },
                      }))
                    }
                  >
                    <option value="">No primary</option>
                    {candidates.map((colleague) => (
                      <option key={colleague.id} value={colleague.id}>
                        {colleague.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            );
          })}

          <div className="item-row">
            <strong>Runtime seats</strong>
            <p>
              Manual state is your operating intent. Effective state is what the
              center actually uses after applying heartbeat and failover policy.
            </p>
          </div>

          <div className="communications-seat-grid">
            {colleagues.map((colleague) => {
              const status = colleagueStatus(colleague.id);
              return (
                <div key={colleague.id} className="communications-seat-card">
                  <div className="communications-seat-head">
                    <div>
                      <strong>{colleague.name}</strong>
                      <p>{colleague.role_name || colleague.role_code || "-"}</p>
                    </div>
                    <span
                      className={runtimeStateBadgeClass(status.effective_state)}
                    >
                      {runtimeStateLabel(status.effective_state)}
                    </span>
                  </div>

                  <div className="communications-seat-meta">
                    <span>
                      Manual: {runtimeStateLabel(status.manual_state)}
                    </span>
                    <span>
                      Effective: {runtimeStateLabel(status.effective_state)}
                    </span>
                  </div>

                  <p className="communications-seat-reason">
                    {effectiveReasonLabel(status.reason)}
                  </p>

                  <div className="communications-seat-meta">
                    <span>
                      Heartbeat:{" "}
                      {formatTimestamp(lastHeartbeatForColleague(colleague.id))}
                    </span>
                  </div>

                  <select
                    value={manualStateForColleague(colleague.id)}
                    onChange={(event) =>
                      setRoutingSettings((prev) => ({
                        ...prev,
                        runtime_state_by_colleague: {
                          ...prev.runtime_state_by_colleague,
                          [colleague.id]: event.target
                            .value as ColleagueRuntimeState,
                        },
                      }))
                    }
                  >
                    <option value="active">active</option>
                    <option value="standby">standby</option>
                    <option value="unhealthy">unhealthy</option>
                  </select>
                </div>
              );
            })}
          </div>

          <div className="item-row">
            <strong>Recent routing commands</strong>
            <p>
              Live audit trail for board-level routing actions executed from
              this control surface.
            </p>
            <div className="communications-audit-list">
              {recentRoutingActions.length > 0 ? (
                recentRoutingActions.map((log) => {
                  const detail = parseRoutingAuditDetail(log.error_msg);
                  return (
                    <div key={log.id} className="communications-audit-item">
                      <strong>{log.summary || "role routing action"}</strong>
                      <span
                        className={`badge ${log.status === "ok" ? "ok" : "warn"}`}
                      >
                        {log.status}
                      </span>
                      <p>
                        {log.provider_id} | {log.model}
                      </p>
                      {detail.before || detail.after ? (
                        <div className="communications-audit-diff">
                          <div className="communications-audit-snapshot">
                            <span className="badge info">Before</span>
                            <p className="communications-audit-detail">
                              {detail.before || "not recorded"}
                            </p>
                          </div>
                          <div className="communications-audit-snapshot">
                            <span className="badge ok">After</span>
                            <p className="communications-audit-detail">
                              {detail.after || "not recorded"}
                            </p>
                          </div>
                        </div>
                      ) : detail.raw ? (
                        <p className="communications-audit-detail">
                          {detail.raw}
                        </p>
                      ) : null}
                      <span className="communication-task-meta">
                        {formatTimestamp(log.created_at)}
                      </span>
                    </div>
                  );
                })
              ) : (
                <div className="communications-audit-item">
                  <strong>No routing commands yet</strong>
                  <p>
                    Executed board actions will appear here for quick review.
                  </p>
                </div>
              )}
            </div>
          </div>

          {routingMessage ? (
            <div className="item-row">
              <strong>Routing</strong>
              <p>{routingMessage}</p>
            </div>
          ) : null}
        </div>
      </SectionCard>
    </div>
  );
}


