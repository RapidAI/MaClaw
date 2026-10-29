import { useState, type MouseEvent } from "react";
import { ExportTaskResultFile, FileCompanionImportKnowledge, FileCompanionUploadCloud, OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import { useToast } from "../Toast";
import { companionSay } from "../../i18n/mnCompanion";

function noteMarkdownPath(pdfPath: string): string {
    return pdfPath.toLowerCase().endsWith(".pdf") ? `${pdfPath.slice(0, -4)}.md` : pdfPath;
}

/** Note card: view and save the PDF, import the Markdown, upload the PDF. */
export function NoteResultCard({ filePath, messageId, lang = "en" }: { filePath: string; messageId: string; lang?: string }) {
    const name = filePath.split(/[/\\]/).filter(Boolean).pop() || filePath;
    const markdown = noteMarkdownPath(filePath);
    const { showToast } = useToast();
    const [saveState, setSaveState] = useState<"idle" | "busy" | "done" | "error">("idle");
    const [importBusy, setImportBusy] = useState(false);
    const [uploadBusy, setUploadBusy] = useState(false);
    const open = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (typeof OpenFileOrShowInFolder !== "function") return;
        void OpenFileOrShowInFolder(filePath).catch(() => undefined);
    };
    const save = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (saveState === "busy" || typeof ExportTaskResultFile !== "function") return;
        setSaveState("busy");
        void ExportTaskResultFile(filePath).then((dest) => {
            setSaveState(dest ? "done" : "idle");
        }).catch(() => {
            setSaveState("error");
        });
    };
    const importNote = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (importBusy || typeof FileCompanionImportKnowledge !== "function") return;
        setImportBusy(true);
        void FileCompanionImportKnowledge(markdown).then(() => {
            showToast(companionSay(lang, "已进入桌面知识库"), "success", 6000);
        }).catch((err: unknown) => {
            const raw = err instanceof Error ? err.message.trim() : typeof err === "string" ? err.trim() : "";
            showToast(raw ? companionSay(lang, raw) : companionSay(lang, "导入失败"), "error", 6000);
        }).finally(() => setImportBusy(false));
    };
    const uploadNote = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (uploadBusy || typeof FileCompanionUploadCloud !== "function") return;
        setUploadBusy(true);
        void FileCompanionUploadCloud(filePath).then(() => {
            showToast(companionSay(lang, "已上传到云盘，手机端「文档」可打开"), "success", 6000);
        }).catch((err: unknown) => {
            const raw = err instanceof Error ? err.message.trim() : typeof err === "string" ? err.trim() : "";
            showToast(raw ? companionSay(lang, raw) : companionSay(lang, "上传失败"), "error", 6000);
        }).finally(() => setUploadBusy(false));
    };
    const saveLabel = saveState === "busy"
        ? companionSay(lang, "保存中...")
        : saveState === "done"
            ? companionSay(lang, "已保存")
            : saveState === "error"
                ? companionSay(lang, "保存失败")
                : companionSay(lang, "保存");
    return (
        <div className="mc-task-artifacts" data-testid={`task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__label">{companionSay(lang, "产出物")}</div>
            <div className="mc-task-artifacts__row">
                <a className="mc-task-artifact" href="#" title={filePath} onClick={open}>
                    <span className="mc-task-artifact__name">{name}</span>
                </a>
            </div>
            <div className="mc-task-result-card__actions">
                <button type="button" data-testid="task-result-view-btn" onClick={open}>{companionSay(lang, "查看文档")}</button>
                <button type="button" data-testid="file-companion-paper-save" aria-busy={saveState === "busy"} disabled={saveState === "busy"} onClick={save}>{saveLabel}</button>
                <button type="button" data-testid="file-companion-note-import" aria-busy={importBusy} disabled={importBusy} onClick={importNote}>{importBusy ? companionSay(lang, "正在导入…") : companionSay(lang, "导入知识库")}</button>
                <button type="button" data-testid="file-companion-note-upload" aria-busy={uploadBusy} disabled={uploadBusy} onClick={uploadNote}>{uploadBusy ? companionSay(lang, "正在上传…") : companionSay(lang, "上传到云盘")}</button>
            </div>
        </div>
    );
}
