export type EmailVerificationState = {
    target: string;
    code: string;
    codeLength: number;
    sending: boolean;
    busy: boolean;
};

export function normalizeEmailVerificationTarget(value: string): string {
    return value.trim().toLowerCase();
}

export function canSubmitEmailVerification(state: EmailVerificationState): boolean {
    const currentTarget = normalizeEmailVerificationTarget(state.target);
    return currentTarget !== ''
        && !state.sending
        && !state.busy
		&& state.codeLength >= 4
		&& state.codeLength <= 8
		&& /^\d+$/.test(state.code)
		&& state.code.length === state.codeLength;
}

export function sanitizeEmailVerificationCode(value: string, codeLength: number): string {
    return value.replace(/\D/g, '').slice(0, Math.max(0, codeLength));
}

export function emailVerificationCooldownSeconds(value: unknown, fallback = 60): number {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed <= 0) return fallback;
    return Math.min(3600, Math.max(1, Math.ceil(parsed)));
}
