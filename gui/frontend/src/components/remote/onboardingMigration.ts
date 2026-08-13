export type OnboardingMigrationPackage = {
    exportId: string;
    status: string;
    sourceMachineId: string;
    sourceMachineName: string;
    updatedAt: string;
    size: number;
};

const objectValue = (value: unknown): Record<string, any> | null => (
    value && typeof value === "object" ? value as Record<string, any> : null
);

export function findOnboardingMigrationPackage(
    statusResponse: unknown,
    instancesResponse: unknown,
): OnboardingMigrationPackage | null {
    const status = objectValue(statusResponse);
    const instances = objectValue(instancesResponse);
    const rows = Array.isArray(instances?.instances) ? [...instances.instances] : [];
    const current = objectValue(status?.current_export);
    if (current?.export_id && !rows.some(row => String(row?.export_id || "") === String(current.export_id))) {
        rows.push({
            export_id: current.export_id,
            export_status: current.status,
            machine_id: current.source_machine_id,
            machine_name: current.source_machine_name,
            export_updated_at: current.updated_at,
            export_size: current.encrypted_size,
            export_claimed_by_machine_id: current.claimed_by_machine_id,
            has_export: true,
        });
    }

    const currentMachineId = String(status?.machine_id || "").trim();
    const candidate = rows.find(row => {
        const exportStatus = String(row?.export_status || "").trim().toLowerCase();
        const claimedByCurrentMachine = currentMachineId !== ""
            && String(row?.export_claimed_by_machine_id || "").trim() === currentMachineId;
        const available = exportStatus === "ready"
            || (exportStatus === "importing" && claimedByCurrentMachine);
        return row?.has_export !== false
            && String(row?.export_id || "").trim() !== ""
            && available;
    });
    if (!candidate) return null;

    return {
        exportId: String(candidate.export_id).trim(),
        status: String(candidate.export_status || "ready").trim().toLowerCase(),
        sourceMachineId: String(candidate.machine_id || candidate.instance_id || "").trim(),
        sourceMachineName: String(candidate.machine_name || candidate.instance_name || candidate.machine_id || "").trim(),
        updatedAt: String(candidate.export_updated_at || "").trim(),
        size: Math.max(0, Number(candidate.export_size || 0) || 0),
    };
}

export const MIGRATION_JOB_POLL_INTERVAL_MS = 900;
export const MIGRATION_JOB_POLL_MAX_FAILURES = 5;

export function migrationJobStatus(status: unknown): string {
    return String(status || "").trim().toLowerCase();
}

export function isTerminalMigrationJob(status: unknown): boolean {
    return ["succeeded", "failed", "canceled"].includes(migrationJobStatus(status));
}

export function isMigrationJobRunning(status: unknown, starting = false): boolean {
    return starting || migrationJobStatus(status) === "running";
}

export function shouldShowMigrationPassword(job: { status?: unknown } | null | undefined): boolean {
    if (!job) return true;
    return migrationJobStatus(job.status) === "failed";
}

export function migrationProgressPercent(value: unknown): number {
    const progress = Number(value || 0);
    if (!Number.isFinite(progress)) return 0;
    return Math.max(0, Math.min(100, Math.round(progress * 100)));
}

export function migrationJobId(job: { id?: unknown } | null | undefined): string {
    return String(job?.id || "").trim();
}

/** Best-effort string for Wails/JS thrown values (Error, string, or {message}). */
export function migrationErrorMessage(error: unknown): string {
    if (error instanceof Error) return error.message || error.name || "migration failed";
    if (typeof error === "string") return error;
    if (error && typeof error === "object") {
        const message = (error as { message?: unknown }).message;
        if (typeof message === "string" && message.trim()) return message;
        try {
            return JSON.stringify(error);
        } catch {
            // fall through
        }
    }
    return String(error || "migration failed");
}

/**
 * A wrong password and tampered ciphertext deliberately share the same
 * user-facing diagnosis: AEAD authentication cannot safely distinguish them.
 * A transport EOF is only credential-related once downloading has completed
 * and package decryption has begun.
 */
export function isMigrationCredentialError(error: unknown, progressText?: unknown): boolean {
    const message = migrationErrorMessage(error).toLowerCase();
    if (/password is incorrect|package is corrupted/.test(message)) return true;
    if (!/\b(?:unexpected )?eof\b/.test(message)) return false;
    return String(progressText || "").trim().toLowerCase() === "decrypting and verifying package";
}

/** True when the package could not be downloaded completely and can be retried. */
export function isMigrationTransferError(error: unknown): boolean {
    const message = migrationErrorMessage(error).toLowerCase();
    return /\b(?:unexpected )?eof\b|response ended unexpectedly|read hub migration response/.test(message);
}

/**
 * Optimistic UI state used between clicking Restore and Start* returning a
 * real backend job id. Intentionally omits `id` so a previous failed job id
 * cannot be mistaken for an active backend job.
 */
export function optimisticMigrationRunningJob(
    prev: Record<string, any> | null | undefined,
): Record<string, any> {
    return {
        status: "running",
        error: "",
        progress: typeof prev?.progress === "number" && Number.isFinite(prev.progress) ? prev.progress : 0,
        progress_text: typeof prev?.progress_text === "string" ? prev.progress_text : "",
    };
}

/**
 * Poll a backend migration job until it reaches a terminal status.
 * Transient getJob failures are tolerated up to maxFailures in a row so a
 * temporary bridge glitch cannot bounce the onboarding UI back to the
 * password form while the import is still running.
 */
export async function pollUntilMigrationJobTerminal(
    jobId: string,
    getJob: (id: string) => Promise<Record<string, any>>,
    options: {
        isCancelled?: () => boolean;
        onUpdate?: (job: Record<string, any>) => void;
        intervalMs?: number;
        maxFailures?: number;
        sleep?: (ms: number) => Promise<void>;
        initialJob?: Record<string, any> | null;
    } = {},
): Promise<Record<string, any>> {
    const id = String(jobId || "").trim();
    if (!id) throw new Error("migration job id missing");

    const intervalMs = Math.max(0, options.intervalMs ?? MIGRATION_JOB_POLL_INTERVAL_MS);
    const maxFailures = Math.max(1, options.maxFailures ?? MIGRATION_JOB_POLL_MAX_FAILURES);
    const sleep = options.sleep ?? ((ms: number) => new Promise(resolve => window.setTimeout(resolve, ms)));
    const isCancelled = options.isCancelled ?? (() => false);

    let nextJob: Record<string, any> = options.initialJob && migrationJobId(options.initialJob) === id
        ? options.initialJob
        : { id, status: "running" };
    let pollFailures = 0;

    while (!isTerminalMigrationJob(nextJob.status)) {
        await sleep(intervalMs);
        if (isCancelled()) {
            return nextJob;
        }
        try {
            const polled = await getJob(id);
            const polledId = migrationJobId(polled);
            // Reject empty payloads and mismatched ids so a stale/wrong response
            // cannot mark the wrong job terminal or reset the UI incorrectly.
            if (!polled || !polledId || polledId !== id) {
                pollFailures += 1;
                if (pollFailures >= maxFailures) {
                    throw new Error("migration job status unavailable");
                }
                continue;
            }
            pollFailures = 0;
            nextJob = polled;
            options.onUpdate?.(nextJob);
        } catch (pollError) {
            if (isCancelled()) {
                return nextJob;
            }
            // Re-throw only after the budget is exhausted. Distinct terminal
            // errors from getJob (e.g. permanent "not found") still count as
            // failures and eventually surface.
            pollFailures += 1;
            if (pollFailures >= maxFailures) {
                throw pollError instanceof Error
                    ? pollError
                    : new Error(migrationErrorMessage(pollError) || "migration job status unavailable");
            }
        }
    }
    return nextJob;
}

export async function completeOnboardingAfterMigration({
    markComplete,
    close,
    refresh,
    onRefreshError,
}: {
    markComplete: () => void | Promise<unknown>;
    close: () => void;
    refresh?: () => void | Promise<unknown>;
    onRefreshError?: (error: unknown) => void;
}): Promise<void> {
    // Completion is the only durable operation that may block closing. Once it
    // succeeds, app-level refresh is best-effort and cannot undo the import.
    await markComplete();
    close();
    if (!refresh) return;
    try {
        void Promise.resolve(refresh()).catch(error => onRefreshError?.(error));
    } catch (error) {
        onRefreshError?.(error);
    }
}
