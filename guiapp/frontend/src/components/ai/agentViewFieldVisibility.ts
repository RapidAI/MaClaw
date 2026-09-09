import type { AgentView, AgentViewField, AgentViewVariant, AgentViewVisibleWhen } from "./agentViewTypes";

/** Shown only when user picks 新建连接 (__new__). */
const CODING_REMOTE_NEW_ONLY_FIELDS = new Set([
    "remote_host",
    "remote_user",
    "remote_port",
    "ssh_key_path",
]);

/** Shown whenever a profile is selected (including __new__), optional for saved hosts. */
const CODING_REMOTE_PASSWORD_FIELD = "ssh_password";

const SSH_PROFILE_NEW = "__new__";

function controlFieldValue(data: Record<string, unknown> | undefined, fieldName: string): string {
    if (!data || !fieldName) return "";
    const raw = data[fieldName];
    if (raw === null || raw === undefined) return "";
    if (typeof raw === "string") return raw.trim();
    if (typeof raw === "number" || typeof raw === "boolean") return String(raw);
    return String(raw).trim();
}

function matchesEquals(value: string, expected: string | string[] | undefined): boolean {
    if (expected === undefined) return true;
    const list = Array.isArray(expected) ? expected : [expected];
    return list.some((item) => String(item) === value);
}

/**
 * Evaluate a visibleWhen rule against the current form data.
 * Missing / empty rules mean the field is always visible.
 */
export function matchesVisibleWhen(
    rule: AgentViewVisibleWhen | undefined | null,
    data: Record<string, unknown> | undefined,
): boolean {
    if (!rule || !rule.field) return true;
    const value = controlFieldValue(data, rule.field);

    if (rule.empty === true && value !== "") return false;
    if (rule.notEmpty === true && value === "") return false;

    if (rule.equals !== undefined && !matchesEquals(value, rule.equals)) {
        return false;
    }
    if (rule.notEquals !== undefined && matchesEquals(value, rule.notEquals)) {
        return false;
    }
    return true;
}

/**
 * Coding workflow fallback: when the remote variant is active, host/auth fields
 * are only needed for "新建连接" (__new__). Explicit visibleWhen on the field wins.
 */
export function isAgentViewFieldVisible(
    field: AgentViewField,
    data: Record<string, unknown> | undefined,
    variant?: AgentViewVariant,
): boolean {
    if (!field) return false;
    if (field.type === "hidden") return true; // still "present" for payload; not rendered by UI

    if (field.visibleWhen) {
        return matchesVisibleWhen(field.visibleWhen, data);
    }

    // Fallback for coding remote forms without server-side visible_when.
    if (variant?.id === "remote") {
        const profile = controlFieldValue(data, "ssh_profile");
        if (field.name === CODING_REMOTE_PASSWORD_FIELD) {
            // Optional session password whenever a host is chosen.
            return profile !== "";
        }
        if (CODING_REMOTE_NEW_ONLY_FIELDS.has(field.name)) {
            // Only for 新建连接; empty profile still hides clutter until user chooses.
            return profile === SSH_PROFILE_NEW;
        }
    }

    return true;
}

export function visibleFormFields(
    view: AgentView,
    variant: AgentViewVariant | undefined,
    data?: Record<string, unknown>,
): AgentViewField[] {
    if (view.type !== "form") return [];
    const combined = [...view.fields, ...(variant?.fields || [])];
    return combined.filter((field) => {
        if (field.type === "hidden") return true;
        return isAgentViewFieldVisible(field, data, variant);
    });
}

/** Fields rendered in the UI (excludes type=hidden routing/const fields). */
export function visibleInteractiveFormFields(
    view: AgentView,
    variant: AgentViewVariant | undefined,
    data?: Record<string, unknown>,
): AgentViewField[] {
    return visibleFormFields(view, variant, data).filter((field) => field.type !== "hidden");
}

/**
 * Fields that participate in client-side validation.
 * Includes visible interactive fields plus hidden fields that declare required/const constraints.
 */
export function formValidationFields(
    view: AgentView,
    variant: AgentViewVariant | undefined,
    data?: Record<string, unknown>,
): AgentViewField[] {
    return visibleFormFields(view, variant, data).filter((field) => {
        if (field.type === "hidden") {
            return field.required === true || field.constValue !== undefined;
        }
        return true;
    });
}
