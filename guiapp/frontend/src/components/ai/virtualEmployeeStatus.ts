export function isVirtualEmployeeOnline(ve?: { online_status?: string } | null): boolean {
    return String(ve?.online_status || "").trim().toLowerCase() === "online";
}
