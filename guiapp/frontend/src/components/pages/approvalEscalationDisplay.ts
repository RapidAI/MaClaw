/**
 * Pure display helpers for Hub escalation markers on the Apps approval console.
 * Kept free of React so Vitest can cover Chinese/English labels and attempt formatting.
 */

export type EscalationDisplayInstance = {
    escalationPending?: boolean;
    escalationApprovers?: string[];
    escalationExhaustedApprovers?: string[];
    escalationAttempts?: Record<string, number>;
};

function isZhLang(lang?: string): boolean {
    const n = String(lang || '').trim().toLowerCase();
    return n === 'zh' || n.startsWith('zh-') || n === 'cn' || n === 'zh_cn';
}

/** Format Hub escalation retry line for list/detail (pending peers + attempts). */
export function approvalEscalationRetryText(
    instance: EscalationDisplayInstance | undefined,
    lang?: string,
): string {
    if (!instance) return '';
    const peers = instance.escalationApprovers || [];
    if (!instance.escalationPending && peers.length === 0) return '';
    const attempts = instance.escalationAttempts || {};
    const parts = peers.slice(0, 4).map((id) => {
        const n = attempts[id];
        return n && n > 0 ? `${id}\u00d7${n}` : id;
    });
    const more = peers.length > 4 ? ` +${peers.length - 4}` : '';
    // 升级重投
    const head = isZhLang(lang) ? '\u5347\u7ea7\u91cd\u6295' : 'Escalation retry';
    return parts.length ? `${head}: ${parts.join(', ')}${more}` : head;
}

export function approvalEscalationExhaustedText(
    instance: EscalationDisplayInstance | undefined,
    lang?: string,
): string {
    const peers = instance?.escalationExhaustedApprovers || [];
    if (!peers.length) return '';
    // 离线耗尽
    const head = isZhLang(lang) ? '\u79bb\u7ebf\u8017\u5c3d' : 'Escalation exhausted';
    const body = peers.slice(0, 4).join(', ') + (peers.length > 4 ? ` +${peers.length - 4}` : '');
    return `${head}: ${body}`;
}

/** data-escalation attribute value for list rows. */
export function approvalEscalationDataAttr(
    instance: EscalationDisplayInstance | undefined,
): '' | 'pending' | 'exhausted' {
    if (!instance) return '';
    if (instance.escalationPending || (instance.escalationApprovers && instance.escalationApprovers.length > 0)) {
        return 'pending';
    }
    if (instance.escalationExhaustedApprovers && instance.escalationExhaustedApprovers.length > 0) {
        return 'exhausted';
    }
    return '';
}
