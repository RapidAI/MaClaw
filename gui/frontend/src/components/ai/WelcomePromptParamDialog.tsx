import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { CSSProperties, FormEvent, KeyboardEvent, Ref } from "react";
import { SelectWorkingDir } from "../../../wailsjs/go/main/App";
import { resolvePrimaryFilledColors, type Theme } from "./aiAssistantPanelTheme";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import {
    extractWelcomeTemplateFields,
    fillWelcomeTemplate,
    type WelcomeTemplateField,
} from "./welcomePromptTemplate";
import {
    loadWelcomeCodingEnv,
    loadWelcomeFieldValues,
    loadWelcomePreviewOpen,
    mergeWelcomeStoredCodingEnv,
    normalizeWelcomeSshPassword,
    normalizeWelcomeSshPort,
    saveWelcomeCodingEnv,
    saveWelcomeFieldValues,
    saveWelcomePreviewOpen,
    type WelcomeStoredCodingEnv,
} from "./welcomeTaskMemory";

/** Above app chrome (~50k–99k); below create-task dialog (100000). */
const WELCOME_PARAM_DIALOG_Z_INDEX = 90000;

function getPortalThemeAttrs(): {
    "data-ai-theme"?: string;
    "data-ai-dark-scheme"?: string;
    "data-ai-light-scheme"?: string;
} {
    const app = typeof document !== "undefined" ? document.getElementById("App") : null;
    if (!app) return {};
    return {
        "data-ai-theme": app.getAttribute("data-ai-theme") || undefined,
        "data-ai-dark-scheme": app.getAttribute("data-ai-dark-scheme") || undefined,
        "data-ai-light-scheme": app.getAttribute("data-ai-light-scheme") || undefined,
    };
}

export type WelcomePromptParamSubmitMode = "chat" | "coding_dev" | "remote_coding_dev";

/** insert = fill composer; send = dispatch immediately (chat only). */
export type WelcomePromptParamAction = "insert" | "send";

/** Coding environment collected in the param dialog (merged create-task step). */
export type WelcomeCodingSubmitEnv = {
    workingDir?: string;
    remote?: {
        host: string;
        port: number;
        user: string;
        password: string;
        workDir: string;
    };
    /** Prefer creating the task immediately without a second dialog. */
    autoCreate?: boolean;
};

export interface WelcomePromptParamDialogProps {
    open: boolean;
    onClose: () => void;
    lang: string;
    theme: Theme;
    /** Card title shown in the dialog header. */
    title: string;
    /** Short description under the title. */
    description?: string;
    /** Full prompt template with [placeholders]. */
    template: string;
    /**
     * Stable task key for remembering field values
     * (e.g. `${tabId}::${textEn}`).
     */
    taskKey?: string;
    /**
     * Optional clipboard snippet to prefill paste-like fields
     * (only when the saved field value is empty).
     */
    clipboardPrefill?: string;
    /** Label of the preferred field for clipboard prefill. */
    clipboardPrefillLabel?: string | null;
    /**
     * Where the filled prompt goes:
     * - chat: insert into assistant input / send
     * - coding_*: create coding task (env collected here when possible)
     */
    submitMode?: WelcomePromptParamSubmitMode;
    /**
     * Called after user confirms.
     * action is always "insert" for coding modes; chat supports "send".
     * codingEnv is set for coding modes.
     */
    onSubmit: (
        filledPrompt: string,
        mode: WelcomePromptParamSubmitMode,
        action: WelcomePromptParamAction,
        codingEnv?: WelcomeCodingSubmitEnv,
    ) => void;
    /**
     * Optional: save the assembled prompt as a custom template without closing.
     * Returns true when save succeeded.
     * codingEnv may include SSH password for local-only storage.
     */
    onSaveTemplate?: (
        filledPrompt: string,
        title: string,
        codingEnv?: WelcomeStoredCodingEnv,
    ) => boolean;
    /**
     * Prefer this coding environment when the dialog opens (e.g. per-template
     * snapshot from "save as favorite"). Falls back to the global last-used env.
     * May restore local-stored SSH password.
     */
    initialCodingEnv?: WelcomeStoredCodingEnv;
    /** When false, hide the direct-send button (e.g. hub not ready). Default true for chat. */
    canSend?: boolean;
}

type WailsNoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

function buildInitialValues(
    fields: WelcomeTemplateField[],
    taskKey?: string,
    clipboardPrefill?: string,
    clipboardPrefillLabel?: string | null,
): Record<string, string> {
    const values: Record<string, string> = {};
    const saved = taskKey ? loadWelcomeFieldValues(taskKey) : {};
    for (const field of fields) {
        values[field.id] = saved[field.label] ?? "";
    }
    const clip = (clipboardPrefill || "").trim();
    if (!clip || fields.length === 0) return values;

    // Prefer the caller-picked label, then first multiline/paste field, then first empty field.
    let target = clipboardPrefillLabel
        ? fields.find((f) => f.label === clipboardPrefillLabel)
        : undefined;
    if (!target) {
        target = fields.find(
            (f) =>
                !(values[f.id] || "").trim()
                && (f.multiline
                    || /粘贴|日志|材料|内容|要点|paste|log|note|material|content/i.test(`${f.label} ${f.hint}`)),
        );
    }
    if (!target) {
        target = fields.find((f) => !(values[f.id] || "").trim());
    }
    if (target && !(values[target.id] || "").trim()) {
        // Cap huge clipboard pastes so the form stays responsive.
        values[target.id] = clip.length > 12000 ? `${clip.slice(0, 12000)}\n…` : clip;
    }
    return values;
}

function valuesByLabel(
    fields: WelcomeTemplateField[],
    values: Record<string, string>,
): Record<string, string> {
    const out: Record<string, string> = {};
    for (const field of fields) {
        out[field.label] = values[field.id] ?? "";
    }
    return out;
}

export function WelcomePromptParamDialog({
    open,
    onClose,
    lang,
    theme: t,
    title,
    description,
    template,
    taskKey,
    clipboardPrefill,
    clipboardPrefillLabel,
    submitMode = "chat",
    onSubmit,
    onSaveTemplate,
    initialCodingEnv,
    canSend = true,
}: WelcomePromptParamDialogProps) {
    const isZh = !lang?.startsWith("en");
    const isCoding = submitMode === "coding_dev" || submitMode === "remote_coding_dev";
    const isRemote = submitMode === "remote_coding_dev";
    const fields = useMemo(() => extractWelcomeTemplateFields(template), [template]);
    const [values, setValues] = useState<Record<string, string>>(() =>
        buildInitialValues(fields, taskKey, clipboardPrefill, clipboardPrefillLabel),
    );
    const [previewOpen, setPreviewOpen] = useState(() => loadWelcomePreviewOpen(false));
    const [workingDir, setWorkingDir] = useState("");
    const [remoteHost, setRemoteHost] = useState("");
    const [remotePort, setRemotePort] = useState("22");
    const [remoteUser, setRemoteUser] = useState("");
    const [remotePassword, setRemotePassword] = useState("");
    const [remoteWorkDir, setRemoteWorkDir] = useState("");
    const [envError, setEnvError] = useState("");
    const [selectingDir, setSelectingDir] = useState(false);
    const [saveNote, setSaveNote] = useState("");
    const firstFieldRef = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    /** Prevent double primary-click from firing onSubmit twice. */
    const submitLockRef = useRef(false);
    const [submitting, setSubmitting] = useState(false);
    const handleDismiss = useCallback(() => {
        if (submitLockRef.current) return;
        onClose();
    }, [onClose]);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(handleDismiss, {
        enabled: !submitting,
    });

    const resetCodingEnvFromMemory = useCallback(() => {
        // Prefer per-template snapshot (from "save as favorite"), then global last-used.
        // Password is only reused across sources when host+user match.
        const saved = mergeWelcomeStoredCodingEnv(initialCodingEnv, loadWelcomeCodingEnv()) || {};
        setWorkingDir(saved.workingDir || "");
        setRemoteHost(saved.remote?.host || "");
        setRemotePort(String(saved.remote?.port || 22));
        setRemoteUser(saved.remote?.user || "");
        setRemotePassword(saved.remote?.password || "");
        setRemoteWorkDir(saved.remote?.workDir || "");
        setEnvError("");
        setSelectingDir(false);
    }, [initialCodingEnv]);

    useEffect(() => {
        if (!open) {
            submitLockRef.current = false;
            setSubmitting(false);
            return;
        }
        submitLockRef.current = false;
        setSubmitting(false);
        setValues(buildInitialValues(fields, taskKey, clipboardPrefill, clipboardPrefillLabel));
        setPreviewOpen(loadWelcomePreviewOpen(false));
        setSaveNote("");
        resetCodingEnvFromMemory();
        // Prefer first template field; for "no params" remote tasks focus password when
        // host/user already restored (common one-click path), else host.
        const timer = window.setTimeout(() => {
            firstFieldRef.current?.focus();
        }, 30);
        return () => window.clearTimeout(timer);
    }, [open, fields, template, taskKey, clipboardPrefill, clipboardPrefillLabel, resetCodingEnvFromMemory]);

    useEffect(() => {
        if (!open) return;
        const onKey = (event: globalThis.KeyboardEvent) => {
            if (event.key === "Escape") {
                if (submitLockRef.current) return;
                event.preventDefault();
                handleDismiss();
                return;
            }
            // Keep Tab cycling inside the portaled dialog.
            if (event.key !== "Tab") return;
            const root = dialogRef.current;
            if (!root) return;
            const nodes = root.querySelectorAll<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
            );
            const list = Array.from(nodes).filter(
                (el) => !el.hasAttribute("disabled") && (el.offsetParent !== null || el === document.activeElement),
            );
            if (list.length === 0) return;
            const first = list[0];
            const last = list[list.length - 1];
            if (event.shiftKey) {
                if (document.activeElement === first || !root.contains(document.activeElement)) {
                    event.preventDefault();
                    last.focus();
                }
            } else if (document.activeElement === last || !root.contains(document.activeElement)) {
                event.preventDefault();
                first.focus();
            }
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, handleDismiss]);

    // Lock background scroll while the modal is open (portaled to body).
    useEffect(() => {
        if (!open || typeof document === "undefined") return;
        const prev = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        return () => {
            document.body.style.overflow = prev;
        };
    }, [open]);

    const filledCount = fields.filter((field) => (values[field.id] || "").trim().length > 0).length;
    const previewText = useMemo(
        () => fillWelcomeTemplate(template, fields, values),
        [template, fields, values],
    );

    const handleChange = useCallback((id: string, next: string) => {
        setValues((prev) => ({ ...prev, [id]: next }));
    }, []);

    const togglePreview = useCallback(() => {
        setPreviewOpen((prev) => {
            const next = !prev;
            saveWelcomePreviewOpen(next);
            return next;
        });
    }, []);

    const pickWorkingDir = useCallback(async () => {
        if (selectingDir || submitting) return;
        setSelectingDir(true);
        try {
            const dir = await SelectWorkingDir();
            if (dir) setWorkingDir(dir);
        } catch (err) {
            console.warn("[WelcomePromptParamDialog] SelectWorkingDir failed", err);
        } finally {
            setSelectingDir(false);
        }
    }, [selectingDir, submitting]);

    /**
     * Coding env snapshot for "save as favorite".
     * Always includes `password` (possibly "") when remote fields are set so an
     * empty password is an explicit clear rather than "omit / keep previous".
     */
    const collectStoredCodingEnv = useCallback((): WelcomeStoredCodingEnv | undefined => {
        if (!isCoding) return undefined;
        if (isRemote) {
            const host = remoteHost.trim();
            const user = remoteUser.trim();
            const workDir = remoteWorkDir.trim();
            const password = normalizeWelcomeSshPassword(remotePassword);
            const port = normalizeWelcomeSshPort(remotePort);
            if (!host && !user && !workDir && !password) return undefined;
            return {
                remote: {
                    host,
                    port,
                    user,
                    workDir,
                    // Always set: "" clears stored password on re-save.
                    password,
                },
            };
        }
        const dir = workingDir.trim();
        return dir ? { workingDir: dir } : undefined;
    }, [isCoding, isRemote, remoteHost, remoteUser, remoteWorkDir, remotePort, remotePassword, workingDir]);

    const handleSaveTemplate = useCallback(() => {
        if (!onSaveTemplate || submitting) return;
        const filled = fillWelcomeTemplate(template, fields, values).trim();
        if (!filled) {
            setSaveNote(isZh ? "内容为空，无法保存" : "Nothing to save");
            return;
        }
        const codingEnv = collectStoredCodingEnv();
        const ok = onSaveTemplate(filled, title, codingEnv);
        const hasEnv = !!(codingEnv?.remote || codingEnv?.workingDir);
        const hasPassword = !!codingEnv?.remote?.password;
        setSaveNote(
            ok
                ? (isZh
                    ? (hasPassword
                        ? "已保存到「我的模板」（含运行环境与密码，仅本机）"
                        : hasEnv
                            ? "已保存到「我的模板」（含运行环境，仅本机）"
                            : "已保存到「我的模板」")
                    : (hasPassword
                        ? "Saved to My templates (env + password, this device only)"
                        : hasEnv
                            ? "Saved to My templates (env, this device only)"
                            : "Saved to My templates"))
                : (isZh ? "保存失败" : "Save failed"),
        );
    }, [onSaveTemplate, submitting, template, fields, values, title, isZh, collectStoredCodingEnv]);

    const commit = useCallback((action: WelcomePromptParamAction) => {
        if (submitLockRef.current) return;
        const filled = fillWelcomeTemplate(template, fields, values);
        if (taskKey) {
            saveWelcomeFieldValues(taskKey, valuesByLabel(fields, values));
        }

        let codingEnv: WelcomeCodingSubmitEnv | undefined;
        if (isCoding) {
            if (isRemote) {
                const host = remoteHost.trim();
                const user = remoteUser.trim();
                const workDir = remoteWorkDir.trim();
                const password = normalizeWelcomeSshPassword(remotePassword);
                const port = normalizeWelcomeSshPort(remotePort);
                if (!host || !user || !password || !workDir) {
                    setEnvError(
                        isZh
                            ? "请填写主机、用户名、密码和远程工作目录。"
                            : "Please fill host, username, password, and remote work directory.",
                    );
                    return;
                }
                codingEnv = {
                    autoCreate: true,
                    remote: { host, port, user, password, workDir },
                };
                saveWelcomeCodingEnv({
                    remote: { host, port, user, workDir, password },
                });
            } else {
                const dir = workingDir.trim();
                codingEnv = {
                    autoCreate: true,
                    workingDir: dir || undefined,
                };
                if (dir) saveWelcomeCodingEnv({ workingDir: dir });
            }
        }

        const resolvedFilled = (filled || "").trim();
        if (!resolvedFilled) {
            setEnvError(isZh ? "任务内容为空。" : "Task content is empty.");
            return;
        }

        submitLockRef.current = true;
        setSubmitting(true);
        setEnvError("");
        // Coding paths only support insert → create-task.
        const resolved: WelcomePromptParamAction = isCoding ? "insert" : action;
        onSubmit(resolvedFilled, submitMode, resolved, codingEnv);
    }, [
        template,
        fields,
        values,
        taskKey,
        isCoding,
        isRemote,
        isZh,
        remoteHost,
        remoteUser,
        remoteWorkDir,
        remotePassword,
        remotePort,
        workingDir,
        onSubmit,
        submitMode,
    ]);

    const handleFormSubmit = useCallback((event?: FormEvent) => {
        event?.preventDefault();
        // Default primary: send for chat when allowed, else insert / create-task.
        if (!isCoding && canSend) {
            commit("send");
        } else {
            commit("insert");
        }
    }, [isCoding, canSend, commit]);

    const handleFieldKeyDown = useCallback((event: KeyboardEvent<HTMLElement>, isLast: boolean) => {
        if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            handleFormSubmit();
            return;
        }
        // Enter on single-line fields moves focus forward.
        if (event.key === "Enter" && !event.shiftKey && event.currentTarget.tagName === "INPUT") {
            if (!isLast) {
                event.preventDefault();
                const form = (event.currentTarget as HTMLElement).closest("form");
                const controls = form?.querySelectorAll<HTMLElement>("input,textarea");
                if (!controls) return;
                const list = Array.from(controls);
                const idx = list.indexOf(event.currentTarget as HTMLElement);
                const next = list[idx + 1];
                next?.focus();
            }
        }
    }, [handleFormSubmit]);

    if (!open) return null;

    const insertLabel = isCoding
        ? (isRemote
            ? (isZh ? "创建并开始远程任务" : "Create remote task")
            : (isZh ? "创建并开始本地任务" : "Create local task"))
        : (isZh ? "填入输入框" : "Insert into input");

    const sendLabel = isZh ? "直接发送" : "Send now";

    const helper = isCoding
        ? (isZh
            ? "填写任务参数与运行环境后可直接创建，无需二次弹窗。参数会记住。"
            : "Fill task params and the runtime environment, then create in one step. Values are remembered.")
        : (isZh
            ? "填写关键信息后确认。可留空的项，助手会在对话中追问。已自动记住上次填写。"
            : "Fill in the key details, then confirm. Blanks are fine — the assistant can follow up. Last values are remembered.");

    const shortcutHint = !isCoding && canSend
        ? (isZh ? "Ctrl/⌘+Enter 直接发送" : "Ctrl/⌘+Enter to send")
        : (isZh ? "Ctrl/⌘+Enter 确认" : "Ctrl/⌘+Enter to confirm");

    const fieldInputStyle = (multiline: boolean): CSSProperties => ({
        width: "100%",
        boxSizing: "border-box",
        borderRadius: 8,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.inputText || t.text,
        padding: multiline ? "8px 10px" : "7px 10px",
        fontSize: 13,
        lineHeight: 1.4,
        fontFamily: "system-ui, -apple-system, sans-serif",
        outline: "none",
        resize: multiline ? "vertical" : undefined,
        minHeight: multiline ? 72 : undefined,
    });

    const overlayStyle: WailsNoDragStyle = {
        position: "fixed",
        inset: 0,
        background: "rgba(0, 0, 0, 0.48)",
        backdropFilter: "blur(3px)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: WELCOME_PARAM_DIALOG_Z_INDEX,
        padding: "16px",
        boxSizing: "border-box",
        WebkitAppRegion: "no-drag",
        "--wails-draggable": "no-drag",
    };

    const modalStyle: WailsNoDragStyle = {
        width: "min(500px, 100%)",
        maxHeight: "min(85vh, 740px)",
        borderRadius: "12px",
        background: t.bg,
        color: t.text,
        border: `1px solid ${t.fieldBorder}`,
        boxShadow: "0 20px 60px rgba(0, 0, 0, 0.28), 0 0 0 1px rgba(0, 0, 0, 0.04)",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        // Portal to body inherits `html { text-align: center }`; form dialogs must be left-aligned
        // (same pattern as SidebarTaskManagement create-task modal).
        textAlign: "left",
        WebkitAppRegion: "no-drag",
        "--wails-draggable": "no-drag",
    };

    const secondaryBtnStyle: CSSProperties = {
        padding: "7px 12px",
        borderRadius: 8,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.text,
        fontSize: 13,
        cursor: "pointer",
        fontFamily: "system-ui, -apple-system, sans-serif",
        whiteSpace: "nowrap",
    };

    // sendBtn* pair with luminance-safe ink (never white-on-light btnColor accent)
    const primaryFilled = resolvePrimaryFilledColors(t);
    const primaryBtnStyle: CSSProperties = {
        padding: "7px 14px",
        borderRadius: 8,
        border: "none",
        boxShadow: `inset 0 0 0 1px ${t.sendBtnBorder || primaryFilled.bg}`,
        background: primaryFilled.bg,
        color: primaryFilled.fg,
        fontSize: 13,
        fontWeight: 600,
        cursor: "pointer",
        fontFamily: "system-ui, -apple-system, sans-serif",
        whiteSpace: "nowrap",
    };

    const dialog = (
        <div
            role="presentation"
            style={overlayStyle}
            {...backdropProps}
            {...getPortalThemeAttrs()}
            data-testid="welcome-prompt-param-overlay"
        >
            <div
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby="welcome-prompt-param-title"
                aria-describedby="welcome-prompt-param-desc"
                style={modalStyle}
                {...dialogProps}
                data-testid="welcome-prompt-param-dialog"
            >
                <header style={{
                    display: "flex",
                    alignItems: "flex-start",
                    justifyContent: "space-between",
                    gap: "12px",
                    padding: "14px 16px 10px",
                    borderBottom: `1px solid ${t.divider || t.fieldBorder}`,
                    flexShrink: 0,
                }}>
                    <div style={{ minWidth: 0 }}>
                        <h2
                            id="welcome-prompt-param-title"
                            style={{
                                margin: 0,
                                fontSize: "15px",
                                fontWeight: 600,
                                lineHeight: 1.35,
                                color: t.titleText || t.text,
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                        >
                            {title}
                        </h2>
                        {(description || helper) && (
                            <p
                                id="welcome-prompt-param-desc"
                                style={{
                                    margin: "6px 0 0",
                                    fontSize: "12px",
                                    lineHeight: 1.45,
                                    color: t.textMuted,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}
                            >
                                {description ? `${description} · ${helper}` : helper}
                            </p>
                        )}
                    </div>
                    <button
                        type="button"
                        aria-label={isZh ? "关闭" : "Close"}
                        onClick={onClose}
                        style={{
                            flexShrink: 0,
                            width: 28,
                            height: 28,
                            borderRadius: 6,
                            border: `1px solid ${t.fieldBorder}`,
                            background: t.fieldBg,
                            color: t.textMuted,
                            cursor: "pointer",
                            fontSize: 16,
                            lineHeight: 1,
                        }}
                    >
                        ×
                    </button>
                </header>

                <form
                    onSubmit={handleFormSubmit}
                    style={{
                        display: "flex",
                        flexDirection: "column",
                        minHeight: 0,
                        flex: 1,
                    }}
                >
                    <div style={{
                        flex: 1,
                        overflowY: "auto",
                        padding: "12px 16px 8px",
                        display: "flex",
                        flexDirection: "column",
                        gap: "12px",
                    }}>
                        {fields.length === 0 ? (
                            <p style={{ margin: 0, fontSize: 13, color: t.textMuted }}>
                                {isZh ? "该任务无需额外参数，可直接确认。" : "No extra parameters — confirm to continue."}
                            </p>
                        ) : fields.map((field, index) => {
                            const controlId = `welcome-param-${field.id}`;
                            const commonProps = {
                                id: controlId,
                                value: values[field.id] ?? "",
                                onChange: (e: { currentTarget: { value: string } }) =>
                                    handleChange(field.id, e.currentTarget.value),
                                onKeyDown: (e: KeyboardEvent<HTMLElement>) =>
                                    handleFieldKeyDown(e, index === fields.length - 1),
                                placeholder: field.hint,
                                "aria-label": field.label,
                                style: {
                                    width: "100%",
                                    boxSizing: "border-box" as const,
                                    borderRadius: 8,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.fieldBg,
                                    color: t.inputText || t.text,
                                    padding: field.multiline ? "8px 10px" : "7px 10px",
                                    fontSize: 13,
                                    lineHeight: 1.4,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                    outline: "none",
                                    resize: field.multiline ? ("vertical" as const) : undefined,
                                    minHeight: field.multiline ? 72 : undefined,
                                },
                            };
                            return (
                                <label
                                    key={field.id}
                                    htmlFor={controlId}
                                    style={{
                                        display: "flex",
                                        flexDirection: "column",
                                        gap: 5,
                                        margin: 0,
                                    }}
                                >
                                    <span style={{
                                        fontSize: 12,
                                        fontWeight: 600,
                                        color: t.fieldLabel || t.text,
                                        fontFamily: "system-ui, -apple-system, sans-serif",
                                    }}>
                                        {field.label}
                                        <span style={{
                                            marginLeft: 6,
                                            fontWeight: 400,
                                            color: t.textMuted,
                                            fontSize: 11,
                                        }}>
                                            {isZh ? "可选" : "optional"}
                                        </span>
                                    </span>
                                    {field.multiline ? (
                                        <textarea
                                            ref={index === 0 ? (firstFieldRef as Ref<HTMLTextAreaElement>) : undefined}
                                            rows={3}
                                            {...commonProps}
                                        />
                                    ) : (
                                        <input
                                            ref={index === 0 ? (firstFieldRef as Ref<HTMLInputElement>) : undefined}
                                            type="text"
                                            autoComplete="off"
                                            {...commonProps}
                                        />
                                    )}
                                    {field.chips.length > 0 && (
                                        <div
                                            role="group"
                                            aria-label={isZh ? `${field.label} 快捷选项` : `${field.label} suggestions`}
                                            style={{ display: "flex", flexWrap: "wrap", gap: 6 }}
                                        >
                                            {field.chips.map((chip) => {
                                                const active = (values[field.id] || "").trim() === chip;
                                                return (
                                                    <button
                                                        key={chip}
                                                        type="button"
                                                        data-testid={`welcome-param-chip-${field.id}-${chip}`}
                                                        onClick={() => handleChange(field.id, chip)}
                                                        style={{
                                                            padding: "3px 8px",
                                                            borderRadius: 999,
                                                            border: `1px solid ${active ? (t.sendBtnBorder || primaryFilled.bg) : t.fieldBorder}`,
                                                            background: active
                                                                ? primaryFilled.bg
                                                                : t.inputBarBg || t.bg,
                                                            color: active
                                                                ? primaryFilled.fg
                                                                : t.text,
                                                            fontSize: 11,
                                                            lineHeight: 1.3,
                                                            cursor: "pointer",
                                                            fontFamily: "system-ui, -apple-system, sans-serif",
                                                        }}
                                                    >
                                                        {chip}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    )}
                                </label>
                            );
                        })}

                        {/* Coding environment — merged create-task step */}
                        {isCoding && (
                            <div
                                data-testid="welcome-coding-env"
                                style={{
                                    display: "flex",
                                    flexDirection: "column",
                                    gap: 10,
                                    padding: "10px 12px",
                                    borderRadius: 8,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: t.inputBarBg || t.fieldBg,
                                }}
                            >
                                <div style={{
                                    fontSize: 12,
                                    fontWeight: 600,
                                    color: t.text,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                }}>
                                    {isRemote
                                        ? (isZh ? "远程环境（SSH）" : "Remote environment (SSH)")
                                        : (isZh ? "本地工作目录" : "Local working directory")}
                                </div>
                                {isRemote ? (
                                    <>
                                        <label style={{ display: "flex", flexDirection: "column", gap: 4, margin: 0 }}>
                                            <span style={{ fontSize: 11, color: t.textMuted }}>{isZh ? "主机" : "Host"}</span>
                                            <input
                                                data-testid="welcome-remote-host"
                                                ref={
                                                    fields.length === 0
                                                        && !(remoteHost.trim() && remoteUser.trim() && !remotePassword)
                                                        ? (firstFieldRef as Ref<HTMLInputElement>)
                                                        : undefined
                                                }
                                                value={remoteHost}
                                                onChange={(e) => setRemoteHost(e.target.value)}
                                                placeholder="192.168.1.10"
                                                autoComplete="off"
                                                style={fieldInputStyle(false)}
                                            />
                                        </label>
                                        <div style={{ display: "grid", gridTemplateColumns: "88px 1fr", gap: 8 }}>
                                            <label style={{ display: "flex", flexDirection: "column", gap: 4, margin: 0 }}>
                                                <span style={{ fontSize: 11, color: t.textMuted }}>{isZh ? "端口" : "Port"}</span>
                                                <input
                                                    data-testid="welcome-remote-port"
                                                    value={remotePort}
                                                    onChange={(e) => setRemotePort(e.target.value)}
                                                    placeholder="22"
                                                    autoComplete="off"
                                                    style={fieldInputStyle(false)}
                                                />
                                            </label>
                                            <label style={{ display: "flex", flexDirection: "column", gap: 4, margin: 0 }}>
                                                <span style={{ fontSize: 11, color: t.textMuted }}>{isZh ? "用户名" : "Username"}</span>
                                                <input
                                                    data-testid="welcome-remote-user"
                                                    value={remoteUser}
                                                    onChange={(e) => setRemoteUser(e.target.value)}
                                                    placeholder="ubuntu"
                                                    autoComplete="off"
                                                    style={fieldInputStyle(false)}
                                                />
                                            </label>
                                        </div>
                                        <label style={{ display: "flex", flexDirection: "column", gap: 4, margin: 0 }}>
                                            <span style={{ fontSize: 11, color: t.textMuted }}>{isZh ? "密码" : "Password"}</span>
                                            <input
                                                data-testid="welcome-remote-password"
                                                ref={
                                                    fields.length === 0
                                                        && !!remoteHost.trim()
                                                        && !!remoteUser.trim()
                                                        && !remotePassword
                                                        ? (firstFieldRef as Ref<HTMLInputElement>)
                                                        : undefined
                                                }
                                                type="password"
                                                value={remotePassword}
                                                onChange={(e) => setRemotePassword(e.target.value)}
                                                autoComplete="new-password"
                                                style={fieldInputStyle(false)}
                                            />
                                        </label>
                                        <label style={{ display: "flex", flexDirection: "column", gap: 4, margin: 0 }}>
                                            <span style={{ fontSize: 11, color: t.textMuted }}>{isZh ? "远程工作目录" : "Remote work directory"}</span>
                                            <input
                                                data-testid="welcome-remote-workdir"
                                                value={remoteWorkDir}
                                                onChange={(e) => setRemoteWorkDir(e.target.value)}
                                                placeholder="/home/ubuntu/app"
                                                autoComplete="off"
                                                style={fieldInputStyle(false)}
                                            />
                                        </label>
                                    </>
                                ) : (
                                    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                                        <input
                                            data-testid="welcome-local-workdir"
                                            value={workingDir}
                                            onChange={(e) => setWorkingDir(e.target.value)}
                                            placeholder={isZh ? "可选：项目目录" : "Optional project folder"}
                                            autoComplete="off"
                                            style={{ ...fieldInputStyle(false), flex: 1 }}
                                        />
                                        <button
                                            type="button"
                                            data-testid="welcome-local-workdir-browse"
                                            onClick={() => void pickWorkingDir()}
                                            disabled={selectingDir || submitting}
                                            style={{
                                                flexShrink: 0,
                                                padding: "7px 10px",
                                                borderRadius: 8,
                                                border: `1px solid ${t.fieldBorder}`,
                                                background: t.fieldBg,
                                                color: t.text,
                                                fontSize: 12,
                                                cursor: selectingDir ? "wait" : "pointer",
                                                fontFamily: "system-ui, -apple-system, sans-serif",
                                            }}
                                        >
                                            {selectingDir
                                                ? (isZh ? "选择中…" : "Choosing…")
                                                : (isZh ? "浏览" : "Browse")}
                                        </button>
                                    </div>
                                )}
                                {envError && (
                                    <p
                                        data-testid="welcome-coding-env-error"
                                        style={{ margin: 0, fontSize: 12, color: t.errorText || "#ef4444" }}
                                    >
                                        {envError}
                                    </p>
                                )}
                            </div>
                        )}

                        {/* Collapsible live preview of assembled prompt */}
                        <div
                            style={{
                                borderRadius: 8,
                                border: `1px solid ${t.fieldBorder}`,
                                background: t.fieldBg,
                                overflow: "hidden",
                            }}
                        >
                            <button
                                type="button"
                                onClick={togglePreview}
                                aria-expanded={previewOpen}
                                data-testid="welcome-prompt-preview-toggle"
                                style={{
                                    width: "100%",
                                    display: "flex",
                                    alignItems: "center",
                                    justifyContent: "space-between",
                                    gap: 8,
                                    padding: "8px 10px",
                                    border: "none",
                                    background: "transparent",
                                    color: t.text,
                                    cursor: "pointer",
                                    fontSize: 12,
                                    fontWeight: 600,
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                    textAlign: "left",
                                }}
                            >
                                <span>
                                    {isZh ? "将发送的内容" : "Message preview"}
                                    <span style={{ marginLeft: 6, fontWeight: 400, color: t.textMuted }}>
                                        {previewText.length}
                                        {isZh ? " 字" : " chars"}
                                    </span>
                                </span>
                                <span style={{ color: t.textMuted, fontSize: 11 }} aria-hidden>
                                    {previewOpen ? "▾" : "▸"}
                                </span>
                            </button>
                            {previewOpen && (
                                <pre
                                    data-testid="welcome-prompt-preview-body"
                                    style={{
                                        margin: 0,
                                        padding: "0 10px 10px",
                                        maxHeight: 160,
                                        overflow: "auto",
                                        whiteSpace: "pre-wrap",
                                        wordBreak: "break-word",
                                        fontSize: 11.5,
                                        lineHeight: 1.45,
                                        color: t.textMuted,
                                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                                    }}
                                >
                                    {previewText || (isZh ? "（空）" : "(empty)")}
                                </pre>
                            )}
                        </div>
                    </div>

                    <footer style={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 10,
                        padding: "12px 16px 14px",
                        borderTop: `1px solid ${t.divider || t.fieldBorder}`,
                        flexShrink: 0,
                    }}>
                        <div style={{
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "space-between",
                            gap: 8,
                            flexWrap: "wrap",
                        }}>
                            <span style={{ fontSize: 11, color: t.textMuted }}>
                                {fields.length > 0
                                    ? (isZh ? `已填 ${filledCount}/${fields.length}` : `${filledCount}/${fields.length} filled`)
                                    : ""}
                                {fields.length > 0 ? " · " : ""}
                                {shortcutHint}
                            </span>
                            {saveNote && (
                                <span
                                    data-testid="welcome-prompt-save-note"
                                    style={{ fontSize: 11, color: t.sendBtnBg || t.btnColor }}
                                >
                                    {saveNote}
                                </span>
                            )}
                        </div>
                        <div style={{
                            display: "flex",
                            flexWrap: "wrap",
                            justifyContent: "flex-end",
                            gap: 8,
                        }}>
                            {onSaveTemplate && (
                                <button
                                    type="button"
                                    data-testid="welcome-prompt-save-template"
                                    onClick={handleSaveTemplate}
                                    disabled={submitting}
                                    style={secondaryBtnStyle}
                                >
                                    {isZh ? "保存为常用" : "Save template"}
                                </button>
                            )}
                            <button type="button" onClick={onClose} disabled={submitting} style={secondaryBtnStyle}>
                                {isZh ? "取消" : "Cancel"}
                            </button>
                            {!isCoding && (
                                <button
                                    type="button"
                                    data-testid="welcome-prompt-param-insert"
                                    onClick={() => commit("insert")}
                                    disabled={submitting}
                                    style={{
                                        ...secondaryBtnStyle,
                                        opacity: submitting ? 0.55 : 1,
                                        cursor: submitting ? "not-allowed" : "pointer",
                                    }}
                                >
                                    {insertLabel}
                                </button>
                            )}
                            {isCoding ? (
                                <button
                                    type="submit"
                                    data-testid="welcome-prompt-param-submit"
                                    disabled={submitting}
                                    style={{
                                        ...primaryBtnStyle,
                                        opacity: submitting ? 0.55 : 1,
                                        cursor: submitting ? "not-allowed" : "pointer",
                                    }}
                                >
                                    {insertLabel}
                                </button>
                            ) : (
                                <button
                                    type="submit"
                                    data-testid="welcome-prompt-param-submit"
                                    disabled={!canSend || submitting}
                                    title={!canSend ? (isZh ? "当前不可发送" : "Send unavailable") : undefined}
                                    style={{
                                        ...primaryBtnStyle,
                                        opacity: canSend && !submitting ? 1 : 0.5,
                                        cursor: canSend && !submitting ? "pointer" : "not-allowed",
                                    }}
                                >
                                    {sendLabel}
                                </button>
                            )}
                        </div>
                    </footer>
                </form>
            </div>
        </div>
    );

    // Portal to body so fixed overlay is not clipped by welcome scroll containers.
    if (typeof document === "undefined") return dialog;
    return createPortal(dialog, document.body);
}
