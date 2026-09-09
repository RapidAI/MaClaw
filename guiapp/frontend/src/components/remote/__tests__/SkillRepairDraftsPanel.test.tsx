// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const ListSkillRepairDraftsMock = vi.fn();
const ApplySkillRepairDraftMock = vi.fn();
const RejectSkillRepairDraftMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListSkillRepairDrafts: (...args: unknown[]) => ListSkillRepairDraftsMock(...args),
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

    it('expands steps with a capture object without crashing and renders var → pattern entries', async () => {
        ListSkillRepairDraftsMock.mockResolvedValue(
            JSON.stringify({ ok: true, count: 1, drafts: [draftWithCapture] }),
        );
        renderPanel();

        // Draft row appears after the initial load.
        await screen.findByText('web_scraper');

        // Expand the old/new steps comparison.
        fireEvent.click(await screen.findByText(/步骤 \(1 → 1\)/));

        // capture (a JSON object) renders as per-entry var → pattern rows —
        // never as a raw object (which React would refuse to render).
        await screen.findByText('page_html → $.body');
        expect(screen.getByText('next_url → $.links.next')).toBeTruthy();
        expect(screen.getByText('page_html → $.data')).toBeTruthy();
        expect(screen.queryByText(/\[object Object\]/)).toBeNull();

        // The step name field shows up among the meta rows.
        expect(screen.getByText(/name: fetch list/)).toBeTruthy();
        expect(screen.getByText(/label: fetch/)).toBeTruthy();
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
