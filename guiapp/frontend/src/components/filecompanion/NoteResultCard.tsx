import { useState, type MouseEvent } from "react";
import { ExportTaskResultFile, FileCompanionImportKnowledge, FileCompanionUploadCloud, OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import { useToast } from "../Toast";

function noteMarkdownPath(pdfPath: string): string {
    return pdfPath.toLowerCase().endsWith(".pdf") ? `${pdfPath.slice(0, -4)}.md` : pdfPath;
}

/** Note card: view and save the PDF, import the Markdown, upload the PDF. */
export function NoteResultCard({ filePath, messageId }: { filePath: string; messageId: string }) {
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
            showToast("已进入桌面知识库", "success", 6000);
        }).catch((err: unknown) => {
            showToast(err instanceof Error ? err.message : "导入失败", "error", 6000);
        }).finally(() => setImportBusy(false));
    };
    const uploadNote = (event: MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (uploadBusy || typeof FileCompanionUploadCloud !== "function") return;
        setUploadBusy(true);
        void FileCompanionUploadCloud(filePath).then(() => {
            showToast("已上传到云盘，手机端「文档」可打开", "success", 6000);
        }).catch((err: unknown) => {
            showToast(err instanceof Error ? err.message : "上传失败", "error", 6000);
        }).finally(() => setUploadBusy(false));
    };
    const saveLabel = saveState === "busy" ? "保存中..." : saveState === "done" ? "已保存" : saveState === "error" ? "保存失败" : "保存";
    return (
        <div className="mc-task-artifacts" data-testid={`task-result-card-${messageId}`}>
            <div className="mc-task-artifacts__label">产出物</div>
            <div className="mc-task-artifacts__row">
                <a className="mc-task-artifact" href="#" title={filePath} onClick={open}>
                    <span className="mc-task-artifact__name">{name}</span>
                </a>
            </div>
            <div className="mc-task-result-card__actions">
                <button type="button" data-testid="task-result-view-btn" onClick={open}>查看文档</button>
                <button type="button" data-testid="file-companion-paper-save" aria-busy={saveState === "busy"} disabled={saveState === "busy"} onClick={save}>{saveLabel}</button>
                <button type="button" data-testid="file-companion-note-import" aria-busy={importBusy} disabled={importBusy} onClick={importNote}>{importBusy ? "正在导入…" : "导入知识库"}</button>
                <button type="button" data-testid="file-companion-note-upload" aria-busy={uploadBusy} disabled={uploadBusy} onClick={uploadNote}>{uploadBusy ? "正在上传…" : "上传到云盘"}</button>
            </div>
        </div>
    );
}
