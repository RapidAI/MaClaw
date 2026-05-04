// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';

const ListNLSkillsMock = vi.fn();
const CreateNLSkillMock = vi.fn();
const UpdateNLSkillMock = vi.fn();
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

import { SkillsManagementPanel, getLearnedSkillDescriptionPreview } from '../SkillsManagementPanel';
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
];

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
            expect(ListNLSkillsMock).toHaveBeenCalled();
        });

        expect(screen.getByText('类型')).toBeTruthy();
        expect(screen.getByText('代理 Skill')).toBeTruthy();
        expect(screen.getByText('原生 Skill')).toBeTruthy();
        expect(screen.getByTitle('导入的 Markdown 类 Skill，通过 agent skill 流程执行。')).toBeTruthy();
        expect(screen.getByTitle('常规 Skill，直接由原生 skill runner 执行。')).toBeTruthy();
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
        const editButtons = await screen.findAllByText('编辑');
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
