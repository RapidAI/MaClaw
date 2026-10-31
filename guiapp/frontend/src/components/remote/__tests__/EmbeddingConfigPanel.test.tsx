import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CheckYOLOModel, DownloadYOLOModel, GetScreenParsingEnabled } from '../../../../wailsjs/go/main/App';
import { EventsOn } from '../../../../wailsjs/runtime';
import { EmbeddingConfigPanel } from '../EmbeddingConfigPanel';

const mocks = vi.hoisted(() => ({
    vectorEnabled: vi.fn(),
    setComputerUse: vi.fn(),
    confirm: vi.fn(),
    events: new Map<string, (data: any) => void>(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetVectorSearchEnabled: mocks.vectorEnabled,
    SetVectorSearchEnabled: vi.fn(),
    CheckEmbeddingModel: vi.fn().mockResolvedValue({ exists: true, size: 609500000 }),
    DownloadEmbeddingModel: vi.fn(),
    DeleteEmbeddingModel: vi.fn(),
    GetScreenParsingEnabled: vi.fn().mockResolvedValue(true),
    SetScreenParsingEnabled: vi.fn(),
    CheckYOLOModel: vi.fn().mockResolvedValue({ exists: true, size: 77000000 }),
    DownloadYOLOModel: vi.fn(),
    GetComputerUseEnabled: vi.fn().mockResolvedValue(true),
    SetComputerUseEnabled: mocks.setComputerUse,
    GetComputerUseStatus: vi.fn().mockResolvedValue({ session_active: false }),
    ComputerUseSelfCheck: vi.fn(),
    GetComputerUseLastWarmup: vi.fn().mockResolvedValue(null),
    OpenComputerUsePermissionSettingsDefault: vi.fn(),
    ExportComputerUseDiagnostics: vi.fn(),
    ExportComputerUseObserveHistoryCSV: vi.fn(),
    ComputerUseE2ESmoke: vi.fn(),
    ComputerUseE2EInteract: vi.fn(),
    OpenComputerUseLogsFolder: vi.fn(),
    OpenComputerUseLastDiagnostics: vi.fn(),
    CopyComputerUsePath: vi.fn(),
    ListComputerUseLogArtifacts: vi.fn().mockResolvedValue({ count: 0, items: [] }),
    PruneComputerUseLogArtifacts: vi.fn(),
    GetComputerUseLogPrunePolicy: vi.fn().mockResolvedValue({ keep_newest: 10, max_age_days: 0, auto_prune: false }),
    SetComputerUseLogPrunePolicy: vi.fn(),
    SetComputerUseLogAutoPrune: vi.fn(),
    OpenComputerUseLogArtifact: vi.fn(),
    DeleteComputerUseLogArtifact: vi.fn(),
    BatchDeleteComputerUseLogArtifacts: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((name: string, fn: (data: any) => void) => {
        mocks.events.set(name, fn);
    }),
    EventsOff: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: mocks.confirm }),
}));

describe('EmbeddingConfigPanel', () => {
    beforeEach(() => {
        mocks.vectorEnabled.mockResolvedValue(true);
        mocks.setComputerUse.mockResolvedValue(undefined);
        mocks.confirm.mockResolvedValue(true);
        mocks.events.clear();
        vi.mocked(GetScreenParsingEnabled).mockResolvedValue(true);
    });

    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
    });

    it('renders every feature as its own section card with a switch', async () => {
        render(<EmbeddingConfigPanel lang="zh-Hans" />);

        expect(await screen.findByText('Computer Use（桌面操控）')).toBeTruthy();
        expect(screen.getByText('OminiParser-V2（屏幕解析）')).toBeTruthy();
        expect(screen.getByText('嵌入模型：Qwen3-Embedding-0.6B（向量搜索）')).toBeTruthy();

        for (const name of ['启用 Computer Use', '启用屏幕解析', '启用向量搜索']) {
            const box = screen.getByRole('switch', { name }) as HTMLInputElement;
            expect(box.checked).toBe(true);
        }

        // Ready models surface their status box with the size and management actions.
        expect(screen.getAllByText('模型已就绪').length).toBeGreaterThanOrEqual(1);
        expect(screen.getByRole('button', { name: '重新下载' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '删除' })).toBeTruthy();
    });

    it('starts OmniParser download without a click when the weights are missing', async () => {
        vi.mocked(CheckYOLOModel)
            .mockResolvedValueOnce({ exists: false, size: 0, downloading: false } as never)
            .mockResolvedValueOnce({ exists: false, size: 0, downloading: true } as never);
        render(<EmbeddingConfigPanel lang="zh-Hans" />);

        expect(await screen.findByText('正在下载模型...')).toBeTruthy();
        await waitFor(() => expect(vi.mocked(CheckYOLOModel)).toHaveBeenCalled());
        await waitFor(() => expect(vi.mocked(DownloadYOLOModel)).toHaveBeenCalled());
        expect(screen.queryByRole('button', { name: '下载模型' })).toBeNull();
    });

    it('keeps screen parsing off when the finished download says it stayed off', async () => {
        vi.mocked(GetScreenParsingEnabled).mockResolvedValue(false);
        vi.mocked(CheckYOLOModel)
            .mockResolvedValueOnce({ exists: false, size: 0, downloading: false } as never)
            .mockResolvedValueOnce({ exists: true, size: 77, downloading: false } as never);
        render(<EmbeddingConfigPanel lang="zh-Hans" />);

        const box = await screen.findByRole('switch', { name: '启用屏幕解析' }) as HTMLInputElement;
        await waitFor(() => expect(vi.mocked(DownloadYOLOModel)).toHaveBeenCalled());
        await waitFor(() => expect(box.checked).toBe(false));

        const onProgress = mocks.events.get('yolo-download-progress');
        expect(onProgress).toBeTypeOf('function');
        act(() => { onProgress?.({ percent: 100, downloaded: 77 }); });
        expect(box.checked).toBe(false);
        act(() => { onProgress?.({ percent: 100, screen_parsing_enabled: true }); });
        expect(box.checked).toBe(true);
        act(() => { onProgress?.({ percent: 100, screen_parsing_enabled: false }); });
        expect(box.checked).toBe(false);
        expect(vi.mocked(EventsOn)).toHaveBeenCalled();
    });

    it('follows an OmniParser download that has already started', async () => {
        vi.mocked(CheckYOLOModel).mockResolvedValueOnce({ exists: false, size: 0, downloading: true } as never);
        render(<EmbeddingConfigPanel lang="zh-Hans" />);

        expect(await screen.findByText('正在下载模型...')).toBeTruthy();
        expect(vi.mocked(DownloadYOLOModel)).not.toHaveBeenCalled();
        expect(screen.queryByRole('button', { name: '下载模型' })).toBeNull();
    });

    it('persists turning Computer Use off from its section switch', async () => {
        render(<EmbeddingConfigPanel lang="zh-Hans" />);
        const box = await screen.findByRole('switch', { name: '启用 Computer Use' }) as HTMLInputElement;
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setComputerUse).toHaveBeenCalledWith(false));
    });
});
