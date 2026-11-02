// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CodingKnowledgeConfirmMany, CodingKnowledgeList, CodingKnowledgeStats } from '../../../../wailsjs/go/main/App';
import { CodingKnowledgeSection } from '../CodingKnowledgeSection';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CodingKnowledgeCapacity: vi.fn(async () => ({ max_total: 1000 })),
    CodingKnowledgeConfirm: vi.fn(),
    CodingKnowledgeConfirmMany: vi.fn(async () => ({ confirmed: [], failed: [] })),
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
    useDialog: () => ({ showConfirm: vi.fn(async () => true), showPrompt: vi.fn() }),
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

    it('shows the balanced save defaults and the auto-promotion threshold', async () => {
        render(
            <CodingKnowledgeSection
                config={null}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );

        const strategy = await screen.findByLabelText('策略');
        expect((strategy as HTMLSelectElement).value).toBe('on_success');

        const threshold = screen.getByLabelText('自动晋升观测次数');
        expect((threshold as HTMLInputElement).value).toBe('2');
    });

    it('shows candidate observation progress toward auto-promotion', async () => {
        vi.mocked(CodingKnowledgeList).mockResolvedValueOnce([
            {
                id: 'k1',
                title: 'Nil HTTP client',
                category: 'pitfall',
                scope: 'universal',
                trigger_condition: 'nil http client',
                content: 'Check the client before reuse.',
                confidence: 1,
                recall_count: 0,
                success_count: 0,
                failure_count: 0,
                observation_count: 1,
                status: 'candidate',
                created_at: null,
                updated_at: null,
            } as any,
        ]);
        render(
            <CodingKnowledgeSection
                config={{ coding_knowledge_auto_save_mode: 'auto', coding_knowledge_auto_promote_observations: 2 } as any}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );
        expect(await screen.findByText('1/2')).toBeTruthy();
    });

    it('shows recall usage and the recall success rate', async () => {
        vi.mocked(CodingKnowledgeStats).mockResolvedValueOnce({
            total_count: 3,
            active_count: 2,
            verified_count: 1,
            candidate_count: 0,
            deprecated_count: 0,
            recall_count: 4,
            recall_success_count: 3,
            recall_failure_count: 1,
            by_project: {},
            by_category: {},
            by_language: {},
            avg_confidence: 1.2,
        });
        render(
            <CodingKnowledgeSection
                config={null}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );
        expect(await screen.findByText('75%')).toBeTruthy();
        expect(screen.getByText('召回')).toBeTruthy();
        expect(screen.getByText('召回成功率')).toBeTruthy();
    });

    it('bulk-confirms selected candidates', async () => {
        const candidate = (id: string, title: string) => ({
            id,
            title,
            category: 'pitfall',
            scope: 'universal',
            trigger_condition: 'nil http client',
            content: 'Check the client before reuse.',
            confidence: 1,
            recall_count: 0,
            success_count: 0,
            failure_count: 0,
            observation_count: 1,
            status: 'candidate',
            created_at: null,
            updated_at: null,
        });
        vi.mocked(CodingKnowledgeList).mockResolvedValueOnce([candidate('k1', 'First candidate'), candidate('k2', 'Second candidate')] as any);
        vi.mocked(CodingKnowledgeConfirmMany).mockResolvedValueOnce({ confirmed: ['k1', 'k2'], failed: [] } as any);

        render(
            <CodingKnowledgeSection
                config={{ coding_knowledge_auto_save_mode: 'observe' } as any}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );

        const checkboxes = await screen.findAllByLabelText('选择审核');
        expect(checkboxes).toHaveLength(2);
        fireEvent.click(checkboxes[0]);
        fireEvent.click(checkboxes[1]);

        fireEvent.click(await screen.findByRole('button', { name: '确认所选（2）' }));
        await waitFor(() => expect(CodingKnowledgeConfirmMany).toHaveBeenCalledWith(['k1', 'k2']));
    });

    it('offers a one-click promotion for candidates that reached the observation threshold', async () => {
        vi.mocked(CodingKnowledgeList).mockResolvedValueOnce([
            {
                id: 'ready-1',
                title: 'Ready candidate',
                category: 'pitfall',
                scope: 'universal',
                trigger_condition: 'ready candidate',
                content: 'Reached the observation threshold.',
                confidence: 1,
                recall_count: 0,
                success_count: 0,
                failure_count: 0,
                observation_count: 2,
                status: 'candidate',
                created_at: null,
                updated_at: null,
            } as any,
        ]);
        vi.mocked(CodingKnowledgeConfirmMany).mockResolvedValueOnce({ confirmed: ['ready-1'], failed: [] } as any);

        render(
            <CodingKnowledgeSection
                config={{ coding_knowledge_auto_save_mode: 'observe' } as any}
                setConfig={vi.fn()}
                lang="zh-Hans"
                versionRef={{ current: 0 }}
            />,
        );

        fireEvent.click(await screen.findByRole('button', { name: '确认达标候选（1）' }));
        await waitFor(() => expect(CodingKnowledgeConfirmMany).toHaveBeenCalledWith(['ready-1']));
    });
});
