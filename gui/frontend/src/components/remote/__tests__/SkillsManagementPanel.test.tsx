// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

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
}));

import { SkillsManagementPanel } from '../SkillsManagementPanel';

const localizeText = (en: string, zhHans: string) => zhHans || en;

describe('SkillsManagementPanel execution class', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        ListNLSkillsMock.mockResolvedValue([
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
        ]);
        CheckHubSkillUpdatesMock.mockResolvedValue([]);
        SearchMixedSkillsMock.mockResolvedValue([]);
        ListExternalSkillDirsDetailedMock.mockResolvedValue([]);
    });

    it('shows execution class badges for installed skills', async () => {
        render(<SkillsManagementPanel localizeText={localizeText} />);

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
