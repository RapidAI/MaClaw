import { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { PatchConfigFields } from '../../../wailsjs/go/main/App';
import { corelib, main } from '../../../wailsjs/go/models';

/** Typed config accessor to reduce `as any` casts. */
export const cfgVal = <T,>(config: corelib.AppConfig | null, key: string, fallback: T): T => {
    if (!config) return fallback;
    const val = (config as unknown as Record<string, unknown>)[key];
    return (val === undefined || val === null) ? fallback : val as T;
};

/**
 * Patch config with optimistic update + stale-response protection.
 * Uses a monotonic version counter so overlapping patches cannot clobber newer state.
 */
export const saveConfigPatch = (
    config: corelib.AppConfig | null,
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>,
    patch: Record<string, any>,
    versionRef: MutableRefObject<number>,
) => {
    if (!config) return;
    const myVersion = ++versionRef.current;
    const next = new corelib.AppConfig({ ...config, ...patch } as any);
    setConfig(next);
    PatchConfigFields(patch).then((saved) => {
        if (myVersion === versionRef.current) {
            setConfig(new corelib.AppConfig(saved));
        }
    }).catch((err) => {
        console.error('Failed to patch programming tool settings:', err);
        if (myVersion === versionRef.current) {
            setConfig(config);
        }
    });
};
