/** Model name on one coding-workbench route chip. */
export function codingRouteChipModel(
    pref: string,
    cap: { model?: string; source?: string } | undefined,
    taskModel: string | undefined,
): string {
    const stored = String(cap?.model || "");
    const live = String(taskModel || "").trim();
    if (!live || !cap || cap.source === "route") return stored;
    if (pref === "primary" || pref === "auto" || cap.source === "primary" || cap.source === "fallback") {
        return live;
    }
    return stored;
}
