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
      resolved_base_url: 'http://127.0.0.1:9377',
    } as never);
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([] as never);
    vi.mocked(SaveDiWorkerSettings).mockResolvedValue(undefined);
    vi.mocked(SaveTaskHistory).mockResolvedValue(undefined);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: '鑷敱杈撳叆',
      colleague_name: '鑷姩鍖归厤鍚屼簨',
      expected_output: 'summary',
      model: 'test-model',
      content: '榛樿杩斿洖鍐呭',
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
      resolved_base_url: 'http://127.0.0.1:9377',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' }));

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('杩炴帴姝ｅ父')).toBeTruthy();
      expect(screen.getByText('涓績杩炴帴姝ｅ父')).toBeTruthy();
      expect(screen.getAllByText('http://127.0.0.1:9377').length).toBeGreaterThan(0);
      expect(screen.getByText('3')).toBeTruthy();
      expect(screen.getByText('鎵嬪姩妫€娴?)).toBeTruthy();
      expect(screen.getByText((content) => /^\d{2}-\d{2} \d{2}:\d{2}$/.test(content))).toBeTruthy();
      expect(screen.getByText('/tmp/center.json')).toBeTruthy();
    });
  });

  it('shows offline center health hint when Wails bridge is unavailable', async () => {
    delete (window as Window & { go?: unknown }).go;

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' }));

    await waitFor(() => {
      expect(screen.getByText('褰撳墠鏈繛鎺?Wails锛屾棤娉曟祴璇曚腑蹇冭繛鎺ャ€?)).toBeTruthy();
    });

    expect(CheckCenterHealth).not.toHaveBeenCalled();
  });

  it('clears stale center health after settings change', async () => {
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' }));

    await waitFor(() => {
      expect(screen.getByText('涓績杩炴帴姝ｅ父')).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('灏忚开'), {
      target: { value: '灏忚开鏂? },
    });

    await waitFor(() => {
      expect(screen.queryByText('涓績杩炴帴姝ｅ父')).toBeNull();
      expect(screen.queryByText('Provider 鏁伴噺锛?')).toBeNull();
      expect(screen.getAllByText('鏈夋湭淇濆瓨鏇存敼').length).toBeGreaterThan(0);
    });
  });

  it('shows unsaved settings state and clears it after save', async () => {
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getAllByText('褰撳墠宸蹭繚瀛?).length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('灏忚开'), {
      target: { value: '灏忚开寰呬繚瀛? },
    });

    await waitFor(() => {
      expect(screen.getAllByText('鏈夋湭淇濆瓨鏇存敼').length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' })).toBeTruthy();
      expect(screen.getByText('褰撳墠淇敼灏氭湭淇濆瓨銆?)).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getAllByText('褰撳墠宸蹭繚瀛?).length).toBeGreaterThan(0);
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
      expect(screen.getByText('閰嶇疆宸蹭繚瀛?)).toBeTruthy();
    });
  });

  it('rechecks center health after saving settings', async () => {
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 4,
      config_path: '/tmp/after-save-center.json',
      message: 'ok',
      resolved_base_url: 'http://10.0.0.8:9377',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('灏忚开'), {
      target: { value: '灏忚开鍔╂墜' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('閰嶇疆宸蹭繚瀛?)).toBeTruthy();
      expect(screen.getByText('杩炴帴姝ｅ父')).toBeTruthy();
      expect(screen.getByText('涓績杩炴帴姝ｅ父')).toBeTruthy();
      expect(screen.getByText('4')).toBeTruthy();
      expect(screen.getByText('淇濆瓨鍚庤嚜鍔ㄦ娴?)).toBeTruthy();
      expect(screen.getByText((content) => /^\d{2}-\d{2} \d{2}:\d{2}$/.test(content))).toBeTruthy();
      expect(screen.getByText('/tmp/after-save-center.json')).toBeTruthy();
    });
  });

  it('keeps save success message when post-save health check fails', async () => {
    vi.mocked(CheckCenterHealth).mockRejectedValue(new Error('health probe failed'));

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('灏忚开'), {
      target: { value: '淇濆瓨鍚庢帰娴嬪け璐? },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('閰嶇疆宸蹭繚瀛?)).toBeTruthy();
      expect(screen.getByText('鎺㈡祴寮傚父')).toBeTruthy();
      expect(screen.getByText('health probe failed')).toBeTruthy();
    });

    expect(screen.queryByText('淇濆瓨閰嶇疆澶辫触')).toBeNull();
  });

  it('shows unreachable badge when center health returns unreachable status', async () => {
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: false,
      status: '',
      provider_count: 0,
      config_path: '',
      message: 'dial tcp 127.0.0.1:9377: connect: connection refused',
      resolved_base_url: 'http://127.0.0.1:9377',
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '娴嬭瘯涓績杩炴帴' }));

    await waitFor(() => {
      expect(screen.getByText('杩炴帴涓嶅彲杈?)).toBeTruthy();
      expect(screen.getByText('涓績鏆備笉鍙揪')).toBeTruthy();
      expect(screen.getByText('dial tcp 127.0.0.1:9377: connect: connection refused')).toBeTruthy();
    });
  });

  it('shows provider summaries by default and expands one provider at a time', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '闃垮畞',
        description: '鎿呴暱鏁版嵁姹囨€讳笌缁撴瀯鍖栨暣鐞嗐€?,
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 9377,
        base_url: 'http://10.0.0.8:9377',
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
          name: '鍒嗘瀽褰掑洜鏈嶅姟',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['鍒嗘瀽', '褰掑洜'],
          description: '閫傚悎寮傚父褰掑洜',
          capabilities: {
            supports_stream: true,
            supports_vision: false,
            max_context: 200000,
          },
        },
        {
          id: 'office-openai',
          name: '鍔炲叕鍐欎綔鏈嶅姟',
          enabled: true,
          protocol: 'openai',
          base_url: 'https://office.example.com',
          api_key: 'token-b',
          model: 'gpt-4.1',
          priority: 80,
          features: ['鍏枃', '涓枃'],
          description: '閫傚悎鍏枃鑽夋嫙',
          capabilities: {
            supports_stream: true,
            supports_vision: true,
            max_context: 128000,
          },
        },
      ],
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByText('鍒嗘瀽褰掑洜鏈嶅姟')).toBeTruthy();
      expect(screen.getByText('鍔炲叕鍐欎綔鏈嶅姟')).toBeTruthy();
      expect(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍔炲叕鍐欎綔鏈嶅姟' })).toBeTruthy();
    });

    expect(screen.queryByDisplayValue('https://analysis.example.com')).toBeNull();
    expect(screen.queryByDisplayValue('https://office.example.com')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '鏀惰捣缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' })).toBeTruthy();
      expect(screen.getByDisplayValue('https://analysis.example.com')).toBeTruthy();
    });

    expect(screen.queryByDisplayValue('https://office.example.com')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍔炲叕鍐欎綔鏈嶅姟' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '鏀惰捣缂栬緫 鍔炲叕鍐欎綔鏈嶅姟' })).toBeTruthy();
      expect(screen.queryByDisplayValue('https://analysis.example.com')).toBeNull();
      expect(screen.getByDisplayValue('https://office.example.com')).toBeTruthy();
    });
  });

  it('saves expanded provider edits and keeps feature parsing', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '闃垮畞',
        description: '鎿呴暱鏁版嵁姹囨€讳笌缁撴瀯鍖栨暣鐞嗐€?,
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 9377,
        base_url: 'http://10.0.0.8:9377',
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
          name: '鍒嗘瀽褰掑洜鏈嶅姟',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['鍒嗘瀽', '褰掑洜'],
          description: '閫傚悎寮傚父褰掑洜',
          capabilities: {
            supports_stream: true,
            supports_vision: false,
            max_context: 200000,
          },
        },
      ],
    } as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' })).toBeTruthy();
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '灞曞紑缂栬緫 鍒嗘瀽褰掑洜鏈嶅姟' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('鍒嗘瀽褰掑洜鏈嶅姟')).toBeTruthy();
      expect(screen.getByDisplayValue('鍒嗘瀽锛屽綊鍥?)).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('鍒嗘瀽褰掑洜鏈嶅姟'), {
      target: { value: '鍒嗘瀽褰掑洜鏈嶅姟澧炲己鐗? },
    });
    fireEvent.change(screen.getByDisplayValue('鍒嗘瀽锛屽綊鍥?), {
      target: { value: '鍒嗘瀽锛屽綊鍥狅紝澶嶇洏' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getByText('閰嶇疆宸蹭繚瀛?)).toBeTruthy();
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as {
      providers: Array<{ name: string; features: string[] }>;
    };
    expect(saved.providers[0]?.name).toBe('鍒嗘瀽褰掑洜鏈嶅姟澧炲己鐗?);
    expect(saved.providers[0]?.features).toEqual(['鍒嗘瀽', '褰掑洜', '澶嶇洏']);
  });

  it('opens settings from side entry and saves role profile through Wails bridge', async () => {
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue({
      role_profile: {
        name: '闃垮畞',
        description: '鎿呴暱鏁版嵁姹囨€讳笌缁撴瀯鍖栨暣鐞嗐€?,
      },
      center: {
        enabled: true,
        host: '10.0.0.8',
        port: 9377,
        base_url: 'http://10.0.0.8:9377',
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
          name: '鍒嗘瀽褰掑洜鏈嶅姟',
          enabled: true,
          protocol: 'anthropic',
          base_url: 'https://analysis.example.com',
          api_key: 'token-a',
          model: 'claude-sonnet-4-6',
          priority: 90,
          features: ['鍒嗘瀽', '褰掑洜'],
          description: '閫傚悎寮傚父褰掑洜',
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
      expect(screen.getByLabelText('褰撳墠瑙掕壊淇℃伅').textContent).toContain('闃垮畞');
      expect(screen.getByLabelText('褰撳墠瑙掕壊淇℃伅').textContent).toContain('鎿呴暱鏁版嵁姹囨€讳笌缁撴瀯鍖栨暣鐞嗐€?);
    });

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '鏁板瓧鍛樺伐涓績閰嶇疆' })).toBeTruthy();
      expect(screen.getByDisplayValue('闃垮畞')).toBeTruthy();
      expect(screen.getByDisplayValue('http://10.0.0.8:9377')).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('闃垮畞'), {
      target: { value: '闃垮畞鍔╂墜' },
    });
    fireEvent.change(screen.getByDisplayValue('鎿呴暱鏁版嵁姹囨€讳笌缁撴瀯鍖栨暣鐞嗐€?), {
      target: { value: '璐熻矗鏁版嵁娓呮礂銆佹眹鎬诲拰鍒嗘瀽杈撳嚭銆? },
    });
    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(SaveDiWorkerSettings).toHaveBeenCalledTimes(1);
      expect(screen.getByText('閰嶇疆宸蹭繚瀛?)).toBeTruthy();
    });

    const saved = vi.mocked(SaveDiWorkerSettings).mock.calls[0]?.[0] as {
      role_profile: { name: string; description: string };
      center: { base_url: string };
    };
    expect(saved.role_profile.name).toBe('闃垮畞鍔╂墜');
    expect(saved.role_profile.description).toBe('璐熻矗鏁版嵁娓呮礂銆佹眹鎬诲拰鍒嗘瀽杈撳嚭銆?);
    expect(saved.center.base_url).toBe('http://10.0.0.8:9377');
  });

  it('shows offline save hint when Wails bridge is unavailable', async () => {
    delete (window as Window & { go?: unknown }).go;

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '鎵撳紑閰嶇疆鐣岄潰' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '鏁板瓧鍛樺伐涓績閰嶇疆' })).toBeTruthy();
      expect(screen.getByText('绛夊緟 Wails 缁戝畾')).toBeTruthy();
      expect(screen.getByRole('button', { name: '宸蹭繚瀛? })).toBeTruthy();
    });

    fireEvent.change(screen.getByDisplayValue('灏忚开'), {
      target: { value: '绂荤嚎淇敼' },
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '淇濆瓨閰嶇疆' }));

    await waitFor(() => {
      expect(screen.getByText('褰撳墠鏈繛鎺?Wails锛岄厤缃粎淇濈暀鍦ㄥ綋鍓嶇晫闈€?)).toBeTruthy();
    });

    expect(SaveDiWorkerSettings).not.toHaveBeenCalled();
  });

  it('loads persisted history and maps Wails fields for display and resume', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-201',
        title: '琛ュ啓鏃ユ姤',
        owner: '灏忚开',
        status: '宸插畬鎴?,
        updated_at: '04-07 10:30',
        description: '鎶婃棭浼氬唴瀹规暣鐞嗘垚鏃ユ姤',
        draft: '鏍规嵁鏃╀細璁板綍琛ュ啓鏃ユ姤',
        expected_output: 'document',
        result: '鏃ユ姤姝ｆ枃',
        model: 'model-a',
      },
    ] as never);

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText('琛ュ啓鏃ユ姤')).toBeTruthy();
      expect(screen.getByText('04-07 10:30')).toBeTruthy();
    });

    fireEvent.click(screen.getByText('琛ュ啓鏃ユ姤'));

    await waitFor(() => {
      expect(screen.getByDisplayValue('琛ュ啓鏃ユ姤')).toBeTruthy();
      expect(screen.getByDisplayValue('鏍规嵁鏃╀細璁板綍琛ュ啓鏃ユ姤')).toBeTruthy();
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('document');
      expect(screen.getByText('澶勭悊缁撴灉')).toBeTruthy();
      expect(screen.getByText('鏃ユ姤姝ｆ枃')).toBeTruthy();
    });
  });

  it('views history result without entering edit mode', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-301',
        title: '鍛ㄤ細绾',
        owner: '灏忚开',
        status: '宸插畬鎴?,
        updated_at: '04-07 11:00',
        description: '鏁寸悊鍛ㄤ細缁撹鍜屽緟鍔?,
        draft: '鏍规嵁浼氳褰曢煶鏁寸悊鍛ㄤ細绾',
        expected_output: 'document',
        result: '杩欐槸鍘嗗彶绾缁撴灉',
        model: 'model-c',
      },
    ] as never);

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText('鍛ㄤ細绾')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: /鍘嗗彶浠诲姟/ }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '鏌ョ湅缁撴灉' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '鏌ョ湅缁撴灉' }));

    await waitFor(() => {
      expect(screen.getByText('鍘嗗彶缁撴灉')).toBeTruthy();
      expect(screen.getByText('杩欐槸鍘嗗彶绾缁撴灉')).toBeTruthy();
      expect(screen.getByText('姝ｅ湪鏌ョ湅锛氬懆浼氱邯瑕?)).toBeTruthy();
      expect(screen.queryByDisplayValue('鏍规嵁浼氳褰曢煶鏁寸悊鍛ㄤ細绾')).toBeNull();
    });
  });

  it('selects colleague from home and carries the colleague into new task page', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '鎵?TA 甯繖' })[0]);

    await waitFor(() => {
      expect(screen.getByDisplayValue('鍐欓€氱煡')).toBeTruthy();
      expect(screen.getByText('褰撳墠宸查€夊悓浜?)).toBeTruthy();
      expect(screen.queryByText('鏆傛湭鎸囧畾锛屾寜浠诲姟鑷姩鍖归厤')).toBeNull();
      expect(screen.getByText('宸查€夊悓浜嬶細灏忚开')).toBeTruthy();
    });
  });

  it('clones a history task into new task mode without carrying old result view', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-401',
        title: '鏈堟姤鏁寸悊',
        owner: '灏忚开',
        status: '宸插畬鎴?,
        updated_at: '04-07 14:30',
        description: '鏁寸悊鏈湀缁忚惀鏁版嵁鍜岄噸鐐逛簨椤?,
        draft: '鏍规嵁缁忚惀鐪嬫澘鏁寸悊鏈堟姤鍒濈',
        expected_output: 'document',
        result: '杩欐槸鏃ф湀鎶ョ粨鏋?,
        model: 'model-d',
      },
    ] as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: /鍘嗗彶浠诲姟/ }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '澶嶅埗涓烘柊浠诲姟' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '澶嶅埗涓烘柊浠诲姟' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('鏈堟姤鏁寸悊')).toBeTruthy();
      expect(screen.getByDisplayValue('鏍规嵁缁忚惀鐪嬫澘鏁寸悊鏈堟姤鍒濈')).toBeTruthy();
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('document');
      expect(screen.queryByText('澶勭悊缁撴灉')).toBeNull();
      expect(screen.queryByText('杩欐槸鏃ф湀鎶ョ粨鏋?)).toBeNull();
      expect(screen.queryByText('宸查€夊悓浜嬶細灏忚开')).toBeNull();
      expect(screen.getByText('鏆傛湭鎸囧畾锛屾寜浠诲姟鑷姩鍖归厤')).toBeTruthy();
    });
  });

  it('clears current task state from new task page', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);
    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '璇锋暣鐞嗘湰鍛ㄥ紓甯告棩鎶? },
    });
    fireEvent.click(screen.getByRole('button', { name: '閫夋嫨鍚屼簨' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '鎵惧悓浜? })).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '鎵?TA 甯繖' })[0]);

    await waitFor(() => {
      expect(screen.getByDisplayValue('鍐欓€氱煡')).toBeTruthy();
      expect(screen.getByText('宸查€夊悓浜嬶細灏忚开')).toBeTruthy();
    });

    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '璇锋暣鐞嗘湰鍛ㄥ紓甯告棩鎶? },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['寮傚父宸叉仮澶嶃€?], '琛ュ厖璇存槑.txt', { type: 'text/plain' });
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('寮傚父宸叉仮澶嶃€?),
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('琛ュ厖璇存槑.txt')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '娓呯┖褰撳墠浠诲姟' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue('鑷敱杈撳叆')).toBeTruthy();
      expect((screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?) as HTMLTextAreaElement).value).toBe('');
      expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('summary');
      expect(screen.getByText('鏆傛湭鎸囧畾锛屾寜浠诲姟鑷姩鍖归厤')).toBeTruthy();
      expect(screen.queryByText('琛ュ厖璇存槑.txt')).toBeNull();
      expect(screen.queryByText('宸查€夊悓浜嬶細灏忚开')).toBeNull();
    });
  });

  it('applies suggestion text into draft and appends on repeated clicks', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);

    fireEvent.click(screen.getByRole('button', { name: '鏃ユ姤鏁寸悊' }));

    const draftInput = screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?) as HTMLTextAreaElement;

    await waitFor(() => {
      expect(draftInput.value).toBe('璇锋牴鎹粖澶╃殑宸ヤ綔杩涘睍鏁寸悊涓€浠芥棩鎶ワ紝绐佸嚭宸插畬鎴愪簨椤广€佸綋鍓嶉闄╁拰鏄庢棩璁″垝銆?);
    });

    fireEvent.click(screen.getByRole('button', { name: '寮傚父璇存槑' }));

    await waitFor(() => {
      expect(draftInput.value).toBe('璇锋牴鎹粖澶╃殑宸ヤ綔杩涘睍鏁寸悊涓€浠芥棩鎶ワ紝绐佸嚭宸插畬鎴愪簨椤广€佸綋鍓嶉闄╁拰鏄庢棩璁″垝銆俓n\n璇疯鏄庢湰娆″紓甯哥殑鐜拌薄銆佸奖鍝嶈寖鍥淬€佸垵姝ュ師鍥犮€佸綋鍓嶅鐞嗚繘搴﹀拰鍚庣画寤鸿銆?);
    });
  });

  it('deletes a history task and persists the remaining list', async () => {
    vi.mocked(LoadTaskHistory).mockResolvedValue([
      {
        id: 'task-501',
        title: '寰呭垹鏃ユ姤',
        owner: '灏忚开',
        status: '宸插畬鎴?,
        updated_at: '04-07 16:00',
        description: '鍒犻櫎鍓嶇殑绗竴鏉′换鍔?,
        draft: '鍒犻櫎鍓嶇殑绗竴鏉′换鍔¤崏绋?,
        expected_output: 'summary',
        result: '绗竴鏉＄粨鏋?,
        model: 'model-e',
      },
      {
        id: 'task-502',
        title: '淇濈暀绾',
        owner: '灏忕編',
        status: '宸插畬鎴?,
        updated_at: '04-07 16:10',
        description: '鍒犻櫎鍚庡簲璇ヤ繚鐣?,
        draft: '淇濈暀浠诲姟鑽夌',
        expected_output: 'document',
        result: '绗簩鏉＄粨鏋?,
        model: 'model-f',
      },
    ] as never);

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: /鍘嗗彶浠诲姟/ }));

    await waitFor(() => {
      expect(screen.getByText('寰呭垹鏃ユ姤')).toBeTruthy();
      expect(screen.getByText('淇濈暀绾')).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '鍒犻櫎浠诲姟' })[0]);

    await waitFor(() => {
      expect(screen.queryByText('寰呭垹鏃ユ姤')).toBeNull();
      expect(screen.getByText('淇濈暀绾')).toBeTruthy();
    });

    expect(SaveTaskHistory).toHaveBeenCalledTimes(1);
    const saved = vi.mocked(SaveTaskHistory).mock.calls[0]?.[0] as unknown as Array<{
      id: string;
      title: string;
    }>;
    expect(saved).toHaveLength(1);
    expect(saved[0].id).toBe('task-502');
    expect(saved[0].title).toBe('淇濈暀绾');
  });

  it('adds attachments and includes material summary in submit payload', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);
    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '璇锋暣鐞嗘湰鍛ㄤ骇绾垮紓甯? },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['浜х嚎寮傚父闆嗕腑鍦ㄤ簩鍙风嚎锛岄渶浼樺厛璇存槑鍘熷洜鍜屽奖鍝嶃€?], '寮傚父璁板綍.txt', { type: 'text/plain' });
    const expectedSizeLabel = `${file.size} B`;
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('浜х嚎寮傚父闆嗕腑鍦ㄤ簩鍙风嚎锛岄渶浼樺厛璇存槑鍘熷洜鍜屽奖鍝嶃€?),
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('宸叉坊鍔犳潗鏂?)).toBeTruthy();
      expect(screen.getByText('寮傚父璁板綍.txt')).toBeTruthy();
      expect(screen.getByText(`text/plain 路 ${expectedSizeLabel}`)).toBeTruthy();
      expect(screen.getByText('浜х嚎寮傚父闆嗕腑鍦ㄤ簩鍙风嚎锛岄渶浼樺厛璇存槑鍘熷洜鍜屽奖鍝嶃€?)).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬪鐞? })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '鑷敱杈撳叆',
      selected_colleague_name: '',
      draft: `璇锋暣鐞嗘湰鍛ㄤ骇绾垮紓甯竆n\n琛ュ厖鏉愭枡锛歕n1. 寮傚父璁板綍.txt锛坱ext/plain锛?{expectedSizeLabel}锛夛細浜х嚎寮傚父闆嗕腑鍦ㄤ簩鍙风嚎锛岄渶浼樺厛璇存槑鍘熷洜鍜屽奖鍝嶃€俓n浜х嚎寮傚父闆嗕腑鍦ㄤ簩鍙风嚎锛岄渶浼樺厛璇存槑鍘熷洜鍜屽奖鍝嶃€俙,
      expected_output: 'summary',
    });
  });

  it('shows non-text attachment metadata without reading body content', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);
    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '璇锋暣鐞嗚澶囩幇鍦虹収鐗囪鏄? },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['binary-image-content'], '鐜板満鐓х墖.png', { type: 'image/png' });
    const expectedSizeLabel = `${file.size} B`;
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('鐜板満鐓х墖.png')).toBeTruthy();
      expect(screen.getByText(`image/png 路 ${expectedSizeLabel}`)).toBeTruthy();
      expect(screen.getByText('闈炴枃鏈潗鏂欏凡涓婁紶锛屽彲缁撳悎鏂囦欢绫诲瀷鍜屾枃浠跺悕涓€璧峰鐞嗐€?)).toBeTruthy();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬪鐞? })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '鑷敱杈撳叆',
      selected_colleague_name: '',
      draft: `璇锋暣鐞嗚澶囩幇鍦虹収鐗囪鏄嶾n\n琛ュ厖鏉愭枡锛歕n1. 鐜板満鐓х墖.png锛坕mage/png锛?{expectedSizeLabel}锛夛細闈炴枃鏈潗鏂欏凡涓婁紶锛屽彲缁撳悎鏂囦欢绫诲瀷鍜屾枃浠跺悕涓€璧峰鐞嗐€俙,
      expected_output: 'summary',
    });
  });

  it('handles multiple attachments and only submits retained files after removal', async () => {
    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);
    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '璇锋眹鎬讳粖鏃ュ紓甯镐笌鐜板満鎯呭喌' },
    });

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    const textFile = new File(['涓€鍙风嚎鍦ㄤ笅鍗堝嚭鐜扮煭鏆傚仠鏈恒€?], '寮傚父鎽樿.txt', { type: 'text/plain' });
    const imageFile = new File(['image-binary'], '鐜板満鐓х墖.png', { type: 'image/png' });
    const textSizeLabel = `${textFile.size} B`;
    Object.defineProperty(textFile, 'text', {
      value: () => Promise.resolve('涓€鍙风嚎鍦ㄤ笅鍗堝嚭鐜扮煭鏆傚仠鏈恒€?),
    });

    fireEvent.change(fileInput, { target: { files: [textFile, imageFile] } });

    await waitFor(() => {
      expect(screen.getByText('鍏?2 浠芥潗鏂?)).toBeTruthy();
      expect(screen.getByText('寮傚父鎽樿.txt')).toBeTruthy();
      expect(screen.getByText('鐜板満鐓х墖.png')).toBeTruthy();
    });

    const removeButtons = screen.getAllByRole('button', { name: '绉婚櫎' });
    fireEvent.click(removeButtons[1]);

    await waitFor(() => {
      expect(screen.getByText('鍏?1 浠芥潗鏂?)).toBeTruthy();
      expect(screen.getByText('寮傚父鎽樿.txt')).toBeTruthy();
      expect(screen.queryByText('鐜板満鐓х墖.png')).toBeNull();
    });

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬪鐞? })[0]);

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '鑷敱杈撳叆',
      selected_colleague_name: '',
      draft: `璇锋眹鎬讳粖鏃ュ紓甯镐笌鐜板満鎯呭喌\n\n琛ュ厖鏉愭枡锛歕n1. 寮傚父鎽樿.txt锛坱ext/plain锛?{textSizeLabel}锛夛細涓€鍙风嚎鍦ㄤ笅鍗堝嚭鐜扮煭鏆傚仠鏈恒€俓n涓€鍙风嚎鍦ㄤ笅鍗堝嚭鐜扮煭鏆傚仠鏈恒€俙,
      expected_output: 'summary',
    });
  });

  it('appends submitted result to history and persists mapped payload', async () => {
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: '鑷敱杈撳叆',
      colleague_name: '鑷姩鍖归厤鍚屼簨',
      expected_output: 'summary',
      model: 'model-b',
      content: '鏂扮殑澶勭悊缁撴灉',
    } as never);

    render(<App />);

    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬫柊浠诲姟' })[0]);
    fireEvent.change(screen.getByPlaceholderText('璇峰憡璇夋垜浣犳兂瀹屾垚浠€涔堝伐浣?), {
      target: { value: '鏁寸悊浠婂ぉ鐨勬棩鎶ュ唴瀹? },
    });
    fireEvent.click(screen.getAllByRole('button', { name: '寮€濮嬪鐞? })[0]);

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '鍘嗗彶浠诲姟' })).toBeTruthy();
      expect(screen.getAllByText('鑷敱杈撳叆').length).toBeGreaterThan(0);
      expect(screen.getByText('鏁寸悊浠婂ぉ鐨勬棩鎶ュ唴瀹?)).toBeTruthy();
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '鑷敱杈撳叆',
      selected_colleague_name: '',
      draft: '鏁寸悊浠婂ぉ鐨勬棩鎶ュ唴瀹?,
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
    expect(saved[0].title).toBe('鑷敱杈撳叆');
    expect(saved[0].owner).toBe('鑷姩鍖归厤鍚屼簨');
    expect(saved[0].description).toBe('鏁寸悊浠婂ぉ鐨勬棩鎶ュ唴瀹?);
    expect(saved[0].expected_output).toBe('summary');
    expect(saved[0].result).toBe('鏂扮殑澶勭悊缁撴灉');
    expect(saved[0].model).toBe('model-b');
    expect(saved[0].updated_at).toMatch(/^\d{2}-\d{2} \d{2}:\d{2}$/);
  });
});
