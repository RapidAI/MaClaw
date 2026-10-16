import { describe, expect, it } from 'vitest';
import {
    isKnowledgeImportJobActive,
    isKnowledgeImportJobTerminal,
    knowledgeImportSupportedFormatsHint,
    knowledgeImportSupportedFormatsHintZhHans,
    mergeKnowledgeImportProgress,
} from '../KnowledgeImportDialog';

describe('knowledge import job helpers', () => {
    it('advertises every Office format while retaining the PPT rich-content caveat', () => {
        expect(knowledgeImportSupportedFormatsHint).toContain('PPT/PPTX');
        expect(knowledgeImportSupportedFormatsHint).toContain('DOC/DOCX');
        expect(knowledgeImportSupportedFormatsHint).toContain('XLS/XLSX');
        expect(knowledgeImportSupportedFormatsHintZhHans).toContain('PPT/PPTX');
        expect(knowledgeImportSupportedFormatsHintZhHans).toContain('.ppt 富内容需启用 OfficeRead 知识库灰度');
    });

    it('detects active and terminal statuses', () => {
        expect(isKnowledgeImportJobActive({ status: 'running' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'queued' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'pending' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'indexing' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'completed' })).toBe(false);
        expect(isKnowledgeImportJobTerminal({ status: 'completed' })).toBe(true);
        expect(isKnowledgeImportJobTerminal({ status: 'failed' })).toBe(true);
        expect(isKnowledgeImportJobTerminal({ status: 'indexing' })).toBe(false);
        expect(isKnowledgeImportJobTerminal({ status: 'running' })).toBe(false);
    });

    it('merges indexing post-work step fields', () => {
        const prev = {
            id: 'kjob_1',
            status: 'indexing',
            result: { processed_files: 10, imported_files: 10, total_files: 10 },
        };
        const next = mergeKnowledgeImportProgress(prev, {
            job_id: 'kjob_1',
            status: 'indexing',
            processed_files: 10,
            imported_files: 10,
            total_files: 10,
            current_step: 'embedding',
            step_progress: 40,
        });
        expect(next.status).toBe('indexing');
        expect(next.result?.current_step).toBe('embedding');
        expect(next.result?.step_progress).toBe(40);
    });

    it('does not regress completed status back to indexing', () => {
        const prev = {
            id: 'kjob_1',
            status: 'completed',
            result: { processed_files: 2, imported_files: 2, total_files: 2, current_step: '' },
        };
        const next = mergeKnowledgeImportProgress(prev, {
            job_id: 'kjob_1',
            status: 'indexing',
            processed_files: 2,
            imported_files: 2,
            total_files: 2,
            current_step: 'linking',
            step_progress: 0,
        });
        expect(next.status).toBe('completed');
    });

    it('does not overwrite failed with completed', () => {
        const prev = {
            id: 'kjob_1',
            status: 'failed',
            result: { processed_files: 3, imported_files: 2, failed_files: 1 },
        };
        const next = mergeKnowledgeImportProgress(prev, {
            job_id: 'kjob_1',
            status: 'completed',
            processed_files: 3,
            imported_files: 2,
            failed_files: 1,
        });
        expect(next.status).toBe('failed');
    });

    it('clears step fields on terminal completion events', () => {
        const prev = {
            id: 'kjob_1',
            status: 'indexing',
            result: { processed_files: 2, imported_files: 2, current_step: 'embedding', step_progress: 80 },
        };
        const next = mergeKnowledgeImportProgress(prev, {
            job_id: 'kjob_1',
            status: 'completed',
            processed_files: 2,
            imported_files: 2,
        });
        expect(next.status).toBe('completed');
        expect(next.result?.current_step).toBe('');
        expect(next.result?.step_progress).toBe(0);
    });

    it('merges progress events and clears stale step fields when a file completes', () => {
        const prev = {
            id: 'kjob_1',
            status: 'running',
            result: {
                processed_files: 1,
                current_step: 'parsing',
                step_progress: 40,
                current_step_num: 2,
                total_steps: 5,
            },
        };
        const next = mergeKnowledgeImportProgress(prev, {
            job_id: 'kjob_1',
            status: 'running',
            processed_files: 2,
            imported_files: 2,
            current_file: 'b.md',
            // omit step fields (omitempty zeros)
        });
        expect(next.status).toBe('running');
        expect(next.result?.processed_files).toBe(2);
        expect(next.result?.imported_files).toBe(2);
        expect(next.result?.current_step).toBe('');
        expect(next.result?.step_progress).toBe(0);
        expect(next.result?.current_step_num).toBe(0);
        expect(next.result?.total_steps).toBe(0);
    });

    it('creates a job from the first progress event', () => {
        const next = mergeKnowledgeImportProgress(null, {
            job_id: 'kjob_new',
            status: 'running',
            total_files: 3,
            processed_files: 0,
        });
        expect(next.id).toBe('kjob_new');
        expect(next.status).toBe('running');
        expect(next.result?.total_files).toBe(3);
    });
});
