/**
 * Closing the preview pane must drop an in-flight cloud download while the
 * directory tree is still mounted for the slide-out.
 */
import { describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
    EventsOff: vi.fn(),
}));

import { cloudAgentPreviewOpenGeneration } from '../cloudAgentVisualPreview';
import { useCodePreviewState } from '../useCodePreviewState';

describe('useCodePreviewState closePanel', () => {
    it('invalidates an in-flight cloud preview when the open pane closes', () => {
        const path = 'C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_live';
        const before = cloudAgentPreviewOpenGeneration(path);
        const { result } = renderHook(() => useCodePreviewState(path, false, { previewWorkspacePath: path }));
        act(() => { result.current.reopenPanel(); });
        act(() => { result.current.closePanel(); });
        expect(result.current.state.active).toBe(false);
        expect(cloudAgentPreviewOpenGeneration(path)).toBe(before + 1);
        act(() => { result.current.closePanel(); });
        expect(cloudAgentPreviewOpenGeneration(path)).toBe(before + 1);
    });
});
