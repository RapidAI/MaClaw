import { useCallback, useEffect, useRef, useState } from 'react';

export type GitRepoNameStatusKind = 'idle' | 'checking' | 'ok' | 'taken' | 'invalid';

export type GitRepoNameState = {
    status: GitRepoNameStatusKind;
    // reason carries the backend's taken-note (or is empty when the caller
    // renders its own localized label for the status).
    reason: string;
};

const DEBOUNCE_MS = 500;

// gitRepoNameShapeValid mirrors the backend's path-segment pre-check
// (oauth.GitHubRepoNameShape): letters, digits, dot, underscore, hyphen;
// 1-200 chars; not "." or "..". The provider stays the authority — this only
// catches obvious typos before a probe.
export function gitRepoNameShapeValid(name: string): boolean {
    const trimmed = name.trim();
    if (!trimmed || trimmed === '.' || trimmed === '..' || trimmed.length > 200) return false;
    return /^[A-Za-z0-9._-]+$/.test(trimmed);
}

// useGitRepoNameStatus debounces GitPlatformRepoNameStatus probes for the
// repository-creation panels. A probe fires only after DEBOUNCE_MS of quiet
// for a syntactically valid name under a resolved namespace; generation
// guards keep a slow answer from overwriting a newer input's state. API
// failures surface through `error` — a broken probe must never read as
// "available".
export function useGitRepoNameStatus(input: {
    service: string;
    credentialId: string;
    owner: string;
    name: string;
    enabled: boolean;
}): { state: GitRepoNameState; error: string; recheck: () => void } {
    const { service, credentialId, owner, name, enabled } = input;
    const [state, setState] = useState<GitRepoNameState>({ status: 'idle', reason: '' });
    const [error, setError] = useState('');
    const [recheckNonce, setRecheckNonce] = useState(0);
    const generation = useRef(0);

    useEffect(() => {
        const gen = ++generation.current;
        const current = () => gen === generation.current;
        setError('');
        const trimmed = name.trim();
        if (!enabled || !trimmed || !owner.trim()) {
            // No verdict while the namespace is still resolving — "invalid"
            // would misreport a merely-waiting probe.
            setState({ status: 'idle', reason: '' });
            return;
        }
        if (!gitRepoNameShapeValid(trimmed)) {
            setState({ status: 'invalid', reason: '' });
            return;
        }
        const binding = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App?.GitPlatformRepoNameStatus;
        if (typeof binding !== 'function') {
            setState({ status: 'idle', reason: '' });
            return;
        }
        setState({ status: 'checking', reason: '' });
        const timer = setTimeout(() => {
            void (async () => {
                try {
                    const view = await (binding as (service: string, credentialId: string, owner: string, name: string) => Promise<unknown>)(
                        service, credentialId, owner.trim(), trimmed,
                    ) as { available?: boolean; reason?: string };
                    if (!current()) return;
                    if (view?.available) {
                        setState({ status: 'ok', reason: '' });
                    } else {
                        setState({ status: 'taken', reason: String(view?.reason || '') });
                    }
                } catch (err) {
                    if (!current()) return;
                    setError(err instanceof Error ? err.message : String(err));
                    setState({ status: 'idle', reason: '' });
                }
            })();
        }, DEBOUNCE_MS);
        return () => clearTimeout(timer);
    }, [service, credentialId, owner, name, enabled, recheckNonce]);

    const recheck = useCallback(() => setRecheckNonce((nonce) => nonce + 1), []);
    return { state, error, recheck };
}
