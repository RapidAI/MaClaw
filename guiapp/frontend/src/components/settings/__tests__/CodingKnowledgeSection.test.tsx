// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CodingKnowledgeSection } from '../CodingKnowledgeSection';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CodingKnowledgeCapacity: vi.fn(async () => ({ max_total: 1000 })),
    CodingKnowledgeConfirm: vi.fn(),
    CodingKnowledgeContributeToOrg: vi.fn(),
    CodingKnowledgeCreateRevisionCandidate: vi.fn(),
    CodingKnowledgeDelete: vi.fn(),
    CodingKnowledgeEvict: vi.fn(),
    CodingKnowledgeExportToFile: vi.fn(),
    CodingKnowledgeGet: vi.fn(),
    CodingKnowledgeGraduateToSteering: vi.fn(),
    CodingKnowledgeImportFromFile: vi.fn(),
    CodingKnowledgeLifecycle: vi.fn(),
    CodingKnowledgeList: vi.fn(async () => []),
    CodingKnowledgeMarkConflict: vi.fn(),
    CodingKnowledgeResetFile: vi.fn(),
    CodingKnowledgeSearch: vi.fn(async () => []),
    CodingKnowledgeStats: vi.fn(async () => ({ total_count: 0 })),
    CodingKnowledgeUpdate: vi.fn(),
    DigitalAssetListContributableLibraries: vi.fn(async () => []),
    DigitalAssetListMySubmissions: vi.fn(async () => []),
    SelectCodingKnowledgeExportPath: vi.fn(),
    SelectCodingKnowledgeImportFile: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: vi.fn(), showPrompt: vi.fn() }),
}));

afterEach(() => {
    cleanup();
});

describe('CodingKnowledgeSection action buttons', () => {
    it('renders knowledge actions as a horizontal button row including Clear', async () => {
        render(
            <CodingKnowledgeSection
                config={null}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );

        const clear = await screen.findByRole('button', { name: '清空所有知识' });
        await waitFor(() => expect(screen.getByText('暂无经验记录')).toBeTruthy());
        expect(clear.textContent).toBe('清空');
        expect(clear.className).toMatch(/prog-tools__kb-btn/);
        expect(clear.className).toMatch(/prog-tools__btn-reset--danger/);
        expect(clear.parentElement?.className).toBe('prog-tools__kb-actions');

        const actionRow = clear.parentElement as HTMLElement;
        const labels = Array.from(actionRow.querySelectorAll('button')).map((btn) => btn.textContent);
        expect(labels).toEqual(['导出', '导入', '投稿到组织', '执行淘汰', '清空']);
    });
});
