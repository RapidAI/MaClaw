// @vitest-environment jsdom
/**
 * Tests for the expert editor dialog: AI generation flow + save payload.
 * Like UtilitiesPage, the dialog reaches the backend via a dynamic import,
 * so the tests drive the real bindings through the injected `window.go` bridge.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ExpertEditorDialog } from '../ExpertEditorDialog';

type AppSpies = {
    GenerateExpertProfile: ReturnType<typeof vi.fn>;
    SaveExpert: ReturnType<typeof vi.fn>;
    ListAvailableToolNames: ReturnType<typeof vi.fn>;
    ListNLSkills: ReturnType<typeof vi.fn>;
};

function installAppSpies(): AppSpies {
    const spies: AppSpies = {
        GenerateExpertProfile: vi.fn(),
        SaveExpert: vi.fn(),
        ListAvailableToolNames: vi.fn().mockResolvedValue(JSON.stringify([
            { name: 'fs_read', description: 'Read files' },
            { name: 'fs_write', description: 'Write files' },
        ])),
        ListNLSkills: vi.fn().mockResolvedValue([{ name: 'pptx-gen' }, { name: 'pdf-word' }]),
    };
    (window as any).go = { main: { App: spies } };
    return spies;
}

describe('ExpertEditorDialog', () => {
    let spies: AppSpies;
    beforeEach(() => {
        spies = installAppSpies();
    });

    afterEach(() => {
        delete (window as any).go;
    });

    it('keeps AI generate disabled until an idea is entered, then fills the form', async () => {
        spies.GenerateExpertProfile.mockResolvedValue(JSON.stringify({
            name: '翻译专家',
            description: '中英互译',
            icon: '🌐',
            system_prompt: '你是翻译专家',
            suggested_tools: ['fs_read'],
            suggested_skills: ['pptx-gen'],
        }));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);

        const generateButton = screen.getByTestId('expert-generate-button') as HTMLButtonElement;
        expect(generateButton.disabled).toBe(true);

        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: '论文翻译' } });
        expect(generateButton.disabled).toBe(false);
        fireEvent.click(generateButton);

        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalledWith('论文翻译'));
        await waitFor(() => expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('翻译专家'));
        expect((screen.getByTestId('expert-icon-input') as HTMLInputElement).value).toBe('🌐');
        expect((screen.getByTestId('expert-desc-input') as HTMLInputElement).value).toBe('中英互译');
        expect((screen.getByTestId('expert-prompt-input') as HTMLTextAreaElement).value).toBe('你是翻译专家');
        // Suggested tools/skills land as checked entries in the pickers.
        const toolsList = screen.getByTestId('expert-tools-list');
        expect((toolsList.querySelector('input') as HTMLInputElement).checked).toBe(true);
    });

    it('shows a retryable error when generation fails', async () => {
        spies.GenerateExpertProfile.mockRejectedValue(new Error('LLM offline'));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: 'x' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect(screen.getByText(/生成失败/)).toBeTruthy());
        // Button re-enabled for retry.
        expect((screen.getByTestId('expert-generate-button') as HTMLButtonElement).disabled).toBe(false);
    });

    it('saves a new expert without id and reports via onSaved', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => JSON.stringify({ ...JSON.parse(json), id: 'new-id-1' }));
        const onSaved = vi.fn();
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={onSaved} />);

        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '新专家' } });
        fireEvent.change(screen.getByTestId('expert-desc-input'), { target: { value: '描述' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));

        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.id).toBeUndefined();
        expect(payload.name).toBe('新专家');
        expect(payload.tools).toEqual([]);
        expect(payload.skills).toEqual([]);
        await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
        expect(onSaved.mock.calls[0][0].id).toBe('new-id-1');
    });

    it('requires a name before saving', async () => {
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.click(screen.getByTestId('expert-save-button'));
        expect(await screen.findByText('请填写名称')).toBeTruthy();
        expect(spies.SaveExpert).not.toHaveBeenCalled();
    });

    it('edit mode prefills the form and saves with the existing id', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        const expert = {
            id: 'builtin-paper-polish',
            name: '论文润色',
            description: '润色',
            icon: '📝',
            system_prompt: '原提示词',
            tools: ['fs_read'],
            skills: [],
            builtin: true,
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
        };
        render(<ExpertEditorDialog lang="zh-Hans" expert={expert} onClose={vi.fn()} onSaved={vi.fn()} />);
        expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('论文润色');
        fireEvent.change(screen.getByTestId('expert-prompt-input'), { target: { value: '改后的提示词' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.id).toBe('builtin-paper-polish');
        expect(payload.system_prompt).toBe('改后的提示词');
        expect(payload.tools).toEqual(['fs_read']);
    });

    it('intersects AI suggestions with available tools/skills and shows ignored ones as chips', async () => {
        spies.GenerateExpertProfile.mockResolvedValue(JSON.stringify({
            name: '翻译专家',
            description: '中英互译',
            icon: '🌐',
            system_prompt: '你是翻译专家',
            suggested_tools: ['fs_read', 'ghost_tool'],
            suggested_skills: ['pptx-gen', 'ghost_skill'],
        }));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        // Wait until the available tool list is loaded so the intersection applies.
        await waitFor(() => expect(screen.getByTestId('expert-tools-list').textContent).toContain('fs_read'));

        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: '论文翻译' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('翻译专家'));

        const ignored = await screen.findByTestId('expert-ignored-suggestions');
        expect(ignored.textContent).toContain('2 项 AI 建议未匹配到可用工具/技能，已忽略');
        expect(ignored.textContent).toContain('ghost_tool');
        expect(ignored.textContent).toContain('ghost_skill');

        // Only matched suggestions are checked.
        const toolBoxes = screen.getByTestId('expert-tools-list').querySelectorAll('input[type="checkbox"]');
        const checked = Array.from(toolBoxes).filter((b) => (b as HTMLInputElement).checked);
        expect(checked.length).toBe(1);

        // Save never persists unmatched names.
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(['fs_read']);
        expect(payload.skills).toEqual(['pptx-gen']);
    });

    it('marks deferred tools with an on-demand tag', async () => {
        spies.ListAvailableToolNames.mockResolvedValue(JSON.stringify([
            { name: 'fs_read', description: 'Read files' },
            { name: 'deep_search', description: 'Search', deferred: true },
        ]));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        await waitFor(() => expect(screen.getByTestId('expert-tools-list').textContent).toContain('deep_search'));
        expect(screen.getByTestId('expert-tools-list').textContent).toContain('（按需发现）');
    });

    it('closes on Escape (but not while generating)', async () => {
        const onClose = vi.fn();
        render(<ExpertEditorDialog lang="zh-Hans" onClose={onClose} onSaved={vi.fn()} />);
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).toHaveBeenCalledTimes(1);

        // While generating, Esc is suppressed so the in-flight result isn't lost.
        onClose.mockClear();
        let resolveGenerate: ((v: string) => void) | undefined;
        spies.GenerateExpertProfile.mockImplementation(() => new Promise<string>((resolve) => { resolveGenerate = resolve; }));
        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: 'x' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalled());
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();
        resolveGenerate?.('{}');
    });
});
