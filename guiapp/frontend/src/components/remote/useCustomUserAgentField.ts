import { useRef, useState } from "react";
import type { LLMProvider } from "./LLMConfigPanelShared";
import {
    agentIdentityUnchanged,
    canonicalAgentType,
    editableCustomAgentValue,
    effectiveAgentType,
    isExplicitCustomAgent,
    selectableAgentType,
} from "./userAgent";

type CustomAgentDraft = { scope: string | number | null; value: string };

/**
 * Custom User-Agent field.
 * An empty agent_type is read as the provider default, which is a known chip,
 * so writing "" would unmount the input. The draft keeps the field mounted
 * and shows the cleared text. Keystrokes stay in the draft so a partial
 * delete does not replace the saved identity or invalidate a connection test.
 * A blank field keeps using the identity currently stored on the provider.
 * Opening Custom on a preset only shows the field; the placeholder is not
 * stored. A new provider identity or scope drops the draft.
 */
export function useCustomUserAgentField(provider: LLMProvider | null | undefined, scopeKey: string | number | null) {
    const [draftState, setDraftState] = useState<CustomAgentDraft | null>(null);
    const storedAgent = provider?.agent_type ?? "";
    const storedAgentRef = useRef(storedAgent);
    const scopeRef = useRef(scopeKey);
    let visibleDraft = draftState;
    if (scopeRef.current !== scopeKey) {
        scopeRef.current = scopeKey;
        storedAgentRef.current = storedAgent;
        visibleDraft = null;
        if (draftState) setDraftState(null);
    } else if (draftState && draftState.scope === scopeKey && storedAgent !== storedAgentRef.current) {
        storedAgentRef.current = storedAgent;
        visibleDraft = null;
        setDraftState(null);
    } else {
        storedAgentRef.current = storedAgent;
    }

    const draft = visibleDraft && visibleDraft.scope === scopeKey ? visibleDraft.value : null;
    const currentAgent = selectableAgentType(provider);
    const isCustom = draft !== null || isExplicitCustomAgent(provider);
    const inputValue = draft !== null ? draft : editableCustomAgentValue(provider);
    const editing = draft !== null;
    const committedAgent = effectiveAgentType(provider);
    const typedAgent = editing ? canonicalAgentType(draft ?? "") : "";
    const agentForRequest = typedAgent || committedAgent;
    const pending = !agentIdentityUnchanged(provider, agentForRequest);

    const write = (value: string, commit: (value: string) => void) => {
        const next = canonicalAgentType(value);
        if (!next || agentIdentityUnchanged(provider, next)) return;
        commit(next);
    };

    const endSession = () => {
        setDraftState(null);
    };

    const selectPreset = (ua: string, commit: (value: string) => void) => {
        endSession();
        write(ua, commit);
    };

    const selectCustom = (commit: (value: string) => void) => {
        // Already custom, including an open draft. Blur and save apply the
        // text; a second click must not persist a placeholder or a partial.
        if (isCustom) {
            void commit;
            return;
        }
        setDraftState({ scope: scopeKey, value: "" });
        void commit;
    };

    const onInputChange = (value: string, commit: (value: string) => void) => {
        setDraftState({ scope: scopeKey, value });
        void commit;
    };

    const onInputBlur = (value: string, commit: (value: string) => void) => {
        const next = value.trim();
        endSession();
        if (!next) return;
        write(next, commit);
    };

    return {
        agentForRequest,
        currentAgent,
        editing,
        isCustom,
        inputValue,
        pending,
        presetActive: (ua: string) => draft === null && currentAgent === ua,
        selectPreset,
        selectCustom,
        onInputChange,
        onInputBlur,
    };
}
