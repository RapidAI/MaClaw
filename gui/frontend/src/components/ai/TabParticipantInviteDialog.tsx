import { useEffect, useState } from "react";
import type { AITab } from "./AITabTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { participantAddErrorText } from "./participantAddError";
import { addParticipantIdentityKeys, participantIdentityKeys } from "./participantIdentity";
import type { VirtualEmployeeEntry } from "./VirtualEmployeeTab";
import { virtualEmployeeDisplayName, virtualEmployeeParticipantId } from "./VEGroupChat";
import { isVirtualEmployeeOnline } from "./virtualEmployeeStatus";
import { getWailsAppModule } from "../../utils/wailsAppModule";

type TabParticipantInviteDialogProps = {
    tab: AITab;
    lang?: string;
    theme: Theme;
    onClose: () => void;
    onAddParticipantToTab: (tab: AITab, veId: string, veName: string) => Promise<unknown> | unknown;
};

export function TabParticipantInviteDialog({ tab, lang, theme, onClose, onAddParticipantToTab }: TabParticipantInviteDialogProps) {
    const [available, setAvailable] = useState<VirtualEmployeeEntry[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [addingId, setAddingId] = useState("");
    const isZh = !lang || lang.startsWith("zh");

    useEffect(() => {
        const handler = (event: KeyboardEvent) => {
            if (event.key === "Escape" && !addingId) onClose();
        };
        document.addEventListener("keydown", handler);
        return () => document.removeEventListener("keydown", handler);
    }, [addingId, onClose]);

    useEffect(() => {
        if (tab.readOnly || (!tab.veId && !tab.discussionId)) return;
        let cancelled = false;
        const currentIds = new Set<string>();
        for (const id of tab.participants || [tab.veId]) addParticipantIdentityKeys(currentIds, id);
        setLoading(true);
        setError("");
        getWailsAppModule().then(async (mod) => {
            const listFn = (mod as any).ListVirtualEmployees;
            const detailFn = (mod as any).GroupDiscussionGetConsultationDetail;
            if (tab.discussionId && typeof detailFn === "function") {
                try {
                    const detail = await detailFn(tab.discussionId);
                    for (const id of detail?.discussion?.participant_ids || []) {
                        addParticipantIdentityKeys(currentIds, id);
                    }
                    for (const participant of detail?.session?.participants || []) {
                        addParticipantIdentityKeys(currentIds, participant?.id || participant?.ID);
                    }
                } catch {
                    // Fall back to tab metadata when detail refresh is unavailable.
                }
            }
            const all: VirtualEmployeeEntry[] = typeof listFn === "function" ? await listFn() : [];
            if (cancelled) return;
            setAvailable((all || []).filter((ve) => {
                const keys = participantIdentityKeys(ve.id, ve.machine_id, virtualEmployeeParticipantId(ve));
                return isVirtualEmployeeOnline(ve) && !keys.some((key) => currentIds.has(key));
            }));
        }).catch(() => {
            if (!cancelled) {
                setError(isZh ? "\u83b7\u53d6\u5217\u8868\u5931\u8d25" : "Failed to load");
                setAvailable([]);
            }
        }).finally(() => {
            if (!cancelled) setLoading(false);
        });
        return () => { cancelled = true; };
    }, [isZh, tab]);

    if (tab.readOnly || (!tab.veId && !tab.discussionId)) return null;

    return (
        <div data-testid="tab-participant-invite-dialog" role="dialog" aria-modal="true" onMouseDown={() => { if (!addingId) onClose(); }} style={{ position: "absolute", inset: 0, zIndex: 10000, background: "rgba(0,0,0,0.08)", display: "flex", alignItems: "flex-start", justifyContent: "flex-end", padding: "44px 16px", boxSizing: "border-box" }}>
            <div onMouseDown={(e) => e.stopPropagation()} style={{ width: 260, maxHeight: 320, overflowY: "auto", background: theme.bg, color: theme.text, border: `1px solid ${theme.divider}`, borderRadius: 8, boxShadow: "0 8px 24px rgba(0,0,0,0.18)", padding: 6 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 8px", borderBottom: `1px solid ${theme.divider}`, marginBottom: 4 }}>
                    <span style={{ fontSize: 13, fontWeight: 600 }}>{isZh ? "\u9080\u8bf7\u6570\u5b57\u5458\u5de5" : "Invite digital employee"}</span>
                    <button data-testid="tab-participant-invite-close" type="button" disabled={!!addingId} onClick={onClose} style={{ border: "none", background: "transparent", color: theme.textMuted, cursor: addingId ? "default" : "pointer", fontSize: 16, lineHeight: 1, opacity: addingId ? 0.45 : 1 }}>x</button>
                </div>
                {loading && <div style={{ padding: "10px 8px", fontSize: 12, color: theme.textMuted }}>{isZh ? "\u52a0\u8f7d\u4e2d..." : "Loading..."}</div>}
                {!loading && error && <div data-testid="tab-participant-invite-error" style={{ padding: "10px 8px", fontSize: 12, color: theme.errorText || "#dc2626" }}>{error}</div>}
                {!loading && !error && available.length === 0 && <div data-testid="tab-participant-invite-empty" style={{ padding: "10px 8px", fontSize: 12, color: theme.textMuted }}>{isZh ? "\u6ca1\u6709\u53ef\u6dfb\u52a0\u7684\u6570\u5b57\u5458\u5de5" : "No available digital employees"}</div>}
                {!loading && !error && available.map((ve, index) => {
                    const participantId = virtualEmployeeParticipantId(ve);
                    const displayName = virtualEmployeeDisplayName(ve, index, lang);
                    return <button key={ve.id || participantId} data-testid={`tab-participant-invite-item-${ve.id || participantId}`} type="button" disabled={!!addingId} onClick={async () => {
                        if (addingId) return;
                        setAddingId(participantId);
                        setError("");
                        try {
                            const result = await onAddParticipantToTab(tab, participantId, displayName);
                            if (result === false || result === null) throw new Error("participant_add_failed");
                            onClose();
                        } catch (err) {
                            setError(participantAddErrorText(err, lang));
                        } finally {
                            setAddingId("");
                        }
                    }} style={{ width: "100%", display: "flex", alignItems: "center", gap: 8, padding: "7px 8px", border: "none", background: "transparent", color: theme.text, cursor: addingId ? "default" : "pointer", textAlign: "left", borderRadius: 6, opacity: addingId && addingId !== participantId ? 0.55 : 1 }}>
                        <span style={{ width: 7, height: 7, borderRadius: "50%", background: "#22c55e", flexShrink: 0 }} />
                        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{addingId === participantId ? (isZh ? "\u6dfb\u52a0\u4e2d..." : "Adding...") : displayName}</span>
                    </button>;
                })}
            </div>
        </div>
    );
}
