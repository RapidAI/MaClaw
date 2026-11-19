import { useEffect, useRef } from "react";

/**
 * Deep link from the agent model menu ("添加自定义模型"): auto-open the
 * provider-management dialog once per delivered seq. openDialog snapshots the
 * loaded provider list, so the open waits until the list is ready; a load
 * failure keeps the seq armed and the panel's error strip with Retry on
 * screen instead. Kept out of LLMConfigPanel to honor its line-count guard.
 */
export function useProviderDialogDeepLink(seq: number, providerListReady: boolean, openDialog: () => void): void {
    const consumedSeqRef = useRef(0);
    useEffect(() => {
        if (!seq || seq === consumedSeqRef.current || !providerListReady) return;
        consumedSeqRef.current = seq;
        openDialog();
    }, [seq, providerListReady, openDialog]);
}
