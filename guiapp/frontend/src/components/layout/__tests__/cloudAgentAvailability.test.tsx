import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render } from '@testing-library/react';
import {
    currentCloudAgentsEnabled,
    noteCloudAgentsEnabled,
    resetCloudAgentsEnabledForTests,
    subscribeCloudAgentsEnabled,
    useCloudAgentsEnabled,
} from '../cloudAgentAvailability';
import { requestCloudAgentListRefresh } from '../cloudAgentAssistant';

type Binding = { ListCloudAgents?: (...args: unknown[]) => Promise<unknown> };

const setBinding = (binding: Binding | undefined) => {
    (window as unknown as { go?: unknown }).go = binding ? { main: { App: binding } } : undefined;
};

function Probe({ onValue }: { onValue: (enabled: boolean) => void }) {
    onValue(useCloudAgentsEnabled());
    return null;
}

describe('cloudAgentAvailability', () => {
    beforeEach(() => {
        resetCloudAgentsEnabledForTests();
    });

    afterEach(() => {
        resetCloudAgentsEnabledForTests();
        setBinding(undefined);
        vi.restoreAllMocks();
    });

    it('starts closed until Hub answers', () => {
        setBinding(undefined);
        expect(currentCloudAgentsEnabled()).toBe(false);
    });

    it('notes the Hub switch once and stays silent on repeats', () => {
        const seen: boolean[] = [];
        const unsubscribe = subscribeCloudAgentsEnabled((enabled) => seen.push(enabled));
        noteCloudAgentsEnabled(true);
        noteCloudAgentsEnabled(true);
        noteCloudAgentsEnabled(false);
        unsubscribe();
        expect(seen).toEqual([true, false]);
        expect(currentCloudAgentsEnabled()).toBe(false);
    });

    it('polls the binding on subscribe and keeps the last answer when a poll rejects', async () => {
        let answer: Promise<unknown> = Promise.resolve({ instances: [], enabled: true });
        const list = vi.fn(() => answer);
        setBinding({ ListCloudAgents: list as unknown as Binding['ListCloudAgents'] });
        const seen: boolean[] = [];
        const unsubscribe = subscribeCloudAgentsEnabled((enabled) => seen.push(enabled));
        await act(async () => {
            await Promise.resolve();
        });
        expect(list).toHaveBeenCalled();
        expect(currentCloudAgentsEnabled()).toBe(true);

        answer = Promise.reject(new Error('signed out'));
        await act(async () => {
            requestCloudAgentListRefresh();
            await Promise.resolve();
            await Promise.resolve();
            await Promise.resolve();
        });
        // A rejected call is not Hub's answer: the last known value stays.
        expect(currentCloudAgentsEnabled()).toBe(true);

        answer = Promise.resolve({ instances: [], enabled: false });
        await act(async () => {
            requestCloudAgentListRefresh();
            await Promise.resolve();
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(currentCloudAgentsEnabled()).toBe(false);
        unsubscribe();
        expect(seen).toEqual([true, false]);
    });

    it('ignores answers from a fetch that started before an unsubscribe', async () => {
        let resolveDeferred: (value: unknown) => void = () => {};
        const deferred = new Promise<unknown>((resolve) => { resolveDeferred = resolve; });
        const list = vi.fn(() => deferred);
        setBinding({ ListCloudAgents: list as unknown as Binding['ListCloudAgents'] });
        const unsubscribe = subscribeCloudAgentsEnabled(() => {});
        unsubscribe();
        // The late answer from the in-flight poll must not flip the closed store.
        resolveDeferred({ instances: [], enabled: true });
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(currentCloudAgentsEnabled()).toBe(false);
    });

    it('drives React state through useCloudAgentsEnabled', async () => {
        setBinding({ ListCloudAgents: async () => ({ instances: [], enabled: true }) });
        const seen: boolean[] = [];
        const view = render(<Probe onValue={(enabled) => seen.push(enabled)} />);
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(seen[0]).toBe(false);
        expect(seen).toContain(true);
        view.unmount();
    });
});
