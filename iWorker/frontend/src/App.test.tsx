import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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

describe('App', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('FileReader', MockFileReader as unknown as typeof FileReader);
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
    vi.mocked(CheckCenterHealth).mockResolvedValue({
      reachable: true,
      status: 'ok',
      provider_count: 3,
      config_path: '/tmp/center.json',
      message: 'ok',
      resolved_base_url: 'http://127.0.0.1:9377',
    } as never);
    vi.mocked(LoadDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(LoadTaskHistory).mockResolvedValue([] as never);
    vi.mocked(SaveDiWorkerSettings).mockResolvedValue(undefined as never);
    vi.mocked(SaveTaskHistory).mockResolvedValue(undefined as never);
    vi.mocked(SubmitTask).mockResolvedValue({
      task_type: '自由输入',
      colleague_name: '自动匹配同事',
      expected_output: 'summary',
      model: 'test-model',
      content: '默认返回内容',
    } as never);
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
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    cleanup();
  });

  it('tests center health from settings page and shows snapshot', async () => {
    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: '测试中心连接' }));

    await waitFor(() => {
      expect(CheckCenterHealth).toHaveBeenCalledTimes(1);
      expect(screen.getByText('连接正常')).toBeTruthy();
      expect(screen.getByText('中心连接正常')).toBeTruthy();
      expect(screen.getByText('手动检测')).toBeTruthy();
      expect(screen.getByText('/tmp/center.json')).toBeTruthy();
    });
  });

  it('shows offline save hint when Wails bridge is unavailable', async () => {
    delete (window as Window & { go?: unknown }).go;

    render(<App />);

    fireEvent.click(screen.getByRole('button', { name: '打开配置界面' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '数字员工中心配置' })).toBeTruthy();
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

  it('adds text attachment and includes summary in submit payload', async () => {
    render(<App />);

    const homeInput = screen.getByPlaceholderText('输入问题...');
    fireEvent.change(homeInput, {
      target: { value: '请整理本周产线异常' },
    });
    fireEvent.keyDown(homeInput, {
      key: 'Enter',
      code: 'Enter',
      charCode: 13,
    });

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '新建任务' })).toBeTruthy();
    });

    const newTaskHeading = screen.getByRole('heading', { name: '新建任务' });
    const taskPanel = newTaskHeading.closest('section');
    expect(taskPanel).toBeTruthy();

    const fileInput = taskPanel?.querySelector('input[type="file"]') as HTMLInputElement | null;
    expect(fileInput).toBeTruthy();

    const file = new File(['产线异常集中在二号线，需要优先说明原因和影响。'], '异常记录.txt', { type: 'text/plain' });
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('产线异常集中在二号线，需要优先说明原因和影响。'),
    });

    fireEvent.change(fileInput as HTMLInputElement, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText('异常记录.txt')).toBeTruthy();
    });

    fireEvent.click(within(taskPanel as HTMLElement).getByRole('button', { name: '开始处理' }));

    await waitFor(() => {
      expect(SubmitTask).toHaveBeenCalledTimes(1);
    });

    expect(SubmitTask).toHaveBeenCalledWith({
      task_type: '自由输入',
      selected_colleague_name: '',
      draft: expect.stringContaining('补充材料：'),
      expected_output: 'summary',
    });
    expect(String(vi.mocked(SubmitTask).mock.calls[0]?.[0]?.draft)).toContain('异常记录.txt');
    expect(String(vi.mocked(SubmitTask).mock.calls[0]?.[0]?.draft)).toContain('产线异常集中在二号线，需要优先说明原因和影响。');
  });
});
