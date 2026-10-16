import React from "react";
import { ExportTaskResultFile } from "../../../wailsjs/go/main/App";
import { localizeText } from "./aiAssistantI18n";
import { localizeTaskResultExportError } from "./taskResultPreview";

function wailsErrorText(err: unknown): string {
    if (err instanceof Error) return err.message;
    if (err && typeof err === "object" && "message" in err) return String((err as { message?: unknown }).message || "");
    return String(err || "");
}

export function TaskResultExportButton({ filePath, lang }: { filePath: string; lang: string }) {
    const [status, setStatus] = React.useState<"idle" | "busy" | "done" | "error">("idle");
    const [errorMsg, setErrorMsg] = React.useState("");
    const [savedPath, setSavedPath] = React.useState("");
    const aliveRef = React.useRef(true);
    const busyRef = React.useRef(false);
    React.useEffect(() => {
        aliveRef.current = true;
        return () => {
            aliveRef.current = false;
        };
    }, []);
    const failLabel = localizeText(lang, "Export failed", "导出失败", "匯出失敗");
    const label = status === "busy"
        ? localizeText(lang, "Exporting...", "导出中...", "匯出中...")
        : status === "done"
            ? localizeText(lang, "Exported", "已导出", "已匯出")
            : status === "error"
                ? failLabel
                : localizeText(lang, "Export", "导出", "匯出");
    const title = status === "error"
        ? (failLabel + (errorMsg ? `: ${errorMsg}` : ""))
        : status === "done"
            ? (savedPath
                ? localizeText(lang, `Exported to ${savedPath}`, `已复制到 ${savedPath}`, `已複製到 ${savedPath}`)
                : localizeText(lang, "Saved a copy of this file", "已复制到所选路径", "已複製到所選路徑"))
            : localizeText(lang, "Save a copy of this file", "将文档复制到所选路径", "將文件複製到所選路徑");
    const fail = (err: unknown) => {
        busyRef.current = false;
        if (!aliveRef.current) return;
        setStatus("error");
        setErrorMsg(localizeTaskResultExportError(wailsErrorText(err) || "export failed", lang));
    };
    const onClick = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (busyRef.current) return;
        busyRef.current = true;
        setStatus("busy");
        setErrorMsg("");
        if (typeof ExportTaskResultFile !== "function") {
            fail("无法保存文件");
            return;
        }
        try {
            void ExportTaskResultFile(filePath)
                .then((dest) => {
                    busyRef.current = false;
                    if (!aliveRef.current) return;
                    if (dest) {
                        setSavedPath(dest);
                        setStatus("done");
                        return;
                    }
                    setStatus("idle");
                })
                .catch(fail);
        } catch (err) {
            fail(err);
        }
    };
    return (
        <button
            type="button"
            data-testid="task-result-export-btn"
            title={title}
            aria-busy={status === "busy"}
            aria-live="polite"
            disabled={status === "busy"}
            onClick={onClick}
        >
            {label}
        </button>
    );
}
