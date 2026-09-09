import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ExpertCapabilityRule, ExpertDefinition, ExpertOptimizeDraft, GeneratedExpertProfile } from '../ai/expertTypes';
import { buildPromptLineDiff, changedNames, omitOptimizationReviewFields, summarizePromptDiff } from '../ai/expertOptimizationDiff';
import './UtilitiesPage.css';
import {
    CAPABILITY_TIER_ORDER,
    EXPERT_STARTER_TEMPLATES,
    countDangerousSelections,
    type CapabilityTierId,
    type ExpertStarterTemplateId,
    inferCapabilityTier,
    resolveCapabilityTier,
} from '../ai/expertCapabilityTiers';
import {
    groupSkills,
    groupTools,
    riskLabel,
    skillCategoryLabel,
    skillDisplayLabel,
    skillRisk,
    toolCategoryLabel,
    toolDisplayLabel,
    toolRisk,
    type ToolCatalogEntry,
    type ToolRisk,
} from '../ai/expertToolMeta';

async function getApp(): Promise<any | null> {
    try {
        return await import('../../../wailsjs/go/main/App');
    } catch {
        return null;
    }
}

function intersectKnown(names: string[], known: Set<string> | null): string[] {
    if (!known) return names;
    return names.filter((n) => known.has(n));
}

/** Order-sensitive equality — tier resolve preserves catalog order. */
function sameStringList(a: string[], b: string[]): boolean {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i += 1) {
        if (a[i] !== b[i]) return false;
    }
    return true;
}

/** Dedupe while preserving first-seen order (stable save payload). */
function dedupePreserveOrder(names: string[]): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const raw of names) {
        const n = String(raw || '').trim();
        if (!n || seen.has(n)) continue;
        seen.add(n);
        out.push(n);
    }
    return out;
}

type ToolNameEntry = ToolCatalogEntry;

type ExpertEditorDialogProps = {
    lang?: string;
    /** null/undefined = create a new expert; otherwise edit (saving a builtin stores a user override). */
    expert?: ExpertDefinition | null;
    /**
     * "专家优化" draft from OptimizeExpertFromSession. When present the form is
     * prefilled from the draft; `update_existing` + `id` means saving updates
     * the source expert's existing optimized expert (edit-mode semantics).
     */
    optimizeDraft?: ExpertOptimizeDraft | null;
    onClose: () => void;
    onSaved: (saved: ExpertDefinition) => void;
};

/**
 * The only fields that may cross the editor's save boundary. Review-only
 * source/diff data must remain in the dialog and never become expert context.
 */
type ExpertSavePayload = {
    id?: string;
    name: string;
    icon: string;
    description: string;
    system_prompt: string;
    tools: string[];
    skills: string[];
    optimized_from_id: string;
    about: string;
    capability_rules?: ExpertCapabilityRule[];
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
                category: typeof item.category === 'string' ? item.category : undefined,
                risk: typeof item.risk === 'string' ? item.risk : undefined,
                label_zh: typeof item.label_zh === 'string' ? item.label_zh : undefined,
                label_en: typeof item.label_en === 'string' ? item.label_en : undefined,
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

function riskClass(risk: ToolRisk): string {
    if (risk === 'dangerous') return 'expert-editor__risk expert-editor__risk--danger';
    if (risk === 'elevated') return 'expert-editor__risk expert-editor__risk--elevated';
    return 'expert-editor__risk expert-editor__risk--safe';
}

function dialogFocusableElements(dialog: HTMLElement): HTMLElement[] {
    const selector = [
        'a[href]',
        'button:not([disabled])',
        'input:not([disabled])',
        'select:not([disabled])',
        'textarea:not([disabled])',
        '[tabindex]:not([tabindex="-1"])',
    ].join(',');
    return Array.from(dialog.querySelectorAll<HTMLElement>(selector)).filter((element) => (
        !element.hasAttribute('disabled')
        && element.getAttribute('aria-hidden') !== 'true'
        && element.tabIndex >= 0
    ));
}

/**
 * Expert create/edit dialog.
 * - Create mode: optional starter template + multi-line brief + AI generate.
 * - Capability tier is the primary way to set tool/skill access (not raw checkboxes).
 * - Tool/skill pickers live under Advanced; empty = no restriction.
 * - AI suggestions are reference-only unless "Adopt as whitelist" is checked.
 */
export const ExpertEditorDialog = ({ lang, expert, optimizeDraft, onClose, onSaved }: ExpertEditorDialogProps) => {
    const isZh = !lang || lang.startsWith('zh');
    const t = useMemo(() => ({
        titleNew: lang === 'zh-Hant' ? '新建專家' : isZh ? '新建专家' : 'New expert',
        titleEdit: lang === 'zh-Hant' ? '編輯專家' : isZh ? '编辑专家' : 'Edit expert',
        titleOptimizationReview: lang === 'zh-Hant' ? '檢視優化專家' : isZh ? '审核优化专家' : 'Review optimized expert',
        ideaLabel: lang === 'zh-Hant' ? '一句話描述你想要的專家' : isZh ? '一句话描述你想要的专家' : 'Describe the expert you want in one sentence',
        ideaPlaceholder: lang === 'zh-Hant'
            ? '例如：幫我把中文論文翻譯成地道英文'
            : isZh
                ? '例如：帮我把中文论文翻译成地道英文'
                : 'e.g. Translate my Chinese papers into idiomatic English',
        generate: lang === 'zh-Hant' ? 'AI 生成' : isZh ? 'AI 生成' : 'Generate with AI',
        generating: lang === 'zh-Hant' ? '生成中…' : isZh ? '生成中…' : 'Generating…',
        generateShortcut: lang === 'zh-Hant' ? 'Ctrl / ⌘ + Enter 生成' : isZh ? 'Ctrl / ⌘ + Enter 生成' : 'Ctrl / ⌘ + Enter to generate',
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
        discard: lang === 'zh-Hant' ? '放棄修改並關閉' : isZh ? '放弃修改并关闭' : 'Discard changes',
        nameRequired: lang === 'zh-Hant' ? '請填寫名稱' : isZh ? '请填写名称' : 'Name is required',
        nameMustDifferFromSource: (sourceName: string) => lang === 'zh-Hant'
            ? `優化後的專家不能與來源專家「${sourceName}」同名，請換一個名稱`
            : isZh
                ? `优化后的专家不能与来源专家「${sourceName}」同名，请换一个名称`
                : `The optimized expert cannot keep the source name "${sourceName}" — pick a different name`,
        aboutLabel: lang === 'zh-Hant' ? '關於' : isZh ? '关于' : 'About',
        aboutPlaceholder: lang === 'zh-Hant' ? '作者、版權、備註等' : isZh ? '作者、版权、备注等' : 'Author, copyright, notes, etc.',
        deferredTag: lang === 'zh-Hant' ? '（按需發現）' : isZh ? '（按需发现）' : ' (on-demand)',
        ignoredSuggestions: (n: number) => lang === 'zh-Hant'
            ? `${n} 項 AI 建議未匹配到可用工具/技能，已忽略`
            : isZh
                ? `${n} 项 AI 建议未匹配到可用工具/技能，已忽略`
                : `${n} AI suggestion(s) did not match available tools/skills and were ignored`,
        defaultAccess: lang === 'zh-Hant'
            ? '默認不限制工具與技能（與內置專家一致，保證可用）'
            : isZh
                ? '默认不限制工具与技能（与内置专家一致，保证可用）'
                : 'By default tools and skills are unrestricted (same as builtin experts)',
        advancedTitle: lang === 'zh-Hant' ? '高級設置：工具 / 技能白名單' : isZh ? '高级设置：工具 / 技能白名单' : 'Advanced: tool / skill allow-list',
        advancedHint: lang === 'zh-Hant'
            ? '一般無需修改。勾選後將變成白名單限制，漏選會導致專家無法完成任務。'
            : isZh
                ? '一般无需修改。勾选后将变成白名单限制，漏选会导致专家无法完成任务。'
                : 'Usually leave this alone. Checking items turns on a whitelist — missing entries can break the expert.',
        expandAdvanced: lang === 'zh-Hant' ? '展開' : isZh ? '展开' : 'Expand',
        collapseAdvanced: lang === 'zh-Hant' ? '收起' : isZh ? '收起' : 'Collapse',
        suggestedTitle: lang === 'zh-Hant' ? 'AI 建議能力（僅供參考）' : isZh ? 'AI 建议能力（仅供参考）' : 'AI suggested capabilities (reference only)',
        suggestedEmpty: lang === 'zh-Hant'
            ? 'AI 未給出具體工具/技能建議，保存後將不限制能力'
            : isZh
                ? 'AI 未给出具体工具/技能建议，保存后将不限制能力'
                : 'No tool/skill suggestions — saving will leave capabilities unrestricted',
        adoptLabel: lang === 'zh-Hant' ? '採用為白名單限制' : isZh ? '采用为白名单限制' : 'Adopt as whitelist',
        adoptHint: lang === 'zh-Hant'
            ? '默認關閉。開啟後僅允許下方建議的工具/技能，可能因漏選導致專家半殘。'
            : isZh
                ? '默认关闭。开启后仅允许下方建议的工具/技能，可能因漏选导致专家半残。'
                : 'Off by default. When on, only the suggested tools/skills are allowed — incomplete lists can break the expert.',
        toolsChipPrefix: lang === 'zh-Hant' ? '工具' : isZh ? '工具' : 'Tools',
        skillsChipPrefix: lang === 'zh-Hant' ? '技能' : isZh ? '技能' : 'Skills',
        restrictedSummary: (toolCount: number, skillCount: number, dangerCount: number) => {
            const base = lang === 'zh-Hant'
                ? `已限制：${toolCount} 個工具、${skillCount} 個技能`
                : isZh
                    ? `已限制：${toolCount} 个工具、${skillCount} 个技能`
                    : `Restricted: ${toolCount} tool(s), ${skillCount} skill(s)`;
            if (dangerCount <= 0) return base;
            const danger = lang === 'zh-Hant'
                ? `，其中 ${dangerCount} 項高風險`
                : isZh
                    ? `，其中 ${dangerCount} 项高风险`
                    : `, including ${dangerCount} high-risk`;
            return base + danger;
        },
        startersLabel: lang === 'zh-Hant' ? '快速模板' : isZh ? '快速模板' : 'Quick start',
        startersHint: lang === 'zh-Hant'
            ? '選一個起點（可再改名稱與提示詞），比自己勾工具更安全'
            : isZh
                ? '选一个起点（可再改名称与提示词），比自己勾工具更安全'
                : 'Pick a starting point (you can edit name/prompt). Safer than picking tools by hand.',
        tierLabel: lang === 'zh-Hant' ? '能力檔位' : isZh ? '能力档位' : 'Capability profile',
        tierHint: lang === 'zh-Hant'
            ? '按場景限制能力；「全功能」與內置專家一致。自訂可展開高級白名單。'
            : isZh
                ? '按场景限制能力；「全功能」与内置专家一致。自定义可展开高级白名单。'
                : 'Limit capabilities by scenario. Full access matches builtin experts. Custom opens the advanced whitelist.',
        tierNames: {
            full: lang === 'zh-Hant' ? '全功能' : isZh ? '全功能' : 'Full access',
            advisor: lang === 'zh-Hant' ? '對話顧問' : isZh ? '对话顾问' : 'Advisor',
            docs: lang === 'zh-Hant' ? '文檔助手' : isZh ? '文档助手' : 'Documents',
            office: lang === 'zh-Hant' ? '辦公專家' : isZh ? '办公专家' : 'Office',
            custom: lang === 'zh-Hant' ? '自訂' : isZh ? '自定义' : 'Custom',
        } as Record<CapabilityTierId, string>,
        tierDescs: {
            full: lang === 'zh-Hant' ? '不限制工具與技能' : isZh ? '不限制工具与技能' : 'No tool/skill limits',
            advisor: lang === 'zh-Hant' ? '偏對話與記憶，收斂高風險工具' : isZh ? '偏对话与记忆，收敛高风险工具' : 'Chat & memory; high-risk tools limited',
            docs: lang === 'zh-Hant' ? '讀寫與檢索文檔、網頁' : isZh ? '读写与检索文档、网页' : 'Read/write docs and web research',
            office: lang === 'zh-Hant' ? '文檔 + 辦公技能（PPT/表格等）' : isZh ? '文档 + 办公技能（PPT/表格等）' : 'Docs + office skills (PPT/sheets…)',
            custom: lang === 'zh-Hant' ? '手動白名單（高級）' : isZh ? '手动白名单（高级）' : 'Manual whitelist (advanced)',
        } as Record<CapabilityTierId, string>,
        riskDangerNote: lang === 'zh-Hant'
            ? '標有「高風險」的工具可操作系統或發送輸入，請僅在必要時勾選。'
            : isZh
                ? '标有「高风险」的工具可操作系统或发送输入，请仅在必要时勾选。'
                : 'Tools marked High risk can control the system or send input — enable only when needed.',
        groupSelectAll: lang === 'zh-Hant' ? '全選本組' : isZh ? '全选本组' : 'Select group',
        groupClear: lang === 'zh-Hant' ? '清空本組' : isZh ? '清空本组' : 'Clear group',
        accessCardTitle: lang === 'zh-Hant' ? '當前能力邊界' : isZh ? '当前能力边界' : 'Current capability boundary',
        accessCardTier: lang === 'zh-Hant' ? '檔位' : isZh ? '档位' : 'Profile',
        accessCardDanger: lang === 'zh-Hant' ? '高風險項' : isZh ? '高风险项' : 'High-risk items',
        accessCardNoDanger: lang === 'zh-Hant' ? '未包含高風險工具/技能' : isZh ? '未包含高风险工具/技能' : 'No high-risk tools/skills selected',
        accessCardOpenAdvanced: lang === 'zh-Hant' ? '查看白名單' : isZh ? '查看白名单' : 'Review whitelist',
        unsavedChanges: lang === 'zh-Hant' ? '尚有未儲存的修改，確定要關閉嗎？' : isZh ? '有未保存的修改，确定关闭吗？' : 'You have unsaved changes. Close anyway?',
        optimizationChanges: lang === 'zh-Hant' ? '本次優化變更' : isZh ? '本次优化变更' : 'Optimization changes',
        optimizationChangesHint: lang === 'zh-Hant'
            ? '綠色為新增，紅色為相對原專家移除；下方內容仍可直接編輯。'
            : isZh
                ? '绿色为新增，红色为相对原专家移除；下方内容仍可直接编辑。'
                : 'Green is added and red is removed compared with the source expert. You can still edit the fields below.',
        promptChanges: lang === 'zh-Hant' ? '提示詞差異' : isZh ? '提示词差异' : 'Prompt changes',
        promptChangeSummary: (added: number, removed: number) => lang === 'zh-Hant'
            ? `新增 ${added} 行 · 移除 ${removed} 行`
            : isZh
                ? `新增 ${added} 行 · 移除 ${removed} 行`
                : `${added} added · ${removed} removed`,
        noPromptChanges: lang === 'zh-Hant' ? '提示詞沒有變更' : isZh ? '提示词没有变更' : 'No prompt changes',
        capabilityChanges: lang === 'zh-Hant' ? '工具與技能差異' : isZh ? '工具与技能差异' : 'Tool and skill changes',
        added: lang === 'zh-Hant' ? '新增' : isZh ? '新增' : 'Added',
        removed: lang === 'zh-Hant' ? '移除' : isZh ? '移除' : 'Removed',
        toolAccessRestricted: (count: number) => lang === 'zh-Hant'
            ? `工具存取改為白名單（${count} 項）`
            : isZh
                ? `工具访问改为白名单（${count} 项）`
                : `Tools changed to a ${count}-item allow-list`,
        toolAccessUnrestricted: lang === 'zh-Hant'
            ? '工具存取改為不限制'
            : isZh
                ? '工具访问改为不限制'
                : 'Tools changed to unrestricted access',
        skillAccessRestricted: (count: number) => lang === 'zh-Hant'
            ? `技能存取改為白名單（${count} 項）`
            : isZh
                ? `技能访问改为白名单（${count} 项）`
                : `Skills changed to a ${count}-item allow-list`,
        skillAccessUnrestricted: lang === 'zh-Hant'
            ? '技能存取改為不限制'
            : isZh
                ? '技能访问改为不限制'
                : 'Skills changed to unrestricted access',
        noCapabilityChanges: lang === 'zh-Hant' ? '工具與技能沒有變更' : isZh ? '工具与技能没有变更' : 'No tool or skill changes',
    }), [isZh, lang]);

    /** Update mode for an optimize draft: reuse the existing optimized expert's id. */
    const draftUpdateId = optimizeDraft?.update_existing ? String(optimizeDraft?.id || '').trim() : '';
    const editing = !!expert?.id || !!draftUpdateId;
    const initialTools = expert?.tools || (Array.isArray(optimizeDraft?.tools) ? optimizeDraft.tools : []);
    const initialSkills = expert?.skills || (Array.isArray(optimizeDraft?.skills) ? optimizeDraft.skills : []);
    const hadInitialRestrictions = !!(initialTools.length || initialSkills.length);
    /** Lineage carried into the save payload (专家优化). */
    const optimizedFromId = String(expert?.optimized_from_id || optimizeDraft?.optimized_from_id || '').trim();
    const draftSourceName = String(optimizeDraft?.source_name || '').trim();
    const sourceSystemPrompt = String(optimizeDraft?.source_system_prompt || '');
    const sourceTools = Array.isArray(optimizeDraft?.source_tools) ? optimizeDraft.source_tools : [];
    const sourceSkills = Array.isArray(optimizeDraft?.source_skills) ? optimizeDraft.source_skills : [];
    // Optimization already has a generated draft to review. Creation aids can
    // overwrite it and consume valuable dialog space, so keep this surface
    // focused on reviewing and editing the optimized expert.
    const isOptimizationDraft = !!optimizeDraft;
    const showCreationAids = !editing && !isOptimizationDraft;
    const isOptimizationReview = !!optimizeDraft && (
        Object.prototype.hasOwnProperty.call(optimizeDraft, 'source_system_prompt')
        || Object.prototype.hasOwnProperty.call(optimizeDraft, 'source_tools')
        || Object.prototype.hasOwnProperty.call(optimizeDraft, 'source_skills')
    );

    const [idea, setIdea] = useState('');
    const [generating, setGenerating] = useState(false);
    const [generateError, setGenerateError] = useState('');
    const [name, setName] = useState(expert?.name || optimizeDraft?.name || '');
    const [icon, setIcon] = useState(expert?.icon || optimizeDraft?.icon || '');
    const [description, setDescription] = useState(expert?.description || optimizeDraft?.description || '');
    const [systemPrompt, setSystemPrompt] = useState(expert?.system_prompt || optimizeDraft?.system_prompt || '');
    const [about, setAbout] = useState(expert?.about || optimizeDraft?.about || '');
    const [tools, setTools] = useState<string[]>(initialTools);
    const [skills, setSkills] = useState<string[]>(initialSkills);
    const [availableTools, setAvailableTools] = useState<ToolNameEntry[]>([]);
    const [availableSkills, setAvailableSkills] = useState<string[]>([]);
    /** AI suggestions dropped because they don't exist in the available tool/skill lists. */
    const [ignoredSuggestions, setIgnoredSuggestions] = useState<string[]>([]);
    /** Matched AI suggestions shown as a summary; not applied unless adopt is on. */
    const [suggestedTools, setSuggestedTools] = useState<string[]>([]);
    const [suggestedSkills, setSuggestedSkills] = useState<string[]>([]);
    const [hasGeneratedSuggestions, setHasGeneratedSuggestions] = useState(false);
    const [adoptSuggestions, setAdoptSuggestions] = useState(false);
    const [capabilityTier, setCapabilityTier] = useState<CapabilityTierId>(
        () => (hadInitialRestrictions ? 'custom' : 'full'),
    );
    const [activeStarter, setActiveStarter] = useState<ExpertStarterTemplateId | null>(null);
    /** Collapsed by default on create; open when editing an already-restricted expert. */
    const [advancedOpen, setAdvancedOpen] = useState(hadInitialRestrictions);
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [confirmDiscard, setConfirmDiscard] = useState(false);
    // Prompt diffs are quadratic in line count. Keep editing immediate even for
    // lengthy expert prompts, while React refreshes the visual review shortly after.
    const deferredSystemPrompt = useDeferredValue(systemPrompt);

    /** Invalidates in-flight applyTier backend refinements (race / manual edit). */
    const tierApplySeq = useRef(0);
    /** Invalidates a late AI generation after the editor has been closed. */
    const generateSeq = useRef(0);
    /** Blocks duplicate generate triggers before React can render disabled UI. */
    const generatingRef = useRef(false);
    /** Blocks duplicate save triggers before React can render disabled UI. */
    const savingRef = useRef(false);
    /** Invalidates a late save completion after the editor has been closed. */
    const saveSeq = useRef(0);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previouslyFocusedRef = useRef<HTMLElement | null>(null);
    const discardConfirmRef = useRef<HTMLDivElement | null>(null);
    const toolsRef = useRef(tools);
    const skillsRef = useRef(skills);
    const capabilityTierRef = useRef(capabilityTier);
    const availableToolsRef = useRef(availableTools);
    const availableSkillsRef = useRef(availableSkills);
    const initialEditorStateRef = useRef('');
    toolsRef.current = tools;
    skillsRef.current = skills;
    capabilityTierRef.current = capabilityTier;
    availableToolsRef.current = availableTools;
    availableSkillsRef.current = availableSkills;

    const editorStateSignature = JSON.stringify({
        name: name.trim(),
        icon: icon.trim(),
        description: description.trim(),
        systemPrompt,
        about: about.trim(),
        tools,
        skills,
    });
    const isDirty = initialEditorStateRef.current !== '' && initialEditorStateRef.current !== editorStateSignature;

    const knownToolSet = useMemo(() => {
        if (availableTools.length === 0) return null;
        return new Set(availableTools.map((t) => t.name));
    }, [availableTools]);
    const knownSkillSet = useMemo(() => {
        if (availableSkills.length === 0) return null;
        return new Set(availableSkills);
    }, [availableSkills]);

    const toolDescByName = useMemo(() => {
        const map = new Map<string, string>();
        for (const tool of availableTools) {
            if (tool.description) map.set(tool.name, tool.description);
        }
        return map;
    }, [availableTools]);

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

    useEffect(() => {
        if (!initialEditorStateRef.current) initialEditorStateRef.current = editorStateSignature;
    }, [editorStateSignature]);

    const setToolsStable = (next: string[]) => {
        setTools((prev) => (sameStringList(prev, next) ? prev : next));
    };
    const setSkillsStable = (next: string[]) => {
        setSkills((prev) => (sameStringList(prev, next) ? prev : next));
    };

    // Once catalogs load: re-resolve preset tiers, or re-infer tier for edit mode.
    // Refs avoid freezing tools/skills from an earlier render.
    useEffect(() => {
        if (!availableTools.length && !availableSkills.length) return;

        const current = capabilityTierRef.current;
        if (current !== 'custom' && current !== 'full') {
            // Cancel any in-flight backend refine that was started against a stale catalog.
            tierApplySeq.current += 1;
            const resolved = resolveCapabilityTier(current, availableTools, availableSkills);
            setToolsStable(resolved.tools);
            setSkillsStable(resolved.skills);
            return;
        }
        if (editing || hadInitialRestrictions) {
            const inferred = inferCapabilityTier(
                toolsRef.current,
                skillsRef.current,
                availableTools,
                availableSkills,
            );
            setCapabilityTier((prev) => (prev === inferred ? prev : inferred));
            if (inferred === 'custom' && (toolsRef.current.length > 0 || skillsRef.current.length > 0)) {
                setAdvancedOpen(true);
            }
        }
        // setToolsStable/setSkillsStable are pure helpers closed over setState
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [availableTools, availableSkills, editing, hadInitialRestrictions]);

    // Keep keyboard focus in the review surface while it is open. The overlay
    // is portalled outside the transformed app-scale layer, so restoring focus
    // here also returns users to the exact control that launched it.
    useEffect(() => {
        previouslyFocusedRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        dialogRef.current?.focus();
        return () => previouslyFocusedRef.current?.focus();
    }, []);

    // Treat the discard prompt as the active dialog while it is present. Its
    // controls receive focus rather than leaving keyboard users on actions
    // hidden beneath the confirmation layer.
    useEffect(() => {
        if (!confirmDiscard) return;
        const firstAction = discardConfirmRef.current?.querySelector<HTMLElement>('button:not([disabled])');
        firstAction?.focus();
    }, [confirmDiscard]);

    const requestClose = () => {
        if (generating || saving) return;
        if (isDirty) {
            setConfirmDiscard(true);
            return;
        }
        onClose();
    };

    // Esc closes the dialog unless an operation is in flight; Tab cycles within
    // the modal so keyboard users cannot interact with obscured page controls.
    useEffect(() => {
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                if (!generating && !saving) {
                    e.preventDefault();
                    e.stopPropagation();
                    requestClose();
                }
                return;
            }
            if (e.key !== 'Tab') return;
            // Do not steal an IME candidate-list navigation keystroke from an
            // active text editor. This is especially important for Chinese
            // prompt authoring in the desktop WebView.
            if ((e as KeyboardEvent & { isComposing?: boolean }).isComposing) return;
            const dialog = confirmDiscard ? discardConfirmRef.current : dialogRef.current;
            if (!dialog) return;
            const focusable = dialogFocusableElements(dialog);
            if (focusable.length === 0) {
                e.preventDefault();
                dialog.focus();
                return;
            }
            const active = document.activeElement;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (e.shiftKey && (active === dialog || active === first || !dialog.contains(active))) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && (active === last || !dialog.contains(active))) {
                e.preventDefault();
                first.focus();
            }
        };
        window.addEventListener('keydown', onKeyDown);
        return () => window.removeEventListener('keydown', onKeyDown);
    }, [confirmDiscard, generating, isDirty, onClose, saving]);

    useEffect(() => () => {
        generateSeq.current += 1;
        saveSeq.current += 1;
    }, []);

    // A generated profile is a suggestion. If the user starts editing the
    // expert while it is in flight, discard that stale suggestion rather than
    // overwriting their changes when the backend eventually responds.
    const cancelPendingGeneration = () => {
        if (!generatingRef.current) return;
        generateSeq.current += 1;
        generatingRef.current = false;
        setGenerating(false);
    };

    /** Intersect AI suggestions with the available lists; when a list failed to
     * load (empty), keep suggestions as-is rather than dropping everything. */
    const reconcileSuggestions = (incomingTools: string[], incomingSkills: string[]) => {
        const ignored: string[] = [];
        const nextTools: string[] = [];
        const nextSkills: string[] = [];
        for (const t of incomingTools) {
            if (!knownToolSet || knownToolSet.has(t)) nextTools.push(t);
            else ignored.push(t);
        }
        for (const s of incomingSkills) {
            if (!knownSkillSet || knownSkillSet.has(s)) nextSkills.push(s);
            else ignored.push(s);
        }
        setIgnoredSuggestions(ignored);
        return { nextTools, nextSkills };
    };

    /** Apply tier allow-lists: local rules first (sync UI), then backend refinement. */
    const applyTier = async (tier: CapabilityTierId, opts?: { openAdvanced?: boolean }) => {
        const seq = ++tierApplySeq.current;
        setCapabilityTier(tier);
        setAdoptSuggestions(false);
        if (tier === 'custom') {
            if (opts?.openAdvanced !== false) setAdvancedOpen(true);
            return;
        }
        if (tier === 'full') {
            setToolsStable([]);
            setSkillsStable([]);
            return;
        }
        // Immediate local resolve so starters/clicks don't race with async backend.
        const local = resolveCapabilityTier(tier, availableTools, availableSkills);
        setToolsStable(local.tools);
        setSkillsStable(local.skills);
        try {
            const mod = await getApp();
            if (!mod?.ResolveExpertCapabilityTier) return;
            // Drop if user switched tier / edited whitelist while we were waiting.
            if (seq !== tierApplySeq.current || capabilityTierRef.current !== tier) return;
            const raw = await mod.ResolveExpertCapabilityTier(tier);
            if (seq !== tierApplySeq.current || capabilityTierRef.current !== tier) return;
            const parsed = JSON.parse(raw || '{}') as { tools?: string[]; skills?: string[] };
            const nextTools = Array.isArray(parsed.tools) ? parsed.tools.map(String).filter(Boolean) : [];
            const nextSkills = Array.isArray(parsed.skills) ? parsed.skills.map(String).filter(Boolean) : [];
            // Latest catalogs via refs (avoid stale closure after await).
            const liveTools = availableToolsRef.current;
            const liveSkills = availableSkillsRef.current;
            const toolsKnown = liveTools.length > 0 ? new Set(liveTools.map((t) => t.name)) : null;
            const skillsKnown = liveSkills.length > 0 ? new Set(liveSkills) : null;
            setToolsStable(intersectKnown(nextTools, toolsKnown));
            setSkillsStable(intersectKnown(nextSkills, skillsKnown));
        } catch {
            // Keep local resolution.
        }
    };

    const invalidatePendingTierApply = () => {
        tierApplySeq.current += 1;
    };

    const markCustomFromManualEdit = (nextTools: string[], nextSkills: string[]) => {
        cancelPendingGeneration();
        invalidatePendingTierApply();
        setAdoptSuggestions(false);
        setActiveStarter(null);
        if (!nextTools.length && !nextSkills.length) {
            setCapabilityTier('full');
            return;
        }
        const inferred = inferCapabilityTier(nextTools, nextSkills, availableTools, availableSkills);
        setCapabilityTier(inferred);
    };

    const applyAdoptState = (adopt: boolean, nextTools: string[], nextSkills: string[]) => {
        cancelPendingGeneration();
        invalidatePendingTierApply();
        setAdoptSuggestions(adopt);
        setActiveStarter(null);
        if (adopt) {
            setToolsStable(nextTools);
            setSkillsStable(nextSkills);
            setCapabilityTier(
                !nextTools.length && !nextSkills.length
                    ? 'full'
                    : inferCapabilityTier(nextTools, nextSkills, availableTools, availableSkills),
            );
            if (nextTools.length > 0 || nextSkills.length > 0) {
                setAdvancedOpen(true);
            }
        } else {
            setToolsStable([]);
            setSkillsStable([]);
            setCapabilityTier('full');
        }
    };

    const handleStarterPick = (id: ExpertStarterTemplateId) => {
        const tpl = EXPERT_STARTER_TEMPLATES.find((x) => x.id === id);
        if (!tpl) return;
        cancelPendingGeneration();
        setActiveStarter(id);
        setName(isZh ? tpl.nameZh : tpl.nameEn);
        setIcon(tpl.icon);
        setDescription(isZh ? tpl.descZh : tpl.descEn);
        setIdea(isZh ? tpl.ideaZh : tpl.ideaEn);
        // Prompt left empty so AI generate (or user) can fill a full system prompt.
        void applyTier(tpl.tier, { openAdvanced: tpl.tier === 'custom' });
    };

    const handleGenerate = async () => {
        const text = idea.trim();
        if (!text || generatingRef.current) return;
        const seq = ++generateSeq.current;
        generatingRef.current = true;
        setGenerating(true);
        setGenerateError('');
        try {
            const mod = await getApp();
            if (!mod?.GenerateExpertProfile) throw new Error('GenerateExpertProfile unavailable');
            const raw = await mod.GenerateExpertProfile(text);
            if (seq !== generateSeq.current) return;
            const profile = JSON.parse(raw || '{}') as GeneratedExpertProfile;
            if (profile.name) setName(String(profile.name));
            if (profile.icon) setIcon(String(profile.icon));
            if (profile.description) setDescription(String(profile.description));
            if (profile.system_prompt) setSystemPrompt(String(profile.system_prompt));
            const rawTools = Array.isArray(profile.suggested_tools) ? profile.suggested_tools.map(String).filter(Boolean) : [];
            const rawSkills = Array.isArray(profile.suggested_skills) ? profile.suggested_skills.map(String).filter(Boolean) : [];
            const { nextTools, nextSkills } = reconcileSuggestions(rawTools, rawSkills);
            setSuggestedTools(nextTools);
            setSuggestedSkills(nextSkills);
            setHasGeneratedSuggestions(true);
            // Keep the user's chosen tier; only clear adopt. Do not auto-tighten whitelist.
            setAdoptSuggestions(false);
            // Read tier from ref — generate may finish after the user changed profile mid-flight.
            const tierNow = capabilityTierRef.current;
            if (tierNow === 'full') {
                setToolsStable([]);
                setSkillsStable([]);
            } else if (tierNow !== 'custom') {
                void applyTier(tierNow);
            }
            // custom: leave the user's current allow-lists alone
        } catch (e: unknown) {
            if (seq !== generateSeq.current) return;
            const msg = e instanceof Error ? e.message : String(e);
            setGenerateError(msg ? `${t.generateFailed}: ${msg}` : t.generateFailed);
        } finally {
            if (seq === generateSeq.current) {
                generatingRef.current = false;
                setGenerating(false);
            }
        }
    };

    const handleAdoptChange = (checked: boolean) => {
        applyAdoptState(checked, suggestedTools, suggestedSkills);
    };

    const handleSave = async () => {
        if (!name.trim()) {
            setSaveError(t.nameRequired);
            return;
        }
        if (draftSourceName && name.trim() === draftSourceName) {
            setSaveError(t.nameMustDifferFromSource(draftSourceName));
            return;
        }
        if (savingRef.current) return;
        const seq = ++saveSeq.current;
        savingRef.current = true;
        setSaving(true);
        setSaveError('');
        try {
            const mod = await getApp();
            if (!mod?.SaveExpert) throw new Error('SaveExpert unavailable');
            // Final guard: never persist names the backend doesn't know about; dedupe for stable store.
            const payload: ExpertSavePayload = {
                name: name.trim(),
                icon: icon.trim(),
                description: description.trim(),
                system_prompt: systemPrompt,
                tools: dedupePreserveOrder(intersectKnown(tools, knownToolSet)),
                skills: dedupePreserveOrder(intersectKnown(skills, knownSkillSet)),
                // Always include lineage/about so edits never silently drop them.
                optimized_from_id: optimizedFromId,
                about: about.trim(),
                // Control-plane rules are not edited here; pass them through.
                capability_rules: expert?.capability_rules,
            };
            const editId = expert?.id || draftUpdateId;
            if (editing && editId) payload.id = editId;
            // Deliberately sanitize at the actual API boundary as a defense in
            // depth measure: diff/source metadata must never reach persistence.
            const raw = await mod.SaveExpert(JSON.stringify(omitOptimizationReviewFields(payload)));
            let saved: ExpertDefinition;
            try {
                saved = JSON.parse(raw || '{}') as ExpertDefinition;
            } catch {
                saved = { ...(expert || {}), ...payload, id: payload.id || '' } as ExpertDefinition;
            }
            // Parent typically unmounts on success; reset saving so a stuck dialog can retry.
            if (seq !== saveSeq.current) return;
            savingRef.current = false;
            setSaving(false);
            onSaved(saved);
        } catch (e: unknown) {
            if (seq !== saveSeq.current) return;
            savingRef.current = false;
            setSaveError(e instanceof Error ? e.message : String(e));
            setSaving(false);
        }
    };

    // Suggestions and unmatched-suggestion notices belong exclusively to the
    // creation flow. Keep optimization review free of every AI-generation
    // surface even if this component later receives prefilled suggestion state.
    const showSuggestionPanel = showCreationAids && hasGeneratedSuggestions;
    const hasMatchedSuggestions = suggestedTools.length > 0 || suggestedSkills.length > 0;
    const isRestricted = tools.length > 0 || skills.length > 0;
    const toolGroups = useMemo(() => groupTools(availableTools), [availableTools]);
    const skillGroups = useMemo(() => groupSkills(availableSkills), [availableSkills]);
    const toolEntryByName = useMemo(() => {
        const map = new Map<string, ToolNameEntry>();
        for (const tool of availableTools) map.set(tool.name, tool);
        return map;
    }, [availableTools]);
    const promptDiff = useMemo(
        () => isOptimizationReview ? buildPromptLineDiff(sourceSystemPrompt, deferredSystemPrompt) : [],
        [isOptimizationReview, sourceSystemPrompt, deferredSystemPrompt],
    );
    const hasPromptChanges = useMemo(
        () => promptDiff.some((line) => line.kind !== 'same'),
        [promptDiff],
    );
    const promptDiffSummary = useMemo(() => summarizePromptDiff(promptDiff), [promptDiff]);
    const toolChanges = useMemo(() => changedNames(sourceTools, tools), [sourceTools, tools]);
    const skillChanges = useMemo(() => changedNames(sourceSkills, skills), [sourceSkills, skills]);
    const toolAccessModeChanged = (sourceTools.length > 0) !== (tools.length > 0);
    const skillAccessModeChanged = (sourceSkills.length > 0) !== (skills.length > 0);
    const hasCapabilityChanges = toolAccessModeChanged || skillAccessModeChanged || toolChanges.added.length > 0 || toolChanges.removed.length > 0 || skillChanges.added.length > 0 || skillChanges.removed.length > 0;

    const dangerCounts = useMemo(
        () => countDangerousSelections(tools, skills, availableTools),
        [tools, skills, availableTools],
    );
    const hasDangerousSelected = dangerCounts.total > 0;
    const dangerousItems = useMemo(() => {
        const items: Array<{ kind: 'tool' | 'skill'; name: string; label: string }> = [];
        for (const name of tools) {
            const entry = toolEntryByName.get(name);
            if (toolRisk(name, entry) !== 'dangerous') continue;
            items.push({
                kind: 'tool',
                name,
                label: toolDisplayLabel(name, isZh, entry?.description, entry),
            });
        }
        for (const name of skills) {
            if (skillRisk(name) !== 'dangerous') continue;
            items.push({
                kind: 'skill',
                name,
                label: skillDisplayLabel(name, isZh),
            });
        }
        return items;
    }, [tools, skills, toolEntryByName, isZh]);

    const setGroupTools = (names: string[], selected: boolean) => {
        setTools((prev) => {
            let next: string[];
            if (selected) {
                const set = new Set(prev);
                for (const n of names) set.add(n);
                next = Array.from(set);
            } else {
                const remove = new Set(names);
                next = prev.filter((n) => !remove.has(n));
            }
            markCustomFromManualEdit(next, skills);
            return next;
        });
    };

    const setGroupSkills = (names: string[], selected: boolean) => {
        setSkills((prev) => {
            let next: string[];
            if (selected) {
                const set = new Set(prev);
                for (const n of names) set.add(n);
                next = Array.from(set);
            } else {
                const remove = new Set(names);
                next = prev.filter((n) => !remove.has(n));
            }
            markCustomFromManualEdit(tools, next);
            return next;
        });
    };

    const dialogTitle = isOptimizationDraft ? t.titleOptimizationReview : editing ? t.titleEdit : t.titleNew;
    const dialog = (
        <div
            className="expert-editor-overlay"
            data-testid="expert-editor-overlay"
            onMouseDown={(event) => {
                if (event.target === event.currentTarget) requestClose();
            }}
        >
            <div ref={dialogRef} className="expert-editor" role="dialog" aria-modal="true" aria-labelledby="expert-editor-title" aria-busy={saving || generating || undefined} tabIndex={-1}>
                <h3 id="expert-editor-title" className="expert-editor__title">{dialogTitle}</h3>
                <fieldset className="expert-editor__body" data-testid="expert-editor-scroll-body" disabled={saving || undefined}>

                {showCreationAids && (
                    <div className="expert-editor__starters" data-testid="expert-starters">
                        <div className="expert-editor__label">{t.startersLabel}</div>
                        <p className="expert-editor__hint">{t.startersHint}</p>
                        <div className="expert-editor__starter-row" role="list">
                            {EXPERT_STARTER_TEMPLATES.map((tpl) => {
                                const label = isZh ? tpl.nameZh : tpl.nameEn;
                                const selected = activeStarter === tpl.id;
                                return (
                                    <button
                                        key={tpl.id}
                                        type="button"
                                        role="listitem"
                                        data-testid={`expert-starter-${tpl.id}`}
                                        className={`expert-editor__starter${selected ? ' expert-editor__starter--active' : ''}`}
                                        onClick={() => handleStarterPick(tpl.id)}
                                        title={isZh ? tpl.descZh : tpl.descEn}
                                    >
                                        <span className="expert-editor__starter-icon" aria-hidden="true">{tpl.icon}</span>
                                        <span className="expert-editor__starter-name">{label}</span>
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                )}

                {showCreationAids && (
                    <div className="expert-editor__idea">
                        <label className="expert-editor__label" htmlFor="expert-idea-input">{t.ideaLabel}</label>
                        <div className="expert-editor__idea-row">
                            <textarea
                                id="expert-idea-input"
                                data-testid="expert-idea-input"
                                className="expert-editor__input expert-editor__idea-input"
                                rows={4}
                                value={idea}
                                placeholder={t.ideaPlaceholder}
                                onChange={(e) => setIdea(e.target.value)}
                                onKeyDown={(e) => {
                                    if (!e.nativeEvent.isComposing && (e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                                        e.preventDefault();
                                        void handleGenerate();
                                    }
                                }}
                                disabled={generating}
                            />
                            <div className="expert-editor__generate-action">
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
                                <span className="expert-editor__generate-shortcut">{t.generateShortcut}</span>
                            </div>
                        </div>
                        {generateError ? <p className="expert-editor__error" role="alert">{generateError}</p> : null}
                    </div>
                )}

                {showCreationAids && ignoredSuggestions.length > 0 && (
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
                            disabled={saving}
                            onChange={(e) => {
                                cancelPendingGeneration();
                                setName(e.target.value);
                            }}
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
                            disabled={saving}
                            onChange={(e) => {
                                cancelPendingGeneration();
                                setIcon(e.target.value);
                            }}
                        />
                    </div>
                </div>

                <div className="expert-editor__field">
                    <label className="expert-editor__label" htmlFor="expert-desc-input">{t.descLabel}</label>
                    <textarea
                        id="expert-desc-input"
                        data-testid="expert-desc-input"
                        className="expert-editor__textarea expert-editor__textarea--description"
                        rows={3}
                        value={description}
                        disabled={saving}
                        onChange={(e) => {
                            cancelPendingGeneration();
                            setDescription(e.target.value);
                        }}
                    />
                </div>

                <div className="expert-editor__field">
                    <label className="expert-editor__label" htmlFor="expert-about-input">{t.aboutLabel}</label>
                    <textarea
                        id="expert-about-input"
                        data-testid="expert-about-input"
                        className="expert-editor__textarea expert-editor__textarea--description"
                        rows={3}
                        value={about}
                        disabled={saving}
                        placeholder={t.aboutPlaceholder}
                        onChange={(e) => {
                            cancelPendingGeneration();
                            setAbout(e.target.value);
                        }}
                    />
                </div>

                <div className="expert-editor__field">
                    <label className="expert-editor__label" htmlFor="expert-prompt-input">{t.promptLabel}</label>
                    {isOptimizationReview ? (
                        <section className="expert-editor__optimization-diff" data-testid="expert-optimization-diff" aria-label={t.optimizationChanges}>
                            <div className="expert-editor__optimization-diff-title">{t.optimizationChanges}</div>
                            <p className="expert-editor__hint">{t.optimizationChangesHint}</p>
                            <div className="expert-editor__diff-section">
                                <div className="expert-editor__diff-heading">
                                    <div className="expert-editor__diff-label">{t.promptChanges}</div>
                                    {hasPromptChanges ? <span className="expert-editor__diff-summary">{t.promptChangeSummary(promptDiffSummary.added, promptDiffSummary.removed)}</span> : null}
                                </div>
                                {hasPromptChanges ? (
                                    <pre className="expert-editor__prompt-diff" data-testid="expert-prompt-diff">
                                        {promptDiff.map((line, index) => (
                                            <span key={`${line.kind}-${index}-${line.text}`} className={`expert-editor__prompt-diff-line expert-editor__prompt-diff-line--${line.kind}`}>
                                                <span aria-hidden="true">{line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : ' '}</span>{line.text || ' '}
                                            </span>
                                        ))}
                                    </pre>
                                ) : <p className="expert-editor__hint" data-testid="expert-prompt-diff-empty">{t.noPromptChanges}</p>}
                            </div>
                            <div className="expert-editor__diff-section" data-testid="expert-capability-diff">
                                <div className="expert-editor__diff-label">{t.capabilityChanges}</div>
                                {hasCapabilityChanges ? (
                                    <div className="expert-editor__diff-chips">
                                        {toolAccessModeChanged ? <span className="expert-editor__diff-chip expert-editor__diff-chip--access">{tools.length > 0 ? t.toolAccessRestricted(tools.length) : t.toolAccessUnrestricted}</span> : null}
                                        {skillAccessModeChanged ? <span className="expert-editor__diff-chip expert-editor__diff-chip--access">{skills.length > 0 ? t.skillAccessRestricted(skills.length) : t.skillAccessUnrestricted}</span> : null}
                                        {toolChanges.added.map((name) => <span key={`tool-add-${name}`} className="expert-editor__diff-chip expert-editor__diff-chip--added">{t.added} · {t.toolsChipPrefix}: {toolDisplayLabel(name, isZh, toolDescByName.get(name), toolEntryByName.get(name))}</span>)}
                                        {toolChanges.removed.map((name) => <span key={`tool-remove-${name}`} className="expert-editor__diff-chip expert-editor__diff-chip--removed">{t.removed} · {t.toolsChipPrefix}: {toolDisplayLabel(name, isZh, toolDescByName.get(name), toolEntryByName.get(name))}</span>)}
                                        {skillChanges.added.map((name) => <span key={`skill-add-${name}`} className="expert-editor__diff-chip expert-editor__diff-chip--added">{t.added} · {t.skillsChipPrefix}: {skillDisplayLabel(name, isZh)}</span>)}
                                        {skillChanges.removed.map((name) => <span key={`skill-remove-${name}`} className="expert-editor__diff-chip expert-editor__diff-chip--removed">{t.removed} · {t.skillsChipPrefix}: {skillDisplayLabel(name, isZh)}</span>)}
                                    </div>
                                ) : <p className="expert-editor__hint">{t.noCapabilityChanges}</p>}
                            </div>
                        </section>
                    ) : null}
                    <textarea
                        id="expert-prompt-input"
                        data-testid="expert-prompt-input"
                        className="expert-editor__textarea"
                        rows={8}
                        value={systemPrompt}
                        disabled={saving}
                        onChange={(e) => {
                            cancelPendingGeneration();
                            setSystemPrompt(e.target.value);
                        }}
                    />
                </div>

                <div className="expert-editor__tiers" data-testid="expert-capability-tiers">
                    <div className="expert-editor__label">{t.tierLabel}</div>
                    <p className="expert-editor__hint">{t.tierHint}</p>
                    <div className="expert-editor__tier-row" role="radiogroup" aria-label={t.tierLabel}>
                        {CAPABILITY_TIER_ORDER.map((id) => {
                            const selected = capabilityTier === id;
                            return (
                                <button
                                    key={id}
                                    type="button"
                                    role="radio"
                                    aria-checked={selected}
                                    data-testid={`expert-tier-${id}`}
                                    className={`expert-editor__tier${selected ? ' expert-editor__tier--active' : ''}`}
                                    onClick={() => {
                                        cancelPendingGeneration();
                                        setActiveStarter(null);
                                        void applyTier(id);
                                    }}
                                    title={t.tierDescs[id]}
                                >
                                    <span className="expert-editor__tier-name">{t.tierNames[id]}</span>
                                    <span className="expert-editor__tier-desc">{t.tierDescs[id]}</span>
                                </button>
                            );
                        })}
                    </div>
                </div>

                <div className="expert-editor__access" data-testid="expert-default-access">
                    <p className={`expert-editor__hint${hasDangerousSelected ? ' expert-editor__hint--danger' : ''}`}>
                        {isRestricted
                            ? t.restrictedSummary(tools.length, skills.length, dangerCounts.total)
                            : t.defaultAccess}
                    </p>
                </div>

                {isRestricted ? (
                    <div className="expert-editor__access-card" data-testid="expert-access-card">
                        <div className="expert-editor__access-card-title">{t.accessCardTitle}</div>
                        <div className="expert-editor__access-card-row">
                            <span className="expert-editor__access-card-label">{t.accessCardTier}</span>
                            <span className="expert-editor__access-card-value" data-testid="expert-access-card-tier">
                                {t.tierNames[capabilityTier]}
                                <span className="expert-editor__access-card-desc">{t.tierDescs[capabilityTier]}</span>
                            </span>
                        </div>
                        <div className="expert-editor__access-card-row">
                            <span className="expert-editor__access-card-label">{t.accessCardDanger}</span>
                            {dangerousItems.length === 0 ? (
                                <span className="expert-editor__hint" data-testid="expert-access-card-no-danger">
                                    {t.accessCardNoDanger}
                                </span>
                            ) : (
                                <div className="expert-editor__access-card-chips" data-testid="expert-access-card-danger-list">
                                    {dangerousItems.map((item) => (
                                        <span
                                            key={`${item.kind}-${item.name}`}
                                            className="expert-editor__chip expert-editor__chip--danger"
                                            title={item.name}
                                        >
                                            {item.kind === 'tool' ? t.toolsChipPrefix : t.skillsChipPrefix}: {item.label}
                                        </span>
                                    ))}
                                </div>
                            )}
                        </div>
                        {!advancedOpen ? (
                            <button
                                type="button"
                                className="expert-editor__link-btn"
                                data-testid="expert-access-card-open-advanced"
                                onClick={() => setAdvancedOpen(true)}
                            >
                                {t.accessCardOpenAdvanced}
                            </button>
                        ) : null}
                    </div>
                ) : null}

                {showSuggestionPanel && (
                    <div className="expert-editor__suggestions" data-testid="expert-suggested-capabilities">
                        <div className="expert-editor__label">{t.suggestedTitle}</div>
                        {!hasMatchedSuggestions ? (
                            <p className="expert-editor__hint">{t.suggestedEmpty}</p>
                        ) : (
                            <>
                                <div className="expert-editor__suggestion-chips" data-testid="expert-suggestion-chips">
                                    {suggestedTools.map((toolName) => (
                                        <span
                                            key={`tool-${toolName}`}
                                            className="expert-editor__chip expert-editor__chip--suggest"
                                            title={toolName}
                                        >
                                            {t.toolsChipPrefix}: {toolDisplayLabel(toolName, isZh, toolDescByName.get(toolName), toolEntryByName.get(toolName))}
                                        </span>
                                    ))}
                                    {suggestedSkills.map((skillName) => (
                                        <span
                                            key={`skill-${skillName}`}
                                            className="expert-editor__chip expert-editor__chip--suggest"
                                            title={skillName}
                                        >
                                            {t.skillsChipPrefix}: {skillDisplayLabel(skillName, isZh)}
                                        </span>
                                    ))}
                                </div>
                                <label className="expert-editor__adopt" data-testid="expert-adopt-suggestions">
                                    <input
                                        type="checkbox"
                                        checked={adoptSuggestions}
                                        onChange={(e) => handleAdoptChange(e.target.checked)}
                                    />
                                    <span>
                                        <span className="expert-editor__adopt-label">{t.adoptLabel}</span>
                                        <span className="expert-editor__hint">{t.adoptHint}</span>
                                    </span>
                                </label>
                            </>
                        )}
                    </div>
                )}

                <div className="expert-editor__advanced" data-testid="expert-advanced-section">
                    <button
                        type="button"
                        className="expert-editor__advanced-toggle"
                        data-testid="expert-advanced-toggle"
                        aria-expanded={advancedOpen}
                        onClick={() => setAdvancedOpen((open) => !open)}
                    >
                        <span>{t.advancedTitle}</span>
                        <span className="expert-editor__advanced-caret">{advancedOpen ? t.collapseAdvanced : t.expandAdvanced}</span>
                    </button>
                    <p className="expert-editor__hint">{t.advancedHint}</p>
                    {hasDangerousSelected ? (
                        <p className="expert-editor__danger-note" data-testid="expert-danger-note" role="status">
                            {t.riskDangerNote}
                        </p>
                    ) : null}
                    {advancedOpen && (
                        <div className="expert-editor__advanced-body" data-testid="expert-advanced-body">
                            <div className="expert-editor__field">
                                <div className="expert-editor__label">{t.toolsLabel}</div>
                                <p className="expert-editor__hint">{t.toolsHint}</p>
                                <div className="expert-editor__grouped" data-testid="expert-tools-list">
                                    {toolGroups.map((group) => {
                                        const names = group.items.map((i) => i.name);
                                        const selectedCount = names.filter((n) => tools.includes(n)).length;
                                        return (
                                            <div
                                                key={group.category}
                                                className="expert-editor__group"
                                                data-testid={`expert-tool-group-${group.category}`}
                                            >
                                                <div className="expert-editor__group-head">
                                                    <span className="expert-editor__group-title">
                                                        {toolCategoryLabel(group.category, isZh)}
                                                        <span className="expert-editor__group-count">
                                                            {selectedCount}/{names.length}
                                                        </span>
                                                    </span>
                                                    <span className="expert-editor__group-actions">
                                                        <button
                                                            type="button"
                                                            className="expert-editor__link-btn"
                                                            onClick={() => setGroupTools(names, true)}
                                                        >
                                                            {t.groupSelectAll}
                                                        </button>
                                                        <button
                                                            type="button"
                                                            className="expert-editor__link-btn"
                                                            onClick={() => setGroupTools(names, false)}
                                                        >
                                                            {t.groupClear}
                                                        </button>
                                                    </span>
                                                </div>
                                                <div className="expert-editor__checks expert-editor__checks--grouped">
                                                    {group.items.map((tool) => {
                                                        const risk = toolRisk(tool.name, tool);
                                                        const label = toolDisplayLabel(tool.name, isZh, tool.description, tool);
                                                        const wasSelected = sourceTools.includes(tool.name);
                                                        const isAdded = isOptimizationReview && tools.includes(tool.name) && !wasSelected;
                                                        const isRemoved = isOptimizationReview && wasSelected && !tools.includes(tool.name);
                                                        return (
                                                            <label
                                                                key={tool.name}
                                                                className={`expert-editor__check expert-editor__check--meta${risk === 'dangerous' ? ' expert-editor__check--danger' : ''}${isAdded ? ' expert-editor__check--added' : ''}${isRemoved ? ' expert-editor__check--removed' : ''}`}
                                                                title={`${tool.name}${tool.description ? ` — ${tool.description}` : ''}`}
                                                            >
                                                                <input
                                                                    type="checkbox"
                                                                    checked={tools.includes(tool.name)}
                                                                    onChange={() => {
                                                                        setTools((prev) => {
                                                                            const next = toggleInList(prev, tool.name);
                                                                            markCustomFromManualEdit(next, skills);
                                                                            return next;
                                                                        });
                                                                    }}
                                                                />
                                                                <span className="expert-editor__check-body">
                                                                    <span className="expert-editor__check-label">{label}</span>
                                                                    <span className="expert-editor__check-id">{tool.name}</span>
                                                                    <span className={riskClass(risk)}>{riskLabel(risk, isZh)}</span>
                                                                    {isAdded ? <span className="expert-editor__change-tag expert-editor__change-tag--added">{t.added}</span> : null}
                                                                    {isRemoved ? <span className="expert-editor__change-tag expert-editor__change-tag--removed">{t.removed}</span> : null}
                                                                    {tool.deferred ? (
                                                                        <span className="expert-editor__deferred">{t.deferredTag}</span>
                                                                    ) : null}
                                                                </span>
                                                            </label>
                                                        );
                                                    })}
                                                </div>
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>

                            <div className="expert-editor__field">
                                <div className="expert-editor__label">{t.skillsLabel}</div>
                                <p className="expert-editor__hint">{t.skillsHint}</p>
                                <div className="expert-editor__grouped" data-testid="expert-skills-list">
                                    {skillGroups.map((group) => {
                                        const names = group.items;
                                        const selectedCount = names.filter((n) => skills.includes(n)).length;
                                        return (
                                            <div
                                                key={group.category}
                                                className="expert-editor__group"
                                                data-testid={`expert-skill-group-${group.category}`}
                                            >
                                                <div className="expert-editor__group-head">
                                                    <span className="expert-editor__group-title">
                                                        {skillCategoryLabel(group.category, isZh)}
                                                        <span className="expert-editor__group-count">
                                                            {selectedCount}/{names.length}
                                                        </span>
                                                    </span>
                                                    <span className="expert-editor__group-actions">
                                                        <button
                                                            type="button"
                                                            className="expert-editor__link-btn"
                                                            onClick={() => setGroupSkills(names, true)}
                                                        >
                                                            {t.groupSelectAll}
                                                        </button>
                                                        <button
                                                            type="button"
                                                            className="expert-editor__link-btn"
                                                            onClick={() => setGroupSkills(names, false)}
                                                        >
                                                            {t.groupClear}
                                                        </button>
                                                    </span>
                                                </div>
                                                <div className="expert-editor__checks expert-editor__checks--grouped">
                                                    {group.items.map((skill) => {
                                                        const risk = skillRisk(skill);
                                                        const label = skillDisplayLabel(skill, isZh);
                                                        const wasSelected = sourceSkills.includes(skill);
                                                        const isAdded = isOptimizationReview && skills.includes(skill) && !wasSelected;
                                                        const isRemoved = isOptimizationReview && wasSelected && !skills.includes(skill);
                                                        return (
                                                            <label
                                                                key={skill}
                                                                className={`expert-editor__check expert-editor__check--meta${risk === 'dangerous' ? ' expert-editor__check--danger' : ''}${isAdded ? ' expert-editor__check--added' : ''}${isRemoved ? ' expert-editor__check--removed' : ''}`}
                                                                title={skill}
                                                            >
                                                                <input
                                                                    type="checkbox"
                                                                    checked={skills.includes(skill)}
                                                                    onChange={() => {
                                                                        setSkills((prev) => {
                                                                            const next = toggleInList(prev, skill);
                                                                            markCustomFromManualEdit(tools, next);
                                                                            return next;
                                                                        });
                                                                    }}
                                                                />
                                                                <span className="expert-editor__check-body">
                                                                    <span className="expert-editor__check-label">{label}</span>
                                                                    {label !== skill ? (
                                                                        <span className="expert-editor__check-id">{skill}</span>
                                                                    ) : null}
                                                                    <span className={riskClass(risk)}>{riskLabel(risk, isZh)}</span>
                                                                    {isAdded ? <span className="expert-editor__change-tag expert-editor__change-tag--added">{t.added}</span> : null}
                                                                    {isRemoved ? <span className="expert-editor__change-tag expert-editor__change-tag--removed">{t.removed}</span> : null}
                                                                </span>
                                                            </label>
                                                        );
                                                    })}
                                                </div>
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>
                        </div>
                    )}
                </div>

                {saveError ? <p className="expert-editor__error" role="alert">{saveError}</p> : null}
                </fieldset>

                <div className="expert-editor__actions">
                    <button
                        type="button"
                        className="expert-editor__button expert-editor__button--secondary"
                        onClick={requestClose}
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
                {confirmDiscard ? (
                    <div ref={discardConfirmRef} className="expert-editor__discard-confirm" role="alertdialog" aria-modal="true" aria-label={t.unsavedChanges}>
                        <p>{t.unsavedChanges}</p>
                        <div className="expert-editor__discard-actions">
                            <button type="button" className="expert-editor__button expert-editor__button--secondary" onClick={() => setConfirmDiscard(false)}>
                                {t.cancel}
                            </button>
                            <button type="button" className="expert-editor__button expert-editor__button--danger" onClick={onClose}>
                                {t.discard}
                            </button>
                        </div>
                    </div>
                ) : null}
            </div>
        </div>
    );
    // `.app-scale-layer` intentionally uses transform for DPI scaling. A fixed
    // descendant of a transformed element is fixed to that element, not the
    // WebView, which is why this dialog could be clipped by the app frame.
    // The viewport keeps the current theme variables but is outside that
    // transform and its overflow boundary.
    if (typeof document === 'undefined') return dialog;
    const overlayHost = document.querySelector<HTMLElement>('.app-viewport') || document.body;
    return createPortal(dialog, overlayHost);
};
