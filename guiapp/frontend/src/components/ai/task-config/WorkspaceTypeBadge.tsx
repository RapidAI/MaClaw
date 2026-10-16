import type { CSSProperties } from "react";
import { TaskConfigIcon } from "./taskConfigIcons";
import type { WorkspaceKind } from "./taskDraft";
import "./WorkspaceTypeBadge.css";

/**
 * 工作空间类型徽标（本地 / 云端 / 远程）。
 * 引导页配置条与左侧任务列表共用。
 * 浅色保持设计定稿的三色；暗色改用低饱和底，避免近白底在深色表面上发亮。
 * 颜色放在 CSS 里，不能写进 inline style，否则盖过 [data-ai-theme='dark']。
 * 文案一律由调用方按语言传入（label 必填），组件内不写死语言。
 */
const KIND_ICON: Record<WorkspaceKind, "folder" | "cloud" | "server"> = {
    local: "folder",
    cloud: "cloud",
    remote: "server",
};

export interface WorkspaceTypeBadgeProps {
    kind: WorkspaceKind;
    /** 徽标文案（调用方按语言传入，如「本地」/ "Local"）。 */
    label: string;
    style?: CSSProperties;
}

export function WorkspaceTypeBadge({ kind, label, style }: WorkspaceTypeBadgeProps) {
    return (
        <span
            className={`mc-workspace-kind mc-workspace-kind--${kind}`}
            style={style}
            data-testid={`workspace-badge-${kind}`}
        >
            <TaskConfigIcon name={KIND_ICON[kind]} size={11} />
            {label}
        </span>
    );
}
