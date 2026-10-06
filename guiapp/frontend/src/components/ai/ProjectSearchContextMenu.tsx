import { ArchiveProject, DeleteTask, PinTask } from "../../../wailsjs/go/main/App";
import type { Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";
import { expertIDFromTaskTags, purgeDeletedExpertTabLocalCache, purgeDeletedProjectTabLocalCache } from "./aiAssistantPanelSessionUtils";
import { useDialog } from "../CustomDialog";
import type { ProjectSearchItem } from "./projectSearchTypes";

export function ProjectSearchContextMenu({ ctxMenu, lang, theme: t, refreshResults, setCtxMenu, setRenamingPath, setRenameVal, onCloseProjectTab }: {
    ctxMenu: { x: number; y: number; item: ProjectSearchItem }; lang: string; theme: Theme; refreshResults: () => void; setCtxMenu: (menu: null) => void; setRenamingPath: (path: string | null) => void; setRenameVal: (value: string) => void; onCloseProjectTab?: (projectPath: string) => void;
}) {
    const { showConfirm } = useDialog();
    const removeTask = async () => {
        console.info("[ProjectSearch] deleting task", { projectPath: ctxMenu.item.project_path });
        try {
            await DeleteTask(ctxMenu.item.project_path);
            purgeDeletedProjectTabLocalCache(ctxMenu.item.project_path);
            const expertID = expertIDFromTaskTags(ctxMenu.item.tags);
            if (expertID) purgeDeletedExpertTabLocalCache(expertID);
            // The backend emits project-task:closed after deletion. Keep this
            // direct close for hosts where runtime events are unavailable.
            onCloseProjectTab?.(ctxMenu.item.project_path);
            refreshResults();
            setCtxMenu(null);
        } catch (error) {
            console.error("[ProjectSearch] DeleteTask failed:", error);
        }
    };
    const actions = [
        { label: localizeText(lang, "Rename", "\u91cd\u547d\u540d"), icon: "edit", action: () => { setRenamingPath(ctxMenu.item.project_path); setRenameVal(ctxMenu.item.name || ""); setCtxMenu(null); } },
        { label: ctxMenu.item.pinned ? localizeText(lang, "Unpin", "\u53d6\u6d88\u7f6e\u9876") : localizeText(lang, "Pin", "\u7f6e\u9876"), icon: "pin", action: async () => { await PinTask(ctxMenu.item.project_path, !ctxMenu.item.pinned); refreshResults(); setCtxMenu(null); } },
        { label: localizeText(lang, "Remove", "\u79fb\u9664"), icon: "x", action: removeTask },
        { label: localizeText(lang, "Archive", "\u5f52\u6863"), icon: "ARC", action: async () => { setCtxMenu(null); const confirmed = await showConfirm(localizeText(lang, "After archiving, you will not be able to continue this task, but the experience will be preserved. Confirm archive?", "\u5f52\u6863\u540e\u5c06\u65e0\u6cd5\u7ee7\u7eed\u6b64\u4efb\u52a1\uff0c\u4f46\u7ecf\u9a8c\u4f1a\u88ab\u4fdd\u7559\u3002\u786e\u8ba4\u5f52\u6863\uff1f"), localizeText(lang, "Archive task", "\u5f52\u6863\u4efb\u52a1"), { confirmText: localizeText(lang, "Archive", "\u5f52\u6863"), cancelText: localizeText(lang, "Cancel", "\u53d6\u6d88") }); if (!confirmed) return; try { console.info("[ProjectSearch] archiving task", { projectPath: ctxMenu.item.project_path }); await ArchiveProject(ctxMenu.item.project_path); onCloseProjectTab?.(ctxMenu.item.project_path); refreshResults(); } catch (error) { console.error("[ProjectSearch] ArchiveProject failed:", error); } } },
    ];
    return <><div className="psp-ctx-backdrop" onClick={() => setCtxMenu(null)} /><div style={{ position: "fixed", left: ctxMenu.x, top: ctxMenu.y, zIndex: 9999, background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`, borderRadius: "6px", boxShadow: "0 4px 12px rgba(0,0,0,0.15)", padding: "4px 0", minWidth: "120px" }}>{actions.map(item => <div key={item.label} onClick={item.action} style={{ display: "flex", alignItems: "center", gap: "6px", padding: "6px 12px", cursor: "pointer", fontSize: "12px", color: t.text, transition: "background 0.1s" }} onMouseEnter={event => (event.currentTarget.style.background = t.codeBlockBg)} onMouseLeave={event => (event.currentTarget.style.background = "transparent")}><span className="psp-ctx-icon">{item.icon}</span><span>{item.label}</span></div>)}</div></>;
}
