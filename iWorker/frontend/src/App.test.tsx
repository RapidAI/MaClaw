import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import App from "./App";
import i18n from "./i18n";
import {
  AckGoalPush,
  ApplyCenterEnrollment,
  AutoHandleGoalPush,
  CheckCenterHealth,
  DeleteWorkerMemory,
  DiscoverCenterEnrollment,
  FetchAgentInstances,
  FetchConfigBundle,
  FetchCollaborations,
  FetchGoalPushes,
  FetchInstalledTools,
  FetchWorkflowInstances,
  FetchWorkerMemoryStats,
  GetGoalWatchAutoHandleStatus,
  HeartbeatAgentRuntime,
  LoadDiWorkerSettings,
  LoadTaskHistory,
  RecallWorkerMemories,
  RecoverGoalPush,
  SaveDiWorkerSettings,
  SaveTaskHistory,
  SaveWorkerMemory,
  SubmitTask,
  TransitionCollaborationTask,
  TransitionWorkflowStep,
} from "../wailsjs/go/main/App";

vi.mock("../wailsjs/go/main/App", () => ({
  AckGoalPush: vi.fn(),
  ApplyCenterEnrollment: vi.fn(),
  AutoHandleGoalPush: vi.fn(),
  CheckCenterHealth: vi.fn(),
  DeleteWorkerMemory: vi.fn(),
  DiscoverCenterEnrollment: vi.fn(),
  FetchAgentInstances: vi.fn(),
  FetchConfigBundle: vi.fn(),
  FetchCollaborations: vi.fn(),
  FetchGoalPushes: vi.fn(),
  FetchInstalledTools: vi.fn(),
  FetchWorkflowInstances: vi.fn(),
  FetchWorkerMemoryStats: vi.fn(),
  GetGoalWatchAutoHandleStatus: vi.fn(),
  HeartbeatAgentRuntime: vi.fn(),
  LoadDiWorkerSettings: vi.fn(),
  LoadTaskHistory: vi.fn(),
  RecallWorkerMemories: vi.fn(),
  RecoverGoalPush: vi.fn(),
  SaveDiWorkerSettings: vi.fn(),
  SaveTaskHistory: vi.fn(),
  SaveWorkerMemory: vi.fn(),
  SubmitTask: vi.fn(),
  TransitionCollaborationTask: vi.fn(),
  TransitionWorkflowStep: vi.fn(),
}));

const settingsFixture = {
  role_profile: {
    name: "Xiao Di",
    description:
      "Digital office colleague for notices, notes, reports, and operational summaries.",
  },
  center: {
    enabled: true,
    host: "127.0.0.1",
    port: 9377,
    base_url: "http://127.0.0.1:9377",
    tenant_id: "acme",
    department_id: "ops",
    worker_id: "worker-1",
    timeout_sec: 60,
  },
  routing: {
    mode: "smart",
    default_provider: "office-openai",
    allow_fallback: true,
  },
  providers: [
    {
      id: "office-openai",
      name: "Office writing service",
      enabled: true,
      protocol: "openai",
      base_url: "https://office.example.com/v1",
      api_key: "",
      model: "gpt-4.1",
      priority: 100,
      features: ["docs", "minutes"],
      description: "Office writing provider for notices, minutes, and reports.",
      capabilities: {
        supports_stream: true,
        supports_vision: false,
        max_context: 128000,
      },
    },
  ],
};

const installWailsBridge = () => {
  Object.defineProperty(window, "go", {
    value: {
      main: {
        App: {
          GetWelcomeData: vi.fn().mockResolvedValue({ quick_tasks: [] }),
        },
      },
    },
    configurable: true,
    writable: true,
  });
};

describe("App", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.changeLanguage("en");
    vi.clearAllMocks();
    vi.stubGlobal(
      "matchMedia",
      vi.fn().mockImplementation(() => ({
        matches: false,
        media: "",
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })) as unknown as typeof window.matchMedia,
    );
    installWailsBridge();

    vi.mocked(AckGoalPush).mockResolvedValue(undefined as never);
    vi.mocked(RecoverGoalPush).mockResolvedValue(undefined as never);
    vi.mocked(DiscoverCenterEnrollment).mockResolvedValue({
      base_url: "http://127.0.0.1:9377",
      selected_tenant_id: "acme",
      tenants: [{ id: "acme", company_name: "Acme" }],
      roles: [
        {
          id: "role-ops",
          name: "Xiao Di",
          code: "ops",
          description: "Operations role",
          default_strengths: [],
          applicable_tasks: [],
        },
      ],
      colleagues: [
        {
          id: "worker-ops",
          name: "Xiao Di",
          avatar: "",
          role_id: "role-ops",
          role_name: "Xiao Di",
          role_code: "ops",
          description: "Runs operations work",
          strengths: ["ops"],
          tasks: ["daily brief"],
        },
      ],
    } as never);
    vi.mocked(ApplyCenterEnrollment).mockResolvedValue({
      ...settingsFixture,
      center: {
        ...settingsFixture.center,
        worker_id: "worker-ops",
        department_id: "ops",
      },
      role_profile: { name: "Xiao Di", description: "Runs operations work" },
    } as never);
    vi.mocked(AutoHandleGoalPush).mockResolvedValue(undefined as never);
    vi.mocked(FetchAgentInstances).mockResolvedValue([] as never);
    vi.mocked(FetchConfigBundle).mockResolvedValue({
      id: "cfgb-1",
      version: 3,
      content_type: "full",
      payload: '{"local_continuity":true}',
      status: "published",
      note: "test bundle",
      created_at: "2026-05-01T00:00:00Z",
      published_at: "2026-05-01T00:01:00Z",
      source: "center",
      cached_at: "2026-05-04T00:00:00Z",
      stale: false,
      apply_status: "success",
      apply_message: "iWorker fetched and cached the published config bundle",
    } as never);
    vi.mocked(FetchCollaborations).mockResolvedValue([] as never);
    vi.mocked(TransitionCollaborationTask).mockImplementation(async (taskId, action, result) => ({
      id: taskId,
      title: taskId,
      description: "",
      from_colleague_id: "worker-office",
      to_colleague_id: "worker-ops",
      to_role_code: "ops",
      status: action === "accept" ? "accepted" : action === "start" ? "in_progress" : action === "reject" ? "rejected" : "completed",
      priority: 0,
      result: result || "",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:03:00Z",
    }) as never);
    vi.mocked(TransitionWorkflowStep).mockImplementation(async (stepId, action, result) => ({
      status: "ok",
      step: {
        id: stepId,
        instance_id: "wf-1",
        step_definition_id: stepId,
        assignee_colleague_id: "worker-1",
        collaboration_task_id: "",
        status: action === "complete" ? "completed" : action === "reject" ? "rejected" : "in_progress",
        result: result || "",
        sort_order: 1,
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:03:00Z",
      },
      instance: {
        id: "wf-1",
        definition_id: "def-onboarding",
        title: "Approve onboarding workflow",
        initiator_id: "center-admin",
        current_step_id: stepId,
        current_step_assignee_colleague_id: "worker-1",
        status: action === "reject" ? "rejected" : "running",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:03:00Z",
        source: "center",
        cached_at: "2026-05-01T00:03:00Z",
        stale: false,
      },
    }) as never);
    vi.mocked(FetchGoalPushes).mockResolvedValue([] as never);
    vi.mocked(FetchInstalledTools).mockResolvedValue({
      source: "center",
      cached_at: "",
      stale: false,
      skills: [
        {
          capability_id: "skill-brief",
          name: "Brief Writer",
          source: "iWorkerCenter",
          version: "1.0.0",
          risk_level: "low",
          entry: {
            name: "Brief Writer",
            description: "Writes operating briefs",
            triggers: ["brief"],
          },
        },
      ],
      mcp_servers: [
        {
          id: "mcp-crm",
          name: "CRM MCP",
          description: "CRM lookup",
          server_type: "http",
          endpoint: "https://mcp.example.com",
          args: [],
          env_keys: ["CRM_TOKEN"],
          department_id: "ops",
          risk_level: "medium",
          status: "enabled",
          installed_at: "2026-05-01T00:00:00Z",
        },
      ],
    } as never);
    vi.mocked(FetchWorkflowInstances).mockResolvedValue([] as never);
    vi.mocked(GetGoalWatchAutoHandleStatus).mockResolvedValue({
      enabled: true,
      running: false,
      current_run_id: 0,
      run_count: 0,
      skip_count: 0,
      timeout_cancel_count: 0,
      last_handled_count: 0,
      total_handled_count: 0,
      last_error: "",
      last_started_at: "",
      last_finished_at: "",
      last_timeout_at: "",
      interval_seconds: 30,
      max_duration_seconds: 120,
    } as never);
    vi.mocked(HeartbeatAgentRuntime).mockResolvedValue(undefined as never);
    vi.mocked(DeleteWorkerMemory).mockResolvedValue(undefined as never);
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: "ok",
      provider_count: 3,
      config_path: "/tmp/center.json",
      message: "ok",
      resolved_base_url: "http://127.0.0.1:9377",
      iworker_readiness: {
        ready: true,
        status: "ready",
        tenant_count: 1,
        role_count: 1,
        colleague_count: 1,
        local_account_count: 1,
        agent_runtime_ready: true,
        goalwatch_ready: true,
        required_client_paths: ["/client/goalwatch/pushes"],
        checks: [
          { name: "database", ready: true, status: "ready" },
          { name: "tenant", ready: true, status: "ready", count: 1 },
          { name: "roles", ready: true, status: "ready", count: 1 },
          { name: "iworkers", ready: true, status: "ready", count: 1 },
          { name: "agent_runtime", ready: true, status: "ready" },
          { name: "goalwatch", ready: true, status: "ready" },
          { name: "routes", ready: true, status: "ready" },
        ],
        auth_methods: [
          {
            method: "local",
            label: "Local account",
            ready: true,
            implemented: true,
            status: "ready",
          },
          {
            method: "ldap",
            label: "LDAP",
            ready: false,
            implemented: true,
            status: "not_configured",
          },
          {
            method: "oidc",
            label: "OIDC / OAuth SSO",
            ready: false,
            implemented: false,
            status: "reserved",
          },
        ],
      },
    } as never);
    vi.mocked(FetchWorkerMemoryStats).mockResolvedValue({
      tenant_id: "acme",
      department_id: "ops",
      worker_id: "worker-1",
      total: 7,
      by_scope: {
        company: 2,
        department: 3,
        personal: 2,
      },
      by_category: {
        policy: 4,
        preference: 3,
      },
      visible_scopes: ["company", "department", "personal"],
    } as never);
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue(settingsFixture as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([] as never);
    vi.mocked(RecallWorkerMemories).mockResolvedValue([
      {
        id: "mem-1",
        tenant_id: "acme",
        department_id: "ops",
        worker_id: "worker-1",
        scope: "department",
        content: "Escalate red orders before 10am.",
        category: "policy",
        tags: ["handoff", "sla"],
        source_type: "iworker-gui",
        created_at: "2026-04-28T00:00:00Z",
        updated_at: "2026-04-28T00:00:00Z",
      },
    ] as never);
    vi.mocked(SaveDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(SaveTaskHistory).mockResolvedValue(undefined as never);
    vi.mocked(SaveWorkerMemory).mockResolvedValue({
      id: "mem-1",
      tenant_id: "acme",
      department_id: "ops",
      worker_id: "worker-1",
      scope: "department",
      content: "Escalate red orders before 10am.",
      category: "policy",
      tags: ["handoff", "sla"],
      source_type: "iworker-gui",
      created_at: "2026-04-28T00:00:00Z",
      updated_at: "2026-04-28T00:00:00Z",
    } as never);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: "free input",
      colleague_name: "auto matched colleague",
      expected_output: "summary",
      model: "test-model",
      content: "default response",
    } as never);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanup();
  });

  it("turns a home quick chip into a structured task draft", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    expect(screen.getAllByText("Doc").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Office").length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "Customer reply" }));

    expect(await screen.findByDisplayValue("Customer reply")).toBeTruthy();
    expect(screen.getByDisplayValue("Customer reply")).toBeTruthy();
    expect(
      screen.getByDisplayValue(/Draft a customer-ready response/),
    ).toBeTruthy();
    expect(
      screen.getByDisplayValue(
        /Route reason: communication\/report\/document work/,
      ),
    ).toBeTruthy();
    expect(screen.getAllByText(/Office iWorker/).length).toBeGreaterThan(0);
  });
  it("previews routing and collaboration before opening a quick task", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();
    expect(screen.getByRole("region", { name: "Route preview" })).toBeTruthy();

    fireEvent.mouseEnter(
      screen.getByRole("button", { name: "Ask peer iWorkers" }),
    );

    expect(screen.getAllByText("Ask peer iWorkers").length).toBeGreaterThan(1);
    expect(screen.getByText(/Center-mediated A2A discussion/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Use this route" }));

    expect(await screen.findByDisplayValue("Ask peer iWorkers")).toBeTruthy();
    expect(
      screen.getByDisplayValue(
        /Please discuss this with the relevant peer iWorkers through iWorkerCenter/,
      ),
    ).toBeTruthy();
  });
  it("adds Center memory context from the home composer", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Memory" }));

    expect(
      screen.getByDisplayValue(/Use Center memory for context/),
    ).toBeTruthy();
  });

  it("opens memory settings from the home quick start", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Capture memory/ }));

    expect(await screen.findByText("Memory Capture")).toBeTruthy();
  });
  it("runs a home self-check across Center memory runtime and watcher queue", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Run self-check/ }));

    expect(
      screen.getByDisplayValue(/Run iWorker body self-check/),
    ).toBeTruthy();
    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(FetchWorkerMemoryStats).toHaveBeenCalled();
      expect(HeartbeatAgentRuntime).toHaveBeenCalled();
      expect(FetchAgentInstances).toHaveBeenCalled();
      expect(FetchGoalPushes).toHaveBeenCalled();
      expect(FetchCollaborations).toHaveBeenCalledWith("worker-1");
      expect(FetchWorkflowInstances).toHaveBeenCalled();
      expect(screen.getByText("Ready to work")).toBeTruthy();
      expect(screen.getAllByText("Center registration").length).toBeGreaterThan(
        1,
      );
      expect(screen.getAllByText("Memory authority").length).toBeGreaterThan(1);
      expect(screen.getByText("Agent runtime")).toBeTruthy();
      expect(screen.getByText("Goal watcher queue")).toBeTruthy();
      expect(screen.getByText("Collaboration queue")).toBeTruthy();
      expect(screen.getByText("Workflow queue")).toBeTruthy();
      expect(screen.getAllByText("Installed tools").length).toBeGreaterThan(0);
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Create readiness task" }),
    );

    expect(
      await screen.findByDisplayValue("iWorker readiness repair"),
    ).toBeTruthy();
    expect(
      screen.getByDisplayValue(/Create an iWorker readiness repair task/),
    ).toBeTruthy();
    expect(screen.getByDisplayValue(/Center push queue:/)).toBeTruthy();
    expect(screen.getByDisplayValue(/Human handoff queue:/)).toBeTruthy();
    expect(screen.getByDisplayValue(/Workflow queue:/)).toBeTruthy();
    expect(screen.getByDisplayValue(/GoalWatcher:/)).toBeTruthy();
  });

  it("treats no published Center config as a ready empty state", async () => {
    vi.mocked(FetchConfigBundle).mockResolvedValue({
      id: "",
      version: 0,
      content_type: "",
      payload: "",
      status: "not_published",
      note: "",
      created_at: "",
      published_at: "",
      source: "none",
      cached_at: "2026-05-04T00:00:00Z",
      stale: false,
    } as never);

    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Run self-check/ }));

    await waitFor(() => {
      expect(screen.getByText("Ready to work")).toBeTruthy();
      expect(screen.queryByText("Needs attention")).toBeNull();
      expect(screen.getByText("Config bundle").className).toContain("is-ok");
    });
  });

  it("shows cached runtime instances when heartbeat fails", async () => {
    vi.mocked(HeartbeatAgentRuntime).mockRejectedValue(
      new Error("center offline"),
    );
    vi.mocked(FetchAgentInstances).mockResolvedValue([
      {
        tenant_id: "acme",
        worker_id: "worker-ops",
        instance_id: "worker-ops:executor",
        role: "executor",
        status: "online",
        org_unit_id: "ops",
        capabilities: ["base", "skill:brief"],
        memory_authority: "iWorkerCenter",
        local_cache_mode: "cache_only",
        host_id: "desktop-a",
        process_id: 100,
        started_at: "2026-05-02T00:00:00Z",
        last_heartbeat_at: "2026-05-02T00:01:00Z",
        heartbeat_age_seconds: 12,
        effective_status: "online",
        source: "cache",
        cached_at: "2026-05-02T00:02:00Z",
        stale: true,
      },
    ] as never);

    render(<App />);

    expect(
      (await screen.findAllByText(/cached runtime snapshot/i)).length,
    ).toBeGreaterThan(0);
    const continuity = screen.getByRole("region", {
      name: "Local continuity status",
    });
    expect(continuity).toBeTruthy();
    expect(within(continuity).getByText("Center reconnecting")).toBeTruthy();
    expect(
      within(continuity).getByText(/Local work can continue/),
    ).toBeTruthy();
    expect(within(continuity).getByText(/1 cached runtime/)).toBeTruthy();
    expect(screen.getByText(/center offline/i)).toBeTruthy();
    expect(
      screen.getByText(/Cached snapshot \/ cached 2026-05-02T00:02:00Z/),
    ).toBeTruthy();
  });

  it("marks self-check as needing attention when agent runtime sync fails", async () => {
    vi.mocked(FetchAgentInstances).mockRejectedValue(
      new Error("runtime offline"),
    );

    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Run self-check/ }));

    await waitFor(() => {
      expect(screen.getByText("Needs attention")).toBeTruthy();
      expect(screen.getByText("runtime offline")).toBeTruthy();
      expect(screen.getByText("Agent runtime").className).toContain("is-fail");
    });
  });

  it("summarizes digital coworker work status across Center pushes and local history", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-open",
        task_id: "task-open",
        title: "Check delivery exception",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_task",
        age_seconds: 120,
        created_at: "2026-05-01T00:00:00Z",
      },
      {
        event_id: "evt-done",
        task_id: "task-done",
        title: "Restart completed",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "acked",
        reason: "goal_push",
        recommended_action: "restart_executor",
        age_seconds: 60,
        created_at: "2026-05-01T00:00:00Z",
      },
      {
        event_id: "evt-blocked",
        task_id: "task-blocked",
        title: "Needs credentials",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "missing_input",
        recommended_action: "ask_human",
        age_seconds: 300,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: "hist-done",
        title: "Daily brief",
        owner: "Ops iWorker",
        status: "completed",
        updated_at: "09:00",
        description: "",
        draft: "",
        expected_output: "summary",
        result: "",
        model: "",
      },
      {
        id: "hist-review",
        title: "Approve order reply",
        owner: "Office iWorker",
        status: "waiting approval",
        updated_at: "09:30",
        description: "",
        draft: "",
        expected_output: "document",
        result: "",
        model: "",
      },
      {
        id: "hist-blocked",
        title: "Missing SAP access",
        owner: "Data iWorker",
        status: "blocked by credentials",
        updated_at: "10:00",
        description: "",
        draft: "",
        expected_output: "summary",
        result: "",
        model: "",
      },
    ] as never);

    render(<App />);

    const board = await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    await waitFor(() => {
      expect(
        within(board).getAllByText("Check delivery exception").length,
      ).toBeGreaterThan(0);
      expect(screen.getAllByText("Needs credentials").length).toBeGreaterThan(
        0,
      );
      expect(
        within(board).getByText("Active").previousElementSibling?.textContent,
      ).toBe("1");
      expect(
        within(board).getByText("Completed").previousElementSibling
          ?.textContent,
      ).toBe("2");
      expect(
        within(board).getByText("Review").previousElementSibling?.textContent,
      ).toBe("1");
      expect(
        within(board).getByText("Blocked").previousElementSibling?.textContent,
      ).toBe("2");
    });
  });

  it("shows Center workflow instances in the work status board", async () => {
    vi.mocked(FetchWorkflowInstances).mockResolvedValue([
      {
        id: "wf-1",
        definition_id: "def-onboarding",
        title: "Approve onboarding workflow",
        initiator_id: "center-admin",
        current_step_id: "legal-review",
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:10:00Z",
        source: "cache",
        cached_at: "2026-05-01T00:11:00Z",
        stale: true,
      },
    ] as never);

    render(<App />);

    const board = await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    await waitFor(() => {
      expect(FetchWorkflowInstances).toHaveBeenCalled();
      expect(within(board).getAllByText("Approve onboarding workflow").length).toBeGreaterThan(0);
      expect(screen.getByText("Workflow instances")).toBeTruthy();
      expect(screen.getAllByText(/legal-review/).length).toBeGreaterThan(0);
      expect(screen.getAllByText(/Cached snapshot/).length).toBeGreaterThan(0);
    });
  });

  it("can transition an assigned Center workflow step from the workflow inspector", async () => {
    vi.mocked(FetchWorkflowInstances).mockResolvedValue([
      {
        id: "wf-1",
        definition_id: "def-onboarding",
        title: "Approve onboarding workflow",
        initiator_id: "center-admin",
        current_step_id: "legal-review",
        current_step_assignee_colleague_id: "worker-1",
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:10:00Z",
        source: "center",
        cached_at: "2026-05-01T00:11:00Z",
        stale: false,
      },
    ] as never);

    render(<App />);

    await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    await waitFor(() => {
      expect(screen.getAllByText("Approve onboarding workflow").length).toBeGreaterThan(0);
    });
    const workflowCard = screen.getAllByText("Approve onboarding workflow").find((node) => node.closest("article"))?.closest("article");
    expect(workflowCard).toBeTruthy();
    fireEvent.click(within(workflowCard as HTMLElement).getByRole("button", { name: "Complete" }));

    await waitFor(() => {
      expect(TransitionWorkflowStep).toHaveBeenCalledWith(
        "legal-review",
        "complete",
        "Approve onboarding workflow completed from iWorker",
        "iWorker client complete",
      );
    });
  });

  it("shows a human-readable workflow authorization error", async () => {
    vi.mocked(FetchWorkflowInstances).mockResolvedValue([
      {
        id: "wf-1",
        definition_id: "def-onboarding",
        title: "Approve onboarding workflow",
        initiator_id: "center-admin",
        current_step_id: "legal-review",
        current_step_assignee_colleague_id: "worker-1",
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:10:00Z",
        source: "center",
        cached_at: "2026-05-01T00:11:00Z",
        stale: false,
      },
    ] as never);
    vi.mocked(TransitionWorkflowStep).mockRejectedValueOnce(new Error("iWorkerCenter workflow step transition failed: status=403 STEP_ACTOR_FORBIDDEN: actor worker-1 cannot operate workflow step legal-review assigned to worker-2"));

    render(<App />);

    await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    const workflowCard = screen.getAllByText("Approve onboarding workflow").find((node) => node.closest("article"))?.closest("article");
    expect(workflowCard).toBeTruthy();
    fireEvent.click(within(workflowCard as HTMLElement).getByRole("button", { name: "Complete" }));

    await waitFor(() => {
      expect(screen.getByText("This workflow step is assigned to another iWorker. Refresh the workflow queue or ask iWorkerCenter to reassign it.")).toBeTruthy();
    });
  });

  it("promotes Center runtime work status to the main work board", async () => {
    vi.mocked(FetchAgentInstances).mockResolvedValue([
      {
        tenant_id: "acme",
        worker_id: "worker-ops",
        instance_id: "worker-ops:executor",
        role: "operations_executor",
        status: "online",
        org_unit_id: "ops",
        capabilities: ["base", "skill:brief"],
        memory_authority: "iWorkerCenter",
        local_cache_mode: "cache_only",
        work_status: {
          current_task: "Runtime is preparing exception brief",
          current_detail: "Collecting evidence",
          active_count: 2,
          completed_count: 5,
          review_count: 1,
          blocked_count: 0,
          updated_at: "2026-05-02T00:02:00Z",
        },
        host_id: "desktop-a",
        process_id: 100,
        started_at: "2026-05-02T00:00:00Z",
        last_heartbeat_at: "2026-05-02T00:02:00Z",
        heartbeat_age_seconds: 8,
        effective_status: "online",
        source: "center",
        cached_at: "",
        stale: false,
      },
      {
        tenant_id: "acme",
        worker_id: "worker-ops",
        instance_id: "worker-ops:reviewer",
        role: "operations_reviewer",
        status: "online",
        org_unit_id: "ops",
        capabilities: ["base"],
        memory_authority: "iWorkerCenter",
        local_cache_mode: "cache_only",
        work_status: {
          current_task: "Runtime reviewer mirrors the same local history",
          active_count: 2,
          completed_count: 5,
          review_count: 1,
          blocked_count: 0,
          updated_at: "2026-05-02T00:02:00Z",
        },
        host_id: "desktop-a",
        process_id: 101,
        started_at: "2026-05-02T00:00:00Z",
        last_heartbeat_at: "2026-05-02T00:02:00Z",
        heartbeat_age_seconds: 8,
        effective_status: "online",
        source: "center",
        cached_at: "",
        stale: false,
      },
    ] as never);

    render(<App />);

    const board = await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    await waitFor(() => {
      expect(
        within(board).getAllByText("Runtime is preparing exception brief")
          .length,
      ).toBeGreaterThan(0);
      expect(
        within(board).getAllByText(/Center runtime status/).length,
      ).toBeGreaterThan(0);
      expect(
        within(board).getByText("Active").previousElementSibling?.textContent,
      ).toBe("2");
      expect(
        within(board).getByText("Completed").previousElementSibling
          ?.textContent,
      ).toBe("5");
      expect(
        within(board).getByText("Review").previousElementSibling?.textContent,
      ).toBe("1");
    });
  });

  it("surfaces Center collaboration handoffs in the human queue and work board", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-1",
        title: "Human shift handoff",
        description: "Please review the night shift exceptions.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
          status: "pending",
          priority: 4,
          result: "",
          workflow_step_instance_id: "wf-step-1",
          created_at: "2026-05-01T00:00:00Z",
          updated_at: "2026-05-01T00:02:00Z",
        },
    ] as never);

    render(<App />);

    const board = await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    expect(await screen.findByText("1 pending handoff")).toBeTruthy();
    expect(within(board).getByText("Human shift handoff")).toBeTruthy();
    expect(within(board).getByText("Workflow handoff")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Open handoff" }));
    expect(
      await screen.findByDisplayValue("Human shift handoff"),
    ).toBeTruthy();
      expect(
        screen.getByDisplayValue(/Workflow step: wf-step-1/),
      ).toBeTruthy();
      expect(screen.getByDisplayValue(/Please review the night shift exceptions./)).toBeTruthy();
    });

  it("advances Center collaboration handoffs from the iWorker workbench", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-accept",
        title: "Accept shift handoff",
        description: "Please take the shift.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
          status: "pending",
          priority: 4,
          result: "",
          workflow_step_instance_id: "wf-step-submit",
          created_at: "2026-05-01T00:00:00Z",
          updated_at: "2026-05-01T00:02:00Z",
        },
    ] as never);

    render(<App />);

    expect(await screen.findByText("Accept shift handoff")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));

    await waitFor(() => {
      expect(TransitionCollaborationTask).toHaveBeenCalledWith(
        "collab-accept",
        "accept",
        "",
        "iWorker client accept",
      );
    });
    expect(await screen.findByRole("button", { name: "Start" })).toBeTruthy();
  });

  it("allows completing an in-progress Center handoff from the reminder card", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-progress",
        title: "Finish verified handoff",
        description: "Use the workbench so the final result is captured.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "running",
        priority: 4,
        result: "",
        workflow_step_instance_id: "wf-step-progress",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:02:00Z",
      },
    ] as never);

    render(<App />);

    expect(await screen.findByText("Finish verified handoff")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));

    await waitFor(() => {
      expect(TransitionCollaborationTask).toHaveBeenCalledWith(
        "collab-progress",
        "complete",
        "Use the workbench so the final result is captured.",
        "iWorker client complete",
      );
    });
    expect(screen.queryByText("Finish verified handoff")).toBeNull();
  });

  it("completes the source Center handoff after task workspace submission", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-submit",
        title: "Finish shift handoff",
        description: "Prepare the shift summary.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
          status: "pending",
          priority: 4,
          result: "",
          workflow_step_instance_id: "wf-step-submit",
          created_at: "2026-05-01T00:00:00Z",
          updated_at: "2026-05-01T00:02:00Z",
        },
    ] as never);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: "Finish shift handoff",
      task_title: "Finish shift handoff",
      colleague_name: "ops",
      expected_output: "summary",
      model: "office-openai",
      content: "Shift summary is ready.",
    } as never);

    render(<App />);

    expect(await screen.findByText("Finish shift handoff")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open handoff" }));
    expect(
      await screen.findByDisplayValue(/Prepare the shift summary./),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Start work" }));

    await waitFor(() => {
        expect(TransitionCollaborationTask).toHaveBeenCalledWith(
          "collab-submit",
          "complete",
          "Shift summary is ready.",
          "completed from iWorker task workspace",
        );
        const saved = vi.mocked(SaveTaskHistory).mock.calls.at(-1)?.[0] as Array<{
          source_type?: string;
          center_handoff_id?: string;
          workflow_step_instance_id?: string;
        }>;
        expect(saved?.[0]).toMatchObject({
          source_type: "workflow_handoff",
          center_handoff_id: "collab-submit",
          workflow_step_instance_id: "wf-step-submit",
        });
      });
      expect(await screen.findByRole("region", { name: "Selected work result" })).toBeTruthy();
      expect(screen.getAllByText("Workflow handoff").length).toBeGreaterThan(0);
      expect(screen.getAllByText("Step wf-step-submit").length).toBeGreaterThan(0);
      expect(screen.getByText("Shift summary is ready.")).toBeTruthy();
    });

  it("does not mark a Center handoff complete locally when Center completion fails", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-fail",
        title: "Finish exception handoff",
        description: "Prepare the exception summary.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        priority: 4,
        result: "",
        workflow_step_instance_id: "wf-step-fail",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:02:00Z",
      },
    ] as never);
    vi.mocked(TransitionCollaborationTask).mockRejectedValueOnce(new Error("center transition failed"));
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: "Finish exception handoff",
      task_title: "Finish exception handoff",
      colleague_name: "ops",
      expected_output: "summary",
      model: "office-openai",
      content: "Exception summary is ready.",
    } as never);

    render(<App />);

    expect(await screen.findByText("Finish exception handoff")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open handoff" }));
    expect(await screen.findByDisplayValue(/Prepare the exception summary./)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Start work" }));

    await waitFor(() => {
      expect(screen.getByText("center transition failed")).toBeTruthy();
      expect(SaveTaskHistory).not.toHaveBeenCalled();
      expect(screen.queryByRole("region", { name: "Selected work result" })).toBeNull();
    });
  });

  it("keeps cached Center collaboration handoffs visible but read-only", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValue([
      {
        id: "collab-cached",
        title: "Cached human handoff",
        description: "Reconnect before acting.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        priority: 4,
        result: "",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:02:00Z",
        source: "cache",
        cached_at: "2026-05-01T00:03:00Z",
        stale: true,
      },
    ] as never);

    render(<App />);

    const board = await screen.findByRole("region", {
      name: "Digital coworker work status",
    });
    expect(within(board).getByText("Cached human handoff")).toBeTruthy();
    expect(within(board).getByText("Cached Center handoff")).toBeTruthy();
    const continuity = screen.getByRole("region", {
      name: "Local continuity status",
    });
    expect(within(continuity).getByText("Cached continuity active")).toBeTruthy();
    expect(within(continuity).getByText("1 cached handoff")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Accept" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("marks live Center collaboration handoffs read-only when refresh fails", async () => {
    vi.mocked(FetchCollaborations).mockResolvedValueOnce([
      {
        id: "collab-live",
        title: "Live human handoff",
        description: "Reconnect before acting if refresh fails.",
        from_colleague_id: "worker-office",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        priority: 4,
        result: "",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:02:00Z",
      },
    ] as never);

    render(<App />);

    expect(await screen.findByText("Live human handoff")).toBeTruthy();
    vi.mocked(FetchCollaborations).mockRejectedValue(new Error("center offline"));
    for (const refreshButton of screen.getAllByRole("button", { name: "Refresh" })) {
      fireEvent.click(refreshButton);
    }

    await waitFor(() => {
      expect(screen.getAllByText("Cached Center handoff").length).toBeGreaterThan(0);
      expect((screen.getByRole("button", { name: "Accept" }) as HTMLButtonElement).disabled).toBe(true);
    });
  });

  it("keeps the last Center push snapshot read-only when refresh fails", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-open",
        task_id: "task-open",
        title: "Check delivery exception",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_task",
        age_seconds: 120,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    expect(
      (await screen.findAllByText("Check delivery exception")).length,
    ).toBeGreaterThan(0);

    vi.mocked(FetchGoalPushes).mockRejectedValueOnce(
      new Error("center offline"),
    );
    const pushQueue = screen
      .getByText("Push queue")
      .closest("section") as HTMLElement;
    fireEvent.click(within(pushQueue).getByRole("button", { name: "Refresh" }));

    await waitFor(() => {
      const continuity = screen.getByRole("region", {
        name: "Local continuity status",
      });
      expect(within(continuity).getByText("Center reconnecting")).toBeTruthy();
      expect(within(continuity).getByText(/1 cached push/)).toBeTruthy();
      expect(screen.getAllByText(/center offline/).length).toBeGreaterThan(0);
      expect(screen.getAllByText(/Cached Center push/).length).toBeGreaterThan(
        0,
      );
    });

    const openCached = screen.getByRole("button", {
      name: "Open cached task",
    }) as HTMLButtonElement;
    expect(openCached.disabled).toBe(false);
    fireEvent.click(openCached);
    expect(
      await screen.findByDisplayValue("Check delivery exception"),
    ).toBeTruthy();
    expect(screen.getByText("Cached Center push")).toBeTruthy();
  });

  it("runs recoverable Center workflow pushes through recovery instead of plain ack", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-recover",
        task_id: "task-recover",
        workflow_step_instance_id: "wfsi-recover",
        title: "Resume invoice workflow",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_workflow_step",
        recovery_action: "resume_workflow_step",
        recovery_method: "POST",
        recovery_path: "/runtime/workflows/steps/wfsi-recover/resume",
        age_seconds: 90,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    expect(
      (await screen.findAllByText("Resume invoice workflow")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Run Center push" }));

    await waitFor(() => {
      expect(RecoverGoalPush).toHaveBeenCalledWith(
        "evt-recover",
        "interaction agent ran workflow recovery",
      );
      expect(AutoHandleGoalPush).not.toHaveBeenCalled();
      expect(AckGoalPush).not.toHaveBeenCalled();
      const saved = vi.mocked(SaveTaskHistory).mock.calls.at(-1)?.[0] as Array<{
        title?: string;
        status?: string;
        result?: string;
      }>;
      expect(saved?.[0]).toMatchObject({
        title: "Resume invoice workflow",
        status: "recovered",
      });
      expect(saved?.[0]?.result).toContain("Workflow recovery");
    });
  });

  it("treats legacy workflow pushes with task actions as recoverable", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-legacy-recover",
        task_id: "task-legacy-recover",
        workflow_step_instance_id: "wfsi-legacy-recover",
        title: "Resume legacy workflow push",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_task",
        age_seconds: 90,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    expect(
      (await screen.findAllByText("Resume legacy workflow push")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Run Center push" }));

    await waitFor(() => {
      expect(RecoverGoalPush).toHaveBeenCalledWith(
        "evt-legacy-recover",
        "interaction agent ran workflow recovery",
      );
      expect(AutoHandleGoalPush).not.toHaveBeenCalled();
      expect(AckGoalPush).not.toHaveBeenCalled();
    });
  });

  it("acks explicit non-workflow recovery actions instead of calling recovery", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-task-recovery",
        task_id: "task-task-recovery",
        workflow_step_instance_id: "wfsi-task-recovery",
        title: "Resume task-level push",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_task",
        recovery_action: "resume_task",
        age_seconds: 90,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    expect(
      (await screen.findAllByText("Resume task-level push")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole("button", { name: "Resume" })[0]);

    await waitFor(() => {
      expect(AckGoalPush).toHaveBeenCalledWith(
        "evt-task-recovery",
        "resumed",
        "interaction agent confirmed resume",
      );
      expect(RecoverGoalPush).not.toHaveBeenCalled();
    });
  });

  it("does not fail a recovered Center push when local history persistence fails", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-history-fail",
        task_id: "task-history-fail",
        workflow_step_instance_id: "wfsi-history-fail",
        title: "Resume after local history issue",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "pending",
        reason: "goal_push",
        recommended_action: "resume_workflow_step",
        recovery_action: "resume_workflow_step",
        recovery_method: "POST",
        recovery_path: "/runtime/workflows/steps/wfsi-history-fail/resume",
        age_seconds: 90,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);
    vi.mocked(SaveTaskHistory).mockRejectedValueOnce(new Error("disk full"));

    render(<App />);

    expect(
      (await screen.findAllByText("Resume after local history issue")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Run Center push" }));

    await waitFor(() => {
      expect(RecoverGoalPush).toHaveBeenCalledWith(
        "evt-history-fail",
        "interaction agent ran workflow recovery",
      );
      expect(SaveTaskHistory).toHaveBeenCalled();
      expect(screen.queryByText(/disk full/)).toBeNull();
    });
  });

  it("keeps human-intervention alerts visible outside the workbench", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-human",
        task_id: "task-human",
        title: "Approve exception reply",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "missing_input",
        recommended_action: "ask_human",
        age_seconds: 180,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    expect(
      await screen.findByRole("status", { name: "Human input needed" }),
    ).toBeTruthy();
    expect(screen.getByLabelText("1 human input needed")).toBeTruthy();

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Human input")).toBeTruthy();
    expect(screen.getByText("Approve exception reply")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Open human intervention workbench" }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();
  });

  it("surfaces a reminder when an automatic Center push needs human input", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-human",
        task_id: "task-human",
        workflow_step_instance_id: "wfsi-human",
        title: "Approve exception reply",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "missing_input",
        recommended_action: "resume_workflow_step",
        recovery_action: "resume_workflow_step",
        recovery_method: "POST",
        recovery_path: "/runtime/workflows/steps/wfsi-human/resume",
        age_seconds: 180,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    const banner = await screen.findByRole("status", {
      name: "Human input needed",
    });
    expect(within(banner).getByText("Human input needed")).toBeTruthy();
    expect(within(banner).getByText("Approve exception reply")).toBeTruthy();

    const reviewButton = screen.getByRole("button", {
      name: "Open review task",
    }) as HTMLButtonElement;
    expect(reviewButton.disabled).toBe(false);
    fireEvent.click(reviewButton);
    expect(AutoHandleGoalPush).not.toHaveBeenCalled();
    expect(
      await screen.findByDisplayValue("Approve exception reply"),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "Center push intervention task" }),
    ).toBeTruthy();
    expect(screen.getByText("Live Center push")).toBeTruthy();
    expect(
      screen.getByText(/return to the workbench to Resume or Block/),
    ).toBeTruthy();
    expect(
      screen.getByDisplayValue(
        /Human intervention needed: review the pushed work/,
      ),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Talk" }));
    const refreshedBanner = await screen.findByRole("status", {
      name: "Human input needed",
    });
    fireEvent.click(
      within(refreshedBanner).getByRole("button", { name: "Recover workflow" }),
    );

    await waitFor(() => {
      expect(RecoverGoalPush).toHaveBeenCalledWith(
        "evt-human",
        "interaction agent confirmed workflow recovery",
      );
      expect(AckGoalPush).not.toHaveBeenCalled();
      const saved = vi.mocked(SaveTaskHistory).mock.calls.at(-1)?.[0] as Array<{
        title?: string;
        status?: string;
        result?: string;
      }>;
      expect(saved?.[0]).toMatchObject({
        title: "Approve exception reply",
        status: "recovered",
      });
      expect(saved?.[0]?.result).toContain("Human approved resume");
    });
  });

  it("records blocked Center pushes in local work history", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-block-human",
        task_id: "task-block-human",
        title: "Block risky payment run",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "approval_required",
        recommended_action: "ask_human",
        age_seconds: 210,
        created_at: "2026-05-01T00:00:00Z",
      },
    ] as never);

    render(<App />);

    const banner = await screen.findByRole("status", {
      name: "Human input needed",
    });
    fireEvent.click(
      within(banner).getByRole("button", { name: "Block" }),
    );

    await waitFor(() => {
      expect(AckGoalPush).toHaveBeenCalledWith(
        "evt-block-human",
        "blocked",
        "interaction agent reported blocked",
      );
      const saved = vi.mocked(SaveTaskHistory).mock.calls.at(-1)?.[0] as Array<{
        title?: string;
        status?: string;
        result?: string;
      }>;
      expect(saved?.[0]).toMatchObject({
        title: "Block risky payment run",
        status: "blocked",
      });
      expect(saved?.[0]?.result).toContain("Human blocked");
    });
  });

  it("keeps cached human-intervention pushes display-only", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-cached",
        task_id: "task-cached",
        title: "Approve cached exception",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "missing_input",
        recommended_action: "ask_human",
        age_seconds: 300,
        created_at: "2026-05-01T00:00:00Z",
        source: "cache",
        cached_at: "2026-05-02T00:02:00Z",
        stale: true,
      },
    ] as never);

    render(<App />);

    const banner = await screen.findByRole("status", {
      name: "Human input needed",
    });
    const continuity = screen.getByRole("region", {
      name: "Local continuity status",
    });
    expect(continuity).toBeTruthy();
    expect(
      within(continuity).getByText("Cached continuity active"),
    ).toBeTruthy();
    expect(within(continuity).getByText(/1 cached push/)).toBeTruthy();
    expect(within(banner).getByText("Cached notice")).toBeTruthy();
    expect(within(banner).getByText(/Reconnect iWorkerCenter/)).toBeTruthy();
    const cachedPrimaryAction = screen.getByRole("button", {
      name: "Open cached task",
    }) as HTMLButtonElement;
    expect(cachedPrimaryAction.disabled).toBe(false);
    fireEvent.click(cachedPrimaryAction);
    expect(AutoHandleGoalPush).not.toHaveBeenCalled();
    expect(
      await screen.findByDisplayValue("Approve cached exception"),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "Center push intervention task" }),
    ).toBeTruthy();
    expect(screen.getByText("Cached Center push")).toBeTruthy();
    expect(
      screen.getByText(
        /Reconnect iWorkerCenter before Resume, Block, or Run actions/,
      ),
    ).toBeTruthy();
    expect(screen.getByDisplayValue(/This push is cached/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Talk" }));
    const refreshedBanner = await screen.findByRole("status", {
      name: "Human input needed",
    });
    expect(
      (
        within(refreshedBanner).getByRole("button", {
          name: "Resume",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (
        within(refreshedBanner).getByRole("button", {
          name: "Block",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("recognizes localized cached Center push drafts as intervention tasks", async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      {
        event_id: "evt-cached-zh",
        task_id: "task-cached-zh",
        title: "审批缓存异常",
        to_colleague_id: "worker-ops",
        to_role_code: "ops",
        status: "blocked",
        reason: "missing_input",
        recommended_action: "ask_human",
        age_seconds: 300,
        created_at: "2026-05-01T00:00:00Z",
        source: "cache",
        cached_at: "2026-05-02T00:02:00Z",
        stale: true,
      },
    ] as never);

    render(<App />);

    await screen.findByRole("status", { name: "Human input needed" });
    fireEvent.click(screen.getByRole("button", { name: "Language" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "打开缓存任务" })).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "打开缓存任务" }));

    expect(await screen.findByDisplayValue("审批缓存异常")).toBeTruthy();
    expect(screen.getByRole("region", { name: "Center 推送人工介入任务" })).toBeTruthy();
    expect(screen.getByText("缓存的 Center 推送")).toBeTruthy();
    expect(screen.getByText(/先重连 iWorkerCenter，再执行恢复、阻塞或运行操作/)).toBeTruthy();
  });

  it("labels partially cached installed tools without implying local-only mode", async () => {
    vi.mocked(FetchInstalledTools).mockResolvedValue({
      source: "partial-cache",
      cached_at: "2026-05-02T00:02:00Z",
      stale: true,
      mcp_error: "iWorkerCenter MCP servers failed",
      skills: [
        {
          capability_id: "skill-brief",
          name: "Brief Writer",
          source: "iWorkerCenter",
          version: "1.0.0",
          risk_level: "low",
          entry: {
            name: "Brief Writer",
            description: "Writes operating briefs",
            triggers: ["brief"],
          },
        },
      ],
      mcp_servers: [
        {
          id: "mcp-cached",
          name: "Cached MCP",
          description: "Cached lookup",
          server_type: "http",
          endpoint: "https://mcp.example.com",
          args: [],
          env_keys: [],
          department_id: "ops",
          risk_level: "medium",
          status: "enabled",
          installed_at: "2026-05-01T00:00:00Z",
        },
      ],
    } as never);

    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText(/Partial Center snapshot/)).toBeTruthy();
      expect(screen.getByText("Cached MCP")).toBeTruthy();
      expect(screen.getByText(/MCP sync issue/)).toBeTruthy();
      expect(screen.getByText(/iWorkerCenter MCP servers failed/)).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Skills & Work" }));
    await waitFor(() => {
      expect(screen.getByText(/Partial Center snapshot/)).toBeTruthy();
      expect(screen.getByText("Cached MCP")).toBeTruthy();
    });
    expect(screen.queryByText(/Local only/)).toBeNull();
  });
  it("shows installed tools and availability from Center-managed MCP and skills", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    await waitFor(() => {
      expect(FetchInstalledTools).toHaveBeenCalled();
      expect(screen.getAllByText("Availability").length).toBeGreaterThan(0);
      expect(screen.getByText("Installed tools")).toBeTruthy();
      expect(screen.getByText("Brief Writer")).toBeTruthy();
      expect(screen.getByText("CRM MCP")).toBeTruthy();
      expect(screen.getByText("Center live")).toBeTruthy();
    });
    expect(screen.queryByText(/CRM_TOKEN=/)).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "Use in task" })[0]);
    expect(
      screen.getByDisplayValue(/Use Center-installed Skill "Brief Writer"/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Skills & Work" }));
    await waitFor(() => {
      expect(screen.getByText("Transport http")).toBeTruthy();
      expect(screen.getByText("Department ops")).toBeTruthy();
      expect(screen.getByText("Env keys CRM_TOKEN")).toBeTruthy();
    });
    expect(screen.queryByText(/CRM_TOKEN=/)).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "Use in task" })[1]);
    expect(
      screen.getByDisplayValue(/Use Center-installed MCP "CRM MCP"/),
    ).toBeTruthy();
    expect(screen.getByDisplayValue(/Required env keys: CRM_TOKEN/)).toBeTruthy();
    expect(screen.queryByDisplayValue(/CRM_TOKEN=/)).toBeNull();
  });

  it("keeps Center-installed tools usable when legacy payloads omit optional fields", async () => {
    vi.mocked(FetchInstalledTools).mockResolvedValue({
      source: "center",
      cached_at: "2026-05-02T00:02:00Z",
      stale: false,
      skills: [
        {
          source: "iWorkerCenter",
          version: "2.0.0",
          entry: {
            name: "Legacy Skill",
            description: "Legacy skill payload with no capability id",
          },
        },
      ],
      mcp_servers: [
        {
          name: "Legacy MCP",
          description: "Command-only MCP payload",
          server_type: "stdio",
          command: "legacy-mcp",
          args: "not-an-array",
          status: "enabled",
        },
      ],
    } as never);

    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();

    await waitFor(() => {
      expect(screen.getByText("Legacy Skill")).toBeTruthy();
      expect(screen.getByText("Legacy MCP")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Skills & Work" }));
    await waitFor(() => {
      expect(screen.getByText("No env keys required")).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole("button", { name: "Use in task" })[0]);
    expect(
      screen.getByDisplayValue(/Use Center-installed Skill "Legacy Skill"/),
    ).toBeTruthy();
    expect(screen.getByDisplayValue(/Capability ID: Legacy Skill/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Skills & Work" }));
    fireEvent.click(screen.getAllByRole("button", { name: "Use in task" })[1]);
    expect(
      screen.getByDisplayValue(/Use Center-installed MCP "Legacy MCP"/),
    ).toBeTruthy();
    expect(screen.getByDisplayValue(/Endpoint\/command: legacy-mcp/)).toBeTruthy();
    expect(screen.getByDisplayValue(/Required env keys: none/)).toBeTruthy();
  });

  it("tests center health from settings page and shows snapshot", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );

    expect(
      await screen.findByRole("heading", { name: "Center configuration" }),
    ).toBeTruthy();
    expect(screen.getByText("Installed tools status")).toBeTruthy();
    expect(screen.getByText("Center live")).toBeTruthy();
    expect(screen.getByText("Brief Writer")).toBeTruthy();
    expect(screen.getByText("CRM MCP")).toBeTruthy();
    expect(screen.getByText("Config apply")).toBeTruthy();
    expect(screen.getByText("iWorker fetched and cached the published config bundle")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: /Test Center connection/ }),
    );

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText("/tmp/center.json")).toBeTruthy();
    });
  });

  it("refreshes worker memory stats from iWorkerCenter", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Memory records")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Refresh memory" }));

    await waitFor(() => {
      expect(FetchWorkerMemoryStats).toHaveBeenCalledTimes(1);
      expect(screen.getAllByText(/7/).length).toBeGreaterThan(0);
      expect(screen.getByText("acme / ops / worker-1")).toBeTruthy();
    });
  });

  it("discovers and applies Center enrollment from settings", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Center Enrollment")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Discover Center" }));

    await waitFor(() => {
      expect(DiscoverCenterEnrollment).toHaveBeenCalledTimes(1);
    });

    vi.mocked(CheckCenterHealth).mockClear();
    vi.mocked(FetchWorkerMemoryStats).mockClear();
    vi.mocked(HeartbeatAgentRuntime).mockClear();
    vi.mocked(FetchAgentInstances).mockClear();
    vi.mocked(FetchGoalPushes).mockClear();
    vi.mocked(FetchWorkflowInstances).mockClear();
    vi.mocked(FetchInstalledTools).mockClear();
    vi.mocked(FetchConfigBundle).mockClear();

    fireEvent.change(screen.getByPlaceholderText("alice@example.com"), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByPlaceholderText("Required before binding"), {
      target: { value: "secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Bind here" }));

    await waitFor(() => {
      expect(ApplyCenterEnrollment).toHaveBeenCalledTimes(1);
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(FetchWorkerMemoryStats).toHaveBeenCalledTimes(1);
      expect(HeartbeatAgentRuntime).toHaveBeenCalled();
      expect(FetchAgentInstances).toHaveBeenCalled();
      expect(FetchGoalPushes).toHaveBeenCalled();
      expect(FetchWorkflowInstances).toHaveBeenCalled();
      expect(FetchInstalledTools).toHaveBeenCalled();
      expect(FetchConfigBundle).toHaveBeenCalled();
      expect(screen.getByText(/Usage flow connected/)).toBeTruthy();
    });

    const applied = vi.mocked(ApplyCenterEnrollment).mock.calls[0]?.[0] as {
      worker_id?: string;
      tenant_id?: string;
      department_id?: string;
    };
    expect(applied.worker_id).toBe("worker-ops");
    expect(applied.tenant_id).toBe("acme");
    expect(applied.department_id).toBe("ops");
    expect((applied as any).auth_method).toBe("local");
    expect((applied as any).auth_username).toBe("alice");
    expect((applied as any).auth_password).toBe("secret");
  });
  it("saves tenant department and worker context for center registration", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByDisplayValue("ops")).toBeTruthy();

    fireEvent.change(screen.getByDisplayValue("ops"), {
      target: { value: "quality" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save configuration" }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as {
      center?: {
        tenant_id?: string;
        department_id?: string;
        worker_id?: string;
      };
    };
    expect(saved.center?.tenant_id).toBe("acme");
    expect(saved.center?.department_id).toBe("quality");
    expect(saved.center?.worker_id).toBe("worker-1");
  });
  it("saves worker memory to the registered iWorkerCenter", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Memory Capture")).toBeTruthy();

    fireEvent.change(screen.getByDisplayValue("Personal memory"), {
      target: { value: "department" },
    });
    fireEvent.change(screen.getByPlaceholderText("note"), {
      target: { value: "policy" },
    });
    fireEvent.change(screen.getByPlaceholderText("policy, preference"), {
      target: { value: "handoff, sla" },
    });
    fireEvent.change(
      screen.getByPlaceholderText(
        "Write a reusable fact, rule, preference, or handoff note.",
      ),
      {
        target: { value: "Escalate red orders before 10am." },
      },
    );

    fireEvent.click(screen.getByRole("button", { name: "Save memory" }));

    await waitFor(() => {
      expect(SaveWorkerMemory).toHaveBeenCalledTimes(1);
      expect(screen.getByText("Memory saved to iWorkerCenter.")).toBeTruthy();
    });

    const saved = vi.mocked(SaveWorkerMemory).mock.calls[0]?.[0] as {
      scope?: string;
      content?: string;
      category?: string;
      tags?: string[];
      source_type?: string;
    };
    expect(saved.scope).toBe("department");
    expect(saved.content).toBe("Escalate red orders before 10am.");
    expect(saved.category).toBe("policy");
    expect(saved.tags).toEqual(["handoff", "sla"]);
    expect(saved.source_type).toBe("iworker-gui");
    expect(FetchWorkerMemoryStats).toHaveBeenCalledTimes(1);
  });
  it("recalls visible worker memories from iWorkerCenter", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Memory Browser")).toBeTruthy();

    fireEvent.change(
      screen.getByPlaceholderText("Search registered center memory"),
      { target: { value: "red orders" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Recall memories" }));

    await waitFor(() => {
      expect(RecallWorkerMemories).toHaveBeenCalledWith("red orders");
      expect(screen.getByText("Escalate red orders before 10am.")).toBeTruthy();
      expect(screen.getByText("department / policy")).toBeTruthy();
    });
  });
  it("deletes recalled worker memory from iWorkerCenter", async () => {
    render(<App />);

    fireEvent.click(
      screen.getAllByRole("button", { name: "Open settings" })[0],
    );
    expect(await screen.findByText("Memory Browser")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Recall memories" }));
    expect(
      await screen.findByText("Escalate red orders before 10am."),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Forget" }));

    await waitFor(() => {
      expect(DeleteWorkerMemory).toHaveBeenCalledWith("mem-1");
      expect(screen.queryByText("Escalate red orders before 10am.")).toBeNull();
    });
  });

  it("switches the iWorker shell between English and Chinese", async () => {
    render(<App />);

    expect(
      await screen.findByRole("heading", {
        name: "Digital coworker workbench",
      }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Talk" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Language" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "对话" })).toBeTruthy();
      expect(screen.getByText("数字员工本体")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "语言" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Talk" })).toBeTruthy();
      expect(screen.getByText("digital employee body")).toBeTruthy();
    });
  });
});
