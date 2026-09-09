import { describe, expect, it } from 'vitest';
import {
    groupSkills,
    groupTools,
    resolveToolMeta,
    skillDisplayLabel,
    skillRisk,
    toolDisplayLabel,
    toolRisk,
} from '../expertToolMeta';

describe('expertToolMeta', () => {
    it('classifies known tools with Chinese labels and risk', () => {
        expect(toolDisplayLabel('ssh', true)).toBe('SSH 远程');
        expect(toolRisk('ssh')).toBe('dangerous');
        expect(toolDisplayLabel('read_file', true)).toBe('读取文件');
        expect(toolDisplayLabel('fs_read', true)).toBe('读取文件');
        expect(toolRisk('fs_read')).toBe('safe');
        expect(toolRisk('write_file')).toBe('elevated');
        expect(toolDisplayLabel('send_input', false)).toBe('Send input');
        expect(toolRisk('send_input')).toBe('dangerous');
        expect(toolRisk('bash')).toBe('dangerous');
    });

    it('prefers backend-enriched metadata when present', () => {
        const resolved = resolveToolMeta({
            name: 'custom_widget',
            category: 'knowledge',
            risk: 'safe',
            label_zh: '自定义组件',
            label_en: 'Custom widget',
        });
        expect(resolved.category).toBe('knowledge');
        expect(resolved.risk).toBe('safe');
        expect(resolved.labelZh).toBe('自定义组件');
        expect(toolDisplayLabel('custom_widget', true, '', {
            name: 'custom_widget',
            label_zh: '自定义组件',
        })).toBe('自定义组件');
    });

    it('infers prefixes for knowledge/gui/browser tools', () => {
        expect(resolveToolMeta('knowledge_list_sources').category).toBe('knowledge');
        expect(resolveToolMeta('knowledge_list_sources').risk).toBe('safe');
        expect(resolveToolMeta('gui_click').risk).toBe('dangerous');
        expect(resolveToolMeta('browser_observe').category).toBe('automation');
    });

    it('falls back for unknown tools', () => {
        expect(toolDisplayLabel('brand_new_tool', true, '短描述')).toBe('短描述');
        expect(toolDisplayLabel('brand_new_tool', true)).toBe('brand_new_tool');
        expect(toolRisk('brand_new_tool')).toBe('elevated');
    });

    it('groups tools by category including knowledge and system', () => {
        const groups = groupTools([
            { name: 'ssh' },
            { name: 'web_search' },
            { name: 'memory' },
            { name: 'knowledge_search' },
            { name: 'mystery' },
            { name: 'gui_click', category: 'system', risk: 'dangerous', label_zh: '点击' },
        ]);
        expect(groups.map((g) => g.category)).toEqual([
            'interaction',
            'web',
            'knowledge',
            'system',
            'other',
        ]);
        expect(groups.find((g) => g.category === 'system')?.items.map((i) => i.name)).toEqual([
            'ssh',
            'gui_click',
        ]);
    });

    it('classifies skills by substring rules', () => {
        expect(skillRisk('Catfee Ssh')).toBe('dangerous');
        expect(skillDisplayLabel('pptx-gen', true)).toBe('PPT 生成');
        expect(skillRisk('pptx-gen')).toBe('elevated');
        const groups = groupSkills(['pptx-gen', 'paper_pdf_translator', 'craft_task_abc', 'unknown-skill']);
        expect(groups.map((g) => g.category)).toEqual(['docs', 'office', 'dev', 'other']);
    });
});
