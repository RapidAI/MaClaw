export type HistoryDiscussionLike = {
    local_relation?: string;
    role?: string;
    readonly?: boolean;
    status?: string;
};

export function historyRelationFromRole(role: unknown): string {
    const normalized = String(role || '').trim().toLowerCase();
    if (normalized === 'initiator') return 'initiated_by_me';
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

    const relation = getHistoryDiscussionRelation(discussion);
    if (relation === 'initiated_by_me') return false;
    if (relation) return true;

    if (discussion?.readonly === true) return true;
    return relation !== 'initiated_by_me';
}
