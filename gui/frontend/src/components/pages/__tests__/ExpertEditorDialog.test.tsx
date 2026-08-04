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
            { name: 'memory', description: 'Memory' },
            { name: 'ask_user', description: 'Ask user' },
            { name: 'fs_read', description: 'Read files' },
            { name: 'fs_write', description: 'Write files' },
            { name: 'web_search', description: 'Search web' },
            { name: 'ssh', description: 'SSH' },
            { name: 'screenshot', description: 'Screenshot' },
            { name: 'send_file', description: 'Send file' },
        ])),
        ListNLSkills: vi.fn().mockResolvedValue([{ name: 'pptx-gen' }, { name: 'pdf-word' }, { name: 'sheet-analysis' }]),
    };
    (window as any).go = { main: { App: spies } };
    return spies;
}

async function expandAdvanced() {
    const toggle = screen.getByTestId('expert-advanced-toggle');
    if (toggle.getAttribute('aria-expanded') !== 'true') {
        fireEvent.click(toggle);
    }
    await waitFor(() => expect(screen.getByTestId('expert-advanced-body')).toBeTruthy());
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

        const ideaInput = screen.getByTestId('expert-idea-input') as HTMLTextAreaElement;
        expect(ideaInput.tagName).toBe('TEXTAREA');
        expect(ideaInput.getAttribute('rows')).toBe('4');

        fireEvent.change(ideaInput, { target: { value: '论文翻译' } });
        expect(generateButton.disabled).toBe(false);
        fireEvent.keyDown(ideaInput, { key: 'Enter', ctrlKey: true });

        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalledWith('论文翻译'));
        await waitFor(() => expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('翻译专家'));
        expect((screen.getByTestId('expert-icon-input') as HTMLInputElement).value).toBe('🌐');
        expect((screen.getByTestId('expert-desc-input') as HTMLTextAreaElement).value).toBe('中英互译');
        expect(screen.getByTestId('expert-desc-input').tagName).toBe('TEXTAREA');
        expect(screen.getByTestId('expert-desc-input').getAttribute('rows')).toBe('3');
        expect(screen.getByText('Ctrl / ⌘ + Enter 生成')).toBeTruthy();
        expect((screen.getByTestId('expert-prompt-input') as HTMLTextAreaElement).value).toBe('你是翻译专家');

        // Suggestions show as summary chips (friendly labels); whitelist is NOT auto-applied.
        const chips = await screen.findByTestId('expert-suggestion-chips');
        expect(chips.textContent).toMatch(/读取文件|Read files|fs_read/);
        expect(chips.textContent).toMatch(/PPT 生成|pptx-gen/);
        expect(chips.querySelector('[title="fs_read"]')).toBeTruthy();
        expect((screen.getByTestId('expert-adopt-suggestions').querySelector('input') as HTMLInputElement).checked).toBe(false);

        // Advanced is collapsed by default; tools list is not mounted.
        expect(screen.queryByTestId('expert-tools-list')).toBeNull();
        expect(screen.getByTestId('expert-default-access').textContent).toContain('默认不限制');

        // Save without adopting keeps unrestricted empty allow-lists.
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual([]);
        expect(payload.skills).toEqual([]);
    });

    it('keeps Ctrl+Enter available to an active IME composition', async () => {
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        const ideaInput = screen.getByTestId('expert-idea-input');
        fireEvent.change(ideaInput, { target: { value: '论文翻译' } });
        fireEvent.keyDown(ideaInput, { key: 'Enter', ctrlKey: true, isComposing: true });
        expect(spies.GenerateExpertProfile).not.toHaveBeenCalled();
    });

    it('starts only one AI generation when shortcut and click arrive in the same render', async () => {
        let resolveGenerate: ((value: string) => void) | undefined;
        spies.GenerateExpertProfile.mockImplementation(() => new Promise<string>((resolve) => { resolveGenerate = resolve; }));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        const ideaInput = screen.getByTestId('expert-idea-input');
        fireEvent.change(ideaInput, { target: { value: '论文翻译' } });
        fireEvent.keyDown(ideaInput, { key: 'Enter', ctrlKey: true });
        fireEvent.click(screen.getByTestId('expert-generate-button'));

        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalledTimes(1));
        resolveGenerate?.('{}');
        await waitFor(() => expect((screen.getByTestId('expert-generate-button') as HTMLButtonElement).disabled).toBe(false));
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
        fireEvent.change(screen.getByTestId('expert-desc-input'), { target: { value: '描述\n第二行' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));

        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.id).toBeUndefined();
        expect(payload.name).toBe('新专家');
        expect(payload.description).toBe('描述\n第二行');
        expect(payload.tools).toEqual([]);
        expect(payload.skills).toEqual([]);
        await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
        expect(onSaved.mock.calls[0][0].id).toBe('new-id-1');
    });

    it('starts only one save when duplicate clicks arrive in the same render', async () => {
        let resolveSave: ((value: string) => void) | undefined;
        spies.SaveExpert.mockImplementation(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '新专家' } });
        const saveButton = screen.getByTestId('expert-save-button');
        fireEvent.click(saveButton);
        fireEvent.click(saveButton);

        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        resolveSave?.('{}');
        await waitFor(() => expect((screen.getByTestId('expert-save-button') as HTMLButtonElement).disabled).toBe(false));
    });

    it('keeps meaningful line breaks while trimming only surrounding description whitespace', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);

        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '研究助手' } });
        fireEvent.change(screen.getByTestId('expert-desc-input'), { target: { value: '  归纳资料\n输出重点  \n' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));

        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.description).toBe('归纳资料\n输出重点');
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
        // Existing restrictions open advanced so the user can see them.
        await waitFor(() => expect(screen.getByTestId('expert-advanced-body')).toBeTruthy());
        fireEvent.change(screen.getByTestId('expert-prompt-input'), { target: { value: '改后的提示词' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.id).toBe('builtin-paper-polish');
        expect(payload.system_prompt).toBe('改后的提示词');
        expect(payload.tools).toEqual(['fs_read']);
    });

    it('does not auto-apply AI suggestions as whitelist; adopt toggle applies them', async () => {
        spies.GenerateExpertProfile.mockResolvedValue(JSON.stringify({
            name: '翻译专家',
            description: '中英互译',
            icon: '🌐',
            system_prompt: '你是翻译专家',
            suggested_tools: ['fs_read', 'ghost_tool'],
            suggested_skills: ['pptx-gen', 'ghost_skill'],
        }));
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);

        // Wait until catalogs load so intersection can drop ghosts.
        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());
        await waitFor(() => expect(spies.ListNLSkills).toHaveBeenCalled());

        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: '论文翻译' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('翻译专家'));

        const ignored = await screen.findByTestId('expert-ignored-suggestions');
        expect(ignored.textContent).toContain('2 项 AI 建议未匹配到可用工具/技能，已忽略');
        expect(ignored.textContent).toContain('ghost_tool');
        expect(ignored.textContent).toContain('ghost_skill');

        // Without adopt, default access stays unrestricted.
        expect(screen.getByTestId('expert-default-access').textContent).toContain('默认不限制');
        expect((screen.getByTestId('expert-adopt-suggestions').querySelector('input') as HTMLInputElement).checked).toBe(false);

        // Adopt as whitelist → advanced opens with matched suggestions only.
        const adopt = screen.getByTestId('expert-adopt-suggestions').querySelector('input') as HTMLInputElement;
        fireEvent.click(adopt);
        expect(adopt.checked).toBe(true);
        await waitFor(() => expect(screen.getByTestId('expert-tools-list')).toBeTruthy());
        const toolBoxes = screen.getByTestId('expert-tools-list').querySelectorAll('input[type="checkbox"]');
        const checked = Array.from(toolBoxes).filter((b) => (b as HTMLInputElement).checked);
        expect(checked.length).toBe(1);
        expect(screen.getByTestId('expert-default-access').textContent).toMatch(/已限制/);

        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(['fs_read']);
        expect(payload.skills).toEqual(['pptx-gen']);
    });

    it('marks deferred tools with an on-demand tag when advanced is expanded', async () => {
        spies.ListAvailableToolNames.mockResolvedValue(JSON.stringify([
            { name: 'fs_read', description: 'Read files' },
            { name: 'deep_search', description: 'Search', deferred: true },
        ]));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        await expandAdvanced();
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
        // Flush generate completion so React does not warn about unwrapped act updates.
        await waitFor(() => expect((screen.getByTestId('expert-generate-button') as HTMLButtonElement).disabled).toBe(false));
    });

    it('ignores a late AI generation result after the editor unmounts', async () => {
        let resolveGenerate: ((value: string) => void) | undefined;
        spies.GenerateExpertProfile.mockImplementation(() => new Promise<string>((resolve) => { resolveGenerate = resolve; }));
        const { unmount } = render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: '论文翻译' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalledTimes(1));

        unmount();
        resolveGenerate?.(JSON.stringify({ name: '不应写入已关闭的编辑器' }));
        await Promise.resolve();
    });

    it('keeps manual edits when an earlier AI generation finishes late', async () => {
        let resolveGenerate: ((value: string) => void) | undefined;
        spies.GenerateExpertProfile.mockImplementation(() => new Promise<string>((resolve) => { resolveGenerate = resolve; }));
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.change(screen.getByTestId('expert-idea-input'), { target: { value: '论文翻译' } });
        fireEvent.click(screen.getByTestId('expert-generate-button'));
        await waitFor(() => expect(spies.GenerateExpertProfile).toHaveBeenCalledTimes(1));

        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '手动专家' } });
        resolveGenerate?.(JSON.stringify({ name: '迟到的 AI 专家', system_prompt: '不应覆盖' }));

        await waitFor(() => expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('手动专家'));
        expect((screen.getByTestId('expert-prompt-input') as HTMLTextAreaElement).value).toBe('');
        expect((screen.getByTestId('expert-generate-button') as HTMLButtonElement).disabled).toBe(false);
    });

    it('ignores a late save completion after the editor unmounts', async () => {
        let resolveSave: ((value: string) => void) | undefined;
        const onSaved = vi.fn();
        spies.SaveExpert.mockImplementation(() => new Promise<string>((resolve) => { resolveSave = resolve; }));
        const { unmount } = render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={onSaved} />);
        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '新专家' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));

        unmount();
        resolveSave?.(JSON.stringify({ id: 'late-expert', name: '新专家' }));
        await Promise.resolve();
        expect(onSaved).not.toHaveBeenCalled();
    });

    it('defaults to full capability tier and applies docs tier allow-list on save', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);

        expect(screen.getByTestId('expert-tier-full').getAttribute('aria-checked')).toBe('true');
        expect(screen.getByTestId('expert-default-access').textContent).toContain('默认不限制');
        expect(screen.queryByTestId('expert-access-card')).toBeNull();

        // Wait for catalog so docs resolution intersects live tools.
        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());
        fireEvent.click(screen.getByTestId('expert-tier-docs'));
        expect(screen.getByTestId('expert-tier-docs').getAttribute('aria-checked')).toBe('true');
        await waitFor(() => expect(screen.getByTestId('expert-default-access').textContent).toMatch(/已限制/));
        // Boundary card shows current tier without high-risk items for docs.
        expect(screen.getByTestId('expert-access-card-tier').textContent).toContain('文档助手');
        expect(screen.getByTestId('expert-access-card-no-danger')).toBeTruthy();

        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '文档专家' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(expect.arrayContaining(['fs_read', 'fs_write', 'web_search', 'memory']));
        expect(payload.tools).not.toContain('ssh');
        expect(payload.skills).toEqual(expect.arrayContaining(['pdf-word']));
        expect(payload.skills).not.toContain('pptx-gen');
    });

    it('uses backend ResolveExpertCapabilityTier when available', async () => {
        (spies as any).ResolveExpertCapabilityTier = vi.fn().mockResolvedValue(JSON.stringify({
            tier: 'advisor',
            tools: ['memory', 'ask_user'],
            skills: [],
        }));
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());

        fireEvent.click(screen.getByTestId('expert-tier-advisor'));
        await waitFor(() => expect((spies as any).ResolveExpertCapabilityTier).toHaveBeenCalledWith('advisor'));
        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '顾问' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(['memory', 'ask_user']);
        expect(payload.skills).toEqual([]);
    });

    it('ignores stale backend tier responses after a later tier click', async () => {
        let resolveAdvisor: ((v: string) => void) | undefined;
        (spies as any).ResolveExpertCapabilityTier = vi.fn().mockImplementation((tier: string) => {
            if (tier === 'advisor') {
                return new Promise<string>((resolve) => { resolveAdvisor = resolve; });
            }
            return Promise.resolve(JSON.stringify({
                tier: 'docs',
                tools: ['fs_read', 'web_search'],
                skills: ['pdf-word'],
            }));
        });
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());

        fireEvent.click(screen.getByTestId('expert-tier-advisor'));
        await waitFor(() => expect((spies as any).ResolveExpertCapabilityTier).toHaveBeenCalledWith('advisor'));
        fireEvent.click(screen.getByTestId('expert-tier-docs'));
        await waitFor(() => expect((spies as any).ResolveExpertCapabilityTier).toHaveBeenCalledWith('docs'));

        // Late advisor response must not clobber docs.
        resolveAdvisor?.(JSON.stringify({ tier: 'advisor', tools: ['memory'], skills: [] }));
        await waitFor(() => expect(screen.getByTestId('expert-tier-docs').getAttribute('aria-checked')).toBe('true'));

        fireEvent.change(screen.getByTestId('expert-name-input'), { target: { value: '文档' } });
        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(expect.arrayContaining(['fs_read', 'web_search']));
        expect(payload.tools).not.toEqual(['memory']);
    });

    it('starter template fills seed fields and selects a capability tier', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);

        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());
        // Ensure catalog state is applied before starter resolve.
        await waitFor(() => expect(screen.getByTestId('expert-tier-full')).toBeTruthy());
        fireEvent.click(screen.getByTestId('expert-starter-office'));

        expect((screen.getByTestId('expert-name-input') as HTMLInputElement).value).toBe('办公助手');
        expect((screen.getByTestId('expert-icon-input') as HTMLInputElement).value).toBe('📊');
        expect((screen.getByTestId('expert-idea-input') as HTMLTextAreaElement).value).toContain('办公');
        expect(screen.getByTestId('expert-tier-office').getAttribute('aria-checked')).toBe('true');
        await waitFor(() => expect(screen.getByTestId('expert-default-access').textContent).toMatch(/已限制/));

        fireEvent.click(screen.getByTestId('expert-save-button'));
        await waitFor(() => expect(spies.SaveExpert).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(spies.SaveExpert.mock.calls[0][0] as string);
        expect(payload.tools).toEqual(expect.arrayContaining(['screenshot', 'send_file']));
        expect(payload.skills).toEqual(expect.arrayContaining(['pptx-gen', 'sheet-analysis']));
    });

    it('custom tier expands advanced whitelist', async () => {
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        fireEvent.click(screen.getByTestId('expert-tier-custom'));
        expect(screen.getByTestId('expert-tier-custom').getAttribute('aria-checked')).toBe('true');
        await waitFor(() => expect(screen.getByTestId('expert-advanced-body')).toBeTruthy());
    });

    it('groups advanced tools with Chinese labels and danger markers', async () => {
        spies.SaveExpert.mockImplementation(async (json: string) => json);
        render(<ExpertEditorDialog lang="zh-Hans" onClose={vi.fn()} onSaved={vi.fn()} />);
        await waitFor(() => expect(spies.ListAvailableToolNames).toHaveBeenCalled());
        await expandAdvanced();

        expect(screen.getByTestId('expert-tool-group-system')).toBeTruthy();
        expect(screen.getByTestId('expert-tool-group-files')).toBeTruthy();
        expect(screen.getByTestId('expert-tools-list').textContent).toContain('读取文件');
        expect(screen.getByTestId('expert-tools-list').textContent).toContain('SSH 远程');
        expect(screen.getByTestId('expert-tools-list').textContent).toContain('高风险');

        // Selecting a dangerous tool surfaces the risk note + summary danger count.
        const sshLabel = Array.from(screen.getByTestId('expert-tools-list').querySelectorAll('label'))
            .find((el) => el.textContent?.includes('SSH 远程')) as HTMLLabelElement;
        expect(sshLabel).toBeTruthy();
        fireEvent.click(sshLabel.querySelector('input') as HTMLInputElement);
        expect(await screen.findByTestId('expert-danger-note')).toBeTruthy();
        expect(screen.getByTestId('expert-tier-custom').getAttribute('aria-checked')).toBe('true');
        expect(screen.getByTestId('expert-default-access').textContent).toMatch(/高风险/);
        expect(screen.getByTestId('expert-access-card-danger-list').textContent).toContain('SSH 远程');
    });
});
