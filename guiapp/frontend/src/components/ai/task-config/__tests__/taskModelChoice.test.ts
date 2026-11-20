import { describe, expect, it } from 'vitest';
import { defaultTaskDraft, withMachine, withTaskModel } from '../taskDraft';
import {
    assignmentForTaskType,
    newTaskModelChipValue,
    taskModelCatalogFromPanel,
    listedModelsWithCurrent,
    taskModelPairEqual,
} from '../taskModelChoice';

describe('taskModelChoice', () => {
    it('treats official capability bands as the same model', () => {
        expect(taskModelPairEqual('hub', 'mid', 'hub', 'official-mid')).toBe(true);
        expect(taskModelPairEqual('hub', 'mid', 'other', 'official-mid')).toBe(false);
        expect(taskModelPairEqual('local', 'coder', 'local', 'other')).toBe(false);
        expect(listedModelsWithCurrent(['auto', 'low', 'mid', 'high'], 'official-mid')).toEqual(['auto', 'low', 'mid', 'high']);
        expect(listedModelsWithCurrent(['auto', 'low', 'mid', 'high'], 'custom-x')[0]).toBe('custom-x');
    });

    it('reads the panel snapshot and follows the row for the task type', () => {
        const catalog = taskModelCatalogFromPanel({
            providers: [{ id: 'devin', name: 'Devin', model: 'swe-2-max', is_hub_service: true }],
            assistant: { provider_id: 'devin', provider_name: 'Devin', model: 'swe-2-max' },
            coding: { provider_id: 'local', provider_name: 'Local', model: 'coder' },
        });
        expect(assignmentForTaskType(catalog, 'chat')?.model).toBe('swe-2-max');
        expect(assignmentForTaskType(catalog, 'coding')?.providerName).toBe('Local');
        expect(catalog.providers[0]?.isHubService).toBe(true);
    });

    it('labels a follow, a pin, and a remote lock', () => {
        const providers = [{ id: 'devin', name: 'Devin' }];
        const assignment = { providerId: 'devin', providerName: 'Devin', model: 'swe-2-max' };
        expect(newTaskModelChipValue(defaultTaskDraft(), providers, assignment)).toEqual({
            text: 'Devin · swe-2-max',
            pinned: false,
            remote: false,
        });
        expect(newTaskModelChipValue(withTaskModel(defaultTaskDraft(), 'devin', 'other'), providers, assignment).pinned).toBe(true);
        expect(newTaskModelChipValue(withMachine(defaultTaskDraft(), { grantId: 'g', machineName: 'office' }), providers, assignment).remote).toBe(true);
    });
});
