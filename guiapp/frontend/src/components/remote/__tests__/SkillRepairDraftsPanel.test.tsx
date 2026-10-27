// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const ListSkillRepairDraftsMock = vi.fn();
const GetSkillRepairDraftMock = vi.fn();
const ApplySkillRepairDraftMock = vi.fn();
const RejectSkillRepairDraftMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListSkillRepairDrafts: (...args: unknown[]) => ListSkillRepairDraftsMock(...args),
    GetSkillRepairDraft: (...args: unknown[]) => GetSkillRepairDraftMock(...args),
    ApplySkillRepairDraft: (...args: unknown[]) => ApplySkillRepairDraftMock(...args),
    RejectSkillRepairDraft: (...args: unknown[]) => RejectSkillRepairDraftMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

import { SkillRepairDraftsPanel } from '../SkillRepairDraftsPanel';
import { DialogProvider } from '../../CustomDialog';
import { ToastProvider } from '../../Toast';

const localizeText = (en: string, zhHans: string, _zhHant?: string) => zhHans || en;

function renderPanel() {
    return render(
        <DialogProvider>
            <ToastProvider>
                <SkillRepairDraftsPanel
                    localizeText={localizeText}
                    busy={false}
                    setBusy={() => { }}
                    evolutionFocusSkill={null}
                    onFocusSkill={() => { }}
                    onDraftsChanged={() => { }}
                />
            </ToastProvider>
        </DialogProvider>
    );
}

/** Draft whose steps carry capture as a map[string]string JSON object — the
 *  shape Go repairDraftStepView.Capture actually serializes to. */
const draftWithCapture = {
    skill: 'web_scraper',
    draft: 'draft-001.json',
    explanation: 'fix selector',
    created_at: '2026-07-01T00:00:00Z',
    old_steps: [
        {
            action: 'http_get',
            name: 'fetch list',
            label: 'fetch',
            when: 'always',
            params: { url: 'https://example.com' },
            capture: { page_html: '$.body', next_url: '$.links.next' },
            condition: 'ok',
        },
    ],
    new_steps: [
        {
            action: 'http_get',
            params: { url: 'https://example.com/v2' },
            capture: { page_html: '$.data' },
        },
    ],
};

describe('SkillRepairDraftsPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('loads full steps on expansion and renders capture map entries', async () => {
        ListSkillRepairDraftsMock.mockResolvedValue(JSON.stringify({
            ok: true,
            count: 1,
            drafts: [{
                skill: draftWithCapture.skill,
                draft: draftWithCapture.draft,
                explanation: draftWithCapture.explanation,
                created_at: draftWithCapture.created_at,
                old_steps_summary: { count: 1, actions: ['http_get'] },
                new_steps_summary: { count: 1, actions: ['http_get'] },
            }],
        }));
        GetSkillRepairDraftMock.mockResolvedValue(
            JSON.stringify({ ok: true, draft: draftWithCapture }),
        );
        renderPanel();

        await screen.findByText('web_scraper');
        expect(GetSkillRepairDraftMock).not.toHaveBeenCalled();

        fireEvent.click(await screen.findByText(/步骤 \(1 → 1\)/));

        await screen.findByText('page_html → $.body');
        expect(GetSkillRepairDraftMock).toHaveBeenCalledWith('web_scraper', 'draft-001.json');
        expect(screen.getByText('next_url → $.links.next')).toBeTruthy();
        expect(screen.getByText('page_html → $.data')).toBeTruthy();
        expect(screen.queryByText(/\[object Object\]/)).toBeNull();
        expect(screen.getByText(/name: fetch list/)).toBeTruthy();
        expect(screen.getByText(/label: fetch/)).toBeTruthy();

        fireEvent.click(screen.getByText('收起步骤'));
        fireEvent.click(screen.getByText(/步骤 \(1 → 1\)/));
        expect(GetSkillRepairDraftMock).toHaveBeenCalledTimes(1);
    });

    it('shows the empty state when there are no drafts', async () => {
        ListSkillRepairDraftsMock.mockResolvedValue(
            JSON.stringify({ ok: true, count: 0, drafts: [] }),
        );
        renderPanel();
        await screen.findByText('暂无待评审的修复。');
    });

    it('renders a disable draft without a steps button', async () => {
        ListSkillRepairDraftsMock.mockResolvedValue(
            JSON.stringify({
                ok: true,
                count: 1,
                drafts: [{
                    skill: 'broken_skill',
                    draft: 'draft-002.json',
                    explanation: 'disable it',
                    disable: true,
                }],
            }),
        );
        renderPanel();
        await screen.findByText('broken_skill');
        expect(screen.getByText('建议禁用')).toBeTruthy();
        expect(screen.getByText('禁用该技能')).toBeTruthy();
        expect(screen.queryByText(/步骤 \(/)).toBeNull();
    });
});
