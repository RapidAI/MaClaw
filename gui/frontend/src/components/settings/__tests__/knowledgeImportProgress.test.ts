import { describe, expect, it } from 'vitest';
import {
    isKnowledgeImportJobActive,
    isKnowledgeImportJobTerminal,
    mergeKnowledgeImportProgress,
} from '../KnowledgeImportDialog';

describe('knowledge import job helpers', () => {
    it('detects active and terminal statuses', () => {
        expect(isKnowledgeImportJobActive({ status: 'running' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'queued' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'pending' })).toBe(true);
        expect(isKnowledgeImportJobActive({ status: 'completed' })).toBe(false);
        expect(isKnowledgeImportJobTerminal({ status: 'completed' })).toBe(true);
        expect(isKnowledgeImportJobTerminal({ status: 'failed' })).toBe(true);
        expect(isKnowledgeImportJobTerminal({ status: 'running' })).toBe(false);
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
