import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import App from './App';
import { CheckCenterHealth, LoadDiWorkerSettings, LoadTaskHistory, SaveDiWorkerSettings, SaveTaskHistory, SubmitTask } from '../wailsjs/go/main/App';

class MockFileReader {
  public result: string | ArrayBuffer | null = null;
  public onload: null | (() => void) = null;
  public onerror: null | (() => void) = null;

  readAsText(file: Blob) {
    file.text().then((text) => {
      this.result = text;
      this.onload?.();
    }).catch(() => {
      this.onerror?.();
    });
  }
}

vi.mock('../wailsjs/go/main/App', () => ({
  CheckCenterHealth: vi.fn(),
  LoadDiWorkerSettings: vi.fn(),
  LoadTaskHistory: vi.fn(),
  SaveDiWorkerSettings: vi.fn(),
  SaveTaskHistory: vi.fn(),
  SubmitTask: vi.fn(),
}));

describe('App history persistence', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('FileReader', MockFileReader as unknown as typeof FileReader);
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 2,
      config_path: '/tmp/iworkercenter/settings.json',
      message: 'ok',
      resolved_base_url: 'http://127.0.0.1:8714',
    } as never);
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([] as never);
    vi.mocked(SaveDiWorkerSettings).mockResolvedValue(undefined);
    vi.mocked(SaveTaskHistory).mockResolvedValue(undefined);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: '自由输入',
      colleague_name: '自动匹配同事',
      expected_output: 'summary',
      model: 'test-model',
      content: '默认返回内容',
    } as never);
    Object.defineProperty(window, 'go', {
      value: {},
      configurable: true,
      writable: true,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanup();
  });

  it('tests center health from settings page and shows snapshot', async () => {
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 3,
      config_path: '/tmp/center.json',
      message: 'ok',
      resolved_base_url: 'http://127.0.0.1:8714',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '测试中心连接' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('连接正常')).toBeTruthy();
      expect(screen.getByText('中心连接正常')).toBeTruthy();
      expect(screen.getAllByText('http://127.0.0.1:8714').length).toBeGreaterThan(0);
      expect(screen.getByText('3')).toBeTruthy();
      expect(screen.getByText('手动检测')).toBeTruthy();
      expect(screen.getByText((content) => /^\d{2}-\d{2} \d{2}:\d{2}$/.test(content))).toBeTruthy();
      expect(screen.getByText('/tmp/center.json')).toBeTruthy();
    });
  });

  it('shows offline center health hint when Wails bridge is unavailable', async () => {
    delete (window as Window & { go?: unknown }).go;

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '测试中心连接' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(screen.getByText('当前未连接 Wails，无法测试中心连接。')).toBeTruthy();
    });

    expect(CheckCenterHealth).not.toHaveBeenCalled();
  });

  it('clears stale center health after settings change', async () => {
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '测试中心连接' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(screen.getByText('中心连接正常')).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('小迪'), {
      target: { value: '小迪新' },
    });

    await waitFor(() => {
      expect(screen.queryByText('中心连接正常')).toBeNull();
      expect(screen.queryByText('Provider 数量：2')).toBeNull();
      expect(screen.getAllByText('有未保存更改').length).toBeGreaterThan(0);
    });
  });

  it('shows unsaved settings state and clears it after save', async () => {
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getAllByText('当前已保存').length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('小迪'), {
      target: { value: '小迪待保存' },
    });

    await waitFor(() => {
      expect(screen.getAllByText('有未保存更改').length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '保存配置' })).toBeTruthy();
      expect(screen.getByText('当前修改尚未保存。')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getAllByText('当前已保存').length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
      expect(screen.getByText('配置已保存')).toBeTruthy();
    });
  });

  it('rechecks center health after saving settings', async () => {
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 4,
      config_path: '/tmp/after-save-center.json',
      message: 'ok',
      resolved_base_url: 'http://10.0.0.8:8714',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('小迪'), {
      target: { value: '小迪助手' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存配置' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('配置已保存')).toBeTruthy();
      expect(screen.getByText('连接正常')).toBeTruthy();
      expect(screen.getByText('中心连接正常')).toBeTruthy();
      expect(screen.getByText('4')).toBeTruthy();
      expect(screen.getByText('保存后自动检测')).toBeTruthy();
      expect(screen.getByText((content) => /^\d{2}-\d{2} \d{2}:\d{2}$/.test(content))).toBeTruthy();
      expect(screen.getByText('/tmp/after-save-center.json')).toBeTruthy();
    });
  });

  it('keeps save success message when post-save health check fails', async () => {
    vi.mocked(CheckCenterHealth).mockRejectedValue(new Error('health probe failed'));

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('小迪'), {
      target: { value: '保存后探测失败' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存配置' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('配置已保存')).toBeTruthy();
      expect(screen.getByText('探测异常')).toBeTruthy();
      expect(screen.getByText('health probe failed')).toBeTruthy();
    });

    expect(screen.queryByText('保存配置失败')).toBeNull();
  });

  it('shows unreachable badge when center health returns unreachable status', async () => {
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: false,
      status: '',
      provider_count: 0,
      config_path: '',
      message: 'dial tcp 127.0.0.1:8714: connect: connection refused',
      resolved_base_url: 'http://127.0.0.1:8714',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '测试中心连接' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(screen.getByText('连接不可达')).toBeTruthy();
      expect(screen.getByText('中心暂不可达')).toBeTruthy();
      expect(screen.getByText('dial tcp 127.0.0.1:8714: connect: connection refused')).toBeTruthy();
    });
  });

  it('shows provider summaries by default and expands one provider at a time', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '阿宁',
        description: '擅长数据汇总与结构化整理。',
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 8714,
        base_url: 'http://10.0.0.8:8714',
        timeout_sec: 45,
      },
      routing: {
        mode: 'priority',
        default_provider: 'analysis-anthropic',
        allow_fallback: true,
      },
      providers: [
        {
          id: 'analysis-anthropic',
          name: '分析归因服务',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['分析', '归因'],
          description: '适合异常归因',
          capabilities: {
            supports_stream: true,
            supports_vision: false,
            max_context: 200000,
          },
        },
        {
          id: 'office-openai',
          name: '办公写作服务',
          enabled: true,
          protocol: 'openai',
          base_url: 'https://office.example.com',
          api_key: 'token-b',
          model: 'gpt-4.1',
          priority: 80,
          features: ['公文', '中文'],
          description: '适合公文草拟',
          capabilities: {
            supports_stream: true,
            supports_vision: true,
            max_context: 128000,
          },
        },
      ],
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByText('分析归因服务')).toBeTruthy();
      expect(screen.getByText('办公写作服务')).toBeTruthy();
      expect(screen.getByRole('button', { name: '展开编辑 分析归因服务' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '展开编辑 办公写作服务' })).toBeTruthy();
    });

    expect(screen.queryByDisplayValue('https://analysis.example.com')).toBeNull();
    expect(screen.queryByDisplayValue('https://office.example.com')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '展开编辑 分析归因服务' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '收起编辑 分析归因服务' })).toBeTruthy();
      expect(screen.getByDisplayValue('https://analysis.example.com')).toBeTruthy();
    });

    expect(screen.queryByDisplayValue('https://office.example.com')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '展开编辑 办公写作服务' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开编辑 分析归因服务' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '收起编辑 办公写作服务' })).toBeTruthy();
      expect(screen.queryByDisplayValue('https://analysis.example.com')).toBeNull();
      expect(screen.getByDisplayValue('https://office.example.com')).toBeTruthy();
    });
  });

  it('saves expanded provider edits and keeps feature parsing', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '阿宁',
        description: '擅长数据汇总与结构化整理。',
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 8714,
        base_url: 'http://10.0.0.8:8714',
        timeout_sec: 45,
      },
      routing: {
        mode: 'priority',
        default_provider: 'analysis-anthropic',
        allow_fallback: true,
      },
      providers: [
        {
          id: 'analysis-anthropic',
          name: '分析归因服务',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['分析', '归因'],
          description: '适合异常归因',
          capabilities: {
            supports_stream: true,
            supports_vision: false,
            max_context: 200000,
          },
        },
      ],
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '展开编辑 分析归因服务' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '展开编辑 分析归因服务' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('分析归因服务')).toBeTruthy();
      expect(screen.getByDisplayValue('分析，归因')).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('分析归因服务'), {
      target: { value: '分析归因服务增强版' },
    });
    fireEvent.change(screen.getByDisplayValue('分析，归因'), {
      target: { value: '分析，归因，复盘' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存配置' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getByText('配置已保存')).toBeTruthy();
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as {
      providers: Array<{ name: string; features: string[] }>;
    };
    expect(saved.providers[0]?.name).toBe('分析归因服务增强版');
    expect(saved.providers[0]?.features).toEqual(['分析', '归因', '复盘']);
  });

  it('opens settings from side entry and saves role profile through Wails bridge', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '阿宁',
        description: '擅长数据汇总与结构化整理。',
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 8714,
        base_url: 'http://10.0.0.8:8714',
        timeout_sec: 45,
      },
      routing: {
        mode: 'priority',
        default_provider: 'analysis-anthropic',
        allow_fallback: true,
      },
      providers: [
        {
          id: 'analysis-anthropic',
          name: '分析归因服务',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['分析', '归因'],
          description: '适合异常归因',
          capabilities: {
            supports_stream: true,
            supports_vision: false,
            max_context: 200000,
          },
        },
      ],
    } as never);

    render(<App />);

    await waitFor(() => {
      expect(screen.getByLabelText('当前角色信息').textContent).toContain('阿宁');
      expect(screen.getByLabelText('当前角色信息').textContent).toContain('擅长数据汇总与结构化整理。');
    });

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();
      expect(screen.getByDisplayValue('阿宁')).toBeTruthy();
      expect(screen.getByDisplayValue('http://10.0.0.8:8714')).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('阿宁'), {
      target: { value: '阿宁助手' },
    });
    fireEvent.change(screen.getByDisplayValue('擅长数据汇总与结构化整理。'), {
      target: { value: '负责数据清洗、汇总和分析输出。' },
    });
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getByText('配置已保存')).toBeTruthy();
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as {
      role_profile: { name: string; description: string };
      center: { base_url: string };
    };
    expect(saved.role_profile.name).toBe('阿宁助手');
    expect(saved.role_profile.description).toBe('负责数据清洗、汇总和分析输出。');
    expect(saved.center.base_url).toBe('http://10.0.0.8:8714');
  });

  it('shows offline save hint when Wails bridge is unavailable', async () => {
    delete (window as Window & { go?: unknown }).go;

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();
      expect(screen.getByText('等待 Wails 绑定')).toBeTruthy();
      expect(screen.getByRole('button', { name: '已保存' })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('小迪'), {
      target: { value: '离线修改' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存配置' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));

    await waitFor(() => {
      expect(screen.getByText('当前未连接 Wails，配置仅保留在当前界面。')).toBeTruthy();
    });

    expect(SaveDiWorkerSettings).not.toHaveBeenCalled();
  });

  it('loads persisted history and maps Wails fields for display and resume', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-201',
        title: '补写日报',
        owner: '小迪',
        status: '已完成',
        updated_at: '04-07 10:30',
        description: '把早会内容整理成日报',
        draft: '根据早会记录补写日报',
        expected_output: 'document',
        result: '日报正文',
        model: 'model-a',
      },
    ] as never);

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText('补写日报')).toBeTruthy();
      expect(screen.getByText('04-07 10:30')).toBeTruthy();
    });

    fireEvent.click(screen.getByText('补写日报'));

    await waitFor(() => {
      expect(screen.getByDisplayValue('补写日报')).toBeTruthy();
      expect(screen.getByDisplayValue('根据早会记录补写日报')).toBeTruthy();
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('document');
      expect(screen.getByText('处理结果')).toBeTruthy();
      expect(screen.getByText('日报正文')).toBeTruthy();
    });
  });

  it('views history result without entering edit mode', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-301',
        title: '周会纪要',
        owner: '小迪',
        status: '已完成',
        updated_at: '04-07 11:00',
        description: '整理周会结论和待办',
        draft: '根据会议录音整理周会纪要',
        expected_output: 'document',
        result: '这是历史纪要结果',
        model: 'model-c',
      },
    ] as never);

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText('周会纪要')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: /历史任务/ }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '查看结果' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '查看结果' }));

    await waitFor(() => {
      expect(screen.getByText('历史结果')).toBeTruthy();
      expect(screen.getByText('这是历史纪要结果')).toBeTruthy();
      expect(screen.getByText('正在查看：周会纪要')).toBeTruthy();
      expect(screen.queryByDisplayValue('根据会议录音整理周会纪要')).toBeNull();
    });
  });

  it('selects colleague from home and carries the colleague into new task page', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '找 TA 帮忙' })[0]);

    await waitFor(() => {
      expect(screen.getByDisplayValue('写通知')).toBeTruthy();
      expect(screen.getByText('当前已选同事')).toBeTruthy();
      expect(screen.queryByText('暂未指定，按任务自动匹配')).toBeNull();
      expect(screen.getByText('已选同事：小迪')).toBeTruthy();
    });
  });

  it('clones a history task into new task mode without carrying old result view', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-401',
        title: '月报整理',
        owner: '小迪',
        status: '已完成',
        updated_at: '04-07 14:30',
        description: '整理本月经营数据和重点事项',
        draft: '根据经营看板整理月报初稿',
        expected_output: 'document',
        result: '这是旧月报结果',
        model: 'model-d',
      },
    ] as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: /历史任务/ }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '复制为新任务' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '复制为新任务' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('月报整理')).toBeTruthy();
      expect(screen.getByDisplayValue('根据经营看板整理月报初稿')).toBeTruthy();
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('document');
      expect(screen.queryByText('处理结果')).toBeNull();
      expect(screen.queryByText('这是旧月报结果')).toBeNull();
      expect(screen.queryByText('已选同事：小迪')).toBeNull();
      expect(screen.getByText('暂未指定，按任务自动匹配')).toBeTruthy();
    });
  });

  it('clears current task state from new task page', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);
    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '请整理本周异常日报' },
    });
    fireEvent.click(screen.getByRole('button', { name: '选择同事' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '找同事' })).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '找 TA 帮忙' })[0]);

    await waitFor(() => {
      expect(screen.getByDisplayValue('写通知')).toBeTruthy();
      expect(screen.getByText('已选同事：小迪')).toBeTruthy();
    });

    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '请整理本周异常日报' },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['异常已恢复。'], '补充说明.txt', { type: 'text/plain' });
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('异常已恢复。'),
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('补充说明.txt')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '清空当前任务' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('自由输入')).toBeTruthy();
      expect((screen.getByPlaceholderText('请告诉我你想完成什么工作') as HTMLTextAreaElement).value).toBe('');
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('summary');
      expect(screen.getByText('暂未指定，按任务自动匹配')).toBeTruthy();
      expect(screen.queryByText('补充说明.txt')).toBeNull();
      expect(screen.queryByText('已选同事：小迪')).toBeNull();
    });
  });

  it('applies suggestion text into draft and appends on repeated clicks', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);

    fireEvent.click(screen.getByRole('button', { name: '日报整理' }));

    const draftInput = screen.getByPlaceholderText('请告诉我你想完成什么工作') as HTMLTextAreaElement;

    await waitFor(() => {
      expect(draftInput.value).toBe('请根据今天的工作进展整理一份日报，突出已完成事项、当前风险和明日计划。');
    });

    fireEvent.click(screen.getByRole('button', { name: '异常说明' }));

    await waitFor(() => {
      expect(draftInput.value).toBe('请根据今天的工作进展整理一份日报，突出已完成事项、当前风险和明日计划。\n\n请说明本次异常的现象、影响范围、初步原因、当前处理进度和后续建议。');
    });
  });

  it('deletes a history task and persists the remaining list', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-501',
        title: '待删日报',
        owner: '小迪',
        status: '已完成',
        updated_at: '04-07 16:00',
        description: '删除前的第一条任务',
        draft: '删除前的第一条任务草稿',
        expected_output: 'summary',
        result: '第一条结果',
        model: 'model-e',
      },
      {
        id: 'task-502',
        title: '保留纪要',
        owner: '小美',
        status: '已完成',
        updated_at: '04-07 16:10',
        description: '删除后应该保留',
        draft: '保留任务草稿',
        expected_output: 'document',
        result: '第二条结果',
        model: 'model-f',
      },
    ] as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: /历史任务/ }));

    await waitFor(() => {
      expect(screen.getByText('待删日报')).toBeTruthy();
      expect(screen.getByText('保留纪要')).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '删除任务' })[0]);

    await waitFor(() => {
      expect(screen.queryByText('待删日报')).toBeNull();
      expect(screen.getByText('保留纪要')).toBeTruthy();
    });

    expect(SaveTaskHistory).toHaveBeenCalledTimes(1);
    const saved = vi.mocked(SaveTaskHistory).mock.calls[0]?.[0] as unknown as Array<{
      id: string;
      title: string;
    }>;
    expect(saved).toHaveLength(1);
    expect(saved[0].id).toBe('task-502');
    expect(saved[0].title).toBe('保留纪要');
  });

  it('adds attachments and includes material summary in submit payload', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);
    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '请整理本周产线异常' },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['产线异常集中在二号线，需优先说明原因和影响。'], '异常记录.txt', { type: 'text/plain' });
    const expectedSizeLabel = `${file.size} B`;
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('产线异常集中在二号线，需优先说明原因和影响。'),
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('已添加材料')).toBeTruthy();
      expect(screen.getByText('异常记录.txt')).toBeTruthy();
      expect(screen.getByText(`text/plain · ${expectedSizeLabel}`)).toBeTruthy();
      expect(screen.getByText('产线异常集中在二号线，需优先说明原因和影响。')).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '开始处理' })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '自由输入',
      selected_colleague_name: '',
      draft: `请整理本周产线异常\n\n补充材料：\n1. 异常记录.txt（text/plain，${expectedSizeLabel}）：产线异常集中在二号线，需优先说明原因和影响。\n产线异常集中在二号线，需优先说明原因和影响。`,
      expected_output: 'summary',
    });
  });

  it('shows non-text attachment metadata without reading body content', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);
    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '请整理设备现场照片说明' },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['binary-image-content'], '现场照片.png', { type: 'image/png' });
    const expectedSizeLabel = `${file.size} B`;
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('现场照片.png')).toBeTruthy();
      expect(screen.getByText(`image/png · ${expectedSizeLabel}`)).toBeTruthy();
      expect(screen.getByText('非文本材料已上传，可结合文件类型和文件名一起处理。')).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '开始处理' })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '自由输入',
      selected_colleague_name: '',
      draft: `请整理设备现场照片说明\n\n补充材料：\n1. 现场照片.png（image/png，${expectedSizeLabel}）：非文本材料已上传，可结合文件类型和文件名一起处理。`,
      expected_output: 'summary',
    });
  });

  it('handles multiple attachments and only submits retained files after removal', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);
    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '请汇总今日异常与现场情况' },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const textFile = new File(['一号线在下午出现短暂停机。'], '异常摘要.txt', { type: 'text/plain' });
    const imageFile = new File(['image-binary'], '现场照片.png', { type: 'image/png' });
    const textSizeLabel = `${textFile.size} B`;
    Object.defineProperty(textFile, 'text', {
      value: () => Promise.resolve('一号线在下午出现短暂停机。'),
    });

    fireEvent.change(fileInput, { target: { files: [textFile, imageFile] } });

    await waitFor(() => {
      expect(screen.getByText('共 2 份材料')).toBeTruthy();
      expect(screen.getByText('异常摘要.txt')).toBeTruthy();
      expect(screen.getByText('现场照片.png')).toBeTruthy();
    });

    const removeButtons = screen.getAllByRole('button', { name: '移除' });
    fireEvent.click(removeButtons[1]);

    await waitFor(() => {
      expect(screen.getByText('共 1 份材料')).toBeTruthy();
      expect(screen.getByText('异常摘要.txt')).toBeTruthy();
      expect(screen.queryByText('现场照片.png')).toBeNull();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '开始处理' })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '自由输入',
      selected_colleague_name: '',
      draft: `请汇总今日异常与现场情况\n\n补充材料：\n1. 异常摘要.txt（text/plain，${textSizeLabel}）：一号线在下午出现短暂停机。\n一号线在下午出现短暂停机。`,
      expected_output: 'summary',
    });
  });

  it('appends submitted result to history and persists mapped payload', async () => {
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: '自由输入',
      colleague_name: '自动匹配同事',
      expected_output: 'summary',
      model: 'model-b',
      content: '新的处理结果',
    } as never);

    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '开始新任务' })[0]);
    fireEvent.change(screen.getByPlaceholderText('请告诉我你想完成什么工作'), {
      target: { value: '整理今天的日报内容' },
    });
    fireEvent.click(screen.getAllByRole('button', { name: '开始处理' })[0]);

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '历史任务' })).toBeTruthy();
      expect(screen.getAllByText('自由输入').length).toBeGreaterThan(0);
      expect(screen.getByText('整理今天的日报内容')).toBeTruthy();
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '自由输入',
      selected_colleague_name: '',
      draft: '整理今天的日报内容',
      expected_output: 'summary',
    });

    expect(SaveTaskHistory).toHaveBeenCalledTimes(1);
    const saved = vi.mocked(SaveTaskHistory).mock.calls[0]?.[0] as unknown as Array<{
      title: string;
      owner: string;
      description: string;
      expected_output?: string;
      result?: string;
      model?: string;
      updated_at?: string;
    }>;
    expect(saved[0].title).toBe('自由输入');
    expect(saved[0].owner).toBe('自动匹配同事');
    expect(saved[0].description).toBe('整理今天的日报内容');
    expect(saved[0].expected_output).toBe('summary');
    expect(saved[0].result).toBe('新的处理结果');
    expect(saved[0].model).toBe('model-b');
    expect(saved[0].updated_at).toMatch(/^\d{2}-\d{2} \d{2}:\d{2}$/);
  });
});
