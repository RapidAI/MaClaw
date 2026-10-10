import type { InProgressBotTask } from "../bots/desktopBots";
import {
    colors,
    remoteEmptyStateStyle,
    remoteTableCellStyle,
    remoteTableContainerStyle,
    remoteTableHeaderCellStyle,
    remoteTableHeaderRowStyle,
} from "./styles";

type Props = {
    tasks: InProgressBotTask[];
    names: Record<string, string>;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
};

function startedLabel(ms: number): string {
    if (!ms) return "—";
    const date = new Date(ms);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function phaseLabel(phase: InProgressBotTask["phase"], localizeText: Props["localizeText"]): string {
    if (phase === "execute") return localizeText("Execute", "执行", "執行");
    if (phase === "plan") return localizeText("Arrange", "安排", "安排");
    return "—";
}

function statusLabel(status: InProgressBotTask["status"], localizeText: Props["localizeText"]): { text: string; color: string } {
    if (status === "waiting") return { text: localizeText("Waiting for you", "等待你", "等待你"), color: colors.warning };
    if (status === "queued") return { text: localizeText("Queued", "排队", "排隊"), color: colors.textSecondary };
    return { text: localizeText("In progress", "进行中", "進行中"), color: colors.primaryDark };
}

export function BotTasksPanel({ tasks, names, localizeText }: Props) {
    return (
        <div>
            <div style={{ fontSize: "0.86rem", fontWeight: 700, color: colors.text }}>
                {localizeText("Bot tasks", "Bot任务", "Bot任務")}
            </div>
            <div style={{ marginTop: 3, marginBottom: 8, color: colors.textSecondary, fontSize: "0.72rem", lineHeight: 1.45 }}>
                {localizeText(
                    "Tasks this account's bots are still working on. A finished turn leaves this list.",
                    "当前账号的 Bot 还在处理的任务。做完后会从这里消失。",
                    "目前帳號的 Bot 還在處理的任務。做完後會從這裡消失。",
                )}
            </div>
            <div style={remoteTableContainerStyle}>
                <table style={{ width: "100%", borderCollapse: "collapse" }}>
                    <thead>
                        <tr style={remoteTableHeaderRowStyle}>
                            <th style={remoteTableHeaderCellStyle}>{localizeText("Bot", "Bot", "Bot")}</th>
                            <th style={remoteTableHeaderCellStyle}>{localizeText("Task", "任务", "任務")}</th>
                            <th style={remoteTableHeaderCellStyle}>{localizeText("Phase", "阶段", "階段")}</th>
                            <th style={remoteTableHeaderCellStyle}>{localizeText("Started", "开始", "開始")}</th>
                            <th style={remoteTableHeaderCellStyle}>{localizeText("Status", "状态", "狀態")}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {tasks.length === 0 ? (
                            <tr>
                                <td colSpan={5} style={remoteEmptyStateStyle}>
                                    {localizeText("No bot tasks in progress.", "暂无进行中的 Bot 任务。", "暫無進行中的 Bot 任務。")}
                                </td>
                            </tr>
                        ) : tasks.map((task) => {
                            const status = statusLabel(task.status, localizeText);
                            const title = names[task.botId] || task.botId;
                            const text = task.text || "—";
                            return (
                                <tr key={`${task.botId}:${task.id}`} style={{ borderTop: `1px solid ${colors.border}` }}>
                                    <td style={remoteTableCellStyle}>
                                        <div style={{ fontWeight: 700 }}>{title}</div>
                                    </td>
                                    <td style={{ ...remoteTableCellStyle, maxWidth: 360, wordBreak: "break-word" }} title={task.text}>
                                        {text}
                                    </td>
                                    <td style={remoteTableCellStyle}>{phaseLabel(task.phase, localizeText)}</td>
                                    <td style={remoteTableCellStyle}>{startedLabel(task.startedAt)}</td>
                                    <td style={{ ...remoteTableCellStyle, color: status.color, fontWeight: 700 }}>{status.text}</td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
