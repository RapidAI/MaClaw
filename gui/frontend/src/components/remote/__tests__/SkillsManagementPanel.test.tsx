// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';

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
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

import { SkillsManagementPanel, getLearnedSkillDescriptionPreview, hubSourceFilterMatches } from '../SkillsManagementPanel';
import { getSkillSourceLabel, getSkillSourceTooltip } from '../SkillSourceBadge';
import { DialogProvider } from '../../CustomDialog';
import { ToastProvider } from '../../Toast';

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
            expect(screen.getByText('类型')).toBeTruthy();
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

        fireEvent.click(screen.getByRole('button', { name: /App \(1\)/ }));

        expect(screen.getByText('invoice_app')).toBeTruthy();
        expect(screen.queryByText('paper_digest')).toBeNull();
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
    it('keeps MaClaw App skills filterable from their category', async () => {
        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByRole('button', { name: /App \(1\)/ }));

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
    });
    it('does not show the obsolete MaClaw App upload action in the filtered category', async () => {
        UploadNLSkillToMarketMock.mockResolvedValue('submission-app-1');
        renderPanel();

        await waitFor(() => {
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        fireEvent.click(screen.getByRole('button', { name: /App \(1\)/ }));

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
        expect(screen.getByTitle('MaClaw App Skill')).toBeTruthy();
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
        expect(screen.getByTitle('MaClaw App Skill')).toBeTruthy();
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
});
