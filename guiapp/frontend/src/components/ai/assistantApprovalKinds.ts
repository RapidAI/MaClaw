// Approval kind identifiers used by the assistant scope-approval flow.
export const LOCAL_HIGH_RISK_APPROVAL_KIND = "local_high_risk_bash";
export const REMOTE_HIGH_RISK_APPROVAL_KIND = "remote_high_risk_bash";
export const REMOTE_DIRECTORY_WRITE_APPROVAL_KIND = "remote_shell_directory_write";
export const REMOTE_PATH_ACCESS_APPROVAL_KIND = "remote_path_access";
// Human-class tool shape. The dialog waits until the person answers or the task is cancelled.
export const COMMIT_SHAPE_APPROVAL_KIND = "commit_shape";

export function scopeApprovalShouldAutoDismiss(
    pending: { autoAllow: boolean; timeoutSeconds: number } | null,
    countdown: number,
): boolean {
    if (!pending) return false;
    if (pending.autoAllow && countdown <= 1) return true;
    return !pending.autoAllow && pending.timeoutSeconds > 0 && countdown <= 0;
}
