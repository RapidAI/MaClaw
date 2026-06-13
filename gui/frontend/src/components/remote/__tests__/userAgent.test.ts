import { describe, expect, it } from 'vitest';

import {
    commitCustomAgentValue,
    customAgentSeedForProvider,
    editableCustomAgentValue,
    effectiveAgentType,
    isKnownUserAgent,
} from '../userAgent';
import type { LLMProvider } from '../LLMConfigPanelShared';

const provider = (patch: Partial<LLMProvider>): LLMProvider => ({
    name: 'Custom1',
    url: '',
    key: '',
    model: '',
    ...patch,
});

describe('userAgent helpers', () => {
    describe('effectiveAgentType', () => {
        it('defaults to openclaw when no agent_type is set', () => {
            expect(effectiveAgentType(provider({}))).toBe('openclaw');
            expect(effectiveAgentType(undefined)).toBe('openclaw');
        });

        it('returns trimmed agent_type when set', () => {
            expect(effectiveAgentType(provider({ agent_type: '  my-agent  ' }))).toBe('my-agent');
        });

        it('returns tigerclaw for CodeGen SSO provider', () => {
            expect(effectiveAgentType(provider({ name: 'CodeGen', auth_type: 'sso' }))).toBe('tigerclaw');
        });
    });

    describe('isKnownUserAgent', () => {
        it('recognises all known agents', () => {
            expect(isKnownUserAgent('openclaw')).toBe(true);
            expect(isKnownUserAgent('Claude Code')).toBe(true);
            expect(isKnownUserAgent('Cline')).toBe(true);
            expect(isKnownUserAgent('OpenCode')).toBe(true);
            expect(isKnownUserAgent('Roo Code')).toBe(true);
            expect(isKnownUserAgent('Kilo Code')).toBe(true);
            expect(isKnownUserAgent('Cursor')).toBe(true);
            expect(isKnownUserAgent('Crush')).toBe(true);
            expect(isKnownUserAgent('Goose')).toBe(true);
            expect(isKnownUserAgent('claude code 2.0')).toBe(true);
            expect(isKnownUserAgent('tigerclaw')).toBe(true);
        });

        it('recognises legacy agent aliases', () => {
            expect(isKnownUserAgent('opencode')).toBe(true);
            expect(isKnownUserAgent('claude-code/2.0.0')).toBe(true);
        });

        it('rejects unknown values', () => {
            expect(isKnownUserAgent('')).toBe(false);
            expect(isKnownUserAgent('custom-client')).toBe(false);
            expect(isKnownUserAgent('my-agent')).toBe(false);
        });
    });

    describe('editableCustomAgentValue', () => {
        it('returns empty string when agent_type is empty (no flicker on full deletion)', () => {
            expect(editableCustomAgentValue(provider({ agent_type: '' }))).toBe('');
        });

        it('preserves raw value including leading/trailing spaces', () => {
            expect(editableCustomAgentValue(provider({ agent_type: '  my-agent  ' }))).toBe('  my-agent  ');
        });

        it('seeds when agent_type is a known value (user just clicked Custom button)', () => {
            expect(editableCustomAgentValue(provider({ agent_type: 'openclaw' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'OpenCode' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'opencode' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'Kilo Code' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'Cursor' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'tigerclaw' }))).toBe('custom-client');
        });

        it('seeds to custom value when agent_type is absent', () => {
            expect(editableCustomAgentValue(provider({}))).toBe('custom-client');
            expect(editableCustomAgentValue(undefined)).toBe('custom-client');
        });

        it('seeds to tigerclaw-derived seed for CodeGen SSO provider', () => {
            // CodeGen SSO: effectiveAgentType → 'tigerclaw' (known) → seed → 'custom-client'
            const codegen = provider({ name: 'CodeGen', auth_type: 'sso' });
            expect(editableCustomAgentValue(codegen)).toBe('custom-client');
        });
    });

    describe('commitCustomAgentValue', () => {
        it('trims non-empty values', () => {
            const p = provider({ agent_type: 'my-agent' });
            expect(commitCustomAgentValue(p, 'my-agent')).toBe('my-agent');
            expect(commitCustomAgentValue(p, '  trimmed  ')).toBe('trimmed');
        });

        it('falls back to customAgentSeedForProvider when value is blank', () => {
            const custom = provider({ agent_type: '  my-agent  ' });
            expect(commitCustomAgentValue(custom, '')).toBe('my-agent');
            expect(commitCustomAgentValue(custom, '   ')).toBe('my-agent');
        });

        it('seeds to custom value when provider has no custom agent and value is blank', () => {
            expect(commitCustomAgentValue(provider({}), '')).toBe('custom-client');
        });
    });

    describe('customAgentSeedForProvider', () => {
        it('returns existing custom value when already a custom agent', () => {
            expect(customAgentSeedForProvider(provider({ agent_type: 'my-agent' }))).toBe('my-agent');
        });

        it('returns "custom-client" when current effective agent is a known value', () => {
            expect(customAgentSeedForProvider(provider({ agent_type: 'openclaw' }))).toBe('custom-client');
            expect(customAgentSeedForProvider(provider({ agent_type: 'OpenCode' }))).toBe('custom-client');
            expect(customAgentSeedForProvider(provider({ agent_type: 'opencode' }))).toBe('custom-client');
            expect(customAgentSeedForProvider(provider({ agent_type: 'Kilo Code' }))).toBe('custom-client');
            expect(customAgentSeedForProvider(provider({ agent_type: 'Cursor' }))).toBe('custom-client');
            expect(customAgentSeedForProvider(provider({}))).toBe('custom-client');
        });
    });
});
