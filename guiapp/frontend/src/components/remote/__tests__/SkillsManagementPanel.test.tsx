// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';

class ResizeObserverMock {
    observe() { }
    unobserve() { }
    disconnect() { }
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock);

const ListNLSkillsMock = vi.fn();
const CreateNLSkillMock = vi.fn();
const UpdateNLSkillMock = vi.fn();
const SetNLSkillStatusMock = vi.fn();
const DeleteNLSkillMock = vi.fn();
const ImportNLSkillZipMock = vi.fn();
const SearchMixedSkillsMock = vi.fn();
const InstallMixedSkillMock = vi.fn();
const CheckHubSkillUpdatesMock = vi.fn();
const UpdateHubSkillMock = vi.fn();
const ExportLearnedSkillsZipMock = vi.fn();
const ImportLearnedSkillsZipMock = vi.fn();
const UploadNLSkillToMarketMock = vi.fn();
const DiagnoseSkillFilesMock = vi.fn();
const ListExternalSkillDirsDetailedMock = vi.fn();
const AddExternalSkillDirMock = vi.fn();
const RemoveExternalSkillDirMock = vi.fn();
const SelectProjectDirMock = vi.fn();
const OpenSystemUrlMock = vi.fn();
const GetHubRecommendationsMock = vi.fn();
const ListSkillSuitesMock = vi.fn();
const InstallSkillSuiteMock = vi.fn();
const UploadSkillSuiteMock = vi.fn();
const DownloadSkillSuiteZipMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListNLSkills: (...args: unknown[]) => ListNLSkillsMock(...args),
    CreateNLSkill: (...args: unknown[]) => CreateNLSkillMock(...args),
    UpdateNLSkill: (...args: unknown[]) => UpdateNLSkillMock(...args),
    SetNLSkillStatus: (...args: unknown[]) => SetNLSkillStatusMock(...args),
    DeleteNLSkill: (...args: unknown[]) => DeleteNLSkillMock(...args),
    ImportNLSkillZip: (...args: unknown[]) => ImportNLSkillZipMock(...args),
    SearchMixedSkills: (...args: unknown[]) => SearchMixedSkillsMock(...args),
    InstallMixedSkill: (...args: unknown[]) => InstallMixedSkillMock(...args),
    CheckHubSkillUpdates: (...args: unknown[]) => CheckHubSkillUpdatesMock(...args),
    UpdateHubSkill: (...args: unknown[]) => UpdateHubSkillMock(...args),
    ExportLearnedSkillsZip: (...args: unknown[]) => ExportLearnedSkillsZipMock(...args),
    ImportLearnedSkillsZip: (...args: unknown[]) => ImportLearnedSkillsZipMock(...args),
    UploadNLSkillToMarket: (...args: unknown[]) => UploadNLSkillToMarketMock(...args),
    DiagnoseSkillFiles: (...args: unknown[]) => DiagnoseSkillFilesMock(...args),
    ListExternalSkillDirsDetailed: (...args: unknown[]) => ListExternalSkillDirsDetailedMock(...args),
    AddExternalSkillDir: (...args: unknown[]) => AddExternalSkillDirMock(...args),
    RemoveExternalSkillDir: (...args: unknown[]) => RemoveExternalSkillDirMock(...args),
    SelectProjectDir: (...args: unknown[]) => SelectProjectDirMock(...args),
    OpenSystemUrl: (...args: unknown[]) => OpenSystemUrlMock(...args),
    GetHubRecommendations: (...args: unknown[]) => GetHubRecommendationsMock(...args),
    ListSkillSuites: (...args: unknown[]) => ListSkillSuitesMock(...args),
    InstallSkillSuite: (...args: unknown[]) => InstallSkillSuiteMock(...args),
    UploadSkillSuite: (...args: unknown[]) => UploadSkillSuiteMock(...args),
    DownloadSkillSuiteZip: (...args: unknown[]) => DownloadSkillSuiteZipMock(...args),
    // Exports the panel imports but individual tests do not drive; safe
    // defaults keep renders and background loads working.
    ApplySkillMaintenanceAction: vi.fn(async () => undefined),
    BatchSetNLSkillStatus: vi.fn(async () => undefined),
    CancelSkillEvolution: vi.fn(async () => undefined),
    ClearSkillEvolutionCompensation: vi.fn(async () => undefined),
    ExportTextFile: vi.fn(async () => undefined),
    GetExperienceAuditHealth: vi.fn(async () => ({})),
    GetSkillEvolutionStatus: vi.fn(async () => ({})),
    ListExperienceAudit: vi.fn(async () => []),
    ListSkillEvolutionAudit: vi.fn(async () => []),
    ListSkillEvolutionCompensations: vi.fn(async () => []),
    ListSkillMaintenanceDrafts: vi.fn(async () => []),
    ListSkillRepairDrafts: vi.fn(async () => []),
    ListSkillYAMLBackups: vi.fn(async () => []),
    LoadConfig: vi.fn(async () => ({})),
    OpenFileOrShowInFolder: vi.fn(async () => undefined),
    PatchConfigFields: vi.fn(async () => undefined),
    PurchaseSkillSuite: vi.fn(async () => undefined),
    RenameNLSkill: vi.fn(async () => undefined),
    ResolveCriticalConfirm: vi.fn(async () => false),
    RestoreSkillYAMLBackup: vi.fn(async () => undefined),
    RetrySkillEvolutionCompensation: vi.fn(async () => undefined),
    TriggerSkillOptimize: vi.fn(async () => undefined),
    TriggerSkillSelfRepair: vi.fn(async () => undefined),
    VerifyAndActivateNLSkillWithArgs: vi.fn(async () => undefined),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

import { EventsOn } from '../../../../wailsjs/runtime';

import { SkillsManagementPanel, getLearnedSkillDescriptionPreview, hubSourceFilterMatches, skillDescriptionTooltip, SKILL_CARD_COLUMNS, SKILL_CARD_PAGE_SIZE } from '../SkillsManagementPanel';
import { getSkillSourceLabel, getSkillSourceTooltip } from '../SkillSourceBadge';
import { DialogProvider } from '../../CustomDialog';
import { ToastProvider } from '../../Toast';
import { miniAppLabels } from '../../../i18n/maclawMiniAppLabels';

const localizeText = (en: string, zhHans: string, _zhHant?: string) => zhHans || en;

/** Wrap component with required context providers */
function renderPanel() {
    return render(
        <DialogProvider>
            <ToastProvider>
                <SkillsManagementPanel localizeText={localizeText} />
            </ToastProvider>
        </DialogProvider>
    );
}

const sampleSkills = [
    {
        name: 'paper_digest',
        description: 'Generate paper digest PDF',
        triggers: ['daily papers'],
        steps: [{ action: 'craft_tool', params: {}, on_error: 'stop' }],
        status: 'active',
        created_at: '2026-04-09T00:00:00Z',
        source: 'github',
        source_project: 'hf-daily-papers',
        execution_class: 'agent_markdown_skill',
        usage_count: 3,
        success_rate: 1,
    },
    {
        name: 'local_helper',
        description: 'Native helper skill',
        triggers: ['helper'],
        steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
        status: 'active',
        created_at: '2026-04-09T00:00:00Z',
        source: 'manual',
        execution_class: 'native_skill',
        usage_count: 0,
        success_rate: 0,
    },
    {
        name: 'invoice_app',
        description: 'Invoice review app',
        triggers: ['invoice'],
        steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
        status: 'active',
        created_at: '2026-04-09T00:00:00Z',
        source: 'manual',
        execution_class: 'native_skill',
        usage_count: 0,
        success_rate: 0,
        is_maclaw_app: true,
        maclaw_app_count: 1,
        maclaw_app_entry: 'maclaw.app.json',
    },
];

describe('hubSourceFilterMatches', () => {
    it('treats every HubCenter API alias as one Hub / HubCenter source', () => {
        for (const source of ['enterprise_hub', 'hub', 'hubcenter', 'skillmarket', 'skillhub']) {
            expect(hubSourceFilterMatches(source, 'hubcenter')).toBe(true);
        }
    });

    it('does not mix external sources into Hub / HubCenter', () => {
        expect(hubSourceFilterMatches('clawhub', 'hubcenter')).toBe(false);
        expect(hubSourceFilterMatches('github', 'hubcenter')).toBe(false);
    });
});

describe('Hub / HubCenter source presentation', () => {
    it('normalizes every legacy HubCenter alias in the result badge', () => {
        for (const source of ['enterprise_hub', 'hub', 'hubcenter', 'skillmarket', 'skillhub']) {
            const skill = { source, source_label: 'legacy label' };
            expect(getSkillSourceLabel(skill)).toBe('Hub / HubCenter');
            expect(getSkillSourceTooltip(skill, localizeText)).toBe('Hub / HubCenter 能力市场。');
        }
    });
});

describe('SkillsManagementPanel marketplace source filter', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ListNLSkillsMock.mockResolvedValue(sampleSkills);
        CheckHubSkillUpdatesMock.mockResolvedValue([]);
        ListExternalSkillDirsDetailedMock.mockResolvedValue([]);
        GetHubRecommendationsMock.mockResolvedValue([]);
        SearchMixedSkillsMock.mockResolvedValue([
            ...['enterprise_hub', 'hub', 'hubcenter', 'skillmarket', 'skillhub'].map((source) => ({
                id: `${source}-pdf`, name: `${source} PDF`, description: '', tags: [], source, source_label: 'legacy label',
                avg_rating: 0, rating_count: 0, downloads: 0, score: 0, price: 0, installed: false, can_update: false, has_update: false,
            })),
            { id: 'clawhub-pdf', name: 'ClawHub PDF', description: '', tags: [], source: 'clawhub', source_label: 'ClawHub', avg_rating: 0, rating_count: 0, downloads: 0, score: 0, price: 0, installed: false, can_update: false, has_update: false },
            { id: 'github-pdf', name: 'GitHub PDF', description: '', tags: [], source: 'github', source_label: 'GitHub', avg_rating: 0, rating_count: 0, downloads: 0, score: 0, price: 0, installed: false, can_update: false, has_update: false },
        ]);
    });

    it('shows all HubCenter aliases and excludes other sources when the Hub / HubCenter filter is selected', async () => {
        renderPanel();
        await screen.findByText('paper_digest');
        fireEvent.click(screen.getByText('能力市场'));
        fireEvent.change(document.querySelector('input.form-input') as HTMLInputElement, { target: { value: 'pdf' } });
        fireEvent.click(document.querySelector('button.btn-primary') as HTMLButtonElement);
        await screen.findByText('skillhub PDF');

        fireEvent.change(screen.getByLabelText('Market source'), { target: { value: 'hubcenter' } });

        for (const source of ['enterprise_hub', 'hub', 'hubcenter', 'skillmarket', 'skillhub']) {
            expect(screen.getByText(`${source} PDF`)).toBeTruthy();
        }
        expect(screen.queryByText('ClawHub PDF')).toBeNull();
        expect(screen.queryByText('GitHub PDF')).toBeNull();
    });
});

describe('SkillsManagementPanel execution class', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ListNLSkillsMock.mockResolvedValue(sampleSkills);
        CheckHubSkillUpdatesMock.mockResolvedValue([]);
        SearchMixedSkillsMock.mockResolvedValue([]);
        ListExternalSkillDirsDetailedMock.mockResolvedValue([]);
        GetHubRecommendationsMock.mockResolvedValue([]);
    });

    it('shows execution class badges for installed skills', async () => {
        renderPanel();

        await waitFor(() => {
            expect(screen.getByText('paper_digest')).toBeTruthy();
        });

        expect(ListNLSkillsMock).toHaveBeenCalled();
        expect(screen.getByText('代理 Skill')).toBeTruthy();
        expect(screen.getAllByText('原生 Skill').length).toBeGreaterThan(0);
        expect(screen.getByTitle('导入的 Markdown 类 Skill，通过 agent skill 流程执行。')).toBeTruthy();
        expect(screen.getAllByTitle('常规 Skill，直接由原生 skill runner 执行。').length).toBeGreaterThan(0);
        expect(screen.getByText('invoice_app')).toBeTruthy();
    });
    it('shows MaClaw App skills in their own category', async () => {
        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByRole('button', { name: new RegExp(`${miniAppLabels.short.zhHans} \\(1\\)`) }));

        expect(screen.getByText('invoice_app')).toBeTruthy();
        expect(screen.queryByText('paper_digest')).toBeNull();
    });
    it('starts an active agent-guided workflow through the AI assistant, not the GUI runner', async () => {
        const started: string[] = [];
        const onStart = (event: Event) => {
            started.push(String((event as CustomEvent).detail?.name || ''));
        };
        window.addEventListener('maclaw:start-agent-guided-workflow', onStart);
        ListNLSkillsMock.mockResolvedValue([{
            name: 'Book-PDF',
            description: 'Create a book PDF with research and multiple agents',
            triggers: ['book pdf'],
            steps: [{ action: 'craft_tool', params: {}, on_error: 'stop' }],
            status: 'active',
            created_at: '2026-04-09T00:00:00Z',
            source: 'clawhub',
            execution_class: 'agent_guided_workflow',
            usage_count: 0,
            success_rate: 0,
        }]);

        try {
            renderPanel();
            await screen.findByText('Book-PDF');
            expect(screen.getByText('需 Agent 编排')).toBeTruthy();
            fireEvent.click(screen.getByRole('button', { name: '用 AI 助手启动' }));
            expect(started).toEqual(['Book-PDF']);
            expect(screen.queryByRole('button', { name: '运行' })).toBeNull();
            expect(screen.queryByRole('button', { name: '修复' })).toBeNull();
            expect(screen.queryByRole('button', { name: '优化' })).toBeNull();
        } finally {
            window.removeEventListener('maclaw:start-agent-guided-workflow', onStart);
        }
    });
    it('shows review reasons and can approve a needs-review skill', async () => {
        ListNLSkillsMock.mockResolvedValue([
            {
                name: 'RapidOCR',
                description: 'OCR images',
                triggers: ['ocr'],
                steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
                status: 'needs_review',
                review_reason: 'auto-repair blocked by security scan: level=high summary=uses shell',
                last_error: 'auto-repair blocked by security scan',
                created_at: '2026-04-09T00:00:00Z',
                source: 'hub',
                execution_class: 'native_skill',
                usage_count: 5,
                success_rate: 0.2,
            },
        ]);
        SetNLSkillStatusMock.mockResolvedValue(undefined);

        renderPanel();

        await waitFor(() => expect(screen.getByText('RapidOCR')).toBeTruthy());
        expect(screen.getAllByTitle('auto-repair blocked by security scan: level=high summary=uses shell').length).toBeGreaterThan(0);
        expect(screen.getByText(/\u5ba1\u6838\u539f\u56e0/)).toBeTruthy();

        fireEvent.click(screen.getByTitle('审核并启用'));
        await waitFor(() => expect(screen.getAllByText(/auto-repair blocked by security scan/).length).toBeGreaterThan(0));
        fireEvent.click(screen.getByRole('button', { name: '审核通过并启用' }));

        await waitFor(() => {
            expect(SetNLSkillStatusMock).toHaveBeenCalledWith('RapidOCR', 'active');
        });
    });
    it('keeps a real agent-workflow security review visible and approvable', async () => {
        const securityReason = 'auto-repair blocked by security scan: level=high summary=uses shell';
        ListNLSkillsMock.mockResolvedValue([{
            name: 'Book-PDF',
            description: 'Create a book PDF with research and multiple agents',
            triggers: ['book pdf'],
            steps: [{ action: 'craft_tool', params: {}, on_error: 'stop' }],
            status: 'needs_review',
            review_reason: securityReason,
            last_error: securityReason,
            created_at: '2026-04-09T00:00:00Z',
            source: 'clawhub',
            execution_class: 'agent_guided_workflow',
            usage_count: 0,
            success_rate: 0,
        }]);
        SetNLSkillStatusMock.mockResolvedValue(undefined);

        renderPanel();

        await screen.findByText('Book-PDF');
        expect(screen.getAllByTitle(securityReason).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByTitle(/\u5ba1\u6838\u5e76\u542f\u7528/));
        const approveButton = await screen.findByRole('button', { name: /\u5ba1\u6838\u901a\u8fc7\u5e76\u542f\u7528/ });
        fireEvent.click(approveButton);
        await waitFor(() => {
            expect(SetNLSkillStatusMock).toHaveBeenCalledWith('Book-PDF', 'active');
        });
    });
    it('keeps MaClaw App skills filterable from their category', async () => {
        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByRole('button', { name: new RegExp(`${miniAppLabels.short.zhHans} \\(1\\)`) }));

        expect(screen.getByText('invoice_app')).toBeTruthy();
        expect(screen.queryByText('paper_digest')).toBeNull();
    });
    it('keeps learned skill names and descriptions compact in the list', async () => {
        const longDescription = '读取 C:\\Users\\ma139\\Desktop\\test\\auth-*.json 文件的数量，并列出前 5 个和后 5 个文件名。';
        const longName = 'craft_c_users_ma139_desktop_test_auth_json_file_counter';
        ListNLSkillsMock.mockResolvedValue([
            {
                name: longName,
                description: longDescription,
                triggers: ['auth json'],
                steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
                status: 'active',
                created_at: '2026-04-09T00:00:00Z',
                source: 'learned',
                execution_class: 'native_skill',
                usage_count: 0,
                success_rate: 0,
            },
        ]);

        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });
        fireEvent.click(screen.getByRole('button', { name: /自学习 \(1\)/ }));

        expect(screen.getByTitle(longName)).toBeTruthy();
        expect(screen.getByText(getLearnedSkillDescriptionPreview(longDescription))).toBeTruthy();
        expect(screen.queryByText(longDescription)).toBeNull();
        expect(screen.getByTitle(longDescription)).toBeTruthy();
        expect(screen.queryByRole('button', { name: '上一页' })).toBeNull();
        expect(screen.queryByRole('button', { name: '下一页' })).toBeNull();
    });
    it('shows four skill cards per row and twenty per page', async () => {
        expect(SKILL_CARD_COLUMNS).toBe(4);
        expect(SKILL_CARD_PAGE_SIZE).toBe(20);
        const many = Array.from({ length: SKILL_CARD_PAGE_SIZE + 1 }, (_, index) => ({
            name: `skill_${index}`,
            description: `description ${index}`,
            triggers: ['skill'],
            steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
            status: 'active',
            created_at: '2026-04-09T00:00:00Z',
            source: 'manual',
            execution_class: 'native_skill',
            usage_count: 0,
            success_rate: 0,
        }));
        ListNLSkillsMock.mockResolvedValue(many);
        renderPanel();

        await waitFor(() => expect(screen.getByText('skill_0')).toBeTruthy());
        const grid = screen.getByTestId('skill-card-grid');
        expect(grid.style.gridTemplateColumns).toBe(`repeat(${SKILL_CARD_COLUMNS}, minmax(0, 1fr))`);
        expect(screen.getAllByTestId('skill-card')).toHaveLength(SKILL_CARD_PAGE_SIZE);
        expect(screen.queryByText('skill_20')).toBeNull();

        const scroller = screen.getByTestId('skills-tab-scroll');
        scroller.scrollTop = 48;
        fireEvent.click(screen.getByRole('button', { name: '下一页' }));
        expect(scroller.scrollTop).toBe(0);
        expect(screen.getByText('skill_20')).toBeTruthy();
        expect(screen.queryByText('skill_0')).toBeNull();
        expect(screen.getAllByTestId('skill-card')).toHaveLength(1);

        ListNLSkillsMock.mockResolvedValue(many.slice(0, 5));
        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        await waitFor(() => expect(screen.getByText('skill_0')).toBeTruthy());
        expect(screen.queryByText('skill_20')).toBeNull();
        expect(screen.queryByRole('button', { name: '下一页' })).toBeNull();
        expect(screen.getAllByTestId('skill-card')).toHaveLength(5);
    });
    it('keeps the visible catalog while a refresh is in flight', async () => {
        const many = Array.from({ length: SKILL_CARD_PAGE_SIZE + 1 }, (_, index) => ({
            name: `skill_${index}`,
            description: `description ${index}`,
            triggers: ['skill'],
            steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
            status: 'active',
            created_at: '2026-04-09T00:00:00Z',
            source: 'manual',
            execution_class: 'native_skill',
            usage_count: 0,
            success_rate: 0,
        }));
        let releaseRefresh: (skills: typeof many) => void = () => {};
        let calls = 0;
        ListNLSkillsMock.mockImplementation(() => {
            calls += 1;
            if (calls === 1) return Promise.resolve(many);
            return new Promise<typeof many>((resolve) => {
                releaseRefresh = resolve;
            });
        });
        renderPanel();
        await waitFor(() => expect(screen.getByText('skill_0')).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: '下一页' }));
        expect(screen.getByText('skill_20')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: '刷新' }));
        expect(screen.getByText('skill_20')).toBeTruthy();
        expect(screen.queryByText('加载中...')).toBeNull();

        await act(async () => {
            releaseRefresh(many.slice(0, 5));
        });
        await waitFor(() => expect(screen.getByText('skill_0')).toBeTruthy());
        expect(screen.queryByText('skill_20')).toBeNull();
        expect(screen.getAllByTestId('skill-card')).toHaveLength(5);
    });
    it('keeps skill settings tabs above the scroller', () => {
        renderPanel();
        fireEvent.click(screen.getByRole('button', { name: '设置' }));
        const scroller = screen.getByTestId('skills-tab-scroll');
        const settingsTabs = screen.getByRole('tablist', { name: '技能设置' });
        expect(scroller.contains(settingsTabs)).toBe(false);
        expect(settingsTabs.compareDocumentPosition(scroller) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(screen.getByRole('tab', { name: '自进化' }).getAttribute('aria-selected')).toBe('true');
    });
    it('keeps the suite member dialog outside the scroller', async () => {
        ListSkillSuitesMock.mockResolvedValue([{
            id: 'suite-1',
            name: 'Research Suite',
            description: 'Papers and notes',
            members: [
                { name: 'paper_digest', required: true },
                { name: 'local_helper', required: false },
            ],
        }]);
        renderPanel();
        fireEvent.click(screen.getByRole('button', { name: '能力市场' }));
        fireEvent.click(await screen.findByRole('button', { name: '详情' }));
        const dialog = screen.getByRole('dialog');
        const scroller = screen.getByTestId('skills-tab-scroll');
        expect(scroller.contains(dialog)).toBe(false);
        expect(screen.getByRole('button', { name: '全选' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '安装' })).toBeTruthy();
    });
    it('scrolls capability market rows under the search field', async () => {
        GetHubRecommendationsMock.mockResolvedValue(Array.from({ length: 3 }, (_, index) => ({
            id: `hub-${index}`,
            name: `hub_skill_${index}`,
            description: 'market row',
            tags: [],
            source: 'hubcenter',
            source_label: 'Hub / HubCenter',
            avg_rating: 0,
            rating_count: 0,
            downloads: 0,
            score: 0,
            price: 0,
            installed: false,
            can_update: false,
            has_update: false,
        })));
        renderPanel();
        const outer = screen.getByTestId('skills-tab-scroll');
        expect(outer.style.overflowY).toBe('scroll');

        fireEvent.click(screen.getByRole('button', { name: '能力市场' }));
        const search = await screen.findByPlaceholderText('搜索 Hub Skill...');
        const list = await screen.findByTestId('hub-catalog-scroll');
        expect(await screen.findByText('hub_skill_2')).toBeTruthy();
        expect(list.contains(search)).toBe(false);
        expect(list.contains(screen.getByText('hub_skill_0'))).toBe(true);
        expect(list.contains(screen.getByText('热门 Skill'))).toBe(false);
        expect(list.style.overflowY).toBe('scroll');
        expect(list.style.flexBasis).toBe('0%');
        expect(list.style.scrollbarGutter).toBe('stable');
        expect(list.style.scrollbarColor).toBe('auto');
        expect(outer.style.overflowY).toBe('auto');
        expect(outer.contains(list)).toBe(true);
        const catalog = list.parentElement as HTMLElement;
        expect(catalog.style.flexShrink).toBe('1');
        expect(catalog.style.flexBasis).toBe('0%');
        expect(catalog.style.minHeight).toBe('180px');
        expect(catalog.contains(search)).toBe(true);
        expect(catalog.contains(screen.getByText('热门 Skill'))).toBe(true);

        list.scrollTop = 40;
        fireEvent.change(search, { target: { value: 'pdf' } });
        fireEvent.click(screen.getByRole('button', { name: '搜索' }));
        expect(list.scrollTop).toBe(0);
    });
    it('does not scroll Market when the skill catalog page clamps', async () => {
        const handlers = new Map<string, () => void>();
        vi.mocked(EventsOn).mockImplementation((event: string, handler: () => void) => {
            handlers.set(event, handler);
            return () => undefined;
        });
        const many = Array.from({ length: SKILL_CARD_PAGE_SIZE + 1 }, (_, index) => ({
            name: `skill_${index}`,
            description: `description ${index}`,
            triggers: ['skill'],
            steps: [{ action: 'run_skill', params: {}, on_error: 'stop' }],
            status: 'active',
            created_at: '2026-04-09T00:00:00Z',
            source: 'manual',
            execution_class: 'native_skill',
            usage_count: 0,
            success_rate: 0,
        }));
        ListNLSkillsMock.mockResolvedValue(many);
        try {
            renderPanel();
            await waitFor(() => expect(screen.getByText('skill_0')).toBeTruthy());
            fireEvent.click(screen.getByRole('button', { name: '下一页' }));
            expect(screen.getByText('skill_20')).toBeTruthy();
            fireEvent.click(screen.getByRole('button', { name: '能力市场' }));
            const scroller = screen.getByTestId('skills-tab-scroll');
            scroller.scrollTop = 72;
            ListNLSkillsMock.mockResolvedValue(many.slice(0, 5));
            await act(async () => {
                handlers.get('skill:index_refreshed')?.();
            });
            expect(scroller.scrollTop).toBe(72);
        } finally {
            vi.mocked(EventsOn).mockImplementation(() => () => undefined);
        }
    });
    it('does not show the obsolete MaClaw App upload action in the filtered category', async () => {
        UploadNLSkillToMarketMock.mockResolvedValue('submission-app-1');
        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByRole('button', { name: new RegExp(`${miniAppLabels.short.zhHans} \\(1\\)`) }));

        expect(screen.getByText('invoice_app')).toBeTruthy();
        expect(screen.queryByText('上传')).toBeNull();
        expect(UploadNLSkillToMarketMock).not.toHaveBeenCalled();
    });
    it('shows public and private market source badges with tooltips for search results', async () => {
        SearchMixedSkillsMock.mockResolvedValue([
            {
                id: 'private-skill',
                name: 'Private Paper Skill',
                description: 'Private market result',
                tags: [],
                source: 'enterprise_hub',
                source_label: 'Hub / HubCenter',
                avg_rating: 0,
                rating_count: 0,
                downloads: 0,
                score: 100,
                price: 0,
                installed: false,
                can_update: false,
                has_update: false,
            },
            {
                id: 'public-skill',
                name: 'Public Paper Skill',
                description: 'Public market result',
                tags: [],
                source: 'skillmarket',
                source_label: 'Hub / HubCenter',
                avg_rating: 0,
                rating_count: 0,
                downloads: 0,
                score: 90,
                price: 0,
                installed: false,
                can_update: false,
                has_update: false,
            },
        ]);

        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByText('能力市场'));

        const input = document.querySelector('input.form-input') as HTMLInputElement;
        expect(input).toBeTruthy();
        fireEvent.change(input, { target: { value: 'paper' } });

        const searchButton = document.querySelector('button.btn-primary') as HTMLButtonElement;
        expect(searchButton).toBeTruthy();
        fireEvent.click(searchButton);

        await waitFor(() => {
            expect(SearchMixedSkillsMock).toHaveBeenCalledWith('paper');
        });

        expect(screen.getByText('Private Paper Skill')).toBeTruthy();
        expect(screen.getByText('Public Paper Skill')).toBeTruthy();
        expect(screen.getAllByTitle('Hub / HubCenter 能力市场。')).toHaveLength(2);
    });
    it('marks MaClaw App Skill search results', async () => {
        SearchMixedSkillsMock.mockResolvedValue([
            {
                id: 'invoice-app',
                name: 'Invoice App',
                description: 'Invoice review app skill',
                tags: [],
                source: 'skillmarket',
                source_label: 'Hub / HubCenter',
                avg_rating: 0,
                rating_count: 0,
                downloads: 0,
                score: 100,
                price: 0,
                installed: false,
                can_update: false,
                has_update: false,
                product_kind: 'maclaw_app_skill',
                is_maclaw_app: true,
                maclaw_app_name: 'Invoice Review',
                maclaw_app_category: 'finance',
                maclaw_app_icon: 'receipt',
                maclaw_app_output_modes: ['pdf', 'docx'],
                artifact_contract_output_modes: ['pdf'],
            },
        ]);

        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByText('能力市场'));
        fireEvent.change(document.querySelector('input.form-input') as HTMLInputElement, { target: { value: 'invoice' } });
        fireEvent.click(document.querySelector('button.btn-primary') as HTMLButtonElement);

        await waitFor(() => {
            expect(SearchMixedSkillsMock).toHaveBeenCalledWith('invoice');
        });
        expect(screen.getByText('Invoice App')).toBeTruthy();
        expect(screen.getByText(/Invoice Review/)).toBeTruthy();
        expect(screen.getByText('finance')).toBeTruthy();
        expect(screen.getByText('pdf')).toBeTruthy();
        expect(screen.getByTitle(miniAppLabels.skill.zhHans)).toBeTruthy();
    });

    it('marks MaClaw App Skill recommendations', async () => {
        GetHubRecommendationsMock.mockResolvedValue([
            {
                id: 'invoice-app',
                name: 'Invoice App',
                description: 'Invoice review app skill',
                tags: [],
                source: 'skillhub',
                source_label: 'Hub / HubCenter',
                avg_rating: 0,
                rating_count: 0,
                downloads: 0,
                score: 100,
                price: 0,
                installed: false,
                can_update: false,
                has_update: false,
                product_kind: 'maclaw_app_skill',
                is_maclaw_app: true,
                maclaw_app_name: 'Invoice Review',
                maclaw_app_category: 'finance',
                maclaw_app_output_modes: ['pdf'],
                artifact_contract_output_modes: ['pdf'],
            },
        ]);

        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByText('能力市场'));

        await waitFor(() => {
            expect(GetHubRecommendationsMock).toHaveBeenCalled();
        });
        expect(screen.getByText('Invoice App')).toBeTruthy();
        expect(screen.getByText(/Invoice Review/)).toBeTruthy();
        expect(screen.getByText('pdf')).toBeTruthy();
        expect(screen.getByTitle(miniAppLabels.skill.zhHans)).toBeTruthy();
    });
});

describe('SkillsManagementPanel needs-setup recovery', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ListNLSkillsMock.mockResolvedValue([{
            ...sampleSkills[1],
            name: 'needs_setup_skill',
            status: 'needs_setup',
        }]);
        CheckHubSkillUpdatesMock.mockResolvedValue([]);
        SearchMixedSkillsMock.mockResolvedValue([]);
        ListExternalSkillDirsDetailedMock.mockResolvedValue([]);
        GetHubRecommendationsMock.mockResolvedValue([]);
        UpdateNLSkillMock.mockResolvedValue(undefined);
    });

    it('provides a configuration action that saves the skill as active', async () => {
        renderPanel();

        const configureButton = await screen.findByRole('button', { name: '配置并启用' });
        fireEvent.click(configureButton);

        await waitFor(() => {
            expect(screen.getByText('配置 Skill')).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: '保存并启用' }));

        await waitFor(() => {
            expect(UpdateNLSkillMock).toHaveBeenCalledWith(expect.objectContaining({
                name: 'needs_setup_skill',
                status: 'active',
            }));
        });
    });

    it('does not override a status changed to needs review while opening configuration', async () => {
        const needsSetupSkill = { ...sampleSkills[1], name: 'needs_setup_skill', status: 'needs_setup' };
        const needsReviewSkill = { ...needsSetupSkill, status: 'needs_review' };
        ListNLSkillsMock.mockReset()
            .mockResolvedValueOnce([needsSetupSkill])
            .mockResolvedValue([needsReviewSkill]);

        renderPanel();
        fireEvent.click(await screen.findByRole('button', { name: '配置并启用' }));

        await waitFor(() => {
            expect(screen.getByText('编辑 Skill')).toBeTruthy();
        });
        expect(screen.queryByText('配置 Skill')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: '保存' }));
        await waitFor(() => {
            expect(UpdateNLSkillMock).toHaveBeenCalledWith(expect.objectContaining({ status: 'needs_review' }));
        });
    });
});

/**
 * Bug B fix tests: modal backdrop mousedown+click guard.
 *
 * The fix uses a `backdropMouseDownRef` pattern: the dialog only closes when
 * BOTH mousedown AND click originate on the modal-backdrop itself. If mousedown
 * starts inside modal-content (e.g., on an input) but the click lands on the
 * backdrop (drag-to-backdrop), the dialog stays open.
 *
 * Validates: Requirements 2.4, 2.5, 3.8, 3.9
 */
describe('SkillsManagementPanel modal backdrop mousedown+click guard', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ListNLSkillsMock.mockResolvedValue(sampleSkills);
        CheckHubSkillUpdatesMock.mockResolvedValue([]);
        SearchMixedSkillsMock.mockResolvedValue([]);
        ListExternalSkillDirsDetailedMock.mockResolvedValue([]);
        GetHubRecommendationsMock.mockResolvedValue([]);
    });

    /** Helper: open the create/edit form dialog and return the backdrop element */
    async function openEditFormDialog() {
        renderPanel();

        // Wait for skills to load
        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        // Wait for the edit button to appear and click it to open the edit form
        const editButtons = await screen.findAllByRole('button', { name: '编辑' });
        fireEvent.click(editButtons[0]);

        // Wait for the form dialog to appear (loadData is called again inside openEditForm)
        await waitFor(() => {
            expect(screen.getByText('编辑 Skill')).toBeTruthy();
        });

        // Find the backdrop and modal-content elements
        const backdrop = document.querySelector('.modal-backdrop') as HTMLElement;
        const modalContent = backdrop.querySelector('.modal-content') as HTMLElement;
        const nameInput = backdrop.querySelector('input.form-input') as HTMLElement;

        expect(backdrop).toBeTruthy();
        expect(modalContent).toBeTruthy();
        expect(nameInput).toBeTruthy();

        return { backdrop, modalContent, nameInput };
    }

    // Task 4.1: mousedown inside modal-content → click on backdrop → dialog stays open
    it('does NOT close when mousedown starts inside modal-content and click lands on backdrop', async () => {
        const { backdrop, nameInput } = await openEditFormDialog();

        // Simulate mousedown on the input (inside modal-content)
        fireEvent.mouseDown(nameInput);

        // Simulate click on the backdrop (drag ended on backdrop)
        // The click event's target is the backdrop, but mousedown was on the input
        fireEvent.click(backdrop);

        // Dialog should remain open — the guard prevents closing
        expect(screen.getByText('编辑 Skill')).toBeTruthy();
    });

    // Task 4.2: mousedown on backdrop → click on backdrop → dialog closes
    it('closes when both mousedown and click originate on the backdrop', async () => {
        const { backdrop } = await openEditFormDialog();

        // Simulate mousedown on the backdrop itself
        fireEvent.mouseDown(backdrop);

        // Simulate click on the backdrop itself
        fireEvent.click(backdrop);

        // Dialog should be closed — intentional dismiss
        await waitFor(() => {
            expect(screen.queryByText('编辑 Skill')).toBeNull();
        });
    });

    // Task 4.3: close button (×) and Cancel button still close the dialog normally
    it('closes when the × close button is clicked', async () => {
        await openEditFormDialog();

        // Find and click the × close button
        const closeButton = document.querySelector('.btn-close') as HTMLElement;
        expect(closeButton).toBeTruthy();
        fireEvent.click(closeButton);

        // Dialog should be closed
        await waitFor(() => {
            expect(screen.queryByText('编辑 Skill')).toBeNull();
        });
    });

    it('closes when the Cancel button is clicked', async () => {
        await openEditFormDialog();

        // Find and click the Cancel button
        const cancelButton = screen.getByText('取消');
        expect(cancelButton).toBeTruthy();
        fireEvent.click(cancelButton);

        // Dialog should be closed
        await waitFor(() => {
            expect(screen.queryByText('编辑 Skill')).toBeNull();
        });
    });
});


describe('learned skill description preview', () => {
    it('keeps the table preview compact and leaves full text for title tooltips', () => {
        const full = '编写一个 PowerShell 脚本，从 Hugging Face Daily Papers API 获取最近一周的论文数据';

        expect(getLearnedSkillDescriptionPreview(full)).toBe('编写一个 PowerShell 脚本，从...');
        expect(getLearnedSkillDescriptionPreview('short description')).toBe('short description');
        expect(getLearnedSkillDescriptionPreview('   ')).toBe('-');
    });

    it('exposes a tooltip only when the preview cannot show the full description', () => {
        const full = '编写一个 PowerShell 脚本，从 Hugging Face Daily Papers API 获取最近一周的论文数据';

        expect(skillDescriptionTooltip(full)).toBe(full);
        expect(skillDescriptionTooltip('short description')).toBeUndefined();
        expect(skillDescriptionTooltip('   ')).toBeUndefined();
        expect(skillDescriptionTooltip(`  ${full}  `)).toBe(full);
    });
});
