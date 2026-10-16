export enum WorkflowStatus {
    Unknown = "",
    Active = "active",
    Completed = "completed",
    Cancelled = "cancelled",
}

/** Accept legacy outcome spellings while exposing one stable UI vocabulary. */
export function normalizeWorkflowStatus(status: unknown): WorkflowStatus {
    if (typeof status !== "string") return WorkflowStatus.Unknown;
    switch (status.trim().toLowerCase()) {
        case WorkflowStatus.Active:
            return WorkflowStatus.Active;
        case WorkflowStatus.Completed:
        case "complete":
        case "success":
        case "succeeded":
        case "done":
        case "passed":
            return WorkflowStatus.Completed;
        case WorkflowStatus.Cancelled:
        case "canceled":
            return WorkflowStatus.Cancelled;
        default:
            return WorkflowStatus.Unknown;
    }
}

export function isWorkflowActive(status: unknown): boolean {
    return normalizeWorkflowStatus(status) === WorkflowStatus.Active;
}
