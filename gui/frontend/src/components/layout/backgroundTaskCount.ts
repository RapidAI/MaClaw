export function isActiveManageableBackgroundStatus(status: unknown): boolean {
    const normalized = String(status || '').trim().toLowerCase();
    return normalized === 'running' || normalized === 'paused';
}

export function countActiveBackgroundLoops(loops: unknown): number {
    if (!Array.isArray(loops)) return 0;
    return loops.filter((loop: any) => isActiveManageableBackgroundStatus(loop?.status ?? loop?.Status)).length;
}
