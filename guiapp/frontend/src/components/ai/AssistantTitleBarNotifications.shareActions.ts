import { useCallback, useRef, useState } from "react";
import { companionHostTransferErrorText, companionShareOpenErrorText } from "./AssistantTitleBarNotifications.companionShare";
import { tailText } from "../../i18n/mnTail"

/** The Wails bindings live on window.go.main.App; a stale webview may lack them. */
function appBinding(name: string): ((arg: string) => Promise<unknown>) | null {
    const app = (window as unknown as {
        go?: { main?: { App?: Record<string, unknown> } };
    }).go?.main?.App;
    const binding = app?.[name];
    return typeof binding === "function" ? binding as (arg: string) => Promise<unknown> : null;
}

/** A webview older than the binding can only be fixed by updating the app. */
function staleBindingErrorText(lang: string, action: "share" | "hostTransfer"): string {
    const isZh = lang.startsWith("zh");
    if (action === "share") {
        return tailText(lang, "This version cannot open shares. Please update the app.", "当前版本无法打开分享，请更新应用");
    }
    return tailText(lang, "This version cannot handle host transfers. Please update the app.", "当前版本无法处理主持人转移，请更新应用");
}

export type ShareActions = {
    shareOpenBusy: boolean;
    shareOpenError: string;
    hostTransferBusy: boolean;
    hostTransferError: string;
    /** Clears both errors; called when the panel switches notices or closes. */
    resetShareErrors: () => void;
    openSharedDocument: (token: string) => Promise<void>;
    handleHostTransfer: (token: string, decision: "accept" | "decline") => Promise<void>;
};

/**
 * The share/host-transfer actions of a notification detail.
 *
 * Both calls end the notice: the file companion is already in front, and an
 * accepted or declined transfer spends the request. `dismissNotice` is the
 * host's way of closing the panel after a successful call.
 */
export function useShareActions(lang: string, dismissNotice: () => void): ShareActions {
    const [shareOpenBusy, setShareOpenBusy] = useState(false);
    const [shareOpenError, setShareOpenError] = useState("");
    const [hostTransferBusy, setHostTransferBusy] = useState(false);
    const [hostTransferError, setHostTransferError] = useState("");
    // Refs, not state: a second click before re-render must not start a
    // duplicate call.
    const shareOpenBusyRef = useRef(false);
    const hostTransferBusyRef = useRef(false);

    const resetShareErrors = useCallback(() => {
        setShareOpenError("");
        setHostTransferError("");
    }, []);

    const openSharedDocument = useCallback(async (token: string) => {
        if (!token || shareOpenBusyRef.current) return;
        shareOpenBusyRef.current = true;
        setShareOpenBusy(true);
        setShareOpenError("");
        try {
            const binding = appBinding("OpenFileCompanionShareFromNotification");
            if (!binding) throw new Error(staleBindingErrorText(lang, "share"));
            await binding(token);
            // The companion is in front now; clear the panel so the main
            // window does not sit over it.
            dismissNotice();
        } catch (err) {
            setShareOpenError(companionShareOpenErrorText(err, lang));
        } finally {
            shareOpenBusyRef.current = false;
            setShareOpenBusy(false);
        }
    }, [dismissNotice, lang]);

    const handleHostTransfer = useCallback(async (token: string, decision: "accept" | "decline") => {
        if (!token || hostTransferBusyRef.current) return;
        hostTransferBusyRef.current = true;
        setHostTransferBusy(true);
        setHostTransferError("");
        try {
            const binding = appBinding(decision === "accept" ? "AcceptFileCompanionShareHost" : "DeclineFileCompanionShareHost");
            if (!binding) throw new Error(staleBindingErrorText(lang, "hostTransfer"));
            await binding(token);
            // The seat moved (or the request was declined); the notice is spent.
            dismissNotice();
        } catch (err) {
            setHostTransferError(companionHostTransferErrorText(err, lang));
        } finally {
            hostTransferBusyRef.current = false;
            setHostTransferBusy(false);
        }
    }, [dismissNotice, lang]);

    return {
        shareOpenBusy,
        shareOpenError,
        hostTransferBusy,
        hostTransferError,
        resetShareErrors,
        openSharedDocument,
        handleHostTransfer,
    };
}
