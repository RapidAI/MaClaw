import { useCallback, useEffect, useMemo, useState } from "react";
import { useDialog } from "../CustomDialog";
import {
    DeletePassthroughCommand,
    ExportPassthroughCommand,
    GetPassthroughSettings,
    ListPassthroughAudit,
    ListPassthroughCommands,
    PassthroughRegistryPath,
    PreviewPassthroughCommand,
    PreviewPassthroughDraftCommand,
    RunPassthroughCommand,
    SavePassthroughCommand,
    SavePassthroughSettings,
    SetPassthroughCommandEnabled,
} from "../../../wailsjs/go/main/App";
import {
    colors,
    radius,
    remoteActionButtonStyle,
    remoteDangerActionButtonStyle,
    remoteEmptyStateStyle,
    remotePrimaryActionButtonStyle,
    remoteTableCellStyle,
    remoteTableContainerStyle,
    remoteTableHeaderCellStyle,
    remoteTableHeaderRowStyle,
} from "./styles";

type PassthroughParam = {
    name: string;
    type?: string;
    required?: boolean;
    default?: string;
    example?: string;
};

type PassthroughCommand = {
    name: string;
    title?: string;
    description?: string;
    script_path: string;
    template_args?: string[];
    runtime: string;
    cwd?: string;
    timeout_seconds: number;
    confirm_required: boolean;
    enabled: boolean;
    params?: PassthroughParam[];
    last_run_at?: string;
    last_exit_code?: number;
    last_status?: string;
};

type RunResult = {
    command_name: string;
    status: string;
    exit_code: number;
    duration_ms: number;
    output: string;
};

type PassthroughSettings = { allow_exec?: boolean };

type PassthroughAuditEntry = {
    id: string;
    kind: string;
    command_name: string;
    source?: string;
    args?: string[];
    status: string;
    exit_code: number;
    duration_ms: number;
    started_at: string;
    finished_at: string;
    error?: string;
};

type Props = { lang: string };

const emptyForm: PassthroughCommand = {
    name: "",
    title: "",
    description: "",
    script_path: "",
    template_args: [],
    runtime: "direct",
    cwd: "",
    timeout_seconds: 120,
    confirm_required: true,
    enabled: true,
    params: [],
};

const inputStyle: React.CSSProperties = {
    width: "100%",
    padding: "7px 9px",
    border: `1px solid ${colors.border}`,
    borderRadius: radius.sm,
    background: colors.surface,
    color: colors.text,
    fontSize: "0.78rem",
    boxSizing: "border-box",
};

const labelStyle: React.CSSProperties = {
    display: "block",
    color: colors.textSecondary,
    fontSize: "0.7rem",
    fontWeight: 600,
    marginBottom: 4,
};

const paramTypes = ["text", "number", "boolean", "path"];
const commandNamePattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const paramNamePattern = /^[A-Za-z_][A-Za-z0-9_-]{0,63}$/;
const placeholderPattern = /\$\{([^}]*)\}/g;

function text(lang: string, zh: string, en: string) {
    return lang === "en" ? en : zh;
}

function splitCommandLine(input: string): string[] {
    const out: string[] = [];
    let current = "";
    let quote = "";
    let escaped = false;
    let fieldStarted = false;
    for (const ch of input.trim()) {
        if (escaped) {
            if (ch !== quote && ch !== "\\") current += "\\";
            current += ch;
            escaped = false;
            fieldStarted = true;
            continue;
        }
        if (quote) {
            if (quote === "\"" && ch === "\\") {
                escaped = true;
                continue;
            }
            if (ch === quote) quote = "";
            else {
                current += ch;
                fieldStarted = true;
            }
            continue;
        }
        if (ch === "'" || ch === "\"") {
            quote = ch;
            fieldStarted = true;
            continue;
        }
        if (/\s/.test(ch)) {
            if (fieldStarted) {
                out.push(current);
                current = "";
                fieldStarted = false;
            }
            continue;
        }
        current += ch;
        fieldStarted = true;
    }
    if (escaped) current += "\\";
    if (fieldStarted) out.push(current);
    if (quote) throw new Error("命令模板引号未闭合 / command template has an unterminated quote");
    return out;
}

function quoteArg(arg: string): string {
    if (!arg) return "\"\"";
    if (/[\s"']/u.test(arg)) return `"${arg.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")}"`;
    return arg;
}

function commandLineFromCommand(cmd: PassthroughCommand): string {
    return [cmd.script_path, ...(cmd.template_args || [])].map(quoteArg).join(" ");
}

function commandFromForm(form: PassthroughCommand, commandLine: string, params: PassthroughParam[], examples: Record<string, string> = {}): PassthroughCommand {
	const fields = splitCommandLine(commandLine);
	return {
		...form,
		script_path: fields[0] || "",
		template_args: fields.slice(1),
		params: params.filter((p) => p.name.trim()).map((p) => {
			const name = p.name.trim();
			const hasExample = Object.prototype.hasOwnProperty.call(examples, name);
			const example = hasExample ? (examples[name] ?? "") : (p.example || "");
			return { ...p, name, type: p.type || "text", example };
		}),
	};
}

function validateParamDraftValue(param: PassthroughParam, value: string, label: string): string {
    const trimmed = value.trim();
    if (!trimmed) return "";
    const type = (param.type || "text").toLowerCase();
    if (type === "number" && !/^-?\d+(\.\d+)?$/.test(trimmed)) {
        return `形参 ${param.name} 的${label}必须是数字 / parameter ${param.name} ${label} must be a number`;
    }
    if (type === "boolean" && !["true", "false", "1", "0", "yes", "no"].includes(trimmed.toLowerCase())) {
        return `形参 ${param.name} 的${label}必须是布尔值 / parameter ${param.name} ${label} must be boolean`;
    }
    if (type === "path" && /[<>|?*]/.test(trimmed)) {
        return `形参 ${param.name} 的${label}不是有效路径 / parameter ${param.name} ${label} is not a valid path`;
    }
    return "";
}

function validateCommandDraft(name: string, commandLine: string, timeoutSeconds: number, params: PassthroughParam[], examples: Record<string, string>): string {
    const trimmedName = name.trim();
    if (!trimmedName) return "任务名不能为空 / task name is required";
    if (!commandNamePattern.test(trimmedName)) return "任务名需以字母或数字开头，仅支持字母、数字、点、下划线、短横线，最多 64 字符 / task name must start with a letter or number and may contain letters, numbers, dots, underscores, and hyphens";
    if (!commandLine.trim()) return "命令模板不能为空 / command template is required";
    if (!Number.isFinite(timeoutSeconds) || timeoutSeconds <= 0 || timeoutSeconds > 3600) return "超时秒数必须在 1 到 3600 之间 / timeout must be between 1 and 3600 seconds";
    splitCommandLine(commandLine);
    const names = new Set<string>();
    for (const p of params) {
        const name = p.name.trim();
        if (!name) continue;
        if (!paramNamePattern.test(name)) return `形参名 ${name} 无效 / invalid parameter name ${name}`;
        if (names.has(name)) return `形参名 ${name} 重复 / duplicate parameter name ${name}`;
        names.add(name);
        const defaultError = validateParamDraftValue({ ...p, name }, p.default || "", "默认值");
        if (defaultError) return defaultError;
        const exampleError = validateParamDraftValue({ ...p, name }, examples[name] || p.example || "", "测试值");
        if (exampleError) return exampleError;
    }
    placeholderPattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = placeholderPattern.exec(commandLine)) !== null) {
        const name = match[1];
        if (!paramNamePattern.test(name)) return `占位符 \${${name}} 无效 / invalid placeholder \${${name}}`;
        if (!names.has(name)) return `占位符 \${${name}} 未定义 / undefined placeholder \${${name}}`;
    }
    const opens = commandLine.match(/\$\{/g)?.length || 0;
    const closes = commandLine.match(/}/g)?.length || 0;
    if (opens > closes) return "命令模板占位符未闭合 / command template has an unclosed placeholder";
    return "";
}

function testValuesFor(params: PassthroughParam[], previous: Record<string, string>): Record<string, string> {
    const next: Record<string, string> = {};
    for (const p of params) {
        if (!p.name) continue;
        next[p.name] = previous[p.name] ?? p.example ?? p.default ?? "";
    }
    return next;
}

function commandExample(cmd: PassthroughCommand, values?: Record<string, string>): string {
    const parts = [`/run ${cmd.name}`];
    for (const p of cmd.params || []) {
        const value = values && Object.prototype.hasOwnProperty.call(values, p.name) ? values[p.name] : (p.example || p.default || `<${p.name}>`);
        parts.push(value.startsWith("--") ? `--${p.name}=${quoteArg(value)}` : `--${p.name} ${quoteArg(value)}`);
    }
    if (cmd.confirm_required) parts.push("--confirm");
    return parts.join(" ");
}

function valuesForPreview(params: PassthroughParam[], values: Record<string, string>): Record<string, string> {
    const out: Record<string, string> = {};
    for (const p of params) {
        const name = p.name.trim();
        if (!name) continue;
        if (Object.prototype.hasOwnProperty.call(values, name)) {
            out[name] = values[name];
            continue;
        }
        const value = p.example ?? p.default ?? previewSampleValue(p);
        if (value) out[name] = value;
    }
    return out;
}

function previewSampleValue(param: PassthroughParam): string {
    switch ((param.type || "text").toLowerCase()) {
        case "number":
            return "1";
        case "boolean":
            return "true";
        case "path":
            return ".";
        default:
            return "sample";
    }
}

function formatAuditTime(value: string): string {
    if (!value) return "-";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
}

export function PassthroughCommandsPanel({ lang }: Props) {
    const [commands, setCommands] = useState<PassthroughCommand[]>([]);
    const [settings, setSettings] = useState<PassthroughSettings>({});
    const [audit, setAudit] = useState<PassthroughAuditEntry[]>([]);
    const [form, setForm] = useState<PassthroughCommand>(emptyForm);
    const [commandLine, setCommandLine] = useState("");
    const [params, setParams] = useState<PassthroughParam[]>([]);
    const [testValues, setTestValues] = useState<Record<string, string>>({});
    const [editing, setEditing] = useState(false);
    const [message, setMessage] = useState("");
    const [runningName, setRunningName] = useState("");
    const [lastResult, setLastResult] = useState<RunResult | null>(null);
    const [registryPath, setRegistryPath] = useState("");
    const [showForm, setShowForm] = useState(false);
    const { showConfirm } = useDialog();

    // Close form modal on Escape key
    useEffect(() => {
        if (!showForm) return;
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === "Escape") closeForm();
        };
        document.addEventListener("keydown", handleKeyDown);
        return () => document.removeEventListener("keydown", handleKeyDown);
    }, [showForm]); // eslint-disable-line react-hooks/exhaustive-deps

    const refresh = useCallback(async () => {
        try {
            const [nextCommands, nextSettings, nextAudit, nextRegistryPath] = await Promise.all([
                ListPassthroughCommands(),
                GetPassthroughSettings(),
                ListPassthroughAudit(20),
                PassthroughRegistryPath(),
            ]);
            setCommands(nextCommands || []);
            setSettings(nextSettings || {});
            setAudit(nextAudit || []);
            setRegistryPath(nextRegistryPath || "");
        } catch (err) {
            setMessage(String(err));
        }
    }, []);

    useEffect(() => { refresh(); }, [refresh]);

    const draftError = useMemo(() => {
        try {
            return validateCommandDraft(form.name, commandLine, form.timeout_seconds, params, testValues);
        } catch (err) {
            return String(err);
        }
    }, [form.name, form.timeout_seconds, commandLine, params, testValues]);
	const draft = useMemo(() => {
		try {
			return commandFromForm(form, commandLine, params, testValues);
		} catch {
			return { ...form, script_path: "", template_args: [], params };
		}
	}, [form, commandLine, params, testValues]);
    const showDraftError = editing || !!form.name.trim() || !!commandLine.trim() || params.some((p) => !!p.name.trim());
    const selectedExample = useMemo(() => draft.name ? commandExample(draft, testValues) : "", [draft, testValues]);

    const resetForm = () => {
        setForm(emptyForm);
        setCommandLine("");
        setParams([]);
        setTestValues({});
        setEditing(false);
        setLastResult(null);
    };

    const openNewForm = () => {
        resetForm();
        setShowForm(true);
    };

    const closeForm = () => {
        resetForm();
        setShowForm(false);
    };

    const editCommand = (cmd: PassthroughCommand) => {
        const nextParams = cmd.params || [];
        setForm({ ...emptyForm, ...cmd, params: nextParams, template_args: cmd.template_args || [] });
        setCommandLine(commandLineFromCommand(cmd));
        setParams(nextParams);
        setTestValues(testValuesFor(nextParams, {}));
        setEditing(true);
        setLastResult(null);
        setShowForm(true);
    };

    const updateParam = (index: number, patch: Partial<PassthroughParam>) => {
        setParams((prev) => {
            const next = prev.map((p, i) => i === index ? { ...p, ...patch } : p);
            setTestValues((old) => testValuesFor(next, old));
            return next;
        });
    };

    const addParam = () => {
        setParams((prev) => [...prev, { name: "", type: "text", required: true }]);
    };

    const removeParam = (index: number) => {
        setParams((prev) => {
            const next = prev.filter((_, i) => i !== index);
            setTestValues((old) => testValuesFor(next, old));
            return next;
        });
    };

    const insertParamToken = (name: string) => {
        if (!name.trim()) return;
        setCommandLine((old) => `${old}${old.trim() ? " " : ""}\${${name.trim()}}`);
    };

    const save = async (): Promise<PassthroughCommand | null> => {
        setMessage("");
        if (draftError) {
            setMessage(draftError);
            return null;
        }
        try {
			const next = commandFromForm(form, commandLine, params, testValues);
            const saved = await SavePassthroughCommand(next);
            const savedCmd = saved as PassthroughCommand;
            setForm(savedCmd);
            setCommandLine(commandLineFromCommand(savedCmd));
            setParams(savedCmd.params || []);
            setTestValues((old) => testValuesFor(savedCmd.params || [], old));
            setEditing(true);
            setMessage(text(lang, "已保存直通任务。", "Passthrough task saved."));
            await refresh();
            return savedCmd;
        } catch (err) {
            setMessage(String(err));
            return null;
        }
    };

    const confirmTestRun = async (name: string): Promise<boolean> => {
        return showConfirm(text(lang, `确认测试运行直通任务 ${name}？`, `Run passthrough task ${name} for testing?`));
    };

    const runTest = async (cmd: PassthroughCommand, values?: Record<string, string>, skipConfirm = false) => {
        if (!skipConfirm && !(await confirmTestRun(cmd.name))) return;
        setRunningName(cmd.name);
        setMessage("");
        setLastResult(null);
        try {
            const runValues = values || testValuesFor(cmd.params || [], {});
            const result = await RunPassthroughCommand(cmd.name, runValues, true);
            setLastResult(result as RunResult);
            await refresh();
        } catch (err) {
            setMessage(String(err));
        } finally {
            setRunningName("");
        }
    };

    const saveAndTest = async () => {
        const name = form.name.trim() || text(lang, "当前草稿", "current draft");
        if (!confirmTestRun(name)) return;
        const saved = await save();
        if (saved) await runTest(saved, testValuesFor(saved.params || [], testValues), true);
    };

    const preview = async (cmd: PassthroughCommand) => {
        try {
            const values = testValuesFor(cmd.params || [], testValues);
            const args = await PreviewPassthroughCommand(cmd.name, values);
            setMessage((args || []).map(quoteArg).join(" "));
        } catch (err) {
            setMessage(String(err));
        }
    };

    const previewDraft = async () => {
        setMessage("");
        if (draftError) {
            setMessage(draftError);
            return;
        }
        try {
            const values = valuesForPreview(draft.params || [], testValues);
            const args = await PreviewPassthroughDraftCommand(draft, values);
            setMessage((args || []).map(quoteArg).join(" "));
        } catch (err) {
            setMessage(String(err));
        }
    };

    const copyText = async (value: string) => {
        try {
            await navigator.clipboard.writeText(value);
            setMessage(text(lang, "远程命令已复制。", "Remote command copied."));
        } catch {
            setMessage(value);
        }
    };

    const copyRunCommand = async (cmd: PassthroughCommand) => {
        await copyText(commandExample(cmd));
    };

    const copyRegistrationCommand = async (cmd: PassthroughCommand) => {
        try {
            await copyText(await ExportPassthroughCommand(cmd.name));
        } catch (err) {
            setMessage(String(err));
        }
    };

    const toggleAllowExec = async (allowExec: boolean) => {
        if (allowExec) {
            const confirmed = await showConfirm(text(lang, "确认允许 /exec 一次性系统命令？该能力可从 AI 助手或 IM 通道执行系统程序。", "Allow /exec one-time system commands? This can run system programs from the AI assistant or IM channels."));
            if (!confirmed) return;
        }
        setMessage("");
        try {
            const saved = await SavePassthroughSettings({ ...settings, allow_exec: allowExec });
            setSettings(saved || { allow_exec: allowExec });
            setMessage(allowExec ? text(lang, "已允许 /exec 一次性系统命令。", "/exec one-time system commands enabled.") : text(lang, "已关闭 /exec 一次性系统命令。", "/exec one-time system commands disabled."));
        } catch (err) {
            setMessage(String(err));
        }
    };

    const deleteCommand = async (cmd: PassthroughCommand) => {
        const confirmed = await showConfirm(text(lang, `确认删除直通任务 ${cmd.name}？`, `Delete passthrough task ${cmd.name}?`));
        if (!confirmed) return;
        setMessage("");
        try {
            await DeletePassthroughCommand(cmd.name);
            await refresh();
            resetForm();
            setMessage(text(lang, "已删除直通任务。", "Passthrough task deleted."));
        } catch (err) {
            setMessage(String(err));
        }
    };

    const toggleCommandEnabled = async (cmd: PassthroughCommand) => {
        const nextEnabled = !cmd.enabled;
        if (nextEnabled) {
            const confirmed = await showConfirm(text(lang, `确认启用直通任务 ${cmd.name}？启用后可从 AI 助手或 IM 通道执行。`, `Enable passthrough task ${cmd.name}? It can then run from the AI assistant or IM channels.`));
            if (!confirmed) return;
        }
        setMessage("");
        try {
            await SetPassthroughCommandEnabled(cmd.name, nextEnabled);
            await refresh();
            setMessage(nextEnabled ? text(lang, "已启用直通任务。", "Passthrough task enabled.") : text(lang, "已禁用直通任务。", "Passthrough task disabled."));
        } catch (err) {
            setMessage(String(err));
        }
    };

    return (
        <div style={{ position: "relative" }}>
            <div>
                <div style={{ display: "flex", alignItems: "flex-start", marginBottom: 8, gap: 8 }}>
                    <div style={{ textAlign: "left", flex: 1, minWidth: 0 }}>
                        <div style={{ fontSize: "0.86rem", fontWeight: 700, color: colors.text }}>
                            {text(lang, "直通任务", "Passthrough Tasks")}
                        </div>
                        <div style={{ marginTop: 3, color: colors.textSecondary, fontSize: "0.72rem", lineHeight: 1.45 }}>
                            {text(lang, "直通任务是预先注册的命令模板，可在 AI 助手或 IM 通道中用 /run 直接执行；即使 LLM 或 agent 不可用，也会把输出原样返回发起通道。", "Passthrough tasks are pre-registered command templates that can be run with /run from the AI assistant or IM channels; even when the LLM or agent is unavailable, output is returned directly to the originating channel.")}
                        </div>
                        {registryPath && (
                            <div style={{ marginTop: 5, color: colors.textMuted, fontSize: "0.68rem", wordBreak: "break-all" }}>
                                {text(lang, "注册表：", "Registry: ")}{registryPath}
                            </div>
                        )}
                        <label style={{ display: "flex", gap: 8, alignItems: "center", marginTop: 8, color: colors.textSecondary, fontSize: "0.72rem" }}>
                            <input type="checkbox" checked={!!settings.allow_exec} onChange={(e) => toggleAllowExec(e.target.checked)} />
                            {text(lang, "允许 /exec 一次性系统命令（需 --confirm，不经过 shell）", "Allow /exec one-time system commands (requires --confirm, no shell interpretation)")}
                        </label>
                        <div style={{ marginTop: 5, color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.45 }}>
                            {text(lang, "远程开关：/runctl exec enable 或 /runctl exec disable", "Remote toggle: /runctl exec enable or /runctl exec disable")}
                        </div>
                        <div style={{ marginTop: 5, color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.45 }}>
                            {text(lang, "远程注册复杂形参可用 --params-json，例：/runctl save git-status --cmd 'git -C ${target} status --short' --params-json '[{\"name\":\"target\",\"type\":\"path\",\"required\":true,\"example\":\"D:\\\\workprj\\\\aicoder\"}]' --confirm", "Use --params-json for complex remote registration, for example: /runctl save git-status --cmd 'git -C ${target} status --short' --params-json '[{\"name\":\"target\",\"type\":\"path\",\"required\":true,\"example\":\"D:\\\\workprj\\\\aicoder\"}]' --confirm")}
                        </div>
                        {settings.allow_exec && (
                            <div style={{ marginTop: 6, color: colors.warning, fontSize: "0.7rem", lineHeight: 1.45 }}>
                                {text(lang, "远程应急示例：/exec git status --short --confirm。/exec 会记录审计，只运行程序和 argv，不解释管道、重定向或 &&。", "Emergency example: /exec git status --short --confirm. /exec is audited and runs only a program plus argv; pipes, redirection, and && are not interpreted.")}
                            </div>
                        )}
                    </div>
                    <button style={{ ...remotePrimaryActionButtonStyle, whiteSpace: "nowrap", flexShrink: 0 }} onClick={openNewForm}>
                        {text(lang, "+ 新建", "+ New")}
                    </button>
                </div>
                <div style={remoteTableContainerStyle}>
                    <table style={{ width: "100%", borderCollapse: "collapse" }}>
                        <thead>
                            <tr style={remoteTableHeaderRowStyle}>
                                <th style={remoteTableHeaderCellStyle}>{text(lang, "名称", "Name")}</th>
                                <th style={remoteTableHeaderCellStyle}>{text(lang, "命令模板", "Command")}</th>
                                <th style={remoteTableHeaderCellStyle}>{text(lang, "形参", "Params")}</th>
                                <th style={remoteTableHeaderCellStyle}>{text(lang, "状态", "Status")}</th>
                                <th style={remoteTableHeaderCellStyle}>{text(lang, "操作", "Actions")}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {commands.length === 0 ? (
                                <tr><td colSpan={5} style={remoteEmptyStateStyle}>{text(lang, "暂无直通任务。", "No passthrough tasks yet.")}</td></tr>
                            ) : commands.map((cmd) => (
                                <tr key={cmd.name} style={{ borderTop: `1px solid ${colors.border}` }}>
                                    <td style={remoteTableCellStyle}>
                                        <div style={{ fontWeight: 700 }}>{cmd.title || cmd.name}</div>
                                        <div style={{ color: colors.textMuted, fontSize: "0.7rem" }}>{cmd.name}</div>
                                    </td>
                                    <td style={{ ...remoteTableCellStyle, maxWidth: 300, wordBreak: "break-all" }}>{commandLineFromCommand(cmd)}</td>
                                    <td style={remoteTableCellStyle}>{(cmd.params || []).map((p) => p.name).join(", ") || "-"}</td>
                                    <td style={remoteTableCellStyle}>
                                        <span style={{ color: cmd.enabled ? colors.success : colors.textMuted, fontWeight: 700 }}>
                                            {cmd.enabled ? text(lang, "启用", "Enabled") : text(lang, "禁用", "Disabled")}
                                        </span>
                                        {cmd.confirm_required && <div style={{ fontSize: "0.68rem", color: colors.warning }}>{text(lang, "需确认", "Confirm")}</div>}
                                        {cmd.last_status && <div style={{ fontSize: "0.68rem", color: colors.textMuted }}>{cmd.last_status} {cmd.last_exit_code}</div>}
                                    </td>
                                    <td style={remoteTableCellStyle}>
                                        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                                            <button style={remoteActionButtonStyle} onClick={() => editCommand(cmd)}>{text(lang, "编辑", "Edit")}</button>
                                            <button style={remoteActionButtonStyle} onClick={() => copyRunCommand(cmd)}>{text(lang, "复制运行", "Copy Run")}</button>
                                            <button style={remoteActionButtonStyle} onClick={() => copyRegistrationCommand(cmd)}>{text(lang, "复制注册", "Copy Save")}</button>
                                            <button style={remoteActionButtonStyle} disabled={runningName === cmd.name} onClick={() => runTest(cmd)}>{text(lang, "测试", "Test")}</button>
                                            <button style={remoteActionButtonStyle} onClick={() => preview(cmd)}>{text(lang, "预览", "Preview")}</button>
                                            <button style={remoteActionButtonStyle} onClick={() => toggleCommandEnabled(cmd)}>
                                                {cmd.enabled ? text(lang, "禁用", "Disable") : text(lang, "启用", "Enable")}
                                            </button>
                                            <button style={remoteDangerActionButtonStyle} onClick={() => deleteCommand(cmd)}>
                                                {text(lang, "删除", "Delete")}
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
                {lastResult && (
                    <pre style={{ marginTop: 10, padding: 10, border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.bg, color: colors.text, whiteSpace: "pre-wrap", fontSize: "0.74rem" }}>
                        {lastResult.output || `(no output) ${lastResult.status} exit=${lastResult.exit_code}`}
                    </pre>
                )}
                <div style={{ marginTop: 12 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
						<div style={{ fontSize: "0.78rem", fontWeight: 700, color: colors.text }}>
							{text(lang, "最近审计记录", "Recent Audit")}
						</div>
                        <div style={{ fontSize: "0.68rem", color: colors.textMuted }}>
                            {text(lang, "显示 argv；敏感参数值会脱敏。", "Shows argv; sensitive parameter values are redacted.")}
                        </div>
                        <div style={{ flex: 1 }} />
                        <button style={remoteActionButtonStyle} onClick={refresh}>{text(lang, "刷新", "Refresh")}</button>
                    </div>
                    <div style={remoteTableContainerStyle}>
                        <table style={{ width: "100%", borderCollapse: "collapse" }}>
                            <thead>
                                <tr style={remoteTableHeaderRowStyle}>
                                    <th style={remoteTableHeaderCellStyle}>{text(lang, "时间", "Time")}</th>
                                    <th style={remoteTableHeaderCellStyle}>{text(lang, "来源", "Source")}</th>
                                    <th style={remoteTableHeaderCellStyle}>{text(lang, "命令", "Command")}</th>
                                    <th style={remoteTableHeaderCellStyle}>{text(lang, "状态", "Status")}</th>
                                </tr>
                            </thead>
                            <tbody>
								{audit.length === 0 ? (
									<tr><td colSpan={4} style={remoteEmptyStateStyle}>{text(lang, "暂无审计记录。", "No audit records yet.")}</td></tr>
                                ) : audit.map((entry) => (
                                    <tr key={entry.id} style={{ borderTop: `1px solid ${colors.border}` }}>
                                        <td style={remoteTableCellStyle}>{formatAuditTime(entry.started_at)}</td>
                                        <td style={remoteTableCellStyle}>{entry.source || "-"}</td>
                                        <td style={{ ...remoteTableCellStyle, maxWidth: 280, wordBreak: "break-all" }}>
                                            <div style={{ fontWeight: 700 }}>{entry.kind} {entry.command_name}</div>
                                            <div style={{ color: colors.textMuted, fontSize: "0.68rem" }}>{(entry.args || []).join(" ")}</div>
                                            {entry.error && <div style={{ color: colors.danger, fontSize: "0.68rem" }}>{entry.error}</div>}
                                        </td>
                                        <td style={remoteTableCellStyle}>{entry.status} exit={entry.exit_code} {entry.duration_ms}ms</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>

            {showForm && <>
                <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.4)", zIndex: 1000 }} onClick={closeForm} />
                <div style={{
                    position: "fixed",
                    top: "50%",
                    left: "50%",
                    transform: "translate(-50%, -50%)",
                    width: "min(520px, 90vw)",
                    maxHeight: "85vh",
                    overflowY: "auto",
                    border: `1px solid ${colors.border}`,
                    borderRadius: radius.lg,
                    padding: 16,
                    background: colors.surface,
                    boxShadow: "0 8px 32px rgba(0,0,0,0.5)",
                    zIndex: 1001,
                }}>
                <div style={{ display: "flex", alignItems: "center", marginBottom: 10 }}>
                    <div style={{ fontSize: "0.82rem", fontWeight: 700, color: colors.text }}>
                        {editing ? text(lang, "编辑直通任务", "Edit Task") : text(lang, "新增直通任务", "New Task")}
                    </div>
                    <div style={{ flex: 1 }} />
                    <button style={remoteActionButtonStyle} onClick={closeForm}>{text(lang, "关闭", "Close")}</button>
                </div>
                <div style={{ display: "grid", gap: 9 }}>
                    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                        <div>
                            <label style={labelStyle}>{text(lang, "任务名", "Name")}</label>
                            <input style={inputStyle} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="repair-env" />
                        </div>
                        <div>
                            <label style={labelStyle}>{text(lang, "显示名", "Title")}</label>
                            <input style={inputStyle} value={form.title || ""} onChange={(e) => setForm({ ...form, title: e.target.value })} />
                        </div>
                    </div>
                    <div>
                        <label style={labelStyle}>{text(lang, "命令模板", "Command Template")}</label>
                        <input style={{ ...inputStyle, fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace" }} value={commandLine} onChange={(e) => setCommandLine(e.target.value)} placeholder={text(lang, "例如：git -C ${target} status --short", "Example: git -C ${target} status --short")} />
                        <div style={{ marginTop: 4, color: colors.textMuted, fontSize: "0.68rem", lineHeight: 1.4 }}>
                            {text(lang, "系统命令选择 runtime=direct，例如 git -C ${target} status；脚本任务选择对应 runtime，例如 powershell 脚本写 D:\\ops\\repair.ps1 --target ${target}。参数占位符写成 ${name}，执行时按 argv 传入，不经过 shell。", "Use runtime=direct for executables, for example git -C ${target} status. For scripts, choose the matching runtime, for example D:\\ops\\repair.ps1 --target ${target} with powershell. Use ${name} placeholders; execution passes argv directly without shell interpretation.")}
                        </div>
                        {showDraftError && draftError && <div style={{ marginTop: 4, color: colors.danger, fontSize: "0.68rem" }}>{draftError}</div>}
                    </div>
                    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                        <div>
                            <label style={labelStyle}>{text(lang, "运行方式", "Runtime")}</label>
                            <select style={inputStyle} value={form.runtime} onChange={(e) => setForm({ ...form, runtime: e.target.value })}>
                                {["direct", "auto", "powershell", "pwsh", "cmd", "bash", "python", "node"].map((r) => <option key={r} value={r}>{r}</option>)}
                            </select>
                        </div>
                        <div>
                            <label style={labelStyle}>{text(lang, "超时秒数", "Timeout")}</label>
                            <input style={inputStyle} type="number" min={1} max={3600} value={form.timeout_seconds} onChange={(e) => setForm({ ...form, timeout_seconds: Number(e.target.value) })} />
                        </div>
                    </div>
                    <div>
                        <label style={labelStyle}>{text(lang, "工作目录", "Working Directory")}</label>
                        <input style={inputStyle} value={form.cwd || ""} onChange={(e) => setForm({ ...form, cwd: e.target.value })} placeholder={text(lang, "留空则使用脚本目录或当前目录", "Blank uses the script directory or current directory")} />
                    </div>
                    <div>
                        <div style={{ display: "flex", alignItems: "center", marginBottom: 6, gap: 8 }}>
                            <label style={{ ...labelStyle, marginBottom: 0 }}>{text(lang, "形参", "Parameters")}</label>
                            <div style={{ flex: 1 }} />
                            <button style={remoteActionButtonStyle} onClick={addParam}>{text(lang, "添加形参", "Add Param")}</button>
                        </div>
                        <div style={remoteTableContainerStyle}>
                            <table style={{ width: "100%", borderCollapse: "collapse" }}>
                                <thead>
                                    <tr style={remoteTableHeaderRowStyle}>
                                        <th style={remoteTableHeaderCellStyle}>{text(lang, "名称", "Name")}</th>
                                        <th style={remoteTableHeaderCellStyle}>{text(lang, "类型", "Type")}</th>
                                        <th style={remoteTableHeaderCellStyle}>{text(lang, "必填", "Req")}</th>
                                        <th style={remoteTableHeaderCellStyle}>{text(lang, "默认", "Default")}</th>
										<th style={remoteTableHeaderCellStyle}>{text(lang, "测试值/示例", "Test/Example")}</th>
                                        <th style={remoteTableHeaderCellStyle}></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {params.length === 0 ? (
                                        <tr><td colSpan={6} style={remoteEmptyStateStyle}>{text(lang, "暂无形参。", "No parameters.")}</td></tr>
                                    ) : params.map((p, index) => (
                                        <tr key={index} style={{ borderTop: `1px solid ${colors.border}` }}>
                                            <td style={remoteTableCellStyle}><input style={inputStyle} value={p.name} onChange={(e) => updateParam(index, { name: e.target.value })} placeholder="target" /></td>
                                            <td style={remoteTableCellStyle}>
                                                <select style={inputStyle} value={p.type || "text"} onChange={(e) => updateParam(index, { type: e.target.value })}>
                                                    {paramTypes.map((typeName) => <option key={typeName} value={typeName}>{typeName}</option>)}
                                                </select>
                                            </td>
                                            <td style={remoteTableCellStyle}><input type="checkbox" checked={p.required !== false} onChange={(e) => updateParam(index, { required: e.target.checked })} /></td>
                                            <td style={remoteTableCellStyle}><input style={inputStyle} value={p.default || ""} onChange={(e) => updateParam(index, { default: e.target.value })} /></td>
                                            <td style={remoteTableCellStyle}><input style={inputStyle} value={testValues[p.name] || ""} onChange={(e) => setTestValues({ ...testValues, [p.name]: e.target.value })} /></td>
                                            <td style={remoteTableCellStyle}>
                                                <div style={{ display: "flex", gap: 4 }}>
                                                    <button style={remoteActionButtonStyle} onClick={() => insertParamToken(p.name)}>{text(lang, "插入", "Insert")}</button>
                                                    <button style={remoteDangerActionButtonStyle} onClick={() => removeParam(index)}>{text(lang, "删", "Del")}</button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>
                    <label style={{ display: "flex", gap: 8, alignItems: "center", color: colors.textSecondary, fontSize: "0.76rem" }}>
                        <input type="checkbox" checked={form.confirm_required} onChange={(e) => setForm({ ...form, confirm_required: e.target.checked })} />
                        {text(lang, "远程执行需要 --confirm", "Require --confirm for remote runs")}
                    </label>
                    <label style={{ display: "flex", gap: 8, alignItems: "center", color: colors.textSecondary, fontSize: "0.76rem" }}>
                        <input type="checkbox" checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />
                        {text(lang, "启用", "Enabled")}
                    </label>
                    {selectedExample && (
                        <pre style={{ padding: 8, background: colors.bg, border: `1px solid ${colors.border}`, borderRadius: radius.md, color: colors.text, whiteSpace: "pre-wrap", fontSize: "0.72rem" }}>
                            {`${text(lang, "执行示例：", "Run example:")}\n${selectedExample}`}
                        </pre>
                    )}
                    <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                        <button style={remotePrimaryActionButtonStyle} onClick={save} disabled={!!draftError}>{text(lang, "保存", "Save")}</button>
                        <button style={remoteActionButtonStyle} onClick={previewDraft} disabled={!!draftError}>{text(lang, "预览 argv", "Preview argv")}</button>
                        <button style={remoteActionButtonStyle} onClick={saveAndTest} disabled={!!runningName || !!draftError}>{text(lang, "保存并测试", "Save & Test")}</button>
                    </div>
                    {message && <div style={{ fontSize: "0.74rem", color: message.includes("Error") || message.includes("错误") || message.includes("failed") ? colors.danger : colors.textSecondary, whiteSpace: "pre-wrap" }}>{message}</div>}
                </div>
            </div></>}
        </div>
    );
}
