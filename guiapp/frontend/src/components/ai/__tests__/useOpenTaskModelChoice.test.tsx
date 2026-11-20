// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { FetchMaclawLLMProfileModels, GetTaskLLMChoice, SetTaskLLMChoice } from '../../../../wailsjs/go/main/App';
import { useOpenTaskModelChoice } from '../useOpenTaskModelChoice';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetTaskLLMChoice: vi.fn(),
    SetTaskLLMChoice: vi.fn(),
    FetchMaclawLLMProfileModels: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => () => {}),
}));

const providers = [
    { id: 'local', name: 'Local', url: '', model: 'assistant-model', models: ['assistant-model'], configured: true, isHubService: false },
    { id: 'devin', name: 'Devin', url: '', model: 'swe-2-max', isHubService: true, configured: true },
];

describe('useOpenTaskModelChoice', () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
    });

    it('stays inactive without a task path', () => {
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: '',
            providers,
            lang: 'zh',
        }));
        expect(result.current).toBeNull();
        expect(GetTaskLLMChoice).not.toHaveBeenCalled();
    });

    it('follows the assignment and pins only this task', async () => {
        vi.mocked(GetTaskLLMChoice).mockResolvedValue({
            providerId: 'devin',
            providerName: 'Devin',
            model: 'swe-2-max',
            followsAssignment: true,
        });
        vi.mocked(SetTaskLLMChoice).mockResolvedValue(undefined);
        const showToast = vi.fn();
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
            showToast,
        }));
        expect(result.current?.onSwitchProvider).toEqual(expect.any(Function));
        await waitFor(() => {
            expect(result.current?.label).toContain('Devin');
            expect(result.current?.label).toContain('swe-2-max');
            expect(result.current?.label).not.toContain('本任务');
            expect(result.current?.providers[0]?.id).toBe('devin');
            expect(result.current?.followsAssignment).toBe(true);
        });

        await act(async () => {
            result.current?.onSwitchModel('other-model');
        });
        expect(SetTaskLLMChoice).toHaveBeenCalledWith('D:/tasks/one', 'devin', 'other-model');
        expect(showToast).toHaveBeenCalled();

        await act(async () => {
            result.current?.onFollow();
        });
        expect(SetTaskLLMChoice).toHaveBeenCalledWith('D:/tasks/one', '', '');
    });

    it('can pin before the first read returns', async () => {
        let resolveGet: (value: any) => void = () => {};
        vi.mocked(GetTaskLLMChoice).mockReturnValue(new Promise((resolve) => { resolveGet = resolve; }));
        vi.mocked(SetTaskLLMChoice).mockResolvedValue(undefined);
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
        }));
        await act(async () => {
            result.current?.onSwitchProvider('local');
        });
        expect(SetTaskLLMChoice).toHaveBeenCalledWith('D:/tasks/one', 'local', 'assistant-model');
        await act(async () => {
            resolveGet({ providerId: 'local', providerName: 'Local', model: 'assistant-model', followsAssignment: false });
        });
        expect(FetchMaclawLLMProfileModels).not.toHaveBeenCalled();
    });

    it('writes a later choice only after the earlier save finishes', async () => {
        vi.mocked(GetTaskLLMChoice).mockResolvedValue({
            providerId: 'devin',
            providerName: 'Devin',
            model: 'swe-2-max',
            followsAssignment: false,
        });
        let releaseFirst: () => void = () => {};
        let calls = 0;
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => {
            calls += 1;
            if (calls === 1) return new Promise((resolve) => { releaseFirst = () => resolve(undefined); });
            return Promise.resolve();
        });
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(false));
        await act(async () => {
            result.current?.onSwitchModel('first-model');
        });
        expect(SetTaskLLMChoice).toHaveBeenCalledTimes(1);
        await act(async () => {
            result.current?.onSwitchModel('second-model');
        });
        expect(SetTaskLLMChoice).toHaveBeenCalledTimes(1);
        await act(async () => { releaseFirst(); });
        await waitFor(() => expect(SetTaskLLMChoice).toHaveBeenCalledTimes(2));
        expect(SetTaskLLMChoice).toHaveBeenLastCalledWith('D:/tasks/one', 'devin', 'second-model');
    });

    it('stores a queued choice on the task it was made for', async () => {
        vi.mocked(GetTaskLLMChoice).mockResolvedValue({
            providerId: 'devin',
            providerName: 'Devin',
            model: 'swe-2-max',
            followsAssignment: true,
        });
        let releaseFirst: () => void = () => {};
        let calls = 0;
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => {
            calls += 1;
            if (calls === 1) return new Promise((resolve) => { releaseFirst = () => resolve(undefined); });
            return Promise.resolve();
        });
        const showToast = vi.fn();
        const { result, rerender } = renderHook(
            ({ path }: { path: string }) => useOpenTaskModelChoice({
                projectPath: path,
                providers,
                lang: 'zh',
                showToast,
            }),
            { initialProps: { path: 'D:/tasks/one' } },
        );
        await waitFor(() => expect(result.current?.followsAssignment).toBe(true));
        await act(async () => { result.current?.onSwitchModel('first-model'); });
        await act(async () => { result.current?.onSwitchModel('second-model'); });
        rerender({ path: 'D:/tasks/two' });
        await act(async () => { releaseFirst(); });
        await waitFor(() => expect(SetTaskLLMChoice).toHaveBeenCalledTimes(2));
        expect(SetTaskLLMChoice).toHaveBeenNthCalledWith(2, 'D:/tasks/one', 'devin', 'second-model');
        expect(showToast).not.toHaveBeenCalled();
    });

    it('keeps a newer choice when the earlier save fails', async () => {
        vi.mocked(GetTaskLLMChoice).mockResolvedValue({
            providerId: 'devin',
            providerName: 'Devin',
            model: 'swe-2-max',
            followsAssignment: false,
        });
        let rejectFirst: (err: Error) => void = () => {};
        let calls = 0;
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => {
            calls += 1;
            if (calls === 1) return new Promise((_, reject) => { rejectFirst = reject; });
            return Promise.resolve();
        });
        const showToast = vi.fn();
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
            showToast,
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(false));
        await act(async () => { result.current?.onSwitchModel('first-model'); });
        await act(async () => { result.current?.onSwitchModel('second-model'); });
        await act(async () => { rejectFirst(new Error('busy')); });
        await waitFor(() => expect(SetTaskLLMChoice).toHaveBeenCalledTimes(2));
        expect(SetTaskLLMChoice).toHaveBeenLastCalledWith('D:/tasks/one', 'devin', 'second-model');
        await waitFor(() => expect(showToast).toHaveBeenCalled());
        expect(showToast.mock.calls.some((call) => String(call[0]).includes('没能记住'))).toBe(false);
    });

    it('does not announce a pin when the read-back is missing', async () => {
        let gets = 0;
        vi.mocked(GetTaskLLMChoice).mockImplementation(() => {
            gets += 1;
            if (gets === 1) {
                return Promise.resolve({
                    providerId: 'devin',
                    providerName: 'Devin',
                    model: 'swe-2-max',
                    followsAssignment: true,
                });
            }
            return Promise.reject(new Error('read failed'));
        });
        vi.mocked(SetTaskLLMChoice).mockResolvedValue(undefined);
        const showToast = vi.fn();
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
            showToast,
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(true));
        await act(async () => { result.current?.onSwitchModel('other-model'); });
        await waitFor(() => expect(SetTaskLLMChoice).toHaveBeenCalledWith('D:/tasks/one', 'devin', 'other-model'));
        expect(showToast).not.toHaveBeenCalled();
        expect(result.current?.currentModel).toBe('other-model');
    });

    it('keeps a newer choice while the previous read is still in flight', async () => {
        let gets = 0;
        let releaseGet: (value: any) => void = () => {};
        vi.mocked(GetTaskLLMChoice).mockImplementation(() => {
            gets += 1;
            if (gets === 1) {
                return Promise.resolve({
                    providerId: 'devin',
                    providerName: 'Devin',
                    model: 'swe-2-max',
                    followsAssignment: true,
                });
            }
            if (gets === 2) return new Promise((resolve) => { releaseGet = resolve; });
            return Promise.resolve({
                providerId: 'devin',
                providerName: 'Devin',
                model: 'second-model',
                followsAssignment: false,
            });
        });
        let releaseSet: () => void = () => {};
        let sets = 0;
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => {
            sets += 1;
            if (sets === 1) return new Promise((resolve) => { releaseSet = () => resolve(undefined); });
            return Promise.resolve();
        });
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(true));
        await act(async () => { result.current?.onSwitchModel('first-model'); });
        await act(async () => { releaseSet(); });
        await waitFor(() => expect(result.current?.currentModel).toBe('first-model'));
        await act(async () => { result.current?.onSwitchModel('second-model'); });
        expect(result.current?.currentModel).toBe('second-model');
        await act(async () => {
            releaseGet({
                providerId: 'devin',
                providerName: 'Devin',
                model: 'first-model',
                followsAssignment: false,
            });
        });
        expect(result.current?.currentModel).toBe('second-model');
        await waitFor(() => expect(SetTaskLLMChoice).toHaveBeenLastCalledWith('D:/tasks/one', 'devin', 'second-model'));
    });

    it('lets the next send continue once the pin is stored', async () => {
        let reads = 0;
        let releaseRead: (value: any) => void = () => {};
        vi.mocked(GetTaskLLMChoice).mockImplementation(() => {
            reads += 1;
            if (reads === 1) {
                return Promise.resolve({
                    providerId: 'devin',
                    providerName: 'Devin',
                    model: 'swe-2-max',
                    followsAssignment: true,
                });
            }
            return new Promise((resolve) => { releaseRead = resolve; });
        });
        let releaseSet: () => void = () => {};
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => new Promise((resolve) => {
            releaseSet = () => resolve(undefined);
        }));
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(true));
        expect(result.current?.whenSaved()).toBeUndefined();
        await act(async () => { result.current?.onSwitchModel('pinned-coder'); });
        const gate = result.current?.whenSaved();
        expect(gate).toBeInstanceOf(Promise);
        let opened = false;
        const finished = gate?.then(() => { opened = true; });
        await Promise.resolve();
        expect(opened).toBe(false);
        await act(async () => { releaseSet(); });
        await finished;
        expect(opened).toBe(true);
        await act(async () => {
            releaseRead({
                providerId: 'devin',
                providerName: 'Devin',
                model: 'pinned-coder',
                followsAssignment: false,
            });
        });
    });

    it('holds the next send until a replacement pin is stored', async () => {
        vi.mocked(GetTaskLLMChoice).mockResolvedValue({
            providerId: 'devin',
            providerName: 'Devin',
            model: 'swe-2-max',
            followsAssignment: true,
        });
        let releaseFirst: () => void = () => {};
        let sets = 0;
        vi.mocked(SetTaskLLMChoice).mockImplementation(() => {
            sets += 1;
            if (sets === 1) return new Promise((resolve) => { releaseFirst = () => resolve(undefined); });
            return Promise.resolve();
        });
        const { result } = renderHook(() => useOpenTaskModelChoice({
            projectPath: 'D:/tasks/one',
            providers,
            lang: 'zh',
        }));
        await waitFor(() => expect(result.current?.followsAssignment).toBe(true));
        await act(async () => { result.current?.onSwitchModel('first-model'); });
        const gate = result.current?.whenSaved();
        await act(async () => { result.current?.onSwitchModel('second-model'); });
        let opened = false;
        const finished = gate?.then(() => { opened = true; });
        await Promise.resolve();
        expect(opened).toBe(false);
        expect(SetTaskLLMChoice).toHaveBeenCalledTimes(1);
        await act(async () => { releaseFirst(); });
        await finished;
        expect(opened).toBe(true);
        expect(SetTaskLLMChoice).toHaveBeenLastCalledWith('D:/tasks/one', 'devin', 'second-model');
    });
});
