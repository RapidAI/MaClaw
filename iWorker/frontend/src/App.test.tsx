import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import App from './App';
import { AckGoalPush, ApplyCenterEnrollment, AutoHandleGoalPush, CheckCenterHealth, DeleteWorkerMemory, DiscoverCenterEnrollment, FetchAgentInstances, FetchGoalPushes, FetchWorkerMemoryStats, GetGoalWatchAutoHandleStatus, HeartbeatAgentRuntime, LoadDiWorkerSettings, LoadTaskHistory, RecallWorkerMemories, SaveDiWorkerSettings, SaveTaskHistory, SaveWorkerMemory, SubmitTask } from '../wailsjs/go/main/App';

vi.mock('../wailsjs/go/main/App', () => ({
  AckGoalPush: vi.fn(),
  ApplyCenterEnrollment: vi.fn(),
  AutoHandleGoalPush: vi.fn(),
  CheckCenterHealth: vi.fn(),
  DeleteWorkerMemory: vi.fn(),
  DiscoverCenterEnrollment: vi.fn(),
  FetchAgentInstances: vi.fn(),
  FetchGoalPushes: vi.fn(),
  FetchWorkerMemoryStats: vi.fn(),
  GetGoalWatchAutoHandleStatus: vi.fn(),
  HeartbeatAgentRuntime: vi.fn(),
  LoadDiWorkerSettings: vi.fn(),
  LoadTaskHistory: vi.fn(),
  RecallWorkerMemories: vi.fn(),
  SaveDiWorkerSettings: vi.fn(),
  SaveTaskHistory: vi.fn(),
  SaveWorkerMemory: vi.fn(),
  SubmitTask: vi.fn(),
}));

const settingsFixture = {
  role_profile: {
    name: 'Xiao Di',
    description: '你的数字办公助理，擅长通知、纪要与汇报整理。',
  },
  center: {
    enabled: true,
    host: '127.0.0.1',
    port: 9377,
    base_url: 'http://127.0.0.1:9377',
    tenant_id: 'acme',
    department_id: 'ops',
    worker_id: 'worker-1',
    timeout_sec: 60,
  },
  routing: {
    mode: 'smart',
    default_provider: 'office-openai',
    allow_fallback: true,
  },
  providers: [
    {
      id: 'office-openai',
      name: 'Office writing service',
      enabled: true,
      protocol: 'openai',
      base_url: 'https://office.example.com/v1',
      api_key: '',
      model: 'gpt-4.1',
      priority: 100,
      features: ['docs', 'minutes'],
      description: 'Office writing provider for notices, minutes, and reports.',
      capabilities: {
        supports_stream: true,
        supports_vision: false,
        max_context: 128000,
      },
    },
  ],
};

const installWailsBridge = () => {
  Object.defineProperty(window, 'go', {
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

describe('App', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('matchMedia', vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })) as unknown as typeof window.matchMedia);
    installWailsBridge();

    vi.mocked(AckGoalPush).mockResolvedValue(undefined as never);
    vi.mocked(DiscoverCenterEnrollment).mockResolvedValue({
      base_url: 'http://127.0.0.1:9377',
      selected_tenant_id: 'acme',
      tenants: [{ id: 'acme', company_name: 'Acme' }],
      roles: [{ id: 'role-ops', name: 'Xiao Di', code: 'ops', description: 'Operations role', default_strengths: [], applicable_tasks: [] }],
      colleagues: [{ id: 'worker-ops', name: 'Xiao Di', avatar: '', role_id: 'role-ops', role_name: 'Xiao Di', role_code: 'ops', description: 'Runs operations work', strengths: ['ops'], tasks: ['daily brief'] }],
    } as never);
    vi.mocked(ApplyCenterEnrollment).mockResolvedValue({ ...settingsFixture, center: { ...settingsFixture.center, worker_id: 'worker-ops', department_id: 'ops' }, role_profile: { name: 'Xiao Di', description: 'Runs operations work' } } as never);
    vi.mocked(AutoHandleGoalPush).mockResolvedValue(undefined as never);
    vi.mocked(FetchAgentInstances).mockResolvedValue([] as never);
    vi.mocked(FetchGoalPushes).mockResolvedValue([] as never);
    vi.mocked(GetGoalWatchAutoHandleStatus).mockResolvedValue({
      enabled: true,
      running: false,
      current_run_id: 0,
      run_count: 0,
      skip_count: 0,
      timeout_cancel_count: 0,
      last_handled_count: 0,
      total_handled_count: 0,
      last_error: '',
      last_started_at: '',
      last_finished_at: '',
      last_timeout_at: '',
      interval_seconds: 30,
      max_duration_seconds: 120,
    } as never);
    vi.mocked(HeartbeatAgentRuntime).mockResolvedValue(undefined as never);    vi.mocked(DeleteWorkerMemory).mockResolvedValue(undefined as never);
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 3,
      config_path: '/tmp/center.json',
      message: 'ok',
      resolved_base_url: 'http://127.0.0.1:9377',
      iworker_readiness: {
        ready: true,
        status: 'ready',
        tenant_count: 1,
        role_count: 1,
        colleague_count: 1,
        local_account_count: 1,
        agent_runtime_ready: true,
        goalwatch_ready: true,
        required_client_paths: ['/client/goalwatch/pushes'],
        checks: [
          { name: 'database', ready: true, status: 'ready' },
          { name: 'tenant', ready: true, status: 'ready', count: 1 },
          { name: 'roles', ready: true, status: 'ready', count: 1 },
          { name: 'iworkers', ready: true, status: 'ready', count: 1 },
          { name: 'agent_runtime', ready: true, status: 'ready' },
          { name: 'goalwatch', ready: true, status: 'ready' },
          { name: 'routes', ready: true, status: 'ready' },
        ],
        auth_methods: [
          { method: 'local', label: 'Local account', ready: true, implemented: true, status: 'ready' },
          { method: 'ldap', label: 'LDAP', ready: false, implemented: true, status: 'not_configured' },
          { method: 'oidc', label: 'OIDC / OAuth SSO', ready: false, implemented: false, status: 'reserved' },
        ],
      },
    } as never);
    vi.mocked(FetchWorkerMemoryStats).mockResolvedValue({
      tenant_id: 'acme',
      department_id: 'ops',
      worker_id: 'worker-1',
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
      visible_scopes: ['company', 'department', 'personal'],
    } as never);
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue(settingsFixture as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([] as never);
    vi.mocked(RecallWorkerMemories).mockResolvedValue([
      {
        id: 'mem-1',
        tenant_id: 'acme',
        department_id: 'ops',
        worker_id: 'worker-1',
        scope: 'department',
        content: 'Escalate red orders before 10am.',
        category: 'policy',
        tags: ['handoff', 'sla'],
        source_type: 'iworker-gui',
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
      },
    ] as never);    vi.mocked(SaveDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(SaveTaskHistory).mockResolvedValue(undefined as never);
    vi.mocked(SaveWorkerMemory).mockResolvedValue({
      id: 'mem-1',
      tenant_id: 'acme',
      department_id: 'ops',
      worker_id: 'worker-1',
      scope: 'department',
      content: 'Escalate red orders before 10am.',
      category: 'policy',
      tags: ['handoff', 'sla'],
      source_type: 'iworker-gui',
      created_at: '2026-04-28T00:00:00Z',
      updated_at: '2026-04-28T00:00:00Z',
    } as never);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: 'free input',
      colleague_name: 'auto matched colleague',
      expected_output: 'summary',
      model: 'test-model',
      content: 'default response',
    } as never);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanup();
  });

  it('turns a home quick chip into a structured task draft', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Command inbox for Center pushes and human teamwork' })).toBeTruthy();

    expect(screen.getAllByText('Doc').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Office').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'Customer reply' }));

    expect(await screen.findByDisplayValue('Customer reply')).toBeTruthy();
    expect(screen.getByDisplayValue('Customer reply')).toBeTruthy();
    expect(screen.getByDisplayValue(/Draft a customer-ready response/)).toBeTruthy();
    expect(screen.getByDisplayValue(/Route reason: communication\/report\/document work/)).toBeTruthy();
    expect(screen.getAllByText(/Office iWorker/).length).toBeGreaterThan(0);
  });
  it('previews routing and collaboration before opening a quick task', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Command inbox for Center pushes and human teamwork' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Route preview' })).toBeTruthy();

    fireEvent.mouseEnter(screen.getByRole('button', { name: 'Ask peer iWorkers' }));

    expect(screen.getAllByText('Ask peer iWorkers').length).toBeGreaterThan(1);
    expect(screen.getByText(/Center-mediated A2A discussion/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Use this route' }));

    expect(await screen.findByDisplayValue('Ask peer iWorkers')).toBeTruthy();
    expect(screen.getByDisplayValue(/Please discuss this with the relevant peer iWorkers through iWorkerCenter/)).toBeTruthy();
  });
  it('adds Center memory context from the home composer', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Command inbox for Center pushes and human teamwork' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Memory' }));

    expect(screen.getByDisplayValue(/Use Center memory for context/)).toBeTruthy();
  });

  it('opens memory settings from the home quick start', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Command inbox for Center pushes and human teamwork' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Capture memory/ }));

    expect(await screen.findByText('Memory Capture')).toBeTruthy();
  });
  it('runs a home self-check across Center memory runtime and watcher queue', async () => {
    render(<App />);

    expect(await screen.findByText(/acme\/ops/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Run self-check/ }));

    expect(screen.getByDisplayValue(/Run iWorker body self-check/)).toBeTruthy();
    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(FetchWorkerMemoryStats).toHaveBeenCalled();
      expect(HeartbeatAgentRuntime).toHaveBeenCalled();
      expect(FetchAgentInstances).toHaveBeenCalled();
      expect(FetchGoalPushes).toHaveBeenCalled();
      expect(screen.getByText('Self-check complete')).toBeTruthy();
      expect(screen.getAllByText('Center registration').length).toBeGreaterThan(1);
      expect(screen.getAllByText('Memory authority').length).toBeGreaterThan(1);
      expect(screen.getByText('Agent runtime')).toBeTruthy();
      expect(screen.getByText('Goal watcher queue')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Create readiness task' }));

    expect(await screen.findByDisplayValue('iWorker readiness repair')).toBeTruthy();
    expect(screen.getByDisplayValue(/Create an iWorker readiness repair task/)).toBeTruthy();
  });

  it('summarizes digital coworker work status across Center pushes and local history', async () => {
    vi.mocked(FetchGoalPushes).mockResolvedValue([
      { event_id: 'evt-open', task_id: 'task-open', title: 'Check delivery exception', to_colleague_id: 'worker-ops', to_role_code: 'ops', status: 'pending', reason: 'goal_push', recommended_action: 'resume_task', age_seconds: 120, created_at: '2026-05-01T00:00:00Z' },
      { event_id: 'evt-done', task_id: 'task-done', title: 'Restart completed', to_colleague_id: 'worker-ops', to_role_code: 'ops', status: 'acked', reason: 'goal_push', recommended_action: 'restart_executor', age_seconds: 60, created_at: '2026-05-01T00:00:00Z' },
      { event_id: 'evt-blocked', task_id: 'task-blocked', title: 'Needs credentials', to_colleague_id: 'worker-ops', to_role_code: 'ops', status: 'blocked', reason: 'missing_input', recommended_action: 'ask_human', age_seconds: 300, created_at: '2026-05-01T00:00:00Z' },
    ] as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      { id: 'hist-done', title: 'Daily brief', owner: 'Ops iWorker', status: 'completed', updated_at: '09:00', description: '', draft: '', expected_output: 'summary', result: '', model: '' },
      { id: 'hist-review', title: 'Approve order reply', owner: 'Office iWorker', status: 'waiting approval', updated_at: '09:30', description: '', draft: '', expected_output: 'document', result: '', model: '' },
      { id: 'hist-blocked', title: 'Missing SAP access', owner: 'Data iWorker', status: 'blocked by credentials', updated_at: '10:00', description: '', draft: '', expected_output: 'summary', result: '', model: '' },
    ] as never);

    render(<App />);

    const board = await screen.findByRole('region', { name: 'Digital coworker work status' });
    await waitFor(() => {
      expect(within(board).getAllByText('Check delivery exception').length).toBeGreaterThan(0);
      expect(screen.getByText('Needs credentials')).toBeTruthy();
      expect(within(board).getByText('Active').previousElementSibling?.textContent).toBe('1');
      expect(within(board).getByText('Completed').previousElementSibling?.textContent).toBe('2');
      expect(within(board).getByText('Review').previousElementSibling?.textContent).toBe('1');
      expect(within(board).getByText('Blocked').previousElementSibling?.textContent).toBe('2');
    });
  });
  it('tests center health from settings page and shows snapshot', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);

    expect(await screen.findByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /中心连接/ }));

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('/tmp/center.json')).toBeTruthy();
    });
  });

  it('refreshes worker memory stats from iWorkerCenter', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByText('记忆沉淀')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '刷新记忆' }));

    await waitFor(() => {
      expect(FetchWorkerMemoryStats).toHaveBeenCalledTimes(1);
      expect(screen.getAllByText(/7/).length).toBeGreaterThan(0);
      expect(screen.getByText('acme / ops / worker-1')).toBeTruthy();
    });
  });

  it('discovers and applies Center enrollment from settings', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByText('Center Enrollment')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Discover Center' }));

    await waitFor(() => {
      expect(DiscoverCenterEnrollment).toHaveBeenCalledTimes(1);
      expect(DiscoverCenterEnrollment).toHaveBeenCalledTimes(1);
    });

    fireEvent.change(screen.getByPlaceholderText('alice@example.com'), { target: { value: 'alice' } });
    fireEvent.change(screen.getByPlaceholderText('Required before binding'), { target: { value: 'secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Bind here' }));

    await waitFor(() => {
      expect(ApplyCenterEnrollment).toHaveBeenCalledTimes(1);
      expect(ApplyCenterEnrollment).toHaveBeenCalledTimes(1);
    });

    const applied = vi.mocked(ApplyCenterEnrollment).mock.calls[0]?.[0] as { worker_id?: string; tenant_id?: string; department_id?: string };
    expect(applied.worker_id).toBe('worker-ops');
    expect(applied.tenant_id).toBe('acme');
    expect(applied.department_id).toBe('ops');
    expect((applied as any).auth_method).toBe('local');
    expect((applied as any).auth_username).toBe('alice');
    expect((applied as any).auth_password).toBe('secret');
  });
  it('saves tenant department and worker context for center registration', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByDisplayValue('ops')).toBeTruthy();

    fireEvent.change(screen.getByDisplayValue('ops'), { target: { value: 'quality' } });
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as { center?: { tenant_id?: string; department_id?: string; worker_id?: string } };
    expect(saved.center?.tenant_id).toBe('acme');
    expect(saved.center?.department_id).toBe('quality');
    expect(saved.center?.worker_id).toBe('worker-1');
  });
  it('saves worker memory to the registered iWorkerCenter', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByText('Memory Capture')).toBeTruthy();

    fireEvent.change(screen.getByDisplayValue('Personal memory'), { target: { value: 'department' } });
    fireEvent.change(screen.getByPlaceholderText('note'), { target: { value: 'policy' } });
    fireEvent.change(screen.getByPlaceholderText('policy, preference'), { target: { value: 'handoff, sla' } });
    fireEvent.change(screen.getByPlaceholderText('Write a reusable fact, rule, preference, or handoff note.'), {
      target: { value: 'Escalate red orders before 10am.' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Save memory' }));

    await waitFor(() => {
      expect(SaveWorkerMemory).toHaveBeenCalledTimes(1);
      expect(screen.getByText('Memory saved to iWorkerCenter.')).toBeTruthy();
    });

    const saved = vi.mocked(SaveWorkerMemory).mock.calls[0]?.[0] as { scope?: string; content?: string; category?: string; tags?: string[]; source_type?: string };
    expect(saved.scope).toBe('department');
    expect(saved.content).toBe('Escalate red orders before 10am.');
    expect(saved.category).toBe('policy');
    expect(saved.tags).toEqual(['handoff', 'sla']);
    expect(saved.source_type).toBe('iworker-gui');
    expect(FetchWorkerMemoryStats).toHaveBeenCalledTimes(1);
  });
  it('recalls visible worker memories from iWorkerCenter', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByText('Memory Browser')).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('Search registered center memory'), { target: { value: 'red orders' } });
    fireEvent.click(screen.getByRole('button', { name: 'Recall memories' }));

    await waitFor(() => {
      expect(RecallWorkerMemories).toHaveBeenCalledWith('red orders');
      expect(screen.getByText('Escalate red orders before 10am.')).toBeTruthy();
      expect(screen.getByText('department / policy')).toBeTruthy();
    });
  });
  it('deletes recalled worker memory from iWorkerCenter', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);
    expect(await screen.findByText('Memory Browser')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Recall memories' }));
    expect(await screen.findByText('Escalate red orders before 10am.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Forget' }));

    await waitFor(() => {
      expect(DeleteWorkerMemory).toHaveBeenCalledWith('mem-1');
      expect(screen.queryByText('Escalate red orders before 10am.')).toBeNull();
    });
  });
});
