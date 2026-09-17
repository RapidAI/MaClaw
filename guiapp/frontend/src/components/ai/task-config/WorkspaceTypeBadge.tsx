import type { CSSProperties } from "react";
import { TaskConfigIcon } from "./taskConfigIcons";
import type { WorkspaceKind } from "./taskDraft";

/**
 * 工作空间类型徽标（📁 本地 / ☁️ 云端 / 🖧 远程，三色小徽标）。
 * 引导页配置条与左侧任务列表共用（设计 §5）。
 * 三色为点缀色（设计定稿），不随主题 token 变化。
 * 文案一律由调用方按语言传入（label 必填），组件内不写死语言。
 */
const KIND_STYLE: Record<WorkspaceKind, { bg: string; color: string }> = {
    local: { bg: "#eef4fd", color: "#2f78d0" },
    cloud: { bg: "#ecfdf3", color: "#16a34a" },
    remote: { bg: "#f3eefe", color: "#7c3aed" },
};

export interface WorkspaceTypeBadgeProps {
    kind: WorkspaceKind;
    /** 徽标文案（调用方按语言传入，如「本地」/ "Local"）。 */
    label: string;
    style?: CSSProperties;
}

export function WorkspaceTypeBadge({ kind, label, style }: WorkspaceTypeBadgeProps) {
    const meta = KIND_STYLE[kind];
    const icon = kind === "local" ? "folder" : kind === "cloud" ? "cloud" : "server";
    const badgeStyle: CSSProperties = {
        display: "inline-flex",
        alignItems: "center",
        gap: 3,
        fontSize: 11,
        lineHeight: 1.4,
        borderRadius: 6,
        padding: "1px 6px",
        whiteSpace: "nowrap",
        flexShrink: 0,
        background: meta.bg,
        color: meta.color,
        fontFamily: "system-ui, -apple-system, sans-serif",
        ...style,
    };
    return (
        <span style={badgeStyle} data-testid={`workspace-badge-${kind}`}>
            <TaskConfigIcon name={icon} size={11} />
            {label}
        </span>
    );
}
