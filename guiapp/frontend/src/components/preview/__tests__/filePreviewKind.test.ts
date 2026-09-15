import { describe, expect, it } from 'vitest';
import {
    filePreviewKindFromName,
    previewSourceName,
    isChromeLessPreviewKind,
    languageFromFileName,
    previewNeedsLocalFile,
    previewShouldMaterialize,
    rewriteMarkdownImageUrls,
} from '../filePreviewKind';

describe('filePreviewKindFromName', () => {
    it('classifies office, slides, pdf, media, and source', () => {
        expect(filePreviewKindFromName('deck.pptx')).toBe('pptx');
        expect(filePreviewKindFromName('report.PDF')).toBe('pdf');
        expect(filePreviewKindFromName('photo.jpg')).toBe('image');
        expect(filePreviewKindFromName('clip.mp4')).toBe('video');
        expect(filePreviewKindFromName('note.m4a')).toBe('audio');
        expect(filePreviewKindFromName('page.html')).toBe('html');
        expect(filePreviewKindFromName('notes.md')).toBe('markdown');
        expect(filePreviewKindFromName('brief.docx')).toBe('office');
        expect(filePreviewKindFromName('sheet.xlsx')).toBe('office');
        expect(filePreviewKindFromName('old.ppt')).toBe('office');
        expect(filePreviewKindFromName('main.go')).toBe('code');
        expect(filePreviewKindFromName('readme.txt')).toBe('text');
    });

    it('lets language override unknown extensions, but not Office/visual suffixes', () => {
        expect(filePreviewKindFromName('notes.txt', 'markdown')).toBe('markdown');
        expect(filePreviewKindFromName('x.bin', 'pdf')).toBe('pdf');
        expect(filePreviewKindFromName('brief.docx', 'markdown')).toBe('office');
        expect(filePreviewKindFromName('deck.pptx', 'markdown')).toBe('pptx');
    });
});

describe('isChromeLessPreviewKind', () => {
    it('hides code chrome for visual media', () => {
        expect(isChromeLessPreviewKind('pptx')).toBe(true);
        expect(isChromeLessPreviewKind('pdf')).toBe(true);
        expect(isChromeLessPreviewKind('image')).toBe(true);
        expect(isChromeLessPreviewKind('html')).toBe(true);
        expect(isChromeLessPreviewKind('markdown')).toBe(false);
        expect(isChromeLessPreviewKind('office')).toBe(false);
        expect(isChromeLessPreviewKind('code')).toBe(false);
        expect(previewNeedsLocalFile('pptx')).toBe(true);
        expect(previewNeedsLocalFile('office')).toBe(false);
        expect(previewNeedsLocalFile('html')).toBe(false);
        expect(previewShouldMaterialize('pptx', true)).toBe(true);
        expect(previewShouldMaterialize('office', true)).toBe(false);
        expect(previewShouldMaterialize('office', false)).toBe(true);
        expect(previewShouldMaterialize('html', true)).toBe(true);
        expect(previewShouldMaterialize('code', true)).toBe(true);
        expect(previewShouldMaterialize('markdown', true)).toBe(false);
    });
});

describe('previewSourceName', () => {
    it('prefers a path that still has an extension', () => {
        expect(previewSourceName('布偶小猫5岁生日', 'D:/docs/cat.pptx')).toBe('D:/docs/cat.pptx');
        expect(previewSourceName('deck.pptx', 'D:/docs/cat.pptx')).toBe('deck.pptx');
        expect(previewSourceName('', '', 'notes.md')).toBe('notes.md');
    });
});

describe('languageFromFileName', () => {
    it('maps office extracts to markdown and source to syntax ids', () => {
        expect(languageFromFileName('a.docx')).toBe('markdown');
        expect(languageFromFileName('a.pptx')).toBe('pptx');
        expect(languageFromFileName('a.ts')).toBe('typescript');
        expect(languageFromFileName('a.go')).toBe('go');
    });
});

describe('rewriteMarkdownImageUrls', () => {
    it('replaces hub draft image URLs with resolver data URLs', async () => {
        const md = 'Hello\n![cat](/api/mobile/documents/drafts/d1/images/img1)\n';
        const out = await rewriteMarkdownImageUrls(md, async (draftId, imageId) => {
            expect(draftId).toBe('d1');
            expect(imageId).toBe('img1');
            return 'data:image/png;base64,abc';
        });
        expect(out).toContain('![cat](data:image/png;base64,abc)');
        expect(out).not.toContain('/api/mobile/documents');
    });

    it('resolves each unique image once', async () => {
        let calls = 0;
        const md = '![a](/api/mobile/documents/drafts/d1/images/img1)\n![a](/api/mobile/documents/drafts/d1/images/img1)\n![b](/api/mobile/documents/drafts/d1/images/img2)';
        const out = await rewriteMarkdownImageUrls(md, async (_draft, imageId) => {
            calls += 1;
            return `data:image/png;base64,${imageId}`;
        });
        expect(calls).toBe(2);
        expect(out).toContain('![a](data:image/png;base64,img1)');
        expect(out).toContain('![b](data:image/png;base64,img2)');
    });

    it('ignores resolver URLs that are not inline images', async () => {
        const md = '![x](/api/mobile/documents/drafts/d1/images/img1)';
        const out = await rewriteMarkdownImageUrls(md, async () => 'javascript:alert(1)');
        expect(out).toBe(md);
        const svg = await rewriteMarkdownImageUrls(md, async () => 'data:image/svg+xml;base64,PHN2Zz4=');
        expect(svg).toBe(md);
    });
});
