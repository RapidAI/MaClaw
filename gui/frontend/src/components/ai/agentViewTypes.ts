export type AgentViewFieldType =
    | "text"
    | "textarea"
    | "number"
    | "date"
    | "datetime"
    | "select"
    | "multiselect"
    | "object_form"
    | "array_table"
    | "boolean"
    | "file"
    | "directory"
    | "hidden"
    | "user_ref"
    | "department_ref"
    | "business_ref";

export interface AgentViewOption {
    label: string;
    value: string;
}

export interface AgentViewTableColumn {
    name: string;
    label?: string;
    type?: "text" | "number" | "select" | "boolean" | "date" | "directory" | "file";
    required?: boolean;
    readOnly?: boolean;
    sensitive?: boolean;
    options?: Array<string | AgentViewOption>;
    constValue?: unknown;
    min?: number;
    max?: number;
    exclusiveMin?: number;
    exclusiveMax?: number;
    step?: number;
    minLength?: number;
    maxLength?: number;
    pattern?: string;
    format?: string;
}

export interface AgentViewField {
    name: string;
    label?: string;
    type?: AgentViewFieldType;
    required?: boolean;
    readOnly?: boolean;
    sensitive?: boolean;
    description?: string;
    placeholder?: string;
    defaultValue?: unknown;
    value?: unknown;
    constValue?: unknown;
    error?: string;
    options?: Array<string | AgentViewOption>;
    columns?: AgentViewTableColumn[];
    min?: number;
    max?: number;
    exclusiveMin?: number;
    exclusiveMax?: number;
    step?: number;
    minItems?: number;
    maxItems?: number;
    uniqueItems?: boolean;
    minLength?: number;
    maxLength?: number;
    pattern?: string;
    format?: string;
    dependentRequired?: AgentViewDependentRequired;
    // Prefill provenance (set by workflow form prefill system)
    prefill_source?: "context" | "memory" | "knowledge" | "web";
    prefill_detail?: string;
    prefill_needs_confirm?: boolean;
}

export interface AgentViewStep {
    id?: string;
    title: string;
    status?: "pending" | "running" | "done" | "error";
    description?: string;
}

export type AgentViewDependentRequired = Record<string, string[]>;

export interface AgentViewVariant {
    id: string;
    label: string;
    description?: string;
    fields: AgentViewField[];
    dependentRequired?: AgentViewDependentRequired;
}

export interface AgentViewActionSummary {
    summary: string;
    risk?: "low" | "medium" | "high";
    effects?: string[];
    reviewData?: Record<string, unknown>;
    parameters?: Record<string, unknown>;
}

export interface AgentViewResultItem {
    id?: string;
    title: string;
    subtitle?: string;
    status?: string;
    data?: Record<string, unknown>;
    actions?: AgentViewResultAction[];
}

export interface AgentViewResultAction {
    label: string;
    viewId?: string;
    data?: Record<string, unknown>;
    primary?: boolean;
}

export interface AgentViewWizardStep {
    id: string;
    title: string;
    description?: string;
    fields: AgentViewField[];
    dependentRequired?: AgentViewDependentRequired;
}

export interface AgentViewResourceOption extends AgentViewOption {
    description?: string;
    status?: string;
    data?: Record<string, unknown>;
}

export type AgentView =
    | {
        type: "form";
        id?: string;
        title: string;
        description?: string;
        fields: AgentViewField[];
        dependentRequired?: AgentViewDependentRequired;
        variants?: AgentViewVariant[];
        formErrors?: string[];
        submitLabel?: string;
        /** When true, the form supports auto-fill from an uploaded resume/CV document. */
        accepts_resume?: boolean;
        /** When set, the form accepts optional supplementary documents as reference context. */
        accepts_supplementary?: {
            label: string;
            description: string;
            max_files?: number;
            accepted_types?: string[];
        };
    }
    | {
        type: "wizard";
        id?: string;
        title: string;
        description?: string;
        steps: AgentViewWizardStep[];
        formErrors?: string[];
        submitLabel?: string;
    }
    | {
        type: "table_editor";
        id?: string;
        title: string;
        description?: string;
        columns: AgentViewTableColumn[];
        rows: Array<Record<string, unknown>>;
        dataKey?: string;
        hiddenData?: Record<string, unknown>;
        minItems?: number;
        maxItems?: number;
        uniqueItems?: boolean;
        dependentRequired?: AgentViewDependentRequired;
        formErrors?: string[];
        submitLabel?: string;
    }
    | {
        type: "resource_picker";
        id?: string;
        title: string;
        description?: string;
        resourceType?: string;
        options: AgentViewResourceOption[];
        multiple?: boolean;
        value?: string | string[];
        dataKey?: string;
        hiddenData?: Record<string, unknown>;
        submitLabel?: string;
    }
    | {
        type: "field_mapper";
        id?: string;
        title: string;
        description?: string;
        sourceFields: string[];
        targetFields: AgentViewField[];
        value?: Record<string, string>;
        dataKey?: string;
        hiddenData?: Record<string, unknown>;
        submitLabel?: string;
    }
    | {
        type: "approval";
        id?: string;
        title: string;
        description?: string;
        action: AgentViewActionSummary;
        approveLabel?: string;
        rejectLabel?: string;
        /** Show a free-text note field for the decision. */
        noteLabel?: string;
        notePlaceholder?: string;
        /** When true, note is required for both approve and reject. */
        requireNote?: boolean;
        /** When true (default for AppView approvals), note is required only on reject. */
        requireNoteOnReject?: boolean;
    }
    | {
        type: "progress";
        id?: string;
        title: string;
        description?: string;
        steps: AgentViewStep[];
        actions?: AgentViewResultAction[];
    }
    | {
        type: "result_browser";
        id?: string;
        title: string;
        description?: string;
        results: AgentViewResultItem[];
    }
    | {
        type: "artifact";
        id?: string;
        title: string;
        description?: string;
        artifact: { label?: string; kind?: string; uri?: string; summary?: string };
    }
    /** Controlled multi-region workspace (maclaw.appview.v1). Nested views are never app_view. */
    | {
        type: "app_view";
        schema?: "maclaw.appview.v1" | string;
        id?: string;
        appId: string;
        sessionId?: string;
        title: string;
        description?: string;
        layout?: "workspace" | "record" | "report" | "tool" | string;
        viewRevision?: number;
        meta?: Record<string, unknown>;
        regions: {
            header?: { title?: string; subtitle?: string };
            nav?: AppViewNavItem[];
            main: AgentView | AgentView[];
            side?: AgentView | AgentView[];
            footer?: { actions?: AgentViewResultAction[] };
        };
        actions?: AppViewAction[];
    };

export interface AppViewNavItem {
    id: string;
    label: string;
    /** When set, selects the main view with this id; otherwise uses index order. */
    targetViewId?: string;
    icon?: string;
}

export interface AppViewAction {
    id?: string;
    label: string;
    primary?: boolean;
    viewId?: string;
    data?: Record<string, unknown>;
}
