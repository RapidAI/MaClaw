import { localizeText } from '../i18n/langSelect';
import { getWailsAppModule } from './wailsAppModule';

/**
 * LaTeX paper template library — shared types, labels and the hub/local
 * catalogue contract.
 *
 * A template is always a packaged .zip on the Hub and an extracted pack on this
 * machine, so the library has exactly three sources: the implicit blank option,
 * a locally imported pack, and a pack synced from HubCenter after an
 * administrator approved it.
 */

/** Built-in LaTeX paper expert. The LaTeX editing mode and the new-task
 * template picker are both keyed off this id, so it must stay stable. */
export const LATEX_EXPERT_ID = 'builtin-latex-paper';

/** Implicit "no template" choice. A new LaTeX task starts from this. */
export const LATEX_BLANK_TEMPLATE_ID = 'blank';

/** Sidebar library entry that opens the LaTeX template library. */
export const LATEX_TEMPLATES_NAV_TAB = 'latex-templates';

/** Options the create-task dialog passes when it opens an expert. Only the LaTeX
 * paper expert consumes them today; other experts ignore the field. */
export type LatexExpertTaskOptions = {
    /** Installed template to seed the document with. Blank/absent = no template. */
    latexTemplateId?: string;
    /** Display name of that template, so the opening message can name it. */
    latexTemplateName?: string;
};

export type LatexTemplateCategory = {
    id: string;
    name: string;
    description?: string;
    sort_order?: number;
    builtin?: boolean;
};

export type LatexTemplate = {
    id: string;
    name: string;
    description?: string;
    category_id?: string;
    version?: string;
    author?: string;
    main_file?: string;
    file_count?: number;
    size_bytes?: number;
    /** blank | local | hub */
    source?: string;
    remote_id?: string;
    /** Hub review state of a locally shared template. */
    share_status?: string;
    /** Moderation note left by the Hub administrator. */
    share_note?: string;
    created_at?: string;
    updated_at?: string;
};

export type LatexTemplateLibrary = {
    categories: LatexTemplateCategory[];
    templates: LatexTemplate[];
};

export type LatexDocumentResult = {
    project_path: string;
    relative_path: string;
    main_file: string;
    template_id: string;
    template_name: string;
    /** Directory the template zip was unpacked into. Expert tools use this path. */
    workspace_path?: string;
    /** Workspace-relative sources. A file may sit at the root or in a subdirectory. */
    source_files?: string[];
    /** False when an existing document was reused rather than created. */
    created?: boolean;
};

export function isBlankLatexTemplate(template: Pick<LatexTemplate, 'id'> | null | undefined): boolean {
    return String(template?.id || '').trim() === LATEX_BLANK_TEMPLATE_ID;
}

export function isLatexExpertId(expertId: string | null | undefined): boolean {
    return String(expertId || '').trim() === LATEX_EXPERT_ID;
}

/** The built-in LaTeX paper expert card, used whenever the definition is not
 * loaded yet (library click, task dialog) but the expert id is known. The
 * built-in persona is compiled into the binary, so an empty system prompt here
 * is a placeholder the backend fills in — this object only has to be a valid
 * `ExpertDefinition` so the tab machinery can open it. */
export function latexExpertStub(lang: string) {
    return {
        id: LATEX_EXPERT_ID,
        name: latexTemplateText(lang, 'LaTeX Paper Expert', 'LaTeX 论文专家', 'LaTeX 論文專家'),
        description: latexTemplateText(
            lang,
            'Writes a .tex paper end to end from a template, compiling as it goes',
            '基于 LaTeX 模板从提纲到成稿，边写边编译预览，直接产出可投稿的 .tex',
            '基於 LaTeX 模板從提綱到成稿，邊寫邊編譯預覽，直接產出可投稿的 .tex',
        ),
        icon: '📄',
        system_prompt: '',
        tools: [] as string[],
        skills: [] as string[],
        builtin: true,
        created_at: '',
        updated_at: '',
    };
}

/** Parse the `ListLatexTemplates` JSON payload defensively: a malformed or
 * partially written index must degrade to "no templates", never throw inside a
 * render pass. */
export function parseLatexTemplateLibrary(raw: unknown): LatexTemplateLibrary {
    let payload: unknown = raw;
    if (typeof raw === 'string') {
        try {
            payload = JSON.parse(raw);
        } catch {
            return { categories: [], templates: [] };
        }
    }
    const source = (payload || {}) as Partial<LatexTemplateLibrary>;
    const categories = Array.isArray(source.categories) ? source.categories.filter(Boolean) : [];
    const templates = Array.isArray(source.templates) ? source.templates.filter(Boolean) : [];
    return { categories, templates };
}

export function parseLatexDocumentResult(raw: unknown): LatexDocumentResult | null {
    let payload: unknown = raw;
    if (typeof raw === 'string') {
        try {
            payload = JSON.parse(raw);
        } catch {
            return null;
        }
    }
    const result = (payload || {}) as Partial<LatexDocumentResult>;
    const projectPath = String(result.project_path || '').trim();
    const relativePath = String(result.relative_path || result.main_file || '').trim();
    if (!projectPath || !relativePath) return null;
    const sourceFiles = Array.isArray(result.source_files)
        ? result.source_files.map((item) => String(item || '').trim()).filter(Boolean)
        : undefined;
    return {
        project_path: projectPath,
        relative_path: relativePath,
        main_file: String(result.main_file || relativePath),
        template_id: String(result.template_id || LATEX_BLANK_TEMPLATE_ID),
        template_name: String(result.template_name || ''),
        workspace_path: String(result.workspace_path || '').trim() || undefined,
        source_files: sourceFiles && sourceFiles.length > 0 ? sourceFiles : undefined,
        created: result.created !== false,
    };
}

/** The four product sections, in the order the library renders them. Anything
 * an administrator adds on the Hub is appended after these. */
export const LATEX_BUILTIN_CATEGORY_IDS = ['conference', 'journal', 'thesis', 'other'] as const;

export function latexCategoryFallbackName(categoryId: string, lang: string): string {
    switch (categoryId) {
        case 'conference':
            return latexTemplateText(lang, 'Conference', '会议', '會議');
        case 'journal':
            return latexTemplateText(lang, 'Journal', '期刊', '期刊');
        case 'thesis':
            return latexTemplateText(lang, 'Thesis', '毕业论文', '畢業論文');
        case 'other':
            return latexTemplateText(lang, 'Other', '其它', '其它');
        default:
            return categoryId;
    }
}

export type LatexTemplateSection = {
    categoryId: string;
    name: string;
    description: string;
    templates: LatexTemplate[];
};

/** Group the flat catalogue into the library's category sections. The blank
 * option is not a section of its own: it is the first card of the first
 * section, so "no template" stays one click away. */
export function groupLatexTemplatesByCategory(
    library: LatexTemplateLibrary,
    lang: string,
): LatexTemplateSection[] {
    const names = new Map<string, LatexTemplateCategory>();
    for (const category of library.categories) {
        const id = String(category.id || '').trim();
        if (id) names.set(id, category);
    }
    const order: string[] = [];
    for (const id of LATEX_BUILTIN_CATEGORY_IDS) {
        if (names.has(id)) order.push(id);
    }
    for (const id of Array.from(names.keys()).sort()) {
        if (!order.includes(id)) order.push(id);
    }
    // A template can reference a category that is not in the index (for example
    // a Hub category whose merge was interrupted). Surface it rather than hide
    // the template.
    for (const template of library.templates) {
        const id = String(template.category_id || '').trim() || 'other';
        if (id && !names.has(id)) names.set(id, { id, name: latexCategoryFallbackName(id, lang) });
        if (id && !order.includes(id)) order.push(id);
    }
    const sections: LatexTemplateSection[] = [];
    for (const categoryId of order) {
        const category = names.get(categoryId);
        const templates = library.templates.filter(
            (template) => String(template.category_id || '').trim() === categoryId,
        );
        sections.push({
            categoryId,
            name: String(category?.name || latexCategoryFallbackName(categoryId, lang)),
            description: String(category?.description || ''),
            templates,
        });
    }
    return sections;
}

export function latexTemplateText(lang: string, en: string, zhHans: string, zhHant: string): string {
    return localizeText(lang, en, zhHans, zhHant);
}

export function latexShareStatusLabel(lang: string, status: string | undefined): string {
    switch (String(status || '').trim().toLowerCase()) {
        case 'pending':
            return latexTemplateText(lang, 'Pending review', '待审核', '待審核');
        case 'approved':
            return latexTemplateText(lang, 'Published', '已展示', '已展示');
        case 'rejected':
            return latexTemplateText(lang, 'Not published', '未展示', '未展示');
        default:
            return '';
    }
}

export function latexTemplateSourceLabel(lang: string, source: string | undefined): string {
    switch (String(source || '').trim()) {
        case 'hub':
            return latexTemplateText(lang, 'Hub', '中心', '中心');
        case 'local':
            return latexTemplateText(lang, 'Local', '本地', '本機');
        default:
            return '';
    }
}

/** Display name of the implicit "no template" choice. */
export function latexBlankTemplateName(lang: string): string {
    return latexTemplateText(lang, 'Blank template', '空白模板', '空白模板');
}

/**
 * Materialise the LaTeX document for a task. Returns null (after reporting the
 * failure) so callers can simply bail: a task with no document is not a usable
 * LaTeX session, and opening the expert anyway would show an empty workspace.
 *
 * The binding is read through the dynamic Wails module so an older host (or a
 * test harness without the method) degrades to a clear message instead of a
 * hard crash.
 */
export async function createLatexDocumentForTask(
    projectPath: string,
    templateId: string,
    fileName: string,
    lang: string,
    onError?: (message: string) => void,
): Promise<LatexDocumentResult | null> {
    const cleanProjectPath = String(projectPath || '').trim();
    if (!cleanProjectPath) return null;
    const report = (message: string) => {
        if (onError) onError(message);
        else console.error('[latex_template]', message);
    };
    try {
        const mod = await getWailsAppModule();
        const create = mod?.CreateLatexDocument;
        if (typeof create !== 'function') {
            report(latexTemplateText(lang, 'LaTeX documents are unavailable on this build.', '当前版本不支持创建 LaTeX 文档。', '目前版本不支援建立 LaTeX 文件。'));
            return null;
        }
        const result = parseLatexDocumentResult(await create(cleanProjectPath, String(templateId || LATEX_BLANK_TEMPLATE_ID), String(fileName || '')));
        if (!result) {
            report(latexTemplateText(lang, 'The LaTeX document could not be created.', '无法创建 LaTeX 文档。', '無法建立 LaTeX 文件。'));
        }
        return result;
    } catch (error) {
        report(error instanceof Error ? error.message : String(error || ''));
        return null;
    }
}

/** First message sent into the LaTeX paper session. It names the document and
 * the template so the expert starts from what is actually on disk instead of
 * asking the user to restate the setup. When the document already existed the
 * wording changes: claiming to have just created a paper the user has been
 * writing for a while would be wrong. */
function latexParentDir(path: string): string {
    const norm = path.replace(/\\/g, '/');
    const slash = norm.lastIndexOf('/');
    return slash < 0 ? '' : norm.slice(0, slash + 1);
}

function latexDocSource(path: string): boolean {
    return path.replace(/\\/g, '/').split('/').some((segment) => {
        switch (segment.toLowerCase()) {
            case 'doc':
            case 'docs':
            case 'documentation':
            case 'docsrc':
                return true;
            default:
                return false;
        }
    });
}

function latexPaperSupport(path: string): boolean {
    return /\.(cls|sty|bst|bib)$/i.test(path);
}

function latexCompanionClause(lang: string, file: string, sources: string[] | undefined): string {
    const entryDir = latexParentDir(file);
    // A root entry sits next to alternate samples (elsarticle-template-harv.tex).
    // Those are other papers. Class, style and bibliography files stay listed
    // even when they live outside the entry's directory, because that is where
    // \documentclass and \bibliography look.
    const pool = (sources || [])
        .map((item) => String(item || '').trim().replace(/\\/g, '/'))
        .filter((item) => item && item !== file)
        .filter((item) => !latexDocSource(item))
        .filter((item) => !/\.(dtx|ins)$/i.test(item))
        .filter((item) => latexParentDir(item) === entryDir || latexPaperSupport(item));
    const support = pool.filter((item) => latexPaperSupport(item));
    const chapters = entryDir === ''
        ? []
        : pool.filter((item) => !latexPaperSupport(item));
    const others = [...support, ...chapters].slice(0, 6);
    if (others.length === 0) return '';
    const listed = others.join(lang === 'en' ? ', ' : '、');
    return latexTemplateText(
        lang,
        ` Related files: ${listed}.`,
        `相关文件还有：${listed}。`,
        `相關檔案還有：${listed}。`,
    );
}

export function latexPaperOpeningMessage(
    lang: string,
    template: Pick<LatexTemplate, 'id' | 'name'>,
    document: Pick<LatexDocumentResult, 'relative_path' | 'created' | 'source_files'>,
): string {
    const file = String(document.relative_path || 'main.tex');
    const companions = latexCompanionClause(lang, file, document.source_files);
    if (isBlankLatexTemplate(template)) {
        if (document.created === false) {
            return latexTemplateText(
                lang,
                `The LaTeX document is ${file}, already in the current working directory. Read what is already there before suggesting changes.`,
                `LaTeX 文档是 ${file}，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。`,
                `LaTeX 文件是 ${file}，已經在目前工作目錄裡。請先讀一遍現有內容，再給修改建議。`,
            );
        }
        return latexTemplateText(
            lang,
            `I started a blank LaTeX paper. The document is ${file} in the current working directory. Ask me for an outline first, then write section by section and compile as you go.`,
            `我已经建好一份空白的 LaTeX 论文，源文件是当前工作目录里的 ${file}。先给我一份提纲，然后按章节逐节写，每写完一节就编译确认。`,
            `我已經建好一份空白的 LaTeX 論文，原始檔是目前工作目錄裡的 ${file}。先給我一份提綱，然後按章節逐節寫，每寫完一節就編譯確認。`,
        );
    }
    const name = String(template.name || '').trim();
    if (document.created === false) {
        return latexTemplateText(
            lang,
            `The template package is already unpacked in the current working directory. The entry file is ${file} and it carries the "${name}" preamble.${companions} Read that relative path first, then tell me what to change.`,
            `模板包已经解压在当前工作目录，入口文件是 ${file}，并且保留了「${name}」模板的导言区设置。${companions}请先读这个相对路径，再告诉我需要改什么。`,
            `模板包已經解壓在目前工作目錄，入口檔是 ${file}，並且保留了「${name}」模板的前言區設定。${companions}請先讀這個相對路徑，再告訴我需要改什麼。`,
        );
    }
    return latexTemplateText(
        lang,
        `I started a LaTeX paper from the "${name}" template. The package is already unpacked in the current working directory. The entry file is ${file} and it carries the template's preamble.${companions} Read that relative path first, then tell me the outline you plan to follow before writing.`,
        `我已经用「${name}」模板建好一份 LaTeX 论文。模板包已经解压到当前工作目录，入口文件是 ${file}，导言区保留了模板原有的设置。${companions}请先读这个相对路径，再把你打算遵循的提纲告诉我，然后再开始写。`,
        `我已經用「${name}」模板建好一份 LaTeX 論文。模板包已經解壓到目前工作目錄，入口檔是 ${file}，前言區保留了模板原有的設定。${companions}請先讀這個相對路徑，再把你打算遵循的提綱告訴我，然後再開始寫。`,
    );
}
