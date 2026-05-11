export enum WorkflowStatus {
    Unknown = "",
    Active = "active",
    Completed = "completed",
    Cancelled = "cancelled",
}

export function normalizeWorkflowStatus(status: unknown): WorkflowStatus {
    if (typeof status !== "string") return WorkflowStatus.Unknown;
    switch (status.trim()) {
        case WorkflowStatus.Active:
            return WorkflowStatus.Active;
        case WorkflowStatus.Completed:
            return WorkflowStatus.Completed;
        case WorkflowStatus.Cancelled:
            return WorkflowStatus.Cancelled;
        default:
            return WorkflowStatus.Unknown;
    }
}

export function isWorkflowActive(status: unknown): boolean {
    return normalizeWorkflowStatus(status) === WorkflowStatus.Active;
}
