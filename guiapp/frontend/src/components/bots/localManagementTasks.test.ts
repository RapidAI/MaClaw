import { describe, expect, it } from 'vitest';
import { localManagementTasksFrom } from './localManagementTasks';

describe('localManagementTasksFrom', () => {
    it('keeps a running install and drops a finished desktop', () => {
        const tasks = localManagementTasksFrom([
            { id: 'dn_1', title: '部署 desktopd · znsoft@192.168.1.49', status: 'deploying', started_at: '2026-11-15T06:39:00Z' },
            { id: 'dn_reg', title: '部署 desktopd · znsoft@192.168.1.49', status: 'registering', started_at: '2026-11-15T06:39:00Z' },
            { id: 'dn_2', title: '更新 desktopd · a@10.0.0.8', status: 'ready', started_at: '2026-11-15T06:00:00Z' },
            { id: '', title: '部署 desktopd', status: 'deploying' },
        ]);
        expect(tasks).toEqual([
            expect.objectContaining({
                id: 'dn_1',
                text: '部署 desktopd · znsoft@192.168.1.49',
                local: true,
                localKind: 'deploying',
                status: 'running',
            }),
            expect.objectContaining({ id: 'dn_reg', localKind: 'registering' }),
        ]);
    });
});