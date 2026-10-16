import { describe, expect, it } from 'vitest';
import {
    countDangerousSelections,
    inferCapabilityTier,
    pickSkillsFromMatchers,
    pickToolsFromCandidates,
    resolveCapabilityTier,
} from '../expertCapabilityTiers';

describe('expertCapabilityTiers', () => {
    const tools = [
        'memory',
        'ask_user',
        'read_file',
        'write_file',
        'web_search',
        'web_fetch',
        'search_files',
        'office',
        'ssh',
        'bash',
        'screenshot',
        'send_file',
        'task',
        'tts',
        'knowledge_search',
    ];
    const skills = ['pptx-gen', 'pdf-word', 'sheet-analysis', 'contract-review', 'empty-skill', 'craft_task_abc', 'Catfee Ssh'];

    it('full and custom resolve to unrestricted empty lists', () => {
        expect(resolveCapabilityTier('full', tools, skills)).toEqual({ tools: [], skills: [] });
        expect(resolveCapabilityTier('custom', tools, skills)).toEqual({ tools: [], skills: [] });
    });

    it('advisor picks only safe interaction/media tools', () => {
        const r = resolveCapabilityTier('advisor', tools, skills);
        expect(r.tools).toContain('memory');
        expect(r.tools).toContain('ask_user');
        expect(r.tools).toContain('tts');
        expect(r.tools).not.toContain('ssh');
        expect(r.tools).not.toContain('write_file');
        expect(r.tools).not.toContain('screenshot'); // elevated media
        expect(r.skills).toEqual([]);
    });

    it('docs includes read/search/write/knowledge but never system tools', () => {
        const r = resolveCapabilityTier('docs', tools, skills);
        expect(r.tools).toEqual(expect.arrayContaining(['read_file', 'web_search', 'write_file', 'knowledge_search']));
        expect(r.tools).not.toContain('ssh');
        expect(r.tools).not.toContain('bash');
        expect(r.tools).not.toContain('task'); // automation reserved for office
        expect(r.skills).toEqual(expect.arrayContaining(['pdf-word', 'contract-review']));
        expect(r.skills).not.toContain('craft_task_abc');
        expect(r.skills).not.toContain('pptx-gen'); // office skill
        expect(r.skills).not.toContain('Catfee Ssh');
    });

    it('office includes screenshot/send_file/task and office skills, still no ssh', () => {
        const r = resolveCapabilityTier('office', tools, skills);
        expect(r.tools).toEqual(expect.arrayContaining(['screenshot', 'send_file', 'office', 'task']));
        expect(r.tools).not.toContain('ssh');
        expect(r.tools).not.toContain('bash');
        expect(r.skills).toEqual(expect.arrayContaining(['pptx-gen', 'sheet-analysis', 'contract-review']));
        expect(r.skills).not.toContain('Catfee Ssh');
    });

    it('uses backend-enriched catalog entries when provided', () => {
        const catalog = [
            { name: 'memory', category: 'interaction', risk: 'safe' },
            { name: 'weird_tool', category: 'files', risk: 'elevated', label_zh: '奇怪工具' },
            { name: 'nuke', category: 'system', risk: 'dangerous' },
        ];
        const docs = resolveCapabilityTier('docs', catalog, []);
        expect(docs.tools).toEqual(expect.arrayContaining(['memory', 'weird_tool']));
        expect(docs.tools).not.toContain('nuke');
    });

    it('resolves fs_read/fs_write via meta aliases/categories', () => {
        const alt = ['fs_read', 'fs_write', 'memory'];
        const r = resolveCapabilityTier('docs', alt, []);
        expect(r.tools).toEqual(expect.arrayContaining(['fs_read', 'fs_write', 'memory']));
    });

    it('returns empty tools when catalog not loaded yet', () => {
        expect(resolveCapabilityTier('docs', [], skills)).toEqual({ tools: [], skills: [] });
    });

    it('infers full when unrestricted', () => {
        expect(inferCapabilityTier([], [], tools, skills)).toBe('full');
    });

    it('infers matching preset when sets equal resolved tier', () => {
        const docs = resolveCapabilityTier('docs', tools, skills);
        expect(inferCapabilityTier(docs.tools, docs.skills, tools, skills)).toBe('docs');
        const office = resolveCapabilityTier('office', tools, skills);
        expect(inferCapabilityTier(office.tools, office.skills, tools, skills)).toBe('office');
    });

    it('infers custom for arbitrary allow-lists', () => {
        expect(inferCapabilityTier(['ssh'], [], tools, skills)).toBe('custom');
        expect(inferCapabilityTier(['read_file'], ['pptx-gen'], tools, skills)).toBe('custom');
    });

    it('counts dangerous selections for summary', () => {
        const counts = countDangerousSelections(['ssh', 'read_file'], ['Catfee Ssh', 'pptx-gen'], tools.map((n) => ({ name: n })));
        expect(counts.tools).toBe(1);
        expect(counts.skills).toBe(1);
        expect(counts.total).toBe(2);
    });

    it('pickToolsFromCandidates returns preferred names when catalog empty', () => {
        expect(pickToolsFromCandidates(['memory', 'ssh'], [])).toEqual(['memory', 'ssh']);
    });

    it('pickSkillsFromMatchers is case-insensitive substring', () => {
        expect(pickSkillsFromMatchers(['PPTX', 'sheet'], skills)).toEqual(['pptx-gen', 'sheet-analysis']);
        expect(pickSkillsFromMatchers([], skills)).toEqual([]);
    });
});
