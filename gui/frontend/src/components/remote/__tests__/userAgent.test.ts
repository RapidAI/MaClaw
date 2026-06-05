import { describe, expect, it } from 'vitest';

import {
    customAgentSeedForProvider,
    editableCustomAgentValue,
    effectiveAgentType,
    isKnownUserAgent,
    nextCustomAgentValue,
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
    it('defaults CodeGen SSO to tigerclaw and treats it as known', () => {
        const codegen = provider({ name: 'CodeGen', auth_type: 'sso' });

        expect(effectiveAgentType(codegen)).toBe('tigerclaw');
        expect(isKnownUserAgent('tigerclaw')).toBe(true);
        expect(editableCustomAgentValue(codegen)).toBe('custom-client');
    });

    it('preserves custom input while avoiding blank custom values', () => {
        const custom = provider({ agent_type: '  my-agent  ' });

        expect(effectiveAgentType(custom)).toBe('my-agent');
        expect(editableCustomAgentValue(custom)).toBe('  my-agent  ');
        expect(customAgentSeedForProvider(custom)).toBe('my-agent');
        expect(nextCustomAgentValue(custom, '')).toBe('my-agent');
        expect(nextCustomAgentValue(custom, 'next-agent')).toBe('next-agent');
    });
});
