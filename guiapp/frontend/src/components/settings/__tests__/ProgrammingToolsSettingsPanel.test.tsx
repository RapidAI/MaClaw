// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { corelib } from '../../../../wailsjs/go/models';
import { ProgrammingToolsSettingsPanel } from '../ProgrammingToolsSettingsPanel';

const PatchConfigFieldsMock = vi.fn(async (patch: Record<string, unknown>) => new corelib.AppConfig(patch as never));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetACPHostStatus: vi.fn(async () => ({ ready: false })),
    RestartACPHost: vi.fn(async () => ({ ready: true })),
    PatchConfigFields: (patch: Record<string, unknown>) => PatchConfigFieldsMock(patch),
    CodingKnowledgeStats: vi.fn(async () => ({})),
    CodingKnowledgeCapacity: vi.fn(async () => ({})),
    CodingKnowledgeList: vi.fn(async () => []),
    CodingKnowledgeSearch: vi.fn(async () => []),
    DigitalAssetListContributableLibraries: vi.fn(async () => []),
    DigitalAssetListMySubmissions: vi.fn(async () => []),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe('ProgrammingToolsSettingsPanel quality gate', () => {
    it('shows the quality gate off by default under coding settings', () => {
        render(
            <ProgrammingToolsSettingsPanel
                config={new corelib.AppConfig({})}
                setConfig={vi.fn()}
                lang="zh-Hans"
            />,
        );

        const toggle = screen.getByLabelText('质量门') as HTMLInputElement;
        expect(toggle.checked).toBe(false);
        expect(screen.getByText(/默认关闭/)).toBeTruthy();
    });

    it('persists turning the quality gate on', () => {
        render(
            <ProgrammingToolsSettingsPanel
                config={new corelib.AppConfig({})}
                setConfig={vi.fn()}
                lang="zh-Hans"
            />,
        );

        fireEvent.click(screen.getByLabelText('质量门'));

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ coding_quality_gate_enabled: true });
    });
});
