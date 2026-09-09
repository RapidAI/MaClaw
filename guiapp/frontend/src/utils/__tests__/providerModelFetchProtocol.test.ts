import { describe, expect, it } from 'vitest';
import { inferProviderModelFetchProtocol } from '../providerModelFetchProtocol';

describe('inferProviderModelFetchProtocol', () => {
    it('uses Anthropic for Claude with non-CodeGen endpoints', () => {
        expect(inferProviderModelFetchProtocol('claude', 'https://api.example.com/v1')).toBe('anthropic');
        expect(inferProviderModelFetchProtocol('claude', 'https://api.anthropic.com')).toBe('anthropic');
    });

    it('uses OpenAI for Claude with CodeGen endpoint (hybrid gateway)', () => {
        expect(inferProviderModelFetchProtocol('claude', 'https://codegen.qianxin-inc.cn/api')).toBe('openai');
        expect(inferProviderModelFetchProtocol('claude', 'https://codegen.qianxin-inc.cn/api/v1')).toBe('openai');
    });

    it('uses OpenAI for ordinary OpenAI-compatible endpoints', () => {
        expect(inferProviderModelFetchProtocol('codex', 'https://api.example.com/v1')).toBe('openai');
        expect(inferProviderModelFetchProtocol('codex', 'http://127.0.0.1:9999/openai')).toBe('openai');
    });

    it('detects Anthropic-compatible proxy endpoints by path segment', () => {
        expect(inferProviderModelFetchProtocol('codex', 'http://127.0.0.1:9999/anthropic')).toBe('anthropic');
        expect(inferProviderModelFetchProtocol('codex', 'http://127.0.0.1:9999/anthropic/v1/messages')).toBe('anthropic');
        expect(inferProviderModelFetchProtocol('opencode', '/anthropic/v1')).toBe('anthropic');
    });

    it('detects official Anthropic hosts', () => {
        expect(inferProviderModelFetchProtocol('opencode', 'https://api.anthropic.com')).toBe('anthropic');
    });
});
