// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import {
    TaskPlaceIcon,
    asCloudAgentListItem,
    cloudAgentRecency,
    mixRecentEntries,
    readCloudOpenStamps,
    rememberCloudOpen,
    taskPlaceStamp,
} from '../taskPlace';

const OPEN_STAMP_KEY = 'maclaw.cloud-agent-opened.v1';

afterEach(() => {
    localStorage.removeItem(OPEN_STAMP_KEY);
    cleanup();
});

describe('task place', () => {
    it('drops a missing or zero time and keeps a real stamp', () => {
        expect(taskPlaceStamp(0)).toBe(0);
        expect(taskPlaceStamp('')).toBe(0);
        expect(taskPlaceStamp('0001-01-01T00:00:00Z')).toBe(0);
        expect(taskPlaceStamp('2026-11-23T00:00:00Z')).toBe(Date.parse('2026-11-23T00:00:00Z'));
    });

    it('mixes local and cloud by time and the cap counts both', () => {
        const mixed = mixRecentEntries(
            [{ key: 'local-a', at: 3_000 }, { key: 'local-b', at: 1_000 }],
            [{ id: 'cag_1', at: 2_000 }],
            5,
        );
        expect(mixed.order['local-a']).toBe(0);
        expect(mixed.order['cloud:cag_1']).toBe(1);
        expect(mixed.order['local-b']).toBe(2);

        const capped = mixRecentEntries(
            [{ key: 'local-a', at: 3_000 }],
            [{ id: 'cag_1', at: 2_000 }],
            1,
        );
        expect(capped.order).toEqual({ 'local-a': 0 });
    });

    it('skips a deleted row and reads either json casing', () => {
        expect(asCloudAgentListItem({ id: 'cag_gone', status: 'deleted' })).toBeNull();
        expect(asCloudAgentListItem({ ID: 'cag_x', Name: '演示', Kind: 'coding', UpdatedAt: '2026-11-23T00:00:00Z' })).toEqual({
            id: 'cag_x',
            name: '演示',
            kind: 'coding',
            status: '',
            updatedAt: '2026-11-23T00:00:00Z',
        });
    });

    it('uses a local open stamp when the server time is ancient', () => {
        const item = asCloudAgentListItem({ id: 'cag_old', kind: 'session', updated_at: '0001-01-01T00:00:00Z' });
        expect(item).not.toBeNull();
        expect(cloudAgentRecency(item!, {})).toBe(0);
        const opened = Date.parse('2026-11-23T12:00:00Z');
        rememberCloudOpen('cag_old', opened);
        expect(cloudAgentRecency(item!, readCloudOpenStamps())).toBe(opened);
    });

    it('draws a laptop for a local task and a cloud outline for a cloud task', () => {
        render(<><TaskPlaceIcon place="local" lang="zh" /><TaskPlaceIcon place="cloud" lang="zh" /></>);
        expect(screen.getByTestId('task-place-icon-local').getAttribute('aria-label')).toBe('本地任务');
        expect(screen.getByTestId('task-place-icon-cloud').getAttribute('aria-label')).toBe('云端任务');
        expect(screen.getByTestId('task-place-icon-cloud').innerHTML).not.toContain('M12 13v5');
    });
});
