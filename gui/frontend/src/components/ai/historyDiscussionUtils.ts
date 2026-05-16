export type HistoryDiscussionLike = {
    local_relation?: string;
    role?: string;
    readonly?: boolean;
    status?: string;
};

export function historyRelationFromRole(role: unknown): string {
    const normalized = String(role || '').trim().toLowerCase();
    if (['review', 'speak', 'speaker', 'observe', 'observer', 'participant'].includes(normalized)) return 'owned_ve_invited';
    return '';
}

export function getHistoryDiscussionRelation(discussion: HistoryDiscussionLike | null | undefined): string {
    const relation = String(discussion?.local_relation || '').trim().toLowerCase();
    return relation || historyRelationFromRole(discussion?.role);
}

export function isHistoryDiscussionReadOnly(discussion: HistoryDiscussionLike | null | undefined): boolean {
    const status = String(discussion?.status || '').trim().toLowerCase();
    if (status && status !== 'open') return true;

    if (discussion?.readonly === true) return true;

    const relation = getHistoryDiscussionRelation(discussion);
    return relation !== 'initiated_by_me';
}
