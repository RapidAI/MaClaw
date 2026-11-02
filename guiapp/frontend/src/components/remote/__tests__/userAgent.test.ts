import { describe, expect, it } from 'vitest';

import {
    CODEGEN_USER_AGENT,
    KNOWN_USER_AGENTS,
    agentIdentityUnchanged,
    canonicalAgentType,
    commitCustomAgentValue,
    customAgentSeedForProvider,
    withLiveAgentType,
    editableCustomAgentValue,
    effectiveAgentType,
    explicitCustomAgentValue,
    isExplicitCustomAgent,
    isKnownUserAgent,
    selectableAgentType,
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
    it('does not render the duplicate Claude Code chip', () => {
        expect(KNOWN_USER_AGENTS).not.toContain('Claude Code');
        expect(KNOWN_USER_AGENTS).toContain('claude code 2.0');
    });

    describe('effectiveAgentType', () => {
        it('defaults to openclaw when no agent_type is set', () => {
            expect(effectiveAgentType(provider({}))).toBe('openclaw');
            expect(effectiveAgentType(undefined)).toBe('openclaw');
        });

        it('returns trimmed agent_type when set', () => {
            expect(effectiveAgentType(provider({ agent_type: '  my-agent  ' }))).toBe('my-agent');
        });

        it('returns QAgent for CodeGen SSO provider', () => {
            expect(effectiveAgentType(provider({ name: 'CodeGen', auth_type: 'sso' }))).toBe(CODEGEN_USER_AGENT);
        });

        it('uses each imported agent identity', () => {
            expect(effectiveAgentType(provider({ import_source: 'codex' }))).toBe('Codex');
            expect(effectiveAgentType(provider({ import_source: 'claude_code' }))).toBe('Claude Code');
            expect(effectiveAgentType(provider({ import_source: 'opencode' }))).toBe('OpenCode');
        });

        it('uses OpenCode identity for the builtin OpenCode provider', () => {
            expect(effectiveAgentType(provider({ name: 'OpenCode' }))).toBe('OpenCode');
        });
    });

    describe('selectableAgentType', () => {
        it('maps the hidden legacy Claude Code value to the visible canonical chip', () => {
            expect(selectableAgentType(provider({ agent_type: 'Claude Code' }))).toBe('claude code 2.0');
            expect(selectableAgentType(provider({ agent_type: 'Cline' }))).toBe('Cline');
        });

        it('maps the legacy tigerclaw identity to the QAgent chip', () => {
            expect(selectableAgentType(provider({ agent_type: 'tigerclaw' }))).toBe(CODEGEN_USER_AGENT);
            expect(selectableAgentType(provider({ agent_type: 'TigerClaw' }))).toBe(CODEGEN_USER_AGENT);
            expect(selectableAgentType(provider({ agent_type: CODEGEN_USER_AGENT }))).toBe(CODEGEN_USER_AGENT);
        });

        it('selects the preset chip for case and legacy spellings', () => {
            expect(canonicalAgentType('cursor')).toBe('Cursor');
            expect(canonicalAgentType('  OPENCLAW ')).toBe('openclaw');
            expect(selectableAgentType(provider({ agent_type: 'opencode' }))).toBe('OpenCode');
            expect(selectableAgentType(provider({ agent_type: 'claude-code/2.0.0' }))).toBe('claude code 2.0');
        });
    });

    describe('isKnownUserAgent', () => {
        it('recognises all known agents', () => {
            expect(isKnownUserAgent('openclaw')).toBe(true);
            expect(isKnownUserAgent('Claude Code')).toBe(true);
            expect(isKnownUserAgent('Cline')).toBe(true);
            expect(isKnownUserAgent('OpenCode')).toBe(true);
            expect(isKnownUserAgent('Codex')).toBe(true);
            expect(isKnownUserAgent('Roo Code')).toBe(true);
            expect(isKnownUserAgent('Kilo Code')).toBe(true);
            expect(isKnownUserAgent('Cursor')).toBe(true);
            expect(isKnownUserAgent('Crush')).toBe(true);
            expect(isKnownUserAgent('Goose')).toBe(true);
            expect(isKnownUserAgent('claude code 2.0')).toBe(true);
            expect(isKnownUserAgent(CODEGEN_USER_AGENT)).toBe(true);
            expect(isKnownUserAgent('qagent')).toBe(true);
            expect(isKnownUserAgent('tigerclaw')).toBe(true);
            expect(isKnownUserAgent('TigerClaw')).toBe(true);
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

    describe('isExplicitCustomAgent', () => {
        it('is false for an empty default and for preset identities', () => {
            expect(isExplicitCustomAgent(provider({}))).toBe(false);
            expect(isExplicitCustomAgent(provider({ agent_type: '' }))).toBe(false);
            expect(isExplicitCustomAgent(provider({ agent_type: 'openclaw' }))).toBe(false);
            expect(isExplicitCustomAgent(provider({ agent_type: 'Claude Code' }))).toBe(false);
            expect(isExplicitCustomAgent(provider({ agent_type: 'tigerclaw' }))).toBe(false);
        });

        it('is true for a typed identity such as workbuddy', () => {
            expect(isExplicitCustomAgent(provider({ agent_type: 'workbuddy' }))).toBe(true);
            expect(isExplicitCustomAgent(provider({ agent_type: '  my-agent  ' }))).toBe(true);
        });
    });

    describe('withLiveAgentType', () => {
        const row = (agentType: string, passed = true) => ({
            name: 'Custom1',
            agent_type: agentType,
            connection_test_passed: passed,
        });

        it('leaves the list untouched when the live agent is the stored or default identity', () => {
            const implicit = [{ name: 'Custom1', agent_type: '', connection_test_passed: true }];
            expect(withLiveAgentType(implicit, 0, 'openclaw')).toBe(implicit);
            const saved = [row('workbuddy')];
            expect(withLiveAgentType(saved, 0, 'workbuddy')).toBe(saved);
            const cased = [row('cursor')];
            expect(withLiveAgentType(cased, 0, 'Cursor')).toBe(cased);
        });

        it('leaves a provider default and a display alias untouched', () => {
            const claude = [{ name: 'Custom1', import_source: 'claude_code', agent_type: '', connection_test_passed: true }];
            expect(withLiveAgentType(claude, 0, 'claude code 2.0')).toBe(claude);
            const codegen = [{ name: 'CodeGen', auth_type: 'sso', agent_type: '', connection_test_passed: true }];
            expect(withLiveAgentType(codegen, 0, 'QAgent')).toBe(codegen);
            const alias = [row('Claude Code')];
            expect(withLiveAgentType(alias, 0, 'claude code 2.0')).toBe(alias);
            expect(agentIdentityUnchanged({ name: 'Custom1', agent_type: 'cursor' }, 'Cursor')).toBe(true);
        });

        it('applies a real identity change and requires a new connection test', () => {
            const saved = [row('workbuddy')];
            expect(withLiveAgentType(saved, 0, 'myagent')).toEqual([
                { name: 'Custom1', agent_type: 'myagent', connection_test_passed: false },
            ]);
        });
    });

    describe('explicitCustomAgentValue', () => {
        it('returns the trimmed custom identity and nothing for a preset', () => {
            expect(explicitCustomAgentValue(provider({ agent_type: '  workbuddy  ' }))).toBe('workbuddy');
            expect(explicitCustomAgentValue(provider({ agent_type: 'openclaw' }))).toBe('');
            expect(explicitCustomAgentValue(provider({ agent_type: '' }))).toBe('');
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
            expect(editableCustomAgentValue(provider({ agent_type: 'QAgent' }))).toBe('custom-client');
            expect(editableCustomAgentValue(provider({ agent_type: 'tigerclaw' }))).toBe('custom-client');
        });

        it('seeds to custom value when agent_type is absent', () => {
            expect(editableCustomAgentValue(provider({}))).toBe('custom-client');
            expect(editableCustomAgentValue(undefined)).toBe('custom-client');
        });

        it('seeds to QAgent-derived seed for CodeGen SSO provider', () => {
            // CodeGen SSO: effectiveAgentType → 'QAgent' (known) → seed → 'custom-client'
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
