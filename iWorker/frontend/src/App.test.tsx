import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import App from './App';
import { AckGoalPush, AutoHandleGoalPush, CheckCenterHealth, DeleteWorkerMemory, FetchAgentInstances, FetchGoalPushes, FetchWorkerMemoryStats, GetGoalWatchAutoHandleStatus, HeartbeatAgentRuntime, LoadDiWorkerSettings, LoadTaskHistory, RecallWorkerMemories, SaveDiWorkerSettings, SaveTaskHistory, SaveWorkerMemory, SubmitTask } from '../wailsjs/go/main/App';

vi.mock('../wailsjs/go/main/App', () => ({
  AckGoalPush: vi.fn(),
  AutoHandleGoalPush: vi.fn(),
  CheckCenterHealth: vi.fn(),
  DeleteWorkerMemory: vi.fn(),
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
    name: '小迪',
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
      name: '办公写作服务',
      enabled: true,
      protocol: 'openai',
      base_url: 'https://office.example.com/v1',
      api_key: '',
      model: 'gpt-4.1',
      priority: 100,
      features: ['公文', '纪要'],
      description: '适合通知、纪要、日报与正式文档。',
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
      task_type: '自由输入',
      colleague_name: '自动匹配同事',
      expected_output: 'summary',
      model: 'test-model',
      content: '默认返回内容',
    } as never);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanup();
  });

  it('adds Center memory context from the home composer', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'What should your iWorker handle next?' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Memory' }));

    expect(screen.getByDisplayValue(/Use Center memory for context/)).toBeTruthy();
  });

  it('opens memory settings from the home quick start', async () => {
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'What should your iWorker handle next?' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Capture memory/ }));

    expect(await screen.findByText('Memory Capture')).toBeTruthy();
  });
  it('tests center health from settings page and shows snapshot', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: 'Open settings' })[0]);

    expect(await screen.findByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('连接正常')).toBeTruthy();
      expect(screen.getByText('中心连接正常')).toBeTruthy();
      expect(screen.getByText('手动检测')).toBeTruthy();
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
      expect(screen.getByText('7 条')).toBeTruthy();
      expect(screen.getByText('acme / ops / worker-1')).toBeTruthy();
    });
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
