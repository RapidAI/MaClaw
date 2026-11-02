// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { LLMProvider } from '../LLMConfigPanelShared';
import { useCustomUserAgentField } from '../useCustomUserAgentField';

const provider = (agentType: string): LLMProvider => ({
    name: 'WorkBuddy 国内版',
    url: '',
    key: '',
    model: '',
    agent_type: agentType,
});

describe('useCustomUserAgentField', () => {
    it('keeps a cleared field editable without persisting partial keystrokes', () => {
        let current = provider('workbuddy');
        const commits: string[] = [];
        const { result, rerender } = renderHook(() => useCustomUserAgentField(current, 0));
        const commit = (value: string) => {
            commits.push(value);
            current = provider(value);
        };

        act(() => result.current.onInputChange('w', commit));
        expect(result.current.inputValue).toBe('w');
        expect(result.current.agentForRequest).toBe('w');
        expect(result.current.pending).toBe(true);
        expect(commits).toEqual([]);

        rerender();
        act(() => result.current.onInputChange('', commit));
        expect(result.current.isCustom).toBe(true);
        expect(result.current.inputValue).toBe('');
        expect(result.current.agentForRequest).toBe('workbuddy');
        expect(result.current.pending).toBe(false);

        rerender();
        act(() => result.current.onInputBlur('', commit));
        expect(commits).toEqual([]);
        expect(result.current.inputValue).toBe('workbuddy');
        expect(result.current.isCustom).toBe(true);
    });

    it('does not bring a cleared draft back after switching providers', () => {
        const { result, rerender } = renderHook(
            ({ scope, agentType }: { scope: number; agentType: string }) => useCustomUserAgentField(provider(agentType), scope),
            { initialProps: { scope: 0, agentType: 'workbuddy' } },
        );

        act(() => {
            result.current.onInputChange('', () => {});
        });
        expect(result.current.inputValue).toBe('');

        rerender({ scope: 1, agentType: 'openclaw' });
        expect(result.current.isCustom).toBe(false);
        expect(result.current.inputValue).not.toBe('');

        rerender({ scope: 0, agentType: 'workbuddy' });
        expect(result.current.inputValue).toBe('workbuddy');
        expect(result.current.isCustom).toBe(true);
    });

    it('stores a case-insensitive preset as its chip label without closing the field mid-edit', () => {
        let current = provider('workbuddy');
        const commits: string[] = [];
        const { result, rerender } = renderHook(() => useCustomUserAgentField(current, 0));

        act(() => {
            result.current.onInputChange('cursor', value => {
                commits.push(value);
                current = provider(value);
            });
        });
        expect(result.current.inputValue).toBe('cursor');
        expect(result.current.agentForRequest).toBe('Cursor');
        expect(result.current.isCustom).toBe(true);
        expect(commits).toEqual([]);

        rerender();
        act(() => {
            result.current.onInputBlur('cursor', value => {
                commits.push(value);
                current = provider(value);
            });
        });
        rerender();
        expect(commits).toEqual(['Cursor']);
        expect(result.current.isCustom).toBe(false);
        expect(result.current.presetActive('Cursor')).toBe(true);
    });

    it('leaves a typed preset name on that chip after blur', () => {
        let current = provider('workbuddy');
        const { result, rerender } = renderHook(() => useCustomUserAgentField(current, 0));

        act(() => {
            result.current.onInputChange('Cursor', value => {
                current = provider(value);
            });
        });
        rerender();
        expect(result.current.isCustom).toBe(true);
        expect(result.current.presetActive('Cursor')).toBe(false);

        act(() => {
            result.current.onInputBlur('Cursor', value => {
                current = provider(value);
            });
        });
        rerender();
        expect(result.current.isCustom).toBe(false);
        expect(result.current.presetActive('Cursor')).toBe(true);
    });

    it('keeps the identity saved during the edit when the field is cleared', () => {
        let current = provider('workbuddy');
        const commits: string[] = [];
        const { result, rerender } = renderHook(() => useCustomUserAgentField(current, 0));

        act(() => {
            result.current.onInputChange('myagent', value => {
                commits.push(value);
                current = provider(value);
            });
        });
        current = provider('myagent');
        rerender();

        act(() => result.current.onInputChange('', () => {}));
        expect(result.current.inputValue).toBe('');
        expect(result.current.agentForRequest).toBe('myagent');
        expect(result.current.pending).toBe(false);

        act(() => result.current.onInputBlur('', value => {
            commits.push(value);
            current = provider(value);
        }));
        rerender();
        expect(commits).toEqual([]);
        expect(result.current.inputValue).toBe('myagent');
    });

    it('does not rewrite a chip that is already the stored identity', () => {
        let current = provider('cursor');
        const commits: string[] = [];
        const { result } = renderHook(() => useCustomUserAgentField(current, 0));

        act(() => {
            result.current.selectPreset('Cursor', value => {
                commits.push(value);
                current = provider(value);
            });
        });
        expect(commits).toEqual([]);
        expect(result.current.pending).toBe(false);
        expect(result.current.presetActive('Cursor')).toBe(true);

        act(() => {
            result.current.selectPreset('Cline', value => {
                commits.push(value);
                current = provider(value);
            });
        });
        expect(commits).toEqual(['Cline']);
    });

    it('opens Custom without storing a placeholder identity', () => {
        const commits: string[] = [];
        const { result } = renderHook(() => useCustomUserAgentField(provider('openclaw'), 0));
        const commit = (value: string) => commits.push(value);

        act(() => result.current.selectCustom(commit));
        expect(commits).toEqual([]);
        expect(result.current.isCustom).toBe(true);
        expect(result.current.inputValue).toBe('');
        expect(result.current.pending).toBe(false);
        expect(result.current.agentForRequest).toBe('openclaw');

        act(() => result.current.onInputBlur('', commit));
        expect(commits).toEqual([]);
        expect(result.current.isCustom).toBe(false);
        expect(result.current.presetActive('openclaw')).toBe(true);

        act(() => result.current.selectCustom(commit));
        act(() => result.current.onInputChange('myagent', commit));
        expect(result.current.pending).toBe(true);
        act(() => result.current.onInputBlur('myagent', commit));
        expect(commits).toEqual(['myagent']);
    });

    it('does not persist a partial value when Custom is clicked again', () => {
        const commits: string[] = [];
        const { result } = renderHook(() => useCustomUserAgentField(provider('workbuddy'), 0));

        act(() => result.current.onInputChange('w', value => commits.push(value)));
        act(() => result.current.selectCustom(value => commits.push(value)));
        expect(commits).toEqual([]);
        expect(result.current.inputValue).toBe('w');
        expect(result.current.agentForRequest).toBe('w');
    });

    it('drops a draft when the stored identity changes underneath it', () => {
        const { result, rerender } = renderHook(
            ({ agentType }: { agentType: string }) => useCustomUserAgentField(provider(agentType), 0),
            { initialProps: { agentType: 'workbuddy' } },
        );

        act(() => result.current.onInputChange('temp-agent', () => {}));
        expect(result.current.inputValue).toBe('temp-agent');
        expect(result.current.agentForRequest).toBe('temp-agent');

        rerender({ agentType: 'claude code 2.0' });
        expect(result.current.inputValue).not.toBe('temp-agent');
        expect(result.current.isCustom).toBe(false);
        expect(result.current.presetActive('claude code 2.0')).toBe(true);
        expect(result.current.agentForRequest).toBe('claude code 2.0');
    });
});
