import React from "react";
import { ImportMobileDocumentFromPath } from "../../../wailsjs/go/main/App";
import { chromeText } from "../../i18n/chromeText";
import { isCloudWorkspaceFilePath, isCloudWorkspacePath } from "./codingTaskMode";

function uploadErrorText(err: unknown): string {
    if (err instanceof Error && err.message.trim()) return err.message.trim();
    if (typeof err === "string" && err.trim()) return err.trim();
    return "";
}

/** Known Hub failures become a sentence. Anything else stays, clipped so a response body cannot fill the tooltip. */
function uploadErrorTitle(err: unknown, lang: string): string {
    const text = uploadErrorText(err);
    if (text.includes("MaClaw Hub login is required")) {
        return chromeText(lang, "Sign in from the main window before uploading to the cloud drive", "需要先在主窗口登录，才能上传到云盘", "需要先在主視窗登入，才能上傳到雲盤");
    }
    if (text === "file too large to compress safely" || text.includes("file too large")) {
        return chromeText(lang, "The file is too large to upload to the cloud drive", "文件太大，无法上传到云盘", "檔案太大，無法上傳到雲盤");
    }
    if (text === "file is empty" || text.includes("file is empty")) {
        return chromeText(lang, "The file is empty", "文件是空的", "檔案是空的");
    }
    if (text === "path is a directory") {
        return chromeText(lang, "Folders cannot be uploaded", "不支持上传文件夹", "不支援上傳資料夾");
    }
    if (text === "Hub did not return a file" || text.includes("upload mobile document failed") || text.includes("Hub upload") || text.includes("decode upload response")) {
        return chromeText(lang, "Upload to the cloud drive failed. Try again later", "上传到云盘失败，请稍后重试", "上傳到雲盤失敗，請稍後重試");
    }
    const fail = chromeText(lang, "Upload failed", "上传失败", "上傳失敗");
    if (!text) return fail;
    const detail = text.length > 160 ? `${text.slice(0, 160)}…` : text;
    return `${fail}: ${detail}`;
}

// Cloud workspace artifacts are not guaranteed to exist on the local disk, so
// the desktop cannot read and upload them.
export function taskResultUploadSupported(filePath: string): boolean {
    return !isCloudWorkspaceFilePath(filePath) && !isCloudWorkspacePath(filePath);
}

export function TaskResultUploadButton({ filePath, lang }: { filePath: string; lang: string }) {
    const [status, setStatus] = React.useState<"idle" | "uploading" | "done" | "error">("idle");
    const [errorMsg, setErrorMsg] = React.useState("");
    const [alreadyThere, setAlreadyThere] = React.useState(false);
    const aliveRef = React.useRef(true);
    const busyRef = React.useRef(false);
    React.useEffect(() => {
        aliveRef.current = true;
        return () => { aliveRef.current = false; };
    }, []);
    const label = status === "uploading"
        ? chromeText(lang, "Uploading…", "上传中…", "上傳中…")
        : status === "done"
            ? (alreadyThere
                ? chromeText(lang, "Already in the cloud drive", "已在云盘", "已在雲盤")
                : chromeText(lang, "Uploaded", "已上传", "已上傳"))
            : status === "error"
                ? chromeText(lang, "Upload failed", "上传失败", "上傳失敗")
                : chromeText(lang, "Upload", "上传云盘", "上傳雲盤");
    const title = status === "done"
        ? (alreadyThere
            ? chromeText(lang, "This file is already in the cloud drive. Not uploaded again.", "云盘中已有相同内容，未再次上传。")
            : chromeText(lang, "Uploaded to the cloud drive. Phone app → Documents can open it.", "已上传到云盘，手机端「文档」可直接打开。"))
        : status === "error"
            ? uploadErrorTitle(errorMsg, lang)
            : chromeText(lang, "Upload to cloud drive", "上传到云盘", "上傳到雲盤");
    const fail = (err: unknown) => {
        busyRef.current = false;
        if (!aliveRef.current) return;
        setStatus("error");
        setErrorMsg(uploadErrorText(err) || "upload failed");
    };
    const onClick = (event: React.MouseEvent) => {
        event.preventDefault();
        event.stopPropagation();
        // busyRef closes the gap before the disabled re-render; done stays latched.
        if (busyRef.current || status === "done") return;
        busyRef.current = true;
        setStatus("uploading");
        setErrorMsg("");
        setAlreadyThere(false);
        try {
            void ImportMobileDocumentFromPath(filePath)
                .then((draft) => {
                    if (!aliveRef.current) return;
                    if (draft && draft.id) {
                        setAlreadyThere(!!draft.duplicate);
                        setStatus("done");
                        return;
                    }
                    fail("Hub did not return a file");
                })
                .catch(fail);
        } catch (err) {
            fail(err);
        }
    };
    return (
        <button
            type="button"
            className={`mc-task-upload mc-task-upload--${status}`}
            title={title}
            aria-busy={status === "uploading"}
            aria-disabled={status === "done" ? true : undefined}
            aria-live="polite"
            disabled={status === "uploading"}
            onClick={onClick}
        >
            {label}
        </button>
    );
}
