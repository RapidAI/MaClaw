import { describe, expect, it } from 'vitest';
import {
    assistantLiveActivityFromToolName,
    assistantLiveActivityLabel,
    assistantLiveReasoningSource,
    assistantMessageOwnsLiveActivity,
    codingTimelineLiveThoughtIndex,
    isGenericCodingLiveKind,
    parseLiveActivityFromProgressText,
    reasoningHasModelThought,
    resolveAssistantLiveActivity,
    resolveStandaloneLiveActivityLabel,
} from '../assistantLiveActivity';

describe('assistantLiveActivity', () => {
    it('maps tool names to action kinds', () => {
        expect(assistantLiveActivityFromToolName('write_file')).toBe('writing_file');
        expect(assistantLiveActivityFromToolName('ssh_write_file')).toBe('writing_file');
        expect(assistantLiveActivityFromToolName('web_fetch')).toBe('fetching_page');
        expect(assistantLiveActivityFromToolName('http_get')).toBe('fetching_page');
        expect(assistantLiveActivityFromToolName('bash')).toBe('running_command');
        expect(assistantLiveActivityFromToolName('edit_file')).toBe('editing_file');
        expect(assistantLiveActivityFromToolName('Edit')).toBe('editing_file');
        expect(assistantLiveActivityFromToolName('Glob')).toBe('searching_files');
        expect(assistantLiveActivityFromToolName('unknown_mcp')).toBe('calling_tool');
    });

    it('parses IM tool-status cards and coding events', () => {
        expect(parseLiveActivityFromProgressText('工具 · 写入文件\nsrc/a.ts')).toBe('writing_file');
        expect(parseLiveActivityFromProgressText('Tool · Open page\nhttps://example.com')).toBe('fetching_page');
        expect(parseLiveActivityFromProgressText('正在执行工具: read_file')).toBe('reading_file');
        expect(parseLiveActivityFromProgressText('正在执行工具')).toBe('calling_tool');
        expect(parseLiveActivityFromProgressText('正在执行工具，请稍候...')).toBe('calling_tool');
        expect(parseLiveActivityFromProgressText('\u{1f680} 正在执行 Skill「Weather Query」...')).toBe('running_skill');
        expect(parseLiveActivityFromProgressText('Coding Agent Event: {"agent":"coding","event":"tool_started","detail":"web_fetch"}')).toBe('fetching_page');
        expect(parseLiveActivityFromProgressText('Coding Agent Event: {"agent":"coding","event":"tool_finished","detail":"web_fetch"}')).toBe('thinking');
        expect(parseLiveActivityFromProgressText('preflight checks done')).toBeNull();
        expect(parseLiveActivityFromProgressText('Working on screenshot')).toBeNull();
        expect(parseLiveActivityFromProgressText('工具 · CustomMCP')).toBe('calling_tool');
    });

    it('prefers the current tool over generic thinking while busy', () => {
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            progressMessages: [{ content: '工具 · 访问网页\nhttps://weather' }],
        })).toBe('fetching_page');
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
        })).toBe('accessing_model');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
        })).toBe('accessing_model');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            hasReasoning: true,
        })).toBe('calling_tool');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            progressMessages: [{ content: '正在执行工具' }],
        })).toBe('calling_tool');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: false,
        })).toBeNull();
    });

    it('does not keep a stale tool card after thinking tokens resume', () => {
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            progressMessages: [{ content: '正在执行工具' }],
        })).toBe('calling_tool');
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            progressMessages: [{ content: '正在执行工具: read_file' }],
        })).toBe('reading_file');
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            progressMessages: [{ content: '工具 · 写入文件\nsrc/a.ts' }],
        })).toBe('accessing_model');
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            codingProgress: { event: 'tool_started', detail: 'write_file' },
        })).toBe('writing_file');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            codingProgress: { event: 'tool_started', detail: 'write_file' },
            progressMessages: [{ content: '工具 · 写入文件\nsrc/a.ts' }],
        })).toBe('writing_file');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            progressMessages: [
                { content: '工具 · 写入文件\nsrc/a.ts' },
                { content: 'preflight checks done' },
            ],
        })).toBe('accessing_model');
    });

    it('promotes chat [Status] milestones onto the live header', () => {
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            reasoningText: '• 执行环境已就绪\n• 正在同步会话上下文\n• 正在分析任务并开始处理',
        })).toBe('analyzing');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            reasoningText: '• 正在同步会话上下文',
        })).toBe('syncing_context');
        expect(resolveAssistantLiveActivity({
            streaming: false,
            busy: true,
            reasoningText: '[Status] 模型请求已发送，正在等待响应',
        })).toBe('accessing_model');
        expect(resolveAssistantLiveActivity({
            streaming: true,
            busy: true,
            reasoningText: '• 正在分析任务并开始处理\n先查天气源。',
        })).toBe('thinking');
        expect(reasoningHasModelThought('• 正在分析任务并开始处理')).toBe(false);
        expect(reasoningHasModelThought('• 正在分析任务并开始处理\n先查天气源。')).toBe(true);
        expect(assistantLiveReasoningSource({
            role: 'assistant',
            content: '',
            reasoning: '• 正在同步会话上下文',
            codingTimeline: [{ kind: 'thinking', content: 'Let me explore.' }],
        })).toBe('• 正在同步会话上下文\nLet me explore.');
        expect(assistantLiveReasoningSource({ role: 'assistant', content: 'Done.', reasoning: 'Checked.' })).toBe('');
    });

    it('only lets an in-flight assistant own the live header', () => {
        expect(assistantMessageOwnsLiveActivity({ role: 'assistant', content: '' }, false)).toBe(true);
        expect(assistantMessageOwnsLiveActivity({ role: 'assistant', content: 'It is sunny.' }, false)).toBe(false);
        expect(assistantMessageOwnsLiveActivity({ role: 'assistant', content: 'It is sunny.' }, true)).toBe(true);
        expect(assistantMessageOwnsLiveActivity({ role: 'assistant', content: '' }, true, false)).toBe(false);
        expect(assistantMessageOwnsLiveActivity({ role: 'user', content: '' }, true)).toBe(false);
        expect(assistantMessageOwnsLiveActivity(undefined, true)).toBe(false);
    });

    it('uses progressive status copy in zh and en', () => {
        expect(assistantLiveActivityLabel('thinking', 'zh-Hans')).toBe('正在思考');
        expect(assistantLiveActivityLabel('calling_tool', 'zh-Hans')).toBe('正在调用工具');
        expect(assistantLiveActivityLabel('accessing_model', 'zh-Hans')).toBe('正在访问模型');
        expect(assistantLiveActivityLabel('analyzing', 'zh-Hans')).toBe('正在分析任务');
        expect(assistantLiveActivityLabel('syncing_context', 'zh-Hans')).toBe('正在同步上下文');
        expect(assistantLiveActivityLabel('searching_web', 'zh-Hans')).toBe('正在搜索网络');
        expect(assistantLiveActivityLabel('fetching_page', 'zh-Hans')).toBe('正在提取网页');
        expect(assistantLiveActivityLabel('writing_file', 'zh-Hans')).toBe('正在写入文件');
        expect(assistantLiveActivityLabel('editing_file', 'zh-Hans')).toBe('正在编辑文件');
        expect(assistantLiveActivityLabel('thinking', 'en')).toBe('Thinking');
        expect(assistantLiveActivityLabel('writing_file', 'zh-Hant')).toBe('正在寫入檔案');
    });

    it('puts coding live sheen on the last thought only when it is the current last item', () => {
        const timeline = [
            { kind: 'thinking' },
            { kind: 'progress' },
            { kind: 'thinking' },
        ];
        expect(codingTimelineLiveThoughtIndex(timeline, true)).toBe(2);
        expect(codingTimelineLiveThoughtIndex(timeline, false)).toBe(-1);
        expect(codingTimelineLiveThoughtIndex([{ kind: 'thinking' }, { kind: 'progress' }], true)).toBe(-1);
        expect(codingTimelineLiveThoughtIndex([], true)).toBe(-1);
        expect(codingTimelineLiveThoughtIndex(undefined, true)).toBe(-1);
    });

    it('keeps a trailing live header for coding tools and ordinary follow-ups', () => {
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在思考',
            liveKind: 'thinking',
            coding: true,
            lastAssistantOwnsLive: true,
            timelineOwnsLiveThought: true,
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在思考',
            liveKind: 'thinking',
            coding: true,
            lastAssistantOwnsLive: false,
            lastMessageRole: 'user',
            timelineOwnsLiveThought: false,
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在访问模型',
            liveKind: 'accessing_model',
            coding: true,
            lastAssistantOwnsLive: false,
            lastMessageRole: 'user',
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在编辑文件',
            liveKind: 'editing_file',
            coding: true,
            lastAssistantOwnsLive: true,
            timelineOwnsLiveThought: false,
        })).toBe('正在编辑文件');
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在调用工具',
            liveKind: 'calling_tool',
            coding: true,
            lastAssistantOwnsLive: true,
            timelineOwnsLiveThought: true,
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在思考',
            coding: true,
            lastAssistantOwnsLive: false,
            lastMessageRole: 'user',
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在思考',
            liveKind: 'thinking',
            coding: false,
            lastAssistantOwnsLive: true,
            lastMessageRole: 'assistant',
        })).toBeUndefined();
        expect(resolveStandaloneLiveActivityLabel({
            liveLabel: '正在思考',
            liveKind: 'thinking',
            coding: false,
            lastAssistantOwnsLive: false,
            lastMessageRole: 'user',
        })).toBe('正在思考');
        expect(isGenericCodingLiveKind('thinking')).toBe(true);
        expect(isGenericCodingLiveKind('editing_file')).toBe(false);
    });
});
