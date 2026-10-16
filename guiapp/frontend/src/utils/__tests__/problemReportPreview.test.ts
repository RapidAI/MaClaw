import { describe, expect, it } from 'vitest';
import {
    problemReportDescriptionPreview,
    problemReportDescriptionPreviewLimit,
    problemReportDescriptionTitleLimit,
} from '../problemReportPreview';

describe('problemReportDescriptionPreview', () => {
    it('drops blank descriptions', () => {
        expect(problemReportDescriptionPreview(null)).toEqual({ text: '', title: '' });
        expect(problemReportDescriptionPreview(' \n\t ')).toEqual({ text: '', title: '' });
    });

    it('keeps a short description and collapses internal whitespace', () => {
        expect(problemReportDescriptionPreview('登录后\n点击发送\t没有反应')).toEqual({
            text: '登录后 点击发送 没有反应',
            title: '',
        });
    });

    it('shows a leading excerpt and a longer hover string', () => {
        const description = `开头故障。${'后续细节。'.repeat(40)}`;
        const preview = problemReportDescriptionPreview(description);
        expect(Array.from(preview.text).length).toBe(problemReportDescriptionPreviewLimit + 1);
        expect(preview.text.startsWith('开头故障。')).toBe(true);
        expect(preview.text.endsWith('…')).toBe(true);
        expect(preview.title.startsWith(preview.text.slice(0, -1))).toBe(true);
        expect(Array.from(preview.title).length).toBeGreaterThan(Array.from(preview.text).length);
        expect(Array.from(preview.title).length).toBeLessThanOrEqual(problemReportDescriptionTitleLimit + 1);
    });

    it('does not mark a description that already fits', () => {
        const description = '字'.repeat(problemReportDescriptionPreviewLimit);
        expect(problemReportDescriptionPreview(description)).toEqual({ text: description, title: '' });
    });

    it('caps the hover text', () => {
        const description = '详'.repeat(problemReportDescriptionTitleLimit + 30);
        const preview = problemReportDescriptionPreview(description);
        expect(Array.from(preview.title).length).toBe(problemReportDescriptionTitleLimit + 1);
        expect(preview.title.endsWith('…')).toBe(true);
        expect(preview.title.startsWith(preview.text.slice(0, -1))).toBe(true);
    });

    it('does not split an emoji that sits on the preview boundary', () => {
        const emoji = '😀';
        expect(emoji.length).toBe(2);
        const description = `${'a'.repeat(problemReportDescriptionPreviewLimit - 1)}${emoji}tail`;
        const preview = problemReportDescriptionPreview(description);
        expect(preview.text.endsWith(`${emoji}…`)).toBe(true);
        expect(Array.from(preview.text).at(-2)).toBe(emoji);
    });
});
