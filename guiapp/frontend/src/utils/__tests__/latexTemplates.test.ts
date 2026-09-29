import { describe, expect, it } from 'vitest';
import {
    LATEX_BLANK_TEMPLATE_ID,
    LATEX_EXPERT_ID,
    groupLatexTemplatesByCategory,
    isBlankLatexTemplate,
    isLatexExpertId,
    latexBlankTemplateName,
    latexPaperOpeningMessage,
    parseLatexDocumentResult,
    parseLatexTemplateLibrary,
    type LatexTemplateLibrary,
} from '../latexTemplates';

const LIBRARY: LatexTemplateLibrary = {
    categories: [
        { id: 'conference', name: '会议', builtin: true },
        { id: 'journal', name: '期刊', builtin: true },
        { id: 'thesis', name: '毕业论文', builtin: true },
        { id: 'other', name: '其它', builtin: true },
        { id: 'poster', name: '海报' },
    ],
    templates: [
        { id: LATEX_BLANK_TEMPLATE_ID, name: '空白模板', category_id: 'other' },
        { id: 'tpl-ieee', name: 'IEEE', category_id: 'conference' },
        { id: 'tpl-nature', name: 'Nature', category_id: 'journal' },
        { id: 'tpl-thesis', name: '学位论文', category_id: 'thesis' },
        { id: 'tpl-notes', name: '会议笔记', category_id: 'other' },
        { id: 'tpl-poster', name: '海报', category_id: 'poster' },
    ],
};

describe('parseLatexTemplateLibrary', () => {
    it('parses the backend JSON payload', () => {
        const parsed = parseLatexTemplateLibrary(JSON.stringify({ categories: LIBRARY.categories, templates: LIBRARY.templates }));
        expect(parsed.templates).toHaveLength(6);
        expect(parsed.categories[0].id).toBe('conference');
    });

    it('degrades to an empty library instead of throwing', () => {
        // A truncated or hand-edited index file must not break the page render.
        expect(parseLatexTemplateLibrary('not json')).toEqual({ categories: [], templates: [] });
        expect(parseLatexTemplateLibrary(undefined)).toEqual({ categories: [], templates: [] });
        expect(parseLatexTemplateLibrary({ templates: 'nope' })).toEqual({ categories: [], templates: [] });
    });
});

describe('groupLatexTemplatesByCategory', () => {
    it('orders the four product sections first and appends hub-only ones', () => {
        const sections = groupLatexTemplatesByCategory(LIBRARY, 'zh-Hans');
        expect(sections.map((section) => section.categoryId)).toEqual(['conference', 'journal', 'thesis', 'other', 'poster']);
        expect(sections[0].templates.map((item) => item.id)).toEqual(['tpl-ieee']);
        expect(sections[3].templates.map((item) => item.id)).toEqual([LATEX_BLANK_TEMPLATE_ID, 'tpl-notes']);
    });

    it('surfaces a template whose category is missing from the index', () => {
        // An interrupted category merge must hide nothing.
        const sections = groupLatexTemplatesByCategory(
            { categories: [], templates: [{ id: 'tpl-x', name: 'X', category_id: 'ghost' }] },
            'zh-Hans',
        );
        expect(sections.map((section) => section.categoryId)).toContain('ghost');
        expect(sections.find((section) => section.categoryId === 'ghost')?.templates).toHaveLength(1);
    });
});

describe('latex ids', () => {
    it('recognises the blank option and the built-in expert', () => {
        expect(isBlankLatexTemplate({ id: LATEX_BLANK_TEMPLATE_ID })).toBe(true);
        expect(isBlankLatexTemplate({ id: 'tpl-ieee' })).toBe(false);
        expect(isLatexExpertId(LATEX_EXPERT_ID)).toBe(true);
        expect(isLatexExpertId('builtin-pptx-maker')).toBe(false);
        expect(isLatexExpertId(undefined)).toBe(false);
    });
});

describe('parseLatexDocumentResult', () => {
    it('requires both a project path and a source path', () => {
        expect(parseLatexDocumentResult('{"project_path":"D:/t","relative_path":"main.tex"}')).toEqual({
            project_path: 'D:/t',
            relative_path: 'main.tex',
            main_file: 'main.tex',
            template_id: LATEX_BLANK_TEMPLATE_ID,
            template_name: '',
            created: true,
        });
        expect(parseLatexDocumentResult('{"relative_path":"main.tex"}')).toBeNull();
        expect(parseLatexDocumentResult('garbage')).toBeNull();
    });

    it('honours an explicit created=false', () => {
        // Reusing an existing paper must not be reported as a fresh document, or
        // the opening message would claim to have just created it.
        const parsed = parseLatexDocumentResult('{"project_path":"D:/t","relative_path":"main.tex","created":false}');
        expect(parsed?.created).toBe(false);
    });
});

describe('latexPaperOpeningMessage', () => {
    it('describes a freshly created blank paper', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: LATEX_BLANK_TEMPLATE_ID, name: '空白模板' }, { relative_path: 'main.tex', created: true });
        expect(message).toContain('空白');
        expect(message).toContain('main.tex');
    });

    it('does not claim to have created a paper that already exists', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: LATEX_BLANK_TEMPLATE_ID, name: '空白模板' }, { relative_path: 'main.tex', created: false });
        expect(message).not.toContain('我已经建好');
        expect(message).toContain('main.tex');
    });

    it('names the template the user picked', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: 'tpl-ieee', name: 'IEEE 会议模板' }, { relative_path: 'main.tex', created: true });
        expect(message).toContain('IEEE 会议模板');
        expect(message).toContain('当前工作目录');
    });

    it('lists files next to the entry and leaves the class manual out', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: 'tpl-els', name: 'elsarticle' }, {
            relative_path: 'elsarticle-template-num.tex',
            created: true,
            source_files: ['elsarticle-template-num.tex', 'elsarticle-template-harv.tex', 'elsarticle-num.bst', 'elsarticle.dtx', 'doc/elsdoc.tex'],
        });
        expect(message).toContain('elsarticle-template-num.tex');
        expect(message).toContain('elsarticle-num.bst');
        expect(message).not.toContain('elsarticle-template-harv.tex');
        expect(message).not.toContain('doc/elsdoc.tex');
        expect(message).not.toContain('elsarticle.dtx');
        expect(message).toContain('已经解压');
    });

    it('lists a chapter beside a nested entry and the bibliography at the root', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: 'tpl-thesis', name: '毕业论文' }, {
            relative_path: 'chapters/main.tex',
            created: true,
            source_files: ['chapters/main.tex', 'chapters/intro.tex', 'refs.bib', 'other-sample.tex'],
        });
        expect(message).toContain('chapters/main.tex');
        expect(message).toContain('chapters/intro.tex');
        expect(message).toContain('refs.bib');
        expect(message).not.toContain('other-sample.tex');
    });

    it('describes a reused document without the creation claim', () => {
        const message = latexPaperOpeningMessage('zh-Hans', { id: 'tpl-ieee', name: 'IEEE 会议模板' }, { relative_path: 'main.tex', created: false });
        expect(message).toContain('IEEE 会议模板');
        expect(message).not.toContain('我已经用');
    });
});

describe('latexBlankTemplateName', () => {
    it('is localized', () => {
        expect(latexBlankTemplateName('zh-Hans')).toBe('空白模板');
        expect(latexBlankTemplateName('zh-Hant')).toBe('空白模板');
        expect(latexBlankTemplateName('en')).toBe('Blank template');
    });
});
