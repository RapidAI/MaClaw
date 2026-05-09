import type { GroupDiscussionPanelStatus } from "./aiAssistantPanelTypes";

export function getPrimaryDiscussionTraceFocus(status?: GroupDiscussionPanelStatus | null): string {
    const discussions = Array.isArray(status?.discussions) ? [...status!.discussions] : [];
    const candidate = discussions
        .filter((item: any) => String(item?.id || "").trim())
        .sort((a: any, b: any) => discussionTime(b) - discussionTime(a))
        .find((item: any) => item?.result_summary || item?.ready_to_summarize || String(item?.status || "").toLowerCase() === "decided") || discussions[0];
    const id = String((candidate as any)?.id || "").trim();
    return id ? "discussion:" + id : "";
}

function discussionTime(item: any): number {
    const value = Date.parse(String(item?.updated_at || item?.created_at || ""));
    return Number.isFinite(value) ? value : 0;
}
