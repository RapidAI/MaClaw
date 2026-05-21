export function isActiveManageableBackgroundStatus(status: unknown): boolean {
    const normalized = String(status || '').trim().toLowerCase();
    return normalized === 'running' || normalized === 'paused';
}

export function countActiveSshBackgroundTasks(loops: unknown): number {
    if (!Array.isArray(loops)) return 0;
    return loops.filter((loop: any) => {
        const slotKind = String(loop?.slot_kind ?? loop?.slotKind ?? loop?.SlotKind ?? '').trim().toLowerCase();
        return slotKind === 'ssh' && isActiveManageableBackgroundStatus(loop?.status ?? loop?.Status);
    }).length;
}
