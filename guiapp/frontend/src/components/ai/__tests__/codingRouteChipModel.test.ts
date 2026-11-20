import { describe, expect, it } from 'vitest';
import { codingRouteChipModel } from '../codingRouteChipModel';

describe('codingRouteChipModel', () => {
    it('shows the task model on the primary chip before status refreshes', () => {
        expect(codingRouteChipModel('primary', { model: 'assigned', source: 'primary' }, 'pinned-coder')).toBe('pinned-coder');
        expect(codingRouteChipModel('reasoning', { model: 'assigned', source: 'primary' }, 'pinned-coder')).toBe('pinned-coder');
        expect(codingRouteChipModel('reasoning', { model: 'route-model', source: 'route' }, 'pinned-coder')).toBe('route-model');
        expect(codingRouteChipModel('primary', { model: 'assigned', source: 'primary' }, '')).toBe('assigned');
    });
});
