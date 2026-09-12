import React from "react";
import { ImportMobileDocumentFromPath } from "../../../wailsjs/go/main/App";
import { isCloudWorkspaceFilePath, isCloudWorkspacePath } from "./codingTaskMode";

// Cloud workspace artifacts are not guaranteed to exist on the local disk, so
// the desktop cannot read and upload them.
export function taskResultUploadSupported(filePath: string): boolean {
    return !isCloudWorkspaceFilePath(filePath) && !isCloudWorkspacePath(filePath);
}

export function TaskResultUploadButton({ filePath, lang }: { filePath: string; lang: string }) {
    const [status, setStatus] = React.useState<"idle" | "uploading" | "done" | "error">("idle");
    const [errorMsg, setErrorMsg] = React.useState("");
    const en = lang === "en";
    const title = status === "done"
        ? (en ? "Uploaded to the mobile library. Phone app → Documents can open it." : "已上传到移动文稿库，手机端「文档」可直接打开。")
        : status === "error"
            ? ((en ? "Upload failed" : "上传失败") + (errorMsg ? `: ${errorMsg}` : ""))
            : (en ? "Upload to mobile library" : "上传到移动文稿库");
    const onClick = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        if (status === "uploading" || status === "done") return;
        setStatus("uploading");
        setErrorMsg("");
        void ImportMobileDocumentFromPath(filePath)
            .then((draft) => {
                if (draft && draft.id) {
                    setStatus("done");
                } else {
                    setStatus("error");
                    setErrorMsg(en ? "Hub did not return a document" : "Hub 未返回文稿");
                }
            })
            .catch((err: unknown) => {
                setStatus("error");
                setErrorMsg(String((err as Error)?.message || err || "upload failed"));
            });
    };
    return (
        <button
            type="button"
            className={`mc-task-result-card__upload mc-task-result-card__upload--${status}`}
            title={title}
            aria-label={en ? "Upload to mobile library" : "上传到移动文稿库"}
            aria-busy={status === "uploading"}
            aria-live="polite"
            disabled={status === "uploading"}
            onClick={onClick}
        >
            {status === "done" ? "✓" : status === "error" ? "✗" : status === "uploading" ? "…" : "⤴"}
        </button>
    );
}
