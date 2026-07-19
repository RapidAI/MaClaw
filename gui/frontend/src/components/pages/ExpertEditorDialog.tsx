import { useEffect, useMemo, useState } from 'react';
import type { ExpertDefinition, GeneratedExpertProfile } from '../ai/expertTypes';

async function getApp(): Promise<any | null> {
    try {
        return await import('../../../wailsjs/go/main/App');
    } catch {
        return null;
    }
}

type ToolNameEntry = { name: string; description?: string; deferred?: boolean };

type ExpertEditorDialogProps = {
    lang?: string;
    /** null/undefined = create a new expert; otherwise edit (saving a builtin stores a user override). */
    expert?: ExpertDefinition | null;
    onClose: () => void;
    onSaved: (saved: ExpertDefinition) => void;
};

function parseToolNames(raw: string | null | undefined): ToolNameEntry[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter((item) => item && typeof item === 'object' && typeof item.name === 'string' && item.name)
            .map((item) => ({
                name: String(item.name),
                description: typeof item.description === 'string' ? item.description : '',
                deferred: item.deferred === true,
            }));
    } catch {
        return [];
    }
}

function parseSkillNames(raw: unknown): string[] {
    if (!Array.isArray(raw)) return [];
    return raw
        .map((item) => String(item?.name || '').trim())
        .filter(Boolean);
}

function toggleInList(list: string[], value: string): string[] {
    return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

/**
 * Expert create/edit dialog.
 * - Create mode offers a one-line idea input + "AI generate" (GenerateExpertProfile)
 *   whose result pre-fills the fully editable form.
 * - Tool/skill pickers are allow-lists; leaving everything unchecked means "no restriction".
 */
export const ExpertEditorDialog = ({ lang, expert, onClose, onSaved }: ExpertEditorDialogProps) => {
    const isZh = !lang || lang.startsWith('zh');
    const t = useMemo(() => ({
        titleNew: lang === 'zh-Hant' ? '新建專家' : isZh ? '新建专家' : 'New expert',
        titleEdit: lang === 'zh-Hant' ? '編輯專家' : isZh ? '编辑专家' : 'Edit expert',
        ideaLabel: lang === 'zh-Hant' ? '一句話描述你想要的專家' : isZh ? '一句话描述你想要的专家' : 'Describe the expert you want in one sentence',
        ideaPlaceholder: lang === 'zh-Hant'
            ? '例如：幫我把中文論文翻譯成地道英文'
            : isZh
                ? '例如：帮我把中文论文翻译成地道英文'
                : 'e.g. Translate my Chinese papers into idiomatic English',
        generate: lang === 'zh-Hant' ? 'AI 生成' : isZh ? 'AI 生成' : 'Generate with AI',
        generating: lang === 'zh-Hant' ? '生成中…' : isZh ? '生成中…' : 'Generating…',
        generateFailed: lang === 'zh-Hant' ? '生成失败，请重试' : isZh ? '生成失败，请重试' : 'Generation failed — please retry',
        nameLabel: lang === 'zh-Hant' ? '名稱' : isZh ? '名称' : 'Name',
        iconLabel: lang === 'zh-Hant' ? '圖標（emoji）' : isZh ? '图标（emoji）' : 'Icon (emoji)',
        descLabel: lang === 'zh-Hant' ? '描述' : isZh ? '描述' : 'Description',
        promptLabel: lang === 'zh-Hant' ? '系統提示詞' : isZh ? '系统提示词' : 'System prompt',
        toolsLabel: lang === 'zh-Hant' ? '可用工具' : isZh ? '可用工具' : 'Allowed tools',
        toolsHint: lang === 'zh-Hant' ? '全部不勾選 = 不限制可用工具' : isZh ? '全部不勾选 = 不限制可用工具' : 'Leave all unchecked for no tool restriction',
        skillsLabel: lang === 'zh-Hant' ? '可用技能' : isZh ? '可用技能' : 'Allowed skills',
        skillsHint: lang === 'zh-Hant' ? '全部不勾選 = 不限制可用技能' : isZh ? '全部不勾选 = 不限制可用技能' : 'Leave all unchecked for no skill restriction',
        save: lang === 'zh-Hant' ? '保存' : isZh ? '保存' : 'Save',
        saving: lang === 'zh-Hant' ? '保存中…' : isZh ? '保存中…' : 'Saving…',
        cancel: lang === 'zh-Hant' ? '取消' : isZh ? '取消' : 'Cancel',
        nameRequired: lang === 'zh-Hant' ? '請填寫名稱' : isZh ? '请填写名称' : 'Name is required',
        deferredTag: lang === 'zh-Hant' ? '（按需發現）' : isZh ? '（按需发现）' : ' (on-demand)',
        ignoredSuggestions: (n: number) => lang === 'zh-Hant'
            ? `${n} 項 AI 建議未匹配到可用工具/技能，已忽略`
            : isZh
                ? `${n} 项 AI 建议未匹配到可用工具/技能，已忽略`
                : `${n} AI suggestion(s) did not match available tools/skills and were ignored`,
    }), [isZh, lang]);

    const editing = !!expert?.id;
    const [idea, setIdea] = useState('');
    const [generating, setGenerating] = useState(false);
    const [generateError, setGenerateError] = useState('');
    const [name, setName] = useState(expert?.name || '');
    const [icon, setIcon] = useState(expert?.icon || '');
    const [description, setDescription] = useState(expert?.description || '');
    const [systemPrompt, setSystemPrompt] = useState(expert?.system_prompt || '');
    const [tools, setTools] = useState<string[]>(expert?.tools || []);
    const [skills, setSkills] = useState<string[]>(expert?.skills || []);
    const [availableTools, setAvailableTools] = useState<ToolNameEntry[]>([]);
    const [availableSkills, setAvailableSkills] = useState<string[]>([]);
    /** AI suggestions dropped because they don't exist in the available tool/skill lists. */
    const [ignoredSuggestions, setIgnoredSuggestions] = useState<string[]>([]);
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');

    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const mod = await getApp();
                if (!mod || cancelled) return;
                if (mod.ListAvailableToolNames) {
                    const raw = await mod.ListAvailableToolNames().catch(() => '');
                    if (!cancelled) setAvailableTools(parseToolNames(raw));
                }
                if (mod.ListNLSkills) {
                    const rawSkills = await mod.ListNLSkills().catch(() => []);
                    if (!cancelled) setAvailableSkills(parseSkillNames(rawSkills));
                }
            } catch {
                // Tool/skill lists stay empty — the form still works with manual entries.
            }
        })();
        return () => { cancelled = true; };
    }, []);

    // Esc closes the dialog (suppressed while AI generation is in flight).
    useEffect(() => {
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && !generating) onClose();
        };
        window.addEventListener('keydown', onKeyDown);
        return () => window.removeEventListener('keydown', onKeyDown);
    }, [generating, onClose]);

    /** Intersect AI suggestions with the available lists; when a list failed to
     * load (empty), keep suggestions as-is rather than dropping everything. */
    const reconcileSuggestions = (suggestedTools: string[], suggestedSkills: string[]) => {
        const ignored: string[] = [];
        let nextTools = suggestedTools;
        let nextSkills = suggestedSkills;
        if (availableTools.length > 0) {
            const known = new Set(availableTools.map((t) => t.name));
            nextTools = suggestedTools.filter((t) => known.has(t));
            ignored.push(...suggestedTools.filter((t) => !known.has(t)));
        }
        if (availableSkills.length > 0) {
            const known = new Set(availableSkills);
            nextSkills = suggestedSkills.filter((s) => known.has(s));
            ignored.push(...suggestedSkills.filter((s) => !known.has(s)));
        }
        setIgnoredSuggestions(ignored);
        return { nextTools, nextSkills };
    };

    const handleGenerate = async () => {
        const text = idea.trim();
        if (!text || generating) return;
        setGenerating(true);
        setGenerateError('');
        try {
            const mod = await getApp();
            if (!mod?.GenerateExpertProfile) throw new Error('GenerateExpertProfile unavailable');
            const raw = await mod.GenerateExpertProfile(text);
            const profile = JSON.parse(raw || '{}') as GeneratedExpertProfile;
            if (profile.name) setName(String(profile.name));
            if (profile.icon) setIcon(String(profile.icon));
            if (profile.description) setDescription(String(profile.description));
            if (profile.system_prompt) setSystemPrompt(String(profile.system_prompt));
            const suggestedTools = Array.isArray(profile.suggested_tools) ? profile.suggested_tools.map(String).filter(Boolean) : [];
            const suggestedSkills = Array.isArray(profile.suggested_skills) ? profile.suggested_skills.map(String).filter(Boolean) : [];
            const { nextTools, nextSkills } = reconcileSuggestions(suggestedTools, suggestedSkills);
            setTools(nextTools);
            setSkills(nextSkills);
        } catch (e: any) {
            setGenerateError(e?.message ? `${t.generateFailed}: ${e.message}` : t.generateFailed);
        } finally {
            setGenerating(false);
        }
    };

    const handleSave = async () => {
        if (!name.trim()) {
            setSaveError(t.nameRequired);
            return;
        }
        if (saving) return;
        setSaving(true);
        setSaveError('');
        try {
            const mod = await getApp();
            if (!mod?.SaveExpert) throw new Error('SaveExpert unavailable');
            // Final guard: never persist names the backend doesn't know about.
            const knownTools = availableTools.length > 0 ? new Set(availableTools.map((t) => t.name)) : null;
            const knownSkills = availableSkills.length > 0 ? new Set(availableSkills) : null;
            const payload: Record<string, unknown> = {
                name: name.trim(),
                icon: icon.trim(),
                description: description.trim(),
                system_prompt: systemPrompt,
                tools: knownTools ? tools.filter((t) => knownTools.has(t)) : tools,
                skills: knownSkills ? skills.filter((s) => knownSkills.has(s)) : skills,
            };
            if (editing && expert?.id) payload.id = expert.id;
            const raw = await mod.SaveExpert(JSON.stringify(payload));
            let saved: ExpertDefinition;
            try {
                saved = JSON.parse(raw || '{}') as ExpertDefinition;
            } catch {
                saved = { ...(expert || {}), ...payload, id: (payload.id as string) || '' } as ExpertDefinition;
            }
            onSaved(saved);
        } catch (e: any) {
            setSaveError(e?.message || String(e));
            setSaving(false);
        }
    };

    return (
        <div className="expert-editor-overlay" data-testid="expert-editor-overlay">
            <div className="expert-editor" role="dialog" aria-modal="true" aria-label={editing ? t.titleEdit : t.titleNew}>
                <h3 className="expert-editor__title">{editing ? t.titleEdit : t.titleNew}</h3>

                {!editing && (
                    <div className="expert-editor__idea">
                        <label className="expert-editor__label" htmlFor="expert-idea-input">{t.ideaLabel}</label>
                        <div className="expert-editor__idea-row">
                            <input
                                id="expert-idea-input"
                                data-testid="expert-idea-input"
                                className="expert-editor__input"
                                type="text"
                                value={idea}
                                placeholder={t.ideaPlaceholder}
                                onChange={(e) => setIdea(e.target.value)}
                                disabled={generating}
                            />
                            <button
                                type="button"
                                data-testid="expert-generate-button"
                                className="expert-editor__button expert-editor__button--primary"
                                onClick={() => { void handleGenerate(); }}
                                disabled={generating || !idea.trim()}
                                aria-busy={generating || undefined}
                            >
                                {generating ? t.generating : t.generate}
                            </button>
                        </div>
                        {generateError ? <p className="expert-editor__error" role="alert">{generateError}</p> : null}
                    </div>
                )}

                {ignoredSuggestions.length > 0 && (
                    <div className="expert-editor__ignored" data-testid="expert-ignored-suggestions">
                        <p className="expert-editor__hint">{t.ignoredSuggestions(ignoredSuggestions.length)}</p>
                        <div className="expert-editor__ignored-chips">
                            {ignoredSuggestions.map((item) => (
                                <span key={item} className="expert-editor__chip" aria-readonly="true">{item}</span>
                            ))}
                        </div>
                    </div>
                )}

                <div className="expert-editor__grid">
                    <div className="expert-editor__field expert-editor__field--name">
                        <label className="expert-editor__label" htmlFor="expert-name-input">{t.nameLabel}</label>
                        <input
                            id="expert-name-input"
                            data-testid="expert-name-input"
                            className="expert-editor__input"
                            type="text"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                        />
                    </div>
                    <div className="expert-editor__field expert-editor__field--icon">
                        <label className="expert-editor__label" htmlFor="expert-icon-input">{t.iconLabel}</label>
                        <input
                            id="expert-icon-input"
                            data-testid="expert-icon-input"
                            className="expert-editor__input"
                            type="text"
                            value={icon}
                            onChange={(e) => setIcon(e.target.value)}
                        />
                    </div>
                </div>

                <div className="expert-editor__field">
                    <label className="expert-editor__label" htmlFor="expert-desc-input">{t.descLabel}</label>
                    <input
                        id="expert-desc-input"
                        data-testid="expert-desc-input"
                        className="expert-editor__input"
                        type="text"
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
                    />
                </div>

                <div className="expert-editor__field">
                    <label className="expert-editor__label" htmlFor="expert-prompt-input">{t.promptLabel}</label>
                    <textarea
                        id="expert-prompt-input"
                        data-testid="expert-prompt-input"
                        className="expert-editor__textarea"
                        rows={8}
                        value={systemPrompt}
                        onChange={(e) => setSystemPrompt(e.target.value)}
                    />
                </div>

                <div className="expert-editor__field">
                    <div className="expert-editor__label">{t.toolsLabel}</div>
                    <p className="expert-editor__hint">{t.toolsHint}</p>
                    <div className="expert-editor__checks" data-testid="expert-tools-list">
                        {availableTools.map((tool) => (
                            <label key={tool.name} className="expert-editor__check" title={tool.description || tool.name}>
                                <input
                                    type="checkbox"
                                    checked={tools.includes(tool.name)}
                                    onChange={() => setTools((prev) => toggleInList(prev, tool.name))}
                                />
                                <span>{tool.name}{tool.deferred ? <span className="expert-editor__deferred">{t.deferredTag}</span> : null}</span>
                            </label>
                        ))}
                    </div>
                </div>

                <div className="expert-editor__field">
                    <div className="expert-editor__label">{t.skillsLabel}</div>
                    <p className="expert-editor__hint">{t.skillsHint}</p>
                    <div className="expert-editor__checks" data-testid="expert-skills-list">
                        {availableSkills.map((skill) => (
                            <label key={skill} className="expert-editor__check">
                                <input
                                    type="checkbox"
                                    checked={skills.includes(skill)}
                                    onChange={() => setSkills((prev) => toggleInList(prev, skill))}
                                />
                                <span>{skill}</span>
                            </label>
                        ))}
                    </div>
                </div>

                {saveError ? <p className="expert-editor__error" role="alert">{saveError}</p> : null}

                <div className="expert-editor__actions">
                    <button
                        type="button"
                        className="expert-editor__button expert-editor__button--secondary"
                        onClick={onClose}
                        disabled={saving}
                    >
                        {t.cancel}
                    </button>
                    <button
                        type="button"
                        data-testid="expert-save-button"
                        className="expert-editor__button expert-editor__button--primary"
                        onClick={() => { void handleSave(); }}
                        disabled={saving}
                        aria-busy={saving || undefined}
                    >
                        {saving ? t.saving : t.save}
                    </button>
                </div>
            </div>
        </div>
    );
};
